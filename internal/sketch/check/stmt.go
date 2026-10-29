package check

import (
	"ergo.local/prototype/internal/sketch/syntax"
)

func (c *checker) stmtList(list []syntax.Stmt, _ bool) {
	for _, s := range list {
		c.stmt(s)
	}
}

func (c *checker) openScope() func() {
	saved := c.fn.scope
	c.fn.scope = newScope(saved)
	return func() { c.fn.scope = saved }
}

// declareLocal declares a local in the current block. A second declaration
// in one block is S108; a local beside a capability-row binding of the same
// name in the body's frame is S104 (rows-and-constraints §7 amendment 3).
func (c *checker) declareLocal(id *syntax.Ident, t Type) *Var {
	if id.Name == "_" {
		return nil
	}
	sc := c.fn.scope
	if prev, ok := sc.names[id.Name]; ok {
		switch prev := prev.(type) {
		case *capBinding:
			c.errorf(id, CodeUnknown, "%s is in this function's capability row, so a local of that name in the same frame is an error; rename the local (rows-and-constraints §7 amendment 3)", id.Name)
		case *Var:
			// The second declaration is the error; the first is not
			// also reported as unused (no cascade, D3).
			prev.used = true
			c.errorf(id, CodeType, "%s is already declared in this block", id.Name)
		default:
			c.errorf(id, CodeType, "%s is already declared in this block", id.Name)
		}
	}
	if t == nil {
		t = invalid
	}
	v := c.bindLocal(&Var{Name: id.Name, Type: t, Pos: id.Pos()})
	c.def(id, v)
	sc.insert(id.Name, v)
	if isInvalid(t) {
		v.used = true
	}
	c.fn.locals = append(c.fn.locals, v)
	c.recordUse(id, v)
	return v
}

func (c *checker) stmt(s syntax.Stmt) {
	savedMark := c.stmtMark
	c.stmtMark = c.undiagSeq
	defer func() { c.stmtMark = savedMark }()
	switch s := s.(type) {
	case nil:
	case *syntax.DeclStmt:
		c.varDecl(s.Decl)
	case *syntax.ExprStmt:
		c.exprStmt(s)
	case *syntax.AssignStmt:
		c.assignStmt(s)
	case *syntax.ReturnStmt:
		c.returnStmt(s)
	case *syntax.BranchStmt:
		c.branchStmt(s)
	case *syntax.BlockStmt:
		// A bare block yields its last expression; return, break and
		// continue may not leave it (grammar.md, Statements 5).
		done := c.openScope()
		c.fn.branches = append(c.fn.branches, branch{kind: brBarrier})
		c.yieldingBlock(s, s.List, nil, false, func(op *operand) { c.handled(op) })
		c.fn.branches = c.fn.branches[:len(c.fn.branches)-1]
		done()
	case *syntax.IfStmt:
		if s.Init != nil {
			c.undecided(s.Init, "an if header's init statement")
			return
		}
		done := c.openScope()
		c.condition(s.Cond)
		c.block(s.Body)
		if s.Else != nil {
			c.stmt(s.Else)
		}
		done()
	case *syntax.SwitchStmt:
		c.switchStmt(s)
	case *syntax.TypeSwitchStmt:
		c.typeSwitch(s)
	case *syntax.ForStmt:
		c.forStmt(s)
	case *syntax.RangeStmt:
		c.rangeStmt(s)
	case *syntax.AssertStmt:
		c.notYet(s)
	default:
		c.undecided(s, "")
	}
}

func (c *checker) block(b *syntax.BlockStmt) {
	if b == nil {
		return
	}
	done := c.openScope()
	c.stmtList(b.List, false)
	done()
}

func (c *checker) condition(x syntax.Expr) {
	if x == nil {
		return
	}
	op := c.valueExpr(x, nil)
	if op.invalid() {
		return
	}
	if !isBoolean(op.typ) {
		c.errorf(x, CodeType, "a condition must be a bool, not %s", op.typ)
	}
}

