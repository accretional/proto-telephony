#!/usr/bin/env bash
# LET_IT_RIP.sh – the whole pipeline: setup, build, test (via test.sh), then
# run itu-e212 against ITU to rebuild the committed CSVs in data/carriers/,
# then verify them. Extra args go to itu-e212, e.g.
#   ./LET_IT_RIP.sh -workers 8 -v
#   ./LET_IT_RIP.sh -format sqlite      # data/carriers.sqlite instead
set -euo pipefail
cd "$(dirname "$0")"

./test.sh

echo ""
echo "==> itu-e212"
./bin/itu-e212 -format csv "$@"

echo ""
./bin/itu-e212 -verify -format csv "$@"

echo ""
echo "LET_IT_RIP: done"
