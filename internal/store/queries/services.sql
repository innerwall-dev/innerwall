-- Named service definitions: reusable protocol/port sets (ADR-0018).

-- name: CreateService :exec
INSERT INTO services (id, name, created_at, updated_at)
VALUES ($1, $2, $3, $3);

-- Conditional writes: expected is the version token the caller last read,
-- compared byte-exact against the stored version's decimal form, or NULL
-- for an unconditional write. Every write advances the version by one.
-- name: UpdateService :one
UPDATE services
SET name = $2, updated_at = $3, version = version + 1
WHERE id = $1 AND (sqlc.narg(expected)::text IS NULL OR version::text = sqlc.narg(expected)::text)
RETURNING version;

-- name: DeleteService :execrows
DELETE FROM services
WHERE id = $1 AND (sqlc.narg(expected)::text IS NULL OR version::text = sqlc.narg(expected)::text);

-- name: GetService :one
SELECT * FROM services
WHERE id = $1;

-- name: ListServices :many
SELECT * FROM services
ORDER BY name, id;

-- name: AddServiceEntry :exec
INSERT INTO service_entries (service_id, ordinal, protocol, port_start, port_end)
VALUES ($1, $2, $3, $4, $5);

-- name: DeleteServiceEntries :exec
DELETE FROM service_entries
WHERE service_id = $1;

-- name: ListServiceEntries :many
SELECT * FROM service_entries
WHERE service_id = $1
ORDER BY ordinal;

-- name: ListAllServiceEntries :many
SELECT * FROM service_entries
ORDER BY service_id, ordinal;

-- name: CountRulesReferencingService :one
SELECT count(*) FROM rule_service_refs
WHERE service_id = $1;
