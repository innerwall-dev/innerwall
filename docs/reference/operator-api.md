# Operator API

The operator surface is the control plane's public REST/JSON API: what the console and automation consume, and the only thing the console talks to (ADR-0007, ADR-0021). Its contract is hand-authored in [`api/openapi.yaml`](../../api/openapi.yaml), which describes the implementation as it is; this page is the orientation and the index.

## The listener

- **One TLS-only listener** beside the agent gateway, on `--operator-listen` (`:8080` by default), serving every endpoint under `/api/v1` and the embedded console at `/`. There is no plaintext option. Without `--operator-tls-cert` and `--operator-tls-key` the control plane generates a self-signed certificate on first start and logs its SHA-256 fingerprint ([docs/deploy/README.md](../deploy/README.md#operator-surface)).
- **No domain logic in the handlers.** Each handler calls the same domain function the command line calls, so the two transports admit and write exactly the same things.

## Authentication

Every endpoint except `POST /api/v1/session` requires a credential, and both forms resolve to the one operator; authorization is binary.

- **A session cookie**, set by `POST /api/v1/session` with the operator password. It lives seven days, is never extended, and is stored in Postgres, so it survives a control-plane restart. `DELETE /api/v1/session` ends it.
- **An operator token**, presented as `Authorization: Bearer <token>`. Tokens are minted on the host (`innerwall operator token mint`) or through `POST /api/v1/operator-tokens`; the plaintext is shown once and only its digest is stored.

```sh
curl -H "Authorization: Bearer $INNERWALL_OPERATOR_TOKEN" https://localhost:8080/api/v1/me
```

A request without a valid credential is `401 unauthenticated`. A request that is not a read is refused as `403 cross-origin` when a browser marks it as coming from another origin. No endpoint sets or resets the password: the command line on the host does that.

## Conventions

- **Errors** are problem documents (`application/problem+json`) with a stable `type` (a URN under `urn:innerwall:problem:`) a client branches on. A `validation` problem carries `errors`, one finding per field to fix, each with a `path` into the request body, the stable name of the `rule` it breaks, and a `message`.
- **Conditional writes.** Every authored object (ruleset, rule, service, address group) and a workload's label set carries an opaque `version`, also returned as the `ETag`. Updating or deleting one requires `If-Match` with that version: without it the write is `428 precondition-required`, and against a version that has moved it is `412 precondition-failed` with the current version. Creating needs no precondition.
- **Cursors.** Unbounded reads page by an opaque `cursor`; a page carries `next_cursor`, null on the last page.
- **Timestamps** are RFC 3339 in UTC; fields are `snake_case`.
- **Fan-out** is acknowledged, not awaited. `POST /mode-changes` records an intent; convergence is observed on the workload reads as each workload's applied version against its latest. `POST /workloads/{id}/resend-snapshot` is best-effort by design ([states](states.md#resend-snapshot)).
- **Filters.** `label` filters are repeatable `key=value` requirements in the label grammar, ANDed. The `sync_state` filter on `GET /workloads` matches the stored sync state, so it does not exclude workloads whose enforcement was removed on the host; `GET /workloads/counts` counts those in `removed` and in no sync state ([states](states.md#sync-states)). `GET /workloads` lists the active fleet; `lifecycle=retired` lists the retired workloads instead ([states](states.md#retired)).

## Endpoints

Paths are relative to `/api/v1`. Parameters, request and response schemas, and every problem each endpoint returns are in [`api/openapi.yaml`](../../api/openapi.yaml).

### Session

| Method | Path | What it does |
|---|---|---|
| `POST` | `/session` | Log in with the operator password. |
| `DELETE` | `/session` | Log out. |
| `GET` | `/me` | The authenticated operator, the site label, the advertised agent gateway address, and the control plane's version. |

### Workloads

| Method | Path | What it does |
|---|---|---|
| `GET` | `/workloads` | One page of the fleet in fleet order. |
| `GET` | `/workloads/counts` | How many workloads the active fleet holds, in total, in each sync state, and with enforcement removed, and how many are retired. |
| `GET` | `/workloads/{id}` | One workload, in the shape the list uses. |
| `GET` | `/workloads/{id}/rendered-policy` | The persisted rendered policy of one workload. |
| `GET` | `/workloads/{id}/labels` | A workload's labels, as the resource a label edit is conditioned on. |
| `PUT` | `/workloads/{id}/labels` | Replace a workload's labels. |
| `POST` | `/workloads/{id}/resend-snapshot` | Direct a workload's agent to reconnect and receive a fresh snapshot. |
| `POST` | `/workloads/{id}/retire` | Retire a workload, one way: its credential is refused from then on and it leaves every peer set ([states](states.md#retired)). |

### Flows

| Method | Path | What it does |
|---|---|---|
| `GET` | `/flows/rollup` | A grouped rollup of stored flow windows. |
| `GET` | `/flows/gaps` | The evidence gaps of a workload set intersecting a range. |
| `GET` | `/flows` | One page of a workload's stored flow windows. |

### Policy

| Method | Path | What it does |
|---|---|---|
| `GET` | `/rulesets` | Every ruleset, and the version of the state a policy editor authors against. |
| `POST` | `/rulesets` | Create a ruleset with its rules. |
| `GET` | `/rulesets/{id}` | One ruleset with its rules. |
| `PUT` | `/rulesets/{id}` | Replace a ruleset and all of its rules. |
| `DELETE` | `/rulesets/{id}` | Delete a ruleset and its rules. |
| `POST` | `/rulesets/{id}/rules` | Add one rule to a ruleset. |
| `GET` | `/rulesets/{id}/rules/{rule_id}` | One rule. |
| `PUT` | `/rulesets/{id}/rules/{rule_id}` | Replace one rule, conditioned on the rule's own version. |
| `DELETE` | `/rulesets/{id}/rules/{rule_id}` | Remove one rule, conditioned on the rule's own version. |
| `GET` | `/services` | Every named service. |
| `POST` | `/services` | Create a named service. |
| `GET` | `/services/{id}` | One service. |
| `PUT` | `/services/{id}` | Replace a service's name and entries. |
| `DELETE` | `/services/{id}` | Delete a service no rule references. |
| `GET` | `/address-groups` | Every address group. |
| `POST` | `/address-groups` | Create an address group. |
| `GET` | `/address-groups/{id}` | One address group. |
| `PUT` | `/address-groups/{id}` | Replace an address group's name and members. |
| `DELETE` | `/address-groups/{id}` | Delete an address group no rule references. |
| `POST` | `/selectors/preview` | What a selector resolves to now. |
| `POST` | `/policies/render-dryrun` | Render a hypothetical policy set and report what would change, persisting nothing. |

### Mode changes

| Method | Path | What it does |
|---|---|---|
| `POST` | `/mode-changes` | Change the enforcement mode of a set of workloads. |

### Tokens

| Method | Path | What it does |
|---|---|---|
| `GET` | `/provisioning-tokens` | Every provisioning token, prefix and metadata only. |
| `POST` | `/provisioning-tokens` | Mint a provisioning token. |
| `DELETE` | `/provisioning-tokens/{id}` | Revoke a provisioning token. |
| `GET` | `/operator-tokens` | Every operator token, prefix and metadata only. |
| `POST` | `/operator-tokens` | Mint an operator token for automation. |
| `DELETE` | `/operator-tokens/{id}` | Revoke an operator token. |

## Deliberately absent

Generic bulk operations, any endpoint that sets or resets the operator password, any job or progress resource, and roles, scopes, or permissions of any kind (ADR-0021). There is also no endpoint, and no command, that deletes a workload or undoes a retirement (ADR-0023).
