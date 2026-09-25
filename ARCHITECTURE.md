# Innerwall Architecture

**Status:** Living document. Decisions with lasting consequences are recorded as ADRs in `docs/adr/`; this document describes the system those decisions produce. Where the two disagree, the ADR wins and this document has a bug.

---

## 1. What Innerwall is

Innerwall is an open-source microsegmentation platform for heterogeneous server estates — VMs, bare metal, and cloud instances, with no orchestrator required. A lightweight agent on each workload observes network flows and programs the operating system's native firewall. A central control plane turns label-based policy into per-host rulesets, distributes them, and renders the estate's real traffic as a live dependency map.

The product thesis, argued from first principles:

1. **East-west traffic is the blind spot.** Perimeter controls see what enters and leaves a network; they say nothing about what moves laterally inside it. Most damage in a compromised environment comes from lateral movement, and most environments cannot even *see* that traffic, let alone constrain it.
2. **You cannot safely enforce what you cannot see.** Writing segmentation rules against an unobserved network is guesswork, and guesswork with firewalls causes outages. Therefore visibility comes first, simulation second, enforcement last — as a workflow the product enforces, not a best practice it suggests.
3. **The host already has a firewall.** Every mainstream OS ships a capable packet filter. A segmentation system does not need its own datapath; it needs to *program the existing ones* correctly, atomically, and observably. This keeps the agent small, the failure modes legible, and the performance cost near zero.
4. **Central planes fail by being chatty.** Any design where thousands of endpoints poll a central API — and where humans, dashboards, and automation hit the same API — eventually meets rate limits, thundering herds, and throttled front doors. Innerwall is push-based and desired-state from day one so that class of failure is designed out rather than mitigated later.

## 2. Design principles

These recur throughout the system and are the tie-breakers when decisions conflict:

- **Visibility → simulation → enforcement.** Read-only value first; enforcement is opt-in, per scope, after the operator has seen what would break.
- **Desired state, reconciled — never imperative commands.** The control plane declares what each host's ruleset should be; agents converge on it and report what they actually run. Drift is detectable by definition.
- **Push over poll.** Long-lived streams carry policy down and telemetry up. Nothing in the steady state polls.
- **Separate the read path from the policy path.** Analytical load (dashboards, flow queries, exports) must never contend with policy distribution.
- **Fail static.** Loss of the control plane changes nothing on any host: last-known policy stays enforced, telemetry buffers locally. Control-plane availability is a convenience property, not a safety property.
- **Boring to operate.** One control-plane binary, one Postgres. Everything that could be a second stateful system is behind an interface until scale forces it into existence.
- **Design the seam now, implement later.** Flow storage, the CA, relays, and federation all exist today as interfaces with a single default implementation. Scaling is a documented path, not a rewrite.

## 3. System overview

```
                         ┌────────────────────────────────────────────┐
                         │              Control plane                 │
                         │  (single Go binary, N stateless replicas)  │
                         │                                            │
   Browser ──────────────┼─► API service ──► Policy compiler          │
   Automation ─ REST/JSON│      │                  │                  │
                         │      ▼                  ▼                  │
                         │   Postgres ◄──── compiled ruleset versions │
                         │      ▲                  │                  │
                         │      │                  ▼                  │
   Agents ══ gRPC/mTLS ══╪═► Agent gateway ◄── push per-agent deltas  │
   (persistent streams)  │      │                                     │
                         │      ▼                                     │
                         │   Flow ingestion ──► FlowStore (Postgres)  │
                         │   CA / identity service (embedded)         │
                         └────────────────────────────────────────────┘

   Host ┌──────────────────────────────┐
        │ innerwall-agent (static Go)  │
        │  sync loop ── policy in      │
        │  collect loop ── flows out   │
        │  reconcile loop ── nftables  │
        │  health loop ── heartbeat    │
        └──────────────────────────────┘
```

## 4. Control plane

One deployable binary, internally organized as services with clean boundaries so they can split into separate processes if scale ever demands it (ADR-0017). All durable state lives in Postgres; replicas are stateless and interchangeable. The signing authority's key is the one exception: operator-provisioned configuration, identical on every replica.

### 4.1 API service

