package check

import (
	"strings"
	"testing"
)

// TestNestedFailure: an argument that is invalid or refused fails its
// enclosing generic call with no new diagnostic, and the call's result is
// invalid (round-8 M5; round 9 class D; D3). Each case states how many root
// causes it has: the reviewer's Id(Apply(Id, 1)) and Id(G(1, s)) have one;
// independent errors in two arguments both report.
func TestNestedFailure(t *testing.T) {
	const decls = `func Id[U any](x U) U { return x }

func Apply[T any](f func(T) T, x T) T { return f(x) }

func Pair[A, B any](a A, b B) A { return a }

type S struct{}

func (S) M(int8) {}

type IM[T any] interface{ M(T) }

func G[T comparable, P IM[T]](v T, p P) T { return v }
`
	const ergoDecls = `func Id[U](x U) U { return x }

func Apply[T](f func(T) T, x T) T { return f(x) }

func Pair[A, B](a A, b B) A { return a }

type S {}

func M(S, int8) {}

type IM[T] { M(T) }

func G[T comparable, P IM[T]](v T, p P) T { return v }
`
	cases := []struct {
		name, body string
		causes     int
	}{
		{"refused generic argument", `func F() int { return Id(Apply(Id, 1)) }`, 1},
		{"refused constraint", `func F(s S) int8 { return Id(G(1, s)) }`, 1},
		{"refused twice nested", `func F() int { return Id(Id(Apply(Id, 1))) }`, 1},
		{"refused in a second call", `func F(n int) int { return Pair(Apply(Id, 1), n) }`, 1},
		{"invalid argument", `func F() int { return Id(missing) }`, 1},
		{"invalid nested argument", `func F() int { return Id(Pair(missing, 1)) }`, 1},
		{"two independent causes", `func F() int { return Pair(Apply(Id, 1), missing) }`, 2},
		{"two refusals", `func F() int { return Pair(Apply(Id, 1), Apply(Id, 2)) }`, 2},
		{"explicit control", `func F() int { return Id(Apply(Id[int], 1)) }`, 0},
		{"constraint control", `func F(s S) int8 { return Id(G[int8](1, s)) }`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newTwin("package p\n\n"+decls+"\n"+tc.body+"\n", ergoDecls+"\n"+tc.body+"\n")
			_, diags := CheckSource(src.ergoSrc)
			var ds []string
			for _, d := range diags {
				ds = append(ds, d.String())
			}
			if len(ds) != tc.causes {
				t.Fatalf("want %d diagnostics, one per root cause, got %q", tc.causes, ds)
			}
			if tc.causes == 0 {
				if d := diffOne(src); len(d) > 0 {
					t.Fatalf("Info differs from go/types:\n%s", strings.Join(d, "\n"))
				}
			}
		})
	}
}
