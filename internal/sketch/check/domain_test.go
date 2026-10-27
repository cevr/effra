package check_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"effra.local/prototype/internal/sketch/check"
	"effra.local/prototype/internal/sketch/syntax"
)

// The round-2 review found the round-1 classes again on inputs the round-2
// tests did not reach (round-2-findings.md, "Test strength"). These tests
// cover each class's rule domain with generators and tables, not points.

// checkWithin checks src and fails if the checker does not finish in d.
func checkWithin(t *testing.T, src string, d time.Duration) (*check.Package, []check.Diagnostic) {
	t.Helper()
	type result struct {
		pkg   *check.Package
		diags []check.Diagnostic
	}
	done := make(chan result, 1)
	go func() {
		pkg, diags := check.CheckSource(src)
		done <- result{pkg, diags}
	}()
	select {
	case r := <-done:
		return r.pkg, r.diags
	case <-time.After(d):
		t.Fatalf("the checker did not finish within %s", d)
		return nil, nil
	}
}

// ---------------------------------------------------------------- D7 rows

// TestRowGraphDomain: random reference graphs over four kinds of body:
// functions and package vars holding function literals (inferred rows),
// methods (closed rows; two method names over many receivers, so the body
// graph's by-name method edges over-approximate) and exported functions
// (closed rows). A body refers to another by
// a call or by a bare reference (_ = f), and may call C.M itself. The
// oracle is independent of the checker:
//   - the initialization graph is every reference; a var in a cyclic
//     strongly connected component of it is on an initialization cycle:
//     one S108 per such component (go/types initorder), and the var is
//     invalid, so a call through it charges nothing (D3; round-2 M2);
//   - an inferred row holds C exactly when the body calls C.M or calls an
//     inferred body whose row holds C (least fixpoint);
//   - a closed body gets one S102 per such call and per C.M of its own.
//
// Nothing else is reported.
func TestRowGraphDomain(t *testing.T) {
	rng := rand.New(rand.NewSource(1963))
	const (
		kFunc = iota
		kVar
		kMethod
		kExported
	)
	cycles := 0
	for trial := 0; trial < 1500; trial++ {
		n := 2 + rng.Intn(9)
		kind := make([]int, n)
		seed := make([]bool, n)
		type edge struct {
			to   int
			call bool
		}
		edges := make([][]edge, n)
		for i := range n {
			switch r := rng.Intn(8); {
			case r < 3:
				kind[i] = kFunc
			case r < 6:
				kind[i] = kVar
			case r < 7:
				kind[i] = kMethod
			default:
				kind[i] = kExported
			}
			seed[i] = rng.Intn(5) == 0
		}
		for i := range n {
			for j := range n {
				if kind[j] == kExported || rng.Intn(4) != 0 {
					continue
				}
				edges[i] = append(edges[i], edge{to: j, call: rng.Intn(4) != 0})
			}
		}
		name := func(j int) string {
			switch kind[j] {
			case kMethod:
				return fmt.Sprintf("R%d{}.m%d", j, j%2)
			case kExported:
				return fmt.Sprintf("P%d", j)
			}
			return fmt.Sprintf("f%d", j)
		}
		var b strings.Builder
		b.WriteString("package main\ntype C interface{ M() }\n")
		for i := range n {
			if kind[i] == kMethod {
				fmt.Fprintf(&b, "type R%d struct{}\n", i)
			}
		}
		for i := range n {
			switch kind[i] {
			case kFunc:
				fmt.Fprintf(&b, "func f%d() {\n", i)
			case kVar:
				fmt.Fprintf(&b, "var f%d = func() {\n", i)
			case kMethod:
				fmt.Fprintf(&b, "func (R%d) m%d() {\n", i, i%2)
			case kExported:
				fmt.Fprintf(&b, "func P%d() {\n", i)
			}
			for _, e := range edges[i] {
				if e.call {
					fmt.Fprintf(&b, "\t%s()\n", name(e.to))
				} else {
					fmt.Fprintf(&b, "\t_ = %s\n", name(e.to))
				}
			}
			if seed[i] {
				b.WriteString("\tC.M()\n")
			}
			b.WriteString("}\n")
		}
		src := b.String()

		// The initialization graph's components (Tarjan over every edge).
		comp := make([]int, n)
		index := make([]int, n)
		low := make([]int, n)
		on := make([]bool, n)
		for i := range index {
			index[i] = -1
		}
		var stack []int
		next, ncomp := 0, 0
		var visit func(int)
		visit = func(v int) {
			index[v], low[v] = next, next
			next++
			stack = append(stack, v)
			on[v] = true
			for _, e := range edges[v] {
				if index[e.to] < 0 {
					visit(e.to)
					low[v] = min(low[v], low[e.to])
				} else if on[e.to] {
					low[v] = min(low[v], index[e.to])
				}
			}
			if low[v] == index[v] {
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					on[w] = false
					comp[w] = ncomp
					if w == v {
						break
					}
				}
				ncomp++
			}
		}
		for i := range n {
			if index[i] < 0 {
				visit(i)
			}
		}
		size := make([]int, ncomp)
		selfLoop := make([]bool, n)
		for i := range n {
			size[comp[i]]++
			for _, e := range edges[i] {
				if e.to == i {
					selfLoop[i] = true
				}
			}
		}
		cyclicComp := func(i int) bool { return size[comp[i]] > 1 || selfLoop[i] }
		poisoned := make([]bool, n)
		withVar := map[int]bool{}
		for i := range n {
			if kind[i] == kVar && cyclicComp(i) {
				poisoned[i] = true
				withVar[comp[i]] = true
			}
		}
		wantS108 := len(withVar)
		if wantS108 > 0 {
			cycles++
		}

		// Inferred rows: least fixpoint over calls to unpoisoned inferred bodies.
		inferred := func(j int) bool { return kind[j] == kFunc || kind[j] == kVar }
		hasC := make([]bool, n)
		for changed := true; changed; {
			changed = false
			for i := range n {
				if !inferred(i) || hasC[i] {
					continue
				}
				c := seed[i]
				for _, e := range edges[i] {
					if e.call && inferred(e.to) && !poisoned[e.to] && hasC[e.to] {
						c = true
					}
				}
				if c {
					hasC[i], changed = true, true
				}
			}
		}
		wantS102 := 0
		for i := range n {
			if inferred(i) {
				continue
			}
			if seed[i] {
				wantS102++
			}
			for _, e := range edges[i] {
				if e.call && inferred(e.to) && !poisoned[e.to] && hasC[e.to] {
					wantS102++
				}
			}
		}

		pkg, diags := checkWithin(t, src, 5*time.Second)
		gotS108, gotS102 := 0, 0
		for _, d := range diags {
			switch {
			case d.Code == "S108" && strings.Contains(d.Message, "initialization cycle"):
				gotS108++
			case d.Code == "S102":
				gotS102++
			default:
				t.Fatalf("trial %d: unexpected %s\n%s", trial, describe(diags), src)
			}
		}
		if gotS108 != wantS108 || gotS102 != wantS102 {
			t.Fatalf("trial %d: want %d initialization cycles and %d S102, got %s\n%s", trial, wantS108, wantS102, describe(diags), src)
		}
		for _, f := range pkg.Funcs {
			var i int
			if _, err := fmt.Sscanf(f.Name, "f%d", &i); err != nil {
				continue
			}
			if got := strings.Contains(f.Sig.String(), "main.C"); got != hasC[i] {
				t.Fatalf("trial %d: %s has row %s, want C=%v\n%s", trial, f.Name, f.Sig, hasC[i], src)
			}
		}
	}
	if cycles < 100 {
		t.Fatalf("only %d trials held an initialization cycle; the generator misses the domain", cycles)
	}
}

