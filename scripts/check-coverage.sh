#!/bin/sh
# Fails unless docs/coverage.md names every element of the core's declared
# client contract (in backticks) and every matrix row is done, verified,
# blocked or n/a. The elements are the blocks of the core's
# docs/configuration.md, each pinned by a test in the core: stable-settings,
# stable-kinds, reserved-kinds and stable-reasons.
# Usage: scripts/check-coverage.sh [path to straum core]
set -eu
core=${1:-../straum}
matrix=docs/coverage.md
here=$(dirname "$0")
fail=0
elements=$(for b in stable-settings stable-kinds reserved-kinds stable-reasons; do
	"$here/core-block.sh" "$b" "$core" || exit 1
done) || exit 1
for e in $elements; do
	if ! grep -qF "\`$e\`" "$matrix"; then
		echo "$matrix: core element $e is not in the matrix" >&2
		fail=1
	fi
done
# The Status column is the fourth; header and separator rows are skipped.
bad=$(awk -F'|' '/^\|/ && $2 !~ /Capability|---/ {
	s = $5; gsub(/^ +| +$/, "", s)
	if (s != "done" && s != "verified" && s != "blocked" && s != "n/a") print NR ": " s ": " $2
}' "$matrix")
if [ -n "$bad" ]; then
	echo "$matrix: rows not done, verified, blocked or n/a:" >&2
	echo "$bad" >&2
	fail=1
fi
exit $fail
