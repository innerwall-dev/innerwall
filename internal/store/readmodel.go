package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/store/db"
)

var _ readmodel.Store = (*Store)(nil)

// ListWorkloadPage implements readmodel.Store. The page is one statement
// in fleet order; the page's labels, addresses, and listening services
// are three more, each over the page's ids.
func (s *Store) ListWorkloadPage(ctx context.Context, q readmodel.WorkloadPageQuery) ([]readmodel.WorkloadRecord, error) {
	ids := make([]uuid.UUID, 0, len(q.IDs))
	for _, id := range q.IDs {
		ids = append(ids, id.UUID())
	}
	// The first page continues after a rank below every row.
	cursorRank, cursorSeen, cursorID := int32(-1), time.Time{}, uuid.Nil
	if q.After != nil {
		cursorRank, cursorSeen, cursorID = q.After.SyncRank, q.After.SeenKey, q.After.ID.UUID()
	}
	limit := q.Limit
	if limit <= 0 {
		limit = readmodel.DefaultWorkloadPageLimit
	}
	rows, err := s.q.ListWorkloadPage(ctx, db.ListWorkloadPageParams{
		WorkloadIds: ids, Mode: int32(q.Mode), SyncState: int32(q.SyncState),
		CursorRank: cursorRank, CursorSeen: cursorSeen, CursorID: cursorID,
		RowLimit: int32(limit), //nolint:gosec // bounded by the caller
	})
	if err != nil {
		return nil, fmt.Errorf("store: listing workload page: %w", err)
	}
	pageIDs := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		pageIDs = append(pageIDs, rows[i].ID)
	}
	children, err := s.workloadChildren(ctx, pageIDs)
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.WorkloadRecord, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		base := db.Workload{
			ID: r.ID, RegionID: r.RegionID, ProvisioningTokenID: r.ProvisioningTokenID, Hostname: r.Hostname, EnrolledAt: r.EnrolledAt,
			CredentialSerial: r.CredentialSerial, CredentialExpiresAt: r.CredentialExpiresAt, LastRenewedAt: r.LastRenewedAt,
			Mode: r.Mode, Facts: r.Facts, AgentVersion: r.AgentVersion, AgentCapabilities: r.AgentCapabilities, LastSeenAt: r.LastSeenAt,
			SyncState: r.SyncState, AppliedPolicyVersion: r.AppliedPolicyVersion, SyncError: r.SyncError, DroppedFlowRecords: r.DroppedFlowRecords,
			CredentialRenewalError: r.CredentialRenewalError,
		}
		w, err := workloadFromRow(&base, children.labels[r.ID], children.addrs[r.ID])
		if err != nil {
			return nil, err
		}
		rec := readmodel.WorkloadRecord{Workload: *w, LastRenewedAt: r.LastRenewedAt, LatestRenderedAt: r.LatestRenderedAt, ListeningServices: children.services[r.ID], SyncRank: r.SyncRank, SeenKey: r.SeenKey}
		if r.LatestVersion.Valid {
			rec.LatestVersion = uint64(r.LatestVersion.Int64) //nolint:gosec // versions are non-negative
		}
		out = append(out, rec)
	}
	return out, nil
}

// GetWorkloadRecord implements readmodel.Store.
func (s *Store) GetWorkloadRecord(ctx context.Context, id identity.WorkloadID) (*readmodel.WorkloadRecord, error) {
	row, err := s.q.GetWorkloadWithPolicy(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, registry.ErrWorkloadUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up workload: %w", err)
	}
	children, err := s.workloadChildren(ctx, []uuid.UUID{row.Workload.ID})
	if err != nil {
		return nil, err
	}
	w, err := workloadFromRow(&row.Workload, children.labels[row.Workload.ID], children.addrs[row.Workload.ID])
	if err != nil {
		return nil, err
	}
	rec := &readmodel.WorkloadRecord{Workload: *w, LastRenewedAt: row.Workload.LastRenewedAt, LatestRenderedAt: row.LatestRenderedAt, ListeningServices: children.services[row.Workload.ID]}
	if row.LatestVersion.Valid {
		rec.LatestVersion = uint64(row.LatestVersion.Int64) //nolint:gosec // versions are non-negative
	}
	return rec, nil
}

type workloadChildren struct {
	labels   map[uuid.UUID][]registry.Label
	addrs    map[uuid.UUID][]netip.Addr
	services map[uuid.UUID][]registry.ListeningService
}

func (s *Store) workloadChildren(ctx context.Context, ids []uuid.UUID) (*workloadChildren, error) {
	c := &workloadChildren{labels: map[uuid.UUID][]registry.Label{}, addrs: map[uuid.UUID][]netip.Addr{}, services: map[uuid.UUID][]registry.ListeningService{}}
	if len(ids) == 0 {
		return c, nil
	}
	labels, err := s.q.ListWorkloadLabelsFor(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("store: listing workload labels: %w", err)
	}
	for _, l := range labels {
		c.labels[l.WorkloadID] = append(c.labels[l.WorkloadID], registry.Label{Key: l.Key, Value: l.Value})
	}
	addrs, err := s.q.ListWorkloadAddressesFor(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("store: listing workload addresses: %w", err)
	}
	for _, a := range addrs {
		if addr, err := netip.ParseAddr(a.Address); err == nil {
			c.addrs[a.WorkloadID] = append(c.addrs[a.WorkloadID], addr)
		}
	}
	services, err := s.q.ListWorkloadListeningServicesFor(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("store: listing listening services: %w", err)
	}
	for _, r := range services {
		c.services[r.WorkloadID] = append(c.services[r.WorkloadID], registry.ListeningService{Protocol: innerwallv1.Protocol(r.Protocol), Port: uint32(r.Port), ProcessName: r.ProcessName, ProcessPath: r.ProcessPath}) //nolint:gosec // ports are non-negative
	}
	return c, nil
}

// ListWorkloadLabelIndex implements readmodel.Store.
func (s *Store) ListWorkloadLabelIndex(ctx context.Context) (map[identity.WorkloadID]map[string]string, error) {
	rows, err := s.q.ListWorkloads(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing workloads: %w", err)
	}
	labels, err := s.q.ListAllWorkloadLabels(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing workload labels: %w", err)
	}
	out := make(map[identity.WorkloadID]map[string]string, len(rows))
	for i := range rows {
		out[identity.FromUUID(rows[i].ID)] = map[string]string{}
	}
	for _, l := range labels {
		id := identity.FromUUID(l.WorkloadID)
		if out[id] == nil {
			out[id] = map[string]string{}
		}
		out[id][l.Key] = l.Value
	}
	return out, nil
}
