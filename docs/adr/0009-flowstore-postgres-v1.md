# ADR-0009: Postgres-only flow storage in v1, behind a FlowStore interface with a columnar-ready schema

**Status:** Accepted

## Context

Flow telemetry is the one workload that will eventually outgrow a row store: append-heavy, time-windowed, analytical. Columnar stores are the honest endgame — but a second stateful system doubles operational burden for every deployment, including the small ones that are the entire early user base. The volume question is also upstream of the storage question: raw per-connection reporting produces orders of magnitude more data than aggregated reporting.

## Decision

- **Agents aggregate before shipping**: flows roll up to `(src, dst, port, proto, count, bytes)` tuples over a 1–10 minute window. This is the load-bearing volume decision.
- **v1 stores flows in Postgres only**: time-partitioned tables, retention by partition drop. With agent-side aggregation this serves tens of millions of flow records/day.
- All flow access goes through a **`FlowStore` interface**; the schema is deliberately column-shaped and portable so a columnar backend can be added **only when a real estate's scale demands it**, moving the analytical read path off the policy plane wholesale.

## Consequences

- Small and mid-size deployments run one database, full stop.
- The migration path is an implementation of an existing interface, not a redesign.
- Postgres flow queries need discipline (partition pruning, pre-aggregation for map rendering).
- Per-connection forensic granularity is traded away at the agent; the aggregation window is configurable but aggregation itself is not optional.
