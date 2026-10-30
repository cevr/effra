package check

import (
	"container/heap"
	"sort"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
)

// initDeps records, for each package-level body (a function, a method or a
// var initializer), the package functions, methods and vars it refers to,
// as its reporting check resolves them: the dependency graph of Go's
// initialization order (go/types initorder.go).
type initDeps struct {
	owner any                  // *Func or *pkgVar being checked
	deps  map[any]map[any]bool // owner → referenced *Func or *pkgVar
	byVar map[*Var]*pkgVar
	// tentative: discoverInit's silent pass records references too, at
	// its own silence level only (a nested peek is checked again at that
	// level), so the cycles are known before the reporting pass, which
	// records them again.
	tentative bool
}

// recordDep notes that the body being checked refers to o.
func (c *checker) recordDep(o any) {
	if c.init.owner == nil {
		return
	}
	if c.silent > 0 && !(c.init.tentative && c.silent == 1) {
		return
	}
	if v, ok := o.(*Var); ok {
		if !v.pkg {
			return
		}
		if pv := c.pkgVarOf(v); pv != nil {
			o = pv
		} else {
			return
		}
	}
	if f, ok := o.(*Func); ok && f.Pkg != c.pkg {
		return
	}
	if c.init.deps == nil {
		c.init.deps = map[any]map[any]bool{}
	}
	m := c.init.deps[c.init.owner]
	if m == nil {
		m = map[any]bool{}
		c.init.deps[c.init.owner] = m
	}
	m[o] = true
}

// beginDeps starts recording for owner; the returned func restores the
// previous owner (var initializers check on demand, nested).
func (c *checker) beginDeps(owner any) func() {
	prev := c.init.owner
	c.init.owner = owner
	if c.silent == 0 && c.init.deps != nil {
		delete(c.init.deps, owner)
	}
	return func() { c.init.owner = prev }
}

func (c *checker) pkgVarOf(v *Var) *pkgVar {
	if c.init.byVar == nil {
		c.init.byVar = make(map[*Var]*pkgVar, len(c.vars))
		for _, pv := range c.vars {
			c.init.byVar[pv.obj] = pv
		}
	}
	return c.init.byVar[v]
}

// An initialization cycle is a cycle of references through at least one
// package var, directly or through functions and methods. Go rejects it
// ("initialization cycle"): a var's initializer would need its own value
// (round-2 M2). A cycle through functions alone is recursion, legal.
//
// discoverInit (rows.go) records the references of every var initializer
// and of the functions and methods they reach before any body
// is checked with diagnostics, and checkInitCycles reports the cycles then:
// the cycle's vars become invalid, so their rows never reach a caller and
// no capability or error diagnostic follows from them (D3). It runs once
// more after the reporting pass, which records the references again, for
// anything left; a cycle already reported is not reported twice.

// checkInitCycles reports the initialization cycles of the whole package
// that no component-level report covers.
func (c *checker) checkInitCycles() {
	var nodes []any
	for _, f := range c.funcs {
		nodes = append(nodes, f)
	}
	for _, pv := range c.vars {
		nodes = append(nodes, pv)
	}
	c.reportInitCycles(nodes, func(any) bool { return true })
}

