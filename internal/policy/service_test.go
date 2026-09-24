package policy_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/fleet/fleettest"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// authoringFixture is an authoring service over the in-memory store with
// a settable clock, one workload for the render to touch, and one
// service to reference.
type authoringFixture struct {
	store *fleettest.MemStore
	auth  *policy.Authoring
	now   time.Time
	svc   policy.Service
	db    identity.WorkloadID
}

func newAuthoringFixture(t *testing.T) *authoringFixture {
	t.Helper()
	f := &authoringFixture{store: fleettest.New(), now: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)}
	f.db, _ = identity.NewWorkloadID()
	f.store.Workloads = []registry.Workload{{ID: f.db, Hostname: "db-1", Labels: []registry.Label{{Key: "role", Value: "db"}}, Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.20")}, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY}}
	engine := &compiler.Engine{Store: f.store, Now: func() time.Time { return f.now }}
	f.auth = &policy.Authoring{Store: f.store, Renderer: fleettest.Renderer{Engine: engine}, Now: func() time.Time { return f.now }}
	f.svc = policy.Service{Name: "postgres", Entries: []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 5432, End: 5432}}}}}
	if err := f.auth.CreateService(context.Background(), &f.svc); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *authoringFixture) rule(desc string) policy.Rule {
	return policy.Rule{Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true, Description: desc, Peers: []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "192.0.2.0/24"}}, ServiceIDs: []uuid.UUID{f.svc.ID}}
}

