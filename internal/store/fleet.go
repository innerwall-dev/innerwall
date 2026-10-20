package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/store/db"
)

var _ fleet.Store = (*Store)(nil)

// ModeChangeTx implements fleet.Store: one transaction holding the render
// lock, in which the change is resolved, recorded, applied, and rendered.
func (s *Store) ModeChangeTx(ctx context.Context, fn func(ctx context.Context, tx fleet.ModeChangeTx) error) error {
	return s.tx(ctx, func(q *db.Queries) error {
		if err := q.AcquireRenderLock(ctx, renderLockKey); err != nil {
			return fmt.Errorf("store: acquiring render lock: %w", err)
		}
		return fn(ctx, &modeChangeTx{renderTx: renderTx{q: q}})
	})
}

type modeChangeTx struct {
	renderTx
}

// RecordModeChange implements fleet.ModeChangeTx.
func (t *modeChangeTx) RecordModeChange(ctx context.Context, rec *fleet.ModeChangeRecord) error {
	var selector []byte
	if rec.Selector != nil {
		b, err := json.Marshal(map[string][]string(rec.Selector))
		if err != nil {
			return fmt.Errorf("store: encoding selector: %w", err)
		}
		selector = b
	}
	if err := t.q.CreateModeChange(ctx, db.CreateModeChangeParams{
		ID: rec.ID, CreatedAt: rec.CreatedAt, TargetMode: int32(rec.TargetMode), Selector: selector,
		ExpectedMatchCount: int32(rec.ExpectedMatchCount), Matched: int32(len(rec.Workloads)), DesiredUpdated: int32(rec.DesiredUpdated), //nolint:gosec // small counts
	}); err != nil {
		return fmt.Errorf("store: recording mode change: %w", err)
	}
	for _, w := range rec.Workloads {
		if err := t.q.AddModeChangeWorkload(ctx, db.AddModeChangeWorkloadParams{ModeChangeID: rec.ID, WorkloadID: w.ID.UUID(), PreviousMode: int32(w.PreviousMode)}); err != nil {
			return fmt.Errorf("store: recording mode change workload: %w", err)
		}
	}
	return nil
}

// SetWorkloadModes implements fleet.ModeChangeTx.
func (t *modeChangeTx) SetWorkloadModes(ctx context.Context, ids []identity.WorkloadID, mode innerwallv1.EnforcementMode) (int, error) {
	uuids := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		uuids = append(uuids, id.UUID())
	}
	n, err := t.q.SetWorkloadModes(ctx, db.SetWorkloadModesParams{Mode: int32(mode), WorkloadIds: uuids})
	if err != nil {
		return 0, fmt.Errorf("store: setting modes: %w", err)
	}
	return int(n), nil
}

// GetModeChange returns a recorded mode change with its resolved set, for
// audit and for tests; there is no operator read of it yet.
func (s *Store) GetModeChange(ctx context.Context, id uuid.UUID) (*fleet.ModeChangeRecord, error) {
	row, err := s.q.GetModeChange(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("store: looking up mode change: %w", err)
	}
	rec := &fleet.ModeChangeRecord{ID: row.ID, CreatedAt: row.CreatedAt, TargetMode: innerwallv1.EnforcementMode(row.TargetMode), ExpectedMatchCount: int(row.ExpectedMatchCount), DesiredUpdated: int(row.DesiredUpdated)}
	if len(row.Selector) > 0 {
		var sel map[string][]string
		if err := json.Unmarshal(row.Selector, &sel); err != nil {
			return nil, fmt.Errorf("store: decoding selector: %w", err)
		}
		rec.Selector = policy.Selector(sel)
	}
	workloads, err := s.q.ListModeChangeWorkloads(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("store: listing mode change workloads: %w", err)
	}
	for _, w := range workloads {
		rec.Workloads = append(rec.Workloads, fleet.ModeChangeWorkload{ID: identity.FromUUID(w.WorkloadID), PreviousMode: innerwallv1.EnforcementMode(w.PreviousMode)})
	}
	return rec, nil
}

// LoadRenderState implements fleet.Store: the render inputs and the
// persisted policies read together in one read-only, repeatable-read
// transaction, so a dry run sees one consistent state and takes no lock.
func (s *Store) LoadRenderState(ctx context.Context) (*compiler.Inputs, map[identity.WorkloadID]*innerwallv1.WorkloadPolicy, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, nil, fmt.Errorf("store: beginning read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rtx := &renderTx{q: s.q.WithTx(tx)}
	in, err := rtx.LoadInputs(ctx)
	if err != nil {
		return nil, nil, err
	}
	previous, err := rtx.LoadPolicies(ctx)
	if err != nil {
		return nil, nil, err
	}
	return in, previous, nil
}

// RetireTx implements fleet.Store: one transaction holding the render
// lock, in which a retirement locks the workload's row, stamps it, writes
// the silence it closes through the flow store bound to the same
// transaction, renders, and publishes the directive that ends the
// workload's stream, delivered on commit (ADR-0023). The render lock is
// taken before the row lock, and nothing takes them in the other order.
func (s *Store) RetireTx(ctx context.Context, fn func(ctx context.Context, tx fleet.RetireTx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err := q.AcquireRenderLock(ctx, renderLockKey); err != nil {
		return fmt.Errorf("store: acquiring render lock: %w", err)
	}
	if err := fn(ctx, &retireTx{renderTx: renderTx{q: q}, gaps: s.flows.Within(tx)}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: committing transaction: %w", err)
	}
	return nil
}

type retireTx struct {
	renderTx
	gaps flowstore.GapWriter
}

// LockWorkload implements fleet.RetireTx.
func (t *retireTx) LockWorkload(ctx context.Context, id identity.WorkloadID) (*fleet.RetireSubject, error) {
	row, err := t.q.LockWorkload(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, registry.ErrWorkloadUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("store: locking workload: %w", err)
	}
	subj := &fleet.RetireSubject{RetiredAt: row.RetiredAt, LastFlowReportAt: row.LastFlowReportAt}
	if row.AgentStartedAt != nil && row.LastHeartbeatAt != nil {
		subj.Process = &registry.AgentProcess{StartedAt: *row.AgentStartedAt, HeartbeatAt: *row.LastHeartbeatAt}
	}
	return subj, nil
}

// StampRetired implements fleet.RetireTx.
func (t *retireTx) StampRetired(ctx context.Context, id identity.WorkloadID, at time.Time) error {
	n, err := t.q.RetireWorkload(ctx, db.RetireWorkloadParams{ID: id.UUID(), RetiredAt: &at})
	if err != nil {
		return fmt.Errorf("store: retiring workload: %w", err)
	}
	if n == 0 {
		// The row is locked and was read active in this transaction.
		return fmt.Errorf("store: workload %s changed under its lock", id)
	}
	return nil
}

// WriteGaps implements fleet.RetireTx through the flow store.
func (t *retireTx) WriteGaps(ctx context.Context, id identity.WorkloadID, gaps []flowstore.Gap) error {
	return t.gaps.WriteGaps(ctx, id, gaps)
}

// DirectEnd implements fleet.RetireTx.
func (t *retireTx) DirectEnd(ctx context.Context, id identity.WorkloadID) error {
	return directEnd(ctx, t.q, id)
}
