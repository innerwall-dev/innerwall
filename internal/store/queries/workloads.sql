-- Workload identity registry (ADR-0016). The id column is the workload_id
-- carried in the credential's URI SAN; labels are assigned at enrollment from
-- the provisioning token's scope and are never read from a certificate.

-- name: CreateWorkload :exec
INSERT INTO workloads (id, provisioning_token_id, hostname, enrolled_at, credential_serial, credential_expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: AddWorkloadLabel :exec
INSERT INTO workload_labels (workload_id, key, value)
VALUES ($1, $2, $3);

-- name: GetWorkload :one
SELECT * FROM workloads
WHERE id = $1;

-- name: ListWorkloadLabels :many
SELECT * FROM workload_labels
WHERE workload_id = $1
ORDER BY key;

-- name: RecordWorkloadRenewal :execrows
UPDATE workloads
SET credential_serial = $2, credential_expires_at = $3, last_renewed_at = $4
WHERE id = $1;

-- --- inventory and status (ADR-0018) ------------------------------------------

-- name: ListWorkloads :many
SELECT * FROM workloads
ORDER BY enrolled_at, id;

-- name: ListAllWorkloadLabels :many
SELECT * FROM workload_labels
ORDER BY workload_id, key;

-- name: DeleteWorkloadLabels :exec
DELETE FROM workload_labels
WHERE workload_id = $1;

-- name: SetWorkloadMode :execrows
UPDATE workloads
SET mode = $2
WHERE id = $1;

-- name: RecordWorkloadInventory :execrows
UPDATE workloads
SET facts = $2, hostname = $3, last_seen_at = $4
WHERE id = $1;

-- name: RecordWorkloadAgent :execrows
UPDATE workloads
SET agent_version = $2, agent_capabilities = $3, applied_policy_version = $4, last_seen_at = $5
WHERE id = $1;

-- name: ListWorkloadAddresses :many
SELECT address FROM workload_addresses
WHERE workload_id = $1
ORDER BY address;

-- name: ListAllWorkloadAddresses :many
SELECT * FROM workload_addresses
ORDER BY workload_id, address;

-- name: DeleteWorkloadAddresses :exec
DELETE FROM workload_addresses
WHERE workload_id = $1;

-- name: AddWorkloadAddress :exec
INSERT INTO workload_addresses (workload_id, address)
VALUES ($1, $2);

-- name: DeleteWorkloadListeningServices :exec
DELETE FROM workload_listening_services
WHERE workload_id = $1;

-- name: AddWorkloadListeningService :exec
INSERT INTO workload_listening_services (workload_id, protocol, port, process_name, process_path)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workload_id, protocol, port) DO UPDATE
SET process_name = EXCLUDED.process_name, process_path = EXCLUDED.process_path;

-- name: ListWorkloadListeningServices :many
SELECT * FROM workload_listening_services
WHERE workload_id = $1
ORDER BY protocol, port;

-- name: RecordWorkloadHeartbeat :execrows
UPDATE workloads
SET last_seen_at = $2, dropped_flow_records = $3, credential_renewal_error = $4
WHERE id = $1;

-- name: SetWorkloadSyncState :execrows
UPDATE workloads
SET sync_state = $2, sync_error = $3, last_seen_at = $4
WHERE id = $1;

-- An APPLIED acknowledgement: the version, the resulting state, and the
-- instant, which only this and the failure below write (migration 00007).
-- name: RecordWorkloadApplied :execrows
UPDATE workloads
SET applied_policy_version = $2, sync_state = $3, sync_error = '', last_seen_at = $4, last_acked_at = $4
WHERE id = $1;

-- A FAILED acknowledgement: the degraded state, the agent's detail, and the
-- instant. The applied version is untouched; the agent is still on it.
-- name: RecordWorkloadApplyFailed :execrows
UPDATE workloads
SET sync_state = $2, sync_error = $3, last_seen_at = $4, last_apply_failed_at = $4
WHERE id = $1;

