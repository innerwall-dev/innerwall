# Deploying Innerwall

## Supported v1 deployment

One control-plane binary and one Postgres, co-located or adjacent (ADR-0017). `docker-compose.yml` at the repository root is that deployment:

```sh
make dev
```

It starts Postgres with a persistent volume and builds the control plane from source. The control plane reads its Postgres connection string from `INNERWALL_DATABASE_URL`. Ports: the operator surface (REST/JSON API and, once it lands, the console) on 8080 over TLS (bound to localhost in the compose file), the agent gateway on 8443.

On first start the control plane applies migrations, creates its signing authority in the `innerwall-state` volume, and serves the agent gateway on 8443. Enrolling an agent from the host:

```sh
docker compose exec innerwall /innerwall token mint --name dev --label env=dev
docker compose cp innerwall:/var/lib/innerwall/ca/ca.crt ./bootstrap-ca.crt
innerwall-agent enroll --server localhost:8443 --token <token> --bootstrap-ca ./bootstrap-ca.crt --state-dir ./agent-state
innerwall-agent daemon --server localhost:8443 --state-dir ./agent-state
```

Labels follow one grammar everywhere they enter (ADR-0022): a key is 1 to 63 ASCII letters, digits, `.`, `_`, `-`, or `/`, a value 1 to 63 letters, digits, `.`, `_`, or `-`, each beginning and ending with a letter or digit, so `--label app=web --label env=lab` is two labels and nothing can be stored that the `key=value` form cannot write back. A token, a label edit, a selector, or a read filter outside the grammar is refused with the offending text quoted. A token minted before the grammar with labels outside it is refused at enrollment; mint a new one, and repair the labels of workloads it already enrolled with `innerwall workload set-labels`.

The bootstrap anchor is the authority's certificate, handed to the agent out of band with the token; without it the agent would send the token to whatever answered at the address. The daemon holds the sync stream, applies policy as it is pushed into the nftables table it owns (`inet innerwall`; every policy is persisted durably in the state directory before it reaches the kernel, and acknowledged applied only when both succeeded; every applied change is logged), reports inventory and heartbeats, renews its credential when less than a third of its lifetime remains, and observes inbound connections through the kernel's connection tracker and the terminal rule's log, aggregating them per reporting window and shipping them on a stream of their own. Observation needs the daemon to run as root (the connection-tracking events are a privileged netlink subscription), and byte counts need the kernel's per-connection accounting (`sysctl net.netfilter.nf_conntrack_acct=1`); without it connections are counted and bytes are zero, and the daemon says so at start. Each open window holds at most `--flow-window-keys` distinct flow keys; observations for new keys past it are dropped until the window closes. Windows the control plane cannot receive are held in a bounded in-memory buffer (`--flow-buffer-records`); beyond it the oldest are dropped and the count is reported on the heartbeat. Connections already open when the daemon starts are found by a dump of the connection table, bounded by `--flow-dump-max`; each source's netlink socket is sized by `--flow-netlink-buffer`; and every interval in which the daemon knows it lost evidence (the kernel dropped events, a source restarted, the dump was cut at its bound, the open window was full, the buffer dropped windows) is logged and shipped with the next window as an evidence gap; the heartbeat also counts the kernel's overruns. It exits, with the reason, when the credential has expired or the control plane directs re-enrollment; both need a new provisioning token. `innerwall-agent renew` remains for rotating a credential by hand.

Policy is authored with the control-plane command line against the database; the running control plane learns of every change through Postgres and pushes it to connected agents (ADR-0018):

```sh
docker compose exec innerwall /innerwall service create --name postgres --entry tcp:5432
docker compose exec innerwall /innerwall address-group create --name corp --cidr 10.0.0.0/8
cat > web-to-db.json <<'JSON'
{"name": "web-to-db", "scope": {"role": ["db"]},
 "rules": [{"direction": "inbound",
            "peers": [{"workloads": {"role": ["web"]}}, {"address_group": "corp"}],
            "services": ["postgres"]}]}
JSON
docker compose exec -T innerwall /innerwall ruleset create -f - < web-to-db.json
docker compose exec innerwall /innerwall workload list
docker compose exec innerwall /innerwall workload set-mode <workload-id> simulation
docker compose exec innerwall /innerwall policy show <workload-id>
```

