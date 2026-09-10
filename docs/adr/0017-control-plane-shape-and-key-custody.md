# ADR-0017: Single Go binary, stateless replicas, Postgres for state, and the one exception: signing-key custody

**Status:** Accepted
**Supersedes:** ADR-0005

## Context

ADR-0005 fixed the shape of the control plane: one binary with internal service boundaries, all durable state in Postgres, replicas stateless and interchangeable. Implementing enrollment (ADR-0016) produced the first artifact that does not fit that sentence: the signing authority's private key. The key is durable, it outlives any process, and it must not be in Postgres, because a key readable by everything that can read the database is not a signing boundary. ADR-0005 stated its rule without qualification, so the implementation either contradicts it or the rule needs restating with the exception it always implied. This ADR restates ADR-0005 in full, unchanged except for that exception, so that one record carries the control-plane shape.

## Decision

- One deployable **Go binary** containing internally separated services: API service, agent gateway, policy compiler, flow ingestion, signing authority. Boundaries are package-level interfaces, kept clean enough to split into processes later. The agent binary shares only the packages that define the wire contract and the identity representation; control-plane packages never link into it.
- **All durable state in Postgres**, with one named exception below; replicas are stateless and interchangeable. HA = N replicas behind a load balancer + HA Postgres. Postgres is the availability story.
- **Signing-key custody is the exception.** The private key of the signing authority is operator-provisioned configuration, in the same class as the database connection string and the listener's TLS certificate, and never database state. With the file-backed authority (ADR-0016) the operator creates the authority directory once (`innerwall ca init`) and provisions it identically on every replica; every replica then signs with the same key and verifies against the same bundle, so interchangeability holds exactly as it does for any other identically configured replica. Two replicas that each generate their own authority are misconfigured, not two valid deployments. A backend that holds the key in a secrets manager or a hardware module removes even this from the replica's disk without changing the rule. Nothing else durable lives on a replica.
- A **presence table** (LISTEN/NOTIFY propagation at v1 scale) maps agent → replica so any replica can route pushes to the replica holding the stream.
- The **policy compiler runs singly** via Postgres advisory lock; compilation recomputes only agents affected by a change.
- Supported v1 deployment: single binary + single Postgres (co-located or adjacent), docker-compose. The distributed variants are documented seams, not shipped artifacts.

## Consequences

- Operations stay boring; a single artifact, a single database, and one configuration directory are the whole platform.
- Statelessness makes scaling and upgrades mechanical, at the cost of the presence-table indirection. Provisioning a new replica means copying configuration, the authority directory included, and nothing else.
- LISTEN/NOTIFY and advisory locks tie coordination to Postgres — acceptable, since Postgres is already the availability dependency.
- Internal boundaries must be defended in review; monoliths rot at exactly those seams. The agent binary's dependency set is part of that review.
- The signing key is the highest-value secret the control plane holds and it lives on the operator's terms, not the database's. Backup, rotation, and custody of the authority directory are operator responsibilities until an external key backend takes them over.
