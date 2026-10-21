package gateway

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// errRefused is the one refusal every door gives a retired identity
// (ADR-0023): renewal, every agent-service call, and the end of a live
// stream. It names no reason. Whoever presents a retired identity's
// credential is either the host the operator decommissioned, which needs
// none, or a party holding a copy of its key, which should not learn that
// it was detected; recovery is re-enrollment, an operator act on the host.
// The control plane's log names the workload and its retirement instant
// beside every refusal, and the operator surface reports the state.
var errRefused = status.Error(codes.Unauthenticated, "workload credential refused")

// refuseRetired logs a refusal of a retired identity precisely and returns
// the opaque one the agent receives. log names the workload; err must
// carry a *RetiredError, whose instant the line adds.
func refuseRetired(log *slog.Logger, err error, door string) error {
	attrs := []any{"door", door}
	var re *registry.RetiredError
	if errors.As(err, &re) {
		attrs = append(attrs, "retired_at", re.RetiredAt)
	}
	log.Warn("refused a retired workload's credential", attrs...)
	return errRefused
}

// isRetired says err refuses a retired workload.
func isRetired(err error) bool { return errors.Is(err, registry.ErrWorkloadRetired) }

// streamEnd is the cause a stream is ended with through EndStream: the
// status its agent receives.
type streamEnd struct{ err error }

func (e *streamEnd) Error() string { return e.err.Error() }

// endedWith returns the status a stream ended through EndStream must
// return, or nil when it ended any other way.
func endedWith(ctx context.Context) error {
	var end *streamEnd
	if errors.As(context.Cause(ctx), &end) {
		return end.err
	}
	return nil
}

// end ends sess with err as the status its agent receives. It is the
// one mechanism every deliberate end of a stream shares: EndStream from
// outside the stream, and the stream's own credential bound from inside
// it (ADR-0016 as amended). The stream's own cleanup runs as for any
// other end, the offline mark included.
func (sess *session) end(err error) {
	sess.cancel(&streamEnd{err: err})
}

// EndStream ends the live sync stream of id, if this replica holds one,
// with err as the status its agent receives, and reports whether it held
// one. It is the gateway's one lever for ending a stream from outside it;
// retirement pulls it, with the opaque refusal. A stream whose credential
// expires is ended by the same mechanism from inside its own session,
// with a refusal that names the expiry (boundByCredential).
func (s *Server) EndStream(id identity.WorkloadID, err error) bool {
	sess := s.sessions.get(id)
	if sess == nil {
		return false
	}
	sess.end(err)
	return true
}

// EndRetired implements fleet.Streams: it ends the live stream of a
// retired workload with the opaque refusal, if this replica holds it.
func (s *Server) EndRetired(id identity.WorkloadID) bool {
	if !s.EndStream(id, errRefused) {
		return false
	}
	s.log.Info("sync stream ended: the workload was retired", "workload_id", id)
	return true
}

// direct routes one directive to the stream of the workload it names.
func (s *Server) direct(d compiler.Directive) {
	switch d.Kind {
	case compiler.DirectiveReconnect:
		s.directReconnect(d.ID)
	case compiler.DirectiveEnd:
		if !s.EndRetired(d.ID) {
			// Every replica hears the directive and at most one holds
			// the stream; the rest have nothing to do.
			s.log.Debug("stream end directive: this replica holds no stream for the workload", "workload_id", d.ID)
		}
	}
}

// reconcile brings one live stream back in line with persisted state when
// the listener (re)connects, since announcements and directives published
// while it was down are gone: a stream whose workload was retired
// meanwhile is ended, which is what repairs a lost end directive
// (ADR-0018 as amended, ADR-0023); any other is pushed what it missed.
func (s *Server) reconcile(sess *session) {
	if err := s.enroll.CheckNotRetired(sess.ctx, sess.id); err != nil {
		if isRetired(err) {
			s.EndRetired(sess.id)
			return
		}
		if sess.ctx.Err() == nil {
			s.log.Error("checking a live stream's workload for retirement", "workload_id", sess.id, "error", err)
		}
	}
	s.pushLatest(sess)
}
