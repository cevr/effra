package check

import (
	"go/constant"

	"ergo.local/prototype/internal/sketch/syntax"
)

// isEntry reports whether f is this package's entry function, main.
func (c *checker) isEntry(f *Func) bool {
	return f.Name == "main" && f.Recv == nil && f.Pkg == c.pkg
}

// call checks a call, a conversion, a builtin or the with construct. In call
// position, with is always the construct (grammar.md, Reserved names 2).
func (c *checker) call(x *syntax.CallExpr, hint Type) operand {
	if id, ok := x.Fun.(*syntax.Ident); ok && id.Name == "with" {
		return c.with(x, hint)
	}
	if id, ok := unparen(x.Fun).(*syntax.Ident); ok {
		if f, ok := c.lookup(id.Name).(*Func); ok && c.isEntry(f) {
			c.recordUse(id, f)
			c.errorf(x, CodeEntry, "main is called only by the host (rows.md, Capability rows 4)")
			c.silentArgs(x)
			return bad(x)
		}
	}
	fun := c.expr(x.Fun, nil)
	switch fun.mode {
	case modeInvalid:
		c.silentArgs(x)
		return bad(x)
	case modeType:
		if isInvalid(fun.typ) {
			c.errorf(x.Fun, CodeType, "error is not a conversion")
			return bad(x)
		}
		return c.conversion(x, fun.typ)
	case modeBuiltin:
		return c.builtin(x, fun.obj.(*Builtin), hint)
	case modeCapFn:
		c.charge(x, fun.cap)
		return c.callSig(x, fun.typ.(*Signature), fun)
	case modeGeneric:
		return c.callGeneric(x, fun.typ.(*Signature), nil, fun)
	case modeValue:
		c.handled(&fun)
		sig, ok := under(fun.typ).(*Signature)
		if !ok {
			c.errorf(x.Fun, CodeType, "cannot call %s, a value of type %s", exprText(x.Fun), fun.typ)
			c.silentArgs(x)
			return bad(x)
		}
		return c.callSig(x, sig, fun)
	}
	c.needValue(&fun)
	c.silentArgs(x)
	return bad(x)
}

// silentArgs checks the arguments of a call whose callee is invalid, so
// their own mistakes are still reported, without further cascades.
func (c *checker) silentArgs(x *syntax.CallExpr) {
	for _, a := range x.Args {
		if _, ok := a.(*syntax.FuncLit); ok {
			continue
		}
		c.valueExpr(a, nil)
	}
}

// argList is the arguments of a call: the written ones and the trailing
// block, which is the final function-literal argument (grammar.md,
// Expressions 3).
type argList struct {
	exprs    []syntax.Expr
	trailing *syntax.BlockStmt
}

func (a argList) n() int {
	if a.trailing != nil {
		return len(a.exprs) + 1
	}
	return len(a.exprs)
}

func argsOf(x *syntax.CallExpr) argList { return argList{exprs: x.Args, trailing: x.Trailing} }

// paramType is the type the i-th argument is checked against.
func paramType(sig *Signature, i int, spread bool) Type {
	if sig.Variadic && i >= len(sig.Params)-1 {
		last := sig.Params[len(sig.Params)-1].Type
		if spread {
			return last
		}
		if s, ok := last.(*Slice); ok {
			return s.Elem
		}
		return last
	}
	if i < len(sig.Params) {
		return sig.Params[i].Type
	}
	return nil
}

func (c *checker) arity(x *syntax.CallExpr, sig *Signature, n int) bool {
	spread := x.Ellipsis.IsValid()
	if spread && !sig.Variadic {
		c.errorf(x, CodeType, "cannot use ... with a non-variadic function")
		return false
	}
	if sig.Variadic && !spread {
		if n < len(sig.Params)-1 {
			c.errorf(x, CodeType, "not enough arguments: want at least %d, have %d", len(sig.Params)-1, n)
			return false
		}
		return true
	}
	if n != len(sig.Params) {
		which := "too many"
		if n < len(sig.Params) {
			which = "not enough"
		}
		c.errorf(x, CodeType, "%s arguments: want %d, have %d", which, len(sig.Params), n)
		return false
	}
	return true
}

