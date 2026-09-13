package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/store/db"
)

var _ registry.Store = (*Store)(nil)

// ListWorkloads implements registry.Store.
func (s *Store) ListWorkloads(ctx context.Context) ([]registry.Workload, error) {
	return listWorkloads(ctx, s.q)
}

func listWorkloads(ctx context.Context, q *db.Queries) ([]registry.Workload, error) {
	rows, err := q.ListWorkloads(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing workloads: %w", err)
	}
	labels, err := q.ListAllWorkloadLabels(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing workload labels: %w", err)
	}
	addrs, err := q.ListAllWorkloadAddresses(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing workload addresses: %w", err)
	}
	labelIndex := map[uuid.UUID][]registry.Label{}
	for _, l := range labels {
		labelIndex[l.WorkloadID] = append(labelIndex[l.WorkloadID], registry.Label{Key: l.Key, Value: l.Value})
	}
	addrIndex := map[uuid.UUID][]netip.Addr{}
	for _, a := range addrs {
		if addr, err := netip.ParseAddr(a.Address); err == nil {
			addrIndex[a.WorkloadID] = append(addrIndex[a.WorkloadID], addr)
		}
	}
	out := make([]registry.Workload, 0, len(rows))
	for i := range rows {
		w, err := workloadFromRow(&rows[i], labelIndex[rows[i].ID], addrIndex[rows[i].ID])
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, nil
}

func workloadFromRow(row *db.Workload, labels []registry.Label, addrs []netip.Addr) (*registry.Workload, error) {
	w := &registry.Workload{
		ID:                  identity.FromUUID(row.ID),
		Hostname:            row.Hostname,
		Labels:              labels,
		Mode:                innerwallv1.EnforcementMode(row.Mode),
		Addresses:           addrs,
		Agent:               registry.AgentInfo{Version: row.AgentVersion, Capabilities: row.AgentCapabilities},
		EnrolledAt:          row.EnrolledAt,
		LastSeenAt:          row.LastSeenAt,
		SyncState:           innerwallv1.SyncState(row.SyncState),
		AppliedVersion:      uint64(row.AppliedPolicyVersion), //nolint:gosec // non-negative by construction
		SyncError:           row.SyncError,
		DroppedFlowRecords:  uint64(row.DroppedFlowRecords), //nolint:gosec // non-negative by construction
		CredentialExpiresAt: row.CredentialExpiresAt,
	}
	if labels == nil {
		w.Labels = []registry.Label{}
	}
	if len(row.Facts) > 0 {
		w.Facts = &innerwallv1.HostFacts{}
		if err := proto.Unmarshal(row.Facts, w.Facts); err != nil {
			return nil, fmt.Errorf("store: parsing facts of %s: %w", row.ID, err)
		}
	}
	return w, nil
}

// LookupWorkload implements registry.Store.
func (s *Store) LookupWorkload(ctx context.Context, id identity.WorkloadID) (*registry.Workload, error) {
	row, err := s.q.GetWorkload(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, registry.ErrWorkloadUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up workload: %w", err)
	}
	labelRows, err := s.q.ListWorkloadLabels(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("store: listing workload labels: %w", err)
	}
	labels := make([]registry.Label, 0, len(labelRows))
	for _, l := range labelRows {
		labels = append(labels, registry.Label{Key: l.Key, Value: l.Value})
	}
	addrRows, err := s.q.ListWorkloadAddresses(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("store: listing workload addresses: %w", err)
	}
	addrs := make([]netip.Addr, 0, len(addrRows))
	for _, a := range addrRows {
		if addr, err := netip.ParseAddr(a); err == nil {
			addrs = append(addrs, addr)
		}
	}
	return workloadFromRow(&row, labels, addrs)
}

// ListListeningServices implements registry.Store.
func (s *Store) ListListeningServices(ctx context.Context, id identity.WorkloadID) ([]registry.ListeningService, error) {
	rows, err := s.q.ListWorkloadListeningServices(ctx, id.UUID())
	if err != nil {
		return nil, fmt.Errorf("store: listing listening services: %w", err)
	}
	out := make([]registry.ListeningService, 0, len(rows))
	for _, r := range rows {
		out = append(out, registry.ListeningService{Protocol: innerwallv1.Protocol(r.Protocol), Port: uint32(r.Port), ProcessName: r.ProcessName, ProcessPath: r.ProcessPath}) //nolint:gosec // ports are non-negative
	}
	return out, nil
}

// SetWorkloadLabels implements registry.Store.
func (s *Store) SetWorkloadLabels(ctx context.Context, id identity.WorkloadID, labels []registry.Label) error {
	return s.tx(ctx, func(q *db.Queries) error {
		if _, err := q.GetWorkload(ctx, id.UUID()); errors.Is(err, pgx.ErrNoRows) {
			return registry.ErrWorkloadUnknown
		} else if err != nil {
			return fmt.Errorf("store: looking up workload: %w", err)
		}
		if err := q.DeleteWorkloadLabels(ctx, id.UUID()); err != nil {
			return fmt.Errorf("store: replacing labels: %w", err)
		}
		for _, l := range labels {
			if err := q.AddWorkloadLabel(ctx, db.AddWorkloadLabelParams{WorkloadID: id.UUID(), Key: l.Key, Value: l.Value}); err != nil {
				return fmt.Errorf("store: adding label: %w", err)
			}
		}
		return nil
	})
}

// SetWorkloadMode implements registry.Store.
func (s *Store) SetWorkloadMode(ctx context.Context, id identity.WorkloadID, mode innerwallv1.EnforcementMode) error {
	n, err := s.q.SetWorkloadMode(ctx, db.SetWorkloadModeParams{ID: id.UUID(), Mode: int32(mode)})
	if err != nil {
		return fmt.Errorf("store: setting mode: %w", err)
	}
	if n == 0 {
		return registry.ErrWorkloadUnknown
	}
	return nil
}

// RecordFacts implements registry.Store.
func (s *Store) RecordFacts(ctx context.Context, id identity.WorkloadID, facts *innerwallv1.HostFacts, now time.Time) (bool, error) {
	changed := false
	err := s.tx(ctx, func(q *db.Queries) error {
		var factBytes []byte
		if facts != nil {
			b, err := proto.MarshalOptions{Deterministic: true}.Marshal(facts)
			if err != nil {
				return fmt.Errorf("store: encoding facts: %w", err)
			}
			factBytes = b
		}
		n, err := q.RecordWorkloadInventory(ctx, db.RecordWorkloadInventoryParams{ID: id.UUID(), Facts: factBytes, Hostname: facts.GetHostname(), LastSeenAt: &now})
		if err != nil {
			return fmt.Errorf("store: recording facts: %w", err)
		}
		if n == 0 {
			return registry.ErrWorkloadUnknown
		}
		old, err := q.ListWorkloadAddresses(ctx, id.UUID())
		if err != nil {
			return fmt.Errorf("store: listing addresses: %w", err)
		}
		current := registry.AddressesFromFacts(facts)
		if sameAddresses(old, current) {
			return nil
		}
		changed = true
		if err := q.DeleteWorkloadAddresses(ctx, id.UUID()); err != nil {
			return fmt.Errorf("store: replacing addresses: %w", err)
		}
		for _, a := range current {
			if err := q.AddWorkloadAddress(ctx, db.AddWorkloadAddressParams{WorkloadID: id.UUID(), Address: a.String()}); err != nil {
				return fmt.Errorf("store: adding address: %w", err)
			}
		}
		return nil
	})
	return changed, err
}

// RecordListeningServices implements registry.Store.
func (s *Store) RecordListeningServices(ctx context.Context, id identity.WorkloadID, services []registry.ListeningService, now time.Time) error {
	return s.tx(ctx, func(q *db.Queries) error {
		n, err := q.RecordWorkloadHeartbeatSeen(ctx, db.RecordWorkloadHeartbeatSeenParams{ID: id.UUID(), LastSeenAt: &now})
		if err != nil {
			return fmt.Errorf("store: refreshing last seen: %w", err)
		}
		if n == 0 {
			return registry.ErrWorkloadUnknown
		}
		if err := q.DeleteWorkloadListeningServices(ctx, id.UUID()); err != nil {
			return fmt.Errorf("store: replacing listening services: %w", err)
		}
		for _, svc := range services {
			if err := q.AddWorkloadListeningService(ctx, db.AddWorkloadListeningServiceParams{
				WorkloadID: id.UUID(), Protocol: int32(svc.Protocol), Port: int32(svc.Port), ProcessName: svc.ProcessName, ProcessPath: svc.ProcessPath, //nolint:gosec // ports fit
			}); err != nil {
				return fmt.Errorf("store: adding listening service: %w", err)
			}
		}
		return nil
	})
}

// sameAddresses compares the persisted textual addresses (sorted by the
// query) with the derived set (sorted by AddressesFromFacts).
func sameAddresses(old []string, current []netip.Addr) bool {
	if len(old) != len(current) {
		return false
	}
	seen := make(map[string]struct{}, len(old))
	for _, a := range old {
		seen[a] = struct{}{}
	}
	for _, a := range current {
		if _, ok := seen[a.String()]; !ok {
			return false
		}
	}
	return true
}

// RecordAgent implements registry.Store.
func (s *Store) RecordAgent(ctx context.Context, id identity.WorkloadID, agent registry.AgentInfo, appliedVersion uint64, now time.Time) error {
	caps := agent.Capabilities
	if caps == nil {
		caps = []string{}
	}
	n, err := s.q.RecordWorkloadAgent(ctx, db.RecordWorkloadAgentParams{
		ID: id.UUID(), AgentVersion: agent.Version, AgentCapabilities: caps,
		AppliedPolicyVersion: int64(appliedVersion), LastSeenAt: &now, //nolint:gosec // versions are small
	})
	if err != nil {
		return fmt.Errorf("store: recording agent: %w", err)
	}
	if n == 0 {
		return registry.ErrWorkloadUnknown
	}
	return nil
}

// RecordHeartbeat implements registry.Store.
func (s *Store) RecordHeartbeat(ctx context.Context, id identity.WorkloadID, dropped uint64, now time.Time) error {
	n, err := s.q.RecordWorkloadHeartbeat(ctx, db.RecordWorkloadHeartbeatParams{ID: id.UUID(), LastSeenAt: &now, DroppedFlowRecords: int64(dropped)}) //nolint:gosec // counter
	if err != nil {
		return fmt.Errorf("store: recording heartbeat: %w", err)
	}
	if n == 0 {
		return registry.ErrWorkloadUnknown
	}
	return nil
}

// SetSyncState implements registry.Store.
func (s *Store) SetSyncState(ctx context.Context, id identity.WorkloadID, state innerwallv1.SyncState, detail string, now time.Time) error {
	n, err := s.q.SetWorkloadSyncState(ctx, db.SetWorkloadSyncStateParams{ID: id.UUID(), SyncState: int32(state), SyncError: detail, LastSeenAt: &now})
	if err != nil {
		return fmt.Errorf("store: setting sync state: %w", err)
	}
	if n == 0 {
		return registry.ErrWorkloadUnknown
	}
	return nil
}

// RecordApplied implements registry.Store.
func (s *Store) RecordApplied(ctx context.Context, id identity.WorkloadID, version uint64, state innerwallv1.SyncState, now time.Time) error {
	n, err := s.q.RecordWorkloadApplied(ctx, db.RecordWorkloadAppliedParams{ID: id.UUID(), AppliedPolicyVersion: int64(version), SyncState: int32(state), LastSeenAt: &now}) //nolint:gosec // versions are small
	if err != nil {
		return fmt.Errorf("store: recording applied version: %w", err)
	}
	if n == 0 {
		return registry.ErrWorkloadUnknown
	}
	return nil
}
