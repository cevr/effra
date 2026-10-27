package check

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"ergo.local/prototype/internal/sketch/syntax"
)

// run checks the package: collect declarations, resolve types lazily, check
// signatures, check bodies one strongly connected component at a time, then
// the package-level rules that need every body (Go types2's phase order).
func (c *checker) run(files []*syntax.File) *Package {
	for i, f := range files {
		c.files = append(c.files, &fileInfo{syntax: f, index: i, scope: newScope(nil)})
	}
	c.pkg.files = c.files
	if len(files) > 0 {
		c.pkg.Name = files[0].Name.Name
	}
	c.pkg.qualify = true
	c.collect()
	c.solveDeclKinds()
	for _, tn := range c.pkg.Types {
		c.cur = tn.file
		c.resolveNamed(tn.Type.(*Named))
	}
	// Expanding type instantiations are rejected before any walk expands
	// instances (round-2 M1, M5).
	c.checkInstantiationCycles()
	c.collectMethods()
	for _, f := range c.funcs {
		c.cur = f.file
		c.funcSig(f)
	}
	// Expanding instantiations in signatures, a method's receiver
	// parameters included, are rejected before any row is inferred
	// (round-4 M1).
	c.checkInstantiationCycles()
	c.collectBodyErrorRows()
	c.checkEntry()
	// Bodies converge first: a machine's initial value and the named wires
	// are closed contexts that read the converged inferred rows (M2).
	c.checkBodies()
	c.checkTypeDecls()
	c.checkWires()
	c.checkWireCycles()
	c.checkErrorMethods()
	c.checkUnusedImports()
	c.pkg.ErrorTypes = make(map[*Named]bool, len(c.errorTypes))
	for n := range c.errorTypes {
		c.pkg.ErrorTypes[n] = true
	}
	c.cur = nil
	return c.pkg
}

// collectBodyErrorRows records every struct type that an error row of the
// package names, anywhere: signatures, bodies, function literals, package
// var initializers, machine initial values and wire providers, before any
// body is checked, so whether a type is an error type (types.md, Error types
// 1) does not depend on declaration order (M3). It reads each element's
// head type name syntactically (`Fault`, `pkg.Fault`, `Fault[T]`), so an
// element whose type arguments name a body's type parameter still counts.
// It reports nothing; the checks of the rows report.
func (c *checker) collectBodyErrorRows() {
	for _, fi := range c.files {
		c.cur = fi
		syntax.Inspect(fi.syntax, func(n syntax.Node) bool {
			if er, ok := n.(*syntax.ErrorRow); ok {
				for _, x := range er.Types {
					if n := c.errorRowHead(x); n != nil {
						if _, seen := c.errorTypes[n]; !seen {
							c.errorTypes[n] = x.Pos()
						}
					}
				}
			}
			return true
		})
	}
}

// errorRowHead returns the origin struct type an error-row element names by
// its head, or nil.
func (c *checker) errorRowHead(x syntax.Expr) *Named {
	if ix, ok := x.(*syntax.IndexExpr); ok {
		x = ix.X
	}
	tn, n := c.namedHead(x)
	if n == nil || tn.Decl == nil {
		return nil
	}
	if _, isStruct := tn.Decl.Spec.(*syntax.StructType); !isStruct {
		return nil
	}
	return n.origin()
}

// namedHead returns the declared type a name or a qualified name denotes,
// without resolving it, or nil.
func (c *checker) namedHead(x syntax.Expr) (*TypeName, *Named) {
	var o Object
	switch x := x.(type) {
	case *syntax.Ident:
		o = c.lookupType(x.Name, nil)
	case *syntax.SelectorExpr:
		id, ok := x.X.(*syntax.Ident)
		if !ok {
			return nil, nil
		}
		if pn, ok := c.lookupType(id.Name, nil).(*PkgName); ok {
			o = pn.Pkg.Scope.names[x.Sel.Name]
		}
	}
	tn, ok := o.(*TypeName)
	if !ok {
		return nil, nil
	}
	n, ok := tn.Type.(*Named)
	if !ok {
		return nil, nil
	}
	return tn, n
}

// ---------------------------------------------------------------- collection

func (c *checker) collect() {
	for _, fi := range c.files {
		c.cur = fi
		f := fi.syntax
		if c.pkg.Name != "" && f.Name.Name != c.pkg.Name {
			c.errorf(f.Name, CodeType, "package %s; expected package %s", f.Name.Name, c.pkg.Name)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *syntax.ImportDecl:
				for _, spec := range d.Specs {
					c.importSpec(fi, spec)
				}
			case *syntax.TypeDecl:
				tn := &TypeName{Name: d.Name.Name, Pkg: c.pkg, Pos: d.Name.Pos(), Decl: d, file: fi, id: newID()}
				tn.Type = &Named{Obj: tn}
				if c.declare(d.Name, tn) {
					c.pkg.Types = append(c.pkg.Types, tn)
				}
			case *syntax.FuncDecl:
				fn := &Func{Name: d.Name.Name, Pkg: c.pkg, Pos: d.Name.Pos(), Decl: d, file: fi}
				c.funcs = append(c.funcs, fn)
				if d.Recv == nil {
					if c.declare(d.Name, fn) {
						c.pkg.Funcs = append(c.pkg.Funcs, fn)
					}
				}
			case *syntax.VarDecl:
				for _, spec := range d.Specs {
					for i, name := range spec.Names {
						v := &Var{Name: name.Name, Pos: name.Pos(), pkg: true, used: true}
						c.declare(name, v)
						c.vars = append(c.vars, &pkgVar{obj: v, spec: spec, idx: i, file: fi})
					}
				}
			case *syntax.WireDecl:
				w := &Wire{Name: d.Name.Name, Pos: d.Name.Pos(), Decl: d, file: fi, pkg: c.pkg}
				if _, dup := c.pkg.Wires[w.Name]; dup {
					c.errorf(d.Name, CodeType, "wire %s is declared twice", w.Name)
					continue
				}
				c.pkg.Wires[w.Name] = w
			}
		}
	}
	// The export guard: the step of an exported machine type spells its
	// rows even when it is unexported (rows-and-constraints §2).
	for _, tn := range c.pkg.Types {
		m, ok := tn.Decl.Spec.(*syntax.MachineType)
		if !ok || !isExported(tn.Name) {
			continue
		}
		if id, ok := m.Step.(*syntax.Ident); ok {
			if f, ok := c.pkg.Scope.names[id.Name].(*Func); ok {
				c.guarded[f] = true
			}
		}
	}
}

