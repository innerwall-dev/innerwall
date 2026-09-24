package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/operator"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// The write endpoints (M3.3). Each handler decodes its body, reads the
// version a conditional write names, calls one domain function (the same
// one the command line calls), and encodes the result; admission runs in
// the domain, so what the surface refuses is what the command line
// refuses (ADR-0007 as amended, ADR-0021).
//
// Conditional requests: every update and delete of an existing resource
// requires If-Match with the resource's version, which every read of it
// returns as its entity tag and in its body. A write without one is a
// 428 problem; one naming a version the resource no longer holds is a 412
// problem carrying the current version. A create needs none.

// ifMatch reads the version a conditional write names. It reports false,
// having written the 428 problem, when the header is absent or is the
// wildcard, which names no version. A weak tag is passed through as it
// is: it can never equal a version, so the write is refused with the
// current one, as the strong comparison If-Match requires.
func ifMatch(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" || raw == "*" {
		writeProblem(w, Problem{Type: ProblemPreconditionRequired, Title: "Precondition required", Status: http.StatusPreconditionRequired, Detail: "this write requires If-Match with the resource version a read returned"})
		return "", false
	}
	if strings.HasPrefix(raw, "W/") {
		return raw, true
	}
	return strings.Trim(raw, `"`), true
}

// versionTag is the entity tag of a resource version.
func versionTag(version string) string { return `"` + version + `"` }

// writeProblemFor maps a domain error to its problem document.
func (s *Server) writeProblemFor(w http.ResponseWriter, err error) {
	var vm *policy.VersionMismatchError
	var lv *registry.LabelsVersionError
	var mc *fleet.MatchCountError
	var off *fleet.AgentOfflineError
	switch {
	case policy.AsFindings(err) != nil:
		writeProblem(w, validationProblem(policy.AsFindings(err)))
	case errors.As(err, &vm):
		writeProblem(w, Problem{Type: ProblemPreconditionFailed, Title: "Precondition failed", Status: http.StatusPreconditionFailed, Detail: "the resource has changed since it was read; re-read it and retry with its current version", CurrentVersion: vm.Current})
	case errors.As(err, &lv):
		writeProblem(w, Problem{Type: ProblemPreconditionFailed, Title: "Precondition failed", Status: http.StatusPreconditionFailed, Detail: "the labels have changed since they were read; re-read them and retry with their current version", CurrentVersion: lv.Current})
	case errors.As(err, &mc):
		expected, matched := mc.Expected, mc.Matched
		writeProblem(w, Problem{Type: ProblemMatchCountMismatch, Title: "Match count mismatch", Status: http.StatusConflict, Detail: "the selection resolved to a different number of workloads than expected; preview it again", Expected: &expected, Matched: &matched})
	case errors.As(err, &off):
		writeProblem(w, Problem{Type: ProblemAgentOffline, Title: "Agent offline", Status: http.StatusConflict, Detail: "the workload's agent is offline as the control plane last recorded it, so no reconnect was directed; it receives a fresh snapshot whenever it next connects", LastSeenAt: &nullableInstant{At: optionalTimestamp(off.LastSeenAt)}})
	case errors.Is(err, policy.ErrRulesetUnknown), errors.Is(err, policy.ErrRuleUnknown), errors.Is(err, policy.ErrServiceUnknown), errors.Is(err, policy.ErrAddressGroupUnknown),
		errors.Is(err, registry.ErrWorkloadUnknown), errors.Is(err, enroll.ErrTokenUnknown), errors.Is(err, operator.ErrTokenUnknown):
		writeProblem(w, problemNotFound)
	case errors.Is(err, policy.ErrDuplicateName), errors.Is(err, policy.ErrDuplicateRuleID):
		writeProblem(w, Problem{Type: ProblemDuplicateName, Title: "Conflict", Status: http.StatusConflict, Detail: strings.TrimPrefix(err.Error(), "policy: ")})
	case errors.Is(err, policy.ErrInUse):
		writeProblem(w, Problem{Type: ProblemInUse, Title: "Conflict", Status: http.StatusConflict, Detail: "the object is referenced by a rule and cannot be deleted"})
	case errors.Is(err, enroll.ErrTokenRevoked), errors.Is(err, operator.ErrTokenRevoked):
		writeProblem(w, Problem{Type: ProblemAlreadyRevoked, Title: "Conflict", Status: http.StatusConflict, Detail: "the token is already revoked"})
	default:
		s.log.Error("write failed", "error", err)
		writeProblem(w, problemInternal)
	}
}

