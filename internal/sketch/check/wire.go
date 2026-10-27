package check

import (
	"sort"

	"effra.local/prototype/internal/sketch/syntax"
)

// with checks `with(W) { block }`: W is a named wire or an inline wire
// literal; inside the block every capability W binds is discharged; the
// block yields its last expression, whose failures pass through (rows.md,
// With 1 to 6). An inline wire's providers are evaluated in the enclosing
// scope, before the discharge, so they charge the enclosing function and do
// not see their own wire (With 3).
func (c *checker) with(x *syntax.CallExpr, hint Type) operand {
	if x.Trailing == nil {
		c.errorf(x, CodeType, "with takes a wire and a block: with(W) { … } (rows.md, With 1)")
		c.silentArgs(x)
		return bad(x)
	}
	var binds *Row
	known := true
	switch {
	case len(x.Args) == 0:
		c.errorf(x, CodeType, "with takes one wire (rows.md, With 1)")
		known = false
	case len(x.Args) > 1:
		c.errorf(x.Args[1], CodeOutside, "with takes one wire; context values in with are G1's (runtime.md, G1) and are not checked yet")
		known = false
	default:
		binds, known = c.withArg(x.Args[0])
	}
	t := c.fn.t
	if known {
		t.withs = append(t.withs, binds)
	} else {
		t.withs = append(t.withs, nil) // unknown: discharge everything, so no cascade
	}
	done := c.openScope()
	c.fn.branches = append(c.fn.branches, branch{kind: brBarrier})
	var errs *Row
	res := c.yieldingBlock(x.Trailing, x.Trailing.List, nil, false, func(op *operand) { errs = op.errs })
	c.fn.branches = c.fn.branches[:len(c.fn.branches)-1]
	done()
	t.withs = t.withs[:len(t.withs)-1]
	if res == nil {
		return bad(x)
	}
	if res == NoValue {
		return operand{mode: modeNoValue, typ: NoValue, errs: errs}
	}
	op := operand{mode: modeValue, typ: res, errs: errs}
	return op
}

// withArg resolves with's argument to the capabilities it binds.
func (c *checker) withArg(arg syntax.Expr) (*Row, bool) {
	switch a := unparen(arg).(type) {
	case *syntax.WireLit:
		return c.wireLit(a), true
	case *syntax.Ident:
		w := c.pkg.Wires[a.Name]
		if w == nil {
			if o := c.lookup(a.Name); o != nil {
				if _, isVar := o.(*Var); isVar {
					c.errorf(a, CodeOutside, "with takes a wire; a value in with is a context value, G1's (runtime.md, G1), not checked yet")
					return nil, false
				}
			}
			c.errorf(a, CodeUnknown, "unknown wire %s", a.Name)
			return nil, false
		}
		if c.fn != nil && c.fn.reach != nil {
			c.fn.reach.wires = append(c.fn.reach.wires, w)
		}
		return c.wireBinds(w), true
	case *syntax.SelectorExpr:
		if id, ok := a.X.(*syntax.Ident); ok {
			if pn, ok := c.lookup(id.Name).(*PkgName); ok {
				pn.used = true
				if w := pn.Pkg.Wires[a.Sel.Name]; w != nil && isExported(a.Sel.Name) {
					return c.wireBinds(w), true
				}
				c.errorf(a.Sel, CodeUnknown, "%s.%s is not an exported wire of package %s", id.Name, a.Sel.Name, pn.Pkg.Path)
				return nil, false
			}
		}
	}
	c.errorf(arg, CodeOutside, "with takes a wire; a value in with is a context value, G1's (runtime.md, G1), not checked yet")
	c.silent++
	c.valueExpr(arg, nil)
	c.silent--
	return nil, false
}

// wireLit checks an inline wire's entries and returns what it binds.
func (c *checker) wireLit(w *syntax.WireLit) *Row {
	binds := newRow()
	embedded := newRow()
	for _, e := range w.Body.Entries {
		if e.Kind != syntax.WireEmbed {
			continue
		}
		id, ok := e.Key.(*syntax.Ident)
		named := (*Wire)(nil)
		if ok {
			named = c.pkg.Wires[id.Name]
		}
		if named == nil {
			c.errorf(e.Key, CodeUnknown, "unknown wire %s", exprText(e.Key))
			continue
		}
		if c.fn != nil && c.fn.reach != nil {
			c.fn.reach.wires = append(c.fn.reach.wires, named)
		}
		for _, cap := range c.wireBinds(named).Elems() {
			if embedded.Has(cap) {
				c.errorf(e.Key, CodeDuplicateCap, "%s is bound by two embedded wires; replace one entry (rows.md, Wire 7)", cap)
			}
			embedded = embedded.with(cap)
		}
	}
	binds = binds.union(embedded)
	for _, e := range w.Body.Entries {
		if e.Kind == syntax.WireEmbed {
			continue
		}
		binds = c.wireEntry(e, binds, embedded)
	}
	return binds
}