// declare enters a package-level name; a second declaration of one name is
// S108 (a type and a function share the package's namespace).
func (c *checker) declare(id *syntax.Ident, o Object) bool {
	if id.Name == "_" {
		return false
	}
	if prev, ok := c.pkg.Scope.names[id.Name]; ok {
		what := "declared"
		if _, isType := prev.(*TypeName); isType {
			what = "declared as a type"
		}
		c.errorf(id, CodeType, "%s is already %s in this package", id.Name, what)
		return false
	}
	c.pkg.Scope.insert(id.Name, o)
	return true
}

func (c *checker) importSpec(fi *fileInfo, spec *syntax.ImportSpec) {
	path, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		c.errorf(spec.Path, CodeType, "malformed import path %s", spec.Path.Value)
		return
	}
	switch spec.Kind {
	case syntax.ImportGo:
		c.errorf(spec, CodeOutside, "import go %q is #202's (Go imports); it is not checked yet", path)
		c.bindBrokenImport(fi, spec, path)
		return
	case syntax.ImportDot:
		c.errorf(spec, CodeOutside, "a dot import is not checked yet (#206)")
		return
	}
	pkg, err := c.imp.Import(path)
	if err != nil || pkg == nil {
		c.errorf(spec.Path, CodeUnknown, "unknown package %q", path)
		c.bindBrokenImport(fi, spec, path)
		return
	}
	name := pkg.Name
	if spec.Kind == syntax.ImportNamed {
		name = spec.Name.Name
	}
	pn := &PkgName{Name: name, Pos: spec.Pos(), Pkg: pkg, spec: spec}
	if name == "_" {
		return
	}
	if _, dup := fi.scope.names[name]; dup {
		c.errorf(spec, CodeType, "%s is imported twice", name)
		return
	}
	fi.scope.insert(name, pn)
	fi.imports = append(fi.imports, pn)
}

// bindBrokenImport binds the name of a failed import to an empty package,
// so its uses add no diagnostic of their own (D3).
func (c *checker) bindBrokenImport(fi *fileInfo, spec *syntax.ImportSpec, path string) {
	name := path[strings.LastIndex(path, "/")+1:]
	if spec.Kind == syntax.ImportNamed && spec.Name != nil {
		name = spec.Name.Name
	}
	if name == "_" || name == "" {
		return
	}
	if _, dup := fi.scope.names[name]; dup {
		return
	}
	empty := &Package{Path: path, Name: name, Scope: newScope(nil)}
	fi.scope.insert(name, &PkgName{Name: name, Pos: spec.Pos(), Pkg: empty, spec: spec, used: true, broken: true})
}

func (c *checker) checkUnusedImports() {
	for _, fi := range c.files {
		c.cur = fi
		for _, pn := range fi.imports {
			if !pn.used {
				c.errorf(pn.spec, CodeUnused, "package %s is imported and not used", pn.Name)
			}
		}
	}
}

// ---------------------------------------------------------------- type declarations

// resolveNamed resolves a declared type's body once, with Go types2's object
// colouring: a declaration reached again while it resolves is a cycle.
func (c *checker) resolveNamed(n *Named) {
	if n.state != 0 || n.Obj.Decl == nil {
		return
	}
	n.state = 1
	saved := c.cur
	c.cur = n.Obj.file
	defer func() { c.cur = saved }()
	d := n.Obj.Decl
	ctx := &typeCtx{params: map[string]*TypeParam{}, decl: true}
	if d.TypeParams != nil {
		kinds, conflicts := c.bracketKinds(n.Obj)
		for _, field := range d.TypeParams.List {
			for _, name := range field.Names {
				tp := &TypeParam{Name: name.Name, Pos: name.Pos(), id: newID()}
				switch {
				case field.Type != nil && isErrorName(field.Type):
					tp.Kind = KindErrors
				case field.Type != nil:
					tp.Constraint = c.constraint(field.Type, ctx)
				case conflicts[name.Name]:
					c.errorf(name, CodeType, "type parameter %s is used both as a type and as a row (types.md, Generics 1)", name.Name)
					tp.conflict = true
				default:
					tp.Kind = kinds[name.Name]
				}
				if _, dup := ctx.params[name.Name]; dup {
					c.errorf(name, CodeType, "type parameter %s is declared twice", name.Name)
				}
				ctx.params[name.Name] = tp
				n.TParams = append(n.TParams, tp)
			}
		}
	}
	restore := c.monoOwner(n.TParams)
	defer restore()
	switch spec := d.Spec.(type) {
	case *syntax.StructType:
		ctx.field = true
		n.under = c.structType(spec, ctx)
	case *syntax.InterfaceType:
		if len(spec.Union) > 0 {
			n.under = c.unionType(n, spec, ctx)
		} else {
			n.under = c.interfaceType(spec, ctx)
		}
	case *syntax.MachineType:
		m := &MachineType{State: c.typExpr(spec.State, ctx), Signal: c.typExpr(spec.Signal, ctx)}
		for _, part := range []struct {
			t Type
			x syntax.Expr
			w string
		}{{m.State, spec.State, "state"}, {m.Signal, spec.Signal, "signal"}} {
			if !isInvalid(part.t) && asUnion(part.t) == nil {
				c.errorf(part.x, CodeType, "a machine's %s type must be a union, not %s", part.w, part.t)
			}
		}
		n.under = m
		c.machines = append(c.machines, n.Obj)
	default:
		n.under = invalid
	}
	n.state = 2
}

