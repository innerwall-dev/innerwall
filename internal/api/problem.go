package api

import (
	"encoding/json"
	"net/http"
)

// ContentTypeProblem is the media type of every error the surface returns.
const ContentTypeProblem = "application/problem+json"

// Problem is an error document. Type is a stable identifier a client
// branches on; the other fields are for people, except the extensions,
// which carry what a client needs to act: the findings of a refused
// write, the current version of a resource a stale write named, the two
// counts of a refused mode change, and the last-seen instant of an agent
// a directed reconnect found offline.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	// Errors are the admission findings of a validation problem, one per
	// field the caller must fix.
	Errors []Finding `json:"errors,omitempty"`
	// CurrentVersion is the version the resource holds now, on a
	// precondition-failed problem.
	CurrentVersion string `json:"current_version,omitempty"`
	// Expected and Matched are the counts of a match-count problem.
	Expected *int `json:"expected,omitempty"`
	Matched  *int `json:"matched,omitempty"`
	// LastSeenAt is when the agent was last heard from, on an
	// agent-offline problem, where it is always present and null when the
	// agent never was.
	LastSeenAt *nullableInstant `json:"last_seen_at,omitempty"`
}

// nullableInstant is an extension member that is present on its problem
// type whether or not it holds an instant, so a client reads null rather
// than an absent member.
type nullableInstant struct {
	At *string
}

func (n nullableInstant) MarshalJSON() ([]byte, error) {
	return json.Marshal(n.At)
}

// Finding is one admission failure: where it is, which rule it breaks,
// and what to tell the operator.
type Finding struct {
	Path    string `json:"path"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// problemTypeBase namespaces the surface's problem types. They are opaque
// identifiers, not locations; a client compares them and never fetches
// them.
const problemTypeBase = "urn:innerwall:problem:"

// The problem types the surface emits. A console branches on
// ProblemNoPassword to show its fresh-install state, and on
// ProblemUnauthenticated to show the login form. ProblemInvalidRequest is
// a body that cannot be read; ProblemInvalidParameter is a query or path
// parameter that cannot be, and its detail names the parameter.
// ProblemValidation is a body that was read and refused by admission,
// with its findings. ProblemPreconditionRequired is a write without a
// version; ProblemPreconditionFailed is a write naming a version the
// resource no longer holds. ProblemMatchCountMismatch is a mode change
// whose selection resolved to a different number of workloads than the
// operator expected. ProblemDuplicateName, ProblemInUse, and
// ProblemAlreadyRevoked are the conflicts an authored or token write can
// meet. ProblemAgentOffline is a directed reconnect refused because the
// workload's agent is offline as last recorded.
const (
	ProblemUnauthenticated      = problemTypeBase + "unauthenticated"
	ProblemInvalidCredentials   = problemTypeBase + "invalid-credentials"
	ProblemNoPassword           = problemTypeBase + "no-password"
	ProblemCrossOrigin          = problemTypeBase + "cross-origin"
	ProblemTooManyAttempts      = problemTypeBase + "too-many-attempts"
	ProblemInvalidRequest       = problemTypeBase + "invalid-request"
	ProblemInvalidParameter     = problemTypeBase + "invalid-parameter"
	ProblemValidation           = problemTypeBase + "validation"
	ProblemPreconditionRequired = problemTypeBase + "precondition-required"
	ProblemPreconditionFailed   = problemTypeBase + "precondition-failed"
	ProblemMatchCountMismatch   = problemTypeBase + "match-count-mismatch"
	ProblemDuplicateName        = problemTypeBase + "duplicate-name"
	ProblemInUse                = problemTypeBase + "in-use"
	ProblemAlreadyRevoked       = problemTypeBase + "already-revoked"
	ProblemAgentOffline         = problemTypeBase + "agent-offline"
	ProblemNotFound             = problemTypeBase + "not-found"
	ProblemMethodNotAllowed     = problemTypeBase + "method-not-allowed"
	ProblemInternal             = problemTypeBase + "internal"
)

// writeProblem sends p. Headers set before the call are kept.
func writeProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", ContentTypeProblem)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// writeJSON sends v as the body of a successful response.
func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

// writeJSONStatus sends v with a status.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// The problems the surface emits for conditions it names in one place.
var (
	problemUnauthenticated = Problem{Type: ProblemUnauthenticated, Title: "Authentication required", Status: http.StatusUnauthorized}
	problemInternal        = Problem{Type: ProblemInternal, Title: http.StatusText(http.StatusInternalServerError), Status: http.StatusInternalServerError}
	problemNotFound        = Problem{Type: ProblemNotFound, Title: http.StatusText(http.StatusNotFound), Status: http.StatusNotFound}
)
