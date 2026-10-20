# Roadmap

Direction, not dates. Ordering follows ADR-0001: visibility value first, enforcement as the destination. Every item below that the record already names cites where; an item without a citation is a direction the record has not decided yet and gets its own ADR before it is built.

## Shipped

The milestones M1 to M5 shipped in v0.1.0; every release since, and what it carries, is on the [releases page](https://github.com/innerwall-dev/innerwall/releases).

## Near-term priorities

- Native packages (.deb/.rpm)
- Modeling inbound connections to Docker published ports ([#176](https://github.com/innerwall-dev/innerwall/issues/176))
- Outbound enforcement (ADR-0010)
- Broader platform coverage
- Notifications
- Definitions management in the console
- Flow retention controls

## Backlog: recorded, not yet built

Each of these is a decision or a named gap in the record; none is implied to exist.

**Security and identity**
- A deny-list of workload identities, for locking out an enrolled workload before its credential lapses (ADR-0004, ADR-0016 as amended).
- Duplicate-identity (cloned image) detection on connect, with forced re-enrollment (ADR-0004 as amended).
- A server-side evidence guard on mode changes, refusing an unacknowledged promotion at the domain layer; today the gate lives in the console (ADR-0001 as amended).
- The signing tripwire: an authenticity envelope for rendered policy before it transits anything but the mutually authenticated sync stream (ADR-0012, ADR-0018).

**Agent**
- A netlink address-change watch replacing the 30-second re-read of the host's addresses (ADR-0002 as amended).
- Sampling, buffering to disk across restarts, and a whole-process memory budget (ADR-0011 as amended); reading the connection-table dump in bounded batches.
- An installer that wraps `innerwall-agent enroll` and the daemon's service unit.
- Outbound observation, which arrives with outbound enforcement (ADR-0010 as amended).

**Operator surface and console**
- Cursor pagination for the configuration collections (ADR-0007 as amended).
- A response-validation harness: the surface's responses checked against `api/openapi.yaml` in tests.
- The command line over the surface, holding an operator token, as the single authentication plane (ADR-0021).
- Notifications, saved views, definitions screens, and a findings collection.

**Scale**
- An inverted label index that narrows each render to the workloads a change can affect (ADR-0018).
- Time-partitioned flow tables with retention by partition drop (ADR-0009, ADR-0019).

**1.0 gate (unchanged):** M1 to M5 complete (shipped, above), an upgrade path documented, and a security review of the enrollment and agent paths.

## Longer horizon

Each gated on its own ADRs: an eBPF collector as an additive backend beside conntrack, and a Windows agent as a second enforcement backend behind the enforcer interface (ADR-0003).

## Non-goals (deliberate)

- No orchestrator dependency: heterogeneous server estates are the point
- No custom datapath, kernel module, or overlay network
- No agent self-update (revisited only with signing and staged rollout worthy of it)
