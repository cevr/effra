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
//     entry (TestInfoComplete).
//  2. Final: no type in Info is invalid or holds a row variable; a type
//     parameter appears only inside its own generic declaration.
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
	files   []*fileInfo
}

// Info records the checker's results for the lowering (#197, #50).
type Info struct {
	// Types is every expression's final type; a literal carries its one
	// type (196u).
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
	// ConvRow puts a error row's value into a union.
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
	if c.silent > 0 || x == nil || c.identical(from, t) {
		return
	}
	switch {
	case isUnion(t) && !isUnion(from):
		c.pkg.Info.Implicit[x] = Conversion{Kind: ConvVariant, From: from, To: t}
	case asInterface(t) != nil && asInterface(from) == nil:
		table := map[string]*Func{}
		for _, m := range asInterface(t).Methods {
			if _, fn := c.methodOf(from, m.Name); fn != nil {
				table[m.Name] = fn
			}
		}
		c.pkg.Info.Implicit[x] = Conversion{Kind: ConvOperations, From: from, To: t, Table: table}
	}
}

func isUnion(t Type) bool {
	_, ok := under0(t).(*Union)
	return ok
}
