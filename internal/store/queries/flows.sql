-- Flow storage (ADR-0009, ADR-0019). These are the only statements that
-- touch flow_windows and flow_totals; every caller goes through the
-- FlowStore interface in internal/flowstore.

-- name: InsertFlowWindows :copyfrom
INSERT INTO flow_windows (
    workload_id, window_start, window_end,
    peer_kind, peer_key, peer_labels,
    src_address, dst_address, dst_port, protocol, direction, decision, matched_rule_id,
    connection_count, byte_count, first_seen, last_seen, process_name
) VALUES (
    $1, $2, $3,
    $4, $5, $6,
    $7, $8, $9, $10, $11, $12, $13,
    $14, $15, $16, $17, $18
);

-- name: UpsertFlowTotal :batchexec
INSERT INTO flow_totals (
    workload_id, peer_kind, peer_key, peer_labels,
    dst_port, protocol, direction, decision, matched_rule_id,
    first_seen, last_seen, connection_count, byte_count, window_count
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8, $9,
    $10, $11, $12, $13, 1
)
ON CONFLICT (workload_id, peer_kind, peer_key, dst_port, protocol, direction, decision) DO UPDATE
SET peer_labels      = EXCLUDED.peer_labels,
    matched_rule_id  = EXCLUDED.matched_rule_id,
    first_seen       = LEAST(flow_totals.first_seen, EXCLUDED.first_seen),
    last_seen        = GREATEST(flow_totals.last_seen, EXCLUDED.last_seen),
    connection_count = flow_totals.connection_count + EXCLUDED.connection_count,
    byte_count       = flow_totals.byte_count + EXCLUDED.byte_count,
    window_count     = flow_totals.window_count + 1;

-- name: ListFlowWindows :many
SELECT * FROM flow_windows
WHERE workload_id = sqlc.arg(workload_id)
  AND window_start >= sqlc.arg(since)
  AND window_start < sqlc.arg(until)
  AND (sqlc.arg(decision)::integer = 0 OR decision = sqlc.arg(decision)::integer)
ORDER BY window_start DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- The rollup the operator console issues for a label scope: the scope is
-- resolved to workload ids by the caller, and the rows are grouped by
-- resolved peer and service (destination port and protocol). A decision of
-- 0 means every decision.
-- name: RollupFlowWindows :many
SELECT peer_kind, peer_key, dst_port, protocol, decision,
       count(DISTINCT workload_id)::bigint AS workload_count,
       sum(connection_count)::bigint       AS connection_count,
       sum(byte_count)::bigint             AS byte_count,
       min(first_seen)::timestamptz        AS first_seen,
       max(last_seen)::timestamptz         AS last_seen
FROM flow_windows
WHERE workload_id = ANY(sqlc.arg(workload_ids)::uuid[])
  AND window_start >= sqlc.arg(since)
  AND window_start < sqlc.arg(until)
  AND (sqlc.arg(decision)::integer = 0 OR decision = sqlc.arg(decision)::integer)
GROUP BY peer_kind, peer_key, dst_port, protocol, decision
ORDER BY connection_count DESC, peer_kind, peer_key, dst_port, protocol, decision;

-- name: ListFlowTotals :many
SELECT * FROM flow_totals
WHERE workload_id = sqlc.arg(workload_id)
  AND (sqlc.arg(decision)::integer = 0 OR decision = sqlc.arg(decision)::integer)
ORDER BY last_seen DESC, peer_kind, peer_key, dst_port, protocol, decision;

-- Retention: one bounded batch of the oldest windows before the horizon.
-- The caller loops until a batch deletes nothing, so no single statement
-- holds locks for the whole backlog (ADR-0019).
-- name: DeleteFlowWindowsBefore :execrows
DELETE FROM flow_windows
WHERE id IN (
    SELECT oldest.id FROM flow_windows AS oldest
    WHERE oldest.window_start < sqlc.arg(horizon)
    ORDER BY oldest.window_start
    LIMIT sqlc.arg(batch_size)
);

-- Retention runs singly across replicas: whichever replica acquires the
-- lock prunes, the others skip this round (ADR-0017).
-- name: TryAcquireRetentionLock :one
SELECT pg_try_advisory_xact_lock(sqlc.arg(key)::bigint);

-- name: CountFlowWindows :one
SELECT count(*) FROM flow_windows;
