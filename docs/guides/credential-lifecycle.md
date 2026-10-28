# Credential lifecycle

Every agent proves its identity with a short-lived credential: a certificate carrying exactly one identity, `innerwall://workload/<uuid>`, issued by the control plane's signing authority (ADR-0016). This guide covers how credentials are renewed, what happens when one expires, how long an expired one can still recover, and what re-enrollment does when it cannot.

## Renewal

- **Lifetime.** A credential lives 24 hours (`innerwall serve --leaf-ttl`).
- **When it renews.** The daemon renews at two thirds of the lifetime, less a random jitter of up to a twelfth of it, so a 24-hour credential renews between about 14 and 16 hours after issue and a fleet's renewals spread out instead of arriving together.
- **Two clocks.** The renewal is scheduled on the wall clock and on the time actually elapsed since the agent obtained the credential, whichever comes first. A host whose wall clock is wrong or steps backwards still renews on time; when elapsed time wins by more than a minute the daemon logs `the wall clock is behind the credential's age; renewing on elapsed time instead`.
- **When it fails.** A failed renewal is logged (`credential renewal failed; will retry`), retried with backoff, never past the credential's expiry, and reported on the agent's heartbeat, so the workload page and `innerwall workload status` show the renewal error.

Renewal needs nothing from the operator. The rest of this guide is about what happens when it cannot reach the control plane in time.

## Expiry is never fatal

A credential expires when the control plane was unreachable for the whole stretch between the renewal point and expiry (roughly eight to ten hours), or the agent was stopped across it. The agent does not stop for it:

- **Enforcement continues.** The daemon keeps enforcing its persisted policy, at start or while running (fail static, ADR-0011), and logs that the credential has expired.
- **Renewal keeps trying**: at once, then with jittered backoff, never more than about five minutes between attempts.
- **Flow reports are refused** while the credential is expired, because each report opens a new connection that must authenticate. The daemon holds the closed windows in its bounded buffer (`--flow-buffer-records`, 50,000 by default) and delivers them once it has renewed; beyond the buffer, the oldest are dropped and recorded as a `buffer_overflow` evidence gap.
- **A sync stream ends with the credential it opened on** (ADR-0016 as amended). Five minutes before that credential expires, the control plane directs the agent to reconnect: an agent that renewed on schedule redials with its fresh credential, and the swap costs one snapshot and no error. A stream still open at the expiry is ended with `workload credential has expired: it expired at …, and a sync stream ends with the credential it opened on; reconnect with a renewed credential`, and a new stream is refused until the credential is renewed. The host's enforcement is untouched either way.
- **The record says which credential the stream is using.** The workload's `health.credential.serial` and `expires_at` are the credential last issued; `health.credential.last_presented` is the one its latest sync stream opened on, which the control plane also logs on `sync stream opened` (`credential_serial`, `credential_expires_at`). They differ while a stream outlives a renewal, and after a hand renewal the running daemon has not loaded. `last_presented` is the credential in use only while `sync.state` is not `offline`; when the stream ends it stays, as what was presented last.

## The renewal grace

The control plane accepts an expired credential for **one thing only, renewal**, for `--renewal-grace` after its expiry: 72 hours by default, set on `innerwall serve`. So an outage of up to about three days heals by itself when the control plane returns: each agent renews on its next attempt and carries on.

- **Within the grace**, any other call is refused with `workload credential has expired: it expired at …, … ago; an expired credential may only renew, which it may for … more`.
- **A late renewal is recorded.** The control plane logs `credential renewed late, within the renewal grace` at warning level with how late it was; the agent logs `credential renewed after it had expired`. The workload keeps the record (`health.credential.last_late_renewal` in the API): the console shows how late the renewal was, and `innerwall workload status` prints `renewed late`.
- **After a late renewal, give it a few minutes.** The sync stream and the flow reporter take the renewed credential on their next connection, which comes when their current reconnect backoff ends: up to about five minutes. Until then the workload may still read offline or show stale state; nothing on the host is affected.

> [!NOTE]
> The console says which side of the grace an expired credential is on, as the control plane judges it against the grace it is configured with: within it, "will recover by renewal" until the instant the grace ends; beyond it, "needs re-enrollment", with the way to mint a provisioning token. The API's `health.credential.lifecycle` is `in-grace` or `past-grace`, and `health.credential.renewable_until` is when the grace ends.

## Beyond the grace: re-enrollment