// bracketKinds returns the solved kind of each unconstrained bracket
// parameter of a type declaration and the parameters whose positions
// disagree (solveDeclKinds).
func (c *checker) bracketKinds(tn *TypeName) (map[string]Kind, map[string]bool) {
	kinds := map[string]Kind{}
	i := 0
	ks := c.kinds.kinds[tn]
	for _, f := range tn.Decl.TypeParams.List {
		for _, n := range f.Names {
			if i < len(ks) {
				kinds[n.Name] = ks[i]
			}
			i++
		}
	}
	return kinds, c.kinds.conflicts[tn]
}

func isErrorName(x syntax.Expr) bool {
	id, ok := x.(*syntax.Ident)
	return ok && id.Name == "error"
}

func (c *checker) structType(spec *syntax.StructType, ctx *typeCtx) *Struct {
	s := &Struct{}
	seen := map[string]bool{}
	for _, field := range spec.Fields.List {
		t := c.typExpr(field.Type, ctx)
		for _, name := range field.Names {
			if seen[name.Name] && name.Name != "_" {
				c.errorf(name, CodeType, "field %s is declared twice", name.Name)
				continue
			}
			seen[name.Name] = true
			s.Fields = append(s.Fields, &Field{Name: name.Name, Type: t, Pos: name.Pos()})
		}
	}
	return s
}

func (c *checker) interfaceType(spec *syntax.InterfaceType, ctx *typeCtx) *Interface {
	it := &Interface{}
	seen := map[string]bool{}
	for _, m := range spec.Methods {
		ft, ok := m.Type.(*syntax.FuncType)
		if !ok || len(m.Names) == 0 {
			c.errorf(m, CodeType, "an interface lists method signatures")
			continue
		}
		name := m.Names[0]
		mctx := &typeCtx{params: ctx.params, method: true, decl: ctx.decl, reported: new(bool)}
		sig := c.signature(ft, mctx)
		if ft.Needs != nil {
			c.errorf(ft.Needs, CodeType, "an interface method writes no capability row: a provider's requirements are captured when it is built (types.md, Capabilities 4; README Gap 17)")
			sig.Uses = newRow()
		}
		if seen[name.Name] {
			c.errorf(name, CodeType, "method %s is declared twice", name.Name)
			continue
		}
		seen[name.Name] = true
		it.Methods = append(it.Methods, &Method{Name: name.Name, Sig: sig, Pos: name.Pos()})
	}
	return it
}

func (c *checker) unionType(n *Named, spec *syntax.InterfaceType, ctx *typeCtx) Type {
	u := &Union{}
	seen := map[string]bool{}
	for _, x := range spec.Union {
		t := c.typExpr(x, ctx)
		if isInvalid(t) {
			u.Bad = true
			continue
		}
		vn, ok := t.(*Named)
		if !ok || vn.origin().Obj.Pkg != c.pkg {
			c.errorf(x, CodeType, "a union variant must be a struct type declared in this package, not %s", t)
			u.Bad = true
			continue
		}
		// The variant's kind comes from its declaration, so a variant that is
		// still resolving (a struct whose field is this union) is accepted.
		if _, isStruct := vn.origin().Obj.Decl.Spec.(*syntax.StructType); !isStruct {
			c.errorf(x, CodeType, "a union variant must be a struct type, not %s", t)
			u.Bad = true
			continue
		}
		key := typeKey(t)
		if seen[key] {
			c.errorf(x, CodeType, "variant %s is listed twice", t)
			continue
		}
		seen[key] = true
		u.Variants = append(u.Variants, t)
	}
	return u
}

// ---------------------------------------------------------------- type expressions

// typeCtx is the context of a type expression: the type parameters in scope
// and the rules for unbound one-letter names (rows-and-constraints §3).
type typeCtx struct {
	params   map[string]*TypeParam // bracket, receiver and implicit parameters
	implicit map[string]*TypeParam // implicit parameters found by the prescan
	method   bool                  // a method or interface method: no type parameters of its own (D6)
	decl     bool                  // a type declaration: bracket parameters only
	field    bool                  // a struct field: *T is not a field type (types.md, Borrows 1)
	param    bool                  // a parameter or receiver: *T allowed
	reported *bool                 // shared by the copies of one method context: one diagnostic per method
	scope    *Scope
}

// isParamName reports whether name has the shape of an implicit type
// parameter: one upper-case letter optionally followed by digits.
func isParamName(name string) bool {
	r, size := utf8.DecodeRuneInString(name)
	if !unicode.IsUpper(r) {
		return false
	}
	for _, d := range name[size:] {
		if d < '0' || d > '9' {
			return false
		}
	}
	return true
}

