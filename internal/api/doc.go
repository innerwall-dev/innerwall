// Package api serves the operator surface: the public REST/JSON API
// consumed by the console and by automation, on a listener of its own
// (ADR-0021). Its handlers are hand-shaped over the shared domain layer
// (ADR-0007 as amended): they carry the semantics a browser-facing surface
// needs and hold no domain logic, calling the same domain functions the
// command line calls. Design rules for estate scale: aggregation is
// server-side, unbounded reads paginate by cursor, a fan-out mutation
// returns a recorded intent and never blocks on convergence, and bulk
// operations exist only where the console's screens demand them.
//
// The read endpoints (a grouped flow rollup, a page of one workload's
// windows, the fleet and one workload in one shape, a workload's rendered
// policy) are thin handlers over internal/readmodel: each reads its
// parameters, calls one read model function, and encodes the result.
//
// The write endpoints (rulesets, rules, services, address groups, a
// workload's labels, bulk mode changes, the selector preview, the dry-run
// render, and token management) are thin handlers over internal/policy,
// internal/fleet, internal/enroll, and internal/operator: each decodes
// its body, reads the version a conditional write names, calls one domain
// function, and encodes the result. Admission runs in the domain and its
// findings are returned as they are; a write without If-Match is refused
// as precondition required and one naming a stale version as precondition
// failed with the current one. The contract is api/openapi.yaml.
//
// The package owns the listener's transport rules: TLS only; a middleware
// that resolves a session cookie or a bearer operator token to the one
// operator principal; an origin guard on every request that is not a read;
// a fixed-window throttle on the login endpoint; problem documents for
// every error. Handlers see a principal and nothing about how it was
// authenticated.
package api
