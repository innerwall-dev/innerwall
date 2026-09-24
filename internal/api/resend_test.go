package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/api"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
)

// TestResendSnapshot covers the directed reconnect endpoint: a workload
// whose agent is not offline has the directive fired and is answered with
// its snapshot instant as it stood (null when none is recorded), which a
// workload read also carries; an unknown workload is not found; an
// offline agent is a problem naming its last-seen instant, null when it
// was never seen. Refusals fire nothing.
func TestResendSnapshot(t *testing.T) {
	s := newWriteSurface(t)
	sent := time.Date(2026, 9, 24, 11, 58, 0, 123456000, time.UTC)
	s.mem.Workloads[0].LastSnapshotSentAt = &sent
	path := func(id string) string { return "/api/v1/workloads/" + id + "/resend-snapshot" }

	resp, body := s.call(t, http.MethodPost, path(s.web.String()), "", nil)
	if resp.status != http.StatusOK || body["last_snapshot_sent_at"] != "2026-09-24T11:58:00.123456Z" {
		t.Fatalf("resend web: %d %v", resp.status, body)
	}
	resp, body = s.call(t, http.MethodPost, path(s.db.String()), "", nil)
	if v, present := body["last_snapshot_sent_at"]; resp.status != http.StatusOK || !present || v != nil {
		t.Fatalf("resend db: %d %v", resp.status, body)
	}
	if got := s.directives.Reconnects(); len(got) != 2 || got[0] != s.web || got[1] != s.db {
		t.Fatalf("directed = %v", got)
	}

	// The workload read carries the instant the console watches advance.
	_, body = s.call(t, http.MethodGet, "/api/v1/workloads/"+s.web.String(), "", nil)
	if sync, _ := body["sync"].(map[string]any); sync["last_snapshot_sent_at"] != "2026-09-24T11:58:00.123456Z" {
		t.Fatalf("workload sync = %v", body["sync"])
	}

	unknown, _ := identity.NewWorkloadID()
	resp, body = s.call(t, http.MethodPost, path(unknown.String()), "", nil)
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)
	resp, body = s.call(t, http.MethodPost, path("not-an-id"), "", nil)
	expectProblem(t, resp, body, http.StatusNotFound, api.ProblemNotFound)

	// Offline, last seen a minute before the fixture's now.
	s.mem.Workloads[2].SyncState = innerwallv1.SyncState_SYNC_STATE_OFFLINE
	resp, body = s.call(t, http.MethodPost, path(s.cache.String()), "", nil)
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemAgentOffline)
	if body["last_seen_at"] != "2026-09-13T11:59:00Z" {
		t.Fatalf("offline problem = %v", body)
	}
	// Offline and never seen: the member is present and null.
	s.mem.Workloads[2].LastSeenAt = nil
	resp, body = s.call(t, http.MethodPost, path(s.cache.String()), "", nil)
	expectProblem(t, resp, body, http.StatusConflict, api.ProblemAgentOffline)
	if v, present := body["last_seen_at"]; !present || v != nil {
		t.Fatalf("never-seen problem = %v", body)
	}
	if got := s.directives.Reconnects(); len(got) != 2 {
		t.Fatalf("refusals fired directives: %v", got)
	}

	// Only POST is served; the credential requirement is in the write
	// surface's credential matrix.
	resp, body = s.call(t, http.MethodGet, path(s.web.String()), "", nil)
	expectProblem(t, resp, body, http.StatusMethodNotAllowed, api.ProblemMethodNotAllowed)
	if got := s.directives.Reconnects(); len(got) != 2 {
		t.Fatalf("a refused request fired a directive: %v", got)
	}
}
