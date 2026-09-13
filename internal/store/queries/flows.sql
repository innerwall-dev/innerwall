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

-- --- operator read model -----------------------------------------------------
--
-- The rollups the operator surface and the command line issue (ADR-0007 as
-- amended, ADR-0019 decision 4). Each grouping the surface offers is one
-- named statement over the same windows; the caller picks the statement
-- and never assembles one. Every rollup shares one filter convention: an
-- empty workload id array means every workload, a zero decision or
-- direction means any, and a zero protocol means every service (a service
-- is one destination port and protocol). Ordering is by connection count
-- unless order_by is 'recent', in which case the most recently seen group
-- comes first. The window bounds actually covered, the number of groups,
-- and the totals across every group ride on each row as window aggregates,
-- so a truncated result still says how it relates to the whole. The
-- decision-and-time and workload-and-time indexes serve all four.

-- Grouped by the resolved rule that admitted the traffic; records with no
-- matched rule form the group with the empty rule id.
-- name: RollupFlowsByRule :many
SELECT matched_rule_id,
       count(*)::bigint                            AS flow_count,
       sum(connection_count)::bigint               AS connection_count,
       sum(byte_count)::bigint                     AS byte_count,
       min(first_seen)::timestamptz                AS first_seen,
       max(last_seen)::timestamptz                 AS last_seen,
       CAST(min(min(window_start)) OVER () AS timestamptz) AS effective_from,
       CAST(max(max(window_end)) OVER () AS timestamptz)   AS effective_to,
       CAST(count(*) OVER () AS bigint)                     AS group_count,
       CAST(sum(count(*)) OVER () AS bigint)                AS total_flow_count,
       CAST(sum(sum(connection_count)) OVER () AS bigint)   AS total_connection_count,
       CAST(sum(sum(byte_count)) OVER () AS bigint)         AS total_byte_count
FROM flow_windows
WHERE (cardinality(sqlc.arg(workload_ids)::uuid[]) = 0 OR workload_id = ANY(sqlc.arg(workload_ids)::uuid[]))
  AND window_start >= sqlc.arg(since)
  AND window_start < sqlc.arg(until)
  AND (sqlc.arg(decision)::integer = 0 OR decision = sqlc.arg(decision)::integer)
  AND (sqlc.arg(direction)::integer = 0 OR direction = sqlc.arg(direction)::integer)
  AND (sqlc.arg(protocol)::integer = 0 OR (protocol = sqlc.arg(protocol)::integer AND dst_port = sqlc.arg(dst_port)::integer))
GROUP BY matched_rule_id
ORDER BY CASE WHEN sqlc.arg(order_by)::text = 'recent' THEN max(last_seen) END DESC NULLS LAST,
         sum(connection_count) DESC, matched_rule_id
LIMIT sqlc.arg(group_limit);

-- Grouped by rule and the resolved peer that hit it. The label snapshot
-- of a peer is the one stored with its most recently seen record.
-- name: RollupFlowsByRulePeer :many
SELECT matched_rule_id, peer_kind, peer_key,
       CAST((array_agg(peer_labels ORDER BY last_seen DESC))[1] AS jsonb) AS peer_labels,
       count(*)::bigint                            AS flow_count,
       sum(connection_count)::bigint               AS connection_count,
       sum(byte_count)::bigint                     AS byte_count,
       min(first_seen)::timestamptz                AS first_seen,
       max(last_seen)::timestamptz                 AS last_seen,
       CAST(min(min(window_start)) OVER () AS timestamptz) AS effective_from,
       CAST(max(max(window_end)) OVER () AS timestamptz)   AS effective_to,
       CAST(count(*) OVER () AS bigint)                     AS group_count,
       CAST(sum(count(*)) OVER () AS bigint)                AS total_flow_count,
       CAST(sum(sum(connection_count)) OVER () AS bigint)   AS total_connection_count,
       CAST(sum(sum(byte_count)) OVER () AS bigint)         AS total_byte_count
FROM flow_windows
WHERE (cardinality(sqlc.arg(workload_ids)::uuid[]) = 0 OR workload_id = ANY(sqlc.arg(workload_ids)::uuid[]))
  AND window_start >= sqlc.arg(since)
  AND window_start < sqlc.arg(until)
  AND (sqlc.arg(decision)::integer = 0 OR decision = sqlc.arg(decision)::integer)
  AND (sqlc.arg(direction)::integer = 0 OR direction = sqlc.arg(direction)::integer)
  AND (sqlc.arg(protocol)::integer = 0 OR (protocol = sqlc.arg(protocol)::integer AND dst_port = sqlc.arg(dst_port)::integer))
GROUP BY matched_rule_id, peer_kind, peer_key
ORDER BY CASE WHEN sqlc.arg(order_by)::text = 'recent' THEN max(last_seen) END DESC NULLS LAST,
         sum(connection_count) DESC, matched_rule_id, peer_kind, peer_key
LIMIT sqlc.arg(group_limit);

