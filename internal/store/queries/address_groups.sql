-- Address groups: named CIDR sets for peers that are not managed workloads
-- (ADR-0018).

-- name: CreateAddressGroup :exec
INSERT INTO address_groups (id, name, created_at, updated_at)
VALUES ($1, $2, $3, $3);

-- Conditional writes: expected is the version token the caller last read,
-- compared byte-exact against the stored version's decimal form, or NULL
-- for an unconditional write. Every write advances the version by one.
-- name: UpdateAddressGroup :one
UPDATE address_groups
SET name = $2, updated_at = $3, version = version + 1
WHERE id = $1 AND (sqlc.narg(expected)::text IS NULL OR version::text = sqlc.narg(expected)::text)
RETURNING version;

-- name: DeleteAddressGroup :execrows
DELETE FROM address_groups
WHERE id = $1 AND (sqlc.narg(expected)::text IS NULL OR version::text = sqlc.narg(expected)::text);

-- name: GetAddressGroup :one
SELECT * FROM address_groups
WHERE id = $1;

-- name: ListAddressGroups :many
SELECT * FROM address_groups
ORDER BY name, id;

-- name: AddAddressGroupCIDR :exec
INSERT INTO address_group_cidrs (address_group_id, cidr)
VALUES ($1, $2);

-- name: DeleteAddressGroupCIDRs :exec
DELETE FROM address_group_cidrs
WHERE address_group_id = $1;

-- name: ListAddressGroupCIDRs :many
SELECT * FROM address_group_cidrs
WHERE address_group_id = $1
ORDER BY cidr;

-- name: ListAllAddressGroupCIDRs :many
SELECT * FROM address_group_cidrs
ORDER BY address_group_id, cidr;

-- name: CountRulesReferencingAddressGroup :one
SELECT count(*) FROM rule_peers
WHERE address_group_id = $1;
