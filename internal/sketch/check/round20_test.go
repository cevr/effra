package check

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"ergo.local/prototype/internal/sketch/syntax"
)

// A workCount counts the wire summaries' work through the wireWork hook,
// apart from the production meter: each unit as it is done, per meter and
// per root meter (an allowance's work counts toward its parent too). A unit
// done before the meter charged it is recorded in early (round 19 M1).
type workCount struct {
	mu    sync.Mutex
	done  map[*wireMeter]int
	early []string
}

func countWork(t *testing.T) *workCount {
	t.Helper()
	wc := &workCount{done: map[*wireMeter]int{}}
	wireWork = func(m *wireMeter) {
		wc.mu.Lock()
		defer wc.mu.Unlock()
		for r := m; r != nil; r = r.parent {
			wc.done[r]++
			if wc.done[r] > r.used && len(wc.early) < 5 {
				wc.early = append(wc.early, fmt.Sprintf("unit %d done with %d of %d charged", wc.done[r], r.used, r.limit))
			}
		}
	}
	t.Cleanup(func() { wireWork = nil })
	return wc
}

// checkLimited checks src as one package, with its wire budget replaced by
// limit when limit is not nil.
func checkLimited(t *testing.T, src string, limit func(int) int) ([]Diagnostic, *checker) {
	t.Helper()
	file, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	c := newChecker("main", Std, prelude().scope)
	c.wireLimit = limit
	c.run([]*syntax.File{file})
	return c.sorted(), c
}

func allBudget(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Code != CodeDuplicateCap || d.Message != wireBudgetMessage {
			return false
		}
	}
	return len(diags) > 0
}

var budgetShapes = []string{"entry chain", "wide fan-in", "large-base chain", "replace diamond", "disjoint", "empty independent", "inline fan-in"}

// TestWireBudgetPrefixes: for every shape, at every budget from zero to
// the work the full check charges, no unit of work is done before it is
// charged, the meter stays within its budget, and the verdict is the full
// check's at its exact cost and only budget refusals below it: every
// prefix of the work that ends at a refusal, a zero and a small budget
// among them.
func TestWireBudgetPrefixes(t *testing.T) {
	wc := countWork(t)
	for _, shape := range budgetShapes {
		src := wireFamily(shape, 4)
		full, c := checkLimited(t, src, nil)
		cost := c.wires.used
		if wc.done[c.wires] > cost {
			t.Fatalf("%s: %d units done, %d charged", shape, wc.done[c.wires], cost)
		}
		for limit := 0; limit <= cost; limit++ {
			diags, c := checkLimited(t, src, func(int) int { return limit })
			m := c.wires
			if m.used > limit || wc.done[m] > m.used {
				t.Fatalf("%s at %d: %d charged, %d done", shape, limit, m.used, wc.done[m])
			}
			switch {
			case limit == cost:
				if fmt.Sprint(diags) != fmt.Sprint(full) {
					t.Errorf("%s at its cost %d: %v, want %v", shape, cost, diags, full)
				}
			case !allBudget(diags):
				t.Errorf("%s at %d of %d: want only budget refusals, got %v", shape, limit, cost, diags)
			}
		}
		t.Logf("%s n=4: %d steps, %d budgets checked", shape, cost, cost+1)
	}
	if len(wc.early) > 0 {
		t.Errorf("work done before it was charged: %v", wc.early)
	}
}

