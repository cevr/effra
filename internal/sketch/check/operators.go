package check

import (
	"go/constant"
	"go/token"
	"math"

	"ergo.local/prototype/internal/sketch/syntax"
)

// representable checks that a constant fits the basic type t (constant
// overflow is S108; types.md, Basic types 2) and rounds it to t's kind.
func (c *checker) representable(op *operand, t *Basic) bool {
	if op.val == nil {
		return true
	}
	v := op.val
	ok := true
	switch {
	case t.Kind == Bool || t.Kind == UntypedBool:
		ok = v.Kind() == constant.Bool
	case t.Kind == String || t.Kind == UntypedString:
		ok = v.Kind() == constant.String
	case t.Kind >= Int && t.Kind <= Uintptr || t.Kind == Duration || t.Kind == UntypedInt || t.Kind == UntypedRune:
		iv := constant.ToInt(v)
		if iv.Kind() != constant.Int {
			c.errorf(op.expr, CodeType, "%s is not an integer, so it is not representable as %s", v.ExactString(), t)
			op.mode = modeInvalid
			return false
		}
		if t.Kind == UntypedInt || t.Kind == UntypedRune {
			return true
		}
		lo, hi := intBounds(t.Kind)
		if constant.Compare(iv, token.LSS, lo) || constant.Compare(iv, token.GTR, hi) {
			c.errorf(op.expr, CodeType, "the constant %s overflows %s (types.md, Basic types 2)", iv.ExactString(), t)
			op.mode = modeInvalid
			return false
		}
		op.val = iv
	case t.Kind == Float32 || t.Kind == Float64 || t.Kind == UntypedFloat:
		fv := constant.ToFloat(v)
		if fv.Kind() != constant.Float && fv.Kind() != constant.Int {
			ok = false
			break
		}
		f, _ := constant.Float64Val(fv)
		if math.IsInf(f, 0) {
			c.errorf(op.expr, CodeType, "the constant %s overflows %s", v.ExactString(), t)
			op.mode = modeInvalid
			return false
		}
		switch t.Kind {
		case Float32:
			f32, _ := constant.Float32Val(fv)
			if math.IsInf(float64(f32), 0) {
				c.errorf(op.expr, CodeType, "the constant %s overflows float32", v.ExactString())
				op.mode = modeInvalid
				return false
			}
			// A typed constant holds its type's correctly rounded value, so
			// later constant arithmetic sees what Go sees (types2
			// roundFloat32; M12).
			fv = constant.MakeFloat64(float64(f32))
		case Float64:
			fv = constant.MakeFloat64(f)
		}
		op.val = fv
	case t.Kind == Complex64 || t.Kind == Complex128:
		cv := constant.ToComplex(v)
		if cv.Kind() != constant.Complex {
			ok = false
			break
		}
		re, _ := constant.Float64Val(constant.Real(cv))
		im, _ := constant.Float64Val(constant.Imag(cv))
		if t.Kind == Complex64 {
			re32, _ := constant.Float32Val(constant.Real(cv))
			im32, _ := constant.Float32Val(constant.Imag(cv))
			re, im = float64(re32), float64(im32)
		}
		if math.IsInf(re, 0) || math.IsInf(im, 0) {
			c.errorf(op.expr, CodeType, "the constant %s overflows %s", v.ExactString(), t)
			op.mode = modeInvalid
			return false
		}
		// Rounded per part to the type's float size, as for floats (M12).
		op.val = constant.BinaryOp(constant.MakeFloat64(re), token.ADD, constant.MakeImag(constant.MakeFloat64(im)))
	}
	if !ok {
		c.errorf(op.expr, CodeType, "the constant %s is not representable as %s", v.ExactString(), t)
		op.mode = modeInvalid
		return false
	}
	return true
}

