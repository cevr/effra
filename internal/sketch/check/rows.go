package check

import (
	"fmt"
	"slices"

	"ergo.local/prototype/internal/sketch/syntax"
)

// target is a failure target and a capability boundary: a function body, a
// function literal, a trailing block, a go block, a named wire's build or a
// package var's initializer (rows.md, Error rows 4). A with block is not a
// target: it discharges capabilities inside the target that holds it.
type target struct {
	name string // for messages: "function f", "this literal", "wire W"
	// Declared rows; nil means the row is inferred from the body
	// (rows-and-constraints §7 amendment 1).
	declErrors, declUses *Row
	errors, uses         *Row // what escapes or is charged (inferred rows)
	withs                []*Row
	// results are the value results a return gives; resultKnown is false
	// for a block whose result is its last expression's type.
	results     []Type
	resultKnown bool
	isFunc      bool // return is allowed (a function or a func literal)
	main        bool
	wire        bool // a named wire's build: closed (README Gap 6)
	// internal is what a named wire binds itself: its providers may use it
	// (rows.md, Wire 4); charged collects the internal capabilities the
	// entry being checked uses, for the wire's entry-cycle check (M14).
	internal, charged *Row
}

func (t *target) row(declared, inferred *Row) *Row {
	if declared != nil {
		return declared
	}
	return inferred
}

// charge records that the body charges capability cap at n: a with of this
// target discharges it; otherwise it must be in the declared row (S102), or
// it joins the inferred row (rows.md, Capability rows 2 and 3).
func (c *checker) charge(n syntax.Node, cap Type) {
	if c.fn == nil || c.fn.t == nil {
		return
	}
	t := c.fn.t
	for i := len(t.withs) - 1; i >= 0; i-- {
		// A nil scope is a with whose wire is unknown (reported): it
		// discharges everything, so no charge cascades from it (D3; M18).
		if t.withs[i] == nil || t.withs[i].has(c.types, cap) {
			return
		}
	}
	if t.wire && t.internal.has(c.types, cap) {
		t.charged = t.charged.with(c.types, cap)
		return
	}
	if t.declUses == nil {
		t.uses = t.uses.with(c.types, cap)
		return
	}
	if t.declUses.has(c.types, cap) {
		return
	}
	switch {
	case t.wire:
		c.errorf(n, CodeMissingCap, "%s requires %s while it is built; a named wire is closed: its providers require no capability from the with site (README Gap 6)", t.name, cap)
	default:
		c.errorf(n, CodeMissingCap, "%s requires %s, which is not in the capability row of %s and no with provides it", describeCharge(n), cap, t.name)
	}
}

func describeCharge(n syntax.Node) string {
	switch n.(type) {
	case *syntax.CallExpr:
		return "this call"
	}
	return "this expression"
}

// escape records failures that escape to the current target at n (try, a
// fallible return, a return of an error value, the rest of a partial catch):
// each must be in the declared error row (S101), or joins the inferred row.
func (c *checker) escape(n syntax.Node, row *Row) {
	if row.Empty() || c.fn == nil || c.fn.t == nil {
		return
	}
	t := c.fn.t
	if t.declErrors == nil {
		t.errors = t.errors.union(c.types, row)
		return
	}
	extra := row.minus(c.types, t.declErrors)
	if extra.Empty() {
		return
	}
	switch {
	case t.wire:
		c.errorf(n, CodeUndeclared, "%s can fail with %s while it is built; a named wire is closed: no failure escapes its build (README Gap 6)", t.name, rowText(extra))
	default:
		c.errorf(n, CodeUndeclared, "%s escapes %s, whose error row %s does not list it", rowText(extra), t.name, rowText(t.declErrors))
	}
}

func (c *checker) chargeRow(n syntax.Node, row *Row) {
	for _, e := range row.Elems() {
		c.charge(n, e)
	}
}

// ---------------------------------------------------------------- bodies in SCC order

