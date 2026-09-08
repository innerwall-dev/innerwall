# ADR-0014: docs/ is the shared source of truth; PRs are reviewed against ADRs, including by automated review

**Status:** Accepted

## Context

The project is designed in conversational sessions, prototyped with design tools, and implemented largely with AI coding agents — plus, eventually, human contributors. Multiple actors with no shared memory need one durable place where intent lives, or the codebase drifts from its own architecture one plausible-looking PR at a time.

## Decision

- **The repo's `docs/` tree — this ADR set, ARCHITECTURE.md, and CLAUDE.md — is the canonical design record.** Sessions and tools produce decisions; decisions are only real once landed in `docs/`.
- ADRs are immutable once Accepted; changes happen by superseding ADR.
- CI runs **claude-code-action** in two modes: `@claude` interactive assistance on PRs, and an automated review pass that evaluates every PR **against the Accepted ADRs** and flags contradictions.
- CLAUDE.md carries the standing implementation instructions (conventions, commands, ADR pointers) that any coding agent — or new human — reads first.

## Consequences

- Architectural intent survives across tools, sessions, and contributors by construction.
- A PR that violates an ADR gets flagged mechanically; the fix is either the PR or a superseding ADR, never silent drift.
- Docs gain a maintenance cost equal to code; that cost is the point.
