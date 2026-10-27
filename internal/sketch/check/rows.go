package check

import (
	"fmt"

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
		if t.withs[i] == nil || t.withs[i].Has(cap) {
			return
		}
	}
	if t.wire && t.internal.Has(cap) {
		t.charged = t.charged.with(cap)
		return
	}
	if t.declUses == nil {
		t.uses = t.uses.with(cap)
		return
	}
	if t.declUses.Has(cap) {
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
		t.errors = t.errors.union(row)
		return
	}
	extra := row.minus(t.declErrors)
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
// function whose rows are inferred and to a package var. A method or a
// function that spells its rows is read by its signature, so it adds no
// edge: one edge per referenced name, never one per same-named method
// (round-3 M5). Initialization cycles are found before, by discoverInit
// over resolved references.
func (c *checker) checkBodies() {
	var nodes []*bodyNode
	byFunc := map[*Func]*bodyNode{}
	byVar := map[*Var]*bodyNode{}
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
		byVar[v.obj] = n
	}
	c.bodyEdges = 0
	for _, n := range nodes {
		var root syntax.Node
		if n.fn != nil {
			root = n.fn.Decl.Body
		} else if n.v.spec != nil {
			if n.v.idx < len(n.v.spec.Values) {
				root = n.v.spec.Values[n.v.idx]
			} else if len(n.v.spec.Values) == 1 {
				root = n.v.spec.Values[0]
			}
		}
		if root == nil {
			continue
		}
		seen := map[*bodyNode]bool{}
		skip := map[*syntax.Ident]bool{}
		edge := func(to *bodyNode) {
			if to != nil && !seen[to] {
				seen[to] = true
				n.edges = append(n.edges, to)
				c.bodyEdges++
			}
		}
		syntax.Inspect(root, func(x syntax.Node) bool {
			switch x := x.(type) {
			case *syntax.SelectorExpr:
				skip[x.Sel] = true
			case *syntax.Ident:
				if skip[x] {
					return true
				}
				switch o := c.pkg.Scope.names[x.Name].(type) {
				case *Func:
					if o.inferErrors || o.inferUses {
						edge(byFunc[o])
					}
				case *Var:
					edge(byVar[o])
				}
			}
			return true
		})
	}
	c.discoverInit()
	for _, scc := range tarjan(nodes) {
		c.checkComponent(scc)
	}
	// Instantiation cycles through methods alone close outside any cyclic
	// component (methods add no edge); the reporting pass recorded them.
	c.checkInstantiationCycles()
	c.checkInitCycles()
}

// discoverInit finds the package's initialization cycles before any body
// is checked with diagnostics (round-2 M2, round-3 M5). It checks, silently,
// each package var's initializer and every function and method those reach
// through resolved references (a call, a value, a method value or
// expression), each once: the dependency closure of Go's initialization
// order, linear in its size. A cycle through a var is S108 and its vars
// become invalid, so no row diagnostic follows from them. The silent checks
// leave no row behind: the inferred rows restart empty.
func (c *checker) discoverInit() {
	if len(c.vars) == 0 {
		return
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
		if f.inferErrors {
			f.Sig.Errors = newRow()
		}
		if f.inferUses {
			f.Sig.Uses = newRow()
		}
	}
	c.checkInitCycles()
}

func (c *checker) checkComponent(scc []*bodyNode) {
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
			if n.fn.inferErrors {
				n.fn.Sig.Errors = newRow()
			}
			if n.fn.inferUses {
				n.fn.Sig.Uses = newRow()
			}
		}
	}
	if cyclic && inferred {
		c.converge(scc)
	}
	for _, n := range scc {
		c.checkNode(n)
	}
}

// converge joins the inferred rows of a cyclic component to their fixpoint,
// silently, with a dependency worklist: a body is checked again only when a
// body it references grew. Rows are finite sets that only grow (a body's
// rows are monotone in its callees' rows), so the worklist empties; no row
// is published to another component before it has converged.
//
// Prior art: Koka and Gleam infer per strongly connected component; the
// worklist is the standard chaotic-iteration solver of dataflow analysis.
func (c *checker) converge(scc []*bodyNode) {
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
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		queued[n] = false
		if !c.checkNode(n) {
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
}

// checkNode checks one body and reports whether its inferred rows grew.
func (c *checker) checkNode(n *bodyNode) bool {
	if n.v != nil {
		// A var's type carries its literal's inferred rows: when it
		// changes, the bodies that call the var are checked again.
		before := n.v.obj.Type
		c.checkPkgVar(n.v)
		after := n.v.obj.Type
		return before == nil || after == nil || !identical(before, after)
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
				rt = c.instantiate(f.Recv, typeParamsAsArgs(f.Recv.TParams))
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
	if f.inferErrors {
		if !t.errors.equal(sig.Errors) {
			changed = true
		}
		sig.Errors = t.errors
	}
	if f.inferUses {
		if !t.uses.equal(sig.Uses) {
			changed = true
		}
		sig.Uses = t.uses
	}
	return changed
}

func typeParamsAsArgs(tps []*TypeParam) []Type {
	out := make([]Type, len(tps))
	for i, tp := range tps {
		if tp.Kind != KindType {
			out[i] = &RowArg{Row: newRow(tp)}
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
