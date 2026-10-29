package check

import (
	"go/constant"

	"ergo.local/prototype/internal/sketch/syntax"
)

// The Info contract (check-v1 spec, "Interface to lower-v1"). The lowering
// reads only Check's result and the syntax tree: it never re-derives a
// type, a row or a resolution, and a package with any diagnostic is not
// lowered. In a package with no diagnostic:
//
//  1. Complete: every expression in a body or a package let has a Types
//     entry, every identifier a Defs or Uses entry, every call a Calls
//     entry, every pattern a Patterns entry, every match and catch a
//     Matches entry, every for-in a Loops entry and every with a Withs
//     entry, whose Entries hold its named wires' providers with their
//     conversions (TestInfoComplete). A package name before a dot and a
//     built-in callee are no values and have no Types entry. An inline
//     wire value's entries are stage E's (196x).
//  2. Final: no type in Info is invalid or holds a row variable; a type
//     parameter appears only inside its own generic declaration. The one
//     exception is go/types': the name in an explicit instantiation
//     Fn[int] keeps its generic type (round-4 M3).
//  3. Proved: a Match is total, so the lowering adds no default arm except
//     a catch's defect re-panic (rows.md, Catch 6).
//  4. Deterministic: every slice in Package and in Info's values is in
//     source order or by name, never in map order.
//
// Each stage of check-v1 populates the entries of the forms it checks;
// stage A populates every form the merge-round checker types.
//
// Tree names: the tree keeps parse-v1's desugared nodes (spec D-A8, Q8).
// A syntax.VarDecl is a `let`; a SwitchStmt is a `match` on literal
// patterns; a TypeSwitchStmt is any other `match`; a CallExpr whose Fun is
// the identifier `with` is a `with` block; a WireDecl is a package-level
// wire `let`; a RangeStmt is a `for … in`; a ForStmt is a `for` on a
// condition.

// Package is a checked package.
type Package struct {
	Path  string
	Name  string
	Scope *Scope
	// Wires is the wire namespace: wires are named only by with and by
	// wire embedding, so they do not collide with types (fixture with-name).
	Wires map[string]*Wire
	// Funcs is every function, extern included, in source order;
	// same-named functions (196m rule 3) keep separate *Func.
	Funcs []*Func
	// Types is every declared type in source order: records, operation
	// types, unions and defined types.
	Types []*TypeName
	// Tests is every test block with its parameters (stage E, 196l).
	Tests []*Test
	// Info is the checker's result for the lowering.
	Info *Info
	// ErrorTypes are the struct types this package names in an error row
	// (types.md, Error types 1); returning a value of one is a failure.
	ErrorTypes map[*Named]bool

	qualify bool // print its types with the package name
	// bindWork counts the steps that resolved the named wires' bindings
	// (finishInfo): wires, entries and the nodes and entries read.
	bindWork int
	files    []*fileInfo
}

// Info records the checker's results for the lowering (#197, #50).
type Info struct {
	// Types is every expression's final type; a literal carries its one
	// type (196u). A signed literal is one literal: `-128` where int8 is
	// expected has Types and Values entries (int8, -128) on the signed
	// expression, and the literal 128 under the sign, with any parentheses
	// around it, has none, so the lowering emits int8(-128) and never
	// int8(128).
	Types map[syntax.Expr]Type
	// Values is every constant expression's exact value, already checked
	// against its type.
	Values map[syntax.Expr]constant.Value
	// Defs is every declaring identifier: declarations, parameters, type
	// parameters, let, pattern, as, case err and loop bindings.
	Defs map[*syntax.Ident]Object
	// Uses is every referring identifier.
	Uses map[*syntax.Ident]Object
	// Calls is every call.
	Calls map[*syntax.CallExpr]*Call
	// Implicit is every implicit conversion at an assignment site.
	Implicit map[syntax.Expr]Conversion
	// Patterns is every pattern of a match, catch, let and for.
	Patterns map[syntax.Expr]*Pattern
	// Matches is every match (SwitchStmt, TypeSwitchStmt) and catch
	// (CatchExpr).
	Matches map[syntax.Node]*Match
	// Tails is every block-final expression statement whose value is the
	// block's value, with that type; an absent statement is discarded.
	Tails map[syntax.Stmt]Type
	// Literals is every function literal and trailing block, with its
	// final rows (196a).
	Literals map[syntax.Node]*Signature
	// Loops is every for-in.
	Loops map[*syntax.RangeStmt]*Loop
	// Withs is every with site.
	Withs map[*syntax.CallExpr]*With
}

