# ADR-0001: Visibility and simulation precede enforcement

**Status:** Accepted

## Context

Segmentation fails in adoption more often than in engineering. Installing a root-privileged daemon that can block traffic is a large ask for any operator; writing deny rules against a network whose actual dependencies are unknown is how segmentation projects cause outages and get rolled back. Both problems share a root cause: enforcement is being attempted before observation.

## Decision

The product workflow is **visibility → simulation → enforcement**. The platform makes that sequence the default path and surfaces the evidence each step produces; promotion to enforcement is an operator's act, and the console gates it behind an explicit acknowledgment whenever the evidence does not support it: *(Amended 2026-10-06; see Amendments.)*

1. Agents start in visibility: they collect flows and build the dependency map, and the table they own holds only an observation chain, one rule that engages the kernel's connection tracking and accepts, so no policy is evaluated and nothing is dropped (ADR-0020). *(Amended 2026-10-06 and 2026-10-08; see Amendments.)*
2. Simulation runs the exact ruleset enforcement would install on real traffic, accepting what enforcement would drop and recording it (ADR-0020), and the simulation review reports exactly which real connections would have been denied, beside any evidence gaps in the range. Policy is edited live, with no draft; a dry-run render shows what a hypothetical policy would install before it is written. *(Amended 2026-10-06; see Amendments.)*
3. Enforcement is opt-in per workload or label scope, never a global switch.

All v1 milestones deliver visibility value standalone; enforcement is the second act.

## Consequences

- The trust barrier at install time drops to that of an observability agent.
- Every enforcement decision is offered with its evidence; "what would break" is a query, not a guess. The console's promotion refuses to proceed past an unsafe verdict or missing evidence without the operator's explicit acknowledgment. The surface and the command line accept a mode change directly, for automation, so the guarantee is the default and the evidence, not a server-side refusal; a server-side evidence guard on mode changes is recorded backlog. *(Amended 2026-10-06; see Amendments.)*
- The flow pipeline and map must be production-quality from day one — they are the product, not instrumentation.
- Revenue-of-attention risk: the project must resist becoming only a mapping tool; enforcement is the destination and stays on the roadmap's critical path.

## Amendments

- **2026-10-06 (PR #25, conformance-audit absorption; maintainer ruling).** The core discipline stands: visibility by default, simulation on the exact enforced ruleset, enforcement opt-in per workload or label scope and never a global switch. Four passages overstated the mechanism and are corrected in place. The decision said the sequence is "enforced by the platform"; what ships is visibility as every workload's starting mode, evidence surfaced at every step, and promotion operator-gated in the console behind an explicit acknowledgment when the verdict is not safe or the evidence has gaps, while the surface and the command line accept a mode change directly. Decision 1 said agents "touch nothing"; in visibility the agent installs its owned table empty, with no verdict chain. Decision 2 described policy drafts replayed against observed flows; policy is edited live with no draft, simulation is the enforced ruleset with a terminal rule that accepts and records (ADR-0020), and a dry-run render previews a hypothetical policy. The consequence that every enforcement decision is preceded by evidence is corrected to say it is offered with its evidence. A server-side evidence guard on mode changes, refusing an unacknowledged promotion at the domain layer, is recorded backlog; until it lands the console is where the gate lives, and the record says so. Maintainer ruling on PR #25: amendment, not supersession, because the platform-enforced-sequence wording overstated the mechanism while the core discipline stands. The ruling was made with the tradeoff in view: no version of the code ever refused a mode change at the domain layer, so this records what has always shipped rather than withdrawing a guarantee that existed, and the guard that would make the stronger wording true is the backlog item above.
- **2026-10-08 (PR #32, issue #30).** The core discipline stands: visibility by default, simulation on the exact enforced ruleset, enforcement opt-in per workload or label scope. Decision 1 said the visibility table holds no verdict chain and nothing is evaluated, and the 2026-10-06 note above says the table is installed empty. An empty table references no connection tracking, so on a host where nothing else did, the kernel tracked nothing and visibility collected nothing. Decision 1 is corrected in place: the table holds one observation chain whose one rule engages connection tracking and accepts. No policy is evaluated and nothing is dropped, so visibility still asks of an operator only what an observability agent does. ADR-0020 carries the chain's shape and the decision that visibility logs nothing.
