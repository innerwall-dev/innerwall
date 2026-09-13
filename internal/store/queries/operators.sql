-- Operator surface (ADR-0021). Passwords are stored as argon2id strings;
-- session identifiers and operator tokens only as SHA-256 digests. No query
-- here touches a plaintext credential.

-- name: GetOperator :one
SELECT * FROM operators
WHERE id = true;

-- name: SetOperator :one
INSERT INTO operators (password_hash, display_name, created_at, updated_at)
VALUES ($1, $2, $3, $3)
ON CONFLICT (id) DO UPDATE
SET password_hash = EXCLUDED.password_hash,
    display_name = EXCLUDED.display_name,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: CreateOperatorSession :exec
INSERT INTO operator_sessions (id_hash, created_at, expires_at)
VALUES ($1, $2, $3);

-- name: GetOperatorSession :one
SELECT * FROM operator_sessions
WHERE id_hash = $1;

-- name: DeleteOperatorSession :execrows
DELETE FROM operator_sessions
WHERE id_hash = $1;

-- Retention: expired sessions are removed on the same schedule as aged
-- flow windows (ADR-0019, ADR-0021).
-- name: DeleteExpiredOperatorSessions :execrows
DELETE FROM operator_sessions
WHERE expires_at <= sqlc.arg(now);

-- name: CreateOperatorToken :exec
INSERT INTO operator_tokens (id, token_hash, token_prefix, name, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetOperatorTokenByHash :one
SELECT * FROM operator_tokens
WHERE token_hash = $1;

-- name: GetOperatorToken :one
SELECT * FROM operator_tokens
WHERE id = $1;

-- name: ListOperatorTokens :many
SELECT * FROM operator_tokens
ORDER BY created_at DESC, id;

-- name: RevokeOperatorToken :execrows
UPDATE operator_tokens
SET revoked_at = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: RecordOperatorTokenUse :exec
UPDATE operator_tokens
SET last_used_at = $2
WHERE id = $1;