// TestWireBudgetOrder: the budget verdict, the charged work and the place
// of a refusal do not depend on the order of the declarations (round 19
// M2), for the reviewer's saved pair, the disjoint ladder across the
// refusal boundary at the real budget, and every shape at budgets within
// 10% of its cost, for whole-file and wire-only permutations.
func TestWireBudgetOrder(t *testing.T) {
	var used [2]int
	var verdict [2]string
	for i, name := range []string{"order-original.ef", "order-permuted.ef"} {
		data, err := os.ReadFile("testdata/wireorder/" + name)
		if err != nil {
			t.Fatal(err)
		}
		diags, c := checkLimited(t, string(data), nil)
		used[i], verdict[i] = c.wires.used, fmt.Sprint(len(diags) == 0)
		if p := OrderProblems(string(data), 4); len(p) > 0 {
			t.Errorf("%s: %s", name, p[0])
		}
	}
	if used[0] != used[1] || verdict[0] != verdict[1] {
		t.Errorf("the saved pair: %d steps (accepted %s) and %d steps (accepted %s)", used[0], verdict[0], used[1], verdict[1])
	}
	t.Logf("saved pair: %d steps each, accepted %s", used[0], verdict[0])
	check := func(limit func(int) int) func(string) []Diagnostic {
		return func(s string) []Diagnostic { d, _ := checkLimited(t, s, limit); return d }
	}
	refused := 0
	for n := 16; n <= 96; n += 4 {
		src := wireFamily("disjoint", n)
		if d, _ := checkLimited(t, src, nil); len(d) > 0 {
			refused++
		}
		for _, permute := range []func(string, uint64) (string, func(int) (int, int), func(int) (int, int), bool){PermuteDecls, PermuteWires} {
			if p := orderProblems(src, 3, permute, check(nil)); len(p) > 0 {
				t.Fatalf("disjoint n=%d: %s", n, p[0])
			}
		}
	}
	if refused == 0 || refused == 21 {
		t.Errorf("the ladder from 16 to 96 should cross the refusal boundary; %d of 21 refused", refused)
	}
	for _, shape := range budgetShapes {
		src := wireFamily(shape, 12)
		_, c := checkLimited(t, src, nil)
		cost := c.wires.used
		for _, pct := range []int{90, 95, 99, 100, 101, 105, 110} {
			limit := cost * pct / 100
			if pct == 99 {
				limit = cost - 1
			}
			if pct == 101 {
				limit = cost + 1
			}
			for _, permute := range []func(string, uint64) (string, func(int) (int, int), func(int) (int, int), bool){PermuteDecls, PermuteWires} {
				if p := orderProblems(src, 3, permute, check(func(int) int { return limit })); len(p) > 0 {
					t.Fatalf("%s n=12 at %d%% of %d: %s", shape, pct, cost, p[0])
				}
			}
		}
		for seed := uint64(1); seed <= 3; seed++ {
			permuted, _, _, ok := PermuteDecls(src, seed)
			if !ok {
				t.Fatalf("%s: no permutation", shape)
			}
			if _, pc := checkLimited(t, permuted, nil); pc.wires.used != cost {
				t.Errorf("%s n=12 seed %d: %d steps, the original %d", shape, seed, pc.wires.used, cost)
			}
		}
	}
}

// TestWireBudgetGroups: two declarations of one name (`var _`) form one
// inline wire group, refused or kept as a whole, so their order does not
// choose which of them is refused.
func TestWireBudgetGroups(t *testing.T) {
	src := wireFamily("wide fan-in", 6) +
		"var _ = with(wire{ X0\n X1\n X2 }) { 1 }\nvar _ = with(wire{ X3\n X4\n X5 }) { 2 }\n"
	full, c := checkLimited(t, src, nil)
	if len(full) > 0 {
		t.Fatalf("want acceptance, got %v", full)
	}
	cost := c.wires.used
	for limit := 0; limit < cost; limit++ {
		diags, _ := checkLimited(t, src, func(int) int { return limit })
		if !allBudget(diags) {
			t.Fatalf("at %d: %v", limit, diags)
		}
		check := func(s string) []Diagnostic { d, _ := checkLimited(t, s, func(int) int { return limit }); return d }
		if p := orderProblems(src, 2, PermuteDecls, check); len(p) > 0 {
			t.Fatalf("at %d of %d: %s", limit, cost, p[0])
		}
	}
}
