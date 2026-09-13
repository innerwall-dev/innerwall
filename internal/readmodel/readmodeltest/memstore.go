// Package readmodeltest holds in-memory doubles of the read model's
// store and flow store for tests of the transports above it. They serve
// fixtures and record the queries they receive; the aggregation itself
// is the Postgres store's and is tested there.
package readmodeltest

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// MemStore implements readmodel.Store over fixtures.
type MemStore struct {
	mu            sync.Mutex
	Workloads     []readmodel.WorkloadRecord
	AddressGroups []policy.AddressGroup
	Rulesets      []policy.Ruleset
	Policies      map[identity.WorkloadID]*innerwallv1.WorkloadPolicy
	// LastPage is the last page query received.
	LastPage *readmodel.WorkloadPageQuery
}

var _ readmodel.Store = (*MemStore)(nil)

// ListWorkloads implements readmodel.Store.
func (m *MemStore) ListWorkloads(context.Context) ([]registry.Workload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]registry.Workload, 0, len(m.Workloads))
	for i := range m.Workloads {
		out = append(out, m.Workloads[i].Workload)
	}
	return out, nil
}

// ListAddressGroups implements readmodel.Store.
func (m *MemStore) ListAddressGroups(context.Context) ([]policy.AddressGroup, error) {
	return append([]policy.AddressGroup{}, m.AddressGroups...), nil
}

// ListRulesets implements readmodel.Store.
func (m *MemStore) ListRulesets(context.Context) ([]policy.Ruleset, error) {
	return append([]policy.Ruleset{}, m.Rulesets...), nil
}

// GetWorkloadPolicy implements readmodel.Store.
func (m *MemStore) GetWorkloadPolicy(_ context.Context, id identity.WorkloadID) (*innerwallv1.WorkloadPolicy, error) {
	return m.Policies[id], nil
}

// SyncRank is the fleet order of a sync state, as the store ranks it:
// degraded, offline, pending, synced, then anything else.
func SyncRank(s innerwallv1.SyncState) int32 {
	switch s {
	case innerwallv1.SyncState_SYNC_STATE_DEGRADED:
		return 0
	case innerwallv1.SyncState_SYNC_STATE_OFFLINE:
		return 1
	case innerwallv1.SyncState_SYNC_STATE_PENDING:
		return 2
	case innerwallv1.SyncState_SYNC_STATE_SYNCED:
		return 3
	case innerwallv1.SyncState_SYNC_STATE_UNSPECIFIED:
		return 4
	default:
		return 4
	}
}

func seenKey(t *time.Time) time.Time {
	if t == nil {
		return time.Unix(0, 0).UTC()
	}
	return t.UTC()
}

