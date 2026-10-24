#!/usr/bin/env bash
# fetch.sh – rebuild data/vendored/third_party_indices/ from the third-party
# MCC/MNC indices, then compare them with the official data:
#   wireshark.py  -> wireshark/   (Wireshark packet-e212.c tables)
#   aosp.py       -> aosp/        (AOSP TelephonyProvider carrier ID table)
#   wikipedia.py  -> wikipedia/   (English Wikipedia MNC lists)
#   compare.py    -> comparison/  (vs data/carriers/merged/current.csv)
# Arguments are passed to every script:
#   scripts/third_party_indices/fetch.sh                        # CSV
#   scripts/third_party_indices/fetch.sh --format sqlite        # <source>.sqlite instead
#   scripts/third_party_indices/fetch.sh --format csv,sqlite    # both
#   scripts/third_party_indices/fetch.sh --offline              # rebuild from data/cache/
# Per-source flags (e.g. aosp.py --ref, wireshark.py --ref) need the script run directly.
set -euo pipefail
cd "$(dirname "$0")/../.."

PYTHON=${PYTHON:-python3}
for source in wireshark aosp wikipedia; do
  echo "==> $source"
  "$PYTHON" "scripts/third_party_indices/$source.py" "$@"
done

if [[ -f data/carriers/merged/current.csv ]]; then
  echo "==> compare"
  "$PYTHON" scripts/third_party_indices/compare.py "$@"
else
  echo "==> compare: skipped (no data/carriers/merged/current.csv; run ./LET_IT_RIP.sh first)"
fi

echo "fetch: done"
