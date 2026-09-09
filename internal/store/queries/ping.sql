-- Queries are hand-written SQL compiled by sqlc into internal/store/db
-- (ADR-0006). One file per concern; every production query lives here.
--
-- Ping is a connectivity probe that touches no tables. It exists so the data
-- layer is exercised end to end before the first migration lands.

-- name: Ping :one
SELECT 1::int AS one;