// exprStmt checks an expression statement: a call, try, catch or with
// (S110 otherwise). A fallible statement is S105.
func (c *checker) exprStmt(s *syntax.ExprStmt) {
	x := unparen(s.X)
	var op operand
	switch e := x.(type) {
	case *syntax.CatchExpr:
		op = c.catch(e, nil, false, false)
		op.expr = s.X
	case *syntax.TryExpr:
		if ce, ok := unparen(e.X).(*syntax.CatchExpr); ok {
			op = c.catch(ce, nil, false, true)
			if !op.invalid() {
				c.escape(e, op.errs)
				op.errs = nil
			}
			return
		}
		op = c.expr(s.X, nil)
	default:
		op = c.expr(s.X, nil)
	}
	if op.invalid() {
		return
	}
	switch op.mode {
	case modeValue, modeConst, modeNoValue:
	default:
		c.needValue(&op)
		if op.invalid() {
			return
		}
	}
	c.handled(&op)
	if !isCallLike(x) {
		c.errorf(s.X, CodeUnused, "this value is not used; an expression statement is a call, try, catch or with")
		return
	}
	if call, ok := x.(*syntax.CallExpr); ok && op.mode != modeNoValue {
		if fun, ok := unparen(call.Fun).(*syntax.Ident); ok && fun.Name != "with" {
			if o, isB := c.lookup(fun.Name).(*Builtin); isB {
				c.errorf(s.X, CodeUnused, "the value of %s is not used", o.Name)
				return
			}
			if tn, isT := c.lookup(fun.Name).(*TypeName); isT && !isCapability(tn.Type) {
				c.errorf(s.X, CodeUnused, "the conversion to %s is not used", tn.Name)
				return
			}
		}
	}
}

// varDecl checks a local var declaration. A var of a type with no zero
// value needs a value (types.md, Zero values 3).
func (c *checker) varDecl(d *syntax.VarDecl) {
	for _, spec := range d.Specs {
		if spec.Pattern != nil {
			c.patternLet(spec)
			continue
		}
		var declared Type
		if spec.Type != nil {
			declared = c.typExpr(spec.Type, c.localTypeCtx())
		}
		switch {
		case len(spec.Values) == 0:
			if declared != nil && !isInvalid(declared) && !defaultable(declared) {
				c.errorf(spec.Names[0], CodeType, "var %s of type %s needs a value: %s has no zero value (types.md, Zero values 3)", spec.Names[0].Name, declared, declared)
			}
			for _, n := range spec.Names {
				c.declareLocal(n, declared)
			}
		case len(spec.Values) == len(spec.Names):
			types := make([]Type, len(spec.Names))
			for i, v := range spec.Values {
				op := c.valueExpr(v, declared)
				if declared != nil {
					c.assign(&op, declared, "var "+spec.Names[i].Name)
					types[i] = declared
				} else {
					types[i] = c.typeOf(&op)
				}
			}
			for i, n := range spec.Names {
				c.declareLocal(n, types[i])
			}
		case len(spec.Values) == 1:
			types := c.multiValue(spec.Values[0], len(spec.Names))
			for i, n := range spec.Names {
				t := types[i]
				if declared != nil && !isInvalid(t) {
					if ok, why := c.assignable(t, declared); !ok {
						c.errorf(spec.Values[0], CodeType, "cannot use %s as %s: %s", t, declared, why)
					}
					t = declared
				}
				c.declareLocal(n, t)
			}
		default:
			c.errorf(spec, CodeType, "%d names and %d values", len(spec.Names), len(spec.Values))
			for _, n := range spec.Names {
				c.declareLocal(n, invalid)
			}
		}
	}
}

// multiValue checks a value that must give n values: a call with n
// results, or a comma-ok map index.
func (c *checker) multiValue(x syntax.Expr, n int) []Type {
	out := make([]Type, n)
	for i := range out {
		out[i] = invalid
	}
	if n == 2 {
		switch e := unparen(x).(type) {
		case *syntax.IndexExpr:
			op := c.valueExpr(e, nil)
			if op.invalid() {
				return out
			}
			if base, ok := c.pkg.Info.Types[e.X]; ok {
				if _, isMap := under(base).(*Map); isMap {
					out[0], out[1] = op.typ, Typ[Bool]
					return out
				}
			}
			c.errorf(x, CodeType, "2 values from one value")
			return out
		}
	}
	op := c.valueExpr(x, nil)
	if op.invalid() {
		return out
	}
	tup, ok := op.typ.(*Tuple)
	if !ok || len(tup.Types) != n {
		got := 1
		if ok {
			got = len(tup.Types)
		}
		if op.mode == modeNoValue {
			got = 0
		}
		c.errorf(x, CodeType, "%d names for %d values", n, got)
		return out
	}
	copy(out, tup.Types)
	return out
}

