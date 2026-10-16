#!/bin/sh
# Prints one declared block of the core's client contract, one entry per
# line: what docs/configuration.md holds between `<!-- NAME:begin -->` and
# `<!-- NAME:end -->`, without the fence and without a ` = n` suffix.
# Fails if the block is missing or empty.
# Usage: scripts/core-block.sh NAME [path to straum core]
set -eu
name=$1
core=${2:-../straum}
out=$(awk -v b="<!-- $name:begin -->" -v e="<!-- $name:end -->" '
	$0 == b { f = 1; next }
	$0 == e { f = 0 }
	f && !/^```/ && NF { sub(/ = [0-9]+$/, ""); print }
' "$core/docs/configuration.md")
[ -n "$out" ] || { echo "no $name block in $core/docs/configuration.md" >&2; exit 1; }
echo "$out"
