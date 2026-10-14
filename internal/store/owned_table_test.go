package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/readmodel/readmodeltest"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestOwnedTableStateFollowsHeartbeatsAndAcks covers the owned table's
// recorded state (issue #64): a heartbeat that says absent records the
// instant it first did and later ones keep it; a heartbeat that does not
// know leaves the state as it was, so a failed read never turns a removed
// table healthy; an applied acknowledgement means the table was installed;
// a heartbeat that says present clears the instant. Each reads back
// through the registry and both read-model paths.
func TestOwnedTableStateFollowsHeartbeatsAndAcks(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id := enrollWorkload(t, s)
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	reads := &readmodel.Reader{Store: s, Flows: s.Flows()}

	check := func(what string, want innerwallv1.OwnedTableState, since *time.Time) {
		t.Helper()
		w, err := s.LookupWorkload(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		detail, err := reads.GetWorkload(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		page, err := reads.ListWorkloads(ctx, readmodel.WorkloadsRequest{})
		if err != nil || len(page.Workloads) != 1 {
			t.Fatalf("page = %v, %v", page, err)
		}
		for path, got := range map[string]struct {
			st    innerwallv1.OwnedTableState
			since *time.Time
		}{
			"registry": {w.OwnedTable, w.OwnedTableAbsentSince},
			"detail":   {detail.Health.OwnedTable, detail.Health.OwnedTableAbsentSince},
			"page":     {page.Workloads[0].Health.OwnedTable, page.Workloads[0].Health.OwnedTableAbsentSince},
		} {
			if got.st != want || (since == nil) != (got.since == nil) || (since != nil && !got.since.Equal(*since)) {
				t.Fatalf("%s: %s state = %v since %v, want %v since %v", what, path, got.st, got.since, want, since)
			}
		}
	}
	heartbeat := func(st innerwallv1.OwnedTableState, now time.Time) {
		t.Helper()
		if err := s.RecordHeartbeat(ctx, id, registry.Heartbeat{OwnedTable: st}, now); err != nil {
			t.Fatal(err)
		}
	}

	check("fresh", innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED, nil)
	heartbeat(innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT, at)
	check("present", innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT, nil)

	removed := at.Add(30 * time.Second)
	heartbeat(innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT, removed)
	heartbeat(innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT, removed.Add(30*time.Second))
	check("absent", innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT, &removed)
	heartbeat(innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED, removed.Add(time.Minute))
	check("unknown after absent", innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT, &removed)

	if err := s.RecordApplied(ctx, id, 3, innerwallv1.SyncState_SYNC_STATE_SYNCED, removed.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	check("after an applied ack", innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT, nil)

	heartbeat(innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT, removed.Add(3*time.Minute))
	heartbeat(innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT, removed.Add(4*time.Minute))
	check("present again", innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT, nil)

	// An agent that predates the field never reported a state; an applied
	// ack does not invent one for it.
	other := enrollWorkload(t, s)
	if err := s.RecordApplied(ctx, other, 1, innerwallv1.SyncState_SYNC_STATE_SYNCED, at); err != nil {
		t.Fatal(err)
	}
	if w, err := s.LookupWorkload(ctx, other); err != nil || w.OwnedTable != innerwallv1.OwnedTableState_OWNED_TABLE_STATE_UNSPECIFIED {
		t.Fatalf("old agent after ack = %v, %v", w, err)
	}
}

// TestRemovedTableRanksAndCounts covers the attention a removed owned table
// draws in the fleet reads (issue #96), against the seeded fleet of a
// synced web-1, a degraded db-1, and an offline cache-1. The kill switch on
// web-1 leaves its stored state synced, the agent's honest record, yet the
// fleet order ranks it with the degraded workloads, at the top, and the
// counts put it in removed and not in synced; the apply that reinstalls the
// table returns both reads to the baseline.
func TestRemovedTableRanksAndCounts(t *testing.T) {
	s := storetest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := storetest.SeedFleet(t, s)
	reads := &readmodel.Reader{Store: s, Flows: s.Flows(), Now: func() time.Time { return f.Now }}

	order := func(what string, want ...string) {
		t.Helper()
		page, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for i := range page {
			got = append(got, page[i].Hostname)
			if r := readmodeltest.SyncRank(page[i].SyncState, page[i].OwnedTable); page[i].SyncRank != r {
				t.Fatalf("%s: %s ranked %d by the statement, %d by the double", what, page[i].Hostname, page[i].SyncRank, r)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: fleet order = %v, want %v", what, got, want)
		}
	}
	counts := func(what string, removed, synced int64) {
		t.Helper()
		c, err := reads.CountWorkloads(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if c.Total != 3 || c.Removed != removed || c.BySync[innerwallv1.SyncState_SYNC_STATE_SYNCED] != synced ||
			c.BySync[innerwallv1.SyncState_SYNC_STATE_DEGRADED] != 1 || c.BySync[innerwallv1.SyncState_SYNC_STATE_OFFLINE] != 1 {
			t.Fatalf("%s: counts = %+v, want removed %d synced %d", what, c, removed, synced)
		}
	}

	order("baseline", "db-1", "cache-1", "web-1")
	counts("baseline", 0, 1)

	// The kill switch: web-1's next heartbeat says the table is gone.
	if err := s.RecordHeartbeat(ctx, f.Web, registry.Heartbeat{OwnedTable: innerwallv1.OwnedTableState_OWNED_TABLE_STATE_ABSENT}, f.Now); err != nil {
		t.Fatal(err)
	}
	w, err := s.LookupWorkload(ctx, f.Web)
	if err != nil || w.SyncState != innerwallv1.SyncState_SYNC_STATE_SYNCED {
		t.Fatalf("web-1 after the kill switch = %+v, %v; the stored sync state is the agent's and stays", w, err)
	}
	// Ranked with db-1, the degraded one, and seen more recently than it.
	order("removed", "web-1", "db-1", "cache-1")
	counts("removed", 1, 0)

	// A cursor walk of one row per page serves each workload once in the
	// same order, so the rank the cursor carries agrees with the statement.
	var walked []string
	var after *readmodel.WorkloadCursor
	for range 4 {
		page, err := s.ListWorkloadPage(ctx, readmodel.WorkloadPageQuery{Limit: 1, After: after})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		walked = append(walked, page[0].Hostname)
		after = &readmodel.WorkloadCursor{SyncRank: page[0].SyncRank, SeenKey: page[0].SeenKey, ID: page[0].ID}
	}
	if fmt.Sprint(walked) != "[web-1 db-1 cache-1]" {
		t.Fatalf("cursor walk = %v", walked)
	}

	// Restored: the apply reinstalls the table.
	pol, err := s.GetWorkloadPolicy(ctx, f.Web)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplied(ctx, f.Web, pol.GetVersion(), innerwallv1.SyncState_SYNC_STATE_SYNCED, f.Now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	order("restored", "db-1", "cache-1", "web-1")
	counts("restored", 0, 1)
}