func intBounds(k BasicKind) (constant.Value, constant.Value) {
	switch k {
	case Int8:
		return constant.MakeInt64(math.MinInt8), constant.MakeInt64(math.MaxInt8)
	case Int16:
		return constant.MakeInt64(math.MinInt16), constant.MakeInt64(math.MaxInt16)
	case Int32:
		return constant.MakeInt64(math.MinInt32), constant.MakeInt64(math.MaxInt32)
	case Uint8:
		return constant.MakeInt64(0), constant.MakeInt64(math.MaxUint8)
	case Uint16:
		return constant.MakeInt64(0), constant.MakeInt64(math.MaxUint16)
	case Uint32:
		return constant.MakeInt64(0), constant.MakeInt64(math.MaxUint32)
	case Uint, Uint64, Uintptr:
		return constant.MakeInt64(0), constant.MakeUint64(math.MaxUint64)
	}
	return constant.MakeInt64(math.MinInt64), constant.MakeInt64(math.MaxInt64)
}

// ---------------------------------------------------------------- unary

func (c *checker) unary(x *syntax.UnaryExpr, hint Type) operand {
	switch x.Op {
	case syntax.AND:
		op := c.valueExpr(x.X, nil)
		if op.invalid() {
			return op
		}
		if !op.addressable {
			c.errorf(x, CodeType, "cannot take the address of this expression; a borrow &x names a variable, field or element")
			return bad(x)
		}
		return operand{mode: modeValue, typ: &Pointer{Elem: op.typ}}
	case syntax.ARROW:
		op := c.valueExpr(x.X, nil)
		if op.invalid() {
			return op
		}
		ch, ok := under(op.typ).(*Chan)
		if !ok {
			c.errorf(x, CodeType, "cannot receive from %s", op.typ)
			return bad(x)
		}
		if ch.Dir == syntax.ChanSend {
			c.errorf(x, CodeType, "cannot receive from a send-only channel")
			return bad(x)
		}
		return operand{mode: modeValue, typ: ch.Elem}
	}
	op := c.valueExpr(x.X, hint)
	if op.invalid() {
		return op
	}
	t := op.typ
	switch x.Op {
	case syntax.NOT:
		if !isBoolean(t) {
			c.errorf(x, CodeType, "operator ! is not defined on %s", t)
			return bad(x)
		}
	case syntax.SUB, syntax.ADD:
		if !isNumeric(t) && !isDuration(t) {
			c.errorf(x, CodeType, "operator %s is not defined on %s", x.Op, t)
			return bad(x)
		}
	case syntax.XOR:
		if !isInteger(t) {
			c.errorf(x, CodeType, "operator ^ is not defined on %s", t)
			return bad(x)
		}
	default:
		c.errorf(x, CodeType, "unexpected unary operator %s", x.Op)
		return bad(x)
	}
	if op.mode == modeConst {
		tok := map[syntax.Token]token.Token{syntax.NOT: token.NOT, syntax.SUB: token.SUB, syntax.ADD: token.ADD, syntax.XOR: token.XOR}[x.Op]
		prec := uint(0)
		if isUnsigned(t) {
			prec = uint(basicSize(under(t).(*Basic).Kind) * 8)
		}
		op.val = constant.UnaryOp(tok, op.val, prec)
		if b, ok := t.(*Basic); ok && !isUntyped(t) {
			op.expr = x
			if !c.representable(&op, b) {
				return bad(x)
			}
		}
		return operand{mode: modeConst, typ: t, val: op.val}
	}
	return operand{mode: modeValue, typ: t}
}

func basicSize(k BasicKind) int {
	switch k {
	case Int8, Uint8:
		return 1
	case Int16, Uint16:
		return 2
	case Int32, Uint32, Float32:
		return 4
	}
	return 8
}

// ---------------------------------------------------------------- binary

var tokenOf = map[syntax.Token]token.Token{
	syntax.ADD: token.ADD, syntax.SUB: token.SUB, syntax.MUL: token.MUL, syntax.QUO: token.QUO, syntax.REM: token.REM,
	syntax.AND: token.AND, syntax.OR: token.OR, syntax.XOR: token.XOR, syntax.SHL: token.SHL, syntax.SHR: token.SHR, syntax.AND_NOT: token.AND_NOT,
	syntax.LAND: token.LAND, syntax.LOR: token.LOR,
	syntax.EQL: token.EQL, syntax.NEQ: token.NEQ, syntax.LSS: token.LSS, syntax.LEQ: token.LEQ, syntax.GTR: token.GTR, syntax.GEQ: token.GEQ,
}

