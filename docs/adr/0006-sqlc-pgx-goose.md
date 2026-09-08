# ADR-0006: sqlc + pgx for data access, goose for migrations — no ORM

**Status:** Accepted

## Context

The schema is relational and the queries are knowable in advance. ORMs trade SQL transparency for object-mapping convenience, generate queries that are hard to review, and fight hand-tuned access patterns (partitioned flow tables, bulk upserts, advisory locks, LISTEN/NOTIFY). This codebase is also built for heavy AI-assisted implementation, which favors explicit, reviewable artifacts over runtime magic.

## Decision

- **sqlc** generates type-safe Go from hand-written SQL; **pgx** is the driver.
- **goose** manages migrations as ordered SQL files in the repo.
- The full data layer — schema, queries, generated code — is committed and reviewable. No query builders, no runtime SQL generation.

## Consequences

- Every query that will run in production is literally in the repo and diffable in PRs.
- Postgres-specific features are first-class rather than fought for.
- Boilerplate for trivial CRUD is accepted; sqlc generation absorbs most of it.
- This commits the relational layer to Postgres — consistent with ADR-0005 and ADR-0009.