func newInfo() *Info {
	return &Info{
		Types:    map[syntax.Expr]Type{},
		Values:   map[syntax.Expr]constant.Value{},
		Defs:     map[*syntax.Ident]Object{},
		Uses:     map[*syntax.Ident]Object{},
		Calls:    map[*syntax.CallExpr]*Call{},
		Implicit: map[syntax.Expr]Conversion{},
		Patterns: map[syntax.Expr]*Pattern{},
		Matches:  map[syntax.Node]*Match{},
		Tails:    map[syntax.Stmt]Type{},
		Literals: map[syntax.Node]*Signature{},
		Loops:    map[*syntax.RangeStmt]*Loop{},
		Withs:    map[*syntax.CallExpr]*With{},
	}
}

// CallKind is what a call calls.
type CallKind int

const (
	// CallFunc calls a declared function, Func; a dot call x.f(…) (196m)
	// has Recv, lowered as the first argument.
	CallFunc CallKind = iota
	// CallOp calls an operation of an operation-type value: Op on Recv.
	CallOp
	// CallProviderOp calls an operation of a capability, Op, on the
	// provider the nearest with binds (types.md, Capabilities 2).
	CallProviderOp
	// CallFuncValue calls a function value.
	CallFuncValue
	// CallConversion converts its one argument to Type.
	CallConversion
	// CallLen is the len built-in.
	CallLen
	// CallWith is a with block; Info.Withs holds its site.
	CallWith
	// CallPrelude is a prelude dot call, xs.Append(x) or m.Delete(k), named
	// by Name, on Recv (stage D owns the prelude's dot calls, D-D6).
	CallPrelude
)

// Call is one call site.
type Call struct {
	Kind CallKind
	// Func is the called function (CallFunc).
	Func *Func
	// Op is the called operation of an operation type or capability
	// (CallOp, CallProviderOp).
	Op *Method
	// Name names a CallPrelude's library function.
	Name string
	// Recv is the receiver of a dot call, an operation call or a prelude
	// call; nil otherwise.
	Recv syntax.Expr
	// TypeArgs are the type arguments of a generic function, written or
	// inferred, in its parameter order.
	TypeArgs []Type
	// Type is a conversion's target type.
	Type Type
	// Fails and Needs are the callee's error and capability rows at this
	// site, after substitution.
	Fails, Needs *Row
}

// ConversionKind is an implicit conversion's kind.
type ConversionKind int

const (
	// ConvVariant puts a variant into its union.
	ConvVariant ConversionKind = iota + 1
	// ConvOperations puts a value into an operation type; Table maps each
	// operation name to the function that implements it.
	ConvOperations
	// ConvRow puts an error row's value into a union.
	ConvRow
)

// Conversion is an implicit conversion at an assignment site: From's value
// becomes To.
type Conversion struct {
	Kind     ConversionKind
	From, To Type
	Table    map[string]*Func
}

// PatternKind is a pattern's form.
type PatternKind int

const (
	PatWildcard PatternKind = iota + 1 // _
	PatBind                            // a name: Var
	PatTypeTest                        // a type: Type
	PatRecord                          // T{F: p, …}: Type and Fields
	PatConst                           // a literal: Value of Type
	PatTuple                           // (p, q): Elems
	PatAs                              // p as x: Elems[0] and Var
)

// Pattern is one pattern node.
type Pattern struct {
	Kind   PatternKind
	Var    *Var
	Type   Type
	Value  constant.Value
	Fields []FieldPattern
	Elems  []*Pattern
}

// FieldPattern is one field of a record pattern.
type FieldPattern struct {
	Name    string
	Pattern *Pattern
}

