# Troubleshooting

Symptoms an operator meets, what causes them, and the remedy. Each entry starts from what you see.

## Flow times are wrong: the agent's clock has drifted

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

## Resend snapshot is accepted but the snapshot instant never moves

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