Only inbound rules are admitted (ADR-0010). A workload in `visibility` mode has no verdict installed: its table holds one chain, `observe`, whose one rule engages the kernel's connection tracking and accepts, so connections are observed on a host where nothing else engages it, and nothing is dropped; `simulation` installs the enforced ruleset with the terminal rule accepting and reporting what enforcement would drop as `would_block`; `enforced` drops it and reports `blocked` (ADR-0020). The agent re-applies its last acknowledged policy from disk on every start, before it dials the control plane, and leaves the kernel rules in place when it exits. Root on the host removes them with the local kill switch:

```sh
innerwall-agent down --state-dir ./agent-state
```

It deletes the owned table and nothing else, and says so: the persisted policy stays on disk, a running daemon re-applies it on its next update and a restarted one on start, so stopping enforcement for good means stopping the daemon too. The agent writes its rule attribution into bits 16 through 31 of the connection mark (mask `0xffff0000`) and leaves the low sixteen bits exactly as it finds them, so a host tool that marks connections in the low bits keeps working; a tool that uses the high bits conflicts with the agent (ADR-0020). The other firewall tables on the host are never touched: the agent's chain runs at input priority `filter + 10`, after the host's own filter chains, and a packet must be accepted by both.

Stored flows are read with the `flows` commands, which are the query shapes the operator console will issue (ADR-0019):

```sh
docker compose exec innerwall /innerwall flows list <workload-id> --since 1h
docker compose exec innerwall /innerwall flows rollup --label role=db --decision observed --since 24h
docker compose exec innerwall /innerwall flows totals <workload-id>
docker compose exec innerwall /innerwall flows gaps --label role=db --since 24h
```

Windows older than `--flow-retention` (30 days by default) are deleted by a job that runs every `--flow-retention-interval` on whichever replica takes the lock first, and evidence gaps once they ended before the same horizon; totals since first seen are kept. Expired operator sessions are pruned on the same schedule.

## Operator surface

