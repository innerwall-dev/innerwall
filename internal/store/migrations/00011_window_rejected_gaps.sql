-- The window-rejected evidence gap (ADR-0015 and ADR-0019 as amended
-- 2026-10-11, PR #90). A closed window the control plane refuses as
-- malformed can never be accepted, so the agent drops it rather than retry it
-- ahead of every window behind it, and reports one gap over the dropped
-- window's bounds, counting its records. On the wire that is EvidenceGapKind
-- 6, so flow_gaps admits kind 6 beside the five kinds 00009 admitted. A
-- rejected window is not one source's loss, so its source is 0, as for a
-- buffer overflow.

-- +goose Up
ALTER TABLE flow_gaps DROP CONSTRAINT flow_gaps_kind_check;
ALTER TABLE flow_gaps ADD CONSTRAINT flow_gaps_kind_check CHECK (kind BETWEEN 1 AND 6);

-- +goose Down
DELETE FROM flow_gaps WHERE kind = 6;
ALTER TABLE flow_gaps DROP CONSTRAINT flow_gaps_kind_check;
ALTER TABLE flow_gaps ADD CONSTRAINT flow_gaps_kind_check CHECK (kind BETWEEN 1 AND 5);