type bodyNode struct {
	fn    *Func
	v     *pkgVar
	edges []*bodyNode
	// Tarjan state
	index, low int
	onStack    bool
}

// checkBodies checks every body, one strongly connected component of the
// package's reference graph at a time, in reverse topological order: a
// callee's inferred rows are final before its callers are checked. Inside a
// cycle the rows are joined to a fixpoint (finite sets ordered by
// inclusion), silently, then the component is checked once more with
// diagnostics. An acyclic graph is linear: each body is checked once.
//
// The graph's edges are the references that row inference needs: to a
// function whose rows are inferred and to a package var. They are the
// plain identifiers of each body resolved through its lexical scopes by a
// walk that runs before typing and visits every node (refs.go, class H):
// a local, a parameter or a receiver that shadows a package function adds
// no edge (round 14 M1), and a tentative type error hides none (round 15
// M1). A method or a function that spells its
// rows is read by its signature, so it adds no edge: one edge per
// referenced object, never one per same-named method (round-3 M5).
func (c *checker) checkBodies() {
	var nodes []*bodyNode
	byFunc := map[*Func]*bodyNode{}
	byVar := map[*pkgVar]*bodyNode{}
	for _, f := range c.funcs {
		if f.Decl.Body == nil || f.Sig == nil {
			continue
		}
		n := &bodyNode{fn: f, index: -1}
		nodes = append(nodes, n)
		byFunc[f] = n
	}
	for _, v := range c.vars {
		n := &bodyNode{v: v, index: -1}
		nodes = append(nodes, n)
		byVar[v] = n
	}
	// Class H: the edges come from the lexical walk of every body, before
	// any body is typed (refs.go).
	c.refs = make(map[any]map[any]bool, len(nodes))
	c.valueRefs = make(map[any]map[*Func][2]bool, len(nodes))
	for _, n := range nodes {
		var owner any = n.v
		var w *refWalker
		if n.fn != nil {
			owner, w = n.fn, c.bodyRefs(n.fn)
		} else {
			w = c.varRefs(n.v)
		}
		c.refs[owner], c.valueRefs[owner] = w.refs, w.values
	}
	c.discoverInit()
	c.bodyEdges = 0
	for _, n := range nodes {
		var owner any = n.v
		if n.fn != nil {
			owner = n.fn
		}
		for _, d := range sortedDeps(c.refs[owner]) {
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
				c.bodyEdges++
			}
		}
	}
	c.refs = nil
	for _, scc := range tarjan(nodes) {
		c.checkComponent(scc)
	}
	c.valueRefs = nil
	// Instantiation cycles through methods alone close outside any cyclic
	// component (methods add no edge); the reporting pass recorded them.
	c.checkInstantiationCycles()
	c.checkInitCycles()
}

// discoverInit finds the package's initialization cycles before any body
// is checked with diagnostics (round-2 M2, round-3 M5). Each body's
// dependencies start from its lexical references (c.refs, class H), so no
// function or var dependency depends on typing; it then checks, silently,
// each package var's initializer and every function and method those
// reach, each once, which adds the references that need types (a method
// value or call, round-3 M5): the dependency closure of Go's
// initialization order, linear in its size. A cycle through a var is S108
// and its vars become invalid, so no row diagnostic follows from them. The
// silent checks leave no row behind: the inferred rows restart empty.
func (c *checker) discoverInit() {
	if len(c.vars) == 0 {
		return
	}
	if c.init.deps == nil {
		c.init.deps = map[any]map[any]bool{}
	}
	for owner, refs := range c.refs {
		m := c.init.deps[owner]
		if m == nil {
			m = map[any]bool{}
			c.init.deps[owner] = m
		}
		for o := range refs {
			m[o] = true
		}
	}
	c.silent++
	c.init.tentative = true
	visited := map[*Func]bool{}
	var queue []*Func
	enqueue := func(owner any) {
		for _, d := range sortedDeps(c.init.deps[owner]) {
			if f, ok := d.(*Func); ok && !visited[f] && f.Decl != nil && f.Decl.Body != nil && f.Sig != nil {
				visited[f] = true
				queue = append(queue, f)
			}
		}
	}
	for _, pv := range c.vars {
		c.checkPkgVar(pv)
		enqueue(pv)
	}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		c.checkFuncBody(f)
		enqueue(f)
	}
	c.init.tentative = false
	c.silent--
	for _, f := range c.funcs {
		if f.Sig == nil {
			continue
		}
		f.resetRows()
	}
	c.newSolutions()
	c.checkInitCycles()
}

