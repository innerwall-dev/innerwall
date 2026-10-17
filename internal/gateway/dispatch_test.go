package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// records is a slog handler that keeps every record, for asserting on
// what the dispatch paths log.
type records struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (r *records) Enabled(context.Context, slog.Level) bool { return true }
func (r *records) WithAttrs([]slog.Attr) slog.Handler       { return r }
func (r *records) WithGroup(string) slog.Handler            { return r }
func (r *records) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, rec.Clone())
	return nil
}

// wait returns the first record at level whose message starts with msg,
// waiting for it because sendReconnect runs on its own goroutine.
func (r *records) wait(t *testing.T, level slog.Level, msg string) map[string]string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		for _, rec := range r.recs {
			if rec.Level == level && strings.HasPrefix(rec.Message, msg) {
				attrs := map[string]string{}
				rec.Attrs(func(a slog.Attr) bool {
					attrs[a.Key] = fmt.Sprint(a.Value.Any())
					return true
				})
				r.mu.Unlock()
				return attrs
			}
		}
		r.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("no %s record %q; have %d records", level, msg, len(r.recs))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (r *records) count(level slog.Level) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, rec := range r.recs {
		if rec.Level == level {
			n++
		}
	}
	return n
}

func newDispatchServer() (*Server, *records) {
	rec := &records{}
	return New(Deps{Log: slog.New(rec)}), rec
}

func newSession(t *testing.T, id identity.WorkloadID, depth int) *session {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })
	return &session{id: id, ctx: ctx, cancel: cancel, send: make(chan *innerwallv1.SyncResponse, depth)}
}

