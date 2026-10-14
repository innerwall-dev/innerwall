-- The latest late renewal of a workload's credential (ADR-0016 as amended
-- 2026-10-14, PR #102, issue #93).
--
-- A credential that expired while nothing could renew it (a control-plane
-- outage spanning the renewal window) may still renew within the control
-- plane's renewal grace, and do nothing else. Such a renewal is recorded
-- here so a read can say the workload renewed late, and by how much: the
-- instant of the renewal on the control plane's clock, and the expiry of
-- the credential it replaced. Written by renewal alone; a later on-time
-- renewal leaves it. Both NULL when the workload has never renewed late.

-- +goose Up
ALTER TABLE workloads
    ADD COLUMN late_renewal_at timestamptz,
    ADD COLUMN late_renewal_expired_at timestamptz,
    ADD CONSTRAINT workloads_late_renewal_pair
        CHECK ((late_renewal_at IS NULL) = (late_renewal_expired_at IS NULL));

-- +goose Down
ALTER TABLE workloads
    DROP CONSTRAINT workloads_late_renewal_pair,
    DROP COLUMN late_renewal_expired_at,
    DROP COLUMN late_renewal_at;
