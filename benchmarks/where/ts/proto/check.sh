#!/usr/bin/env bash
# Builds sample.ef with the prototype compiler (branch wf10/spike-where-proto)
# to JavaScript, then type-checks and runs the TypeScript callers against the
# generated .d.mts. out/ is ignored; `effect` resolves from the repository's
# node_modules. Usage: check.sh EF_PROTO_BINARY
set -u
here=$(cd "$(dirname "$0")" && pwd)
EF=${1:?usage: check.sh EF_PROTO_BINARY}
TSC=${TSC:-/home/exedev/Developer/personal/effect-oxlint/node_modules/typescript/lib/tsc.js}
FLAGS=(--noEmit --strict --module NodeNext --moduleResolution NodeNext --target ES2022 --lib ES2022,DOM,ESNext.Disposable)
cd "$here" || exit 1
mkdir -p out
"$EF" build sample.ef --target js -o out/sample.mjs >/dev/null || exit 1
echo "== generated witness signatures"
grep -E 'function_(sort3|insert|lookup|User_compare|UserId_hash|UserId_equals):' out/sample.d.mts
for f in consumer.mts negatives.mts; do
  echo "== tsc $f"
  node "$TSC" "${FLAGS[@]}" "$f"
  echo "tsc exit=$?"
done
echo "== node consumer.mts"
node consumer.mts
echo "node exit=$?"
echo "== each directive opened"
node "$here/../tools/open-negatives.cjs" . negatives.mts
echo "== any"
node "$here/../tools/count-any.cjs" out/sample.d.mts out/sample.mjs consumer.mts negatives.mts
