#!/usr/bin/env bash
# setup.sh – check prerequisites and fetch Go module dependencies. Safe to run
# repeatedly: it only downloads what's missing. Called by build.sh.
#
# Needs: go >= 1.21 (go.mod's toolchain line makes go fetch the exact
# toolchain it needs). No cgo: SQLite is the pure-Go modernc.org/sqlite.
set -euo pipefail
cd "$(dirname "$0")"

die() { echo "setup: $*" >&2; exit 1; }

command -v go >/dev/null || die "go not found; install from https://go.dev/dl/"
have=$(go env GOVERSION | sed -E 's/^go([0-9]+)\.([0-9]+).*/\1 \2/')
read -r major minor <<<"$have"
(( major > 1 || minor >= 21 )) || die "go >= 1.21 required, found $(go env GOVERSION)"

# Downloads only modules not already in the module cache.
go mod download
go mod verify >/dev/null

mkdir -p data bin
echo "setup: ok ($(go env GOVERSION), modules verified)"
