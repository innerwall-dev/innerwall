# Troubleshooting

Symptoms an operator meets, what causes them, and the remedy. Each entry starts from what you see. An entry for a defect that is fixed says in which version, and what to do on an earlier one.

## The control plane and the console

### Sign-in fails with a server error, not "That password is not correct."

**Symptom.** The console's sign-in answers with a server error rather than *That password is not correct.*, so it reads like a wrong password even when the password is right, and other pages fail to load. `docker compose logs postgres` shows Postgres restarting over and over, with `No space left on device`.

**Cause.** The Docker host's disk is full. Postgres cannot write, so it crash-loops, and every request that needs the database fails. Built images and the build cache are the usual cause on a machine that rebuilds the stack often (`make dev` builds the image every time).

**Remedy.** Free space without touching the volumes:

```sh
docker system df          # what is using the space
docker image prune        # dangling images
docker builder prune      # the build cache
```

Then `docker compose up -d`; Postgres recovers by itself once it can write.

> [!CAUTION]
> Never free space with `docker volume prune` or `docker compose down -v`. The `pgdata` volume holds every workload, policy, token, and flow, and `innerwall-state` holds the signing authority every agent's credential chains to. Removing either is starting over.

A log line such as `invalid record length at …: expected at least …, got 0` as Postgres starts is its normal end-of-WAL message during recovery, not a sign of damage.

### A new clone starts with an earlier install's state

**Symptom.** A stack you expected to be new is not. On what should be its first start:

- `docker compose logs postgres` shows `PostgreSQL Database directory appears to contain a database; Skipping initialization`;
- the control plane's `operator listener certificate` line reads `generated=false`, and no `signing authority created` line appears;
- the fleet already holds workloads, and agents enrolled against an earlier install reconnect to this one;
- an operator password you never set here is accepted, and an agent dialing a name you added to `--tls-hosts` is refused at the TLS handshake, because the certificate is the earlier one.

**Cause.** Docker Compose names a stack after the directory it runs in, unless `COMPOSE_PROJECT_NAME` says otherwise, and names the stack's volumes after the stack. A clone into a directory named `innerwall` is therefore the same stack, with the same `pgdata` and `innerwall-state` volumes, as every earlier `innerwall` checkout on that Docker host, even one since deleted. The volumes outlive the directory.

**Remedy.** If the earlier state is worth keeping, nothing is wrong: this is the earlier install, and [upgrading in place](guides/upgrading.md) is how to move it forward. To start clean, choose one:

- **A new project name**, which leaves the earlier volumes untouched. Stop the other stack first if it is running (`docker compose down`, without `-v`), since both publish the same ports. Then, in the new checkout, add `COMPOSE_PROJECT_NAME=<a new name>` to `.env`, so every `docker compose` command and `make dev` in that directory uses it, and start the stack.
- **Delete the earlier state** with `docker compose down -v` in the directory, then start again.

> [!CAUTION]
> `docker compose down -v` deletes both volumes: `pgdata`, which holds every workload, ruleset, token, flow, and the operator password, and `innerwall-state`, which holds the signing authority and the operator certificate. Every agent enrolled against the old authority can no longer connect and must be enrolled again, as a new workload. Use it only on a stack you are willing to start over.

### A command is refused with its own usage line

**Symptom.** A command such as `innerwall flows list <workload-id> --since 1h` or `innerwall workload set-labels <workload-id> --label role=web` prints `innerwall: usage: …` and does nothing, although every argument is there.

**Cause.** `innerwall` v0.4.1 and earlier stops reading flags at a command's first positional argument, so flags written after the workload id are read as extra arguments. Later versions read flags on either side of a command's positional arguments, but a positional argument beyond those the command takes still ends its flags: a value with an unquoted space in it reads as two arguments. `ruleset update` with `-f` before the ruleset refuses anything after the ruleset.