// expandingCases are generic functions and types whose instantiation grows
// without end: Go rejects each as an instantiation cycle, and the checker
// reports S108 and terminates (round-2 M1; go/types mono.go).
var expandingCases = []struct{ name, src string }{
	{"self-slice", `func f[T](x T) { f([]T{x}) }`},
	{"self-row", `type Fault[T] struct{ Value T }
func Mark() error[Fault[int]]
func f[T](x T) { try f([]T{x}); return Fault[T]{x} }`},
	{"self-row-map", `type Fault[T] struct{ Value T }
func f[T](x T) error[Fault[T]] { try f(map[int]T{0: x}); return Fault[T]{x} }`},
	{"mutual", `type Fault[T] struct{ Value T }
func Mark() error[Fault[int]]
func f[T](x T) { try g([]T{x}); return Fault[T]{x} }
func g[T](x T) { try f([1]T{x}) }`},
	{"three", `func f[T](x T) { g(x) }
func g[T](x T) { h([]T{x}) }
func h[T](x T) { f(x) }`},
	{"explicit", `func f[T](x T) { var y []T; f[[]T](y) }`},
	{"through-type", `type Box[T] struct{ V T }
func f[T](x T) { f(Box[T]{x}) }`},
	{"type-field", `type L[T] struct { next []L[[1]T] }`},
	{"type-by-value", `type L[T] struct { next L[[1]T] }`},
	{"type-mutual", `type A[T] struct { b []B[[]T] }
type B[T] struct { a []A[T] }`},
}