func isComparison(op syntax.Token) bool {
	switch op {
	case syntax.EQL, syntax.NEQ, syntax.LSS, syntax.LEQ, syntax.GTR, syntax.GEQ:
		return true
	}
	return false
}

// binary checks Go's binary operators on Go's operand types (README Gap 10,
// types.md Operators), with exact constant arithmetic. duration takes +, -,
// comparisons and equality with another duration only (D2; #175).
func (c *checker) binary(x *syntax.BinaryExpr, hint Type) operand {
	if x.Op == syntax.SHL || x.Op == syntax.SHR {
		return c.shift(x, hint)
	}
	lhint, rhint := hint, hint
	if isComparison(x.Op) || x.Op == syntax.LAND || x.Op == syntax.LOR {
		lhint, rhint = nil, nil
	}
	l := c.valueExpr(x.X, lhint)
	r := c.valueExpr(x.Y, rhint)
	if l.invalid() || r.invalid() {
		return bad(x)
	}
	// Match the operands' types: an untyped constant takes the other side's.
	if !c.matchTypes(x, &l, &r) {
		return bad(x)
	}
	t := l.typ
	if isComparison(x.Op) {
		switch x.Op {
		case syntax.EQL, syntax.NEQ:
			if !comparable(t) {
				c.errorf(x, CodeType, "%s values cannot be compared with %s", t, x.Op)
				return bad(x)
			}
		default:
			if !ordered(t) {
				c.errorf(x, CodeType, "operator %s is not defined on %s", x.Op, t)
				return bad(x)
			}
		}
		if l.mode == modeConst && r.mode == modeConst {
			return operand{mode: modeConst, typ: Typ[UntypedBool], val: constant.MakeBool(constant.Compare(l.val, tokenOf[x.Op], r.val))}
		}
		return operand{mode: modeValue, typ: Typ[UntypedBool]}
	}
	if !c.opDefined(x, x.Op, t) {
		return bad(x)
	}
	if l.mode == modeConst && r.mode == modeConst {
		if (x.Op == syntax.QUO || x.Op == syntax.REM) && constant.Sign(r.val) == 0 {
			c.errorf(x, CodeType, "division by zero")
			return bad(x)
		}
		tok := tokenOf[x.Op]
		if x.Op == syntax.QUO && isInteger(t) {
			tok = token.QUO_ASSIGN // integer division
		}
		var v constant.Value
		if tok == token.QUO_ASSIGN {
			v = constant.BinaryOp(constant.ToInt(l.val), token.QUO_ASSIGN, constant.ToInt(r.val))
		} else {
			v = constant.BinaryOp(l.val, tok, r.val)
		}
		res := operand{mode: modeConst, typ: t, val: v, expr: x}
		if b, ok := under(t).(*Basic); ok && !isUntyped(t) {
			if !c.representable(&res, b) {
				return bad(x)
			}
		}
		if b, ok := t.(*Basic); ok && isUntyped(t) && (b.Kind == UntypedInt || b.Kind == UntypedRune) {
			// Untyped integer constants stay exact; an overflow shows where
			// the constant takes a type.
			_ = b
		}
		return res
	}
	if (x.Op == syntax.QUO || x.Op == syntax.REM) && r.mode == modeConst && constant.Sign(r.val) == 0 && isInteger(t) {
		c.errorf(x, CodeType, "division by zero")
		return bad(x)
	}
	return operand{mode: modeValue, typ: t}
}

