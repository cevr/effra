package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// unionSwitch writes a one-subject match over a union of variants empty
// records, with one case for each of the first cases variants and a
// case _.
func unionSwitch(variants, cases int) string {
	var b strings.Builder
	for v := range variants {
		fmt.Fprintf(&b, "type V%d {}\n", v)
	}
	b.WriteString("type U = ")
	for v := range variants {
		if v > 0 {
			b.WriteString(" | ")
		}
		fmt.Fprintf(&b, "V%d", v)
	}
	b.WriteString("\nfunc F(u U) int {\n\tmatch u {\n")
	if cases == variants {
		cases-- // Leave a variant for the default arm.
	}
	for v := range cases {
		fmt.Fprintf(&b, "\tcase V%d:\n\t\treturn %d\n", v, v)
	}
	b.WriteString("\tcase _:\n\t\treturn -1\n\t}\n}\n")
	return check.Decided(b.String())
}

// TestVariantResolutionLinear: resolving a type switch's cases to variants
// is a keyed lookup, so its counted operations (index entries built and
// lookups) grow linearly in the variants and the cases, never as their
// product (round-5 M2; D16 as amended in round 5: nothing before the
// solvers' counters is superlinear). Round 5 scanned the variants for
// each case: n(n+1)/2 comparisons. Counted, not timed.
func TestVariantResolutionLinear(t *testing.T) {
	type size struct{ variants, cases int }
	ops := func(s size) int {
		n, diags := check.VariantOps(unionSwitch(s.variants, s.cases))
		for _, d := range diags {
			// A long unary switch may exhaust the counted reachability
			// budget (S103): allowed, and counted there.
			if d.Code != "S103" {
				t.Fatalf("%v: unexpected diagnostic %s", s, d)
			}
		}
		return n
	}
	for _, pair := range [][2]size{
		{{1000, 1000}, {4000, 4000}}, // variants and cases together
		{{1000, 10}, {4000, 10}},     // variants only
		{{4000, 1000}, {4000, 4000}}, // cases only
	} {
		small, large := ops(pair[0]), ops(pair[1])
		t.Logf("%v: %d operations; %v: %d operations", pair[0], small, pair[1], large)
		if large > 3*(pair[1].variants+pair[1].cases) {
			t.Errorf("%v: %d operations, want at most %d", pair[1], large, 3*(pair[1].variants+pair[1].cases))
		}
		if ratio := float64(large) / float64(small); ratio > 4.5 {
			t.Errorf("%v to %v: the operations grow %.2fx; resolution is superlinear", pair[0], pair[1], ratio)
		}
	}
}
