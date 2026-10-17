package store_test

import (
	"context"
	"testing"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestFlowReportReceiptAndLiveTelemetry covers the receipt of flow reports
// (issue #61): the instant is recorded, never moved backwards, and left by
// every sync-path write, the offline mark included; both read-model paths
// carry it and judge the telemetry live within two flow windows of the
// read, and not after.
func TestFlowReportReceiptAndLiveTelemetry(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id := enrollWorkload(t, s)
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	now := at
	reads := &readmodel.Reader{Store: s, Flows: s.Flows(), FlowWindow: time.Minute, Now: func() time.Time { return now }}

	check := func(what string, want *time.Time, live bool) {
		t.Helper()
		detail, err := reads.GetWorkload(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		page, err := reads.ListWorkloads(ctx, readmodel.WorkloadsRequest{})
		if err != nil || len(page.Workloads) != 1 {
			t.Fatalf("page = %v, %v", page, err)
		}
		for path, h := range map[string]readmodel.Health{"detail": detail.Health, "page": page.Workloads[0].Health} {
			got := h.LastFlowReportAt
			if (want == nil) != (got == nil) || (want != nil && !got.Equal(*want)) || h.TelemetryLive != live {
				t.Fatalf("%s: %s last report %v live %v, want %v live %v", what, path, got, h.TelemetryLive, want, live)
			}
		}
	}

	check("fresh", nil, false)
	prev, proc, advanced, err := s.RecordFlowReport(ctx, id, at)
	if err != nil || prev != nil || proc != nil || !advanced {
		t.Fatalf("first receipt = %v, %v, %v, %v; want nil prev, no process, advanced", prev, proc, advanced, err)
	}
	// A stale receipt, handled out of order, replaces nothing and hands
	// back no previous instant to derive a silence from.
	prev, _, advanced, err = s.RecordFlowReport(ctx, id, at.Add(-time.Minute))
	if err != nil || prev != nil || advanced {
		t.Fatalf("stale receipt = %v, %v, %v; want no advance", prev, advanced, err)
	}
	check("received", &at, true)
	// A later receipt returns the one it replaced: the bound of the
	// silence between them (migration 00015).
	later := at.Add(10 * time.Minute)
	prev, _, advanced, err = s.RecordFlowReport(ctx, id, later)
	if err != nil || prev == nil || !prev.Equal(at) || !advanced {
		t.Fatalf("second receipt = %v, %v, %v; want prev %v", prev, advanced, err, at)
	}
	now = later
	at = later
	check("advanced", &later, true)

	// The sync stream goes away; the reports keep the instant they set.
	if err := s.SetSyncState(ctx, id, innerwallv1.SyncState_SYNC_STATE_OFFLINE, "", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	now = at.Add(2 * time.Minute)
	check("offline, two windows on", &at, true)
	now = at.Add(2*time.Minute + time.Second)
	check("offline, past two windows", &at, false)

	w, err := s.LookupWorkload(ctx, id)
	if err != nil || w.LastFlowReportAt == nil || !w.LastFlowReportAt.Equal(at) {
		t.Fatalf("registry = %v, %v", w, err)
	}

	// A reader with no window judges nothing live.
	blind := &readmodel.Reader{Store: s, Flows: s.Flows(), Now: func() time.Time { return at }}
	if d, err := blind.GetWorkload(ctx, id); err != nil || d.Health.TelemetryLive {
		t.Fatalf("windowless reader = %v, %v", d, err)
	}
}

// TestFlowReportCarriesAgentProcess covers the heartbeat pair the silence
// derivation reads (migration 00016): a heartbeat records its receipt and
// the start of the process it came from together, the receipt statement
// hands both back under its lock, and no other sync-path write moves
// either — a new process's Hello must not lend the old process's start to
// a fresh instant.
func TestFlowReportCarriesAgentProcess(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id := enrollWorkload(t, s)
	hb := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if err := s.RecordHeartbeat(ctx, id, registry.Heartbeat{Uptime: 2 * time.Hour}, hb); err != nil {
		t.Fatal(err)
	}
	want := registry.AgentProcess{StartedAt: hb.Add(-2 * time.Hour), HeartbeatAt: hb}
	// Later sync-path writes refresh last-seen, never the pair.
	if err := s.RecordAgent(ctx, id, registry.AgentInfo{Version: "v0.4.0"}, 0, hb.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSyncState(ctx, id, innerwallv1.SyncState_SYNC_STATE_SYNCED, "", hb.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	_, proc, advanced, err := s.RecordFlowReport(ctx, id, hb.Add(30*time.Second))
	if err != nil || !advanced || proc == nil || !proc.StartedAt.Equal(want.StartedAt) || !proc.HeartbeatAt.Equal(want.HeartbeatAt) {
		t.Fatalf("receipt process = %+v, %v, %v; want %+v", proc, advanced, err, want)
	}
	// A restarted process's heartbeat replaces both at once.
	restart := hb.Add(time.Hour)
	if err := s.RecordHeartbeat(ctx, id, registry.Heartbeat{Uptime: 30 * time.Second}, restart); err != nil {
		t.Fatal(err)
	}
	_, proc, _, err = s.RecordFlowReport(ctx, id, restart.Add(time.Minute))
	if err != nil || proc == nil || !proc.StartedAt.Equal(restart.Add(-30*time.Second)) || !proc.HeartbeatAt.Equal(restart) {
		t.Fatalf("after restart, process = %+v, %v", proc, err)
	}
}
