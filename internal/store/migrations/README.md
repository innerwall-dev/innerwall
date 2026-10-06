# Migrations

Ordered goose SQL files, applied in filename order and reviewed like code
(ADR-0006). Never edit a migration once it has been applied; add a new one.

"Applied" means present on `main`. Once a file is on `main`, any database may
have recorded it, goose will never run it again, and an edit would leave
databases migrated before and after the edit with different schemas. While a
migration exists only on the branch that adds it, it may be amended in that
pull request before merge. CI enforces the rule: `scripts/check-migrations.sh`
fails a pull request that modifies, renames, or deletes a migration its base
already has, and a push to `main` that does the same.

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
