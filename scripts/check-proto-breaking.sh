#!/usr/bin/env sh
# Fails on a breaking change to proto/ against a base ref unless the change is
# accompanied by an ADR reference (ADR-0007). Usage:
#   scripts/check-proto-breaking.sh <base-ref>
# The text searched for an ADR reference (PR title and body) comes from
# PR_TEXT in the environment.
set -eu

base="${1:?usage: check-proto-breaking.sh <base-ref>}"

cd "$(dirname "$0")/.."

if ! git cat-file -e "${base}:proto/buf.yaml" 2>/dev/null; then
	echo "base ref ${base} has no proto module; nothing to compare"
	exit 0
fi

if buf breaking proto --against ".git#ref=${base},subdir=proto"; then
	echo "no breaking proto changes against ${base}"
	exit 0
fi

if printf '%s' "${PR_TEXT:-}" | grep -qE 'ADR-[0-9]{4}'; then
	echo "breaking proto change is referenced to an ADR; allowing"
	exit 0
fi

echo "breaking proto change without an ADR reference in the PR title or body" >&2
exit 1
