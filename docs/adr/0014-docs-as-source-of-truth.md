# ADR-0014: docs/ is the shared source of truth; PRs are reviewed against ADRs, including by automated review

**Status:** Accepted

## Context

The project is designed in conversational sessions, prototyped with design tools, and implemented largely with AI coding agents — plus, eventually, human contributors. Multiple actors with no shared memory need one durable place where intent lives, or the codebase drifts from its own architecture one plausible-looking PR at a time.

## Decision

- **The repo's `docs/` tree — this ADR set, ARCHITECTURE.md, and CLAUDE.md — is the canonical design record.** Sessions and tools produce decisions; decisions are only real once landed in `docs/`.
- ADRs record architecture decisions. The mechanics of changing them (supersession, in-place amendment, and the status lines that record either) are governed by `docs/adr/README.md` under maintainer authority.
- CI runs **claude-code-action** in two modes: `@claude` interactive assistance on PRs, and an automated review pass that evaluates every PR **against the Accepted ADRs** and flags contradictions.
- CLAUDE.md carries the standing implementation instructions (conventions, commands, ADR pointers) that any coding agent — or new human — reads first.

## Consequences

- Architectural intent survives across tools, sessions, and contributors by construction.
- A PR that violates an ADR gets flagged mechanically; the fix is either the PR or a change to the record under the mechanics `docs/adr/README.md` defines, never silent drift.
- Docs gain a maintenance cost equal to code; that cost is the point.

## Amendments

- **2026-09-13 (PR #5).** The decision bullet that read "ADRs are immutable once Accepted; changes happen by superseding ADR" is corrected in place to delegate: ADRs record architecture decisions, and the mechanics of changing them are governed by `docs/adr/README.md` under maintainer authority, which today defines supersession for a reversed or replaced core decision and dated in-place amendment for everything else. The consequence that named only a superseding ADR as the fix follows. The authority for this process change is the maintainer sign-off carried by this pull request's rulings; no governance ADR is created. The core decision, that `docs/` is the canonical design record and every PR is reviewed against it, stands.
