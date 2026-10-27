package check

import (
	"strings"
	"testing"
)

// TestContextualInfo: Info.Types records the final contextual type of an
// untyped expression, as go/types updateExprType does (round-7 M1; D1, D6
// rule 1 as amended in round 8). After assignment, conversion to a
// parameter type, a return or an inferred type argument, the constant and
// its untyped sub-expressions (parentheses, unary and binary operands,
// constant-producing builtin calls) carry the converted type; a comparison
// keeps its operands' own types and a shift converts only its left operand.
// The oracle compares every expression go/types records.
func TestContextualInfo(t *testing.T) {
	const decls = `func Id[T any](x T) T { return x }

func Two[T any](a T, b T) T { return a }

func Take(v int8) {}
`
	for _, body := range []string{
		`func F() bool { return Id(true) }`,
		`func F() int { return Id(1) }`,
		`func F() int32 { return Id(min(1, 'a')) }`,
		`func F() int32 { return Id(max('a', 1)) }`,
		`func F() int { return Id(len("ab")) }`,
		`func F() int { return Id((1)) }`,
		`func F() int { return Id(-(1 + 2)) }`,
		`func F() int32 { return Two(1 + 'a', 2) }`,
		`func F() bool { return Id(1 < 2) }`,
		`func F() bool { return Id(!(1 < 2)) }`,
		`func F() int { return Id(1 << 3) }`,
		`func F() string { return Id("a" + "b") }`,
		`func F() int8 { return Id[int8](1 + 2) }`,
		`func F() int8 { return Two[int8]((1), -2) }`,
		`func F() { Take(1 + 2) }`,
		`func F() { Take(min(1, 2)) }`,
		`func F() int8 { return 1 + (2) }`,
		`func F() { var v int8 = -(1 + 2); _ = v }`,
		`func F() { var v any = 1 + 2; _ = v }`,
		`func F() { v := 1 + 'a'; _ = v }`,
		`func F() { var a [2]int; _ = a }`,
		`func F(n int8) int8 { return n + 1 }`,
		`func F(n int8) bool { return n < 1 + 2 }`,
		`func F() { f := func(v int) int { return v + 1 }; _ = f }`,
		`func Fix(f func(int) int) {}

func F() { Fix(func(v int) int { return v }) }`,
	} {
		src := "package p\n\n" + decls + "\n" + body + "\n"
		if g := goErrors(src); len(g) > 0 {
			t.Fatalf("%s: go/types rejects the case: %q", body, g)
		}
		if d := diffOne(src); len(d) > 0 {
			t.Errorf("%s:\n%s", body, strings.Join(d, "\n"))
		}
	}
}
