-- Flow storage (ADR-0009, ADR-0019) and the heartbeat's renewal status.
--
-- Two tables carry every flow record an agent reports:
--
--   * flow_windows holds one row per aggregated record per reporting window,
--     exactly as the agent shipped it plus what ingestion resolved at that
--     moment: the peer behind the source address (a workload with the labels
--     it carried then, an address group, or nothing) is captured with the
--     row, because addresses are reassigned and a query-time join would
--     rewrite history. Rows age out under the retention horizon.
--   * flow_totals holds one row per (workload, resolved peer, port, protocol,
--     direction, decision), upserted at ingest with the running counters and
--     the first and last time the key was seen. It answers "since first seen"
--     questions without a scan of the windows and is never pruned.
--
-- Both are column-shaped: no nested structure beyond the label snapshot, so
-- the same rows port to a columnar store behind the FlowStore interface when
-- an estate's volume demands it (ADR-0009).
--
-- region_id is reserved on both tables (ADR-0012).

-- +goose Up

-- peer_kind: 0 = unresolved (peer_key is the source address), 1 = workload
-- (peer_key is its id), 2 = address group (peer_key is its id). peer_labels
-- is the workload's label set at ingest, as a JSON object, and is empty for
-- the other kinds.
CREATE TABLE flow_windows (
    id                bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    region_id         text        NOT NULL DEFAULT 'default',
    workload_id       uuid        NOT NULL REFERENCES workloads (id) ON DELETE CASCADE,
    window_start      timestamptz NOT NULL,
    window_end        timestamptz NOT NULL,
    peer_kind         integer     NOT NULL,
    peer_key          text        NOT NULL,
    peer_labels       jsonb       NOT NULL DEFAULT '{}',
    src_address       text        NOT NULL,
    dst_address       text        NOT NULL,
    dst_port          integer     NOT NULL,
    protocol          integer     NOT NULL,
    direction         integer     NOT NULL,
    decision          integer     NOT NULL,
    matched_rule_id   text        NOT NULL DEFAULT '',
    connection_count  bigint      NOT NULL,
    byte_count        bigint      NOT NULL,
    first_seen        timestamptz NOT NULL,
    last_seen         timestamptz NOT NULL,
    process_name      text        NOT NULL DEFAULT '',
    CHECK (window_start <= window_end),
    CHECK (dst_port >= 0 AND dst_port <= 65535)
);

-- Per-workload flow lists, newest window first.
CREATE INDEX flow_windows_workload_time_idx ON flow_windows (workload_id, window_start DESC);
-- Decision-filtered rollups over a time range (would-block and observed
-- verdicts grouped by peer, service, or rule).
CREATE INDEX flow_windows_decision_time_idx ON flow_windows (decision, window_start);
-- Retention deletes by window_start.
CREATE INDEX flow_windows_window_start_idx ON flow_windows (window_start);

CREATE TABLE flow_totals (
    region_id         text        NOT NULL DEFAULT 'default',
    workload_id       uuid        NOT NULL REFERENCES workloads (id) ON DELETE CASCADE,
    peer_kind         integer     NOT NULL,
    peer_key          text        NOT NULL,
    peer_labels       jsonb       NOT NULL DEFAULT '{}',
    dst_port          integer     NOT NULL,
    protocol          integer     NOT NULL,
    direction         integer     NOT NULL,
    decision          integer     NOT NULL,
    matched_rule_id   text        NOT NULL DEFAULT '',
    first_seen        timestamptz NOT NULL,
    last_seen         timestamptz NOT NULL,
    connection_count  bigint      NOT NULL,
    byte_count        bigint      NOT NULL,
    window_count      bigint      NOT NULL,
    PRIMARY KEY (workload_id, peer_kind, peer_key, dst_port, protocol, direction, decision)
);

CREATE INDEX flow_totals_decision_idx ON flow_totals (decision, last_seen);

-- The heartbeat's renewal status: the reason the agent's last automatic
-- credential renewal failed, or empty.
ALTER TABLE workloads
    ADD COLUMN credential_renewal_error text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE workloads DROP COLUMN credential_renewal_error;
DROP TABLE flow_totals;
DROP TABLE flow_windows;
