package check_test

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// The round-3 review found its classes again on wider inputs
// (round-3-findings.md). These tests cover the wider domains; the go/types
// differential test (differential_test.go) covers M1 and M4.

// ---------------------------------------------------------------- M2: order-invariant totality

// matrixSwitch writes a switch over len(sizes) subjects whose case rows are
// rows (-1 is _), with the subjects written in the order order: written
// position k holds subject order[k]. Subject j's union has sizes[j]
// variants, of the struct types V0, V1, ….
func matrixSwitch(sizes []int, rows [][]int, order []int) string {
	var b strings.Builder
	b.WriteString("package main\n")
	maxSize := 0
	for _, s := range sizes {
		maxSize = max(maxSize, s)
	}
	for v := range maxSize {
		fmt.Fprintf(&b, "type V%d struct{}\n", v)
	}
	for s := 1; s <= maxSize; s++ {
		fmt.Fprintf(&b, "type U%d interface{", s)
		for v := range s {
			if v > 0 {
				b.WriteString(" | ")
			}
			fmt.Fprintf(&b, "V%d", v)
		}
		b.WriteString("}\n")
	}
	b.WriteString("func F(")
	for k, j := range order {
		if k > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "x%d U%d", j, sizes[j])
	}
	b.WriteString(") int {\n\tswitch ")
	for k := range order {
		if k > 0 {
			b.WriteString(", ")
		}
		b.WriteString("_")
	}
	b.WriteString(" := ")
	for k, j := range order {
		if k > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "x%d.(type)", j)
	}
	b.WriteString(" {\n")
	for _, r := range rows {
		cells := make([]string, len(order))
		for k, j := range order {
			cells[k] = "_"
			if r[j] >= 0 {
				cells[k] = fmt.Sprintf("V%d", r[j])
			}
		}
		if len(order) == 1 {
			fmt.Fprintf(&b, "\tcase %s:\n\t\treturn 1\n", cells[0])
		} else {
			fmt.Fprintf(&b, "\tcase (%s):\n\t\treturn 1\n", strings.Join(cells, ", "))
		}
	}
	b.WriteString("\t}\n\treturn 0\n}\n")
	return b.String()
}

// verdict classifies a switch's totality outcome.
func verdict(diags []check.Diagnostic) string {
	for _, d := range diags {
		if d.Code == "S103" {
			switch {
			case strings.Contains(d.Message, "too many cases to prove this switch total"):
				return "refused"
			case strings.Contains(d.Message, "reachable"):
				return "refused-reachability"
			}
			return "not total"
		}
	}
	return "total"
}

