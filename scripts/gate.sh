#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
python3 scripts/wayfinder.py check
if [ -f go.mod ]; then
  test -z "$(gofmt -l cmd internal)"
  go vet ./...
  go test ./...
fi
