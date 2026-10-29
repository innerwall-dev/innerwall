# Versioning and install

What a build says it is, where each version shows, and how Innerwall is installed.

## Install from a release

Each release carries both binaries, the control-plane image, and what is needed to check that they came from the release workflow at that release's tag:

| Asset | What it is |
|---|---|
| `innerwall_<version>_<os>_<arch>.tar.gz` | The control plane, console embedded, for `linux` and `darwin` on `amd64` and `arm64` |
| `innerwall-agent_<version>_linux_<arch>.tar.gz` | The agent, for `linux` only, on `amd64` and `arm64` |
| `<archive>.spdx.json` | An SPDX SBOM for each archive |
| `checksums.txt` | The SHA-256 of every archive and SBOM |
| `checksums.txt.sig`, `checksums.txt.pem` | The cosign signature over `checksums.txt`, and the certificate it was made with |
| `ghcr.io/innerwall-dev/innerwall:<version>` | The control-plane image, for `linux/amd64` and `linux/arm64`, signed |

`<version>` is the tag without its `v`: release `v0.5.1` carries `innerwall-agent_0.5.1_linux_amd64.tar.gz` and the image `ghcr.io/innerwall-dev/innerwall:0.5.1`. Each archive holds the binary and `LICENSE`. Images are tagged by version only; nothing is tagged `latest`.

Signing is keyless. No signing key exists to leak: the certificate names the release workflow, at the tag, as the signer, and the signature is recorded in the public transparency log. Verifying takes two checks. The signature proves `checksums.txt` came from that workflow at that tag, and the checksum proves the archive is the one the file lists.

### Download, verify, install

Set the release and the asset (for the control plane, `innerwall_${VERSION}_<os>_<arch>.tar.gz`), and download it with the checksum file, its signature, and its certificate:

```sh
VERSION=0.5.1
ASSET=innerwall-agent_${VERSION}_linux_amd64.tar.gz
base=https://github.com/innerwall-dev/innerwall/releases/download/v$VERSION
for f in "$ASSET" checksums.txt checksums.txt.sig checksums.txt.pem; do curl -fsSLO "$base/$f"; done
```

Verify the checksum file's signature with cosign 2.x. It prints `Verified OK` only for a signature by the release workflow at `v$VERSION`; any other signer, or any other tag, fails:

```sh
cosign verify-blob checksums.txt --signature checksums.txt.sig --certificate checksums.txt.pem \
  --certificate-identity "https://github.com/innerwall-dev/innerwall/.github/workflows/release.yml@refs/tags/v$VERSION" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Check the archive against it. This prints the asset's name and `OK`; on macOS use `shasum -a 256 -c` in place of `sha256sum -c`:

```sh
awk -v f="$ASSET" '$2 == f' checksums.txt | sha256sum -c
```

Install the binary:

```sh
tar -xzf "$ASSET"
sudo install -m 0755 innerwall-agent /usr/local/bin/innerwall-agent    # the control plane: innerwall
```

A host without cosign can be served by a machine that has it: verify there, copy the archive and `checksums.txt` to the host, and repeat the checksum line on the host.

> [!NOTE]
> On macOS, a binary downloaded by a browser carries the quarantine attribute, and the system refuses to run it. Clear it after verifying: `xattr -d com.apple.quarantine innerwall`. A download made with `curl` carries no quarantine attribute.

**The image.** The [quickstart](../../README.md#with-docker-compose) runs it with Docker Compose by naming it in `.env` as `INNERWALL_IMAGE`. The image is signed by the same workflow, by digest; to verify it before running it:

```sh
cosign verify ghcr.io/innerwall-dev/innerwall:0.5.1 \
  --certificate-identity https://github.com/innerwall-dev/innerwall/.github/workflows/release.yml@refs/tags/v0.5.1 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

**What the binaries do not do.** Neither binary checks for updates or sends telemetry. The control plane opens no connection except to its Postgres; the agent dials only the control-plane address it is given. Nothing updates itself either (ADR-0011): an upgrade is always an operator replacing a binary or an image ([upgrading](#upgrading)).

The agent runs on Linux only, as root, with nftables. Running the control plane is in the [quickstart](../../README.md#quickstart) and [docs/deploy/README.md](../deploy/README.md); placing agents on other machines is in [multi-host evaluation](../deploy/multi-host-evaluation.md).

### Fallback: build from source

Build from source when no release asset fits: another architecture, an unreleased commit, or a tree with changes. Building needs Go (the version `go.mod` names) and, for the console, Node 22:

```sh
git clone https://github.com/innerwall-dev/innerwall.git
cd innerwall
git checkout v0.5.1   # the release to build; without it, the build is of main
make console    # builds the console into ui/dist/
make build      # bin/innerwall and bin/innerwall-agent, the console embedded
```

`make build-noconsole` builds both binaries without Node; that control plane serves the API and no console. Both binaries are static (`CGO_ENABLED=0`), so the agent can be built for another Linux architecture on the build machine and copied to the host:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/linux-arm64/ ./cmd/innerwall-agent
```

`make dev` is the same fallback for the Docker Compose stack: it builds the control-plane image from the checkout, names it `innerwall:dev`, and runs it, whatever `.env` names. A plain `docker compose up -d` never builds; it runs the image `.env` names, as pulled.

## Version strings

Both binaries report one version string, semantic-version shaped, with the commit as build metadata:

| String | Built from |
|---|---|
| `v0.5.1+6007581f652b` | a release, or a build of its tag from source |
| `dev+f347cd966f58` | an untagged commit |
| `dev+f347cd966f58.dirty` | an untagged commit with uncommitted changes (a tagged one reads `v0.5.1+6007581f652b.dirty`) |
| `dev+unknown` | a build that carries no commit at all |

The commit is the one the Go toolchain embeds in every build made in a git checkout, so a plain `make build` needs no flags. The Docker image builds with git present for the same reason. A release sets both parts at link time, to the same string a source build of its tag reports.

Untracked files count as uncommitted changes. A checkout holding a file git does not track, such as the `docker-compose.override.yml` the [multi-host guide](../deploy/multi-host-evaluation.md#name-the-control-plane-before-the-first-start) adds, reports `.dirty` on every build made from it, `make dev` included. That is expected, and says nothing about the source. The released image and binaries are not built from your checkout, so they never carry it.

## Where each version shows

| Where | What |
|---|---|
| `innerwall version`, `innerwall-agent version` | The binary's own version. |
| The `control plane serving` and `agent starting` log lines | The same string, at start. |
| The console's account popover | The control plane's version, from `GET /api/v1/me`. Like every read but login, it answers only an authenticated caller, so the version is not disclosed to anyone who has not signed in. |
| The Workloads table's Agent column, the workload page, `innerwall workload status` | Each agent's version, as the agent reports it on its sync stream. |

## Upgrading

Upgrade the control plane first, then the agents; each release's notes in [CHANGELOG.md](../../CHANGELOG.md) say what an older agent keeps doing against a newer control plane. What survives an in-place upgrade, and the steps, are in [upgrading in place](../guides/upgrading.md). A new release is installed as above: download, verify, install. There is no agent self-update, by design (ADR-0011): an agent is upgraded by replacing its binary and restarting the daemon, which re-applies its persisted policy before it dials.