// Match is one match or catch.
type Match struct {
	// Subjects are the subject types: one, or a tuple's elements; a catch's
	// subjects are its error row's elements.
	Subjects []Type
	// Arms counts the arms, a case _ included.
	Arms int
	// Result is the match's value type, nil for a statement.
	Result Type
	// Partial marks try … catch: the failures no arm catches escape.
	Partial bool
}

// LoopDomain is what a for-in iterates.
type LoopDomain int

const (
	LoopSlice LoopDomain = iota + 1
	LoopMap
	LoopRange
	LoopString
	LoopInt
)

// Loop is one for-in.
type Loop struct {
	Domain LoopDomain
	// Elem is the element type, Pos the position type (an index or a Map
	// key); Pos is nil when there is none.
	Elem, Pos Type
	// Bindings are the loop's binding objects, in source order.
	Bindings []*Var
}

// With is one with site.
type With struct {
	// Wires are the named wires bound, in nesting order; an inline wire
	// value's entries are stage E's (196x).
	Wires []*Wire
	// Discharged are the capability types the site binds, by name.
	Discharged []Type
	// Entries holds, for a with of a named wire, exactly one entry per
	// capability: Entries[i] is the provider entry that binds
	// Discharged[i] once replace and embedding are resolved (rows.md,
	// Wire 7). An entry of several capabilities appears at each of its
	// capabilities; its conversion for Discharged[i] is the one at that
	// capability's index in its Caps. Empty for an inline wire value
	// (stage E, 196x).
	Entries []*WireEntry
	// Overridden are the bindings a replace overrode, outermost replace
	// first, so a replaced provider stays traceable: each names the
	// capability, the overridden entry and the entry that replaced it for
	// that capability. The overridden entry is not built for that
	// capability; an entry of several capabilities that still binds another
	// one is in Entries for it and is built for it.
	Overridden []Override
}

// Override is a binding a replace overrode (With.Overridden): Entry no
// longer binds Cap, By does.
type Override struct {
	Cap       Type
	Entry, By *WireEntry
}

// WireEntry is one entry of a named wire, as written: a provider for
// capabilities, or an embedded wire.
type WireEntry struct {
	// Replace marks a `replace Cap: provider` entry.
	Replace bool
	// Caps are the capabilities the entry provides, in key order; none for
	// an embedding.
	Caps []Type
	// Provider is the provider expression; nil for an embedding.
	Provider syntax.Expr
	// Conversions put the provider into each capability, in Caps order: a
	// ConvOperations table, or the zero Conversion when the provider's type
	// is the capability itself. Implicit holds the same conversion at
	// Provider when the entry has one capability.
	Conversions []Conversion
	// Embedded is the embedded wire; nil for a provider entry.
	Embedded *Wire
}

// Test is a test block (stage E, 196l).
type Test struct {
	Name   string
	Decl   *syntax.TestDecl
	Params []*Var
}

// A type parameter is an Object, so Defs records its declaring name.
func (o *TypeParam) name() string    { return o.Name }
func (o *TypeParam) pos() syntax.Pos { return o.Pos }

// def records a declaring identifier (Info.Defs).
func (c *checker) def(id *syntax.Ident, o Object) {
	if c.silent == 0 && id != nil && id.Name != "_" {
		c.pkg.Info.Defs[id] = o
	}
}

// callInfo is x's Calls entry, made on first use; under silent checking it
// is a scratch entry that Info never sees.
func (c *checker) callInfo(x *syntax.CallExpr) *Call {
	if c.silent > 0 {
		return &Call{}
	}
	call := c.pkg.Info.Calls[x]
	if call == nil {
		call = &Call{}
		c.pkg.Info.Calls[x] = call
	}
	return call
}

