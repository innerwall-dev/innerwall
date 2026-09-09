#!/usr/bin/env sh
# Regenerates all generated code and fails if the result differs from what is
# committed. Generated code is never hand-edited; the source of truth is proto/
# and internal/store/queries (ADR-0006, ADR-0007).
set -eu

cd "$(dirname "$0")/.."

GENERATED_PATHS="internal/gen internal/store/db"

make proto sqlc

if ! git diff --exit-code -- $GENERATED_PATHS; then
	echo "generated code is out of date: run 'make proto sqlc' and commit the result" >&2
	exit 1
fi

untracked=$(git ls-files --others --exclude-standard -- $GENERATED_PATHS)
if [ -n "$untracked" ]; then
	echo "generated files are not committed:" >&2
	echo "$untracked" >&2
	exit 1
fi

echo "generated code is up to date"