// validationProblem is the 400 problem of a refused body, with the
// domain's findings as the client renders them.
func validationProblem(f *policy.Findings) Problem {
	p := Problem{Type: ProblemValidation, Title: "Validation failed", Status: http.StatusBadRequest, Detail: "the request was refused by admission; see errors", Errors: make([]Finding, 0, len(f.Errors))}
	for _, e := range f.Errors {
		p.Errors = append(p.Errors, Finding{Path: e.Path, Rule: e.Rule(), Message: e.Message()})
	}
	return p
}

// finding is a decode-level fault at a path, in the same shape admission
// reports, for a body whose value cannot even be read into the domain's
// type (an id that is not an id, a name that is not a mode).
func finding(path, rule, message string) Problem {
	return Problem{Type: ProblemValidation, Title: "Validation failed", Status: http.StatusBadRequest, Detail: "the request was refused; see errors", Errors: []Finding{{Path: path, Rule: rule, Message: message}}}
}

// pathUUID reads a {name} segment as a UUID. A malformed id is not found:
// no object has it.
func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(pathParam(r, name))
	if err != nil {
		writeProblem(w, problemNotFound)
		return uuid.Nil, false
	}
	return id, true
}

// names is the name index the document form resolves against: the same
// index the command line builds before decoding a document.
func (s *Server) names(r *http.Request) (policy.Names, error) {
	services, err := s.authoring.Store.ListServices(r.Context())
	if err != nil {
		return policy.Names{}, err
	}
	groups, err := s.authoring.Store.ListAddressGroups(r.Context())
	if err != nil {
		return policy.Names{}, err
	}
	return policy.NewNames(services, groups), nil
}

// --- rulesets ----------------------------------------------------------------

// listRulesets is GET /api/v1/rulesets: every ruleset, and the version
// of the state a policy editor authors against.
func (s *Server) listRulesets(w http.ResponseWriter, r *http.Request) {
	rulesets, err := s.authoring.Store.ListRulesets(r.Context())
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	stateVersion, err := s.fleet.StateVersion(r.Context())
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	out := rulesetsResponse{Rulesets: make([]*policy.RulesetDoc, 0, len(rulesets)), StateVersion: stateVersion}
	for i := range rulesets {
		out.Rulesets = append(out.Rulesets, rulesetJSON(&rulesets[i]))
	}
	writeJSON(w, out)
}

// decodeRuleset reads a ruleset document, resolving names the way the
// command line does; a document fault is a validation problem.
func (s *Server) decodeRuleset(w http.ResponseWriter, r *http.Request) (*policy.Ruleset, bool) {
	var doc policy.RulesetDoc
	if !decodeJSON(w, r, &doc) {
		return nil, false
	}
	names, err := s.names(r)
	if err != nil {
		s.writeProblemFor(w, err)
		return nil, false
	}
	rs, err := policy.RulesetFromDoc(&doc, names)
	if err != nil {
		s.writeProblemFor(w, err)
		return nil, false
	}
	return rs, true
}

// createRuleset is POST /api/v1/rulesets.
func (s *Server) createRuleset(w http.ResponseWriter, r *http.Request) {
	rs, ok := s.decodeRuleset(w, r)
	if !ok {
		return
	}
	rs.ID = uuid.Nil
	if err := s.authoring.CreateRuleset(r.Context(), rs); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.respondRuleset(w, r, rs.ID, http.StatusCreated)
}