**Remedy.** Put the flags first, which every version reads: `innerwall flows list --since 1h <workload-id>`, `innerwall workload set-labels --label role=web <workload-id>`. Quote a value that holds a space ([command-line reference](reference/innerwall-cli.md#conventions)).

### Resend snapshot is accepted but the snapshot instant never moves

**Symptom.** **Resend snapshot** on a workload (or `innerwall workload resend-snapshot`) answers that the reconnect was requested, but the workload's *last snapshot sent* instant stays where it was, and the agent's journal shows no `control plane directed a reconnect` line.

**What the control plane logs.** Every request accepted on the operator surface logs `reconnect requested` with the workload's id. The replica that hears the directive then logs exactly one of these for it:

```
level=INFO msg="reconnect directed" workload_id=…
level=WARN msg="reconnect directive not sent: this replica holds no stream for the workload" workload_id=…
level=WARN msg="reconnect directive not sent" workload_id=… reason="…"
```

- **`reconnect directed`** means the directive was queued on the workload's stream. If the instant still does not move, read the agent's journal.
- **`this replica holds no stream for the workload`** means the replica that heard the directive has no stream registered for that workload. With one replica, no stream was held at that moment: the agent is between streams (redialing after a failure, or waiting out its backoff), or its stream is on another control-plane process sharing the database, such as an outgoing process still running after a deploy. With several replicas, every replica that does not hold the stream logs this line, and the one that does logs `reconnect directed`.
- **`reconnect directive not sent` with a reason** means the stream was held but the directive could not be queued on it: the stream ended first (the reason names why, for example that a newer stream from the same workload superseded it), or its send queue stayed full for ten seconds, which means the stream has stopped moving.
- **`reconnect requested` with none of the three after it** means no replica heard the directive. The directive travels on the database's notification channel, and it is lost if the listener was down at that moment: look for `policy listener lost; reconnecting`. Otherwise check whether another control-plane process with a different log is serving the agent.

**Remedy.** Press **Resend snapshot** again once the workload reads synced. Delivery is best-effort by design (ADR-0018): nothing records or retries a directive. If no replica holds the stream, restart the agent on the host instead. It re-applies its persisted policy before it dials, and its new stream begins with a snapshot.

### Offline right after Resend snapshot on a healthy host

**Symptom.** Right after **Resend snapshot**, a workload whose agent was healthy reads Offline, with no stream, for up to several minutes.

**Cause.** Fixed in v0.2.0, in two parts. Earlier agents did not redial at once on a directed reconnect: they waited out a reconnect backoff built up by earlier reconnects, up to five minutes, and the workload was offline meanwhile. And the control plane could record the old stream's end after the new stream had registered, so the workload read offline while its new stream was live, and a further **Resend snapshot** was refused as offline, until the agent's next acknowledgment. Since v0.2.0 the agent redials immediately on a directed reconnect and resets its backoff once a stream is established, and an ending stream never marks a newer live stream of the same workload offline.

**Remedy.** Upgrade the control plane and the agent. On an earlier version, wait for the agent to reconnect, or restart it on the host.

### Offline while the workload's flows keep arriving

**Symptom.** A workload reads Offline, yet its flows keep appearing on the flow map and its last flow report is recent.

**Cause.** The sync stream (the control channel) is down while flow reports, which travel on connections of their own, still arrive: the agent is running and reconnecting, not gone. Since v0.2.0 the console says so: the workload reads **Reconnecting** ("control channel down · flow reports arriving"). Earlier versions show Offline.

**Remedy.** Usually none: the agent reconnects by itself with backoff. If it stays Reconnecting, read the agent's journal for why its stream is refused or cut, and check that anything between the agent and the gateway passes TLS through at layer 4 and keeps idle connections open for more than 30 seconds ([docs/deploy/README.md](deploy/README.md#high-availability)).

## Agents and hosts

### A remote agent cannot enroll: the certificate is not valid for the address

**Symptom.** `innerwall-agent enroll --server <address>:8443` on another machine fails with a TLS error such as `x509: certificate is valid for localhost, innerwall, not <address>`.

**Cause.** The gateway's certificate does not name the address the agent dials. The Docker Compose stack's certificate names `localhost`, `127.0.0.1`, and `innerwall` only, and the certificate is generated once, when the stack first starts: changing `--tls-hosts` afterwards changes nothing by itself. The error lists the names the certificate carries.

**Remedy.** Dial an address the certificate names, or make the certificate name the address. On a stack that has not started yet, name the control plane's address in `--tls-hosts` before the first start ([multi-host evaluation](deploy/multi-host-evaluation.md#name-the-control-plane-before-the-first-start)). On one that has, issue a new certificate from the existing signing authority for every name and address agents dial, then restart the control plane:

```sh
docker compose exec innerwall /innerwall ca issue-server --hosts localhost,127.0.0.1,innerwall,<address>
docker compose restart innerwall
```

The new certificate chains to the same root, so agents already enrolled keep trusting the gateway; nothing is enrolled again, and nothing needs starting over ([a stack that has already started](deploy/multi-host-evaluation.md#a-stack-that-has-already-started), [`ca issue-server`](reference/innerwall-cli.md#ca-issue-server)).

### The agent is gone after the host restarts

**Symptom.** After a VM or host restart, the workload reads Offline and stays so; nothing runs `innerwall-agent` on the host.

**Cause.** The quickstart runs the daemon in the foreground, so it ends with the terminal session and nothing starts it at boot. The kernel rules it installed survive a daemon exit (fail static) but not a reboot of the host; on a reboot nothing reinstalls them until the daemon runs.

**Remedy.** Run the agent under the service manager, enabled at boot ([multi-host evaluation](deploy/multi-host-evaluation.md#run-the-agent-as-a-service)). On start it re-applies its persisted policy before it dials.

### Visibility mode observes no flows on a clean host

**Symptom.** A workload in visibility on a freshly installed host shows no inbound flows at all, while connections to it are certainly arriving, and no error is reported.

**Cause.** Fixed in v0.2.0 (it was listed under v0.1.0 but merged after that tag). The kernel only tracks connections in a network namespace where some rule references connection tracking; on a host whose own firewall has no such rule, visibility installed nothing that did, so the agent observed nothing. Since v0.2.0 visibility installs one `observe` chain whose rule engages connection tracking and accepts everything.

**Remedy.** Upgrade the agent to v0.2.0 or later.

### Flow byte counts are zero

**Symptom.** A workload's flows arrive with connection counts but zero bytes, and the daemon's log at start reads `connection byte accounting is off; flow byte counts will be zero` with `sysctl=net.netfilter.nf_conntrack_acct`.

**Cause.** The kernel keeps per-connection byte counters only when connection-tracking accounting is on, and it is off by default on most distributions. The agent reads flows from connection tracking, so without the counters it sees the connections but not their bytes. This is a host setting the agent does not change.

**Remedy.** Turn accounting on:

```sh
sudo sysctl -w net.netfilter.nf_conntrack_acct=1
```

Connections opened after that carry byte counts; ones already open stay at zero. To keep the setting across reboots, add the line `net.netfilter.nf_conntrack_acct = 1` to a file in `/etc/sysctl.d/`. The daemon checks the setting each time it starts and repeats the warning while it is off, so a restart after a reboot shows whether the setting held.

### After the kill switch, the console still shows the workload enforced

**Symptom.** `innerwall-agent down` reported the table removed, but the console keeps showing the workload as enforced and synced.

**Cause.** Fixed in v0.2.0. Earlier agents did not report the table's state, so the kill switch was invisible to the control plane. Since v0.2.0 a running daemon reports it on its next heartbeat, within 30 seconds: the workload reads **Enforcement removed on host**, the fleet's `removed` filter selects it, and the time is recorded as a `table_removed` evidence gap.

**Remedy.** Upgrade the agent. Note that the report needs a running daemon: a host where `down` is followed by stopping the daemon before its next heartbeat reads Offline, not removed ([the kill switch](guides/promotion-and-enforcement.md#the-kill-switch)).

### The workload's flow count stops while the host keeps receiving traffic

**Symptom.** A workload's flows stop arriving: its newest flow keeps aging and its counts stay frozen, while the host is plainly busy and the console shows it healthy. It began after the host's clock stepped backwards (a manual clock change, a time sync correction).

**Cause.** Fixed in v0.2.0. A flow window open across a backwards clock step closed with an end before its start; the control plane refused it as malformed, and the agent retried that one window forever, holding every window behind it. Since v0.2.0 a window open across a backwards step is cut at the step, and a window the control plane refuses as malformed is dropped and recorded as a `window_rejected` evidence gap, so reporting moves on.

**Remedy.** Upgrade the agent. On an earlier version, restart the agent on the host; the windows it was holding are lost.

### The workload's credential state disagrees with the running agent

**Symptom.** The console's credential state for a workload does not match the agent that is evidently running on the host: it shows an expiry, or an expired credential, that is not the one the daemon logs and presents, while the agent's stream works.

**Cause.** `innerwall-agent renew` was run from the command line against the state directory of a running daemon. The control plane's record follows the credential it last issued, which was the command's; the daemon read its credential from disk once, at start, and keeps presenting the one it holds in memory.

**Remedy.** Restart the daemon so that it adopts the credential on disk, and do not run `renew` against a running daemon ([credential lifecycle](guides/credential-lifecycle.md#rotating-a-credential-by-hand)).

### The agent logs "workload credential refused" on every attempt

**Symptom.** The daemon's journal repeats `workload credential refused` for its sync stream, its flow reports, and its renewals, and keeps retrying with backoff. The host keeps enforcing the policy it last applied.

**Cause.** The workload was retired (`innerwall workload retire`, ADR-0023). A retired identity is refused at every door, and the refusal deliberately names no reason to the agent. The control plane's log names it: `refused a retired workload's credential` with the workload and its `retired_at`, and the workload reads as retired in `innerwall workload status`.

**Remedy.** Retirement is one way. If the host should come back, enroll it again with a provisioning token, which makes it a new workload ([credential lifecycle](guides/credential-lifecycle.md#retiring-a-workload)); if it should not, stop and remove the agent, since the host keeps enforcing its last policy until you do.

### An evidence gap: "the agent was not reporting"

**Symptom.** A workload shows an evidence gap reading *the agent was not reporting between t1 and t2*, and a simulation review whose range includes it is not safe to enforce.

**Cause.** No flow report arrived from the agent for longer than two flow windows, no window delivered afterwards covered the interval, and the agent's heartbeats did not show one process running through it: the agent process was down (stopped, crashed, the host rebooted or was suspended), or it lost what it would have reported. The control plane derives this gap itself, from when it received the reports on either side (`agent_silent`), and writes it when the agent reports again. This is the evidence model working, not a fault: anything that reached the host in that interval went unseen.

**Remedy.** Find out why the agent was down: the host's journal around the interval. The review stays unsafe while the gap is in its range; promote past it only with the acknowledgment, or wait until the range has moved past it. A control-plane outage the agent rode out records no such gap, because its buffered windows cover the time, and neither does a workload that simply received no traffic, because its agent's heartbeats show it running throughout.

A gap of this kind dated before an upgrade to v0.4.0, on a workload that receives little inbound traffic and whose agent's journal shows no stop or restart in the interval, can be spurious: v0.3.0 read such quiet stretches as silence (#144). Nothing stored distinguishes those gaps from an outage, so they are kept and age out with flow retention; acknowledge them in a review once the journal confirms the agent was running.

### Flow times are wrong: the agent's clock has drifted

**Symptom.** One of these:

- The workload's **Inbound flows** tab shows a note above the table: *This agent's clock is 1h 12m behind the control plane's, as of its latest flow report…*, or, once the clock is back, that it *was* off in a report some time ago.
- The control plane logs a WARN line for the workload, once per flow report while the skew lasts:

  ```
  level=WARN msg="agent clock skewed: flow windows are stamped with the agent's clock" workload_id=… hostname=db-1 offset=-1h12m0s offset_seconds=-4320 measured_by=sent_at tolerance=2m0s …
  ```

- `innerwall workload status <workload-id>` prints a `clock skew` line.
- Without any of those: a workload's flows show "last seen" ages that are far too old or in the future, traffic you just generated is missing from a recent time range on the flow map or in the simulation review, or the evidence gaps sit at the wrong times.

**Cause.** The agent stamps every flow window, and every evidence gap, with its host's clock. When that clock is off, every window it reports lands at the wrong time, and nothing else about the window looks wrong. The common causes:

- **A VM after its host slept.** A VM's clock stops or slows while a laptop or workstation host sleeps, and it does not always catch up on resume. Lab estates of VMs on a laptop meet this routinely: an hour of sleep can leave a VM over an hour behind.
- **Time sync not running or not working** on the host: the time sync service stopped, disabled, or unable to reach its time servers.

**How it is detected.** Every flow report carries the agent's clock at the moment it was sent. The control plane compares that with the moment it received the report. A difference of more than twice the flow aggregation window (two minutes with the default one-minute window) is skew. The control plane records it on the workload, logs the WARN line, and stores the window as reported: skew is surfaced, never refused. An agent that predates the stamp is judged by its window's end instead, and only for a clock running ahead; a clock running behind on such an agent is not detected until the agent is upgraded.

Windows that arrive late after the control plane was unreachable are **not** skew. The agent holds closed windows in a buffer while it cannot report and delivers them when the control plane returns, so after an outage they arrive minutes or hours after they ended, stamped correctly.

**Remedy.** Re-sync the host's clock. On a host that uses systemd's time sync:

```sh
timedatectl status                          # "System clock synchronized: no" confirms it
sudo systemctl restart systemd-timesyncd
timedatectl status                          # synchronized: yes, and the time is right
```

On a host that uses chrony, `sudo chronyc makestep` steps the clock at once. For a lab VM whose host sleeps, re-sync it after every resume before trusting its flow timelines, or enable the hypervisor's time synchronization for the guest so it catches up by itself. The agent needs no restart: its next report is stamped with the corrected clock.

**After the fix.** The next report within tolerance clears the skew. The note above the flow table then says the clock *was* off, and when the last skewed report arrived, for as long as that report falls inside the table's range. Windows already stored keep the times they were reported with: a stored flow is history and is never rewritten (ADR-0019). Read the windows from before the fix as shifted by about the offset the note gives; they age out with flow retention.

## Policy and the review

### A mistyped CIDR is refused as an address group that does not exist

**Symptom.** Saving a rule whose peer is a mistyped address, such as `10.0.0.0/8/`, is refused with *rule references an address group that does not exist (10.0.0.0/8/)*, which sends you looking at address groups.

**Cause.** Fixed in v0.2.0. Earlier consoles read peer text as a CIDR only when the whole of it was digits, dots, and colons with an optional prefix length, so a mistyped address with anything else in it fell through to the address-group form. Since v0.2.0 text that opens the way an address does is always read as a CIDR, and a mistyped one is refused as *address is not a valid CIDR (10.0.0.0/8/)*.

**Remedy.** Correct the CIDR, prefix length included (`10.0.0.0/8`, or `/32` for one host).

### "not evaluated (visibility)", or "no matched traffic", where you expected a verdict

**Symptom.** A workload's **Inbound flows** tab shows *not evaluated (visibility)* in the rule column, or the policy editor's simulation column shows *no matched traffic* for a rule you expected to match, and the simulation review is empty or thin.

**Cause.** Rules are evaluated only where they are installed. A flow recorded while its workload was in visibility was `observed`: no rule was evaluated, and stored flows are never rewritten, so it keeps that label. *no matched traffic* means no connection was recorded as allowed by that rule in the last day, which is what a rule shows before its workloads are in simulation, or before the traffic it admits has arrived since. Evaluation starts when the workloads enter simulation; nothing before that is evaluated retroactively.

**Remedy.** Move the tier's workloads to simulation and wait for post-switch traffic: a peer that connects once a day needs a day ([reading a simulation review](guides/simulation-review.md#what-simulation-records)).