Serves the UI and automation over a REST/JSON surface of hand-shaped HTTP handlers over the shared domain layer (ADR-0007 as amended by ADR-0021), on a listener of its own beside the agent gateway. The handlers carry the transport semantics a browser needs and hold no domain logic; they call the same domain functions the command line calls, and that shared layer is what keeps the two transports from drifting. Protobuf remains the source of truth for the agent contract only. The operator listener serves TLS only: an operator-supplied certificate, or a self-signed one generated on first start, persisted beside the authority directory, and announced by its SHA-256 fingerprint in the log. Version 1 has a single operator and binary authorization. The operator logs in with a password set from the command line on the host (there is no endpoint that sets it) and receives a fixed-lifetime session cookie, or presents an operator token as a bearer credential from automation; a middleware resolves either to the one principal and handlers never learn which. Writes are refused when a browser marks them cross-origin, which is the whole forgery defense for a console served from the same origin; login attempts are throttled per source in process. The command line remains a second authentication plane, holding database credentials and calling the same domain functions directly; consolidating it onto the surface is the recorded v2 direction. Design rules that exist specifically to keep a central API healthy at estate scale (ADR-0007 as amended):

- **Aggregation is server-side**, never assembled client-side from raw rows; aggregate reads return bounded rollups.
- **Unbounded reads paginate by cursor**; no offset pagination, no unbounded lists.
- **A mutation that fans out returns a recorded intent immediately** and never blocks on convergence. Convergence is observed through the read model (applied version against latest), not through a job resource.
- **Bulk operations exist only where the console's screens demand them**, each with its own record; generic bulk operations are out of v1.

