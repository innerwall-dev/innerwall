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

The bootstrap anchor is the authority's certificate, handed to the agent out of band with the token; without it the agent would send the token to whatever answered at the address. The daemon holds the sync stream, applies policy as it is pushed (into an in-memory store until enforcement lands; every applied change is logged), reports inventory and heartbeats, and renews its credential when less than a third of its lifetime remains. It exits, with the reason, when the credential has expired or the control plane directs re-enrollment; both need a new provisioning token. `innerwall-agent renew` remains for rotating a credential by hand.

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

Only inbound rules are admitted (ADR-0010). The REST/JSON façade and UI are wired in by later milestones.

Running more than one replica: the signing authority directory (`INNERWALL_CA_DIR`, created once by `innerwall ca init`) is configuration and must be identical on every replica, like the database connection string. `serve --init-ca` is a single-replica development convenience; two replicas that each initialise their own authority issue credentials the other will not accept (ADR-0017).

## High availability

Control-plane replicas are stateless; anything durable is in Postgres. HA is therefore N replicas behind a load balancer plus a properly highly available Postgres. Postgres is the availability story.

The property that makes downtime survivable is agent-side: agents fail static (ADR-0011). Control-plane downtime degrades management, never enforcement.

Long-lived agent streams need a load balancer and any intermediate firewalls that tolerate persistent outbound TLS; keepalive and middlebox-timeout guidance lands with the gateway (M3).

## Sizing honesty

Single binary plus single Postgres is the supported deployment until real estates demand more. The seams for splitting the control plane into processes, adding a relay tier, and federating regions exist as interfaces and schema (ADR-0012); the split is not built and no sizing numbers are published until they are measured.
