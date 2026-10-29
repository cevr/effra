package check

import (
	"strings"
	"testing"
)

// TestInferenceSubset: the reviewer's round-6 witnesses under D6 as
// amended in round 7. Inference is sound and a documented subset of Go's:
//   - a parameter only literals bind takes their common default, and
//     mixed defaults are refused (196u rule 6): Two(1, 'a') names no type
//     in either order, though go/types infers int32 (M2); the written
//     Two[int](1, 'a') stays valid;
//   - inference through method constraints (M3) and generic functions as
//     arguments (M4) are outside
//     the subset: go/types accepts each call, Ergo refuses it with the one
//     diagnostic "cannot infer T; write the type arguments, e.g. Fn[int](…)"
//     at the call (D3), and the explicit forms stay valid. The decided
//     language has no channels (#196), so the directed-channel cases (M1)
//     are gone.
func TestInferenceSubset(t *testing.T) {
	const decls = `func Two[T any](a T, b T) T { return a }

func Id[U any](x U) U { return x }

func Apply[T any](f func(T) T, x T) T { return f(x) }

func Fix(f func(int) int) int { return f(1) }

func Pick[A, B any](a A, b B) A { return a }

func Each[A, B any](f func(A, B) A, a A, b B) {}

type S struct{}

func (S) M(int) {}

type I[T any] interface{ M(T) }

type IM[T any] interface{ M(T) }

func Use[T comparable, P IM[T]](x P) {}

func UseN[T comparable, P I[T]](x P) {}
`
	const ergoDecls = `func Two[T](a T, b T) T { return a }

func Id[U](x U) U { return x }

func Apply[T](f func(T) T, x T) T { return f(x) }

func Fix(f func(int) int) int { return f(1) }

func Pick[A, B](a A, b B) A { return a }

func Each[A, B](f func(A, B) A, a A, b B) {}

type S {}

func M(S, int) {}

type I[T] { M(T) }

type IM[T] { M(T) }

func Use[T comparable, P IM[T]](x P) {}

func UseN[T comparable, P I[T]](x P) {}
`
	type verdict int
	const (
		valid verdict = iota
		invalid
		refused
	)
	cases := []struct {
		name, body string
		want       verdict
	}{
		{"literals-int-rune", `func F() int32 { return Two(1, 'a') }`, refused},
		{"literals-rune-int", `func F() int32 { return Two('a', 1) }`, refused},
		{"literals-int-rune-as-int", `func F() int { return Two(1, 'a') }`, invalid},
		{"literals-rune-int-as-int", `func F() int { return Two('a', 1) }`, invalid},
		{"literals-explicit-int", `func F() int { return Two[int](1, 'a') }`, valid},
		{"literals-mismatched-kinds", `func F() { _ = Two(1, "a") }`, invalid},
		{"method-constraint", `func F(x S) { Use(x) }`, refused},
		{"method-constraint-named", `func F(x S) { UseN(x) }`, refused},
		{"method-constraint-explicit", `func F(x S) { Use[int](x) }`, valid},
		{"generic-function-argument", `func F() int { return Apply(Id, 1) }`, refused},
		{"generic-function-to-fixed-parameter", `func F() int { return Fix(Id) }`, refused},
		{"partial-generic-function-argument", `func F() { Each(Pick[int], 1, "a") }`, refused},
		{"generic-function-explicit", `func F() int { return Apply(Id[int], 1) }`, valid},
		{"generic-function-explicit-fixed", `func F() int { return Fix(Id[int]) }`, valid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newTwin("package p\n\n"+decls+"\n"+tc.body+"\n", ergoDecls+"\n"+tc.body+"\n")
			g := goErrors(src.goSrc)
			if goValid := len(g) == 0; goValid != (tc.want != invalid) {
				t.Fatalf("go/types disagrees with the case: %q", g)
			}
			_, diags := CheckSource(src.ergoSrc)
			var ds []string
			for _, d := range diags {
				ds = append(ds, d.String())
			}
			switch tc.want {
			case valid:
				if len(ds) > 0 {
					t.Fatalf("want valid, got %q", ds)
				}
				if d := diffOne(src); len(d) > 0 {
					t.Fatalf("Info differs from go/types:\n%s", strings.Join(d, "\n"))
				}
			case invalid:
				if !mentions(ds, "S108") {
					t.Fatalf("want S108, got %q", ds)
				}
			case refused:
				if len(ds) != 1 || !strings.Contains(ds[0], "S108 cannot infer") || !strings.Contains(ds[0], "write the type arguments, e.g.") {
					t.Fatalf("want exactly the refusal, got %q", ds)
				}
			}
		})
	}
}
