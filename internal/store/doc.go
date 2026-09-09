// Package store is the relational data layer: hand-written SQL in queries/,
// goose migrations in migrations/, and sqlc-generated Go in db/. There is no
// ORM, no query builder, and no runtime SQL generation; every production query
// is in the repository and diffable (ADR-0006). region_id is reserved in the
// schema from the first migration (ADR-0012).
package store
