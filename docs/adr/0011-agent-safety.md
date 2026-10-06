# ADR-0011: Fail static, local kill switch, resource budgets, no self-update

**Status:** Accepted

## Context

The agent is root-privileged software that manipulates firewalls on production hosts. Its worst-case behaviors — failing open (silently un-enforcing), failing closed (isolating a host because the management plane blinked), starving the host, or serving as a self-updating supply-chain vector — are worse than any feature is good. The safety properties are the product's credibility.

## Decision

- **Fail static.** Loss of control-plane connectivity changes nothing: the last-ACKed ruleset stays enforced from local disk, across agent restarts and host reboots. Closed flow windows are held in a bounded in-memory buffer while the control plane is unreachable; what does not fit is dropped and recorded as an evidence gap. Control-plane availability is a management concern, never a safety concern. *(Amended 2026-10-06; see Amendments.)*
- **Local kill switch.** Root on the host can always disable enforcement locally (drop the owned table) with a documented command, without control-plane involvement. Host operators outrank the platform on their own machine — deliberately, and documented loudly.
- **Resource budgets.** The agent bounds the telemetry it holds and drops before it would ever burden the host: the open aggregation window is bounded in distinct keys and the closed-window buffer in records, both configurable, and every drop at either bound is recorded as an evidence gap and counted. The connection-table dump at each subscribe is bounded in the entries it processes, not in the memory the kernel's reply occupies. Sampling, buffering to disk, and a whole-process memory budget are deferred. *(Amended 2026-10-06; see Amendments.)*
- **No self-update in v1.** Agents update via the host's normal package/config management. A root binary that replaces itself over the network is a supply-chain liability; managed rollout returns only when it can ship with signing and staged deployment worthy of it.
- The agent is a few independent pieces sharing local state: the sync daemon (the stream, heartbeats, inventory, and the apply into the owned table, which runs synchronously inside it), the credential renewal timer, the collector, and the reporter. Keeping them independent keeps these behaviors independently testable. *(Amended 2026-10-06; see Amendments.)*

## Consequences

- Control-plane HA is a convenience tier, not a safety requirement — this single property de-risks the entire architecture.
- Version skew across the estate is normal and the protocol must tolerate it (proto discipline, ADR-0007).
- The kill switch is honest about the host trust model rather than pretending root can be resisted.

## Amendments

- **2026-10-06 (PR #25, conformance-audit absorption).** The core decision stands: fail static, a local kill switch, the agent never burdening the host, no self-update. Three bullets described more than ships and are corrected in place. Flow buffering is in memory and bounded, with drops recorded as evidence gaps (ADR-0019 as amended), not buffered to disk. The resource-budget bullet promised a memory and buffer-disk cap with sampling before dropping; what ships bounds the open window in keys and the buffer in records, drops past either bound, and records every drop, while sampling, disk buffering, and a whole-process memory budget are deferred and recorded. The four-loop structure (sync, collect, reconcile, health) is corrected to the pieces the agent is: enforcement runs synchronously inside the sync daemon's apply, heartbeats ride the stream, and there is no separate health loop; the `health` package's comment says so.
