# Innerwall

Open-source microsegmentation for heterogeneous server estates: VMs, bare metal, and cloud instances, no orchestrator required.

A small agent on each workload observes the connections that reach it and programs the operating system's native firewall. A control plane turns label-based policy into per-host rulesets, pushes them over persistent streams, and renders the estate's real traffic as a live dependency map. An operator console embedded in the control plane shows that map, the fleet, the policy, and what enforcing it would break.

**Status: v0.3.0.** Enrollment and short-lived workload identity, the policy sync stream, flow telemetry with evidence gaps, policy authoring with dry runs, simulation, and enforcement on Linux (nftables), and the operator console all work end to end. This release makes an agent's silence an evidence gap — a simulation review spanning an agent-process outage fails rather than reading safe — and documents the operating loop: task guides, a reference for both command lines and the operator API, and a troubleshooting catalog ([docs/guides/](docs/guides/), [docs/reference/](docs/reference/)). What is deliberately not built yet, and what comes next, is in [ROADMAP.md](ROADMAP.md); what changed in each release is in [CHANGELOG.md](CHANGELOG.md).

## The console

Each image puts a screen of the shipped console in its dark theme (left) beside the same state in its light theme. More are in [`docs/img/console/`](docs/img/console/).

**Simulation review.** A ruleset's would-block traffic from the workloads simulating it, with the verdict on whether it is safe to enforce, the evidence it rests on, and the promotion to enforcement.

![Simulation review, dark and light](docs/img/console/01-simulation-review-grouped.jpg)

**Flow map.** The estate's real traffic between label groups, colored by what the policy decided or would decide.

![Flow map, dark and light](docs/img/console/07-flow-map-graph.jpg)

**Policy editor.** Rules edited live, with every admission finding in place and a dry run that shows each workload's rendered change before anything is written.

![Policy editor dry run, dark and light](docs/img/console/policy-editor-dry-run.jpg)

**Workloads.** Every workload's mode, sync state against its latest rendered version, and agent health.

![Workloads, dark and light](docs/img/console/13-fleet-workloads.jpg)

## Why

- **East-west traffic is the blind spot.** Perimeter controls say nothing about lateral movement, and most estates cannot even see it.
- **You cannot safely enforce what you cannot see.** The workflow is visibility, then simulation, then enforcement: visibility is every workload's starting mode, the evidence is shown at each step, and promotion past evidence that does not support it takes an explicit acknowledgment (ADR-0001).
- **The host already has a firewall.** Innerwall programs the existing packet filter atomically and observably instead of shipping a datapath (ADR-0003).
- **Central planes fail by being chatty.** Agents hold one persistent stream; policy moves as versioned desired state; nothing polls the control plane (ADR-0002).
- **Fail static.** Losing the control plane changes nothing on any host. The policy an agent acknowledged is persisted before it is applied and stays enforced from local disk (ADR-0011, ADR-0020).

## Quickstart