// assignStmt checks `x = e` and `x op= e`: one target and one value. A
// `:=` or a statement with several sides is not produced by the decided
// parser (D-A5).
func (c *checker) assignStmt(s *syntax.AssignStmt) {
	if s.Tok == syntax.DEFINE || len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		c.undecided(s, "a declaring assignment, or one with several sides,")
		return
	}
	l, r := s.Lhs[0], s.Rhs[0]
	if s.Tok == syntax.ASSIGN {
		if isBlank(l) {
			op := c.valueExpr(r, nil)
			c.typeOf(&op)
			return
		}
		lop := c.lhs(l)
		var hint Type
		if !lop.invalid() {
			hint = lop.typ
		}
		op := c.valueExpr(r, hint)
		if lop.invalid() {
			return
		}
		c.assign(&op, lop.typ, "assignment")
		return
	}
	lop := c.lhs(l)
	bin := &syntax.BinaryExpr{X: l, OpPos: s.TokPos, Op: assignOp(s.Tok), Y: r}
	if lop.invalid() {
		c.valueExpr(r, nil)
		return
	}
	op := c.binary(bin, nil)
	op.expr = r
	if !op.invalid() {
		c.assign(&op, lop.typ, "assignment")
	}
}

func isBlank(x syntax.Expr) bool {
	id, ok := x.(*syntax.Ident)
	return ok && id.Name == "_"
}

func assignOp(t syntax.Token) syntax.Token {
	switch t {
	case syntax.ADD_ASSIGN:
		return syntax.ADD
	case syntax.SUB_ASSIGN:
		return syntax.SUB
	case syntax.MUL_ASSIGN:
		return syntax.MUL
	case syntax.QUO_ASSIGN:
		return syntax.QUO
	case syntax.REM_ASSIGN:
		return syntax.REM
	case syntax.AND_ASSIGN:
		return syntax.AND
	case syntax.OR_ASSIGN:
		return syntax.OR
	case syntax.XOR_ASSIGN:
		return syntax.XOR
	case syntax.SHL_ASSIGN:
		return syntax.SHL
	case syntax.SHR_ASSIGN:
		return syntax.SHR
	case syntax.AND_NOT_ASSIGN:
		return syntax.AND_NOT
	}
	return syntax.ILLEGAL
}

// lhs checks an assignment target: a local, a field or an element. A
// package-level var is immutable (grammar.md, Declarations 6). Assigning
// does not read the variable.
func (c *checker) lhs(x syntax.Expr) operand {
	if id, ok := unparen(x).(*syntax.Ident); ok {
		o := c.lookup(id.Name)
		if v, ok := o.(*Var); ok {
			if v.pkg {
				c.errorf(x, CodeType, "the package-level var %s is immutable (grammar.md, Declarations 6)", id.Name)
				return bad(x)
			}
			if v.Type == nil || isInvalid(v.Type) {
				return bad(x)
			}
			c.recordUse(id, v)
			op := operand{mode: modeValue, typ: v.Type, addressable: true, expr: x}
			// Info records the assigned variable's type, as go/types
			// records an assignment's left side (D1).
			for e := x; ; {
				c.record(e, op)
				p, ok := e.(*syntax.ParenExpr)
				if !ok {
					break
				}
				e = p.X
			}
			return op
		}
	}
	op := c.valueExpr(x, nil)
	if op.invalid() {
		return op
	}
	c.assignable0(x, &op)
	return op
}

func (c *checker) assignable0(x syntax.Expr, op *operand) {
	if !op.addressable && !op.mapElem {
		c.errorf(x, CodeType, "cannot assign to %s", exprText(x))
		op.mode = modeInvalid
	}
}

