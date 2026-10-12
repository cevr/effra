#!/usr/bin/env bash
# spike-where TS caller view (#116). For each option directory: tsc strict on
# the consumer, the negatives and the attacks (each file on its own, because
# the attacks augment globals); node runs the consumer and the attacks; every
# @ts-expect-error is opened to show the diagnostic it suppresses; `any` is
# counted in the declarations and runtime.
#
# TypeScript 6.0.3 at $TSC; node >= 22.6 runs .mts directly. The Effect
# baseline resolves `effect` from the repository's node_modules (4.0.1).
# Usage: benchmarks/where/ts/run.sh [option-dir...]   (default: all)
set -u
here=$(cd "$(dirname "$0")" && pwd)
TSC=${TSC:-/home/exedev/Developer/personal/effect-oxlint/node_modules/typescript/lib/tsc.js}
FLAGS=(--noEmit --strict --module NodeNext --moduleResolution NodeNext --target ES2022 --lib ES2022,DOM,ESNext.Disposable)
options=("$@")
[ ${#options[@]} -eq 0 ] && options=(a-dict b-witness b-struct c-comparable baseline-effect)
echo "== versions"
echo "tsc $(node "$TSC" --version)"
echo "node $(node --version)"
echo "effect $(node -p "require('$here/../../../node_modules/effect/package.json').version")"
for option in "${options[@]}"; do
  cd "$here/$option" || exit 1
  echo
  echo "==== $option"
  for f in $(ls *.mts | grep -v "\.d\.mts$"); do
    echo "== tsc $f ($(grep -c '@ts-expect-error' "$f") directives)"
    node "$TSC" "${FLAGS[@]}" "$f"
    echo "tsc exit=$?"
  done
  for f in consumer.mts $(ls attacks*.mts 2>/dev/null); do
    [ -f "$f" ] || continue
    echo "== node $f"
    node "$f"
    echo "node exit=$?"
  done
  echo "== each directive opened"
  for f in *.mts; do
    grep -q '@ts-expect-error' "$f" && node "$here/tools/open-negatives.cjs" . "$f"
  done
  echo "== any"
  node "$here/tools/count-any.cjs" $(ls ./*.mjs ./*.mts 2>/dev/null)
done
