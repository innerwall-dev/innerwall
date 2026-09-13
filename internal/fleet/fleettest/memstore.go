// Package fleettest holds an in-memory double of the stores the authoring
// and fleet domains write through (policy.Store, fleet.Store, and the
// compiler's render transaction), for tests of those domains and of the
// transports above them. It keeps the same version, reference, and
// resolution semantics as the Postgres store, which is tested in
// internal/store; a transaction here is a snapshot restored on failure.
package fleettest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// MemStore is the double. Exported fields are fixtures a test sets and
// reads; the methods keep them consistent the way the store would.
type MemStore struct {
	mu            sync.Mutex
	Workloads     []registry.Workload
	Services      []policy.Service
	AddressGroups []policy.AddressGroup
	Rulesets      []policy.Ruleset
	Policies      map[identity.WorkloadID]*innerwallv1.WorkloadPolicy
	ModeChanges   []*fleet.ModeChangeRecord
	// Writes counts every write that reached the store, so a test can
	// assert that a read left nothing behind.
	Writes int
	// AfterResolve, when set, runs inside a mode change after its inputs
	// were loaded and before the modes are flipped, with the store lock
	// released, so a test can move labels underneath the change.
	AfterResolve func()
}

var (
	_ policy.Store   = (*MemStore)(nil)
	_ fleet.Store    = (*MemStore)(nil)
	_ compiler.Store = (*MemStore)(nil)
)

// New returns an empty store.
func New() *MemStore {
	return &MemStore{Policies: map[identity.WorkloadID]*innerwallv1.WorkloadPolicy{}}
}

// Renderer adapts an engine over the store to the authoring service's
// Renderer.
type Renderer struct{ Engine *compiler.Engine }

// Render implements policy.Renderer.
func (r Renderer) Render(ctx context.Context) error {
	_, err := r.Engine.Render(ctx)
	return err
}

// --- copies ------------------------------------------------------------------

func cloneSelector(s policy.Selector) policy.Selector {
	if s == nil {
		return nil
	}
	out := make(policy.Selector, len(s))
	for k, v := range s {
		out[k] = slices.Clone(v)
	}
	return out
}

func cloneEntries(in []policy.ServiceEntry) []policy.ServiceEntry {
	out := make([]policy.ServiceEntry, 0, len(in))
	for _, e := range in {
		out = append(out, policy.ServiceEntry{Protocol: e.Protocol, Ports: slices.Clone(e.Ports)})
	}
	return out
}

func cloneService(s policy.Service) policy.Service {
	s.Entries = cloneEntries(s.Entries)
	return s
}

func cloneGroup(g policy.AddressGroup) policy.AddressGroup {
	g.CIDRs = slices.Clone(g.CIDRs)
	return g
}

func cloneRule(r policy.Rule) policy.Rule {
	peers := make([]policy.Peer, 0, len(r.Peers))
	for _, p := range r.Peers {
		p.Workloads = cloneSelector(p.Workloads)
		peers = append(peers, p)
	}
	r.Peers = peers
	r.ServiceIDs = slices.Clone(r.ServiceIDs)
	r.Entries = cloneEntries(r.Entries)
	return r
}

func cloneRuleset(rs policy.Ruleset) policy.Ruleset {
	rs.Scope = cloneSelector(rs.Scope)
	rules := make([]policy.Rule, 0, len(rs.Rules))
	for _, r := range rs.Rules {
		rules = append(rules, cloneRule(r))
	}
	rs.Rules = rules
	return rs
}

func cloneWorkload(w registry.Workload) registry.Workload {
	w.Labels = slices.Clone(w.Labels)
	w.Addresses = slices.Clone(w.Addresses)
	return w
}

type snapshot struct {
	workloads []registry.Workload
	services  []policy.Service
	groups    []policy.AddressGroup
	rulesets  []policy.Ruleset
	policies  map[identity.WorkloadID]*innerwallv1.WorkloadPolicy
	changes   []*fleet.ModeChangeRecord
	writes    int
}

