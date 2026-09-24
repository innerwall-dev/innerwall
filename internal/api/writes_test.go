package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/enroll/enrolltest"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/fleet/fleettest"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/readmodel/readmodeltest"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// writeSurface is a surface with every domain behind it over one
// in-memory store: three workloads, one service, one ruleset admitting
// web-1 to db-1 (so db-1 is at rendered version 2 and the others at 1,
// all applied at 1), and a live operator token. The domain clock advances
// a millisecond per reading, so consecutive writes take distinct
// versions.
type writeSurface struct {
	*surface
	mem            *fleettest.MemStore
	enrollStore    *enrolltest.MemStore
	web, db, cache identity.WorkloadID
	service        policy.Service
	ruleset        policy.Ruleset
	bearer         map[string]string
}

func newWriteSurface(t *testing.T) *writeSurface {
	t.Helper()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	mem := fleettest.New()
	ws := &writeSurface{mem: mem, enrollStore: enrolltest.NewMemStore()}
	ws.web, _ = identity.NewWorkloadID()
	ws.db, _ = identity.NewWorkloadID()
	ws.cache, _ = identity.NewWorkloadID()
	seen := now.Add(-time.Minute)
	mk := func(id identity.WorkloadID, host, role, addr string, mode innerwallv1.EnforcementMode) registry.Workload {
		return registry.Workload{ID: id, Hostname: host, Labels: []registry.Label{{Key: "env", Value: "prod"}, {Key: "role", Value: role}}, Addresses: []netip.Addr{netip.MustParseAddr(addr)}, Mode: mode, EnrolledAt: now.Add(-24 * time.Hour), LastSeenAt: &seen, SyncState: innerwallv1.SyncState_SYNC_STATE_SYNCED, CredentialExpiresAt: now.Add(20 * time.Hour)}
	}
	mem.Workloads = []registry.Workload{
		mk(ws.web, "web-1", "web", "10.0.0.10", innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED),
		mk(ws.db, "db-1", "db", "10.0.0.20", innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION),
		mk(ws.cache, "cache-1", "cache", "10.0.0.30", innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY),
	}
	clock := now
	tick := func() time.Time { clock = clock.Add(time.Millisecond); return clock }
	engine := &compiler.Engine{Store: mem, Now: tick}
	authoring := &policy.Authoring{Store: mem, Renderer: fleettest.Renderer{Engine: engine}, Now: tick}
	ws.service = policy.Service{Name: "postgres", Entries: []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 5432, End: 5432}}}}}
	if err := authoring.CreateService(context.Background(), &ws.service); err != nil {
		t.Fatal(err)
	}
	ws.ruleset = policy.Ruleset{Name: "web-to-db", Enabled: true, Scope: policy.Selector{"role": {"db"}}, Rules: []policy.Rule{{
		Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true, Description: "postgres from web",
		Peers:      []policy.Peer{{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"role": {"web"}}}},
		ServiceIDs: []uuid.UUID{ws.service.ID},
	}}}
	if err := authoring.CreateRuleset(context.Background(), &ws.ruleset); err != nil {
		t.Fatal(err)
	}
	// The agents applied the first render.
	for i := range mem.Workloads {
		mem.Workloads[i].AppliedVersion = 1
	}
	fleetSvc := &fleet.Service{Store: mem, Engine: engine, Now: tick}
	enrollSvc := &enroll.Service{Store: ws.enrollStore, Now: func() time.Time { return now }}
	reads := &readmodel.Reader{Store: mem, Flows: &readmodeltest.MemFlows{}, Now: func() time.Time { return now }}
	ws.surface = newSurface(t, api.Deps{Reads: reads, Authoring: authoring, Fleet: fleetSvc, Enroll: enrollSvc})
	ws.setPassword(t, "Ada")
	token, _, err := ws.operators.MintToken(context.Background(), "writes", 0)
	if err != nil {
		t.Fatal(err)
	}
	ws.bearer = map[string]string{"Authorization": "Bearer " + token}
	return ws
}

// call sends an authenticated request with a JSON body and the given
// extra headers.
func (s *writeSurface) call(t *testing.T, method, path, body string, headers map[string]string) (*reply, map[string]any) {
	t.Helper()
	h := map[string]string{}
	for k, v := range s.bearer {
		h[k] = v
	}
	for k, v := range headers {
		h[k] = v
	}
	return s.do(t, s.client(false), request{method: method, path: path, body: body, headers: h})
}

func ifMatch(etag string) map[string]string { return map[string]string{"If-Match": etag} }

