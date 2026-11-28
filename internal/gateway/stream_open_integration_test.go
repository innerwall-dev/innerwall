package gateway_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/gateway"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/store"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// failedOpen fails the next stream-open writes with the errors queued on
// it, one each, and passes every other write through. With commit set, a
// failure is reported after the write itself has landed, as a write whose
// reply was lost is.
type failedOpen struct {
	registry.Store
	errs   chan error
	commit bool
}

func newFailedOpen(st registry.Store, commit bool) *failedOpen {
	return &failedOpen{Store: st, errs: make(chan error, 4), commit: commit}
}

func (f *failedOpen) RecordStreamOpen(ctx context.Context, id identity.WorkloadID, stream uuid.UUID, now time.Time) error {
	select {
	case err := <-f.errs:
		if f.commit {
			if werr := f.Store.RecordStreamOpen(ctx, id, stream, now); werr != nil {
				return werr
			}
		}
		return err
	default:
		return f.Store.RecordStreamOpen(ctx, id, stream, now)
	}
}

// errStoreDown stands for a transient database failure. Its text must
// never reach the agent.
var errStoreDown = errors.New("store: recording stream open: connection reset by peer")

// notHeld fails the test if the replica still holds a stream of a.
func notHeld(t *testing.T, h *harness, a *testAgent) {
	t.Helper()
	if h.gw.EndStream(a.id, status.Error(codes.Aborted, "ended by the test")) {
		t.Fatal("a stream whose open was not recorded is still registered")
	}
}

// syncedThenOffline opens a stream of a that must be served, brings it to
// SYNCED, ends it, and waits for its end to be recorded OFFLINE.
func syncedThenOffline(t *testing.T, st *store.Store, a *testAgent, what string) {
	t.Helper()
	a.connect(0, nil)
	a.expectHelloAck()
	snap := a.expectSnapshot()
	a.ack(snap.GetVersion(), innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, a.id, what+": synced", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED
	})
	a.disconnect()
	waitFor(t, st, a.id, what+": offline", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_OFFLINE
	})
}

// A stream whose open record fails is never served (#264): it ends with
// Unavailable, naming nothing of the store's error, before its HelloAck,
// so its agent redials with its ordinary backoff. It leaves the registry
// and the workload's hold, so the next stream of the workload opens at
// once, and that stream's end is recorded offline. The record the failed
// stream leaves behind is the one the previous stream's end wrote.
func TestStreamEndsWhenItsOpenIsNotRecorded(t *testing.T) {
	st := storetest.Open(t)
	fail := newFailedOpen(st, false)
	h := newHarness(t, st, func(d *gateway.Deps) { d.Registry = fail })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")
	syncedThenOffline(t, st, a, "the first stream")

	fail.errs <- errStoreDown
	a.connect(1, nil)
	err = streamEnded(t, a)
	wantCode(t, err, codes.Unavailable)
	if strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("the agent was given the store's error: %v", err)
	}
	notHeld(t, h, a)
	if w, err := st.LookupWorkload(ctx, a.id); err != nil || w.SyncState != innerwallv1.SyncState_SYNC_STATE_OFFLINE {
		t.Fatalf("after a stream whose open was not recorded the workload reads %v (err %v), want offline", w.SyncState, err)
	}

	syncedThenOffline(t, st, a, "the stream after the failed one")
}

// An open that landed though its write reported failure names the failed
// stream, which is not served: the stream's offline mark, conditioned on
// the record naming it as every mark is, records its end, so the workload
// does not go on reading pending with no stream (#264).
func TestStreamOpenRecordedButReportedFailedEndsOffline(t *testing.T) {
	st := storetest.Open(t)
	fail := newFailedOpen(st, true)
	h := newHarness(t, st, func(d *gateway.Deps) { d.Registry = fail })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")

	fail.errs <- errStoreDown
	a.connect(0, nil)
	wantCode(t, streamEnded(t, a), codes.Unavailable)
	notHeld(t, h, a)
	if w, err := st.LookupWorkload(ctx, a.id); err != nil || w.SyncState != innerwallv1.SyncState_SYNC_STATE_OFFLINE {
		t.Fatalf("after a stream whose open landed but was reported failed the workload reads %v (err %v), want offline", w.SyncState, err)
	}

	syncedThenOffline(t, st, a, "the stream after the failed one")
}

