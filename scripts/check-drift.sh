#!/usr/bin/env sh
# Regenerates all generated code and fails if the result differs from what is
# committed. Generated code is never hand-edited; the source of truth is proto/,
# internal/store/queries (ADR-0006, ADR-0007), and api/openapi.yaml for the
# console's types. The console step needs ui/node_modules (npm --prefix ui ci).
set -eu

cd "$(dirname "$0")/.."

GENERATED_PATHS="internal/gen internal/store/db ui/src/api/generated.ts"

make proto sqlc console-api

if ! git diff --exit-code -- $GENERATED_PATHS; then
	echo "generated code is out of date: run 'make proto sqlc console-api' and commit the result" >&2
	exit 1
fi

untracked=$(git ls-files --others --exclude-standard -- $GENERATED_PATHS)
if [ -n "$untracked" ]; then
	echo "generated files are not committed:" >&2
	echo "$untracked" >&2
	exit 1
fi

echo "generated code is up to date"
