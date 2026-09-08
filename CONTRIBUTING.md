# Contributing to Innerwall

Thanks for considering a contribution. Innerwall is early; the most valuable contributions right now are issues from real environments, docs fixes, and focused PRs.

## Before you write code

1. Read `ARCHITECTURE.md`, then skim `docs/adr/` — the ADRs are the project's source of truth, and PRs are reviewed against them (including by an automated pass).
2. For anything beyond a small fix, **open an issue first**. Agreeing on the shape before the diff exists saves everyone time.
3. If your change contradicts an Accepted ADR, it needs a superseding ADR in the same PR. That's not bureaucracy — it's how design intent survives contributors and coding agents alike.

## Developer Certificate of Origin

Every commit must be signed off (`git commit -s`), certifying the [DCO](https://developercertificate.org/). No CLA. CI enforces the `Signed-off-by` trailer.

## PR flow

- Fork, branch, keep PRs focused — one logical change each.
- `make build test lint` must pass locally; CI additionally checks generated-code drift (`make proto`, `make sqlc`) and DCO.
- Never hand-edit generated code; change the source (`proto/`, `internal/store/queries/`) and regenerate.
- UI changes: Biome is the only formatter/linter (`ui/biome.json`).
- Automated review may comment on ADR conformance; treat it like any reviewer — respond, fix, or argue with a superseding ADR.

## Style notes

- Go: `golangci-lint` config in the repo is the ruleset; no debates in PRs.
- Docs and comments follow the project's framing rules: project terminology only, first-principles reasoning, no references to other products or companies.
- The Makefile stays thin: targets wrap tools; logic lives in `scripts/`.

## Security issues

Do **not** open public issues for vulnerabilities — see `SECURITY.md`.