The read model behind the console's screens lives in `internal/readmodel`, one package of pure reads over persisted state that the handlers and the command line both call. Its endpoints are a grouped flow rollup (`GET /api/v1/flows/rollup`: one of four named groupings over the stored windows, aggregated in the store, bounded to a documented number of groups with the totals of the whole and a marker when truncated, and honest about the window boundaries it actually covered), a cursor-paginated list of one workload's windows (`GET /api/v1/flows`, which has no unbounded form), the fleet and one workload in one object shape (`GET /api/v1/workloads`, `GET /api/v1/workloads/{id}`: identity, labels, mode, sync state against the latest rendered version with the instant the last snapshot was sent, and the agent's health as its heartbeats recorded it, including a credential state derived only from the stored expiry, the stored renewal error, and the clock), and a workload's persisted rendered policy as configuration (`GET /api/v1/workloads/{id}/rendered-policy`, no counts; the console pairs it with a rollup). Peers in every flow read are what ingestion resolved and stored (ADR-0019); the read model adds only the current display name of a stored identity.

The write model is two domain packages the handlers and the command line share. `internal/policy` authors rulesets, rules, services, and address groups: admission runs there and collects every finding with a stable rule name and a path, so the surface's `400` problem and the command line's output are the same findings; every update and delete is conditioned on the version the caller last read, checked inside the write, and refused with the current version when the object has moved (the surface carries this as `If-Match`, `428` when absent, `412` when stale); the version is a stored integer every write advances by one, and the token a caller holds is compared byte-exact against its decimal form, never parsed; a rule keeps its own `created_at` across an edit of its ruleset and its `updated_at` and version move only when the rule itself changed. `internal/fleet` edits a workload's labels (versioned by content), changes the enforcement mode of a set of workloads, previews what a selector resolves to, dry-runs a hypothetical policy set, and directs one workload's agent to reconnect (`POST /api/v1/workloads/{id}/resend-snapshot`, and `innerwall workload resend-snapshot` on the command line). A directed reconnect is refused for an unknown workload and, with the agent's last-seen instant, for a workload whose recorded sync state is offline (an approximation of whether a stream exists, not a presence check); otherwise it publishes the directive on the notification bridge, writes nothing, and answers with the snapshot instant as it stood, which a later workload read shows advancing once the agent has reconnected. A mode change (`POST /api/v1/mode-changes`) is the one bulk operation the console's screens demand: in one transaction under the render lock it resolves its selector through the renderer's own scope match (the same resolution the preview and the scoped reads use, so a preview and the change it precedes cannot differ), refuses when the resolved count is not the one the operator expected, records the intent as the set it resolved to, flips the desired mode of exactly that set by id, and renders; the response acknowledges the recorded intent and nothing about progress, and convergence is observed on the workload reads as applied version against latest. A dry run (`POST /api/v1/policies/render-dryrun`) admits the hypothetical set as a write would, renders it against a read-only snapshot of persisted state, diffs each workload through the same delta implementation the sync stream uses, and discards everything; it reports the derived version of the state it computed against so a stale author can tell. Provisioning tokens and operator tokens are minted (secret shown once), listed (the listing prefix and metadata only; a provisioning token minted before the prefix was kept lists none), and revoked on the same surface, through the domain functions the command line's token commands call. The whole surface is described by a hand-authored OpenAPI 3.1 document, `api/openapi.yaml`, linted in CI and checked by a test against the routes the surface mounts; nothing conforms responses to it at runtime in this version.

### 4.2 Agent gateway

Terminates agent mTLS and holds one persistent bidirectional gRPC stream per agent (ADR-0002). Policy deltas go down; heartbeats, inventory reports, and version ACKs come up on the same stream; flow telemetry has its own RPC so it can never head-of-line-block a policy update (ADR-0015). Every stream begins with a snapshot of the workload's persisted rendered policy; deltas are sent only within that stream, pipelined without waiting for acknowledgements. A FAILED acknowledgement marks the workload degraded and is answered with a fresh snapshot, never a retried delta. Where it records an acknowledgement the gateway also stamps its instant: `last_acked_at` for an applied version (the version a Hello claims is not one) and `last_apply_failed_at` for a failure, which a later successful apply leaves in place while the sync state says whether it still stands. The gateway keeps one in-process map from workload to live stream and nothing else: a render, in whichever process performed it, announces changed workloads on a Postgres notification channel, and the replica holding a workload's stream pushes the delta from what that stream last received to the persisted policy (ADR-0018). Because any stateless replica must be able to accept any agent, that is how "push to agent X" finds the connection without a presence table. A directive for one workload's stream rides the same channel, keyed by the workload: the replica holding that stream sends the `Reconnect` directive and every other replica drops it. Delivery is best-effort by design, since no replica can know for certain that a stream exists; the gateway stamps `last_snapshot_sent_at` whenever it writes a snapshot to a stream, whatever the cause, and a directed reconnect is observed as that instant advancing (ADR-0018 as amended).

Reconnection storms are a first-class design case: a control-plane restart means every agent reconnects, each with a CPU-expensive mTLS handshake. Agents use exponential backoff with full jitter (`wait = random(0, min(cap, base × 2^attempt))`), which turns synchronized waves into a smooth trickle.

### 4.3 Policy compiler

Consumes the authored model (rulesets with label selectors, services, address groups) plus current registry state and renders **per-workload policies**: label selectors resolve to the workloads that match them and to those workloads' current addresses as host routes, address groups to their CIDRs, services to one resolved rule per protocol, with the authored rule id kept for provenance. Rendering runs on every change, synchronously, inside one transaction under a Postgres advisory lock, and re-renders every workload; the previously persisted policy is diffed against the new one and a workload's version advances only when its rendered output changed (ADR-0018). The blast radius of a version bump therefore follows the blast radius of the edit even though the render does not; an inverted label index is the documented path to narrowing the render itself. Rendered policies and versions are durable in Postgres. Agents ACK the version they run; desired vs. actual version is a queryable fact, and sync drift is an alert, not a mystery. Compilation is inbound-only in v1 (ADR-0010); outbound rules are rejected at admission.

### 4.4 Flow ingestion

Receives pre-aggregated flow windows from agents on a telemetry stream of their own (each record classified on the host: OBSERVED in visibility, ALLOWED with the matching rule by connection mark, BLOCKED or WOULD_BLOCK from the terminal rule's log, ADR-0020), validates them, resolves each record's source address once, at ingest, against the registry as it stands at that instant (a workload with a snapshot of its labels, else the most specific address group, else the bare address), and writes the window and its cumulative totals through the FlowStore in one transaction (ADR-0019). In v1 every flow is reported once, by its inbound end (ADR-0010), so nothing is deduplicated; when outbound observation lands, both ends report and ingestion reconciles them. Agent-side aggregation is the load-bearing decision here: agents roll flows up into `(src, dst, port, proto, count, bytes)` tuples over a 1–10 minute window before shipping, cutting central volume by orders of magnitude (ADR-0009). A scheduled job prunes windows older than a configurable horizon, one replica at a time under an advisory lock; totals are never pruned.

### 4.5 Identity / CA service

An embedded signing authority issues short-lived workload certificates. It sits behind the `ca.Authority` interface so external issuers can replace it without touching enrollment logic (ADR-0016). Version 1 ships a file-backed authority (key and self-signed root created once by `innerwall ca init`); the interface anticipates signing by a secrets manager or hardware-backed key. The authority directory is operator-provisioned configuration, distributed identically to every replica alongside the database connection string and listener certificate; it is the one durable artifact deliberately kept out of Postgres, because a signing key readable by everything that reads the database is not a signing boundary (ADR-0017). Every backend reads a signing request through one function that returns the public key and nothing else: the subject, names, and extensions an enrollee requests never reach a certificate.

### 4.6 High availability

- Control-plane replicas are stateless; HA is N replicas behind a load balancer plus a properly HA Postgres. Postgres is the availability story. The signing authority's key is configuration provisioned identically on each replica (or held by an external backend), not state a replica accumulates (ADR-0016).
- The property that actually makes the system safe is agent-side: **fail static** (ADR-0011). With that in place, control-plane downtime degrades management, never enforcement.
- v1 sizing honesty: single binary + single Postgres (co-located or adjacent) is the supported deployment until real estates demand more. The seams for splitting are designed; the split is not built.

## 5. Agent

A single static Go binary (`innerwall-agent`), structured as four independent loops sharing local state:

1. **Sync loop** — maintains the gRPC stream, applies each snapshot or delta strictly in order as one complete policy through the installed-policy store, ACKs versions (FAILED with the reason when an apply is refused, staying on the last good version), reports inventory and heartbeats at the intervals the control plane sets, and reconnects with jittered backoff, always from a fresh snapshot. A renewal timer beside it rotates the credential when less than a third of its lifetime remains and swaps it in without dropping the stream.
2. **Collect loop** — reads inbound connections from the kernel's connection tracker (v1) behind a source interface, aggregates in memory over the reporting window the control plane configures, holds closed windows in a buffer bounded in records (oldest dropped first, every drop counted into the heartbeat), and ships one window per `ReportFlows` stream on a connection of its own with its own backoff (ADR-0019). Buffering to local disk across restarts is additive behind the same buffer. eBPF-based collection is a later, additive backend behind the same source interface.
3. **Reconcile loop** — converges the host firewall onto the desired ruleset. On Linux this means an **Innerwall-owned nftables table**, replaced atomically as a unit: rule application is all-or-nothing, never a partially applied ruleset, and never a mutation of tables owned by other software (ADR-0003). The table holds one named set per resolved rule and address family and one input chain: accept loopback, accept established and related (which is why the agent can never cut its own control-plane stream), one accept per rule that writes the rule's number into the agent's claimed region of the connection mark (bits 16 through 31, the other bits untouched), then a terminal rule that logs and drops (enforced) or logs, marks, and accepts (simulation); visibility installs no chain. Peer-only changes are set element updates in one transaction. Every successful apply persists the policy to disk, the daemon re-applies it on start before dialing anything, and the rules stay in the kernel when the daemon exits; `innerwall-agent down` is the only thing that removes them (ADR-0020).
4. **Health loop** — heartbeats (piggybacked on the stream), resource self-accounting, and local safety controls.

Safety properties (ADR-0011):

- **Fail static.** Disconnection changes nothing. Last-known policy remains enforced from disk across agent restarts and host reboots: the persisted file is checksummed, a corrupt one applies nothing and is reported, and the kernel keeps what it holds.
- **Local kill switch.** An operator with root on the host can always disable enforcement locally (`innerwall-agent down` deletes the Innerwall table and nothing else) without control-plane involvement. Root on the box outranks the platform — by design, and stated loudly, because it is the first question a security architect asks.
- **Resource budgets.** The agent enforces caps on its own memory and flow-buffer disk usage; when exceeded, it degrades telemetry (sampling, then dropping) before it ever degrades the host.
- **No self-update in v1.** The agent updates through the host's normal package management. A platform that can silently replace its own root-privileged binary is a supply-chain liability; that convenience is deferred until it can be done with proper signing and staged rollout.

## 6. Identity and enrollment

The hard problem is the first certificate; everything after is routine mTLS rotation.

- **Provisioning tokens** carry the constraints of enrollment: the label set they assign, an expiry (30 days by default), and revocation. A token is `iw_` plus 32 random bytes; the control plane looks it up by its SHA-256 hash, keeps beside that only a listing prefix (the `iw_` prefix and eight leading characters), and shows the plaintext once at mint. A token may enroll many workloads within its scope, so it can be baked into an image or a provisioning pipeline (ADR-0015, ADR-0016).
- **Enrollment.** The agent generates its keypair locally (the private key never leaves the host), submits CSR + token + host facts over server-authenticated TLS, and receives a short-lived certificate plus the authority bundle. The trust anchor for that first connection is distributed out of band with the token: the token proves the agent to the control plane, the anchor proves the control plane to the agent.
- **Identity lives in the cert; attributes live in the registry.** Certificates carry exactly one URI SAN, `innerwall://workload/<uuid>`, with a control-plane-assigned UUID; one package formats and parses it. Mutable facts — hostname, labels, enforcement state — never enter the certificate. Cert = authentication; registry = authorization.
- **One listener, one boundary.** The enrollment service and the agent service share a TLS listener. Client certificates are verified when presented; a server interceptor requires one, with a parseable workload identity, for every service except enrollment, and hands the identity to handlers through the request context. Handlers read identity from nowhere else. The operator surface is a sibling listener with its own TLS configuration and its own credentials (ADR-0021); the agent listener's boundary is not stretched to cover operators.
- **Rotation over revocation.** 24h credential lifetimes with renewal via authenticated re-CSR over mutual TLS; the same identity is reissued with a fresh serial. The daemon renews when less than a third of the lifetime remains, jittered, and swaps the credential in without dropping its stream (ADR-0016). Short lifetimes mostly obviate revocation machinery; a control-plane deny-list of workload IDs covers the rest. A workload that misses its renewal window re-enrolls.
- **Designed-for edge cases:** clock skew (small `notBefore` backdating), cloned VM images presenting duplicate identities (detected on connect, forced re-enrollment), signing-key custody (the interface anticipates external key holders).

Trust-on-first-use is explicitly rejected: an enrollment window where anyone reachable can register as anything is unacceptable for a system that will enforce policy and whose policy discloses network topology.

## 7. Policy model

- **Labels, not addresses.** Workloads carry labels (e.g. `role=db`, `env=prod`, `app=billing`); rules are written between label sets. IPs are an output of compilation, never an input to policy.
- **Draft → simulate → provision.** Edits accumulate in a draft with a first-class diff against active policy. Simulation replays observed flows against the draft and reports exactly which real, recent connections would have been blocked. Provisioning creates an immutable numbered version — the unit of audit and rollback.
- **Enforcement scope, v1: inbound only** (ADR-0010). Each workload's ruleset constrains what may reach it. Inbound-only halves the policy surface an operator must reason about, and it is the direction where a mistake is legible (a blocked *inbound* dependency shows up in the map immediately). Outbound enforcement arrives in v2 as a layered model: shared **baseline policies** for estate-wide dependencies (DNS, directory services, NTP, package mirrors) composed under **app-scoped policies**, so application teams never re-declare infrastructure.
- **Enforcement is per-scope and gradual.** Workloads move visibility → simulated → enforced individually or by label selection; nothing forces estate-wide enforcement as a single event.

## 8. Flow data

- **Schema:** two column-shaped tables (ADR-0019). `flow_windows` holds one row per aggregated record per reporting window: workload, window bounds, the peer resolved at ingest (kind, identity, label snapshot), destination port, protocol, direction, decision, matched rule, counters, first and last seen. `flow_totals` holds one row per (workload, resolved peer, port, protocol, direction, decision), upserted at ingest with running counters and the first and last instants seen, and is never pruned.
- **Peers are resolved at ingest**, never by a query-time join, because addresses are reassigned and labels change: a stored row is what the operator would have seen at the time.
- **v1 store: Postgres only** (ADR-0009), retention by a bounded periodic delete of aged windows (30 days by default), with time partitioning as the recorded path when volume demands it. With agent-side aggregation this comfortably serves tens of millions of flow records per day.
- **Query shapes the schema serves:** a workload's windows over a time range; a decision-filtered rollup over a label scope grouped by peer and service; a workload's totals since first seen; and, for the operator surface, four named groupings over the same windows (by rule; by rule and peer; by source peer and workload; by workload and service), each one statement, plus a cursor-keyed page of a workload's windows. The `innerwall flows` commands and the surface's read endpoints issue exactly these (ADR-0019 as amended).
- **The seam:** all access goes through a `FlowStore` interface, and the schema is written to port cleanly to a columnar store. A second stateful system enters the deployment only when a real estate's query latency demands it — and when it does, the read path moves wholesale, keeping analytical load permanently off the policy plane.

## 9. UI

A static single-page application (Vite + React) embedded into the control-plane binary via `go:embed` — one artifact to deploy, no server-side rendering, no separate frontend service (ADR-0008). The UI speaks only the public REST surface, which keeps that API honest: anything the UI can do, automation can do.

Surfaces, in product order:

1. **Flow map** — the live dependency graph (ReactFlow), aggregated by label group rather than per-host, because a thousand-node hairball is not visibility. Drill-down expands groups to workloads to flows.
2. **Policy editor** — draft/diff-first. The diff against active policy *is* the review artifact.
3. **Simulation view** — the draft replayed against observed flows: "these seventeen real connections from the last week would have been denied."
4. **Workload inventory** — registry, labels, agent health, desired-vs-actual ruleset version.
5. **Enrollment** — generate scoped, expiring, pre-labeled provisioning tokens with a copy-paste install command. Deliberately polished: it is the first thirty seconds of every evaluation.

## 10. Scale-out roadmap (designed, not built)

Two extensions exist today only as documented seams (ADR-0012):

- **Relay tier.** For very large estates, agents connect to a nearby relay; relays multiplex to the core, collapsing connection counts by orders of magnitude. Relays cache **signed policy bundles** — the control plane signs compiled rulesets, so a relay can serve cached policy during a core outage without ever being trusted to *author* policy. Agents always retain fallback-to-direct.
- **Regional federation.** Multi-region estates run regional control planes under a thin global coordinator. Label-membership summaries sync across regions so cross-region policy compiles correctly; raw flow data never leaves its region (a compliance property, not just a bandwidth one). The CA extends hierarchically with per-region intermediates. Region isolation is the failure-mode goal: a severed region keeps enforcing and keeps serving its own map.

## 11. Security posture

- The agent runs privileged because programming the host firewall requires it; everything else is minimized — static binary, no runtime downloads, no self-update, no listening ports (the agent only dials out).
- The control plane is the high-value target and is treated accordingly: policy provisioning is versioned and audited, enrollment is token-gated, agent identity is short-lived, and the deny-list provides immediate lockout.
- Supply chain: reproducible static builds, signed releases, DCO-enforced provenance on every commit (ADR-0013).

## 12. Document map

| Concern | Where |
|---|---|
| Why visibility precedes enforcement | ADR-0001 |
| Agent transport & desired-state sync | ADR-0002 |
| Native-firewall enforcement | ADR-0003 |
| Identity, enrollment, CA, renewal | ADR-0004, ADR-0016 |
| Control-plane shape & HA | ADR-0017 (supersedes ADR-0005) |
| Data access layer | ADR-0006 |
| API surface | ADR-0007 |
| UI delivery | ADR-0008 |
| Flow storage | ADR-0009 |
| Enforcement scope | ADR-0010 |
| Agent safety properties | ADR-0011 |
| Relays & federation seams | ADR-0012 |
| License & provenance | ADR-0013 |
| Docs & review workflow | ADR-0014 |
| Agent wire contract | ADR-0015 |
| Policy rendering, delta versioning, rendered state | ADR-0018 |
| Flow storage, ingest-time resolution, retention | ADR-0019 |
| Enforcement backend: owned table, marks, logged drops, persisted policy | ADR-0020 |
| Operator surface: second listener, TLS only, single-operator authentication | ADR-0021 |
