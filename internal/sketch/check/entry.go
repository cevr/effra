package check

import (
	"ergo.local/prototype/internal/sketch/syntax"
)

// checkTypeDecls checks what a type declaration states beyond its body: a
// struct does not contain itself by value (types.md, Structs 4), and a
// machine's initial value and step function fit its state and signal unions
// (runtime.md, Machines and actors 1).
func (c *checker) checkTypeDecls() {
	c.checkRecursiveStructs()
	for _, tn := range c.machines {
		if tn.Pkg != c.pkg {
			continue
		}
		c.checkMachine(tn)
	}
}

// checkRecursiveStructs reports each struct type that contains itself by
// value through its fields (directly, through an array, an anonymous struct
// or an instance of a generic struct): its size would be infinite. Slices,
// maps, channels, interfaces and unions break the cycle, as in Go. The walk
// follows instances with their type arguments substituted, so
// `type U struct{ F Wrap[U] }` closes through Wrap's `Value T`. It has no
// depth cutoff (round-2 M5): instantiation cycles are rejected first
// (checkInstantiationCycles) and the walk skips their types, so the
// instances it reaches are finitely many and each is visited once. One
// diagnostic per cycle, at the cycle's first declaration in source order.
//
// Prior art: go/types validType walks the same instance graph with the
// type path as its cycle witness.
func (c *checker) checkRecursiveStructs() {
	const (
		white = iota
		grey
		black
	)
	colour := map[*Named]int{}
	reported := map[*Named]bool{}
	var stack []*Named
	var cycles [][]*Named
	var fields func(t Type, out *[]*Named)
	fields = func(t Type, out *[]*Named) {
		switch t := t.(type) {
		case *Named:
			if t.foreign {
				return
			}
			if t.Orig == nil && t.Obj.Decl == nil {
				return
			}
			if c.monoBad(t.origin().TParams) {
				return // on an instantiation cycle: reported
			}
			if _, ok := t.underlying().(*Struct); ok {
				*out = append(*out, t)
			}
		case *Array:
			fields(t.Elem, out)
		case *Struct:
			for _, f := range t.Fields {
				fields(f.Type, out)
			}
		}
	}
	// visit walks iteratively, so a chain of thousands of types needs no
	// deep recursion.
	type frame struct {
		n    *Named
		next []*Named
		i    int
	}
	visit := func(root *Named) {
		var work []frame
		push := func(n *Named) {
			colour[n] = grey
			stack = append(stack, n)
			var next []*Named
			if s, ok := n.underlying().(*Struct); ok {
				for _, f := range s.Fields {
					fields(f.Type, &next)
				}
			}
			work = append(work, frame{n: n, next: next})
		}
		push(root)
		for len(work) > 0 {
			top := &work[len(work)-1]
			if top.i == len(top.next) {
				colour[top.n] = black
				stack = stack[:len(stack)-1]
				work = work[:len(work)-1]
				continue
			}
			m := top.next[top.i]
			top.i++
			switch colour[m] {
			case grey:
				for k := len(stack) - 1; k >= 0; k-- {
					if stack[k] == m {
						cycles = append(cycles, append([]*Named(nil), stack[k:]...))
						break
					}
				}
			case white:
				push(m)
			}
		}
	}
	for _, tn := range c.pkg.Types {
		n, ok := tn.Type.(*Named)
		if !ok || colour[n] != white || c.monoBad(n.TParams) {
			continue
		}
		visit(n)
	}
	for _, cyc := range cycles {
		var first *Named
		dup := false
		for _, m := range cyc {
			o := m.origin()
			if reported[o] {
				dup = true
			}
			if o.Obj.Pkg != c.pkg || o.Obj.Decl == nil {
				continue
			}
			if first == nil || o.Obj.file.index < first.Obj.file.index ||
				o.Obj.file.index == first.Obj.file.index && o.Obj.Pos < first.Obj.Pos {
				first = o
			}
		}
		for _, m := range cyc {
			reported[m.origin()] = true
		}
		if dup || first == nil {
			continue
		}
		c.cur = first.Obj.file
		c.errorf(first.Obj.Decl.Name, CodeType, "struct %s contains itself by value; its size would be infinite (types.md, Structs 4)", first.Obj.Name)
	}
}

