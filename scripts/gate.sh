#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
python3 scripts/wayfinder.py check
python3 -B scripts/import_effect_conformance.py --self-check
python3 -B scripts/test_import_effect_conformance.py
python3 -B scripts/check_effect_conformance.py
python3 -B scripts/test_effect_conformance.py
if [ -f go.mod ]; then
  test -z "$(gofmt -l cmd internal runtime examples/go-interop examples/sdk)"
  go vet ./...
  go test ./...
  go build -o bin/ef ./cmd/ef
  authored_examples_file=scripts/authored_examples.txt
  test -f "$authored_examples_file"
  set --
  while IFS= read -r example || [ -n "$example" ]; do
    if [ -z "$example" ]; then
      echo "empty authored example entry in $authored_examples_file" >&2
      exit 1
    fi
    if [ ! -f "$example" ]; then
      echo "missing authored example: $example" >&2
      exit 1
    fi
    set -- "$@" "$example"
  done < "$authored_examples_file"
  if [ "$#" -eq 0 ]; then
    echo "authored example selection is empty: $authored_examples_file" >&2
    exit 1
  fi
  ./bin/ef fmt --check "$@"
  python3 scripts/diagnostics_smoke.py
  python3 scripts/lsp_smoke.py
  python3 scripts/smoke.py
  python3 scripts/bundled_smoke.py
  python3 scripts/format_smoke.py
  python3 scripts/layer_smoke.py
  python3 scripts/http_smoke.py
fi