func jsonBody(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func expectFindings(t *testing.T, resp *reply, body map[string]any, paths ...string) {
	t.Helper()
	expectProblem(t, resp, body, http.StatusBadRequest, api.ProblemValidation)
	errs, _ := body["errors"].([]any)
	got := map[string]map[string]any{}
	for _, e := range errs {
		m := e.(map[string]any)
		got[m["path"].(string)] = m
	}
	for _, p := range paths {
		if got[p] == nil || got[p]["rule"] == "" || got[p]["message"] == "" {
			t.Fatalf("findings %v lack a complete finding at %q", errs, p)
		}
	}
}

func TestWriteEndpointsRequireCredential(t *testing.T) {
	s := newWriteSurface(t)
	id := s.ruleset.ID.String()
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/rulesets"}, {http.MethodPost, "/api/v1/rulesets"}, {http.MethodGet, "/api/v1/rulesets/" + id}, {http.MethodPut, "/api/v1/rulesets/" + id}, {http.MethodDelete, "/api/v1/rulesets/" + id},
		{http.MethodPost, "/api/v1/rulesets/" + id + "/rules"}, {http.MethodGet, "/api/v1/rulesets/" + id + "/rules/" + id}, {http.MethodPut, "/api/v1/rulesets/" + id + "/rules/" + id}, {http.MethodDelete, "/api/v1/rulesets/" + id + "/rules/" + id},
		{http.MethodGet, "/api/v1/services"}, {http.MethodPost, "/api/v1/services"}, {http.MethodGet, "/api/v1/services/" + id}, {http.MethodPut, "/api/v1/services/" + id}, {http.MethodDelete, "/api/v1/services/" + id},
		{http.MethodGet, "/api/v1/address-groups"}, {http.MethodPost, "/api/v1/address-groups"}, {http.MethodGet, "/api/v1/address-groups/" + id}, {http.MethodPut, "/api/v1/address-groups/" + id}, {http.MethodDelete, "/api/v1/address-groups/" + id},
		{http.MethodGet, "/api/v1/workloads/" + s.db.String() + "/labels"}, {http.MethodPut, "/api/v1/workloads/" + s.db.String() + "/labels"},
		{http.MethodPost, "/api/v1/mode-changes"}, {http.MethodPost, "/api/v1/selectors/preview"}, {http.MethodPost, "/api/v1/policies/render-dryrun"},
		{http.MethodGet, "/api/v1/provisioning-tokens"}, {http.MethodPost, "/api/v1/provisioning-tokens"}, {http.MethodDelete, "/api/v1/provisioning-tokens/" + id},
		{http.MethodGet, "/api/v1/operator-tokens"}, {http.MethodPost, "/api/v1/operator-tokens"}, {http.MethodDelete, "/api/v1/operator-tokens/" + id},
	}
	c := s.client(false)
	for _, rt := range routes {
		resp, body := s.do(t, c, request{method: rt.method, path: rt.path, body: `{}`, headers: map[string]string{"If-Match": `"1"`}})
		expectProblem(t, resp, body, http.StatusUnauthorized, api.ProblemUnauthenticated)
	}
	// Every route above is one the surface mounts, and nothing is mounted
	// that this matrix does not cover, session and reads aside.
	covered := map[string]bool{}
	for _, rt := range routes {
		p := strings.ReplaceAll(rt.path, id, "{id}")
		p = strings.ReplaceAll(p, s.db.String(), "{id}")
		p = strings.Replace(p, "/rules/{id}", "/rules/{rule_id}", 1)
		covered[rt.method+" "+p] = true
	}
	for _, mounted := range api.New(api.Deps{}).Routes() {
		if strings.Contains(mounted, "/session") || strings.Contains(mounted, "/me") || strings.Contains(mounted, "/flows") || strings.HasSuffix(mounted, "/workloads") || strings.HasSuffix(mounted, "/workloads/{id}") || strings.HasSuffix(mounted, "/rendered-policy") {
			continue
		}
		if !covered[mounted] {
			t.Fatalf("mounted write route %q is not in the credential matrix", mounted)
		}
	}
}

