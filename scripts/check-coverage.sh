#!/bin/sh
# Fails unless docs/coverage.md names every core contract element (in
# backticks) and every matrix row is done, verified, blocked or n/a.
# Usage: scripts/check-coverage.sh [path to straum core]
set -eu
core=${1:-../straum}
matrix=docs/coverage.md
here=$(dirname "$0")
fail=0
elements=$("$here/core-elements.sh" "$core")
[ -n "$elements" ] || { echo "no core elements found under $core: did the core move them?" >&2; exit 1; }
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
