-- When the agent last acknowledged an applied version and when it last
-- reported a failed apply (M3.5b, ADR-0018).
--
-- workloads already holds what the sync stream reports: the convergence
-- state, the applied version, and the agent's detail on a failed apply. None
-- of those carries a time, and last_seen_at moves on every message, so it
-- cannot stand in for either event. These two columns sit beside them and are
-- written by the sync path alone, where it records the acknowledgement:
--
-- - last_acked_at is stamped with the applied version on an APPLIED
--   acknowledgement. The version a Hello claims is not an acknowledgement and
--   does not stamp it.
-- - last_apply_failed_at is stamped with the degraded state and the agent's
--   detail on a FAILED acknowledgement. A later successful apply clears the
--   detail and leaves this instant: it answers when the last failure was, and
--   the state says whether the workload is still degraded.
--
-- NULL means no such acknowledgement has been recorded since the column
-- existed; nothing is backfilled, because no earlier instant is known.

-- +goose Up
ALTER TABLE workloads
    ADD COLUMN last_acked_at        timestamptz,
    ADD COLUMN last_apply_failed_at timestamptz;

-- +goose Down
ALTER TABLE workloads
    DROP COLUMN last_apply_failed_at,
    DROP COLUMN last_acked_at;
