#!/usr/bin/env bash
# Runs the live tests: the CLI as a process against straum-broker and
# straum-fake-server, both built from the Straum commit in CONTRACT.
#
#   scripts/live.sh <straum-checkout> [go test flags...]
#
# The checkout must be at the commit CONTRACT pins; cargo builds both
# binaries in the checkout's target/.
set -euo pipefail

src="${1:?usage: scripts/live.sh <straum-checkout> [go test flags...]}"
shift
src="$(cd "$src" && pwd)"
root="$(cd "$(dirname "$0")/.." && pwd)"
pinned="$(sed -n 's/^commit: //p' "$root/CONTRACT")"
commit="$(git -C "$src" rev-parse HEAD)"
if [ "$commit" != "$pinned" ]; then
    echo "live: $src is at $commit; CONTRACT pins $pinned" >&2
    exit 1
fi
(cd "$src" && cargo build --release --locked -p straum-broker -p straum-conformance)
export STRAUM_BROKER_BIN="$src/target/release/straum-broker"
export STRAUM_FAKE_SERVER_BIN="$src/target/release/straum-fake-server"
cd "$root"
go test -tags live -count=1 -run '^TestLive' "$@" .
