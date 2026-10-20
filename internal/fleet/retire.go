package fleet

import (
	"context"
	"log/slog"
	"time"

	"github.com/innerwall-dev/innerwall/internal/authn"
	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/ingest"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// RetireSubject is what a retirement reads from the workload's row, under
// its lock: whether and when it was already retired, and what the silence
// derivation needs, the last flow report's receipt and the agent process
// the latest heartbeat proved running (each nil when none is recorded).
type RetireSubject struct {
	RetiredAt        *time.Time
	LastFlowReportAt *time.Time
	Process          *registry.AgentProcess
}

// RetireTx is the store's side of one retirement: the render transaction,
// plus the locked row, the stamp, the closed silence, and the directive
// that ends the workload's live stream, all committed together.
type RetireTx interface {
	compiler.Tx
	// LockWorkload locks the workload's row for the rest of the
	// transaction and reads it, or returns registry.ErrWorkloadUnknown.
	// Flow ingest's receipt and label writes take the same lock.
	LockWorkload(ctx context.Context, id identity.WorkloadID) (*RetireSubject, error)
	// StampRetired records the retirement instant.
	StampRetired(ctx context.Context, id identity.WorkloadID, at time.Time) error
	// WriteGaps stores derived gaps through the flow store, inside this
	// transaction.
	WriteGaps(ctx context.Context, id identity.WorkloadID, gaps []flowstore.Gap) error
	// DirectEnd asks every replica, once the transaction commits, to end
	// the workload's live stream if it holds it. Delivery is best-effort
	// by design, as for every directive (ADR-0018 as amended).
	DirectEnd(ctx context.Context, id identity.WorkloadID) error
}

// Streams ends the live sync streams this process holds. The gateway
// implements it; a process that holds no streams, such as the command
// line, has none, and the directive alone reaches the replica that does.
type Streams interface {
	// EndRetired ends the live stream of a retired workload, if this
	// process holds it, with the refusal every door gives a retired
	// identity, and reports whether it held one.
	EndRetired(id identity.WorkloadID) bool
}

// Retirement acknowledges one retirement: the instant, the render it
// triggered, and the silence it closed as a stored gap, nil when none was
// open.
type Retirement struct {
	ID        identity.WorkloadID
	RetiredAt time.Time
	Rendered  *compiler.Report
	Closed    *flowstore.Gap
}

// Retire retires a workload (ADR-0023). It is one way. In one transaction
// under the render lock it locks the workload's row, refuses an unknown
// workload (registry.ErrWorkloadUnknown) and one already retired (a
// *registry.RetiredError naming the instant), stamps the retirement,
// closes an open agent silence as a stored gap by ADR-0019's derivation at
// the retirement instant, renders so that the workload's addresses leave
// every peer set and every workload whose output changed advances, and
// publishes the directive that ends its live stream on whichever replica
// holds it. Once committed, a stream this process holds is ended directly.
// The row stays, so history keeps naming the identity. The caller is the
// principal its context carries; one without is refused.
func (s *Service) Retire(ctx context.Context, id identity.WorkloadID) (*Retirement, error) {
	p, ok := authn.FromContext(ctx)
	if !ok {
		return nil, authn.ErrNoPrincipal
	}
	res := &Retirement{ID: id}
	err := s.Store.RetireTx(ctx, func(ctx context.Context, tx RetireTx) error {
		subj, err := tx.LockWorkload(ctx, id)
		if err != nil {
			return err
		}
		if subj.RetiredAt != nil {
			return &registry.RetiredError{ID: id, RetiredAt: *subj.RetiredAt}
		}
		// Postgres keeps microseconds; the instant returned is the one
		// stored, so a later refusal names the same one.
		at := s.now().Truncate(time.Microsecond)
		if err := tx.StampRetired(ctx, id, at); err != nil {
			return err
		}
		if gap, ok := ingest.SilenceAtRetirement(subj.LastFlowReportAt, subj.Process, at, s.FlowWindow); ok {
			if err := tx.WriteGaps(ctx, id, []flowstore.Gap{gap}); err != nil {
				return err
			}
			res.Closed = &gap
		}
		report, err := s.Engine.RenderIn(ctx, tx)
		if err != nil {
			return err
		}
		if err := tx.DirectEnd(ctx, id); err != nil {
			return err
		}
		res.RetiredAt, res.Rendered = at, report
		return nil
	})
	if err != nil {
		return nil, err
	}
	held := s.Streams != nil && s.Streams.EndRetired(id)
	attrs := []any{
		"workload_id", id, "retired_at", res.RetiredAt, "operator", operatorName(p),
		"versions_advanced", len(res.Rendered.Changed), "stream_ended_here", held,
	}
	if res.Closed != nil {
		attrs = append(attrs, "silence_closed_from", res.Closed.From)
	}
	s.log().Info("workload retired", attrs...)
	return res, nil
}

// operatorName is how a log line names the acting operator: the display
// name when one is set, otherwise the one operator's role.
func operatorName(p *authn.Principal) string {
	if p.DisplayName != nil && *p.DisplayName != "" {
		return *p.DisplayName
	}
	return "operator"
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// refuseRetired refuses a write naming a workload the operator retired:
// its attributes are frozen as they stood (ADR-0023).
func refuseRetired(w *registry.Workload) error {
	if w.Retired() {
		return &registry.RetiredError{ID: w.ID, RetiredAt: *w.RetiredAt}
	}
	return nil
}
