#!/usr/bin/env bash
# Validation levels; success is the exit status.
#
#   scripts/validate.sh quick                    before every commit
#   scripts/validate.sh push <straum-checkout>   before every push
#   scripts/validate.sh pr <straum-checkout>     before a PR
#
# quick: gofmt, go vet and staticcheck (also with the live tag), unit,
#        golden and docs tests.
# push:  quick, govulncheck, unit tests with -race, the live tests
#        (scripts/live.sh) with -race.
# pr:    push, then the six-platform cross build and the goreleaser
#        configuration check.
set -euo pipefail

STATICCHECK="honnef.co/go/tools/cmd/staticcheck@v0.8.2"
GOVULNCHECK="golang.org/x/vuln/cmd/govulncheck@v1.9.0"
level="${1:?usage: scripts/validate.sh quick|push|pr [straum-checkout]}"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

quick() {
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then
        echo "gofmt: $unformatted" >&2
        return 1
    fi
    go vet ./...
    go vet -tags live ./...
    GOOS=windows go vet ./...
    go run "$STATICCHECK" ./...
    go run "$STATICCHECK" -tags live ./...
    go test -count=1 ./...
}

push() {
    straum="${1:?usage: scripts/validate.sh $level <straum-checkout>}"
    quick
    go run "$GOVULNCHECK" ./...
    go test -race -count=1 ./...
    scripts/live.sh "$straum" -race
}

case "$level" in
quick) quick ;;
push) push "${2:-}" ;;
pr)
    push "${2:-}"
    make cross
    make release-check
    ;;
*)
    echo "unknown level $level" >&2
    exit 2
    ;;
esac
echo "validate $level: ok"
