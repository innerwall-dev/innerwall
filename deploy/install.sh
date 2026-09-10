#!/usr/bin/env sh
# Agent installer. Takes a provisioning token; that is the whole interface.
#
#   curl -fsSL https://<control-plane>/install.sh | sh -s -- --token <provisioning-token>
#
# The installer places the agent binary, generates the agent's keypair locally
# (the private key never leaves this host), and enrolls with the provisioning
# token (ADR-0016) by running `innerwall-agent enroll`. Wiring the binary
# placement and the enroll call into this script is a later milestone; until
# then it only validates its arguments.
set -eu

usage() {
	echo "usage: install.sh --token <provisioning-token> [--control-plane <host:port>]" >&2
	exit 2
}

TOKEN=""
CONTROL_PLANE=""
while [ $# -gt 0 ]; do
	case "$1" in
		--token) TOKEN="${2:-}"; shift 2 ;;
		--control-plane) CONTROL_PLANE="${2:-}"; shift 2 ;;
		-h|--help) usage ;;
		*) echo "unknown argument: $1" >&2; usage ;;
	esac
done

[ -n "$TOKEN" ] || usage

echo "innerwall-agent install: enrollment is not available yet (milestone M2)." >&2
exit 1
