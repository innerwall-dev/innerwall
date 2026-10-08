package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/innerwall-dev/innerwall/internal/agent/credential"
	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/ca/fileca"
	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/ingest"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

type renderer struct{ engine *compiler.Engine }

func (r renderer) Render(ctx context.Context) error {
	_, err := r.engine.Render(ctx)
	return err
}

// TestOperatorWritesEndToEnd drives the whole write path against seeded
// storage, through the surface only: author a ruleset, preview the
// selector a mode change will use, dry-run the change to the policy set,
// submit the mode change, and observe convergence on the workload reads
// as the agents would report it. It skips without a database.
func TestOperatorWritesEndToEnd(t *testing.T) {
	st := storetest.Open(t)
	f := storetest.SeedFleet(t, st)
	ctx := context.Background()
	clock := f.Now
	tick := func() time.Time { clock = clock.Add(time.Second); return clock }
	engine := &compiler.Engine{Store: st, Now: tick}
	authoring := &policy.Authoring{Store: st, Renderer: renderer{engine}, Now: tick}
	fleetSvc := &fleet.Service{Store: st, Engine: engine, Now: tick}
	enrollSvc := &enroll.Service{Store: st, Now: tick}
	reads := &readmodel.Reader{Store: st, Flows: st.Flows(), Now: tick}
	s := newSurface(t, api.Deps{Reads: reads, Authoring: authoring, Fleet: fleetSvc, Enroll: enrollSvc})
	// The surface's own operator store is in memory; the domains behind
	// the writes are the Postgres store.
	s.setPassword(t, "Ada")
	token, _, err := s.operators.MintToken(ctx, "e2e", 0)
	if err != nil {
		t.Fatal(err)
	}
	ws := &writeSurface{surface: s, bearer: map[string]string{"Authorization": "Bearer " + token}}

	// 1. Author: the office may reach every prod workload on tcp/22.
	_, body := ws.call(t, http.MethodGet, "/api/v1/rulesets", "", nil)
	stateVersion := body["state_version"].(string)
	doc := map[string]any{"name": "office-ssh", "scope": map[string][]string{"env": {"prod"}}, "rules": []map[string]any{{"direction": "inbound", "description": "ssh from the office", "peers": []map[string]any{{"address_group": "office"}}, "entries": []map[string]any{{"protocol": "tcp", "ports": []string{"22"}}}}}}
	// Dry-run it first: every prod workload gains one rule, nothing is
	// persisted, and the state version is the one the editor read.
	existing := field(body, "rulesets").([]any)
	hypothetical := append(append([]any{}, existing...), doc)
	resp, body := ws.call(t, http.MethodPost, "/api/v1/policies/render-dryrun", jsonBody(map[string]any{"state_version": stateVersion, "rulesets": hypothetical}), nil)
	if resp.status != http.StatusOK || body["stale"] != false || len(field(body, "workloads").([]any)) != 3 {
		t.Fatalf("dry run: %d %v", resp.status, body)
	}
	for _, w := range field(body, "workloads").([]any) {
		wm := w.(map[string]any)
		if len(wm["added"].([]any)) != 1 || field(wm, "added.0.peer_cidrs.0") != "192.0.2.0/24" || field(wm, "added.0.ports.0.start") != float64(22) {
			t.Fatalf("dry run diff = %v", wm)
		}
	}
	_, body = ws.call(t, http.MethodGet, "/api/v1/rulesets", "", nil)
	if len(field(body, "rulesets").([]any)) != 1 || body["state_version"] != stateVersion {
		t.Fatal("the dry run persisted something")
	}
	resp, body = ws.call(t, http.MethodPost, "/api/v1/rulesets", jsonBody(doc), nil)
	if resp.status != http.StatusCreated {
		t.Fatalf("create: %d %v", resp.status, body)
	}
	if field(body, "rules.0.peers.0.address_group") != f.Office.ID.String() || field(body, "rules.0.created_at") == nil {
		t.Fatalf("created ruleset = %v", body)
	}
	rulesetID := body["id"].(string)

	// The authored change rendered: every workload's latest version
	// moved ahead of what its agent applied (web-1 was seeded synced at
	// its version 2).
	_, body = ws.call(t, http.MethodGet, "/api/v1/workloads/"+f.Web.String(), "", nil)
	if field(body, "sync.latest_version") != float64(3) || field(body, "sync.applied_version") != float64(2) || field(body, "sync.state") != "synced" {
		t.Fatalf("web-1 after authoring = %v", body["sync"])
	}
	// The rendered policy read shows the rule with its own instants.
	_, body = ws.call(t, http.MethodGet, "/api/v1/workloads/"+f.Web.String()+"/rendered-policy", "", nil)
	rules := field(body, "rules").([]any)
	if len(rules) != 1 || field(rules[0], "ruleset.id") != rulesetID || field(rules[0], "created_at") == nil || field(rules[0], "updated_at") == nil {
		t.Fatalf("web-1 rendered policy = %v", body)
	}

	// 2. Preview the selection a mode change will use.
	resp, body = ws.call(t, http.MethodPost, "/api/v1/selectors/preview", jsonBody(map[string]any{"selector": map[string][]string{"env": {"prod"}, "role": {"db", "cache"}}}), nil)
	if resp.status != http.StatusOK || body["count"] != float64(2) {
		t.Fatalf("preview: %d %v", resp.status, body)
	}
	count := int(body["count"].(float64))

	// 3. Change the mode of exactly that set; a wrong expectation is
	// refused with both numbers.
	resp, body = ws.call(t, http.MethodPost, "/api/v1/mode-changes", jsonBody(map[string]any{"selector": map[string][]string{"env": {"prod"}, "role": {"db", "cache"}}, "target_mode": "enforced", "expected_match_count": count + 1}), nil)
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemMatchCountMismatch)
	resp, body = ws.call(t, http.MethodPost, "/api/v1/mode-changes", jsonBody(map[string]any{"selector": map[string][]string{"env": {"prod"}, "role": {"db", "cache"}}, "target_mode": "enforced", "expected_match_count": count}), nil)
	if resp.status != http.StatusOK || body["matched"] != float64(2) || body["desired_updated"] != float64(2) {
		t.Fatalf("mode change: %d %v", resp.status, body)
	}

	// 4. Convergence is observed on the reads, never on the change: the
	// desired mode is enforced and latest is ahead of applied until the
	// agents acknowledge, as the gateway records it.
	_, body = ws.call(t, http.MethodGet, "/api/v1/workloads/"+f.DB.String(), "", nil)
	if body["mode"] != "enforced" || field(body, "sync.latest_version") != float64(f.DBVersion+2) || field(body, "sync.applied_version") != float64(0) {
		t.Fatalf("db-1 after mode change = mode %v sync %v", body["mode"], body["sync"])
	}
	_, body = ws.call(t, http.MethodGet, "/api/v1/workloads/"+f.Web.String(), "", nil)
	if body["mode"] != "enforced" || field(body, "sync.latest_version") != float64(3) {
		t.Fatalf("web-1 was in the change: %v", body)
	}
	latest := f.DBVersion + 2
	if err := st.RecordApplied(ctx, f.DB, latest, innerwallv1.SyncState_SYNC_STATE_SYNCED, tick()); err != nil {
		t.Fatal(err)
	}
	_, body = ws.call(t, http.MethodGet, "/api/v1/workloads?sync_state=synced&mode=enforced", "", nil)
	hosts := map[string]bool{}
	for _, w := range field(body, "workloads").([]any) {
		wm := w.(map[string]any)
		hosts[wm["hostname"].(string)] = field(wm, "sync.applied_version") == field(wm, "sync.latest_version")
	}
	if !hosts["db-1"] {
		t.Fatalf("db-1 did not converge on the read: %v", hosts)
	}
	// 5. The editor's next read carries a new state version, and a rule
	// edit through the rule endpoint is conditioned on the rule's own
	// version.
	_, body = ws.call(t, http.MethodGet, "/api/v1/rulesets", "", nil)
	if body["state_version"] == stateVersion {
		t.Fatal("state version did not move after authoring and a mode change")
	}
	_, body = ws.call(t, http.MethodGet, "/api/v1/rulesets/"+rulesetID, "", nil)
	rule := field(body, "rules.0").(map[string]any)
	resp, body = ws.call(t, http.MethodPut, "/api/v1/rulesets/"+rulesetID+"/rules/"+rule["id"].(string), jsonBody(map[string]any{"direction": "inbound", "description": "ssh from the office, reworded", "peers": rule["peers"], "entries": rule["entries"]}), ifMatch(`"`+rule["version"].(string)+`"`))
	if resp.status != http.StatusOK || body["description"] != "ssh from the office, reworded" || body["created_at"] != rule["created_at"] || body["version"] == rule["version"] {
		t.Fatalf("rule edit: %d %v", resp.status, body)
	}
	// A reworded description renders nothing: no version moved.
	_, body = ws.call(t, http.MethodGet, "/api/v1/workloads/"+f.Web.String(), "", nil)
	if field(body, "sync.latest_version") != float64(3) {
		t.Fatalf("a description edit advanced a rendered version: %v", body["sync"])
	}
}