-- name: RecordWorkloadHeartbeatSeen :execrows
UPDATE workloads
SET last_seen_at = $2
WHERE id = $1;

-- Stamped by the sync path whenever it sends the workload a snapshot,
-- whatever caused it; nothing else writes it (ADR-0018 as amended).
-- name: RecordWorkloadSnapshotSent :execrows
UPDATE workloads
SET last_snapshot_sent_at = $2
WHERE id = $1;

-- --- operator read model (ADR-0007 as amended) -------------------------------

-- One page of the fleet in the order the fleet screen shows it: the
-- workloads needing attention first (degraded, then offline, then
-- pending, then synced), most recently seen first within a state, then by
-- id. The cursor is the (rank, last seen, id) of the last row served; the
-- first page passes a rank below every row. A workload never seen sorts
-- as if seen at the epoch. An empty id array means every workload; a zero
-- mode or sync state means any. The latest rendered version rides along
-- from workload_policies so sync drift is one read.
-- name: ListWorkloadPage :many
SELECT w.id, w.region_id, w.provisioning_token_id, w.hostname, w.enrolled_at,
       w.credential_serial, w.credential_expires_at, w.last_renewed_at,
       w.mode, w.facts, w.agent_version, w.agent_capabilities, w.last_seen_at,
       w.sync_state, w.applied_policy_version, w.sync_error, w.dropped_flow_records,
       w.credential_renewal_error, w.last_snapshot_sent_at, w.last_acked_at, w.last_apply_failed_at,
       w.sync_rank::integer AS sync_rank, w.seen_key::timestamptz AS seen_key,
       p.version AS latest_version, p.rendered_at AS latest_rendered_at
FROM (
    SELECT workloads.*,
           CASE workloads.sync_state WHEN 3 THEN 0 WHEN 4 THEN 1 WHEN 2 THEN 2 WHEN 1 THEN 3 ELSE 4 END AS sync_rank,
           coalesce(workloads.last_seen_at, '1970-01-01 00:00:00+00'::timestamptz) AS seen_key
    FROM workloads
) AS w
LEFT JOIN workload_policies p ON p.workload_id = w.id
WHERE (cardinality(sqlc.arg(workload_ids)::uuid[]) = 0 OR w.id = ANY(sqlc.arg(workload_ids)::uuid[]))
  AND (sqlc.arg(mode)::integer = 0 OR w.mode = sqlc.arg(mode)::integer)
  AND (sqlc.arg(sync_state)::integer = 0 OR w.sync_state = sqlc.arg(sync_state)::integer)
  AND (w.sync_rank > sqlc.arg(cursor_rank)::integer
       OR (w.sync_rank = sqlc.arg(cursor_rank)::integer
           AND (w.seen_key < sqlc.arg(cursor_seen)::timestamptz
                OR (w.seen_key = sqlc.arg(cursor_seen)::timestamptz AND w.id > sqlc.arg(cursor_id)::uuid))))
ORDER BY w.sync_rank, w.seen_key DESC, w.id
LIMIT sqlc.arg(row_limit);

-- One workload with its latest rendered version, for the detail read.
-- name: GetWorkloadWithPolicy :one
SELECT sqlc.embed(w), p.version AS latest_version, p.rendered_at AS latest_rendered_at
FROM workloads w
LEFT JOIN workload_policies p ON p.workload_id = w.id
WHERE w.id = $1;

-- The children of the workloads on one page, fetched once per page.
-- name: ListWorkloadLabelsFor :many
SELECT * FROM workload_labels
WHERE workload_id = ANY(sqlc.arg(workload_ids)::uuid[])
ORDER BY workload_id, key;

-- name: ListWorkloadAddressesFor :many
SELECT * FROM workload_addresses
WHERE workload_id = ANY(sqlc.arg(workload_ids)::uuid[])
ORDER BY workload_id, address;

-- name: ListWorkloadListeningServicesFor :many
SELECT * FROM workload_listening_services
WHERE workload_id = ANY(sqlc.arg(workload_ids)::uuid[])
ORDER BY workload_id, protocol, port;
