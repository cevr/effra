package check

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ergo.local/prototype/internal/sketch/syntax"
)

// Linear work of the body checks (round 8 review M1). Every check of a
// body costs its size: c.workNodes adds the body's syntax nodes at each
// check, in every pass (the typing from empty rows, settling, the
// initialization discovery, the silent pass of a cycle, converge and the
// reporting pass). The tests bound that work by workFactor times the
// source's syntax nodes, and the worklist's graph steps by workFactor times
// (bodies + edges), from n = 250 to n = 4000. They never bound the time;
// they log it.

const workFactor = 8

// chainSource is the reviewer's star: f0 calls f1…f(n-1) and uses C, and
// each fi calls f(i-1), so every fi is in one component whose capability
// row grows one body at a time.
func chainSource(b *strings.Builder, n int) {
	b.WriteString("type C { Use() }\nfunc main() {}\n")
	chainBodies(b, n, nil)
}

// chainBodies writes the star's bodies, f0 first or in the order given.
func chainBodies(b *strings.Builder, n int, order []int) {
	if order == nil {
		order = make([]int, n)
		for i := range order {
			order[i] = i
		}
	}
	for _, i := range order {
		if i == 0 {
			b.WriteString("func f0() {\n\tC.Use()\n")
			for j := 1; j < n; j++ {
				fmt.Fprintf(b, "\tf%d()\n", j)
			}
			b.WriteString("}\n")
			continue
		}
		fmt.Fprintf(b, "func f%d() { f%d() }\n", i, i-1)
	}
}

// rowTail is the reviewer's row-dependent receiver: m fails E (inferred),
// so a catch of E around a call of a function whose row is inferred types
// its receiver.
const rowTail = "type E {}\ntype B {}\nfunc load() B fails E { return B{} }\nfunc w() B { return try load() }\nfunc m(b B) { _ = try load() }\n"

// kchain: k0 fails E (inferred), and each ki catches E around k(i-1)
// and calls b.m() on the value, so ki's row is inferred too.
func kchain(b *strings.Builder, n int) {
	b.WriteString(rowTail + "func main() {}\nfunc k0() B { return try load() }\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(b, "func k%d() B {\n\tlet b = k%d() catch {\n\tcase E:\n\t\tB{}\n\t}\n\ttry b.m()\n\treturn b\n}\n", i, i-1)
	}
}

type workCase struct {
	name string
	src  func(n int) string
	// diags is the verdict at every n: the codes, in order.
	diags string
}

var workCases = []workCase{
	{"r7: an unknown receiver beside the star", func(n int) string {
		var b strings.Builder
		chainSource(&b, n)
		b.WriteString("type R {}\nfunc m(_ R) {}\nfunc bad() { nope.m() }\n")
		return b.String()
	}, "S104"},
	{"star: a local bound to an unknown name in a caller of the star", func(n int) string {
		var b strings.Builder
		chainSource(&b, n)
		b.WriteString("type R {}\nfunc m(_ R) {}\nfunc bad() { f0(); let x = nope; x.m() }\n")
		return b.String()
	}, "S104"},
	{"star: a receiver that waits for a row in a caller of the star", func(n int) string {
		var b strings.Builder
		chainSource(&b, n)
		b.WriteString(rowTail + `func k() {
	f0()
	let b = w() catch {
	case E:
		B{}
	}
	try b.m()
}
`)
		return b.String()
	}, ""},
	{"star: declared from the middle", func(n int) string {
		var b strings.Builder
		b.WriteString("type C { Use() }\nfunc main() {}\n")
		var order []int
		for i := n / 2; i < n; i++ {
			order = append(order, i)
		}
		for i := 0; i < n/2; i++ {
			order = append(order, i)
		}
		chainBodies(&b, n, order)
		return b.String()
	}, ""},
	{"star: a heavier body declared first reaches the middle", func(n int) string {
		var b strings.Builder
		b.WriteString("type C { Use() }\nfunc main() {}\n")
		fmt.Fprintf(&b, "func h() {\n\tf%d()\n", n/2)
		for i := 0; i <= n; i++ {
			fmt.Fprintf(&b, "\tC.Use()\n")
		}
		b.WriteString("}\n")
		chainBodies(&b, n, nil)
		fmt.Fprintf(&b, "func g() { h() }\n")
		return b.String()
	}, ""},
	{"r7 generator: receivers through locals bound to calls", func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "type R%d {}\nfunc mk%d() R%d { return R%d{} }\nfunc m(_ R%d) {}\nfunc f%d() {\n\tlet x = mk%d()\n\tx.m()\n}\n", i, i, i, i, i, i, i)
		}
		b.WriteString("func main() {}\n")
		return b.String()
	}, ""},
	{"wideWaiting: one wide body over receivers that wait in waves", func(n int) string {
		var b strings.Builder
		kchain(&b, n)
		b.WriteString("func wide() {\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "\tlet b%d = k%d() catch {\n\tcase E:\n\t\tB{}\n\t}\n\ttry b%d.m()\n", i, i, i)
		}
		b.WriteString("}\n")
		return b.String()
	}, ""},
	{"one body whose receivers each wait for the previous call's row", func(n int) string {
		var b strings.Builder
		b.WriteString("type E {}\ntype B {}\nfunc load() B fails E { return B{} }\nfunc main() {}\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "func m%d(b B) B { return try load() }\n", i)
		}
		b.WriteString("func deep() {\n\tlet c0 = load() catch {\n\tcase E:\n\t\tB{}\n\t}\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "\tlet c%d = c%d.m%d() catch {\n\tcase E:\n\t\tB{}\n\t}\n", i, i-1, i)
		}
		fmt.Fprintf(&b, "\t_ = c%d\n}\n", n-1)
		return b.String()
	}, ""},
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

