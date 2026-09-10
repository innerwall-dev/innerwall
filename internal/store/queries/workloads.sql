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