-- Grouped by the resolved source peer and the reporting workload: the
-- edges of the dependency map. Inbound only in this version, so the
-- source is the peer and the destination is the workload.
-- name: RollupFlowsBySrcDst :many
SELECT peer_kind, peer_key,
       CAST((array_agg(peer_labels ORDER BY last_seen DESC))[1] AS jsonb) AS peer_labels,
       workload_id,
       count(*)::bigint                            AS flow_count,
       sum(connection_count)::bigint               AS connection_count,
       sum(byte_count)::bigint                     AS byte_count,
       min(first_seen)::timestamptz                AS first_seen,
       max(last_seen)::timestamptz                 AS last_seen,
       CAST(min(min(window_start)) OVER () AS timestamptz) AS effective_from,
       CAST(max(max(window_end)) OVER () AS timestamptz)   AS effective_to,
       CAST(count(*) OVER () AS bigint)                     AS group_count,
       CAST(sum(count(*)) OVER () AS bigint)                AS total_flow_count,
       CAST(sum(sum(connection_count)) OVER () AS bigint)   AS total_connection_count,
       CAST(sum(sum(byte_count)) OVER () AS bigint)         AS total_byte_count
FROM flow_windows
WHERE (cardinality(sqlc.arg(workload_ids)::uuid[]) = 0 OR workload_id = ANY(sqlc.arg(workload_ids)::uuid[]))
  AND window_start >= sqlc.arg(since)
  AND window_start < sqlc.arg(until)
  AND (sqlc.arg(decision)::integer = 0 OR decision = sqlc.arg(decision)::integer)
  AND (sqlc.arg(direction)::integer = 0 OR direction = sqlc.arg(direction)::integer)
  AND (sqlc.arg(protocol)::integer = 0 OR (protocol = sqlc.arg(protocol)::integer AND dst_port = sqlc.arg(dst_port)::integer))
GROUP BY peer_kind, peer_key, workload_id
ORDER BY CASE WHEN sqlc.arg(order_by)::text = 'recent' THEN max(last_seen) END DESC NULLS LAST,
         sum(connection_count) DESC, peer_kind, peer_key, workload_id
LIMIT sqlc.arg(group_limit);

-- Grouped by the reporting workload and the service reached on it: the
-- cells of the matrix.
-- name: RollupFlowsByDstService :many
SELECT workload_id, dst_port, protocol,
       count(*)::bigint                            AS flow_count,
       sum(connection_count)::bigint               AS connection_count,
       sum(byte_count)::bigint                     AS byte_count,
       min(first_seen)::timestamptz                AS first_seen,
       max(last_seen)::timestamptz                 AS last_seen,
       CAST(min(min(window_start)) OVER () AS timestamptz) AS effective_from,
       CAST(max(max(window_end)) OVER () AS timestamptz)   AS effective_to,
       CAST(count(*) OVER () AS bigint)                     AS group_count,
       CAST(sum(count(*)) OVER () AS bigint)                AS total_flow_count,
       CAST(sum(sum(connection_count)) OVER () AS bigint)   AS total_connection_count,
       CAST(sum(sum(byte_count)) OVER () AS bigint)         AS total_byte_count
FROM flow_windows
WHERE (cardinality(sqlc.arg(workload_ids)::uuid[]) = 0 OR workload_id = ANY(sqlc.arg(workload_ids)::uuid[]))
  AND window_start >= sqlc.arg(since)
  AND window_start < sqlc.arg(until)
  AND (sqlc.arg(decision)::integer = 0 OR decision = sqlc.arg(decision)::integer)
  AND (sqlc.arg(direction)::integer = 0 OR direction = sqlc.arg(direction)::integer)
  AND (sqlc.arg(protocol)::integer = 0 OR (protocol = sqlc.arg(protocol)::integer AND dst_port = sqlc.arg(dst_port)::integer))
GROUP BY workload_id, dst_port, protocol
ORDER BY CASE WHEN sqlc.arg(order_by)::text = 'recent' THEN max(last_seen) END DESC NULLS LAST,
         sum(connection_count) DESC, workload_id, dst_port, protocol
LIMIT sqlc.arg(group_limit);

-- One page of a workload's windows, newest first, keyed by
-- (window_start, id) so a page never shifts when new windows land. The
-- first page passes a cursor beyond any row. An empty peer key means every
-- peer; a zero protocol means every service.
-- name: ListFlowWindowPage :many
SELECT * FROM flow_windows
WHERE workload_id = sqlc.arg(workload_id)
  AND window_start >= sqlc.arg(since)
  AND window_start < sqlc.arg(until)
  AND (sqlc.arg(decision)::integer = 0 OR decision = sqlc.arg(decision)::integer)
  AND (sqlc.arg(direction)::integer = 0 OR direction = sqlc.arg(direction)::integer)
  AND (sqlc.arg(peer_key)::text = '' OR peer_key = sqlc.arg(peer_key)::text)
  AND (sqlc.arg(protocol)::integer = 0 OR (protocol = sqlc.arg(protocol)::integer AND dst_port = sqlc.arg(dst_port)::integer))
  AND (window_start < sqlc.arg(cursor_start)::timestamptz
       OR (window_start = sqlc.arg(cursor_start)::timestamptz AND id < sqlc.arg(cursor_id)::bigint))
ORDER BY window_start DESC, id DESC
LIMIT sqlc.arg(row_limit);
