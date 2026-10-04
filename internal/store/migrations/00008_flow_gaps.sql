-- Evidence gaps (ADR-0019 as amended 2026-10-04) and the heartbeat's
-- overrun counter (ADR-0015 as amended 2026-10-04).
--
-- An agent reports, with its flow windows, the intervals in which it knows
-- its evidence is incomplete: the kernel dropped events because a source's
-- socket overflowed, a source was down between a failure and its next
-- subscribe, the agent's buffer dropped closed windows while the control
-- plane was unreachable, or the connection table was larger at subscribe
-- than the source processes. What was lost cannot be recovered; the
-- interval bounds it, and a reader asks of a time range whether the
-- evidence in it is whole.
--
-- flow_gaps holds one row per reported interval [gap_from, gap_to) of one
-- workload, taken from the connection credential, never the payload. kind
-- and source are the wire enums' numbers (kind: 1 source overrun, 2 source
-- restart, 3 buffer overflow, 4 truncated dump; source: 0 none, the loss is
-- not one source's, 1 connection tracking, 2 the packet log). lost_count is
-- the records or table entries lost where that is known, and NULL where it
-- is not (a kernel-side loss). Delivery is at least once, so a repeated
-- interval is recognized by its key and stored once.
--
-- Gaps are pruned with the windows by the same retention job, a gap once
-- it ended before the horizon: a gap is kept at least as long as any
-- window it could describe.
--
-- region_id is reserved (ADR-0012).

-- +goose Up
CREATE TABLE flow_gaps (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    region_id    text        NOT NULL DEFAULT 'default',
    workload_id  uuid        NOT NULL REFERENCES workloads (id) ON DELETE CASCADE,
    kind         integer     NOT NULL,
    source       integer     NOT NULL,
    gap_from     timestamptz NOT NULL,
    gap_to       timestamptz NOT NULL,
    lost_count   bigint,
    received_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (gap_from <= gap_to),
    CHECK (kind BETWEEN 1 AND 4),
    CHECK (source BETWEEN 0 AND 2),
    CHECK (lost_count IS NULL OR lost_count >= 0),
    UNIQUE (workload_id, kind, source, gap_from, gap_to)
);

-- Gaps of a workload set intersecting a time range: per workload, the gaps
-- ending at or after the range's start.
CREATE INDEX flow_gaps_workload_to_idx ON flow_gaps (workload_id, gap_to);
-- Retention deletes by gap_to.
CREATE INDEX flow_gaps_gap_to_idx ON flow_gaps (gap_to);

-- The heartbeat's count of kernel-side overruns of the agent's flow
-- sources since the agent started.
ALTER TABLE workloads
    ADD COLUMN source_overruns bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE workloads DROP COLUMN source_overruns;
DROP TABLE flow_gaps;
