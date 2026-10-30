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
		op.expr = ce
		c.record(ce, op)
		if op.invalid() {
			return op
		}
		c.escape(x, op.errs)
		op.errs = nil
		return op
	}
	// try covers the whole expression to its right (196r, sixth round):
	// every fallible value inside it propagates (handled).
	var t *target
	if c.fn != nil {
		t = c.fn.t
	}
	inner := false
	if t != nil {
		saved := t.tried
		t.tried = false
		t.tries++
		defer func() {
			t.tries--
			t.tried = saved || t.tried
		}()
	}
	op := c.expr(x.X, hint)
	if t != nil {
		inner = t.tried
	}
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
	if !op.fallible() && inner {
		op.addressable = false
		return op
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
		valueType = c.typeOf(&op)
		if op.mode == modeConst {
			op.typ = valueType
		}
	}
	// covered: the arm types in the row; seen: every arm type. Both are hash
	// sets of interned ids, so an arm costs O(1) whatever the arm count (a
	// Row copied per arm made a catch quadratic in its arms; round 14
	// review M1). The table's generation moves only between body checks
	// (newSolutions, rows.go), so the ids hold for the whole catch.
	// covered: the arm types in the row that an arm handles whole; seen: every
	// arm type handled whole. Both are hash sets of interned ids, so an arm
	// costs O(1) whatever the arm count (a Row copied per arm made a catch
	// quadratic in its arms; round 14 review M1). The table's generation
	// moves only between body checks (newSolutions, rows.go), so the ids hold
	// for the whole catch. A record pattern that does not match every value of
	// its type handles part of it, so the type stays in the rest (D-C3).
	covered, seen := map[int32]bool{}, map[int32]bool{}
	// rested: a `case err:` or `case _:` arm handled the rest; every arm
	// after it is unreachable (D-C3).
	rested := false
	for _, arm := range x.Arms {
		// The `as` name of each pattern of the arm, with its type: with
		// several patterns, each binds the same name with one type (rule 6
		// of the pattern language, D-B3), else S108.
		var asNames []*syntax.Ident
		var asTypes []Type
		// The bindings of the arm's record patterns, one list per pattern.
		var recordBinds [][]patBinding
		// The `case err:` binding of the rest, with its row value type.
		var restName *syntax.Ident
		var restType Type
		for _, te := range arm.Types {
			core, name := asParts(te)
			asNames = append(asNames, name)
			asTypes = append(asTypes, nil)
			unreachable := rested && valid
			if unreachable {
				c.errorf(te, CodeType, "this arm is unreachable: the case err: or case _ before it handles every failure (rows.md, Catch 2)")
			}
			if id, ok := core.(*syntax.Ident); ok && (id.Name == "_" || c.catchAll(id)) {
				if len(arm.Types) > 1 || name != nil {
					c.errorf(te, CodeType, "case %s: handles the rest of the row and stands alone in its arm (rows.md, Catch 2)", id.Name)
					valid = false
					continue
				}
				rest := row.without(c.types, covered)
				if valid && !unreachable && rest.Empty() {
					c.errorf(te, CodeType, "this arm is unreachable: the arms before it handle every failure of the row %s (rows.md, Catch 2)", rowText(row))
				}
				rested = true
				for _, e := range rest.Elems() {
					covered[c.types.id(e)] = true
				}
				if id.Name == "_" {
					c.pattern(core, &Pattern{Kind: PatWildcard})
					continue
				}
				restName, restType = id, c.rowValue(rest)
				if !valid || rest.Empty() {
					restType = invalid
				}
				c.pattern(core, &Pattern{Kind: PatBind})
				continue
			}
			orig := te
			te = core
			whole := true
			var t Type
			var p *Pattern
			if lit, ok := core.(*syntax.CompositeLit); ok {
				if lit.Type == nil {
					c.errorf(lit, CodeType, "a record pattern in a catch arm names its error type: T{F: p} (rows.md, Catch 2)")
					valid = false
					continue
				}
				t = c.catchType(lit.Type, row, open, operandOK)
				if t == nil {
					valid = false
					continue
				}
				var binds []patBinding
				var ok bool
				binds, p, whole, ok = c.catchRecord(lit, t)
				if !ok {
					valid = false
				}
				recordBinds = append(recordBinds, binds)
			} else {
				t = c.catchType(te, row, open, operandOK)
				if t == nil {
					continue
				}
				p = c.pattern(core, &Pattern{Kind: PatTypeTest, Type: t})
			}
			asTypes[len(asTypes)-1] = t
			if name != nil {
				c.asPattern(orig, p, name)
			}
			if unreachable {
				continue
			}
			// A duplicate arm is a fact of the arms alone: it holds
			// whatever the row, so a row check on this catch (Catch 1 or
			// the row half of Catch 2) never hides it (round 13 review M1).
			id := c.types.id(t)
			c.types.charge(1)
			if seen[id] {
				c.errorf(te, CodeType, "%s is caught twice (rows.md, Catch 2)", t)
				continue
			}
			if whole {
				seen[id] = true
			}
			if !valid {
				continue
			}
			if !row.has(c.types, t) {
				c.errorf(te, CodeType, "%s is not in the caught row %s (rows.md, Catch 2)", t, rowText(row))
				continue
			}
			if whole {
				covered[id] = true
			}
		}
		saved := c.fn.scope
		c.fn.scope = newScope(saved)
		var asBinds []asBinding
		if name, bt := c.armBinding(asNames, asTypes); name != nil {
			if b, ok := c.declareAs(name, bt); ok {
				asBinds = append(asBinds, b)
				v, _ := c.fn.scope.names[name.Name].(*Var)
				for _, other := range asNames[1:] {
					if other != nil && other.Name == name.Name && v != nil {
						c.def(other, v)
					}
				}
			}
		}
		var restBind []asBinding
		if restName != nil {
			if b, ok := c.declareAs(restName, restType); ok {
				restBind = append(restBind, b)
				if v, isVar := c.fn.scope.names[restName.Name].(*Var); isVar && c.silent == 0 {
					if p := c.pkg.Info.Patterns[restName]; p != nil {
						p.Var = v
					}
				}
			}
		}
		recordVars := c.declareArm(recordBinds)
		c.catchArm(arm, x.Rbrace, valueType, hasValue, wantValue && hasValue, valid)
		c.unusedAs(asBinds)
		c.unusedBindings(recordVars)
		for _, b := range restBind {
			if !b.group.used {
				c.errorf(b.name, CodeUnused, "the binding %s is read nowhere in its arm; write case _ (rows.md, Totality 1)", b.name.Name)
			}
		}
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
	m := &Match{Subjects: row.Elems(), Arms: len(x.Arms), Partial: partial}
	if hasValue && wantValue {
		m.Result = valueType
	}
	c.match(x, m)
	res := operand{mode: modeValue, typ: valueType}
	if !hasValue {
		res = operand{mode: modeNoValue, typ: NoValue}
	}
	if partial {
		res.errs = rest
	}
	return res
}