// checkMachine checks `type X machine[S, E] { initial v; step f }`: v is a
// value of S, checked in a closed context like a package var; f names a
// package function from S and E to Decision[S, O] with any rows
// (runtime.md, Machines and actors 1).
func (c *checker) checkMachine(tn *TypeName) {
	c.cur = tn.file
	spec := tn.Decl.Spec.(*syntax.MachineType)
	n := tn.Type.(*Named)
	m, ok := n.under.(*MachineType)
	if !ok {
		return
	}
	state, signal := m.State, m.Signal
	for _, part := range []struct {
		t Type
		x syntax.Expr
		w string
	}{{state, spec.State, "state"}, {signal, spec.Signal, "signal"}} {
		if !isInvalid(part.t) && asUnion(part.t) == nil {
			c.errorf(part.x, CodeType, "a machine's %s type must be a union, not %s", part.w, part.t)
		}
	}
	valid := !isInvalid(state) && !isInvalid(signal) && asUnion(state) != nil && asUnion(signal) != nil
	if spec.Initial == nil || spec.Step == nil {
		c.errorf(tn.Decl.Name, CodeType, "machine %s declares both initial and step (runtime.md, Machines and actors 1)", tn.Name)
	}
	if spec.Initial != nil {
		t := &target{name: "the initial value of " + tn.Name, declErrors: newRow(), declUses: newRow(), resultKnown: true}
		prev := c.fn
		c.fn = &funcCtx{t: t, scope: newScope(c.pkg.Scope)}
		var hint Type
		if valid {
			hint = state
		}
		op := c.valueExpr(spec.Initial, hint)
		if valid {
			c.assign(&op, state, "machine "+tn.Name+"'s initial value")
		}
		c.fn = prev
	}
	if spec.Step == nil {
		return
	}
	id, ok := spec.Step.(*syntax.Ident)
	if !ok {
		c.errorf(spec.Step, CodeType, "a machine's step names a function of this package (runtime.md, Machines and actors 1)")
		return
	}
	o := c.pkg.Scope.names[id.Name]
	if o == nil {
		c.errorf(id, CodeUnknown, "unknown function %s", id.Name)
		return
	}
	f, ok := o.(*Func)
	if !ok {
		c.errorf(id, CodeType, "%s is not a function; a machine's step names a function (runtime.md, Machines and actors 1)", id.Name)
		return
	}
	f.called = true
	c.pkg.Info.Uses[id] = f
	if !valid || f.Sig == nil {
		return
	}
	sig := f.Sig
	decision := preludeType("Decision")
	want := "func(" + state.String() + ", " + signal.String() + ") Decision[" + state.String() + ", O]"
	bad := len(sig.TParams) > 0 || sig.Variadic || len(sig.Params) != 2 || len(sig.Results) != 1
	if !bad {
		bad = !c.identical(sig.Params[0].Type, state) || !c.identical(sig.Params[1].Type, signal)
	}
	if !bad {
		r, ok := sig.Results[0].(*Named)
		bad = !ok || decision == nil || r.origin() != decision || len(r.TArgs) == 0 || !c.identical(r.TArgs[0], state)
	}
	if bad {
		c.errorf(id, CodeType, "the step %s of machine %s must have the type %s, with any error and capability rows (runtime.md, Machines and actors 1)", id.Name, tn.Name, want)
	}
}

// checkEntry checks main (rows.md, Capability rows 4; README Gaps 4 and 5):
// no parameters, no type parameters, and no value result; an error row is
// allowed. Its capability row is not restricted at check time (Q4). A call
// of main is refused where it is checked (S113).
func (c *checker) checkEntry() {
	f, ok := c.pkg.Scope.names["main"].(*Func)
	if !ok || f.Recv != nil {
		return
	}
	c.cur = f.file
	d := f.Decl
	if d.TypeParams != nil && len(d.TypeParams.List) > 0 {
		c.errorf(d.TypeParams, CodeEntry, "main takes no type parameters; the runtime calls it (rows.md, Capability rows 4)")
	}
	if d.Type.Params != nil && len(d.Type.Params.List) > 0 {
		c.errorf(d.Type.Params, CodeEntry, "main takes no parameters; the runtime calls it (rows.md, Capability rows 4)")
	}
	if d.Type.Results != nil {
		for _, r := range d.Type.Results.List {
			if _, isRow := r.Type.(*syntax.ErrorRow); isRow {
				continue
			}
			c.errorf(r.Type, CodeEntry, "main returns no value; only an error row is allowed, as Go's main returns nothing (rows.md, Capability rows 4; README Gap 4)")
			break
		}
	}
}

// checkErrorMethods reports a method named Error on an error type: the
// emitter generates it from the fields (types.md, Methods 5).
func (c *checker) checkErrorMethods() {
	for _, f := range c.funcs {
		if f.Recv == nil || f.Name != "Error" {
			continue
		}
		if _, ok := c.errorTypes[f.Recv.origin()]; !ok {
			continue
		}
		c.cur = f.file
		c.errorf(f.Decl.Name, CodeType, "%s is an error type, so it declares no Error method: its text comes from its fields (types.md, Methods 5)", f.Recv.Obj.Name)
	}
}