func (c *checker) lookupType(name string, ctx *typeCtx) Object {
	if ctx != nil {
		if tp, ok := ctx.params[name]; ok {
			return &TypeName{Name: name, Type: tp, Pkg: c.pkg}
		}
	}
	scope := c.pkg.Scope
	if ctx != nil && ctx.scope != nil {
		scope = ctx.scope
	}
	if o, _ := scope.lookup(name); o != nil {
		return o
	}
	if c.cur != nil {
		if o, ok := c.cur.scope.names[name]; ok {
			return o
		}
	}
	if ctx != nil {
		if tp, ok := ctx.implicit[name]; ok {
			return &TypeName{Name: name, Type: tp, Pkg: c.pkg}
		}
	}
	return nil
}

// typExpr resolves a type expression. An invalid type is reported once and
// suppresses every diagnostic that would follow from it (D3).
func (c *checker) typExpr(x syntax.Expr, ctx *typeCtx) Type {
	t := c.typExpr0(x, ctx)
	if n, ok := t.(*Named); ok && n.Orig == nil && len(n.TParams) > 0 {
		c.errorf(x, CodeType, "generic type %s needs type arguments", n)
		return invalid
	}
	if tp, ok := t.(*TypeParam); ok && tp.conflict {
		return invalid
	}
	if tp, ok := t.(*TypeParam); ok && tp.Kind != KindType {
		c.errorf(x, CodeType, "%s is %s parameter, not a type", tp.Name, tp.Kind)
		return invalid
	}
	return t
}

func (c *checker) typExpr0(x syntax.Expr, ctx *typeCtx) Type {
	switch x := x.(type) {
	case *syntax.Ident:
		return c.typeName(x, ctx)
	case *syntax.SelectorExpr:
		return c.qualifiedType(x)
	case *syntax.ParenExpr:
		return c.typExpr0(x.X, ctx)
	case *syntax.IndexExpr:
		base := c.typExpr0(x.X, ctx)
		if isInvalid(base) {
			return invalid
		}
		n, ok := base.(*Named)
		if !ok || n.Orig != nil || len(n.TParams) == 0 {
			c.errorf(x, CodeType, "%s is not a generic type", base)
			return invalid
		}
		return c.instantiateExprs(n, x.Indices, x, ctx)
	case *syntax.ArrayType:
		elem := c.typExpr(x.Elt, ctx)
		if x.Len == nil {
			return &Slice{Elem: elem}
		}
		if _, ok := x.Len.(*syntax.Ellipsis); ok {
			c.errorf(x, CodeType, "[...]T is the type of a composite literal only")
			return invalid
		}
		n, ok := c.constLen(x.Len)
		if !ok {
			return invalid
		}
		return &Array{Len: n, Elem: elem}
	case *syntax.MapType:
		k := c.typExpr(x.Key, ctx)
		v := c.typExpr(x.Value, ctx)
		if !isInvalid(k) && !comparable(k) {
			c.errorf(x.Key, CodeType, "map key type %s is not comparable", k)
			return invalid
		}
		return &Map{Key: k, Elem: v}
	case *syntax.ChanType:
		return &Chan{Dir: x.Dir, Elem: c.typExpr(x.Value, ctx)}
	case *syntax.StarExpr:
		if ctx == nil || !ctx.param {
			c.errorf(x, CodeType, "a borrow *T is only a parameter or receiver type (types.md, Borrows 1)")
			return invalid
		}
		inner := *ctx
		inner.param = false
		return &Pointer{Elem: c.typExpr(x.X, &inner)}
	case *syntax.FuncType:
		inner := typeCtx{}
		if ctx != nil {
			inner = *ctx
		}
		inner.field, inner.param = false, false
		return c.signature(x, &inner)
	case *syntax.StructType:
		inner := typeCtx{field: true}
		if ctx != nil {
			inner = *ctx
			inner.field = true
		}
		return c.structType(x, &inner)
	case *syntax.InterfaceType:
		if len(x.Union) > 0 {
			c.errorf(x, CodeType, "a union is a declared type")
			return invalid
		}
		return c.interfaceType(x, ctx)
	case *syntax.Ellipsis:
		c.errorf(x, CodeType, "...T is the type of a final parameter only")
		return invalid
	case *syntax.ErrorRow:
		c.errorf(x, CodeType, "an error row appears only in a result list")
		return invalid
	}
	c.errorf(x, CodeType, "expected a type")
	return invalid
}

func (c *checker) typeName(x *syntax.Ident, ctx *typeCtx) Type {
	o := c.lookupType(x.Name, ctx)
	if o == nil {
		if ctx != nil && ctx.method && isParamName(x.Name) {
			if ctx.reported == nil || !*ctx.reported {
				if ctx.reported != nil {
					*ctx.reported = true
				}
				c.errorf(x, CodeType, "%s would be a type parameter of a method; methods take no type parameters of their own (D6; Go proposal 43651)", x.Name)
			}
			return invalid
		}
		c.errorf(x, CodeUnknown, "unknown type %s", x.Name)
		return invalid
	}
	c.recordUse(x, o)
	switch o := o.(type) {
	case *TypeName:
		if o == universeOnce.errorName {
			c.errorf(x, CodeType, "error is only the head of an error row or a constraint")
			return invalid
		}
		if n, ok := o.Type.(*Named); ok {
			c.resolveNamed(n)
		}
		return o.Type
	case *capBinding:
		return o.tn.Type
	case *PkgName:
		c.errorf(x, CodeType, "package %s is not a type", x.Name)
		return invalid
	}
	c.errorf(x, CodeType, "%s is not a type", x.Name)
	return invalid
}