// recordCall fills x's Calls entry after its callee resolved: the kind,
// what it calls and the receiver of a dot call. The rows came from result.
func (c *checker) recordCall(x *syntax.CallExpr, fun operand) {
	call := c.callInfo(x)
	sel, _ := unparen(x.Fun).(*syntax.SelectorExpr)
	if ix, ok := unparen(x.Fun).(*syntax.IndexExpr); ok {
		sel, _ = unparen(ix.X).(*syntax.SelectorExpr)
	}
	recv := func() syntax.Expr {
		if sel == nil {
			return nil
		}
		if id, ok := sel.X.(*syntax.Ident); ok {
			if _, isPkg := c.pkg.Info.Uses[id].(*PkgName); isPkg {
				return nil // a qualified name pkg.f, not a dot call
			}
		}
		return sel.X
	}
	switch {
	case fun.mode == modeType:
		call.Kind, call.Type = CallConversion, fun.typ
	case fun.mode == modeBuiltin && fun.recv != nil:
		call.Kind, call.Name, call.Recv = CallPrelude, fun.obj.name(), recv()
	case fun.mode == modeBuiltin:
		call.Kind = CallLen
	case fun.mode == modeCapFn:
		call.Kind, call.Op = CallProviderOp, fun.method
	case fun.fn != nil:
		call.Kind, call.Func, call.Recv = CallFunc, fun.fn, recv()
	case fun.method != nil:
		call.Kind, call.Op, call.Recv = CallOp, fun.method, recv()
	default:
		call.Kind = CallFuncValue
	}
}

// recordWith records a with site: the named wire and what it binds.
func (c *checker) recordWith(x *syntax.CallExpr, binds *capSet) {
	if c.silent > 0 {
		return
	}
	w := &With{}
	if len(x.Args) == 1 {
		if id, ok := unparen(x.Args[0]).(*syntax.Ident); ok && c.pkg.Wires[id.Name] != nil {
			w.Wires = []*Wire{c.pkg.Wires[id.Name]}
		}
	}
	if binds != nil {
		var walk func(n *capNode)
		walk = func(n *capNode) {
			for n != nil {
				walk(n.left)
				w.Discharged = append(w.Discharged, n.t)
				n = n.right
			}
		}
		walk(binds.root)
	}
	c.pkg.Info.Withs[x] = w
	c.callInfo(x).Kind = CallWith
}

// finishInfo resolves the rows Info holds once every body is final, so no
// row variable remains (invariant 2).
func (c *checker) finishInfo() {
	for _, call := range c.pkg.Info.Calls {
		if call.Fails != nil {
			call.Fails = c.types.resolve(call.Fails)
		}
		if call.Needs != nil {
			call.Needs = c.types.resolve(call.Needs)
		}
	}
	for p, name := range c.asNames {
		p.Var, _ = c.pkg.Info.Defs[name].(*Var)
	}
	// A function's type keeps its row variables (inference writes the
	// solution beside them); Info holds the solved rows (invariant 2).
	memo := map[Type]Type{}
	for x, t := range c.pkg.Info.Types {
		if r := c.finalType(t, memo); r != t {
			c.pkg.Info.Types[x] = r
		}
	}
	// A with's bindings, final once every wire body is checked: the entry
	// that binds each discharged capability, and what a replace overrode.
	b := &binder{c: c, nodes: map[*Wire]*bindNode{}, results: map[*bindNode]*bindResult{}}
	for _, w := range c.pkg.Info.Withs {
		w.Entries, w.Overridden = nil, nil
		if len(w.Wires) != 1 {
			continue
		}
		r := b.result(b.node(w.Wires[0], map[*Wire]bool{}))
		for _, d := range w.Discharged {
			w.Entries = append(w.Entries, r.bound[c.types.id(d)])
		}
		w.Overridden = r.overridden
	}
	c.pkg.bindWork = b.work
}

// The bindings of named wires (rows.md, Wire 7), resolved once per
// package (round-3 review M1). Each wire gets one bindNode, made once:
// its own entries and the nodes of its embedded wires. A wire with no own
// entry and one embedding shares its embedding's node, and an empty node
// is dropped, so a chain of wires that only embed shares one node. A with
// reads the result of its wire's node, made once per node: the entry that
// binds each capability and the overrides. work counts the wires and the
// entries each step reads; the result of a node is made by one walk of the
// nodes it reaches, each node once, so work is at most twice the wires
// and entries plus the nodes and entries the results read. In a clean
// package every own entry a walk reads is in its result (it binds, or a
// replace overrode it) and a node of several embeddings joins disjoint
// capabilities (a capability two embeddings bind is S111 unless replaced),
// so the results' reading is bounded by their size (TestWireInfoLinear).
type binder struct {
	c       *checker
	nodes   map[*Wire]*bindNode
	results map[*bindNode]*bindResult
	work    int
}