// A failed open on one replica writes nothing over the stream of the
// workload another replica serves: the record names that stream, so the
// failed stream's mark is not written and the live stream's state stands
// (#264).
func TestFailedStreamOpenLeavesAnotherReplicasStream(t *testing.T) {
	st := storetest.Open(t)
	fail := newFailedOpen(st, false)
	a := newHarness(t, st, func(d *gateway.Deps) { d.Registry = fail })
	b := newHarnessWith(t, st, a.authority)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := a.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	live := enrollAgent(t, a, token, "h-1", "10.0.0.10/24")
	live.h = b
	live.connect(0, nil)
	live.expectHelloAck()
	snap := live.expectSnapshot()
	live.ack(snap.GetVersion(), innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	waitFor(t, st, live.id, "synced on replica B", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_SYNCED
	})

	failed := live.twin()
	failed.h = a
	fail.errs <- errStoreDown
	failed.connect(snap.GetVersion(), nil)
	wantCode(t, streamEnded(t, failed), codes.Unavailable)
	notHeld(t, a, failed)
	if w, err := st.LookupWorkload(ctx, live.id); err != nil || w.SyncState != innerwallv1.SyncState_SYNC_STATE_SYNCED {
		t.Fatalf("a failed open on replica A left the workload served on replica B reading %v (err %v)", w.SyncState, err)
	}
	live.ack(snap.GetVersion(), innerwallv1.AckStatus_ACK_STATUS_APPLIED, "")
	live.disconnect()
	waitFor(t, st, live.id, "offline once replica B's stream ends", func(w *registry.Workload) bool {
		return w.SyncState == innerwallv1.SyncState_SYNC_STATE_OFFLINE
	})
}

// An open that finds the workload unknown or retired is answered as the
// Hello is for the same finding: PermissionDenied naming the unknown
// workload, and the one opaque refusal for a retired one (ADR-0023). The
// stream leaves the registry and the hold either way (#264).
func TestStreamOpenRefusalsAnswerAsHello(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want func(t *testing.T, err error)
	}{
		{"unknown", registry.ErrWorkloadUnknown, func(t *testing.T, err error) {
			t.Helper()
			wantCode(t, err, codes.PermissionDenied)
		}},
		{"retired", &registry.RetiredError{RetiredAt: time.Now()}, func(t *testing.T, err error) {
			t.Helper()
			wantOpaqueRefusal(t, "a stream whose open found the workload retired", err)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := storetest.Open(t)
			fail := newFailedOpen(st, false)
			h := newHarness(t, st, func(d *gateway.Deps) { d.Registry = fail })
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			token, _, err := h.service.MintToken(ctx, "t", nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			a := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")
			fail.errs <- tc.err
			a.connect(0, nil)
			tc.want(t, streamEnded(t, a))
			notHeld(t, h, a)
			syncedThenOffline(t, st, a, "the stream after the refused one")
		})
	}
}

// heldStamp holds every snapshot stamp until release closes, and notes in
// order when each stamp and each offline mark returns.
type heldStamp struct {
	registry.Store
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	order   []string
}

func (h *heldStamp) note(what string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.order = append(h.order, what)
}

func (h *heldStamp) noted() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.order...)
}

func (h *heldStamp) RecordSnapshotSent(ctx context.Context, id identity.WorkloadID, at time.Time) error {
	select {
	case h.entered <- struct{}{}:
	default:
	}
	select {
	case <-h.release:
	case <-ctx.Done():
		h.note("stamp gave up")
		return ctx.Err()
	}
	err := h.Store.RecordSnapshotSent(ctx, id, at)
	h.note("stamp")
	return err
}

func (h *heldStamp) MarkStreamOffline(ctx context.Context, id identity.WorkloadID, stream uuid.UUID, now time.Time) (bool, error) {
	written, err := h.Store.MarkStreamOffline(ctx, id, stream, now)
	h.note("mark")
	return written, err
}

// A stream that ends while its snapshot's stamp is still being written
// ends only once the stamp has returned, and its offline mark lands after
// the stamp: the handler, which shutdown waits for before the store
// closes, is the last thing the stream runs, and the record never shows a
// snapshot sent after the stream's end (#260).
func TestStreamEndWaitsForItsSnapshotStamp(t *testing.T) {
	st := storetest.Open(t)
	held := &heldStamp{Store: st, entered: make(chan struct{}, 1), release: make(chan struct{})}
	h := newHarness(t, st, func(d *gateway.Deps) { d.Registry = held })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	token, _, err := h.service.MintToken(ctx, "t", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollAgent(t, h, token, "h-1", "10.0.0.10/24")
	a.connect(0, nil)
	a.expectHelloAck()
	a.expectSnapshot()
	select {
	case <-held.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the snapshot was never stamped")
	}

	if !h.gw.EndStream(a.id, status.Error(codes.Aborted, "ended by the test")) {
		t.Fatal("the replica does not hold the stream")
	}
	select {
	case err := <-a.errs:
		t.Fatalf("the stream ended (%v) while its snapshot stamp was still being written", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(held.release)
	wantCode(t, streamEnded(t, a), codes.Aborted)
	if got := held.noted(); len(got) != 2 || got[0] != "stamp" || got[1] != "mark" {
		t.Fatalf("writes in order %v, want the stamp and then the offline mark", got)
	}
	w, err := st.LookupWorkload(ctx, a.id)
	if err != nil {
		t.Fatal(err)
	}
	if w.LastSnapshotSentAt == nil || w.SyncState != innerwallv1.SyncState_SYNC_STATE_OFFLINE {
		t.Fatalf("after the stream ended the workload reads %v with snapshot instant %v", w.SyncState, w.LastSnapshotSentAt)
	}
}
