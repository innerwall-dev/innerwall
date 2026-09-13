package fleet_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/fleet/fleettest"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

const (
	visibility = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY
	simulation = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION
	enforced   = innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED
)

type fixture struct {
	store          *fleettest.MemStore
	svc            *fleet.Service
	engine         *compiler.Engine
	web, db, cache identity.WorkloadID
	ruleset        policy.Ruleset
}

// newFixture is three workloads, one ruleset admitting web to db, and a
// first render so every workload has a version 1.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{store: fleettest.New()}
	f.web, _ = identity.NewWorkloadID()
	f.db, _ = identity.NewWorkloadID()
	f.cache, _ = identity.NewWorkloadID()
	mk := func(id identity.WorkloadID, host, role string, addr string) registry.Workload {
		return registry.Workload{ID: id, Hostname: host, Labels: []registry.Label{{Key: "env", Value: "prod"}, {Key: "role", Value: role}}, Addresses: []netip.Addr{netip.MustParseAddr(addr)}, Mode: visibility}
	}
	f.store.Workloads = []registry.Workload{mk(f.web, "web-1", "web", "10.0.0.10"), mk(f.db, "db-1", "db", "10.0.0.20"), mk(f.cache, "cache-1", "cache", "10.0.0.30")}
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	f.ruleset = policy.Ruleset{ID: uuid.New(), Name: "web-to-db", Enabled: true, Scope: policy.Selector{"role": {"db"}}, CreatedAt: now, UpdatedAt: now, Rules: []policy.Rule{{
		ID: uuid.New(), Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
		Peers:     []policy.Peer{{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"role": {"web"}}}},
		Entries:   []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 5432, End: 5432}}}},
		CreatedAt: now, UpdatedAt: now,
	}}}
	f.store.Rulesets = []policy.Ruleset{f.ruleset}
	f.engine = &compiler.Engine{Store: f.store, Now: func() time.Time { return now }}
	if _, err := f.engine.Render(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.svc = &fleet.Service{Store: f.store, Engine: f.engine, Now: func() time.Time { return now }}
	return f
}

func (f *fixture) version(t *testing.T, id identity.WorkloadID) uint64 {
	t.Helper()
	p, err := f.store.GetWorkloadPolicy(context.Background(), id)
	if err != nil || p == nil {
		t.Fatalf("policy of %s: %v %v", id, p, err)
	}
	return p.GetVersion()
}

