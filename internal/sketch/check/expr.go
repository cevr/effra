package check

import (
	"go/constant"
	"go/token"
	"strconv"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
)

type opMode int

const (
	modeInvalid opMode = iota // already reported: suppresses every diagnostic that follows (D3)
	modeNoValue
	modeValue
	modeConst
	modeType
	modeBuiltin
	modeCap     // a capability name: only the receiver of a method call
	modeCapFn   // a capability's method: only called
	modePkg     // a package name: only before a selector
	modeGeneric // a generic function not yet instantiated
)

// operand is the result of checking an expression. errs is its failure row
// while unhandled: a non-empty row makes the expression fallible (rows.md,
// Error rows 2).
type operand struct {
	mode opMode
	typ  Type
	val  constant.Value
	errs *Row
	obj  Object
	cap  Type // modeCap/modeCapFn: the capability
	expr syntax.Expr
	// addressable marks a variable, field or element: &x and assignment.
	addressable bool
	// mapElem marks a map index: assignable, as in Go, but not
	// addressable, so neither &m[k] nor m[k].f = v (M17).
	mapElem bool
	fn      *Func // the function an identifier names (main is S113 when called)
	recvVar *Var
}

func (o *operand) invalid() bool { return o.mode == modeInvalid }

func (o *operand) fallible() bool { return !o.errs.Empty() }

func bad(x syntax.Expr) operand { return operand{mode: modeInvalid, typ: invalid, expr: x} }

// record notes the type of an expression for later passes.
func (c *checker) record(x syntax.Expr, op operand) {
	if c.silent == 0 && op.typ != nil && op.mode != modeInvalid {
		c.pkg.Info.Types[x] = op.typ
	}
}

// expr checks x with an optional expected type (bidirectional: composite
// literals, function literals and untyped constants take it). The result
// may be fallible, a type or a package; positions decide what they accept.
func (c *checker) expr(x syntax.Expr, hint Type) operand {
	op := c.expr0(x, hint)
	op.expr = x
	c.record(x, op)
	return op
}

// valueExpr checks x where a value is needed: a fallible value is S105
// (rows.md, Error rows 3), a type, package or capability is an error.
func (c *checker) valueExpr(x syntax.Expr, hint Type) operand {
	op := c.expr(x, hint)
	c.needValue(&op)
	c.handled(&op)
	return op
}

// handled reports an unhandled failure in a value position (S105).
func (c *checker) handled(op *operand) {
	if op.invalid() || !op.fallible() {
		return
	}
	c.errorf(op.expr, CodeUnhandled, "the failure %s of this expression is not handled: use try, catch or return (rows.md, Error rows 3)", rowText(op.errs))
	op.errs = nil
}

// needValue requires a value: not a type, a package, a capability or no
// value.
func (c *checker) needValue(op *operand) {
	switch op.mode {
	case modeInvalid, modeValue, modeConst:
		return
	case modeNoValue:
		c.errorf(op.expr, CodeType, "this expression has no value")
	case modeType:
		c.errorf(op.expr, CodeType, "%s is a type, not a value", op.typ)
	case modePkg:
		c.errorf(op.expr, CodeType, "package %s is not a value", exprText(op.expr))
	case modeCap:
		c.errorf(op.expr, CodeUnknown, "the capability %s is not a value: it denotes the provider the nearest with binds, and only its methods are called (types.md, Capabilities 2; README Gap 29)", op.cap)
	case modeCapFn:
		c.errorf(op.expr, CodeType, "a capability method is only called (types.md, Capabilities 2)")
	case modeBuiltin:
		c.errorf(op.expr, CodeType, "%s is only called", exprText(op.expr))
	case modeGeneric:
		if ea, ok := op.obj.(*explicitArgs); ok {
			sig := op.typ.(*Signature)
			if len(ea.targs) == len(sig.TParams) {
				// Every parameter is written: Id[int] is a function value
				// (Go's explicit instantiation; M10).
				if c.instantiateValue(op, sig, ea.targs) {
					return
				}
				break
			}
			c.errorf(op.expr, CodeType, "%s has implicit type parameters, which only a call infers; call it or write a function literal", exprText(op.expr))
			break
		}
		c.errorf(op.expr, CodeType, "the generic function %s is only called or instantiated", exprText(op.expr))
	}
	op.mode, op.typ, op.errs = modeInvalid, invalid, nil
}

// paramMap maps each type parameter to its argument.
func paramMap(tps []*TypeParam, targs []Type) map[*TypeParam]Type {
	m := make(map[*TypeParam]Type, len(tps))
	for i, tp := range tps {
		if i < len(targs) && targs[i] != nil {
			m[tp] = targs[i]
		}
	}
	return m
}

