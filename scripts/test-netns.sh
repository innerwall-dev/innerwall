#!/usr/bin/env sh
# Runs the network-namespace enforcement suite (build tag netns): real
# nftables, real conntrack and log events, real traffic over a veth pair
# into a fresh network namespace. Needs root, nft, and nsenter; re-executes
# itself under sudo when not root, carrying the Go caches so the run does
# not rebuild the world.
set -eu

cd "$(dirname "$0")/.."

if [ "$(id -u)" -ne 0 ]; then
	exec sudo -E env "PATH=$PATH" \
		"GOMODCACHE=$(go env GOMODCACHE)" \
		"GOCACHE=$(go env GOCACHE)" \
		"GOFLAGS=${GOFLAGS:-}" \
		"$0" "$@"
fi

for bin in nft nsenter; do
	if ! command -v "$bin" >/dev/null 2>&1; then
		echo "$bin is required for the network-namespace suite" >&2
		exit 1
	fi
done

exec go test -tags netns -count=1 -run '^TestEnforcementInNamespace$' -v ./internal/agent/enforce/nft/ "$@"
