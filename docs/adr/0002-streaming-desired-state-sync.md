# ADR-0002: Persistent gRPC streams with desired-state delta sync

**Status:** Accepted

## Context

A central plane serving thousands of endpoints fails predictably when the protocol is chatty: agents polling for policy, reporting via repeated REST calls, and sharing a rate-limited front door with human and automation traffic. Poll intervals force a trade between convergence latency and load; reconnection after outages produces thundering herds; imperative APIs make missed calls a correctness problem.

## Decision

- Each agent holds **one persistent bidirectional gRPC stream** over mTLS for policy: deltas flow down; heartbeats, inventory, and ACKs flow up. Aggregated flow telemetry travels on its own RPC, `ReportFlows`, over a connection of its own with its own jittered backoff, so telemetry volume can never head-of-line-block a policy update (ADR-0015, ADR-0019). Nothing polls in steady state on either. *(Amended 2026-09-13; see Amendments.)*
- Policy distribution is **desired-state**: the control plane versions each agent's compiled ruleset; agents reconcile to the latest version and report the version they run. An agent offline for an hour reconnects and fetches one delta, not a backlog of missed calls.
- Reconnection uses **exponential backoff with full jitter** (`wait = random(0, min(cap, base × 2^attempt))`) because a control-plane restart otherwise triggers a synchronized storm of CPU-expensive mTLS handshakes.

## Consequences

- The dominant failure mode of chatty central APIs is designed out rather than rate-limited.
- Desired-vs-actual version is queryable; drift detection and rollback are structural features.
- Long-lived connections make the gateway stateful for the life of a stream: a render's announcement reaches the replica holding a workload's stream through the database notification channel, so any replica can push to any agent it holds without a presence table (see ADR-0017, as amended).
- Firewalls between agents and the control plane must permit long-lived outbound TLS; documentation must cover keepalives and middlebox timeouts.

## Amendments

- **2026-09-13 (PR #5).** The consequence that required a presence table for routing pushes is corrected in place: pushes reach the replica holding a stream through the database notification channel, per ADR-0017's amended propagation design. The core decision, one persistent stream per agent carrying desired-state deltas with full-jitter backoff and nothing polling, stands.
- **2026-09-13 (PR #7, ADR-0015, ADR-0019).** The decision bullet that listed aggregated flows among what flows up the policy stream was written before the wire contract existed and is corrected in place: ADR-0015 fixed flow telemetry as a separate RPC so that its volume can never head-of-line-block a policy update, and ADR-0019 built it on a connection of its own with the same full-jitter backoff. The core decision stands: one persistent policy stream per agent, desired-state deltas, jittered reconnects, and nothing polling anywhere in steady state.
