#!/usr/bin/env bash
# test.sh – formatting, vet and unit/integration tests. Runs build.sh first
# (idempotent). The tests are hermetic: they build synthetic source documents
# and serve them from local fake sites, so no network is needed.
#
# For each downloader whose cache (data/cache/<name>) holds a real run, also
# rebuilds its output offline, in both formats, into a temp dir and verifies it.
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

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
for pair in itu-e212:itu us-hni:us; do
  cmd=${pair%%:*} name=${pair##*:}
  [[ -d "data/cache/$name" ]] || continue
  for format in sqlite csv; do
    echo "==> offline rebuild: $cmd -format $format (from data/cache/$name)"
    args=(-format "$format" -db "$tmp/$name.sqlite" -csv-dir "$tmp/$name")
    "./bin/$cmd" -offline "${args[@]}" | sed -n '/== summary/,$p'
    "./bin/$cmd" -verify "${args[@]}"
  done
done

echo "test: ok"
