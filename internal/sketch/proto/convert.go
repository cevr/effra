package proto

import (
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
)

// codeOutside is the prototype's rule for a program outside the implemented
// subset (docs/spec/diagnostics.md, S109).
const codeOutside = "S109"

// FromSyntax converts a parsed file to the prototype AST. Every node the
// prototype models converts one to one; `else if` becomes an else block
// holding the if, and `a, b T` one parameter or field per name, which the
// prototype AST expresses exactly. Every other node is refused with S109 at
// its position, and its subtree is not converted. A file with refusals has
// no AST: the checker never sees a partial program.
func FromSyntax(f *syntax.File) (*File, []Diagnostic) {
	c := &converter{src: f}
	out := c.file(f)
	if len(c.diags) > 0 {
		return nil, c.diags
	}
	return out, nil
}

type converter struct {
	src   *syntax.File
	diags []Diagnostic
	// methods are the functions the bridge reads as methods of their first
	// parameter's type (#196, 196m: receivers are first parameters).
	methods     map[*syntax.FuncDecl]bool
	methodNames map[string]bool
}

func (c *converter) pos(p syntax.Pos) Pos {
	line, col := c.src.Position(p)
	return Pos{Line: line, Col: col}
}

func (c *converter) refuse(n syntax.Node, what string) {
	c.refuseAt(n.Pos(), what)
}

func (c *converter) refuseAt(p syntax.Pos, what string) {
	c.diags = append(c.diags, Diagnostic{Pos: c.pos(p), Code: codeOutside, Msg: what + " is outside the implemented subset"})
}

func (c *converter) file(f *syntax.File) *File {
	out := &File{Package: "main"}
	if f.Name != nil {
		out.Package = f.Name.Name
	}
	c.findMethods(f)
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *syntax.ImportDecl:
			for _, s := range d.Specs {
				if s.Kind != syntax.ImportPlain {
					c.refuseAt(s.NamePos, "a named, dot or go import")
					continue
				}
				out.Imports = append(out.Imports, strings.Trim(s.Path.Value, "\"`"))
			}
		case *syntax.TypeDecl:
			if t := c.typeDecl(d); t != nil {
				out.Types = append(out.Types, t)
			}
		case *syntax.FuncDecl:
			if fn := c.funcDecl(d); fn != nil {
				out.Funcs = append(out.Funcs, fn)
			}
		case *syntax.WireDecl:
			// The checker of #196 owns what a wire header means: provides
			// hides the unlisted entries, needs joins the with site's row.
			refused := false
			if d.Provides != nil {
				c.refuse(d.Provides, "a wire's provides clause (checked by #196)")
				refused = true
			}
			if d.Needs != nil {
				c.refuse(d.Needs, "a wire's needs clause (checked by #196)")
				refused = true
			}
			if refused {
				continue
			}
			out.Wires = append(out.Wires, &WireDecl{Pos: c.pos(d.Wire), Name: d.Name.Name, Entries: c.wireEntries(d.Body)})
		case *syntax.VarDecl:
			c.refuse(d, "a var declaration")
		default:
			c.refuse(d, "this declaration")
		}
	}
	return out
}

