-- The instant a workload was retired (ADR-0023; issues #178, #180, #126).
--
-- Retirement is one way and is the whole of a workload's lifecycle end: the
-- row is kept, so flow windows, totals, and gaps keep naming an identity the
-- registry resolves, and retired_at is the only state it adds. There is no
-- separate deny-list: the registry is where authorization lives (ADR-0004),
-- so a retired identity is refused wherever the registry already authorizes
-- (renewal, every agent call, every write a live stream makes), the render
-- leaves it out as a subject and as anyone's peer, and the fleet reads it as
-- a state of its own. Nothing clears it; a host that comes back re-enrolls
-- as a new identity.
--
-- The agent_silent gap a retirement closes goes in flow_gaps as it is, so
-- nothing else changes. NULL is every workload until it is retired.

-- +goose Up
ALTER TABLE workloads ADD COLUMN retired_at timestamptz;

-- +goose Down
ALTER TABLE workloads DROP COLUMN retired_at;
