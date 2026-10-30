package check_test

import (
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// Stage D1 round 5: the round 4 review's findings (stage-D1-round-4-
// findings.md), each as the program that showed it. runD1 compares the
// exact diagnostics, as CODE:line; wantMessages checks the independent
// ones by text, and that no message prints invalid.

func wantMessages(t *testing.T, name, src string, msgs ...string) {
	t.Helper()
	_, ds := check.CheckSource(src)
	all := describe(ds)
	for _, m := range msgs {
		if !strings.Contains(all, m) {
			t.Errorf("%s: want %q, got %s", name, m, all)
		}
	}
	for _, d := range ds {
		if strings.Contains(d.Message, "invalid") {
			t.Errorf("%s: invalid printed: %s", name, all)
		}
	}
}

// capDecls declares walk, a class G member refused for its capability row
// only (line 5): its error row {Bang} is written, so it is evidence, and a
// caller's catch and try are checked against it.
const capDecls = "type Cap { M() }\ntype Bang {}\ntype Other {}\nextern go fixture.mark func mark() fails Bang\n" +
	"func walk() int fails Bang { let m = Map[int, func() int fails Bang]{1: walk}; _ = m; Cap.M(); try mark(); 1 }\n"

// M1: only an unknown row holds invalid. A member refused for its
// capability row keeps its written error row, and an inferred error row
// whose pinned empty row held stays empty, so the errors a caller makes
// against them are independent and reported. The needs Cap control
// reports the same errors with no refusal; the error-row refusal keeps a
// written capability row.
func TestOnlyTheRefusedRowIsUnknown(t *testing.T) {
	const callers = "func F() int { walk() catch { case Bang: 1; case Other: 2 } }\n" +
		"func G() int { walk() }\n" +
		"func H() int fails Bang { try walk() catch { case Bang: 1 } }\n"
	written := strings.Replace(capDecls, "func walk() int fails Bang {", "func walk() int fails Bang needs Cap {", 1)
	written = strings.Replace(written, "func() int fails Bang]", "func() int fails Bang needs Cap]", 1)
	writtenCallers := strings.NewReplacer("func F() int {", "func F() int needs Cap {", "func G() int {", "func G() int needs Cap {", "func H() int fails Bang {", "func H() int fails Bang needs Cap {").Replace(callers)
	cases := []d1Case{
		// p01: the catch and the try read the written {Bang}.
		{"caponly-written-error-row", capDecls + callers, []string{"S108:5", "S108:6", "S101:7", "S108:8"}},
		// p02: walk's inferred error row did not grow, so {} held.
		{"caponly-held-empty-error-row", "type Cap { M() }\nfunc walk() { let m = Map[int, func()]{1: walk}; _ = m; Cap.M() }\nfunc F() { try walk() }\n", []string{"S108:2", "S108:3"}},
		// p17: the same callers with needs Cap written: no refusal.
		{"needs-written-control", written + writtenCallers, []string{"S108:6", "S101:7", "S108:8"}},
		// An error-row refusal keeps the written capability row: G's
		// pinned empty row does not hold Cap.
		{"errors-only-written-capability-row", "type Cap { M() }\ntype Bang {}\nextern go fixture.mark func mark() fails Bang\n" +
			"func w() needs Cap { let m = Map[int, func() needs Cap]{1: w}; _ = m; Cap.M(); try mark() }\n" +
			"func G() { w() }\n", []string{"S108:4", "S102:5"}},
	}
	runD1(t, cases)
	wantMessages(t, cases[0].name, cases[0].src,
		"main.Other is not in the caught row {main.Bang}",
		"{main.Bang} escapes function G",
		"try over a catch that handles every failure")
	wantMessages(t, cases[1].name, cases[1].src, "try on an expression that cannot fail")
	wantMessages(t, cases[3].name, cases[3].src, "requires main.Cap, which is not in the capability row of function G")
}
