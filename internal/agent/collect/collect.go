// Package collect is the agent's flow collection loop: a Source observes
// connections on the host, an Aggregator folds them into one record per
// (source, destination, port, protocol, decision, rule) over the reporting
// window the control plane configures, a bounded Buffer holds closed
// windows until the Reporter ships them over ReportFlows, and overflow
// drops the oldest windows and counts what it dropped for the heartbeat.
// Per-connection records never leave the host (ADR-0009); the conntrack
// source is the first implementation behind the Source interface, and an
// eBPF source is a later additive one (ADR-0003).
package collect

import (
	"context"
	"net/netip"
	"sort"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Observation is one event from a Source: a connection seen inbound to
// this host. A source reports the start of a connection with Connections
// set and its byte total, once known, with Bytes set; an aggregator adds
// both into the record for the connection's key.
type Observation struct {
	At          time.Time
	Src         netip.Addr
	Dst         netip.Addr
	DstPort     uint16
	Protocol    innerwallv1.Protocol
	Decision    innerwallv1.PolicyDecision
	RuleID      string
	Connections uint64
	Bytes       uint64
	ProcessName string
}

// Source emits observations until ctx ends. It returns an error when it
// cannot observe at all (the collector restarts it with backoff); an
// observation that cannot be classified is dropped by the source, never
// reported as an error.
type Source interface {
	Run(ctx context.Context, emit func(Observation)) error
}

// Key is what records aggregate on within a window. The ephemeral source
// port is deliberately absent (ADR-0015).
type Key struct {
	Src      netip.Addr
	Dst      netip.Addr
	DstPort  uint16
	Protocol innerwallv1.Protocol
	Decision innerwallv1.PolicyDecision
	RuleID   string
}

type record struct {
	connections uint64
	bytes       uint64
	first, last time.Time
	process     string
}

// Window is one closed reporting window: its bounds and the aggregated
// records, in a deterministic order.
type Window struct {
	Start   time.Time
	End     time.Time
	Records []*innerwallv1.FlowRecord
}

// Len returns the number of records.
func (w *Window) Len() int { return len(w.Records) }

// Aggregator folds observations into the open window.
type Aggregator struct {
	mu      sync.Mutex
	start   time.Time
	records map[Key]*record
}

// NewAggregator opens a window starting at start.
func NewAggregator(start time.Time) *Aggregator {
	return &Aggregator{start: start, records: map[Key]*record{}}
}

// Add folds one observation into the open window.
func (a *Aggregator) Add(o Observation) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := Key{Src: o.Src, Dst: o.Dst, DstPort: o.DstPort, Protocol: o.Protocol, Decision: o.Decision, RuleID: o.RuleID}
	r, ok := a.records[key]
	if !ok {
		r = &record{first: o.At, last: o.At}
		a.records[key] = r
	}
	r.connections += o.Connections
	r.bytes += o.Bytes
	if o.At.Before(r.first) {
		r.first = o.At
	}
	if o.At.After(r.last) {
		r.last = o.At
	}
	if o.ProcessName != "" {
		r.process = o.ProcessName
	}
}

// Len returns the number of distinct keys in the open window.
func (a *Aggregator) Len() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.records)
}

// Close ends the open window at end, returns it, and opens the next one
// starting at end.
func (a *Aggregator) Close(end time.Time) *Window {
	a.mu.Lock()
	defer a.mu.Unlock()
	w := &Window{Start: a.start, End: end, Records: make([]*innerwallv1.FlowRecord, 0, len(a.records))}
	keys := make([]Key, 0, len(a.records))
	for k := range a.records {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return lessKey(keys[i], keys[j]) })
	for _, k := range keys {
		r := a.records[k]
		w.Records = append(w.Records, &innerwallv1.FlowRecord{
			SrcAddress:      k.Src.String(),
			DstAddress:      k.Dst.String(),
			DstPort:         uint32(k.DstPort),
			Protocol:        k.Protocol,
			Direction:       innerwallv1.Direction_DIRECTION_INBOUND,
			Decision:        k.Decision,
			MatchedRuleId:   k.RuleID,
			ConnectionCount: r.connections,
			ByteCount:       r.bytes,
			FirstSeen:       timestamppb.New(r.first),
			LastSeen:        timestamppb.New(r.last),
			ProcessName:     r.process,
		})
	}
	a.start = end
	a.records = map[Key]*record{}
	return w
}

func lessKey(a, b Key) bool {
	switch {
	case a.Src != b.Src:
		return a.Src.Less(b.Src)
	case a.Dst != b.Dst:
		return a.Dst.Less(b.Dst)
	case a.DstPort != b.DstPort:
		return a.DstPort < b.DstPort
	case a.Protocol != b.Protocol:
		return a.Protocol < b.Protocol
	case a.Decision != b.Decision:
		return a.Decision < b.Decision
	default:
		return a.RuleID < b.RuleID
	}
}
