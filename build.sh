#!/usr/bin/env bash
# build.sh – build bin/itu-e212. Runs setup.sh first (idempotent).
set -euo pipefail
cd "$(dirname "$0")"

./setup.sh
go build -o bin/itu-e212 ./cmd/itu-e212
echo "build: ok (bin/itu-e212)"