func TestConditionalRequests(t *testing.T) {
	s := newWriteSurface(t)
	path := "/api/v1/rulesets/" + s.ruleset.ID.String()
	resp, body := s.call(t, http.MethodGet, path, "", nil)
	if resp.status != http.StatusOK || resp.header.Get("ETag") == "" || body["version"] == nil || resp.header.Get("ETag") != `"`+body["version"].(string)+`"` {
		t.Fatalf("GET: %d etag %q body %v", resp.status, resp.header.Get("ETag"), body)
	}
	current := resp.header.Get("ETag")
	doc := jsonBody(map[string]any{"name": "web-to-db", "description": "edited", "scope": map[string][]string{"role": {"db"}}, "rules": body["rules"]})

	// Absent, wildcard: precondition required.
	for _, h := range []map[string]string{nil, ifMatch("*"), ifMatch("")} {
		resp, body = s.call(t, http.MethodPut, path, doc, h)
		expectProblem(t, resp, body, http.StatusPreconditionRequired, api.ProblemPreconditionRequired)
		resp, body = s.call(t, http.MethodDelete, path, "", h)
		expectProblem(t, resp, body, http.StatusPreconditionRequired, api.ProblemPreconditionRequired)
	}
	// Stale, weak, or a form that is not byte-exact the current token:
	// precondition failed, naming the current version.
	for _, h := range []map[string]string{ifMatch(`"0"`), ifMatch(`"01"`), ifMatch("W/" + current), ifMatch(`"not-a-version"`)} {
		resp, body = s.call(t, http.MethodPut, path, doc, h)
		expectProblem(t, resp, body, http.StatusPreconditionFailed, api.ProblemPreconditionFailed)
		if body["current_version"] != strings.Trim(current, `"`) {
			t.Fatalf("412 current_version = %v, want %s", body["current_version"], current)
		}
	}
	if rs, _ := s.mem.GetRuleset(context.Background(), s.ruleset.ID); rs.Description != "" {
		t.Fatal("a refused write changed the ruleset")
	}
	// The current version, quoted or bare, is accepted and the tag moves.
	resp, body = s.call(t, http.MethodPut, path, doc, ifMatch(current))
	if resp.status != http.StatusOK || body["description"] != "edited" || resp.header.Get("ETag") == current || resp.header.Get("ETag") == "" {
		t.Fatalf("PUT: %d %v etag %q", resp.status, body, resp.header.Get("ETag"))
	}
	next := resp.header.Get("ETag")
	resp, body = s.call(t, http.MethodPut, path, doc, ifMatch(current))
	expectProblem(t, resp, body, http.StatusPreconditionFailed, api.ProblemPreconditionFailed)
	// A rewrite with no change still moves the version: it counts
	// writes, it is not a digest.
	resp, _ = s.call(t, http.MethodPut, path, doc, ifMatch(strings.Trim(next, `"`)))
	if resp.status != http.StatusOK {
		t.Fatalf("bare version refused: %d", resp.status)
	}
	// Delete is conditional too, and the second delete is not found.
	resp, body = s.call(t, http.MethodDelete, path, "", ifMatch(next))
	expectProblem(t, resp, body, http.StatusPreconditionFailed, api.ProblemPreconditionFailed)
	resp, _ = s.call(t, http.MethodDelete, path, "", ifMatch(`"`+body["current_version"].(string)+`"`))
	if resp.status != http.StatusNoContent {
		t.Fatalf("DELETE: %d", resp.status)
	}
	resp, body = s.call(t, http.MethodDelete, path, "", ifMatch(`"1"`))
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
}

