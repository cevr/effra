package check

import (
	"effra.local/prototype/internal/sketch/syntax"
)

func unparen(x syntax.Expr) syntax.Expr {
	for {
		p, ok := x.(*syntax.ParenExpr)
		if !ok {
			return x
		}
		x = p.X
	}
}

// try checks `try e`: e's whole row escapes to the enclosing failure target
// (rows.md, Error rows 4); try on an expression that cannot fail is S108.
// `try e catch { … }` is one partial catch: the rest escapes through try.
func (c *checker) try(x *syntax.TryExpr, hint Type) operand {
	if ce, ok := unparen(x.X).(*syntax.CatchExpr); ok {
		op := c.catch(ce, hint, true, true)
		if op.invalid() {
			return op
		}
		c.escape(x, op.errs)
		op.errs = nil
		return op
	}
	op := c.expr(x.X, hint)
	if op.invalid() {
		return bad(x)
	}
	switch op.mode {
	case modeValue, modeConst, modeNoValue:
	default:
		c.needValue(&op)
		if op.invalid() {
			return bad(x)
		}
	}
	if !op.fallible() {
		c.errorf(x, CodeType, "try on an expression that cannot fail (rows.md, Error rows 6)")
		op.addressable = false
		return op
	}
	c.escape(x, op.errs)
	op.errs = nil
	op.addressable = false
	return op
}

// catch checks `e catch [b] { case T…: … }`. Each type must be in e's row
// and in one arm (S108); a catch without try handles every type (S103,
// rows.md Catch 4); a partial catch leaves the rest in its row. An arm is
// statements: when the catch's value is used, the arm's last statement
// yields it, and return in an arm leaves the function (rows.md, Catch 7).
func (c *checker) catch(x *syntax.CatchExpr, hint Type, wantValue, partial bool) operand {
	op := c.expr(x.X, hint)
	valid := !op.invalid()
	if valid {
		switch op.mode {
		case modeValue, modeConst, modeNoValue:
		default:
			c.needValue(&op)
			valid = !op.invalid()
		}
	}
	row := op.errs
	if valid && row.Empty() {
		c.errorf(x.X, CodeType, "catch on an expression that cannot fail (rows.md, Catch 1)")
		valid = false
	}
	hasValue := valid && op.mode != modeNoValue
	var valueType Type
	if hasValue {
		valueType = c.defaultOf(&op)
		if op.mode == modeConst {
			op.typ = valueType
		}
	}
	covered := newRow()
	var group *switchGroup
	if x.Bind != nil && x.Bind.Name != "_" {
		group = &switchGroup{}
	}
	for _, arm := range x.Arms {
		var types []Type
		for _, te := range arm.Types {
			t := c.catchType(te, row)
			if t == nil {
				continue
			}
			types = append(types, t)
			if !valid {
				continue
			}
			if !row.Has(t) {
				c.errorf(te, CodeType, "%s is not in the caught row %s (rows.md, Catch 2)", t, rowText(row))
				continue
			}
			if covered.Has(t) {
				c.errorf(te, CodeType, "%s is caught twice (rows.md, Catch 2)", t)
				continue
			}
			covered = covered.with(t)
		}
		saved := c.fn.scope
		c.fn.scope = newScope(saved)
		if group != nil {
			bt := Type(invalid)
			switch len(types) {
			case 0:
			case 1:
				bt = types[0]
			default:
				bt = &Union{Variants: types}
			}
			v := &Var{Name: x.Bind.Name, Type: bt, Pos: x.Bind.Pos(), group: group, used: true}
			c.fn.scope.insert(v.Name, v)
		}
		c.catchArm(arm, valueType, hasValue, wantValue && hasValue, valid)
		c.fn.scope = saved
	}
	if group != nil && !group.used {
		c.errorf(x.Bind, CodeUnused, "the catch binding %s is read in no arm", x.Bind.Name)
	}
	if !valid {
		if hasValue {
			return operand{mode: modeValue, typ: valueType}
		}
		return bad(x)
	}
	rest := row.minus(covered)
	if !partial && !rest.Empty() {
		c.errorAt(x.Catch, x.Catch+5, CodeNotTotal, "this catch does not handle %s; a catch without try handles every error of the row (rows.md, Catch 4)", rowText(rest))
	}
	res := operand{mode: modeValue, typ: valueType}
	if !hasValue {
		res = operand{mode: modeNoValue, typ: NoValue}
	}
	if partial {
		res.errs = rest
	}
	return res
}

