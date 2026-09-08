# ADR-0001: Visibility and simulation precede enforcement

**Status:** Accepted

## Context

Segmentation fails in adoption more often than in engineering. Installing a root-privileged daemon that can block traffic is a large ask for any operator; writing deny rules against a network whose actual dependencies are unknown is how segmentation projects cause outages and get rolled back. Both problems share a root cause: enforcement is being attempted before observation.

## Decision

The product workflow is **visibility → simulation → enforcement**, enforced by the platform:

1. Agents start in a read-only mode: collect flows, build the dependency map, touch nothing.
2. Policy drafts are simulated against observed flows, reporting exactly which real connections would have been denied.
3. Enforcement is opt-in per workload or label scope, never a global switch.

All v1 milestones deliver visibility value standalone; enforcement is the second act.

## Consequences

- The trust barrier at install time drops to that of an observability agent.
- Every enforcement decision is preceded by evidence; "what would break" is a query, not a guess.
- The flow pipeline and map must be production-quality from day one — they are the product, not instrumentation.
- Revenue-of-attention risk: the project must resist becoming only a mapping tool; enforcement is the destination and stays on the roadmap's critical path.
