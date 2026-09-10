# Roadmap

Direction, not dates. Ordering follows ADR-0001: every milestone before enforcement delivers standalone visibility value. Numbers are milestones, not versions.

## M1 — Foundation (repo is reviewable before it is runnable)
- Docs: ARCHITECTURE.md, ADR set, CLAUDE.md, this file
- CI: build/test/lint, generated-code drift checks, DCO, ADR-conformance review
- Proto contract for enrollment, sync stream, flow reporting, inventory

## M2 — Enrollment and identity
- Control plane: Postgres schema + migrations, provisioning tokens, file-backed signing authority behind the `ca.Authority` interface
- Agent: enrollment client, mTLS bring-up, cert renewal loop
- `install.sh` — token in, enrolled agent out

## M3 — Flows and the map (first demo-able moment)
- Agent: conntrack collector, windowed aggregation, disk buffering, sync loop with fail-static persistence
- Control plane: gateway streams, ingestion (enrichment + bidirectional dedupe), Postgres `FlowStore`
- UI: embedded SPA shell, ReactFlow dependency map with label-group aggregation, workload inventory

## M4 — Policy, simulation, drafts
- Labels and label-based rules; policy compiler emitting versioned per-agent rulesets (inbound-only)
- Draft/diff editor; simulation replaying observed flows against drafts
- Desired-vs-actual version drift surfaced in inventory

## M5 — Enforcement (opt-in, per scope)
- Agent reconcile loop: owned nftables table, atomic replacement, local kill switch, resource budgets
- Per-workload / per-label enforcement state transitions: visibility → simulated → enforced
- Audit trail for provision and enforcement-state changes

**1.0 gate:** M1–M5 complete, upgrade path documented, security review of enrollment + agent paths.

## Post-1.0 themes (each gated on its own ADRs)
- **Outbound enforcement** as the layered model — shared baseline policies composed under app-scoped policies (ADR-0010)
- **eBPF collector** as an additive backend beside conntrack (ADR-0003)
- **Windows agent** — second enforcement backend behind the enforcer interface (ADR-0003)
- **Columnar FlowStore** implementation when a real estate's query latency demands it (ADR-0009)
- **Relay tier** with signed-bundle caching; **regional federation** with per-region intermediates (ADR-0012)
- External signing backends behind the existing interface (ADR-0016)

## Non-goals (deliberate)
- No orchestrator dependency — heterogeneous server estates are the point
- No custom datapath, kernel module, or overlay network
- No agent self-update (revisited only with signing + staged rollout worthy of it)