// sourceNodes counts the syntax nodes of src.
func sourceNodes(t *testing.T, src string) int {
	t.Helper()
	file, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	syntax.Inspect(file, func(syntax.Node) bool {
		count++
		return true
	})
	return count
}

func TestSelfCallWorkLinear(t *testing.T) {
	for _, settleOnly := range []bool{false, true} {
		for _, wc := range workCases {
			t.Run(fmt.Sprintf("%s settle-only=%v", wc.name, settleOnly), func(t *testing.T) {
				t.Parallel()
				for _, n := range []int{250, 500, 1000, 2000, 4000} {
					src := wc.src(n)
					nodes := sourceNodes(t, src)
					start := time.Now()
					c, diags := checkSettling(src, settleOnly)
					elapsed := time.Since(start)
					var codes []string
					for _, d := range diags {
						codes = append(codes, d.Code)
					}
					if got := strings.Join(codes, " "); got != wc.diags {
						t.Fatalf("n = %d: want %q; got %v", n, wc.diags, diags)
					}
					graph := c.bodyNodes + c.bodyEdges
					t.Logf("n = %d: %d source nodes, %d bodies, %d edges; work %d nodes (%.2f per source node); %d settling checks, %d worklist steps, %d fixpoint checks; %s",
						n, nodes, c.bodyNodes, c.bodyEdges, c.workNodes, float64(c.workNodes)/float64(nodes), c.selfChecks, c.worklistSteps, c.convergeChecks, elapsed)
					if c.workNodes > workFactor*nodes {
						t.Errorf("n = %d: the body checks visited %d nodes for %d source nodes (bound %d)", n, c.workNodes, nodes, workFactor*nodes)
					}
					if c.worklistSteps > workFactor*graph {
						t.Errorf("n = %d: %d worklist steps for %d bodies and %d edges (bound %d)", n, c.worklistSteps, c.bodyNodes, c.bodyEdges, workFactor*graph)
					}
				}
			})
		}
	}
}

// gwaitSource is a call that typing from empty rows cannot resolve: the
// catch arm `case G as g` takes G's instance from w's row, which is empty
// until w settles, so g.m() waits for the worklist.
const gwaitSource = `type G[T] {}
func m[T](g G[T]) {}
func load() fails G[int] { return G[int]{} }
func w() { try load() }
func k() {
	w() catch {
	case G as g:
		g.m()
	}
}
func main() {}
`

// TestSelfCallWaitsForRow: a call whose receiver's type comes from a
// callee's row is resolved to its function before the bodies are ordered,
// by the worklist, and the source is clean in both modes. The receiver of
// the reviewer's catch (`let b = w() catch { case E: B{} }`) does not wait:
// a catch keeps its value's type whatever the row (handlers.go catch), so
// the typing from empty rows resolves b.m().
func TestSelfCallWaitsForRow(t *testing.T) {
	cases := []struct {
		name, src, call, fn string
		waits               bool
	}{
		{"the instance of a generic catch arm", gwaitSource, "g.m", "m", true},
		{"the value of a catch", workCases[2].src(3), "b.m", "m", false},
	}
	for _, tc := range cases {
		for _, settleOnly := range []bool{false, true} {
			var got map[*syntax.SelectorExpr]*Func
			selfTrace = func(_ *checker, _ []*syntax.SelectorExpr, resolved map[*syntax.SelectorExpr]*Func) {
				got = resolved
			}
			c, diags := checkSettling(tc.src, settleOnly)
			selfTrace = nil
			if len(diags) != 0 {
				t.Fatalf("%s, settle-only=%v: %v", tc.name, settleOnly, diags)
			}
			resolved := false
			for x, f := range got {
				if exprText(x) == tc.call && f != nil && f.Name == tc.fn {
					resolved = true
				}
			}
			if !resolved {
				t.Errorf("%s, settle-only=%v: %s() was not resolved before the bodies were ordered: %v", tc.name, settleOnly, tc.call, got)
			}
			if ran := c.selfChecks > 0; ran != (tc.waits || settleOnly) {
				t.Errorf("%s, settle-only=%v: the worklist ran: %v", tc.name, settleOnly, ran)
			}
			if p := RefWalkProblems(tc.src); len(p) > 0 {
				t.Errorf("%s, settle-only=%v: %s", tc.name, settleOnly, strings.Join(p, "; "))
			}
		}
	}
}
