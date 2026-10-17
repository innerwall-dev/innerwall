# Upgrading in place

The control plane holds no state of its own: everything durable is in Postgres, and the signing authority is configuration in its directory (ADR-0017). So upgrading it is replacing the binary (or the image) and applying the new migrations, with the database and the authority directory kept. Agents fail static across the gap and reconnect by themselves.

## What an upgrade keeps

An upgrade that keeps the database and the authority directory keeps everything:

- workloads, their labels and modes, and their rendered policy versions;
- rulesets, services, and address groups;
- provisioning tokens and operator tokens;
- every agent's credential, which stays valid because the authority that issued it is unchanged;
- the operator's sessions, which are stored in Postgres, so a signed-in console stays signed in;
- the flow history, evidence gaps, and totals.

> [!CAUTION]
> With Docker Compose, the database lives in the `pgdata` volume and the signing authority (with the operator certificate) in the `innerwall-state` volume. `docker compose down -v`, or deleting either volume, destroys it: every workload, policy, token, and flow, and the authority every agent's credential chains to. Agents would then have to be enrolled again, each as a new workload. Never remove the volumes to upgrade.

## Order

Upgrade the control plane first, then the agents. An older agent keeps working against a newer control plane; each release's notes in [CHANGELOG.md](../../CHANGELOG.md) say what it keeps doing and what works in full only once both ends are upgraded.

## The control plane

<!-- tabs -->
### Docker Compose

1. **Check out the new release.** In the checkout the stack runs from: `git fetch --tags`, then `git checkout <release-tag>`.
2. **Rebuild and restart.** `make dev` (or `docker compose up --build -d`) rebuilds the image and recreates the control-plane container. The compose file runs `serve --migrate`, so pending migrations are applied before it serves. Keep the volumes.
3. **Confirm the version.** The account popover in the console shows the control plane's version, as does `docker compose exec innerwall /innerwall version`.

### From source

1. **Build the new release.** Check out the tag and run `make console` and `make build`.
2. **Apply the migrations.** `bin/innerwall migrate` (or start with `serve --migrate`).
3. **Restart** each control-plane process on the new binary, with the same flags, database, and `--ca-dir`.
4. **Confirm the version** with `bin/innerwall version` or the console's account popover.
<!-- /tabs -->

## While it is down

- **Enforcement is unchanged on every host.** Agents keep enforcing their persisted policy (ADR-0011).
- **Agents reconnect unaided** when the control plane returns, with full-jitter backoff (at most about five minutes apart), and each new stream begins with a snapshot.
- **Flow windows are buffered** in each agent's bounded buffer and delivered after it reconnects, stamped with when they happened. A window delivered late covers the time it was held back, so an outage the agents ride out records no evidence gap; only windows that overflowed the buffer are recorded as lost (`buffer_overflow`).
- **Renewals that came due** during the outage succeed on the agent's next attempt. An outage long enough for credentials to expire still heals by itself within the renewal grace ([credential lifecycle](credential-lifecycle.md#the-renewal-grace)).

## The agents

Agents never update themselves (ADR-0011). On each host, replace the binary and restart the daemon:

```sh
sudo systemctl stop innerwall-agent        # or however the daemon is run on the host
sudo install -m 0755 innerwall-agent /usr/local/bin/innerwall-agent
sudo systemctl start innerwall-agent
```

The kernel rules stay in place while the daemon is stopped, and the restarted daemon re-applies its persisted policy before it dials, so the host is enforced throughout. The workload's Agent column shows the new version once it has reconnected. Running the daemon as a service is in [multi-host evaluation](../deploy/multi-host-evaluation.md#run-the-agent-as-a-service).
