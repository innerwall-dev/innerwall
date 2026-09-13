package api

import (
	"encoding/json"
	"net/http"
)

// ContentTypeProblem is the media type of every error the surface returns.
const ContentTypeProblem = "application/problem+json"

// Problem is an error document. Type is a stable identifier a client
// branches on; the other fields are for people.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
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
const (
	ProblemUnauthenticated    = problemTypeBase + "unauthenticated"
	ProblemInvalidCredentials = problemTypeBase + "invalid-credentials"
	ProblemNoPassword         = problemTypeBase + "no-password"
	ProblemCrossOrigin        = problemTypeBase + "cross-origin"
	ProblemTooManyAttempts    = problemTypeBase + "too-many-attempts"
	ProblemInvalidRequest     = problemTypeBase + "invalid-request"
	ProblemInvalidParameter   = problemTypeBase + "invalid-parameter"
	ProblemNotFound           = problemTypeBase + "not-found"
	ProblemMethodNotAllowed   = problemTypeBase + "method-not-allowed"
	ProblemInternal           = problemTypeBase + "internal"
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

// The problems the surface emits for conditions it names in one place.
var (
	problemUnauthenticated = Problem{Type: ProblemUnauthenticated, Title: "Authentication required", Status: http.StatusUnauthorized}
	problemInternal        = Problem{Type: ProblemInternal, Title: http.StatusText(http.StatusInternalServerError), Status: http.StatusInternalServerError}
	problemNotFound        = Problem{Type: ProblemNotFound, Title: http.StatusText(http.StatusNotFound), Status: http.StatusNotFound}
)
