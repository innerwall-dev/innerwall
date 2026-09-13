package api

import (
	"net/http"

	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
)

// The read endpoints (M3.2). Each handler reads its parameters, calls one
// read model function, and encodes the result; every aggregation,
// resolution, and filter is the read model's and the store's (ADR-0007 as
// amended, ADR-0021). Reads are bounded: a rollup returns at most its
// group limit and says when it was truncated, and a list paginates by an
// opaque cursor.

// getFlowsRollup is GET /api/v1/flows/rollup: a grouped rollup over the
// windows in a range. group_by is required and must be one of the named
// groupings; from and to default to the last day; verdict, direction,
// workload, label (repeatable), service, order, and limit filter, order,
// and bound it.
func (s *Server) getFlowsRollup(w http.ResponseWriter, r *http.Request) {
	q := queryOf(r)
	var req readmodel.RollupRequest
	var err error
	if req.GroupBy, err = q.groupBy("group_by"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.From, err = q.timestamp("from"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.To, err = q.timestamp("to"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Verdict, err = q.verdict("verdict"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Direction, err = q.direction("direction"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Workload, err = q.workloadID("workload"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Selector, err = q.selector("label"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Service, err = q.service("service"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Order, err = q.order("order"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Limit, err = q.limit("limit"); err != nil {
		s.readProblem(w, err)
		return
	}
	res, err := s.reads.Rollup(r.Context(), req)
	if err != nil {
		s.readProblem(w, err)
		return
	}
	writeJSON(w, rollupJSON(res))
}

// getFlows is GET /api/v1/flows: one page of a workload's stored windows.
// workload is required; from and to default to the last day; verdict,
// direction, peer, and service filter; cursor and limit page.
func (s *Server) getFlows(w http.ResponseWriter, r *http.Request) {
	q := queryOf(r)
	if !q.has("workload") {
		writeProblem(w, invalidParameter("workload", "required; this endpoint has no unbounded form"))
		return
	}
	var req readmodel.FlowsRequest
	var err error
	id, err := q.workloadID("workload")
	if err != nil {
		s.readProblem(w, err)
		return
	}
	req.Workload = *id
	if req.From, err = q.timestamp("from"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.To, err = q.timestamp("to"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Verdict, err = q.verdict("verdict"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Direction, err = q.direction("direction"); err != nil {
		s.readProblem(w, err)
		return
	}
	req.Peer = q.text("peer")
	if req.Service, err = q.service("service"); err != nil {
		s.readProblem(w, err)
		return
	}
	req.Cursor = q.text("cursor")
	if req.Limit, err = q.limit("limit"); err != nil {
		s.readProblem(w, err)
		return
	}
	page, err := s.reads.ListFlows(r.Context(), req)
	if err != nil {
		s.readProblem(w, err)
		return
	}
	writeJSON(w, flowsPageJSON(page))
}

// getWorkloads is GET /api/v1/workloads: one page of the fleet in fleet
// order. label (repeatable), mode, and sync_state filter; cursor and
// limit page.
func (s *Server) getWorkloads(w http.ResponseWriter, r *http.Request) {
	q := queryOf(r)
	var req readmodel.WorkloadsRequest
	var err error
	if req.Selector, err = q.selector("label"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.Mode, err = q.mode("mode"); err != nil {
		s.readProblem(w, err)
		return
	}
	if req.SyncState, err = q.syncState("sync_state"); err != nil {
		s.readProblem(w, err)
		return
	}
	req.Cursor = q.text("cursor")
	if req.Limit, err = q.limit("limit"); err != nil {
		s.readProblem(w, err)
		return
	}
	page, err := s.reads.ListWorkloads(r.Context(), req)
	if err != nil {
		s.readProblem(w, err)
		return
	}
	writeJSON(w, workloadsPageJSON(page))
}

// pathWorkloadID reads the {id} segment. A malformed id is not found:
// no workload has it.
func pathWorkloadID(w http.ResponseWriter, r *http.Request) (identity.WorkloadID, bool) {
	id, err := identity.ParseWorkloadID(pathParam(r, "id"))
	if err != nil {
		writeProblem(w, problemNotFound)
		return identity.WorkloadID{}, false
	}
	return id, true
}

// getWorkload is GET /api/v1/workloads/{id}: one workload, in the same
// shape the list uses.
func (s *Server) getWorkload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathWorkloadID(w, r)
	if !ok {
		return
	}
	wl, err := s.reads.GetWorkload(r.Context(), id)
	if err != nil {
		s.readProblem(w, err)
		return
	}
	writeJSON(w, workloadJSON(wl))
}

// getRenderedPolicy is GET /api/v1/workloads/{id}/rendered-policy: the
// persisted rendered policy of one workload. Configuration only.
func (s *Server) getRenderedPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathWorkloadID(w, r)
	if !ok {
		return
	}
	p, err := s.reads.RenderedPolicy(r.Context(), id)
	if err != nil {
		s.readProblem(w, err)
		return
	}
	writeJSON(w, renderedPolicyJSON(p))
}
