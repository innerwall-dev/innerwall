-- Operator surface: the single operator, browser sessions, and operator
-- tokens (ADR-0021).
--
-- operators is constrained to one row. Version 1 has exactly one operator
-- and authorization is binary, so there is no subject column on sessions or
-- tokens and no permission column anywhere; the constraint is the schema's
-- statement of that decision, not a placeholder for more rows later.
--
-- operator_sessions holds only the SHA-256 digest of a session identifier;
-- the identifier itself lives in the browser's cookie. Lifetime is fixed at
-- creation and never extended. Expired rows are pruned on the retention
-- schedule.
--
-- operator_tokens mirrors provisioning_tokens: digest at rest, a stored
-- prefix for listing, optional expiry, revocation, and use recorded rather
-- than consumed.
--
-- region_id is reserved on every table (ADR-0012).

-- +goose Up
CREATE TABLE operators (
    id             boolean     PRIMARY KEY DEFAULT true CHECK (id),
    region_id      text        NOT NULL DEFAULT 'default',
    password_hash  text        NOT NULL,
    display_name   text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE operator_sessions (
    id_hash     bytea       PRIMARY KEY,
    region_id   text        NOT NULL DEFAULT 'default',
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL
);

-- Retention deletes by expiry.
CREATE INDEX operator_sessions_expires_at_idx ON operator_sessions (expires_at);

CREATE TABLE operator_tokens (
    id            uuid        PRIMARY KEY,
    region_id     text        NOT NULL DEFAULT 'default',
    token_hash    bytea       NOT NULL UNIQUE,
    token_prefix  text        NOT NULL,
    name          text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz,
    revoked_at    timestamptz,
    last_used_at  timestamptz
);

-- +goose Down
DROP TABLE operator_tokens;
DROP TABLE operator_sessions;
DROP TABLE operators;
