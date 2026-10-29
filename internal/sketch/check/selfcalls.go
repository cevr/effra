package check

import (
	"maps"

	"ergo.local/prototype/internal/sketch/syntax"
)

// Typed edges of x.f(…) calls (196m; round 6 review M1, round 7 review M1).
//
// A call x.f(…) of a package function on x's type reaches one function,
// which x's type picks. The reference walk resolves the call when x's
// declaration fixes its type (selfCallee), and retires it when x names
// nothing (an S104 whatever the rows); any other call waits here for
// typing, as a method use did in 196a: the round-15 rule, references that
// need types come from typing. No edge is ever taken by name.
//
// Typing a body needs its callees' rows only for the types of function
// values and for the rows of calls (a catch on a call that cannot fail is
// an error), and a call's callee depends only on the head of the
// receiver's type. So each body with a waiting call is typed once,
// silently, from empty inferred rows (the rows of a body typed earlier are
// reset, so the result does not depend on the order of the bodies), and
// every call typing reaches is resolved to its function: one edge per
// call, linear in the source.
//
// A receiver whose type depends on a callee's row stays unresolved there:
// `let b = w() catch { case E: B{} }; b.m()` with w's error row inferred
// is a catch on a call that cannot fail while w's row is empty, so b is
// invalid. settleWaiting resolves such calls with a dependency worklist
// over the bodies the waiting bodies reach, the only bodies whose rows
// their typing reads.
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
		for _, n := range nodes {
			for _, p := range pending[n.owner()] {
				all = append(all, p.sel)
			}
		}
	}
	if !selfSettleOnly && !c.settleOnly {
		c.silent++
		for _, n := range nodes {
			if len(pending[n.owner()]) == 0 {
				continue
			}
			c.selfChecks++
			c.checkNode(n)
			if n.fn != nil {
				n.fn.resetRows()
			}
		}
		c.silent--
		c.newSolutions()
		for _, n := range nodes {
			c.applySelfCalls(n.owner(), pending)
		}
	}
	if len(pending) > 0 {
		c.settleWaiting(nodes, byFunc, byVar, pending)
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

// selfSettleOnly (or the checker's settleOnly), set by a test, skips the
// typing from empty rows, so the worklist alone resolves the calls.
var selfSettleOnly bool

// applySelfCalls moves each pending call of owner that typing resolved
// into owner's references: an edge to its function and, when the
// function's rows are inferred, the value uses its frames give (the
// earliest site of each row is kept, as the walk keeps the first). It
// leaves the unresolved calls and returns the functions with inferred rows
// it added an edge to.
func (c *checker) applySelfCalls(owner any, pending map[any][]pendingSelf) []*Func {
	list := pending[owner]
	if len(list) == 0 {
		return nil
	}
	var left []pendingSelf
	var added []*Func
	for _, p := range list {
		f, seen := c.selfRes[p.sel]
		if !seen {
			left = append(left, p)
			continue
		}
		if f == nil {
			continue
		}
		if !c.refs[owner][f] && (f.inferErrors || f.inferUses) {
			added = append(added, f)
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
	return added
}

// settleWaiting resolves the calls still pending after the typing from
// empty rows, silently, with a dependency worklist (round 7 review M1).
//
// The worklist holds the waiting bodies and every body they reach in the
// graph known so far, the only bodies whose rows their typing reads; each
// joins it once, callees first (a depth-first postorder). A body is checked
// again only when a body it references grew its rows, as in converge: so
// an acyclic part is checked once per body, and a body no waiting body
// reaches is never checked. A call typing resolves adds its edge at once;
// a callee new to the worklist joins it with the bodies it reaches, and
// the caller is checked again when the callee's rows grow. A waiting body
// whose calls stay unresolved is checked once its callees settle and wakes
// nobody. The rows restart empty afterwards: the reporting order is decided
// only once every edge is known.
//
// The bound is converge's: 2·(bodies + edges)·(D + 1) checks, where D
// counts the distinct row elements produced so far. Past it the worklist
// stops and the calls left stay unresolved; no known source reaches it
// (rows that only grow fit it, as in converge, whose bound
// TestConvergeBoundOscillation checks). c.selfChecks counts the checks and c.selfSteps the bodies and edges the
// worklist visits, for the linear-work tests.
func (c *checker) settleWaiting(nodes []*bodyNode, byFunc map[*Func]*bodyNode, byVar map[*pkgVar]*bodyNode, pending map[any][]pendingSelf) {
	c.linkBodies(nodes, byFunc, byVar)
	needed := map[*bodyNode]bool{}
	preds := map[*bodyNode][]*bodyNode{}
	queued := map[*bodyNode]bool{}
	var queue []*bodyNode
	edges := 0
	// need adds root and the bodies it reaches that are not in the
	// worklist yet, callees first.
	need := func(root *bodyNode) {
		if needed[root] {
			return
		}
		needed[root] = true
		type frame struct {
			n *bodyNode
			i int
		}
		stack := []frame{{n: root}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.i < len(top.n.edges) {
				e := top.n.edges[top.i]
				top.i++
				c.selfSteps++
				edges++
				preds[e] = append(preds[e], top.n)
				if !needed[e] {
					needed[e] = true
					stack = append(stack, frame{n: e})
				}
				continue
			}
			n := top.n
			stack = stack[:len(stack)-1]
			c.selfSteps++
			queued[n] = true
			queue = append(queue, n)
		}
	}
	for _, n := range nodes {
		if len(pending[n.owner()]) > 0 {
			need(n)
		}
	}
	seen := map[[2]int32]bool{}
	budget := func() int { return 2 * (len(needed) + edges) * (len(seen) + 1) }
	checks := 0
	c.silent++
	for len(queue) > 0 && checks < budget() {
		n := queue[0]
		queue = queue[1:]
		queued[n] = false
		checks++
		changed := c.checkNode(n)
		if n.fn != nil {
			for k, v := range []*RowVar{n.fn.errVar, n.fn.useVar} {
				if v != nil {
					for _, id := range v.sol.idsOf(c.types) {
						seen[[2]int32{int32(k), id}] = true
					}
				}
			}
		}
		for _, f := range c.applySelfCalls(n.owner(), pending) {
			to := byFunc[f]
			if to == nil {
				continue
			}
			c.selfSteps++
			edges++
			n.edges = append(n.edges, to)
			preds[to] = append(preds[to], n)
			need(to)
		}
		if !changed {
			continue
		}
		for _, p := range preds[n] {
			if !queued[p] {
				queued[p] = true
				queue = append(queue, p)
			}
		}
	}
	c.silent--
	c.selfChecks += checks
	for n := range needed {
		if n.fn != nil {
			n.fn.resetRows()
		}
	}
	for _, n := range nodes {
		n.edges = nil
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