// ListWorkloadPage implements readmodel.Store with the same order, filter,
// and cursor semantics as the Postgres statement.
func (m *MemStore) ListWorkloadPage(_ context.Context, q readmodel.WorkloadPageQuery) ([]readmodel.WorkloadRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	qq := q
	m.LastPage = &qq
	want := map[identity.WorkloadID]bool{}
	for _, id := range q.IDs {
		want[id] = true
	}
	var out []readmodel.WorkloadRecord
	for i := range m.Workloads {
		r := m.Workloads[i]
		if len(want) > 0 && !want[r.ID] {
			continue
		}
		if q.Mode != 0 && r.Mode != q.Mode {
			continue
		}
		if q.SyncState != 0 && r.SyncState != q.SyncState {
			continue
		}
		r.SyncRank, r.SeenKey = SyncRank(r.SyncState), seenKey(r.LastSeenAt)
		if q.After != nil {
			a := q.After
			after := r.SyncRank > a.SyncRank || (r.SyncRank == a.SyncRank && (r.SeenKey.Before(a.SeenKey) || (r.SeenKey.Equal(a.SeenKey) && r.ID.String() > a.ID.String())))
			if !after {
				continue
			}
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.SyncRank != b.SyncRank {
			return a.SyncRank < b.SyncRank
		}
		if !a.SeenKey.Equal(b.SeenKey) {
			return a.SeenKey.After(b.SeenKey)
		}
		return a.ID.String() < b.ID.String()
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	if out == nil {
		out = []readmodel.WorkloadRecord{}
	}
	return out, nil
}

// GetWorkloadRecord implements readmodel.Store.
func (m *MemStore) GetWorkloadRecord(_ context.Context, id identity.WorkloadID) (*readmodel.WorkloadRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.Workloads {
		if m.Workloads[i].ID == id {
			r := m.Workloads[i]
			return &r, nil
		}
	}
	return nil, registry.ErrWorkloadUnknown
}

// ListWorkloadLabelIndex implements readmodel.Store.
func (m *MemStore) ListWorkloadLabelIndex(context.Context) (map[identity.WorkloadID]map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[identity.WorkloadID]map[string]string{}
	for i := range m.Workloads {
		out[m.Workloads[i].ID] = m.Workloads[i].LabelMap()
	}
	return out, nil
}

// ErrUnsupported is returned by the flow store double for writes.
var ErrUnsupported = errors.New("readmodeltest: not supported by the double")

// MemFlows implements flowstore.FlowStore over fixtures: it returns the
// grouped result and rows it holds and records the queries it received.
type MemFlows struct {
	mu        sync.Mutex
	Result    *flowstore.GroupResult
	Rows      []flowstore.WindowRow
	Err       error
	LastGroup *flowstore.GroupQuery
	LastPage  *flowstore.WindowPageQuery
}

var _ flowstore.FlowStore = (*MemFlows)(nil)

// WriteWindow implements flowstore.FlowStore.
func (f *MemFlows) WriteWindow(context.Context, flowstore.Window) (int, error) {
	return 0, ErrUnsupported
}

// ListWindows implements flowstore.FlowStore.
func (f *MemFlows) ListWindows(context.Context, flowstore.WindowQuery) ([]flowstore.WindowRow, error) {
	return nil, ErrUnsupported
}

// Rollup implements flowstore.FlowStore.
func (f *MemFlows) Rollup(context.Context, flowstore.RollupQuery) ([]flowstore.RollupRow, error) {
	return nil, ErrUnsupported
}

// ListTotals implements flowstore.FlowStore.
func (f *MemFlows) ListTotals(context.Context, identity.WorkloadID, innerwallv1.PolicyDecision) ([]flowstore.Total, error) {
	return nil, ErrUnsupported
}

// PruneWindows implements flowstore.FlowStore.
func (f *MemFlows) PruneWindows(context.Context, time.Time) (int64, bool, error) {
	return 0, false, ErrUnsupported
}

// RollupGroups implements flowstore.FlowStore.
func (f *MemFlows) RollupGroups(_ context.Context, q flowstore.GroupQuery) (*flowstore.GroupResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	qq := q
	f.LastGroup = &qq
	if f.Err != nil {
		return nil, f.Err
	}
	if f.Result == nil {
		return &flowstore.GroupResult{Groups: []flowstore.Group{}}, nil
	}
	res := *f.Result
	limit := q.Limit
	if limit <= 0 {
		limit = flowstore.DefaultGroupLimit
	}
	if len(res.Groups) > limit {
		res.Groups = res.Groups[:limit]
	}
	return &res, nil
}

// ListWindowPage implements flowstore.FlowStore: the rows after the
// cursor in (window start, id) descending order, at most the limit.
func (f *MemFlows) ListWindowPage(_ context.Context, q flowstore.WindowPageQuery) ([]flowstore.WindowRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	qq := q
	f.LastPage = &qq
	if f.Err != nil {
		return nil, f.Err
	}
	rows := append([]flowstore.WindowRow{}, f.Rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].WindowStart.Equal(rows[j].WindowStart) {
			return rows[i].WindowStart.After(rows[j].WindowStart)
		}
		return rows[i].ID > rows[j].ID
	})
	out := []flowstore.WindowRow{}
	for _, r := range rows {
		if r.WorkloadID != q.WorkloadID {
			continue
		}
		if q.Before != nil && (r.WindowStart.After(q.Before.WindowStart) || (r.WindowStart.Equal(q.Before.WindowStart) && r.ID >= q.Before.ID)) {
			continue
		}
		out = append(out, r)
		if q.Limit > 0 && len(out) == q.Limit {
			break
		}
	}
	return out, nil
}
