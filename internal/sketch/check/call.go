package check

import (
	"go/constant"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
)

// isEntry reports whether f is this package's entry function, main.
func (c *checker) isEntry(f *Func) bool {
	return f.Name == "main" && f.Pkg == c.pkg
}

// call checks a call, a conversion, a builtin or the with construct. In call
// position, with is always the construct (grammar.md, Reserved names 2).
//
// Afterwards, each callback frame of the call that typing did not check is
// recorded invalid-diagnosed or refused-by-class-G, or left provisional
// (skipFrames), whichever route skipped it.
func (c *checker) call(x *syntax.CallExpr, hint Type) operand {
	savedMark := c.callMark
	c.callMark = c.undiagSeq
	defer func() { c.callMark = savedMark }()
	op := c.callOf(x, hint)
	c.skipFrames(x)
	return op
}

func (c *checker) callOf(x *syntax.CallExpr, hint Type) operand {
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
	if sel := calledSelector(x.Fun); sel != nil {
		c.callee = sel
		c.calleeIndex, _ = unparen(x.Fun).(*syntax.IndexExpr)
	}
	fun := c.expr(x.Fun, nil)
	if fun.mode == modeValue && isInvalid(fun.typ) {
		// A value whose type is invalid was reported where the type was
		// (D3): the call is checked as one of an invalid callee.
		fun = bad(x.Fun)
	}
	if fun.mode != modeInvalid {
		c.recordCall(x, fun)
	}
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
		if fun.recv != nil {
			return c.libraryCall(x, fun.obj.(*Builtin), *fun.recv)
		}
		return c.builtin(x, fun.obj.(*Builtin))
	case modeCapFn:
		c.charge(x, fun.cap)
		return c.callSig(x, fun.typ.(*Signature), fun)
	case modeGeneric:
		return c.callGeneric(x, fun.typ.(*Signature), nil, fun)
	case modeValue, modeConst:
		// A constant is a value too: `1()` and `K()` are S108, never an
		// unchecked call (FuzzCheck, round 1b).
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

// calledSelector is the selector x.f a call's callee names, written bare,
// x.f(…), or with type arguments, x.f[T](…): there x.f is called, so it
// reaches a package function on x's type (196m rule 4; round 7 review O1).
func calledSelector(fun syntax.Expr) *syntax.SelectorExpr {
	x := unparen(fun)
	if ix, ok := x.(*syntax.IndexExpr); ok {
		x = unparen(ix.X)
	}
	sel, _ := x.(*syntax.SelectorExpr)
	return sel
}

// silentArgs checks the arguments of a call whose callee is invalid, so
// their own mistakes are still reported, without further cascades. What
// the callee expects is unknown, so a type argument is no mistake:
// new(int) is one S104, never a second "int is a type" (D3).
func (c *checker) silentArgs(x *syntax.CallExpr) {
	for _, a := range x.Args {
		if _, ok := a.(*syntax.FuncLit); ok {
			continue
		}
		op := c.expr(a, nil)
		if op.mode == modeType {
			continue
		}
		c.needValue(&op)
		c.handled(&op)
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
func paramType(sig *Signature, i int) Type {
	if i < len(sig.Params) {
		return sig.Params[i].Type
	}
	return nil
}

// arity checks the argument count. A spread argument `f(xs...)` is not
// produced by the decided parser (D-A5): Ergo has no variadic parameters.
func (c *checker) arity(x *syntax.CallExpr, sig *Signature, n int) bool {
	if x.Ellipsis.IsValid() {
		c.undecided(x, "a spread argument")
		return false
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
	// A tuple is one value (196t): f(g()) passes g's tuple as one argument
	// and never spreads it over f's parameters.
	if !c.arity(x, sig, args.n()) {
		for _, a := range args.exprs {
			if _, isLit := a.(*syntax.FuncLit); !isLit {
				c.valueExpr(a, nil)
			}
		}
		return bad(x)
	}
	refused := false
	for i, a := range args.exprs {
		pt := paramType(sig, i)
		if !c.argument(a, pt, x) {
			refused = true
		}
	}
	if refused {
		return bad(x)
	}
	if args.trailing != nil {
		pt := paramType(sig, len(args.exprs))
		ps, ok := under0(pt).(*Signature)
		if !ok {
			c.errorf(args.trailing, CodeType, "a trailing block is a function-literal argument, but the final parameter has type %s", pt)
		} else {
			lit, refused := c.blockLit(args.trailing, ps, nil, true)
			if refused { // class G: invalid, nothing cascades
				return bad(x)
			}
			if ok, why := c.assignable(lit, pt); !ok {
				c.errorf(args.trailing, CodeType, "this block cannot be %s: %s", pt, why)
			}
		}
	}
	return c.result(x, sig)
}

// argument checks one argument against its parameter type. It reports false
// when the argument is a generic function value that the call refused
// (argValue).
func (c *checker) argument(a syntax.Expr, pt Type, call *syntax.CallExpr) bool {
	if lit, ok := a.(*syntax.FuncLit); ok {
		ps, _ := under0(pt).(*Signature)
		op := c.funcLit(lit, ps, nil)
		op.expr = lit
		c.record(lit, op) // Info records the literal's type (D1)
		if !op.invalid() {
			c.assign(&op, pt, "argument")
		}
		return true
	}
	op, ok := c.argValue(a, pt, call)
	if ok {
		c.assign(&op, pt, "argument")
	}
	return ok
}

// argValue checks an argument as a value. A generic function passed
// without all its type arguments (Apply(Id, 1), Fix(Id), Pick[int]) is
// outside the inferred subset (D6 as amended in round 7): the call is S108,
// asking for the type arguments, with no other diagnostic (D3), and ok is
// false. A function whose type parameters are implicit, which no bracket
// can name, keeps its own diagnostic.
func (c *checker) argValue(a syntax.Expr, hint Type, call *syntax.CallExpr) (operand, bool) {
	op := c.expr(a, hint)
	if op.mode == modeGeneric {
		sig, _ := op.typ.(*Signature)
		written := 0
		if ea, ok := op.obj.(*explicitArgs); ok {
			written = len(ea.targs)
		}
		if sig != nil && sig.NBracket == len(sig.TParams) && written < len(sig.TParams) {
			c.cannotInfer(call, sig.TParams[written].Name, a, sig)
			op.mode, op.typ, op.errs = modeInvalid, invalid, nil
			return op, false
		}
	}
	c.needValue(&op)
	c.handled(&op)
	return op, true
}

// result is the operand of a call: its value results, its error row, and
// the charges of its capability row (rows.md, Capability rows 2).
func (c *checker) result(x *syntax.CallExpr, sig *Signature) operand {
	// An inferred row is read through its variable (class F).
	c.chargeRow(x, c.types.resolve(sig.Uses))
	c.readRow(sig.Errors)
	op := operand{mode: modeValue, errs: c.types.resolve(sig.Errors)}
	call := c.callInfo(x)
	call.Fails, call.Needs = op.errs, c.types.resolve(sig.Uses)
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

// cannotInfer reports a type parameter the inferred subset does not reach
// (D6 as amended in round 7): S108 at the call, naming the fix. It is the
// only diagnostic of the call (D3).
func (c *checker) cannotInfer(at syntax.Node, name string, fun syntax.Expr, sig *Signature) {
	c.errorf(at, CodeType, "cannot infer %s; write the type arguments, e.g. %s (D6)", name, inferExample(fun, sig))
}

// inferExample writes the call with its type arguments, for the fix:
// Fn[int](…), with one placeholder type per bracketed parameter.
func inferExample(fun syntax.Expr, sig *Signature) string {
	n := sig.NBracket
	if n == 0 {
		n = len(sig.TParams)
	}
	parts := make([]string, max(n, 1))
	for i := range parts {
		parts[i] = "int"
	}
	name := exprText(fun)
	if ix, ok := fun.(*syntax.IndexExpr); ok {
		name = exprText(ix.X)
	}
	return name + "[" + strings.Join(parts, ", ") + "](…)"
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
	case *Map:
		return in.hasFree(t.Key) || in.hasFree(t.Elem)
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
				rest = rest.union(in.c.types, b.Row)
				continue
			}
		}
		rest = rest.with(in.c.types, e)
	}
	extra := a.minus(in.c.types, rest)
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
		in.m[tp] = &RowArg{Row: in.m[tp].(*RowArg).Row.union(in.c.types, extra)}
	}
}

// callGeneric infers the type arguments of a call to a generic function:
// typed value arguments and function literals first, in source order, then
// the literals' common defaults (196u rule 6), then a trailing block, whose
// rows and result bind the remaining parameters (D6).
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
	in := &inferer{c: c, m: map[*TypeParam]Type{}, set: map[*TypeParam]bool{}}
	for _, tp := range sig.TParams {
		in.set[tp] = true
	}
	for i, t := range explicit {
		in.m[sig.TParams[i]] = t
	}
	n := args.n()
	allExplicit := len(explicit) == len(sig.TParams)
	failed := false
	ops := make([]*operand, n)
	lits := make([]Type, n)
	mismatch := func(at syntax.Node, t, pt Type) {
		// A typed argument that does not match its parameter, or
		// conflicts with what an earlier argument inferred (round-5 M3;
		// go/types infer.go).
		if want := c.subst(pt, in.m); in.hasFree(pt) || c.identical(want, pt) {
			c.errorf(at, CodeType, "the argument has type %s, which does not match %s (D6; type inference)", t, pt)
		} else {
			c.errorf(at, CodeType, "the argument has type %s, which does not match %s, inferred for %s (D6; type inference)", t, want, pt)
		}
	}
	var deferred []int
	for i := 0; i < len(args.exprs); i++ {
		pt := paramType(sig, i)
		if lit, ok := args.exprs[i].(*syntax.FuncLit); ok {
			// A function literal's parameter and result types are written
			// (#214 would infer them), so it is a typed argument: it
			// unifies in source order with the others, before the
			// literals default (round-7 M3; D6 rule 2). Only its unwritten
			// rows take the parameter type's, when known.
			expected, _ := under0(c.subst(pt, in.m)).(*Signature)
			op := c.funcLit(lit, expected, in)
			op.expr = lit
			c.record(lit, op)
			lt, isSig := op.typ.(*Signature)
			if !isSig { // refused (class G): invalid, nothing cascades
				failed = true
				continue
			}
			lits[i] = lt
			if containsInvalid(lt) {
				failed = true // see the invalid-operand rule below
				continue
			}
			if allExplicit || failed || !in.mentions(pt) {
				continue
			}
			if !in.unify(pt, lt, unifyAssign) {
				mismatch(lit, lt, pt)
				failed = true
			}
			continue
		}
		if !allExplicit && in.mentions(pt) && c.isLiteral(args.exprs[i]) {
			// A literal binds a parameter only when no typed argument does
			// (196u rule 6), so it is checked once the typed arguments
			// have bound theirs, with its parameter's type expected.
			deferred = append(deferred, i)
			continue
		}
		var hint Type
		if !in.hasFree(pt) {
			hint = c.subst(pt, in.m)
		}
		op, ok := c.argValue(args.exprs[i], hint, x)
		ops[i] = &op
		if !ok || op.invalid() || containsInvalid(op.typ) {
			// The invalid-operand rule, in this one place (round-8 M5;
			// round 9 class D; D3): an argument that is invalid, or was
			// refused, already has its diagnostic. The call fails with no
			// new diagnostic and its result is invalid, so nothing that
			// depends on the argument (a mismatch, an unbound parameter)
			// is reported. The other arguments are still checked, so
			// their independent errors still report.
			failed = true
			continue
		}
		// Every type argument written: nothing is inferred, and each
		// argument is only assigned (Go calls no inference then).
		if allExplicit || failed || !in.mentions(pt) {
			continue
		}
		if !in.unify(pt, op.typ, unifyAssign) {
			mismatch(args.exprs[i], op.typ, pt)
			failed = true
		}
	}
	if failed {
		for _, i := range deferred {
			c.valueExpr(args.exprs[i], nil) // its own errors still report
		}
		return bad(x)
	}
	// A parameter that only literals bind takes their common default, and
	// mixed defaults are S108 (196u rule 6): Two(1, 'a') binds int and
	// rune, so it names no type. There is no widening to the widest kind (Go)
	// and no inference through a constraint before the default.
	defaults := map[*TypeParam]Type{}
	var order []*TypeParam
	for _, i := range deferred {
		tp, ok := paramType(sig, i).(*TypeParam)
		if !ok || !in.free(tp) {
			continue
		}
		c.inferOps++
		c.silent++
		op := c.expr(args.exprs[i], nil)
		c.silent--
		if op.invalid() {
			// Its own diagnostic, and no other (D3).
			c.valueExpr(args.exprs[i], nil)
			return bad(x)
		}
		prev, seen := defaults[tp]
		switch {
		case !seen:
			defaults[tp] = op.typ
			order = append(order, tp)
		case !c.identical(prev, op.typ):
			c.errorf(x, CodeType, "cannot infer %s; write the type arguments, e.g. %s: its literal arguments take %s and %s (D6; 196u rule 6)", tp.Name, inferExample(x.Fun, sig), prev, op.typ)
			return bad(x)
		}
	}
	for _, tp := range order {
		in.m[tp] = defaults[tp]
	}
	if args.trailing != nil {
		// A trailing block writes no types: its result binds what is still
		// free, after the constants (D6).
		i := len(args.exprs)
		pt := paramType(sig, i)
		expected, _ := under0(c.subst(pt, in.m)).(*Signature)
		if expected == nil {
			c.errorf(args.trailing, CodeType, "a trailing block is a function-literal argument, but the final parameter has type %s", pt)
			return bad(x)
		}
		lt, refused := c.blockLit(args.trailing, expected, in, true)
		if refused { // class G: invalid, nothing cascades
			return bad(x)
		}
		lits[i] = lt
		_ = in.unify(pt, lt, unifyAssign) // a block's mismatch is the assignment's error below
	}
	for _, tp := range sig.TParams {
		if in.m[tp] != nil {
			continue
		}
		if tp.Kind != KindType {
			in.m[tp] = &RowArg{Row: newRow()}
			continue
		}
		// Outside the inferred subset (D6 as amended in round 7): never
		// from the expected result, nor through a method or core-type
		// constraint; the fix is to write the type arguments.
		c.cannotInfer(x, tp.Name, x.Fun, sig)
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
		c.callInfo(x).TypeArgs = targs
	}
	inst := c.subst(sig, in.m).(*Signature)
	inst.TParams = nil
	inst.NBracket = 0
	// The callee and its parentheses record the instantiated signature, as
	// go/types records it, not the generic one recorded while they were
	// checked (D1; round-3 M4). An explicit instantiation Id[int] records
	// it on the index expression and the parentheses around it; the
	// generic name inside keeps its generic type, as in go/types (round-4
	// M3). An inferred call Id(1) records the instance on the name.
	for f := x.Fun; f != nil; {
		c.record(f, operand{mode: modeValue, typ: inst})
		if p, ok := f.(*syntax.ParenExpr); ok {
			f = p.X
			continue
		}
		break
	}
	for i := 0; i < n; i++ {
		pt := paramType(inst, i)
		if lits[i] != nil {
			if ok, why := c.assignable(lits[i], pt); !ok {
				at := syntax.Node(args.trailing)
				if i < len(args.exprs) {
					at = args.exprs[i]
				}
				c.errorf(at, CodeType, "this function literal cannot be %s: %s", pt, why)
				// One diagnostic for the call; its result is invalid, so
				// nothing cascades (round-7 M3; D3).
				return bad(x)
			}
			continue
		}
		if ops[i] == nil {
			// A deferred literal takes its parameter's type (D-A2).
			op, _ := c.argValue(args.exprs[i], pt, x)
			ops[i] = &op
		}
		c.assign(ops[i], pt, "argument")
	}
	return c.result(x, inst)
}

