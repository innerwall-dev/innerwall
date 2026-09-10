# Architecture Decision Records

Numbered, immutable once **Accepted**. To change a decision, write a new ADR that supersedes the old one; never edit history. Format: Status / Context / Decision / Consequences.

Two headers relate ADRs to each other, both placed under Status:

- `**Supersedes:** ADR-NNNN` — the new ADR replaces the old one in full. The old ADR's status line becomes `Superseded by ADR-MMMM`; nothing else in it changes.
- `**Narrows:** ADR-NNNN (scope)` — the new ADR carves a named exception out of one decision in the old ADR, which otherwise stands. The new ADR's Decision must state the exception and argue it from first principles. The old ADR's status line becomes `Accepted; narrowed by ADR-MMMM (scope)` so a reader of the old rule finds the carve-out; nothing else in it changes. Narrowing is for one exception to one decision. A change that touches more than that supersedes.

The status line is the only part of an Accepted ADR that is ever edited, and only to record one of these two relations.

These ADRs are the source of truth for automated and human PR review — a change that contradicts an Accepted ADR needs a superseding ADR first.
