-- Rendered per-workload policy and its version (ADR-0018). The render
-- transaction holds an advisory lock so that two renders never interleave
-- their version bumps, and it notifies listeners of every changed workload
-- on commit so that the replica holding the workload's stream pushes the
-- change without anything polling (ADR-0002).

-- name: AcquireRenderLock :exec
SELECT pg_advisory_xact_lock(sqlc.arg(key)::bigint);

-- name: ListWorkloadPolicies :many
SELECT * FROM workload_policies
ORDER BY workload_id;

-- name: GetWorkloadPolicy :one
SELECT * FROM workload_policies
WHERE workload_id = $1;

-- name: UpsertWorkloadPolicy :exec
INSERT INTO workload_policies (workload_id, version, policy, rendered_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workload_id) DO UPDATE
SET version = EXCLUDED.version, policy = EXCLUDED.policy, rendered_at = EXCLUDED.rendered_at;

-- name: NotifyPolicyChanged :exec
SELECT pg_notify(sqlc.arg(channel)::text, sqlc.arg(payload)::text);
