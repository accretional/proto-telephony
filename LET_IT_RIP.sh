#!/usr/bin/env bash
# LET_IT_RIP.sh – the whole pipeline: setup, build, test (via test.sh), then
# run every downloader against its live source to rebuild the committed CSVs,
# then verify them:
#   bin/itu-e212 -> data/carriers/itu/*.csv   (ITU-T E.212 MNC list)
#   bin/us-hni   -> data/carriers/us/*.csv    (US HNIs from IMSI Admin)
#   bin/national-mnc -> data/carriers/{ca,mx,jp}/*.csv (CNAC, IFT/CRT, MIC)
# Extra args go to itu-e212 and us-hni (-workers, -v, -format, -offline...);
# national-mnc gets -format and -offline:
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

# National registries (data/carriers/{ca,mx,jp}/). They share only -format
# and -offline with the downloaders above, so pass just those through.
national_args=(-format csv)
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  case "${args[i]}" in
    -format) national_args+=(-format "${args[i+1]}"); i=$((i + 1)) ;;
    -format=*|-offline) national_args+=("${args[i]}") ;;
  esac
done
echo ""
echo "==> national-mnc"
./bin/national-mnc "${national_args[@]}"

echo ""
echo "LET_IT_RIP: done"
