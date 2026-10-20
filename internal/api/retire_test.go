package api_test

import (
	"net/http"
	"testing"

	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// TestRetireEndpoint covers POST /workloads/{id}/retire (ADR-0023): the
// retirement answers with its instant and how many versions its render
// advanced, and directs the workload's stream ended; the workload then
// reads as retired, leaves the active fleet list and the attention counts
// for a retired list and count of its own, matches no selector, and every
// write naming it, a second retirement included, is a conflict naming the
// instant. An unknown workload is not found.
func TestRetireEndpoint(t *testing.T) {
	s := newWriteSurface(t)
	path := func(id string) string { return "/api/v1/workloads/" + id + "/retire" }

	resp, body := s.call(t, http.MethodPost, path(s.web.String()), "", nil)
	if resp.status != http.StatusOK || body["workload_id"] != s.web.String() || body["versions_advanced"] != float64(1) {
		t.Fatalf("retire web: %d %v", resp.status, body)
	}
	if v, present := body["silence_closed"]; !present || v != nil {
		t.Fatalf("retire web: silence_closed = %v, want present and null", body)
	}
	retiredAt, _ := body["retired_at"].(string)
	if w := s.mem.Workload(s.web); w == nil || w.RetiredAt == nil || retiredAt == "" {
		t.Fatalf("stored workload = %+v, answered %q", w, retiredAt)
	}
	if ended := s.mem.Ended; len(ended) != 1 || ended[0] != s.web {
		t.Fatalf("stream ends directed = %v", ended)
	}

	// The workload reads as retired.
	_, body = s.call(t, http.MethodGet, "/api/v1/workloads/"+s.web.String(), "", nil)
	if body["lifecycle"] != "retired" || body["retired_at"] != retiredAt {
		t.Fatalf("workload read = lifecycle %v retired_at %v", body["lifecycle"], body["retired_at"])
	}
	_, body = s.call(t, http.MethodGet, "/api/v1/workloads/"+s.db.String(), "", nil)
	if v, present := body["retired_at"]; body["lifecycle"] != "active" || !present || v != nil {
		t.Fatalf("active workload read = lifecycle %v retired_at %v", body["lifecycle"], v)
	}
	listed := func(query string) []string {
		t.Helper()
		resp, body := s.call(t, http.MethodGet, "/api/v1/workloads"+query, "", nil)
		if resp.status != http.StatusOK {
			t.Fatalf("list %q: %d %v", query, resp.status, body)
		}
		var ids []string
		for _, w := range body["workloads"].([]any) {
			ids = append(ids, w.(map[string]any)["id"].(string))
		}
		return ids
	}
	for _, id := range listed("") {
		if id == s.web.String() {
			t.Fatal("the retired workload is in the active fleet list")
		}
	}
	if got := listed("?lifecycle=active"); len(got) != 2 {
		t.Fatalf("active list = %v", got)
	}
	if got := listed("?lifecycle=retired"); len(got) != 1 || got[0] != s.web.String() {
		t.Fatalf("retired list = %v", got)
	}
	resp, body = s.call(t, http.MethodGet, "/api/v1/workloads?lifecycle=gone", "", nil)
	expectProblem(t, resp, body, http.StatusBadRequest, api.ProblemInvalidParameter)
	_, body = s.call(t, http.MethodGet, "/api/v1/workloads/counts", "", nil)
	if body["retired"] != float64(1) || body["total"] != float64(2) {
		t.Fatalf("counts = %v, want two active and one retired", body)
	}

	// No selector resolves to it.
	_, body = s.call(t, http.MethodPost, "/api/v1/selectors/preview", `{"selector":{"role":["web"]}}`, nil)
	if m, _ := body["matched"].([]any); len(m) != 0 {
		t.Fatalf("preview = %v", body)
	}

	// Every write naming it is a conflict naming the instant.
	wantRetired := func(what string, resp *reply, body map[string]any) {
		t.Helper()
		expectProblem(t, resp, body, http.StatusConflict, api.ProblemWorkloadRetired)
		if body["retired_at"] != retiredAt {
			t.Fatalf("%s: problem %v, want retired_at %s", what, body, retiredAt)
		}
	}
	resp, body = s.call(t, http.MethodPost, path(s.web.String()), "", nil)
	wantRetired("a second retirement", resp, body)
	labels, _ := s.call(t, http.MethodGet, "/api/v1/workloads/"+s.web.String()+"/labels", "", nil)
	resp, body = s.call(t, http.MethodPut, "/api/v1/workloads/"+s.web.String()+"/labels", `{"labels":{"role":"web"}}`, ifMatch(labels.header.Get("ETag")))
	wantRetired("a label edit", resp, body)
	resp, body = s.call(t, http.MethodPost, "/api/v1/workloads/"+s.web.String()+"/resend-snapshot", "", nil)
	wantRetired("a directed reconnect", resp, body)
	resp, body = s.call(t, http.MethodPost, "/api/v1/mode-changes", `{"workload_ids":["`+s.web.String()+`"],"target_mode":"enforced","expected_match_count":1}`, nil)
	wantRetired("a mode change", resp, body)

	unknown, _ := identity.NewWorkloadID()
	resp, body = s.call(t, http.MethodPost, path(unknown.String()), "", nil)
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	resp, body = s.call(t, http.MethodGet, path(s.db.String()), "", nil)
	expectProblem(t, resp, body, http.StatusMethodNotAllowed, api.ProblemMethodNotAllowed)
}
