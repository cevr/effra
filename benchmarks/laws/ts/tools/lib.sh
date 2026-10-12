# Copied from benchmarks/where/ts/tools/lib.sh on wf10/spike-where (769e2dde) so the laws probe runs the same pinned TypeScript.
# Shared by run.sh and proto/check.sh (sourced). Resolves the repository's
# pinned TypeScript relative to this file, asserts its version, and collects
# failures so the caller can exit non-zero.
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)
TS_DIR=conformance/framework-ports/ports/node_modules/typescript
TSC="$repo/$TS_DIR/lib/tsc.js"
TS_VERSION=6.0.3
FLAGS=(--noEmit --strict --module NodeNext --moduleResolution NodeNext --target ES2022 --lib ES2022,DOM,ESNext.Disposable)
failures=()
fail() { failures+=("$1"); echo "FAILED: $1"; }
# check LABEL: records the previous command's exit status.
check() { local status=$?; echo "exit=$status"; [ "$status" -eq 0 ] || fail "$1 (exit $status)"; return "$status"; }
finish() {
  echo
  if [ ${#failures[@]} -eq 0 ]; then echo "== result: all checks passed"; exit 0; fi
  echo "== result: ${#failures[@]} failed"; printf '  %s\n' "${failures[@]}"; exit 1
}
if [ ! -f "$TSC" ]; then
  echo "missing $repo/$TS_DIR: install the framework-ports dependencies (the gate does)" >&2; exit 2
fi
found=$(node -p "require('$repo/$TS_DIR/package.json').version")
if [ "$found" != "$TS_VERSION" ]; then
  echo "TypeScript at $TS_DIR is $found, expected $TS_VERSION" >&2; exit 2
fi