func TestRulesetAndRuleEndpoints(t *testing.T) {
	s := newWriteSurface(t)
	resp, body := s.call(t, http.MethodGet, "/api/v1/rulesets", "", nil)
	if resp.status != http.StatusOK || len(field(body, "rulesets").([]any)) != 1 || body["state_version"] == "" {
		t.Fatalf("list: %d %v", resp.status, body)
	}
	stateVersion := body["state_version"].(string)
	// The surface writes ids for references, never names.
	if field(body, "rulesets.0.rules.0.services.0") != s.service.ID.String() || field(body, "rulesets.0.rules.0.version") == nil || field(body, "rulesets.0.rules.0.created_at") == nil {
		t.Fatalf("rule as listed = %v", field(body, "rulesets.0.rules.0"))
	}

	// Create: a document naming the service by name is accepted; the
	// response carries the id, the version, and the location.
	doc := map[string]any{"name": "office-to-web", "scope": map[string][]string{"role": {"web"}}, "rules": []map[string]any{{"direction": "inbound", "peers": []map[string]any{{"cidr": "192.0.2.0/24"}}, "services": []string{"postgres"}}}}
	resp, body = s.call(t, http.MethodPost, "/api/v1/rulesets", jsonBody(doc), nil)
	if resp.status != http.StatusCreated || body["id"] == nil || body["version"] == nil || resp.header.Get("ETag") == "" || resp.header.Get("Location") != "/api/v1/rulesets/"+body["id"].(string) || field(body, "rules.0.services.0") != s.service.ID.String() {
		t.Fatalf("create: %d %v %v", resp.status, body, resp.header)
	}
	created := body["id"].(string)
	// The state version moved with the write, and the render admitted
	// the new ruleset onto web-1.
	_, body = s.call(t, http.MethodGet, "/api/v1/rulesets", "", nil)
	if body["state_version"] == stateVersion {
		t.Fatal("state version did not move")
	}
	resp, body = s.call(t, http.MethodGet, "/api/v1/workloads/"+s.web.String(), "", nil)
	if resp.status != http.StatusOK || field(body, "sync.latest_version") != float64(2) || field(body, "sync.applied_version") != float64(1) {
		t.Fatalf("web-1 after create = %v", body["sync"])
	}

	// Validation: every finding at once, in the client's shape.
	bad := map[string]any{"name": "", "scope": map[string][]string{}, "rules": []map[string]any{{"direction": "outbound", "peers": []map[string]any{{"cidr": "nope"}}, "services": []string{"missing"}}}}
	resp, body = s.call(t, http.MethodPost, "/api/v1/rulesets", jsonBody(bad), nil)
	expectFindings(t, resp, body, "rules[0].services[0]")
	bad["rules"].([]map[string]any)[0]["services"] = []string{"postgres"}
	resp, body = s.call(t, http.MethodPost, "/api/v1/rulesets", jsonBody(bad), nil)
	expectFindings(t, resp, body, "name", "scope", "rules[0].direction", "rules[0].peers[0].cidr")
	if r := field(body, "errors.0.rule"); r != "name-required" {
		t.Fatalf("finding rule = %v", r)
	}
	// Conflicts and misses.
	resp, body = s.call(t, http.MethodPost, "/api/v1/rulesets", jsonBody(doc), nil)
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemDuplicateName)
	resp, body = s.call(t, http.MethodGet, "/api/v1/rulesets/"+uuid.NewString(), "", nil)
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	resp, body = s.call(t, http.MethodGet, "/api/v1/rulesets/not-an-id", "", nil)
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	resp, body = s.call(t, http.MethodPost, "/api/v1/rulesets", `{"name":`, nil)
	expectProblem(t, resp, body, http.StatusBadRequest, api.ProblemInvalidRequest)

	// Rules: create under the ruleset, read, update conditioned on the
	// rule's own version, delete.
	rulePath := "/api/v1/rulesets/" + created + "/rules"
	resp, body = s.call(t, http.MethodPost, rulePath, jsonBody(map[string]any{"direction": "inbound", "description": "ssh", "peers": []map[string]any{{"workloads": map[string][]string{"role": {"db"}}}}, "entries": []map[string]any{{"protocol": "tcp", "ports": []string{"22"}}}}), nil)
	if resp.status != http.StatusCreated || body["id"] == nil || resp.header.Get("Location") != rulePath+"/"+body["id"].(string) || field(body, "entries.0.ports.0") != "22" {
		t.Fatalf("rule create: %d %v", resp.status, body)
	}
	ruleID, ruleTag := body["id"].(string), resp.header.Get("ETag")
	resp, body = s.call(t, http.MethodPost, rulePath, jsonBody(map[string]any{"direction": "inbound", "peers": []map[string]any{}, "entries": []map[string]any{{"protocol": "icmp", "ports": []string{"1"}}}}), nil)
	expectFindings(t, resp, body, "peers", "entries[0].ports")
	resp, body = s.call(t, http.MethodGet, rulePath+"/"+ruleID, "", nil)
	if resp.status != http.StatusOK || resp.header.Get("ETag") != ruleTag || body["description"] != "ssh" {
		t.Fatalf("rule get: %d %v", resp.status, body)
	}
	resp, body = s.call(t, http.MethodGet, "/api/v1/rulesets/"+created, "", nil)
	if len(field(body, "rules").([]any)) != 2 {
		t.Fatalf("ruleset after rule create = %v", body)
	}
	rulesetTag := resp.header.Get("ETag")
	update := jsonBody(map[string]any{"direction": "inbound", "description": "ssh from db", "peers": []map[string]any{{"workloads": map[string][]string{"role": {"db"}}}}, "entries": []map[string]any{{"protocol": "tcp", "ports": []string{"22"}}}})
	resp, body = s.call(t, http.MethodPut, rulePath+"/"+ruleID, update, nil)
	expectProblem(t, resp, body, http.StatusPreconditionRequired, api.ProblemPreconditionRequired)
	resp, body = s.call(t, http.MethodPut, rulePath+"/"+ruleID, update, ifMatch(`"0"`))
	expectProblem(t, resp, body, http.StatusPreconditionFailed, api.ProblemPreconditionFailed)
	if body["current_version"] != strings.Trim(ruleTag, `"`) {
		t.Fatalf("rule 412 names %v, want the rule's version %s", body["current_version"], ruleTag)
	}
	// The ruleset's own tag moved when the rule was added; the rule's is
	// what its write is judged by, and the ruleset's tag is refused on
	// it once they differ.
	resp, _ = s.call(t, http.MethodGet, "/api/v1/rulesets/"+created, "", nil)
	if resp.header.Get("ETag") != rulesetTag {
		t.Fatalf("ruleset tag moved without a write: %s vs %s", resp.header.Get("ETag"), rulesetTag)
	}
	resp, body = s.call(t, http.MethodPut, rulePath+"/"+ruleID, update, ifMatch(ruleTag))
	if resp.status != http.StatusOK || body["description"] != "ssh from db" || resp.header.Get("ETag") == ruleTag {
		t.Fatalf("rule update: %d %v", resp.status, body)
	}
	resp, body = s.call(t, http.MethodPut, rulePath+"/"+uuid.NewString(), update, ifMatch(ruleTag))
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	resp, _ = s.call(t, http.MethodDelete, rulePath+"/"+ruleID, "", ifMatch(`"0"`))
	if resp.status != http.StatusPreconditionFailed {
		t.Fatalf("delete with a wrong tag: %d", resp.status)
	}
	resp, _ = s.call(t, http.MethodGet, rulePath+"/"+ruleID, "", nil)
	resp, _ = s.call(t, http.MethodDelete, rulePath+"/"+ruleID, "", ifMatch(resp.header.Get("ETag")))
	if resp.status != http.StatusNoContent {
		t.Fatalf("rule delete: %d", resp.status)
	}
	resp, body = s.call(t, http.MethodGet, rulePath+"/"+ruleID, "", nil)
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
}

