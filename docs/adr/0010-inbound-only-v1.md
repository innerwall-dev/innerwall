# ADR-0010: v1 enforces inbound only; outbound arrives in v2 as a layered model

**Status:** Accepted

## Context

Full bidirectional policy doubles the surface an operator must reason about before anything can be safely enforced. The two directions also fail differently: an over-tight inbound rule manifests immediately and legibly (a known dependency stops reaching a workload on the map); an over-tight outbound rule breaks infrastructure dependencies — DNS, directory services, NTP, package mirrors — in ways that present as mysterious host misbehavior rather than as a blocked flow. Outbound policy also has a structural problem: every application shares the same infrastructure dependencies, and forcing each app scope to re-declare them produces either massive duplication or massive holes.

## Decision

- **v1 compiles and enforces inbound rules only.** Each workload's ruleset constrains what may reach it. Outbound remains observed (full flow visibility in both directions) but unenforced.
- **v2 introduces outbound as a layered model**: shared **baseline policies** owned by platform operators (estate-wide dependencies declared once) composed under **app-scoped policies** owned by application teams. Composition, precedence, and simulation semantics for layering are v2 design work and get their own ADRs.

## Consequences

- Operators reach safe, comprehensible enforcement dramatically sooner.
- Lateral-movement protection is already substantially delivered inbound-side: a workload that accepts connections only from its declared dependents is a hard target regardless of its neighbors' egress.
- Compromised-host egress control is honestly deferred and documented as such.
- The policy schema reserves direction now so v2 is additive, not migratory.