// instantiateValue turns a fully instantiated generic function into a
// function value: each type argument satisfies its constraint (S108), and
// the value's type is the signature with the arguments substituted.
func (c *checker) instantiateValue(op *operand, sig *Signature, targs []Type) bool {
	m := paramMap(sig.TParams, targs)
	for i, tp := range sig.TParams {
		if tp.Kind != KindType || tp.Constraint == nil {
			continue
		}
		cons := c.subst(tp.Constraint, m)
		if why := c.satisfies(targs[i], cons); why != "" {
			c.errorf(op.expr, CodeType, "%s does not satisfy the constraint %s of %s: %s (types.md, Generics 2)", targs[i], cons, tp.Name, why)
			return false
		}
	}
	if op.fn != nil {
		c.recordInstantiation(op.fn.Name, sig.TParams, targs, op.expr)
	}
	inst := c.subst(sig, m).(*Signature)
	inst.TParams = nil
	inst.NBracket = 0
	op.mode, op.typ, op.obj = modeValue, inst, nil
	// The expression and its parentheses record the instantiated type, not
	// the generic one recorded while they were checked (D1; round-2 M8).
	for x := op.expr; x != nil; {
		c.record(x, *op)
		p, ok := x.(*syntax.ParenExpr)
		if !ok {
			break
		}
		x = p.X
	}
	return true
}

func exprText(x syntax.Expr) string {
	switch x := x.(type) {
	case *syntax.Ident:
		return x.Name
	case *syntax.SelectorExpr:
		return exprText(x.X) + "." + x.Sel.Name
	case *syntax.ParenExpr:
		return exprText(x.X)
	}
	return "this expression"
}

func (c *checker) expr0(x syntax.Expr, hint Type) operand {
	switch x := x.(type) {
	case *syntax.Ident:
		return c.ident(x)
	case *syntax.BasicLit:
		return c.basicLit(x)
	case *syntax.ParenExpr:
		// Parentheses keep what the operand denotes: (x) = 1 assigns x,
		// as in Go (N4). A generic function is the exception: Go reads
		// the inside of parentheses as a value, so (Fn)[int] and (Fn)(1)
		// are errors there and (Fn[int]) is the instance (round 5).
		op := c.expr(x.X, hint)
		if op.mode == modeGeneric {
			c.needValue(&op)
		}
		return op
	case *syntax.CompositeLit:
		return c.compositeLit(x, hint)
	case *syntax.FuncLit:
		sig, _ := under0(hint).(*Signature)
		return c.funcLit(x, sig, nil)
	case *syntax.SelectorExpr:
		return c.selector(x)
	case *syntax.IndexExpr:
		return c.index(x, hint)
	case *syntax.SliceExpr:
		return c.sliceExpr(x)
	case *syntax.CallExpr:
		return c.call(x, hint)
	case *syntax.StarExpr:
		op := c.valueExpr(x.X, nil)
		if op.invalid() {
			return op
		}
		p, ok := op.typ.(*Pointer)
		if !ok {
			c.errorf(x, CodeType, "cannot indirect %s", op.typ)
			return bad(x)
		}
		return operand{mode: modeValue, typ: p.Elem, addressable: true}
	case *syntax.UnaryExpr:
		return c.unary(x, hint)
	case *syntax.BinaryExpr:
		return c.binary(x, hint)
	case *syntax.TryExpr:
		return c.try(x, hint)
	case *syntax.CatchExpr:
		return c.catch(x, hint, true, false)
	case *syntax.WireLit:
		c.errorf(x, CodeType, "a wire literal appears only as the argument of with (grammar.md, Expressions 4)")
		return bad(x)
	case *syntax.GoExpr:
		return c.goExpr(x)
	case *syntax.KeyValueExpr:
		c.errorf(x, CodeType, "a key: value pair belongs in a composite literal")
		return bad(x)
	case *syntax.TupleExpr:
		c.errorf(x, CodeType, "a tuple is a case of a multi-subject type switch only")
		return bad(x)
	case *syntax.TypeGuard:
		c.errorf(x, CodeType, "x.(type) is the subject of a type switch only")
		return bad(x)
	case *syntax.ArrayType, *syntax.MapType, *syntax.ChanType, *syntax.FuncType, *syntax.StructType, *syntax.InterfaceType:
		t := c.typExpr(x, c.localTypeCtx())
		if isInvalid(t) {
			return bad(x)
		}
		return operand{mode: modeType, typ: t}
	case *syntax.Ellipsis, *syntax.ErrorRow, *syntax.MachineType:
		c.errorf(x, CodeType, "unexpected type form in an expression")
		return bad(x)
	}
	c.errorf(x, CodeType, "unexpected expression")
	return bad(x)
}

func under0(t Type) Type {
	if t == nil {
		return nil
	}
	return under(t)
}

