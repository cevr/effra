#!/usr/bin/env bash
# spike-where (#116) hard-case probes: the same key under style (a), explicit
# hash/equals arguments, and style (b), a where clause resolved through the
# owner module. Each probe's first line states the expected outcome
# (`// expect: accepted` or `// expect: EFnnn`). The prototype compiler is
# needed because the main compiler refuses user generic functions (EF127).
# Checks every probe on Go and JavaScript; exits 1 when an outcome differs,
# a first line is missing or malformed, or a result cannot be parsed.
# Usage: run.sh EF_PROTO
set -u
here=$(cd "$(dirname "$0")" && pwd)
EF=${1:?usage: run.sh EF_PROTO}
failures=0
for f in "$here"/*.ef; do
  expect=$(head -1 "$f" | sed -n 's%^// expect: \(accepted\|EF[0-9][0-9][0-9]\)\( .*\)\{0,1\}$%\1%p')
  if [ -z "$expect" ]; then
    printf '%-18s INVALID  first line must be `// expect: accepted` or `// expect: EFnnn`\n' "$(basename "$f")"
    failures=$((failures + 1)); continue
  fi
  for target in go js; do
    # An unparseable result or a refusal without a diagnostic prints UNPARSEABLE.
    got=$("$EF" check "$f" --target "$target" 2>/dev/null | node -e '
      let s = ""; process.stdin.on("data", d => s += d).on("end", () => {
        try {
          const r = JSON.parse(s);
          if (r.checked === true) console.log("accepted");
          else if (r.checked === false && r.diagnostics && r.diagnostics[0] && /^EF[0-9]{3}$/.test(r.diagnostics[0].code))
            console.log(r.diagnostics[0].code + "\t" + r.diagnostics[0].message);
          else console.log("UNPARSEABLE");
        } catch { console.log("UNPARSEABLE"); }
      });')
    status=ok
    [ "${got%%$'\t'*}" = "$expect" ] || { status=MISMATCH; failures=$((failures + 1)); }
    printf '%-18s %-3s %-8s expect=%-8s %s\n' "$(basename "$f")" "$target" "$status" "$expect" "$got"
  done
done
echo "mismatches=$failures"
[ "$failures" -eq 0 ]