// ---------------------------------------------------------------- literals and blocks

// funcLit checks a function literal. Its rows are declared when written,
// else taken from the function type it is passed to when that type's rows
// are known, else inferred from its body (rows-and-constraints §2, "Literals
// land").
func (c *checker) funcLit(x *syntax.FuncLit, expected *Signature, in *inferer) operand {
	c.typedFrame(x)
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
	// Class G: a literal that refers to a refused group member is invalid
	// (D3); which literals are value uses is lexical (refWalker).
	frame := c.pushLit()
	c.body(t, x.Type, sig.Params, func() {
		c.functionBody(x.Body, "this function literal")
	})
	sig.Errors = t.row(t.declErrors, t.errors)
	sig.Uses = t.row(t.declUses, t.uses)
	if !writtenErr && !sig.Errors.Empty() {
		sig.Fallible = true
	}
	if c.silent == 0 {
		c.pkg.Info.Literals[x] = sig
	}
	if c.endLit(frame) {
		return bad(x)
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
		v := &Var{Name: p.Name, Type: p.Type, Pos: p.Pos, param: true, used: true}
		c.def(c.paramIdents[p.Pos], v)
		frame.insert(p.Name, v)
	}
	c.bindRequirements(frame, ft)
	check()
	c.fn.t, c.fn.scope, c.fn.branches = savedT, savedScope, savedBranches
}

