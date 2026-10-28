# Multi-host evaluation

The quickstart enrolls an agent on the machine that runs the control plane. Evaluating Innerwall means agents on other machines (VMs, a second server) enrolling against that control plane. This guide covers what has to change for that, installing the agent on the target, running it as a service, and the pitfalls of a small lab.

## Name the control plane before the first start

The Docker Compose stack is set up for agents on the same machine:

- **The gateway's certificate names only `localhost`, `127.0.0.1`, and `innerwall`** (the `--tls-hosts` in `docker-compose.yml`). An agent verifies the gateway against that certificate, so an agent dialing any other name or address is refused at the TLS handshake.
- **The advertised gateway address is `localhost:8443`** (`INNERWALL_GATEWAY_ADVERTISE_ADDRESS`). It is what the console's mint dialog prints in the enroll command it shows.

Both must name the address remote agents will dial, and the certificate should do so **before the stack first starts**: the gateway's certificate is generated once, when `--init-ca` creates the signing authority, and starting again never reissues it. Changing `--tls-hosts` later changes nothing by itself; a stack that already started needs its certificate reissued, [below](#a-stack-that-has-already-started).

Put the change in a `docker-compose.override.yml` beside `docker-compose.yml`. Docker Compose reads it automatically, and `make dev` passes it on. With the control plane reachable as `cp.example.internal` at `192.0.2.10`:

```yaml title="docker-compose.override.yml"
services:
  innerwall:
    command: ["serve", "--migrate", "--init-ca", "--tls-hosts", "localhost,127.0.0.1,innerwall,cp.example.internal,192.0.2.10"]
    environment:
      INNERWALL_GATEWAY_ADVERTISE_ADDRESS: cp.example.internal:8443
```