// matchTypes converts an untyped operand to the other operand's type, and
// reports mismatched types (S108).
func (c *checker) matchTypes(x *syntax.BinaryExpr, l, r *operand) bool {
	lu := l.mode == modeConst && isUntyped(l.typ)
	ru := r.mode == modeConst && isUntyped(r.typ)
	switch {
	case lu && ru:
		lk, rk := l.typ.(*Basic).Kind, r.typ.(*Basic).Kind
		if lk == rk {
			return true
		}
		numeric := func(k BasicKind) bool { return k == UntypedInt || k == UntypedRune || k == UntypedFloat }
		if numeric(lk) && numeric(rk) {
			k := max(lk, rk)
			l.typ, r.typ = Typ[k], Typ[k]
			// Both operands now carry the larger kind, in Info too (go/types
			// matchTypes; round-7 M1).
			c.updateExprType(x.X, Typ[k])
			c.updateExprType(x.Y, Typ[k])
			return true
		}
		c.errorf(x, CodeType, "mismatched types %s and %s", l.typ, r.typ)
		return false
	case lu:
		if !c.untypedTo(l, r.typ, x) {
			return false
		}
	case ru:
		if !c.untypedTo(r, l.typ, x) {
			return false
		}
	}
	if isUntyped(l.typ) && isBoolean(l.typ) && isBoolean(r.typ) {
		l.typ = r.typ
		c.updateExprType(x.X, r.typ)
	}
	if isUntyped(r.typ) && isBoolean(r.typ) && isBoolean(l.typ) {
		r.typ = l.typ
		c.updateExprType(x.Y, l.typ)
	}
	if !identical(l.typ, r.typ) {
		if isComparison(x.Op) && (x.Op == syntax.EQL || x.Op == syntax.NEQ) {
			if ok, _ := c.assignable(l.typ, r.typ); ok {
				l.typ = r.typ
				return true
			}
			if ok, _ := c.assignable(r.typ, l.typ); ok {
				r.typ = l.typ
				return true
			}
		}
		c.errorf(x, CodeType, "mismatched types %s and %s", l.typ, r.typ)
		return false
	}
	return true
}

func (c *checker) untypedTo(op *operand, t Type, x *syntax.BinaryExpr) bool {
	if _, ok := under(t).(*Basic); !ok {
		c.errorf(x, CodeType, "mismatched types %s and %s", op.typ, t)
		return false
	}
	saved := op.expr
	op.expr = x
	ok := c.convertUntyped(op, t, "this operation")
	op.expr = saved
	if ok {
		// The operand, not the operation, takes the type in Info
		// (round-7 M1).
		c.updateExprType(saved, op.typ)
	}
	return ok
}

func ordered(t Type) bool {
	b, ok := under(t).(*Basic)
	return ok && b.Kind != Bool && b.Kind != UntypedBool && b.Kind != Invalid && !isComplex(b)
}

func (c *checker) opDefined(x syntax.Node, op syntax.Token, t Type) bool {
	ok := false
	switch op {
	case syntax.ADD:
		ok = isNumeric(t) || isString(t) || isDuration(t)
	case syntax.SUB:
		ok = isNumeric(t) || isDuration(t)
	case syntax.MUL, syntax.QUO:
		ok = isNumeric(t)
	case syntax.REM, syntax.AND, syntax.OR, syntax.XOR, syntax.AND_NOT:
		ok = isInteger(t)
	case syntax.LAND, syntax.LOR:
		ok = isBoolean(t)
	}
	if !ok {
		c.errorf(x, CodeType, "operator %s is not defined on %s", op, t)
	}
	return ok
}

