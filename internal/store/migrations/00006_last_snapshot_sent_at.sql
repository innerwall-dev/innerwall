-- When the sync path last sent a workload a snapshot (M3.3b, ADR-0018 as
-- amended).
--
-- workloads already holds the per-workload state the sync stream reports:
-- last-seen, the convergence state, the applied version. last_snapshot_sent_at
-- sits beside them and records what the control plane did, not what the agent
-- said: the sync path writes it every time it puts a snapshot on a stream,
-- whatever caused that (a stream opening, the repair of a workload with no
-- persisted policy, the recovery snapshot after a failed apply). Nothing else
-- writes it. A directed reconnect is a request with no delivery guarantee, and
-- this instant advancing is how its outcome is observed.
--
-- NULL means no snapshot has been sent since the column existed; nothing is
-- backfilled, because no earlier instant is known.

-- +goose Up
ALTER TABLE workloads ADD COLUMN last_snapshot_sent_at timestamptz;

-- +goose Down
ALTER TABLE workloads DROP COLUMN last_snapshot_sent_at;
