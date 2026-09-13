# Migrations

Ordered goose SQL files, applied in filename order and reviewed like code
(ADR-0006). Never edit a migration once it has been applied anywhere; add a new
one.

Naming: `NNNNN_short_description.sql`, starting at `00001`. Each file carries a
`-- +goose Up` section and a `-- +goose Down` section. The files are embedded
into the control plane and applied by `innerwall migrate`; sqlc compiles the
queries against the same files.

Rules the first migrations must respect:

- `region_id` is reserved on every table that will ever be scoped by region,
  from the first migration, even while a single region is the only deployment
  (ADR-0012).
- Flow tables are column-shaped and keyed by window start; retention is a
  bounded periodic delete, with partitioning as the recorded path when volume
  demands it (ADR-0009, ADR-0019).
- The policy schema carries a direction column even though v1 compiles inbound
  only (ADR-0010).