// bindNode is a wire's own entries, by capability, in source order, and
// the distinct non-empty nodes of its embedded wires, in source order.
type bindNode struct {
	own   []ownBinding
	bases []*bindNode
}

type ownBinding struct {
	id    int32
	cap   Type
	entry *WireEntry
}

// bindResult is a node's resolved bindings: the provider entry for each
// capability, keyed by its interned id, and the overrides.
type bindResult struct {
	bound      map[int32]*WireEntry
	overridden []Override
}

var emptyBind = &bindNode{}

// node makes w's node once. Embedding cycles are S108 (rows.md, Wire 5),
// so a wire already on the walk adds nothing.
func (b *binder) node(w *Wire, walk map[*Wire]bool) *bindNode {
	if n, ok := b.nodes[w]; ok {
		return n
	}
	if walk[w] {
		return emptyBind
	}
	walk[w] = true
	b.work++ // the wire
	n := &bindNode{}
	seen := map[*bindNode]bool{}
	for _, e := range w.Entries {
		b.work++ // the entry
		if e.Embedded != nil {
			if base := b.node(e.Embedded, walk); base != emptyBind && !seen[base] {
				seen[base] = true
				n.bases = append(n.bases, base)
			}
			continue
		}
		for _, cap := range e.Caps {
			n.own = append(n.own, ownBinding{id: b.c.types.id(cap), cap: cap, entry: e})
		}
	}
	delete(walk, w)
	switch {
	case len(n.own) == 0 && len(n.bases) == 0:
		n = emptyBind
	case len(n.own) == 0 && len(n.bases) == 1:
		n = n.bases[0] // shared: the wire binds what its embedding binds
	}
	b.nodes[w] = n
	return n
}

// result resolves a node once, in one depth-first walk of the nodes it
// reaches, each once, a node's own entries before its bases'. The first
// entry the walk meets for a capability is the outermost on its path, so
// it binds; a later entry for it is overridden, for that capability, by
// the nearest entry for it on the path above.
func (b *binder) result(root *bindNode) *bindResult {
	if r, ok := b.results[root]; ok {
		return r
	}
	r := &bindResult{bound: map[int32]*WireEntry{}}
	visited := map[*bindNode]bool{}
	above := map[int32][]*WireEntry{} // the entries for each capability on the path
	var walk func(n *bindNode)
	walk = func(n *bindNode) {
		if visited[n] {
			return
		}
		visited[n] = true
		b.work++ // the node
		for _, o := range n.own {
			b.work++ // the entry
			if stack := above[o.id]; len(stack) > 0 {
				r.overridden = append(r.overridden, Override{Cap: o.cap, Entry: o.entry, By: stack[len(stack)-1]})
			} else if r.bound[o.id] == nil {
				r.bound[o.id] = o.entry
			}
			above[o.id] = append(above[o.id], o.entry)
		}
		for _, base := range n.bases {
			walk(base)
		}
		for _, o := range n.own {
			above[o.id] = above[o.id][:len(above[o.id])-1]
		}
	}
	walk(root)
	b.results[root] = r
	return r
}