// localTypeCtx is the type context of a type written inside a body: the
// enclosing function's type parameters are in scope.
func (c *checker) localTypeCtx() *typeCtx {
	ctx := &typeCtx{params: map[string]*TypeParam{}}
	if c.fn != nil && c.fn.fn != nil && c.fn.fn.Sig != nil {
		for _, tp := range c.fn.fn.Sig.TParams {
			ctx.params[tp.Name] = tp
		}
		if c.fn.fn.Recv != nil && c.fn.fn.Decl.Recv != nil {
			rt := c.fn.fn.Decl.Recv.List[0].Type
			if s, ok := rt.(*syntax.StarExpr); ok {
				rt = s.X
			}
			if ix, ok := rt.(*syntax.IndexExpr); ok {
				for i, a := range ix.Indices {
					if id, ok := a.(*syntax.Ident); ok && i < len(c.fn.fn.Recv.TParams) {
						ctx.params[id.Name] = c.fn.fn.Recv.TParams[i]
					}
				}
			}
		}
	}
	if c.fn != nil {
		ctx.scope = c.fn.scope
	}
	return ctx
}

// lookup resolves a name in expression position, innermost frame first:
// locals and the body's frame, the package, the universe and the prelude,
// then the file's import names (D4).
func (c *checker) lookup(name string) Object {
	if c.fn != nil && c.fn.scope != nil {
		if o, _ := c.fn.scope.lookup(name); o != nil {
			return o
		}
	} else if o, _ := c.pkg.Scope.lookup(name); o != nil {
		return o
	}
	if c.cur != nil {
		if o, ok := c.cur.scope.names[name]; ok {
			return o
		}
	}
	return nil
}

func (c *checker) ident(x *syntax.Ident) operand {
	if x.Name == "_" {
		c.errorf(x, CodeType, "_ is not a value")
		return bad(x)
	}
	o := c.lookup(x.Name)
	if o == nil {
		c.errorf(x, CodeUnknown, "unknown name %s", x.Name)
		return bad(x)
	}
	c.recordUse(x, o)
	switch o := o.(type) {
	case *Var:
		o.used = true
		if o.group != nil {
			o.group.used = true
		}
		if o.pkg {
			c.recordDep(o)
		}
		if o.pkg && o.Type == nil {
			c.ensurePkgVar(o)
		}
		if o.Type == nil || isInvalid(o.Type) {
			return bad(x)
		}
		return operand{mode: modeValue, typ: o.Type, addressable: !o.pkg, recvVar: o}
	case *Func:
		if c.isEntry(o) {
			// Only the host calls main, so it is never a value: an alias
			// or an argument would let ordinary code call it (D10; M15).
			c.errorf(x, CodeEntry, "main is called only by the host, so it is not a value (rows.md, Capability rows 4)")
			return bad(x)
		}
		sig := c.funcSig(o)
		c.recordDep(o)
		if c.fn != nil && c.fn.reach != nil {
			c.fn.reach.funcs = append(c.fn.reach.funcs, o)
		}
		if len(sig.TParams) > 0 {
			return operand{mode: modeGeneric, typ: sig, fn: o}
		}
		return operand{mode: modeValue, typ: sig, fn: o}
	case *TypeName:
		if n, ok := o.Type.(*Named); ok {
			c.resolveNamed(n)
		}
		if o == universeOnce.errorName {
			return operand{mode: modeType, typ: invalid, obj: o}
		}
		if isCapability(o.Type) {
			return operand{mode: modeCap, typ: o.Type, cap: o.Type, obj: o}
		}
		return operand{mode: modeType, typ: o.Type, obj: o}
	case *capBinding:
		return operand{mode: modeCap, typ: o.tn.Type, cap: o.tn.Type, obj: o.tn}
	case *Const:
		v := constant.MakeBool(o.Val)
		return operand{mode: modeConst, typ: o.Type, val: v}
	case *Builtin:
		return operand{mode: modeBuiltin, obj: o, typ: invalid}
	case *PkgName:
		o.used = true
		return operand{mode: modePkg, obj: o, typ: invalid}
	}
	c.errorf(x, CodeUnknown, "unknown name %s", x.Name)
	return bad(x)
}

func (c *checker) ensurePkgVar(v *Var) {
	for _, pv := range c.vars {
		if pv.obj == v {
			c.checkPkgVar(pv)
			return
		}
	}
}

// ---------------------------------------------------------------- literals