// respondRuleset answers with the persisted ruleset and its entity tag.
func (s *Server) respondRuleset(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	rs, err := s.authoring.Store.GetRuleset(r.Context(), id)
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	w.Header().Set("ETag", versionTag(policy.FormatVersion(rs.Version)))
	if status == http.StatusCreated {
		w.Header().Set("Location", APIPrefix+"/rulesets/"+rs.ID.String())
	}
	writeJSONStatus(w, status, rulesetJSON(rs))
}

// getRuleset is GET /api/v1/rulesets/{id}.
func (s *Server) getRuleset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	s.respondRuleset(w, r, id, http.StatusOK)
}

// putRuleset is PUT /api/v1/rulesets/{id}: replace the ruleset and all
// its rules, conditioned on its version.
func (s *Server) putRuleset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	rs, ok := s.decodeRuleset(w, r)
	if !ok {
		return
	}
	rs.ID = id
	if err := s.authoring.UpdateRuleset(r.Context(), rs, version); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.respondRuleset(w, r, id, http.StatusOK)
}

// deleteRuleset is DELETE /api/v1/rulesets/{id}, conditioned on its
// version.
func (s *Server) deleteRuleset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	if err := s.authoring.DeleteRuleset(r.Context(), id, version); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- rules -------------------------------------------------------------------

// decodeRule reads a rule document.
func (s *Server) decodeRule(w http.ResponseWriter, r *http.Request) (*policy.Rule, bool) {
	var doc policy.RuleDoc
	if !decodeJSON(w, r, &doc) {
		return nil, false
	}
	names, err := s.names(r)
	if err != nil {
		s.writeProblemFor(w, err)
		return nil, false
	}
	rule, err := policy.RuleFromDoc(&doc, names)
	if err != nil {
		s.writeProblemFor(w, err)
		return nil, false
	}
	return rule, true
}

// respondRule answers with one persisted rule and its entity tag.
func (s *Server) respondRule(w http.ResponseWriter, r *http.Request, rulesetID, ruleID uuid.UUID, status int) {
	rs, err := s.authoring.Store.GetRuleset(r.Context(), rulesetID)
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	for i := range rs.Rules {
		if rs.Rules[i].ID != ruleID {
			continue
		}
		w.Header().Set("ETag", versionTag(policy.FormatVersion(rs.Rules[i].Version)))
		if status == http.StatusCreated {
			w.Header().Set("Location", APIPrefix+"/rulesets/"+rs.ID.String()+"/rules/"+ruleID.String())
		}
		writeJSONStatus(w, status, authoredRuleJSON(&rs.Rules[i]))
		return
	}
	writeProblem(w, problemNotFound)
}

// createRule is POST /api/v1/rulesets/{id}/rules.
func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	rulesetID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	rule, ok := s.decodeRule(w, r)
	if !ok {
		return
	}
	rule.ID = uuid.Nil
	if err := s.authoring.CreateRule(r.Context(), rulesetID, rule); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.respondRule(w, r, rulesetID, rule.ID, http.StatusCreated)
}

// getRule is GET /api/v1/rulesets/{id}/rules/{rule_id}.
func (s *Server) getRule(w http.ResponseWriter, r *http.Request) {
	rulesetID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	ruleID, ok := pathUUID(w, r, "rule_id")
	if !ok {
		return
	}
	s.respondRule(w, r, rulesetID, ruleID, http.StatusOK)
}