// blockLit checks a trailing block or a go block as a function literal with
// no parameters (grammar.md, Expressions 3): f(a) { body } is
// f(a, func() T { return body }). The block's last expression is its result;
// return, break and continue may not leave it (grammar.md, Statements 5).
func (c *checker) blockLit(b *syntax.BlockStmt, expected *Signature, in *inferer, value bool) (sig *Signature, refused bool) {
	c.typedFrame(b)
	sig = &Signature{Errors: newRow(), Uses: newRow()}
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
	// A trailing block is a function-literal argument: one that refers to
	// a refused group member is invalid (class G, D3). A with block is not.
	var frame *litFrame
	if value {
		frame = c.pushLit()
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
	if frame != nil {
		refused = c.endLit(frame)
	}
	if c.silent == 0 {
		c.pkg.Info.Literals[b] = sig
	}
	return sig, refused
}

// yieldingBlock checks a block that yields its last expression: a with
// block or a trailing block. When results are known, the last
// expression is assigned to them; otherwise the result is its type (a
// literal's default type), or no value; a single unknown result
// is still the expected type of the last expression, as a hint (M16). pass
// receives the last expression's error row, which passes through
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
		c.tail(last, results[0])
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
	t := c.typeOf(&op)
	c.tail(last, t)
	return t
}