func TestExpandingInstantiation(t *testing.T) {
	for _, c := range expandingCases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := checkWithin(t, "package main\n"+c.src+"\n", 2*time.Second)
			if len(diags) != 1 || diags[0].Code != "S108" || !strings.Contains(diags[0].Message, "instantiation cycle") {
				t.Fatalf("want one instantiation-cycle S108, got %s", describe(diags))
			}
		})
	}
}

// TestFiniteInstantiation: recursion that does not grow is legal, and its
// rows converge (controls for TestExpandingInstantiation).
func TestFiniteInstantiation(t *testing.T) {
	for _, src := range []string{
		`func f[T](x T) { f(x) }`,
		`func f[T](x T) { f([]int{1}) }`,
		`type Fault[T] struct{ Value T }
func f[T](x T) error[Fault[T]] { try f(x); return Fault[T]{x} }`,
		`func f[T](x T) { g(x) }
func g[T](x T) { f(x) }`,
		`type L[T] struct { next []L[T] }`,
		`type L[T] struct { next []L[int] }`,
	} {
		_, diags := checkWithin(t, "package main\n"+src+"\n", 2*time.Second)
		if len(diags) != 0 {
			t.Fatalf("%s: want no diagnostic, got %s", src, describe(diags))
		}
	}
}

// ---------------------------------------------------------------- D8/D16 totality

// cycleSwitch is the odd-cycle family with its columns in a given order:
// order[j] is the cycle vertex column j holds. Cases cover equal neighbours
// around the cycle; an odd cycle is total, an even one is not.
func cycleSwitch(n int, order []int) string {
	col := make([]int, n)
	for j, v := range order {
		col[v] = j
	}
	var b strings.Builder
	b.WriteString("package main\ntype A struct{}\ntype B struct{}\ntype U interface{A | B}\nfunc F(")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "x%d U", i)
	}
	b.WriteString(") int {\nswitch ")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("_")
	}
	b.WriteString(" := ")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "x%d.(type)", i)
	}
	b.WriteString(" {\n")
	for v := 0; v < n; v++ {
		for _, variant := range []string{"A", "B"} {
			cells := make([]string, n)
			for j := range cells {
				cells[j] = "_"
			}
			cells[col[v]] = variant
			cells[col[(v+1)%n]] = variant
			b.WriteString("case (" + strings.Join(cells, ",") + "): return 1\n")
		}
	}
	b.WriteString("}\n}\n")
	return b.String()
}

func columnOrders(n int, rng *rand.Rand) map[string][]int {
	identity := make([]int, n)
	reversed := make([]int, n)
	evensFirst := make([]int, 0, n)
	for i := range n {
		identity[i] = i
		reversed[i] = n - 1 - i
	}
	for i := 0; i < n; i += 2 {
		evensFirst = append(evensFirst, i)
	}
	for i := 1; i < n; i += 2 {
		evensFirst = append(evensFirst, i)
	}
	out := map[string][]int{"identity": identity, "reversed": reversed, "evens-first": evensFirst}
	for k := 0; k < 3; k++ {
		out[fmt.Sprintf("random%d", k)] = rng.Perm(n)
	}
	return out
}

