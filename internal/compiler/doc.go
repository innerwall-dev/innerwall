// Package compiler renders the authored policy model plus registry state
// into each workload's concrete WorkloadPolicy and decides which versions
// advance (ADR-0018).
//
// Rendering is a pure function of its inputs: label selectors resolve to the
// workloads that match them, and those workloads' current addresses become
// host routes; address groups expand to their CIDRs; service references and
// inline entries expand to one resolved rule per protocol. Versioning is a
// pure function too: the previously persisted policy is diffed against the
// freshly rendered one and the version advances only when the rendered
// output changed. The Engine runs both inside one store transaction that
// holds the render lock, so renders are serialized across processes
// (ADR-0017) and every persisted change is announced to the replica holding
// the workload's stream.
//
// Version 1 re-renders every workload on any change and recovers
// per-workload version semantics by diffing. Compilation is inbound-only
// (ADR-0010): admission never lets an outbound rule reach this package, and
// the renderer emits only inbound rules regardless.
package compiler
