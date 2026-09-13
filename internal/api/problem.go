package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
// ProblemUnauthenticated to show the login form.
const (
	ProblemUnauthenticated    = problemTypeBase + "unauthenticated"
	ProblemInvalidCredentials = problemTypeBase + "invalid-credentials"
	ProblemNoPassword         = problemTypeBase + "no-password"
	ProblemCrossOrigin        = problemTypeBase + "cross-origin"
	ProblemTooManyAttempts    = problemTypeBase + "too-many-attempts"
	ProblemInvalidRequest     = problemTypeBase + "invalid-request"
	ProblemNotFound           = problemTypeBase + "not-found"
	ProblemMethodNotAllowed   = problemTypeBase + "method-not-allowed"
	ProblemInternal           = problemTypeBase + "internal"
)

// Reasons carried in a status detail from a handler to the error handler,
// so a handler states a condition and the transport picks the problem
// type and HTTP status for it.
const (
	reasonNoPassword         = "NO_PASSWORD"
	reasonInvalidCredentials = "INVALID_CREDENTIALS" //nolint:gosec // a condition name, not a credential
	reasonDomain             = "innerwall"
)

var reasonProblems = map[string]Problem{
	reasonNoPassword:         {Type: ProblemNoPassword, Title: "No operator password has been set", Status: http.StatusForbidden},
	reasonInvalidCredentials: {Type: ProblemInvalidCredentials, Title: "Invalid credentials", Status: http.StatusUnauthorized},
}

// writeProblem sends p. Headers set before the call are kept.
func writeProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", ContentTypeProblem)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// conditionError builds a handler error whose reason the error handler
// maps to a problem type.
func conditionError(code codes.Code, reason, detail string) error {
	st := status.New(code, detail)
	withInfo, err := st.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: reasonDomain})
	if err != nil {
		return st.Err()
	}
	return withInfo.Err()
}

// problemFromError translates a handler error into a problem document. A
// reason the surface knows picks the type and status; otherwise the status
// code does, and an internal error is reported without its detail, since
// that detail belongs in the log and not on the wire.
func problemFromError(err error) Problem {
	st := status.Convert(err)
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetDomain() == reasonDomain {
			if p, known := reasonProblems[info.GetReason()]; known {
				p.Detail = st.Message()
				return p
			}
		}
	}
	httpStatus := runtime.HTTPStatusFromCode(st.Code())
	p := Problem{Status: httpStatus, Title: http.StatusText(httpStatus), Detail: st.Message()}
	switch st.Code() { //nolint:exhaustive // every other code is an internal condition
	case codes.Unauthenticated:
		p.Type = ProblemUnauthenticated
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		p.Type = ProblemInvalidRequest
	case codes.NotFound:
		p.Type = ProblemNotFound
	default:
		p.Type = ProblemInternal
		p.Status = http.StatusInternalServerError
		p.Title = http.StatusText(http.StatusInternalServerError)
		p.Detail = ""
	}
	return p
}

// problemErrorHandler is the façade's error handler: every handler error
// becomes a problem document.
func problemErrorHandler(_ context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, _ *http.Request, err error) {
	p := problemFromError(err)
	if p.Status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="innerwall"`)
	}
	writeProblem(w, p)
}

// problemRoutingErrorHandler answers a request no route matches.
func problemRoutingErrorHandler(_ context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, _ *http.Request, httpStatus int) {
	p := Problem{Status: httpStatus, Title: http.StatusText(httpStatus)}
	switch httpStatus {
	case http.StatusNotFound:
		p.Type = ProblemNotFound
	case http.StatusMethodNotAllowed:
		p.Type = ProblemMethodNotAllowed
	default:
		p.Type = ProblemInvalidRequest
	}
	writeProblem(w, p)
}
