package flowstore

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/store/db"
)

// retentionLockKey is the advisory lock a pruning run takes so that only
// one replica prunes at a time (ADR-0017). Distinct from the render lock.
const retentionLockKey int64 = 0x1_4e4e_4552_5746 // arbitrary, fixed

// pruneBatch bounds the rows one delete statement removes.
const pruneBatch = 5000

// Postgres is the FlowStore over the control plane's database.
type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

var _ FlowStore = (*Postgres)(nil)

// NewPostgres wraps an open pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool, q: db.New(pool)}
}

// WriteWindow implements FlowStore. The rows are copied in one statement
// and the totals are upserted in one batch, all in one transaction.
func (p *Postgres) WriteWindow(ctx context.Context, w Window) (int, error) {
	if len(w.Records) == 0 {
		return 0, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("flowstore: beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.q.WithTx(tx)

	rows := make([]db.InsertFlowWindowsParams, 0, len(w.Records))
	for i := range w.Records {
		r := &w.Records[i]
		rows = append(rows, db.InsertFlowWindowsParams{
			WorkloadID:      w.WorkloadID.UUID(),
			WindowStart:     w.Start,
			WindowEnd:       w.End,
			PeerKind:        int32(r.Peer.Kind),
			PeerKey:         r.Peer.Key,
			PeerLabels:      encodeLabels(r.Peer.Labels),
			SrcAddress:      r.SrcAddress.String(),
			DstAddress:      r.DstAddress.String(),
			DstPort:         int32(r.DstPort),
			Protocol:        int32(r.Protocol),
			Direction:       int32(r.Direction),
			Decision:        int32(r.Decision),
			MatchedRuleID:   r.MatchedRuleID,
			ConnectionCount: int64(r.ConnectionCount), //nolint:gosec // counters are far below the signed range
			ByteCount:       int64(r.ByteCount),       //nolint:gosec // counters are far below the signed range
			FirstSeen:       r.FirstSeen,
			LastSeen:        r.LastSeen,
			ProcessName:     r.ProcessName,
		})
	}
	n, err := q.InsertFlowWindows(ctx, rows)
	if err != nil {
		return 0, fmt.Errorf("flowstore: inserting windows: %w", err)
	}

	totals := AggregateTotals(w.Records)
	params := make([]db.UpsertFlowTotalParams, 0, len(totals))
	for i := range totals {
		r := &totals[i]
		params = append(params, db.UpsertFlowTotalParams{
			WorkloadID:      w.WorkloadID.UUID(),
			PeerKind:        int32(r.Peer.Kind),
			PeerKey:         r.Peer.Key,
			PeerLabels:      encodeLabels(r.Peer.Labels),
			DstPort:         int32(r.DstPort),
			Protocol:        int32(r.Protocol),
			Direction:       int32(r.Direction),
			Decision:        int32(r.Decision),
			MatchedRuleID:   r.MatchedRuleID,
			FirstSeen:       r.FirstSeen,
			LastSeen:        r.LastSeen,
			ConnectionCount: int64(r.ConnectionCount), //nolint:gosec // counters are far below the signed range
			ByteCount:       int64(r.ByteCount),       //nolint:gosec // counters are far below the signed range
		})
	}
	var batchErr error
	results := q.UpsertFlowTotal(ctx, params)
	results.Exec(func(_ int, err error) {
		if err != nil && batchErr == nil {
			batchErr = err
		}
	})
	if err := results.Close(); err != nil && batchErr == nil {
		batchErr = err
	}
	if batchErr != nil {
		return 0, fmt.Errorf("flowstore: upserting totals: %w", batchErr)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("flowstore: committing window: %w", err)
	}
	return int(n), nil
}

// ListWindows implements FlowStore.
func (p *Postgres) ListWindows(ctx context.Context, q WindowQuery) ([]WindowRow, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	rows, err := p.q.ListFlowWindows(ctx, db.ListFlowWindowsParams{
		WorkloadID: q.WorkloadID.UUID(), Since: q.Since, Until: q.Until,
		Decision: int32(q.Decision), RowLimit: int32(limit), //nolint:gosec // bounded by the caller
	})
	if err != nil {
		return nil, fmt.Errorf("flowstore: listing windows: %w", err)
	}
	out := make([]WindowRow, 0, len(rows))
	for i := range rows {
		out = append(out, windowRowFromDB(&rows[i]))
	}
	return out, nil
}

func windowRowFromDB(r *db.FlowWindow) WindowRow {
	return WindowRow{
		ID:          r.ID,
		WorkloadID:  identity.FromUUID(r.WorkloadID),
		WindowStart: r.WindowStart,
		WindowEnd:   r.WindowEnd,
		Record: Record{
			Peer:            Peer{Kind: PeerKind(r.PeerKind), Key: r.PeerKey, Labels: decodeLabels(r.PeerLabels)},
			SrcAddress:      parseAddr(r.SrcAddress),
			DstAddress:      parseAddr(r.DstAddress),
			DstPort:         uint16(r.DstPort), //nolint:gosec // checked by the schema
			Protocol:        innerwallv1.Protocol(r.Protocol),
			Direction:       innerwallv1.Direction(r.Direction),
			Decision:        innerwallv1.PolicyDecision(r.Decision),
			MatchedRuleID:   r.MatchedRuleID,
			ConnectionCount: uint64(r.ConnectionCount), //nolint:gosec // non-negative by construction
			ByteCount:       uint64(r.ByteCount),       //nolint:gosec // non-negative by construction
			FirstSeen:       r.FirstSeen,
			LastSeen:        r.LastSeen,
			ProcessName:     r.ProcessName,
		},
	}
}

func parseAddr(s string) netip.Addr {
	a, _ := netip.ParseAddr(s)
	return a
}

// Rollup implements FlowStore.
func (p *Postgres) Rollup(ctx context.Context, q RollupQuery) ([]RollupRow, error) {
	ids := make([]uuid.UUID, 0, len(q.WorkloadIDs))
	for _, id := range q.WorkloadIDs {
		ids = append(ids, id.UUID())
	}
	rows, err := p.q.RollupFlowWindows(ctx, db.RollupFlowWindowsParams{WorkloadIds: ids, Since: q.Since, Until: q.Until, Decision: int32(q.Decision)})
	if err != nil {
		return nil, fmt.Errorf("flowstore: rolling up windows: %w", err)
	}
	out := make([]RollupRow, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		out = append(out, RollupRow{
			Peer:            Peer{Kind: PeerKind(r.PeerKind), Key: r.PeerKey},
			DstPort:         uint16(r.DstPort), //nolint:gosec // checked by the schema
			Protocol:        innerwallv1.Protocol(r.Protocol),
			Decision:        innerwallv1.PolicyDecision(r.Decision),
			Workloads:       r.WorkloadCount,
			ConnectionCount: uint64(r.ConnectionCount), //nolint:gosec // non-negative by construction
			ByteCount:       uint64(r.ByteCount),       //nolint:gosec // non-negative by construction
			FirstSeen:       r.FirstSeen,
			LastSeen:        r.LastSeen,
		})
	}
	return out, nil
}

// ListTotals implements FlowStore.
func (p *Postgres) ListTotals(ctx context.Context, id identity.WorkloadID, decision innerwallv1.PolicyDecision) ([]Total, error) {
	rows, err := p.q.ListFlowTotals(ctx, db.ListFlowTotalsParams{WorkloadID: id.UUID(), Decision: int32(decision)})
	if err != nil {
		return nil, fmt.Errorf("flowstore: listing totals: %w", err)
	}
	out := make([]Total, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		out = append(out, Total{
			WorkloadID:      identity.FromUUID(r.WorkloadID),
			Peer:            Peer{Kind: PeerKind(r.PeerKind), Key: r.PeerKey, Labels: decodeLabels(r.PeerLabels)},
			DstPort:         uint16(r.DstPort), //nolint:gosec // checked by the schema
			Protocol:        innerwallv1.Protocol(r.Protocol),
			Direction:       innerwallv1.Direction(r.Direction),
			Decision:        innerwallv1.PolicyDecision(r.Decision),
			MatchedRuleID:   r.MatchedRuleID,
			FirstSeen:       r.FirstSeen,
			LastSeen:        r.LastSeen,
			ConnectionCount: uint64(r.ConnectionCount), //nolint:gosec // non-negative by construction
			ByteCount:       uint64(r.ByteCount),       //nolint:gosec // non-negative by construction
			WindowCount:     uint64(r.WindowCount),     //nolint:gosec // non-negative by construction
		})
	}
	return out, nil
}

// PruneWindows implements FlowStore. Each batch is its own transaction
// holding the retention lock, so a run never holds row locks for the whole
// backlog and another replica can take over between batches.
func (p *Postgres) PruneWindows(ctx context.Context, horizon time.Time) (int64, bool, error) {
	var total int64
	for {
		n, held, err := p.pruneBatch(ctx, horizon)
		if err != nil {
			return total, true, err
		}
		if !held {
			return total, total > 0, nil
		}
		total += n
		if n < pruneBatch {
			return total, true, nil
		}
	}
}

func (p *Postgres) pruneBatch(ctx context.Context, horizon time.Time) (int64, bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("flowstore: beginning prune: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.q.WithTx(tx)
	held, err := q.TryAcquireRetentionLock(ctx, retentionLockKey)
	if err != nil {
		return 0, false, fmt.Errorf("flowstore: acquiring retention lock: %w", err)
	}
	if !held {
		return 0, false, nil
	}
	n, err := q.DeleteFlowWindowsBefore(ctx, db.DeleteFlowWindowsBeforeParams{Horizon: horizon, BatchSize: pruneBatch})
	if err != nil {
		return 0, true, fmt.Errorf("flowstore: pruning windows: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, true, fmt.Errorf("flowstore: committing prune: %w", err)
	}
	return n, true, nil
}

// CountWindows returns the number of stored windows. Exposed for tests and
// diagnostics.
func (p *Postgres) CountWindows(ctx context.Context) (int64, error) {
	n, err := p.q.CountFlowWindows(ctx)
	if err != nil {
		return 0, fmt.Errorf("flowstore: counting windows: %w", err)
	}
	return n, nil
}
