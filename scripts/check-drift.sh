#!/bin/sh
# Fails if a metric the CLI relies on (stable-metrics.txt) is not in the
# core's stable set (docs/configuration.md, between the stable-metrics
# markers, pinned by a test in the core's node crate). A stable name the
# CLI does not use yet is reported, not failed.
# Usage: scripts/check-drift.sh [path to straum core]
set -eu
core=${1:-../straum}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
sed -n '/<!-- stable-metrics:begin -->/,/<!-- stable-metrics:end -->/p' "$core/docs/configuration.md" |
	grep -E '^straum_[a-z_]+$' | sort >"$tmp/core"
[ -s "$tmp/core" ] || { echo "no stable-metrics block in $core/docs/configuration.md" >&2; exit 1; }
sort stable-metrics.txt >"$tmp/cli"
missing=$(comm -23 "$tmp/cli" "$tmp/core")
unused=$(comm -13 "$tmp/cli" "$tmp/core")
[ -z "$unused" ] || echo "stable in the core, not used by the CLI: $unused"
if [ -n "$missing" ]; then
	echo "the CLI relies on metrics the core does not declare stable: $missing" >&2
	exit 1
fi
