package check

import (
	"maps"
	"slices"

	"ergo.local/prototype/internal/sketch/syntax"
)

// Typed edges of x.f(…) calls (196m; round 6 review M1).
//
// A call x.f(…) of a package function on x's type reaches one function,
// which x's type picks. The reference walk resolves the call when x's
// declaration fixes its type (selfCallee); any other call waits here for
// typing, as a method use did in 196a: the round-15 rule, references that
// need types come from typing. No edge is ever taken by name.
//
// Typing a body needs its callees' rows only for the types of function
// values (a function's type carries its rows), and a call's callee depends
// only on the head of the receiver's type. So each body with a waiting call
// is typed once, silently, from empty inferred rows (the rows of a body
// typed earlier are reset, so the result does not depend on the order of
// the bodies), and every call typing reaches is resolved to its function:
// one edge per call, linear in the source.
//
// A receiver whose type depends on a row still empty there (a generic call
// that unifies a function with an inferred row against one with a written
// row, round 15's witness shape) does not type from empty rows. While such
// calls remain, the rows are settled silently in the order of the graph
// known so far, every body is typed again with them, and the newly
// resolved calls add their edges; each round resolves at least one call or
// stops. A call that typing never resolves is in code with a type error of
// its own, which the reporting pass reports, and its callee's rows are
// never read.
//
// Prior art: go/types records a method's use when it resolves the selector
// (Info.Uses, LookupFieldOrMethod) and orders initialization by those
// dependencies; MoonBit and Koka resolve x.f by x's inferred type, never
// lexically; rustc resolves methods during type checking, after name
// resolution has fixed every plain path.

// resolveSelfCalls resolves the pending calls x.f(…) of each body by typing
// and adds their edges and class G value uses to the body's references.
func (c *checker) resolveSelfCalls(nodes []*bodyNode, byFunc map[*Func]*bodyNode, byVar map[*pkgVar]*bodyNode, pending map[any][]pendingSelf) {
	if len(pending) == 0 {
		return
	}
	c.selfRes = map[*syntax.SelectorExpr]*Func{}
	defer func() { c.selfRes = nil }()
	var all []*syntax.SelectorExpr
	if selfTrace != nil {
		for _, list := range pending {
			for _, p := range list {
				all = append(all, p.sel)
			}
		}
	}
	c.silent++
	for _, n := range nodes {
		if len(pending[n.owner()]) == 0 || selfSettleOnly {
			continue
		}
		c.checkNode(n)
		if n.fn != nil {
			n.fn.resetRows()
		}
	}
	c.silent--
	c.newSolutions()
	c.applySelfCalls(pending)
	for len(pending) > 0 {
		before := len(c.selfRes)
		c.settleRows(nodes, byFunc, byVar)
		if len(c.selfRes) == before {
			break
		}
		c.applySelfCalls(pending)
	}
	if selfTrace != nil {
		selfTrace(c, all, maps.Clone(c.selfRes))
	}
}

// selfTrace, when set by a test, receives the pending calls of a check and
// what typing resolved them to before any body is checked with
// diagnostics: a 196m function, or nil for another member (a field, an
// interface or capability method). A call typing never reached is absent.
var selfTrace func(c *checker, pending []*syntax.SelectorExpr, resolved map[*syntax.SelectorExpr]*Func)

// selfSettleOnly, set by a test, skips the typing from empty rows, so the
// settling rounds alone resolve the calls.
var selfSettleOnly bool

// applySelfCalls moves each pending call that typing resolved into its
// body's references: an edge to its function and, when the function's rows
// are inferred, the value uses its frames give (the earliest site of each
// row is kept, as the walk keeps the first). It leaves the unresolved calls.
func (c *checker) applySelfCalls(pending map[any][]pendingSelf) {
	for owner, list := range pending {
		var left []pendingSelf
		for _, p := range list {
			f, seen := c.selfRes[p.sel]
			if !seen {
				left = append(left, p)
				continue
			}
			if f == nil {
				continue
			}
			c.refs[owner][f] = true
			if !f.inferErrors && !f.inferUses {
				continue
			}
			uses := c.valueRefs[owner][f]
			for k := range uses {
				if p.site[k].ok && (!uses[k].ok || p.site[k].at < uses[k].at) {
					uses[k] = p.site[k]
				}
			}
			c.valueRefs[owner][f] = uses
		}
		if len(left) == 0 {
			delete(pending, owner)
		} else {
			pending[owner] = left
		}
	}
}

// settleRows checks every body silently, one strongly connected component
// of the graph known so far at a time, in reverse topological order, so
// each body is typed with its callees' rows joined; typing records the
// calls x.f(…) it resolves. The rows restart empty afterwards: the
// reporting order is decided only once every edge is known.
func (c *checker) settleRows(nodes []*bodyNode, byFunc map[*Func]*bodyNode, byVar map[*pkgVar]*bodyNode) {
	c.linkBodies(nodes, byFunc, byVar)
	c.silent++
	for _, scc := range tarjan(nodes) {
		if len(scc) == 1 && !slices.Contains(scc[0].edges, scc[0]) {
			c.checkNode(scc[0])
			continue
		}
		// Rows only grow and are finite; the bound keeps a refused
		// component (whose rows the reporting pass refuses) finite too.
		for round := 0; round <= 2*len(scc); round++ {
			changed := false
			for _, n := range scc {
				if c.checkNode(n) {
					changed = true
				}
			}
			if !changed {
				break
			}
		}
	}
	c.silent--
	for _, n := range nodes {
		n.edges, n.index, n.low, n.onStack = nil, -1, 0, false
		if n.fn != nil {
			n.fn.resetRows()
		}
	}
	c.newSolutions()
}

// linkBodies sets each node's edges from the body references (c.refs): to
// a function whose rows are inferred and to a package var. It returns the
// number of edges.
func (c *checker) linkBodies(nodes []*bodyNode, byFunc map[*Func]*bodyNode, byVar map[*pkgVar]*bodyNode) int {
	count := 0
	for _, n := range nodes {
		n.edges = nil
		for _, d := range sortedDeps(c.refs[n.owner()]) {
			var to *bodyNode
			switch d := d.(type) {
			case *Func:
				if d.inferErrors || d.inferUses {
					to = byFunc[d]
				}
			case *pkgVar:
				to = byVar[d]
			}
			if to != nil {
				n.edges = append(n.edges, to)
				count++
			}
		}
	}
	return count
}
