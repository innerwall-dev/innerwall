#!/usr/bin/env sh
# Fails when a migration that is already on main changes. A migration is
# applied once it is on main: from then on a database somewhere may have
# recorded it, goose never runs it again, and an edit would split the
# schema between databases migrated before and after. Before that, while
# the file exists only on the branch that adds it, it may be amended
# freely. The fix for a mistake in an applied migration is a new one.
# Usage: scripts/check-migrations.sh <base-ref>
set -eu

base="${1:?usage: check-migrations.sh <base-ref>}"

cd "$(dirname "$0")/.."

# Changes since the branch left the base: files the base already has that
# were modified, deleted, renamed, or retyped. Additions pass.
changed=$(git diff --no-renames --diff-filter=MDT --name-only "${base}...HEAD" -- 'internal/store/migrations/*.sql')

if [ -n "$changed" ]; then
	echo "applied migrations changed (present on ${base}; add a new migration instead):" >&2
	echo "$changed" >&2
	exit 1
fi

echo "no applied migration changed against ${base}"