// callSig checks a call to a function value of a known, non-generic type.
func (c *checker) callSig(x *syntax.CallExpr, sig *Signature, fun operand) operand {
	args := argsOf(x)
	// f(g()) with a multi-value g spreads its results.
	if len(args.exprs) == 1 && args.trailing == nil && len(sig.Params) > 1 {
		if _, isCall := args.exprs[0].(*syntax.CallExpr); isCall {
			c.silent++
			peek := c.expr(args.exprs[0], nil)
			c.silent--
			_, multi := peek.typ.(*Tuple)
			var op operand
			if multi {
				op = c.expr(args.exprs[0], nil)
			}
			if tup, ok := op.typ.(*Tuple); multi && ok && op.mode == modeValue {
				c.handled(&op)
				if len(tup.Types) != len(sig.Params) {
					c.errorf(x, CodeType, "%d values for %d parameters", len(tup.Types), len(sig.Params))
					return bad(x)
				}
				for i, t := range tup.Types {
					if ok, why := c.assignable(t, sig.Params[i].Type); !ok {
						c.errorf(args.exprs[0], CodeType, "cannot use %s as %s: %s", t, sig.Params[i].Type, why)
					}
				}
				return c.result(x, sig)
			}
		}
	}
	if !c.arity(x, sig, args.n()) {
		for _, a := range args.exprs {
			if _, isLit := a.(*syntax.FuncLit); !isLit {
				c.valueExpr(a, nil)
			}
		}
		return bad(x)
	}
	spread := x.Ellipsis.IsValid()
	for i, a := range args.exprs {
		pt := paramType(sig, i, spread)
		c.argument(a, pt)
	}
	if args.trailing != nil {
		pt := paramType(sig, len(args.exprs), false)
		ps, ok := under0(pt).(*Signature)
		if !ok {
			c.errorf(args.trailing, CodeType, "a trailing block is a function-literal argument, but the final parameter has type %s", pt)
		} else {
			lit := c.blockLit(args.trailing, ps, nil)
			if ok, why := c.assignable(lit, pt); !ok {
				c.errorf(args.trailing, CodeType, "this block cannot be %s: %s", pt, why)
			}
		}
	}
	return c.result(x, sig)
}

// argument checks one argument against its parameter type.
func (c *checker) argument(a syntax.Expr, pt Type) {
	if lit, ok := a.(*syntax.FuncLit); ok {
		ps, _ := under0(pt).(*Signature)
		op := c.funcLit(lit, ps, nil)
		op.expr = lit
		if !op.invalid() {
			c.assign(&op, pt, "argument")
		}
		return
	}
	op := c.valueExpr(a, pt)
	c.assign(&op, pt, "argument")
}

// result is the operand of a call: its value results, its failure row, and
// the charges of its capability row (rows.md, Capability rows 2).
func (c *checker) result(x *syntax.CallExpr, sig *Signature) operand {
	c.chargeRow(x, sig.Uses)
	op := operand{mode: modeValue, errs: sig.Errors}
	switch len(sig.Results) {
	case 0:
		op.mode, op.typ = modeNoValue, NoValue
	case 1:
		op.typ = sig.Results[0]
	default:
		op.typ = &Tuple{Types: sig.Results}
	}
	return op
}

// ---------------------------------------------------------------- generics

// explicit instantiates a generic function with written type arguments:
// bracket-listed parameters first; implicit ones are inferred (D6).
func (c *checker) explicit(x *syntax.IndexExpr, fun operand) operand {
	sig := fun.typ.(*Signature)
	if len(x.Indices) > sig.NBracket {
		c.errorf(x, CodeType, "%s takes %d explicit type arguments, not %d (implicit parameters are always inferred)", exprText(x.X), sig.NBracket, len(x.Indices))
		return bad(x)
	}
	targs := make([]Type, len(x.Indices))
	ctx := c.localTypeCtx()
	for i, a := range x.Indices {
		tp := sig.TParams[i]
		if tp.Kind != KindType {
			row := c.rowExpr([]syntax.Expr{a}, tp.Kind, ctx)
			if row == nil {
				return bad(x)
			}
			targs[i] = &RowArg{Row: row}
			continue
		}
		t := c.typExpr(a, ctx)
		if isInvalid(t) {
			return bad(x)
		}
		targs[i] = t
	}
	return operand{mode: modeGeneric, typ: sig, fn: fun.fn, val: nil, obj: &explicitArgs{targs: targs}}
}

