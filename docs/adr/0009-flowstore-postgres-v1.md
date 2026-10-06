# ADR-0009: Postgres-only flow storage in v1, behind a FlowStore interface with a columnar-ready schema

**Status:** Accepted

## Context

Flow telemetry is the one workload that will eventually outgrow a row store: append-heavy, time-windowed, analytical. Columnar stores are the honest endgame — but a second stateful system doubles operational burden for every deployment, including the small ones that are the entire early user base. The volume question is also upstream of the storage question: raw per-connection reporting produces orders of magnitude more data than aggregated reporting.

## Decision

- **Agents aggregate before shipping**: flows roll up to `(src, dst, port, proto, count, bytes)` tuples over a reporting window the control plane hands every agent in `SyncConfig`, 60 seconds by default. One to ten minutes is the guidance range for that window, not a bound the protocol enforces. This is the load-bearing volume decision. *(Amended 2026-10-06; see Amendments.)*
- **v1 stores flows in Postgres only**: tables keyed by window start, retention by a bounded periodic delete of aged windows; declarative time partitioning with retention by partition drop is the recorded path when volume makes deletes expensive (ADR-0019). With agent-side aggregation this serves tens of millions of flow records/day. *(Amended 2026-09-13; see Amendments.)*
- All flow access goes through a **`FlowStore` interface**; the schema is deliberately column-shaped and portable so a columnar backend can be added **only when a real estate's scale demands it**, moving the analytical read path off the policy plane wholesale.

## Consequences

- Small and mid-size deployments run one database, full stop.
- The migration path is an implementation of an existing interface, not a redesign.
- Postgres flow queries need discipline (indexes shaped to the queries actually issued, pre-aggregation for map rendering).
- Per-connection forensic granularity is traded away at the agent; the aggregation window is configurable but aggregation itself is not optional.

## Amendments

- **2026-09-13 (ADR-0019, PR #7).** The core decision stands: agents aggregate before shipping, version 1 stores flows in Postgres only, and all access goes through the `FlowStore` interface over a column-shaped schema. The subsidiary point that named time-partitioned tables with retention by partition drop was written before the schema existed and is corrected in place, the same way ADR-0017's affected-set bullet was: the first implementation retains by a bounded, serialized periodic delete keyed on window start, and partition-based retention (declarative time partitioning, retention by partition drop) is the recorded scaling path, a migration behind the same interface taken when volume makes deletes expensive. Maintainer ruling on PR #7: amendment, not supersession. ADR-0019 carries the storage decisions in full.
- **2026-10-06 (PR #25, conformance-audit absorption).** The core decision stands. The decision bullet that stated a 1–10 minute aggregation window is corrected in place to what ships: the window is a value the control plane sets in `SyncConfig` and every agent adopts at its next rotation, 60 seconds by default, which is the bottom of the old range; one to ten minutes is guidance, and the field is not range-checked. The protocol carries any value; in version 0.1.0 the control plane's command line exposes no flag to change it, so changing it is a change to the gateway's default.