// returnStmt checks return: a fallible value passes its success and its
// failures through; a value of an error type is a failure; return cannot
// leave a with block or a trailing block (rows.md, Error rows 4;
// grammar.md, Statements 5).
func (c *checker) returnStmt(s *syntax.ReturnStmt) {
	t := c.fn.t
	for _, b := range c.fn.branches {
		if b.kind == brBarrier {
			c.errorf(s, CodeType, "return cannot leave a with block or a block argument; the block yields its last expression (grammar.md, Statements 5)")
			for _, r := range s.Results {
				c.expr(r, nil)
			}
			return
		}
	}
	if !t.isFunc {
		c.errorf(s, CodeType, "return is not allowed here")
		return
	}
	results := t.results
	switch len(s.Results) {
	case 0:
		if len(results) > 0 {
			c.errorf(s, CodeType, "missing return value of type %s", results[0])
		}
		return
	case 1:
		var hint Type
		if len(results) == 1 {
			hint = results[0]
		}
		op := c.expr(s.Results[0], hint)
		if op.invalid() {
			return
		}
		switch op.mode {
		case modeValue, modeConst, modeNoValue:
		default:
			c.needValue(&op)
			if op.invalid() {
				return
			}
		}
		if op.fallible() {
			c.escape(s.Results[0], op.errs)
			op.errs = nil
			switch {
			case len(results) == 0 && op.mode != modeNoValue:
				c.errorf(s.Results[0], CodeType, "%s returns no value, and this expression has one", c.describeTarget())
			case len(results) == 1:
				c.assign(&op, results[0], "return")
			case len(results) > 1:
				c.returnTuple(s.Results[0], op, results)
			}
			return
		}
		if row := c.errorValueRow(op.typ); row != nil && op.mode == modeValue {
			c.escape(s.Results[0], row)
			return
		}
		switch {
		case len(results) == 0:
			c.errorf(s.Results[0], CodeType, "%s returns no value", c.describeTarget())
		case len(results) == 1:
			c.assign(&op, results[0], "return")
		default:
			c.returnTuple(s.Results[0], op, results)
		}
	default:
		if len(s.Results) != len(results) {
			c.errorf(s, CodeType, "%d values returned, %d expected", len(s.Results), len(results))
			for _, r := range s.Results {
				c.valueExpr(r, nil)
			}
			return
		}
		for i, r := range s.Results {
			op := c.valueExpr(r, results[i])
			c.assign(&op, results[i], "return")
		}
	}
}

func (c *checker) returnTuple(x syntax.Expr, op operand, results []Type) {
	tup, ok := op.typ.(*Tuple)
	if !ok || len(tup.Types) != len(results) {
		c.errorf(x, CodeType, "%d values expected", len(results))
		return
	}
	for i := range results {
		if ok, why := c.assignable(tup.Types[i], results[i]); !ok {
			c.errorf(x, CodeType, "cannot return %s as %s: %s", tup.Types[i], results[i], why)
		}
	}
}

// errorValueRow returns the failures that returning a value of type t
// escapes: {t} for an error type, the variants for the binding of a catch
// arm that names several error types (rows.md, Catch 7; M4), else nil.
func (c *checker) errorValueRow(t Type) *Row {
	if u, ok := t.(*Union); ok {
		row := newRow()
		for _, v := range u.Variants {
			if !c.isErrorValue(v) {
				return nil
			}
			row = row.with(c.types, v)
		}
		return row
	}
	if c.isErrorValue(t) {
		return c.types.row(t)
	}
	return nil
}

// isErrorValue reports whether t is an error type: a type named in an error
// row of its package (types.md, Error types 1 and 2).
func (c *checker) isErrorValue(t Type) bool {
	n, ok := t.(*Named)
	if !ok {
		return false
	}
	o := n.origin()
	if _, ok := c.errorTypes[o]; ok {
		return true
	}
	if p := o.Obj.Pkg; p != nil && p != c.pkg && p.ErrorTypes != nil {
		return p.ErrorTypes[o]
	}
	return o.Obj.Pkg == universeOnce.goPkg && o.Obj.Name == "Error"
}

// ---------------------------------------------------------------- branches

// branchStmt checks break and continue: they may not leave a with block or
// a block argument (grammar.md, Statements 5). A label is not produced by
// the decided parser (D-A5).
func (c *checker) branchStmt(s *syntax.BranchStmt) {
	if s.Label != nil {
		c.undecided(s.Label, "a label on break or continue")
		return
	}
	crossed := false
	for i := len(c.fn.branches) - 1; i >= 0; i-- {
		b := c.fn.branches[i]
		if b.kind == brBarrier {
			crossed = true
			continue
		}
		if s.Tok == syntax.CONTINUE && b.kind != brLoop {
			continue
		}
		if crossed {
			c.errorf(s, CodeType, "%s cannot leave a with block or a block argument (grammar.md, Statements 5)", s.Tok)
		}
		return
	}
	if crossed {
		c.errorf(s, CodeType, "%s cannot leave a with block or a block argument (grammar.md, Statements 5)", s.Tok)
		return
	}
	c.errorf(s, CodeType, "%s is not inside a for or match", s.Tok)
}

