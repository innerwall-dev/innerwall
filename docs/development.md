# Development

One page: what to install, what the make targets do, how to regenerate code, and where things live. Written against what the repository contains today.

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | as pinned in `go.mod` | Both binaries; also runs the protoc plugins via `go tool` |
| Node.js + npm | 22.x | Builds, lints, and tests the operator console in `ui/` |
| buf | 1.72.0 | Proto lint, breaking-change detection, code generation |
| sqlc | 1.31.1 | Compiles `internal/store/queries/*.sql` into Go |
| goose | 3.28.0 | Applies `internal/store/migrations/` |
| golangci-lint | 2.5.0 | Go lint; the config in `.golangci.yml` is the ruleset |
| docker compose | any recent | `make dev` |

`make tools` installs buf, sqlc, goose, and golangci-lint at the pinned versions into `$(go env GOPATH)/bin`. The protoc plugins (`protoc-gen-go`, `protoc-gen-go-grpc`, `protoc-gen-grpc-gateway`) are `tool` directives in `go.mod`; they need no separate install. Biome, Vitest, and the console's whole toolchain come with `npm ci` in `ui/`. Node is needed only to build or test the console: a Go build with `-tags noconsole` compiles a stub in place of the embedded console and never touches `ui/`.

The versions above are mirrored in `.github/workflows/ci.yml`. Keep the two in sync, otherwise local regeneration and the CI drift check disagree.

## Make targets

The Makefile is a thin dispatcher. Every target wraps a tool in one to three lines; anything with logic is a script in `scripts/`.

| Target | Does |
|---|---|
| `make build` | Static (CGO disabled) build of both binaries into `bin/`, embedding whatever `ui/dist/` holds |
| `make build-noconsole` | The same build with the `noconsole` tag: the console stub, no Node needed |
| `make test` | `go test -race ./...`, plus the `noconsole` stub's own test |
| `make test-console` | Vitest over the console (`npm --prefix ui run test`) |
| `make lint` | golangci-lint, `buf lint`, and Biome (`biome ci`) over `ui/` |
| `make openapi` | Lints the operator surface contract, `api/openapi.yaml` (spec lint only) |
| `make proto` | `buf generate` from `proto/` into `internal/gen/` |
| `make sqlc` | `sqlc generate` from `internal/store/queries/` into `internal/store/db/` |
| `make console-api` | Generates the console's types, `ui/src/api/generated.ts`, from `api/openapi.yaml` |
| `make console` | `npm ci` and a production Vite build of the console into `ui/dist/` (`make ui` is the same target) |
| `make dev` | `docker compose up --build`: control plane + Postgres |
| `make seed` | Loads the review fleet into `$INNERWALL_DATABASE_URL` (`innerwall dev seed`, built with `-tags dev`); see below |
| `make migrate` | Applies pending migrations to `$INNERWALL_DATABASE_URL` (`innerwall migrate`) |
| `make drift` | Regenerates everything and fails on any diff (what CI runs) |
| `make tools` | Installs the pinned CLI toolchain |

`make build test lint` is the loop before a PR. CI additionally runs `make drift`, the proto breaking-change gate, and the DCO check.

## Regenerating code

Never edit generated code. Change the source, regenerate, commit both.

- **Protobuf → Go.** Edit `proto/innerwall/v1/*.proto`, run `make proto`. Output lands in `internal/gen/innerwall/v1/`. `buf.yaml` at the repo root declares `proto/` as the module root, so package `innerwall.v1` lives at `proto/innerwall/v1/` and files import as `innerwall/v1/<name>.proto`. `buf.gen.yaml` runs the plugins through `go tool`, so the output is pinned by `go.sum`. The contract itself is recorded in ADR-0015.
- **SQL → Go.** Edit or add `internal/store/queries/*.sql`, run `make sqlc`. Output lands in `internal/store/db/`. `sqlc.yaml` at the repo root builds the schema from `internal/store/migrations/`, so a query can only reference tables a migration creates.
- **Operator surface → console types.** Edit `api/openapi.yaml`, run `make console-api` (after `npm --prefix ui ci`). Output lands in `ui/src/api/generated.ts`, which is excluded from Biome like every generated file. The console names the shapes it uses in `ui/src/api/schema.ts`, as aliases into the generated file and nothing more, so the description stays the one contract.
- **Migrations.** Add a new goose file in `internal/store/migrations/` (`NNNNN_description.sql`, `-- +goose Up` / `-- +goose Down`). Never edit an applied one. See the README in that directory for the schema rules the ADRs impose. The files are embedded into the control plane and applied by `innerwall migrate` (or `serve --migrate`) through the goose library, so the binary carries its own schema; the goose CLI is for inspecting and rolling back.
- **Console.** `make console` builds `ui/dist/`, which `ui/embed.go` embeds into the control-plane binary; the operator listener serves it at every path outside `/api/v1` (ADR-0008, ADR-0021). `ui/dist/.gitkeep` is tracked (and copied back in from `ui/public/` on every build) so the embed directory is never empty on a fresh checkout; a binary built from a checkout where the console has not been built answers those paths with a problem document saying so. `ui/scripts/generate-api.mjs` generates `ui/src/api/generated.ts` from `api/openapi.yaml`, before every console build and under `make console-api`; the generated file is committed and is the console's contract with the surface.

## The console

`ui/` is a Vite + React + TypeScript application. The palette is `ui/src/tokens.css`, its token names and values adopted verbatim from the design package and never restyled: every color in the stylesheet is a plain `var()` reference to a token, the dark theme is the `.dark` class on the root element, and the preference lives in `localStorage`. Do not let component tooling regenerate the stylesheet into a channel-based color convention; that silently breaks every color. Type is IBM Plex Sans and Mono, bundled from their packages, so the console makes no request outside its own origin. Components under `ui/src/components/ui/` follow the shape component tooling emits, with `components.json` pointing at the stylesheet, so a generated component drops in; the palette stays the tokens.

