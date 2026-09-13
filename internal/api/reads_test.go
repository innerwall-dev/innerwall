package api_test

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/readmodel/readmodeltest"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// readFixture is a two-workload estate behind the read model's doubles:
// db-1 degraded in simulation with a failing renewal, web-1 synced and
// enforced, one rule between them, a rollup with would-block traffic,
// and five windows on db-1.
type readFixture struct {
	store  *readmodeltest.MemStore
	flows  *readmodeltest.MemFlows
	reader *readmodel.Reader
	web    identity.WorkloadID
	db     identity.WorkloadID
	ruleID string
	group  policy.AddressGroup
}

func newReadFixture(now time.Time) *readFixture {
	web, _ := identity.NewWorkloadID()
	db, _ := identity.NewWorkloadID()
	seen := now.Add(-time.Minute)
	renderedAt := now.Add(-30 * time.Minute)
	rule := policy.Rule{ID: uuid.New(), Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true, Description: "postgres from web"}
	rs := policy.Ruleset{ID: uuid.New(), Name: "web-to-db", Enabled: true, Scope: policy.Selector{"role": {"db"}}, Rules: []policy.Rule{rule}, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-time.Hour)}
	ruleID := rendered.RuleID(rule.ID.String(), innerwallv1.Protocol_PROTOCOL_TCP)
	group := policy.AddressGroup{ID: uuid.New(), Name: "office", CIDRs: []string{"192.0.2.0/24"}}
	st := &readmodeltest.MemStore{
		Workloads: []readmodel.WorkloadRecord{
			{Workload: registry.Workload{ID: web, Hostname: "web-1", Labels: []registry.Label{{Key: "env", Value: "prod"}, {Key: "role", Value: "web"}}, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.10")}, Agent: registry.AgentInfo{Version: "0.3.0", Capabilities: []string{"nftables"}}, EnrolledAt: now.Add(-24 * time.Hour), LastSeenAt: &seen, SyncState: innerwallv1.SyncState_SYNC_STATE_SYNCED, AppliedVersion: 3, CredentialExpiresAt: now.Add(20 * time.Hour)}, LatestVersion: 3, LatestRenderedAt: &renderedAt},
			{Workload: registry.Workload{ID: db, Hostname: "db-1", Labels: []registry.Label{{Key: "env", Value: "prod"}, {Key: "role", Value: "db"}}, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, Facts: &innerwallv1.HostFacts{Os: &innerwallv1.OsInfo{Family: "linux", Name: "debian"}}, EnrolledAt: now.Add(-20 * time.Hour), SyncState: innerwallv1.SyncState_SYNC_STATE_DEGRADED, SyncError: "apply refused", CredentialExpiresAt: now.Add(2 * time.Hour), CredentialRenewalError: "authority unreachable", DroppedFlowRecords: 42}, LatestVersion: 5, LatestRenderedAt: &renderedAt, ListeningServices: []registry.ListeningService{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Port: 5432, ProcessName: "postgres"}}},
		},
		AddressGroups: []policy.AddressGroup{group},
		Rulesets:      []policy.Ruleset{rs},
		Policies: map[identity.WorkloadID]*innerwallv1.WorkloadPolicy{
			db: {Version: 5, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, InboundRules: []*innerwallv1.ResolvedRule{{RuleId: ruleID, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: []string{"10.0.0.10/32"}, Ports: []*innerwallv1.PortRange{{Start: 5432, End: 5432}}}}},
		},
	}
	fl := &readmodeltest.MemFlows{
		Result: &flowstore.GroupResult{
			Groups: []flowstore.Group{
				{RuleID: ruleID, Peer: flowstore.Peer{Kind: flowstore.PeerWorkload, Key: web.String(), Labels: map[string]string{"role": "web", "env": "prod"}}, WorkloadID: db, FlowCount: 2, ConnectionCount: 240, ByteCount: 960_000, FirstSeen: now.Add(-2 * time.Hour), LastSeen: now.Add(-time.Hour)},
				{Peer: flowstore.Peer{Kind: flowstore.PeerAddressGroup, Key: group.ID.String()}, WorkloadID: db, DstPort: 5432, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, FlowCount: 2, ConnectionCount: 6, FirstSeen: now.Add(-2 * time.Hour), LastSeen: now.Add(-time.Hour)},
				{Peer: flowstore.Peer{Kind: flowstore.PeerUnknown, Key: "198.51.100.7"}, WorkloadID: db, DstPort: 22, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, FlowCount: 2, ConnectionCount: 18, FirstSeen: now.Add(-2 * time.Hour), LastSeen: now.Add(-time.Hour)},
			},
			EffectiveFrom: now.Add(-2 * time.Hour), EffectiveTo: now.Add(-55 * time.Minute), GroupCount: 3, FlowCount: 6, ConnectionCount: 264, ByteCount: 963_600,
		},
	}
	for i := range 5 {
		start := now.Add(-2 * time.Hour).Add(time.Duration(i) * 10 * time.Minute)
		fl.Rows = append(fl.Rows, flowstore.WindowRow{ID: int64(i + 1), WorkloadID: db, WindowStart: start, WindowEnd: start.Add(5 * time.Minute), Record: flowstore.Record{
			Peer: flowstore.Peer{Kind: flowstore.PeerWorkload, Key: web.String(), Labels: map[string]string{"role": "web"}}, SrcAddress: netip.MustParseAddr("10.0.0.10"), DstAddress: netip.MustParseAddr("10.0.0.20"),
			DstPort: 5432, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED, MatchedRuleID: ruleID, ConnectionCount: 10, FirstSeen: start, LastSeen: start.Add(time.Minute),
		}})
	}
	return &readFixture{store: st, flows: fl, reader: &readmodel.Reader{Store: st, Flows: fl, Now: func() time.Time { return now }}, web: web, db: db, ruleID: ruleID, group: group}
}