func (c *checker) push(kind branchKind) func() {
	c.fn.branches = append(c.fn.branches, branch{kind: kind})
	return func() { c.fn.branches = c.fn.branches[:len(c.fn.branches)-1] }
}

func (c *checker) forStmt(s *syntax.ForStmt) {
	if s.Init != nil || s.Post != nil {
		c.undecided(s, "a for header with an init or post statement")
		return
	}
	done := c.openScope()
	defer done()
	c.condition(s.Cond)
	pop := c.push(brLoop)
	c.block(s.Body)
	pop()
}

// countRange checks `for i in lo..hi { }` (grammar.md, One for): the
// bounds are integers of one type, a literal bound takes the other's type
// and two literals take int (196u rules 2 and 4), and the one binding
// counts from lo up to hi, excluding hi, as Go's `for i := lo; i < hi;
// i++` does.
func (c *checker) countRange(s *syntax.RangeStmt, r *syntax.BinaryExpr) {
	lo, hi := c.operands(r.X, r.Y, nil)
	t := Type(invalid)
	if !lo.invalid() && !hi.invalid() && c.matchTypes(r, &lo, &hi) {
		t = c.typeOf(&lo)
		if !isInvalid(t) && !isInteger(under(t)) {
			c.errorf(r, CodeType, "a range's bounds are integers, not %s", t)
			t = invalid
		}
	}
	c.record(r, operand{mode: modeValue, typ: t})
	if s.Key != nil {
		c.errorf(s.Key, CodeType, "for over a range binds one name")
	}
	l := &Loop{Domain: LoopRange, Elem: t}
	if id, ok := s.Value.(*syntax.Ident); ok {
		if v := c.declareLocal(id, t); v != nil {
			l.Bindings = append(l.Bindings, v)
		}
	}
	if !isInvalid(t) {
		c.loop(s, l)
	}
	c.rangePatterns(s)
	pop := c.push(brLoop)
	c.block(s.Body)
	pop()
}

// rangeStmt checks `for x in e` and `for i, x in e`. The decided parser
// always declares the bindings (Tok DEFINE); an assignment form is not
// produced by it (D-A5).
func (c *checker) rangeStmt(s *syntax.RangeStmt) {
	if s.Tok != syntax.DEFINE {
		c.undecided(s, "a for header that assigns")
		return
	}
	done := c.openScope()
	defer done()
	if r, ok := s.X.(*syntax.BinaryExpr); ok && r.Op == syntax.DOTDOT {
		c.countRange(s, r)
		return
	}
	x := c.valueExpr(s.X, nil)
	var kt, vt Type = invalid, invalid
	var domain LoopDomain
	if !x.invalid() {
		t := x.typ
		if p, ok := t.(*Pointer); ok {
			t = p.Elem
		}
		switch u := under(t).(type) {
		case *Slice:
			kt, vt, domain = Typ[Int], u.Elem, LoopSlice
		case *Map:
			kt, vt, domain = u.Key, u.Elem, LoopMap
		case *Basic:
			switch {
			case isString(u):
				kt, vt, domain = Typ[Int], Typ[Int32], LoopString
			case isInteger(u):
				kt, vt, domain = x.typ, nil, LoopInt
			default:
				c.errorf(s.X, CodeType, "cannot range over %s", x.typ)
			}
		default:
			c.errorf(s.X, CodeType, "cannot range over %s", x.typ)
		}
	}
	if vt == nil && s.Value != nil {
		c.errorf(s.Value, CodeType, "range over %s permits only one iteration variable", x.typ)
	}
	c.rangePatterns(s)
	// for x in xs binds the element; for i, x in xs the position first.
	l := &Loop{Domain: domain, Elem: vt, Pos: kt}
	if vt == nil {
		l.Elem, l.Pos = kt, nil
	}
	if id, ok := s.Key.(*syntax.Ident); ok {
		if v := c.declareLocal(id, kt); v != nil {
			l.Bindings = append(l.Bindings, v)
		}
	}
	if id, ok := s.Value.(*syntax.Ident); ok && vt != nil {
		if v := c.declareLocal(id, vt); v != nil {
			l.Bindings = append(l.Bindings, v)
		}
	}
	if domain != 0 {
		c.loop(s, l)
	}
	pop := c.push(brLoop)
	c.block(s.Body)
	pop()
}

