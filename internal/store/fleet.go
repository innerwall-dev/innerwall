package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
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
