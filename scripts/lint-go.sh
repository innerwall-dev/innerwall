#!/usr/bin/env sh
# Lints the Go code in every build configuration that has files of its own.
# A linter analyses only the files the build constraints select, so a file
# behind a build tag is checked only by a run that sets the tag. Two runs
# cover every file:
#
#   - the default build, which is what ships: the release stub of the dev
#     commands (!dev) and the embedded console (!noconsole);
#   - every opt-in tag at once: dev (the development commands), noconsole
#     (the console stub), and netns (the network-namespace suites).
#
# No file is constrained on a combination of these tags, so the two runs
# leave none of them out. A new opt-in tag joins the second run's list.
# Both runs report even when the first fails, so one invocation shows every
# finding. CI's lint job makes the same two runs; keep the lists equal.
set -eu

cd "$(dirname "$0")/.."

status=0
golangci-lint run ./... || status=1
golangci-lint run --build-tags dev,noconsole,netns ./... || status=1
exit "$status"
