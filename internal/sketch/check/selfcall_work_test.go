package check

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ergo.local/prototype/internal/sketch/syntax"
)

// Linear work of the typed self-call edges (round 7 review M1). The
// resolution of the calls x.f(…) that wait for typing checks each waiting
// body once from empty rows, then settles only the bodies the calls still
// waiting reach, with a dependency worklist. The tests bound the counted
// work, never the time: the body checks of the resolution and of the
// reporting pass's fixpoints, and the bodies and edges the worklist visits,
// each at most workFactor·(bodies + edges), from n = 100 to n = 4000.

const workFactor = 4

// chainSource is the reviewer's star: f0 calls f1…f(n-1) and uses C, and
// each fi calls f(i-1), so every fi is in one component whose capability
// row grows one body at a time.
func chainSource(b *strings.Builder, n int) {
	b.WriteString("type C { Use() }\nfunc main() {}\nfunc f0() {\n\tC.Use()\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(b, "\tf%d()\n", i)
	}
	b.WriteString("}\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(b, "func f%d() { f%d() }\n", i, i-1)
	}
}

type workCase struct {
	name string
	src  func(n int) string
	// waiting: the case has a call that typing from empty rows does not
	// resolve, so the worklist runs.
	waiting bool
}

var workCases = []workCase{
	{"an unknown receiver beside a recursive chain", func(n int) string {
		var b strings.Builder
		chainSource(&b, n)
		b.WriteString("type R {}\nfunc m(_ R) {}\nfunc bad() { nope.m() }\n")
		return b.String()
	}, false},
	{"receivers through locals bound to calls", func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "type R%d {}\nfunc mk%d() R%d { return R%d{} }\nfunc m(_ R%d) {}\nfunc f%d() {\n\tlet x = mk%d()\n\tx.m()\n}\n", i, i, i, i, i, i, i)
		}
		b.WriteString("func main() {}\n")
		return b.String()
	}, false},
	// A receiver whose type depends on a callee's row: while w's error row
	// is empty, the catch is on a call that cannot fail and b is invalid.
	// k also reaches the whole chain, so the worklist settles it.
	{"a receiver that waits for a row beside a recursive chain", func(n int) string {
		var b strings.Builder
		chainSource(&b, n)
		b.WriteString(`type E {}
type B {}
func load() B fails E { return B{} }
func w() B { return try load() }
func m(b B) { _ = try load() }
func k() {
	f0()
	let b = w() catch {
	case E:
		B{}
	}
	try b.m()
}
`)
		return b.String()
	}, true},
}

// checkSettling is checkFor with the typing from empty rows skipped when
// settleOnly is set, so the worklist alone resolves the calls.
func checkSettling(src string, settleOnly bool) (*checker, []Diagnostic) {
	file, err := syntax.Parse(src)
	if err != nil {
		return nil, []Diagnostic{{Code: CodeSyntax, Message: err.Error()}}
	}
	c := newChecker("main", Std, prelude().scope)
	c.settleOnly = settleOnly
	c.run([]*syntax.File{file})
	return c, c.sorted()
}

func TestSelfCallWorkLinear(t *testing.T) {
	for _, settleOnly := range []bool{false, true} {
		for _, wc := range workCases {
			t.Run(fmt.Sprintf("%s settle-only=%v", wc.name, settleOnly), func(t *testing.T) {
				t.Parallel()
				for _, n := range []int{100, 500, 1000, 2000, 4000} {
					src := wc.src(n)
					start := time.Now()
					c, diags := checkSettling(src, settleOnly)
					elapsed := time.Since(start)
					if wc.name == workCases[0].name {
						if len(diags) != 1 || diags[0].Code != CodeUnknown {
							t.Fatalf("n = %d: want only the S104 for nope; got %v", n, diags)
						}
					} else if len(diags) != 0 {
						t.Fatalf("n = %d: want no diagnostic; got %v", n, diags)
					}
					size := c.bodyNodes + c.bodyEdges
					checks := c.selfChecks + c.convergeChecks
					t.Logf("n = %d: %d bodies, %d edges; %d resolution checks, %d worklist steps, %d fixpoint checks; %s", n, c.bodyNodes, c.bodyEdges, c.selfChecks, c.selfSteps, c.convergeChecks, elapsed)
					if checks > workFactor*size || c.selfSteps > workFactor*size {
						t.Errorf("n = %d: %d body checks and %d worklist steps for %d bodies and %d edges (bound %d each)", n, checks, c.selfSteps, c.bodyNodes, c.bodyEdges, workFactor*size)
					}
					if wc.waiting && !settleOnly && c.selfSteps == 0 {
						t.Errorf("n = %d: the worklist did not run", n)
					}
				}
			})
		}
	}
}

// TestSelfCallWaitsForRow: the call b.m() of the waiting case is resolved
// to m before the bodies are ordered, by the worklist, and the source is
// clean in both modes.
func TestSelfCallWaitsForRow(t *testing.T) {
	src := workCases[2].src(3)
	for _, settleOnly := range []bool{false, true} {
		var got map[*syntax.SelectorExpr]*Func
		selfTrace = func(_ *checker, _ []*syntax.SelectorExpr, resolved map[*syntax.SelectorExpr]*Func) {
			got = resolved
		}
		c, diags := checkSettling(src, settleOnly)
		selfTrace = nil
		if len(diags) != 0 {
			t.Fatalf("settle-only=%v: %v", settleOnly, diags)
		}
		resolved := false
		for x, f := range got {
			if exprText(x) == "b.m" && f != nil && f.Name == "m" {
				resolved = true
			}
		}
		if !resolved {
			t.Errorf("settle-only=%v: b.m() was not resolved before the bodies were ordered: %v", settleOnly, got)
		}
		if c.selfSteps == 0 {
			t.Errorf("settle-only=%v: the worklist did not run", settleOnly)
		}
		if p := RefWalkProblems(src); len(p) > 0 {
			t.Errorf("settle-only=%v: %s", settleOnly, strings.Join(p, "; "))
		}
	}
}