The review loop against a running control plane, from a checkout with Postgres reachable:

```sh
export INNERWALL_DATABASE_URL=postgres://innerwall:innerwall@127.0.0.1:5432/innerwall?sslmode=disable
make seed                                   # resets the database and loads the review fleet
go run ./cmd/innerwall operator set-password --name "A. Rao"
make console                                # builds ui/dist/
go run ./cmd/innerwall serve --init-ca --site iad1
```

Then open `https://127.0.0.1:8080/` and accept the generated certificate (its fingerprint is in the startup log). The seed is the fleet the store tests use (`internal/storetest`): three workloads in three states, a ruleset rendered onto them, and two windows of flows, so the screens show the same states the tests assert on. The `dev` subcommand is compiled in only with `-tags dev`, and `make seed` builds it that way; it resets every table, so point it only at a disposable database. The operator password is not part of the seed.

For iterating on the console itself, `npm --prefix ui run dev` serves it with hot reload and proxies `/api` to the control plane (`INNERWALL_OPERATOR_URL`, default `https://127.0.0.1:8080`), so the cookie and the origin guard behave as in production. `npm --prefix ui run test` runs Vitest; `npm --prefix ui run lint` runs Biome. Screen review is against both themes: `docs/img/console/` holds the scaffold's states beside the design shots.

## CI

`.github/workflows/ci.yml` runs on every push to `main` and every PR:

- `console` (Biome, Vitest, production build on the pinned Node) runs first and hands its `ui/dist/` to `build`, which embeds it, then proves the `noconsole` stub and the `dev` command compile; `test`, `lint` (Go). The `test` job runs a Postgres service container and sets `INNERWALL_TEST_DATABASE_URL`; tests that need a database skip when it is unset, so `make test` works offline and runs the integration tests when you point that variable at a disposable database. Database tests hold a session-level advisory lock for their duration, so the packages `go test` runs in parallel take turns on the one database rather than truncating each other's tables.
- `openapi`: `scripts/check-openapi.sh`, which lints `api/openapi.yaml` as OpenAPI 3.1 against the rules in `api/vacuum-ruleset.yaml`. That is a check on the document alone; a Go test in `internal/api` checks the document against the routes the surface mounts and the closed sets it names. Nothing conforms responses to the document at runtime.
- `proto`: `buf lint`, `buf build`, and `scripts/check-proto-breaking.sh`, which runs `buf breaking` against `main` and fails unless the PR title or body references an ADR (`ADR-NNNN`)
- `drift`: `scripts/check-drift.sh`, which runs `make proto sqlc console-api` and fails on any diff or untracked generated file
- `dco`: `scripts/check-dco.sh`, which requires a `Signed-off-by` trailer on every commit in the PR

`.github/workflows/claude.yml` runs the coding-agent jobs: `@claude` mentions on issues and PRs, and an advisory ADR-conformance review of every PR that posts findings as a sticky PR comment and never fails the build. Both authenticate through workload identity federation; there is no static credential in the workflow.

## Where things live

```
cmd/innerwall/            control-plane main
cmd/innerwall-agent/      agent main
api/openapi.yaml          the operator surface contract, hand-authored OpenAPI 3.1; api/vacuum-ruleset.yaml is its lint ruleset
internal/api              operator surface: REST/JSON handlers (reads and writes), auth middleware, listener TLS
internal/readmodel        operator read model: rollups, flow pages, workloads, rendered policy (shared by the surface and the command line); readmodeltest/ holds its doubles
internal/fleet            operator write domain over the registry and the renderer: label edits, bulk mode changes, selector preview, dry-run render; fleettest/ holds an in-memory store for it and for policy
internal/gateway          agent gRPC surface: enrollment, renewal, the sync stream and its pushes
internal/compiler         renders the authored model into per-workload policies; versions by diff
internal/policy           authored model (services, address groups, rulesets), admission, documents
internal/rendered         rendered-model algebra: canonical form, Diff, Apply (shared with the agent)
internal/ingest           flow enrichment, bidirectional dedupe
internal/flowstore        FlowStore interface + Postgres implementation
internal/ca               Authority interface; fileca/ is the file-backed implementation
internal/identity         workload identity and its URI SAN form (the only place it is built or parsed)
internal/enroll           provisioning tokens, enrollment, renewal
internal/registry         workloads: labels, mode, facts and addresses, sync status
internal/store            queries/ (SQL), migrations/ (goose), db/ (sqlc output); storetest/ opens a test database and seeds the review fleet (also behind `innerwall dev seed`)
internal/agent            credential/ (enroll, renew, holder, renewal timer), sync/ (daemon), enforce/ (policy store), inventory/, collect/, health/
internal/gen              buf output (generated; never edited)
proto/innerwall/v1        the API contract (buf.yaml and buf.gen.yaml at the repo root)
ui/                       the operator console: Vite + React + TypeScript; src/{api,auth,theme,shell,routes,components}
ui/embed.go               go:embed of ui/dist into the control plane; embed_noconsole.go is the stub behind the noconsole tag
ui/src/tokens.css         the design tokens, names and values verbatim; src/index.css maps them onto utilities
deploy/install.sh         agent installer (provisioning token in)
scripts/                  everything the Makefile calls that has logic
docs/adr                  decisions; docs/deploy: running it; docs/img: diagrams
```

## Commits

Every commit is signed off: `git commit -s`. CI rejects PRs with an unsigned commit (ADR-0013).