func (c *checker) qualifiedType(x *syntax.SelectorExpr) Type {
	id, ok := x.X.(*syntax.Ident)
	if !ok {
		c.errorf(x, CodeType, "expected a type")
		return invalid
	}
	o := c.lookupType(id.Name, nil)
	pn, ok := o.(*PkgName)
	if !ok {
		if o == nil {
			c.errorf(id, CodeUnknown, "unknown package %s", id.Name)
		} else {
			c.errorf(x, CodeType, "%s is not a package", id.Name)
		}
		return invalid
	}
	pn.used = true
	if pn.broken {
		return invalid
	}
	m, ok := pn.Pkg.Scope.names[x.Sel.Name]
	if !ok || !isExported(x.Sel.Name) {
		c.errorf(x.Sel, CodeUnknown, "%s.%s is not an exported declaration of package %s", id.Name, x.Sel.Name, pn.Pkg.Path)
		return invalid
	}
	tn, ok := m.(*TypeName)
	if !ok {
		c.errorf(x, CodeType, "%s.%s is not a type", id.Name, x.Sel.Name)
		return invalid
	}
	return tn.Type
}

func (c *checker) recordUse(id *syntax.Ident, o Object) {
	if c.silent == 0 {
		c.pkg.Info.Uses[id] = o
	}
}

// constraint resolves a bracket constraint: any, comparable, an interface
// with methods (types.md, Generics 2).
func (c *checker) constraint(x syntax.Expr, ctx *typeCtx) Type {
	t := c.typExpr(x, ctx)
	if isInvalid(t) {
		return nil
	}
	it := asInterface(t)
	if it == nil {
		c.errorf(x, CodeType, "a constraint is an interface, not %s", t)
		return nil
	}
	if !it.Comparable && len(it.Methods) == 0 {
		return nil
	}
	return t
}

// instantiateExprs instantiates a generic type with written type arguments.
// A row-kinded parameter takes an error type, a capability or a row
// parameter, as a row of one element.
func (c *checker) instantiateExprs(n *Named, args []syntax.Expr, at syntax.Node, ctx *typeCtx) Type {
	c.resolveNamed(n)
	if len(args) != len(n.TParams) {
		c.errorf(at, CodeType, "%s takes %d type arguments, not %d", n.Obj.Name, len(n.TParams), len(args))
		return invalid
	}
	targs := make([]Type, len(args))
	for i, a := range args {
		tp := n.TParams[i]
		if tp.Kind != KindType {
			row := c.rowExpr([]syntax.Expr{a}, tp.Kind, ctx)
			if row == nil {
				return invalid
			}
			targs[i] = &RowArg{Row: row}
			continue
		}
		t := c.typExpr(a, ctx)
		if isInvalid(t) {
			return invalid
		}
		targs[i] = t
	}
	c.recordInstantiation(n.Obj.Name, n.TParams, targs, at)
	inst := c.instantiate(n, targs)
	for i, tp := range n.TParams {
		if tp.Kind == KindType && tp.Constraint != nil && n.state == 2 {
			cons := c.subst(tp.Constraint, paramMap(n.TParams, targs))
			if why := c.satisfies(targs[i], cons); why != "" {
				c.errorf(args[i], CodeType, "%s does not satisfy %s: %s", targs[i], cons, why)
				return invalid
			}
		}
	}
	return inst
}

// instantiate returns the instance of a generic named type. Its body is
// substituted lazily, so a recursive generic type terminates.
func (c *checker) instantiate(orig *Named, targs []Type) *Named {
	inst := &Named{Obj: orig.Obj, Orig: orig, TArgs: targs, state: 2}
	key := typeKey(inst)
	if prev, ok := c.instance[key]; ok {
		return prev
	}
	inst.ctx = c
	c.instance[key] = inst
	c.instancesMade++
	return inst
}

func (n *Named) instanceUnder(inst *Named) Type {
	if n.under == nil || inst.ctx == nil {
		return invalid
	}
	m := map[*TypeParam]Type{}
	for i, tp := range n.TParams {
		if i < len(inst.TArgs) {
			m[tp] = inst.TArgs[i]
		}
	}
	return inst.ctx.subst(n.under, m)
}

// constLen evaluates an array length.
func (c *checker) constLen(x syntax.Expr) (int64, bool) {
	lit, ok := x.(*syntax.BasicLit)
	if !ok || lit.Kind != syntax.INT {
		c.errorf(x, CodeType, "an array length is an integer literal")
		return 0, false
	}
	n, err := strconv.ParseInt(lit.Value, 10, 64)
	if err != nil || n < 0 {
		c.errorf(x, CodeType, "invalid array length %s", lit.Value)
		return 0, false
	}
	// The length is a constant expression: Info records it untyped, as
	// go/types does (D1).
	c.record(x, operand{mode: modeConst, typ: Typ[UntypedInt]})
	return n, true
}

// ---------------------------------------------------------------- rows in signatures

// rowExpr resolves the elements of an error row or a capability row.
func (c *checker) rowExpr(xs []syntax.Expr, kind Kind, ctx *typeCtx) *Row {
	row := newRow()
	ok := true
	for _, x := range xs {
		t := c.rowElem(x, kind, ctx)
		if t == nil {
			ok = false
			continue
		}
		row = row.with(t)
	}
	if !ok {
		return nil
	}
	return row
}

