# Development

One page: what to install, what the make targets do, how to regenerate code, and where things live. Written against what the repository contains today.

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | as pinned in `go.mod` | Both binaries; also runs the protoc plugins via `go tool` |
| Node.js + npm | 22.x | Builds and lints the UI in `ui/` |
| buf | 1.72.0 | Proto lint, breaking-change detection, code generation |
| sqlc | 1.31.1 | Compiles `internal/store/queries/*.sql` into Go |
| goose | 3.28.0 | Applies `internal/store/migrations/` |
| golangci-lint | 2.5.0 | Go lint; the config in `.golangci.yml` is the ruleset |
| docker compose | any recent | `make dev` |

`make tools` installs buf, sqlc, goose, and golangci-lint at the pinned versions into `$(go env GOPATH)/bin`. The protoc plugins (`protoc-gen-go`, `protoc-gen-go-grpc`, `protoc-gen-grpc-gateway`) are `tool` directives in `go.mod`; they need no separate install. Biome comes with `npm ci` in `ui/`.

The versions above are mirrored in `.github/workflows/ci.yml`. Keep the two in sync, otherwise local regeneration and the CI drift check disagree.

## Make targets

The Makefile is a thin dispatcher. Every target wraps a tool in one to three lines; anything with logic is a script in `scripts/`.

| Target | Does |
|---|---|
| `make build` | Static (CGO disabled) build of both binaries into `bin/` |
| `make test` | `go test -race ./...` |
| `make lint` | golangci-lint, `buf lint`, and Biome (`biome ci`) over `ui/` |
| `make proto` | `buf generate` from `proto/` into `internal/gen/` |
| `make sqlc` | `sqlc generate` from `internal/store/queries/` into `internal/store/db/` |
| `make ui` | `npm ci` and a production Vite build into `ui/dist/` |
| `make dev` | `docker compose up --build`: control plane + Postgres |
| `make drift` | Regenerates everything and fails on any diff (what CI runs) |
| `make tools` | Installs the pinned CLI toolchain |

`make build test lint` is the loop before a PR. CI additionally runs `make drift`, the proto breaking-change gate, and the DCO check.

## Regenerating code

Never edit generated code. Change the source, regenerate, commit both.

- **Protobuf → Go.** Edit `proto/innerwall/v1/*.proto`, run `make proto`. Output lands in `internal/gen/innerwall/v1/`. `buf.yaml` at the repo root declares `proto/` as the module root, so package `innerwall.v1` lives at `proto/innerwall/v1/` and files import as `innerwall/v1/<name>.proto`. `buf.gen.yaml` runs the plugins through `go tool`, so the output is pinned by `go.sum`. The contract itself is recorded in ADR-0015.
- **SQL → Go.** Edit or add `internal/store/queries/*.sql`, run `make sqlc`. Output lands in `internal/store/db/`. `sqlc.yaml` at the repo root builds the schema from `internal/store/migrations/`, so a query can only reference tables a migration creates.
- **Migrations.** Add a new goose file in `internal/store/migrations/` (`NNNNN_description.sql`, `-- +goose Up` / `-- +goose Down`). Never edit an applied one. See the README in that directory for the schema rules the ADRs impose.
- **UI.** `make ui` builds `ui/dist/`, which `ui/embed.go` embeds into the control-plane binary. `ui/dist/.gitkeep` is tracked (and copied back in from `ui/public/` on every build) so the embed directory is never empty on a fresh checkout.

## CI

`.github/workflows/ci.yml` runs on every push to `main` and every PR:

- `build`, `test`, `lint` (Go + Biome), `ui` (production build)
- `proto`: `buf lint`, `buf build`, and `scripts/check-proto-breaking.sh`, which runs `buf breaking` against `main` and fails unless the PR title or body references an ADR (`ADR-NNNN`)
- `drift`: `scripts/check-drift.sh`, which runs `make proto sqlc` and fails on any diff or untracked generated file
- `dco`: `scripts/check-dco.sh`, which requires a `Signed-off-by` trailer on every commit in the PR

`.github/workflows/claude.yml` runs the coding-agent jobs: `@claude` mentions on issues and PRs, and an advisory ADR-conformance review of every PR that posts findings as a sticky PR comment and never fails the build. Both authenticate through workload identity federation; there is no static credential in the workflow.

## Where things live

```
cmd/innerwall/            control-plane main
cmd/innerwall-agent/      agent main
internal/api              REST/JSON façade (generated gateway + handlers)
internal/gateway          agent gRPC streams, presence
internal/compiler         label rules → per-agent versioned rulesets
internal/ingest           flow enrichment, bidirectional dedupe
internal/flowstore        FlowStore interface + Postgres implementation
internal/ca               CertificateAuthority interface + embedded CA
internal/enroll           enrollment policies, join tokens
internal/registry         workloads, agents, labels
internal/store            queries/ (SQL), migrations/ (goose), db/ (sqlc output)
internal/agent            sync/, collect/, enforce/, health/
internal/gen              buf output (generated; never edited)
proto/innerwall/v1        the API contract (buf.yaml and buf.gen.yaml at the repo root)
ui/                       Vite + React SPA; src/{map,policy,simulate,inventory,enroll}
ui/embed.go               go:embed of ui/dist into the control plane
deploy/install.sh         agent installer (join token in)
scripts/                  everything the Makefile calls that has logic
docs/adr                  decisions; docs/deploy: running it; docs/img: diagrams
```

## Commits

Every commit is signed off: `git commit -s`. CI rejects PRs with an unsigned commit (ADR-0013).
