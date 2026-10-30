package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
	"ergo.local/prototype/internal/sketch/syntax"
)

// TestMatchArmsLinear counts all work at the source boundary, including
// pattern construction and irrefutable let proofs. The square matrices can
// exhaust their proof budget; they must stop within a constant per source
// cell and never admit an unproved match. Rect keeps 100 rows as width
// grows; wide grows both dimensions. No host-clock assertion is used.
func TestMatchArmsLinear(t *testing.T) {
	t.Run("union", testUnionArmsLinear)
	for _, shape := range []struct {
		name  string
		sizes []int
		src   func(int) string
	}{
		{"rect", []int{1000, 4000}, func(n int) string { return wideBoolMatch(n, 100) }},
		{"wide", []int{80, 320}, func(n int) string { return wideBoolMatch(n, n) }},
		{"let", []int{8000, 32000}, wideLetPattern},
		{"records", []int{100, 400}, nestedRecordPattern},
		{"unions", []int{15, 60}, nestedUnionPatterns},
	} {
		t.Run(shape.name, func(t *testing.T) {
			for _, n := range shape.sizes {
				src := shape.src(n)
				file, err := syntax.Parse(src)
				if err != nil {
					t.Fatalf("n=%d: parse diagnostics %v", n, err)
				}
				cells := 0
				syntax.Inspect(file, func(syntax.Node) bool { cells++; return true })
				ts, us, d := check.SolverSteps(src)
				if len(ts) != 1 || len(us) != 1 || ts[0]+us[0] == 0 {
					t.Fatalf("n=%d: want one counted proof, got %v/%v; %s", n, ts, us, describe(d))
				}
				for _, x := range d {
					if shape.name != "wide" || x.Code != check.CodeNotTotal || !strings.Contains(x.Message, "too many cases") {
						t.Fatalf("n=%d: unexpected diagnostics %s", n, describe(d))
					}
				}
				work := ts[0] + us[0]
				t.Logf("n=%d source cells=%d steps=%d/%d diagnostics=%s", n, cells, ts[0], us[0], describe(d))
				if work > 512*cells {
					t.Errorf("n=%d: %d steps exceed 512 per source cell (%d)", n, work, 512*cells)
				}
			}
		})
	}
}

func wideBoolMatch(width, rows int) string {
	var b strings.Builder
	b.WriteString("func F(")
	var subjects []string
	for j := range width {
		if j > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "b%d bool", j)
		subjects = append(subjects, fmt.Sprintf("b%d", j))
	}
	fmt.Fprintf(&b, ") int { match (%s) {\n", strings.Join(subjects, ", "))
	for i := range rows {
		var cells []string
		for j := range width {
			p := "true"
			if j == i {
				p = "_"
			}
			cells = append(cells, p)
		}
		fmt.Fprintf(&b, "case (%s): return %d\n", strings.Join(cells, ", "), i)
	}
	b.WriteString("case _: return -1\n} }\n")
	return b.String()
}

func wideLetPattern(n int) string {
	var names, values []string
	for i := range n {
		names = append(names, fmt.Sprintf("a%d", i))
		values = append(values, fmt.Sprint(i))
	}
	return fmt.Sprintf("func F() { let (%s) = (%s)\n_ = (%s)\n}\n", strings.Join(names, ", "), strings.Join(values, ", "), strings.Join(names, ", "))
}

func nestedRecordPattern(n int) string {
	var b strings.Builder
	for i := range n {
		if i == n-1 {
			fmt.Fprintf(&b, "type R%d { On bool }\n", i)
		} else {
			fmt.Fprintf(&b, "type R%d { Child R%d }\n", i, i+1)
		}
	}
	b.WriteString("func F(r R0) int { match r {\ncase ")
	for i := range n - 1 {
		fmt.Fprintf(&b, "R%d{Child: ", i)
	}
	fmt.Fprintf(&b, "R%d{On: true}%s: return 1\ncase _: return 0\n} }\n", n-1, strings.Repeat("}", n-1))
	return b.String()
}

func nestedUnionPatterns(n int) string {
	var b strings.Builder
	for i := range n {
		if i == n-1 {
			fmt.Fprintf(&b, "type U%d = A%d{} | B%d{}\n", i, i, i)
		} else {
			fmt.Fprintf(&b, "type U%d = A%d{Child U%d} | B%d{}\n", i, i, i+1, i)
		}
	}
	b.WriteString("func F(u U0) int { match u {\n")
	for depth := range n {
		b.WriteString("case ")
		for i := range depth {
			fmt.Fprintf(&b, "A%d{Child: ", i)
		}
		fmt.Fprintf(&b, "B%d%s: return %d\n", depth, strings.Repeat("}", depth), depth)
	}
	b.WriteString("case _: return 0\n} }\n")
	return b.String()
}