func TestServiceAndAddressGroupEndpoints(t *testing.T) {
	s := newWriteSurface(t)
	resp, body := s.call(t, http.MethodPost, "/api/v1/services", jsonBody(map[string]any{"name": "redis", "entries": []map[string]any{{"protocol": "tcp", "ports": []string{"6379", "16379-16380"}}}}), nil)
	if resp.status != http.StatusCreated || body["id"] == nil || field(body, "entries.0.ports.1") != "16379-16380" || body["version"] == nil {
		t.Fatalf("service create: %d %v", resp.status, body)
	}
	svcID, svcTag := body["id"].(string), resp.header.Get("ETag")
	resp, body = s.call(t, http.MethodPost, "/api/v1/services", jsonBody(map[string]any{"name": "", "entries": []map[string]any{{"protocol": "tcp", "ports": []string{"x"}}, {"protocol": "icmp", "ports": []string{"1"}}}}), nil)
	expectFindings(t, resp, body, "entries[0].ports[0]")
	resp, body = s.call(t, http.MethodPost, "/api/v1/services", jsonBody(map[string]any{"name": "", "entries": []map[string]any{{"protocol": "icmp", "ports": []string{"1"}}}}), nil)
	expectFindings(t, resp, body, "name", "entries[0].ports")
	resp, body = s.call(t, http.MethodPut, "/api/v1/services/"+svcID, jsonBody(map[string]any{"name": "redis", "entries": []map[string]any{{"protocol": "tcp", "ports": []string{"6379"}}}}), ifMatch(svcTag))
	if resp.status != http.StatusOK || len(field(body, "entries.0.ports").([]any)) != 1 {
		t.Fatalf("service update: %d %v", resp.status, body)
	}
	resp, body = s.call(t, http.MethodGet, "/api/v1/services", "", nil)
	if resp.status != http.StatusOK || len(field(body, "services").([]any)) != 2 {
		t.Fatalf("services: %v", body)
	}
	// The seeded service is referenced by a rule: in use.
	resp, _ = s.call(t, http.MethodGet, "/api/v1/services/"+s.service.ID.String(), "", nil)
	resp, body = s.call(t, http.MethodDelete, "/api/v1/services/"+s.service.ID.String(), "", ifMatch(resp.header.Get("ETag")))
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemInUse)

	resp, body = s.call(t, http.MethodPost, "/api/v1/address-groups", jsonBody(map[string]any{"name": "office", "cidrs": []string{"192.0.2.0/24", "192.0.2.5/24"}}), nil)
	if resp.status != http.StatusCreated || len(field(body, "cidrs").([]any)) != 1 || field(body, "cidrs.0") != "192.0.2.0/24" {
		t.Fatalf("group create: %d %v", resp.status, body)
	}
	groupID, groupTag := body["id"].(string), resp.header.Get("ETag")
	resp, body = s.call(t, http.MethodPost, "/api/v1/address-groups", jsonBody(map[string]any{"name": "office", "cidrs": []string{"10.0.0.0/8"}}), nil)
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemDuplicateName)
	resp, body = s.call(t, http.MethodPost, "/api/v1/address-groups", jsonBody(map[string]any{"name": "bad", "cidrs": []string{"nope"}}), nil)
	expectFindings(t, resp, body, "cidrs[0]")
	// A rule may name the group by id or name; then the group is in use.
	resp, body = s.call(t, http.MethodPost, "/api/v1/rulesets/"+s.ruleset.ID.String()+"/rules", jsonBody(map[string]any{"direction": "inbound", "peers": []map[string]any{{"address_group": "office"}}, "services": []string{s.service.ID.String()}}), nil)
	if resp.status != http.StatusCreated || field(body, "peers.0.address_group") != groupID {
		t.Fatalf("rule with group: %d %v", resp.status, body)
	}
	resp, body = s.call(t, http.MethodDelete, "/api/v1/address-groups/"+groupID, "", ifMatch(groupTag))
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemInUse)
	resp, body = s.call(t, http.MethodGet, "/api/v1/address-groups", "", nil)
	if resp.status != http.StatusOK || field(body, "address_groups.0.name") != "office" {
		t.Fatalf("groups: %v", body)
	}
}