// explicitArgs carries written type arguments to the call.
type explicitArgs struct{ targs []Type }

func (*explicitArgs) name() string    { return "" }
func (*explicitArgs) pos() syntax.Pos { return syntax.NoPos }

// inferer binds type parameters by unification over value arguments, never
// from a call's expected result (Go's function argument type inference,
// types2/infer.go). A row parameter binds to the row set of a function-typed
// argument: rows are closed sets, so unification is set binding.
type inferer struct {
	c   *checker
	m   map[*TypeParam]Type
	set map[*TypeParam]bool
	// joined marks the row parameters bound by unification, not written:
	// a later argument widens them (see unifyRow).
	joined map[*TypeParam]bool
}

func (in *inferer) free(tp *TypeParam) bool { return in.set[tp] && in.m[tp] == nil }

// hasFree reports whether t mentions an unbound parameter.
func (in *inferer) hasFree(t Type) bool {
	switch t := t.(type) {
	case nil:
		return false
	case *TypeParam:
		return in.free(t)
	case *Named:
		for _, a := range t.TArgs {
			if in.hasFree(a) {
				return true
			}
		}
	case *RowArg:
		return in.rowFree(t.Row)
	case *Slice:
		return in.hasFree(t.Elem)
	case *Array:
		return in.hasFree(t.Elem)
	case *Map:
		return in.hasFree(t.Key) || in.hasFree(t.Elem)
	case *Chan:
		return in.hasFree(t.Elem)
	case *Pointer:
		return in.hasFree(t.Elem)
	case *Signature:
		for _, p := range t.Params {
			if in.hasFree(p.Type) {
				return true
			}
		}
		for _, r := range t.Results {
			if in.hasFree(r) {
				return true
			}
		}
		return in.rowFree(t.Errors) || in.rowFree(t.Uses)
	}
	return false
}

func (in *inferer) rowFree(r *Row) bool {
	for _, e := range r.Elems() {
		if tp, ok := e.(*TypeParam); ok && in.free(tp) {
			return true
		}
	}
	return false
}

func (in *inferer) unify(p, a Type) {
	if a == nil || isInvalid(a) {
		return
	}
	switch p := p.(type) {
	case *TypeParam:
		if in.free(p) && p.Kind == KindType {
			if isUntyped(a) {
				return
			}
			in.m[p] = a
		}
	case *Named:
		if an, ok := a.(*Named); ok && an.origin() == p.origin() && len(an.TArgs) == len(p.TArgs) {
			for i := range p.TArgs {
				if pr, ok := p.TArgs[i].(*RowArg); ok {
					if ar, ok := an.TArgs[i].(*RowArg); ok {
						in.unifyRow(pr.Row, ar.Row)
					}
					continue
				}
				in.unify(p.TArgs[i], an.TArgs[i])
			}
		}
	case *Slice:
		if as, ok := under(a).(*Slice); ok {
			in.unify(p.Elem, as.Elem)
		}
	case *Array:
		if as, ok := under(a).(*Array); ok {
			in.unify(p.Elem, as.Elem)
		}
	case *Map:
		if am, ok := under(a).(*Map); ok {
			in.unify(p.Key, am.Key)
			in.unify(p.Elem, am.Elem)
		}
	case *Chan:
		if ac, ok := under(a).(*Chan); ok {
			in.unify(p.Elem, ac.Elem)
		}
	case *Pointer:
		if ap, ok := a.(*Pointer); ok {
			in.unify(p.Elem, ap.Elem)
		}
	case *Signature:
		as, ok := under(a).(*Signature)
		if !ok {
			return
		}
		if len(p.Params) == len(as.Params) {
			for i := range p.Params {
				in.unify(p.Params[i].Type, as.Params[i].Type)
			}
		}
		switch {
		case len(p.Results) == len(as.Results):
			for i := range p.Results {
				in.unify(p.Results[i], as.Results[i])
			}
		case len(p.Results) == 1 && len(as.Results) == 0:
			// A no-value result binds a result-position parameter to
			// "no value" (Q3; Gleam's Nil, Effect's void).
			if tp, ok := p.Results[0].(*TypeParam); ok && in.free(tp) {
				in.m[tp] = NoValue
			}
		}
		in.unifyRow(p.Errors, as.Errors)
		in.unifyRow(p.Uses, as.Uses)
	}
}

