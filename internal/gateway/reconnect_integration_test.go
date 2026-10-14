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
	"google.golang.org/grpc/codes"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	agentsync "github.com/innerwall-dev/innerwall/internal/agent/sync"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/gateway"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
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

// twin is a second client for a's identity: the same credential on a
// connection of its own, as a second agent process on the host holds it.
func (a *testAgent) twin() *testAgent {
	return &testAgent{t: a.t, id: a.id, cred: a.cred, h: a.h}
}

// expectReconnect reads a's stream until a Reconnect directive arrives,
// failing if the stream ends or nothing arrives in time.
func expectReconnect(t *testing.T, a *testAgent, what string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m := <-a.msgs:
			if m.GetDirective().GetReconnect() != nil {
				return
			}
		case err := <-a.errs:
			t.Fatalf("%s: stream ended before the directive arrived: %v", what, err)
		case <-deadline:
			t.Fatalf("%s: no Reconnect directive arrived", what)
		}
	}
}

// superseded waits for a's stream to end with Aborted, as a stream does
// when a newer one from the same workload replaces it.
func superseded(t *testing.T, a *testAgent) {
	t.Helper()
	select {
	case err := <-a.errs:
		wantCode(t, err, codes.Aborted)
	case <-time.After(10 * time.Second):
		t.Fatal("the older stream was not superseded")
	}
}

// TestDirectedReconnectAfterTwoStreams opens two sync streams for one
// identity in quick succession, as a deploy turnover does (the outgoing
// agent reconnecting to a restarted control plane, then its replacement),
// tears the older down in each order the turnover can produce, and fires
// a directed reconnect through the notification bridge: it must reach
// the stream the registry holds. A fresh stream after that one, as an
// agent restart opens, must receive the next directive too, and so must
// a stream opened with a renewed credential (issue #106).
func TestDirectedReconnectAfterTwoStreams(t *testing.T) {
	orders := []struct {
		name string
		// open opens the two streams and returns the one left live.
		open func(t *testing.T, older, newer *testAgent) *testAgent
	}{
		{"older closed after newer opened", func(t *testing.T, older, newer *testAgent) *testAgent {
			older.connect(0, nil)
			older.expectHelloAck()
			older.expectSnapshot()
			newer.connect(0, nil)
			newer.expectHelloAck()
			newer.expectSnapshot()
			older.disconnect()
			return newer
		}},
		{"older closed before newer opened", func(t *testing.T, older, newer *testAgent) *testAgent {
			older.connect(0, nil)
			older.expectHelloAck()
			older.expectSnapshot()
			older.disconnect()
			newer.connect(0, nil)
			newer.expectHelloAck()
			newer.expectSnapshot()
			return newer
		}},
		{"older superseded and left open", func(t *testing.T, older, newer *testAgent) *testAgent {
			older.connect(0, nil)
			older.expectHelloAck()
			older.expectSnapshot()
			newer.connect(0, nil)
			newer.expectHelloAck()
			newer.expectSnapshot()
			superseded(t, older)
			return newer
		}},
		{"older closed before its snapshot", func(t *testing.T, older, newer *testAgent) *testAgent {
			older.connect(0, nil)
			newer.connect(0, nil)
			older.disconnect()
			newer.expectHelloAck()
			newer.expectSnapshot()
			return newer
		}},
		{"hellos together", func(t *testing.T, older, newer *testAgent) *testAgent {
			// Whichever Hello the gateway answers last holds the
			// registry; the other stream is superseded.
			older.connect(0, nil)
			newer.connect(0, nil)
			select {
			case err := <-older.errs:
				wantCode(t, err, codes.Aborted)
				return newer
			case err := <-newer.errs:
				wantCode(t, err, codes.Aborted)
				return older
			case <-time.After(10 * time.Second):
				t.Fatal("neither stream was superseded")
				return nil
			}
		}},
	}
	for _, o := range orders {
		t.Run(o.name, func(t *testing.T) {
			st := storetest.Open(t)
			h := newHarness(t, st)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			token, _, err := h.service.MintToken(ctx, "t", nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			older := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")
			live := o.open(t, older, older.twin())
			if err := st.DirectReconnect(ctx, live.id); err != nil {
				t.Fatal(err)
			}
			expectReconnect(t, live, "the stream left live")

			// The agent restarts: a fresh stream.
			live.disconnect()
			fresh := older.twin()
			fresh.connect(1, nil)
			fresh.expectHelloAck()
			fresh.expectSnapshot()
			if err := st.DirectReconnect(ctx, fresh.id); err != nil {
				t.Fatal(err)
			}
			expectReconnect(t, fresh, "a fresh stream")

			// The credential renews and the next stream carries it.
			renewCredential(t, fresh)
			fresh.disconnect()
			renewed := older.twin()
			renewed.connect(1, nil)
			renewed.expectHelloAck()
			renewed.expectSnapshot()
			if err := st.DirectReconnect(ctx, renewed.id); err != nil {
				t.Fatal(err)
			}
			expectReconnect(t, renewed, "a stream on a renewed credential")
			renewed.disconnect()
		})
	}
}

// renewCredential renews a's credential over the gateway and stores it, as
// the agent's renewer does.
func renewCredential(t *testing.T, a *testAgent) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cred, trust, err := a.cred.Load()
	if err != nil {
		t.Fatal(err)
	}
	key, err := a.cred.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	res, err := credential.Renew(ctx, key, credential.RenewOptions{Server: a.h.addr, Credential: cred, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.cred.ReplaceCertificate(res.CertificatePEM, res.BundlePEM); err != nil {
		t.Fatal(err)
	}
}

// TestDirectedReconnectAcrossDeployTurnover drives the turnover with real
// sync daemons: the outgoing agent holds the stream, its replacement
// opens one for the same identity, the outgoing one is stopped, and a
// directed reconnect fired through the fleet domain moves the snapshot
// instant. The replacement then restarts, as an on-host restart does, and
// the next directed reconnect moves it again (issue #106).
func TestDirectedReconnectAcrossDeployTurnover(t *testing.T) {
	st := storetest.Open(t)
	h := newHarness(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")
	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}
	svc := &fleet.Service{Store: st, Engine: h.engine, Reads: reads, Directives: st}

	start := func(version string) (stop func()) {
		holder, err := credential.LoadHolder(*a.cred)
		if err != nil {
			t.Fatal(err)
		}
		daemon := agentsync.New(agentsync.Config{
			Server: h.addr, Holder: holder, Store: &enforce.MemoryStore{}, AgentVersion: version,
			BackoffBase: 10 * time.Millisecond, BackoffCap: 50 * time.Millisecond,
		})
		dctx, dcancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- daemon.Run(dctx) }()
		return func() {
			dcancel()
			if err := <-done; err != nil {
				t.Fatalf("daemon %s: %v", version, err)
			}
		}
	}
	synced := func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED && w.LastSnapshotSentAt != nil
	}
	// directed fires a directed reconnect once the workload reads synced
	// and waits for the snapshot instant to move. A turnover leaves the
	// workload briefly without a stream (the outgoing agent's ended, the
	// incoming one waits out its backoff), and a request in that gap is
	// refused as offline, which is the stored state's honest answer; the
	// request is repeated then. A workload left recorded offline while a
	// stream is live never reads synced again, and fails the wait.
	directed := func(what string) {
		t.Helper()
		for {
			w := waitFor(t, st, a.id, "synced before "+what, synced)
			before := *w.LastSnapshotSentAt
			res, err := svc.RequestReconnect(ctx, a.id)
			if errors.Is(err, fleet.ErrAgentOffline) {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.LastSnapshotSentAt == nil {
				t.Fatalf("%s: request returned no instant", what)
			}
			waitFor(t, st, a.id, "instant advanced by "+what, snapshotAfter(&before))
			return
		}
	}

	stopOld := start("outgoing")
	waitFor(t, st, a.id, "outgoing synced", synced)
	stopNew := start("incoming")
	time.Sleep(200 * time.Millisecond)
	stopOld()
	directed("the directive after the turnover")
	directed("a second directive")

	stopNew()
	waitFor(t, st, a.id, "offline", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_OFFLINE })
	stopRestarted := start("restarted")
	directed("the directive after a restart")
	stopRestarted()
}