func (c *checker) rowElem(x syntax.Expr, kind Kind, ctx *typeCtx) Type {
	var t Type
	switch x := x.(type) {
	case *syntax.Ident:
		o := c.lookupType(x.Name, ctx)
		if o == nil {
			if ctx != nil && ctx.method && isParamName(x.Name) {
				if ctx.reported == nil || !*ctx.reported {
					if ctx.reported != nil {
						*ctx.reported = true
					}
					c.errorf(x, CodeType, "%s would be a row parameter of a method; methods take no type parameters of their own (D6)", x.Name)
				}
				return nil
			}
			c.errorf(x, CodeUnknown, "unknown %s %s", kindNoun(kind), x.Name)
			return nil
		}
		c.recordUse(x, o)
		switch o := o.(type) {
		case *TypeName:
			if n, ok := o.Type.(*Named); ok {
				c.resolveNamed(n)
			}
			t = o.Type
		case *capBinding:
			t = o.tn.Type
		default:
			c.errorf(x, CodeUnknown, "%s is not %s", x.Name, kindNoun(kind))
			return nil
		}
	default:
		t = c.typExpr0(x, ctx)
		if isInvalid(t) {
			return nil
		}
	}
	if tp, ok := t.(*TypeParam); ok {
		if tp.conflict {
			return nil
		}
		if tp.Kind != kind {
			c.errorf(x, CodeType, "%s is %s parameter, used here as %s", tp.Name, tp.Kind, kind)
			return nil
		}
		return tp
	}
	if ra, ok := t.(*RowArg); ok {
		return ra
	}
	switch kind {
	case KindErrors:
		if n, ok := t.(*Named); ok && n.Orig == nil && len(n.TParams) > 0 {
			c.errorf(x, CodeType, "generic type %s needs type arguments", n)
			return nil
		}
		if !isStructNamed(t) {
			c.errorf(x, CodeUnknown, "%s is not an error type: an error type is a named struct type (types.md, Error types 1)", t)
			return nil
		}
		n := t.(*Named)
		if _, seen := c.errorTypes[n.origin()]; !seen {
			c.errorTypes[n.origin()] = x.Pos()
		}
	case KindUses:
		if !isCapability(t) {
			c.errorf(x, CodeUnknown, "%s is not a capability: a capability is a named interface with methods (rows.md, Capability rows 3)", t)
			return nil
		}
	}
	return t
}

func kindNoun(k Kind) string {
	if k == KindErrors {
		return "error type"
	}
	if k == KindUses {
		return "capability"
	}
	return "type"
}

// ---------------------------------------------------------------- signatures

// walkTypePositions calls f for every name in a type position of x, with
// the kind its position gives (rows-and-constraints §3).
func walkTypePositions(x syntax.Node, k Kind, f func(*syntax.Ident, Kind)) {
	typeWalker{f: f}.walk(x, k)
}

// typeWalker visits the names in type positions with the kind of the
// position. slots, when set, gives the parameter kinds of a generic type
// named as the head of an index expression, so `Task[T, E]` puts E in a row
// position (D6; M6).
type typeWalker struct {
	f     func(*syntax.Ident, Kind)
	slots func(head syntax.Expr) []Kind
	// index, when set, may take over an index expression (returning true)
	// before the default walk.
	index func(ix *syntax.IndexExpr) bool
}

func (w typeWalker) walk(x syntax.Node, k Kind) {
	switch x := x.(type) {
	case nil:
	case *syntax.Ident:
		w.f(x, k)
	case *syntax.SelectorExpr:
	case *syntax.ParenExpr:
		w.walk(x.X, k)
	case *syntax.BinaryExpr:
		// A row argument written as a union, E | Expired.
		w.walk(x.X, k)
		w.walk(x.Y, k)
	case *syntax.IndexExpr:
		if w.index != nil && w.index(x) {
			return
		}
		w.walk(x.X, KindType)
		var kinds []Kind
		if w.slots != nil {
			kinds = w.slots(x.X)
		}
		for i, idx := range x.Indices {
			kind := KindType
			if i < len(kinds) {
				kind = kinds[i]
			}
			w.walk(idx, kind)
		}
	case *syntax.ArrayType:
		w.walk(x.Elt, KindType)
	case *syntax.MapType:
		w.walk(x.Key, KindType)
		w.walk(x.Value, KindType)
	case *syntax.ChanType:
		w.walk(x.Value, KindType)
	case *syntax.StarExpr:
		w.walk(x.X, KindType)
	case *syntax.Ellipsis:
		w.walk(x.Elt, KindType)
	case *syntax.FuncType:
		w.fields(x.Params)
		w.fields(x.Results)
		if x.Needs != nil {
			for _, n := range x.Needs.Names {
				w.walk(n, KindUses)
			}
		}
	case *syntax.ErrorRow:
		for _, t := range x.Types {
			w.walk(t, KindErrors)
		}
	case *syntax.StructType:
		w.fields(x.Fields)
	case *syntax.InterfaceType:
		for _, m := range x.Methods {
			w.walk(m.Type, KindType)
		}
		for _, u := range x.Union {
			w.walk(u, KindType)
		}
	case *syntax.MachineType:
		w.walk(x.State, KindType)
		w.walk(x.Signal, KindType)
	}
}

func (w typeWalker) fields(l *syntax.FieldList) {
	if l == nil {
		return
	}
	for _, field := range l.List {
		w.walk(field.Type, KindType)
	}
}

