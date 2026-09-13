# Architecture Decision Records

Numbered, immutable once **Accepted** in their core decision. Format: Status / Context / Decision / Consequences.

These ADRs are the source of truth for automated and human PR review — a change that contradicts the core decision of an Accepted ADR needs a superseding ADR first.

## Changing an ADR

Two mechanisms, and which one applies is decided by whether the core decision survives:

- **Supersession** is reserved for the case where an ADR's core decision is reversed or replaced. The superseding ADR takes the next number, carries a `Supersedes: ADR-XXXX` header, and restates the full current decision; the old ADR's status line becomes `Superseded by ADR-YYYY` and its body is never edited. The pull request that carries a supersession names the maintainer sign-off for it explicitly in its description.
- **Everything else is an in-place amendment**: additive detail, clarification, narrowing of scope, or correction of a subsidiary point while the core decision stands. An in-place amendment corrects the body text so the document always states the current decision, and records the change in a dated note under an `## Amendments` heading in the ADR itself, referencing the ADR or pull request that carries the new detail; git history preserves the prior text. Amendments never restate the document, and they never touch the core decision.
- **Titles state the current decision** and are corrected by amendment like any other text, with the correction recorded in the dated note; the ADR number is the permanent citation key, and the file name keeps it.

Narrowing is a form of amendment; there is no `Narrows:` header.

`main` is the record: an ADR number is consumed only when the ADR carrying it is on `main`, so a number used by a draft on a branch that never merged is free for the next ADR.

An ADR written to resolve a review finding is scoped to that finding. New architectural decisions encountered along the way get their own commissioned ADR, not bullets inside the fix.
