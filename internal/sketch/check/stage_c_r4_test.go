package check_test

import (
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// Stage C round 3 review m2: one statement-position rule (rows.md,
// Totality 3). In statement position, a tail that is a pure value (a
// literal, a name, an operator or a conversion, with no call at its top) is
// S110 "this value is not used", in every body kind; a tail that is a call,
// a try, a catch or a with has its result discarded, in every body kind;
// an empty body is the spelling of an arm with no work.
func TestOneStatementPositionRule(t *testing.T) {
	const decls = "let W = wire {}\ntype E {}\ntype F {}\n" +
		"func g() int fails E { 1 }\nfunc two() int fails E | F { 2 }\n" +
		"func i() int { 1 }\nfunc s() string { \"s\" }\nfunc each(f func()) { f() }\n"
	// X is the tail under test; every other tail is a call.
	bodies := []struct {
		name, src string
		empty     bool // the body kind has an empty spelling
	}{
		{"void-function", "func f(b bool, n int) { X }", false},
		{"middle-statement", "func f(b bool, n int) int { X; 0 }", false},
		{"if-arm", "func f(b bool, n int) { if b { X } else { i() } }", true},
		{"if-arm-middle", "func f(b bool, n int) int { if b { i() } else { X }; 0 }", true},
		{"match-arm", "func f(b bool, n int) { match n { case 0: X; case _: i() } }", true},
		{"with", "func f(b bool, n int) { with W { X } }", true},
		{"with-middle", "func f(b bool, n int) int { with W { X }; 0 }", true},
		{"trailing-block", "func f(b bool, n int) { each() { X } }", true},
		{"catch-arm", "func f(b bool, n int) { g() catch { case E: X } }", true},
		{"catch-arm-middle", "func f(b bool, n int) int { g() catch { case E: X }; 0 }", true},
		{"try-catch-arm", "func f(b bool, n int) fails F { try two() catch { case E: X } }", true},
	}
	values := []string{"1", "\"s\"", "n", "n + 1", "int8(n)", "i() + 1", "!b"}
	calls := []string{"i()", "s()", "g() catch { case E: s() }", "with W { i() }"}
	for _, body := range bodies {
		for _, v := range values {
			t.Run(body.name+"/value "+v, func(t *testing.T) {
				_, d := check.CheckSource(decls + strings.Replace(body.src, "X", v, 1) + "\n")
				if len(d) != 1 || d[0].Code != "S110" {
					t.Fatalf("want one S110, got %s", describe(d))
				}
			})
		}
		for _, c := range calls {
			t.Run(body.name+"/call "+c, func(t *testing.T) {
				src := decls + strings.Replace(body.src, "X", c, 1) + "\n"
				if _, d := check.CheckSource(src); len(d) != 0 {
					t.Fatalf("want no diagnostic, got %s", describe(d))
				}
				if gaps := check.InfoGaps(src); len(gaps) != 0 {
					t.Fatalf("Info gaps: %v", gaps)
				}
			})
		}
		if body.empty {
			t.Run(body.name+"/empty", func(t *testing.T) {
				src := decls + strings.Replace(body.src, "X", "", 1) + "\n"
				if _, d := check.CheckSource(src); len(d) != 0 {
					t.Fatalf("want no diagnostic, got %s", describe(d))
				}
			})
		}
	}
}
