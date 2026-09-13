# Deploying Innerwall

## Supported v1 deployment

One control-plane binary and one Postgres, co-located or adjacent (ADR-0017). `docker-compose.yml` at the repository root is that deployment:

```sh
make dev
```

It starts Postgres with a persistent volume and builds the control plane from source. The control plane reads its Postgres connection string from `INNERWALL_DATABASE_URL`. Ports: the REST/JSON façade and embedded UI on 8080 (bound to localhost in the compose file), the agent gateway on 8443.

On first start the control plane applies migrations, creates its signing authority in the `innerwall-state` volume, and serves the agent gateway on 8443. Enrolling an agent from the host:

```sh
docker compose exec innerwall /innerwall token mint --name dev --label env=dev
docker compose cp innerwall:/var/lib/innerwall/ca/ca.crt ./bootstrap-ca.crt
innerwall-agent enroll --server localhost:8443 --token <token> --bootstrap-ca ./bootstrap-ca.crt --state-dir ./agent-state
innerwall-agent daemon --server localhost:8443 --state-dir ./agent-state
```

The bootstrap anchor is the authority's certificate, handed to the agent out of band with the token; without it the agent would send the token to whatever answered at the address. The daemon holds the sync stream, applies policy as it is pushed into the nftables table it owns (`inet innerwall`; every applied change is logged and the policy is persisted in the state directory), reports inventory and heartbeats, renews its credential when less than a third of its lifetime remains, and observes inbound connections through the kernel's connection tracker and the terminal rule's log, aggregating them per reporting window and shipping them on a stream of their own. Observation needs the daemon to run as root (the connection-tracking events are a privileged netlink subscription), and byte counts need the kernel's per-connection accounting (`sysctl net.netfilter.nf_conntrack_acct=1`); without it connections are counted and bytes are zero, and the daemon says so at start. Windows the control plane cannot receive are held in a bounded in-memory buffer (`--flow-buffer-records`); beyond it the oldest are dropped and the count is reported on the heartbeat. It exits, with the reason, when the credential has expired or the control plane directs re-enrollment; both need a new provisioning token. `innerwall-agent renew` remains for rotating a credential by hand.

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

Only inbound rules are admitted (ADR-0010). A workload in `visibility` mode has no verdict chain installed; `simulation` installs the enforced ruleset with the terminal rule accepting and reporting what enforcement would drop as `would_block`; `enforced` drops it and reports `blocked` (ADR-0020). The agent re-applies its last acknowledged policy from disk on every start, before it dials the control plane, and leaves the kernel rules in place when it exits. Root on the host removes them with the local kill switch:

```sh
innerwall-agent down --state-dir ./agent-state
```

It deletes the owned table and nothing else, and says so: the persisted policy stays on disk, a running daemon re-applies it on its next update and a restarted one on start, so stopping enforcement for good means stopping the daemon too. The agent writes its rule attribution into bits 16 through 31 of the connection mark (mask `0xffff0000`) and leaves the low sixteen bits exactly as it finds them, so a host tool that marks connections in the low bits keeps working; a tool that uses the high bits conflicts with the agent (ADR-0020). The other firewall tables on the host are never touched: the agent's chain runs at input priority `filter + 10`, after the host's own filter chains, and a packet must be accepted by both.

Stored flows are read with the `flows` commands, which are the query shapes the operator console will issue (ADR-0019):

```sh
docker compose exec innerwall /innerwall flows list <workload-id> --since 1h
docker compose exec innerwall /innerwall flows rollup --label role=db --decision observed --since 24h
docker compose exec innerwall /innerwall flows totals <workload-id>
```

Windows older than `--flow-retention` (30 days by default) are deleted by a job that runs every `--flow-retention-interval` on whichever replica takes the lock first; totals since first seen are kept. The REST/JSON façade and UI are wired in by later milestones.

Running more than one replica: the signing authority directory (`INNERWALL_CA_DIR`, created once by `innerwall ca init`) is configuration and must be identical on every replica, like the database connection string. `serve --init-ca` is a single-replica development convenience; two replicas that each initialise their own authority issue credentials the other will not accept (ADR-0017).

## High availability

Control-plane replicas are stateless; anything durable is in Postgres. HA is therefore N replicas behind a load balancer plus a properly highly available Postgres. Postgres is the availability story.

The property that makes downtime survivable is agent-side: agents fail static (ADR-0011). Control-plane downtime degrades management, never enforcement.

Long-lived agent streams need a load balancer and any intermediate firewalls that tolerate persistent outbound TLS; keepalive and middlebox-timeout guidance lands with the gateway (M3).

## Sizing honesty

Single binary plus single Postgres is the supported deployment until real estates demand more. The seams for splitting the control plane into processes, adding a relay tier, and federating regions exist as interfaces and schema (ADR-0012); the split is not built and no sizing numbers are published until they are measured.
