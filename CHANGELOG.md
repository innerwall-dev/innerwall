# Changelog

Notable changes to Innerwall, newest first. Versions follow semantic versioning; before 1.0, a minor version may change anything.

## Unreleased

- **Admitted untracked traffic is accepted** (#142, #154). A rule's accept also wrote the connection mark, and the kernel ends a rule at that write for a packet it does not track, so admitted untracked traffic fell through to the terminal rule. IPv6 neighbor discovery is never tracked: a rule admitting link-local ICMPv6, the simulation review's own remedy, recorded neighbor discovery as would-block in simulation and dropped it under enforcement, which breaks IPv6 neighbors. Each rule's accept is now paired with one that admits the same traffic unmarked when it is untracked; tracked traffic is marked and attributed exactly as before, and the two modes still differ in the terminal rule alone (ADR-0020 as amended). Agent-side only: no migration and no wire change. Upgrade agents before enforcing on a host with IPv6.
- **The connection-mark mask, as `nft list` prints it** (#143, #154). A listed rule shows `ct mark & 0x000Nffff | 0x000N0000`, which read as a mask leaking region bits. It is `nft`'s reading of the kernel's mask-and-xor form of the rendered `(ct mark & 0x0000ffff) | 0x000N0000`, the same write; the rendering is unchanged, and tests now pin the rendered mask and the installed bytecode. The promotion guide and ADR-0020 say how the listing reads.
- **A quiet workload is no longer called silent** (#144). The agent ships only the flow windows that hold something, so a workload that receives little inbound traffic — a bastion, a standby replica — goes minutes between reports while its agent runs and observes. v0.3.0's silence derivation read every such stretch longer than two windows as an agent that was not reporting, and wrote `agent_silent` gaps for evidence that was never lost; the simulation review drew them as one interval spanning the hour and failed its verdict. Every heartbeat already carries the agent's uptime, so the control plane now records the process start each heartbeat implies (migration 00016) and derives no gap for a silence one agent process demonstrably ran through: one that started before the previous report and was still heartbeating within a window of the arriving one (ADR-0019 as amended). A stopped agent still derives its gap with the same bounds — a restarted process's heartbeats place its start after the silence began, and a dead process's last heartbeat is too old to cover the report that ends it — and a quick restart still records nothing. Gaps v0.3.0 already wrote for quiet stretches stay in place, since nothing stored tells them from an outage, and age out with flow retention. The agent is untouched; the migration makes the next release v0.4.0.

## v0.3.0 — the silence gap, and operator docs

An agent process that is down now counts against the evidence its workload's verdict rests on, and the operating loop is documented end to end: task guides, a reference for both command lines and the operator API, and a troubleshooting catalog grown from live operation.

- **Operator documentation** (#43, #52, #100, #130). Task guides for the operating loop under [docs/guides/](docs/guides/): labeling, the first ruleset, reading a simulation review, promotion and enforcement (with the kill switch and fail static), the credential lifecycle, and upgrading in place. A [multi-host evaluation](docs/deploy/multi-host-evaluation.md) guide for agents on other machines. A reference under [docs/reference/](docs/reference/): both command lines, the operator API, workload states and every evidence-gap kind, versioning and install. [docs/troubleshooting.md](docs/troubleshooting.md) grows from two entries to fifteen. The deploy guide no longer calls the console unshipped, and its `flows list` example puts the flags before the workload id, the order the command reads. `api/openapi.yaml` states that the `sync_state` filter does not exclude workloads whose enforcement was removed on the host. The console captures gain per-theme singles beside the composites, and the capture script writes both forms.
- **An agent's silence is an evidence gap** (#114). An agent process that is down leaves the owned table enforcing (fail static, ADR-0011) but observes nothing, and nothing used to say so: a simulation review over the outage could read "safe" on evidence that was never collected. The control plane now derives the gap itself — flow ingest already stamps every report's receipt, so a report that ends a silence longer than the telemetry-liveness allowance (two flow windows) writes an `agent_silent` gap from the previous receipt to the arriving window's start (migration 00015, ADR-0019 as amended). A window that reaches back over the silence is buffered evidence delivered late and derives nothing, so a telemetry outage the agent rode out, and a quick agent restart, record no gap. The review's verdict, the workload page, `GET /api/v1/flows/gaps`, and `innerwall flows gaps` state the new kind like any other; the agent is untouched, and a reported gap claiming the kind is refused at ingest.

## v0.2.0 — console v3, and honest state

The console is restyled to design package v3, with a new workload page, and it says more truthfully what each host is doing: a removed table, a reconnecting control channel, a drifting clock, and a late renewal each read as what they are. Credential expiry is no longer a cliff, and a label means one thing on every surface.

### Console

- **Design package v3** (#118). New tokens and type: DM Sans and Source Code Pro, self-hosted. The chrome, controls and tables are restyled, with 40px rows. The Workloads table gains an Agent column. Every flow map edge is drawn at one 2px width, with decision carried by dash, color and glyph, never by width. The stored theme paints from the first frame. The package is recorded in [`docs/design/`](docs/design/PACKAGE.md). Between the two releases the console was first restyled to package v2 (#27); v3 supersedes it.
- **The workload page** (#118):
  - the workload's identity, mode, and last contact sit in the header strip;
  - the page lands on an Overview of alert cards, status, labels, host facts, and the evidence-gap history;
  - exceptional states are chips beside the tabs on every page, critical first, each opening its explanation;
  - Inbound flows and Listening services gain a filter panel.
- **Search, edit, and navigate** (#85):
  - a search palette behind the header box and ⌘K finds workloads by hostname or label and rulesets by name;
  - a workload's labels are edited in place, as a conditioned write with a review of what changes;
  - the Workloads badge counts the workloads needing attention;
  - an address with no screen renders "No such page" instead of redirecting;
  - "Open in policy editor" carries the pair's peers, services, and matched rule into the editor.
- **Policy editor** (#76, #80):
  - every finding is reported in one pass, for rulesets, rules, and services;
  - address-shaped peer text is read as a CIDR, typo or not;
  - a pending scope edit and rule edit save as one write, so the previewed combination is what lands;
  - rulesets can be renamed and redescribed;
  - creating a ruleset lands on it;
  - a click on the promotion's held-back confirm keeps the dialog.
- **The control plane's version** shows in the account popover, from `/me` (#87).
- **Flow map edges route around the nodes** (#121). An edge that skipped a layer used to run under the cards in it, and a node's edges all left one point, so a fan read as one bundle and its labels landed at ragged offsets. Each edge now follows its own lane, crossing columns level and bending only between them; every label sits in a slot kept for it on its own edge; and a node's edges attach at ports spread along its side.
- **The flow map's graph library and layout load only with the map** (#120). The main bundle drops from 891 kB to 636 kB.

### Agents and the control plane

- **Credential expiry is no longer a cliff** (#102, #110, ADR-0016 amended). A control-plane outage that spanned the agents' renewal window used to lapse every credential and force a re-enrollment per host.
  - The renewal call now accepts a credential expired by at most `--renewal-grace` (72h by default); every other call still requires a valid one.
  - Expiry is never fatal to the agent: it keeps enforcing from disk and keeps retrying renewal.
  - Enrollment may overwrite an expired stored credential.
  - Renewal is scheduled on monotonic time as well as the wall clock.
  - A late renewal is logged, recorded, and shown.
- **The kill switch is visible** (#95, #101). A host whose owned table was removed reports it on its next heartbeat.
  - Its flow reports carry a `table_removed` evidence gap for as long as the table stays removed.
  - The Workloads table, the workload page, the counts, the attention badge, and a `removed` filter all say so.
  - A kill-switched host no longer reads as healthy and idle.
- **Reconnecting is told from offline** (#95). Ingest records each workload's last flow report. A workload whose control channel is down while its reports keep arriving reads "Reconnecting"; one with no contact on either channel reads "Offline".
- **Agent clock skew is detected** (#81, #90).
  - Every flow report carries the agent's clock at send.
  - Skew beyond two windows is logged, recorded on the workload, and shown, with the remedy in [docs/troubleshooting.md](docs/troubleshooting.md).
  - A window open across a backwards clock step is cut at the step.
  - A window the control plane refuses as malformed is dropped and recorded as a `window_rejected` gap. It used to block every window behind it until a restart.
- **Directed reconnects** (#53, #107).
  - The agent redials at once on a directed reconnect, and resets its backoff once a stream is established.
  - The gateway logs every directive it cannot deliver.
  - A stream that ends no longer marks a newer live stream of the same workload offline.
- **One label grammar** (#34, ADR-0022). It is admitted wherever a label enters: token mint, label edits, scopes and peers, selectors, and read filters. A paste such as `app=web env=lab` becomes two labels, where it used to become one label no selector could match. The console's label inputs share one parser that mirrors the grammar, and a stored label outside it is drawn quoted.
- **A label scope can match either end of a flow** (#36, ADR-0019 amended). The flow map uses `endpoint=either`, so scoping to a workload that only sends now draws its traffic.
- **Both binaries report their tag and commit** (#71): `v0.2.0+<commit>`, or `dev+<commit>` for an untagged build.
- **Visibility engages connection tracking itself** (#32). On a host where nothing else referenced connection tracking, visibility observed nothing and reported no error. This was listed under v0.1.0 but merged after the tag, so v0.2.0 is the first release that carries it.
- **A peer-only change on a visibility table is one full apply** (#95). It no longer logs a set-update refusal blamed on the kernel.

### Build and project

- Go 1.26.9 and `golang.org/x/net` v0.60.0 clear the advisories reachable from the shipped code; golangci-lint moves to v2.9.0 with them (#35).
- The automated ADR review runs on `pull_request_target`, so it can authenticate on a fork's pull request; a review that did not post says so in the job summary (#73, ADR-0014 amended).
- [docs/policy.md](docs/policy.md) states the policy model: a ruleset's scope is the destination, one ruleset per tier (#77).

### Upgrading

- **Migrations.** Five are added, 00010 through 00014; all are additive. Apply them with `innerwall migrate` or `innerwall serve --migrate`.
- **Order.** Upgrade the control plane first, then the agents. A v0.1.0 agent keeps working against a v0.2.0 control plane: a report without a send time is judged by its window, and a heartbeat without the table's state leaves the stored one. The renewal grace, skew detection, and kill-switch reporting each work in full only with both ends at v0.2.0.
- **Labels.** Writes and read filters outside the grammar are now refused with findings; a read filter outside it was an empty result. Stored labels are not rewritten. Enrollment refuses a provisioning token whose stored labels fall outside the grammar: mint a new token for those labels. A workload's own labels outside it can be repaired in the console's label editor.
- **API.** Every surface change is additive:
  - `Me.version`;
  - `health.clock_skew`, `health.owned_table`, `health.last_flow_report_at`, `health.telemetry_live`, and `health.credential.last_late_renewal`;
  - `GET /api/v1/workloads/counts`;
  - the rollup's `endpoint` parameter;
  - the gap kinds `window_rejected` and `table_removed`.
- **Agent contract.** The changes are additive fields: `ReportFlowsRequest.sent_at`, the heartbeat's `owned_table`, and the two gap kinds.

### Known limitations

- The server-side evidence guard, the workload deny-list, and clone detection are not built (ROADMAP.md).
- The aggregation window is set in the control plane's defaults; `innerwall serve` exposes no flag for it.
- Linux only, inbound only, one operator.

## v0.1.0 — first release

The first tagged release: one control-plane binary with the operator console embedded, one Postgres, and a Linux agent. What it does is summarized in the [README](README.md); what it deliberately does not do yet is the backlog in [ROADMAP.md](ROADMAP.md).

### What is in it

- **Enrollment and identity.** Token-gated enrollment with no trust-on-first-use window; provisioning tokens scoped to labels, expiring, revocable, and stored only as digests; workload identity as one URI SAN assigned by the control plane; 24-hour credentials renewed automatically in the last third of their lifetime; a file-backed signing authority behind an interface (ADR-0004, ADR-0016).
- **Policy sync.** One persistent mutually authenticated stream per agent: a snapshot on every new stream, deltas within it, versions that advance only when a workload's rendered policy changes, full-jitter reconnects, and directed reconnects (ADR-0002, ADR-0015, ADR-0018).
- **Flows.** Inbound connections observed from connection tracking and the terminal rule's log, aggregated at the agent per window, reported on their own RPC, peers resolved at ingest, retained in Postgres behind the FlowStore interface, with evidence gaps recorded wherever the agent knows it lost evidence (ADR-0009, ADR-0019).
- **Policy, simulation, and enforcement.** Rulesets, services, and address groups authored live with conditional writes and structured findings; dry-run renders; simulation as the exact enforced ruleset with a logging terminal rule; enforcement into one owned nftables table in single kernel transactions, with per-rule sets, connection-mark attribution, logged drops, and a local kill switch; inbound only (ADR-0001, ADR-0003, ADR-0010, ADR-0020).
- **Operator surface and console.** One TLS-only listener with single-operator authentication (argon2id password set on the host, session cookie or bearer operator token), an origin guard on every write, and a hand-authored OpenAPI contract checked against the mounted routes; the console's flow map, fleet, workload detail, policy editor, and simulation review (ADR-0007, ADR-0008, ADR-0021).

### Fixed before release, from the conformance audits

Two independent audits of the release candidate (`main` at `04e3209`) found defects the test suite did not; each fix below landed with a regression test or a CI check where one could hold it.

- **A policy could be acknowledged without a persisted copy.** The agent applied to the kernel first and saved the policy after, logging a save failure and acknowledging anyway, so a reboot could restore an older version or none. It now persists durably first (file and directory synced), then applies, and acknowledges `APPLIED` only when both succeeded (#23).
- **Live connections were attributed to the wrong rule after a policy change.** Connection marks were positions in the installed policy, so adding a rule that sorted first renumbered every rule. Marks now come from a persistent allocator that never renumbers a rule and does not reuse a freed value until the region is exhausted; a connection names the rule that admitted it, or that rule once removed (#23).
- **The open flow window was unbounded.** It now holds at most `--flow-window-keys` distinct keys (50,000 by default) and records what it drops as a new `window_overflow` evidence gap, end to end through migration 00009 (#23).
- **The identity parser accepted non-canonical spellings** (upper-case and percent-encoded); it now accepts exactly the formatter's form (#23).
- **The login throttle's source table was unbounded within a window**; it is now bounded at 4,096 sources with oldest-first eviction (#24).
- **The permitted toolchain built known-vulnerable code.** Go is raised to 1.25.14 in `go.mod`, CI, and the image, and a CI job fails on any known vulnerability reachable from the shipped code (#24).
- **The console shipped font files without their license.** Every build now writes the license texts of all bundled third-party packages into the console (#24).
- **The capture script photographed real minted tokens** into committed composites; it now renders a visibly synthetic value and refuses to capture a real one, and composite 16 is recaptured (#24).
- **A fleet test depended on wall-clock timing**, the OpenAPI contract omitted the origin guard's 403 on 22 unsafe operations, and a rollup test fixture emitted a protocol the contract does not admit (#24).
- **Migrations had no immutability check.** CI now fails any change to a migration already on `main` (#24).
- **The automated ADR review enforced rules the record had amended away**, and the documents claimed it reviews every pull request; both are corrected (#24).
- **The image did not build.** The console stage lacked the API contract its build generates types from, so `make dev` failed; found while verifying this release's quickstart (#26).

### Record corrected before release

The decision record and documents stated controls and workflows the binary did not have. They are corrected in dated amendments, with no decision superseded (#25):

- the visibility → simulation → enforcement guarantee is the default path with evidence surfaced and an acknowledgment-gated promotion in the console, not a server-side refusal (ADR-0001);
- revocation in v1 is short-lived credentials and token revocation; a deny-list and clone detection are named v2 controls (ADR-0004, ADR-0016, SECURITY.md);
- reconnects receive a snapshot (ADR-0002), v1 observes inbound only (ADR-0010), telemetry is bounded in memory with sampling and disk buffering deferred (ADR-0011), the configuration collections return whole (ADR-0007), the aggregation window is a 60-second default (ADR-0009), the session cookie is `SameSite=Lax` with the origin guard as the write defense (ADR-0021), and merge commits are exempt from DCO sign-off (ADR-0013);
- the vocabulary rule now says what it is for: the implementation stack may be named, competing products may not be compared to.

### Removed

- `deploy/install.sh`, a stub that always exited with an error. Agents enroll with `innerwall-agent enroll`; an installer that wraps it is on the roadmap (#25).

### Known limitations

- The server-side evidence guard, the workload deny-list, and clone detection are not built (ROADMAP.md).
- The aggregation window is set in the control plane's defaults; `innerwall serve` exposes no flag for it.
- Linux only, inbound only, one operator.
