package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
)

// paramError says which query parameter could not be read and why. It
// becomes an invalid-parameter problem naming the parameter.
type paramError struct {
	name   string
	reason string
}

func (e *paramError) Error() string { return "parameter " + e.name + ": " + e.reason }

// query reads the read endpoints' query parameters. Every reader returns
// the zero value when the parameter is absent, so the domain applies its
// defaults, and a paramError when it is present and malformed.
type query struct {
	values url.Values
}

func queryOf(r *http.Request) query { return query{values: r.URL.Query()} }

func (q query) has(name string) bool { return q.values.Get(name) != "" }

func (q query) text(name string) string { return strings.TrimSpace(q.values.Get(name)) }

// timestamp reads an RFC 3339 instant.
func (q query) timestamp(name string) (time.Time, error) {
	s := q.text(name)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, &paramError{name, "not an RFC 3339 timestamp"}
	}
	return t.UTC(), nil
}

func (q query) verdict(name string) (innerwallv1.PolicyDecision, error) {
	v, err := readmodel.ParseVerdict(q.text(name))
	if err != nil {
		return 0, &paramError{name, "one of observed, allowed, would_block, blocked"}
	}
	return v, nil
}

func (q query) direction(name string) (innerwallv1.Direction, error) {
	d, err := policy.ParseDirection(q.text(name))
	if err != nil {
		return 0, &paramError{name, "one of inbound, outbound"}
	}
	return d, nil
}

func (q query) mode(name string) (innerwallv1.EnforcementMode, error) {
	s := q.text(name)
	if s == "" {
		return innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED, nil
	}
	m, err := policy.ParseMode(s)
	if err != nil {
		return 0, &paramError{name, "one of visibility, simulation, enforced"}
	}
	return m, nil
}

func (q query) syncState(name string) (innerwallv1.SyncState, error) {
	s, err := readmodel.ParseSyncState(q.text(name))
	if err != nil {
		return 0, &paramError{name, "one of synced, pending, degraded, offline"}
	}
	return s, nil
}

func (q query) workloadID(name string) (*identity.WorkloadID, error) {
	s := q.text(name)
	if s == "" {
		return nil, nil //nolint:nilnil // absent is a valid outcome
	}
	id, err := identity.ParseWorkloadID(s)
	if err != nil {
		return nil, &paramError{name, "not a workload id"}
	}
	return &id, nil
}

func (q query) service(name string) (*readmodel.Service, error) {
	s := q.text(name)
	if s == "" {
		return nil, nil //nolint:nilnil // absent is a valid outcome
	}
	svc, err := readmodel.ParseService(s)
	if err != nil {
		return nil, &paramError{name, "<protocol>/<port> such as tcp/5432, or icmp"}
	}
	return &svc, nil
}

func (q query) groupBy(name string) (flowstore.GroupBy, error) {
	g, err := flowstore.ParseGroupBy(q.text(name))
	if err != nil {
		return "", &paramError{name, "one of " + flowstore.GroupByNames()}
	}
	return g, nil
}

func (q query) order(name string) (flowstore.GroupOrder, error) {
	switch s := q.text(name); s {
	case "":
		return flowstore.OrderByConnections, nil
	case string(flowstore.OrderByConnections), string(flowstore.OrderByRecency):
		return flowstore.GroupOrder(s), nil
	default:
		return "", &paramError{name, "one of connections, recent"}
	}
}

// selector reads repeated key=value pairs into a label selector:
// requirements are ANDed across keys, and a key given more than once ORs
// its values, the syntax the domain's scope matcher accepts.
func (q query) selector(name string) (policy.Selector, error) {
	sel := policy.Selector{}
	for _, s := range q.values[name] {
		k, v, ok := strings.Cut(strings.TrimSpace(s), "=")
		if !ok || k == "" {
			return nil, &paramError{name, "key=value"}
		}
		sel[k] = append(sel[k], v)
	}
	return sel, nil
}

// limit reads a positive page size; the domain caps it.
func (q query) limit(name string) (int, error) {
	s := q.text(name)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, &paramError{name, "a positive integer"}
	}
	return n, nil
}

// readProblem maps a read model error to its problem document. Errors
// that are the caller's are invalid-parameter problems with the reason;
// an unknown workload is not found; anything else is internal.
func (s *Server) readProblem(w http.ResponseWriter, err error) {
	var pe *paramError
	switch {
	case errors.As(err, &pe):
		writeProblem(w, Problem{Type: ProblemInvalidParameter, Title: "Invalid parameter", Status: http.StatusBadRequest, Detail: pe.Error()})
	case readmodel.IsUnknown(err):
		writeProblem(w, problemNotFound)
	case errors.Is(err, readmodel.ErrInvalidCursor):
		writeProblem(w, Problem{Type: ProblemInvalidParameter, Title: "Invalid parameter", Status: http.StatusBadRequest, Detail: "parameter cursor: not a cursor this endpoint issued"})
	case errors.Is(err, readmodel.ErrInvalidRange):
		writeProblem(w, Problem{Type: ProblemInvalidParameter, Title: "Invalid parameter", Status: http.StatusBadRequest, Detail: "parameter to: must be after from"})
	case errors.Is(err, readmodel.ErrWorkloadRequired):
		writeProblem(w, Problem{Type: ProblemInvalidParameter, Title: "Invalid parameter", Status: http.StatusBadRequest, Detail: "parameter workload: required; this endpoint has no unbounded form"})
	case errors.Is(err, readmodel.ErrInvalidService), errors.Is(err, flowstore.ErrUnknownGroupBy):
		writeProblem(w, Problem{Type: ProblemInvalidParameter, Title: "Invalid parameter", Status: http.StatusBadRequest, Detail: err.Error()})
	default:
		s.log.Error("read failed", "error", err)
		writeProblem(w, problemInternal)
	}
}

// paramDetail formats an invalid-parameter problem for a handler-level
// check.
func invalidParameter(name, reason string) Problem {
	return Problem{Type: ProblemInvalidParameter, Title: "Invalid parameter", Status: http.StatusBadRequest, Detail: fmt.Sprintf("parameter %s: %s", name, reason)}
}
