package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// TestSelfCallForms (round 7 review O1 and O2): the 196m forms types.md
// promises.
//
//   - O1: x.f[T](…) is a call of f as x.f(…) is, with T written: it checks
//     as the inferred call does, its type arguments are checked, and the
//     edge to f comes from the call (a row f infers reaches the caller).
//     x.f[T] that is not called is still reached only by a call.
//   - O2: a field and a function on the type may share a name. x.Value is
//     the field, and the function stays in the type's operation set, so it
//     provides an interface method Value.
func TestSelfCallForms(t *testing.T) {
	const box = "type Box { n int }\nfunc same[T](b Box, x T) T { return x }\n"
	const counter = "type Counter { Value int }\nfunc Value(c Counter) int { return c.Value + 1 }\n"
	for _, tc := range []struct {
		name, src string
		want      []string // code and line of each diagnostic
	}{
		{"explicit type argument", box + "func f(b Box) int { return b.same[int](1) }\nfunc main() {}\n", nil},
		{"inferred type argument", box + "func f(b Box) int { return b.same(1) }\nfunc main() {}\n", nil},
		{"explicit type argument checked", box + "func f(b Box) int { return b.same[string](1) }\nfunc main() {}\n", []string{"S108 3", "S108 3"}},
		{"too many type arguments", box + "func f(b Box) int { return b.same[int, int](1) }\nfunc main() {}\n", []string{"S108 3"}},
		{"explicit argument on a generic owner", "type G[T] { v T }\nfunc pick[T, U](g G[T], u U) U { return u }\nfunc f(g G[int]) string { return g.pick[string](\"x\") }\nfunc main() {}\n", nil},
		{"explicit argument not called", box + "func f(b Box) { let g = b.same[int]; _ = g }\nfunc main() {}\n", []string{"S104 3"}},
		{"a row reaches the caller through an explicit call", "type Box { n int }\ntype E {}\nfunc load() int fails E { return 1 }\nfunc same[T](b Box, x T) T { _ = try load(); return x }\nfunc f(b Box) int fails E { return try b.same[int](1) }\nfunc main() {}\n", nil},
		{"a row an explicit call brings is declared", "type Box { n int }\ntype E {}\nfunc load() int fails E { return 1 }\nfunc same[T](b Box, x T) T { _ = try load(); return x }\nfunc F(b Box) int { return try b.same[int](1) }\nfunc main() {}\n", []string{"S101 5"}},
		{"a function beside a field provides an interface method", counter + "type Read { Value() int }\nfunc use(r Read) int { return r.Value() }\nfunc f() int { return use(Counter{Value: 1}) }\nfunc main() {}\n", nil},
		{"the field comes first", counter + "func f(c Counter) int { return c.Value }\nfunc main() {}\n", nil},
		{"the field comes first in a call", counter + "func f(c Counter) int { return c.Value() }\nfunc main() {}\n", []string{"S108 3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := check.CheckSource(tc.src)
			var got []string
			for _, d := range diags {
				got = append(got, fmt.Sprintf("%s %d", d.Code, d.Line))
			}
			if strings.Join(got, ", ") != strings.Join(tc.want, ", ") {
				t.Errorf("want %v; got %v", tc.want, diags)
			}
			if p := check.RefWalkProblems(tc.src); len(p) > 0 {
				t.Errorf("references: %s", strings.Join(p, "; "))
			}
		})
	}
}
