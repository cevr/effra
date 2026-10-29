package check

import (
	"strings"
	"testing"
)

// TestNonConstantShift (spec D-A3; types.md, Operators 3): a shift's left
// literal takes the expected type when that is an integer type, else int,
// and a non-integer expected type is S108 naming int(1) << n, its only
// diagnostic. A conversion gives its literal operand no expected type, so
// float64(1 << n) converts an int shift (Go rejects it: the trap is gone).
// A literal still fits its type (S108 on overflow). Every accepted row
// agrees with go/types on its lowering (difflower_test.go).
func TestNonConstantShift(t *testing.T) {
	const decls = `func Id[T any](x T) T { return x }

func Two[T any](a T, b T) T { return a }
`
	const ergoDecls = `func Id[T](x T) T { return x }

func Two[T](a T, b T) T { return a }
`
	const shiftFix = "S108 the left operand of a shift is an integer, and float64 is expected here; write int(1) << n"
	cases := []struct {
		name, body, want string
	}{
		{"return", `func F(n uint) int { return 1 << n }`, ""},
		{"explicit argument", `func F(n uint) int8 { return Id[int8](1 << n) }`, ""},
		{"overflow", `func F(n uint) int8 { return Id[int8](256 << n) }`, "S108 the constant 256 overflows int8"},
		{"negative to unsigned", `func F(n uint) uint8 { return -1 >> n }`, "S108 the constant -1 overflows uint8"},
		{"parenthesized", `func F(n uint) int8 { return (128) << n }`, "S108 the constant 128 overflows int8"},
		{"conversion", `func F(n uint) float64 { return float64(1 << n) }`, ""},
		{"float context", `func F(n uint) float64 { return 1 << n }`, shiftFix},
		{"float context, constant count", `func F() float64 { return 1 << 3 }`, "S108 the left operand of a shift is an integer, and float64 is expected here; write int(1) << 3"},
		{"inferred with a typed argument", `func F(n uint, v int8) int8 { return Two(1 << n, v) }`, ""},
		{"inferred reversed", `func F(n uint, v int8) int8 { return Two(v, 1 << n) }`, ""},
		{"rune", `func F(n uint) int32 { return 'a' << n }`, ""},
		{"typed left", `func F(n uint) int8 { return int8(1) << n }`, ""},
		{"typed variable", `func F(n uint, x int8) int8 { return x << n }`, ""},
		{"constant shift", `func F() int8 { return 1 << 3 }`, ""},
		{"constant shift inferred", `func F() int { return Id(1 << 3) }`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newTwin("package p\n\n"+decls+"\n"+tc.body+"\n", ergoDecls+"\n"+tc.body+"\n")
			_, diags := CheckSource(src.ergoSrc)
			var ds []string
			for _, d := range diags {
				ds = append(ds, d.String())
			}
			if tc.want == "" {
				if len(ds) > 0 {
					t.Fatalf("want valid, got %q", ds)
				}
				if d := diffOne(src); len(d) > 0 {
					t.Fatalf("Info differs from go/types on the lowering:\n%s", strings.Join(d, "\n"))
				}
				return
			}
			if len(ds) != 1 || !strings.Contains(ds[0], tc.want) {
				t.Fatalf("want exactly %q, got %q", tc.want, ds)
			}
		})
	}
}
