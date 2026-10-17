package ingest_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/ingest"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestSilenceGapEndToEnd drives the derivation through the real store: the
// locking receipt statement hands the silence to the deriver, the gap
// lands in flow_gaps as agent_silent through the FlowStore, the gaps read
// returns it named for the surface, and steady reporting after the outage
// adds nothing (issue #114, ADR-0019 as amended).
func TestSilenceGapEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)

	var now time.Time
	svc := &ingest.Service{Directory: s, Flows: s.Flows(), Clock: s, Reports: s,
		FlowWindow: time.Minute, Now: func() time.Time { return now }}
	report := func(start, at time.Time) ingest.Result {
		t.Helper()
		now = at
		res, err := svc.Ingest(ctx, f.DB, &innerwallv1.ReportFlowsRequest{
			WindowStart: timestamppb.New(start), WindowEnd: timestamppb.New(start.Add(time.Minute)),
			SentAt: timestamppb.New(at),
		})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	gapsIn := func(from, to time.Time) []readmodel.Gap {
		t.Helper()
		reads := &readmodel.Reader{Store: s, Flows: s.Flows(), FlowWindow: time.Minute}
		res, err := reads.Gaps(ctx, readmodel.GapsRequest{From: from, To: to, Workload: &f.DB})
		if err != nil {
			t.Fatal(err)
		}
		var out []readmodel.Gap
		for _, g := range res.Gaps {
			if g.Kind == innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT {
				out = append(out, g)
			}
		}
		return out
	}

	// Settle the receipt on a known instant, clear of whatever the seed
	// recorded.
	base := f.Now.Add(time.Hour)
	report(base.Add(-time.Minute), base)

	// The agent process stops at base and restarts twenty minutes later;
	// its first window closes a minute after that.
	restart := base.Add(20 * time.Minute)
	if res := report(restart, restart.Add(time.Minute)); res.Derived != 1 {
		t.Fatalf("report after the outage derived %d gaps, want 1", res.Derived)
	}
	gaps := gapsIn(base.Add(-time.Hour), restart.Add(time.Hour))
	if len(gaps) != 1 {
		t.Fatalf("agent_silent gaps = %+v, want one", gaps)
	}
	g := gaps[0]
	if !g.From.Equal(base) || !g.To.Equal(restart) {
		t.Fatalf("gap [%v, %v), want [%v, %v)", g.From, g.To, base, restart)
	}
	if g.Count != nil || g.Source != innerwallv1.EvidenceSource_EVIDENCE_SOURCE_UNSPECIFIED {
		t.Fatalf("gap = %+v, want no count and no source", g)
	}
	if name := readmodel.GapKindName(g.Kind); name != "agent_silent" {
		t.Fatalf("kind name = %q", name)
	}

	// Steady reporting, and one quick restart inside the allowance: no
	// further gaps anywhere in the range.
	last := restart.Add(time.Minute)
	for i := range 3 {
		if res := report(last.Add(time.Duration(i)*time.Minute), last.Add(time.Duration(i+1)*time.Minute)); res.Derived != 0 {
			t.Fatalf("steady report %d derived a gap", i)
		}
	}
	quick := last.Add(3 * time.Minute).Add(20 * time.Second) // restarted 20s after the last window closed
	if res := report(quick, quick.Add(time.Minute)); res.Derived != 0 {
		t.Fatal("quick restart derived a gap")
	}
	if gaps := gapsIn(base.Add(-time.Hour), quick.Add(time.Hour)); len(gaps) != 1 {
		t.Fatalf("agent_silent gaps after steady reporting = %+v, want still one", gaps)
	}
}

