#!/usr/bin/env bash
# Acceptance check for the sketch-9 prototype compiler (wf10/sketch-proto).
# Usage: prototypes/sketch9/accept.sh
# Exit 0 means every check passed. Red until the prototype exists.
set -euo pipefail
cd "$(dirname "$0")"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "== unit tests"
go vet ./...
go test -count=1 ./...

echo "== checkout: emit, vet, build, run"
go run ./cmd/sketchc build testdata/checkout.sketch -out "$work/gen" -o "$work/checkout"
(cd "$work/gen" && go vet ./...)
got=$("$work/checkout")
want=$(cat testdata/checkout.out)
if [ "$got" != "$want" ]; then
	echo "checkout output mismatch"
	echo "want: $want"
	echo "got:  $got"
	exit 1
fi

echo "== emitted shape: direct calls, no closure per call"
if grep -q 'func() efEffect\|)(ctx)' "$work/gen/main.go"; then
	echo "emitted Go uses the recipe-closure shape"
	exit 1
fi
grep -q '^//line checkout.sketch:' "$work/gen/main.go"

echo "== rejections"
for f in testdata/reject/*.sketch; do
	code=$(head -1 "$f" | sed 's|^// want ||')
	if out=$(go run ./cmd/sketchc check "$f" 2>&1); then
		echo "$f: accepted, want $code"
		exit 1
	fi
	if ! printf '%s\n' "$out" | grep -q "$code"; then
		echo "$f: want $code, got:"
		printf '%s\n' "$out"
		exit 1
	fi
	echo "$f: $code"
done

echo "== accepted: checkout passes check"
go run ./cmd/sketchc check testdata/checkout.sketch

echo "ACCEPT OK"