// readSurface is a surface with a password set, a live token, and the
// read fixture behind the read endpoints.
type readSurface struct {
	*surface
	fx     *readFixture
	bearer map[string]string
}

func newReadSurface(t *testing.T) *readSurface {
	t.Helper()
	fx := newReadFixture(time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC))
	s := newSurface(t, api.Deps{Reads: fx.reader})
	s.setPassword(t, "Ada")
	token, _, err := s.operators.MintToken(context.Background(), "reads", 0)
	if err != nil {
		t.Fatal(err)
	}
	return &readSurface{surface: s, fx: fx, bearer: map[string]string{"Authorization": "Bearer " + token}}
}

func (s *readSurface) get(t *testing.T, path string) (*reply, map[string]any) {
	t.Helper()
	return s.do(t, s.client(false), request{method: http.MethodGet, path: path, headers: s.bearer})
}

// field walks a dotted path through a decoded JSON document.
func field(body any, path string) any {
	cur := body
	for _, key := range strings.Split(path, ".") {
		switch v := cur.(type) {
		case map[string]any:
			cur = v[key]
		case []any:
			idx := 0
			for _, c := range key {
				idx = idx*10 + int(c-'0')
			}
			if idx >= len(v) {
				return nil
			}
			cur = v[idx]
		default:
			return nil
		}
	}
	return cur
}

func expectInvalidParameter(t *testing.T, resp *reply, body map[string]any, param string) {
	t.Helper()
	expectProblem(t, resp, body, http.StatusBadRequest, api.ProblemInvalidParameter)
	if detail, _ := body["detail"].(string); !strings.HasPrefix(detail, "parameter "+param+":") {
		t.Fatalf("detail %q does not name parameter %s", detail, param)
	}
}