func (c *checker) basicLit(x *syntax.BasicLit) operand {
	switch x.Kind {
	case syntax.INT:
		if len(x.Value) > 1 && x.Value[0] == '0' {
			c.errorf(x, CodeType, "the integer literal %s has a leading zero; Go would read it as octal (grammar.md, Literals 1)", x.Value)
			return bad(x)
		}
		v := constant.MakeFromLiteral(x.Value, token.INT, 0)
		if v.Kind() == constant.Unknown {
			c.errorf(x, CodeType, "malformed integer literal %s", x.Value)
			return bad(x)
		}
		return operand{mode: modeConst, typ: Typ[UntypedInt], val: v}
	case syntax.DURATION:
		i := 0
		for i < len(x.Value) && x.Value[i] >= '0' && x.Value[i] <= '9' {
			i++
		}
		digits, unit := x.Value[:i], x.Value[i:]
		if len(digits) > 1 && digits[0] == '0' {
			c.errorf(x, CodeType, "the duration literal %s has a leading zero", x.Value)
			return bad(x)
		}
		scale := map[string]int64{"ns": 1, "us": 1e3, "ms": 1e6, "s": 1e9, "m": 60e9, "h": 3600e9}[unit]
		n := constant.MakeFromLiteral(digits, token.INT, 0)
		v := constant.BinaryOp(n, token.MUL, constant.MakeInt64(scale))
		if !fitsInt64(v) {
			c.errorf(x, CodeType, "the duration %s overflows int64 nanoseconds (grammar.md, Literals 2)", x.Value)
			return bad(x)
		}
		return operand{mode: modeConst, typ: Typ[Duration], val: v}
	case syntax.STRING:
		s, err := strconv.Unquote(x.Value)
		if err != nil {
			c.errorf(x, CodeType, "invalid escape in the string literal %s (grammar.md, Literals 3)", x.Value)
			return bad(x)
		}
		return operand{mode: modeConst, typ: Typ[UntypedString], val: constant.MakeString(s)}
	case syntax.RUNE:
		s, err := strconv.Unquote(x.Value)
		if err != nil {
			c.errorf(x, CodeType, "malformed rune literal %s", x.Value)
			return bad(x)
		}
		r := []rune(s)
		if len(r) != 1 {
			c.errorf(x, CodeType, "malformed rune literal %s", x.Value)
			return bad(x)
		}
		return operand{mode: modeConst, typ: Typ[UntypedRune], val: constant.MakeInt64(int64(r[0]))}
	}
	c.errorf(x, CodeType, "unexpected literal")
	return bad(x)
}

func fitsInt64(v constant.Value) bool {
	_, exact := constant.Int64Val(constant.ToInt(v))
	return exact
}

// ---------------------------------------------------------------- composite literals

func (c *checker) compositeLit(x *syntax.CompositeLit, hint Type) operand {
	var t Type
	switch {
	case x.Type == nil:
		if hint == nil || isInvalid(hint) {
			if hint == nil {
				c.errorf(x, CodeType, "a composite literal without a type needs one from its context")
			}
			return bad(x)
		}
		t = hint
	default:
		t = c.literalType(x, hint)
		if t == nil {
			return bad(x)
		}
	}
	if isInvalid(t) {
		return bad(x)
	}
	switch u := under(t).(type) {
	case *Struct:
		if !c.structLit(x, t, u) {
			return operand{mode: modeValue, typ: t}
		}
	case *Slice:
		c.elements(x, u.Elem, -1)
	case *Array:
		if u.Len < 0 {
			n := c.elements(x, u.Elem, -1)
			t = &Array{Len: int64(n), Elem: u.Elem}
		} else {
			c.elements(x, u.Elem, u.Len)
		}
	case *Map:
		// A constant key appears once, compared after conversion to the
		// key type, as in Go (N3).
		seen := map[string]bool{}
		for _, e := range x.Elts {
			kv, ok := e.(*syntax.KeyValueExpr)
			if !ok {
				c.errorf(e, CodeType, "a map literal's elements are key: value pairs")
				continue
			}
			k := c.valueExpr(kv.Key, u.Key)
			if c.assign(&k, u.Key, "map key") && k.mode == modeConst && k.val != nil {
				// Keys of an interface type compare by dynamic type and
				// value: int(1) and int64(1) are distinct (round-2 M7).
				dyn := u.Key
				if asInterface(u.Key) != nil {
					dyn = defaultType(k.typ)
				}
				key := typeKey(dyn) + "|" + k.val.Kind().String() + ":" + k.val.ExactString()
				if seen[key] {
					c.errorf(kv.Key, CodeType, "the key %s appears twice in this map literal", k.val.ExactString())
				}
				seen[key] = true
			}
			v := c.valueExpr(kv.Value, u.Elem)
			c.assign(&v, u.Elem, "map value")
		}
	case *MachineType:
		if len(x.Elts) > 0 {
			c.errorf(x, CodeType, "a machine value has no fields")
		}
	default:
		c.errorf(x, CodeType, "cannot write a composite literal of %s", t)
		return bad(x)
	}
	return operand{mode: modeValue, typ: t}
}

