// Package flowstore defines the FlowStore interface and its Postgres
// implementation. All flow reads and writes go through the interface; no other
// package issues SQL against flow tables. The schema is column-shaped so a
// columnar backend can be added behind the same interface when a real
// estate's scale demands it (ADR-0009, ADR-0019).
package flowstore

import (
	"context"
	"encoding/json"
	"net/netip"
	"sort"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// PeerKind says what a flow record's source address resolved to at ingest.
type PeerKind int32

// Peer kinds, in the order the schema encodes them.
const (
	// PeerUnknown is an address that matched no workload and no address
	// group; the peer key is the address itself.
	PeerUnknown PeerKind = 0
	// PeerWorkload is a managed workload; the peer key is its id.
	PeerWorkload PeerKind = 1
	// PeerAddressGroup is a named address group; the peer key is its id.
	PeerAddressGroup PeerKind = 2
)

// String names the kind for display.
func (k PeerKind) String() string {
	switch k {
	case PeerWorkload:
		return "workload"
	case PeerAddressGroup:
		return "group"
	case PeerUnknown:
		return "address"
	default:
		return "unknown"
	}
}

// Peer is the resolved remote end of a flow as captured at ingest. Labels
// is the workload's label set at that moment and is empty for the other
// kinds; it is stored with the row because labels and addresses change and
// a query-time join would rewrite what the operator saw (ADR-0019).
type Peer struct {
	Kind   PeerKind
	Key    string
	Labels map[string]string
}

// Record is one aggregated flow observation as the store keeps it: what
// the agent reported plus the resolved peer.
type Record struct {
	Peer            Peer
	SrcAddress      netip.Addr
	DstAddress      netip.Addr
	DstPort         uint16
	Protocol        innerwallv1.Protocol
	Direction       innerwallv1.Direction
	Decision        innerwallv1.PolicyDecision
	MatchedRuleID   string
	ConnectionCount uint64
	ByteCount       uint64
	FirstSeen       time.Time
	LastSeen        time.Time
	ProcessName     string
}

// Window is one reporting window from one workload, resolved and ready to
// store.
type Window struct {
	WorkloadID identity.WorkloadID
	Start      time.Time
	End        time.Time
	Records    []Record
}

// WindowRow is one stored flow_windows row.
type WindowRow struct {
	ID          int64
	WorkloadID  identity.WorkloadID
	WindowStart time.Time
	WindowEnd   time.Time
	Record
}

// WindowQuery selects a workload's windows in [Since, Until). A zero
// Decision means every decision; Limit bounds the rows returned, newest
// window first.
type WindowQuery struct {
	WorkloadID identity.WorkloadID
	Since      time.Time
	Until      time.Time
	Decision   innerwallv1.PolicyDecision
	Limit      int
}

// RollupQuery aggregates the windows of a set of workloads (a label scope,
// resolved by the caller) in [Since, Until) by resolved peer and service.
// A zero Decision means every decision.
type RollupQuery struct {
	WorkloadIDs []identity.WorkloadID
	Since       time.Time
	Until       time.Time
	Decision    innerwallv1.PolicyDecision
}

// RollupRow is one group of a rollup: a resolved peer talking to one
// service (port and protocol) with one decision, across every workload in
// scope. Workloads counts the distinct workloads that saw it.
type RollupRow struct {
	Peer            Peer
	DstPort         uint16
	Protocol        innerwallv1.Protocol
	Decision        innerwallv1.PolicyDecision
	Workloads       int64
	ConnectionCount uint64
	ByteCount       uint64
	FirstSeen       time.Time
	LastSeen        time.Time
}

// Total is the cumulative record of one flow key since it was first seen.
// It is upserted at ingest and never pruned, so "since first seen" verdicts
// come from one row rather than a scan of the windows (ADR-0019).
type Total struct {
	WorkloadID      identity.WorkloadID
	Peer            Peer
	DstPort         uint16
	Protocol        innerwallv1.Protocol
	Direction       innerwallv1.Direction
	Decision        innerwallv1.PolicyDecision
	MatchedRuleID   string
	FirstSeen       time.Time
	LastSeen        time.Time
	ConnectionCount uint64
	ByteCount       uint64
	WindowCount     uint64
}

// FlowStore is the only path to flow data (ADR-0009). Every method is
// implemented with hand-written SQL in internal/store/queries (ADR-0006).
type FlowStore interface {
	// WriteWindow stores every record of w and folds each into the totals,
	// atomically. It returns the number of records stored.
	WriteWindow(ctx context.Context, w Window) (int, error)
	// ListWindows returns a workload's stored windows, newest first.
	ListWindows(ctx context.Context, q WindowQuery) ([]WindowRow, error)
	// Rollup groups the windows of a workload set by peer and service.
	Rollup(ctx context.Context, q RollupQuery) ([]RollupRow, error)
	// ListTotals returns a workload's cumulative flow keys, most recently
	// seen first. A zero decision means every decision.
	ListTotals(ctx context.Context, id identity.WorkloadID, decision innerwallv1.PolicyDecision) ([]Total, error)
	// PruneWindows deletes windows that started before horizon, in bounded
	// batches. It reports how many rows it deleted and whether it ran at
	// all: only one replica prunes at a time, and a replica that finds the
	// retention lock held by another returns ran == false and deleted == 0.
	PruneWindows(ctx context.Context, horizon time.Time) (deleted int64, ran bool, err error)
}

// TotalsKey identifies one flow_totals row.
type TotalsKey struct {
	PeerKind  PeerKind
	PeerKey   string
	DstPort   uint16
	Protocol  innerwallv1.Protocol
	Direction innerwallv1.Direction
	Decision  innerwallv1.PolicyDecision
}

// Key returns the totals key of r.
func (r *Record) Key() TotalsKey {
	return TotalsKey{PeerKind: r.Peer.Kind, PeerKey: r.Peer.Key, DstPort: r.DstPort, Protocol: r.Protocol, Direction: r.Direction, Decision: r.Decision}
}

// AggregateTotals folds the records of one window into one record per
// totals key, which is the shape the upsert needs: a statement may touch a
// given totals row once, and several records in a window can share a key
// when two source addresses resolve to the same peer. Counts add, the
// first-seen instant is the earliest, the last-seen the latest, and the
// label snapshot, matched rule, and addresses come from the latest-seen
// record. The result is in a deterministic order.
func AggregateTotals(records []Record) []Record {
	index := map[TotalsKey]int{}
	var out []Record
	for i := range records {
		r := records[i]
		key := r.Key()
		j, ok := index[key]
		if !ok {
			index[key] = len(out)
			out = append(out, r)
			continue
		}
		merged := &out[j]
		merged.ConnectionCount += r.ConnectionCount
		merged.ByteCount += r.ByteCount
		if r.FirstSeen.Before(merged.FirstSeen) {
			merged.FirstSeen = r.FirstSeen
		}
		if !r.LastSeen.Before(merged.LastSeen) {
			merged.LastSeen = r.LastSeen
			merged.Peer = r.Peer
			merged.MatchedRuleID = r.MatchedRuleID
			merged.SrcAddress = r.SrcAddress
			merged.DstAddress = r.DstAddress
			merged.ProcessName = r.ProcessName
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return lessKey(out[i].Key(), out[j].Key()) })
	return out
}

func lessKey(a, b TotalsKey) bool {
	switch {
	case a.PeerKind != b.PeerKind:
		return a.PeerKind < b.PeerKind
	case a.PeerKey != b.PeerKey:
		return a.PeerKey < b.PeerKey
	case a.DstPort != b.DstPort:
		return a.DstPort < b.DstPort
	case a.Protocol != b.Protocol:
		return a.Protocol < b.Protocol
	case a.Direction != b.Direction:
		return a.Direction < b.Direction
	default:
		return a.Decision < b.Decision
	}
}

// encodeLabels serializes a label snapshot as a JSON object with sorted
// keys; nil and empty both encode as {}.
func encodeLabels(labels map[string]string) []byte {
	if len(labels) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// decodeLabels is the inverse of encodeLabels; malformed input yields an
// empty snapshot rather than an error, because a display must never fail
// on one row.
func decodeLabels(b []byte) map[string]string {
	out := map[string]string{}
	if len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}
