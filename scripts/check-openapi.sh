#!/usr/bin/env sh
# Lints the hand-authored operator surface contract (api/openapi.yaml): it
# must parse as OpenAPI 3.1 and pass the linter's recommended rules. This is
# a check on the document alone; that the document matches the mounted
# routes is a Go test in internal/api. Nothing conforms responses to the
# document at runtime in this version.
set -eu

cd "$(dirname "$0")/.."

VACUUM_VERSION="v0.30.5"

go run "github.com/daveshanley/vacuum@${VACUUM_VERSION}" lint --ruleset api/vacuum-ruleset.yaml --details --fail-severity warn --no-banner --no-style api/openapi.yaml