func (m *MemStore) snapshot() snapshot {
	s := snapshot{policies: map[identity.WorkloadID]*innerwallv1.WorkloadPolicy{}, writes: m.Writes, changes: slices.Clone(m.ModeChanges)}
	for _, w := range m.Workloads {
		s.workloads = append(s.workloads, cloneWorkload(w))
	}
	for _, x := range m.Services {
		s.services = append(s.services, cloneService(x))
	}
	for _, g := range m.AddressGroups {
		s.groups = append(s.groups, cloneGroup(g))
	}
	for _, rs := range m.Rulesets {
		s.rulesets = append(s.rulesets, cloneRuleset(rs))
	}
	for id, p := range m.Policies {
		s.policies[id] = rendered.Canonical(p)
	}
	return s
}

func (m *MemStore) restore(s snapshot) {
	m.Workloads, m.Services, m.AddressGroups, m.Rulesets, m.Policies, m.ModeChanges, m.Writes = s.workloads, s.services, s.groups, s.rulesets, s.policies, s.changes, s.writes
}

// --- policy.Store ------------------------------------------------------------

func versionOK(expect string, updatedAt time.Time) error {
	if expect == "" || expect == policy.VersionOf(updatedAt) {
		return nil
	}
	return &policy.VersionMismatchError{Current: policy.VersionOf(updatedAt)}
}

// CreateService implements policy.Store.
func (m *MemStore) CreateService(_ context.Context, s *policy.Service) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.Services {
		if x.Name == s.Name {
			return fmt.Errorf("%w: service %q", policy.ErrDuplicateName, s.Name)
		}
	}
	m.Writes++
	m.Services = append(m.Services, cloneService(*s))
	return nil
}

// UpdateService implements policy.Store.
func (m *MemStore) UpdateService(_ context.Context, s *policy.Service, expect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := slices.IndexFunc(m.Services, func(x policy.Service) bool { return x.ID == s.ID })
	if idx < 0 {
		return policy.ErrServiceUnknown
	}
	if err := versionOK(expect, m.Services[idx].UpdatedAt); err != nil {
		return err
	}
	for i, x := range m.Services {
		if i != idx && x.Name == s.Name {
			return fmt.Errorf("%w: service %q", policy.ErrDuplicateName, s.Name)
		}
	}
	m.Writes++
	s.CreatedAt = m.Services[idx].CreatedAt
	m.Services[idx] = cloneService(*s)
	return nil
}

// DeleteService implements policy.Store.
func (m *MemStore) DeleteService(_ context.Context, id uuid.UUID, expect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := slices.IndexFunc(m.Services, func(x policy.Service) bool { return x.ID == id })
	if idx < 0 {
		return policy.ErrServiceUnknown
	}
	if err := versionOK(expect, m.Services[idx].UpdatedAt); err != nil {
		return err
	}
	for _, rs := range m.Rulesets {
		for _, r := range rs.Rules {
			if slices.Contains(r.ServiceIDs, id) {
				return fmt.Errorf("%w: service %s", policy.ErrInUse, id)
			}
		}
	}
	m.Writes++
	m.Services = slices.Delete(m.Services, idx, idx+1)
	return nil
}

// GetService implements policy.Store.
func (m *MemStore) GetService(_ context.Context, id uuid.UUID) (*policy.Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.Services {
		if x.ID == id {
			c := cloneService(x)
			return &c, nil
		}
	}
	return nil, policy.ErrServiceUnknown
}

// ListServices implements policy.Store.
func (m *MemStore) ListServices(context.Context) ([]policy.Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listServices(), nil
}