// wireEntry checks one `Cap = provider` or `replace Cap = provider` entry.
// The provider must implement each capability by method-row compatibility
// (S106); a second entry for one capability without replace is S111 (README
// Gap 28); one value may serve several capabilities (README Gap 34). A
// constructor entry is #37's (S109).
func (c *checker) wireEntry(e *syntax.WireEntry, binds, embedded *Row) *Row {
	keys := append([]syntax.Expr{e.Key}, e.Keys...)
	var caps []Type
	var capKeys []syntax.Expr
	for _, k := range keys {
		t := c.wireKey(k)
		if t != nil {
			caps = append(caps, t)
			capKeys = append(capKeys, k)
		}
	}
	if c.isConstructor(e.Value) {
		c.errorf(e.Value, CodeOutside, "a constructor entry is #37's (wire constructors); write a provider value, not a function")
		for _, cap := range caps {
			binds = binds.with(cap)
		}
		return binds
	}
	op := c.valueExpr(e.Value, nil)
	for i, cap := range caps {
		k := capKeys[i]
		switch {
		case e.Kind == syntax.WireReplace && !embedded.Has(cap):
			c.errorf(k, CodeType, "replace %s names no entry of an embedded wire (rows.md, Wire 7)", cap)
		case e.Kind != syntax.WireReplace && binds.Has(cap):
			c.errorf(k, CodeDuplicateCap, "%s has a second entry in this wire; only replace overrides an embedded entry (rows.md, Wire 1; README Gap 28)", cap)
		}
		binds = binds.with(cap)
		if op.invalid() {
			continue
		}
		if op.mode == modeNoValue {
			c.errorf(e.Value, CodeType, "this provider has no value")
			continue
		}
		pt := c.defaultOf(&op)
		if isInvalid(pt) {
			continue
		}
		if why := c.implements(pt, asInterface(cap)); why != "" {
			c.errorf(e.Value, CodeProvider, "%s cannot provide %s: %s (rows.md, Wire 2)", pt, cap, why)
			continue
		}
		if pn, ok := pt.(*Named); ok {
			cn := cap.(*Named)
			c.providers[cn] = append(c.providers[cn], pn)
		}
	}
	return binds
}

// isConstructor reports whether a provider expression names a function
// without calling it (rows.md, Wire 6: constructor entries, #37).
func (c *checker) isConstructor(x syntax.Expr) bool {
	switch x := unparen(x).(type) {
	case *syntax.Ident:
		_, ok := c.lookup(x.Name).(*Func)
		return ok
	case *syntax.SelectorExpr:
		if id, ok := x.X.(*syntax.Ident); ok {
			if pn, ok := c.lookup(id.Name).(*PkgName); ok {
				_, isFunc := pn.Pkg.Scope.names[x.Sel.Name].(*Func)
				return isFunc
			}
		}
	}
	return false
}

// wireKey resolves a wire entry's key to a capability (S104 otherwise).
func (c *checker) wireKey(k syntax.Expr) Type {
	t := c.rowElem(k, KindUses, c.localTypeCtx())
	if t == nil {
		return nil
	}
	if _, isParam := t.(*TypeParam); isParam {
		c.errorf(k, CodeType, "a wire entry names a capability, not a row parameter")
		return nil
	}
	return t
}

// wireBinds returns the capabilities a named wire binds: its entries and
// those of the wires it embeds. An embedding cycle stops here; the cycle
// itself is reported by checkWireCycles.
func (c *checker) wireBinds(w *Wire) *Row {
	if w.binds != nil {
		return w.binds
	}
	if w.state == 1 {
		return newRow()
	}
	w.state = 1
	saved := c.cur
	c.cur = w.file
	c.silent++
	binds := newRow()
	for _, e := range w.Decl.Body.Entries {
		if e.Kind == syntax.WireEmbed {
			if id, ok := e.Key.(*syntax.Ident); ok {
				if inner := w.pkg.Wires[id.Name]; inner != nil {
					binds = binds.union(c.wireBinds(inner))
				}
			}
			continue
		}
		for _, k := range append([]syntax.Expr{e.Key}, e.Keys...) {
			if t := c.rowElem(k, KindUses, nil); t != nil {
				if _, isParam := t.(*TypeParam); !isParam {
					binds = binds.with(t)
				}
			}
		}
	}
	c.silent--
	c.cur = saved
	w.binds = binds
	w.state = 2
	return binds
}

// checkWires checks the named wires: their entries, in a closed build
// context (README Gap 6: a named wire requires nothing from the with site
// and lets no failure escape).
func (c *checker) checkWires() {
	var wires []*Wire
	for _, w := range c.pkg.Wires {
		wires = append(wires, w)
	}
	sort.Slice(wires, func(i, j int) bool {
		if wires[i].file.index != wires[j].file.index {
			return wires[i].file.index < wires[j].file.index
		}
		return wires[i].Pos < wires[j].Pos
	})
	for _, w := range wires {
		c.cur = w.file
		d := w.Decl
		if d.Uses != nil {
			c.errorf(d.Uses, CodeOutside, "a capability row on a wire is open (README Gap 33): the corpus means exposure, and its spelling is an owner question; it is not checked yet")
		}
		t := &target{name: "wire " + w.Name, declErrors: newRow(), declUses: newRow(), wire: true}
		c.fn = &funcCtx{t: t, scope: newScope(c.pkg.Scope), reach: c.reachOf(w)}
		embedded := newRow()
		for _, e := range d.Body.Entries {
			if e.Kind != syntax.WireEmbed {
				continue
			}
			id, ok := e.Key.(*syntax.Ident)
			var inner *Wire
			if ok {
				inner = c.pkg.Wires[id.Name]
			}
			if inner == nil {
				c.errorf(e.Key, CodeUnknown, "unknown wire %s", exprText(e.Key))
				continue
			}
			c.fn.reach.wires = append(c.fn.reach.wires, inner)
			for _, cap := range c.wireBinds(inner).Elems() {
				if embedded.Has(cap) {
					c.errorf(e.Key, CodeDuplicateCap, "%s is bound by two embedded wires; replace one entry (rows.md, Wire 7)", cap)
				}
				embedded = embedded.with(cap)
			}
		}
		binds := embedded
		for _, e := range d.Body.Entries {
			if e.Kind == syntax.WireEmbed {
				continue
			}
			binds = c.wireEntry(e, binds, embedded)
		}
		c.fn = nil
	}
}
