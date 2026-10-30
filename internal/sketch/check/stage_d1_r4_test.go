package check_test

import (
	"strconv"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// Stage D1 round 4: the round 3 review's findings (stage-D1-round-3-
// findings.md), each as the program that showed it. runD1 compares the
// exact diagnostics, as CODE:line.

// M3: a first parameter that names a duplicated type counts as a distinct
// owner only when the restrictions that do not read the declaration hold:
// its type arguments are distinct type parameters of the function, and the
// function is not main. Otherwise the group is the redeclaration it is,
// whichever duplicate is kept.
func TestDuplicatedTypeOwnerKeepsIndependentRestrictions(t *testing.T) {
	cases := []d1Case{
		{"concrete-type-argument", "type Y[T] { N T }\ntype Y[T] { M T }\ntype E { N int }\nfunc f(y Y[int]) {}\nfunc f(e E) {}\n", []string{"S108:2", "S108:5"}},
		{"repeated-type-argument", "type Y[T, U] { N T, M U }\ntype Y[T, U] { P T, Q U }\ntype E { N int }\nfunc f[T](y Y[T, T]) {}\nfunc f(e E) {}\n", []string{"S108:2", "S108:5"}},
		{"main", "type Y { N int }\ntype Y { M int }\ntype Z { N int }\ntype Z { M int }\nfunc main(y Y) {}\nfunc main(z Z) {}\n", nil},
		{"distinct-type-parameters", "type Y[T] { N T }\ntype Y[T] { M T }\ntype E { N int }\nfunc f[T](y Y[T]) {}\nfunc f(e E) {}\n", []string{"S108:2"}},
	}
	runD1(t, cases[:2])
	runD1(t, cases[3:])
	// main is never a member: the second main is a redeclaration, whatever
	// else its parameters draw.
	_, ds := check.CheckSource(cases[2].src)
	if !hasMessage(ds, "main is already declared in this package") {
		t.Errorf("main: want the redeclaration, got %s", describe(ds))
	}
	for _, tc := range cases {
		if p := check.OrderProblems(tc.src, 16); len(p) > 0 {
			t.Errorf("%s: declaration order: %v", tc.name, p)
		}
	}
}

// machineDecls declares a machine type, Breaker, whose value has no fields.
const machineDecls = "type Closed {}\ntype Open {}\ntype State = Closed | Open\ntype Trip {}\ntype Signal = Trip\n" +
	"func step(s State, sig Signal) Decision[State, string] {\n\tmatch s {\n\tcase Closed:\n\t\treturn Enter{Open}\n\tcase Open:\n\t\treturn Done{\"open\"}\n\t}\n}\n" +
	"type Breaker machine[State, Signal] {\n\tinitial: Closed,\n\tstep:    step,\n}\n"

// M4: every route that refuses a literal's element skips its value as an
// orphan, so the callback frames in it are recorded: the reference walk
// agrees, and a let mut local the frame reassigns draws no false S110
// beside the literal's diagnostic. The first six are the review's; the
// first is its fuzz seed (testdata/fuzz/FuzzCheck/3da3f8287b8175fd).
func TestEveryLiteralRefusalRecordsItsFrames(t *testing.T) {
	for i, src := range []string{
		"func e(A)A{A{{{0(){e}}}}}",
		"func e() { int{func() { e() }} }",
		"func e() { int{0() { e() }} }",
		"func e() { Map[int,int]{0() { e() }} }",
		"func e() int { let mut n = 0; int{0() { n = 1 }}; n }",
		"func e() int { let mut n = 0; Map[int,int]{0() { n = 1 }}; n }",
		"type P { X func() }\nfunc e() int { let mut n = 0; _ = P{X: func() {}, X: func() { n = 1 }}; n }",
		machineDecls + "func e() int { let mut n = 0; _ = Breaker{func() { n = 1 }}; n }",
		machineDecls + "func e() { _ = Breaker{0() { e() }} }",
	} {
		_, ds := check.CheckSource(src)
		if len(ds) == 0 {
			t.Errorf("%d: want the literal's diagnostic\n%s", i, src)
		}
		if p := check.RefWalkProblems(src); len(p) > 0 {
			t.Errorf("%d: reference walk: %v\n%s", i, p, src)
		}
		for _, d := range ds {
			if strings.Contains(d.Message, "never reassigned") {
				t.Errorf("%d: false advice from a skipped reassignment: %s\n%s", i, describe(ds), src)
			}
		}
	}
}

// M1 and M2: a refused class G member's error row holds invalid, and rows
// carry it like any element: through local bindings, if joins, generic
// results, inferred functions and row parameters. A check that needs a
// definite row stays silent on a row that holds invalid; a row that does
// not hold it is checked. pinnedDecls refuses a on line 3, so each program
// that only reaches a's unknown row draws S108:3 alone; the second
// diagnostic, where there is one, is independent of a and keeps its text.
// The shapes are the round 3 review's (review_r3_test.go), with the
// reviewer's typed-field, slice and written-result controls, whose closed
// rows make the outer try independent.
func TestUnknownRowFollowsTheRow(t *testing.T) {
	const choose = "func choose[E1, E2](cb func() int fails E1, ignored func() int fails E2) int fails E1 { _ = ignored; try cb() }\n"
	const id = "func Id[T](x T) T { x }\n"
	const try = "try on an expression that cannot fail"
	cases := []struct {
		name, extra, body string
		outer             string
	}{
		{"direct-control", "", "func F() int fails Bang { try (func() int { a(1) })() }", ""},
		{"deferred-control", "", "func F() int { try keep(func() int { a(1) }) }", try},
		{"local-closure", "", "func F() int fails Bang { let g = func() int { a(1) }; try g() }", ""},
		{"local-member", "", "func F() int fails Bang { let g = a; try g(1) }", ""},
		{"value-if", "", "func F(c bool) int fails Bang { try (if c { func() int { a(1) } } else { func() int { 1 } })() }", ""},
		// The outer literal writes its result `func() int`, so the inner
		// literal's row is closed: the outer try is an independent error,
		// as for the written-result control below.
		{"nested-returned-literal", "", "func F() int fails Bang { try (func() func() int { func() int { a(1) } })()() }", try},
		{"generic-identity", id, "func F() int fails Bang { try Id(func() int { a(1) })() }", ""},
		{"inferred-function", "func b() int { a(1) }\n", "func F() int fails Bang { try b() }", ""},
		{"inferred-function-unhandled", "func b() int { a(1) }\n", "func F() int { b() }", ""},
		{"rowparam-ignored-other", choose, "func F() int { try choose(func() int { 1 }, func() int { a(1) }) }", try},
		{"rowparam-ignored-trailing", choose, "func F() int { try choose(func() int { 1 }) { a(1) } }", try},
		{"rowparam-ignored-catch", choose, "func F() int { choose(func() int { 1 }, func() int { a(1) }) catch { case Bang: 2 } }", "catch on an expression that cannot fail"},
		{"rowparam-kept", choose, "func F() int fails Bang { try choose(func() int { a(1) }, func() int { 1 }) }", ""},
		{"nested-deferred", "", "func F() int { try keep(func() int { keep(func() int { a(1) }) }) }", try},
		{"generic-deferred", id, "func F() int { try keep(Id(func() int { a(1) })) }", try},
		{"typed-field", "type Box { Fn func() int }\n", "func F() int { try (Box{Fn: func() int { a(1) }}).Fn() }", try},
		{"slice", "", "func F() int { try ([]func() int{func() int { a(1) }})[0]() }", try},
		{"written-result", "func mk() func() int { func() int { a(1) } }\n", "func F() int { try mk()() }", try},
	}
	var d1 []d1Case
	for _, tc := range cases {
		line := 7 + strings.Count(tc.extra, "\n")
		want := []string{"S108:3"}
		if tc.outer != "" {
			want = append(want, "S108:"+strconv.Itoa(line))
		}
		d1 = append(d1, d1Case{tc.name, pinnedDecls + tc.extra + tc.body + "\n", want})
	}
	runD1(t, d1)
	for i, tc := range cases {
		_, ds := check.CheckSource(d1[i].src)
		if tc.outer != "" && !strings.Contains(describe(ds), tc.outer) {
			t.Errorf("%s: want the independent %q, got %s", tc.name, tc.outer, describe(ds))
		}
		for _, d := range ds {
			if strings.Contains(d.Message, "invalid") {
				t.Errorf("%s: invalid printed: %s", tc.name, describe(ds))
			}
		}
	}
}

func hasMessage(ds []check.Diagnostic, msg string) bool {
	for _, d := range ds {
		if d.Message == msg {
			return true
		}
	}
	return false
}
