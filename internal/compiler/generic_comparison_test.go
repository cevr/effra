package compiler

import "testing"

func TestComparisonLookaheadStopsAtFunctionBoundary(t *testing.T) {
	const source = `fn less(left: i64, right: i64) -> bool {
    left < right
}
fn greater(value: i64) -> bool {
    value > (0)
}
effect fn main() -> string {
    if less(1, 2) {
        if greater(1) { "ok" } else { "bad" }
    } else { "bad" }
}`
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := CompileFor(source, target)
			if !r.Checked {
				t.Fatalf("independent comparison functions were rejected: %+v", r.Diagnostics)
			}
		})
	}
}

func TestGenericLookaheadArityAndState(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		want   bool
	}{
		{name: "eight arguments admitted", source: "Call<A,B,C,D,E,F,G,H>()", want: true},
		{name: "nine arguments refused", source: "Call<A,B,C,D,E,F,G,H,I>()", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tokens, _, diagnostics := lex(test.source)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			types := map[string]*sourceType{"prior": {Application: "prior"}}
			angles := map[int]bool{77: true}
			p := &parser{tokens: tokens, at: 1, types: types, genericAngles: angles, absent: &absentScan{}}
			if got := p.genericApplicationAhead(); got != test.want {
				t.Fatalf("generic probe admitted=%v want=%v", got, test.want)
			}
			if p.at != 1 || len(types) != 1 || types["prior"] == nil || len(angles) != 1 || !angles[77] {
				t.Fatalf("generic probe mutated parser state: at=%d types=%v angles=%v", p.at, types, angles)
			}
		})
	}
}