// needNoValueOrCall accepts the last expression of a block whose value is
// discarded: as an expression statement (S110 otherwise).
func (c *checker) needNoValueOrCall(op *operand) {
	switch op.mode {
	case modeNoValue:
		return
	case modeValue, modeConst:
		if !isCallLike(op.expr) {
			c.errorf(op.expr, CodeUnused, "this value is not used; an expression statement is a call, try, catch or with")
		}
		return
	}
	c.needValue(op)
}

func isCallLike(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.CallExpr, *syntax.TryExpr, *syntax.CatchExpr:
		return true
	case *syntax.ParenExpr:
		return isCallLike(x.X)
	}
	return false
}

// ---------------------------------------------------------------- builtins

// libraryMethod returns the built-in that a decided library call on t
// replaces: xs.Append(x) is Go's append(xs, x) on a slice, m.Delete(k)
// Go's delete(m, k) on a map (#196, 196v: the built-ins append and delete
// are removed). A slice or map has no field or other method, so the name
// is unambiguous.
func libraryMethod(t Type, name string) *Builtin {
	switch under(t).(type) {
	case *Slice:
		if name == "Append" {
			return &Builtin{Name: "append"}
		}
	case *Map:
		if name == "Delete" {
			return &Builtin{Name: "delete"}
		}
	}
	return nil
}

