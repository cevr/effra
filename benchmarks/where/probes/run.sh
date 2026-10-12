#!/usr/bin/env bash
# spike-where (#116) hard-case probes: the same key under style (a), explicit
# hash/equals arguments, and style (b), a where clause resolved through the
# owner module. Each probe's first line states the expected outcome
# (`// expect: accepted` or `// expect: EFnnn`). The prototype compiler is
# needed because the main compiler refuses user generic functions (EF127).
# Checks every probe on Go and JavaScript; exits 1 when an outcome differs.
# Usage: run.sh EF_PROTO
set -u
here=$(cd "$(dirname "$0")" && pwd)
EF=${1:?usage: run.sh EF_PROTO}
failures=0
for f in "$here"/*.ef; do
  expect=$(head -1 "$f" | sed -n 's|^// expect: \([A-Za-z0-9]*\).*|\1|p')
  for target in go js; do
    got=$("$EF" check "$f" --target "$target" 2>/dev/null | node -e '
      let s = ""; process.stdin.on("data", d => s += d).on("end", () => {
        const r = JSON.parse(s);
        console.log(r.checked ? "accepted" : r.diagnostics[0].code + "\t" + r.diagnostics[0].message);
      });')
    status=ok
    [ "${got%%$'\t'*}" = "$expect" ] || { status=MISMATCH; failures=$((failures + 1)); }
    printf '%-18s %-3s %-8s expect=%-8s %s\n' "$(basename "$f")" "$target" "$status" "$expect" "$got"
  done
done
echo "mismatches=$failures"
[ "$failures" -eq 0 ]