func (c *checker) checkComponent(scc []*bodyNode) {
	// The group whose rows are being inferred (class G, pinGroup).
	c.group = map[*Func]bool{}
	for _, n := range scc {
		if n.fn != nil {
			c.group[n.fn] = true
		}
	}
	c.groupValue, c.grew = map[*Func][2]bool{}, map[*Func][2]bool{}
	defer func() { c.group, c.groupValue, c.grew = nil, nil, nil }()
	cyclic := len(scc) > 1
	if !cyclic {
		for _, e := range scc[0].edges {
			if e == scc[0] {
				cyclic = true
			}
		}
	}
	inferred := false
	for _, n := range scc {
		if n.fn != nil && (n.fn.inferErrors || n.fn.inferUses) {
			inferred = true
		}
	}
	if cyclic {
		// One silent pass records the component's instantiations, so an
		// expanding cycle is rejected before the rows iterate (round-2 M1).
		c.silent++
		for _, n := range scc {
			c.checkNode(n)
		}
		c.silent--
		c.checkInstantiationCycles()
		// The fixpoint starts from empty rows (least fixpoint): the silent
		// pass ran before the cycle's instantiations were rejected.
		for _, n := range scc {
			if n.fn == nil || n.fn.Sig == nil {
				continue
			}
			n.fn.resetRows()
		}
		c.newSolutions()
		c.pinGroup(scc)
	}
	if cyclic && inferred {
		// A refused component is reported as is: its rows are not settled.
		settled := c.converge(scc)
		for _, n := range scc {
			if settled && n.fn != nil {
				c.settled[n.fn] = true
			}
		}
	}
	for _, n := range scc {
		c.checkNode(n)
	}
	c.reportGroup(scc)
	for _, n := range scc {
		if n.fn != nil {
			delete(c.settled, n.fn)
		}
	}
	c.finalComponent(scc)
}

// litFrame is a function literal, trailing block or go expression being
// typed in the reporting pass: the refused group members it refers to make
// it invalid, so nothing cascades from their pinned rows (D3). Which uses
// are value uses is decided before typing (refWalker, class H).
type litFrame struct {
	refs []*Func
}

// pushLit starts a literal frame; endLit ends it.
func (c *checker) pushLit() *litFrame {
	l := &litFrame{}
	c.lits = append(c.lits, l)
	return l
}

// endLit ends the innermost literal frame, l, and reports whether it refers
// to a refused member.
func (c *checker) endLit(l *litFrame) bool {
	c.lits = c.lits[:len(c.lits)-1]
	for _, f := range l.refs {
		if c.refused(f) {
			return true
		}
	}
	return false
}