// literalType resolves a composite literal's written type. A generic type
// written without type arguments takes them from the expected type (a union
// that lists one of its instances, or an instance), else from its field
// values (D6: Some{n}, Enter{x}).
func (c *checker) literalType(x *syntax.CompositeLit, hint Type) Type {
	if at, ok := x.Type.(*syntax.ArrayType); ok {
		if _, ell := at.Len.(*syntax.Ellipsis); ell {
			elem := c.typExpr(at.Elt, c.localTypeCtx())
			if isInvalid(elem) {
				return nil
			}
			return &Array{Len: -1, Elem: elem}
		}
	}
	raw := c.typExpr0(x.Type, c.localTypeCtx())
	if isInvalid(raw) {
		return nil
	}
	n, ok := raw.(*Named)
	if !ok || n.Orig != nil || len(n.TParams) == 0 {
		if tp, ok := raw.(*TypeParam); ok && tp.Kind != KindType {
			c.errorf(x.Type, CodeType, "%s is %s parameter, not a type", tp.Name, tp.Kind)
			return nil
		}
		return raw
	}
	if inst := instanceFromHint(n, hint); inst != nil {
		return inst
	}
	// Infer from the field values.
	s, ok := n.underlying().(*Struct)
	if !ok {
		c.errorf(x.Type, CodeType, "generic type %s needs type arguments", n)
		return nil
	}
	m := map[*TypeParam]Type{}
	set := map[*TypeParam]bool{}
	for _, tp := range n.TParams {
		set[tp] = true
	}
	inf := &inferer{c: c, m: m, set: set}
	for i, e := range x.Elts {
		var ft Type
		val := e
		if kv, ok := e.(*syntax.KeyValueExpr); ok {
			id, _ := kv.Key.(*syntax.Ident)
			if id == nil {
				continue
			}
			f, _ := s.field(id.Name)
			if f == nil {
				continue
			}
			ft, val = f.Type, kv.Value
		} else if i < len(s.Fields) {
			ft = s.Fields[i].Type
		}
		if ft == nil {
			continue
		}
		c.silent++
		op := c.expr(val, nil)
		c.silent--
		if op.invalid() || op.mode == modeType {
			continue
		}
		vt := op.typ
		if op.mode == modeConst {
			vt = defaultType(vt)
		}
		inf.unify(ft, vt)
	}
	targs := make([]Type, len(n.TParams))
	for i, tp := range n.TParams {
		b := m[tp]
		if b == nil {
			if tp.Kind != KindType {
				b = &RowArg{Row: newRow()}
			} else {
				c.errorf(x.Type, CodeType, "cannot infer the type argument %s of %s: write %s[…] (D6)", tp.Name, n.Obj.Name, n.Obj.Name)
				return nil
			}
		}
		targs[i] = b
	}
	// Inferred arguments meet the same constraints as written ones (D6; M5).
	for i, tp := range n.TParams {
		if tp.Kind == KindType && tp.Constraint != nil {
			// A constraint may name other parameters, I[T]: it is checked
			// with every argument substituted (round-2 M6).
			cons := c.subst(tp.Constraint, paramMap(n.TParams, targs))
			if why := c.satisfies(targs[i], cons); why != "" {
				c.errorf(x.Type, CodeType, "the inferred type argument %s of %s does not satisfy %s: %s (types.md, Generics 2)", targs[i], n.Obj.Name, cons, why)
				return nil
			}
		}
	}
	c.recordInstantiation(n.Obj.Name, n.TParams, targs, x)
	return c.instantiate(n, targs)
}

// instanceFromHint finds the instance of a generic origin that an expected
// type names: the hint itself, or the variant of a union hint.
func instanceFromHint(orig *Named, hint Type) Type {
	h, ok := hint.(*Named)
	if !ok {
		return nil
	}
	if h.origin() == orig && h.Orig != nil {
		return h
	}
	if u := asUnion(h); u != nil {
		for _, v := range u.Variants {
			if vn, ok := v.(*Named); ok && vn.origin() == orig {
				return vn
			}
		}
	}
	return nil
}

// structLit checks a struct literal: all keyed or all positional; a
// positional literal lists every field; a keyed one names declared fields
// once and sets every field that has no zero value (types.md, Structs 2;
// Zero values 2).
func (c *checker) structLit(x *syntax.CompositeLit, t Type, s *Struct) bool {
	ok := true
	if len(x.Elts) > 0 {
		if _, keyed := x.Elts[0].(*syntax.KeyValueExpr); !keyed {
			if len(x.Elts) != len(s.Fields) {
				c.errorf(x, CodeType, "a positional literal of %s lists all %d fields, not %d (types.md, Structs 2)", t, len(s.Fields), len(x.Elts))
				for i, e := range x.Elts {
					if i < len(s.Fields) {
						op := c.valueExpr(e, s.Fields[i].Type)
						c.assign(&op, s.Fields[i].Type, "field "+s.Fields[i].Name)
					} else {
						c.valueExpr(e, nil)
					}
				}
				return false
			}
			for i, e := range x.Elts {
				f := s.Fields[i]
				op := c.valueExpr(e, f.Type)
				c.assign(&op, f.Type, "field "+f.Name)
			}
			return true
		}
	}
	set := map[string]bool{}
	for _, e := range x.Elts {
		kv, isKV := e.(*syntax.KeyValueExpr)
		if !isKV {
			continue
		}
		id, isID := kv.Key.(*syntax.Ident)
		if !isID {
			c.errorf(kv.Key, CodeType, "a struct literal's key is a field name")
			c.valueExpr(kv.Value, nil)
			ok = false
			continue
		}
		f, _ := s.field(id.Name)
		if f == nil {
			c.errorf(id, CodeUnknown, "%s has no field %s", t, id.Name)
			c.valueExpr(kv.Value, nil)
			ok = false
			continue
		}
		if set[id.Name] {
			c.errorf(id, CodeType, "field %s is set twice", id.Name)
			ok = false
			continue
		}
		set[id.Name] = true
		op := c.valueExpr(kv.Value, f.Type)
		c.assign(&op, f.Type, "field "+f.Name)
	}
	if !ok {
		return false
	}
	var missing []string
	for _, f := range s.Fields {
		if !set[f.Name] && !isInvalid(f.Type) && !defaultable(f.Type) {
			missing = append(missing, f.Name)
		}
	}
	if len(missing) > 0 {
		c.errorf(x, CodeType, "%s must set %s: a %s has no zero value (types.md, Zero values 2)", t, strings.Join(missing, ", "), kindName(s, missing[0]))
		return false
	}
	return true
}

