#!/bin/sh
# Prints the core's contract elements, one per line: journal record kinds,
# the reason codes the gateway refuses with, and the STRAUM_* variables the
# server reads. Production code only; test modules are cut off.
# Usage: scripts/core-elements.sh [path to straum core]
set -eu
core=${1:-../straum}
nontest() { sed '/#\[cfg(test)\]/,$d' "$1"; }
{
	nontest "$core/crates/journal/src/lib.rs" |
		awk '/pub enum Kind \{/ { f = 1; next } f && /^}/ { exit } f' |
		grep -oE '^[[:space:]]+[A-Z][A-Za-z]+' | tr -d ' \t'
	nontest "$core/crates/gateway/src/lib.rs" |
		grep -oE 'refuse_connect\([^)]*ReasonCode::[A-Za-z]+|ReasonCode::[A-Za-z]+NotSupported' |
		grep -oE '[A-Za-z]+$'
	nontest "$core/crates/server/src/config.rs" | grep -oE '"STRAUM_[A-Z_]+"' | tr -d '"'
} | sort -u
