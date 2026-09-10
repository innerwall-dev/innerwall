// Package gateway is the agent-facing gRPC surface of the control plane. It
// terminates TLS on one listener and serves two services split on the
// authentication boundary (ADR-0015): EnrollmentService, reachable with a
// provisioning token and no client certificate, and AgentService, reachable
// only with a verified workload credential. An interceptor enforces that
// split per service and is the only place identity is derived from a
// connection; handlers read it from the request context and nowhere else
// (ADR-0016).
//
// This milestone serves enrollment and credential renewal. The persistent
// desired-state stream (ADR-0002), flow ingestion, and presence follow.
package gateway
