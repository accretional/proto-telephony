#!/usr/bin/env bash
# test.sh – formatting, vet and unit/integration tests. Runs build.sh first
# (idempotent). The tests are hermetic: they build synthetic ITU documents and
# serve them from a local fake ITU site, so no network is needed.
#
# If data/cache already holds ITU documents from a real run, also rebuilds the
# database from them offline into a temp file and verifies it.
set -euo pipefail
cd "$(dirname "$0")"

./build.sh

echo "==> gofmt"
unformatted=$(gofmt -l .)
[[ -z "$unformatted" ]] || { echo "gofmt needed:"; echo "$unformatted"; exit 1; }

echo "==> go vet"
go vet ./...

echo "==> go test"
go test -count=1 ./...

if compgen -G "data/cache/ob/*.docx" >/dev/null && compgen -G "data/cache/e212b/*.docx" >/dev/null; then
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  for format in sqlite csv; do
    echo "==> offline rebuild from data/cache (-format $format)"
    args=(-format "$format" -db "$tmp/carriers.sqlite" -csv-dir "$tmp/carriers")
    ./bin/itu-e212 -offline "${args[@]}" | tail -n 9
    ./bin/itu-e212 -verify "${args[@]}"
  done
fi

echo "test: ok"