// pinGroup applies class G to a cyclic component (rows.md, Capability rows
// 6): a member used as a value inside its own group has the rows it
// writes, and each row it does not write is pinned empty, as for an
// exported function. The value uses come from the lexical walk of the
// component's bodies (refWalker.values, class H), so they do not depend on
// typing: a use that is not a call is a value use of both rows, and a
// function literal, trailing block or go task that is a value is a value
// use of each row of the members it refers to that its own row would carry
// (not written, not called on the spot). A var on an initialization cycle
// is invalid, so its initializer adds no value use (D3). So no type in the
// group depends on a row still being inferred, call admission does not
// depend on the rows, and the rows grow monotonically to their least
// fixpoint. When the body of a pinned member needs more than the empty
// row, the member's declaration is S108 (reportGroup), never a chosen
// fixpoint.
//
// Prior art: OCaml's let rec is monomorphic inside its group; Haskell needs
// a signature for polymorphic recursion; TypeScript reports a name
// "referenced directly or indirectly in its own initializer"; Go and Rust
// require every signature. Each refuses the circular case instead of
// choosing a fixpoint.
func (c *checker) pinGroup(scc []*bodyNode) {
	for _, n := range scc {
		var owner any = n.fn
		if n.v != nil {
			if n.v.initCycle {
				continue
			}
			owner = n.v
		}
		for f, kinds := range c.valueRefs[owner] {
			if !c.group[f] {
				continue
			}
			uses := c.groupValue[f]
			uses[0] = uses[0] || kinds[0]
			uses[1] = uses[1] || kinds[1]
			c.groupValue[f] = uses
		}
	}
	var pin []*RowVar
	for _, n := range scc {
		if n.fn == nil {
			continue
		}
		uses := c.groupValue[n.fn]
		for k, v := range []*RowVar{n.fn.errVar, n.fn.useVar} {
			if v != nil && !v.final && uses[k] {
				pin = append(pin, v)
			}
		}
	}
	if len(pin) > 0 {
		c.types.final(pin)
		c.newSolutions()
	}
}

// groupRef handles a reference to f while f's component is checked: every
// enclosing literal frame takes f, and the reference is refused when f is
// (refused): in the reporting pass, every reference to a member whose
// pinned row its body exceeds is invalid, so nothing cascades from it
// (D3); the S108 is at the member's declaration (reportGroup).
func (c *checker) groupRef(f *Func) bool {
	if !c.group[f] || !f.inferErrors && !f.inferUses {
		return false
	}
	for _, l := range c.lits {
		if !slices.Contains(l.refs, f) {
			l.refs = append(l.refs, f)
		}
	}
	return c.refused(f)
}

// exceeded returns which of f's pinned rows (errors, capabilities) its body
// exceeds. The fixpoint decides it; only the reporting pass reads it.
func (c *checker) exceeded(f *Func) [2]bool {
	grew, uses := c.grew[f], c.groupValue[f]
	return [2]bool{grew[0] && uses[0], grew[1] && uses[1]}
}

// refused reports, in the reporting pass, whether f is refused by class G.
func (c *checker) refused(f *Func) bool {
	if c.silent > 0 {
		return false
	}
	ex := c.exceeded(f)
	return ex[0] || ex[1]
}

// reportGroup reports the class G S108 of each refused member of a
// component at its declaration, where the row is written: one diagnostic
// per function, at a position that does not depend on the order of the
// declarations (round 16).
func (c *checker) reportGroup(scc []*bodyNode) {
	for _, n := range scc {
		f := n.fn
		if f == nil || !c.refused(f) {
			continue
		}
		ex := c.exceeded(f)
		var row string
		switch {
		case ex[0] && ex[1]:
			row = fmt.Sprintf("write its error row and capability row: func %s() error[…] needs …", f.Name)
		case ex[0]:
			row = fmt.Sprintf("write its error row: func %s() error[…]", f.Name)
		default:
			row = fmt.Sprintf("write its capability row: func %s() needs …", f.Name)
		}
		saved := c.cur
		c.cur = f.file
		c.errorf(f.Decl.Name, CodeType, "%s is used as a value inside its own recursive group, directly or through a function literal or task; %s", f.Name, row)
		c.cur = saved
	}
}

// finalComponent binds the row variables of a component for good and
// discards its inference context (intern.go): the checker's instances and
// switch subjects whose ids depended on an unbound variable.
func (c *checker) finalComponent(scc []*bodyNode) {
	var vars []*RowVar
	for _, n := range scc {
		if n.fn == nil {
			continue
		}
		for _, v := range []*RowVar{n.fn.errVar, n.fn.useVar} {
			if v != nil && !v.final {
				vars = append(vars, v)
			}
		}
	}
	if len(vars) == 0 {
		return
	}
	c.types.final(vars)
	c.newSolutions()
}