The control plane serves the operator-facing REST/JSON API on `--operator-listen` (`:8080` by default), over TLS and nothing else (ADR-0021). Give it a certificate with `--operator-tls-cert` and `--operator-tls-key`; without them it generates a self-signed certificate on first start, keeps it as `operator.crt` and `operator.key` beside the signing authority (so the compose stack's `innerwall-state` volume carries it), and logs its SHA-256 fingerprint. Verify that fingerprint in the browser before trusting the first connection; a deployment with several replicas provisions the same pair on each, as it does the authority directory. A generated certificate that has expired is replaced on the next start, with the new fingerprint logged.

Version 1 has one operator. Set the password on the control-plane host; there is no endpoint that does it, and until it is set the login endpoint refuses with a problem the console recognizes as a fresh install:

```sh
innerwall operator set-password --name "Ada"     # prompts twice; or pipe the password on stdin
```

Automation authenticates with an operator token presented as a bearer credential. Tokens are minted, listed, and revoked on the host; the plaintext is shown once at mint and only its digest is stored:

```sh
innerwall operator token mint --name ci --ttl 720h   # omit --ttl for a token that does not expire
innerwall operator token list
innerwall operator token revoke <token-id>
curl -H "Authorization: Bearer iwo_..." https://localhost:8080/api/v1/me
```

A browser logs in with `POST /api/v1/session` and receives a session cookie that lives seven days and is never extended; `DELETE /api/v1/session` ends it. Every error is a problem document (`application/problem+json`) whose `type` a client branches on. `--site` (or `INNERWALL_SITE`) sets the label the console header shows; `/api/v1/me` returns it with the operator's display name. `--gateway-advertise-address` (or `INNERWALL_GATEWAY_ADVERTISE_ADDRESS`) states the `host:port` agents reach the agent gateway at, which `/api/v1/me` returns as `gateway_address` for the console's enroll command; the bind address (`--listen`) is not one, and when none is configured the field is null and the console shows a placeholder.

The read endpoints, all behind the same credential, all `GET`, with timestamps in RFC 3339 UTC and `snake_case` fields:

| Endpoint | Parameters | Returns |
|---|---|---|
| `/api/v1/flows/rollup` | `group_by` (required: `rule`, `rule,peer`, `src,dst`, `dst,service`, or `peer,service`), `from`/`to` (default the last day), `verdict`, `direction`, `workload`, `label` (repeatable `key=value`), `service` (`tcp/5432` or `icmp`), `order` (`connections` or `recent`), `limit` (default 200, at most 1000) | The requested and the actually covered range, the groups with their keys and counters, the group count, a `truncated` marker, and the totals across every group |
| `/api/v1/flows/gaps` | `from`/`to` (default the last day), `workload`, `label` (repeatable), `limit` (default 500, at most 5000) | The evidence gaps intersecting the range, newest first: workload, kind (`source_overrun`, `source_restart`, `buffer_overflow`, `dump_truncated`, `window_overflow`), source (`conntrack`, `nflog`, or null), bounds, and the count lost where known, with a `truncated` marker |
| `/api/v1/flows` | `workload` (required), `from`/`to`, `verdict`, `direction`, `peer`, `service`, `cursor`, `limit` (default 100, at most 500) | One page of stored windows, newest first, with `next_cursor` |
| `/api/v1/workloads` | `label` (repeatable), `mode`, `sync_state`, `cursor`, `limit` (default 100, at most 500) | One page of the fleet in attention order (degraded, offline, pending, synced; then most recently seen), with `next_cursor` |
| `/api/v1/workloads/{id}` | | One workload in the list's shape: labels, mode, addresses, agent, listening services, sync state against the latest rendered version (with when the agent last acknowledged an applied version and last reported a failed apply), and health (last seen, credential state and last renewal, dropped flow records, source overruns) |
| `/api/v1/workloads/{id}/rendered-policy` | | The persisted rendered policy: version, mode, the terminal verdict of that mode, and the rules with their match criteria and provenance |

Rule hit counters are `group_by=rule` scoped by `workload` or by `label` requirements, where each group also carries `workload_count`, the distinct workloads in scope that reported the rule; a simulation review's rows are `group_by=peer,service` scoped by the ruleset's `label` requirements, where each group also carries `workload_count`, the distinct workloads in scope that saw it, and its would-block traffic is `verdict=would_block`. A malformed parameter is a `urn:innerwall:problem:invalid-parameter` problem whose detail names the parameter. The command line issues the same rollup with `innerwall flows rollup --group-by`.

Running more than one replica: the signing authority directory (`INNERWALL_CA_DIR`, created once by `innerwall ca init`) is configuration and must be identical on every replica, like the database connection string. `serve --init-ca` is a single-replica development convenience; two replicas that each initialise their own authority issue credentials the other will not accept (ADR-0017).

## High availability

Control-plane replicas are stateless; anything durable is in Postgres. HA is therefore N replicas behind a load balancer plus a properly highly available Postgres. Postgres is the availability story.

The property that makes downtime survivable is agent-side: agents fail static (ADR-0011). Control-plane downtime degrades management, never enforcement.

Long-lived agent streams need a load balancer and any intermediate firewalls that tolerate persistent outbound TLS (ADR-0002):

- **Pass TLS through at layer 4.** The agent gateway terminates the agents' mutual TLS itself and reads each workload's identity from its client certificate, so anything in front of it must forward the TCP connection untouched. A proxy that terminates TLS strips the certificate and every agent call except enrollment is refused.
- **Idle timeouts above 30 seconds.** Each agent sends a heartbeat on its sync stream every 30 seconds (the control plane's default in `SyncConfig`), and that traffic is what keeps the stream active through a middlebox: neither side configures transport-level keepalive pings, so a NAT, firewall, or load balancer that drops connections idle for less than the heartbeat interval will cut streams. Any idle timeout comfortably above 30 seconds is enough.
- **A cut stream costs a reconnect, not policy.** An agent whose stream is cut keeps enforcing (ADR-0011), reconnects with full-jitter backoff, and receives a fresh snapshot of its current policy on the new stream (ADR-0015). A load balancer that caps connection lifetime therefore causes periodic reconnects and nothing worse; set the cap long, hours rather than minutes, so a fleet does not churn handshakes.
- **Flow reports are short-lived.** The reporter opens one `ReportFlows` call per window (60 seconds by default) on a connection of its own; it needs nothing beyond what the sync stream needs.

## Sizing honesty

Single binary plus single Postgres is the supported deployment until real estates demand more. The seams for splitting the control plane into processes, adding a relay tier, and federating regions exist as interfaces and schema (ADR-0012); the split is not built and no sizing numbers are published until they are measured.
