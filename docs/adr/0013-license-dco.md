# ADR-0013: Apache-2.0, DCO sign-off, project trademark retained

**Status:** Accepted

## Context

An infrastructure security tool needs a license enterprises can adopt without legal review friction, an express patent grant, and a contribution-provenance mechanism that doesn't gate drive-by contributors behind paperwork. Name control matters independently of code freedom: forks should be free to exist, not free to impersonate.

## Decision

- The entire repository is **Apache-2.0**. Built artifacts that bundle third-party packages, the console's code and fonts, carry those packages under their own licenses, with the license texts beside them. *(Amended 2026-10-06; see Amendments.)*
- Contributions require **DCO sign-off** (`Signed-off-by`, enforced in CI) on every commit a contributor authors. A merge commit that lands a pull request is exempt and CI checks the pull request's own commits, each of which carries its sign-off. No CLA. *(Amended 2026-10-06; see Amendments.)*
- The **Innerwall name and marks are retained by the project**; the trademark is not licensed by the code license. Forks take the code, not the name.

## Consequences

- Enterprise adoption and dependency-inclusion friction is minimized; the patent grant is explicit.
- Provenance is a property of every authored commit, with near-zero contributor friction. A conflict resolution made in a merge commit is reviewed in its pull request but not signed off; that is the accepted cost of the exemption. *(Amended 2026-10-06; see Amendments.)*
- Relicensing later would require chasing every contributor — the license choice is effectively permanent; accepted knowingly.

## Amendments

- **2026-10-06 (PR #25, conformance-audit absorption).** The core decision stands: Apache-2.0, DCO sign-off with no CLA, the name and marks retained. Two subsidiary points are recorded in place. The DCO check has always run over a pull request's non-merge commits (`git rev-list --no-merges`), and every merge on `main` lacks a sign-off; the exemption is now stated rather than implied. The repository is Apache-2.0, but the built console bundles third-party code and font files under their own licenses (the fonts under the SIL Open Font License 1.1); every production build writes their license texts into the console (`THIRD-PARTY-NOTICES.txt` at its root, each font family's license beside the fonts), `THIRD-PARTY-NOTICES.md` at the repository root states the distinction, and the record now draws it too.