// unifyRow binds the free row parameter of p to the elements of a that p's
// other elements do not cover. A row parameter an earlier argument bound
// is widened to cover a as well: rows are finite sets ordered by inclusion,
// and a function with a narrower row is assignable where a wider one is
// expected, so the least binding that admits every argument is the union
// (D6; Koka's row unification reaches the same row through open tails).
// A written type argument is never widened.
func (in *inferer) unifyRow(p, a *Row) {
	var free, joined []*TypeParam
	rest := newRow()
	for _, e := range p.Elems() {
		if tp, ok := e.(*TypeParam); ok {
			if in.free(tp) {
				free = append(free, tp)
				continue
			}
			if b, ok := in.m[tp].(*RowArg); ok {
				if in.joined[tp] {
					joined = append(joined, tp)
				}
				rest = rest.union(b.Row)
				continue
			}
		}
		rest = rest.with(e)
	}
	extra := a.minus(rest)
	switch {
	case len(free) > 0:
		in.m[free[0]] = &RowArg{Row: extra}
		for _, tp := range free[1:] {
			in.m[tp] = &RowArg{Row: newRow()}
		}
		if in.joined == nil {
			in.joined = map[*TypeParam]bool{}
		}
		for _, tp := range free {
			in.joined[tp] = true
		}
	case len(joined) > 0 && !extra.Empty():
		tp := joined[0]
		in.m[tp] = &RowArg{Row: in.m[tp].(*RowArg).Row.union(extra)}
	}
}

// callGeneric infers the type arguments of a call to a generic function:
// typed value arguments first, then untyped constants with their default
// types, then function literals and trailing blocks, whose rows and results
// bind the remaining parameters (D6; Go's order for untyped arguments).
func (c *checker) callGeneric(x *syntax.CallExpr, sig *Signature, explicit []Type, fun operand) operand {
	if ea, ok := fun.obj.(*explicitArgs); ok {
		explicit = ea.targs
	}
	if fun.fn != nil && c.monoBad(fun.fn.Sig.TParams) {
		// On an instantiation cycle (reported): the call adds nothing,
		// so row inference does not follow the cycle's growth.
		c.silentArgs(x)
		return bad(x)
	}
	args := argsOf(x)
	if !c.arity(x, sig, args.n()) {
		return bad(x)
	}
	spread := x.Ellipsis.IsValid()
	in := &inferer{c: c, m: map[*TypeParam]Type{}, set: map[*TypeParam]bool{}}
	for _, tp := range sig.TParams {
		in.set[tp] = true
	}
	for i, t := range explicit {
		in.m[sig.TParams[i]] = t
	}
	n := args.n()
	ops := make([]*operand, n)
	isLit := func(i int) bool {
		if i == len(args.exprs) {
			return true
		}
		_, ok := args.exprs[i].(*syntax.FuncLit)
		return ok
	}
	for i := 0; i < len(args.exprs); i++ {
		if isLit(i) {
			continue
		}
		pt := paramType(sig, i, spread)
		var hint Type
		if !in.hasFree(pt) {
			hint = c.subst(pt, in.m)
		}
		op := c.valueExpr(args.exprs[i], hint)
		ops[i] = &op
		if op.invalid() || isUntyped(op.typ) {
			continue
		}
		in.unify(pt, op.typ)
	}
	for i := 0; i < len(args.exprs); i++ {
		if ops[i] == nil || ops[i].invalid() || !isUntyped(ops[i].typ) {
			continue
		}
		if tp, ok := paramType(sig, i, spread).(*TypeParam); ok && in.free(tp) {
			in.m[tp] = defaultType(ops[i].typ)
		}
	}
	lits := make([]Type, n)
	for i := 0; i < n; i++ {
		if !isLit(i) {
			continue
		}
		pt := paramType(sig, i, spread)
		expected, _ := under0(c.subst(pt, in.m)).(*Signature)
		var lt *Signature
		if i == len(args.exprs) {
			if expected == nil {
				c.errorf(args.trailing, CodeType, "a trailing block is a function-literal argument, but the final parameter has type %s", pt)
				return bad(x)
			}
			lt = c.blockLit(args.trailing, expected, in)
		} else {
			op := c.funcLit(args.exprs[i].(*syntax.FuncLit), expected, in)
			if op.invalid() {
				return bad(x)
			}
			lt = op.typ.(*Signature)
		}
		lits[i] = lt
		in.unify(pt, lt)
	}
	for _, tp := range sig.TParams {
		if in.m[tp] != nil {
			continue
		}
		if tp.Kind != KindType {
			in.m[tp] = &RowArg{Row: newRow()}
			continue
		}
		c.errorf(x, CodeType, "cannot infer the type parameter %s of %s from the arguments; a type argument is never inferred from the expected result (D6)", tp.Name, exprText(x.Fun))
		return bad(x)
	}
	for _, tp := range sig.TParams {
		b := in.m[tp]
		if b == NoValue {
			continue
		}
		if tp.Kind == KindType && tp.Constraint != nil {
			cons := c.subst(tp.Constraint, in.m)
			if why := c.satisfies(b, cons); why != "" {
				c.errorf(x, CodeType, "%s does not satisfy the constraint %s of %s: %s (types.md, Generics 2)", b, cons, tp.Name, why)
				return bad(x)
			}
		}
	}
	if fun.fn != nil {
		targs := make([]Type, len(sig.TParams))
		for i, tp := range sig.TParams {
			targs[i] = in.m[tp]
		}
		c.recordInstantiation(fun.fn.Name, sig.TParams, targs, x)
	}
	inst := c.subst(sig, in.m).(*Signature)
	inst.TParams = nil
	for i := 0; i < n; i++ {
		pt := paramType(inst, i, spread)
		if lits[i] != nil {
			if ok, why := c.assignable(lits[i], pt); !ok {
				at := syntax.Node(args.trailing)
				if i < len(args.exprs) {
					at = args.exprs[i]
				}
				c.errorf(at, CodeType, "this function literal cannot be %s: %s", pt, why)
			}
			continue
		}
		if ops[i] != nil {
			c.assign(ops[i], pt, "argument")
		}
	}
	return c.result(x, inst)
}

