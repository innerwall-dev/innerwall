-- What a workload's flow reports have said about its agent's clock (INN-22,
-- ADR-0015 as amended 2026-10-11).
--
-- Every flow report carries the agent's clock at send (sent_at); ingest
-- compares it with the instant the control plane received the report, and an
-- offset beyond twice the flow window is skew. An agent that predates the
-- field is judged by its window's end instead, and only for a clock running
-- ahead, since a buffered window is legitimately delivered late. A skewed
-- window is stored like any other: these columns record and surface the skew,
-- they reject nothing.
--
-- - clock_skew_offset_ms is the agent's clock minus the control plane's, in
--   milliseconds, of the latest skewed report: negative when the agent is
--   behind.
-- - clock_skew_observed_at is when that report was received.
-- - clock_skew_current says whether the latest report received was skewed. A
--   report within tolerance clears it and leaves the other two, which then
--   say when the windows stamped by the wrong clock arrived.
--
-- NULL means no skewed report has been received since the column existed.
-- The columns are written by flow ingest alone.

-- +goose Up
ALTER TABLE workloads
    ADD COLUMN clock_skew_offset_ms   bigint,
    ADD COLUMN clock_skew_observed_at timestamptz,
    ADD COLUMN clock_skew_current     boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE workloads
    DROP COLUMN clock_skew_current,
    DROP COLUMN clock_skew_observed_at,
    DROP COLUMN clock_skew_offset_ms;
