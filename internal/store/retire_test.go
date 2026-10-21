package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/authn"
	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestRetirementClosesAnOpenSilence retires four workloads against
// Postgres and checks the agent-silence gap each retirement stores, by
// ADR-0019's derivation at the retirement instant (ADR-0023, issue #126):
// one whose agent has been silent past the allowance gets one gap from its
// last report's receipt to the retirement; one that reported within the
// allowance, one whose agent the latest heartbeat shows running through
// the silence, and one that never reported get none. The gap is written in
// the retirement's transaction and nothing derives it again: a report
// racing the retirement does not move the receipt.
func TestRetirementClosesAnOpenSilence(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := &fleet.Service{Store: s, Engine: &compiler.Engine{Store: s}, FlowWindow: time.Minute, Now: func() time.Time { return now }}
	octx := authn.NewContext(ctx, &authn.Principal{})

	silent, quiet, running, never := enrollWorkload(t, s), enrollWorkload(t, s), enrollWorkload(t, s), enrollWorkload(t, s)
	lastReport := now.Add(-10 * time.Minute)
	for id, at := range map[identity.WorkloadID]time.Time{silent: lastReport, quiet: now.Add(-30 * time.Second), running: lastReport} {
		if _, _, _, err := s.RecordFlowReport(ctx, id, at); err != nil {
			t.Fatal(err)
		}
	}
	// running's agent started an hour before its last report and sent a
	// heartbeat twenty seconds ago: one process ran through the silence.
	if err := s.RecordHeartbeat(ctx, running, registry.Heartbeat{Uptime: time.Hour}, now.Add(-20*time.Second)); err != nil {
		t.Fatal(err)
	}

	gaps := func(id identity.WorkloadID) []flowstore.GapRow {
		t.Helper()
		rows, err := s.Flows().ListGaps(ctx, flowstore.GapQuery{WorkloadIDs: []identity.WorkloadID{id}, Since: now.Add(-time.Hour), Until: now.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	for _, c := range []struct {
		name   string
		id     identity.WorkloadID
		closed bool
	}{{"silent", silent, true}, {"quiet", quiet, false}, {"running", running, false}, {"never reported", never, false}} {
		res, err := svc.Retire(octx, c.id)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !res.RetiredAt.Equal(now) {
			t.Fatalf("%s: retired at %v, want %v", c.name, res.RetiredAt, now)
		}
		rows := gaps(c.id)
		if !c.closed {
			if res.Closed != nil || len(rows) != 0 {
				t.Fatalf("%s: closed %+v, stored %+v; want no gap", c.name, res.Closed, rows)
			}
			continue
		}
		if len(rows) != 1 {
			t.Fatalf("%s: stored %+v, want one gap", c.name, rows)
		}
		g := rows[0]
		if g.Kind != innerwallv1.EvidenceGapKind_EVIDENCE_GAP_KIND_AGENT_SILENT || !g.From.Equal(lastReport) || !g.To.Equal(now) || g.Count != nil {
			t.Fatalf("%s: gap %+v, want agent_silent from %v to %v", c.name, g, lastReport, now)
		}
		if res.Closed == nil || !res.Closed.From.Equal(lastReport) || !res.Closed.To.Equal(now) {
			t.Fatalf("%s: reported closed %+v", c.name, res.Closed)
		}
	}

	// A report admitted just before the retirement committed lands after
	// it: the receipt does not move, so no second silence is derived.
	prev, _, advanced, err := s.RecordFlowReport(ctx, silent, now.Add(time.Minute))
	if err != nil || advanced || prev != nil {
		t.Fatalf("receipt after retirement = %v, %v, %v; want no advance", prev, advanced, err)
	}
	if w, _ := s.LookupWorkload(ctx, silent); w.LastFlowReportAt == nil || !w.LastFlowReportAt.Equal(lastReport) {
		t.Fatalf("receipt moved to %v", w.LastFlowReportAt)
	}
}

// TestRetiredWorkloadRefusesStreamWrites checks that every write a sync
// stream makes from what its agent sends, and renewal's record, refuses a
// retired workload with the instant, while an unknown workload is still
// unknown (ADR-0023). Labels are frozen too.
func TestRetiredWorkloadRefusesStreamWrites(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := &fleet.Service{Store: s, Engine: &compiler.Engine{Store: s}, Now: func() time.Time { return now }}
	id := enrollWorkload(t, s)
	if _, err := svc.Retire(authn.NewContext(ctx, &authn.Principal{}), id); err != nil {
		t.Fatal(err)
	}
	wantRetired := func(what string, err error) {
		t.Helper()
		var re *registry.RetiredError
		if !errors.As(err, &re) || re.ID != id || !re.RetiredAt.Equal(now) {
			t.Fatalf("%s: err = %v, want retired at %v", what, err, now)
		}
	}
	_, err := s.RecordFacts(ctx, id, &innerwallv1.HostFacts{Hostname: "h"}, now)
	wantRetired("facts", err)
	wantRetired("listening services", s.RecordListeningServices(ctx, id, nil, now))
	wantRetired("hello", s.RecordAgent(ctx, id, registry.AgentInfo{Version: "test"}, 1, nil, now))
	wantRetired("heartbeat", s.RecordHeartbeat(ctx, id, registry.Heartbeat{}, now))
	wantRetired("applied", s.RecordApplied(ctx, id, 1, innerwallv1.SyncState_SYNC_STATE_SYNCED, now))
	wantRetired("apply failed", s.RecordApplyFailed(ctx, id, "x", now))
	wantRetired("renewal", s.RecordRenewal(ctx, id, "2", now.Add(time.Hour), now, nil))
	wantRetired("labels", s.ReplaceWorkloadLabels(ctx, id, []registry.Label{{Key: "a", Value: "b"}}, ""))

	unknown, _ := identity.NewWorkloadID()
	if err := s.RecordHeartbeat(ctx, unknown, registry.Heartbeat{}, now); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("heartbeat of an unknown workload: err = %v", err)
	}
}