// findMethods picks the functions the prototype reads as methods: the
// first parameter's type is a record declared in the file, and the name is
// an operation of an operation type in the file or is never called by name
// (`f(x)`), so every use is `x.f(…)` (#196, 196m). The prototype has
// methods, not first-parameter dispatch.
func (c *converter) findMethods(f *syntax.File) {
	c.methods, c.methodNames = map[*syntax.FuncDecl]bool{}, map[string]bool{}
	records, ops, called := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range f.Decls {
		if t, ok := d.(*syntax.TypeDecl); ok {
			switch s := t.Spec.(type) {
			case *syntax.StructType:
				records[t.Name.Name] = true
			case *syntax.InterfaceType:
				for _, m := range s.Methods {
					ops[m.Names[0].Name] = true
				}
			}
		}
	}
	syntax.Inspect(f, func(n syntax.Node) bool {
		if call, ok := n.(*syntax.CallExpr); ok {
			if id, ok := call.Fun.(*syntax.Ident); ok {
				called[id.Name] = true
			}
		}
		return true
	})
	for _, d := range f.Decls {
		fn, ok := d.(*syntax.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || len(fn.Type.Params.List) == 0 {
			continue
		}
		first := fn.Type.Params.List[0]
		id, ok := first.Type.(*syntax.Ident)
		unnamed := len(first.Names) == 1 && first.Names[0].Name == "_"
		if !ok || !records[id.Name] && !unnamed || len(first.Names) > 1 {
			continue
		}
		if ops[fn.Name.Name] || !called[fn.Name.Name] {
			c.methods[fn] = true
			c.methodNames[fn.Name.Name] = true
		}
	}
}

// typeName converts a type that is a name or a qualified name.
func (c *converter) typeName(x syntax.Expr) (string, bool) {
	q, ok := c.qname(x)
	if !ok {
		c.refuse(x, "this type")
	}
	return q.String(), ok
}

// qname converts a name or a qualified name without refusing.
func (c *converter) qname(x syntax.Expr) (QName, bool) {
	switch x := x.(type) {
	case *syntax.Ident:
		return QName{Pos: c.pos(x.Pos()), Name: x.Name}, true
	case *syntax.SelectorExpr:
		if pkg, ok := x.X.(*syntax.Ident); ok {
			return QName{Pos: c.pos(pkg.Pos()), Pkg: pkg.Name, Name: x.Sel.Name}, true
		}
	}
	return QName{}, false
}

func (c *converter) qnames(list []syntax.Expr, what string) []QName {
	var out []QName
	for _, x := range list {
		q, ok := c.qname(x)
		if !ok {
			c.refuse(x, what)
			continue
		}
		out = append(out, q)
	}
	return out
}

func (c *converter) typeDecl(d *syntax.TypeDecl) *TypeDecl {
	if d.TypeParams != nil {
		c.refuse(d.TypeParams, "a type parameter list")
		return nil
	}
	out := &TypeDecl{Pos: c.pos(d.Type), Name: d.Name.Name}
	switch t := d.Spec.(type) {
	case *syntax.StructType:
		out.Kind = kindStruct
		out.Fields = c.params(t.Fields, "struct field")
	case *syntax.InterfaceType:
		if t.Union != nil {
			out.Kind = kindUnion
			for _, v := range t.Union {
				id, ok := v.(*syntax.Ident)
				if !ok {
					c.refuse(v, "this union variant")
					continue
				}
				out.Variants = append(out.Variants, id.Name)
			}
			return out
		}
		out.Kind = kindCapability
		for _, m := range t.Methods {
			s := &Sig{Pos: c.pos(m.Names[0].Pos()), Name: m.Names[0].Name}
			c.signature(s, m.Type.(*syntax.FuncType))
			out.Methods = append(out.Methods, s)
		}
	case *syntax.MachineType:
		c.refuse(t, "a machine type")
		return nil
	default:
		c.refuse(d.Spec, "this type declaration")
		return nil
	}
	return out
}

// params converts a parameter or field list whose entries all have names
// and name types; `a, b T` becomes one Param per name.
func (c *converter) params(l *syntax.FieldList, what string) []Param {
	var out []Param
	for _, f := range l.List {
		if len(f.Names) == 0 {
			c.refuse(f, "a "+what+" without a name")
			continue
		}
		if _, ok := f.Type.(*syntax.Ellipsis); ok {
			c.refuse(f.Type, "a variadic parameter")
			continue
		}
		typ, ok := c.typeName(f.Type)
		if !ok {
			continue
		}
		for _, n := range f.Names {
			out = append(out, Param{Pos: c.pos(n.Pos()), Name: n.Name, Type: typ})
		}
	}
	return out
}

// signature converts parameters, results and the needs clause into s. The
// prototype's results are nothing, T, error[E] or (T, error[E]).
func (c *converter) signature(s *Sig, t *syntax.FuncType) {
	s.Params = c.params(t.Params, "parameter")
	if r := t.Results; r != nil {
		switch {
		case len(r.List) == 1 && len(r.List[0].Names) == 0:
			if row, ok := r.List[0].Type.(*syntax.ErrorRow); ok {
				c.errorRow(s, row)
			} else if typ, ok := c.typeName(r.List[0].Type); ok {
				s.Result = typ
			}
		case len(r.List) == 2 && len(r.List[0].Names) == 0 && len(r.List[1].Names) == 0:
			row, ok := r.List[1].Type.(*syntax.ErrorRow)
			if !ok {
				c.refuse(r, "this result list")
				break
			}
			if typ, ok := c.typeName(r.List[0].Type); ok {
				s.Result = typ
			}
			c.errorRow(s, row)
		default:
			c.refuse(r, "this result list")
		}
	}
	if t.Needs != nil {
		if t.Needs.Lparen.IsValid() {
			c.refuse(t.Needs, "a parenthesised needs list")
		} else {
			s.Needs = c.qnames(t.Needs.Names, "this capability")
		}
	}
}

func (c *converter) errorRow(s *Sig, row *syntax.ErrorRow) {
	s.Fallible = true
	s.Errors = c.qnames(row.Types, "this error type")
}

func (c *converter) funcDecl(d *syntax.FuncDecl) *FuncDecl {
	if d.TypeParams != nil {
		c.refuse(d.TypeParams, "a type parameter list")
		return nil
	}
	if d.Body == nil {
		c.refuseAt(d.Func, "a bodiless declaration")
		return nil
	}
	out := &FuncDecl{Sig: Sig{Pos: c.pos(d.Func), Name: d.Name.Name}}
	if d.Recv != nil {
		f := d.Recv.List[0]
		id, ok := f.Type.(*syntax.Ident)
		if !ok {
			c.refuse(f.Type, "this receiver type")
		} else if len(f.Names) == 0 {
			out.Recv = &Param{Pos: c.pos(id.Pos()), Type: id.Name}
		} else {
			out.Recv = &Param{Pos: c.pos(f.Names[0].Pos()), Name: f.Names[0].Name, Type: id.Name}
		}
	}
	c.signature(&out.Sig, d.Type)
	if c.methods[d] && len(out.Sig.Params) > 0 {
		recv := out.Sig.Params[0]
		if recv.Name == "_" {
			recv.Name = ""
		}
		out.Recv = &recv
		out.Sig.Params = out.Sig.Params[1:]
	}
	out.Body = c.block(d.Body)
	return out
}

func (c *converter) wireEntries(b *syntax.WireBody) []WireEntry {
	var out []WireEntry
	for _, e := range b.Entries {
		switch {
		case e.Kind == syntax.WireEmbed:
			c.refuse(e, "an embedded wire")
		case e.Kind == syntax.WireReplace:
			c.refuseAt(e.Replace, "a replace entry")
		case len(e.Keys) > 0:
			c.refuse(e.Keys[0], "a wire entry with several capabilities")
		default:
			id, ok := e.Key.(*syntax.Ident)
			if !ok {
				c.refuse(e.Key, "a qualified wire key")
				continue
			}
			if v := c.expr(e.Value); v != nil {
				out = append(out, WireEntry{Pos: c.pos(id.Pos()), Cap: id.Name, Value: v})
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- statements

func (c *converter) block(b *syntax.BlockStmt) *Block {
	return &Block{Pos: c.pos(b.Lbrace), Stmts: c.stmts(b.List)}
}

func (c *converter) stmts(list []syntax.Stmt) []Stmt {
	var out []Stmt
	for _, s := range list {
		if v := c.stmt(s); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func (c *converter) stmt(s syntax.Stmt) Stmt {
	switch s := s.(type) {
	case *syntax.ExprStmt:
		if x := c.expr(s.X); x != nil {
			return &ExprStmt{Pos: c.pos(s.Pos()), X: x}
		}
	case *syntax.AssignStmt:
		if s.Tok != syntax.DEFINE {
			c.refuseAt(s.TokPos, "an assignment with "+s.Tok.String())
			return nil
		}
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
			c.refuse(s, "a multi-value :=")
			return nil
		}
		id := s.Lhs[0].(*syntax.Ident)
		if x := c.expr(s.Rhs[0]); x != nil {
			return &DefineStmt{Pos: c.pos(id.Pos()), Name: id.Name, Value: x}
		}
	case *syntax.ReturnStmt:
		switch len(s.Results) {
		case 0:
			return &ReturnStmt{Pos: c.pos(s.Return)}
		case 1:
			if x := c.expr(s.Results[0]); x != nil {
				return &ReturnStmt{Pos: c.pos(s.Return), Value: x}
			}
		default:
			c.refuse(s, "a return of several values")
		}
	case *syntax.IfStmt:
		return c.ifStmt(s)
	case *syntax.TypeSwitchStmt:
		return c.typeSwitch(s)
	case *syntax.DeclStmt:
		// `let x = e` (and `let mut x = e`) is the prototype's `x := e`.
		if sp := s.Decl.Specs[0]; len(s.Decl.Specs) == 1 && len(sp.Names) == 1 && sp.Type == nil && len(sp.Values) == 1 {
			if x := c.expr(sp.Values[0]); x != nil {
				return &DefineStmt{Pos: c.pos(sp.Names[0].Pos()), Name: sp.Names[0].Name, Value: x}
			}
			return nil
		}
		c.refuse(s, "a local var declaration")
	case *syntax.ForStmt, *syntax.RangeStmt:
		c.refuse(s, "a for loop")
	case *syntax.DeferStmt:
		c.refuse(s, "a defer statement")
	case *syntax.SwitchStmt:
		c.refuse(s, "an expression switch")
	case *syntax.SelectStmt:
		c.refuse(s, "a select statement")
	case *syntax.LabeledStmt:
		c.refuse(s, "a labelled statement")
	case *syntax.BranchStmt:
		c.refuse(s, "a "+s.Tok.String()+" statement")
	case *syntax.SendStmt:
		c.refuse(s, "a channel send")
	case *syntax.IncDecStmt:
		c.refuseAt(s.TokPos, "the "+s.Tok.String()+" statement")
	case *syntax.BlockStmt:
		c.refuse(s, "a bare block")
	case *syntax.EmptyStmt:
		return nil
	default:
		c.refuse(s, "this statement")
	}
	return nil
}

func (c *converter) ifStmt(s *syntax.IfStmt) Stmt {
	if s.Init != nil {
		c.refuse(s.Init, "an if statement's init statement")
		return nil
	}
	cond := c.expr(s.Cond)
	out := &IfStmt{Pos: c.pos(s.If), Cond: cond, Then: c.block(s.Body)}
	switch e := s.Else.(type) {
	case *syntax.BlockStmt:
		out.Else = c.block(e)
	case *syntax.IfStmt:
		// `else if` is `else { if … }`.
		if nested := c.ifStmt(e); nested != nil {
			out.Else = &Block{Pos: c.pos(e.If), Stmts: []Stmt{nested}}
		}
	}
	if cond == nil {
		return nil
	}
	return out
}

func (c *converter) typeSwitch(s *syntax.TypeSwitchStmt) Stmt {
	switch {
	case s.Init != nil:
		c.refuse(s.Init, "a switch init statement")
		return nil
	case len(s.Subjects) != 1:
		c.refuseAt(s.Switch, "a multi-subject type switch")
		return nil
	}
	// The decided match binds the narrowed value with `case T as p` (#196,
	// ninth round); the prototype's type switch has one binding shared by
	// every case, so every `as` must name the same binding.
	bind := ""
	if len(s.Binds) == 1 {
		bind = s.Binds[0].Name
	}
	for _, cl := range s.Clauses {
		for _, t := range cl.List {
			if as, ok := t.(*syntax.AsPattern); ok {
				if bind != "" && bind != as.Name.Name {
					c.refuse(as, "a second binding name in one match")
					return nil
				}
				bind = as.Name.Name
			}
		}
	}
	if bind == "" {
		c.refuseAt(s.Switch, "a type switch without a binding")
		return nil
	}
	out := &TypeSwitchStmt{Pos: c.pos(s.Switch), Bind: bind, Subject: c.expr(s.Subjects[0].X)}
	for _, cl := range s.Clauses {
		if cl.List == nil {
			c.refuseAt(cl.Case, "a default clause")
			continue
		}
		var types []QName
		for _, t := range cl.List {
			if as, ok := t.(*syntax.AsPattern); ok {
				t = as.X
			}
			q, ok := c.qname(t)
			if !ok || q.Pkg == "" && q.Name == "_" {
				c.refuse(t, "this case")
				continue
			}
			types = append(types, q)
		}
		out.Cases = append(out.Cases, TypeCase{Pos: c.pos(cl.Case), Types: types, Body: &Block{Pos: c.pos(cl.Case), Stmts: c.stmts(cl.Body)}})
	}
	if out.Subject == nil {
		return nil
	}
	return out
}

// ---------------------------------------------------------------- expressions

// binaryOps are the operators the prototype parses; the checker refuses
// all but == and + itself.
var binaryOps = map[syntax.Token]bool{
	syntax.LOR: true, syntax.LAND: true, syntax.EQL: true, syntax.NEQ: true, syntax.LSS: true,
	syntax.LEQ: true, syntax.GTR: true, syntax.GEQ: true, syntax.ADD: true, syntax.SUB: true,
	syntax.MUL: true, syntax.QUO: true,
}

func (c *converter) expr(x syntax.Expr) Expr {
	switch x := x.(type) {
	case *syntax.Ident:
		return &Ident{Pos: c.pos(x.Pos()), Name: x.Name}
	case *syntax.BasicLit:
		kinds := map[syntax.Token]tokKind{syntax.INT: tInt, syntax.DURATION: tDuration, syntax.STRING: tString}
		kind, ok := kinds[x.Kind]
		if !ok {
			c.refuse(x, "a rune literal")
			return nil
		}
		return &BasicLit{Pos: c.pos(x.Pos()), Kind: kind, Text: x.Value}
	case *syntax.ParenExpr:
		return c.expr(x.X)
	case *syntax.BinaryExpr:
		if !binaryOps[x.Op] {
			c.refuseAt(x.OpPos, "the operator "+x.Op.String())
			return nil
		}
		l, r := c.expr(x.X), c.expr(x.Y)
		if l == nil || r == nil {
			return nil
		}
		return &Binary{Pos: c.pos(x.OpPos), Op: x.Op.String(), L: l, R: r}
	case *syntax.SelectorExpr:
		inner := c.expr(x.X)
		if inner == nil {
			return nil
		}
		return &Selector{Pos: c.pos(x.Sel.Pos()), X: inner, Sel: x.Sel.Name}
	case *syntax.CallExpr:
		return c.call(x)
	case *syntax.CompositeLit:
		return c.compositeLit(x)
	case *syntax.TryExpr:
		if inner := c.expr(x.X); inner != nil {
			return &TryExpr{Pos: c.pos(x.Try), X: inner}
		}
		return nil
	case *syntax.CatchExpr:
		return c.catch(x)
	case *syntax.WireLit:
		// As for a named wire, the header's meaning is the checker's (#196).
		if x.Provides != nil || x.Needs != nil {
			if x.Provides != nil {
				c.refuse(x.Provides, "a wire's provides clause (checked by #196)")
			}
			if x.Needs != nil {
				c.refuse(x.Needs, "a wire's needs clause (checked by #196)")
			}
			return nil
		}
		return &WireLit{Pos: c.pos(x.Wire), Entries: c.wireEntries(x.Body)}
	case *syntax.FuncLit:
		c.refuse(x, "a function literal")
	case *syntax.GoExpr:
		c.refuse(x, "a go expression")
	case *syntax.UnaryExpr:
		c.refuse(x, "the unary operator "+x.Op.String())
	case *syntax.StarExpr:
		c.refuse(x, "a pointer")
	case *syntax.IndexExpr:
		c.refuse(x, "an index or instantiation")
	case *syntax.SliceExpr:
		c.refuse(x, "a slice expression")
	default:
		c.refuse(x, "this expression")
	}
	return nil
}

// takesBlock reports whether the prototype models a trailing block on a
// call to fun: with(…) and timeout.After(…) (README Gap 31).
func takesBlock(fun syntax.Expr) bool {
	switch f := fun.(type) {
	case *syntax.Ident:
		return f.Name == "with"
	case *syntax.SelectorExpr:
		if pkg, ok := f.X.(*syntax.Ident); ok {
			return pkg.Name == "timeout" && f.Sel.Name == "After"
		}
	}
	return false
}

func (c *converter) call(x *syntax.CallExpr) Expr {
	if x.Ellipsis.IsValid() {
		c.refuseAt(x.Ellipsis, "a spread argument")
		return nil
	}
	if x.Trailing != nil && !takesBlock(x.Fun) {
		c.refuse(x.Trailing, "a trailing block on this call")
		return nil
	}
	args := x.Args
	var fun Expr
	if id, ok := x.Fun.(*syntax.Ident); ok && c.methodNames[id.Name] && len(args) > 0 {
		// A call by name of a function read as a method: `f(x, a)` is
		// `x.f(a)` (196m).
		if recv := c.expr(args[0]); recv != nil {
			fun = &Selector{Pos: c.pos(id.Pos()), X: recv, Sel: id.Name}
		}
		args = args[1:]
	} else {
		fun = c.expr(x.Fun)
	}
	lparen := x.Lparen
	if !lparen.IsValid() {
		lparen = x.Fun.End() // the with form has no parentheses
	}
	out := &Call{Pos: c.pos(lparen), Fun: fun}
	ok := fun != nil
	for _, a := range args {
		v := c.expr(a)
		ok = ok && v != nil
		out.Args = append(out.Args, v)
	}
	if x.Trailing != nil {
		out.Block = c.block(x.Trailing)
	}
	if !ok {
		return nil
	}
	return out
}

func (c *converter) compositeLit(x *syntax.CompositeLit) Expr {
	id, ok := x.Type.(*syntax.Ident)
	if !ok {
		if x.Type == nil {
			c.refuse(x, "an elided composite literal type")
		} else {
			c.refuse(x.Type, "this composite literal type")
		}
		return nil
	}
	out := &CompositeLit{Pos: c.pos(id.Pos()), Type: id.Name}
	valid := true
	for _, e := range x.Elts {
		if kv, ok := e.(*syntax.KeyValueExpr); ok {
			key, ok := kv.Key.(*syntax.Ident)
			if !ok {
				c.refuse(kv.Key, "this composite literal key")
				valid = false
				continue
			}
			v := c.expr(kv.Value)
			valid = valid && v != nil
			out.Elems = append(out.Elems, KeyValue{Key: key.Name, Value: v})
			continue
		}
		out.Positional = true
		v := c.expr(e)
		valid = valid && v != nil
		out.Elems = append(out.Elems, KeyValue{Value: v})
	}
	if !valid {
		return nil
	}
	return out
}

func (c *converter) catch(x *syntax.CatchExpr) Expr {
	if x.Bind != nil {
		c.refuse(x.Bind, "a catch binding")
		return nil
	}
	inner := c.expr(x.X)
	out := &CatchExpr{Pos: c.pos(x.Catch), X: inner}
	valid := inner != nil
	for _, arm := range x.Arms {
		types := c.qnames(arm.Types, "this caught type")
		es, ok := arm.Body[0:min(1, len(arm.Body))], len(arm.Body) == 1
		var value Expr
		if ok {
			if s, isExpr := es[0].(*syntax.ExprStmt); isExpr {
				value = c.expr(s.X)
			} else {
				ok = false
			}
		}
		if !ok {
			c.refuseAt(arm.Case, "a catch arm that is not one expression")
			valid = false
			continue
		}
		valid = valid && value != nil && len(types) == len(arm.Types)
		out.Arms = append(out.Arms, CatchArm{Pos: c.pos(arm.Case), Types: types, Value: value})
	}
	if !valid {
		return nil
	}
	return out
}