func kindName(s *Struct, field string) string {
	f, _ := s.field(field)
	if f == nil {
		return "field"
	}
	switch under(f.Type).(type) {
	case *Union:
		return "union"
	case *Interface:
		return "interface value"
	case *Signature:
		return "function value"
	case *Struct:
		return "struct of non-defaultable fields"
	}
	return f.Type.String()
}

func (c *checker) elements(x *syntax.CompositeLit, elem Type, n int64) int {
	count := 0
	for _, e := range x.Elts {
		val := e
		if kv, ok := e.(*syntax.KeyValueExpr); ok {
			k := c.valueExpr(kv.Key, Typ[Int])
			c.assign(&k, Typ[Int], "index")
			val = kv.Value
		}
		op := c.valueExpr(val, elem)
		c.assign(&op, elem, "element")
		count++
	}
	if n >= 0 && int64(count) > n {
		c.errorf(x, CodeType, "%d elements in an array of length %d", count, n)
	}
	return count
}

// ---------------------------------------------------------------- selectors

func (c *checker) selector(x *syntax.SelectorExpr) operand {
	if id, ok := x.X.(*syntax.Ident); ok && id.Name == "go" {
		if o := c.lookup("go"); o != nil {
			if pn, ok := o.(*PkgName); ok && pn.Pkg == universeOnce.goPkg {
				return c.qualified(x, pn)
			}
		}
	}
	base := c.expr(x.X, nil)
	switch base.mode {
	case modeInvalid:
		return bad(x)
	case modePkg:
		return c.qualified(x, base.obj.(*PkgName))
	case modeCap:
		it := asInterface(base.cap)
		m := it.method(x.Sel.Name)
		if m == nil {
			c.errorf(x.Sel, CodeUnknown, "the capability %s has no method %s", base.cap, x.Sel.Name)
			return bad(x)
		}
		if c.fn != nil && c.fn.reach != nil {
			c.fn.reach.capCalls = append(c.fn.reach.capCalls, capCall{cap: base.cap.(*Named), name: m.Name})
		}
		return operand{mode: modeCapFn, typ: m.Sig, cap: base.cap}
	case modeType:
		c.errorf(x, CodeType, "%s.%s: a method expression is not part of the language; call the method on a value", base.typ, x.Sel.Name)
		return bad(x)
	}
	c.needValue(&base)
	c.handled(&base)
	if base.invalid() {
		return bad(x)
	}
	t := base.typ
	if p, ok := t.(*Pointer); ok {
		t = p.Elem
	}
	if s := asStruct(t); s != nil {
		if f, _ := s.field(x.Sel.Name); f != nil {
			return operand{mode: modeValue, typ: f.Type, addressable: base.addressable || isPointer(base.typ)}
		}
	}
	sig, fn := c.methodOf(t, x.Sel.Name)
	if sig != nil {
		if fn != nil {
			c.recordDep(fn)
		}
		if c.fn != nil && c.fn.reach != nil {
			if fn != nil {
				c.fn.reach.funcs = append(c.fn.reach.funcs, fn)
			} else {
				c.fn.reach.methodNames = append(c.fn.reach.methodNames, x.Sel.Name)
			}
		}
		return operand{mode: modeValue, typ: sig}
	}
	c.errorf(x.Sel, CodeUnknown, "%s has no field or method %s", base.typ, x.Sel.Name)
	return bad(x)
}

func isPointer(t Type) bool {
	_, ok := t.(*Pointer)
	return ok
}

