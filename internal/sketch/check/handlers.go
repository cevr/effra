package check

import (
	"ergo.local/prototype/internal/sketch/syntax"
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
	if x.Bind != nil {
		c.undecided(x.Bind, "a catch binding before the arms")
		return bad(x)
	}
	openMark := c.openRowSeq
	op := c.expr(x.X, hint)
	valid := !op.invalid()
	// open: the row may still grow in this component (rowWaits). operandOK:
	// the caught expression is valid, so an arm its row lacks is not a
	// cascade of its diagnostic.
	open, operandOK := c.openRowSeq != openMark, valid
	if valid {
		switch op.mode {
		case modeValue, modeConst, modeNoValue:
		default:
			c.needValue(&op)
			valid = !op.invalid()
		}
	}
	row := op.errs
	// The catch's value has the caught expression's type whatever its row:
	// a catch on an expression that cannot fail is a mistake in the row,
	// not in the type, so the value keeps its type and nothing cascades
	// from it. No type depends on an inferred row this way, so a call x.f(…)
	// on the value resolves before the rows settle (selfcalls.go; round 8
	// review M1).
	hasValue := valid && op.mode != modeNoValue
	if valid && row.Empty() {
		c.errorf(x.X, CodeType, "catch on an expression that cannot fail (rows.md, Catch 1)")
		valid = false
	}
	var valueType Type
	if hasValue {
		valueType = c.defaultOf(&op)
		if op.mode == modeConst {
			op.typ = valueType
		}
	}
	// covered: the arm types in the row; seen: every arm type. Both are hash
	// sets of interned ids, so an arm costs O(1) whatever the arm count (a
	// Row copied per arm made a catch quadratic in its arms; round 14
	// review M1). The table's generation moves only between body checks
	// (newSolutions, rows.go), so the ids hold for the whole catch.
	covered, seen := map[int32]bool{}, map[int32]bool{}
	for _, arm := range x.Arms {
		var types []Type
		var asName *syntax.Ident
		for _, te := range arm.Types {
			core, name := asParts(te)
			if name != nil {
				if len(arm.Types) > 1 {
					// A binding in an arm with several types is
					// check-v1's (196p).
					c.notYet(te)
					valid = false // no verdict over an unchecked arm
					continue
				}
				asName = name
			}
			if _, ok := core.(*syntax.CompositeLit); ok {
				c.notYet(core) // a record pattern, 196p
				valid = false
				continue
			}
			if id, ok := core.(*syntax.Ident); ok && c.catchAll(id) {
				c.notYet(id) // `case err:`, 196p
				valid = false
				continue
			}
			te = core
			t := c.catchType(te, row, open, operandOK)
			if t == nil {
				continue
			}
			types = append(types, t)
			// A duplicate arm is a fact of the arms alone: it holds
			// whatever the row, so a row check on this catch (Catch 1 or
			// the row half of Catch 2) never hides it (round 13 review M1).
			id := c.types.id(t)
			c.types.charge(1)
			if seen[id] {
				c.errorf(te, CodeType, "%s is caught twice (rows.md, Catch 2)", t)
				continue
			}
			seen[id] = true
			if !valid {
				continue
			}
			if !row.has(c.types, t) {
				c.errorf(te, CodeType, "%s is not in the caught row %s (rows.md, Catch 2)", t, rowText(row))
				continue
			}
			covered[id] = true
		}
		saved := c.fn.scope
		c.fn.scope = newScope(saved)
		var asBinds []asBinding
		if asName != nil {
			bt := Type(invalid)
			if len(types) == 1 {
				bt = types[0]
			}
			if b, ok := c.declareAs(asName, bt); ok {
				asBinds = append(asBinds, b)
			}
		}
		c.catchArm(arm, valueType, hasValue, wantValue && hasValue, valid)
		c.unusedAs(asBinds)
		c.fn.scope = saved
	}
	if !valid {
		if hasValue {
			return operand{mode: modeValue, typ: valueType}
		}
		return bad(x)
	}
	rest := row.without(c.types, covered)
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
// type arguments takes the instance its row holds. A row that holds no
// instance gives nil with no diagnostic when it may still grow in this
// component (open: the type is provisional, rowWaits) or is no evidence
// (evidence false: the caught expression is invalid, so its own
// diagnostic explains the empty row); otherwise the arm needs its type
// arguments (round 11 review M1 and O1). This error makes the binding
// invalid, so it is the diagnostic an invalid-diagnosed frame, and a
// dropped S108, rest on.
func (c *checker) catchType(te syntax.Expr, row *Row, open, evidence bool) Type {
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
		if c.rowWaits(open) || !evidence {
			return nil
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
