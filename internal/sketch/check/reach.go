package check

import (
	"sort"
)

// reachNode records what one body or one named wire's build can reach: the
// package functions and methods it names, the capability methods it calls,
// the method names it calls on values of interface types, and the wires it
// enters or embeds. The wire-cycle check walks it (rows.md, Wire 5).
type reachNode struct {
	wires       []*Wire
	funcs       []*Func
	capCalls    []capCall
	methodNames []string
}

// capCall is a call of a capability method, Cap.m(…): it reaches method m of
// every provider type bound to Cap in this package.
type capCall struct {
	cap  *Named
	name string
}

// reachOf returns the reach node of a function or a wire, emptied: a body is
// checked again during a fixpoint, and its reach is recorded afresh.
func (c *checker) reachOf(key any) *reachNode {
	n := c.reach[key]
	if n == nil {
		n = &reachNode{}
		c.reach[key] = n
		return n
	}
	*n = reachNode{}
	return n
}

// checkWireCycles reports each named wire whose build can reach itself
// (S108; rows.md, Wire 5): through a provider expression that enters the
// wire, a function or method it calls, a capability method whose provider
// enters it, or an embedding. A wire's successors are the wires its build
// reaches without crossing another wire; the cycles are the strongly
// connected components of that graph (Tarjan), each reported once at every
// wire on it.
func (c *checker) checkWireCycles() {
	if len(c.pkg.Wires) == 0 {
		return
	}
	byMethod := map[string][]*Func{}
	for _, f := range c.funcs {
		if f.Recv != nil || f.owner != nil {
			byMethod[f.Name] = append(byMethod[f.Name], f)
		}
	}
	names := sortedKeys(c.pkg.Wires)
	succ := map[*Wire][]*Wire{}
	for _, name := range names {
		w := c.pkg.Wires[name]
		succ[w] = c.wiresReached(c.reach[w], byMethod)
	}
	// Tarjan over the wire graph (small: one node per named wire).
	index := map[*Wire]int{}
	low := map[*Wire]int{}
	on := map[*Wire]bool{}
	var stack []*Wire
	next := 0
	cyclic := map[*Wire]bool{}
	var visit func(w *Wire)
	visit = func(w *Wire) {
		index[w], low[w] = next, next
		next++
		stack = append(stack, w)
		on[w] = true
		for _, v := range succ[w] {
			if _, seen := index[v]; !seen {
				visit(v)
				low[w] = min(low[w], low[v])
			} else if on[v] {
				low[w] = min(low[w], index[v])
			}
		}
		if low[w] != index[w] {
			return
		}
		var scc []*Wire
		for {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			on[v] = false
			scc = append(scc, v)
			if v == w {
				break
			}
		}
		self := false
		for _, v := range succ[w] {
			if v == w {
				self = true
			}
		}
		if len(scc) > 1 || self {
			for _, v := range scc {
				cyclic[v] = true
			}
		}
	}
	for _, name := range names {
		w := c.pkg.Wires[name]
		if _, seen := index[w]; !seen {
			visit(w)
		}
	}
	for _, name := range names {
		w := c.pkg.Wires[name]
		if !cyclic[w] {
			continue
		}
		c.cur = w.file
		c.errorf(w.Decl.Name, CodeType, "wire %s can reach itself while it is built (a with or an embedding of %s through its providers); a wire's build may not enter itself (rows.md, Wire 5)", w.Name, w.Name)
	}
}

// wiresReached returns the wires a reach node reaches through functions,
// methods and capability providers, without crossing a wire.
func (c *checker) wiresReached(start *reachNode, byMethod map[string][]*Func) []*Wire {
	if start == nil {
		return nil
	}
	seenFunc := map[*Func]bool{}
	seenWire := map[*Wire]bool{}
	seenName := map[string]bool{}
	var out []*Wire
	work := []*reachNode{start}
	visitFunc := func(f *Func) {
		if f == nil || seenFunc[f] {
			return
		}
		seenFunc[f] = true
		if n := c.reach[f]; n != nil {
			work = append(work, n)
		}
	}
	for len(work) > 0 {
		n := work[len(work)-1]
		work = work[:len(work)-1]
		for _, w := range n.wires {
			if !seenWire[w] && w.pkg == c.pkg {
				seenWire[w] = true
				out = append(out, w)
			}
		}
		for _, f := range n.funcs {
			visitFunc(f)
		}
		for _, cc := range n.capCalls {
			for _, p := range c.providers[cc.cap] {
				visitFunc(p.method(cc.name))
			}
		}
		for _, name := range n.methodNames {
			if seenName[name] {
				continue
			}
			seenName[name] = true
			for _, f := range byMethod[name] {
				visitFunc(f)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
