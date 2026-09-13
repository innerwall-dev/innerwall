# ADR-0007: Protobuf is the API source of truth; gRPC for agents, REST/JSON façade for everyone else

**Status:** Accepted

## Context

Two very different clients consume the control plane: agents (long-lived streams, binary efficiency, strict versioning) and humans/automation (curl-ability, browser access, ecosystem familiarity). Maintaining two independently defined APIs guarantees drift; forcing either client onto the other's ideal transport punishes one of them.

## Decision

- The agent contract's types and services are **defined in protobuf** — one schema, one source of truth for everything an agent and the control plane say to each other. *(Amended 2026-09-13; see Amendments.)*
- Agents speak **gRPC over mTLS** (the streaming protocol of ADR-0002).
- The UI and automation consume a **REST/JSON surface of hand-shaped HTTP handlers over the shared domain layer** (ADR-0021). The handlers carry the semantics a browser-facing surface needs and a generated façade cannot express (sessions and cookies, problem documents, conditional requests, blast-radius guards on writes); they contain no domain logic, and every one of them calls the same domain functions the command line calls, which is where drift protection lives. *(Amended 2026-09-13; see Amendments.)*
- The embedded UI uses only this public surface — anything the UI can do, automation can do, by construction.
- Surface design rules for estate scale: aggregation is server-side, never assembled client-side from raw rows; unbounded reads paginate by cursor; a mutation that fans out returns a recorded intent immediately and never blocks on convergence — convergence is observed through the read model, not through a job resource; bulk operations exist only where the console's screens demand them, each with its own record. *(Amended 2026-09-13; see Amendments.)*

## Consequences

- Drift between the agent contract and its implementation is structurally impossible; drift between the human surface and the domain is prevented by the surface having no logic of its own, which review defends at the handler boundary.
- Breaking changes to the agent contract are visible as proto diffs and gated in review; changes to the human surface are visible as handler diffs and reviewed against ADR-0021's transport rules.
- The REST surface is free to be idiomatic for browsers and scripts rather than shaped by a generator.
- Proto toolchain (buf) becomes part of the build.

## Amendments

- **2026-09-13 (PR #9, ADR-0021; maintainer ruling).** The decision that the REST/JSON surface is generated from the protobuf definitions is narrowed to the agent contract: protobuf remains the source of truth for everything agents and the control plane exchange, and the human surface is hand-shaped HTTP handlers over the shared domain layer instead of a generated façade. The reason is that the operator-surface semantics settled in the M3 design (browser sessions and cookies, problem documents, conditional requests, blast-radius guards) are properties of the transport that a generated façade cannot express, and expressing them around a generator costs more than writing the handlers. Drift protection moves from a shared schema to shared domain functions: the command line and the surface call the same functions, and a handler that holds domain logic is the reviewable defect. The principle that the console uses only the public surface carries into ADR-0021 unchanged, where decision 1 states it for the surface's own record. The surface design rules bullet is corrected in the same ruling: the earlier text mandated bulk endpoints, cursor pagination everywhere, and an asynchronous job pattern in which a fan-out mutation returns a job identifier, which contradicts the settled M3 design; the bulk mode change returns a recorded intent with no job resource and convergence is observed through the read model (applied against latest), generic bulk operations are out of version 1, and aggregate reads return bounded rollups computed server-side. The decision and consequence bullets are corrected in place; prior text is in git history.
