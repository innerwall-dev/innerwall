-- The server-derived agent-silence evidence gap (ADR-0019 as amended
-- 2026-10-16, issue #114).
--
-- An agent cannot report the interval in which its own process was not
-- running: the gaps it ships cover only losses it observed (ADR-0011 keeps
-- its on-disk state to the persisted policy, so there is no "last observed"
-- instant to reopen at start). The control plane is the one party that saw
-- the silence — flow ingest stamps last_flow_report_at on every report
-- (migration 00013) — so it derives the gap itself: when a report arrives
-- after more than the telemetry-liveness allowance (two flow windows) of
-- silence, ingest writes one flow_gaps row from the previous report's
-- receipt to the arriving window's start when that start lies within the
-- silence, to the report's receipt when it carries no usable window, and
-- not at all when the window itself reaches back over the silence (a
-- buffered window delivered late is evidence arriving, not evidence lost).
--
-- The row is EvidenceGapKind 8, agent_silent, written by the control
-- plane and refused from the wire at ingest. It is not one
-- source's loss, so its source is 0, and nothing counts what was missed,
-- so lost_count is NULL. flow_gaps admits kind 8 beside the seven kinds
-- 00012 admitted; the key, reads, and retention are unchanged.

-- +goose Up
ALTER TABLE flow_gaps DROP CONSTRAINT flow_gaps_kind_check;
ALTER TABLE flow_gaps ADD CONSTRAINT flow_gaps_kind_check CHECK (kind BETWEEN 1 AND 8);

-- +goose Down
DELETE FROM flow_gaps WHERE kind = 8;
ALTER TABLE flow_gaps DROP CONSTRAINT flow_gaps_kind_check;
ALTER TABLE flow_gaps ADD CONSTRAINT flow_gaps_kind_check CHECK (kind BETWEEN 1 AND 7);
