#!/usr/bin/env sh
# Runs the network-namespace suites (build tag netns): enforcement, with
# real nftables, real conntrack and log events, and real traffic over a
# veth pair into a fresh network namespace, beside visibility in a
# namespace where only the owned table engages connection tracking; and
# collection, with the
# conntrack source's dump, its listen/dump race, and a real overrun, each
# in a namespace of its own. Needs root, nft, nsenter, and unshare;
# re-executes itself under sudo when not root, carrying the Go caches so
# the run does not rebuild the world.
set -eu

cd "$(dirname "$0")/.."

if [ "$(id -u)" -ne 0 ]; then
	exec sudo -E env "PATH=$PATH" \
		"GOMODCACHE=$(go env GOMODCACHE)" \
		"GOCACHE=$(go env GOCACHE)" \
		"GOFLAGS=${GOFLAGS:-}" \
		"$0" "$@"
fi

for bin in nft nsenter unshare; do
	if ! command -v "$bin" >/dev/null 2>&1; then
		echo "$bin is required for the network-namespace suite" >&2
		exit 1
	fi
done

go test -tags netns -count=1 -run 'InNamespace$' -v ./internal/agent/enforce/nft/ "$@"
exec go test -tags netns -count=1 -run 'InNamespace$' -v ./internal/agent/collect/conntrack/ "$@"