func TestWorkloadLabelEndpoints(t *testing.T) {
	s := newWriteSurface(t)
	path := "/api/v1/workloads/" + s.web.String() + "/labels"
	resp, body := s.call(t, http.MethodGet, path, "", nil)
	if resp.status != http.StatusOK || field(body, "labels.role") != "web" || resp.header.Get("ETag") != `"`+body["version"].(string)+`"` {
		t.Fatalf("labels: %d %v %q", resp.status, body, resp.header.Get("ETag"))
	}
	tag := resp.header.Get("ETag")
	update := jsonBody(map[string]any{"labels": map[string]string{"role": "db", "env": "prod"}})
	resp, body = s.call(t, http.MethodPut, path, update, nil)
	expectProblem(t, resp, body, http.StatusPreconditionRequired, api.ProblemPreconditionRequired)
	resp, body = s.call(t, http.MethodPut, path, update, ifMatch(`"stale"`))
	expectProblem(t, resp, body, http.StatusPreconditionFailed, api.ProblemPreconditionFailed)
	if body["current_version"] != strings.Trim(tag, `"`) {
		t.Fatalf("412 current_version = %v", body["current_version"])
	}
	resp, body = s.call(t, http.MethodPut, path, jsonBody(map[string]any{"labels": map[string]string{"": "x"}}), ifMatch(tag))
	expectFindings(t, resp, body, "labels[0]")
	resp, body = s.call(t, http.MethodPut, path, update, ifMatch(tag))
	if resp.status != http.StatusOK || field(body, "labels.role") != "db" || resp.header.Get("ETag") == tag {
		t.Fatalf("labels update: %d %v", resp.status, body)
	}
	// The edit rendered: web-1 is now in the db scope.
	_, body = s.call(t, http.MethodGet, "/api/v1/workloads/"+s.web.String(), "", nil)
	if field(body, "labels.role") != "db" || field(body, "sync.latest_version") != float64(2) {
		t.Fatalf("web-1 after label edit = %v", body)
	}
	resp, body = s.call(t, http.MethodPut, "/api/v1/workloads/"+uuid.NewString()+"/labels", update, ifMatch(tag))
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
}

func TestModeChangeEndpoint(t *testing.T) {
	s := newWriteSurface(t)
	// Shape faults are validation findings; the count guard is a
	// conflict carrying both numbers.
	resp, body := s.call(t, http.MethodPost, "/api/v1/mode-changes", jsonBody(map[string]any{"selector": map[string][]string{"env": {"prod"}}, "target_mode": "enforced"}), nil)
	expectFindings(t, resp, body, "expected_match_count")
	resp, body = s.call(t, http.MethodPost, "/api/v1/mode-changes", jsonBody(map[string]any{"target_mode": "strict", "expected_match_count": 1}), nil)
	expectFindings(t, resp, body, "selector", "target_mode")
	resp, body = s.call(t, http.MethodPost, "/api/v1/mode-changes", jsonBody(map[string]any{"workload_ids": []string{"nope"}, "target_mode": "enforced", "expected_match_count": 1}), nil)
	expectFindings(t, resp, body, "workload_ids[0]")
	resp, body = s.call(t, http.MethodPost, "/api/v1/mode-changes", jsonBody(map[string]any{"workload_ids": []string{uuid.NewString()}, "target_mode": "enforced", "expected_match_count": 1}), nil)
	expectFindings(t, resp, body, "workload_ids[0]")
	resp, body = s.call(t, http.MethodPost, "/api/v1/mode-changes", jsonBody(map[string]any{"selector": map[string][]string{"env": {"prod"}}, "target_mode": "enforced", "expected_match_count": 2}), nil)
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemMatchCountMismatch)
	if body["expected"] != float64(2) || body["matched"] != float64(3) {
		t.Fatalf("409 numbers = %v %v", body["expected"], body["matched"])
	}
	if len(s.mem.ModeChanges) != 0 {
		t.Fatal("a refused change was recorded")
	}

	// The acknowledgment is the recorded intent and nothing about
	// convergence; convergence shows on the workload reads as latest
	// ahead of applied.
	resp, body = s.call(t, http.MethodPost, "/api/v1/mode-changes", jsonBody(map[string]any{"selector": map[string][]string{"env": {"prod"}}, "target_mode": "enforced", "expected_match_count": 3}), nil)
	if resp.status != http.StatusOK || body["mode_change_id"] == nil || body["matched"] != float64(3) || body["desired_updated"] != float64(2) {
		t.Fatalf("mode change: %d %v", resp.status, body)
	}
	for k := range body {
		if k != "mode_change_id" && k != "matched" && k != "desired_updated" {
			t.Fatalf("acknowledgment carries %q; it must not describe progress", k)
		}
	}
	if id, err := uuid.Parse(body["mode_change_id"].(string)); err != nil || len(s.mem.ModeChanges) != 1 || s.mem.ModeChanges[0].ID != id {
		t.Fatalf("mode change %v not recorded", body["mode_change_id"])
	}
	resp, body = s.call(t, http.MethodGet, "/api/v1/workloads?mode=enforced", "", nil)
	if resp.status != http.StatusOK || len(field(body, "workloads").([]any)) != 3 {
		t.Fatalf("workloads after change = %v", body)
	}
	// web-1 was already enforced, so nothing rendered for it; db-1 moved
	// from its version 2, cache-1 from its version 1.
	wantLatest := map[string]float64{"web-1": 1, "db-1": 3, "cache-1": 2}
	for _, w := range field(body, "workloads").([]any) {
		wm := w.(map[string]any)
		if field(wm, "sync.latest_version") != wantLatest[wm["hostname"].(string)] || field(wm, "sync.applied_version") != float64(1) {
			t.Fatalf("%s sync = %v", wm["hostname"], wm["sync"])
		}
	}
}