// putRule is PUT /api/v1/rulesets/{id}/rules/{rule_id}, conditioned on
// the rule's version.
func (s *Server) putRule(w http.ResponseWriter, r *http.Request) {
	rulesetID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	ruleID, ok := pathUUID(w, r, "rule_id")
	if !ok {
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	rule, ok := s.decodeRule(w, r)
	if !ok {
		return
	}
	rule.ID = ruleID
	if err := s.authoring.UpdateRule(r.Context(), rulesetID, rule, version); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.respondRule(w, r, rulesetID, ruleID, http.StatusOK)
}

// deleteRule is DELETE /api/v1/rulesets/{id}/rules/{rule_id}.
func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	rulesetID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	ruleID, ok := pathUUID(w, r, "rule_id")
	if !ok {
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	if err := s.authoring.DeleteRule(r.Context(), rulesetID, ruleID, version); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- services ----------------------------------------------------------------

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	services, err := s.authoring.Store.ListServices(r.Context())
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	out := servicesResponse{Services: make([]serviceDocJSON, 0, len(services))}
	for i := range services {
		out.Services = append(out.Services, serviceJSONOf(&services[i]))
	}
	writeJSON(w, out)
}

func (s *Server) decodeService(w http.ResponseWriter, r *http.Request) (*policy.Service, bool) {
	var doc serviceInput
	if !decodeJSON(w, r, &doc) {
		return nil, false
	}
	svc, err := doc.service()
	if err != nil {
		s.writeProblemFor(w, err)
		return nil, false
	}
	return svc, true
}

func (s *Server) respondService(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	svc, err := s.authoring.Store.GetService(r.Context(), id)
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	w.Header().Set("ETag", versionTag(policy.FormatVersion(svc.Version)))
	if status == http.StatusCreated {
		w.Header().Set("Location", APIPrefix+"/services/"+svc.ID.String())
	}
	writeJSONStatus(w, status, serviceJSONOf(svc))
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.decodeService(w, r)
	if !ok {
		return
	}
	if err := s.authoring.CreateService(r.Context(), svc); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.respondService(w, r, svc.ID, http.StatusCreated)
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	s.respondService(w, r, id, http.StatusOK)
}

func (s *Server) putService(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	svc, ok := s.decodeService(w, r)
	if !ok {
		return
	}
	svc.ID = id
	if err := s.authoring.UpdateService(r.Context(), svc, version); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.respondService(w, r, id, http.StatusOK)
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	if err := s.authoring.DeleteService(r.Context(), id, version); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- address groups ----------------------------------------------------------

func (s *Server) listAddressGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.authoring.Store.ListAddressGroups(r.Context())
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	out := addressGroupsResponse{AddressGroups: make([]addressGroupJSON, 0, len(groups))}
	for i := range groups {
		out.AddressGroups = append(out.AddressGroups, addressGroupJSONOf(&groups[i]))
	}
	writeJSON(w, out)
}

func (s *Server) respondAddressGroup(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	g, err := s.authoring.Store.GetAddressGroup(r.Context(), id)
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	w.Header().Set("ETag", versionTag(policy.FormatVersion(g.Version)))
	if status == http.StatusCreated {
		w.Header().Set("Location", APIPrefix+"/address-groups/"+g.ID.String())
	}
	writeJSONStatus(w, status, addressGroupJSONOf(g))
}

func (s *Server) createAddressGroup(w http.ResponseWriter, r *http.Request) {
	var doc addressGroupInput
	if !decodeJSON(w, r, &doc) {
		return
	}
	g := doc.group()
	if err := s.authoring.CreateAddressGroup(r.Context(), g); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.respondAddressGroup(w, r, g.ID, http.StatusCreated)
}

func (s *Server) getAddressGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	s.respondAddressGroup(w, r, id, http.StatusOK)
}

func (s *Server) putAddressGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var doc addressGroupInput
	if !decodeJSON(w, r, &doc) {
		return
	}
	g := doc.group()
	g.ID = id
	if err := s.authoring.UpdateAddressGroup(r.Context(), g, version); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.respondAddressGroup(w, r, id, http.StatusOK)
}