// libraryCall checks xs.Append(a, …) and m.Delete(k) on the checked
// receiver recv: each element is assignable to the slice's element type
// and the call yields the slice's type; the key is assignable to the
// map's key type and the call has no value.
func (c *checker) libraryCall(x *syntax.CallExpr, b *Builtin, recv operand) operand {
	name := exprText(x.Fun)
	if x.Trailing != nil {
		c.errorf(x.Trailing, CodeType, "%s takes no block", name)
		c.silentArgs(x)
		return bad(x)
	}
	switch b.Name {
	case "append":
		elem := under(recv.typ).(*Slice).Elem
		if len(x.Args) == 0 {
			c.errorf(x, CodeType, "%s takes the elements to append", name)
			return bad(x)
		}
		for _, a := range x.Args {
			op := c.valueExpr(a, elem)
			c.assign(&op, elem, "argument")
		}
		return operand{mode: modeValue, typ: recv.typ}
	default: // delete
		key := under(recv.typ).(*Map).Key
		if len(x.Args) != 1 {
			c.errorf(x, CodeType, "%s takes one key", name)
			c.silentArgs(x)
			return bad(x)
		}
		k := c.valueExpr(x.Args[0], key)
		c.assign(&k, key, "map key")
		return operand{mode: modeNoValue, typ: NoValue}
	}
}