// reportInitCycles finds the strongly connected components of the
// dependency graph restricted to nodes (Tarjan) and reports each one that
// holds a var and a cycle (S108, once per cycle, at the var declared
// first). The cycle's vars become invalid: a later use of them is bad and
// silent.
func (c *checker) reportInitCycles(nodes []any, in func(any) bool) {
	index := map[any]int{}
	low := map[any]int{}
	on := map[any]bool{}
	var stack []any
	next := 0
	var sccs [][]any
	var visit func(n any)
	visit = func(n any) {
		index[n], low[n] = next, next
		next++
		stack = append(stack, n)
		on[n] = true
		for _, m := range sortedDeps(c.init.deps[n]) {
			if !in(m) {
				continue
			}
			if _, seen := index[m]; !seen {
				visit(m)
				low[n] = min(low[n], low[m])
			} else if on[m] {
				low[n] = min(low[n], index[m])
			}
		}
		if low[n] == index[n] {
			var scc []any
			for {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[top] = false
				scc = append(scc, top)
				if top == n {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}
	for _, n := range nodes {
		if _, seen := index[n]; !seen {
			visit(n)
		}
	}
	for _, scc := range sccs {
		var vars []*pkgVar
		covered := false
		for _, n := range scc {
			if pv, ok := n.(*pkgVar); ok {
				vars = append(vars, pv)
				if pv.initCycle {
					covered = true
				}
			}
		}
		if len(vars) == 0 || covered {
			continue
		}
		if len(scc) == 1 && !c.init.deps[scc[0]][scc[0]] {
			continue
		}
		sort.Slice(vars, func(i, j int) bool {
			if vars[i].file.index != vars[j].file.index {
				return vars[i].file.index < vars[j].file.index
			}
			return vars[i].obj.Pos < vars[j].obj.Pos
		})
		for _, pv := range vars {
			pv.initCycle = true
			pv.obj.Type = invalid
		}
		first := vars[0]
		var names []string
		for _, n := range scc {
			names = append(names, depName(n))
		}
		sort.Strings(names)
		saved := c.cur
		c.cur = first.file
		var at syntax.Node = first.spec
		if first.name != nil {
			at = first.name
		}
		c.errorf(at, CodeType, "initialization cycle: the initializer of %s refers to itself through %s (Go's initialization order)", first.obj.Name, strings.Join(names, ", "))
		c.cur = saved
	}
}

func depName(n any) string {
	switch n := n.(type) {
	case *Func:
		// A 196m function is named by its first parameter's type, as Go
		// names the method it lowers to; same-named ones stay apart.
		if n.owner != nil {
			return n.owner.Obj.Name + "." + n.Name
		}
		return n.Name
	case *pkgVar:
		return n.obj.Name
	}
	return "?"
}

// sortedDeps orders a body's dependencies by position, so the walk and its
// report are deterministic.
func sortedDeps(m map[any]bool) []any {
	out := make([]any, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	pos := func(n any) (int, syntax.Pos) {
		switch n := n.(type) {
		case *Func:
			return n.file.index, n.Pos
		case *pkgVar:
			return n.file.index, n.obj.Pos
		}
		return 0, 0
	}
	sort.Slice(out, func(i, j int) bool {
		fi, pi := pos(out[i])
		fj, pj := pos(out[j])
		if fi != fj {
			return fi < fj
		}
		return pi < pj
	})
	return out
}

// recordInitOrder fills Info.InitOrder from the dependency graph of the
// reporting pass, by Go's rule (spec, Package initialization; go/types
// initorder.go): the dependencies of a let are the lets its initializer
// reaches through functions and methods, and the next let to initialize
// is the earliest in declaration order whose dependencies are all
// initialized. A let that binds several names is one initializer. A
// package with an initialization cycle has a diagnostic (checkInitCycles),
// so the order is not used; the earliest remaining let breaks the cycle
// only to keep the walk total.
func (c *checker) recordInitOrder() {
	// node is the one initializer a package var belongs to.
	node := func(pv *pkgVar) *pkgVar {
		if pv.group != nil {
			return pv.group.members[0]
		}
		return pv
	}
	var nodes []*pkgVar
	order := map[*pkgVar]int{}
	for _, pv := range c.vars {
		if node(pv) == pv && len(pv.spec.Values) > 0 {
			nodes = append(nodes, pv)
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.file.index != b.file.index {
			return a.file.index < b.file.index
		}
		if a.spec != b.spec {
			return a.spec.Pos() < b.spec.Pos()
		}
		return a.idx < b.idx
	})
	for i, n := range nodes {
		order[n] = i
	}
	// deps[i] are the initializers node i waits for; users[j] the nodes
	// that wait for j.
	deps := make([]map[int]bool, len(nodes))
	users := make([][]int, len(nodes))
	for i, n := range nodes {
		deps[i] = map[int]bool{}
		seen := map[any]bool{}
		var walk func(o any)
		walk = func(o any) {
			for _, d := range sortedDeps(c.init.deps[o]) {
				if seen[d] {
					continue
				}
				seen[d] = true
				switch d := d.(type) {
				case *pkgVar:
					if j, ok := order[node(d)]; ok && j != i {
						deps[i][j] = true
					}
				case *Func:
					walk(d)
				}
			}
		}
		walk(n)
		for j := range deps[i] {
			users[j] = append(users[j], i)
		}
	}
	waiting := make([]int, len(nodes))
	ready := &intHeap{}
	for i := range nodes {
		waiting[i] = len(deps[i])
		if waiting[i] == 0 {
			heap.Push(ready, i)
		}
	}
	done := make([]bool, len(nodes))
	info := c.pkg.Info
	info.InitOrder = info.InitOrder[:0]
	for emitted := 0; emitted < len(nodes); emitted++ {
		i := -1
		if ready.Len() > 0 {
			i = heap.Pop(ready).(int)
		} else {
			for j := range nodes {
				if !done[j] {
					i = j
					break
				}
			}
		}
		done[i] = true
		n := nodes[i]
		in := &Initializer{}
		switch {
		case n.group != nil:
			in.Value = n.spec.Values[0]
			for _, m := range n.group.members {
				in.Vars = append(in.Vars, m.obj)
			}
		case n.idx < len(n.spec.Values):
			in.Value = n.spec.Values[n.idx]
			in.Vars = []*Var{n.obj}
		default:
			in.Value = n.spec.Values[0]
			in.Vars = []*Var{n.obj}
		}
		info.InitOrder = append(info.InitOrder, in)
		for _, u := range users[i] {
			if waiting[u]--; waiting[u] == 0 && !done[u] {
				heap.Push(ready, u)
			}
		}
	}
}

// intHeap is a min-heap of declaration indices.
type intHeap []int

func (h intHeap) Len() int           { return len(h) }
func (h intHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h intHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *intHeap) Push(x any)        { *h = append(*h, x.(int)) }
func (h *intHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}
