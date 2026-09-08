# ADR-0002: Persistent gRPC streams with desired-state delta sync

**Status:** Accepted

## Context

A central plane serving thousands of endpoints fails predictably when the protocol is chatty: agents polling for policy, reporting via repeated REST calls, and sharing a rate-limited front door with human and automation traffic. Poll intervals force a trade between convergence latency and load; reconnection after outages produces thundering herds; imperative APIs make missed calls a correctness problem.

## Decision

- Each agent holds **one persistent bidirectional gRPC stream** over mTLS. Policy deltas flow down; aggregated flows, heartbeats, and ACKs flow up. Nothing polls in steady state.
- Policy distribution is **desired-state**: the control plane versions each agent's compiled ruleset; agents reconcile to the latest version and report the version they run. An agent offline for an hour reconnects and fetches one delta, not a backlog of missed calls.
- Reconnection uses **exponential backoff with full jitter** (`wait = random(0, min(cap, base × 2^attempt))`) because a control-plane restart otherwise triggers a synchronized storm of CPU-expensive mTLS handshakes.

## Consequences

- The dominant failure mode of chatty central APIs is designed out rather than rate-limited.
- Desired-vs-actual version is queryable; drift detection and rollback are structural features.
- Long-lived connections make the gateway stateful-ish: a presence table is required so any replica can route "push to agent X" (see ADR-0005).
- Firewalls between agents and the control plane must permit long-lived outbound TLS; documentation must cover keepalives and middlebox timeouts.
