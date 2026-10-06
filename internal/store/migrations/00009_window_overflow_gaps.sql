-- The window-overflow evidence gap (ADR-0019 and ADR-0015 as amended
-- 2026-10-05, PR #23). The agent bounds the distinct keys one open
-- aggregation window holds; an observation for a new key past the bound is
-- dropped, and the window reports one gap from the first drop to its close,
-- counting the observations dropped. On the wire that is EvidenceGapKind 5,
-- so flow_gaps admits kind 5 beside the four kinds 00008 admitted. A window
-- overflow is not one source's loss, so its source is 0, as for a buffer
-- overflow.

-- +goose Up
ALTER TABLE flow_gaps DROP CONSTRAINT flow_gaps_kind_check;
ALTER TABLE flow_gaps ADD CONSTRAINT flow_gaps_kind_check CHECK (kind BETWEEN 1 AND 5);

-- +goose Down
DELETE FROM flow_gaps WHERE kind = 5;
ALTER TABLE flow_gaps DROP CONSTRAINT flow_gaps_kind_check;
ALTER TABLE flow_gaps ADD CONSTRAINT flow_gaps_kind_check CHECK (kind BETWEEN 1 AND 4);
