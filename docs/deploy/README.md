# Deploying Innerwall

## Supported v1 deployment

One control-plane binary and one Postgres, co-located or adjacent (ADR-0005). `docker-compose.yml` at the repository root is that deployment:

```sh
make dev
```

It starts Postgres with a persistent volume and builds the control plane from source. The control plane reads its Postgres connection string from `INNERWALL_DATABASE_URL`. Ports: the REST/JSON façade and embedded UI on 8080 (bound to localhost in the compose file), the agent gateway on 8443.

On first start the control plane applies migrations, creates its signing authority in the `innerwall-state` volume, and serves the agent gateway on 8443. Enrolling an agent from the host:

```sh
docker compose exec innerwall /innerwall token mint --name dev --label env=dev
docker compose cp innerwall:/var/lib/innerwall/ca/ca.crt ./bootstrap-ca.crt
innerwall-agent enroll --server localhost:8443 --token <token> --bootstrap-ca ./bootstrap-ca.crt --state-dir ./agent-state
innerwall-agent renew --server localhost:8443 --state-dir ./agent-state
```

The bootstrap anchor is the authority's certificate, handed to the agent out of band with the token; without it the agent would send the token to whatever answered at the address. The REST/JSON façade and UI are wired in by later milestones.

## High availability

Control-plane replicas are stateless; anything durable is in Postgres. HA is therefore N replicas behind a load balancer plus a properly highly available Postgres. Postgres is the availability story.

The property that makes downtime survivable is agent-side: agents fail static (ADR-0011). Control-plane downtime degrades management, never enforcement.

Long-lived agent streams need a load balancer and any intermediate firewalls that tolerate persistent outbound TLS; keepalive and middlebox-timeout guidance lands with the gateway (M3).

## Sizing honesty

Single binary plus single Postgres is the supported deployment until real estates demand more. The seams for splitting the control plane into processes, adding a relay tier, and federating regions exist as interfaces and schema (ADR-0012); the split is not built and no sizing numbers are published until they are measured.
