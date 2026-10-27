package check

import (
	"effra.local/prototype/internal/sketch/syntax"
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
		switch prev.(type) {
		case *capBinding:
			c.errorf(id, CodeUnknown, "%s is in this function's capability row, so a local of that name in the same frame is an error; rename the local (rows-and-constraints §7 amendment 3)", id.Name)
		default:
			c.errorf(id, CodeType, "%s is already declared in this block", id.Name)
		}
	}
	if t == nil {
		t = invalid
	}
	v := &Var{Name: id.Name, Type: t, Pos: id.Pos()}
	sc.insert(id.Name, v)
	if isInvalid(t) {
		v.used = true
	}
	c.fn.locals = append(c.fn.locals, v)
	c.recordUse(id, v)
	return v
}

func (c *checker) stmt(s syntax.Stmt) {
	switch s := s.(type) {
	case nil, *syntax.EmptyStmt:
	case *syntax.DeclStmt:
		c.varDecl(s.Decl)
	case *syntax.LabeledStmt:
		c.labeled(s)
	case *syntax.ExprStmt:
		c.exprStmt(s)
	case *syntax.SendStmt:
		ch := c.valueExpr(s.Chan, nil)
		if ch.invalid() {
			c.valueExpr(s.Value, nil)
			return
		}
		ct, ok := under(ch.typ).(*Chan)
		if !ok || ct.Dir == syntax.ChanRecv {
			c.errorf(s.Chan, CodeType, "cannot send to %s", ch.typ)
			c.valueExpr(s.Value, nil)
			return
		}
		v := c.valueExpr(s.Value, ct.Elem)
		c.assign(&v, ct.Elem, "send")
	case *syntax.IncDecStmt:
		op := c.valueExpr(s.X, nil)
		if op.invalid() {
			return
		}
		if !isNumeric(op.typ) {
			c.errorf(s, CodeType, "%s is not defined on %s", s.Tok, op.typ)
			return
		}
		c.assignable0(s.X, &op)
	case *syntax.AssignStmt:
		c.assignStmt(s)
	case *syntax.DeferStmt:
		c.stmt(s.Stmt)
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
		done := c.openScope()
		if s.Init != nil {
			c.stmt(s.Init)
		}
		c.condition(s.Cond)
		c.block(s.Body)
		if s.Else != nil {
			c.stmt(s.Else)
		}
		done()
	case *syntax.SwitchStmt:
		c.switchStmt(s, "")
	case *syntax.TypeSwitchStmt:
		c.typeSwitch(s, "")
	case *syntax.SelectStmt:
		c.selectStmt(s, "")
	case *syntax.ForStmt:
		c.forStmt(s, "")
	case *syntax.RangeStmt:
		c.rangeStmt(s, "")
	default:
		c.errorf(s, CodeType, "unexpected statement")
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

// exprStmt checks an expression statement: a call, a receive, try, catch,
// with or go, as in Go (S110 otherwise). A fallible statement is S105.
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
		return
	}
	c.handled(&op)
	if !isCallLike(x) {
		c.errorf(s.X, CodeUnused, "this value is not used; an expression statement is a call, a receive, try or catch (as in Go)")
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
					types[i] = c.defaultOf(&op)
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
// results, or a comma-ok form (a map index, a receive).
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
		case *syntax.UnaryExpr:
			if e.Op == syntax.ARROW {
				op := c.valueExpr(e, nil)
				if op.invalid() {
					return out
				}
				out[0], out[1] = op.typ, Typ[Bool]
				return out
			}
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

func (c *checker) assignStmt(s *syntax.AssignStmt) {
	switch s.Tok {
	case syntax.DEFINE:
		ids := make([]*syntax.Ident, len(s.Lhs))
		for i, l := range s.Lhs {
			id, ok := l.(*syntax.Ident)
			if !ok {
				c.errorf(l, CodeType, "the left side of := is names only")
				return
			}
			ids[i] = id
		}
		var types []Type
		switch {
		case len(s.Rhs) == len(ids):
			types = make([]Type, len(ids))
			for i, r := range s.Rhs {
				op := c.valueExpr(r, nil)
				types[i] = c.defaultOf(&op)
			}
		case len(s.Rhs) == 1:
			types = c.multiValue(s.Rhs[0], len(ids))
		default:
			c.errorf(s, CodeType, "%d names and %d values", len(ids), len(s.Rhs))
			types = make([]Type, len(ids))
		}
		for i, id := range ids {
			c.declareLocal(id, types[i])
		}
	case syntax.ASSIGN:
		var types []Type
		if len(s.Rhs) == 1 && len(s.Lhs) > 1 {
			types = c.multiValue(s.Rhs[0], len(s.Lhs))
			for i, l := range s.Lhs {
				if isBlank(l) {
					continue
				}
				lop := c.lhs(l)
				if lop.invalid() || isInvalid(types[i]) {
					continue
				}
				if ok, why := c.assignable(types[i], lop.typ); !ok {
					c.errorf(l, CodeType, "cannot assign %s to %s: %s", types[i], lop.typ, why)
				}
			}
			return
		}
		if len(s.Rhs) != len(s.Lhs) {
			c.errorf(s, CodeType, "%d targets and %d values", len(s.Lhs), len(s.Rhs))
			return
		}
		for i, l := range s.Lhs {
			if isBlank(l) {
				op := c.valueExpr(s.Rhs[i], nil)
				c.defaultOf(&op)
				continue
			}
			lop := c.lhs(l)
			var hint Type
			if !lop.invalid() {
				hint = lop.typ
			}
			op := c.valueExpr(s.Rhs[i], hint)
			if lop.invalid() {
				continue
			}
			c.assign(&op, lop.typ, "assignment")
		}
	default:
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
			c.errorf(s, CodeType, "%s takes one target and one value", s.Tok)
			return
		}
		lop := c.lhs(s.Lhs[0])
		bin := &syntax.BinaryExpr{X: s.Lhs[0], OpPos: s.TokPos, Op: assignOp(s.Tok), Y: s.Rhs[0]}
		if lop.invalid() {
			c.valueExpr(s.Rhs[0], nil)
			return
		}
		op := c.binary(bin, nil)
		op.expr = s.Rhs[0]
		if !op.invalid() {
			c.assign(&op, lop.typ, "assignment")
		}
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
	if id, ok := x.(*syntax.Ident); ok {
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
			return operand{mode: modeValue, typ: v.Type, addressable: true, expr: x}
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
	if !op.addressable {
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
			return
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
		if c.isErrorValue(op.typ) && op.mode == modeValue {
			c.escape(s.Results[0], newRow(op.typ))
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

func (c *checker) labeled(s *syntax.LabeledStmt) {
	label := s.Label.Name
	for _, b := range c.fn.branches {
		if b.label == label {
			c.errorf(s.Label, CodeType, "label %s is already defined", label)
		}
	}
	if c.fn.labelUsed == nil {
		c.fn.labelUsed = map[string]bool{}
	}
	delete(c.fn.labelUsed, label)
	defer func() {
		if !c.fn.labelUsed[label] {
			c.errorf(s.Label, CodeUnused, "label %s is defined and not used (as in Go)", label)
		}
	}()
	switch inner := s.Stmt.(type) {
	case *syntax.ForStmt:
		c.forStmt(inner, label)
	case *syntax.RangeStmt:
		c.rangeStmt(inner, label)
	case *syntax.SwitchStmt:
		c.switchStmt(inner, label)
	case *syntax.TypeSwitchStmt:
		c.typeSwitch(inner, label)
	case *syntax.SelectStmt:
		c.selectStmt(inner, label)
	default:
		c.fn.branches = append(c.fn.branches, branch{kind: brLabel, label: label})
		c.stmt(s.Stmt)
		c.fn.branches = c.fn.branches[:len(c.fn.branches)-1]
	}
}

// branchStmt checks break and continue: they may not leave a with block or
// a block argument (grammar.md, Statements 5).
func (c *checker) branchStmt(s *syntax.BranchStmt) {
	crossed := false
	for i := len(c.fn.branches) - 1; i >= 0; i-- {
		b := c.fn.branches[i]
		if b.kind == brBarrier {
			crossed = true
			continue
		}
		if s.Label != nil {
			if b.label != s.Label.Name {
				continue
			}
			if c.fn.labelUsed != nil {
				c.fn.labelUsed[b.label] = true
			}
			if s.Tok == syntax.CONTINUE && b.kind != brLoop {
				c.errorf(s, CodeType, "continue %s: %s does not label a loop", s.Label.Name, s.Label.Name)
				return
			}
			if b.kind == brLabel && s.Tok == syntax.BREAK {
				c.errorf(s, CodeType, "break %s: %s does not label a loop, switch or select", s.Label.Name, s.Label.Name)
				return
			}
		} else {
			if s.Tok == syntax.CONTINUE && b.kind != brLoop {
				continue
			}
			if b.kind == brLabel {
				continue
			}
		}
		if crossed {
			c.errorf(s, CodeType, "%s cannot leave a with block or a block argument (grammar.md, Statements 5)", s.Tok)
		}
		return
	}
	if s.Label != nil {
		c.errorf(s.Label, CodeUnknown, "unknown label %s", s.Label.Name)
		return
	}
	if crossed {
		c.errorf(s, CodeType, "%s cannot leave a with block or a block argument (grammar.md, Statements 5)", s.Tok)
		return
	}
	c.errorf(s, CodeType, "%s is not inside a loop, switch or select", s.Tok)
}

func (c *checker) push(kind branchKind, label string) func() {
	c.fn.branches = append(c.fn.branches, branch{kind: kind, label: label})
	return func() { c.fn.branches = c.fn.branches[:len(c.fn.branches)-1] }
}

func (c *checker) forStmt(s *syntax.ForStmt, label string) {
	done := c.openScope()
	defer done()
	if s.Init != nil {
		c.stmt(s.Init)
	}
	c.condition(s.Cond)
	if s.Post != nil {
		c.stmt(s.Post)
	}
	pop := c.push(brLoop, label)
	c.block(s.Body)
	pop()
}

func (c *checker) rangeStmt(s *syntax.RangeStmt, label string) {
	done := c.openScope()
	defer done()
	x := c.valueExpr(s.X, nil)
	var kt, vt Type = invalid, invalid
	if !x.invalid() {
		t := x.typ
		if p, ok := t.(*Pointer); ok {
			t = p.Elem
		}
		switch u := under(t).(type) {
		case *Slice:
			kt, vt = Typ[Int], u.Elem
		case *Array:
			kt, vt = Typ[Int], u.Elem
		case *Map:
			kt, vt = u.Key, u.Elem
		case *Chan:
			kt, vt = u.Elem, nil
		case *Basic:
			switch {
			case isString(u):
				kt, vt = Typ[Int], Typ[Int32]
			case isInteger(u):
				kt, vt = defaultType(x.typ), nil
				if x.mode == modeConst {
					c.assign(&x, kt, "range")
				}
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
	switch s.Tok {
	case syntax.DEFINE:
		if id, ok := s.Key.(*syntax.Ident); ok {
			c.declareLocal(id, kt)
		}
		if id, ok := s.Value.(*syntax.Ident); ok && vt != nil {
			c.declareLocal(id, vt)
		}
	case syntax.ASSIGN:
		for i, l := range []syntax.Expr{s.Key, s.Value} {
			if l == nil || isBlank(l) {
				continue
			}
			lop := c.lhs(l)
			want := kt
			if i == 1 {
				want = vt
			}
			if !lop.invalid() && want != nil && !isInvalid(want) {
				if ok, why := c.assignable(want, lop.typ); !ok {
					c.errorf(l, CodeType, "cannot assign %s to %s: %s", want, lop.typ, why)
				}
			}
		}
	}
	pop := c.push(brLoop, label)
	c.block(s.Body)
	pop()
}

func (c *checker) switchStmt(s *syntax.SwitchStmt, label string) {
	done := c.openScope()
	defer done()
	if s.Init != nil {
		c.stmt(s.Init)
	}
	var tag operand
	tagType := Type(Typ[Bool])
	if s.Tag != nil {
		tag = c.valueExpr(s.Tag, nil)
		tagType = c.defaultOf(&tag)
	}
	pop := c.push(brSwitch, label)
	defer pop()
	defaults := 0
	for _, cl := range s.Clauses {
		if cl.List == nil {
			defaults++
			if defaults > 1 {
				c.errorf(cl, CodeType, "a switch has at most one default")
			}
		}
		for _, e := range cl.List {
			op := c.valueExpr(e, tagType)
			if op.invalid() || isInvalid(tagType) {
				continue
			}
			if s.Tag == nil {
				if !isBoolean(op.typ) {
					c.errorf(e, CodeType, "a case of a switch without a tag must be a bool, not %s", op.typ)
				}
				continue
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

func (c *checker) selectStmt(s *syntax.SelectStmt, label string) {
	pop := c.push(brSelect, label)
	defer pop()
	defaults := 0
	for _, cl := range s.Clauses {
		done := c.openScope()
		switch comm := cl.Comm.(type) {
		case nil:
			defaults++
			if defaults > 1 {
				c.errorf(cl, CodeType, "a select has at most one default")
			}
		case *syntax.SendStmt:
			c.stmt(comm)
		case *syntax.ExprStmt:
			if u, ok := unparen(comm.X).(*syntax.UnaryExpr); !ok || u.Op != syntax.ARROW {
				c.errorf(comm, CodeType, "a select case is a send or a receive")
			} else {
				c.valueExpr(comm.X, nil)
			}
		case *syntax.AssignStmt:
			if len(comm.Rhs) != 1 {
				c.errorf(comm, CodeType, "a select case is a send or a receive")
				break
			}
			if u, ok := unparen(comm.Rhs[0]).(*syntax.UnaryExpr); !ok || u.Op != syntax.ARROW {
				c.errorf(comm, CodeType, "a select case is a send or a receive")
				break
			}
			c.assignStmt(comm)
		default:
			c.errorf(comm, CodeType, "a select case is a send or a receive")
		}
		c.stmtList(cl.Body, false)
		done()
	}
}

// ---------------------------------------------------------------- termination

// terminatesList reports whether a statement list ends in a terminating
// statement: Go's rules, plus a total type switch whose cases all terminate
// (rows.md, Totality 3; D8).
func (c *checker) terminatesList(list []syntax.Stmt) bool {
	for i := len(list) - 1; i >= 0; i-- {
		if _, empty := list[i].(*syntax.EmptyStmt); empty {
			continue
		}
		return c.terminates(list[i], "")
	}
	return false
}

func (c *checker) terminates(s syntax.Stmt, label string) bool {
	switch s := s.(type) {
	case *syntax.ReturnStmt:
		return true
	case *syntax.BlockStmt:
		return c.terminatesList(s.List)
	case *syntax.IfStmt:
		return s.Else != nil && c.terminatesList(s.Body.List) && c.terminates(s.Else, "")
	case *syntax.LabeledStmt:
		return c.terminates(s.Stmt, s.Label.Name)
	case *syntax.ForStmt:
		return s.Cond == nil && !hasBreak(s.Body, label, true)
	case *syntax.SwitchStmt:
		hasDefault := false
		for _, cl := range s.Clauses {
			if cl.List == nil {
				hasDefault = true
			}
			if !c.terminatesList(cl.Body) || hasBreakList(cl.Body, label, true) {
				return false
			}
		}
		return hasDefault
	case *syntax.TypeSwitchStmt:
		if !c.totalSwitch[s] {
			return false
		}
		for _, cl := range s.Clauses {
			if !c.terminatesList(cl.Body) || hasBreakList(cl.Body, label, true) {
				return false
			}
		}
		return true
	case *syntax.SelectStmt:
		for _, cl := range s.Clauses {
			if !c.terminatesList(cl.Body) || hasBreakList(cl.Body, label, true) {
				return false
			}
		}
		return true
	}
	return false
}

// hasBreak reports whether a break leaves the statement being analysed: an
// unlabeled break outside any nested loop, switch or select, or a break of
// its label.
func hasBreak(s syntax.Stmt, label string, top bool) bool {
	found := false
	var walk func(n syntax.Node, nested bool) bool
	walk = func(n syntax.Node, nested bool) bool {
		syntax.Inspect(n, func(x syntax.Node) bool {
			if found {
				return false
			}
			switch x := x.(type) {
			case *syntax.FuncLit:
				return false
			case *syntax.BranchStmt:
				if x.Tok == syntax.BREAK && (x.Label == nil && !nested || x.Label != nil && x.Label.Name == label && label != "") {
					found = true
				}
			case *syntax.ForStmt, *syntax.RangeStmt, *syntax.SwitchStmt, *syntax.TypeSwitchStmt, *syntax.SelectStmt:
				if x != n {
					walk(x, true)
					return false
				}
			}
			return true
		})
		return found
	}
	_ = top
	return walk(s, false)
}

func hasBreakList(list []syntax.Stmt, label string, top bool) bool {
	for _, s := range list {
		if hasBreak(s, label, top) {
			return true
		}
	}
	return false
}
