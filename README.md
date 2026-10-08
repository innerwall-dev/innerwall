# Innerwall

Open-source microsegmentation for heterogeneous server estates: VMs, bare metal, and cloud instances, no orchestrator required.

A small agent on each workload observes the connections that reach it and programs the operating system's native firewall. A control plane turns label-based policy into per-host rulesets, pushes them over persistent streams, and renders the estate's real traffic as a live dependency map. An operator console embedded in the control plane shows that map, the fleet, the policy, and what enforcing it would break.

**Status: v0.1.0, the first release.** Enrollment and short-lived workload identity, the policy sync stream, flow telemetry with evidence gaps, policy authoring with dry runs, simulation, and enforcement on Linux (nftables), and the operator console all work end to end. What is deliberately not in this release, and what comes next, is in [ROADMAP.md](ROADMAP.md); what changed, including the fixes from two independent conformance audits, is in [CHANGELOG.md](CHANGELOG.md).

## The console

Each image puts a screen of the shipped console in its dark theme (left) beside the same state in its light theme. More are in [`docs/img/console/`](docs/img/console/).

**Simulation review.** A ruleset's would-block traffic from the workloads simulating it, with the verdict on whether it is safe to enforce, the evidence it rests on, and the promotion to enforcement.

![Simulation review, dark and light](docs/img/console/01-simulation-review-grouped.jpg)

**Flow map.** The estate's real traffic between label groups, colored by what the policy decided or would decide.

![Flow map, dark and light](docs/img/console/07-flow-map-graph.jpg)

**Policy editor.** Rules edited live, with every admission finding in place and a dry run that shows each workload's rendered change before anything is written.

![Policy editor dry run, dark and light](docs/img/console/policy-editor-dry-run.jpg)

**Fleet.** Every workload's mode, sync state against its latest rendered version, and agent health.

![Fleet workloads, dark and light](docs/img/console/13-fleet-workloads.jpg)

## Why

- **East-west traffic is the blind spot.** Perimeter controls say nothing about lateral movement, and most estates cannot even see it.
- **You cannot safely enforce what you cannot see.** The workflow is visibility, then simulation, then enforcement: visibility is every workload's starting mode, the evidence is shown at each step, and promotion past evidence that does not support it takes an explicit acknowledgment (ADR-0001).
- **The host already has a firewall.** Innerwall programs the existing packet filter atomically and observably instead of shipping a datapath (ADR-0003).
- **Central planes fail by being chatty.** Agents hold one persistent stream; policy moves as versioned desired state; nothing polls the control plane (ADR-0002).
- **Fail static.** Losing the control plane changes nothing on any host. The policy an agent acknowledged is persisted before it is applied and stays enforced from local disk (ADR-0011, ADR-0020).

## Quickstart

The supported v1 deployment is one control-plane binary and one Postgres (ADR-0017). Run it with Docker Compose, or build it from source.

### With Docker Compose

```sh
git clone https://github.com/innerwall-dev/innerwall.git
cd innerwall
make dev        # builds the image (console embedded) and starts the control plane and Postgres
```

In a second terminal, set the operator password (the command line on the host is the only place it can be set):

```sh
docker compose exec innerwall /innerwall operator set-password
```

Open `https://localhost:8080/` and log in. The certificate is self-signed on first start; its SHA-256 fingerprint is in `docker compose logs innerwall`.

### From source

Needs Go (the version `go.mod` names), Node 22 for the console, and a reachable Postgres:

```sh
export INNERWALL_DATABASE_URL='postgres://innerwall:innerwall@127.0.0.1:5432/innerwall?sslmode=disable'
make console    # builds the console into ui/dist/
make build      # bin/innerwall and bin/innerwall-agent, the console embedded
bin/innerwall migrate
bin/innerwall operator set-password
bin/innerwall serve --init-ca --ca-dir ./state/ca --site lab --gateway-advertise-address localhost:8443
```

`--init-ca` creates the signing authority in `./state/ca` on first start, with the listeners' certificates beside it. Open `https://localhost:8080/`. `make build-noconsole` builds without Node; that binary serves the API and no console.

### Enroll an agent

The agent runs on Linux as root, with nftables. It needs a provisioning token and the signing authority's certificate, delivered together out of band. Mint a token in the console (**Workloads → Provisioning tokens → Mint token**; the shown-once dialog prints the enroll command with your gateway address filled in) or on the command line, and fetch the certificate:

```sh
# Docker Compose
docker compose exec innerwall /innerwall token mint --name lab --label app=demo
docker compose cp innerwall:/var/lib/innerwall/ca/ca.crt ./ca.crt
# from source
bin/innerwall token mint --name lab --label app=demo
cp ./state/ca/ca.crt ./ca.crt
```

Then, on the host to protect (`make build` produces `bin/innerwall-agent`):

```sh
sudo bin/innerwall-agent enroll --server localhost:8443 --token <provisioning-token> --bootstrap-ca ca.crt
sudo bin/innerwall-agent daemon --server localhost:8443
```

On another host, use the control plane's address in place of `localhost`; the gateway's certificate must name it (`--tls-hosts`). The workload appears in the console's fleet, synced, in visibility mode: observed, never blocked. From there the path is the one the console walks: write a ruleset, move workloads to simulation, read the simulation review, and promote. `sudo bin/innerwall-agent down` removes the agent's firewall table at any time without the control plane. Running it in production, with more than one replica, behind load balancers, is in [docs/deploy/README.md](docs/deploy/README.md).

## Layout

| Path | What lives there |
|---|---|
| `proto/` | Source of truth for the agent contract (ADR-0007, ADR-0015) |
| `api/openapi.yaml` | The operator surface's contract, hand-authored (ADR-0021) |
| `cmd/innerwall`, `cmd/innerwall-agent` | Control-plane and agent binaries |
| `internal/` | Control-plane services and agent internals, one package per boundary |
| `internal/store/` | Hand-written SQL, goose migrations, sqlc output (ADR-0006) |
| `ui/` | The operator console: Vite + React, embedded into the control plane (ADR-0008) |
| `docs/adr/` | Architecture decision records: the design's source of truth |
| `docs/deploy/` | Running it: deployment, the operator surface, high availability |
| `docs/development.md` | Prerequisites, make targets, where things live |

## Contributing

Read [ARCHITECTURE.md](ARCHITECTURE.md), then [docs/adr/](docs/adr/), then [CONTRIBUTING.md](CONTRIBUTING.md). Every authored commit is DCO signed off. Every PR is reviewed against the ADRs, including by an automated pass once it is ready for review. Security reports go to the address in [SECURITY.md](SECURITY.md).

## License

Innerwall's source code is licensed under Apache-2.0 ([LICENSE](LICENSE)). The control-plane binary also embeds the built console, which bundles third-party code and font files under their own licenses: the fonts under the SIL Open Font License 1.1, the code under permissive licenses. Their full texts ship inside the built console; [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) explains where. The Innerwall name and marks are retained by the project (ADR-0013).
