# ADR-0011: Fail static, local kill switch, resource budgets, no self-update

**Status:** Accepted

## Context

The agent is root-privileged software that manipulates firewalls on production hosts. Its worst-case behaviors — failing open (silently un-enforcing), failing closed (isolating a host because the management plane blinked), starving the host, or serving as a self-updating supply-chain vector — are worse than any feature is good. The safety properties are the product's credibility.

## Decision

- **Fail static.** Loss of control-plane connectivity changes nothing: the last-ACKed ruleset stays enforced from local disk, across agent restarts and host reboots. Flows buffer locally. Control-plane availability is a management concern, never a safety concern.
- **Local kill switch.** Root on the host can always disable enforcement locally (drop the owned table) with a documented command, without control-plane involvement. Host operators outrank the platform on their own machine — deliberately, and documented loudly.
- **Resource budgets.** The agent caps its own memory and buffer-disk footprint and degrades telemetry (sample, then drop) before ever degrading the host.
- **No self-update in v1.** Agents update via the host's normal package/config management. A root binary that replaces itself over the network is a supply-chain liability; managed rollout returns only when it can ship with signing and staged deployment worthy of it.
- Four-loop agent structure (sync / collect / reconcile / health) keeps these behaviors independently testable.

## Consequences

- Control-plane HA is a convenience tier, not a safety requirement — this single property de-risks the entire architecture.
- Version skew across the estate is normal and the protocol must tolerate it (proto discipline, ADR-0007).
- The kill switch is honest about the host trust model rather than pretending root can be resisted.
