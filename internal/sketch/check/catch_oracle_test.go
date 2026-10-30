package check_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// The catch oracle of the stage C round 2 review (its test-strength
// finding), adopted: cumulative catch coverage over a row of two error
// types (rows.md, Catch 2: one matrix per error type), with alternatives
// inside an arm, a `case _` rest arm, and both a total catch and a partial
// `try … catch`. The oracle tracks the four values (E or F, B true or
// false) directly. A partial catch that handles the whole row is a
// redundant try, S108 at the try (review O3). It checks a fixed set of
// 12,262 programs and fails rather than run past its time bound.
func TestCatchOracleTwoTypes(t *testing.T) {
	const bound = 60 * time.Second
	start := time.Now()
	type pat struct {
		text string
		ty   int // 0 E, 1 F, -1 rest
		b    int // 0 false, 1 true, 2 any
	}
	var pool []pat
	for ty, name := range []string{"E", "F"} {
		pool = append(pool,
			pat{name + "{B: true}", ty, 1},
			pat{name + "{B: false}", ty, 0},
			pat{name + "{B: _}", ty, 2},
			pat{name, ty, 2})
	}
	rest := pat{"_", -1, 2}
	type arm []pat
	var singles, all []arm
	for _, p := range pool {
		singles = append(singles, arm{p})
	}
	singles = append(singles, arm{rest})
	all = append(all, singles...)
	for _, p := range pool {
		for _, q := range pool {
			all = append(all, arm{p, q})
		}
	}
	run := func(t *testing.T, arms []arm, partial bool) {
		t.Helper()
		var b strings.Builder
		b.WriteString("type E { B bool }\ntype F { B bool }\nfunc g() int fails E | F { 1 }\n")
		if partial {
			b.WriteString("func f() int fails E | F {\n\ttry g() catch {\n")
		} else {
			b.WriteString("func f() int {\n\tg() catch {\n")
		}
		const first = 6
		for i, a := range arms {
			var ps []string
			for _, p := range a {
				ps = append(ps, p.text)
			}
			fmt.Fprintf(&b, "\tcase %s: %d\n", strings.Join(ps, ", "), i)
		}
		b.WriteString("\t}\n}\n")
		var covered [2][2]bool
		rested := false
		var want []string
		for i, a := range arms {
			for _, p := range a {
				if rested {
					want = append(want, fmt.Sprintf("S108:%d", first+i))
					continue
				}
				if p.ty < 0 {
					all := covered[0][0] && covered[0][1] && covered[1][0] && covered[1][1]
					if all {
						want = append(want, fmt.Sprintf("S108:%d", first+i))
					}
					rested = true
					continue
				}
				useful := false
				for v := 0; v < 2; v++ {
					if (p.b == 2 || p.b == v) && !covered[p.ty][v] {
						useful = true
					}
				}
				if !useful {
					want = append(want, fmt.Sprintf("S108:%d", first+i))
					continue
				}
				for v := 0; v < 2; v++ {
					if p.b == 2 || p.b == v {
						covered[p.ty][v] = true
					}
				}
			}
		}
		total := rested || (covered[0][0] && covered[0][1] && covered[1][0] && covered[1][1])
		switch {
		case !partial && !total:
			want = append(want, "S103:5")
		case partial && total:
			want = append(want, "S108:5")
		}
		sort.Strings(want)
		got, d := codeLines(b.String())
		if got != strings.Join(want, " ") {
			t.Errorf("%s\nwant [%s], got [%s] %v", b.String(), strings.Join(want, " "), got, d)
		}
	}
	n := 0
	check := func(arms []arm, partial bool) {
		if time.Since(start) > bound {
			t.Fatalf("over the time bound %v after %d programs", bound, n)
		}
		run(t, arms, partial)
		n++
	}
	for _, partial := range []bool{false, true} {
		for _, a := range all {
			check([]arm{a}, partial)
			for _, c := range all {
				check([]arm{a, c}, partial)
			}
		}
		for _, a := range singles {
			for _, c := range singles {
				for _, d := range singles {
					check([]arm{a, c, d}, partial)
				}
			}
		}
		if t.Failed() {
			break
		}
	}
	if n != 12262 {
		t.Fatalf("checked %d programs, want 12262", n)
	}
}