func (f *authoringFixture) ruleset(t *testing.T, name string, rules ...policy.Rule) *policy.Ruleset {
	t.Helper()
	rs := &policy.Ruleset{Name: name, Enabled: true, Scope: policy.Selector{"role": {"db"}}, Rules: rules}
	if err := f.auth.CreateRuleset(context.Background(), rs); err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestRuleTimestampsCarryAcrossEdits(t *testing.T) {
	ctx := context.Background()
	f := newAuthoringFixture(t)
	t0 := f.now
	rs := f.ruleset(t, "to-db", f.rule("a"), f.rule("b"))
	for i, r := range rs.Rules {
		if !r.CreatedAt.Equal(t0) || !r.UpdatedAt.Equal(t0) || r.ID == uuid.Nil {
			t.Fatalf("rule %d after create = created %v updated %v id %v", i, r.CreatedAt, r.UpdatedAt, r.ID)
		}
	}
	keptID, changedID := rs.Rules[0].ID, rs.Rules[1].ID

	f.now = t0.Add(time.Hour)
	rs.Rules[1].Description = "b, reworded"
	rs.Rules = append(rs.Rules, f.rule("c"))
	if err := f.auth.UpdateRuleset(ctx, rs, "1"); err != nil {
		t.Fatal(err)
	}
	back, err := f.store.GetRuleset(ctx, rs.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !back.CreatedAt.Equal(t0) || !back.UpdatedAt.Equal(f.now) || back.Version != 2 || rs.Version != 2 {
		t.Fatalf("ruleset after edit = created %v updated %v version %d (in hand %d)", back.CreatedAt, back.UpdatedAt, back.Version, rs.Version)
	}
	byID := map[uuid.UUID]policy.Rule{}
	for _, r := range back.Rules {
		byID[r.ID] = r
	}
	if kept := byID[keptID]; !kept.CreatedAt.Equal(t0) || !kept.UpdatedAt.Equal(t0) {
		t.Fatalf("unchanged rule instants moved: created %v updated %v", kept.CreatedAt, kept.UpdatedAt)
	}
	if changed := byID[changedID]; !changed.CreatedAt.Equal(t0) || !changed.UpdatedAt.Equal(f.now) {
		t.Fatalf("changed rule instants = created %v updated %v", changed.CreatedAt, changed.UpdatedAt)
	}
	if added := back.Rules[2]; !added.CreatedAt.Equal(f.now) || !added.UpdatedAt.Equal(f.now) {
		t.Fatalf("new rule instants = created %v updated %v", added.CreatedAt, added.UpdatedAt)
	}
	// A rule's version is its own: the kept rule's did not move, the
	// changed rule's advanced by one, and the new rule starts at 1.
	if byID[keptID].Version != 1 || byID[changedID].Version != 2 || back.Rules[2].Version != 1 {
		t.Fatalf("rule versions = kept %d changed %d new %d", byID[keptID].Version, byID[changedID].Version, back.Rules[2].Version)
	}
}

func TestConditionalWrites(t *testing.T) {
	ctx := context.Background()
	f := newAuthoringFixture(t)
	rs := f.ruleset(t, "to-db", f.rule("a"))
	current := policy.FormatVersion(rs.Version)
	if current != "1" {
		t.Fatalf("a created ruleset's version = %q", current)
	}

	// A stale version is refused and the current one is named. The token
	// is compared byte-exact, never parsed: a string that would parse to
	// the current integer is still not the current token.
	f.now = f.now.Add(time.Minute)
	rs.Description = "changed"
	for _, stale := range []string{"0", "01", "+1", " 1", "1.0", "x"} {
		err := f.auth.UpdateRuleset(ctx, rs, stale)
		var vm *policy.VersionMismatchError
		if !errors.Is(err, policy.ErrVersionMismatch) || !errors.As(err, &vm) || vm.Current != current {
			t.Fatalf("update with %q err = %v", stale, err)
		}
	}
	if back, _ := f.store.GetRuleset(ctx, rs.ID); back.Description != "" {
		t.Fatal("a refused write changed the ruleset")
	}
	// The current version is accepted, and the write moves it.
	if err := f.auth.UpdateRuleset(ctx, rs, current); err != nil {
		t.Fatal(err)
	}
	back, _ := f.store.GetRuleset(ctx, rs.ID)
	if back.Description != "changed" || policy.FormatVersion(back.Version) != "2" {
		t.Fatalf("after update = %+v", back)
	}
	// The old version no longer deletes; the new one does. An
	// unconditional write (the command line's default) always applies.
	if err := f.auth.DeleteRuleset(ctx, rs.ID, current); !errors.Is(err, policy.ErrVersionMismatch) {
		t.Fatalf("stale delete err = %v", err)
	}
	if err := f.auth.DeleteRuleset(ctx, rs.ID, policy.FormatVersion(back.Version)); err != nil {
		t.Fatal(err)
	}
	if err := f.auth.DeleteRuleset(ctx, rs.ID, ""); !errors.Is(err, policy.ErrRulesetUnknown) {
		t.Fatalf("delete of a deleted ruleset err = %v", err)
	}

	// Services and address groups carry the same semantics.
	svc := f.svc
	svc.Name = "pg"
	if err := f.auth.UpdateService(ctx, &svc, "stale"); !errors.Is(err, policy.ErrVersionMismatch) {
		t.Fatalf("stale service update err = %v", err)
	}
	if err := f.auth.UpdateService(ctx, &svc, policy.FormatVersion(f.svc.Version)); err != nil || svc.Version != 2 {
		t.Fatal(err)
	}
	g := policy.AddressGroup{Name: "office", CIDRs: []string{"192.0.2.0/24"}}
	if err := f.auth.CreateAddressGroup(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if err := f.auth.DeleteAddressGroup(ctx, g.ID, "stale"); !errors.Is(err, policy.ErrVersionMismatch) {
		t.Fatalf("stale group delete err = %v", err)
	}
	if err := f.auth.DeleteAddressGroup(ctx, g.ID, policy.FormatVersion(g.Version)); err != nil {
		t.Fatal(err)
	}
}

func TestRuleLevelOperations(t *testing.T) {
	ctx := context.Background()
	f := newAuthoringFixture(t)
	rs := f.ruleset(t, "to-db", f.rule("a"))
	rulesetVersion := policy.FormatVersion(rs.Version)

	f.now = f.now.Add(time.Minute)
	added := f.rule("b")
	if err := f.auth.CreateRule(ctx, rs.ID, &added); err != nil {
		t.Fatal(err)
	}
	if added.ID == uuid.Nil || !added.CreatedAt.Equal(f.now) {
		t.Fatalf("created rule = %+v", added)
	}
	back, _ := f.store.GetRuleset(ctx, rs.ID)
	if len(back.Rules) != 2 || back.Rules[1].ID != added.ID || policy.FormatVersion(back.Version) == rulesetVersion {
		t.Fatalf("ruleset after rule create = %+v", back)
	}

	// A rule update is conditioned on the rule's own version.
	f.now = f.now.Add(time.Minute)
	added.Description = "b, reworded"
	err := f.auth.UpdateRule(ctx, rs.ID, &added, "stale")
	var vm *policy.VersionMismatchError
	if !errors.As(err, &vm) || vm.Current != policy.FormatVersion(back.Rules[1].Version) {
		t.Fatalf("stale rule update err = %v", err)
	}
	if err := f.auth.UpdateRule(ctx, rs.ID, &added, policy.FormatVersion(back.Rules[1].Version)); err != nil {
		t.Fatal(err)
	}
	if !added.UpdatedAt.Equal(f.now) || added.Description != "b, reworded" || added.Version != 2 {
		t.Fatalf("updated rule = %+v", added)
	}
	back, _ = f.store.GetRuleset(ctx, rs.ID)
	if back.Rules[0].UpdatedAt.Equal(f.now) || back.Rules[0].Version != 1 {
		t.Fatal("editing one rule moved another's version")
	}

	// Findings of a rule write are relative to the rule.
	bad := f.rule("bad")
	bad.Peers = nil
	err = f.auth.CreateRule(ctx, rs.ID, &bad)
	findings := policy.AsFindings(err)
	if findings == nil || len(findings.Errors) != 1 || findings.Errors[0].Path != "peers" || findings.Errors[0].Rule() != "peers-required" {
		t.Fatalf("rule findings = %v", err)
	}
	if back2, _ := f.store.GetRuleset(ctx, rs.ID); len(back2.Rules) != 2 {
		t.Fatal("a refused rule was persisted")
	}

	unknown := f.rule("x")
	unknown.ID = uuid.New()
	if err := f.auth.UpdateRule(ctx, rs.ID, &unknown, ""); !errors.Is(err, policy.ErrRuleUnknown) {
		t.Fatalf("unknown rule update err = %v", err)
	}
	if err := f.auth.DeleteRule(ctx, rs.ID, added.ID, "stale"); !errors.Is(err, policy.ErrVersionMismatch) {
		t.Fatalf("stale rule delete err = %v", err)
	}
	if err := f.auth.DeleteRule(ctx, rs.ID, added.ID, policy.FormatVersion(added.Version)); err != nil {
		t.Fatal(err)
	}
	if back, _ := f.store.GetRuleset(ctx, rs.ID); len(back.Rules) != 1 {
		t.Fatalf("after delete = %+v", back.Rules)
	}
	if err := f.auth.DeleteRule(ctx, uuid.New(), added.ID, ""); !errors.Is(err, policy.ErrRulesetUnknown) {
		t.Fatalf("delete in unknown ruleset err = %v", err)
	}
}

func TestFindingsAreCollected(t *testing.T) {
	rs := goodRuleset()
	rs.Name = ""
	rs.Rules[0].Direction = innerwallv1.Direction_DIRECTION_OUTBOUND
	rs.Rules[0].Peers = append(rs.Rules[0].Peers, policy.Peer{Kind: policy.PeerCIDR, CIDR: "nope"})
	rs.Rules[0].ID = uuid.New()
	rs.Rules = append(rs.Rules, policy.Rule{ID: rs.Rules[0].ID, Direction: innerwallv1.Direction_DIRECTION_INBOUND})
	err := policy.ValidateRuleset(rs, refs)
	f := policy.AsFindings(err)
	if f == nil {
		t.Fatalf("err = %v, want findings", err)
	}
	want := map[string]string{
		"name":                   "name-required",
		"rules[0].direction":     "direction-outbound",
		"rules[0].peers[1].cidr": "cidr",
		"rules[1].id":            "rule-id-duplicate",
		"rules[1].peers":         "peers-required",
		"rules[1].services":      "services-required",
	}
	got := map[string]string{}
	for _, e := range f.Errors {
		got[e.Path] = e.Rule()
		if e.Message() == "" || e.Message()[:7] == "policy:" {
			t.Fatalf("message %q", e.Message())
		}
	}
	if len(got) != len(want) {
		t.Fatalf("findings = %v, want %v", got, want)
	}
	for p, r := range want {
		if got[p] != r {
			t.Fatalf("finding at %s = %q, want %q (all: %v)", p, got[p], r, got)
		}
	}
	if !errors.Is(err, policy.ErrBadCIDR) || !errors.Is(err, policy.ErrEmptyName) {
		t.Fatal("errors.Is does not see through the findings")
	}
	if !errors.Is(policy.ValidateSelector(policy.Selector{}), policy.ErrEmptySelector) {
		t.Fatal("empty selector admitted")
	}
	if rule := policy.ValidateRule(&policy.Rule{Direction: innerwallv1.Direction_DIRECTION_INBOUND, Peers: []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "10.0.0.0/8"}}}, refs); policy.AsFindings(rule).Errors[0].Path != "services" {
		t.Fatalf("rule-relative path = %v", rule)
	}
}

func TestDocumentCarriesVersions(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	rs := &policy.Ruleset{ID: uuid.New(), Name: "n", Enabled: true, Scope: policy.Selector{"role": {"db"}}, CreatedAt: now, UpdatedAt: now.Add(time.Hour), Version: 3, Rules: []policy.Rule{{ID: uuid.New(), Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true, Peers: []policy.Peer{{Kind: policy.PeerCIDR, CIDR: "10.0.0.0/8"}}, ServiceIDs: []uuid.UUID{svcID}, CreatedAt: now, UpdatedAt: now.Add(2 * time.Hour), Version: 2}}}
	names := policy.NewNames([]policy.Service{{ID: svcID, Name: "postgres"}}, nil)
	doc := policy.RulesetToDoc(rs, names)
	if doc.Version != "3" || doc.CreatedAt != "2026-09-13T12:00:00Z" || doc.UpdatedAt != "2026-09-13T13:00:00Z" {
		t.Fatalf("ruleset doc instants = %s %s %s", doc.Version, doc.CreatedAt, doc.UpdatedAt)
	}
	if doc.Rules[0].Version != "2" || doc.Rules[0].UpdatedAt != "2026-09-13T14:00:00Z" || doc.Rules[0].Services[0] != "postgres" {
		t.Fatalf("rule doc = %+v", doc.Rules[0])
	}
	// The document round-trips through the decoder with the versions
	// present and ignored.
	data, _ := json.Marshal(doc)
	back, err := policy.DecodeRuleset(data, names)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != rs.ID || back.Rules[0].ID != rs.Rules[0].ID || !back.UpdatedAt.IsZero() || !back.Rules[0].UpdatedAt.IsZero() {
		t.Fatalf("decoded = %+v", back)
	}
	// Document faults are findings at their paths.
	_, err = policy.DecodeRuleset([]byte(`{"name":"x","scope":{"role":["db"]},"rules":[{"id":"nope","direction":"sideways","peers":[{"address_group":"missing"}],"services":["missing"],"entries":[{"protocol":"tcp","ports":["many"]}]}]}`), names)
	f := policy.AsFindings(err)
	if f == nil {
		t.Fatalf("err = %v", err)
	}
	paths := map[string]string{}
	for _, e := range f.Errors {
		paths[e.Path] = e.Rule()
	}
	for p, r := range map[string]string{"rules[0].id": "id", "rules[0].direction": "direction", "rules[0].peers[0].address_group": "address-group-unknown", "rules[0].services[0]": "service-unknown", "rules[0].entries[0].ports[0]": "port-spec"} {
		if paths[p] != r {
			t.Fatalf("finding %s = %q, want %q (all %v)", p, paths[p], r, paths)
		}
	}
	if back.Version != 0 || back.Rules[0].Version != 0 {
		t.Fatal("a version in a document was taken as input")
	}
}

func TestMatchWorkloads(t *testing.T) {
	a, _ := identity.NewWorkloadID()
	b, _ := identity.NewWorkloadID()
	c, _ := identity.NewWorkloadID()
	index := map[identity.WorkloadID]map[string]string{
		a: {"role": "db", "env": "prod"},
		b: {"role": "db", "env": "staging"},
		c: {"role": "web", "env": "prod"},
	}
	got := policy.MatchWorkloads(policy.Selector{"role": {"db"}}, index)
	if len(got) != 2 || got[0].String() > got[1].String() {
		t.Fatalf("matched = %v", got)
	}
	if got := policy.MatchWorkloads(policy.Selector{"role": {"db"}, "env": {"prod"}}, index); len(got) != 1 || got[0] != a {
		t.Fatalf("matched = %v", got)
	}
	if got := policy.MatchWorkloads(nil, index); got == nil || len(got) != 0 {
		t.Fatalf("empty selector matched %v", got)
	}
}