// slowOffline holds every offline mark for delay after announcing it on
// entered, widening the window between a stream leaving the registry and
// its offline mark landing.
type slowOffline struct {
	registry.Store
	delay   time.Duration
	entered chan struct{}
}

func (s *slowOffline) SetSyncState(ctx context.Context, id identity.WorkloadID, state innerwallv1.SyncState, detail string, now time.Time) error {
	if state == innerwallv1.SyncState_SYNC_STATE_OFFLINE {
		select {
		case s.entered <- struct{}{}:
		default:
		}
		time.Sleep(s.delay)
	}
	return s.Store.SetSyncState(ctx, id, state, detail, now)
}

// TestOfflineMarkNeverOverwritesNewerStream ends a stream and opens the
// next one for the same workload while the first stream's offline mark is
// still being written, as an agent obeying a Reconnect directive does: it
// redials at once. The newer stream's state must stand, so the workload
// reads synced and a directed reconnect is accepted and delivered rather
// than refused as offline.
func TestOfflineMarkNeverOverwritesNewerStream(t *testing.T) {
	st := storetest.Open(t)
	slow := &slowOffline{Store: st, delay: 500 * time.Millisecond, entered: make(chan struct{}, 1)}
	h := newHarness(t, st, func(d *gateway.Deps) { d.Registry = slow })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	older := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")
	older.connect(0, nil)
	older.expectHelloAck()
	older.expectSnapshot()
	older.ack(1, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, older.id, "synced", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED })

	older.disconnect()
	select {
	case <-slow.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the closed stream was not marked offline")
	}
	newer := older.twin()
	newer.connect(1, nil)
	newer.expectHelloAck()
	newer.expectSnapshot()
	newer.ack(1, innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, newer.id, "synced on the newer stream", func(w *registry.Workload) bool { return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED })

	// Past the offline mark's delay, the newer stream's state stands.
	time.Sleep(2 * slow.delay)
	if w, err := st.LookupWorkload(ctx, newer.id); err != nil || w.SyncState != innerwallv1.SyncState_SYNC_STATE_SYNCED {
		t.Fatalf("workload with a live stream recorded %v (err %v)", w.SyncState, err)
	}
	reads := &readmodel.Reader{Store: st, Flows: st.Flows()}
	svc := &fleet.Service{Store: st, Engine: h.engine, Reads: reads, Directives: st}
	if _, err := svc.RequestReconnect(ctx, newer.id); err != nil {
		t.Fatalf("directed reconnect refused for a live stream: %v", err)
	}
	expectReconnect(t, newer, "the newer stream")
}