func (s *Server) deleteAddressGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	if err := s.authoring.DeleteAddressGroup(r.Context(), id, version); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- workload labels ---------------------------------------------------------

// respondLabels answers with a workload's labels and their version.
func (s *Server) respondLabels(w http.ResponseWriter, r *http.Request, id identity.WorkloadID) {
	wl, err := s.reads.GetWorkload(r.Context(), id)
	if err != nil {
		s.readProblem(w, err)
		return
	}
	w.Header().Set("ETag", versionTag(registry.LabelsVersion(wl.Labels)))
	writeJSON(w, labelsResponse{Labels: labelMap(wl.Labels), Version: registry.LabelsVersion(wl.Labels)})
}

// getWorkloadLabels is GET /api/v1/workloads/{id}/labels: the labels as
// the resource a label edit is conditioned on.
func (s *Server) getWorkloadLabels(w http.ResponseWriter, r *http.Request) {
	id, ok := pathWorkloadID(w, r)
	if !ok {
		return
	}
	s.respondLabels(w, r, id)
}

// putWorkloadLabels is PUT /api/v1/workloads/{id}/labels: replace the
// labels, conditioned on their version.
func (s *Server) putWorkloadLabels(w http.ResponseWriter, r *http.Request) {
	id, ok := pathWorkloadID(w, r)
	if !ok {
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var doc labelsInput
	if !decodeJSON(w, r, &doc) {
		return
	}
	if err := s.fleet.SetLabels(r.Context(), id, doc.labels(), version); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.respondLabels(w, r, id)
}

// resendSnapshot is POST /api/v1/workloads/{id}/resend-snapshot: direct
// the workload's agent to reconnect, so its stream restarts from a fresh
// snapshot. The response carries the snapshot instant as it stood when
// the directive was fired; delivery is best-effort, and the outcome is
// observed on a later read of the workload as that instant advancing.
func (s *Server) resendSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathWorkloadID(w, r)
	if !ok {
		return
	}
	res, err := s.fleet.RequestReconnect(r.Context(), id)
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	writeJSON(w, resendSnapshotResponse{LastSnapshotSentAt: optionalTimestamp(res.LastSnapshotSentAt)})
}

// --- mode changes, preview, dry run ------------------------------------------

// createModeChange is POST /api/v1/mode-changes: a bulk enforcement-mode
// change. The response acknowledges the recorded intent; convergence is
// observed on the workload reads (ADR-0007 as amended).
func (s *Server) createModeChange(w http.ResponseWriter, r *http.Request) {
	var doc modeChangeInput
	if !decodeJSON(w, r, &doc) {
		return
	}
	req, problem := doc.request()
	if problem != nil {
		writeProblem(w, *problem)
		return
	}
	res, err := s.fleet.ChangeMode(r.Context(), req, doc.ExpectedMatchCount != nil)
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	writeJSON(w, modeChangeResponse{ID: res.ID.String(), Matched: res.Matched, DesiredUpdated: res.DesiredUpdated})
}

// previewSelector is POST /api/v1/selectors/preview: what a selector
// resolves to now, through the same resolution a mode change uses.
func (s *Server) previewSelector(w http.ResponseWriter, r *http.Request) {
	var doc selectorInput
	if !decodeJSON(w, r, &doc) {
		return
	}
	p, err := s.fleet.PreviewSelector(r.Context(), policy.Selector(doc.Selector))
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	writeJSON(w, previewJSON(p))
}

// renderDryRun is POST /api/v1/policies/render-dryrun: render a
// hypothetical policy set against persisted state and report what would
// change, persisting nothing.
func (s *Server) renderDryRun(w http.ResponseWriter, r *http.Request) {
	var doc dryRunInput
	if !decodeJSON(w, r, &doc) {
		return
	}
	names, err := s.names(r)
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	req, err := doc.request(names)
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	res, err := s.fleet.DryRun(r.Context(), req)
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	workloads, err := s.reads.Store.ListWorkloads(r.Context())
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	writeJSON(w, dryRunJSON(res, workloads))
}