// slotKinds returns the parameter kinds of the generic type a head names,
// or nil when the head is not a generic type or is still resolving.
func (c *checker) slotKinds(head syntax.Expr) []Kind {
	tn, n := c.namedHead(head)
	if n == nil {
		return nil
	}
	if ks, ok := c.kinds.kinds[tn]; ok {
		// A local declaration's kinds are solved before any resolves, so
		// a recursive head has them too (round-2 M4).
		return ks
	}
	if n.state == 0 {
		c.resolveNamed(n)
	}
	if n.state != 2 {
		return nil
	}
	kinds := make([]Kind, len(n.TParams))
	for i, tp := range n.TParams {
		kinds[i] = tp.Kind
	}
	return kinds
}

// prescanImplicit creates the implicit type parameters of a function
// signature: every otherwise unbound one-letter name, kinded by position. A
// name used in two kinds is S108 (D6).
func (c *checker) prescanImplicit(ft *syntax.FuncType, ctx *typeCtx) {
	type seen struct {
		kind Kind
		at   *syntax.Ident
	}
	found := map[string]*seen{}
	var order []string
	conflict := map[string]bool{}
	typeWalker{slots: c.slotKinds, f: func(id *syntax.Ident, k Kind) {
		if !isParamName(id.Name) || conflict[id.Name] {
			return
		}
		if _, bound := ctx.params[id.Name]; bound {
			return
		}
		if o, _ := c.pkg.Scope.lookup(id.Name); o != nil {
			return
		}
		if c.cur != nil {
			if _, ok := c.cur.scope.names[id.Name]; ok {
				return
			}
		}
		if s, ok := found[id.Name]; ok {
			if s.kind != k {
				conflict[id.Name] = true
				c.errorf(id, CodeType, "%s is used as %s and as %s; a parameter has one kind (types.md, Generics 1)", id.Name, s.kind, k)
			}
			return
		}
		found[id.Name] = &seen{kind: k, at: id}
		order = append(order, id.Name)
	}}.walk(ft, KindType)
	if ctx.implicit == nil {
		ctx.implicit = map[string]*TypeParam{}
	}
	for _, name := range order {
		if conflict[name] {
			continue
		}
		s := found[name]
		ctx.implicit[name] = &TypeParam{Name: name, Kind: s.kind, Pos: s.at.Pos(), Implicit: true, id: newID()}
	}
	for name := range conflict {
		// A conflicting name resolves to nothing further: no cascade.
		ctx.implicit[name] = &TypeParam{Name: name, Implicit: true, id: newID(), conflict: true}
	}
}

// signature resolves a function type. Parameters, results, the error row and
// the capability row.
func (c *checker) signature(ft *syntax.FuncType, ctx *typeCtx) *Signature {
	sig := &Signature{Errors: newRow(), Uses: newRow()}
	pctx := *ctx
	pctx.param, pctx.field = true, false
	if ft.Params != nil {
		for i, field := range ft.Params.List {
			var t Type
			if el, ok := field.Type.(*syntax.Ellipsis); ok {
				last := i == len(ft.Params.List)-1 && len(field.Names) <= 1
				if !last || el.Elt == nil {
					c.errorf(field.Type, CodeType, "only the final parameter may be variadic")
					t = invalid
				} else {
					t = &Slice{Elem: c.typExpr(el.Elt, &pctx)}
					sig.Variadic = true
				}
			} else {
				t = c.typExpr(field.Type, &pctx)
			}
			if len(field.Names) == 0 {
				sig.Params = append(sig.Params, &Var{Name: "_", Type: t, Pos: field.Pos(), param: true})
			}
			for _, name := range field.Names {
				sig.Params = append(sig.Params, &Var{Name: name.Name, Type: t, Pos: name.Pos(), param: true})
			}
		}
	}
	rctx := *ctx
	rctx.param, rctx.field = false, false
	if ft.Results != nil {
		for _, field := range ft.Results.List {
			if er, ok := field.Type.(*syntax.ErrorRow); ok {
				sig.Fallible = true
				if row := c.rowExpr(er.Types, KindErrors, &rctx); row != nil {
					sig.Errors = row
				}
				continue
			}
			t := c.typExpr(field.Type, &rctx)
			for range max(1, len(field.Names)) {
				sig.Results = append(sig.Results, t)
			}
		}
	}
	if ft.Needs != nil {
		if row := c.rowExpr(ft.Needs.Names, KindUses, &rctx); row != nil {
			sig.Uses = row
		}
	}
	return sig
}