func (c *checker) switchStmt(s *syntax.SwitchStmt) {
	if s.Init != nil {
		c.undecided(s.Init, "a match header's init statement")
		return
	}
	done := c.openScope()
	defer done()
	var tag operand
	tagType := Type(Typ[Bool])
	if s.Tag != nil {
		tag = c.valueExpr(s.Tag, nil)
		tagType = c.typeOf(&tag)
	}
	pop := c.push(brSwitch)
	defer pop()
	if s.Tag == nil {
		c.match(s, &Match{Arms: len(s.Clauses)})
	} else if !isInvalid(tagType) {
		c.match(s, &Match{Subjects: []Type{tagType}, Arms: len(s.Clauses)})
	}
	defaults := 0
	for _, cl := range s.Clauses {
		if cl.List == nil {
			defaults++
			if defaults > 1 {
				c.errorf(cl, CodeType, "a match has at most one case _")
			}
		}
		for _, e := range cl.List {
			op := c.valueExpr(e, tagType)
			if op.invalid() || isInvalid(tagType) {
				continue
			}
			if s.Tag == nil {
				if !isBoolean(op.typ) {
					c.errorf(e, CodeType, "a case of a match without a subject must be a bool, not %s", op.typ)
				}
				continue
			}
			if op.mode == modeConst {
				c.pattern(e, &Pattern{Kind: PatConst, Value: op.val, Type: tagType})
			}
			l := operand{mode: modeValue, typ: tagType, expr: s.Tag}
			bin := &syntax.BinaryExpr{X: s.Tag, Op: syntax.EQL, Y: e, OpPos: cl.Case}
			if c.matchTypes(bin, &l, &op) && !comparable(l.typ) {
				c.errorf(e, CodeType, "%s values cannot be compared", l.typ)
			}
		}
		inner := c.openScope()
		c.stmtList(cl.Body, false)
		inner()
	}
}

// ---------------------------------------------------------------- termination

// terminatesList reports whether a statement list ends in a terminating
// statement: Go's rules, plus a total type switch whose cases all terminate
// (rows.md, Totality 3; D8).
func (c *checker) terminatesList(list []syntax.Stmt) bool {
	if len(list) == 0 {
		return false
	}
	return c.terminates(list[len(list)-1])
}

func (c *checker) terminates(s syntax.Stmt) bool {
	switch s := s.(type) {
	case *syntax.ReturnStmt:
		return true
	case *syntax.BlockStmt:
		return c.terminatesList(s.List)
	case *syntax.IfStmt:
		return s.Else != nil && c.terminatesList(s.Body.List) && c.terminates(s.Else)
	case *syntax.ForStmt:
		return s.Cond == nil && !hasBreak(s.Body)
	case *syntax.SwitchStmt:
		hasDefault := false
		for _, cl := range s.Clauses {
			if cl.List == nil {
				hasDefault = true
			}
			if !c.terminatesList(cl.Body) || hasBreakList(cl.Body) {
				return false
			}
		}
		return hasDefault
	case *syntax.TypeSwitchStmt:
		if !c.totalSwitch[s] {
			return false
		}
		for _, cl := range s.Clauses {
			if !c.terminatesList(cl.Body) || hasBreakList(cl.Body) {
				return false
			}
		}
		return true
	}
	return false
}

// hasBreak reports whether a break leaves the statement being analysed: a
// break outside any nested for or match (the decided language has no
// labels).
func hasBreak(s syntax.Stmt) bool {
	found := false
	syntax.Inspect(s, func(x syntax.Node) bool {
		if found {
			return false
		}
		switch x := x.(type) {
		case *syntax.FuncLit:
			return false
		case *syntax.BranchStmt:
			if x.Tok == syntax.BREAK {
				found = true
			}
		case *syntax.ForStmt, *syntax.RangeStmt, *syntax.SwitchStmt, *syntax.TypeSwitchStmt:
			return syntax.Node(x) == syntax.Node(s)
		}
		return true
	})
	return found
}

func hasBreakList(list []syntax.Stmt) bool {
	for _, s := range list {
		if hasBreak(s) {
			return true
		}
	}
	return false
}