// TestTotalityPermutations: the odd-cycle family in every column order is
// proved total or refused for its size (S103 "too many cases"), within 50
// ms at up to 31 subjects; an even cycle is never accepted (round-2 M3;
// D16 as amended in round 3).
func TestTotalityPermutations(t *testing.T) {
	rng := rand.New(rand.NewSource(1964))
	for _, n := range []int{5, 7, 11, 15, 19, 25, 29, 31} {
		for name, order := range columnOrders(n, rng) {
			src := cycleSwitch(n, order)
			best := time.Duration(1 << 62)
			var diags []check.Diagnostic
			for i := 0; i < 3; i++ {
				start := time.Now()
				_, diags = check.CheckSource(src)
				if e := time.Since(start); e < best {
					best = e
				}
			}
			if best > 50*time.Millisecond {
				t.Errorf("n=%d %s: %s, want under 50ms", n, name, best)
			}
			switch {
			case len(diags) == 0:
			case len(diags) == 1 && diags[0].Code == "S103" && strings.Contains(diags[0].Message, "too many cases"):
				if n <= 11 {
					t.Errorf("n=%d %s: a small total switch hit the budget", n, name)
				}
			default:
				t.Errorf("n=%d %s: want total or refused, got %s", n, name, describe(diags))
			}
			even := cycleSwitch(n+1, append(append([]int(nil), order...), n))
			_, ediags := check.CheckSource(even)
			if len(ediags) == 0 || ediags[0].Code != "S103" {
				t.Errorf("n=%d %s: an even cycle (not total) was accepted: %s", n+1, name, describe(ediags))
			}
		}
	}
}

// cnfSwitch writes a switch over n binary subjects whose cases are m random
// three-constructor rows: a row is the falsifying assignments of a 3-CNF
// clause, so the switch is total exactly when the formula is unsatisfiable.
// It returns the source and the rows.
func cnfSwitch(rng *rand.Rand, n, m int) (string, [][]int) {
	var b strings.Builder
	b.WriteString("package main\ntype A struct{}\ntype B struct{}\ntype U interface{A | B}\nfunc F(")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "x%d U", i)
	}
	b.WriteString(") int {\nswitch ")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("_")
	}
	b.WriteString(" := ")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "x%d.(type)", i)
	}
	b.WriteString(" {\n")
	var rows [][]int
	seen := map[string]bool{}
	for r := 0; r < m; r++ {
		cells := make([]string, n)
		row := make([]int, n)
		for j := range cells {
			cells[j] = "_"
			row[j] = -1
		}
		for _, v := range rng.Perm(n)[:3] {
			row[v] = rng.Intn(2)
			cells[v] = []string{"A", "B"}[row[v]]
		}
		text := strings.Join(cells, ",")
		if seen[text] {
			continue
		}
		seen[text] = true
		rows = append(rows, row)
		b.WriteString("case (" + text + "): return 1\n")
	}
	b.WriteString("}\nreturn 0\n}\n")
	return b.String(), rows
}