func (c *checker) shift(x *syntax.BinaryExpr, hint Type) operand {
	l := c.valueExpr(x.X, hint)
	r := c.valueExpr(x.Y, nil)
	if l.invalid() || r.invalid() {
		return bad(x)
	}
	if !isInteger(r.typ) {
		c.errorf(x.Y, CodeType, "a shift count must be an integer, not %s", r.typ)
		return bad(x)
	}
	if r.mode == modeConst && constant.Sign(r.val) < 0 {
		c.errorf(x.Y, CodeType, "negative shift count")
		return bad(x)
	}
	lt := l.typ
	if l.mode == modeConst && isUntyped(lt) {
		if r.mode != modeConst {
			lt = defaultType(lt)
			if hint != nil && isInteger(hint) {
				lt = hint
			}
			// The left operand of a non-constant shift takes the
			// shift's type (go/types updateExprType; round-7 M1).
			c.updateExprType(x.X, lt)
		} else if constant.ToInt(l.val).Kind() != constant.Int {
			c.errorf(x.X, CodeType, "cannot shift %s", l.val.ExactString())
			return bad(x)
		}
	}
	if !isInteger(lt) {
		c.errorf(x, CodeType, "operator %s is not defined on %s", x.Op, l.typ)
		return bad(x)
	}
	if l.mode == modeConst && r.mode == modeConst {
		s, _ := constant.Uint64Val(r.val)
		if s > 10000 {
			c.errorf(x.Y, CodeType, "shift count too large")
			return bad(x)
		}
		v := constant.Shift(constant.ToInt(l.val), tokenOf[x.Op], uint(s))
		res := operand{mode: modeConst, typ: lt, val: v, expr: x}
		if b, ok := under(lt).(*Basic); ok && !isUntyped(lt) {
			if !c.representable(&res, b) {
				return bad(x)
			}
		}
		return res
	}
	return operand{mode: modeValue, typ: lt}
}

// conversion checks T(x) (Go's conversion rules over the language's types).
func (c *checker) conversion(x *syntax.CallExpr, t Type) operand {
	if len(x.Args) != 1 || x.Trailing != nil {
		c.errorf(x, CodeType, "a conversion to %s takes one value", t)
		return bad(x)
	}
	op := c.valueExpr(x.Args[0], t)
	if op.invalid() || isInvalid(t) {
		return bad(x)
	}
	if op.mode == modeConst {
		if tb, ok := under(t).(*Basic); ok {
			ob := under(op.typ).(*Basic)
			okConv := false
			switch {
			case isNumeric(tb) && (isNumeric(ob) || ob.Kind == Duration):
				okConv = true
			case tb.Kind == Duration && isInteger(ob) && !isUntyped(ob):
				okConv = true
			case tb.Kind == Duration && isUntyped(ob):
				okConv = isInteger(ob)
			case isString(tb) && isString(ob):
				okConv = true
			case isString(tb) && isInteger(ob):
				return operand{mode: modeValue, typ: t}
			case isBoolean(tb) && isBoolean(ob):
				okConv = true
			}
			if !okConv {
				c.errorf(x, CodeType, "cannot convert %s to %s", op.typ, t)
				return bad(x)
			}
			res := operand{mode: modeConst, typ: t, val: op.val, expr: x}
			if isInteger(tb) && isFloat(ob) {
				if constant.ToInt(op.val).Kind() != constant.Int {
					c.errorf(x, CodeType, "the constant %s truncates to an integer", op.val.ExactString())
					return bad(x)
				}
			}
			if !c.representable(&res, tb) {
				return bad(x)
			}
			return res
		}
	}
	if c.convertible(op.typ, t) {
		return operand{mode: modeValue, typ: t}
	}
	c.errorf(x, CodeType, "cannot convert %s to %s", op.typ, t)
	return bad(x)
}

func (c *checker) convertible(v, t Type) bool {
	if ok, _ := c.assignable(v, t); ok {
		return true
	}
	vu, tu := under(v), under(t)
	if identical(vu, tu) {
		return true
	}
	vb, vok := vu.(*Basic)
	tb, tok := tu.(*Basic)
	if vok && tok {
		switch {
		case isNumeric(vb) && isNumeric(tb) && isComplex(vb) == isComplex(tb):
			return true
		case vb.Kind == Duration && isInteger(tb), tb.Kind == Duration && isInteger(vb):
			return true
		case isString(tb) && (isInteger(vb) || isString(vb)):
			return true
		}
	}
	if tok && isString(tb) {
		if s, ok := vu.(*Slice); ok && (isBasic(s.Elem, Uint8) || isBasic(s.Elem, Int32)) {
			return true
		}
	}
	if vok && isString(vb) {
		if s, ok := tu.(*Slice); ok && (isBasic(s.Elem, Uint8) || isBasic(s.Elem, Int32)) {
			return true
		}
	}
	return false
}
