# ADR-0015: The v1 agent wire contract

**Status:** Accepted

## Context

ADR-0002 fixes the transport (one persistent stream per agent, desired-state deltas), ADR-0004 fixes the identity model, ADR-0010 fixes the enforcement scope, and ADR-0012 reserves the multi-region seams. None of them fixes the shape of the messages. The first proto definitions under `proto/innerwall/v1/` make those shapes concrete, and from the moment they land, the breaking-change gate holds every later change to them. The decisions below are the ones the shapes encode and that a later contributor could plausibly undo one field at a time.

## Decision

1. **Two services, split on the authentication boundary.** `EnrollmentService` is reachable with a provisioning token and nothing else; it is the only surface a workload can reach before it has an identity. `AgentService` is reachable only with the credential issued at enrollment, over mutual TLS. Authentication policy is therefore declared per service, not per RPC, and no RPC can be accidentally exposed on the wrong side of the boundary.

2. **Snapshot on connect; deltas are intra-session only.** Every new sync stream begins with the server sending a full `PolicySnapshot`, regardless of the version the agent claims in `Hello`. Deltas are sent only after that snapshot, on the same stream. Rendered per-workload policy is small (a workload's own inbound rules) and reconnects are rare, so the cost is a few kilobytes per reconnect. What it buys is the elimination of cross-session divergence as a failure class: there is no state in which the server and agent disagree about the base a delta applies to.

3. **Atomic apply-or-fail acknowledgement.** The server pipelines `PolicyUpdate` messages without waiting for acknowledgements; the agent applies them strictly in order and acknowledges each with `ACK_STATUS_APPLIED` or `ACK_STATUS_FAILED`. Partial application is not representable: a version number is only meaningful if it describes the complete installed state. A failed apply leaves the agent on its last good version, and the server's recovery move is a fresh snapshot, never a retried delta.

4. **Per-workload monotonic versions.** The wire contract knows only a per-workload version, opaque to everything except ordering. Any estate-wide "generation" that the UI or audit trail wants is a server-internal mapping and is deliberately absent from the agent contract, so that recompiling one workload never forces a version bump on every other.

5. **Authored and rendered models are separate layers.** Operators write `Ruleset`s whose `LabelSelector`s express intent over labels; the control plane resolves that intent into `WorkloadPolicy`, a fully concrete set of `ResolvedRule`s (peer CIDRs, one protocol, port ranges). Agents never evaluate selectors. Agent behavior is deterministic and debuggable from the rendered artifact alone, selector semantics can evolve without an agent upgrade, and deltas operate on the rendered model, where the high-churn quantity, peer membership in a resolved rule, is cheap to express (`PeerChange`).

6. **Direction is defined; outbound is rejected at admission.** `Direction` carries both `INBOUND` and `OUTBOUND` from the first version, and a `Rule` orients its scope and peers by direction rather than carrying separate source and destination fields. In v1 the control plane rejects any rule with `DIRECTION_OUTBOUND` at admission (ADR-0010); enabling outbound later is a validation change, not a schema change. Flow telemetry (`ReportFlows`) is a separate RPC from `Sync`, so telemetry volume can never head-of-line-block a policy update.

7. **Scoped provisioning tokens with server-assigned labels.** A token is minted for a label scope and may enroll many workloads, so it can be baked into an image or a provisioning pipeline. The control plane assigns labels from the token's scope; agents never self-assign labels, because an agent-supplied label would let a compromised host place itself into a more permissive policy scope. Identity is granted at enrollment and is thereafter derived only from the connection credential, never from request payloads (ADR-0004). Enrollment and renewal carry a PKCS#10 certificate signing request, so the private key never leaves the host.

The message that each decision lives in, and the `region_id` reservations required by ADR-0012, are in `proto/innerwall/v1/`. Comments in those files carry the same reasoning at the point of use.

## Consequences

- The proto files are now under the breaking-change gate; undoing any decision above is a visible, reviewed proto diff and requires a superseding ADR.
- The agent is simple by construction: no selector evaluation, no delta reconciliation across sessions, no partial state. Complexity concentrates in the control plane, where it is observable and testable.
- Reconnection costs one snapshot per stream. At the sizes involved this is negligible; if a future estate proves otherwise, the fix is a superseding ADR, not a silent reintroduction of cross-session deltas.
- Outbound policy, when it arrives (ADR-0010), changes admission validation and the rendered model, not the authored `Rule` shape.
- Enrollment tokens are scoped secrets. Their lifecycle (expiry, usage limits, revocation) is enrollment-policy behavior, not wire-contract behavior, and is specified with the enrollment implementation.
