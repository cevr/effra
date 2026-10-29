#!/usr/bin/env bash
# Verdict parity of the merge round (#196, parse-v1 round 6): the check
# package's tests checked old sources at BASE; at HEAD they check decided
# ones. This records every source each run checks, with its diagnostics,
# and compares them (main.go): a base source pairs with the head source
# that is its translation, and the pair keeps its codes and their mapped
# lines, or the report lists it as a changed verdict.
#
# usage: scripts/parity196/run.sh BASE HEAD OUTDIR
# Run from the repository root. BASE and HEAD are commits; each is checked
# out detached under OUTDIR, the recording hook (hook.go.txt) is added to
# its check package, and its check tests run once. The report is
# OUTDIR/report.txt. The scratch worktrees are moved to the trash at the
# end.
set -u -o pipefail
[ $# -eq 3 ] || { echo "usage: $0 BASE HEAD OUTDIR" >&2; exit 2; }
base=$1 head=$2 out=$3
root=$(pwd)
mkdir -p "$out"
export GOCACHE="${GOCACHE:-$HOME/.cache/go-build}"
export GOTMPDIR="${GOTMPDIR:-$HOME/.cache/go-tmp}"
mkdir -p "$GOTMPDIR"

record() { # record NAME COMMIT
  local name=$1 commit=$2 wt=$out/$1
  git worktree add --detach "$wt" "$commit" >/dev/null || exit 1
  cp "$root/scripts/parity196/hook.go.txt" "$wt/internal/sketch/check/zz_parity_hook.go"
  sed -i -e 's/^func (c \*checker) sorted() \[\]Diagnostic {/func (c *checker) sortedUnhooked() []Diagnostic {/' \
    -e 's/^func CheckSource(src string) (\*Package, \[\]Diagnostic) {/func checkSourceUnhooked(src string) (*Package, []Diagnostic) {/' \
    "$wt/internal/sketch/check/check.go"
  : > "$out/$name.jsonl"
  (cd "$wt" && ERGO_PARITY_LOG="$out/$name.jsonl" go test -count=1 ./internal/sketch/check/ > "$out/$name-test.log" 2>&1)
  echo "$name ($commit): $(tail -1 "$out/$name-test.log"); $(wc -l < "$out/$name.jsonl") sources"
}

record base "$base"
record head "$head"
go run ./scripts/parity196 "$out/base.jsonl" "$out/head.jsonl" > "$out/report.txt"
echo "report: $out/report.txt"
head -20 "$out/report.txt"
trash "$out/base" "$out/head"
git worktree prune
