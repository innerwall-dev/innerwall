-- The agent process a workload's heartbeats last proved running (ADR-0019
-- as amended 2026-10-17, issue #144).
--
-- The agent ships only the flow windows that hold something (an empty one
-- rides only when it carries gaps), so a workload that receives little or
-- no inbound traffic goes many windows between reports while its agent is
-- running and observing. The agent-silence derivation (migration 00015)
-- read every such quiet stretch longer than two windows as an agent that
-- was not reporting, and wrote agent_silent gaps for evidence that was
-- never lost.
--
-- Every heartbeat carries the agent's uptime on its own monotonic clock, so
-- one heartbeat proves one process ran from the instant it started until
-- the heartbeat arrived. last_heartbeat_at is that heartbeat's receipt on
-- the control plane's clock, and agent_started_at the receipt minus the
-- uptime it carried. They are written together by heartbeats alone, so the
-- pair always comes from one heartbeat (last_seen_at moves on every sync
-- message, a new process's Hello among them, and cannot stand in). Flow
-- ingest reads the pair under the receipt's row lock: a process that
-- started before the previous report and still ran within one window of
-- the arriving one lived through the whole silence, every loss in it is
-- the agent's own to report, and no agent_silent gap is derived.
--
-- Both are NULL until the first heartbeat after this migration, and the
-- derivation then works as before. The agent_silent rows 00015's rule
-- wrote for quiet stretches are left in place: nothing stored can tell
-- them from an outage, and deleting a true one would let a review read
-- safe on evidence never collected. They age out with flow retention.

-- +goose Up
ALTER TABLE workloads ADD COLUMN last_heartbeat_at timestamptz;
ALTER TABLE workloads ADD COLUMN agent_started_at timestamptz;

-- +goose Down
ALTER TABLE workloads DROP COLUMN agent_started_at;
ALTER TABLE workloads DROP COLUMN last_heartbeat_at;