// qualified resolves pkg.Name: only exported declarations (grammar.md,
// Visibility 2).
func (c *checker) qualified(x *syntax.SelectorExpr, pn *PkgName) operand {
	pn.used = true
	if pn.broken {
		return bad(x)
	}
	o, ok := pn.Pkg.Scope.names[x.Sel.Name]
	if !ok || !isExported(x.Sel.Name) {
		c.errorf(x.Sel, CodeUnknown, "%s.%s is not an exported declaration of package %s", pn.Name, x.Sel.Name, pn.Pkg.Path)
		return bad(x)
	}
	switch o := o.(type) {
	case *TypeName:
		if isCapability(o.Type) {
			return operand{mode: modeCap, typ: o.Type, cap: o.Type, obj: o}
		}
		return operand{mode: modeType, typ: o.Type, obj: o}
	case *Func:
		if len(o.Sig.TParams) > 0 {
			return operand{mode: modeGeneric, typ: o.Sig, fn: o}
		}
		return operand{mode: modeValue, typ: o.Sig, fn: o}
	case *Var:
		return operand{mode: modeValue, typ: o.Type}
	}
	c.errorf(x, CodeUnknown, "%s.%s is not a value or a type", pn.Name, x.Sel.Name)
	return bad(x)
}

// ---------------------------------------------------------------- index and slice

func (c *checker) index(x *syntax.IndexExpr, hint Type) operand {
	base := c.expr(x.X, nil)
	switch base.mode {
	case modeInvalid:
		return bad(x)
	case modeGeneric:
		return c.explicit(x, base)
	case modeType:
		t := c.typExpr(x, c.localTypeCtx())
		if isInvalid(t) {
			return bad(x)
		}
		return operand{mode: modeType, typ: t}
	}
	c.needValue(&base)
	c.handled(&base)
	if base.invalid() {
		return bad(x)
	}
	if len(x.Indices) != 1 {
		c.errorf(x, CodeType, "one index expected")
		return bad(x)
	}
	t := base.typ
	if p, ok := t.(*Pointer); ok {
		t = p.Elem
	}
	switch u := under(t).(type) {
	case *Map:
		k := c.valueExpr(x.Indices[0], u.Key)
		c.assign(&k, u.Key, "map key")
		return operand{mode: modeValue, typ: u.Elem, mapElem: true}
	case *Slice:
		c.intIndex(x.Indices[0])
		return operand{mode: modeValue, typ: u.Elem, addressable: true}
	case *Array:
		if i, ok := c.intIndex(x.Indices[0]); ok && i >= u.Len {
			c.errorf(x.Indices[0], CodeType, "index %d is out of bounds of %s (length %d)", i, base.typ, u.Len)
		}
		return operand{mode: modeValue, typ: u.Elem, addressable: base.addressable}
	case *Basic:
		if isString(t) {
			if i, ok := c.intIndex(x.Indices[0]); ok {
				if n, known := constLength(base); known && i >= n {
					c.errorf(x.Indices[0], CodeType, "index %d is out of bounds of a string of length %d", i, n)
				}
			}
			return operand{mode: modeValue, typ: Typ[Uint8]}
		}
	}
	c.errorf(x, CodeType, "cannot index %s", base.typ)
	return bad(x)
}

// intIndex checks an index, a slice bound or a make size: an integer, and a
// constant one is a nonnegative int (S108), as in Go. It returns the
// constant's value when there is one.
func (c *checker) intIndex(x syntax.Expr) (int64, bool) {
	op := c.valueExpr(x, Typ[Int])
	if op.invalid() {
		return 0, false
	}
	untypedConst := op.mode == modeConst && op.val != nil && isUntyped(op.typ)
	switch {
	case untypedConst && isNumeric(op.typ):
		// An untyped constant becomes an int: it must be representable.
		if !c.assign(&op, Typ[Int], "index") {
			return 0, false
		}
	case !isInteger(op.typ):
		c.errorf(x, CodeType, "an index must be an integer, not %s", op.typ)
		return 0, false
	}
	if op.mode != modeConst || op.val == nil {
		return 0, false
	}
	// A typed integer constant of any integer type indexes when its value
	// is representable as an int (Go's rule; round-2 M10).
	v, exact := constant.Int64Val(constant.ToInt(op.val))
	if !exact {
		c.errorf(x, CodeType, "the index %s overflows int", op.val.ExactString())
		return 0, false
	}
	if v < 0 {
		c.errorf(x, CodeType, "the index %d is negative", v)
		return 0, false
	}
	return v, true
}

// constLength returns the length of a constant string operand.
func constLength(op operand) (int64, bool) {
	if op.mode != modeConst || op.val == nil || op.val.Kind() != constant.String {
		return 0, false
	}
	return int64(len(constant.StringVal(op.val))), true
}

