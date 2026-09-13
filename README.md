# Innerwall

Open-source microsegmentation for heterogeneous server estates: VMs, bare metal, and cloud instances, no orchestrator required.

A small agent on each workload observes network flows and programs the operating system's native firewall. A control plane turns label-based policy into per-host rulesets, pushes them over persistent streams, and renders the estate's real traffic as a live dependency map.

**Status:** pre-release. Enrollment, the sync stream, policy authoring from the command line, flow telemetry from the host to queryable storage, and enforcement and simulation on Linux (nftables) work end to end; the UI follows. See [ROADMAP.md](ROADMAP.md).

## Why

- **East-west traffic is the blind spot.** Perimeter controls say nothing about lateral movement, and most estates cannot even see it.
- **You cannot safely enforce what you cannot see.** The workflow is visibility, then simulation, then enforcement, and the platform enforces that order (ADR-0001).
- **The host already has a firewall.** Innerwall programs the existing packet filter atomically and observably instead of shipping a datapath (ADR-0003).
- **Central planes fail by being chatty.** Agents hold one persistent stream; policy moves as versioned desired state; nothing polls (ADR-0002).
- **Fail static.** Losing the control plane changes nothing on any host. Last-known policy stays enforced from local disk (ADR-0011).

## Quickstart

The supported v1 deployment is one control-plane binary and one Postgres (ADR-0017):

```sh
git clone https://github.com/innerwall-dev/innerwall.git
cd innerwall
make dev        # docker compose: control plane + Postgres
```

Enrolling an agent takes a provisioning token and nothing else:

```sh
curl -fsSL https://<control-plane>/install.sh | sh -s -- --token <provisioning-token>
```

Today the same exchange is `innerwall-agent enroll --server <host:port> --token <provisioning-token> --bootstrap-ca <ca.crt>`; the installer wraps it in a later milestone. See `docs/deploy/README.md`.

## Layout

| Path | What lives there |
|---|---|
| `proto/` | Source of truth for the agent contract (ADR-0007) |
| `cmd/innerwall`, `cmd/innerwall-agent` | Control-plane and agent binaries |
| `internal/` | Control-plane services and agent loops, one package per boundary |
| `internal/store/` | Hand-written SQL, goose migrations, sqlc output (ADR-0006) |
| `ui/` | Vite + React SPA, embedded into the control plane (ADR-0008) |
| `docs/adr/` | Architecture decision records: the design's source of truth |
| `docs/development.md` | Prerequisites, make targets, where things live |

## Contributing

Read [ARCHITECTURE.md](ARCHITECTURE.md), then [docs/adr/](docs/adr/), then [CONTRIBUTING.md](CONTRIBUTING.md). Every commit is DCO signed off. Every PR is reviewed against the ADRs, including by an automated pass.

## License

Apache-2.0. The Innerwall name and marks are retained by the project (ADR-0013).