// funcSig resolves a function's or method's signature, its type parameters
// (bracket-listed first, then implicit ones) and its receiver.
func (c *checker) funcSig(f *Func) *Signature {
	if f.state == 2 || f.state == 1 {
		if f.Sig == nil {
			f.Sig = &Signature{Errors: newRow(), Uses: newRow()}
		}
		return f.Sig
	}
	f.state = 1
	saved := c.cur
	c.cur = f.file
	defer func() { c.cur = saved }()
	d := f.Decl
	ctx := &typeCtx{params: map[string]*TypeParam{}}
	var tparams []*TypeParam
	if d.Recv != nil {
		ctx.method = true
		ctx.reported = new(bool)
		c.bindReceiverParams(f, ctx)
	}
	// The bracket parameters are declared before any constraint resolves,
	// so the instantiations in the constraints and the signature are
	// attributed to them (round-4 M1).
	type pending struct {
		tp *TypeParam
		x  syntax.Expr
	}
	var constraints []pending
	if d.TypeParams != nil {
		for _, field := range d.TypeParams.List {
			for _, name := range field.Names {
				tp := &TypeParam{Name: name.Name, Pos: name.Pos(), id: newID()}
				switch {
				case field.Type != nil && isErrorName(field.Type):
					tp.Kind = KindErrors
				case field.Type != nil:
					constraints = append(constraints, pending{tp, field.Type})
				}
				if _, dup := ctx.params[name.Name]; dup {
					c.errorf(name, CodeType, "type parameter %s is declared twice", name.Name)
				}
				ctx.params[name.Name] = tp
				tparams = append(tparams, tp)
			}
		}
	}
	// A signature instantiates generic types with the declaration's
	// parameters, a method's receiver parameters included, as its body does:
	// they own those instantiations, so a growing one is an instantiation
	// cycle before rows are inferred (round-4 M1; go/types mono.go records
	// signature instances too).
	var owner []*TypeParam
	if f.Recv != nil {
		owner = append(owner, f.Recv.TParams...)
	}
	owner = append(owner, tparams...)
	restore := c.monoOwner(owner)
	for _, p := range constraints {
		p.tp.Constraint = c.constraint(p.x, ctx)
	}
	nbracket := len(tparams)
	var implicit []string
	if d.Recv == nil {
		c.prescanImplicit(d.Type, ctx)
		implicit = implicitOrder(d.Type, ctx)
		for _, name := range implicit {
			owner = append(owner, ctx.implicit[name])
		}
		c.mono.owner = owner
	}
	sig := c.signature(d.Type, ctx)
	restore()
	sig.NBracket = nbracket
	for _, name := range implicit {
		tparams = append(tparams, ctx.implicit[name])
	}
	sig.TParams = tparams
	f.Sig = sig
	f.state = 2
	c.inferFlags(f)
	return sig
}

// implicitOrder lists the implicit parameters in order of first occurrence.
func implicitOrder(ft *syntax.FuncType, ctx *typeCtx) []string {
	var out []string
	seen := map[string]bool{}
	walkTypePositions(ft, KindType, func(id *syntax.Ident, _ Kind) {
		if tp, ok := ctx.implicit[id.Name]; ok && !seen[id.Name] && !tp.conflict {
			seen[id.Name] = true
			out = append(out, id.Name)
		}
	})
	return out
}

// bindReceiverParams resolves a method's receiver and binds the type
// parameters its type arguments name.
func (c *checker) bindReceiverParams(f *Func, ctx *typeCtx) {
	if f.Recv == nil {
		return
	}
	if len(f.Recv.TParams) == 0 {
		return
	}
	field := f.Decl.Recv.List[0]
	rt := field.Type
	if s, ok := rt.(*syntax.StarExpr); ok {
		rt = s.X
	}
	ix, ok := rt.(*syntax.IndexExpr)
	if !ok {
		return
	}
	for i, a := range ix.Indices {
		id, ok := a.(*syntax.Ident)
		if !ok || i >= len(f.Recv.TParams) {
			c.errorf(a, CodeType, "a receiver's type arguments name its type parameters")
			continue
		}
		ctx.params[id.Name] = f.Recv.TParams[i]
	}
}

// collectMethods attaches every method to its receiver type: a struct type
// declared in this package (types.md, Methods 2).
func (c *checker) collectMethods() {
	for _, f := range c.funcs {
		d := f.Decl
		if d.Recv == nil {
			continue
		}
		c.cur = f.file
		field := d.Recv.List[0]
		rt := field.Type
		if s, ok := rt.(*syntax.StarExpr); ok {
			rt = s.X
			f.Ptr = true
		}
		if ix, ok := rt.(*syntax.IndexExpr); ok {
			rt = ix.X
		}
		id, ok := rt.(*syntax.Ident)
		if !ok {
			c.errorf(field.Type, CodeType, "a receiver names a struct type declared in this package")
			f.state = 2
			f.Sig = &Signature{Errors: newRow(), Uses: newRow()}
			continue
		}
		o := c.pkg.Scope.names[id.Name]
		tn, ok := o.(*TypeName)
		var n *Named
		if ok {
			n, _ = tn.Type.(*Named)
		}
		if n == nil || n.Obj.Pkg != c.pkg || asStruct(n) == nil {
			what := id.Name
			if o == nil {
				if u, _ := c.universe.lookup(id.Name); u != nil {
					what = id.Name + " (declared outside this package)"
				}
			}
			c.errorf(field.Type, CodeType, "the receiver %s is not a struct type declared in this package (types.md, Methods 2)", what)
			f.state = 2
			f.Sig = &Signature{Errors: newRow(), Uses: newRow()}
			continue
		}
		f.Recv = n
		if !n.addMethod(f) {
			c.errorf(d.Name, CodeType, "method %s.%s is declared twice", n.Obj.Name, f.Name)
			continue
		}
		if s := asStruct(n); s != nil {
			if fld, _ := s.field(f.Name); fld != nil {
				c.errorf(d.Name, CodeType, "%s has both a field and a method named %s", n.Obj.Name, f.Name)
			}
		}
	}
}

// inferFlags decides which rows of a function are inferred from its body
// (rows.md, Capability rows 6; rows-and-constraints §7 amendment 1): an
// unexported function with a body, when the row is unwritten. main, LawX,
// exported functions, methods, bodiless declarations and the step of an
// exported machine (the export guard) spell their rows.
func (c *checker) inferFlags(f *Func) {
	d := f.Decl
	if d.Recv != nil || d.Body == nil || isExported(f.Name) || f.Name == "main" || c.guarded[f] {
		return
	}
	hasErrRow := false
	if d.Type.Results != nil {
		for _, r := range d.Type.Results.List {
			if _, ok := r.Type.(*syntax.ErrorRow); ok {
				hasErrRow = true
			}
		}
	}
	f.inferErrors = !hasErrRow
	f.inferUses = d.Type.Needs == nil
}