// TestQuietWorkloadSilenceEndToEnd is issue #144 through the real store,
// heartbeats recorded as the gateway records them: an hour of a quiet
// workload whose agent ships only the windows holding a record, its sync
// stream heartbeating every 30s, derives nothing; then the agent process
// stops for ten minutes and the restarted process's first report derives
// the outage's gap with the INN-72 bounds, even though the newest
// heartbeat on record is still the dead process's; then a quick restart
// derives nothing.
func TestQuietWorkloadSilenceEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)

	var now time.Time
	svc := &ingest.Service{Directory: s, Flows: s.Flows(), Clock: s, Reports: s,
		FlowWindow: time.Minute, Now: func() time.Time { return now }}
	report := func(start, at time.Time) ingest.Result {
		t.Helper()
		now = at
		res, err := svc.Ingest(ctx, f.DB, &innerwallv1.ReportFlowsRequest{
			WindowStart: timestamppb.New(start), WindowEnd: timestamppb.New(start.Add(time.Minute)),
			SentAt: timestamppb.New(at),
			Records: []*innerwallv1.FlowRecord{{
				SrcAddress: "10.0.0.99", DstAddress: "10.0.0.10", DstPort: 22,
				Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND,
				Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ConnectionCount: 1,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// heartbeats records the heartbeats of a process started at started,
	// every 30s from *next until at.
	heartbeats := func(started time.Time, next *time.Time, at time.Time) {
		t.Helper()
		for ; !next.After(at); *next = next.Add(30 * time.Second) {
			if err := s.RecordHeartbeat(ctx, f.DB, registry.Heartbeat{Uptime: next.Sub(started).Truncate(time.Second)}, *next); err != nil {
				t.Fatal(err)
			}
		}
	}
	silent := func(from, to time.Time) []readmodel.Gap {
		t.Helper()
		reads := &readmodel.Reader{Store: s, Flows: s.Flows(), FlowWindow: time.Minute}
		res, err := reads.Gaps(ctx, readmodel.GapsRequest{From: from, To: to, Workload: &f.DB})
		if err != nil {
			t.Fatal(err)
		}
		var out []readmodel.Gap
		for _, g := range res.Gaps {
			if g.Kind == innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT {
				out = append(out, g)
			}
		}
		return out
	}

	started := f.Now.Add(time.Hour)
	base := started.Add(2 * time.Hour)
	hb := started.Add(30 * time.Second)
	heartbeats(started, &hb, base)
	report(base.Add(-time.Minute), base)

	// The quiet hour: 58 windows, 39 with a record, the cadence the
	// issue's journal showed.
	const pattern = "xx..xxx...xxxx.xx....xxxxx..xxx.xxxx...xxxxx..xx.xxxxxxxxx"
	var last time.Time
	for i := range pattern {
		start := base.Add(time.Duration(i) * time.Minute)
		end := start.Add(time.Minute)
		heartbeats(started, &hb, end)
		if pattern[i] != 'x' {
			continue
		}
		last = end.Add(500 * time.Millisecond)
		if res := report(start, last); res.Derived != 0 {
			t.Fatalf("quiet window %d derived a gap", i)
		}
	}
	if gaps := silent(base.Add(-time.Hour), last.Add(time.Hour)); len(gaps) != 0 {
		t.Fatalf("a quiet, running agent's hour holds agent_silent gaps %+v", gaps)
	}

	// A real silence: the process dies 25s after its last report, its
	// last heartbeat before that; the restarted process's first window
	// closes, and is reported, before its first heartbeat lands.
	heartbeats(started, &hb, last.Add(25*time.Second))
	restart := last.Add(10 * time.Minute)
	if res := report(restart, restart.Add(time.Minute)); res.Derived != 1 {
		t.Fatalf("report after the outage derived %d gaps, want 1", res.Derived)
	}
	gaps := silent(base.Add(-time.Hour), restart.Add(time.Hour))
	if len(gaps) != 1 || !gaps[0].From.Equal(last) || !gaps[0].To.Equal(restart) {
		t.Fatalf("agent_silent gaps = %+v, want [%v, %v)", gaps, last, restart)
	}

	// The restarted process heartbeats; a quick restart of it (20s down)
	// stays within the allowance and adds nothing.
	hb = restart.Add(30 * time.Second)
	heartbeats(restart, &hb, restart.Add(2*time.Minute))
	report(restart.Add(time.Minute), restart.Add(2*time.Minute))
	quick := restart.Add(2*time.Minute + 20*time.Second)
	if res := report(quick, quick.Add(time.Minute)); res.Derived != 0 {
		t.Fatal("quick restart derived a gap")
	}
	if gaps := silent(base.Add(-time.Hour), quick.Add(time.Hour)); len(gaps) != 1 {
		t.Fatalf("agent_silent gaps after the quick restart = %+v, want still one", gaps)
	}
}

// TestWriteGapsStoresOnce covers the server-side write the derivation
// uses: at-least-once delivery of the same interval lands one row, and the
// row prunes with the others once it ended before the horizon.
func TestWriteGapsStoresOnce(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)
	flows := s.Flows()
	gap := flowstore.Gap{
		Kind: innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT,
		From: f.Now.Add(-30 * time.Minute), To: f.Now.Add(-10 * time.Minute),
	}
	for range 2 {
		if err := flows.WriteGaps(ctx, f.DB, []flowstore.Gap{gap}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := flows.ListGaps(ctx, flowstore.GapQuery{WorkloadIDs: []identity.WorkloadID{f.DB}, Since: f.Now.Add(-time.Hour), Until: f.Now})
	if err != nil {
		t.Fatal(err)
	}
	var silent []flowstore.GapRow
	for _, r := range rows {
		if r.Kind == innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT {
			silent = append(silent, r)
		}
	}
	if len(silent) != 1 || !silent[0].From.Equal(gap.From) || !silent[0].To.Equal(gap.To) {
		t.Fatalf("stored agent_silent rows = %+v, want the one interval once", silent)
	}
	deleted, ran, err := flows.PruneWindows(ctx, f.Now)
	if err != nil || !ran {
		t.Fatalf("prune: %d, %v, %v", deleted, ran, err)
	}
	rows, err = flows.ListGaps(ctx, flowstore.GapQuery{WorkloadIDs: []identity.WorkloadID{f.DB}, Since: f.Now.Add(-2 * time.Hour), Until: f.Now})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Kind == innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT {
			t.Fatalf("agent_silent row survived retention: %+v", r)
		}
	}
}
