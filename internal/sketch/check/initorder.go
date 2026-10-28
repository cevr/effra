package check

import (
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
	// tentative: resolveBodies' silent pass records references too, at
	// its own silence level only (a nested peek is checked again at that
	// level), so the cycles are known before the reporting pass, which
	// records them again.
	tentative bool
}

// recordDep notes that the body being checked refers to o.
// While resolveBodies runs (c.refs set), every resolved reference is also
// recorded, at any silence level, as an edge of the body graph: name
// resolution does not depend on the silence or the rows.
func (c *checker) recordDep(o any) {
	if c.init.owner == nil {
		return
	}
	if c.refs != nil {
		c.recordRef(o)
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

// recordRef records a resolved reference of the body being resolved: a
// package function of this package or a package var.
func (c *checker) recordRef(o any) {
	switch x := o.(type) {
	case *Var:
		if !x.pkg {
			return
		}
		pv := c.pkgVarOf(x)
		if pv == nil {
			return
		}
		o = pv
	case *Func:
		if x.Pkg != c.pkg {
			return
		}
	default:
		return
	}
	m := c.refs[c.init.owner]
	if m == nil {
		m = map[any]bool{}
		c.refs[c.init.owner] = m
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
// resolveBodies (rows.go) records the resolved references of every var
// initializer, function and method before any body
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
		c.errorf(first.spec.Names[first.idx], CodeType, "initialization cycle: the initializer of %s refers to itself through %s (Go's initialization order)", first.obj.Name, strings.Join(names, ", "))
		c.cur = saved
	}
}

func depName(n any) string {
	switch n := n.(type) {
	case *Func:
		if n.Recv != nil {
			return n.Recv.Obj.Name + "." + n.Name
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
