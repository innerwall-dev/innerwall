// Package gateway is the agent-facing gRPC surface of the control plane. It
// terminates TLS on one listener and serves two services split on the
// authentication boundary (ADR-0015): EnrollmentService, reachable with a
// provisioning token and no client certificate, and AgentService, reachable
// only with a verified workload credential. An interceptor enforces that
// split per service and is the only place identity is derived from a
// connection; handlers read it from the request context and nowhere else
// (ADR-0016).
//
// The sync stream (ADR-0002, ADR-0015) lives here too: one live stream per
// workload, registered in an in-process map so that a render's
// announcement, delivered over the database's notification channel, finds
// the stream to push to. Stream state is the only state held in process,
// and a reconnect rebuilds it from a snapshot. Flow ingestion follows.
package gateway
