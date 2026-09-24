package fleet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/fleet/fleettest"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/readmodel/readmodeltest"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// TestRequestReconnect covers the three outcomes of a directed reconnect:
// an unknown workload and an offline agent are refused with nothing
// fired, and any other workload has the directive fired and gets back its
// snapshot instant as it stood, which the domain never writes.
func TestRequestReconnect(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	seen := now.Add(-3 * time.Minute)
	sent := now.Add(-2 * time.Minute)
	for i := range f.store.Workloads {
		w := &f.store.Workloads[i]
		switch w.ID {
		case f.web:
			w.SyncState, w.LastSeenAt, w.LastSnapshotSentAt = innerwallv1.SyncState_SYNC_STATE_SYNCED, &seen, &sent
		case f.db:
			w.SyncState, w.LastSeenAt = innerwallv1.SyncState_SYNC_STATE_OFFLINE, &seen
		case f.cache:
			// Never connected: offline, never seen.
			w.SyncState = innerwallv1.SyncState_SYNC_STATE_OFFLINE
		}
	}
	directives := &fleettest.Directives{}
	f.svc.Reads = &readmodel.Reader{Store: f.store, Flows: &readmodeltest.MemFlows{}, Now: func() time.Time { return now }}
	f.svc.Directives = directives
	ctx := context.Background()

	unknown, _ := identity.NewWorkloadID()
	if _, err := f.svc.RequestReconnect(ctx, unknown); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("unknown: err = %v", err)
	}

	_, err := f.svc.RequestReconnect(ctx, f.db)
	var off *fleet.AgentOfflineError
	if !errors.As(err, &off) || !errors.Is(err, fleet.ErrAgentOffline) || off.LastSeenAt == nil || !off.LastSeenAt.Equal(seen) {
		t.Fatalf("offline: err = %v", err)
	}
	_, err = f.svc.RequestReconnect(ctx, f.cache)
	if !errors.As(err, &off) || off.LastSeenAt != nil {
		t.Fatalf("never seen: err = %v", err)
	}
	if got := directives.Reconnects(); len(got) != 0 {
		t.Fatalf("refusals fired directives: %v", got)
	}

	// Pending and degraded agents are not offline: the directive fires.
	writesBefore := f.store.Writes
	res, err := f.svc.RequestReconnect(ctx, f.web)
	if err != nil {
		t.Fatal(err)
	}
	if res.LastSnapshotSentAt == nil || !res.LastSnapshotSentAt.Equal(sent) {
		t.Fatalf("instant = %v, want %v", res.LastSnapshotSentAt, sent)
	}
	for _, state := range []innerwallv1.SyncState{innerwallv1.SyncState_SYNC_STATE_PENDING, innerwallv1.SyncState_SYNC_STATE_DEGRADED} {
		f.store.Workloads[1].SyncState = state
		res, err := f.svc.RequestReconnect(ctx, f.db)
		if err != nil || res.LastSnapshotSentAt != nil {
			t.Fatalf("%v: res = %+v err = %v", state, res, err)
		}
	}
	if got := directives.Reconnects(); len(got) != 3 || got[0] != f.web || got[1] != f.db || got[2] != f.db {
		t.Fatalf("directed = %v", got)
	}
	if f.store.Writes != writesBefore {
		t.Fatalf("a directed reconnect wrote to the store (%d writes)", f.store.Writes-writesBefore)
	}

	// A bridge failure is the caller's error, not a silent success.
	directives.Err = errors.New("bridge down")
	if _, err := f.svc.RequestReconnect(ctx, f.web); err == nil {
		t.Fatal("bridge failure was swallowed")
	}
}
