package gateway_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/gateway"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// heldOffline holds every offline mark one replica makes until the test
// releases it, and reports each mark's outcome once the write returns,
// so a test can order that write after another replica's.
type heldOffline struct {
	registry.Store
	entered chan struct{}
	release chan struct{}
	written chan markOutcome
}

// markOutcome is what one offline mark returned.
type markOutcome struct {
	written bool
	err     error
}

func newHeldOffline(st registry.Store) *heldOffline {
	return &heldOffline{Store: st, entered: make(chan struct{}, 1), release: make(chan struct{}), written: make(chan markOutcome, 1)}
}

func (g *heldOffline) MarkStreamOffline(ctx context.Context, id identity.WorkloadID, stream uuid.UUID, now time.Time) (bool, error) {
	select {
	case g.entered <- struct{}{}:
	default:
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		g.written <- markOutcome{err: ctx.Err()}
		return false, ctx.Err()
	}
	written, err := g.Store.MarkStreamOffline(ctx, id, stream, now)
	g.written <- markOutcome{written: written, err: err}
	return written, err
}

// awaitEntered waits until the replica has begun its offline mark.
func (g *heldOffline) awaitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the ended stream's offline mark was never attempted")
	}
}

// awaitWritten waits until the held offline mark has returned, fails
// the test if it returned an error, and reports whether it was written.
func (g *heldOffline) awaitWritten(t *testing.T) bool {
	t.Helper()
	select {
	case out := <-g.written:
		if out.err != nil {
			t.Fatalf("offline mark: %v", out.err)
		}
		return out.written
	case <-time.After(10 * time.Second):
		t.Fatal("the offline mark did not return")
		return false
	}
}

// TestOfflineMarkFromAnotherReplicaNeverOverwritesNewerStream is the
// cross-replica form of TestOfflineMarkNeverOverwritesNewerStream (issue
// #109): a stream ends on replica A, its agent redials through replica B,
// and B's stream reaches SYNCED before A's offline mark lands. Nothing
// orders the two replicas' writes, so the mark is held until B's state
// is written and released only then. The workload must still read
// SYNCED, and a directed reconnect must be accepted and reach B's stream
// rather than be refused as offline.
func TestOfflineMarkFromAnotherReplicaNeverOverwritesNewerStream(t *testing.T) {
	st := storetest.Open(t)
	gate := newHeldOffline(st)
	a := newHarness(t, st, func(d *gateway.Deps) { d.Registry = gate })
	b := newHarnessWith(t, st, a.authority)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := a.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	older := enrollAgent(t, a, token, "h-1", "10.0.0.10/24")
	older.connect(0, nil)
	older.expectHelloAck()
	snap := older.expectSnapshot()
	older.ack(snap.GetVersion(), innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, older.id, "synced on replica A", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED
	})

	older.disconnect()
	gate.awaitEntered(t)

	newer := older.twin()
	newer.h = b
	newer.connect(snap.GetVersion(), nil)
	newer.expectHelloAck()
	snap = newer.expectSnapshot()
	waitFor(t, st, newer.id, "pending on replica B", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_PENDING
	})
	newer.ack(snap.GetVersion(), innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, newer.id, "synced on replica B", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED
	})

	close(gate.release)
	if gate.awaitWritten(t) {
		t.Fatal("replica A's offline mark was written over the newer stream on replica B")
	}
	if w, err := st.LookupWorkload(ctx, newer.id); err != nil || w.SyncState != innerwallv1.SyncState_SYNC_STATE_SYNCED {
		t.Fatalf("workload with a live stream on replica B recorded %v after replica A's offline mark (err %v)", w.SyncState, err)
	}
	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}
	svc := &fleet.Service{Store: st, Engine: b.engine, Reads: reads, Directives: st}
	if _, err := svc.RequestReconnect(ctx, newer.id); err != nil {
		t.Fatalf("directed reconnect refused for a stream live on replica B: %v", err)
	}
	expectReconnect(t, newer, "the stream on replica B")
}

// TestOfflineMarkWithNoNewerStream: a stream ends on replica A and no
// stream of the workload opens anywhere after it, so A's offline mark
// is written and the workload reads OFFLINE, and a directed reconnect is
// refused as offline, on every replica alike.
func TestOfflineMarkWithNoNewerStream(t *testing.T) {
	st := storetest.Open(t)
	gate := newHeldOffline(st)
	close(gate.release)
	a := newHarness(t, st, func(d *gateway.Deps) { d.Registry = gate })
	b := newHarnessWith(t, st, a.authority)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := a.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ag := enrollAgent(t, a, token, "h-1", "10.0.0.10/24")
	ag.connect(0, nil)
	ag.expectHelloAck()
	snap := ag.expectSnapshot()
	ag.ack(snap.GetVersion(), innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, ag.id, "synced", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED
	})

	ag.disconnect()
	gate.awaitEntered(t)
	if !gate.awaitWritten(t) {
		t.Fatal("the offline mark of a stream with no newer one was not written")
	}
	waitFor(t, st, ag.id, "offline", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_OFFLINE
	})
	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}
	svc := &fleet.Service{Store: st, Engine: b.engine, Reads: reads, Directives: st}
	if _, err := svc.RequestReconnect(ctx, ag.id); !errors.Is(err, fleet.ErrAgentOffline) {
		t.Fatalf("directed reconnect for a workload with no stream = %v, want refused as offline", err)
	}
}
