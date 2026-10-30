package check_test

import (
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// opsConversionDomain converts every kind of source to every kind of
// operation type (stage C round 5 review M1): a concrete record, a
// pointer to one, an instantiated generic record of one and of two type
// parameters, and pointers to those, each to a plain operation type, an
// instance of a one-parameter one and an instance of a two-parameter one.
// It is a package-level constant so TestCheckCorpusBuilds lowers and
// builds it with the rest of the clean corpus.
const opsConversionDomain = `type J { M() int }
type A[T] { M() T }
type B[T, U] {
	M() T,
	K(u U) U,
}

type S {}
type P {}
type G[T] { V T }
type H[T, U] { V T, W U }

func M(_ S) int { 1 }
func K(_ S, u string) string { u }
func M(_ *P) int { 2 }
func K(_ *P, u string) string { u }
func M[T](g G[T]) T { g.V }
func K[T](g G[T], u string) string { u }
func M[T, U](h H[T, U]) T { h.V }
func K[T, U](h H[T, U], u U) U { h.W }

func useJ(j J) int { j.M() }
func useA(a A[int]) int { a.M() }
func useB(b B[int, string]) string { b.K("k") }

func each(s S, p *P, g G[int], q *G[int], h H[int, string], r *H[int, string]) int {
	useJ(s) + useA(s) + useJ(p) + useA(p) + useJ(g) + useA(g) + useJ(q) + useA(q) + useJ(h) + useA(h) + useJ(r) + useA(r)
}

func both(s S, p *P, g G[int], q *G[int], h H[int, string], r *H[int, string]) string {
	useB(s) + useB(p) + useB(g) + useB(q) + useB(h) + useB(r)
}
`

// The domain checks clean, so the corpus guard must build it.
func TestOpsConversionDomainChecks(t *testing.T) {
	if _, d := check.CheckSource(opsConversionDomain); len(d) != 0 {
		t.Fatalf("want a clean check, got %s", describe(d))
	}
	if gaps := check.InfoGaps(opsConversionDomain); len(gaps) != 0 {
		t.Fatalf("Info gaps: %v", gaps)
	}
}