// ---------------------------------------------------------------- literals and blocks

// funcLit checks a function literal. Its rows are declared when written,
// else taken from the function type it is passed to when that type's rows
// are known, else inferred from its body (rows-and-constraints §2, "Literals
// land").
func (c *checker) funcLit(x *syntax.FuncLit, expected *Signature, in *inferer) operand {
	sig := c.signature(x.Type, c.localTypeCtx())
	writtenErr := false
	if x.Type.Results != nil {
		for _, r := range x.Type.Results.List {
			if _, ok := r.Type.(*syntax.ErrorRow); ok {
				writtenErr = true
			}
		}
	}
	t := &target{name: "this function literal", results: sig.Results, resultKnown: true, isFunc: true}
	switch {
	case writtenErr:
		t.declErrors = sig.Errors
	case expected != nil && (in == nil || !in.rowFree(expected.Errors)):
		t.declErrors = expected.Errors
	default:
		t.errors = newRow()
	}
	switch {
	case x.Type.Needs != nil:
		t.declUses = sig.Uses
	case expected != nil && (in == nil || !in.rowFree(expected.Uses)):
		t.declUses = expected.Uses
	default:
		t.uses = newRow()
	}
	c.body(t, x.Type, sig.Params, func() {
		c.stmtList(x.Body.List, false)
		if len(sig.Results) > 0 && !c.terminatesList(x.Body.List) {
			c.errorAt(x.Body.Rbrace, x.Body.Rbrace+1, CodeMissingRet, "this function literal can reach the end of its body without returning a value (rows.md, Totality 3)")
		}
	})
	sig.Errors = t.row(t.declErrors, t.errors)
	sig.Uses = t.row(t.declUses, t.uses)
	if !writtenErr && !sig.Errors.Empty() {
		sig.Fallible = true
	}
	if c.silent == 0 {
		c.pkg.Info.Literals[x] = sig
	}
	return operand{mode: modeValue, typ: sig}
}