// TestMintedTokenEnrollsSelectableWorkload closes the gap between seeded
// and enrolled labels: the seed writes clean labels straight into the
// store, so no test exercised the path an operator's labels really take.
// Here they go through the surface's mint, the enrollment function the
// agent gateway calls, and ingestion, and are then selected the ways the
// console selects them: the fleet filter, the selector preview, and the
// flow rollup's label scope (ADR-0022). It skips without a database.
func TestMintedTokenEnrollsSelectableWorkload(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	authority, err := fileca.Init(t.TempDir(), fileca.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	engine := &compiler.Engine{Store: st, Now: clock}
	enrollSvc := &enroll.Service{Store: st, Authority: authority, LeafTTL: time.Hour, Now: clock}
	reads := &readmodel.Reader{Store: st, Flows: st.Flows(), Now: clock}
	s := newSurface(t, api.Deps{
		Reads: reads, Enroll: enrollSvc,
		Fleet:     &fleet.Service{Store: st, Engine: engine, Reads: reads, Now: clock},
		Authoring: &policy.Authoring{Store: st, Renderer: renderer{engine}, Now: clock},
	})
	s.setPassword(t, "Ada")
	bearer, _, err := s.operators.MintToken(ctx, "e2e", 0)
	if err != nil {
		t.Fatal(err)
	}
	ws := &writeSurface{surface: s, bearer: map[string]string{"Authorization": "Bearer " + bearer}}
	csr := func() []byte {
		key, err := credential.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		pem, err := credential.NewCSR(key)
		if err != nil {
			t.Fatal(err)
		}
		return pem
	}

	// The body a form that split on the first "=" sent: refused, and
	// nothing is stored.
	resp, body := ws.call(t, http.MethodPost, "/api/v1/provisioning-tokens", jsonBody(map[string]any{"name": "lab", "labels": map[string]string{"app": "web env=lab"}}), nil)
	expectFindings(t, resp, body, "labels[0]")
	_, body = ws.call(t, http.MethodGet, "/api/v1/provisioning-tokens", "", nil)
	if len(field(body, "tokens").([]any)) != 0 {
		t.Fatalf("a refused mint stored a token: %v", body)
	}

	// The labels as two labels: minted, enrolled with, carried.
	resp, body = ws.call(t, http.MethodPost, "/api/v1/provisioning-tokens", jsonBody(map[string]any{"name": "lab", "labels": map[string]string{"app": "web", "env": "lab"}}), nil)
	if resp.status != http.StatusCreated {
		t.Fatalf("mint: %d %v", resp.status, body)
	}
	enrolled, err := enrollSvc.Enroll(ctx, body["token"].(string), csr(), "web-1")
	if err != nil {
		t.Fatal(err)
	}
	web := enrolled.Workload.ID

	_, body = ws.call(t, http.MethodGet, "/api/v1/workloads?label=app%3Dweb&label=env%3Dlab", "", nil)
	if ws := field(body, "workloads").([]any); len(ws) != 1 || field(ws[0], "id") != web.String() || field(ws[0], "labels.app") != "web" || field(ws[0], "labels.env") != "lab" {
		t.Fatalf("fleet filter = %v", body)
	}
	resp, body = ws.call(t, http.MethodPost, "/api/v1/selectors/preview", jsonBody(map[string]any{"selector": map[string][]string{"env": {"lab"}}}), nil)
	if resp.status != http.StatusOK || body["count"] != float64(1) {
		t.Fatalf("preview: %d %v", resp.status, body)
	}

	// Its agent reports a window; the label scope finds it.
	start := now.Add(-time.Hour)
	ing := &ingest.Service{Directory: st, Flows: st.Flows()}
	if _, err := ing.Ingest(ctx, web, &innerwallv1.ReportFlowsRequest{
		WindowStart: timestamppb.New(start), WindowEnd: timestamppb.New(start.Add(5 * time.Minute)),
		Records: []*innerwallv1.FlowRecord{{
			SrcAddress: "198.51.100.7", DstAddress: "10.0.0.10", DstPort: 443, Protocol: innerwallv1.Protocol_PROTOCOL_TCP,
			Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED, ConnectionCount: 4,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"app%3Dweb", "env%3Dlab"} {
		resp, body = ws.call(t, http.MethodGet, "/api/v1/flows/rollup?group_by=src,dst&label="+scope, "", nil)
		if resp.status != http.StatusOK || body["group_count"] != float64(1) || field(body, "groups.0.keys.dst.hostname") != "web-1" {
			t.Fatalf("rollup label=%s: %d %v", scope, resp.status, body)
		}
	}
	// What the corrupt label looked like, typed as a filter, is refused
	// rather than answered with an empty map.
	resp, body = ws.call(t, http.MethodGet, "/api/v1/flows/rollup?group_by=src,dst&label=app%3Dweb%20env%3Dlab", "", nil)
	expectInvalidParameter(t, resp, body, "label")

	// A token stored before the grammar enrolls nothing.
	plain, hash, err := enroll.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateToken(ctx, enroll.Token{ID: uuid.New(), Hash: hash, Name: "legacy", Labels: []enroll.Label{{Key: "app", Value: "web env=lab"}}, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := enrollSvc.Enroll(ctx, plain, csr(), "web-2"); !errors.Is(err, enroll.ErrTokenLabelsInvalid) {
		t.Fatalf("legacy token: err = %v", err)
	}
	_, body = ws.call(t, http.MethodGet, "/api/v1/workloads", "", nil)
	if len(field(body, "workloads").([]any)) != 1 {
		t.Fatalf("a refused enrollment stored a workload: %v", body)
	}
}