// newSolutions discards the inference context (intern.go newSolutions)
// and the checker's instances and switch subjects in it.
func (c *checker) newSolutions() {
	c.types.newSolutions()
	clear(c.inferInstances)
	clear(c.inferSubjects)
}

// refuseRows reports the functions of a component whose inferred rows did
// not reach a fixpoint within the bound (S108: soundness by refusal).
func (c *checker) refuseRows(scc []*bodyNode) {
	for _, n := range scc {
		f := n.fn
		if f == nil || !f.inferErrors && !f.inferUses {
			continue
		}
		which := "error row"
		if !f.inferErrors {
			which = "capability row"
		}
		c.errorf(f.Decl.Name, CodeType, "cannot infer the %s of %s; write it", which, f.Name)
	}
}

// resetRows empties the solutions of a function's row variables that are
// not final (a least fixpoint starts from empty rows).
func (f *Func) resetRows() {
	for _, v := range []*RowVar{f.errVar, f.useVar} {
		if v != nil && !v.final {
			v.sol = nil
		}
	}
}

// converge joins the inferred rows of a cyclic component to their fixpoint,
// silently, with a dependency worklist: a body is checked again only when a
// body it references grew. Rows are finite sets that only grow (a body's
// rows are monotone in its callees' rows), so the worklist empties; no row
// is published to another component before it has converged.
// They are monotone because no type inside the group depends on a row still
// being inferred: a member used as a value inside its group, directly or
// through a function literal or go task that is a value, has its unwritten
// rows pinned empty (class G, pinGroup).
//
// The worklist is bounded whatever the rows do: 2·(bodies + edges)·(D + 1)
// checks, where D counts the distinct row elements (kind and interned id)
// the component has produced so far. A monotone solve changes each row at
// most D times and requeues each predecessor once per change, so it fits.
// Only an element seen for the first time raises D; an element removed and
// added again earns nothing, so an oscillating component reaches the bound
// (TestConvergeBoundOscillation). D is finite: the elements are types of
// the check, and expanding instantiation cycles are rejected before the
// solve (checkInstantiationCycles). Past the bound the component is S108
// "cannot infer the error row of f; write it" (refuseRows), never a guess.
// converge is the only iterating solver path of a component: the silent
// pass before it and the reporting pass after it check each body once.
//
// Prior art: Koka and Gleam infer per strongly connected component; the
// worklist is the standard chaotic-iteration solver of dataflow analysis.
func (c *checker) converge(scc []*bodyNode) bool {
	in := make(map[*bodyNode]bool, len(scc))
	for _, n := range scc {
		in[n] = true
	}
	preds := map[*bodyNode][]*bodyNode{}
	for _, n := range scc {
		for _, e := range n.edges {
			if in[e] {
				preds[e] = append(preds[e], n)
			}
		}
	}
	queue := append([]*bodyNode(nil), scc...)
	queued := make(map[*bodyNode]bool, len(scc))
	for _, n := range scc {
		queued[n] = true
	}
	edges := 0
	for _, ps := range preds {
		edges += len(ps)
	}
	// The bound: (bodies + edges) checks for each distinct row element
	// the component has produced, plus one round. Only an element seen
	// for the first time extends it; one removed and added again earns
	// nothing, so an oscillation ends in the refusal (round 14 O1).
	seen := map[[2]int32]bool{}
	budget := func() int { return 2 * (len(scc) + edges) * (len(seen) + 1) }
	check := c.checkNode
	if c.nodeCheck != nil {
		check = c.nodeCheck // the synthetic oscillation test
	}
	c.silent++
	checks := 0
	defer func() { c.convergeChecks += checks }()
	for len(queue) > 0 {
		if checks >= budget() {
			c.silent--
			c.refuseRows(scc)
			return false
		}
		checks++
		n := queue[0]
		queue = queue[1:]
		queued[n] = false
		changed := check(n)
		if n.fn != nil {
			for k, v := range []*RowVar{n.fn.errVar, n.fn.useVar} {
				if v != nil {
					for _, id := range v.sol.idsOf(c.types) {
						seen[[2]int32{int32(k), id}] = true
					}
				}
			}
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
	return true
}

// checkNode checks one body and reports whether its inferred rows grew.
func (c *checker) checkNode(n *bodyNode) bool {
	if n.v != nil {
		// A var's type carries its literal's inferred rows: when it
		// changes, the bodies that call the var are checked again.
		before := n.v.obj.Type
		c.checkPkgVar(n.v)
		after := n.v.obj.Type
		return before == nil || after == nil || !c.identical(before, after)
	}
	return c.checkFuncBody(n.fn)
}

// tarjan returns the strongly connected components in reverse topological
// order (each component after every component it reaches), iteratively, so
// a 20 000-function chain needs no deep recursion.
func tarjan(nodes []*bodyNode) [][]*bodyNode {
	var out [][]*bodyNode
	var stack []*bodyNode
	index := 0
	type frame struct {
		n *bodyNode
		i int
	}
	for _, root := range nodes {
		if root.index >= 0 {
			continue
		}
		work := []frame{{n: root}}
		root.index, root.low = index, index
		index++
		stack = append(stack, root)
		root.onStack = true
		for len(work) > 0 {
			top := &work[len(work)-1]
			if top.i < len(top.n.edges) {
				next := top.n.edges[top.i]
				top.i++
				if next.index < 0 {
					next.index, next.low = index, index
					index++
					stack = append(stack, next)
					next.onStack = true
					work = append(work, frame{n: next})
				} else if next.onStack && next.index < top.n.low {
					top.n.low = next.index
				}
				continue
			}
			n := top.n
			work = work[:len(work)-1]
			if len(work) > 0 && n.low < work[len(work)-1].n.low {
				work[len(work)-1].n.low = n.low
			}
			if n.low == n.index {
				var scc []*bodyNode
				for {
					m := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					m.onStack = false
					scc = append(scc, m)
					if m == n {
						break
					}
				}
				out = append(out, scc)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- function bodies

// funcCtx is the state of one top-level body: the current target, the
// lexical scope, the branch targets and the locals to report unused.
type funcCtx struct {
	t        *target
	scope    *Scope
	branches []branch
	locals   []*Var
	fn       *Func
	reach    *reachNode
	// labelUsed records the labels a break or continue names (Go: a label
	// defined and not used is an error).
	labelUsed map[string]bool
}

type branchKind int

const (
	brLoop branchKind = iota
	brSwitch
	brSelect
	brBarrier // a with, trailing or bare block: nothing crosses it
	brLabel
)

type branch struct {
	kind  branchKind
	label string
	used  *bool
}

func (c *checker) checkFuncBody(f *Func) bool {
	defer c.beginDeps(f)()
	// A method's receiver binds its type's parameters, which its body
	// instantiates like its own: they own its instantiations too, as
	// go/types canonicalizes receiver parameters (round-3 M1).
	owner := f.Sig.TParams
	if f.Recv != nil && len(f.Recv.TParams) > 0 {
		owner = append(append([]*TypeParam(nil), f.Recv.TParams...), f.Sig.TParams...)
	}
	defer c.monoOwner(owner)()
	saved := c.cur
	c.cur = f.file
	defer func() { c.cur = saved }()
	sig := f.Sig
	d := f.Decl
	name := "function " + f.Name
	if f.Recv != nil {
		name = "method " + f.Recv.Obj.Name + "." + f.Name
	}
	t := &target{name: name, results: sig.Results, resultKnown: true, isFunc: true, main: f.Name == "main" && f.Recv == nil}
	if f.inferErrors {
		t.errors = newRow()
	} else {
		t.declErrors = sig.Errors
	}
	if f.inferUses {
		t.uses = newRow()
	} else {
		t.declUses = sig.Uses
	}
	frame := newScope(c.pkg.Scope)
	frame.frame = true
	fc := &funcCtx{t: t, scope: frame, fn: f, reach: c.reachOf(f)}
	prev := c.fn
	c.fn = fc
	defer func() { c.fn = prev }()
	if d.Recv != nil && f.Recv != nil {
		field := d.Recv.List[0]
		if len(field.Names) > 0 {
			rt := Type(f.Recv)
			if len(f.Recv.TParams) > 0 {
				rt = c.instantiate(f.Recv, c.typeParamsAsArgs(f.Recv.TParams))
			}
			if f.Ptr {
				rt = &Pointer{Elem: rt}
			}
			v := &Var{Name: field.Names[0].Name, Type: rt, Pos: field.Names[0].Pos(), param: true, used: true}
			frame.insert(v.Name, v)
		}
	}
	for _, p := range sig.Params {
		if p.Name == "_" {
			continue
		}
		if prev, ok := frame.names[p.Name]; ok {
			if _, isVar := prev.(*Var); isVar && prev.(*Var).param {
				what := "parameter"
				if d.Recv != nil && len(d.Recv.List[0].Names) > 0 && d.Recv.List[0].Names[0].Name == p.Name {
					what = "receiver"
				}
				c.errorAt(p.Pos, p.Pos+syntax.Pos(len(p.Name)), CodeType, "%s is already a %s of %s", p.Name, what, f.Name)
				continue
			}
		}
		v := &Var{Name: p.Name, Type: p.Type, Pos: p.Pos, param: true, used: true}
		frame.insert(p.Name, v)
	}
	c.bindRequirements(frame, d.Type)
	c.stmtList(d.Body.List, false)
	if len(sig.Results) > 0 && !c.terminatesList(d.Body.List) {
		c.errorAt(d.Body.Rbrace, d.Body.Rbrace+1, CodeMissingRet, "%s can reach the end of its body without returning a value (rows.md, Totality 3)", name)
	}
	c.reportUnused(fc)
	changed := false
	// Inference writes the solved rows into the variables, never into the
	// signature (class F); a final variable keeps its solution. After a
	// component settled, its reporting pass reads the same solutions and
	// must compute no wider row (the invariant c.widened records).
	for _, p := range []struct {
		v   *RowVar
		row *Row
	}{{f.errVar, t.errors}, {f.useVar, t.uses}} {
		v := p.v
		if v == nil {
			continue
		}
		if v.final {
			if c.group[f] && c.silent > 0 {
				// Pinned empty by class G: record whether the body needs more.
				// The fixpoint decides; the reporting pass, which refuses the
				// value uses, only reads it.
				grew := c.grew[f]
				grew[v.Kind-KindErrors] = !p.row.Empty()
				c.grew[f] = grew
			}
			continue
		}
		if c.settled[f] && c.silent == 0 {
			if !p.row.subsetOf(c.types, v.sol) {
				c.widened = append(c.widened, f.Name)
			}
			continue
		}
		if !p.row.equal(c.types, v.sol) {
			changed = true
			v.sol = p.row
			// Ids resolved through the old solution no longer hold.
			c.newSolutions()
		}
	}
	return changed
}

func (c *checker) typeParamsAsArgs(tps []*TypeParam) []Type {
	out := make([]Type, len(tps))
	for i, tp := range tps {
		if tp.Kind != KindType {
			out[i] = &RowArg{Row: c.types.row(tp)}
			continue
		}
		out[i] = tp
	}
	return out
}

// bindRequirements enters the written capability-row names of a body into
// its frame (rows-and-constraints §7 amendment 3): a local of one of these
// names in the same frame is S104.
func (c *checker) bindRequirements(frame *Scope, ft *syntax.FuncType) {
	if ft == nil || ft.Needs == nil {
		return
	}
	for _, x := range ft.Needs.Names {
		var bare string
		switch x := x.(type) {
		case *syntax.Ident:
			bare = x.Name
		case *syntax.SelectorExpr:
			bare = x.Sel.Name
		}
		if bare == "" {
			continue
		}
		tn := c.capTypeName(x)
		if tn == nil {
			continue
		}
		frame.insert(bare, &capBinding{tn: tn, at: x.Pos()})
	}
}

// capTypeName finds the type name a capability-row entry denotes, without
// reporting (the signature already reported a bad entry).
func (c *checker) capTypeName(x syntax.Expr) *TypeName {
	switch x := x.(type) {
	case *syntax.Ident:
		o := c.lookupType(x.Name, nil)
		if tn, ok := o.(*TypeName); ok && isCapability(tn.Type) {
			return tn
		}
	case *syntax.SelectorExpr:
		if id, ok := x.X.(*syntax.Ident); ok {
			if pn, ok := c.lookupType(id.Name, nil).(*PkgName); ok {
				if tn, ok := pn.Pkg.Scope.names[x.Sel.Name].(*TypeName); ok && isCapability(tn.Type) {
					return tn
				}
			}
		}
	}
	return nil
}

func (c *checker) reportUnused(fc *funcCtx) {
	for _, v := range fc.locals {
		if v.used || v.Name == "_" {
			continue
		}
		if v.group != nil {
			continue
		}
		c.errorAt(v.Pos, v.Pos+syntax.Pos(len(v.Name)), CodeUnused, "%s is declared and not used", v.Name)
	}
}

// checkPkgVar checks a package-level var's initializer: a closed context,
// with no row (grammar.md, Declarations 6: package-level var is immutable).
func (c *checker) checkPkgVar(pv *pkgVar) {
	if pv.done || pv.checking {
		return
	}
	pv.checking = true
	defer func() { pv.checking = false }()
	defer c.beginDeps(pv)()
	// A silent check (a fixpoint round) does not count: the reporting
	// pass checks the initializer again against converged rows.
	if c.silent == 0 {
		pv.done = true
	}
	saved := c.cur
	c.cur = pv.file
	defer func() { c.cur = saved }()
	t := &target{name: "the initializer of " + pv.obj.Name, declErrors: newRow(), declUses: newRow(), resultKnown: true}
	prev := c.fn
	c.fn = &funcCtx{t: t, scope: newScope(c.pkg.Scope)}
	defer func() { c.fn = prev }()
	var declared Type
	if pv.spec.Type != nil {
		declared = c.typExpr(pv.spec.Type, nil)
	}
	switch {
	case len(pv.spec.Values) == 0:
		if declared != nil && !isInvalid(declared) && !defaultable(declared) {
			c.errorf(pv.spec.Names[pv.idx], CodeType, "var %s of type %s needs a value: %s has no zero value (types.md, Zero values 3)", pv.obj.Name, declared, declared)
		}
		pv.obj.Type = declared
	case len(pv.spec.Values) == len(pv.spec.Names):
		op := c.valueExpr(pv.spec.Values[pv.idx], declared)
		if declared != nil {
			c.assign(&op, declared, "var "+pv.obj.Name)
			pv.obj.Type = declared
		} else {
			pv.obj.Type = c.defaultOf(&op)
		}
	default:
		c.errorf(pv.spec, CodeType, "%d names and %d values", len(pv.spec.Names), len(pv.spec.Values))
		pv.obj.Type = invalid
	}
	// A var on a reported initialization cycle stays invalid: its
	// initializer is checked for its own errors, but no use of the var
	// cascades (D3).
	if pv.obj.Type == nil || pv.initCycle {
		pv.obj.Type = invalid
	}
}

func (c *checker) describeTarget() string {
	if c.fn == nil || c.fn.t == nil {
		return "this body"
	}
	return c.fn.t.name
}

var _ = fmt.Sprintf