> [!IMPORTANT]
> **Before you start: decide where the agents will run.** The agent runs only on Linux, as root, with nftables. If any agent will run on a machine other than the one running the control plane (always the case when Docker runs on macOS or Windows), first read [Name the control plane before the first start](docs/deploy/multi-host-evaluation.md#name-the-control-plane-before-the-first-start). The gateway's certificate is generated once, at the first start, and names only that machine's local names. No command reissues it, and an agent on another machine cannot enroll against it. Changing the names afterwards means starting the stack over.

The supported v1 deployment is one control-plane binary and one Postgres (ADR-0017). Run the released image with Docker Compose, or install the released binaries. These docs describe v0.4.0, and every command below names it.

### With Docker Compose

```sh
git clone https://github.com/innerwall-dev/innerwall.git
cd innerwall
git checkout v0.4.0     # the release these docs describe; a fresh clone is main
echo 'INNERWALL_IMAGE=ghcr.io/innerwall-dev/innerwall:0.4.0' > .env
docker compose up -d    # pulls the released control-plane image and starts it with Postgres
```

`.env` (git ignores it) names the image for every later `docker compose` command. Compose keys the stack's volumes by the directory's name, so a clone into a directory named `innerwall` picks up whatever state an earlier `innerwall` stack left behind, even after that clone was deleted. To start clean beside an earlier stack, add a line `COMPOSE_PROJECT_NAME=<a new name>` to `.env` before the first start ([how to tell, and the other way out](docs/troubleshooting.md#a-new-clone-starts-with-an-earlier-installs-state)).

Set the operator password (the command line on the host is the only place it can be set):

```sh
docker compose exec innerwall /innerwall operator set-password
```

It prompts twice, without echo: type the password (at least 12 characters) at each prompt. Nothing goes on the command line or into the shell's history. To provision without a terminal, pipe it in on standard input: `docker compose exec -T innerwall /innerwall operator set-password < password-file` reads one line.

Open `https://localhost:8080/` and log in. The certificate is self-signed, so the browser warns once. Before you accept it, compare it with the one the control plane logged: `docker compose logs innerwall | grep "operator listener certificate"` shows its `sha256_fingerprint`, and the browser shows the same value in the certificate's details (open the certificate from the warning page or the address bar; the SHA-256 fingerprint is listed there). A match means the page comes from your control plane and not from something in between, so the password you type next goes only to it.

**Fallback: build the image from the checkout.** `make dev` builds the control-plane image from the checked-out source, console embedded, and starts the stack in the foreground; it ignores `INNERWALL_IMAGE`. Use it for a tree with changes, or a platform the released image does not cover.

### With the release binaries

The control plane is one binary, for Linux or macOS: download `innerwall_0.4.0_<os>_<arch>.tar.gz`, verify it, and install it as [versioning and install](docs/reference/versioning-and-install.md#install-from-a-release) shows (the agent below takes the same steps). Then, with a reachable Postgres:

```sh
export INNERWALL_DATABASE_URL='postgres://innerwall:innerwall@127.0.0.1:5432/innerwall?sslmode=disable'
innerwall migrate
innerwall operator set-password
innerwall serve --init-ca --ca-dir ./state/ca --site lab --gateway-advertise-address localhost:8443
```

`--init-ca` creates the signing authority in `./state/ca` on first start, with the listeners' certificates beside it; they name `--tls-hosts` (by default `localhost,127.0.0.1`), the same once-only choice as above. Open `https://localhost:8080/`, and compare the fingerprint in the `operator listener certificate` log line as above.

**Fallback: build from source.** It needs Go (the version `go.mod` names) and Node 22 for the console:

```sh
make console    # builds the console into ui/dist/
make build      # bin/innerwall and bin/innerwall-agent, the console embedded
```

Use `bin/innerwall` and `bin/innerwall-agent` where this page says `innerwall` and `innerwall-agent`. `make build-noconsole` builds without Node; that binary serves the API and no console.

### Enroll an agent

The agent runs on Linux as root, with nftables. It needs a provisioning token and the signing authority's certificate, delivered together out of band. Mint a token in the console (**Workloads → Provisioning tokens → Mint token**; the shown-once dialog prints the enroll command with your gateway address filled in) or on the command line, and fetch the certificate:

```sh
# Docker Compose
docker compose exec innerwall /innerwall token mint --name lab --label app=demo
docker compose cp innerwall:/var/lib/innerwall/ca/ca.crt ./ca.crt
# release binaries
innerwall token mint --name lab --label app=demo
cp ./state/ca/ca.crt ./ca.crt
```

On the host to protect, download the agent, verify it against the release's signed checksums, and install it (`ARCH` is `amd64` or `arm64`):

```sh
VERSION=0.4.0 ARCH=amd64
ASSET=innerwall-agent_${VERSION}_linux_${ARCH}.tar.gz
base=https://github.com/innerwall-dev/innerwall/releases/download/v$VERSION
for f in "$ASSET" checksums.txt checksums.txt.sig checksums.txt.pem; do curl -fsSLO "$base/$f"; done
cosign verify-blob checksums.txt --signature checksums.txt.sig --certificate checksums.txt.pem \
  --certificate-identity "https://github.com/innerwall-dev/innerwall/.github/workflows/release.yml@refs/tags/v$VERSION" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
awk -v f="$ASSET" '$2 == f' checksums.txt | sha256sum -c
tar -xzf "$ASSET" innerwall-agent
sudo install -m 0755 innerwall-agent /usr/local/bin/innerwall-agent
```

`cosign verify-blob` (cosign 2.x) prints `Verified OK` only if `checksums.txt` was signed by the release workflow at that tag, and `sha256sum -c` prints `OK` only if the archive is the one it lists. A host without cosign can download and verify on a machine that has it, then copy over the archive and `checksums.txt` and repeat the `sha256sum` line. Neither binary checks for updates or sends telemetry. If no release archive fits the host, [build the agent from source](docs/reference/versioning-and-install.md#fallback-build-from-source). Then enroll and run it:

```sh
sudo innerwall-agent enroll --server localhost:8443 --token <provisioning-token> --bootstrap-ca ca.crt
sudo innerwall-agent daemon --server localhost:8443
```

On another host, use the control plane's address in place of `localhost`; the gateway's certificate must name it (`--tls-hosts`, set before the control plane first starts: [docs/deploy/multi-host-evaluation.md](docs/deploy/multi-host-evaluation.md)). The daemon's log warns `connection byte accounting is off; flow byte counts will be zero` on most hosts, because the kernel's per-connection byte accounting is off by default: flows are still counted, with zero bytes. Turn it on with `sudo sysctl -w net.netfilter.nf_conntrack_acct=1`; connections opened after that carry byte counts (keeping it across reboots is in [troubleshooting](docs/troubleshooting.md#flow-byte-counts-are-zero)). The agent stamps its flows with the host's clock, so keep it in sync: a VM on a laptop drifts when the laptop sleeps, and its flows then land at the wrong times until the clock is re-synced ([docs/troubleshooting.md](docs/troubleshooting.md)). The workload appears in the console's fleet, synced, in visibility mode: observed, never blocked. From there the path is the one the console walks: write a ruleset, move workloads to simulation, read the simulation review, and promote. A rule has no destination of its own: the ruleset's scope is the destination, so write one ruleset per tier you protect and let the dry run show which workloads each change reaches ([docs/policy.md](docs/policy.md)). `sudo innerwall-agent down` removes the agent's firewall table at any time without the control plane. Running it in production, with more than one replica, behind load balancers, is in [docs/deploy/README.md](docs/deploy/README.md).

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
| `docs/design/` | The console's design package of record: tokens, brand book, component specs, and the record of its amendments and errata |
| `docs/deploy/` | Running it: deployment, the operator surface, high availability, and agents on other machines |
| `docs/guides/` | The operating loop, task by task: labeling, the first ruleset, the simulation review, promotion and enforcement, credentials, upgrading |
| `docs/reference/` | Both command lines, the operator API, workload states and evidence gaps, versioning and install |
| `docs/policy.md` | The policy model: the scope is the destination, one ruleset per tier, authoring and the dry run |
| `docs/troubleshooting.md` | Symptom, cause, and remedy for what operators meet, such as an agent whose clock has drifted |
| `docs/development.md` | Prerequisites, make targets, where things live |

## Contributing

Read [ARCHITECTURE.md](ARCHITECTURE.md), then [docs/adr/](docs/adr/), then [CONTRIBUTING.md](CONTRIBUTING.md). Every authored commit is DCO signed off. Every PR is reviewed against the ADRs, including by an automated pass once it is ready for review (by an `@claude` mention instead when its author lacks write access). Security reports go to the address in [SECURITY.md](SECURITY.md).

## License

Innerwall's source code is licensed under Apache-2.0 ([LICENSE](LICENSE)). The control-plane binary also embeds the built console, which bundles third-party code and font files under their own licenses: the fonts under the SIL Open Font License 1.1, the code under permissive licenses. Their full texts ship inside the built console; [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) explains where. The Innerwall name and marks are retained by the project (ADR-0013).
