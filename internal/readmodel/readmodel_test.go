package readmodel_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/readmodel/readmodeltest"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

var now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

type fixture struct {
	store  *readmodeltest.MemStore
	flows  *readmodeltest.MemFlows
	reader *readmodel.Reader
	web    identity.WorkloadID
	db     identity.WorkloadID
	group  policy.AddressGroup
	rule   policy.Rule
	ruleID string
}

func newFixture() *fixture {
	web, _ := identity.NewWorkloadID()
	db, _ := identity.NewWorkloadID()
	seen := now.Add(-time.Minute)
	rule := policy.Rule{ID: uuid.New(), Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true, Description: "postgres from web"}
	rs := policy.Ruleset{ID: uuid.New(), Name: "web-to-db", Enabled: true, Scope: policy.Selector{"role": {"db"}}, Rules: []policy.Rule{rule}, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-time.Hour)}
	ruleID := rendered.RuleID(rule.ID.String(), innerwallv1.Protocol_PROTOCOL_TCP)
	group := policy.AddressGroup{ID: uuid.New(), Name: "office", CIDRs: []string{"192.0.2.0/24"}}
	renderedAt := now.Add(-30 * time.Minute)
	st := &readmodeltest.MemStore{
		Workloads: []readmodel.WorkloadRecord{
			{Workload: registry.Workload{ID: web, Hostname: "web-1", Labels: []registry.Label{{Key: "role", Value: "web"}}, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.10")}, EnrolledAt: now.Add(-24 * time.Hour), LastSeenAt: &seen, SyncState: innerwallv1.SyncState_SYNC_STATE_SYNCED, AppliedVersion: 3, CredentialExpiresAt: now.Add(20 * time.Hour)}, LatestVersion: 3, LatestRenderedAt: &renderedAt},
			{Workload: registry.Workload{ID: db, Hostname: "db-1", Labels: []registry.Label{{Key: "role", Value: "db"}}, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, EnrolledAt: now.Add(-20 * time.Hour), SyncState: innerwallv1.SyncState_SYNC_STATE_DEGRADED, SyncError: "apply refused", CredentialExpiresAt: now.Add(2 * time.Hour), CredentialRenewalError: "authority unreachable", DroppedFlowRecords: 42}, LatestVersion: 5, LatestRenderedAt: &renderedAt},
		},
		AddressGroups: []policy.AddressGroup{group},
		Rulesets:      []policy.Ruleset{rs},
		Policies: map[identity.WorkloadID]*innerwallv1.WorkloadPolicy{
			db: {Version: 5, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, InboundRules: []*innerwallv1.ResolvedRule{{RuleId: ruleID, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: []string{"10.0.0.10/32"}, Ports: []*innerwallv1.PortRange{{Start: 5432, End: 5432}}}}},
		},
	}
	fl := &readmodeltest.MemFlows{}
	return &fixture{store: st, flows: fl, reader: &readmodel.Reader{Store: st, Flows: fl, Now: func() time.Time { return now }}, web: web, db: db, group: group, rule: rule, ruleID: ruleID}
}

func TestRollupScopeAndKeys(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	f.flows.Result = &flowstore.GroupResult{
		Groups: []flowstore.Group{
			{RuleID: f.ruleID, Peer: flowstore.Peer{Kind: flowstore.PeerWorkload, Key: f.web.String(), Labels: map[string]string{"role": "web", "env": "prod"}}, FlowCount: 2, ConnectionCount: 240, FirstSeen: now.Add(-2 * time.Hour), LastSeen: now.Add(-time.Hour)},
			{RuleID: "", Peer: flowstore.Peer{Kind: flowstore.PeerAddressGroup, Key: f.group.ID.String()}, FlowCount: 2, ConnectionCount: 6},
			{RuleID: "", Peer: flowstore.Peer{Kind: flowstore.PeerUnknown, Key: "198.51.100.7"}, FlowCount: 2, ConnectionCount: 18},
		},
		EffectiveFrom: now.Add(-2 * time.Hour), EffectiveTo: now.Add(-55 * time.Minute), GroupCount: 3, FlowCount: 6, ConnectionCount: 264,
	}

	// Defaults: the last day, every workload.
	res, err := f.reader.Rollup(ctx, readmodel.RollupRequest{GroupBy: flowstore.GroupByRulePeer})
	if err != nil {
		t.Fatal(err)
	}
	q := f.flows.LastGroup
	if q == nil || !q.Until.Equal(now) || !q.Since.Equal(now.Add(-readmodel.DefaultRange)) || len(q.WorkloadIDs) != 0 || q.GroupBy != flowstore.GroupByRulePeer {
		t.Fatalf("store query = %+v", q)
	}
	if res.EffectiveFrom == nil || !res.EffectiveFrom.Equal(now.Add(-2*time.Hour)) || res.GroupCount != 3 || res.Truncated || res.Totals.ConnectionCount != 264 {
		t.Fatalf("rollup = %+v", res)
	}
	if len(res.Groups) != 3 {
		t.Fatalf("groups = %d", len(res.Groups))
	}
	g := res.Groups[0]
	if g.Keys.Rule == nil || g.Keys.Rule.ID != f.ruleID || g.Keys.Rule.AuthoredRuleID != f.rule.ID.String() || g.Keys.Rule.Protocol != innerwallv1.Protocol_PROTOCOL_TCP {
		t.Fatalf("rule key = %+v", g.Keys.Rule)
	}
	// The peer carries the stored snapshot (env=prod is not a current
	// label of web-1) and the current name of the stored identity.
	if g.Keys.Peer == nil || g.Keys.Peer.Name != "web-1" || g.Keys.Peer.Labels["env"] != "prod" || g.Keys.Src != nil || g.Keys.Dst != nil {
		t.Fatalf("peer key = %+v", g.Keys.Peer)
	}
	if res.Groups[1].Keys.Rule != nil || res.Groups[1].Keys.Peer.Name != "office" || res.Groups[2].Keys.Peer.Name != "" || res.Groups[2].Keys.Peer.Key != "198.51.100.7" {
		t.Fatalf("other keys = %+v %+v", res.Groups[1].Keys, res.Groups[2].Keys)
	}

	// A selector resolves to ids at read time; one matching nothing
	// answers empty without asking the store.
	f.flows.LastGroup = nil
	res, err = f.reader.Rollup(ctx, readmodel.RollupRequest{GroupBy: flowstore.GroupByRule, Selector: policy.Selector{"role": {"cache"}}})
	if err != nil || len(res.Groups) != 0 || res.GroupCount != 0 || res.EffectiveFrom != nil {
		t.Fatalf("empty scope = %+v, %v", res, err)
	}
	if f.flows.LastGroup != nil {
		t.Fatal("an empty scope reached the store, where it would mean every workload")
	}
	if _, err := f.reader.Rollup(ctx, readmodel.RollupRequest{GroupBy: flowstore.GroupByRule, Selector: policy.Selector{"role": {"db", "web"}}, Workload: &f.db}); err != nil {
		t.Fatal(err)
	}
	if q := f.flows.LastGroup; len(q.WorkloadIDs) != 1 || q.WorkloadIDs[0] != f.db {
		t.Fatalf("intersected scope = %v", q.WorkloadIDs)
	}
	svc := readmodel.Service{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Port: 5432}
	if _, err := f.reader.Rollup(ctx, readmodel.RollupRequest{GroupBy: flowstore.GroupByDstService, Service: &svc, Verdict: innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK, Order: flowstore.OrderByRecency, Limit: 7}); err != nil {
		t.Fatal(err)
	}
	if q := f.flows.LastGroup; q.Protocol != svc.Protocol || q.DstPort != 5432 || q.Decision != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK || q.Order != flowstore.OrderByRecency || q.Limit != 7 {
		t.Fatalf("filters not passed through: %+v", q)
	}

	// Refusals.
	unknown := identity.FromUUID(uuid.New())
	if _, err := f.reader.Rollup(ctx, readmodel.RollupRequest{GroupBy: flowstore.GroupByRule, Workload: &unknown}); !readmodel.IsUnknown(err) {
		t.Fatalf("unknown workload err = %v", err)
	}
	if _, err := f.reader.Rollup(ctx, readmodel.RollupRequest{GroupBy: "peer"}); !errors.Is(err, flowstore.ErrUnknownGroupBy) {
		t.Fatalf("bad grouping err = %v", err)
	}
	if _, err := f.reader.Rollup(ctx, readmodel.RollupRequest{GroupBy: flowstore.GroupByRule, From: now, To: now.Add(-time.Hour)}); !errors.Is(err, readmodel.ErrInvalidRange) {
		t.Fatalf("bad range err = %v", err)
	}
}

func TestListFlowsPages(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	base := now.Add(-2 * time.Hour)
	for i := range 5 {
		start := base.Add(time.Duration(i) * 10 * time.Minute)
		f.flows.Rows = append(f.flows.Rows, flowstore.WindowRow{ID: int64(i + 1), WorkloadID: f.db, WindowStart: start, WindowEnd: start.Add(5 * time.Minute), Record: flowstore.Record{
			Peer: flowstore.Peer{Kind: flowstore.PeerWorkload, Key: f.web.String(), Labels: map[string]string{"role": "web"}}, SrcAddress: netip.MustParseAddr("10.0.0.10"), DstAddress: netip.MustParseAddr("10.0.0.20"),
			DstPort: 5432, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED, MatchedRuleID: f.ruleID, ConnectionCount: 10,
		}})
	}
	if _, err := f.reader.ListFlows(ctx, readmodel.FlowsRequest{}); !errors.Is(err, readmodel.ErrWorkloadRequired) {
		t.Fatalf("no workload err = %v", err)
	}
	page, err := f.reader.ListFlows(ctx, readmodel.FlowsRequest{Workload: f.db, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Workload.Hostname != "db-1" || len(page.Flows) != 2 || page.NextCursor == "" || page.Flows[0].ID != 5 || page.Flows[1].ID != 4 {
		t.Fatalf("page 1 = %+v", page)
	}
	if fl := page.Flows[0]; fl.Peer.Name != "web-1" || fl.Rule == nil || fl.Rule.AuthoredRuleID != f.rule.ID.String() || fl.Service.Port != 5432 || fl.Verdict != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED {
		t.Fatalf("flow = %+v", fl)
	}
	if q := f.flows.LastPage; q.Limit != 3 || q.Before != nil {
		t.Fatalf("page query = %+v; the store is asked for one row more than the page", q)
	}
	var ids []int64
	for _, fl := range page.Flows {
		ids = append(ids, fl.ID)
	}
	cursor := page.NextCursor
	for cursor != "" {
		page, err = f.reader.ListFlows(ctx, readmodel.FlowsRequest{Workload: f.db, Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, fl := range page.Flows {
			ids = append(ids, fl.ID)
		}
		cursor = page.NextCursor
	}
	if len(ids) != 5 || ids[0] != 5 || ids[4] != 1 {
		t.Fatalf("walked %v", ids)
	}
	if _, err := f.reader.ListFlows(ctx, readmodel.FlowsRequest{Workload: f.db, Cursor: "nonsense"}); !errors.Is(err, readmodel.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
	unknown := identity.FromUUID(uuid.New())
	if _, err := f.reader.ListFlows(ctx, readmodel.FlowsRequest{Workload: unknown}); !readmodel.IsUnknown(err) {
		t.Fatalf("unknown workload err = %v", err)
	}
}

func TestWorkloadsAndPolicy(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	page, err := f.reader.ListWorkloads(ctx, readmodel.WorkloadsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Workloads) != 2 || page.Workloads[0].ID != f.db || page.Workloads[1].ID != f.web || page.NextCursor != "" {
		t.Fatalf("fleet = %+v", page)
	}
	db := page.Workloads[0]
	if db.Sync.State != innerwallv1.SyncState_SYNC_STATE_DEGRADED || db.Sync.LatestVersion != 5 || db.Sync.AppliedVersion != 0 || db.Sync.LatestRenderedAt == nil || db.Sync.Error != "apply refused" {
		t.Fatalf("db sync = %+v", db.Sync)
	}
	if db.Health.Credential.State != readmodel.CredentialRenewalFailed || db.Health.DroppedFlowRecords != 42 || db.Health.LastSeenAt != nil {
		t.Fatalf("db health = %+v", db.Health)
	}
	if page.Workloads[1].Health.Credential.State != readmodel.CredentialRenews {
		t.Fatalf("web health = %+v", page.Workloads[1].Health)
	}
	one, err := f.reader.ListWorkloads(ctx, readmodel.WorkloadsRequest{Limit: 1})
	if err != nil || len(one.Workloads) != 1 || one.NextCursor == "" {
		t.Fatalf("first page = %+v, %v", one, err)
	}
	two, err := f.reader.ListWorkloads(ctx, readmodel.WorkloadsRequest{Limit: 1, Cursor: one.NextCursor})
	if err != nil || len(two.Workloads) != 1 || two.Workloads[0].ID != f.web || two.NextCursor != "" {
		t.Fatalf("second page = %+v, %v", two, err)
	}
	if _, err := f.reader.ListWorkloads(ctx, readmodel.WorkloadsRequest{Cursor: "x"}); !errors.Is(err, readmodel.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
	none, err := f.reader.ListWorkloads(ctx, readmodel.WorkloadsRequest{Selector: policy.Selector{"role": {"cache"}}})
	if err != nil || len(none.Workloads) != 0 {
		t.Fatalf("no match = %+v, %v", none, err)
	}
	if f.store.LastPage != nil && len(f.store.LastPage.IDs) == 0 && f.store.LastPage.Limit == 0 {
		t.Fatal("an empty selector match reached the store as every workload")
	}
	got, err := f.reader.GetWorkload(ctx, f.web)
	if err != nil || got.Hostname != "web-1" || got.Sync.State != innerwallv1.SyncState_SYNC_STATE_SYNCED {
		t.Fatalf("detail = %+v, %v", got, err)
	}

	p, err := f.reader.RenderedPolicy(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 5 || p.Mode != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION || p.TerminalVerdict != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK || p.RenderedAt == nil || len(p.Rules) != 1 {
		t.Fatalf("policy = %+v", p)
	}
	r := p.Rules[0]
	if r.ID != f.ruleID || r.AuthoredRuleID != f.rule.ID.String() || r.Ruleset == nil || r.Ruleset.Name != "web-to-db" || r.Description != "postgres from web" || r.Verdict != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || len(r.Ports) != 1 || r.Ports[0].Start != 5432 || len(r.PeerCIDRs) != 1 {
		t.Fatalf("rule = %+v", r)
	}
	// No render yet: version zero, no rules, the mode's terminal verdict.
	p, err = f.reader.RenderedPolicy(ctx, f.web)
	if err != nil || p.Version != 0 || len(p.Rules) != 0 || p.TerminalVerdict != innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED {
		t.Fatalf("unrendered policy = %+v, %v", p, err)
	}
	if _, err := f.reader.RenderedPolicy(ctx, identity.FromUUID(uuid.New())); !readmodel.IsUnknown(err) {
		t.Fatalf("unknown err = %v", err)
	}
}
