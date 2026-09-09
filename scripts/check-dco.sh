#!/usr/bin/env sh
# Verifies that every commit in a range carries a Signed-off-by trailer
# (ADR-0013). Usage: scripts/check-dco.sh <base>..<head>
set -eu

range="${1:?usage: check-dco.sh <base>..<head>}"

commits=$(git rev-list --no-merges "$range")

status=0
for sha in $commits; do
	if ! git show -s --format=%B "$sha" | grep -qE '^Signed-off-by: .+ <.+@.+>$'; then
		echo "missing Signed-off-by: $(git show -s --format='%h %s' "$sha")" >&2
		status=1
	fi
done

if [ "$status" -eq 0 ]; then
	echo "all commits in $range are signed off"
fi
exit "$status"