// TestReadEndpointsRequireCredential extends the middleware matrix to
// every read path: none is reachable without a credential, and each
// answers the same request once one is presented.
func TestReadEndpointsRequireCredential(t *testing.T) {
	s := newReadSurface(t)
	paths := []string{
		"/api/v1/flows/rollup?group_by=rule",
		"/api/v1/flows?workload=" + s.fx.db.String(),
		"/api/v1/workloads",
		"/api/v1/workloads/" + s.fx.db.String(),
		"/api/v1/workloads/" + s.fx.db.String() + "/rendered-policy",
		// Even a request that would be refused for its parameters is
		// refused for its credential first.
		"/api/v1/flows/rollup",
		"/api/v1/flows",
		"/api/v1/workloads/not-an-id",
	}
	c := s.client(false)
	for _, p := range paths {
		resp, body := s.do(t, c, request{method: http.MethodGet, path: p})
		expectProblem(t, resp, body, http.StatusUnauthorized, api.ProblemUnauthenticated)
		resp, body = s.do(t, c, request{method: http.MethodGet, path: p, headers: map[string]string{"Cookie": api.SessionCookie + "=" + strings.Repeat("A", 43)}})
		expectProblem(t, resp, body, http.StatusUnauthorized, api.ProblemUnauthenticated)
	}
	for _, p := range paths[:5] {
		resp, body := s.get(t, p)
		if resp.status != http.StatusOK {
			t.Fatalf("GET %s with a credential: %d %v", p, resp.status, body)
		}
	}
}

