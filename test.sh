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

# National registries: rebuild those with a cache, both formats.
national=()
for cc in ca mx jp; do
  [[ -d "data/cache/$cc" ]] && national+=("$cc")
done
if ((${#national[@]})); then
  only=$(IFS=,; echo "${national[*]}")
  for format in sqlite csv; do
    echo "==> offline rebuild: national-mnc -only $only -format $format"
    ./bin/national-mnc -offline -only "$only" -format "$format" -out-dir "$tmp/national" | sed -n '/== national-mnc/,$p'
  done
fi

# Merge the committed per-source CSVs (local only) and verify.
if compgen -G "data/carriers/*/assignments.csv" >/dev/null; then
  echo "==> merge data/carriers/*/assignments.csv"
  ./bin/carriers-merge -format csv -csv-dir "$tmp/merged" | sed -n '/\[2\/3\]/,$p'
fi

echo "test: ok"