// body runs check inside a nested target with its own frame: a function
// literal, a trailing block or a go block.
func (c *checker) body(t *target, ft *syntax.FuncType, params []*Var, check func()) {
	savedT, savedScope, savedBranches := c.fn.t, c.fn.scope, c.fn.branches
	c.fn.t = t
	frame := newScope(c.fn.scope)
	frame.frame = true
	c.fn.scope = frame
	c.fn.branches = nil
	if !t.isFunc {
		c.fn.branches = []branch{{kind: brBarrier}}
	}
	for _, p := range params {
		if p.Name == "_" {
			continue
		}
		if _, dup := frame.names[p.Name]; dup {
			c.errorAt(p.Pos, p.Pos+syntax.Pos(len(p.Name)), CodeType, "%s is already a parameter", p.Name)
			continue
		}
		frame.insert(p.Name, &Var{Name: p.Name, Type: p.Type, Pos: p.Pos, param: true, used: true})
	}
	c.bindRequirements(frame, ft)
	check()
	c.fn.t, c.fn.scope, c.fn.branches = savedT, savedScope, savedBranches
}

// blockLit checks a trailing block or a go block as a function literal with
// no parameters (grammar.md, Expressions 3): f(a) { body } is
// f(a, func() T { return body }). The block's last expression is its result;
// return, break and continue may not leave it (grammar.md, Statements 5).
func (c *checker) blockLit(b *syntax.BlockStmt, expected *Signature, in *inferer) *Signature {
	sig := &Signature{Errors: newRow(), Uses: newRow()}
	t := &target{name: "this block"}
	resultFree := expected == nil || (in != nil && func() bool {
		for _, r := range expected.Results {
			if in.hasFree(r) {
				return true
			}
		}
		return false
	}())
	if expected != nil && len(expected.Params) > 0 {
		c.errorf(b, CodeType, "a trailing block takes no parameters, but the parameter type %s has %d; write a func literal", expected, len(expected.Params))
	}
	switch {
	case expected != nil && (in == nil || !in.rowFree(expected.Errors)):
		t.declErrors = expected.Errors
	default:
		t.errors = newRow()
	}
	switch {
	case expected != nil && (in == nil || !in.rowFree(expected.Uses)):
		t.declUses = expected.Uses
	default:
		t.uses = newRow()
	}
	if !resultFree {
		t.results, t.resultKnown = expected.Results, true
	}
	c.body(t, nil, nil, func() {
		res := c.yieldingBlock(b, b.List, t.results, t.resultKnown, func(op *operand) { c.escape(op.expr, op.errs) })
		if resultFree {
			if res != nil && res != NoValue {
				sig.Results = []Type{res}
			}
		} else {
			sig.Results = expected.Results
		}
	})
	sig.Errors = t.row(t.declErrors, t.errors)
	sig.Uses = t.row(t.declUses, t.uses)
	sig.Fallible = !sig.Errors.Empty() || expected != nil && expected.Fallible
	if c.silent == 0 {
		c.pkg.Info.Literals[b] = sig
	}
	return sig
}

// yieldingBlock checks a block that yields its last expression: a with
// block, a trailing block, a go block. When results are known, the last
// expression is assigned to them; otherwise the result is its type (an
// untyped constant's default type), or no value; a single unknown result
// is still the expected type of the last expression, as a hint (M16). pass
// receives the last expression's failure row, which passes through
// (rows.md, With 6).
func (c *checker) yieldingBlock(at syntax.Node, list []syntax.Stmt, results []Type, known bool, pass func(*operand)) Type {
	if len(list) == 0 {
		if known && len(results) > 0 {
			c.errorf(at, CodeType, "this block yields no value; %s expected", results[0])
		}
		return NoValue
	}
	last, isExpr := list[len(list)-1].(*syntax.ExprStmt)
	if !isExpr {
		c.stmtList(list, false)
		if known && len(results) > 0 {
			c.errorf(list[len(list)-1], CodeType, "this block must end with an expression of type %s; a block yields its last expression", results[0])
		}
		return NoValue
	}
	c.stmtList(list[:len(list)-1], true)
	var hint Type
	if len(results) == 1 {
		hint = results[0]
	}
	op := c.expr(last.X, hint)
	if op.invalid() {
		return nil
	}
	if known && len(results) == 0 {
		c.needNoValueOrCall(&op)
	} else {
		switch op.mode {
		case modeValue, modeConst, modeNoValue:
		default:
			c.needValue(&op)
			if op.invalid() {
				return nil
			}
		}
	}
	if op.fallible() {
		pass(&op)
	}
	switch {
	case known && len(results) == 1:
		if op.mode == modeNoValue {
			c.errorf(last.X, CodeType, "this block yields no value; %s expected", results[0])
			return nil
		}
		c.assign(&op, results[0], "the block's value")
		return results[0]
	case known && len(results) > 1:
		if tup, ok := op.typ.(*Tuple); !ok || len(tup.Types) != len(results) {
			c.errorf(last.X, CodeType, "this block yields the wrong number of values")
		}
		return op.typ
	case known:
		return NoValue
	}
	if op.mode == modeNoValue {
		return NoValue
	}
	return c.defaultOf(&op)
}

