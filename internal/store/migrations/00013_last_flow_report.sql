-- When the control plane last received a flow report from a workload's
-- agent (ADR-0019 as amended 2026-10-13, PR #95,
-- issue #61).
--
-- The sync stream and flow telemetry travel on separate connections with
-- separate backoff (ADR-0002, ADR-0015), so one can be down while the other
-- delivers. The record held only the sync path's last contact, and the
-- console called a workload whose sync stream was waiting out its reconnect
-- backoff Offline while its flow reports kept landing every window. This
-- column lets a read tell "control channel reconnecting, telemetry alive"
-- from "no contact at all".
--
-- last_flow_report_at is the instant of receipt, on the control plane's
-- clock, of the latest report from the workload, whatever its window or its
-- fate at validation: a report proves the agent is running. Written by flow
-- ingest alone, never moved backwards. NULL when none has been received
-- since the column existed.

-- +goose Up
ALTER TABLE workloads ADD COLUMN last_flow_report_at timestamptz;

-- +goose Down
ALTER TABLE workloads DROP COLUMN last_flow_report_at;
