package check

import "ergo.local/prototype/internal/sketch/syntax"

// Decided forms this checker does not check yet (check-v1) are S109, each
// naming the stage that owns it, and nothing cascades from them (D3; round
// 6 review B1 and M2): a pattern after let or for binds its names as
// invalid locals or package vars, so a later use of them is silent, and an
// expression that holds the form is invalid.

// outside reports n as a decided form this checker does not check yet: S109
// with the noun that names its stage. It never panics.
func (c *checker) outside(n syntax.Node, noun string) operand {
	c.errorf(n, CodeOutside, "%s is not checked yet (check-v1)", noun)
	if x, ok := n.(syntax.Expr); ok {
		return bad(x)
	}
	return operand{mode: modeInvalid, typ: invalid}
}

// patternNoun names a let or for pattern by its stage: a tuple is 196t's,
// any other pattern (a record pattern, an as pattern, a literal) 196p's.
func patternNoun(where string, p syntax.Expr) string {
	if _, ok := unparen(p).(*syntax.TupleExpr); ok {
		return "a tuple pattern after " + where + " (196t, tuples)"
	}
	return "a pattern after " + where + " (196p, patterns)"
}

// isPattern reports whether a let or for binding is a pattern rather than
// one name.
func isPattern(p syntax.Expr) bool {
	if p == nil {
		return false
	}
	_, name := p.(*syntax.Ident)
	return !name
}

// patternNames returns the names a pattern binds, in source order: a name
// that is not a type (a type name tests the variant), the names inside a
// record pattern's values, a tuple's elements and an as pattern's name.
// isType reports whether a name is a type in the pattern's scope.
func patternNames(p syntax.Expr, isType func(string) bool) []*syntax.Ident {
	var out []*syntax.Ident
	var walk func(syntax.Expr)
	walk = func(e syntax.Expr) {
		switch e := e.(type) {
		case *syntax.Ident:
			if e.Name != "_" && !isType(e.Name) {
				out = append(out, e)
			}
		case *syntax.ParenExpr:
			walk(e.X)
		case *syntax.TupleExpr:
			for _, x := range e.Elts {
				walk(x)
			}
		case *syntax.CompositeLit:
			for _, x := range e.Elts {
				if kv, ok := x.(*syntax.KeyValueExpr); ok {
					walk(kv.Value)
					continue
				}
				walk(x)
			}
		case *syntax.AsPattern:
			walk(e.X)
			if e.Name.Name != "_" {
				out = append(out, e.Name)
			}
		}
	}
	walk(p)
	return out
}

// isTypeName reports whether name is a type where c checks.
func (c *checker) isTypeName(name string) bool {
	_, isType := c.lookup(name).(*TypeName)
	return isType
}

// patternLet reports a local let with a pattern (S109), checks its value
// for its own mistakes (a tuple value's elements one by one: the tuple is
// the same unchecked form) and binds the pattern's names as invalid locals.
func (c *checker) patternLet(spec *syntax.VarSpec) {
	c.outside(spec.Pattern, patternNoun("let", spec.Pattern))
	for _, v := range spec.Values {
		c.uncheckedValue(v)
	}
	for _, id := range patternNames(spec.Pattern, c.isTypeName) {
		c.declareLocal(id, invalid)
	}
}

// rangePatterns reports a for binding that is a pattern (S109, once for the
// statement) and binds the pattern's names as invalid locals; a binding
// that is one name is the caller's.
func (c *checker) rangePatterns(s *syntax.RangeStmt) {
	reported := false
	for _, l := range []syntax.Expr{s.Key, s.Value} {
		if !isPattern(l) {
			continue
		}
		if !reported {
			c.outside(l, patternNoun("for", l))
			reported = true
		}
		for _, id := range patternNames(l, c.isTypeName) {
			c.declareLocal(id, invalid)
		}
	}
}

// definedNoun names a defined type, `type Celsius float64` (196p's one shape
// for every type declaration). The 196a checker read only records,
// interfaces and machines, so a defined type is S109 at its declaration.
const definedNoun = "a defined type over another type (196p, type declarations)"

// isDefinedSpec reports whether a type declaration's body is a type
// expression: a defined type, not a record, an operation type, a union or
// a machine.
func isDefinedSpec(spec syntax.Expr) bool {
	switch spec.(type) {
	case *syntax.StructType, *syntax.InterfaceType, *syntax.MachineType:
		return false
	}
	return spec != nil
}

// refusedType reports whether t is a defined type (S109 at its
// declaration): every use of it is invalid, so nothing cascades (D3).
func refusedType(t Type) bool {
	n, ok := t.(*Named)
	if !ok {
		return false
	}
	if n.Orig != nil {
		n = n.Orig
	}
	return n.Obj != nil && n.Obj.Decl != nil && isDefinedSpec(n.Obj.Decl.Spec)
}

// uncheckedConstraint reports the decided named constraint `ordered`
// (196g; the translator writes it for cmp.Ordered), which the 196a checker
// has no meaning for: S109 at the constraint, unless a type of that name is
// declared.
func (c *checker) uncheckedConstraint(x syntax.Expr, ctx *typeCtx) bool {
	id, ok := x.(*syntax.Ident)
	if !ok || id.Name != "ordered" || c.lookupType(id.Name, ctx) != nil {
		return false
	}
	c.outside(x, "the ordered constraint (196g, constraints)")
	return true
}

// uncheckedParams reports whether a signature declares a type parameter
// whose constraint is unchecked (uncheckedConstraint).
func uncheckedParams(tps []*TypeParam) bool {
	for _, tp := range tps {
		if tp.unchecked {
			return true
		}
	}
	return false
}

// uncheckedValue checks the value of an unchecked form for its own
// mistakes: a tuple's elements one by one, any other value whole.
func (c *checker) uncheckedValue(v syntax.Expr) {
	if t, ok := unparen(v).(*syntax.TupleExpr); ok {
		for _, x := range t.Elts {
			c.uncheckedValue(x)
		}
		return
	}
	c.valueExpr(v, nil)
}
