# ADR-0005: Single Go binary, modular monolith, stateless replicas, Postgres

**Status:** Accepted; narrowed by ADR-0016 (signing-key custody)

## Context

The control plane is three differently-shaped systems: policy CRUD (low volume, strong consistency), flow ingestion (high volume, eventually consistent), and an agent connection layer (many long-lived streams, soft state). Deploying them as microservices from day one maximizes operational cost precisely when the project has zero operators; fusing them without internal boundaries guarantees a painful split later.

## Decision

- One deployable **Go binary** containing internally separated services: API service, agent gateway, policy compiler, flow ingestion, CA. Boundaries are package-level interfaces, kept clean enough to split into processes later.
- **All durable state in Postgres**; replicas are stateless and interchangeable. HA = N replicas behind a load balancer + HA Postgres. Postgres is the availability story.
- A **presence table** (LISTEN/NOTIFY propagation at v1 scale) maps agent → replica so any replica can route pushes to the replica holding the stream.
- The **policy compiler runs singly** via Postgres advisory lock; compilation recomputes only agents affected by a change.
- Supported v1 deployment: single binary + single Postgres (co-located or adjacent), docker-compose. The distributed variants are documented seams, not shipped artifacts.

## Consequences

- Operations stay boring; a single artifact and a single database are the whole platform.
- Statelessness makes scaling and upgrades mechanical, at the cost of the presence-table indirection.
- LISTEN/NOTIFY and advisory locks tie coordination to Postgres — acceptable, since Postgres is already the availability dependency.
- Internal boundaries must be defended in review; monoliths rot at exactly those seams.
