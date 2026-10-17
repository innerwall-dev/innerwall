# Versioning and install

What a build says it is, where each version shows, and how Innerwall is installed today.

## Install: build from source

> [!IMPORTANT]
> Releases currently carry no binary assets. A release is a tag and its notes; the install path is building from source at the tag (or the Docker Compose stack, which builds the control-plane image from the checkout). A workflow that publishes release binaries is not built yet.

Building needs Go (the version `go.mod` names) and, for the console, Node 22:

```sh
git clone https://github.com/innerwall-dev/innerwall.git
cd innerwall
git checkout <release-tag>   # a tag from the releases page; without it, the build is of main
make console    # builds the console into ui/dist/
make build      # bin/innerwall and bin/innerwall-agent, the console embedded
```

`make build-noconsole` builds both binaries without Node; that control plane serves the API and no console. Both binaries are static (`CGO_ENABLED=0`), so the agent can be built for another Linux architecture on the build machine and copied to the host:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/linux-arm64/ ./cmd/innerwall-agent
```

The agent runs on Linux only, as root, with nftables. Running the control plane is in the [quickstart](../../README.md#quickstart) and [docs/deploy/README.md](../deploy/README.md); placing agents on other machines is in [multi-host evaluation](../deploy/multi-host-evaluation.md).

## Version strings

Both binaries report one version string, semantic-version shaped, with the commit as build metadata:

| String | Built from |
|---|---|
| `v0.2.0+6007581f652b` | a tagged commit |
| `dev+f347cd966f58` | an untagged commit |
| `dev+f347cd966f58.dirty` | an untagged commit with uncommitted changes |
| `dev+unknown` | a build that carries no commit at all |

The commit is the one the Go toolchain embeds in every build made in a git checkout, so a plain `make build` needs no flags. The Docker image builds with git present for the same reason.

## Where each version shows

| Where | What |
|---|---|
| `innerwall version`, `innerwall-agent version` | The binary's own version. |
| The `control plane serving` and `agent starting` log lines | The same string, at start. |
| The console's account popover | The control plane's version, from `GET /api/v1/me`. Like every read but login, it answers only an authenticated caller, so the version is not disclosed to anyone who has not signed in. |
| The Workloads table's Agent column, the workload page, `innerwall workload status` | Each agent's version, as the agent reports it on its sync stream. |

## Upgrading

Upgrade the control plane first, then the agents; each release's notes in [CHANGELOG.md](../../CHANGELOG.md) say what an older agent keeps doing against a newer control plane. What survives an in-place upgrade is in [upgrading in place](../guides/upgrading.md). There is no agent self-update, by design (ADR-0011): an agent is upgraded by replacing its binary and restarting the daemon, which re-applies its persisted policy before it dials.
