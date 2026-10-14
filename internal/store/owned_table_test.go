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
