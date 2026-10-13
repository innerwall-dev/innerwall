-- The owned table's presence as the agent's heartbeats report it, and the
-- table-removed evidence gap (ADR-0015 and ADR-0019 as amended 2026-10-13,
-- PR #95, issue #64).
--
-- The local kill switch deletes the agent's owned nftables table without
-- the control plane (ADR-0011), and the version the agent last acknowledged
-- is unchanged by it, so the sync state alone kept saying a host enforced
-- what it no longer held. Every heartbeat now carries whether the table is
-- in the kernel.
--
-- - owned_table_state is the wire enum's number: 0 unknown (an agent that
--   predates the field, one that has installed nothing, or a failed read),
--   1 present, 2 absent. A heartbeat that says unknown leaves the stored
--   state as it was, so a failed read never turns a removed table back into
--   a healthy one; an applied acknowledgement turns absent into present,
--   because an apply that succeeded installed the table.
-- - owned_table_absent_since is when the control plane first received a
--   heartbeat saying absent, kept while later heartbeats agree, and NULL
--   whenever the state is not absent.
--
-- While the table is gone the agent's observation and its classification
-- by connection mark are gone with it, and the agent reports the interval
-- as an evidence gap, EvidenceGapKind 7; flow_gaps admits kind 7 beside the
-- six kinds 00011 admitted. It is not one source's loss, so its source is 0.

-- +goose Up
ALTER TABLE workloads
    ADD COLUMN owned_table_state        smallint    NOT NULL DEFAULT 0 CHECK (owned_table_state BETWEEN 0 AND 2),
    ADD COLUMN owned_table_absent_since timestamptz;

ALTER TABLE flow_gaps DROP CONSTRAINT flow_gaps_kind_check;
ALTER TABLE flow_gaps ADD CONSTRAINT flow_gaps_kind_check CHECK (kind BETWEEN 1 AND 7);

-- +goose Down
DELETE FROM flow_gaps WHERE kind = 7;
ALTER TABLE flow_gaps DROP CONSTRAINT flow_gaps_kind_check;
ALTER TABLE flow_gaps ADD CONSTRAINT flow_gaps_kind_check CHECK (kind BETWEEN 1 AND 6);

ALTER TABLE workloads
    DROP COLUMN owned_table_absent_since,
    DROP COLUMN owned_table_state;
