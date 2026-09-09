#!/usr/bin/env sh
# Fails on a breaking change to proto/ against a base ref unless the change is
# accompanied by an ADR reference (ADR-0007). Usage:
#   scripts/check-proto-breaking.sh <base-ref>
# The text searched for an ADR reference (PR title and body) comes from
# PR_TEXT in the environment.
set -eu

base="${1:?usage: check-proto-breaking.sh <base-ref>}"

cd "$(dirname "$0")/.."

if ! git ls-tree -r --name-only "${base}" -- proto | grep -q '\.proto$'; then
	echo "base ref ${base} has no proto files; nothing to compare"
	exit 0
fi

# The buf module root is proto/ on both sides (buf.yaml at the repo root
# declares it), so the comparison is module against module.
if buf breaking --against ".git#ref=${base},subdir=proto"; then
	echo "no breaking proto changes against ${base}"
	exit 0
fi

if printf '%s' "${PR_TEXT:-}" | grep -qE 'ADR-[0-9]{4}'; then
	echo "breaking proto change is referenced to an ADR; allowing"
	exit 0
fi

echo "breaking proto change without an ADR reference in the PR title or body" >&2
exit 1
