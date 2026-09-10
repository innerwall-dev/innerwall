-- Enrollment: provisioning tokens and the workloads they enroll (ADR-0016).
--
-- provisioning_tokens holds only a SHA-256 hash of each token; the plaintext
-- is shown once at mint and never stored. A token is scoped to a label set
-- and may enroll many workloads, so labels hang off the token, not off a use.
--
-- workloads is the identity registry: the primary key is the workload_id
-- bound into the credential's URI SAN. Labels live here, never in the
-- certificate; the certificate authenticates, this table authorizes.
--
-- region_id is reserved on every table from the first migration (ADR-0012).
-- A single-region deployment uses the default value everywhere.

-- +goose Up
CREATE TABLE provisioning_tokens (
    id            uuid        PRIMARY KEY,
    region_id     text        NOT NULL DEFAULT 'default',
    token_hash    bytea       NOT NULL UNIQUE,
    name          text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    revoked_at    timestamptz,
    use_count     bigint      NOT NULL DEFAULT 0,
    last_used_at  timestamptz
);

CREATE TABLE provisioning_token_labels (
    token_id  uuid NOT NULL REFERENCES provisioning_tokens (id) ON DELETE CASCADE,
    key       text NOT NULL,
    value     text NOT NULL,
    PRIMARY KEY (token_id, key)
);

CREATE TABLE workloads (
    id                     uuid        PRIMARY KEY,
    region_id              text        NOT NULL DEFAULT 'default',
    provisioning_token_id  uuid        NOT NULL REFERENCES provisioning_tokens (id),
    hostname               text        NOT NULL DEFAULT '',
    enrolled_at            timestamptz NOT NULL DEFAULT now(),
    credential_serial      text        NOT NULL,
    credential_expires_at  timestamptz NOT NULL,
    last_renewed_at        timestamptz
);

CREATE INDEX workloads_provisioning_token_id_idx ON workloads (provisioning_token_id);

CREATE TABLE workload_labels (
    workload_id  uuid NOT NULL REFERENCES workloads (id) ON DELETE CASCADE,
    key          text NOT NULL,
    value        text NOT NULL,
    PRIMARY KEY (workload_id, key)
);

-- Policy compilation resolves label selectors to workloads; this is that lookup.
CREATE INDEX workload_labels_key_value_idx ON workload_labels (key, value);

-- +goose Down
DROP TABLE workload_labels;
DROP TABLE workloads;
DROP TABLE provisioning_token_labels;
DROP TABLE provisioning_tokens;
