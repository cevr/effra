#!/usr/bin/env bash
# Build-time receipt for the emitted checkout program.
# Usage: prototypes/sketch9/bench.sh
# One cold build with an empty GOCACHE, then three rebuilds, each after a
# one-literal source edit to a value never built before.
set -euo pipefail
cd "$(dirname "$0")"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
go build -o "$work/sketchc" ./cmd/sketchc
# The program builds get their own empty cache, so the cold build compiles the
# standard library packages it imports too.
export GOCACHE="$work/gocache"

ms() { date +%s%N; }

run() {
	local label=$1 src=$2
	local t0 t1 t2
	t0=$(ms)
	"$work/sketchc" build "$src" -out "$work/gen"
	t1=$(ms)
	(cd "$work/gen" && go build -o "$work/checkout" .)
	t2=$(ms)
	printf '%s emit_ms=%d go_build_ms=%d output=%s\n' "$label" \
		$(((t1 - t0) / 1000000)) $(((t2 - t1) / 1000000)) "$("$work/checkout" | head -1)"
}

cp testdata/checkout.sketch "$work/checkout.sketch"
run cold "$work/checkout.sketch"
stamp=$(date +%s%N)
for i in 1 2 3; do
	sed "s/\"paid \"/\"paid-$stamp-$i \"/" testdata/checkout.sketch >"$work/checkout.sketch"
	run "edit$i" "$work/checkout.sketch"
done
printf 'emitted main.go lines: %d\n' "$(wc -l <"$work/gen/main.go")"
printf 'emitted rt.go lines: %d\n' "$(wc -l <"$work/gen/rt/rt.go")"
