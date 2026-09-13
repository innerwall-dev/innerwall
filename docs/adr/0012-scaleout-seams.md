# ADR-0012: Relay tier and regional federation are designed seams, deferred implementations

**Status:** Accepted

## Context

Two pressures arrive only at large-estate scale: connection-count fan-in (tens of thousands of persistent streams on one plane) and multi-region operation (latency, failure isolation, and data-residency constraints on flow telemetry). Building either now would burden every small deployment; ignoring them now would bake in architecture they can't extend.

## Decision

Both are specified and seamed today, implemented when demanded:

- **Relay tier (optional).** Agents connect to a nearby relay; relays multiplex to the core, collapsing fan-in by orders of magnitude. Relays cache **signed policy bundles**: the control plane signs compiled rulesets, so a relay can serve cached policy during a core outage while remaining unable to author or alter policy. Agents always retain fallback-to-direct; a relay is an optimization, never a dependency.
- **Regional federation.** Regional control planes under a thin global coordinator. **Label-membership summaries** sync across regions so cross-region policy compiles; **raw flow data never leaves its region** (data-residency as an architectural property). The CA becomes hierarchical with per-region intermediates. Failure goal: a severed region keeps enforcing and keeps serving its own map.

## Consequences

- v1 code must respect the seams: rendered policy carries an authenticity envelope before it transits anything other than the mutually authenticated sync stream (signing deferred until then; see Amendments); region_id exists in the schema; nothing assumes exactly one control plane.
- Small deployments pay nothing; the scaling story is documentation until it's code.
- Signed-bundle verification keys become part of agent trust material now, even with no relays shipped.

## Amendments

- **2026-09-13 (PR #5, ADR-0018): policy-bundle signing deferred, with a tripwire.** The consequence that required policy bundles to be signed from day one is corrected in place. Today the agent's only policy source is the mutually authenticated sync stream to the control plane; a signature on the rendered bundle would re-prove what the connection already proves, and a second key with its own custody story is not free. The requirement stands down until policy transits anything other than that stream. Any future distribution design involving relays, caches, or regional intermediaries (the reserved topology) must include an authenticity envelope for rendered policy; this note is the tripwire for that requirement. The core decision, that the relay tier and regional federation are designed seams with deferred implementations, stands.