func (m *MemStore) listServices() []policy.Service {
	out := make([]policy.Service, 0, len(m.Services))
	for _, x := range m.Services {
		out = append(out, cloneService(x))
	}
	slices.SortFunc(out, func(a, b policy.Service) int { return cmpString(a.Name, b.Name) })
	return out
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// CreateAddressGroup implements policy.Store.
func (m *MemStore) CreateAddressGroup(_ context.Context, g *policy.AddressGroup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.AddressGroups {
		if x.Name == g.Name {
			return fmt.Errorf("%w: address group %q", policy.ErrDuplicateName, g.Name)
		}
	}
	m.Writes++
	m.AddressGroups = append(m.AddressGroups, cloneGroup(*g))
	return nil
}

// UpdateAddressGroup implements policy.Store.
func (m *MemStore) UpdateAddressGroup(_ context.Context, g *policy.AddressGroup, expect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := slices.IndexFunc(m.AddressGroups, func(x policy.AddressGroup) bool { return x.ID == g.ID })
	if idx < 0 {
		return policy.ErrAddressGroupUnknown
	}
	if err := versionOK(expect, m.AddressGroups[idx].UpdatedAt); err != nil {
		return err
	}
	for i, x := range m.AddressGroups {
		if i != idx && x.Name == g.Name {
			return fmt.Errorf("%w: address group %q", policy.ErrDuplicateName, g.Name)
		}
	}
	m.Writes++
	g.CreatedAt = m.AddressGroups[idx].CreatedAt
	m.AddressGroups[idx] = cloneGroup(*g)
	return nil
}

// DeleteAddressGroup implements policy.Store.
func (m *MemStore) DeleteAddressGroup(_ context.Context, id uuid.UUID, expect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := slices.IndexFunc(m.AddressGroups, func(x policy.AddressGroup) bool { return x.ID == id })
	if idx < 0 {
		return policy.ErrAddressGroupUnknown
	}
	if err := versionOK(expect, m.AddressGroups[idx].UpdatedAt); err != nil {
		return err
	}
	for _, rs := range m.Rulesets {
		for _, r := range rs.Rules {
			for _, p := range r.Peers {
				if p.Kind == policy.PeerAddressGroup && p.AddressGroupID == id {
					return fmt.Errorf("%w: address group %s", policy.ErrInUse, id)
				}
			}
		}
	}
	m.Writes++
	m.AddressGroups = slices.Delete(m.AddressGroups, idx, idx+1)
	return nil
}

// GetAddressGroup implements policy.Store.
func (m *MemStore) GetAddressGroup(_ context.Context, id uuid.UUID) (*policy.AddressGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.AddressGroups {
		if x.ID == id {
			c := cloneGroup(x)
			return &c, nil
		}
	}
	return nil, policy.ErrAddressGroupUnknown
}

// ListAddressGroups implements policy.Store.
func (m *MemStore) ListAddressGroups(context.Context) ([]policy.AddressGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listGroups(), nil
}

func (m *MemStore) listGroups() []policy.AddressGroup {
	out := make([]policy.AddressGroup, 0, len(m.AddressGroups))
	for _, x := range m.AddressGroups {
		out = append(out, cloneGroup(x))
	}
	slices.SortFunc(out, func(a, b policy.AddressGroup) int { return cmpString(a.Name, b.Name) })
	return out
}

// CreateRuleset implements policy.Store.
func (m *MemStore) CreateRuleset(_ context.Context, rs *policy.Ruleset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.Rulesets {
		if x.Name == rs.Name {
			return fmt.Errorf("%w: ruleset %q", policy.ErrDuplicateName, rs.Name)
		}
	}
	if err := m.checkRuleIDs(rs); err != nil {
		return err
	}
	m.Writes++
	m.Rulesets = append(m.Rulesets, cloneRuleset(*rs))
	return nil
}

// checkRuleIDs refuses a rule id that another ruleset holds, as the
// primary key on rules does.
func (m *MemStore) checkRuleIDs(rs *policy.Ruleset) error {
	for _, other := range m.Rulesets {
		if other.ID == rs.ID {
			continue
		}
		for _, r := range other.Rules {
			for _, nr := range rs.Rules {
				if r.ID == nr.ID {
					return fmt.Errorf("%w: rule %s", policy.ErrDuplicateRuleID, r.ID)
				}
			}
		}
	}
	return nil
}

// UpdateRuleset implements policy.Store.
func (m *MemStore) UpdateRuleset(_ context.Context, rs *policy.Ruleset, expect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := slices.IndexFunc(m.Rulesets, func(x policy.Ruleset) bool { return x.ID == rs.ID })
	if idx < 0 {
		return policy.ErrRulesetUnknown
	}
	if err := versionOK(expect, m.Rulesets[idx].UpdatedAt); err != nil {
		return err
	}
	for i, x := range m.Rulesets {
		if i != idx && x.Name == rs.Name {
			return fmt.Errorf("%w: ruleset %q", policy.ErrDuplicateName, rs.Name)
		}
	}
	if err := m.checkRuleIDs(rs); err != nil {
		return err
	}
	m.Writes++
	m.Rulesets[idx] = cloneRuleset(*rs)
	return nil
}

// DeleteRuleset implements policy.Store.
func (m *MemStore) DeleteRuleset(_ context.Context, id uuid.UUID, expect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := slices.IndexFunc(m.Rulesets, func(x policy.Ruleset) bool { return x.ID == id })
	if idx < 0 {
		return policy.ErrRulesetUnknown
	}
	if err := versionOK(expect, m.Rulesets[idx].UpdatedAt); err != nil {
		return err
	}
	m.Writes++
	m.Rulesets = slices.Delete(m.Rulesets, idx, idx+1)
	return nil
}

// GetRuleset implements policy.Store.
func (m *MemStore) GetRuleset(_ context.Context, id uuid.UUID) (*policy.Ruleset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.Rulesets {
		if x.ID == id {
			c := cloneRuleset(x)
			return &c, nil
		}
	}
	return nil, policy.ErrRulesetUnknown
}

// ListRulesets implements policy.Store.
func (m *MemStore) ListRulesets(context.Context) ([]policy.Ruleset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listRulesets(), nil
}

func (m *MemStore) listRulesets() []policy.Ruleset {
	out := make([]policy.Ruleset, 0, len(m.Rulesets))
	for _, x := range m.Rulesets {
		out = append(out, cloneRuleset(x))
	}
	slices.SortFunc(out, func(a, b policy.Ruleset) int { return cmpString(a.Name, b.Name) })
	return out
}

// --- registry and fleet.Store ------------------------------------------------

// ListWorkloads implements fleet.Store.
func (m *MemStore) ListWorkloads(context.Context) ([]registry.Workload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listWorkloads(), nil
}

func (m *MemStore) listWorkloads() []registry.Workload {
	out := make([]registry.Workload, 0, len(m.Workloads))
	for _, w := range m.Workloads {
		out = append(out, cloneWorkload(w))
	}
	return out
}

// SetWorkloadLabels replaces labels unconditionally, as a test fixture
// mutation.
func (m *MemStore) SetWorkloadLabels(ctx context.Context, id identity.WorkloadID, labels []registry.Label) error {
	return m.ReplaceWorkloadLabels(ctx, id, labels, "")
}

// ReplaceWorkloadLabels implements fleet.Store.
func (m *MemStore) ReplaceWorkloadLabels(_ context.Context, id identity.WorkloadID, labels []registry.Label, expect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := slices.IndexFunc(m.Workloads, func(w registry.Workload) bool { return w.ID == id })
	if idx < 0 {
		return registry.ErrWorkloadUnknown
	}
	if v := registry.LabelsVersion(m.Workloads[idx].Labels); expect != "" && expect != v {
		return &registry.LabelsVersionError{Current: v}
	}
	m.Writes++
	m.Workloads[idx].Labels = slices.Clone(labels)
	return nil
}

// LoadRenderState implements fleet.Store.
func (m *MemStore) LoadRenderState(context.Context) (*compiler.Inputs, map[identity.WorkloadID]*innerwallv1.WorkloadPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inputs(), m.policies(), nil
}

func (m *MemStore) inputs() *compiler.Inputs {
	return &compiler.Inputs{Workloads: m.listWorkloads(), Rulesets: m.listRulesets(), Services: m.listServices(), AddressGroups: m.listGroups()}
}

func (m *MemStore) policies() map[identity.WorkloadID]*innerwallv1.WorkloadPolicy {
	out := make(map[identity.WorkloadID]*innerwallv1.WorkloadPolicy, len(m.Policies))
	for id, p := range m.Policies {
		out[id] = rendered.Canonical(p)
	}
	return out
}

// memTx is one transaction: the store itself, with a snapshot restored
// when the function fails.
type memTx struct {
	m *MemStore
}

// LoadInputs implements compiler.Tx.
func (t *memTx) LoadInputs(context.Context) (*compiler.Inputs, error) {
	t.m.mu.Lock()
	defer t.m.mu.Unlock()
	in := t.m.inputs()
	hook := t.m.AfterResolve
	if hook != nil {
		t.m.mu.Unlock()
		hook()
		t.m.mu.Lock()
	}
	return in, nil
}

// LoadPolicies implements compiler.Tx.
func (t *memTx) LoadPolicies(context.Context) (map[identity.WorkloadID]*innerwallv1.WorkloadPolicy, error) {
	t.m.mu.Lock()
	defer t.m.mu.Unlock()
	return t.m.policies(), nil
}

// SavePolicy implements compiler.Tx.
func (t *memTx) SavePolicy(_ context.Context, id identity.WorkloadID, p *innerwallv1.WorkloadPolicy, _ time.Time) error {
	t.m.mu.Lock()
	defer t.m.mu.Unlock()
	t.m.Writes++
	t.m.Policies[id] = rendered.Canonical(p)
	return nil
}

// RecordModeChange implements fleet.ModeChangeTx.
func (t *memTx) RecordModeChange(_ context.Context, rec *fleet.ModeChangeRecord) error {
	t.m.mu.Lock()
	defer t.m.mu.Unlock()
	t.m.Writes++
	cp := *rec
	cp.Workloads = slices.Clone(rec.Workloads)
	t.m.ModeChanges = append(t.m.ModeChanges, &cp)
	return nil
}

// SetWorkloadModes implements fleet.ModeChangeTx.
func (t *memTx) SetWorkloadModes(_ context.Context, ids []identity.WorkloadID, mode innerwallv1.EnforcementMode) (int, error) {
	t.m.mu.Lock()
	defer t.m.mu.Unlock()
	t.m.Writes++
	n := 0
	for i := range t.m.Workloads {
		if slices.Contains(ids, t.m.Workloads[i].ID) && t.m.Workloads[i].Mode != mode {
			t.m.Workloads[i].Mode = mode
			n++
		}
	}
	return n, nil
}

func (m *MemStore) inTx(fn func(tx *memTx) error) error {
	m.mu.Lock()
	snap := m.snapshot()
	m.mu.Unlock()
	if err := fn(&memTx{m: m}); err != nil {
		m.mu.Lock()
		m.restore(snap)
		m.mu.Unlock()
		return err
	}
	return nil
}

// RenderTx implements compiler.Store.
func (m *MemStore) RenderTx(ctx context.Context, fn func(ctx context.Context, tx compiler.Tx) error) error {
	return m.inTx(func(tx *memTx) error { return fn(ctx, tx) })
}

// ModeChangeTx implements fleet.Store.
func (m *MemStore) ModeChangeTx(ctx context.Context, fn func(ctx context.Context, tx fleet.ModeChangeTx) error) error {
	return m.inTx(func(tx *memTx) error { return fn(ctx, tx) })
}

// GetWorkloadPolicy returns a workload's rendered policy, or nil.
func (m *MemStore) GetWorkloadPolicy(_ context.Context, id identity.WorkloadID) (*innerwallv1.WorkloadPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.Policies[id]
	if !ok {
		return nil, nil
	}
	return rendered.Canonical(p), nil
}

// Workload returns a copy of one workload, or nil.
func (m *MemStore) Workload(id identity.WorkloadID) *registry.Workload {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.Workloads {
		if w.ID == id {
			c := cloneWorkload(w)
			return &c
		}
	}
	return nil
}
