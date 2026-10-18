#!/usr/bin/env sh
# make dev: the compose stack with the control plane built from this checkout
# (docker-compose.dev.yml over docker-compose.yml), in the foreground.
set -eu

cd "$(dirname "$0")/.."

# Naming files turns off compose's automatic read of docker-compose.override.yml
# (the multi-host guide's), so pass it on when it is present.
override=""
if [ -f docker-compose.override.yml ]; then
	override="-f docker-compose.override.yml"
fi

# docker-compose.yml requires INNERWALL_IMAGE; the dev file replaces the image
# with its own, so the value here only satisfies that check and, set in the
# environment, overrides whatever .env names.
# shellcheck disable=SC2086 # $override is zero or two words
INNERWALL_IMAGE=innerwall:dev exec docker compose \
	-f docker-compose.yml -f docker-compose.dev.yml $override up --build "$@"