// --- tokens ------------------------------------------------------------------

// listProvisioningTokens is GET /api/v1/provisioning-tokens: metadata
// only, never a secret.
func (s *Server) listProvisioningTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.enroll.ListTokens(r.Context())
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	now := s.now()
	out := provisioningTokensResponse{Tokens: make([]provisioningTokenJSON, 0, len(tokens))}
	for i := range tokens {
		out.Tokens = append(out.Tokens, provisioningTokenJSONOf(&tokens[i], now))
	}
	writeJSON(w, out)
}

// mintProvisioningToken is POST /api/v1/provisioning-tokens. The secret
// is in this response and nowhere else, ever.
func (s *Server) mintProvisioningToken(w http.ResponseWriter, r *http.Request) {
	var doc provisioningTokenInput
	if !decodeJSON(w, r, &doc) {
		return
	}
	labels := make([]enroll.Label, 0, len(doc.Labels))
	for _, k := range sortedKeys(doc.Labels) {
		labels = append(labels, enroll.Label{Key: k, Value: doc.Labels[k]})
	}
	plaintext, tok, err := s.enroll.MintToken(r.Context(), doc.Name, labels, doc.ttl())
	if err != nil {
		if strings.HasPrefix(err.Error(), "enroll: ") {
			writeProblem(w, finding("labels", "invalid", strings.TrimPrefix(err.Error(), "enroll: ")))
			return
		}
		s.writeProblemFor(w, err)
		return
	}
	s.log.Info("provisioning token minted", "token_id", tok.ID, "name", tok.Name)
	out := mintedProvisioningTokenJSON{provisioningTokenJSON: provisioningTokenJSONOf(&tok, s.now()), Token: plaintext}
	w.Header().Set("Location", APIPrefix+"/provisioning-tokens/"+tok.ID.String())
	writeJSONStatus(w, http.StatusCreated, out)
}

// revokeProvisioningToken is DELETE /api/v1/provisioning-tokens/{id}:
// revocation, not deletion; the row stays for the record.
func (s *Server) revokeProvisioningToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.enroll.RevokeToken(r.Context(), id); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.log.Info("provisioning token revoked", "token_id", id)
	w.WriteHeader(http.StatusNoContent)
}

// listOperatorTokens is GET /api/v1/operator-tokens: prefix and metadata
// only.
func (s *Server) listOperatorTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.operators.ListTokens(r.Context())
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	now := s.now()
	out := operatorTokensResponse{Tokens: make([]operatorTokenJSON, 0, len(tokens))}
	for i := range tokens {
		out.Tokens = append(out.Tokens, operatorTokenJSONOf(&tokens[i], now))
	}
	writeJSON(w, out)
}

// mintOperatorToken is POST /api/v1/operator-tokens.
func (s *Server) mintOperatorToken(w http.ResponseWriter, r *http.Request) {
	var doc operatorTokenInput
	if !decodeJSON(w, r, &doc) {
		return
	}
	plaintext, tok, err := s.operators.MintToken(r.Context(), doc.Name, doc.ttl())
	if err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.log.Info("operator token minted", "token_id", tok.ID, "name", tok.Name)
	out := mintedOperatorTokenJSON{operatorTokenJSON: operatorTokenJSONOf(&tok, s.now()), Token: plaintext}
	w.Header().Set("Location", APIPrefix+"/operator-tokens/"+tok.ID.String())
	writeJSONStatus(w, http.StatusCreated, out)
}

// revokeOperatorToken is DELETE /api/v1/operator-tokens/{id}.
func (s *Server) revokeOperatorToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.operators.RevokeToken(r.Context(), id); err != nil {
		s.writeProblemFor(w, err)
		return
	}
	s.log.Info("operator token revoked", "token_id", id)
	w.WriteHeader(http.StatusNoContent)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
