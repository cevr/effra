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
// resolved objects the resolving pass recorded (resolveBodies), never names
// looked up in package scope: a local, a parameter or a receiver that
// shadows a package function adds no edge (round 14 M1; go/types
// initorder.go walks objects too). A method or a function that spells its
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
	c.resolveBodies()
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
	// Instantiation cycles through methods alone close outside any cyclic
	// component (methods add no edge); the reporting pass recorded them.
	c.checkInstantiationCycles()
	c.checkInitCycles()
}

// resolveBodies resolves every body once, silently, before any body is
// checked with diagnostics: each package var's initializer, then every
// function and method. Its references are the resolved objects (c.refs,
// recordDep), from which checkBodies builds the body graph (round 14 M1),
// and the dependency graph of Go's initialization order, from which the
// package's initialization cycles are found (round-2 M2, round-3 M5): a
// cycle through a var is S108 and its vars become invalid, so no row
// diagnostic follows from them. Linear in the source. The silent checks
// leave no row behind: the inferred rows restart empty.
//
// A body that names no package var and no function with an inferred row
// (any identifier, shadowed or not) has no edge, so it is resolved only
// when a var initializer reaches it (the initialization order's closure);
// every edge is a resolved identifier with its object's name.
func (c *checker) resolveBodies() {
	c.silent++
	c.init.tentative = true
	c.refs = map[any]map[any]bool{}
	visited := map[*Func]bool{}
	var queue []*Func
	enqueue := func(f *Func) {
		if !visited[f] && f.Decl != nil && f.Decl.Body != nil && f.Sig != nil {
			visited[f] = true
			queue = append(queue, f)
		}
	}
	reached := func(owner any) {
		for _, d := range sortedDeps(c.refs[owner]) {
			if f, ok := d.(*Func); ok {
				enqueue(f)
			}
		}
		for _, d := range sortedDeps(c.init.deps[owner]) {
			if f, ok := d.(*Func); ok {
				enqueue(f)
			}
		}
	}
	for _, pv := range c.vars {
		c.checkPkgVar(pv)
		reached(pv)
	}
	for _, f := range c.funcs {
		if c.namesInferred(f) {
			enqueue(f)
		}
	}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		c.checkFuncBody(f)
		if len(c.vars) > 0 {
			reached(f)
		}
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

// namesInferred reports whether f's body has an identifier spelled as a
// package var or a package function with an inferred row: only such a
// body can have an edge of the body graph. A selector's name is a field
// or method, never a package object of this package.
func (c *checker) namesInferred(f *Func) bool {
	if f.Decl == nil || f.Decl.Body == nil || f.Sig == nil {
		return false
	}
	found := false
	sel := map[*syntax.Ident]bool{}
	syntax.Inspect(f.Decl.Body, func(x syntax.Node) bool {
		if found {
			return false
		}
		switch x := x.(type) {
		case *syntax.SelectorExpr:
			sel[x.Sel] = true
		case *syntax.Ident:
			if sel[x] {
				return true
			}
			switch o := c.pkg.Scope.names[x.Name].(type) {
			case *Func:
				found = o.inferErrors || o.inferUses
			case *Var:
				found = true
			}
		}
		return true
	})
	return found
}

func (c *checker) checkComponent(scc []*bodyNode) {
	// The group whose rows are being inferred (class G, pinGroup).
	c.group = map[*Func]bool{}
	for _, n := range scc {
		if n.fn != nil {
			c.group[n.fn] = true
		}
	}
	c.groupValue, c.grew = map[*Func][2]valueUse{}, map[*Func][2]bool{}
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
	for _, n := range scc {
		if n.fn != nil {
			delete(c.settled, n.fn)
		}
	}
	c.finalComponent(scc)
}

// valueUse is a value use of a group member: an identifier that is not a
// call, or a function literal or go expression that refers to the member
// and is itself a value. file is the index of its file, so the first one in
// source order is well defined.
type valueUse struct {
	file int
	at   syntax.Node
}

// A litMode says, for one row kind, how a function literal, trailing block
// or go expression passes on the rows of the group members it refers to.
type litMode int

const (
	// litPass: the rows flow into the enclosing body, as for a call (a
	// literal called on the spot, the capabilities of a go expression).
	litPass litMode = iota
	// litValue: the rows become part of a value's type (the literal's
	// inferred row, a task's error row): a value use of each member.
	litValue
	// litStop: the row is written or comes from the expected function
	// type, so it depends on no member's row.
	litStop
)

// litModeOf is a literal row's mode: litStop when the row is declared
// (written, or from the expected function type), else the given mode.
func litModeOf(declared *Row, value litMode) litMode {
	if declared != nil {
		return litStop
	}
	return value
}

// litFrame is a function literal, trailing block or go expression being
// checked: the group members it refers to, per row kind, while it is a
// value (class G, round 14 O1).
type litFrame struct {
	at   syntax.Node
	mode [2]litMode
	refs [2][]*Func
}

// pushLit starts a literal frame; endLit ends it.
func (c *checker) pushLit(at syntax.Node, errors, uses litMode) *litFrame {
	l := &litFrame{at: at, mode: [2]litMode{errors, uses}}
	c.lits = append(c.lits, l)
	return l
}

// endLit ends the innermost literal frame, l: each member it refers to
// through a value row has a value use at l. It reports whether the literal
// is refused (the first value use of a member whose pinned row its body
// exceeds).
func (c *checker) endLit(l *litFrame) bool {
	c.lits = c.lits[:len(c.lits)-1]
	var members []*Func
	seen := map[*Func]bool{}
	for k := range 2 {
		for _, f := range l.refs[k] {
			c.noteValueUse(f, k, l.at)
			if !seen[f] {
				seen[f] = true
				members = append(members, f)
			}
		}
	}
	refused := false
	for _, f := range members {
		if c.groupRefused(f, l.at) {
			refused = true
		}
	}
	return refused
}

// noteValueUse records at as a value use of f's row of kind k (0 errors,
// 1 capabilities) when it is the first in source order.
func (c *checker) noteValueUse(f *Func, k int, at syntax.Node) {
	file := -1
	if c.cur != nil {
		file = c.cur.index
	}
	uses := c.groupValue[f]
	first := uses[k]
	if first.at == nil || file < first.file || file == first.file && at.Pos() < first.at.Pos() {
		uses[k] = valueUse{file, at}
		c.groupValue[f] = uses
	}
}

// pinGroup applies class G to a cyclic component after its silent pass has
// recorded the value uses (rows.md, Capability rows 6): a member used as a
// value inside its own group has the rows it writes, and each row it does
// not write is pinned empty, as for an exported function. A function
// literal or go expression that is a value and refers to a member pins the
// member's rows that its own inferred rows would carry. So no type in the
// group depends on a row still being inferred, call admission does not
// depend on the rows, and the rows grow monotonically to their least
// fixpoint. When the body of a pinned member needs more than the empty
// row, its first value use is S108 (groupRefused), never a chosen fixpoint.
//
// Prior art: OCaml's let rec is monomorphic inside its group; Haskell needs
// a signature for polymorphic recursion; TypeScript reports a name
// "referenced directly or indirectly in its own initializer"; Go and Rust
// require every signature. Each refuses the circular case instead of
// choosing a fixpoint.
func (c *checker) pinGroup(scc []*bodyNode) {
	var pin []*RowVar
	for _, n := range scc {
		if n.fn == nil {
			continue
		}
		uses := c.groupValue[n.fn]
		for k, v := range []*RowVar{n.fn.errVar, n.fn.useVar} {
			if v != nil && !v.final && uses[k].at != nil {
				pin = append(pin, v)
			}
		}
	}
	if len(pin) > 0 {
		c.types.final(pin)
		c.newSolutions()
	}
}

// groupRef handles a reference x to f while f's component is checked
// (class G): every enclosing literal frame that is a value takes f, up to
// the first frame whose row of that kind is written, and a use that is not
// a call (value) is a value use of both of f's rows. It reports whether
// the reference is refused: in the reporting pass, every reference to a
// member whose pinned row its body exceeds is invalid, so nothing cascades
// from it (D3), and the member's first value use carries the S108.
func (c *checker) groupRef(f *Func, x *syntax.Ident, value bool) bool {
	if !c.group[f] || !f.inferErrors && !f.inferUses {
		return false
	}
	if c.inertGroup {
		_, exceeded := c.exceeded(f)
		return c.silent == 0 && exceeded
	}
	for k := range 2 {
		for i := len(c.lits) - 1; i >= 0; i-- {
			l := c.lits[i]
			if l.mode[k] == litStop {
				break
			}
			if l.mode[k] == litValue && !slices.Contains(l.refs[k], f) {
				l.refs[k] = append(l.refs[k], f)
			}
		}
	}
	if value {
		c.noteValueUse(f, 0, x)
		c.noteValueUse(f, 1, x)
	}
	if c.groupRefused(f, x) {
		return true
	}
	_, exceeded := c.exceeded(f)
	return c.silent == 0 && exceeded
}

// exceeded returns the first value use of f among its pinned rows that its
// body exceeds, and whether there is one. The fixpoint decides it; only
// the reporting pass reads it.
func (c *checker) exceeded(f *Func) (valueUse, bool) {
	grew, uses := c.grew[f], c.groupValue[f]
	var first valueUse
	found := false
	for k := range 2 {
		u := uses[k]
		if !grew[k] || u.at == nil {
			continue
		}
		if !found || u.file < first.file || u.file == first.file && u.at.Pos() < first.at.Pos() {
			first, found = u, true
		}
	}
	return first, found
}

// groupRefused reports the S108 of f at at when at is f's first value use
// among the pinned rows its body exceeds, in the reporting pass.
func (c *checker) groupRefused(f *Func, at syntax.Node) bool {
	if c.silent > 0 {
		return false
	}
	first, ok := c.exceeded(f)
	if !ok || first.at != at {
		return false
	}
	grew, uses := c.grew[f], c.groupValue[f]
	errs, caps := grew[0] && uses[0].at != nil, grew[1] && uses[1].at != nil
	var row string
	switch {
	case errs && caps:
		row = fmt.Sprintf("write its error row and capability row: func %s() error[…] needs …", f.Name)
	case errs:
		row = fmt.Sprintf("write its error row: func %s() error[…]", f.Name)
	default:
		row = fmt.Sprintf("write its capability row: func %s() needs …", f.Name)
	}
	switch at.(type) {
	case *syntax.Ident:
		c.errorf(at, CodeType, "%s is used as a value inside its own recursive group; %s", f.Name, row)
	case *syntax.GoExpr:
		c.errorf(at, CodeType, "%s is called by this go expression, whose task is a value inside %s's own recursive group; %s", f.Name, f.Name, row)
	default:
		c.errorf(at, CodeType, "%s is referred to by this function literal, a value inside %s's own recursive group; %s", f.Name, f.Name, row)
	}
	return true
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

// rowSize is the number of elements in a body's inferred rows so far.
func (n *bodyNode) rowSize() int {
	if n.fn == nil {
		return 0
	}
	size := 0
	for _, v := range []*RowVar{n.fn.errVar, n.fn.useVar} {
		if v != nil {
			size += len(v.sol.Elems())
		}
	}
	return size
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
// being inferred: a member used as a value inside its group has its
// unwritten rows pinned empty (class G, pinGroup). As a safety net the worklist is
// bounded by the component and its rows so far: each check that grows a row
// adds to the bound, so a monotone fixpoint always fits, and a worklist
// past it refuses the rows (S108) instead of guessing.
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
	c.silent++
	checks, budget, growth := 0, 4*(len(scc)+1), 0
	lastGrowth := map[*bodyNode]int{}
	for len(queue) > 0 {
		if checks > budget {
			c.silent--
			c.refuseRows(scc)
			return false
		}
		checks++
		n := queue[0]
		queue = queue[1:]
		queued[n] = false
		before := n.rowSize()
		if !c.checkNode(n) {
			continue
		}
		// Only growth extends the bound: each new element pays for one
		// more check of each body that references n. A row that changes
		// without growing extends nothing, so a cycle of changes ends in
		// the refusal.
		if grown := n.rowSize() - before; grown > 0 {
			budget += 2 * grown * (len(preds[n]) + 1)
			growth += grown
		} else if n.v != nil && growth > lastGrowth[n] {
			// A var's type follows the rows of the literals it holds: it
			// may change once per growth elsewhere in the component.
			budget += 2 * (len(preds[n]) + 1)
			lastGrowth[n] = growth
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
	// A var on an initialization cycle is invalid: no class G value use
	// follows from its initializer (D3).
	if pv.initCycle {
		prev := c.inertGroup
		c.inertGroup = true
		defer func() { c.inertGroup = prev }()
	}
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