// cnfTotal decides by enumeration whether the rows cover every assignment.
func cnfTotal(n int, rows [][]int) bool {
	v := make([]int, n)
	for mask := 0; mask < 1<<n; mask++ {
		for j := range v {
			v[j] = mask >> j & 1
		}
		covered := false
		for _, r := range rows {
			ok := true
			for j, p := range r {
				if p >= 0 && p != v[j] {
					ok = false
					break
				}
			}
			if ok {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

// totalityVerdict reads the switch's verdict from the diagnostics: "total",
// "not total" or "refused".
func totalityVerdict(diags []check.Diagnostic) string {
	for _, d := range diags {
		if d.Code != "S103" {
			continue
		}
		if strings.Contains(d.Message, "too many cases") {
			return "refused"
		}
		return "not total"
	}
	return "total"
}

// TestTotalityBudget: random 3-CNF switches, the hard family of totality.
// Over 16 subjects the verdict matches enumeration exactly; over 60
// subjects the solver ends within its budget, refusing some switches with
// "too many cases" rather than accepting them unproven (D16 as amended).
func TestTotalityBudget(t *testing.T) {
	rng := rand.New(rand.NewSource(1965))
	for k := 0; k < 12; k++ {
		src, rows := cnfSwitch(rng, 16, 16*43/10)
		_, diags := checkWithin(t, src, 5*time.Second)
		got := totalityVerdict(diags)
		want := "not total"
		if cnfTotal(16, rows) {
			want = "total"
		}
		if got != want {
			t.Fatalf("16 subjects, instance %d: verdict %s, enumeration says %s\n%s", k, got, want, src)
		}
	}
	refused := 0
	for k := 0; k < 4; k++ {
		src, _ := cnfSwitch(rng, 60, 60*43/10)
		start := time.Now()
		_, diags := checkWithin(t, src, 5*time.Second)
		if e := time.Since(start); e > 2*time.Second {
			t.Errorf("60 subjects, instance %d: %s, want under 2s", k, e)
		}
		if totalityVerdict(diags) == "refused" {
			refused++
		}
	}
	if refused == 0 {
		t.Errorf("no 60-subject 3-CNF switch reached the budget; the refusal path is untested")
	}
}

// ---------------------------------------------------------------- D6 kinds and constraints

var kindCases = []struct{ name, want, src string }{
	{"self-recursive-row", "", `type List[T, E] struct { Value T; Tail []List[T, E]; Run func() error[E] }`},
	{"self-recursive-row-first", "", `type List[E, T] struct { Tail []List[E, T]; Value T; Run func() error[E] }`},
	{"self-recursive-explicit", "", `type List[T any, E error] struct { Value T; Tail []List[T, E]; Run func() error[E] }`},
	{"mutually-recursive-row", "", `type Box[T, E] struct { Next []Other[T, E]; Run func() error[E] }
type Other[T, E] struct { Next []Box[T, E] }`},
	{"mutual-row-declared-later", "", `type Other[T, E] struct { Next []Box[T, E] }
type Box[T, E] struct { Next []Other[T, E]; Run func() error[E] }`},
	{"three-cycle-row", "", `type A[E] struct { b []B[E] }
type B[E] struct { c []C[E] }
type C[E] struct { a []A[E]; run func() error[E] }`},
	{"recursive-capability-row", "", `type Svc interface{ Ping() }
type Job[R] struct { next []Job[R]; run func() uses R }
func Run(j Job[Svc]) {}`},
	{"recursive-conflict", "S108", `type L[E] struct { next []L[E]; run func() error[E]; v E }`},
	{"function-of-recursive", "", `type List[T, E] struct { Tail []List[T, E]; Run func() error[E] }
func Head(l List[T, E]) error[E] { return l.Run() }`},
	{"dependent-constraint-literal", "", `type I[T] interface { M(T) }
type S struct{}
func (S) M(x int) {}
type Box[T any, V I[T]] struct{ X T; Y V }
func F() { _ = Box{1, S{}} }`},
	{"dependent-constraint-explicit", "", `type I[T] interface { M(T) }
type S struct{}
func (S) M(x int) {}
type Box[T any, V I[T]] struct{ X T; Y V }
func F() { _ = Box[int, S]{1, S{}} }`},
	{"dependent-constraint-call", "", `type I[T] interface { M(T) }
type S struct{}
func (S) M(x int) {}
func Use[T any, V I[T]](x T, v V) {}
func F() { Use(1, S{}) }`},
	{"dependent-constraint-value", "", `type I[T] interface { M(T) }
type S struct{}
func (S) M(x int) {}
func Use[T any, V I[T]](x T, v V) {}
func F() func(int, S) { return Use[int, S] }`},
	{"dependent-constraint-unsatisfied", "S108", `type I[T] interface { M(T) }
type S struct{}
func (S) M(x int) {}
type Box[T any, V I[T]] struct{ X T; Y V }
func F() { _ = Box{"s", S{}} }`},
	{"dependent-constraint-call-unsatisfied", "S108", `type I[T] interface { M(T) }
type S struct{}
func (S) M(x int) {}
func Use[T any, V I[T]](x T, v V) {}
func F() { Use("s", S{}) }`},
}

func TestKindAndConstraintDomain(t *testing.T) {
	for _, c := range kindCases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := check.CheckSource("package main\n" + c.src + "\n")
			if got := codesOf(diags); got != c.want {
				t.Fatalf("want %q, got %s", c.want, describe(diags))
			}
		})
	}
}

// ---------------------------------------------------------------- D2/D5

func structChain(n int) string {
	var b strings.Builder
	b.WriteString("package main\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "type T%d struct { Next T%d }\n", i, i+1)
	}
	fmt.Fprintf(&b, "type T%d struct{}\n", n)
	return b.String()
}

func genericChain(n int) string {
	var b strings.Builder
	b.WriteString("package main\ntype W[T] struct { V T }\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "type T%d struct { Next W[T%d] }\n", i, i+1)
	}
	fmt.Fprintf(&b, "type T%d struct{}\n", n)
	return b.String()
}

func arrayChain(n int) string {
	var b strings.Builder
	b.WriteString("package main\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "type T%d struct { Next [2]T%d }\n", i, i+1)
	}
	fmt.Fprintf(&b, "type T%d struct{}\n", n)
	return b.String()
}

// TestFiniteChains: deep finite type chains are legal at any depth, and
// closing the chain is S108 once (round-2 M5: no depth cutoff).
func TestFiniteChains(t *testing.T) {
	for _, gen := range []struct {
		name string
		f    func(int) string
	}{{"struct", structChain}, {"generic", genericChain}, {"array", arrayChain}} {
		for _, n := range []int{1, 10, 300, 2000} {
			src := gen.f(n)
			_, diags := checkWithin(t, src, 10*time.Second)
			if len(diags) != 0 {
				t.Fatalf("%s n=%d: want no diagnostic, got %s", gen.name, n, describe(diags))
			}
			closed := strings.Replace(src, fmt.Sprintf("type T%d struct{}", n), fmt.Sprintf("type T%d struct{ Back T0 }", n), 1)
			_, diags = checkWithin(t, closed, 10*time.Second)
			if codesOf(diags) != "S108" {
				t.Fatalf("%s n=%d closed: want one S108, got %s", gen.name, n, describe(diags))
			}
		}
	}
}

var valueCases = []struct{ name, want, src string }{
	// Map keys of interface type compare by dynamic type and value.
	{"interface-keys-distinct-types", "", `func F() map[any]int { return map[any]int{int(1): 1, int64(1): 2} }`},
	{"interface-keys-untyped-default", "S108", `func F() map[any]int { return map[any]int{1: 1, int(1): 2} }`},
	{"interface-keys-string-int", "", `func F() map[any]int { return map[any]int{"1": 1, 1: 2} }`},
	{"interface-keys-equal-strings", "S108", `func F() map[any]int { return map[any]int{"a": 1, "a": 2} }`},
	{"int64-keys-equal", "S108", `func F() map[int64]int { return map[int64]int{1: 1, int64(1): 2} }`},
	// Typed integer constants index when representable as int.
	{"typed-index-int64", "", `func F(xs []int) int { return xs[int64(0)] }`},
	{"typed-index-uint8", "", `func F(xs []int) int { return xs[uint8(1)] }`},
	{"typed-index-negative", "S108", `func F(xs []int) int { return xs[int64(-1)] }`},
	{"typed-index-too-large", "S108", `func F(xs []int) int { return xs[uint64(18446744073709551615)] }`},
	{"typed-slice-bound", "", `func F(xs []int) []int { return xs[int32(1):uint(2)] }`},
	{"typed-make", "", `func F() []int { return make([]int, int8(2), uint16(3)) }`},
	{"typed-make-order", "S108", `func F() []int { return make([]int, int8(3), uint16(2)) }`},
	{"array-index-typed", "S108", `func F() int { return [2]int{1, 2}[int64(2)] }`},
	{"string-slice-implicit-high", "S108", `func F() string { return "ab"[3:] }`},
}

func TestValueDomain(t *testing.T) {
	for _, c := range valueCases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := check.CheckSource("package main\n" + c.src + "\n")
			if got := codesOf(diags); got != c.want {
				t.Fatalf("want %q, got %s", c.want, describe(diags))
			}
		})
	}
}