func TestRollupEndpoint(t *testing.T) {
	s := newReadSurface(t)
	bad := []struct{ query, param string }{
		{"", "group_by"},
		{"group_by=peer", "group_by"},
		{"group_by=rule%2Cpeer%2Cservice", "group_by"},
		{"group_by=rule&from=yesterday", "from"},
		{"group_by=rule&to=2026-09-13", "to"},
		{"group_by=rule&verdict=maybe", "verdict"},
		{"group_by=rule&direction=sideways", "direction"},
		{"group_by=rule&workload=not-an-id", "workload"},
		{"group_by=rule&label=novalue", "label"},
		{"group_by=rule&service=tcp", "service"},
		{"group_by=rule&order=alphabetical", "order"},
		{"group_by=rule&limit=0", "limit"},
		{"group_by=rule&from=2026-09-13T12:00:00Z&to=2026-09-13T11:00:00Z", "to"},
	}
	for _, tc := range bad {
		resp, body := s.get(t, "/api/v1/flows/rollup?"+tc.query)
		expectInvalidParameter(t, resp, body, tc.param)
		if tc.param == "group_by" && !strings.Contains(body["detail"].(string), flowstore.GroupByNames()) {
			t.Fatalf("group_by problem does not name the allowed set: %v", body["detail"])
		}
	}
	resp, body := s.get(t, "/api/v1/flows/rollup?group_by=rule&workload="+uuid.NewString())
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)

	q := url.Values{"group_by": {"rule, peer"}, "verdict": {"would_block"}, "label": {"env=prod", "role=db"}, "service": {"tcp/5432"}, "order": {"recent"}, "limit": {"2"}, "from": {"2026-09-13T09:00:00Z"}}
	resp, body = s.get(t, "/api/v1/flows/rollup?"+q.Encode())
	if resp.status != http.StatusOK {
		t.Fatalf("rollup: %d %v", resp.status, body)
	}
	if ct := resp.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type %q", ct)
	}
	if got := field(body, "group_by"); len(got.([]any)) != 2 || got.([]any)[0] != "rule" || got.([]any)[1] != "peer" {
		t.Fatalf("group_by = %v", got)
	}
	if body["from"] != "2026-09-13T09:00:00Z" || body["to"] != "2026-09-13T12:00:00Z" || body["effective_from"] != "2026-09-13T10:00:00Z" || body["effective_to"] != "2026-09-13T11:05:00Z" {
		t.Fatalf("range fields = %v %v %v %v", body["from"], body["to"], body["effective_from"], body["effective_to"])
	}
	// The doubles truncate at the limit; the totals are the whole's.
	if body["truncated"] != true || body["group_count"] != float64(3) || field(body, "totals.connection_count") != float64(264) || len(field(body, "groups").([]any)) != 2 {
		t.Fatalf("bounds = truncated %v group_count %v totals %v", body["truncated"], body["group_count"], body["totals"])
	}
	if field(body, "groups.0.keys.rule.id") != s.fx.ruleID || field(body, "groups.0.keys.rule.protocol") != "tcp" || field(body, "groups.0.keys.peer.workload_id") != s.fx.web.String() || field(body, "groups.0.keys.peer.name") != "web-1" || field(body, "groups.0.keys.peer.labels.env") != "prod" || field(body, "groups.0.keys.peer.kind") != "workload" {
		t.Fatalf("group 0 keys = %v", field(body, "groups.0.keys"))
	}
	if field(body, "groups.0.connection_count") != float64(240) || field(body, "groups.0.flow_count") != float64(2) || field(body, "groups.0.first_seen") != "2026-09-13T10:00:00Z" {
		t.Fatalf("group 0 = %v", field(body, "groups.0"))
	}
	if v, present := field(body, "groups.1.keys").(map[string]any)["rule"]; !present || v != nil || field(body, "groups.1.keys.peer.address_group_id") != s.fx.group.ID.String() || field(body, "groups.1.keys.peer.name") != "office" {
		t.Fatalf("group 1 keys = %v", field(body, "groups.1.keys"))
	}
	// What reached the store is what was asked, resolved: the selector
	// to db-1, the service, the verdict, the order, the limit.
	sq := s.fx.flows.LastGroup
	if sq == nil || sq.GroupBy != flowstore.GroupByRulePeer || len(sq.WorkloadIDs) != 1 || sq.WorkloadIDs[0] != s.fx.db || sq.DstPort != 5432 || sq.Decision != innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK || sq.Order != flowstore.OrderByRecency || sq.Limit != 2 {
		t.Fatalf("store query = %+v", sq)
	}
	// Other groupings carry their own keys.
	resp, body = s.get(t, "/api/v1/flows/rollup?group_by=src,dst")
	if resp.status != http.StatusOK || field(body, "groups.2.keys.src.address") != "198.51.100.7" || field(body, "groups.2.keys.dst.hostname") != "db-1" || field(body, "groups.2.keys.dst.labels.role") != "db" {
		t.Fatalf("src,dst keys = %v", field(body, "groups.2.keys"))
	}
	resp, body = s.get(t, "/api/v1/flows/rollup?group_by=dst,service")
	if resp.status != http.StatusOK || field(body, "groups.2.keys.service.protocol") != "tcp" || field(body, "groups.2.keys.service.port") != float64(22) || field(body, "groups.2.keys.dst.id") != s.fx.db.String() {
		t.Fatalf("dst,service keys = %v", field(body, "groups.2.keys"))
	}
	// A scope matching nothing is an empty rollup with null bounds.
	resp, body = s.get(t, "/api/v1/flows/rollup?group_by=rule&label=role=cache")
	if resp.status != http.StatusOK || len(field(body, "groups").([]any)) != 0 || body["effective_from"] != nil || body["group_count"] != float64(0) {
		t.Fatalf("empty rollup = %v", body)
	}
}

