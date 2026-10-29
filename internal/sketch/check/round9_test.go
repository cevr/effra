package check

import (
	"strings"
	"testing"
)

// TestContextSites: one row per site where an untyped operand meets a type
// (the list above updateExprType, round-8 class B). Each row is a program
// go/types accepts; the oracle compares Info for every expression node, so
// the untyped operand, its parentheses and its untyped sub-expressions must
// carry the type go/types records at that site, on the checker's
// translation (fromOld). The decided language removed the sites of the any
// type, channels, make, append, copy and delete as built-ins, fixed arrays and a
// switch on a constant against non-literal cases (#196), so their rows are
// gone.
func TestContextSites(t *testing.T) {
	const decls = `type S struct{ X int8 }

func Id[T any](x T) T { return x }

func Take(v int8) {}

func Ret() int8 { return (1 + 2) }
`
	sites := []struct{ site, body string }{
		{"var with a written type", `func F() { var v int8 = (1); _ = v }`},
		{"assignment", `func F() { var v int8; v = -1; _ = v }`},
		{"define with a default type", `func F() { v := 1 + 'a'; _ = v }`},
		{"conversion", `func F() int8 { return int8((1 + 2)) }`},
		{"conversion to a float", `func F() float64 { return float64(1) }`},
		{"conversion of an integer to a string", `func F() string { return string(rune(65)) }`},
		{"conversion inside an inferred argument", `func F() int8 { return Id(int8(1)) }`},
		{"plain call argument", `func F() { Take((1)) }`},
		{"inferred call argument", `func F() int { return Id(-(1)) }`},
		{"explicit call argument", `func F() int8 { return Id[int8](1) }`},
		{"return", `func F() int8 { return -(1) }`},
		{"composite literal element", `func F() []int8 { return []int8{1, (2)} }`},
		{"composite literal field", `func F() S { return S{X: 1} }`},
		{"composite literal key", `func F() map[int8]string { return map[int8]string{1: "a"} }`},
		{"map index", `func F(m map[int8]string) string { return m[(1)] }`},
		{"slice index", `func F(s []int8) int8 { return s[1] }`},
		{"slice expression", `func F(s []int8) []int8 { return s[1:2] }`},
		{"binary with a typed operand", `func F(n int8) int8 { return n + (1) }`},
		{"assignment operation", `func F(n int8) int8 { n += 1; return n }`},
		{"comparison", `func F(n int8) bool { return n == 1 }`},
		{"switch case value", `func F(n int8) int { switch n {
	case 1:
		return 1
	}
	return 0 }`},
		{"len", `func F() int { return len("ab") }`},
		{"min", `func F(n int8) int8 { return min(n, 1) }`},
		{"max of constants", `func F() int8 { return max(1, 2) }`},
		{"range key", `func F(m map[int8]string) { for k := range m { _ = k == 1 } }`},
	}
	for _, s := range sites {
		t.Run(s.site, func(t *testing.T) {
			src := "package p\n\n" + decls + "\n" + s.body + "\n"
			if g := goErrors(src); len(g) > 0 {
				t.Fatalf("go/types rejects the row: %q", g)
			}
			if d := diffOne(src); len(d) > 0 {
				t.Fatalf("%s", strings.Join(d, "\n"))
			}
		})
	}
}
