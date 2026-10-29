package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// genericDefaultSwitch writes the reviewer's round-6 M5 family: k generic
// variants Vj[T], one union U[T] of them, n distinct argument types Ai, and
// a function whose subjects xi have type U[Ai] and whose switch has only
// default. With noSwitch, the function returns without a switch (the
// control).
func genericDefaultSwitch(n, k int, noSwitch bool) string {
	var b strings.Builder
	b.WriteString("package main\n")
	for j := range k {
		fmt.Fprintf(&b, "type V%d[T any] struct{}\n", j)
	}
	b.WriteString("type U[T any] interface{")
	for j := range k {
		if j > 0 {
			b.WriteString(" | ")
		}
		fmt.Fprintf(&b, "V%d[T]", j)
	}
	b.WriteString("}\n")
	for i := range n {
		fmt.Fprintf(&b, "type A%d struct{}\n", i)
	}
	b.WriteString("func F(")
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "x%d U[A%d]", i, i)
	}
	b.WriteString(") int {\n")
	if !noSwitch {
		b.WriteString("\tswitch ")
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
			fmt.Fprintf(&b, "x%d.(type)", i)
		}
		b.WriteString(" {\n\tdefault:\n\t\treturn 0\n\t}\n")
	}
	b.WriteString("\treturn 0\n}\n")
	return check.FromOld(b.String())
}

// TestGenericDefaultSwitchLinear: a default-only switch over n subjects of
// distinct instantiations of a k-variant generic union creates no variant
// instance: it reads the origin's union shape, and substitutes a variant
// only when a case or a binding needs it. The generic instances the whole
// check creates equal the no-switch control's, so they grow as n + k, not
// n × k (round-6 M5; D16: nothing before the counters is superlinear).
// Round 6 created n × k variant instances here. Counted, not timed.
func TestGenericDefaultSwitchLinear(t *testing.T) {
	counts := map[int]int{}
	for _, n := range []int{60, 240} {
		with, d := check.InstancesMade(genericDefaultSwitch(n, n, false))
		if len(d) > 0 {
			t.Fatalf("n=k=%d: diagnostics %s", n, describe(d))
		}
		without, d := check.InstancesMade(genericDefaultSwitch(n, n, true))
		if len(d) > 0 {
			t.Fatalf("n=k=%d control: diagnostics %s", n, describe(d))
		}
		ops, _ := check.VariantOps(genericDefaultSwitch(n, n, false))
		ts, us, _ := check.SolverSteps(genericDefaultSwitch(n, n, false))
		t.Logf("n=k=%d: %d instances with the switch, %d without; %d variant operations; steps %v/%v", n, with, without, ops, ts, us)
		if with != without {
			t.Errorf("n=k=%d: the default-only switch created %d instances beyond the control", n, with-without)
		}
		if with > 4*(n+n) || ops != 0 || ts[0] != 0 || us[0] != 0 {
			t.Errorf("n=k=%d: %d instances (want at most %d), %d variant operations, steps %v/%v (want 0)", n, with, 4*(n+n), ops, ts, us)
		}
		counts[n] = with
	}
	if ratio := float64(counts[240]) / float64(counts[60]); ratio > 4.5 {
		t.Errorf("4x n and k create %.2fx the instances; the check is superlinear", ratio)
	}
}
