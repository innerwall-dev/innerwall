-- Provisioning tokens (ADR-0016). Only the SHA-256 hash of a token is ever
-- stored or looked up; no query here touches plaintext.

-- name: CreateProvisioningToken :one
INSERT INTO provisioning_tokens (id, token_hash, name, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: AddProvisioningTokenLabel :exec
INSERT INTO provisioning_token_labels (token_id, key, value)
VALUES ($1, $2, $3);

-- name: GetProvisioningTokenByHash :one
SELECT * FROM provisioning_tokens
WHERE token_hash = $1;

-- name: GetProvisioningToken :one
SELECT * FROM provisioning_tokens
WHERE id = $1;

-- name: ListProvisioningTokens :many
SELECT * FROM provisioning_tokens
ORDER BY created_at DESC, id;

-- name: ListProvisioningTokenLabels :many
SELECT * FROM provisioning_token_labels
WHERE token_id = $1
ORDER BY key;

-- name: ListAllProvisioningTokenLabels :many
SELECT * FROM provisioning_token_labels
ORDER BY token_id, key;

-- name: RevokeProvisioningToken :execrows
UPDATE provisioning_tokens
SET revoked_at = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: RecordProvisioningTokenUse :exec
UPDATE provisioning_tokens
SET use_count = use_count + 1, last_used_at = $2
WHERE id = $1;
