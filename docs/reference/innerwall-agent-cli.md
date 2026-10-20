# innerwall-agent command line

The agent binary, `innerwall-agent`, runs on each Linux workload: it enrolls, holds the sync stream, applies policy to the nftables table it owns, observes and reports inbound flows, reports inventory, and renews its credential. It only dials out and never listens, and it never updates itself (ADR-0011).

## Conventions

- **Root.** The agent runs as root: it programs nftables and subscribes to the kernel's connection-tracking events.
- **The state directory** holds the agent's key (`workload.key`), credential (`workload.crt`), the trust bundle (`ca-bundle.crt`), and the last applied policy (`policy.bin`, with its rule marks in `marks.bin`). `--state-dir` names it; without the flag, `INNERWALL_AGENT_STATE_DIR` does; otherwise it is `/var/lib/innerwall-agent`.
- **Flags** take one dash or two; no command takes a positional argument. `innerwall-agent <command> -h` prints a command's flags.

| Environment variable | Read by | Stands for |
|---|---|---|
| `INNERWALL_AGENT_STATE_DIR` | every command | `--state-dir` |
| `INNERWALL_TOKEN` | `enroll` | `--token`, which keeps the token out of the process list |

## enroll

```sh
innerwall-agent enroll --server HOST:PORT --bootstrap-ca CA.CRT [--token TOKEN] [--state-dir DIR]
```

Exchanges a provisioning token for a workload credential. The bootstrap anchor (the signing authority's certificate, delivered out of band with the token) is how the agent verifies the control plane before it sends the token: the token proves the agent to the server, the anchor proves the server to the agent. The new workload is assigned the token's labels and starts in visibility.

Enrollment refuses a state directory that already holds a valid credential, and overwrites an expired one, saying whose it was. A new enrollment is always a new identity ([credential lifecycle](../guides/credential-lifecycle.md#beyond-the-grace-re-enrollment)).

<!-- properties -->
| Name | Default | Description |
|---|---|---|
| `--server` | | control-plane address, host:port (required) |
| `--token` | `$INNERWALL_TOKEN` | provisioning token |
| `--bootstrap-ca` | | PEM trust anchor for the control plane, delivered out of band with the token; empty uses the system trust store |
| `--state-dir` | `/var/lib/innerwall-agent` | directory for the key, credential, and bundle |

## daemon

```sh
innerwall-agent daemon --server HOST:PORT [flags]
```

Runs the agent. On start it re-applies the last acknowledged policy from the state directory before it dials anything, then holds the sync stream (policy as a snapshot, then deltas), applies each policy to the owned table (persisted durably first, acknowledged as applied only when both succeeded), observes inbound connections, ships aggregated flow windows on their own calls, reports inventory and heartbeats, and renews its credential. It leaves the kernel rules in place when it exits. An expired credential never stops it ([credential lifecycle](../guides/credential-lifecycle.md)).

<!-- properties -->
| Name | Default | Description |
|---|---|---|
| `--server` | | control-plane address, host:port (required) |
| `--state-dir` | `/var/lib/innerwall-agent` | directory holding the key, credential, bundle, and last applied policy |
| `--nft-table` | `innerwall` | name of the owned nftables table |
| `--nflog-group` | `200` | netlink log group the terminal rule logs to |
| `--flow-window-keys` | `50000` | distinct flow keys one open aggregation window holds; observations for new keys past it are dropped until the window closes, and the loss is recorded as an evidence gap |
| `--flow-buffer-records` | `50000` | flow records held in memory while the control plane is unreachable; the oldest are dropped beyond this |
| `--flow-dump-max` | `50000` | connection-table entries processed from the dump at each conntrack subscribe, and connections tracked to count each once; entries past it are skipped and recorded as an evidence gap. Bounds processing, not the dump's peak memory |
| `--flow-netlink-buffer` | `4194304` | receive buffer, in bytes, of each flow source's netlink socket; a larger buffer rides out longer bursts before the kernel drops events, and every drop is recorded as an evidence gap. 0 keeps the kernel default |

The flow aggregation window (60 seconds), the heartbeat interval (30 seconds), and the inventory interval (5 minutes) are set by the control plane on the sync stream; the agent has no flags for them. Byte counts need the kernel's per-connection accounting (`sysctl net.netfilter.nf_conntrack_acct=1`); without it connections are counted, bytes are zero, and the daemon says so at start.

## renew

```sh
innerwall-agent renew --server HOST:PORT [--state-dir DIR]
```

Renews the credential in the state directory once, by hand. The daemon renews by itself; this command is for a host whose daemon is not running.

> [!WARNING]
> Never run `renew` against the state directory of a running daemon. The daemon never re-reads its credential from disk and keeps presenting the one it holds in memory, so the control plane's record and the agent's actual credential stop agreeing. Restart the daemon to adopt the credential on disk.

<!-- properties -->
| Name | Default | Description |
|---|---|---|
| `--server` | | control-plane address, host:port (required) |
| `--state-dir` | `/var/lib/innerwall-agent` | directory holding the key, credential, and bundle |

## down

```sh
innerwall-agent down [--state-dir DIR] [--nft-table NAME]
```

The local kill switch: deletes the owned nftables table and nothing else, without the control plane, and prints what it did and what happens next. The persisted policy stays on disk: a running daemon reinstalls the table on its next policy update or new stream, and a restarted one on start, so keeping enforcement off means stopping the daemon too ([the kill switch](../guides/promotion-and-enforcement.md#the-kill-switch)).

<!-- properties -->
| Name | Default | Description |
|---|---|---|
| `--state-dir` | `/var/lib/innerwall-agent` | directory holding the last applied policy |
| `--nft-table` | `innerwall` | name of the owned nftables table |

## version

```sh
innerwall-agent version
```

Prints the build's version ([versioning](versioning-and-install.md)).
