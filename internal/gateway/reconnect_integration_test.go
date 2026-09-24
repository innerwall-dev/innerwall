package gateway_test

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// snapshotAfter waits until the workload's snapshot instant is set and
// later than prev (any instant when prev is nil), and returns it.
func snapshotAfter(prev *time.Time) func(*registry.Workload) bool {
	return func(w *registry.Workload) bool {
		return w.LastSnapshotSentAt != nil && (prev == nil || w.LastSnapshotSentAt.After(*prev))
	}
}

// TestSnapshotInstantOnEverySendingPath checks that the sync path stamps
// the snapshot instant each time it sends a snapshot, whatever caused it:
// a stream opening, the recovery snapshot answering a failed apply, a
// reconnect, and the repair of a workload with no persisted policy. A
// Reconnect directive alone stamps nothing; only the snapshot the new
// stream begins with does.
func TestSnapshotInstantOnEverySendingPath(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")
	if w, _ := st.LookupWorkload(ctx, a.id); w.LastSnapshotSentAt != nil {
		t.Fatalf("instant before any stream = %v", w.LastSnapshotSentAt)
	}

	// Connect.
	a.connect(0, nil)
	a.expectHelloAck()
	a.expectSnapshot()
	w := waitFor(t, st, a.id, "instant on connect", snapshotAfter(nil))
	connected := *w.LastSnapshotSentAt

	// Recovery snapshot after a failed apply.
	a.ack(1, innerwallv1.AckStatus_ACK_STATUS_FAILED, "simulated")
	a.expectSnapshot()
	w = waitFor(t, st, a.id, "instant on recovery", snapshotAfter(&connected))
	recovered := *w.LastSnapshotSentAt

	// A Reconnect directive reaches the stream; it is not a snapshot and
	// moves nothing.
	if err := st.DirectReconnect(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if m := a.recv(); m.GetDirective().GetReconnect() == nil {
		t.Fatalf("expected a Reconnect directive, got %v", m)
	}
	if w, _ := st.LookupWorkload(ctx, a.id); !w.LastSnapshotSentAt.Equal(recovered) {
		t.Fatalf("the directive moved the instant to %v", w.LastSnapshotSentAt)
	}

	// Reconnect, as the directive asked.
	a.disconnect()
	waitFor(t, st, a.id, "offline", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_OFFLINE })
	a.connect(1, nil)
	a.expectHelloAck()
	a.expectSnapshot()
	w = waitFor(t, st, a.id, "instant on reconnect", snapshotAfter(&recovered))
	reconnected := *w.LastSnapshotSentAt

	// Repair: a workload with no persisted policy is rendered once when
	// its stream opens, and that snapshot is stamped like any other.
	a.disconnect()
	waitFor(t, st, a.id, "offline", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_OFFLINE })
	dropPersistedPolicy(t, a)
	if p, err := st.GetWorkloadPolicy(ctx, a.id); err != nil || p != nil {
		t.Fatalf("policy after drop = %v err = %v", p, err)
	}
	a.connect(0, nil)
	a.expectHelloAck()
	if snap := a.expectSnapshot(); snap.GetVersion() != 1 {
		t.Fatalf("repair snapshot = %v", snap)
	}
	waitFor(t, st, a.id, "instant on repair", snapshotAfter(&reconnected))
	a.disconnect()
}

// dropPersistedPolicy leaves a workload as only a render failed at
// enrollment can: registered, with no persisted policy. It is a fixture
// statement against the test database, not a production query.
func dropPersistedPolicy(t *testing.T, a *testAgent) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv(storetest.EnvDatabaseURL))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "DELETE FROM workload_policies WHERE workload_id = $1", a.id.UUID()); err != nil {
		t.Fatal(err)
	}
}

// TestDirectedReconnectFullLoop drives a directed reconnect end to end
// against a live stream: the agent side is the real sync daemon, the
// directive is fired through the fleet domain the operator surface and
// the command line call, and it travels the database notification bridge
// to the gateway holding the stream. The agent reconnects, the new stream
// re-sends a snapshot, and the snapshot instant advances past the one the
// request returned. Once the agent is gone, the same request is refused
// with the instant it was last seen.
func TestDirectedReconnectFullLoop(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")

	holder, err := credential.LoadHolder(*a.cred)
	if err != nil {
		t.Fatal(err)
	}
	var dials atomic.Int32
	installed := &enforce.MemoryStore{}
	daemon := agentsync.New(agentsync.Config{
		Server: h.addr, Holder: holder, Store: installed, AgentVersion: "test",
		BackoffBase: 10 * time.Millisecond, BackoffCap: 50 * time.Millisecond,
		Dial: func(ctx context.Context, server string, holder *credential.Holder) (innerwallv1.AgentServiceClient, io.Closer, error) {
			dials.Add(1)
			return agentsync.DialGRPC(ctx, server, holder)
		},
	})
	dctx, dcancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- daemon.Run(dctx) }()

	w := waitFor(t, st, a.id, "synced with an instant", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.AppliedVersion == 1 && w.LastSnapshotSentAt != nil
	})
	first := *w.LastSnapshotSentAt
	if n := dials.Load(); n != 1 {
		t.Fatalf("dials before the directive = %d", n)
	}

	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}
	svc := &fleet.Service{Store: st, Engine: h.engine, Reads: reads, Directives: st}
	res, err := svc.RequestReconnect(ctx, a.id)
	if err != nil {
		t.Fatal(err)
	}
	if res.LastSnapshotSentAt == nil || !res.LastSnapshotSentAt.Equal(first) {
		t.Fatalf("request returned %v, want the instant as it stood (%v)", res.LastSnapshotSentAt, first)
	}

	// The agent reconnects, the new stream re-sends the snapshot, and the
	// instant advances; the console sees it on its next read.
	waitFor(t, st, a.id, "instant advanced by the directed reconnect", snapshotAfter(&first))
	if n := dials.Load(); n != 2 {
		t.Fatalf("dials after the directive = %d, want one reconnect", n)
	}
	wl, err := reads.GetWorkload(ctx, a.id)
	if err != nil || wl.Sync.LastSnapshotSentAt == nil || !wl.Sync.LastSnapshotSentAt.After(first) {
		t.Fatalf("read model instant = %+v err = %v", wl, err)
	}
	waitFor(t, st, a.id, "synced after the reconnect", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.AppliedVersion == 1
	})
	if cur := installed.Current(); cur == nil || cur.GetVersion() != 1 {
		t.Fatalf("installed policy = %v", cur)
	}

	// The agent stops: its stream closes, the workload is recorded
	// offline, and a directed reconnect is refused with the last-seen
	// instant rather than fired into nothing.
	dcancel()
	if err := <-done; err != nil {
		t.Fatalf("daemon: %v", err)
	}
	w = waitFor(t, st, a.id, "offline", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_OFFLINE })
	_, err = svc.RequestReconnect(ctx, a.id)
	var off *fleet.AgentOfflineError
	if !errors.As(err, &off) || off.LastSeenAt == nil || !off.LastSeenAt.Equal(*w.LastSeenAt) {
		t.Fatalf("offline request err = %v, want last seen %v", err, w.LastSeenAt)
	}
}
