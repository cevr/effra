package check

import (
	"sort"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
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
	var binds *capSet
	known := true
	switch {
	case len(x.Args) == 0:
		c.errorf(x, CodeType, "with takes one wire (rows.md, With 1)")
		known = false
	case len(x.Args) > 1:
		c.outside(x.Args[1], "a with over several wires (196x, extern and wire values)")
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
	// The with's expected type is the yielded expression's, as a hint: the
	// block's own result stays its last expression's type (M16).
	var expect []Type
	if hint != nil {
		expect = []Type{hint}
	}
	c.typedFrame(x.Trailing)
	res := c.yieldingBlock(x.Trailing, x.Trailing.List, expect, false, func(op *operand) { errs = op.errs })
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
func (c *checker) withArg(arg syntax.Expr) (*capSet, bool) {
	switch a := unparen(arg).(type) {
	case *syntax.WireLit:
		return c.wireLit(a)
	case *syntax.Ident:
		w := c.pkg.Wires[a.Name]
		if w == nil {
			if o := c.lookup(a.Name); o != nil {
				if v, isVar := o.(*Var); isVar {
					// A local holding a wire value (196x): its value is S109
					// already when it is invalid, and nothing cascades (D3).
					v.used = true
					if !isInvalid(v.Type) {
						c.outside(a, "with over a value (196x, extern and wire values)")
					}
					return nil, false
				}
			}
			c.errorf(a, CodeUnknown, "unknown wire %s", a.Name)
			return nil, false
		}
		if c.fn != nil && c.fn.reach != nil {
			c.fn.reach.wires = append(c.fn.reach.wires, w)
		}
		binds := c.wireBinds(w)
		return binds, !w.unknown
	case *syntax.SelectorExpr:
		if id, ok := a.X.(*syntax.Ident); ok {
			if pn, ok := c.lookup(id.Name).(*PkgName); ok {
				pn.used = true
				if pn.broken {
					return nil, false
				}
				if w := pn.Pkg.Wires[a.Sel.Name]; w != nil && isExported(a.Sel.Name) {
					// Its package's pass summarised it; a set keyed by
					// capability names needs no conversion.
					return w.binds, !w.unknown
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

// wireLit checks an inline wire's entries and returns what it binds; known
// is false when it embeds an unknown wire (reported) or its summary was
// refused (wiresum.go).
func (c *checker) wireLit(w *syntax.WireLit) (binds *capSet, known bool) {
	// The header of an inline wire is refused as a named wire's is
	// (checkWires): never read partly (rows.md, Wire 8).
	if w.Provides != nil {
		c.errorf(w.Provides, CodeOutside, "a wire's provides clause is #196 stage b's (the wire header, rows.md Wire 8); it is not checked yet")
	}
	if w.Needs != nil {
		c.errorf(w.Needs, CodeOutside, "a wire's needs clause is #196 stage b's (the wire header, rows.md Wire 8); it is not checked yet")
	}
	return c.wireBody(w.Body)
}

// wireBody checks the body of a named or an inline wire and returns what
// it binds. The duplicate and replace rules are decided once
// per body by its summary (bodySummary), whose findings are reported on
// every pass; the embedded names, the keys and the providers are checked
// here, on every pass.
func (c *checker) wireBody(body *syntax.WireBody) (*capSet, bool) {
	sum := c.bodySummary(body)
	for _, f := range sum.findings {
		c.errorf(f.at, f.code, "%s", f.msg)
	}
	var build *target
	if c.fn != nil && c.fn.t != nil && c.fn.t.wire {
		build = c.fn.t
	}
	deps := &wireDeps{tab: c.types, needs: map[int32]*Row{}, at: map[int32]syntax.Expr{}}
	for _, e := range body.Entries {
		if e.Kind == syntax.WireEmbed {
			inner := c.embeddedWire(e)
			if inner == nil {
				c.errorf(e.Key, CodeUnknown, "unknown wire %s", exprText(e.Key))
			} else if c.fn != nil && c.fn.reach != nil {
				c.fn.reach.wires = append(c.fn.reach.wires, inner)
			}
			continue
		}
		if build != nil {
			build.charged = newRow()
		}
		caps := c.wireEntry(e)
		if build != nil {
			deps.add(caps, build.charged, e.Key)
		}
	}
	if build != nil {
		c.checkEntryCycles(build.name, deps)
	}
	return sum.binds, sum.known
}

// embeddedWire is the named wire an embedding entry names, or nil.
func (c *checker) embeddedWire(e *syntax.WireEntry) *Wire {
	if id, ok := e.Key.(*syntax.Ident); ok {
		return c.pkg.Wires[id.Name]
	}
	return nil
}

// wireDeps records, for a named wire's entries, the capabilities of the
// wire each entry's provider uses while it is built.
type wireDeps struct {
	tab   *typeTable
	order []Type
	needs map[int32]*Row // by the capability's interned id
	at    map[int32]syntax.Expr
}

func (d *wireDeps) add(caps []Type, needs *Row, at syntax.Expr) {
	for _, cap := range caps {
		id := d.tab.id(cap)
		if _, seen := d.needs[id]; !seen {
			d.order = append(d.order, cap)
			d.at[id] = at
			d.needs[id] = newRow()
		}
		d.needs[id] = d.needs[id].union(d.tab, needs)
	}
}

// checkEntryCycles reports entries of a named wire that need each other
// (S108, once per cycle): a provider that uses a capability of its own wire
// is built after that capability's provider, so a cycle has no first entry
// (rows.md, Wire 4 and 5).
func (c *checker) checkEntryCycles(name string, d *wireDeps) {
	const (
		white = iota
		grey
		black
	)
	colour := map[int32]int{}
	key := c.types.id
	var stack []Type
	var visit func(cap Type) []Type
	visit = func(cap Type) []Type {
		colour[key(cap)] = grey
		stack = append(stack, cap)
		defer func() {
			stack = stack[:len(stack)-1]
			colour[key(cap)] = black
		}()
		for _, next := range d.needs[key(cap)].Elems() {
			if _, entry := d.needs[key(next)]; !entry {
				continue // bound by an embedded wire, built in its own wire
			}
			switch colour[key(next)] {
			case grey:
				for i, s := range stack {
					if key(s) == key(next) {
						return append([]Type(nil), stack[i:]...)
					}
				}
			case white:
				if cycle := visit(next); cycle != nil {
					return cycle
				}
			}
		}
		return nil
	}
	for _, cap := range d.order {
		if colour[key(cap)] != white {
			continue
		}
		cycle := visit(cap)
		if cycle == nil {
			continue
		}
		for _, m := range cycle {
			colour[key(m)] = black
		}
		parts := make([]string, 0, len(cycle)+1)
		for _, m := range cycle {
			parts = append(parts, m.String())
		}
		parts = append(parts, cycle[0].String())
		c.errorf(d.at[key(cycle[0])], CodeType, "the entries of %s need each other (%s), so none can be built first (rows.md, Wire 4 and 5)", name, strings.Join(parts, " -> "))
	}
}

// wireEntry checks the provider of one `Cap = provider` or `replace Cap =
// provider` entry: it must implement each capability by method-row
// compatibility (S106); one value may serve several capabilities (README
// Gap 28 and 34). A constructor entry is #37's (S109). The second-entry
// and replace rules are the body summary's (wiresum.go). It returns the
// capabilities the entry names.
func (c *checker) wireEntry(e *syntax.WireEntry) []Type {
	var caps []Type
	for _, k := range append([]syntax.Expr{e.Key}, e.Keys...) {
		if t := c.wireKey(k); t != nil {
			caps = append(caps, t)
		}
	}
	if c.isConstructor(e.Value) {
		c.errorf(e.Value, CodeOutside, "a constructor entry is #37's (wire constructors); write a provider value, not a function")
		return caps
	}
	op := c.valueExpr(e.Value, nil)
	for _, cap := range caps {
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
	return caps
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
		// The wire header (#217) is checked by #196's second stage; until
		// then it is refused, never read partly (rows.md, Wire 8).
		if d.Provides != nil {
			c.errorf(d.Provides, CodeOutside, "a wire's provides clause is #196 stage b's (the wire header, rows.md Wire 8); it is not checked yet")
		}
		if d.Needs != nil {
			c.errorf(d.Needs, CodeOutside, "a wire's needs clause is #196 stage b's (the wire header, rows.md Wire 8); it is not checked yet")
		}
		t := &target{name: "wire " + w.Name, declErrors: newRow(), declUses: newRow(), wire: true}
		// The wire's providers may use what the wire binds (Wire 4).
		t.internal = c.wireBinds(w)
		if w.budget {
			c.errorf(d.Name, CodeDuplicateCap, "%s", wireBudgetMessage)
		}
		if t.internal == nil {
			// Its summary was refused (wiresum.go): nothing it charges is
			// reported (D3).
			t.withs = []*capSet{nil}
		}
		c.fn = &funcCtx{t: t, scope: newScope(c.pkg.Scope), reach: c.reachOf(w)}
		c.wireBody(d.Body)
		c.fn = nil
	}
}
