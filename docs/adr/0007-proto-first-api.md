# ADR-0007: Protobuf is the API source of truth; gRPC for agents, REST/JSON façade for everyone else

**Status:** Accepted

## Context

Two very different clients consume the control plane: agents (long-lived streams, binary efficiency, strict versioning) and humans/automation (curl-ability, browser access, ecosystem familiarity). Maintaining two independently defined APIs guarantees drift; forcing either client onto the other's ideal transport punishes one of them.

## Decision

- All API types and services are **defined in protobuf** — one schema, one source of truth.
- Agents speak **gRPC over mTLS** (the streaming protocol of ADR-0002).
- The UI and automation consume a **REST/JSON façade generated from the same protos** (gRPC-gateway pattern). Nothing exists in the REST surface that isn't in the proto definitions.
- The embedded UI uses only this public façade — anything the UI can do, automation can do, by construction.
- Façade design rules for estate-scale automation: bulk endpoints, cursor pagination everywhere, async job pattern for expensive operations (mutations that fan out return a job ID, never block).

## Consequences

- API drift between agent and human surfaces is structurally impossible.
- Breaking changes are visible as proto diffs and gated in review.
- gRPC-gateway conventions constrain REST aesthetics; accepted as the price of one schema.
- Proto toolchain (buf) becomes part of the build.