func (c *checker) sliceExpr(x *syntax.SliceExpr) operand {
	base := c.valueExpr(x.X, nil)
	// The constant bounds: ordered low <= high <= max, and within the
	// length of an array or a constant string (S108; Go's rule).
	limit, limited := int64(0), false
	if !base.invalid() {
		if a, ok := under(base.typ).(*Array); ok {
			limit, limited = a.Len, true
		} else if n, ok := constLength(base); ok {
			limit, limited = n, true
		}
	}
	last, haveLast := int64(0), false
	for _, e := range []syntax.Expr{x.Low, x.High, x.Max} {
		if e == nil {
			continue
		}
		v, ok := c.intIndex(e)
		if !ok {
			continue
		}
		switch {
		case limited && v > limit:
			c.errorf(e, CodeType, "the slice bound %d is out of bounds of length %d", v, limit)
		case haveLast && v < last:
			c.errorf(e, CodeType, "the slice bounds are out of order: %d is less than %d", v, last)
		}
		last, haveLast = v, true
	}
	if base.invalid() {
		return bad(x)
	}
	switch u := under(base.typ).(type) {
	case *Slice:
		return operand{mode: modeValue, typ: base.typ}
	case *Array:
		return operand{mode: modeValue, typ: &Slice{Elem: u.Elem}}
	case *Basic:
		if isString(base.typ) {
			if x.Slice3 {
				c.errorf(x, CodeType, "a 3-index slice of a string")
				return bad(x)
			}
			return operand{mode: modeValue, typ: defaultType(base.typ)}
		}
	}
	c.errorf(x, CodeType, "cannot slice %s", base.typ)
	return bad(x)
}

// ---------------------------------------------------------------- assignment

// assign checks that op may be assigned to t: an untyped constant must be
// representable (S108 on overflow), an untyped integer never becomes a
// duration (D2), and the value must be assignable.
func (c *checker) assign(op *operand, t Type, what string) bool {
	if op.invalid() || t == nil || containsInvalid(t) || containsInvalid(op.typ) {
		// Already reported where the invalid part came from (D3).
		return true
	}
	if op.mode == modeNoValue {
		c.errorf(op.expr, CodeType, "this expression has no value; %s needs one", what)
		op.mode = modeInvalid
		return false
	}
	if tp, ok := t.(*TypeParam); ok && isUntyped(op.typ) {
		c.errorf(op.expr, CodeType, "cannot use an untyped constant as %s of type %s", what, tp)
		return false
	}
	if isUntyped(op.typ) {
		return c.convertUntyped(op, t, what)
	}
	if ok, why := c.assignable(op.typ, t); !ok {
		if why != "" {
			c.errorf(op.expr, CodeType, "cannot use %s as %s in %s: %s", op.typ, t, what, why)
		} else {
			c.errorf(op.expr, CodeType, "cannot use %s as %s in %s", op.typ, t, what)
		}
		op.mode = modeInvalid
		return false
	}
	return true
}

// convertUntyped gives an untyped constant the type t.
func (c *checker) convertUntyped(op *operand, t Type, what string) bool {
	target := t
	if it := asInterface(t); it != nil {
		if len(it.Methods) == 0 && !it.Comparable {
			op.typ = defaultType(op.typ)
			if b, ok := op.typ.(*Basic); ok && !c.representable(op, b) {
				return false
			}
			return true
		}
		target = defaultType(op.typ)
		if why := c.implements(target, it); why != "" {
			c.errorf(op.expr, CodeType, "cannot use %s as %s in %s: %s", op.typ, t, what, why)
			op.mode = modeInvalid
			return false
		}
		return true
	}
	b, ok := under(target).(*Basic)
	if !ok {
		c.errorf(op.expr, CodeType, "cannot use %s as %s in %s", op.typ, t, what)
		op.mode = modeInvalid
		return false
	}
	src := op.typ.(*Basic)
	compatible := false
	switch src.Kind {
	case UntypedBool:
		compatible = b.Kind == Bool
	case UntypedString:
		compatible = b.Kind == String
	case UntypedInt, UntypedRune, UntypedFloat:
		compatible = b.Kind >= Int && b.Kind <= Complex128
	}
	if !compatible {
		if b.Kind == Duration && (src.Kind == UntypedInt || src.Kind == UntypedRune) {
			c.errorf(op.expr, CodeType, "an untyped integer is not a duration: write a duration literal such as %ss (D2; #175)", constText(op.val))
		} else {
			c.errorf(op.expr, CodeType, "cannot use %s as %s in %s", op.typ, t, what)
		}
		op.mode = modeInvalid
		return false
	}
	if !c.representable(op, b) {
		return false
	}
	op.typ = t
	return true
}

func constText(v constant.Value) string {
	if v == nil {
		return "1"
	}
	return v.ExactString()
}

// defaultOf gives an operand its type for a declaration without a written
// type: an untyped constant takes its default type.
func (c *checker) defaultOf(op *operand) Type {
	if op.invalid() {
		return invalid
	}
	if op.mode == modeNoValue {
		c.errorf(op.expr, CodeType, "this expression has no value")
		return invalid
	}
	if isUntyped(op.typ) {
		t := defaultType(op.typ)
		if !c.representable(op, t.(*Basic)) {
			return invalid
		}
		return t
	}
	if tup, ok := op.typ.(*Tuple); ok {
		c.errorf(op.expr, CodeType, "%d values where one is expected", len(tup.Types))
		return invalid
	}
	return op.typ
}