// needNoValueOrCall accepts the last expression of a block whose value is
// discarded: as an expression statement (S110 otherwise).
func (c *checker) needNoValueOrCall(op *operand) {
	switch op.mode {
	case modeNoValue:
		return
	case modeValue, modeConst:
		if !isCallLike(op.expr) {
			c.errorf(op.expr, CodeUnused, "this value is not used; an expression statement is a call, a receive, try or catch")
		}
		return
	}
	c.needValue(op)
}

func isCallLike(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.CallExpr, *syntax.TryExpr, *syntax.CatchExpr, *syntax.GoExpr:
		return true
	case *syntax.UnaryExpr:
		return x.Op == syntax.ARROW
	case *syntax.ParenExpr:
		return isCallLike(x.X)
	}
	return false
}

// ---------------------------------------------------------------- builtins

func (c *checker) builtin(x *syntax.CallExpr, b *Builtin, hint Type) operand {
	if x.Trailing != nil {
		c.errorf(x.Trailing, CodeType, "%s takes no block", b.Name)
		return bad(x)
	}
	if x.Ellipsis.IsValid() && b.Name != "append" {
		c.errorf(x, CodeType, "cannot use ... with %s", b.Name)
		return bad(x)
	}
	n := len(x.Args)
	want := func(lo, hi int) bool {
		if n < lo || hi >= 0 && n > hi {
			c.errorf(x, CodeType, "wrong number of arguments to %s", b.Name)
			c.silentArgs(x)
			return false
		}
		return true
	}
	switch b.Name {
	case "len", "cap":
		if !want(1, 1) {
			return bad(x)
		}
		op := c.valueExpr(x.Args[0], nil)
		if op.invalid() {
			return bad(x)
		}
		t := op.typ
		if p, ok := t.(*Pointer); ok {
			t = p.Elem
		}
		switch u := under(t).(type) {
		case *Basic:
			if isString(u) && b.Name == "len" {
				if op.mode == modeConst {
					return operand{mode: modeConst, typ: Typ[Int], val: constant.MakeInt64(int64(len(constant.StringVal(op.val))))}
				}
				return operand{mode: modeValue, typ: Typ[Int]}
			}
		case *Slice, *Array, *Chan:
			return operand{mode: modeValue, typ: Typ[Int]}
		case *Map:
			if b.Name == "len" {
				return operand{mode: modeValue, typ: Typ[Int]}
			}
		}
		c.errorf(x.Args[0], CodeType, "invalid argument %s for %s", op.typ, b.Name)
		return bad(x)
	case "append":
		if !want(1, -1) {
			return bad(x)
		}
		s := c.valueExpr(x.Args[0], hint)
		if s.invalid() {
			c.silentArgs(&syntax.CallExpr{Args: x.Args[1:]})
			return bad(x)
		}
		st, ok := under(s.typ).(*Slice)
		if !ok {
			c.errorf(x.Args[0], CodeType, "the first argument to append must be a slice, not %s", s.typ)
			return bad(x)
		}
		for i, a := range x.Args[1:] {
			if x.Ellipsis.IsValid() && i == len(x.Args)-2 {
				op := c.valueExpr(a, s.typ)
				if !op.invalid() && !(isString(op.typ) && isBasic(st.Elem, Uint8)) {
					c.assign(&op, s.typ, "argument")
				}
				continue
			}
			op := c.valueExpr(a, st.Elem)
			c.assign(&op, st.Elem, "argument")
		}
		return operand{mode: modeValue, typ: s.typ}
	case "make":
		if !want(1, 3) {
			return bad(x)
		}
		t := c.typExpr(x.Args[0], c.localTypeCtx())
		if isInvalid(t) {
			return bad(x)
		}
		switch under(t).(type) {
		case *Slice:
			if n < 2 {
				c.errorf(x, CodeType, "make of a slice needs a length")
				return bad(x)
			}
		case *Map, *Chan:
			if n > 2 {
				c.errorf(x, CodeType, "too many arguments to make")
				return bad(x)
			}
		default:
			c.errorf(x.Args[0], CodeType, "cannot make %s", t)
			return bad(x)
		}
		// Constant sizes are nonnegative, and a constant length is at most
		// a constant capacity (S108; Go's rule).
		var sizes []int64
		known := true
		for _, a := range x.Args[1:] {
			v, ok := c.intIndex(a)
			sizes = append(sizes, v)
			known = known && ok
		}
		if known && len(sizes) == 2 && sizes[0] > sizes[1] {
			c.errorf(x.Args[2], CodeType, "the length %d is larger than the capacity %d", sizes[0], sizes[1])
		}
		return operand{mode: modeValue, typ: t}
	case "copy":
		if !want(2, 2) {
			return bad(x)
		}
		d := c.valueExpr(x.Args[0], nil)
		s := c.valueExpr(x.Args[1], nil)
		if d.invalid() || s.invalid() {
			return bad(x)
		}
		ds, ok := under(d.typ).(*Slice)
		if !ok {
			c.errorf(x.Args[0], CodeType, "copy expects slice arguments")
			return bad(x)
		}
		if ss, ok := under(s.typ).(*Slice); !(ok && identical(ss.Elem, ds.Elem) || isString(s.typ) && isBasic(ds.Elem, Uint8)) {
			c.errorf(x, CodeType, "copy of mismatched slices %s and %s", d.typ, s.typ)
			return bad(x)
		}
		return operand{mode: modeValue, typ: Typ[Int]}
	case "delete":
		if !want(2, 2) {
			return bad(x)
		}
		m := c.valueExpr(x.Args[0], nil)
		if m.invalid() {
			c.valueExpr(x.Args[1], nil)
			return bad(x)
		}
		mt, ok := under(m.typ).(*Map)
		if !ok {
			c.errorf(x.Args[0], CodeType, "delete expects a map, not %s", m.typ)
			return bad(x)
		}
		k := c.valueExpr(x.Args[1], mt.Key)
		c.assign(&k, mt.Key, "map key")
		return operand{mode: modeNoValue, typ: NoValue}
	case "clear":
		if !want(1, 1) {
			return bad(x)
		}
		op := c.valueExpr(x.Args[0], nil)
		if op.invalid() {
			return bad(x)
		}
		switch under(op.typ).(type) {
		case *Map, *Slice:
			return operand{mode: modeNoValue, typ: NoValue}
		}
		c.errorf(x.Args[0], CodeType, "clear expects a map or a slice, not %s", op.typ)
		return bad(x)
	case "close":
		if !want(1, 1) {
			return bad(x)
		}
		op := c.valueExpr(x.Args[0], nil)
		if op.invalid() {
			return bad(x)
		}
		if ch, ok := under(op.typ).(*Chan); !ok || ch.Dir == syntax.ChanRecv {
			c.errorf(x.Args[0], CodeType, "close expects a channel that can send, not %s", op.typ)
			return bad(x)
		}
		return operand{mode: modeNoValue, typ: NoValue}
	case "min", "max":
		if !want(1, -1) {
			return bad(x)
		}
		ops := make([]operand, n)
		for i, a := range x.Args {
			ops[i] = c.valueExpr(a, hint)
			if ops[i].invalid() {
				return bad(x)
			}
		}
		res := ops[0]
		for i := 1; i < n; i++ {
			l, r := res, ops[i]
			be := &syntax.BinaryExpr{X: x.Args[0], Op: syntax.LSS, Y: x.Args[i], OpPos: x.Lparen}
			if !c.matchTypes(be, &l, &r) {
				return bad(x)
			}
			if l.mode == modeConst && r.mode == modeConst {
				if constant.Compare(r.val, tokenOf[syntax.LSS], l.val) == (b.Name == "min") {
					l.val = r.val
				}
				res = operand{mode: modeConst, typ: l.typ, val: l.val}
			} else {
				res = operand{mode: modeValue, typ: l.typ}
			}
		}
		if !ordered(res.typ) {
			c.errorf(x, CodeType, "%s expects ordered values, not %s", b.Name, res.typ)
			return bad(x)
		}
		return res
	}
	c.errorf(x, CodeType, "unknown builtin %s", b.Name)
	return bad(x)
}