> [!NOTE]
> **Agents in VMs on the same machine.** A VM reaches the machine that runs it at its virtual network's gateway address, not at `localhost`, which inside the VM is the VM itself. From inside any of the VMs, `ip route show default` prints that address: `default via <address> …`. It is the address to put in `--tls-hosts` and in `INNERWALL_GATEWAY_ADVERTISE_ADDRESS` (as `<address>:8443`), and the stack must first start with it there, since the gateway's certificate is generated once. The machine's own firewall must admit port 8443 from the VM network too.
>
> **Verify the address before the first start.** From inside a VM, a TCP connection to `<address>:8443` must succeed: `nc -vz <address> 8443` once the stack is up, or, before the first start, against any listener on port 8443 of the machine as a dry run (a throwaway `python3 -m http.server 8443`, stopped before the stack starts). On some virtual networks, OrbStack among them, the default route's gateway is the virtual network itself and not the machine, so the connection is refused; those networks provide a host alias instead, such as `host.internal`. When the check fails, use the alias the virtual network provides in both `--tls-hosts` and `INNERWALL_GATEWAY_ADVERTISE_ADDRESS`, and check again. If the stack already started with an address the VMs cannot reach, nothing needs to start over: `innerwall ca issue-server` reissues the gateway's certificate for the working address ([below](#a-stack-that-has-already-started)).

The `command` replaces the one in `docker-compose.yml`, so it repeats its flags and adds the hosts. Then start the stack as the [quickstart](../../README.md#with-docker-compose) does, `docker compose up -d` with the released image named in `.env` (or `make dev` to build from the checkout). The mint dialog now prints `--server cp.example.internal:8443`, and agents can dial either the name or the address.

Git does not track the override file, and an untracked file marks the checkout as changed: a control plane built from it (`make dev`, `make build`) reports its version with `.dirty` from now on. That is expected; the released image is not built from the checkout and is unaffected ([version strings](../reference/versioning-and-install.md#version-strings)).

The gateway is published on port 8443 on every interface of the machine; let it through that machine's own firewall. The operator surface stays bound to the machine's loopback, so open the console on that machine.

### A stack that has already started

A stack that has already started keeps the certificate it generated. If you did not name the control plane before the first start, issue a new certificate from the existing signing authority with `innerwall ca issue-server`, then restart. Nothing is lost: the new certificate chains to the same root, so every agent already enrolled keeps trusting the gateway and nothing is enrolled again, and the database, the authority, and the operator certificate are untouched.

1. Write the `docker-compose.override.yml` above, so the advertised address and `--tls-hosts` name the control plane too.
2. Issue the certificate in the running container. It names exactly the hosts you list, so keep the local ones:

   ```sh
   docker compose exec innerwall /innerwall ca issue-server --hosts localhost,127.0.0.1,innerwall,cp.example.internal,192.0.2.10
   ```

3. Restart the control plane, applying the override: `docker compose up -d --force-recreate innerwall` (in a checkout run with `make dev`, stop it and run `make dev` again). It presents the new certificate from this start on; agents already enrolled reconnect by themselves.

The command refuses a directory that holds no authority, and it reissues only the gateway's certificate ([`ca issue-server`](../reference/innerwall-cli.md#ca-issue-server)).

## Get the agent for the target

Each release carries the agent for Linux on `amd64` and `arm64`, `innerwall-agent_<version>_linux_<arch>.tar.gz`, with signed checksums. Download it, verify it, and install it on the host as [versioning and install](../reference/versioning-and-install.md#download-verify-install) shows; the host needs nftables and root. The binary makes no update checks and sends no telemetry: it dials only the control plane.

**Fallback: cross-compile.** For an architecture no release covers, or a build of an unreleased commit, the agent is a static Linux binary that any machine with Go can build from a checkout at the tag:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/linux-arm64/ ./cmd/innerwall-agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/linux-amd64/ ./cmd/innerwall-agent
```

## Enroll the remote host

Mint a token (in the console, **Workloads → Provisioning tokens → Mint token**, or `innerwall token mint`), and copy the signing authority's certificate (`ca.crt`, from `docker compose cp innerwall:/var/lib/innerwall/ca/ca.crt ./ca.crt`) and the token to the host, where the agent is installed at `/usr/local/bin/innerwall-agent` as above. Then, on the host:

```sh
sudo install -D -m 0644 ca.crt /etc/innerwall/ca.crt
sudo env INNERWALL_TOKEN='<provisioning-token>' /usr/local/bin/innerwall-agent enroll --server cp.example.internal:8443 --bootstrap-ca /etc/innerwall/ca.crt
```

The token in `INNERWALL_TOKEN` stays out of the process list; `--token` works too. Enrollment stores the key and credential in `/var/lib/innerwall-agent` and prints the workload's identity and labels.

## Run the agent as a service

The quickstart runs the daemon in the foreground, so it ends with the terminal and does not come back after a reboot; the kernel rules it installed stay (fail static), but nothing observes, reports, renews, or receives policy. On any host you keep, run it under the service manager. The repository does not ship a unit file yet; a minimal systemd unit:

```ini title="/etc/systemd/system/innerwall-agent.service"
[Unit]
Description=Innerwall agent
Wants=network-online.target
After=network-online.target

[Service]
ExecStart=/usr/local/bin/innerwall-agent daemon --server cp.example.internal:8443
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now innerwall-agent
journalctl -u innerwall-agent -f
```

The daemon runs as root (connection-tracking events are a privileged subscription). It exits non-zero when it must not go on as it is, for example when the state directory was enrolled again underneath it, and `Restart=on-failure` then starts it as the new workload. On every start it re-applies its persisted policy before it dials. To stop enforcement on the host for good, `systemctl disable --now innerwall-agent` and then `innerwall-agent down` ([the kill switch](../guides/promotion-and-enforcement.md#the-kill-switch)).

## One agent per host

`innerwall-agent enroll` refuses a state directory that already holds a valid credential: `the state directory already holds a valid credential; remove it to enroll again (it expires …)`. That guard is per directory, and a host should run exactly one agent:

> [!WARNING]
> Do not run a second daemon on a host with another `--state-dir`. Two daemons share the host's connection tracking: with the default table name each replaces the other's table, and even with a different `--nft-table` both write their rule attribution into the same connection-mark bits, both chains filter the same inbound traffic (so the host enforces the intersection of two policies), and every connection is reported twice, under two identities.

To give a host a new identity (to move it to another token's labels, say): stop the daemon, move its state directory aside, enroll again, and start the daemon. The new workload starts in visibility with no history, and the old one stays in the fleet as an offline row ([credential lifecycle](../guides/credential-lifecycle.md#beyond-the-grace-re-enrollment)).

## Keep the hosts' clocks in sync

The agent stamps every flow window with its host's clock. A VM on a laptop drifts when the laptop sleeps, and its flows then land at the wrong times until the clock is re-synced. The control plane detects a skew beyond two flow windows and says so on the workload; the remedy is in [troubleshooting](../troubleshooting.md#flow-times-are-wrong-the-agents-clock-has-drifted). Enable the hypervisor's guest time synchronization, or re-sync after every resume.

## Next

With agents enrolled, the operating loop is: [labeling strategy](../guides/labeling.md), [writing the first ruleset](../guides/first-ruleset.md), [reading a simulation review](../guides/simulation-review.md), and [promotion and enforcement](../guides/promotion-and-enforcement.md).
