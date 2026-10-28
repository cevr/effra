package check

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/syntax"
)

// opsOf checks src as one package and returns its diagnostics and the
// operation counts of the reference walk's literals and the wire
// summaries.
func opsOf(t *testing.T, src string) (diags []Diagnostic, walk, wire int) {
	t.Helper()
	file, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	c := newChecker("main", Std, prelude().scope)
	c.run([]*syntax.File{file})
	return c.diags, c.walkOps, c.wireOps
}

// linear asserts that counts measured at sizes 1000, 4000 and 8000 grow
// linearly: each is at most k operations per unit, and the 8000 count is at
// most 8.5 times the 1000 count (a quadratic pass gives 64).
func linear(t *testing.T, what string, counts map[int]int, k int) {
	t.Helper()
	for n, ops := range counts {
		if ops > k*n {
			t.Errorf("%s: %d operations at n=%d, more than %d per unit", what, ops, n, k)
		}
	}
	if r := float64(counts[8000]) / float64(counts[1000]); r > 8.5 {
		t.Errorf("%s: 8000/1000 operation ratio %.1f, want linear growth (%v)", what, r, counts)
	}
	t.Logf("%s: %v", what, counts)
}

// TestWireChainLinear: an acyclic embedding chain of n empty wires costs
// linear work in the wire summaries (round 17 M1: every root and every
// embedding walked the whole chain).
func TestWireChainLinear(t *testing.T) {
	counts := map[int]int{}
	for _, n := range []int{1000, 4000, 8000} {
		var b strings.Builder
		b.WriteString("package p\nwire W0 {}\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "wire W%d { W%d }\n", i, i-1)
		}
		fmt.Fprintf(&b, "func F() { with(W%d) {} }\n", n-1)
		diags, _, wire := opsOf(t, b.String())
		if len(diags) > 0 {
			t.Fatalf("n=%d: want acceptance, got %v", n, diags[0])
		}
		counts[n] = wire
	}
	linear(t, "wire chain", counts, 6)
}

// TestAnonymousStructLinear: keyed and positional anonymous struct literals
// of n fields, each a slice literal with elided children, cost linear work
// in the reference walk (round 17 M2: each element scanned the field list).
func TestAnonymousStructLinear(t *testing.T) {
	for _, keyed := range []bool{true, false} {
		counts := map[int]int{}
		for _, n := range []int{1000, 4000, 8000} {
			var b strings.Builder
			b.WriteString("package p\ntype S struct{ a int }\nfunc F() { _ = struct{\n")
			for i := range n {
				fmt.Fprintf(&b, " f%d []S\n", i)
			}
			b.WriteString("}{")
			for i := range n {
				if keyed {
					fmt.Fprintf(&b, "f%d: ", i)
				}
				b.WriteString("[]S{{a: 1}, {a: 2}}, ")
			}
			b.WriteString("} }\n")
			diags, walk, _ := opsOf(t, b.String())
			if len(diags) > 0 {
				t.Fatalf("keyed=%v n=%d: want acceptance, got %v", keyed, n, diags[0])
			}
			counts[n] = walk
		}
		linear(t, fmt.Sprintf("anonymous struct (keyed=%v)", keyed), counts, 12)
	}
}
