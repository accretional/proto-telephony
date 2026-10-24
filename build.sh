#!/usr/bin/env bash
# build.sh – build the downloaders into bin/. Runs setup.sh first (idempotent).
set -euo pipefail
cd "$(dirname "$0")"

./setup.sh
for cmd in itu-e212 us-hni national-mnc; do
  go build -o "bin/$cmd" "./cmd/$cmd"
done
echo "build: ok (bin/itu-e212, bin/us-hni, bin/national-mnc)"
