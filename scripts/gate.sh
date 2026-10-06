#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
python3 scripts/wayfinder.py check
if [ -f go.mod ]; then
  test -z "$(gofmt -l cmd internal runtime examples/go-interop examples/sdk)"
  go vet ./...
  go test ./...
  go build -o bin/ef ./cmd/ef
  python3 scripts/diagnostics_smoke.py
  python3 scripts/smoke.py
  python3 scripts/format_smoke.py
  python3 scripts/http_smoke.py
fi