func TestPreviewAndDryRunEndpoints(t *testing.T) {
	s := newWriteSurface(t)
	resp, body := s.call(t, http.MethodPost, "/api/v1/selectors/preview", jsonBody(map[string]any{"selector": map[string][]string{"role": {"db", "cache"}}}), nil)
	if resp.status != http.StatusOK || body["count"] != float64(2) || len(field(body, "matched").([]any)) != 2 || field(body, "matched.0.hostname") == nil || field(body, "matched.0.labels.env") != "prod" {
		t.Fatalf("preview: %d %v", resp.status, body)
	}
	resp, body = s.call(t, http.MethodPost, "/api/v1/selectors/preview", jsonBody(map[string]any{"selector": map[string][]string{}}), nil)
	expectFindings(t, resp, body, "selector")
	resp, body = s.call(t, http.MethodPost, "/api/v1/selectors/preview", jsonBody(map[string]any{"selector": map[string][]string{"role": {}}}), nil)
	expectFindings(t, resp, body, "selector[role]")

	_, body = s.call(t, http.MethodGet, "/api/v1/rulesets", "", nil)
	stateVersion := body["state_version"].(string)
	current := field(body, "rulesets.0").(map[string]any)
	current["scope"] = map[string][]string{"role": {"db", "cache"}}
	hypothetical := []any{current, map[string]any{"name": "office-to-web", "scope": map[string][]string{"role": {"web"}}, "rules": []map[string]any{{"direction": "inbound", "peers": []map[string]any{{"cidr": "192.0.2.0/24"}}, "entries": []map[string]any{{"protocol": "tcp", "ports": []string{"443"}}}}}}}
	writes := s.mem.Writes
	resp, body = s.call(t, http.MethodPost, "/api/v1/policies/render-dryrun", jsonBody(map[string]any{"state_version": stateVersion, "rulesets": hypothetical}), nil)
	if resp.status != http.StatusOK || body["state_version"] != stateVersion || body["stale"] != false || len(field(body, "workloads").([]any)) != 2 {
		t.Fatalf("dry run: %d %v", resp.status, body)
	}
	byHost := map[string]map[string]any{}
	for _, w := range field(body, "workloads").([]any) {
		wm := w.(map[string]any)
		byHost[field(wm, "workload.hostname").(string)] = wm
	}
	if d := byHost["cache-1"]; d == nil || d["version"] != float64(1) || len(d["added"].([]any)) != 1 || field(d, "added.0.peer_cidrs.0") != "10.0.0.10/32" || field(d, "added.0.protocol") != "tcp" || d["mode"] != nil {
		t.Fatalf("cache-1 diff = %v", d)
	}
	if d := byHost["web-1"]; d == nil || field(d, "added.0.ports.0.start") != float64(443) || len(d["removed"].([]any)) != 0 || len(d["changed"].([]any)) != 0 {
		t.Fatalf("web-1 diff = %v", d)
	}
	if s.mem.Writes != writes || len(s.mem.Rulesets) != 1 {
		t.Fatal("the dry run wrote something")
	}
	resp, body = s.call(t, http.MethodPost, "/api/v1/policies/render-dryrun", jsonBody(map[string]any{"state_version": "older", "rulesets": hypothetical}), nil)
	if resp.status != http.StatusOK || body["stale"] != true {
		t.Fatalf("stale dry run: %d %v", resp.status, body)
	}
	// Admission on the hypothetical set, with document faults and
	// admission faults both under rulesets[i].
	broken := []any{current, map[string]any{"name": "x", "scope": map[string][]string{"role": {"web"}}, "rules": []map[string]any{{"direction": "sideways", "peers": []map[string]any{{"cidr": "nope"}}, "services": []string{"missing"}}}}}
	resp, body = s.call(t, http.MethodPost, "/api/v1/policies/render-dryrun", jsonBody(map[string]any{"rulesets": broken}), nil)
	expectFindings(t, resp, body, "rulesets[1].rules[0].direction", "rulesets[1].rules[0].services[0]")
	broken[1].(map[string]any)["rules"].([]map[string]any)[0]["direction"] = "inbound"
	broken[1].(map[string]any)["rules"].([]map[string]any)[0]["services"] = []string{"postgres"}
	resp, body = s.call(t, http.MethodPost, "/api/v1/policies/render-dryrun", jsonBody(map[string]any{"rulesets": broken}), nil)
	expectFindings(t, resp, body, "rulesets[1].rules[0].peers[0].cidr")
}

