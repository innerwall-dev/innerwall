# Changelog

Notable changes to Innerwall, newest first. Versions follow semantic versioning; before 1.0, a minor version may change anything.

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