// catchType resolves an arm's type. A generic error type written without
// type arguments takes the instance its row holds.
func (c *checker) catchType(te syntax.Expr, row *Row) Type {
	t := c.typExpr0(te, c.localTypeCtx())
	if isInvalid(t) {
		return nil
	}
	if n, ok := t.(*Named); ok && n.Orig == nil && len(n.TParams) > 0 {
		for _, e := range row.Elems() {
			if en, ok := e.(*Named); ok && en.origin() == n {
				return en
			}
		}
		c.errorf(te, CodeType, "generic type %s needs type arguments", n)
		return nil
	}
	return t
}

func (c *checker) catchArm(arm *syntax.CatchArm, valueType Type, hasValue, yield, valid bool) {
	list := arm.Body
	if len(list) == 0 {
		if yield {
			c.errorAt(arm.Colon, arm.Colon+1, CodeType, "this arm yields no value; %s expected (rows.md, Catch 3)", valueType)
		}
		return
	}
	last, isExpr := list[len(list)-1].(*syntax.ExprStmt)
	if !isExpr {
		c.stmtList(list, false)
		if yield && !c.terminatesList(list) {
			c.errorf(list[len(list)-1], CodeType, "this arm must end with a value of type %s or leave the function (rows.md, Catch 7)", valueType)
		}
		return
	}
	c.stmtList(list[:len(list)-1], true)
	var hint Type
	if hasValue {
		hint = valueType
	}
	op := c.expr(last.X, hint)
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
	switch {
	case yield:
		if op.mode == modeNoValue {
			c.errorf(last.X, CodeType, "this arm yields no value; %s expected (rows.md, Catch 3)", valueType)
			return
		}
		c.assign(&op, valueType, "the catch arm's value")
	case !hasValue && valid:
		if op.mode != modeNoValue {
			c.errorf(last.X, CodeType, "the caught expression has no value, so an arm has no value (rows.md, Catch 3)")
		}
	case !hasValue:
		c.needNoValueOrCall(&op)
	}
}

// goExpr checks `go f(x)` and `go { … }`: they start a task and yield a
// prelude Task[T, E], where T is the body's value and E its error row
// (runtime.md, Tasks 2). The body's requirements charge the enclosing
// function at the go site.
func (c *checker) goExpr(x *syntax.GoExpr) operand {
	var value Type = NoValue
	errs := newRow()
	if x.Call != nil {
		op := c.expr(x.Call, nil)
		if op.invalid() {
			return bad(x)
		}
		if op.mode == modeValue || op.mode == modeConst {
			value = c.defaultOf(&op)
		}
		errs = op.errs
	} else {
		sig := c.blockLit(x.Block, nil, nil)
		c.chargeRow(x, sig.Uses)
		if len(sig.Results) == 1 {
			value = sig.Results[0]
		}
		errs = sig.Errors
	}
	task := preludeType("Task")
	if task == nil {
		c.errorf(x, CodeType, "the prelude has no Task type")
		return bad(x)
	}
	return operand{mode: modeValue, typ: c.instantiate(task, []Type{value, &RowArg{Row: errs}})}
}

// preludeType returns a prelude declaration's type by name.
func preludeType(name string) *Named {
	p := prelude().pkg
	if p == nil {
		return nil
	}
	tn, ok := p.Scope.names[name].(*TypeName)
	if !ok {
		return nil
	}
	n, _ := tn.Type.(*Named)
	return n
}
