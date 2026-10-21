-- The credential a workload's sync stream last presented (issue #112).
--
-- credential_serial and credential_expires_at are the credential the
-- control plane last issued. An agent keeps the stream it opened, on the
-- credential it opened with, until that credential's end, so after a
-- renewal the live stream still runs on the one renewal replaced; and a
-- credential can be issued that no agent ever presents. When the two
-- differ, a record that names only the issued one points every reader at
-- a certificate the live session is not using, and a stream still running
-- while the record reads expired looks like enforcement failing.
--
-- These are the serial and expiry of the verified certificate the
-- workload's most recent sync stream opened on, written at stream open
-- with what its Hello said, and by nothing else. A stream ending leaves
-- them: they say what was presented last, and they describe the credential
-- in use only while the sync state says a stream is open. Both NULL until
-- the first stream opens after this migration; nothing fills them in for
-- older rows, because nothing stored says what those streams presented.

-- +goose Up
ALTER TABLE workloads
    ADD COLUMN presented_credential_serial text,
    ADD COLUMN presented_credential_expires_at timestamptz,
    ADD CONSTRAINT workloads_presented_credential_pair
        CHECK ((presented_credential_serial IS NULL) = (presented_credential_expires_at IS NULL));

-- +goose Down
ALTER TABLE workloads
    DROP CONSTRAINT workloads_presented_credential_pair,
    DROP COLUMN presented_credential_expires_at,
    DROP COLUMN presented_credential_serial;
