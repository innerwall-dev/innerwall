#!/usr/bin/env sh
# Agent installer. Takes a join token; that is the whole interface.
#
#   curl -fsSL https://<control-plane>/install.sh | sh -s -- --token <join-token>
#
# The installer places the agent binary, generates the agent's keypair locally
# (the private key never leaves this host), and enrolls with the join token
# (ADR-0004). Enrollment arrives with milestone M2; until then this script only
# validates its arguments.
set -eu

usage() {
	echo "usage: install.sh --token <join-token> [--control-plane <host:port>]" >&2
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
