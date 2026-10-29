#!/usr/bin/env sh
# Fails when the README's install commands do not name the tag under
# release. The docs are read at the release's tag, so a tag cut without the
# version-literal bump publishes a quickstart that fetches the previous
# release. The README's `git checkout v…` line is the sentinel: it is the one
# literal that cannot name the tag by accident, and the bump moves it with
# every other literal in one PR (docs/development.md#releasing, step 1).
# Reads README.md from the working tree, which the release workflow checks
# out at the tag.
# Usage: scripts/check-release-literals.sh <ref>   (refs/tags/v1.2.3 or v1.2.3)
set -eu

ref="${1:?usage: check-release-literals.sh <ref>}"

case "$ref" in
refs/tags/*) tag="${ref#refs/tags/}" ;;
refs/*)
	echo "${ref} is not a tag; release literals not checked"
	exit 0
	;;
*) tag="$ref" ;;
esac

cd "$(dirname "$0")/.."

named=$(sed -n 's/^git checkout \(v[^[:space:]]*\).*/\1/p' README.md)

if [ -z "$named" ]; then
	echo "README.md has no 'git checkout v…' line; the release-literals sentinel is missing" >&2
	exit 1
fi

for v in $named; do
	if [ "$v" != "$tag" ]; then
		echo "README.md's quickstart checks out ${v}, but the tag under release is ${tag}." >&2
		echo "The version literals were not bumped: docs/development.md#releasing, step 1" >&2
		echo "(write the notes, and bump the version literals) moves every install-doc literal" >&2
		echo "to the release being cut, in the PR the tag goes on." >&2
		exit 1
	fi
done

echo "README.md's quickstart checks out ${tag}"