func mustID(t *testing.T) identity.WorkloadID {
	t.Helper()
	id, err := identity.NewWorkloadID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func register(t *testing.T, s *Server, sess *session) {
	t.Helper()
	if err := s.sessions.add(sess); err != nil {
		t.Fatal(err)
	}
}

func expectDirective(t *testing.T, sess *session) {
	t.Helper()
	select {
	case m := <-sess.send:
		if m.GetDirective().GetReconnect() == nil {
			t.Fatalf("queued %v, want a Reconnect directive", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no directive queued")
	}
}

// A directive for a workload whose stream this replica does not hold is
// dropped with a warning naming the workload, never silently.
func TestDirectiveWithoutHeldStreamWarns(t *testing.T) {
	s, rec := newDispatchServer()
	id := mustID(t)
	s.directReconnect(id)
	attrs := rec.wait(t, slog.LevelWarn, "reconnect directive not sent")
	if attrs["workload_id"] != id.String() {
		t.Fatalf("warning names %q, want %s", attrs["workload_id"], id)
	}
}

// A directive whose stream ended before it could be queued is given up
// with a warning carrying the cause, and nothing is queued for a writer
// that will never send it.
func TestDirectiveOnEndedStreamWarns(t *testing.T) {
	s, rec := newDispatchServer()
	sess := newSession(t, mustID(t), sendQueueDepth)
	register(t, s, sess)
	sess.cancel(errSuperseded)
	s.directReconnect(sess.id)
	attrs := rec.wait(t, slog.LevelWarn, "reconnect directive not sent")
	if attrs["workload_id"] != sess.id.String() || attrs["reason"] != errSuperseded.Error() {
		t.Fatalf("warning = %v", attrs)
	}
	if n := len(sess.send); n != 0 {
		t.Fatalf("%d messages queued on an ended stream", n)
	}
}

// A directive that finds the send queue full for the whole bound is given
// up with a warning, where the unbounded wait would have hung silently.
func TestDirectiveOnStuckQueueWarns(t *testing.T) {
	defer func(d time.Duration) { directiveQueueTimeout = d }(directiveQueueTimeout)
	directiveQueueTimeout = 50 * time.Millisecond
	s, rec := newDispatchServer()
	sess := newSession(t, mustID(t), 1)
	sess.send <- &innerwallv1.SyncResponse{}
	register(t, s, sess)
	s.directReconnect(sess.id)
	attrs := rec.wait(t, slog.LevelWarn, "reconnect directive not sent")
	if attrs["workload_id"] != sess.id.String() || attrs["reason"] != errQueueFull.Error() {
		t.Fatalf("warning = %v", attrs)
	}
}

// A directive for a held, live stream is queued and logged as directed,
// with no warning.
func TestDirectiveQueuedOnHeldStream(t *testing.T) {
	s, rec := newDispatchServer()
	sess := newSession(t, mustID(t), sendQueueDepth)
	register(t, s, sess)
	s.directReconnect(sess.id)
	expectDirective(t, sess)
	if attrs := rec.wait(t, slog.LevelInfo, "reconnect directed"); attrs["workload_id"] != sess.id.String() {
		t.Fatalf("info = %v", attrs)
	}
	if n := rec.count(slog.LevelWarn); n != 0 {
		t.Fatalf("%d warnings for a delivered directive", n)
	}
}

// Supersession leaves the registry holding the newer stream whichever
// stream's teardown runs first, and a directive reaches it.
func TestDirectiveReachesNewerStreamAfterSupersession(t *testing.T) {
	orders := map[string]func(s *Server, older, newer *session){
		"older ends after newer registers": func(s *Server, older, newer *session) {
			register(t, s, older)
			register(t, s, newer)
			if !errors.Is(context.Cause(older.ctx), errSuperseded) {
				t.Fatalf("older stream cause = %v", context.Cause(older.ctx))
			}
			if _, ok := s.sessions.remove(older); ok {
				t.Fatal("removing the superseded stream reported it registered")
			}
		},
		"older ends before newer registers": func(s *Server, older, newer *session) {
			register(t, s, older)
			older.cancel(nil)
			marked, ok := s.sessions.remove(older)
			if !ok {
				t.Fatal("removing the registered stream reported it not registered")
			}
			marked()
			register(t, s, newer)
		},
	}
	for name, run := range orders {
		t.Run(name, func(t *testing.T) {
			s, rec := newDispatchServer()
			id := mustID(t)
			older, newer := newSession(t, id, sendQueueDepth), newSession(t, id, sendQueueDepth)
			run(s, older, newer)
			if got := s.sessions.get(id); got != newer {
				t.Fatal("registry does not hold the newer stream")
			}
			s.directReconnect(id)
			expectDirective(t, newer)
			if n := len(older.send); n != 0 {
				t.Fatalf("%d messages queued on the older stream", n)
			}
			if n := rec.count(slog.LevelWarn); n != 0 {
				t.Fatalf("%d warnings for a delivered directive", n)
			}
		})
	}
}

// A stream that has left the registry holds its workload until its
// offline mark is written: a newer stream registers only after that, and
// one that ends while waiting registers nothing.
func TestNewerStreamWaitsForOfflineMark(t *testing.T) {
	s, _ := newDispatchServer()
	id := mustID(t)
	older := newSession(t, id, sendQueueDepth)
	register(t, s, older)
	older.cancel(nil)
	marked, ok := s.sessions.remove(older)
	if !ok {
		t.Fatal("removing the registered stream reported it not registered")
	}

	gone := newSession(t, id, sendQueueDepth)
	gone.cancel(context.Canceled)
	if err := s.sessions.add(gone); !errors.Is(err, context.Canceled) {
		t.Fatalf("add of an ended stream while the mark is written = %v", err)
	}

	newer := newSession(t, id, sendQueueDepth)
	added := make(chan error, 1)
	go func() { added <- s.sessions.add(newer) }()
	select {
	case err := <-added:
		t.Fatalf("newer stream registered before the offline mark was written (err %v)", err)
	case <-time.After(100 * time.Millisecond):
	}
	if s.sessions.get(id) != nil {
		t.Fatal("a stream is registered while the offline mark is written")
	}
	marked()
	if err := <-added; err != nil {
		t.Fatal(err)
	}
	if s.sessions.get(id) != newer {
		t.Fatal("registry does not hold the newer stream")
	}
	if err := context.Cause(newer.ctx); err != nil {
		t.Fatalf("newer stream ended: %v", err)
	}
}

// TestHeartbeatUptimeHeldInRange: an uptime too long for a duration is
// held at the longest one, never wrapped into a negative that would place
// the process start after its own heartbeat.
func TestHeartbeatUptimeHeldInRange(t *testing.T) {
	if got := heartbeatUptime(90); got != 90*time.Second {
		t.Fatalf("heartbeatUptime(90) = %v", got)
	}
	if got := heartbeatUptime(math.MaxUint64); got <= 0 {
		t.Fatalf("heartbeatUptime(max) = %v, want positive", got)
	}
}
