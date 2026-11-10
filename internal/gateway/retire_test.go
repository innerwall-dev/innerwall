package gateway

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/compiler"
)

// streamEndedRetired is the gateway's line for a stream it ended because
// the workload was retired.
const streamEndedRetired = "sync stream ended: the workload was retired"

// messages counts the records whose message is msg.
func (r *records) messages(msg string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, rec := range r.recs {
		if rec.Message == msg {
			n++
		}
	}
	return n
}

// cleanUp does what a stream's handler does once its session ends: it
// takes the session out of the registry and writes the offline mark.
func cleanUp(t *testing.T, s *Server, sess *session) {
	t.Helper()
	marked, ok := s.sessions.remove(sess)
	if !ok {
		t.Fatal("the session was not registered")
	}
	marked()
}

// Issue #220: on the replica that made the write, the end directive can
// end the stream, and the stream's cleanup take it out of the registry,
// before the retirement's own lever runs. The retirement still reports
// that this replica's stream ended, and the stream's end is logged once.
func TestRetirementReportsAStreamTheDirectiveEnded(t *testing.T) {
	s, rec := newDispatchServer()
	sess := newSession(t, mustID(t), 1)
	register(t, s, sess)
	end := s.WatchRetiring(sess.id)

	s.direct(compiler.Directive{ID: sess.id, Kind: compiler.DirectiveEnd})
	<-sess.ctx.Done()
	cleanUp(t, s, sess)

	if !end(true) {
		t.Fatal("the retirement reported no stream ended here, though the directive ended this replica's stream")
	}
	if err := endedWith(sess.ctx); !errors.Is(err, errRefused) {
		t.Fatalf("stream ended with %v, want the refusal", err)
	}
	if n := rec.messages(streamEndedRetired); n != 1 {
		t.Fatalf("logged %q %d times, want once", streamEndedRetired, n)
	}
}

// Issue #203: the retirement's lever and the end directive both find the
// stream still registered, in either order; the stream's end is logged by
// the one that ended it, once, and the retirement reports it held one.
func TestRetiredStreamEndLoggedOnce(t *testing.T) {
	for _, directiveFirst := range []bool{true, false} {
		s, rec := newDispatchServer()
		sess := newSession(t, mustID(t), 1)
		register(t, s, sess)
		end := s.WatchRetiring(sess.id)
		directive := compiler.Directive{ID: sess.id, Kind: compiler.DirectiveEnd}

		var held bool
		if directiveFirst {
			s.direct(directive)
			held = end(true)
		} else {
			held = end(true)
			s.direct(directive)
		}
		if !held {
			t.Fatalf("directive first %v: the retirement reported no stream held here", directiveFirst)
		}
		if n := rec.messages(streamEndedRetired); n != 1 {
			t.Fatalf("directive first %v: logged %q %d times, want once", directiveFirst, streamEndedRetired, n)
		}
	}
}

// A stream that ended another way while the retirement ran, such as a
// heartbeat write that found the workload retired, is reported as ended
// here, and is not logged as ended by the retirement's lever.
func TestRetirementReportsAStreamEndedAnotherWay(t *testing.T) {
	s, rec := newDispatchServer()
	sess := newSession(t, mustID(t), 1)
	register(t, s, sess)
	end := s.WatchRetiring(sess.id)

	sess.cancel(errRefused)
	cleanUp(t, s, sess)

	if !end(true) {
		t.Fatal("the retirement reported no stream ended here, though this replica's stream ended while it ran")
	}
	if n := rec.messages(streamEndedRetired); n != 0 {
		t.Fatalf("logged %q %d times for a stream the lever did not end", streamEndedRetired, n)
	}
}

// A retirement that did not commit ends nothing and reports nothing, and
// its watch does not outlive it.
func TestUncommittedRetirementEndsNothing(t *testing.T) {
	s := New(Deps{Log: slog.New(&records{})})
	sess := newSession(t, mustID(t), 1)
	register(t, s, sess)
	end := s.WatchRetiring(sess.id)
	if end(false) {
		t.Fatal("a retirement that did not commit reported a stream ended here")
	}
	if sess.ctx.Err() != nil {
		t.Fatal("a retirement that did not commit ended the stream")
	}
	s.sessions.mu.Lock()
	defer s.sessions.mu.Unlock()
	if n := len(s.sessions.watching); n != 0 {
		t.Fatalf("%d watches outlived their retirement", n)
	}
}
