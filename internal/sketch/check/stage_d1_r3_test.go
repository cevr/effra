package check_test

import (
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// Stage D1 round 3: the round 2 review's findings (stage-D1-round-2-
// findings.md), each as the program that showed it. runD1 compares the
// exact diagnostics, as CODE:line, so a cascade or a lost diagnostic fails.

// pinnedDecls declares a, a class G member refused for its pinned rows
// (line 3), two callees that take a callback and never run it (keep,
// store), and one whose error row is its callback's (run).
const pinnedDecls = "type Bang {}\nextern go fixture.mark func mark() fails Bang\n" +
	"func a(n int) int { let m = Map[int, func(int) int]{1: a}; _ = m; try mark(); n }\n" +
	"func keep(cb func() int) int { _ = cb; 1 }\n" +
	"func store(cb func() int) { _ = cb }\n" +
	"func run[E](f func() int fails E) int fails E { try f() }\n"

// M1: creating a function literal or a trailing block runs nothing, so a
// call of the refused member in its body does not pin the row of the call
// that receives it: the absence check on that call is independent and
// stays (the six shapes of the review).
func TestCallbackBodyKeepsTheOuterDiagnostic(t *testing.T) {
	cases := []struct{ name, body, msg string }{
		{"try-literal", "func F() int { try keep(func() int { a(1) }) }\n", "try on an expression that cannot fail"},
		{"catch-literal", "func F() int { keep(func() int { a(1) }) catch { case Bang: 2 } }\n", "catch on an expression that cannot fail"},
		{"return-literal", "func F() { return store(func() int { a(1) }) }\n", "function F returns no value"},
		{"try-trailing", "func F() int { try keep() { a(1) } }\n", "try on an expression that cannot fail"},
		{"catch-trailing", "func F() int { keep() { a(1) } catch { case Bang: 2 } }\n", "catch on an expression that cannot fail"},
		{"return-trailing", "func F() { return store() { a(1) } }\n", "function F returns no value"},
		// The callback's own statements pin only the callback's row.
		{"try-statement-in-literal", "func F() int { try keep(func() int { try a(1) }) }\n", "try on an expression that cannot fail"},
	}
	var d1 []d1Case
	for _, tc := range cases {
		d1 = append(d1, d1Case{tc.name, pinnedDecls + tc.body, []string{"S108:3", "S108:7"}})
	}
	runD1(t, d1)
	for _, tc := range cases {
		_, d := check.CheckSource(pinnedDecls + tc.body)
		found := false
		for _, x := range d {
			found = found || x.Line == 7 && strings.Contains(x.Message, tc.msg)
		}
		if !found {
			t.Errorf("%s: want %q at line 7, got %s", tc.name, tc.msg, describe(d))
		}
	}
}

// M1: a call of the literal runs its body, so the literal keeps the pinned
// row's uncertainty: an immediate call, a try inside the body, a callee
// whose error row is its callback's (run), and a value branch over a call
// add no follow-on to the refused member's S108.
func TestInvokedCallbackKeepsThePinnedRow(t *testing.T) {
	runD1(t, []d1Case{
		{"immediate", pinnedDecls + "func F() int fails Bang { try (func() int { a(1) })() }\n", []string{"S108:3"}},
		{"immediate-double-paren", pinnedDecls + "func F() int fails Bang { try ((func() int { a(1) }))() }\n", []string{"S108:3"}},
		{"immediate-try-in-body", pinnedDecls + "func F() int fails Bang { try (func() int { try a(1) })() }\n", []string{"S108:3"}},
		{"immediate-catch", pinnedDecls + "func F() int { (func() int { a(1) })() catch { case Bang: 2 } }\n", []string{"S108:3"}},
		{"row-parameter-literal", pinnedDecls + "func F() int fails Bang { try run(func() int { a(1) }) }\n", []string{"S108:3"}},
		{"row-parameter-trailing", pinnedDecls + "func F() int fails Bang { try run() { a(1) } }\n", []string{"S108:3"}},
		{"row-parameter-value", pinnedDecls + "func z() int { let m = Map[int, func() int]{1: z}; _ = m; try mark(); 1 }\n" +
			"func F() int fails Bang { try run(z) }\n", []string{"S108:3", "S108:7"}},
		{"nested-under-try", pinnedDecls + "func F() int fails Bang { try keep(func() int { 1 }) + a(1) }\n", []string{"S108:3"}},
		{"value-if", pinnedDecls + "func F(c bool) int fails Bang { try if c { a(1) } else { 2 } }\n", []string{"S108:3"}},
	})
}
