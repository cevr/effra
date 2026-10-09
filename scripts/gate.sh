#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if ! command -v tsc >/dev/null 2>&1; then
  echo "typescript: unchecked (no tsc on PATH); the README comparison program is not strictly type-checked" >&2
else
  echo "typescript: strict ($(command -v tsc))"
fi
# Bytecode is a build product. A tracked one is stale on the next Python.
tracked_pyc=$(git ls-files '*.pyc')
if [ -n "$tracked_pyc" ]; then
  echo "tracked Python bytecode (git rm it; __pycache__/ is ignored):" >&2
  echo "$tracked_pyc" >&2
  exit 1
fi
python3 scripts/wayfinder.py check
python3 scripts/wayfinder_migration.py --input-snapshot hosted-run-binding-2026-10-08 --check
python3 scripts/wayfinder_migration.py --current-identity-intake github-wayfinder-current-intake-2026-10-08 --mapping docs/wayfinder/migration/current-hosted-identities-2026-10-09-source-reconciled.json --check --output-snapshot current-wayfinder-map-2026-10-09-source-reconciled
python3 -B scripts/test_wayfinder_migration.py
python3 -B scripts/test_wayfinder_hosted_reconciliation.py
python3 -B scripts/import_effect_conformance.py
python3 -B scripts/test_import_effect_conformance.py
python3 -B scripts/check_effect_conformance.py
python3 -B scripts/test_effect_conformance.py
if [ -f go.mod ]; then
  test -z "$(gofmt -l cmd internal runtime lint examples/go-interop examples/sdk examples/compare examples/hosttypes examples/lintpack)"
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
  python3 scripts/readme_smoke.py
  python3 scripts/bundled_smoke.py
  python3 scripts/producer_smoke.py
  python3 scripts/format_smoke.py
  python3 scripts/mcp_text_smoke.py
  python3 scripts/layer_smoke.py
  python3 scripts/type_smoke.py
  python3 scripts/http_smoke.py
  python3 scripts/http_transport_smoke.py
fi
