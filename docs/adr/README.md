# Architecture Decision Records

Numbered, immutable once **Accepted** in their core decision. Format: Status / Context / Decision / Consequences.

These ADRs are the source of truth for automated and human PR review — a change that contradicts the core decision of an Accepted ADR needs a superseding ADR first.

## Changing an ADR

Two mechanisms, and which one applies is decided by whether the core decision survives:

- **Supersession** is reserved for the case where an ADR's core decision is reversed or replaced. The superseding ADR takes the next number, carries a `Supersedes: ADR-XXXX` header, and restates the full current decision; the old ADR's status line becomes `Superseded by ADR-YYYY` and its body is never edited. The pull request that carries a supersession names the maintainer sign-off for it explicitly in its description.
- **Everything else is an in-place amendment**: additive detail, clarification, narrowing of scope, or correction of a subsidiary point while the core decision stands. An amendment is a dated note under an `## Amendments` heading in the ADR itself, referencing the ADR or pull request that carries the new detail. Amendments never restate the document, and they never touch the core decision.

Narrowing is a form of amendment; there is no `Narrows:` header.

An ADR written to resolve a review finding is scoped to that finding. New architectural decisions encountered along the way get their own commissioned ADR, not bullets inside the fix.