// finalType is t with every row variable in a function type's rows
// replaced by its solution; t itself when it holds none. Named types are
// not entered: their bodies are declared, with written rows.
func (c *checker) finalType(t Type, memo map[Type]Type) Type {
	if r, ok := memo[t]; ok {
		return r
	}
	out := t
	switch t := t.(type) {
	case *Signature:
		if len(t.TParams) > 0 {
			break // a generic function's own type (an explicit instantiation's name)
		}
		s := *t
		changed := false
		s.Params = make([]*Var, len(t.Params))
		for i, p := range t.Params {
			s.Params[i] = p
			if pt := c.finalType(p.Type, memo); pt != p.Type {
				q := *p
				q.Type = pt
				s.Params[i] = &q
				changed = true
			}
		}
		s.Results = make([]Type, len(t.Results))
		for i, r := range t.Results {
			s.Results[i] = c.finalType(r, memo)
			changed = changed || s.Results[i] != r
		}
		if e := c.types.resolve(t.Errors); e != t.Errors {
			s.Errors, changed = e, true
		}
		if u := c.types.resolve(t.Uses); u != t.Uses {
			s.Uses, changed = u, true
		}
		if changed {
			out = &s
		}
	case *Slice:
		if e := c.finalType(t.Elem, memo); e != t.Elem {
			out = &Slice{Elem: e}
		}
	case *Pointer:
		if e := c.finalType(t.Elem, memo); e != t.Elem {
			out = &Pointer{Elem: e}
		}
	case *Map:
		k, e := c.finalType(t.Key, memo), c.finalType(t.Elem, memo)
		if k != t.Key || e != t.Elem {
			out = &Map{Key: k, Elem: e}
		}
	case *Tuple:
		ts := make([]Type, len(t.Types))
		changed := false
		for i, e := range t.Types {
			ts[i] = c.finalType(e, memo)
			changed = changed || ts[i] != e
		}
		if changed {
			out = &Tuple{Types: ts}
		}
	}
	memo[t] = out
	return out
}

// tail records a block-final expression statement whose value is the
// block's value (Info.Tails).
func (c *checker) tail(s syntax.Stmt, t Type) {
	if c.silent == 0 && t != nil && !isInvalid(t) {
		c.pkg.Info.Tails[s] = t
	}
}

// pattern records a pattern node (Info.Patterns) and returns it.
func (c *checker) pattern(x syntax.Expr, p *Pattern) *Pattern {
	if c.silent == 0 && x != nil {
		c.pkg.Info.Patterns[x] = p
	}
	return p
}

// asPattern records `core as name`: its Var is the binding, filled in by
// finishInfo once the arm has declared it.
func (c *checker) asPattern(x syntax.Expr, core *Pattern, name *syntax.Ident) *Pattern {
	p := c.pattern(x, &Pattern{Kind: PatAs, Elems: []*Pattern{core}})
	if c.silent == 0 {
		if c.asNames == nil {
			c.asNames = map[*Pattern]*syntax.Ident{}
		}
		c.asNames[p] = name
	}
	return p
}

// match records a match or catch (Info.Matches).
func (c *checker) match(n syntax.Node, m *Match) {
	if c.silent == 0 {
		c.pkg.Info.Matches[n] = m
	}
}

// loop records a for-in (Info.Loops).
func (c *checker) loop(s *syntax.RangeStmt, l *Loop) {
	if c.silent == 0 {
		c.pkg.Info.Loops[s] = l
	}
}

// implicit records the implicit conversion an assignment of a value of
// type from to t makes (Info.Implicit): a variant into its union, or a
// value into an operation type with its operations' functions.
func (c *checker) implicit(x syntax.Expr, from, t Type) {
	if c.silent > 0 || x == nil {
		return
	}
	if conv, ok := c.conversionOf(from, t); ok {
		c.pkg.Info.Implicit[x] = conv
	}
}

// conversionOf is the implicit conversion of a value of type from to t,
// and false when there is none (the types are identical, or the value
// already is an interface).
func (c *checker) conversionOf(from, t Type) (Conversion, bool) {
	if c.identical(from, t) {
		return Conversion{}, false
	}
	switch {
	case isUnion(t) && !isUnion(from):
		return Conversion{Kind: ConvVariant, From: from, To: t}, true
	case asInterface(t) != nil && asInterface(from) == nil:
		table := map[string]*Func{}
		for _, m := range asInterface(t).Methods {
			if _, fn := c.methodOf(from, m.Name); fn != nil {
				table[m.Name] = fn
			}
		}
		return Conversion{Kind: ConvOperations, From: from, To: t, Table: table}, true
	}
	return Conversion{}, false
}

func isUnion(t Type) bool {
	_, ok := under0(t).(*Union)
	return ok
}