// ---------------------------------------------------------------- D1 Info.Types

// TestInstantiatedInfo: no expression of a non-generic function has a
// recorded type with an unbound type parameter; an instantiated function
// value records its instantiated type, wrappers included (round-2 M8).
func TestInstantiatedInfo(t *testing.T) {
	for _, src := range []string{
		`func Id[T](x T) T { return x }
func F() func(int) int { return Id[int] }`,
		`func Id[T](x T) T { return x }
func F() func(int) int { return (Id[int]) }`,
		`func Id[T](x T) T { return x }
func Apply(f func(int) int) int { return f(1) }
func F() int { return Apply(Id[int]) + Apply((Id[int])) }`,
		`func Id[T](x T) T { return x }
func F() int { f := Id[int]; g := (Id[string]); _ = g; return f(1) }`,
		`func Pair[A, B](a A, b B) A { return a }
func F() func(int, string) int { var f func(int, string) int = Pair[int, string]; return f }`,
	} {
		full := "package main\n" + src + "\n"
		pkg, diags := check.CheckSource(full)
		if len(diags) != 0 {
			t.Fatalf("%s: %s", src, describe(diags))
		}
		start := strings.Index(full, "func F()")
		found := false
		for x, ty := range pkg.Info.Types {
			if int(x.Pos()) < start {
				continue
			}
			if sig, ok := ty.(*check.Signature); ok && len(sig.TParams) > 0 {
				if _, isName := x.(*syntax.Ident); isName {
					continue // a generic function's name has its generic type, as in go/types
				}
			}
			if hasTypeParam(ty) {
				t.Errorf("%s: %s at %d records %s", src, exprKind(x), x.Pos(), ty)
			}
			if _, ok := x.(*syntax.IndexExpr); ok {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no type recorded for the instantiation", src)
		}
	}
}

func exprKind(x syntax.Expr) string { return fmt.Sprintf("%T", x) }

func hasTypeParam(t check.Type) bool {
	switch t := t.(type) {
	case *check.TypeParam:
		return true
	case *check.Signature:
		if len(t.TParams) > 0 {
			return true
		}
		for _, p := range t.Params {
			if hasTypeParam(p.Type) {
				return true
			}
		}
		for _, r := range t.Results {
			if hasTypeParam(r) {
				return true
			}
		}
	case *check.Slice:
		return hasTypeParam(t.Elem)
	case *check.Named:
		for _, a := range t.TArgs {
			if hasTypeParam(a) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------- D3 := targets

// TestDefineTargetsDomain: every left side of := over an existing x, new
// names and blanks gives the Go verdict with one diagnostic at most: a
// repeated name is one S108, no name new is one S108, and no false S110
// follows from either (round-2 M9).
func TestDefineTargetsDomain(t *testing.T) {
	atoms := []string{"x", "y", "z", "_"}
	var lists [][]string
	var build func(prefix []string)
	build = func(prefix []string) {
		if len(prefix) > 0 {
			lists = append(lists, append([]string(nil), prefix...))
		}
		if len(prefix) == 3 {
			return
		}
		for _, a := range atoms {
			build(append(prefix, a))
		}
	}
	build(nil)
	for _, lhs := range lists {
		seen := map[string]bool{}
		repeated, fresh := false, false
		for _, a := range lhs {
			if a == "_" {
				continue
			}
			if seen[a] {
				repeated = true
			}
			seen[a] = true
			if a != "x" {
				fresh = true
			}
		}
		rhs := make([]string, len(lhs))
		for i := range rhs {
			rhs[i] = fmt.Sprint(i + 1)
		}
		var reads []string
		for _, a := range []string{"x", "y", "z"} {
			if a == "x" || seen[a] {
				reads = append(reads, "_ = "+a)
			}
		}
		src := fmt.Sprintf("package main\nfunc F() { x := 0; %s := %s; %s }\n", strings.Join(lhs, ", "), strings.Join(rhs, ", "), strings.Join(reads, "; "))
		_, diags := check.CheckSource(src)
		want := ""
		if repeated || !fresh {
			want = "S108"
		}
		if got := codesOf(diags); got != want {
			t.Errorf("%s: want %q, got %s", src, want, describe(diags))
		}
	}
}