// armBinding returns the name a catch arm binds and its type, from the
// `as` name and the type of each of its patterns (nil where a pattern
// names none or did not resolve). With several patterns, each binds the
// same name with one type (rule 6 of the pattern language, D-B3): a
// pattern that binds no name or another type is S108, and the name then
// binds with the invalid type, so its reads add nothing (D3).
func (c *checker) armBinding(names []*syntax.Ident, types []Type) (*syntax.Ident, Type) {
	var first *syntax.Ident
	for _, n := range names {
		if n != nil && n.Name != "_" {
			first = n
			break
		}
	}
	if first == nil {
		return nil, nil
	}
	if len(names) == 1 {
		if types[0] == nil {
			return first, invalid
		}
		return first, types[0]
	}
	t := types[0]
	ok := true
	for i, n := range names {
		switch {
		case n == nil || n.Name != first.Name:
			at := syntax.Node(first)
			if n != nil {
				at = n
			}
			c.errorf(at, CodeType, "%s is bound by one pattern of this arm and not by another; every pattern of an arm binds the same names (rows.md, Totality 1)", first.Name)
			return first, invalid
		case types[i] == nil || t == nil:
			ok = false
		case !c.identical(types[i], t):
			c.errorf(n, CodeType, "%s is bound as %s by the first pattern of its arm and as %s here; every pattern of an arm binds each name with one type (rows.md, Totality 1)", n.Name, t, types[i])
			return first, invalid
		}
	}
	if !ok {
		return first, invalid
	}
	return first, t
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

func (c *checker) catchArm(arm *syntax.CatchArm, end syntax.Pos, valueType Type, hasValue, yield, valid bool) {
	list := arm.Body
	if len(list) == 0 {
		if yield {
			c.errorAt(arm.Colon, arm.Colon+1, CodeType, "this arm yields no value; %s expected (rows.md, Catch 3)", valueType)
		}
		return
	}
	if yield && isBranching(list[len(list)-1]) {
		// A final if, match or block yields the arm's value (D-C2).
		k := &sink{want: valueType, what: "the catch arm's value"}
		k.missing = func(at syntax.Pos) {
			c.errorAt(at, at+1, CodeType, "this branch must end with a value of type %s or leave the function (rows.md, Catch 7)", valueType)
		}
		c.yieldList(list, k, end)
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
		c.tail(last, valueType)
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

// catchRecord resolves a record pattern of a catch arm, T{F: p, …}, against
// its error type t (D-C3): its bindings, its Patterns entry, and whether it
// matches every value of t (whole), which only then leaves the rest of the
// row. The usefulness solver decides that under the totality budget
// (rows.md, Totality 5); ok is false after a reported pattern.
func (c *checker) catchRecord(lit *syntax.CompositeLit, t Type) (binds []patBinding, info *Pattern, whole, ok bool) {
	r := c.newPatResolver()
	r.sp.written = 1 + patternSize(lit)
	r.bindings = true
	r.begin()
	col := r.subject(t)
	res := r.pattern(lit, t, col)
	if r.bad || r.resolve.over {
		if r.resolve.over {
			c.errorf(lit, CodeNotTotal, "too many cases to prove what this catch pattern matches; split it (rows.md, Totality 5)")
		}
		for k := range r.binds {
			r.binds[k].t = invalid
		}
		return r.binds, res.info, false, false
	}
	row := solverRow{}
	if res.has {
		row = r.sp.flat(solverRow{{col, res.pat}})
	}
	s := r.sp.solverFor()
	_, missing, proved := s.total([]solverRow{row})
	if c.silent == 0 {
		c.totalitySteps = append(c.totalitySteps, s.steps)
		c.totalityBudgets = append(c.totalityBudgets, s.budget)
		c.usefulSteps = append(c.usefulSteps, 0)
		c.patternCosts = append(c.patternCosts, patternCost{r.sp.written, s.budget, s.meter.cellBudget, s.steps, 0, s.cells, 0})
	}
	if !proved {
		c.errorf(lit, CodeNotTotal, "too many cases to prove what this catch pattern matches; split it (rows.md, Totality 5)")
		return r.binds, res.info, false, false
	}
	return r.binds, res.info, !missing, true
}

// rowValue is the type of a `case err:` binding: the row value of the
// failures rest holds, the anonymous union of their error types (D-C3). It
// can be returned (a failure of those types), matched, caught or assigned
// to a declared union that lists each of them (ConvRow); nothing else.
func (c *checker) rowValue(rest *Row) Type {
	return &Union{Variants: rest.Elems()}
}
