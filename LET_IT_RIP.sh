#!/usr/bin/env bash
# LET_IT_RIP.sh – the whole pipeline: setup, build, test (via test.sh), then
# run every downloader against its live source to rebuild the committed CSVs,
# then verify them:
#   bin/itu-e212 -> data/carriers/itu/*.csv   (ITU-T E.212 MNC list)
#   bin/us-hni   -> data/carriers/us/*.csv    (US HNIs from IMSI Admin)
# Extra args go to both (the shared flags: -workers, -v, -format, -offline...):
#   ./LET_IT_RIP.sh -workers 8
#   ./LET_IT_RIP.sh -format sqlite      # data/carriers/{itu,us}.sqlite instead
set -euo pipefail
cd "$(dirname "$0")"

./test.sh

for cmd in itu-e212 us-hni; do
  echo ""
  echo "==> $cmd"
  "./bin/$cmd" -format csv "$@"
  echo ""
  "./bin/$cmd" -verify -format csv "$@"
done

echo ""
echo "LET_IT_RIP: done"