// builtin checks a call of len, the one built-in function (#196, fourth
// round, item 2): its argument is a slice, a Map or a string, and its value
// an int; the length of a constant string is a constant.
func (c *checker) builtin(x *syntax.CallExpr, b *Builtin) operand {
	if x.Trailing != nil {
		c.errorf(x.Trailing, CodeType, "%s takes no block", b.Name)
		return bad(x)
	}
	if b.Name != "len" {
		c.errorf(x, CodeType, "unknown built-in %s", b.Name)
		return bad(x)
	}
	if len(x.Args) != 1 {
		c.errorf(x, CodeType, "len takes one argument")
		c.silentArgs(x)
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
		if isString(u) {
			if op.mode == modeConst {
				return operand{mode: modeConst, typ: Typ[Int], val: constant.MakeInt64(int64(len(constant.StringVal(op.val))))}
			}
			return operand{mode: modeValue, typ: Typ[Int]}
		}
	case *Slice, *Map:
		return operand{mode: modeValue, typ: Typ[Int]}
	}
	c.errorf(x.Args[0], CodeType, "len takes a slice, a Map or a string, not %s", op.typ)
	return bad(x)
}

// removedBuiltins are Go's built-in functions that Ergo does not
// predeclare, each with the Ergo form that replaces it (#196, fourth round,
// item 2; spec D-D5). A use of one is S104 naming that form.
var removedBuiltins = map[string]string{
	"append":  "write xs.Append(x)",
	"delete":  "write m.Delete(k)",
	"make":    "write a literal: []T{} or Map[K, V]{}",
	"new":     "write a literal T{…}",
	"cap":     "a slice's capacity is not part of the language; use len",
	"copy":    "build the slice with a for loop and xs.Append(x)",
	"close":   "Ergo has no channels",
	"clear":   "assign an empty literal",
	"print":   "write Console.Println(\"…\")",
	"println": "write Console.Println(\"…\")",
	"min":     "write if a < b { a } else { b }",
	"max":     "write if a > b { a } else { b }",
	"panic":   "a failure is a value: declare it with fails and return it",
	"recover": "a failure is a value: handle it with catch",
	"complex": "Ergo has no complex built-ins",
	"real":    "Ergo has no complex built-ins",
	"imag":    "Ergo has no complex built-ins",
}