Past the grace, the renewal is refused too: `workload credential has expired: it expired at …, … ago, beyond the 72h0m0s renewal grace; re-enrollment with a new provisioning token is required`. The daemon still enforces from disk and still retries, and logs that refusal on every attempt; it does not stop by itself, and the control plane never tells it to. The workload reads `past-grace` in `health.credential.lifecycle`. It can only be re-enrolled:

<!-- steps -->
1. **Mint a provisioning token** with the labels the host should carry (the console's **Mint token**, or `innerwall token mint`). The new workload takes the token's labels, not the old workload's.
2. **Enroll over the expired credential.** On the host, run `innerwall-agent enroll` with the new token, the same `--server` and `--bootstrap-ca` as the first enrollment, and the daemon's `--state-dir`. Enrollment overwrites an expired credential, and says whose: `replaced the expired credential of innerwall://workload/… (expired …)`. It refuses a valid one.
3. **Restart the daemon.** A daemon still running over the state directory notices the new key the next time its renewer wakes (within about five minutes while expired), logs that the state directory was enrolled again underneath it, and exits so that its restart runs as the new workload. Restart it, or let the service manager do so.
4. **Set the mode again.** The new workload starts in visibility, like every new workload: on its first snapshot the host's table becomes the observe-only table, and nothing is enforced until you move it to simulation and promote it again.

> [!NOTE]
> **A host returning before its previous credential expires.** Enrollment replaces an expired credential in place, with nothing to remove first, but it refuses to overwrite a valid one, and its error says so: `the state directory already holds a valid credential; remove it to enroll again (it expires …)`. A host enrolled again while its previous credential is still valid, which is always the case right after you [retire](#retiring-a-workload) it, must give that credential up first: stop the daemon (`sudo systemctl stop innerwall-agent`, or however it runs on the host), remove its state directory (`/var/lib/innerwall-agent` unless `--state-dir` names another; moving it aside does as well), then enroll with the new token and start the daemon. The agent cannot tell a retired credential from a healthy one, since the refusal it receives names no reason, so nothing on the host clears it by itself.

> [!IMPORTANT]
> Re-enrollment mints a new identity. The new workload has a new id and no history: the old workload's flows, gaps, and totals stay under the old id, and the old workload stays in the fleet as a permanently offline row until you [retire it](#retiring-a-workload).

## Retiring a workload

Retire a workload when its host is gone for good, when it was re-enrolled as a new workload, or when its key may have been copied: **Retire…** on its page in the console (or **Retire selected…** on the fleet), `innerwall workload retire <workload-id>` on the control-plane host, or `POST /api/v1/workloads/{id}/retire`. Retirement is one way (ADR-0023):

- its credential is refused from then on, at renewal and on every agent call, so a copy of its key renews nothing; a live sync stream is ended at once on the replica holding it, and in any case by its next heartbeat;
- its addresses leave every rendered peer set, and only the workloads whose policy changed advance a version;
- a silence its agent was in is stored as an `agent_silent` evidence gap ending at the retirement;
- its flows, gaps, and totals stay readable under its id, and the workload reads as retired rather than offline ([states](../reference/states.md#retired)).

The refusal the agent receives says only that its credential is refused; the control plane's log names the workload and when it was retired. There is no un-retire: a host that should come back enrolls again with a provisioning token and becomes a new workload. Its old credential is still valid on the host until it expires, and enrollment refuses to overwrite a valid credential, so stop the daemon and remove the agent's state directory before enrolling ([a host returning before its previous credential expires](#beyond-the-grace-re-enrollment)). Retiring does not revoke the token that enrolled the workload; revoke the token too if nothing should enroll with it again.

## Rotating a credential by hand

`innerwall-agent renew` renews the credential in a state directory once, from the command line. Use it only on a host whose daemon is **not** running.

> [!WARNING]
> Never run `innerwall-agent renew` against the state directory of a running daemon. The daemon reads its credential from disk once, at start, and keeps presenting the one it holds in memory; it never re-reads the file. The control plane's record then tracks the credential the command issued while the agent presents a different one: the workload page's credential in use is marked as not the one issued, and the expiry and renewal it states describe a credential the agent does not use. If it has happened, restart the daemon so that it adopts the credential on disk, or simply let its own renewer replace both.

## Related

- What the console shows for each credential state: [states](../reference/states.md#credential).
- Why identity lives only in the certificate, and why labels never do: ADR-0016.
- Agents on hosts whose clocks drift: [troubleshooting](../troubleshooting.md#flow-times-are-wrong-the-agents-clock-has-drifted).
