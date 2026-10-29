package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// rowPattern writes the reviewer's round-10 M1 shape: n distinct empty
// structs A0 … A(n-1); U[T] listing, for each Ai, V[func(Ai) error[Fault[T]]]
// and V[func(Ai) error[Fault[int]]]; a field F of type U[G[G[… int]]] with
// the G argument nested d levels and written once; and a switch over n
// subjects x.F with underscore bindings, one tuple of the fixed
// V[func(Ai) error[Fault[int]]] variants, and default. Each signature path
// substitutes its row pattern error[Fault[T]] with the deep argument.
func rowPattern(n, d int) string {
	var b strings.Builder
	b.WriteString("package main\ntype G[T any] struct{}\ntype V[T any] struct{}\ntype Fault[T any] struct{}\n")
	for i := range n {
		fmt.Fprintf(&b, "type A%d struct{}\n", i)
	}
	b.WriteString("type U[T any] interface {\n\t")
	for i := range n {
		if i > 0 {
			b.WriteString(" | ")
		}
		fmt.Fprintf(&b, "V[func(A%d) error[Fault[T]]] | V[func(A%d) error[Fault[int]]]", i, i)
	}
	b.WriteString("\n}\ntype B struct{ F U[" + strings.Repeat("G[", d) + "int" + strings.Repeat("]", d) + "] }\n")
	b.WriteString("func F(x B) int {\n\tswitch ")
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("_")
	}
	b.WriteString(" := ")
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("x.F.(type)")
	}
	b.WriteString(" {\n\tcase (")
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "V[func(A%d) error[Fault[int]]]", i)
	}
	b.WriteString("):\n\t\treturn 1\n\tdefault:\n\t\treturn 0\n\t}\n}\n")
	return check.FromOld(b.String())
}

// TestRowPatternLinear: substituting a row pattern with a deep bound
// argument costs the pattern, not the argument (class E, hash-consing). The
// instance cache and the row key on interned ids, so Fault[deep] is one
// lookup of (Fault, id of deep) per signature path, and every node the
// table builds in a lookup is charged to the switch. Round 10 made
// 2n(d+2)+1 uncharged recursive key visits here (7441, 116161 and 462721
// at n=d=60, 240 and 480, past the budget at 480 with no S103) while its
// counter showed 1923, 7683 and 15363. Counted, not timed.
func TestRowPatternLinear(t *testing.T) {
	var prevN, prevOps, prevBuilds int
	for _, n := range []int{60, 240, 480} {
		src := rowPattern(n, n)
		ops, builds, d := check.LookupWork(src)
		if len(d) > 0 {
			t.Fatalf("n=d=%d: diagnostics %s", n, describe(d))
		}
		t.Logf("n=d=%d bytes=%d variant ops=%d identity nodes built in lookups=%d", n, len(src), ops, builds)
		if builds > 8*n {
			t.Errorf("n=d=%d: %d identity nodes built inside lookups, more than 8 per signature path", n, builds)
		}
		if ops > 40*n {
			t.Errorf("n=d=%d: %d variant operations, more than 40 per signature path", n, ops)
		}
		if prevN > 0 && (ops*prevN*10 > prevOps*n*11 || builds*prevN*10 > prevBuilds*n*11) {
			t.Errorf("n=d %d→%d: work grows from %d/%d to %d/%d, faster than the source", prevN, n, prevOps, prevBuilds, ops, builds)
		}
		prevN, prevOps, prevBuilds = n, ops, builds
	}
}