func identityOrder(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// TestTotalityOrderInvariance: random case matrices, their columns
// permuted (the subjects written in another order) and their rows
// shuffled. A column permutation leaves the totality steps, the
// reachability steps and every diagnostic code unchanged; a row shuffle
// leaves the totality steps and verdict unchanged (reachability follows
// source order by definition). Matrices range from 2 to 10 subjects of 2
// or 3 variants, and random 3-CNF matrices near the budget (round-3 M2).
func TestTotalityOrderInvariance(t *testing.T) {
	rng := rand.New(rand.NewSource(20261027))
	type gen struct {
		sizes []int
		rows  [][]int
	}
	var gens []gen
	for trial := 0; trial < 300; trial++ {
		n := 2 + rng.Intn(9)
		sizes := make([]int, n)
		for j := range sizes {
			sizes[j] = 2 + rng.Intn(2)
		}
		m := 1 + rng.Intn(40)
		rows := make([][]int, m)
		for i := range rows {
			r := make([]int, n)
			for j := range r {
				r[j] = -1
				if rng.Intn(3) > 0 {
					r[j] = rng.Intn(sizes[j])
				}
			}
			rows[i] = r
		}
		gens = append(gens, gen{sizes, rows})
	}
	// 3-CNF at the satisfiability threshold, at the budget's edge: the
	// reachability checks of these exhaust it.
	for _, n := range []int{24, 40, 72} {
		for k := 0; k < 1; k++ {
			sizes := make([]int, n)
			for j := range sizes {
				sizes[j] = 2
			}
			var rows [][]int
			for i := 0; i < n*43/10; i++ {
				r := make([]int, n)
				for j := range r {
					r[j] = -1
				}
				for _, j := range rng.Perm(n)[:3] {
					r[j] = rng.Intn(2)
				}
				rows = append(rows, r)
			}
			gens = append(gens, gen{sizes, rows})
		}
	}
	edge := 0
	for gi, g := range gens {
		n := len(g.sizes)
		base := matrixSwitch(g.sizes, g.rows, identityOrder(n))
		tsteps, usteps, diags := check.SolverSteps(base)
		want := codes(diags)
		if tsteps[0] > check.TotalityBudget/2 || usteps[0] > check.TotalityBudget {
			edge++
		}
		perms := 3
		if n > 20 {
			perms = 1
		}
		for p := 0; p < perms; p++ {
			order := rng.Perm(n)
			src := matrixSwitch(g.sizes, g.rows, order)
			ts, us, d := check.SolverSteps(src)
			if ts[0] != tsteps[0] || us[0] != usteps[0] || codes(d) != want || verdict(d) != verdict(diags) {
				t.Fatalf("matrix %d, columns %v: totality steps %d vs %d, reachability steps %d vs %d, codes %q vs %q\n%s", gi, order, ts[0], tsteps[0], us[0], usteps[0], codes(d), want, src)
			}
			shuffled := append([][]int(nil), g.rows...)
			rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
			src = matrixSwitch(g.sizes, shuffled, order)
			ts, _, d = check.SolverSteps(src)
			// Reachability follows the cases' order, so a shuffle may
			// change which case is unreachable or whether reachability is
			// proved; the totality proof never changes.
			sameVerdict := verdict(d) == verdict(diags) || verdict(d) == "refused-reachability" || verdict(diags) == "refused-reachability"
			if ts[0] != tsteps[0] || !sameVerdict {
				t.Fatalf("matrix %d, rows shuffled, columns %v: totality steps %d vs %d, verdict %s vs %s\n%s", gi, order, ts[0], tsteps[0], verdict(d), verdict(diags), src)
			}
		}
	}
	if edge == 0 {
		t.Errorf("no generated matrix came near the budget; the edge is untested")
	}
}

func codes(diags []check.Diagnostic) string {
	var out []string
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return strings.Join(out, " ")
}

// TestTotalitySavedPermutation: the reviewer's 39-subject, 147-case pair,
// one the other with its columns permuted, was refused in one order and
// accepted in the other. Both are total (an independent DPLL proves it);
// both orders now take the same steps and get the same verdict.
func TestTotalitySavedPermutation(t *testing.T) {
	var steps []int
	var verdicts []string
	for _, name := range []string{"permutation-original.ef", "permutation-reordered.ef"} {
		data, err := os.ReadFile(filepath.Join("testdata", "totality", name))
		if err != nil {
			t.Fatal(err)
		}
		s, d := check.TotalitySteps(string(data))
		if len(s) != 1 {
			t.Fatalf("%s: steps %v", name, s)
		}
		steps = append(steps, s[0])
		verdicts = append(verdicts, verdict(d))
		if v := verdict(d); v != "total" && v != "refused" {
			t.Errorf("%s: a total switch got %s: %s", name, v, describe(d))
		}
	}
	if steps[0] != steps[1] || verdicts[0] != verdicts[1] {
		t.Errorf("column order changed the proof: steps %v, verdicts %v", steps, verdicts)
	}
}

// ---------------------------------------------------------------- M5: linear body graph

// TestBodyGraphScale: the body graph has one edge per referenced name,
// never one per same-named method, so its edge count is linear in the
// source; it is counted, not timed (round-3 M5). Initialization cycles
// through methods are still found, one per cycle, at any scale.
func TestBodyGraphScale(t *testing.T) {
	for _, n := range []int{10, 500, 2000} {
		var b strings.Builder
		b.WriteString("package main\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "type R%d struct{}\nfunc (R%d) M() { R%d{}.M() }\n", i, i, i)
		}
		edges, diags := check.BodyGraphEdges(b.String())
		if len(diags) != 0 || edges < 0 || edges > n {
			t.Errorf("%d self-calling methods: %d edges (want at most %d), %s", n, edges, n, describe(diags))
		}
		b.Reset()
		b.WriteString("package main\ntype C interface{ M() }\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "type R%d struct{}\nfunc (R%d) M() { f%d() }\nfunc f%d() { R%d{}.M(); f%d() }\n", i, i, i, i, i, (i+1)%n)
		}
		edges, diags = check.BodyGraphEdges(b.String())
		if len(diags) != 0 || edges < 0 || edges > 2*n {
			t.Errorf("%d methods and a ring of functions: %d edges (want at most %d), %s", n, edges, 2*n, describe(diags))
		}
		b.Reset()
		b.WriteString("package main\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "type R%d struct{}\nvar v%d = R%d{}.M\nfunc (R%d) M() { _ = v%d }\n", i, i, i, i, i)
		}
		edges, diags = check.BodyGraphEdges(b.String())
		cycles := 0
		for _, d := range diags {
			if d.Code == "S108" && strings.Contains(d.Message, "initialization cycle") {
				cycles++
			} else {
				t.Fatalf("%d var-method cycles: unexpected %s", n, d)
			}
		}
		if cycles != n || edges < 0 || edges > 2*n {
			t.Errorf("%d var-method cycles: %d cycles reported, %d edges (want %d and at most %d)", n, cycles, edges, n, 2*n)
		}
	}
}

// ---------------------------------------------------------------- M3: kinds from methods

// TestKindsFromMethods: a bracket parameter's kind follows its positions in
// the declaration's body and in its methods' signatures, which bind the
// receiver's parameters by position (D6; round-3 M3).
func TestKindsFromMethods(t *testing.T) {
	cases := []struct{ name, want, src string }{
		{"method-error-row", "", `type Job[E] struct{}
func (Job[E]) Run() error[E] {}`},
		{"method-capability-row", "", `type Job[R] struct{}
func (Job[R]) Run() needs R {}`},
		{"method-renamed-receiver", "", `type Job[E] struct{}
func (Job[F]) Run() error[F] {}`},
		{"pointer-receiver", "", `type Job[E] struct{}
func (j *Job[E]) Run() error[E] {}`},
		{"method-and-body-agree", "", `type Job[R] struct{ f func() needs R }
func (Job[R]) Run() needs R {}`},
		{"recursive-head-through-method", "", `type Job[E] struct{ next []Job[E] }
func (j Job[E]) Next() Job[E] { return j }
func (Job[E]) Run() error[E] {}`},
		{"mutual-heads-through-method", "", `type A[E] struct{ b []B[E] }
type B[E] struct{ a []A[E] }
func (B[E]) Run() error[E] {}`},
		{"method-parameter-type", "", `type Box[T] struct{}
func (Box[T]) Put(x T) {}`},
		{"two-row-method", "", `type A[E, F] struct{}
func (A[E, F]) Run() error[E | F] {}`},
		{"body-type-method-row", "S108", `type Job[E] struct{ v E }
func (Job[E]) Run() error[E] {}`},
		{"explicit-error", "", `type Job[E error] struct{}
func (Job[E]) Run() error[E] {}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := check.CheckSource("package main\n" + tc.src + "\n")
			if got := codes(diags); got != tc.want {
				t.Errorf("want %q, got %s", tc.want, describe(diags))
			}
		})
	}
}

// ---------------------------------------------------------------- N1: invalid union

// TestInvalidUnionCases: a union that lost a variant to a reported error
// adds no case diagnostic, in any subject position (D3; round-3 N1).
func TestInvalidUnionCases(t *testing.T) {
	for _, sw := range []string{
		`switch c.(type) { case A: return A{} }`,
		`switch c.(type) { case B: return A{} }`,
		`switch _, _ := c.(type), d.(type) { case (A, A): return A{} }`,
		`switch _, _ := d.(type), c.(type) { case (A, A): return A{} }`,
		`switch _, _ := d.(type), c.(type) { case (A, B): return A{} }`,
	} {
		src := "package main\ntype A struct{}\ntype B struct{}\ntype Choice interface{ Missing }\ntype Full interface{ A | B }\nfunc F(c Choice, d Full) A {" + sw + "}\n"
		_, diags := check.CheckSource(src)
		if len(diags) != 1 || diags[0].Code != "S104" {
			t.Errorf("%s: want only the unknown-name S104, got %s", sw, describe(diags))
		}
	}
}