func TestFlowsEndpoint(t *testing.T) {
	s := newReadSurface(t)
	resp, body := s.get(t, "/api/v1/flows")
	expectInvalidParameter(t, resp, body, "workload")
	resp, body = s.get(t, "/api/v1/flows?workload=nope")
	expectInvalidParameter(t, resp, body, "workload")
	resp, body = s.get(t, "/api/v1/flows?workload="+s.fx.db.String()+"&cursor=garbage")
	expectInvalidParameter(t, resp, body, "cursor")
	resp, body = s.get(t, "/api/v1/flows?workload="+s.fx.db.String()+"&limit=-1")
	expectInvalidParameter(t, resp, body, "limit")
	resp, body = s.get(t, "/api/v1/flows?workload="+uuid.NewString())
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)

	resp, body = s.get(t, "/api/v1/flows?workload="+s.fx.db.String()+"&limit=2&peer="+s.fx.web.String()+"&service=tcp/5432&verdict=allowed&direction=inbound")
	if resp.status != http.StatusOK {
		t.Fatalf("flows: %d %v", resp.status, body)
	}
	if field(body, "workload.hostname") != "db-1" || len(field(body, "flows").([]any)) != 2 || body["next_cursor"] == nil {
		t.Fatalf("page 1 = %v", body)
	}
	f0 := field(body, "flows.0").(map[string]any)
	if f0["id"] != float64(5) || f0["window_start"] != "2026-09-13T10:40:00Z" || f0["verdict"] != "allowed" || f0["direction"] != "inbound" || field(f0, "service.port") != float64(5432) || field(f0, "peer.name") != "web-1" || field(f0, "rule.authored_rule_id") == nil || f0["src_address"] != "10.0.0.10" {
		t.Fatalf("flow 0 = %v", f0)
	}
	pq := s.fx.flows.LastPage
	if pq == nil || pq.PeerKey != s.fx.web.String() || pq.DstPort != 5432 || pq.Decision != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || pq.Direction != innerwallv1.Direction_DIRECTION_INBOUND || pq.Limit != 3 {
		t.Fatalf("store query = %+v", pq)
	}
	var ids []float64
	for _, f := range field(body, "flows").([]any) {
		ids = append(ids, f.(map[string]any)["id"].(float64))
	}
	for body["next_cursor"] != nil {
		resp, body = s.get(t, "/api/v1/flows?workload="+s.fx.db.String()+"&limit=2&cursor="+url.QueryEscape(body["next_cursor"].(string)))
		if resp.status != http.StatusOK {
			t.Fatalf("next page: %d %v", resp.status, body)
		}
		for _, f := range field(body, "flows").([]any) {
			ids = append(ids, f.(map[string]any)["id"].(float64))
		}
	}
	if len(ids) != 5 || ids[0] != 5 || ids[4] != 1 {
		t.Fatalf("walked %v", ids)
	}
}