func TestTokenEndpoints(t *testing.T) {
	s := newWriteSurface(t)
	// Provisioning tokens: the secret is in the mint response and nowhere
	// else.
	resp, body := s.call(t, http.MethodPost, "/api/v1/provisioning-tokens", jsonBody(map[string]any{"name": "prod", "labels": map[string]string{"env": "prod"}, "ttl_seconds": 3600}), nil)
	if resp.status != http.StatusCreated || !strings.HasPrefix(body["token"].(string), enroll.TokenPrefix) || body["id"] == nil || body["state"] != "valid" || field(body, "labels.env") != "prod" || body["expires_at"] != "2026-09-13T13:00:00Z" || resp.header.Get("Location") != "/api/v1/provisioning-tokens/"+body["id"].(string) {
		t.Fatalf("mint: %d %v", resp.status, body)
	}
	secret, tokenID := body["token"].(string), body["id"].(string)
	if s.enrollStore.Token(uuid.MustParse(tokenID)) == nil {
		t.Fatal("token not persisted")
	}
	resp, body = s.call(t, http.MethodPost, "/api/v1/provisioning-tokens", jsonBody(map[string]any{"name": "bad", "labels": map[string]string{"": "x"}}), nil)
	expectFindings(t, resp, body, "labels")
	resp, body = s.call(t, http.MethodGet, "/api/v1/provisioning-tokens", "", nil)
	if resp.status != http.StatusOK || len(field(body, "tokens").([]any)) != 1 || field(body, "tokens.0.id") != tokenID || field(body, "tokens.0.use_count") != float64(0) {
		t.Fatalf("list: %d %v", resp.status, body)
	}
	if p := field(body, "tokens.0.prefix"); p != enroll.ListingHint(secret) || p == secret {
		t.Fatalf("listed prefix = %v, want the listing hint of the minted secret", p)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), `"token"`) {
		t.Fatalf("the list leaks the secret: %s", raw)
	}
	resp, _ = s.call(t, http.MethodDelete, "/api/v1/provisioning-tokens/"+tokenID, "", nil)
	if resp.status != http.StatusNoContent {
		t.Fatalf("revoke: %d", resp.status)
	}
	resp, body = s.call(t, http.MethodDelete, "/api/v1/provisioning-tokens/"+tokenID, "", nil)
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemAlreadyRevoked)
	resp, body = s.call(t, http.MethodDelete, "/api/v1/provisioning-tokens/"+uuid.NewString(), "", nil)
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	_, body = s.call(t, http.MethodGet, "/api/v1/provisioning-tokens", "", nil)
	if field(body, "tokens.0.state") != "revoked" || field(body, "tokens.0.revoked_at") == nil {
		t.Fatalf("revoked token = %v", field(body, "tokens.0"))
	}
	// A token minted before hints were kept lists a null prefix; nothing
	// is made up for it.
	_, oldHash, _ := enroll.NewToken()
	old := enroll.Token{ID: uuid.New(), Hash: oldHash, Name: "old", CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)}
	if err := s.enrollStore.CreateToken(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	_, body = s.call(t, http.MethodGet, "/api/v1/provisioning-tokens", "", nil)
	var sawOld bool
	for _, tok := range field(body, "tokens").([]any) {
		m := tok.(map[string]any)
		if m["id"] != old.ID.String() {
			continue
		}
		sawOld = true
		if v, ok := m["prefix"]; !ok || v != nil {
			t.Fatalf("a token without a hint lists prefix %v (present %v)", v, ok)
		}
	}
	if !sawOld {
		t.Fatalf("pre-hint token not listed: %v", body)
	}

	// Operator tokens: symmetric, with a listing prefix, and the minted
	// secret authenticates.
	resp, body = s.call(t, http.MethodPost, "/api/v1/operator-tokens", jsonBody(map[string]any{"name": "ci"}), nil)
	if resp.status != http.StatusCreated || !strings.HasPrefix(body["token"].(string), "iwo_") || body["expires_at"] != nil || body["prefix"] == nil || !strings.HasPrefix(body["token"].(string), body["prefix"].(string)) {
		t.Fatalf("mint operator token: %d %v", resp.status, body)
	}
	minted, mintedID := body["token"].(string), body["id"].(string)
	resp, body = s.do(t, s.client(false), request{method: http.MethodGet, path: "/api/v1/me", headers: map[string]string{"Authorization": "Bearer " + minted}})
	if resp.status != http.StatusOK {
		t.Fatalf("minted token refused: %d %v", resp.status, body)
	}
	resp, body = s.call(t, http.MethodGet, "/api/v1/operator-tokens", "", nil)
	raw, _ = json.Marshal(body)
	if resp.status != http.StatusOK || len(field(body, "tokens").([]any)) != 2 || strings.Contains(string(raw), minted) || strings.Contains(string(raw), `"token"`) {
		t.Fatalf("operator token list: %d %s", resp.status, raw)
	}
	found := false
	for _, tk := range field(body, "tokens").([]any) {
		tm := tk.(map[string]any)
		if tm["id"] == mintedID && tm["prefix"] == minted[:12] && tm["last_used_at"] != nil && tm["state"] == "valid" {
			found = true
		}
	}
	if !found {
		t.Fatalf("minted token not listed with its prefix and use: %s", raw)
	}
	resp, _ = s.call(t, http.MethodDelete, "/api/v1/operator-tokens/"+mintedID, "", nil)
	if resp.status != http.StatusNoContent {
		t.Fatalf("revoke operator token: %d", resp.status)
	}
	resp, body = s.do(t, s.client(false), request{method: http.MethodGet, path: "/api/v1/me", headers: map[string]string{"Authorization": "Bearer " + minted}})
	expectProblem(t, resp, body, http.StatusUnauthorized, api.ProblemUnauthenticated)
	resp, body = s.call(t, http.MethodDelete, "/api/v1/operator-tokens/"+mintedID, "", nil)
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemAlreadyRevoked)
}
