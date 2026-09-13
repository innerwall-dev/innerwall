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

-- name: RecordWorkloadApplied :execrows
UPDATE workloads
SET applied_policy_version = $2, sync_state = $3, sync_error = '', last_seen_at = $4
WHERE id = $1;

-- name: RecordWorkloadHeartbeatSeen :execrows
UPDATE workloads
SET last_seen_at = $2
WHERE id = $1;