func TestChangeModeRecordsTheResolvedSet(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.store.Workloads[1].Mode = simulation // db-1 starts in simulation
	before := map[identity.WorkloadID]uint64{f.web: f.version(t, f.web), f.db: f.version(t, f.db), f.cache: f.version(t, f.cache)}

	res, err := f.svc.ChangeMode(ctx, fleet.ModeChangeRequest{Selector: policy.Selector{"role": {"db", "cache"}}, TargetMode: enforced, ExpectedMatchCount: 2}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.ID == uuid.Nil || res.Matched != 2 || res.DesiredUpdated != 2 || res.Rendered == nil || len(res.Rendered.Changed) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if len(f.store.ModeChanges) != 1 {
		t.Fatalf("recorded %d mode changes", len(f.store.ModeChanges))
	}
	rec := f.store.ModeChanges[0]
	if rec.ID != res.ID || rec.TargetMode != enforced || rec.ExpectedMatchCount != 2 || rec.DesiredUpdated != 2 || !reflect.DeepEqual(rec.Selector, policy.Selector{"role": {"db", "cache"}}) {
		t.Fatalf("record = %+v", rec)
	}
	// The resolved set is recorded as "these N workloads", with the
	// mode each held before.
	got := map[identity.WorkloadID]innerwallv1.EnforcementMode{}
	for _, w := range rec.Workloads {
		got[w.ID] = w.PreviousMode
	}
	if !reflect.DeepEqual(got, map[identity.WorkloadID]innerwallv1.EnforcementMode{f.db: simulation, f.cache: visibility}) {
		t.Fatalf("recorded set = %v", got)
	}
	// Exactly that set moved, and only their versions advanced.
	if f.store.Workload(f.web).Mode != visibility || f.store.Workload(f.db).Mode != enforced || f.store.Workload(f.cache).Mode != enforced {
		t.Fatal("modes after change are wrong")
	}
	if f.version(t, f.web) != before[f.web] || f.version(t, f.db) != before[f.db]+1 || f.version(t, f.cache) != before[f.cache]+1 {
		t.Fatalf("versions before %v after web %d db %d cache %d", before, f.version(t, f.web), f.version(t, f.db), f.version(t, f.cache))
	}

	// A change that finds its workloads already in the target mode is
	// recorded with nothing updated and renders nothing.
	res, err = f.svc.ChangeMode(ctx, fleet.ModeChangeRequest{WorkloadIDs: []identity.WorkloadID{f.db}, TargetMode: enforced, ExpectedMatchCount: 1}, true)
	if err != nil || res.Matched != 1 || res.DesiredUpdated != 0 || len(res.Rendered.Changed) != 0 {
		t.Fatalf("no-op change = %+v %v", res, err)
	}
}

func TestChangeModeCountGuard(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	writes := f.store.Writes
	_, err := f.svc.ChangeMode(ctx, fleet.ModeChangeRequest{Selector: policy.Selector{"env": {"prod"}}, TargetMode: enforced, ExpectedMatchCount: 2}, true)
	var mc *fleet.MatchCountError
	if !errors.Is(err, fleet.ErrMatchCountMismatch) || !errors.As(err, &mc) || mc.Expected != 2 || mc.Matched != 3 {
		t.Fatalf("err = %v", err)
	}
	if f.store.Writes != writes || len(f.store.ModeChanges) != 0 || f.store.Workload(f.db).Mode != visibility {
		t.Fatal("a refused change left something behind")
	}
	// Zero matched and zero expected is an honest no-op, recorded.
	res, err := f.svc.ChangeMode(ctx, fleet.ModeChangeRequest{Selector: policy.Selector{"role": {"nothing"}}, TargetMode: enforced, ExpectedMatchCount: 0}, true)
	if err != nil || res.Matched != 0 || res.DesiredUpdated != 0 || len(f.store.ModeChanges) != 1 {
		t.Fatalf("empty change = %+v %v", res, err)
	}
}

// TestChangeModeIsAtomicUnderLabelChanges moves a workload out of the
// selector after the change resolved it and before it flipped: the set
// the change recorded is the set it flipped, because the flip names ids
// and never re-evaluates the selector.
func TestChangeModeIsAtomicUnderLabelChanges(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	moved := false
	f.store.AfterResolve = func() {
		if moved {
			return
		}
		moved = true
		if err := f.store.SetWorkloadLabels(ctx, f.cache, []registry.Label{{Key: "role", Value: "web"}}); err != nil {
			t.Error(err)
		}
	}
	res, err := f.svc.ChangeMode(ctx, fleet.ModeChangeRequest{Selector: policy.Selector{"role": {"db", "cache"}}, TargetMode: enforced, ExpectedMatchCount: 2}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !moved || res.Matched != 2 || res.DesiredUpdated != 2 {
		t.Fatalf("result = %+v moved %v", res, moved)
	}
	rec := f.store.ModeChanges[0]
	flipped := map[identity.WorkloadID]bool{}
	for _, w := range f.store.Workloads {
		flipped[w.ID] = w.Mode == enforced
	}
	recorded := map[identity.WorkloadID]bool{}
	for _, w := range rec.Workloads {
		recorded[w.ID] = true
	}
	if !recorded[f.db] || !recorded[f.cache] || recorded[f.web] {
		t.Fatalf("recorded set = %v", recorded)
	}
	for id, wasRecorded := range map[identity.WorkloadID]bool{f.web: false, f.db: true, f.cache: true} {
		if flipped[id] != wasRecorded {
			t.Fatalf("workload %s flipped=%v recorded=%v", id, flipped[id], wasRecorded)
		}
	}
	// And the labels the other writer set are what stands.
	if l := f.store.Workload(f.cache).Labels; len(l) != 1 || l[0].Value != "web" {
		t.Fatalf("cache labels = %v", l)
	}
}

func TestChangeModeRequestShape(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cases := []struct {
		name        string
		req         fleet.ModeChangeRequest
		hasExpected bool
		path, rule  string
	}{
		{"neither selection", fleet.ModeChangeRequest{TargetMode: enforced, ExpectedMatchCount: 1}, true, "selector", "invalid"},
		{"both selections", fleet.ModeChangeRequest{Selector: policy.Selector{"role": {"db"}}, WorkloadIDs: []identity.WorkloadID{f.db}, TargetMode: enforced, ExpectedMatchCount: 1}, true, "selector", "invalid"},
		{"empty values", fleet.ModeChangeRequest{Selector: policy.Selector{"role": {}}, TargetMode: enforced, ExpectedMatchCount: 1}, true, "selector[role]", "label-values-required"},
		{"no mode", fleet.ModeChangeRequest{Selector: policy.Selector{"role": {"db"}}, ExpectedMatchCount: 1}, true, "target_mode", "invalid"},
		{"no expected count", fleet.ModeChangeRequest{Selector: policy.Selector{"role": {"db"}}, TargetMode: enforced}, false, "expected_match_count", "invalid"},
		{"negative expected count", fleet.ModeChangeRequest{Selector: policy.Selector{"role": {"db"}}, TargetMode: enforced, ExpectedMatchCount: -1}, true, "expected_match_count", "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.ChangeMode(ctx, tc.req, tc.hasExpected)
			fs := policy.AsFindings(err)
			if fs == nil {
				t.Fatalf("err = %v, want findings", err)
			}
			found := false
			for _, e := range fs.Errors {
				if e.Path == tc.path && e.Rule() == tc.rule {
					found = true
				}
			}
			if !found {
				t.Fatalf("findings %v lack %s/%s", err, tc.path, tc.rule)
			}
		})
	}
	// An unknown id in the list is a finding at its index, and nothing
	// is recorded.
	unknown, _ := identity.NewWorkloadID()
	_, err := f.svc.ChangeMode(ctx, fleet.ModeChangeRequest{WorkloadIDs: []identity.WorkloadID{f.db, unknown}, TargetMode: enforced, ExpectedMatchCount: 2}, true)
	fs := policy.AsFindings(err)
	if fs == nil || len(fs.Errors) != 1 || fs.Errors[0].Path != "workload_ids[1]" || !errors.Is(err, fleet.ErrUnknownWorkload) {
		t.Fatalf("unknown id err = %v", err)
	}
	if len(f.store.ModeChanges) != 0 {
		t.Fatal("a refused change was recorded")
	}
}

func TestPreviewSelector(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p, err := f.svc.PreviewSelector(ctx, policy.Selector{"env": {"prod"}, "role": {"db", "web"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Matched) != 2 {
		t.Fatalf("matched = %+v", p.Matched)
	}
	hosts := map[string]bool{}
	for _, m := range p.Matched {
		hosts[m.Hostname] = true
		if m.Labels["env"] != "prod" {
			t.Fatalf("labels = %v", m.Labels)
		}
	}
	if !hosts["web-1"] || !hosts["db-1"] {
		t.Fatalf("hosts = %v", hosts)
	}
	if p, err := f.svc.PreviewSelector(ctx, policy.Selector{"role": {"nothing"}}); err != nil || p.Matched == nil || len(p.Matched) != 0 {
		t.Fatalf("empty preview = %+v %v", p, err)
	}
	_, err = f.svc.PreviewSelector(ctx, policy.Selector{})
	if fs := policy.AsFindings(err); fs == nil || fs.Errors[0].Path != "selector" || fs.Errors[0].Rule() != "selector-empty" {
		t.Fatalf("empty selector err = %v", err)
	}
	// Preview and the change resolve identically.
	res, err := f.svc.ChangeMode(ctx, fleet.ModeChangeRequest{Selector: policy.Selector{"env": {"prod"}, "role": {"db", "web"}}, TargetMode: simulation, ExpectedMatchCount: len(p.Matched)}, true)
	if err != nil || res.Matched != 2 {
		t.Fatalf("change after preview = %+v %v", res, err)
	}
}

func TestDryRunIsPure(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	writes := f.store.Writes
	versions := map[identity.WorkloadID]uint64{f.web: f.version(t, f.web), f.db: f.version(t, f.db), f.cache: f.version(t, f.cache)}
	stateVersion, err := f.svc.StateVersion(ctx)
	if err != nil || stateVersion == "" {
		t.Fatalf("state version %q %v", stateVersion, err)
	}

	// The hypothetical set: the existing ruleset widened to cache, and a
	// new ruleset on web.
	hypothetical := []policy.Ruleset{f.ruleset, {Name: "office-to-web", Enabled: true, Scope: policy.Selector{"role": {"web"}}, Rules: []policy.Rule{{
		Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true,
		Peers:   []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "192.0.2.0/24"}},
		Entries: []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 443, End: 443}}}},
	}}}}
	hypothetical[0].Scope = policy.Selector{"role": {"db", "cache"}}
	first, err := f.svc.DryRun(ctx, fleet.DryRunRequest{Rulesets: hypothetical, StateVersion: stateVersion})
	if err != nil {
		t.Fatal(err)
	}
	if first.Stale || first.StateVersion != stateVersion {
		t.Fatalf("result state = %q stale %v", first.StateVersion, first.Stale)
	}
	if len(first.Workloads) != 2 {
		t.Fatalf("diffs = %+v, want web and cache", first.Workloads)
	}
	byID := map[identity.WorkloadID]compiler.WorkloadDiff{}
	for _, d := range first.Workloads {
		byID[d.ID] = d
	}
	if d := byID[f.cache]; d.Version != versions[f.cache] || len(d.Added) != 1 || d.Added[0].GetPeerCidrs()[0] != "10.0.0.10/32" {
		t.Fatalf("cache diff = %+v", d)
	}
	if d := byID[f.web]; len(d.Added) != 1 || d.Added[0].GetPeerCidrs()[0] != "192.0.2.0/24" {
		t.Fatalf("web diff = %+v", d)
	}
	// Nothing was written and no version moved.
	if f.store.Writes != writes {
		t.Fatalf("dry run wrote %d times", f.store.Writes-writes)
	}
	for id, v := range versions {
		if f.version(t, id) != v {
			t.Fatalf("version of %s moved", id)
		}
	}
	if len(f.store.Rulesets) != 1 {
		t.Fatal("the hypothetical ruleset was persisted")
	}
	// Stable across repeated calls.
	second, err := f.svc.DryRun(ctx, fleet.DryRunRequest{Rulesets: hypothetical, StateVersion: stateVersion})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatalf("dry runs differ:\n%s\n%s", a, b)
	}
	// A stale author is told so, and the state version follows a label
	// change.
	stale, err := f.svc.DryRun(ctx, fleet.DryRunRequest{Rulesets: hypothetical, StateVersion: "older"})
	if err != nil || !stale.Stale {
		t.Fatalf("stale = %+v %v", stale, err)
	}
	if err := f.svc.SetLabels(ctx, f.cache, []registry.Label{{Key: "role", Value: "db"}}, ""); err != nil {
		t.Fatal(err)
	}
	after, err := f.svc.StateVersion(ctx)
	if err != nil || after == stateVersion {
		t.Fatalf("state version after a label change = %q (was %q) %v", after, stateVersion, err)
	}
	// Admission runs on the hypothetical set exactly as a write would.
	bad := hypothetical
	bad[1].Rules[0].Peers = nil
	_, err = f.svc.DryRun(ctx, fleet.DryRunRequest{Rulesets: bad})
	if fs := policy.AsFindings(err); fs == nil || fs.Errors[0].Path != "rulesets[1].rules[0].peers" {
		t.Fatalf("dry run findings = %v", err)
	}
	dup := []policy.Ruleset{f.ruleset, f.ruleset}
	_, err = f.svc.DryRun(ctx, fleet.DryRunRequest{Rulesets: dup})
	if fs := policy.AsFindings(err); fs == nil || fs.Errors[0].Path != "rulesets[1].name" {
		t.Fatalf("duplicate name findings = %v", err)
	}
}

