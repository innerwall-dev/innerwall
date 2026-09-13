# CLAUDE.md — standing instructions for coding agents

You are working on **Innerwall**, an open-source microsegmentation platform: host agents observe flows and program native OS firewalls; a control plane compiles label-based policy into per-host rulesets and renders a live dependency map.

## Read order

1. `ARCHITECTURE.md` — the system as designed.
2. `docs/adr/` — the decisions and their reasoning. ADRs are the source of truth; ARCHITECTURE.md describes the system they produce.
3. This file — the rules you must not casually break.

## Hard rules (distilled from Accepted ADRs)

These are the constraints a plausible-looking change is most likely to violate. Each cites its ADR; a change that contradicts one requires a **superseding ADR in the same PR**.

- **No ORM, no query builders, no runtime SQL generation.** Hand-written SQL in `internal/store/queries/`, sqlc-generated Go, goose migrations. Every production query lives in the repo. (ADR-0006)
- **No polling loops anywhere in steady state.** Agents hold one persistent gRPC stream; policy moves as versioned desired-state deltas; reconnects use exponential backoff with full jitter. (ADR-0002)
- **Agents never touch firewall state outside the Innerwall-owned nftables table**, and every ruleset application is one kernel transaction — a full apply replaces the table as a unit; a peer-only change updates a rule's named sets inside the owned table in one transaction; nothing ever edits live rules one at a time or leaves a mixture of versions installed. (ADR-0003, ADR-0020)
- **Agents fail static — never open, never closed.** Last-ACKed policy persists to disk and survives restarts and reboots. The local kill switch must always work without the control plane. (ADR-0011)
- **No agent self-update code.** Not a flag, not a stub, not "for later." (ADR-0011)
- **Flows are aggregated at the agent** into `(src, dst, port, proto, count, bytes)` windows before shipping. Never ship per-connection records. (ADR-0009)
- **All flow reads/writes go through the `FlowStore` interface.** No direct SQL against flow tables outside its Postgres implementation. (ADR-0009)
- **The UI talks only to the public REST façade.** No private endpoints, no backchannel, no direct DB access from anything in `ui/`. (ADR-0007, ADR-0008)
- **All API surface is defined in `proto/` first.** No hand-added REST routes; the façade is generated. Breaking proto changes fail CI without an ADR reference. (ADR-0007)
- **Policy compilation is inbound-only in v1.** The schema reserves direction; the compiler must not emit outbound rules. (ADR-0010)
- **Certificates carry identity only:** exactly one URI SAN, `innerwall://workload/<uuid>`, with a control-plane-assigned UUID, formatted and parsed by `internal/identity` alone. Labels, hostnames, and other mutable attributes never go in certs. A signing request contributes only its public key; identity is read from the verified connection credential and nowhere else. (ADR-0016)
- **Statelessness is load-bearing:** no control-plane replica may hold state that matters beyond its process; anything durable goes in Postgres. The one exception is the signing authority's key, which is operator-provisioned configuration identical on every replica, never database state. (ADR-0017)
- **Rendered policy carries an authenticity envelope before it transits anything other than the mutually authenticated sync stream** (relays, caches, regional intermediaries); signing is deferred until then, and that condition is the tripwire. `region_id` stays in the schema even while unused. These seams are cheap now and expensive later. (ADR-0012, ADR-0018)
- **A workload's policy version advances only when its rendered output changes.** Deltas are the diff of consecutive rendered policies, computed by the one shared `internal/rendered` implementation, and the snapshot-plus-delta property test is the contract. Rendered policy and versions live in Postgres; a stream is served the persisted artifact, and connecting is not a render trigger (the one exception is repairing a workload that has no persisted policy at all). (ADR-0018)
- **The Makefile stays dumb.** Targets are 1–3-line wrappers over real tools; anything with logic becomes a script in `scripts/` that a target calls.

## Vocabulary and framing

- Project terminology only: **provisioning token**, **agent**, **control plane**, **workload**, **label**, **signing authority**.
- All docs, comments, commit messages, and identifiers argue from **first principles**. No references to other products, companies, or their terminology — anywhere in the repo. If a design needs motivation, derive it (e.g. "central planes fail by being chatty"), don't compare.

## Commands

- `make build` / `make test` / `make lint` — the whole loop. `make dev` runs the compose stack (control plane + Postgres).
- `make proto` regenerates from `proto/` (buf); `make sqlc` regenerates the store. **Never edit generated code** — CI regenerates and fails on drift.
- UI: `make ui` builds; Biome handles lint + format in `ui/` (one `biome.json`, no eslint/prettier).
- Migrations: new goose file in `internal/store/migrations/`, never edit an applied one.

## Change protocol

- An ADR's core decision is immutable once Accepted; reversing or replacing it takes a superseding ADR (next number, `Supersedes: ADR-XXXX` header, old one marked Superseded, maintainer sign-off named in the PR). Additive detail, clarification, narrowing, or correction of a subsidiary point is a dated amendment note in the ADR itself, never a restatement. See `docs/adr/README.md`.
- CI runs an automated review of every PR against the Accepted ADRs. If it flags your change, the fix is either the change or a superseding ADR — never silent drift.
- Commits require DCO sign-off (`git commit -s`). (ADR-0013)
