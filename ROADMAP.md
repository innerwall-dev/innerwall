# Roadmap

Direction, not dates. Ordering follows ADR-0001: visibility value first, enforcement as the destination. Every item below that the record already names cites where; an item without a citation is a direction the record has not decided yet and gets its own ADR before it is built.

## Shipped in v0.1.0

The milestones M1 to M5 as first planned, delivered in this shape:

- **Foundation.** Architecture and decision record, CI (build, test, lint, generated-code drift, proto breaking-change gate, migration immutability, known vulnerabilities, DCO, automated ADR review), the agent contract in protobuf.
- **Enrollment and identity.** Provisioning tokens (scoped, expiring, revocable, hashed at rest), a file-backed signing authority behind the `ca.Authority` interface, one URI SAN per credential, 24-hour credentials renewed in the last third of their lifetime (ADR-0016). Enrollment is the agent's own command.
- **Sync and flows.** One persistent sync stream per agent with snapshot-then-delta policy and full-jitter reconnects; flow windows aggregated at the agent and reported on their own RPC, with peers resolved at ingest and evidence gaps recorded wherever the agent knows it lost evidence (ADR-0015, ADR-0019).
- **Policy, simulation, and enforcement.** Label-based rulesets, services, and address groups, edited live with conditional writes and a dry-run render; per-workload rendered policy versioned by diff (ADR-0018); simulation as the exact enforced ruleset with a logging terminal rule; enforcement into one owned nftables table, persisted before it is applied, with stable rule attribution and a local kill switch (ADR-0020); inbound only (ADR-0010).
- **Operator surface and console.** One TLS-only listener with single-operator authentication (ADR-0021); the flow map, fleet, workload detail, policy editor, and simulation review with an acknowledgment-gated promotion.

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
- A columnar FlowStore implementation, on a ClickHouse path, when a real estate's query latency demands it (ADR-0009).
- The relay tier and regional federation, on their designed seams (ADR-0012).

**1.0 gate (unchanged):** M1 to M5 complete (shipped, above), an upgrade path documented, and a security review of the enrollment and agent paths.

## Post-1.0 themes (each gated on its own ADRs)

- **Outbound enforcement** as the layered model: shared baseline policies composed under app-scoped policies (ADR-0010)
- **eBPF collector** as an additive backend beside conntrack (ADR-0003)
- **Windows agent**, a second enforcement backend behind the enforcer interface (ADR-0003)
- **External signing backends** behind the existing interface (ADR-0016)

## Non-goals (deliberate)

- No orchestrator dependency: heterogeneous server estates are the point
- No custom datapath, kernel module, or overlay network
- No agent self-update (revisited only with signing and staged rollout worthy of it)
