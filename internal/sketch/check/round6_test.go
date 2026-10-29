package check

import (
	"strings"
	"testing"
)

// TestInferenceUnions: Ergo's departure from Go's unifier. A variant is
// assignable to its union (types.md, Unions), so a named union parameter
// unifies with the variant its argument names, and the variant's type
// arguments bind; a conflicting later argument is S108.
func TestInferenceUnions(t *testing.T) {
	const decls = `type Some[T] { V T }

type None {}

type Opt[T] = Some[T] | None

func Get[T](o Opt[T], d T) T { return d }

func Pair[T](o Opt[T], p Opt[T]) {}
`
	cases := []struct {
		name, body string
		accept     bool
	}{
		{"variant-binds", `func G() int { return Get(Some[int]{V: 1}, 2) }`, true},
		{"plain-variant", `func G() int { return Get(None{}, 2) }`, true},
		{"variant-then-conflict", `func G(s string) string { return Get(Some[int]{V: 1}, s) }`, false},
		{"two-variants-agree", `func G() { Pair(Some[int]{V: 1}, Some[int]{V: 2}) }`, true},
		{"two-variants-conflict", `func G() { Pair(Some[int]{V: 1}, Some[string]{V: "a"}) }`, false},
		{"not-a-variant", `func G() int { return Get(Some2{}, 2) }
type Some2 {}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := decls + "\n" + tc.body + "\n"
			_, diags := CheckSource(src)
			var ds []string
			for _, d := range diags {
				ds = append(ds, d.String())
			}
			if (len(ds) == 0) != tc.accept {
				t.Fatalf("accept = %v, want %v: %q", len(ds) == 0, tc.accept, ds)
			}
		})
	}
}

// TestInferenceInterfaces: value-argument inference follows go/types'
// unifier (round-5 M1, M3). An interface parameter infers through the
// argument's method set: a wider interface, a named generic or plain
// interface, a concrete type or a pointer with the method. A typed argument
// that conflicts with what an earlier argument inferred is S108 in either
// order, unless every type argument is written. go/types is the oracle for
// each verdict, on the Go text; the checker reads the Ergo text. The
// decided language has no anonymous interfaces (#196), so the cases that
// tested one are gone and the others name their interfaces.
func TestInferenceInterfaces(t *testing.T) {
	const decls = `type S struct{}

func (S) M() int { return 1 }

type P struct{}

func (*P) M() int { return 1 }

type I[T any] interface{ M() T }

type J interface{ M() int }

type K interface{ M() int }

type W interface {
	M() int
	N()
}

type E interface{ M() }

type F interface{ M() }

type EN interface {
	M()
	N()
}

type A[T any] interface{ M() T }

type R struct{}

func (R) M() {}

func FnI[T any](x A[T]) T { return x.M() }

func FnN[T any](x I[T]) T { return x.M() }

func Two[T any](a T, b T) T { return a }

func FnS[T any](a []T, b T) {}
`
	// ergoDecls are decls in the decided syntax (a method is a function
	// whose first parameter is the receiver, 196m).
	const ergoDecls = `type S {}

func M(_ S) int { return 1 }

type P {}

func M(_ *P) int { return 1 }

type I[T] { M() T }

type J { M() int }

type K { M() int }

type W {
	M() int,
	N(),
}

type E { M() }

type F { M() }

type EN {
	M(),
	N(),
}

type A[T] { M() T }

type R {}

func M(_ R) {}

func FnI[T](x A[T]) T { return x.M() }

func FnN[T](x I[T]) T { return x.M() }

func Two[T](a T, b T) T { return a }

func FnS[T](a []T, b T) {}
`
	cases := []struct {
		name, body string
		accept     bool
	}{
		{"wider-named", `func G(x W) int { return FnI(x) }`, true},
		{"named-plain", `func G(x J) int { return FnI(x) }`, true},
		{"concrete", `func G() int { return FnI(S{}) }`, true},
		{"pointer-receiver", `func G(p *P) int { return FnI(p) }`, true},
		{"named-generic-concrete", `func G() int { return FnN(S{}) }`, true},
		{"named-generic-interface", `func G(x W) int { return FnN(x) }`, true},
		{"no-method", `func G() int { return FnI(R{}) }`, false},
		{"conflict-interface-then-struct", `func G(x E, y R) { _ = Two(x, y) }`, false},
		{"conflict-struct-then-interface", `func G(x E, y R) { _ = Two(y, x) }`, false},
		{"explicit-interface-control", `func G(x E, y R) { _ = Two[E](x, y) }`, true},
		{"explicit-interface-control-reversed", `func G(x E, y R) { _ = Two[E](y, x) }`, true},
		{"two-named-interfaces", `func G(x E, y F) { _ = Two(x, y) }`, false},
		{"narrower-and-wider-interfaces", `func G(x E, y EN) { _ = Two(x, y) }`, false},
		{"same-named-twice", `func G(x J, y J) { _ = Two(x, y) }`, true},
		{"slice-conflict", `func G(a []int, b string) { FnS(a, b) }`, false},
		{"slice-match", `func G(a []int, b int) { FnS(a, b) }`, true},
		{"distinct-named-same-method-set", `func G(x J, y K) { _ = Two(x, y) }`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newTwin("package p\n\n"+decls+"\n"+tc.body+"\n", ergoDecls+"\n"+tc.body+"\n")
			g := goErrors(src.goSrc)
			if (len(g) == 0) != tc.accept {
				t.Fatalf("go/types disagrees with the case (accept = %v): %q", tc.accept, g)
			}
			_, diags := CheckSource(src.ergoSrc)
			var ds []string
			for _, d := range diags {
				ds = append(ds, d.String())
			}
			if (len(ds) == 0) != tc.accept {
				t.Fatalf("ergo accept = %v, want %v: %q (go: %q)", len(ds) == 0, tc.accept, ds, g)
			}
			if !tc.accept && !mentions(ds, "S108") {
				t.Fatalf("want S108, got %q", ds)
			}
			if tc.accept {
				if d := diffOne(src); len(d) > 0 {
					t.Fatalf("Info differs from go/types:\n%s", strings.Join(d, "\n"))
				}
			}
		})
	}
}
