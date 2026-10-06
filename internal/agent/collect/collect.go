// Package collect is the agent's flow collection loop: a Source observes
// connections on the host, an Aggregator folds them into one record per
// (source, destination, port, protocol, decision, rule) over the reporting
// window the control plane configures, holding at most a bounded number
// of distinct keys, a bounded Buffer holds closed windows until the
// Reporter ships them over ReportFlows, and overflow at either bound
// drops what does not fit and records the loss as an evidence gap.
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

// DefaultWindowKeys bounds the distinct keys one open window holds unless
// configured otherwise: the closed-window buffer's figure, so one window
// can never hold more than the buffer would keep (ADR-0011).
const DefaultWindowKeys = DefaultBufferRecords

// Aggregator folds observations into the open window. The window holds at
// most a bounded number of distinct keys: an observation for a new key
// past the bound is dropped, and the window records the loss as a
// window_overflow gap from the first drop to the window's close, with the
// observations dropped. Observations for keys already held still fold in.
type Aggregator struct {
	// Gaps receives the overflow gap when a window closes; nil records
	// nothing.
	Gaps *Gaps

	mu      sync.Mutex
	start   time.Time
	records map[Key]*record
	maxKeys int
	// The open window's overflow: observations dropped and the first
	// drop's instant.
	dropped   uint64
	firstDrop time.Time
}

// NewAggregator opens a window starting at start, holding at most
// DefaultWindowKeys keys.
func NewAggregator(start time.Time) *Aggregator {
	return NewBoundedAggregator(start, DefaultWindowKeys)
}

// NewBoundedAggregator opens a window starting at start holding at most
// maxKeys distinct keys; DefaultWindowKeys when maxKeys is not positive.
func NewBoundedAggregator(start time.Time, maxKeys int) *Aggregator {
	if maxKeys <= 0 {
		maxKeys = DefaultWindowKeys
	}
	return &Aggregator{start: start, records: map[Key]*record{}, maxKeys: maxKeys}
}

// Add folds one observation into the open window, or drops it when its
// key is new and the window is full.
func (a *Aggregator) Add(o Observation) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := Key{Src: o.Src, Dst: o.Dst, DstPort: o.DstPort, Protocol: o.Protocol, Decision: o.Decision, RuleID: o.RuleID}
	r, ok := a.records[key]
	if !ok {
		if len(a.records) >= a.maxKeys {
			if a.dropped == 0 || o.At.Before(a.firstDrop) {
				a.firstDrop = o.At
			}
			a.dropped++
			return
		}
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

// Dropped returns the observations the open window has dropped so far.
func (a *Aggregator) Dropped() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dropped
}

// Close ends the open window at end, returns it, and opens the next one
// starting at end. A window that dropped observations records its
// window_overflow gap now: from the first drop to end, because every new
// key in that interval was lost.
func (a *Aggregator) Close(end time.Time) *Window {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dropped > 0 {
		a.Gaps.Record(Gap{Kind: GapWindowOverflow, Source: GapNoSource, From: a.firstDrop, To: end, Count: a.dropped, HasCount: true})
		a.dropped = 0
	}
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