func TestWorkloadEndpoints(t *testing.T) {
	s := newReadSurface(t)
	resp, body := s.get(t, "/api/v1/workloads")
	if resp.status != http.StatusOK || len(field(body, "workloads").([]any)) != 2 || body["next_cursor"] != nil {
		t.Fatalf("list: %d %v", resp.status, body)
	}
	db := field(body, "workloads.0").(map[string]any)
	if db["id"] != s.fx.db.String() || db["hostname"] != "db-1" || db["mode"] != "simulation" || field(db, "labels.role") != "db" || field(db, "os.family") != "linux" {
		t.Fatalf("db-1 = %v", db)
	}
	if field(db, "sync.state") != "degraded" || field(db, "sync.applied_version") != float64(0) || field(db, "sync.latest_version") != float64(5) || field(db, "sync.latest_rendered_at") != "2026-09-13T11:30:00Z" || field(db, "sync.error") != "apply refused" {
		t.Fatalf("db-1 sync = %v", db["sync"])
	}
	if field(db, "health.credential.state") != "renewal-failed" || field(db, "health.credential.last_error") != "authority unreachable" || field(db, "health.credential.expires_at") != "2026-09-13T14:00:00Z" || field(db, "health.dropped_flow_records") != float64(42) || field(db, "health.last_seen_at") != nil {
		t.Fatalf("db-1 health = %v", db["health"])
	}
	if field(db, "listening_services.0.port") != float64(5432) || field(db, "listening_services.0.protocol") != "tcp" {
		t.Fatalf("db-1 listening = %v", db["listening_services"])
	}
	web := field(body, "workloads.1").(map[string]any)
	if field(web, "sync.state") != "synced" || field(web, "health.credential.state") != "renews" || field(web, "health.last_seen_at") != "2026-09-13T11:59:00Z" || field(web, "agent.capabilities.0") != "nftables" || field(web, "addresses.0") != "10.0.0.10" {
		t.Fatalf("web-1 = %v", web)
	}

	for _, tc := range []struct{ query, param string }{{"mode=strict", "mode"}, {"sync_state=asleep", "sync_state"}, {"label=x", "label"}, {"cursor=x", "cursor"}, {"limit=many", "limit"}} {
		resp, body := s.get(t, "/api/v1/workloads?"+tc.query)
		expectInvalidParameter(t, resp, body, tc.param)
	}
	resp, body = s.get(t, "/api/v1/workloads?sync_state=synced&mode=enforced&label=role=web")
	if resp.status != http.StatusOK || len(field(body, "workloads").([]any)) != 1 || field(body, "workloads.0.hostname") != "web-1" {
		t.Fatalf("filtered list = %v", body)
	}
	resp, body = s.get(t, "/api/v1/workloads?limit=1")
	if resp.status != http.StatusOK || len(field(body, "workloads").([]any)) != 1 || body["next_cursor"] == nil {
		t.Fatalf("first page = %v", body)
	}
	resp, body = s.get(t, "/api/v1/workloads?limit=1&cursor="+url.QueryEscape(body["next_cursor"].(string)))
	if resp.status != http.StatusOK || field(body, "workloads.0.hostname") != "web-1" || body["next_cursor"] != nil {
		t.Fatalf("second page = %v", body)
	}

	// Detail: the same shape.
	resp, body = s.get(t, "/api/v1/workloads/"+s.fx.db.String())
	if resp.status != http.StatusOK || body["hostname"] != "db-1" || field(body, "sync.state") != "degraded" || field(body, "health.credential.state") != "renewal-failed" {
		t.Fatalf("detail = %d %v", resp.status, body)
	}
	resp, body = s.get(t, "/api/v1/workloads/"+uuid.NewString())
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	resp, body = s.get(t, "/api/v1/workloads/not-an-id")
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	resp, body = s.get(t, "/api/v1/workloads/"+s.fx.db.String()+"/nothing")
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	resp, body = s.do(t, s.client(false), request{method: http.MethodPost, path: "/api/v1/workloads", headers: s.bearer})
	expectProblem(t, resp, body, http.StatusMethodNotAllowed, api.ProblemMethodNotAllowed)
	if resp.header.Get("Allow") != "GET" {
		t.Fatalf("Allow %q", resp.header.Get("Allow"))
	}

	// Rendered policy: configuration only.
	resp, body = s.get(t, "/api/v1/workloads/"+s.fx.db.String()+"/rendered-policy")
	if resp.status != http.StatusOK || body["version"] != float64(5) || body["mode"] != "simulation" || body["terminal_verdict"] != "would_block" || body["rendered_at"] != "2026-09-13T11:30:00Z" || field(body, "workload.hostname") != "db-1" {
		t.Fatalf("rendered policy = %d %v", resp.status, body)
	}
	r0 := field(body, "rules.0").(map[string]any)
	if r0["id"] != s.fx.ruleID || r0["verdict"] != "allowed" || r0["protocol"] != "tcp" || field(r0, "ports.0.start") != float64(5432) || field(r0, "peer_cidrs.0") != "10.0.0.10/32" || field(r0, "ruleset.name") != "web-to-db" || field(r0, "ruleset.updated_at") != "2026-09-13T11:00:00Z" || r0["description"] != "postgres from web" {
		t.Fatalf("rule 0 = %v", r0)
	}
	for k := range r0 {
		if strings.Contains(k, "count") {
			t.Fatalf("rendered policy carries a count: %s", k)
		}
	}
	resp, body = s.get(t, "/api/v1/workloads/"+s.fx.web.String()+"/rendered-policy")
	if resp.status != http.StatusOK || body["version"] != float64(0) || body["terminal_verdict"] != "blocked" || len(field(body, "rules").([]any)) != 0 {
		t.Fatalf("unrendered policy = %v", body)
	}
	resp, body = s.get(t, "/api/v1/workloads/"+uuid.NewString()+"/rendered-policy")
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
}
