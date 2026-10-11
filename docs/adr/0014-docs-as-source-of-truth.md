# ADR-0014: docs/ is the shared source of truth; PRs are reviewed against ADRs, including by automated review

**Status:** Accepted

## Context

The project is designed in conversational sessions, prototyped with design tools, and implemented largely with AI coding agents — plus, eventually, human contributors. Multiple actors with no shared memory need one durable place where intent lives, or the codebase drifts from its own architecture one plausible-looking PR at a time.

## Decision

- **The repo's `docs/` tree — this ADR set, ARCHITECTURE.md, and CLAUDE.md — is the canonical design record.** Sessions and tools produce decisions; decisions are only real once landed in `docs/`.
- ADRs record architecture decisions. The mechanics of changing them (supersession, in-place amendment, and the status lines that record either) are governed by `docs/adr/README.md` under maintainer authority.
- CI runs **claude-code-action** in two modes: `@claude` interactive assistance on PRs, and an automated review pass that evaluates every PR that is ready for review **against the Accepted ADRs** and flags contradictions. A draft is reviewed when it is marked ready; a PR whose every changed file is generated output or a lockfile carries no decision and is not reviewed. The automated pass requires the triggering actor to hold write access to the repository; a PR from any other author ends with a visible "did not post" note in the job summary instead of a review, and an `@claude` mention is the manual path for it. *(Amended 2026-10-06 and 2026-10-11; see Amendments.)*
- CLAUDE.md carries the standing implementation instructions (conventions, commands, ADR pointers) that any coding agent — or new human — reads first.

## Consequences

- Architectural intent survives across tools, sessions, and contributors by construction.
- A PR that violates an ADR gets flagged mechanically; the fix is either the PR or a change to the record under the mechanics `docs/adr/README.md` defines, never silent drift.
- Docs gain a maintenance cost equal to code; that cost is the point.

## Amendments

- **2026-09-13 (PR #5).** The decision bullet that read "ADRs are immutable once Accepted; changes happen by superseding ADR" is corrected in place to delegate: ADRs record architecture decisions, and the mechanics of changing them are governed by `docs/adr/README.md` under maintainer authority, which today defines supersession for a reversed or replaced core decision and dated in-place amendment for everything else. The consequence that named only a superseding ADR as the fix follows. The authority for this process change is the maintainer sign-off carried by this pull request's rulings; no governance ADR is created. The core decision, that `docs/` is the canonical design record and every PR is reviewed against it, stands.
- **2026-10-06 (PR #24, conformance-audit absorption).** The core decision stands. The decision bullet that said the automated pass evaluates every PR is corrected in place to the coverage the workflow has had since the scaffold and since PR #6: it reviews a PR once the PR is ready for review, so a draft is reviewed when marked ready, and it does not run on a PR whose every changed file is generated output or a lockfile. CLAUDE.md, the workflow's own header, the development guide, and the README say the same. The review's list of violation classes, which still named hand-added REST routes absent from `proto/` and unsigned policy bundles, is corrected to the rules ADR-0007 and ADR-0012 as amended state: an operator handler that holds domain logic of its own, and rendered policy that transits anything other than the mutually authenticated sync stream without an authenticity envelope.
- **2026-10-11 (PR #73).** The core decision stands. The decision bullet on the automated review pass gains the condition it runs under: the pass requires the triggering actor to hold write access to the repository. A pull request from any other author, in practice a fork pull request from someone who is not a collaborator, ends with a visible "ADR review did not post" note in the job summary instead of a review, and an `@claude` mention, which runs in base context under the account that wrote it, is the manual path for such a pull request. Extending the automated pass to those pull requests is an open decision, tracked in #72 with the security constraints any such change must meet. CLAUDE.md, the workflow's own header, the development guide, and the README say the same.
