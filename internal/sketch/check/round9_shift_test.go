package check

import (
	"strings"
	"testing"
)

// TestNonConstantShift: a shift by a non-constant count needs a typed left
// operand (types.md, Operators 1; round-8 class A). An untyped constant
// there is S108, its only diagnostic, whether Go would accept the program
// (Id[int8](1 << n)) or reject it (float64(1 << n), Id[int8](256 << n)).
// A typed left operand and a constant shift stay Go's.
func TestNonConstantShift(t *testing.T) {
	const decls = `func Id[T any](x T) T { return x }

func Two[T any](a T, b T) T { return a }
`
	cases := []struct {
		name, body string
		refused    bool
	}{
		{"return", `func F(n uint) int { return 1 << n }`, true},
		{"explicit argument", `func F(n uint) int8 { return Id[int8](1 << n) }`, true},
		{"overflow", `func F(n uint) int8 { return Id[int8](256 << n) }`, true},
		{"negative to unsigned", `func F(n uint) uint8 { return -1 >> n }`, true},
		{"parenthesized", `func F(n uint) int8 { return (128) << n }`, true},
		{"float context", `func F(n uint) float64 { return float64(1 << n) }`, true},
		{"inferred with a typed argument", `func F(n uint, v int8) int8 { return Two(1 << n, v) }`, true},
		{"inferred reversed", `func F(n uint, v int8) int8 { return Two(v, 1 << n) }`, true},
		{"rune", `func F(n uint) int32 { return 'a' << n }`, true},
		{"typed left", `func F(n uint) int8 { return int8(1) << n }`, false},
		{"typed variable", `func F(n uint, x int8) int8 { return x << n }`, false},
		{"constant shift", `func F() int8 { return 1 << 3 }`, false},
		{"constant shift inferred", `func F() int { return Id(1 << 3) }`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\n\n" + decls + "\n" + tc.body + "\n"
			_, diags := CheckSource(src)
			var ds []string
			for _, d := range diags {
				ds = append(ds, d.String())
			}
			if !tc.refused {
				if len(ds) > 0 {
					t.Fatalf("want valid, got %q", ds)
				}
				if d := diffOne(src); len(d) > 0 {
					t.Fatalf("Info differs from go/types:\n%s", strings.Join(d, "\n"))
				}
				return
			}
			if len(ds) != 1 || !strings.Contains(ds[0], "S108 the left operand of a shift by a non-constant count needs a type; write int(") {
				t.Fatalf("want exactly the shift refusal, got %q", ds)
			}
		})
	}
}
