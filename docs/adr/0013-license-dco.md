# ADR-0013: Apache-2.0, DCO sign-off, project trademark retained

**Status:** Accepted

## Context

An infrastructure security tool needs a license enterprises can adopt without legal review friction, an express patent grant, and a contribution-provenance mechanism that doesn't gate drive-by contributors behind paperwork. Name control matters independently of code freedom: forks should be free to exist, not free to impersonate.

## Decision

- The entire repository is **Apache-2.0**.
- Contributions require **DCO sign-off** (`Signed-off-by`, enforced in CI). No CLA.
- The **Innerwall name and marks are retained by the project**; the trademark is not licensed by the code license. Forks take the code, not the name.

## Consequences

- Enterprise adoption and dependency-inclusion friction is minimized; the patent grant is explicit.
- Provenance is a per-commit property with near-zero contributor friction.
- Relicensing later would require chasing every contributor — the license choice is effectively permanent; accepted knowingly.