func TestSetLabels(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dbVersion := f.version(t, f.db)
	current := registry.LabelsVersion(f.store.Workload(f.web).Labels)
	err := f.svc.SetLabels(ctx, f.web, []registry.Label{{Key: "role", Value: "db"}}, "stale")
	var lv *registry.LabelsVersionError
	if !errors.Is(err, registry.ErrLabelsChanged) || !errors.As(err, &lv) || lv.Current != current {
		t.Fatalf("stale labels err = %v", err)
	}
	// Moving web-1 into the db scope renders: db-1's peers lose the web
	// address and web-1 gains the ruleset.
	if err := f.svc.SetLabels(ctx, f.web, []registry.Label{{Key: "role", Value: "db"}}, current); err != nil {
		t.Fatal(err)
	}
	if f.version(t, f.db) != dbVersion+1 {
		t.Fatalf("db version %d, want %d", f.version(t, f.db), dbVersion+1)
	}
	if got := f.store.Workload(f.web).Labels; len(got) != 1 || got[0].Value != "db" {
		t.Fatalf("labels = %v", got)
	}
	err = f.svc.SetLabels(ctx, f.web, []registry.Label{{Key: "", Value: "x"}, {Key: "a", Value: "1"}, {Key: "a", Value: "2"}}, "")
	fs := policy.AsFindings(err)
	if fs == nil || len(fs.Errors) != 2 || fs.Errors[0].Path != "labels[0]" || fs.Errors[1].Path != "labels[2]" || !errors.Is(err, fleet.ErrDuplicateLabelKey) {
		t.Fatalf("label findings = %v", err)
	}
	unknown, _ := identity.NewWorkloadID()
	if err := f.svc.SetLabels(ctx, unknown, nil, ""); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("unknown workload err = %v", err)
	}
	if registry.LabelsVersion([]registry.Label{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}}) != registry.LabelsVersion([]registry.Label{{Key: "b", Value: "2"}, {Key: "a", Value: "1"}}) {
		t.Fatal("label version depends on order")
	}
}
