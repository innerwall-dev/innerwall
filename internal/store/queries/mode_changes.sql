-- Recorded bulk mode changes (ADR-0007 as amended): the intent as
-- submitted, the set it resolved to, and what changed. Written inside the
-- render transaction that flips the modes, so the record and the flip are
-- one unit.

-- name: CreateModeChange :exec
INSERT INTO mode_changes (id, created_at, target_mode, selector, expected_match_count, matched, desired_updated)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: AddModeChangeWorkload :exec
INSERT INTO mode_change_workloads (mode_change_id, workload_id, previous_mode)
VALUES ($1, $2, $3);

-- name: GetModeChange :one
SELECT * FROM mode_changes
WHERE id = $1;

-- name: ListModeChangeWorkloads :many
SELECT * FROM mode_change_workloads
WHERE mode_change_id = $1
ORDER BY workload_id;

-- The modes of a resolved set before it is flipped, and the flip itself,
-- by explicit id: the set was resolved once, in this transaction, and a
-- label change committed meanwhile does not move it.
-- name: GetWorkloadModes :many
SELECT id, mode FROM workloads
WHERE id = ANY(sqlc.arg(workload_ids)::uuid[])
ORDER BY id;

-- name: SetWorkloadModes :execrows
UPDATE workloads
SET mode = sqlc.arg(mode)::integer
WHERE id = ANY(sqlc.arg(workload_ids)::uuid[]) AND mode <> sqlc.arg(mode)::integer;
