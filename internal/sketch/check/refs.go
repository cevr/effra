package check

import "ergo.local/prototype/internal/sketch/syntax"

// Class H (round 16): the body graph does not depend on typing. Its edges
// are the plain identifiers of each body that resolve, through the body's
// lexical scopes, to a package function or a package var. refWalker finds
// them before any body is typed, visits every node of the body and never
// stops early, so a tentative type error (a generic call refused while the
// rows are still provisional) cannot hide an edge (round 15 M1).
//
// The scopes mirror the checker's (stmt.go, rows.go checkFuncBody, call.go
// body): a function's frame holds its receiver, its parameters and the bare
// names of its written capability row; a block, an if, a for, a range, a
// switch and each of its clauses, a select clause, a catch arm and a
// function literal or block open a scope; := and a local var bind after
// their right-hand side. Type parameters are not values (lookup does not
// see them), so they shadow nothing.
//
// Identifiers that are not value references are skipped: a selector's
// name, a label, a type (a composite literal's type, a local var's type, a
// function literal's signature, a case type of a type switch or a catch
// arm), a wire key and a named wire, and a struct literal's field names. A
// composite literal's key is a field label when the literal's type is a
// struct and an expression otherwise (a map key, an array index), as in
// go/types: the walk decides which from the literal's written type
// (reflits.go), walks every expression key, so a map key that names a
// package var or function is an edge and a class G value use (round 17),
// and walks a key whose literal's kind it cannot decide as an expression.
//
// References that need types (a method through a selector, a function of
// another package) are no edge: a method and an imported function spell
// their rows. A call x.f(…) that can reach a package function on x's type
// (196m) is an edge to the one function it resolves to, never to every
// function named f (round 6 review M1): when the walk fixes x's type from
// declarations (a typed local, a parameter, a composite literal) it
// resolves the call itself, and otherwise the call waits for typing
// (selfCallee, resolveSelfCalls), as go/types records a method use.
//
// The walk also finds the class G value uses (rows.go pinGroup), so they do
// not depend on typing either (round 16). A reference to a package function
// with an inferred row that is not the callee of a call is a value use of
// both its rows. A call of it is a value use of a row when an enclosing
// frame carries that row as a value, walking the frames from the innermost
// out: a function literal stops the walk for a row it writes (an error row,
// a needs clause) and passes it on when it is called on the spot, else it
// is a value; a trailing block of a call other than with is a value; a go
// expression that is not a statement is a value for errors (its task's
// type carries them) and passes capabilities on (they are charged at the go
// site). A with block and a go statement are no frame. A function literal
// or trailing block passed directly to a declared callee whose parameter
// has a function type with a closed row (no type parameter in it) takes
// that row from the parameter (call.go funcLit, blockLit), so the walk
// stops there for that row (round 17 m1): the callee's signature was
// resolved before any body (decl.go run), so this is order-invariant.
type refWalker struct {
	c      *checker
	scopes []map[string]*localBinding
	refs   map[any]bool
	// values holds the class G value uses: for each package function with
	// an inferred row, where the body first uses each of its rows (errors,
	// capabilities) as a value, in the walk's order.
	values  map[*Func][2]useSite
	frames  []valueFrame
	callees map[syntax.Node]bool // callees of calls: identifiers and function literals
	// argRows marks a function literal passed to a parameter whose function
	// type closes the row (errors, capabilities): see refWalker.
	argRows map[syntax.Node][2]bool
	// tparams are the type parameters of the function walked, which come
	// before declarations in a written type (localTypeCtx).
	tparams map[string]bool
	// ops counts the literal elements walked (an operation count for the
	// linear-growth tests).
	ops int
	// pending holds the calls x.f(…) whose callee needs x's type from
	// typing (selfCallee), in walk order, with the value-use sites their
	// frames give a called function (callSite).
	pending []pendingSelf
	// Test builds only (refTrace set): the resolution of each walked
	// identifier (a package object or nil), the identifiers in type
	// and wire positions (not value references), the composite-literal
	// keys walked as values because the literal's kind is not decided
	// (reflits.go), and every node visited.
	idents     map[*syntax.Ident]any
	nameIdents map[*syntax.Ident]bool
	undecided  map[*syntax.Ident]bool
	visited    map[syntax.Node]bool
}

// A useSite is where a body first uses a row of a function as a value: a
// reference that is not a call (via "") or the literal, trailing block or
// go task that carries the row (via names it).
type useSite struct {
	ok  bool
	at  syntax.Pos
	via string
}

// A frameMode says what a value frame does with a row of the calls inside
// it.
type frameMode uint8

const (
	framePass  frameMode = iota // the row flows to the enclosing frame
	frameValue                  // the row is carried by a value
	frameStop                   // the row is written: it stops here
)

// A valueFrame is a function literal, trailing block or go expression, with
// its mode for errors and for capabilities, its position and its name.
type valueFrame struct {
	mode [2]frameMode
	at   syntax.Pos
	via  string
}

// refTrace, when set by a test, receives each body's walk.
var refTrace func(root syntax.Node, w *refWalker)

// bodyRefs walks f's body: the package functions and vars it refers to, and
// its class G value uses.
func (c *checker) bodyRefs(f *Func) *refWalker {
	w := c.newRefWalker()
	saved := c.cur
	c.cur = f.file
	defer func() { c.cur = saved }()
	w.push()
	d := f.Decl
	if d.Recv != nil {
		for _, field := range d.Recv.List {
			for _, n := range field.Names {
				w.bindAs(n.Name, &localBinding{rt: w.receiverType(f, field.Type)})
			}
			// The receiver's type arguments name its type parameters.
			rt := field.Type
			if s, ok := rt.(*syntax.StarExpr); ok {
				rt = s.X
			}
			if ix, ok := rt.(*syntax.IndexExpr); ok {
				for _, a := range ix.Indices {
					if id, ok := a.(*syntax.Ident); ok {
						w.tparams[id.Name] = true
					}
				}
			}
		}
	}
	if f.Sig != nil {
		for _, tp := range f.Sig.TParams {
			w.tparams[tp.Name] = true
		}
	}
	w.params(d.Type, f.Sig)
	w.list(d.Body.List)
	w.pop()
	w.visit(d.Body)
	if refTrace != nil {
		refTrace(d.Body, w)
	}
	return w
}

// varRefs walks a var initializer, as bodyRefs.
func (c *checker) varRefs(pv *pkgVar) *refWalker {
	w := c.newRefWalker()
	saved := c.cur
	c.cur = pv.file
	defer func() { c.cur = saved }()
	var root syntax.Node
	if pv.spec != nil {
		if pv.idx < len(pv.spec.Values) {
			root = pv.spec.Values[pv.idx]
		} else if len(pv.spec.Values) == 1 {
			root = pv.spec.Values[0]
		}
	}
	if root == nil {
		return w
	}
	w.expr(root.(syntax.Expr))
	if refTrace != nil {
		refTrace(root, w)
	}
	return w
}

func (c *checker) newRefWalker() *refWalker {
	w := &refWalker{c: c, refs: map[any]bool{}, values: map[*Func][2]useSite{}, callees: map[syntax.Node]bool{},
		argRows: map[syntax.Node][2]bool{}, tparams: map[string]bool{}}
	if refTrace != nil {
		w.idents = map[*syntax.Ident]any{}
		w.nameIdents = map[*syntax.Ident]bool{}
		w.undecided = map[*syntax.Ident]bool{}
		w.visited = map[syntax.Node]bool{}
	}
	return w
}

// A localBinding is what the walk knows of a local's type, which its
// declaration fixes: a written type, resolved where it is declared (a
// declared function's parameter or receiver takes its resolved signature's
// type; a function literal's parameter, a typed var or a local bound to a
// composite literal resolves the written name), or the signature of the
// declared function it aliases. Either may be nil (unknown). A local
// declared later with the type's name does not change it (round 18 m1).
type localBinding struct {
	rt  Type
	sig *Signature
}

// receiverType is the type of f's receiver, as its declaration resolved
// it, or the written type when f's receiver did not resolve.
func (w *refWalker) receiverType(f *Func, written syntax.Expr) Type {
	if f.Recv != nil {
		return f.Recv
	}
	return w.writtenType(written)
}

// writtenType resolves a written type at the point it is declared: a type
// name, possibly instantiated or behind *, through the package scope and
// the imports, or a written struct type (`struct{ Run func(…) error[E] }`,
// round 19 m1). It is nil for a type parameter, a name a local shadows
// there, a struct type that mentions either or an implicit type parameter,
// and any other form. An instance resolves to its generic type, whose
// members keep their type parameters, so closedRows treats a row that
// mentions them as open.
func (w *refWalker) writtenType(t syntax.Expr) Type {
	if s, ok := t.(*syntax.StarExpr); ok {
		t = s.X
	}
	// check-v1: dead. The decided grammar has no anonymous struct type (ruling C).
	if st, ok := t.(*syntax.StructType); ok {
		return w.structType(st)
	}
	if ix, ok := t.(*syntax.IndexExpr); ok {
		t = ix.X
	}
	id, ok := t.(*syntax.Ident)
	if !ok || w.tparams[id.Name] || w.local(id.Name) {
		return nil
	}
	if tn, ok := w.c.typeObject(id.Name, w.c.cur).(*TypeName); ok {
		return tn.Type
	}
	return nil
}

// structType resolves a written struct type before any body is typed, as
// the declaration's signature would: silently, through the package scope
// and the imports. Every name in it must be a declared type or package
// that no local shadows, so the type does not depend on the body; a field
// name a local shares makes it nil too, which is conservative.
// check-v1: dead. The decided grammar has no anonymous struct type (ruling C).
func (w *refWalker) structType(st *syntax.StructType) Type {
	fixed := true
	syntax.Inspect(st, func(n syntax.Node) bool {
		id, ok := n.(*syntax.Ident)
		if !ok || !fixed {
			return fixed
		}
		if w.tparams[id.Name] || w.local(id.Name) || isParamName(id.Name) && w.c.typeObject(id.Name, w.c.cur) == nil {
			fixed = false
		}
		return fixed
	})
	if !fixed {
		return nil
	}
	w.c.silent++
	rt := w.c.typExpr0(st, nil)
	w.c.silent--
	if isInvalid(rt) {
		return nil
	}
	return rt
}

func (w *refWalker) push()            { w.scopes = append(w.scopes, map[string]*localBinding{}) }
func (w *refWalker) pop()             { w.scopes = w.scopes[:len(w.scopes)-1] }
func (w *refWalker) bind(name string) { w.bindAs(name, &localBinding{}) }

func (w *refWalker) bindAs(name string, b *localBinding) {
	w.scopes[len(w.scopes)-1][name] = b
}

// bindPatterns binds the names that the `as` patterns of a case or catch
// arm bind (196p): reads of them in the arm are locals.
func (w *refWalker) bindPatterns(list []syntax.Expr) {
	for _, p := range list {
		syntax.Inspect(p, func(n syntax.Node) bool {
			if a, ok := n.(*syntax.AsPattern); ok && a.Name.Name != "_" {
				w.bind(a.Name.Name)
			}
			return true
		})
	}
}

func (w *refWalker) visit(n syntax.Node) {
	if w.visited != nil && n != nil {
		w.visited[n] = true
	}
}

// params binds a function type's parameter names and the bare names of its
// written capability row (bindRequirements), and visits the signature. sig
// is the declared function's resolved signature (nil for a function
// literal): each parameter takes its type from there, so a written
// structural type (`r struct{ Run func(…) }`) is known too.
func (w *refWalker) params(ft *syntax.FuncType, sig *Signature) {
	if ft == nil {
		return
	}
	if ft.Params != nil {
		count := 0
		for _, field := range ft.Params.List {
			count += max(len(field.Names), 1)
		}
		if sig != nil && len(sig.Params) != count {
			sig = nil
		}
		i := 0
		for _, field := range ft.Params.List {
			_, variadic := field.Type.(*syntax.Ellipsis)
			for j := range max(len(field.Names), 1) {
				var rt Type
				switch {
				case variadic:
				case sig != nil:
					rt = sig.Params[i].Type
				default:
					rt = w.writtenType(field.Type)
				}
				i++
				if j < len(field.Names) && field.Names[j].Name != "_" {
					w.bindAs(field.Names[j].Name, &localBinding{rt: rt})
				}
			}
		}
	}
	if ft.Needs != nil {
		for _, x := range ft.Needs.Names {
			if w.c.capTypeName(x) == nil {
				continue
			}
			switch x := x.(type) {
			case *syntax.Ident:
				w.bind(x.Name)
			case *syntax.SelectorExpr:
				w.bind(x.Sel.Name)
			}
		}
	}
	w.typ(ft)
}

// ident resolves a plain identifier in a value position.
func (w *refWalker) ident(x *syntax.Ident) {
	w.visit(x)
	var o any
	if !w.local(x.Name) {
		switch obj := w.c.pkg.Scope.names[x.Name].(type) {
		case *Func:
			o = obj
		case *Var:
			if pv := w.c.pkgVarOf(obj); pv != nil {
				o = pv
			}
		}
	}
	if o != nil {
		w.refs[o] = true
	}
	if f, ok := o.(*Func); ok && (f.inferErrors || f.inferUses) {
		w.valueUse(f, w.callees[x], x.Pos())
	}
	if w.idents != nil {
		w.idents[x] = o
	}
}

// valueUse records the class G value uses of a reference to f at at:
// called or not (see refWalker). The first site of each row is kept.
func (w *refWalker) valueUse(f *Func, called bool, at syntax.Pos) {
	uses := w.values[f]
	for k := range uses {
		if uses[k].ok {
			continue
		}
		if !called {
			uses[k] = useSite{ok: true, at: at}
			continue
		}
		uses[k] = w.callSite()[k]
	}
	w.values[f] = uses
}

// callSite is where a call at the current point of the walk uses each row
// of its callee as a value: the innermost frame that does not pass the row
// on decides, and only a frame that carries it as a value is a use.
func (w *refWalker) callSite() [2]useSite {
	var site [2]useSite
	for k := range site {
		for i := len(w.frames) - 1; i >= 0; i-- {
			fr := w.frames[i]
			if m := fr.mode[k]; m != framePass {
				if m == frameValue {
					site[k] = useSite{ok: true, at: fr.at, via: fr.via}
				}
				break
			}
		}
	}
	return site
}

// A pendingSelf is a call x.f(…) whose callee typing resolves, with the
// value-use sites of its frames (callSite).
type pendingSelf struct {
	sel  *syntax.SelectorExpr
	site [2]useSite
}

// frame walks walk inside a value frame.
func (w *refWalker) frame(f valueFrame, walk func()) {
	w.frames = append(w.frames, f)
	walk()
	w.frames = w.frames[:len(w.frames)-1]
}

// callee marks the callee of a call: a plain identifier, possibly
// instantiated, or a function literal called on the spot.
func (w *refWalker) callee(fun syntax.Expr) {
	x := unparen(fun)
	if ix, ok := x.(*syntax.IndexExpr); ok {
		x = unparen(ix.X)
	}
	switch x.(type) {
	case *syntax.Ident, *syntax.FuncLit:
		w.callees[x] = true
	}
}

// selfCallee records the edge of a call x.f(…) that can reach a package
// function on x's type (196m rule 1). The edge goes to the one function the
// call resolves to, never to every function named f (round 6 review M1):
// an edge by name merges unrelated bodies into one component, which makes
// false class G and initialization-cycle refusals, and k same-named
// functions called m times give k·m edges.
//
// A receiver whose type its declaration fixes (a parameter, a typed local,
// a local bound to a composite literal, a composite literal) resolves here:
// a field f of that type comes first and is no edge, else the function on
// that type is. Any other receiver (a call's result, a local bound to one,
// an element, a field) needs typing: the call is pending, and
// resolveSelfCalls gives it the callee typing resolves. The callee is
// called, so its rows are value uses only through a frame that carries
// them as a value (callSite).
func (w *refWalker) selfCallee(fun syntax.Expr) {
	sel, ok := unparen(fun).(*syntax.SelectorExpr)
	if !ok {
		return
	}
	if id, ok := sel.X.(*syntax.Ident); ok && !w.local(id.Name) {
		if _, isPkg := w.c.typeObject(id.Name, w.c.cur).(*PkgName); isPkg {
			return
		}
	}
	first, ok := w.c.pkg.Scope.names[sel.Sel.Name].(*Func)
	if !ok || first.owner == nil {
		return
	}
	t := w.staticType(sel.X)
	if t == nil {
		w.pending = append(w.pending, pendingSelf{sel: sel, site: w.callSite()})
		return
	}
	if p, ok := t.(*Pointer); ok {
		t = p.Elem
	}
	n, _ := t.(*Named)
	if n == nil {
		return
	}
	if s := asStruct(n); s != nil {
		if fld, _ := s.field(sel.Sel.Name); fld != nil {
			return
		}
	}
	if f := n.method(sel.Sel.Name); f != nil && f.owner != nil {
		w.refs[f] = true
		if f.inferErrors || f.inferUses {
			w.valueUse(f, true, sel.Sel.Pos())
		}
	}
}

// calleeSig returns the signature of a call's callee when it is fixed
// before any body is typed (decl.go run resolves every signature first):
//   - a plain identifier, possibly instantiated, that names a function of
//     the package or the universe, or a local that aliases one;
//   - a function of an imported package;
//   - a method, or a field of function type, selected from a receiver whose
//     type is written: a parameter, a receiver, a typed local or one bound
//     to a composite literal, or a composite literal itself (round 17 m1).
//
// Any other callee (a value whose type is inferred) gives nil, and the
// round-15 rule stays: conservative.
func (w *refWalker) calleeSig(fun syntax.Expr) *Signature {
	x := unparen(fun)
	if ix, ok := x.(*syntax.IndexExpr); ok {
		x = unparen(ix.X)
	}
	switch x := x.(type) {
	case *syntax.Ident:
		if b := w.lookupLocal(x.Name); b != nil {
			return b.sig
		}
		return declaredSig(w.c.typeObject(x.Name, w.c.cur))
	case *syntax.SelectorExpr:
		if id, ok := x.X.(*syntax.Ident); ok && !w.local(id.Name) {
			if pn, ok := w.c.typeObject(id.Name, w.c.cur).(*PkgName); ok && !pn.broken {
				return declaredSig(pn.Pkg.Scope.names[x.Sel.Name])
			}
			return nil
		}
		return memberSig(w.staticType(x.X), x.Sel.Name)
	}
	return nil
}

// declaredSig is the signature of a declared function that is not generic
// in a way the call must infer: a function, not a method.
func declaredSig(o Object) *Signature {
	if f, ok := o.(*Func); ok && f.Recv == nil {
		return f.Sig
	}
	return nil
}

// initBinding is what a local's initializer fixes of its type: the
// signature of the declared function it aliases, or a composite literal's
// written type.
func (w *refWalker) initBinding(e syntax.Expr) *localBinding {
	switch e := unparen(e).(type) {
	case *syntax.Ident:
		if b := w.lookupLocal(e.Name); b != nil {
			return &localBinding{rt: b.rt, sig: b.sig}
		}
		if sig := declaredSig(w.c.typeObject(e.Name, w.c.cur)); sig != nil && len(sig.TParams) == 0 {
			return &localBinding{sig: sig}
		}
	case *syntax.SelectorExpr:
		if id, ok := e.X.(*syntax.Ident); ok && !w.local(id.Name) {
			if pn, ok := w.c.typeObject(id.Name, w.c.cur).(*PkgName); ok && !pn.broken {
				if sig := declaredSig(pn.Pkg.Scope.names[e.Sel.Name]); sig != nil && len(sig.TParams) == 0 {
					return &localBinding{sig: sig}
				}
			}
		}
	case *syntax.CompositeLit:
		return &localBinding{rt: w.writtenType(e.Type)}
	}
	return &localBinding{}
}

// staticType is the type of a receiver expression fixed before bodies, or
// nil: a local whose declaration resolved it (localBinding), or a
// composite literal's written type.
func (w *refWalker) staticType(e syntax.Expr) Type {
	switch e := unparen(e).(type) {
	case *syntax.Ident:
		if b := w.lookupLocal(e.Name); b != nil {
			return b.rt
		}
	case *syntax.CompositeLit:
		return w.writtenType(e.Type)
	}
	return nil
}

// memberSig is the signature of the method or the function-typed field
// name of t: a named type, a pointer to one, or a written struct type. A
// method or field of a generic type keeps its type parameters, so
// closedRows treats a row that mentions them as open.
func memberSig(t Type, name string) *Signature {
	if p, ok := t.(*Pointer); ok {
		t = p.Elem
	}
	// check-v1: dead. A written struct type is anonymous (ruling C).
	if s, ok := t.(*Struct); ok {
		if fld, _ := s.field(name); fld != nil {
			sig, _ := under0(fld.Type).(*Signature)
			return sig
		}
		return nil
	}
	n, ok := t.(*Named)
	if !ok {
		return nil
	}
	switch u := under(n).(type) {
	case *Interface:
		if m := u.method(name); m != nil {
			return m.Sig
		}
		return nil
	case *Struct:
		if f := n.method(name); f != nil {
			return declaredMethodSig(f)
		}
		if fld, _ := u.field(name); fld != nil {
			sig, _ := under0(fld.Type).(*Signature)
			return sig
		}
	}
	if f := n.method(name); f != nil {
		return declaredMethodSig(f)
	}
	return nil
}

// declaredMethodSig is the signature a call x.f(…) of the method f checks
// its arguments against: a package function on x's type without the first
// parameter, which x fills (196m).
func declaredMethodSig(f *Func) *Signature {
	if f.owner != nil && f.Sig != nil && len(f.Sig.Params) > 0 {
		return dropSelf(f.Sig)
	}
	return f.Sig
}

// closedRows reports which rows (errors, capabilities) of a parameter of
// function type are closed: written in the signature with no type
// parameter, so a literal passed there takes the row from it.
func closedRows(pt Type) [2]bool {
	ps, ok := under0(pt).(*Signature)
	if !ok {
		return [2]bool{}
	}
	closed := func(r *Row) bool {
		if r == nil {
			return false
		}
		for _, e := range r.Elems() {
			if _, ok := e.(*TypeParam); ok {
				return false
			}
		}
		return true
	}
	return [2]bool{closed(ps.Errors), closed(ps.Uses)}
}

func (w *refWalker) local(name string) bool { return w.lookupLocal(name) != nil }

// lookupLocal returns the innermost local named name, or nil.
func (w *refWalker) lookupLocal(name string) *localBinding {
	for i := len(w.scopes) - 1; i >= 0; i-- {
		if b, ok := w.scopes[i][name]; ok {
			return b
		}
	}
	return nil
}

// typ visits a type expression: no identifier in it is a value.
func (w *refWalker) typ(n syntax.Node) {
	if n == nil {
		return
	}
	syntax.Inspect(n, func(x syntax.Node) bool {
		if id, ok := x.(*syntax.Ident); ok {
			w.name(id)
		} else {
			w.visit(x)
		}
		return true
	})
}

// name visits an identifier in a type or wire position: not a value
// reference.
func (w *refWalker) name(id *syntax.Ident) {
	w.visit(id)
	if w.nameIdents != nil {
		w.nameIdents[id] = true
	}
}

func (w *refWalker) exprs(list []syntax.Expr) {
	for _, x := range list {
		w.expr(x)
	}
}

func (w *refWalker) expr(x syntax.Expr) {
	if x == nil {
		return
	}
	w.visit(x)
	switch x := x.(type) {
	case *syntax.Ident:
		w.ident(x)
	case *syntax.SelectorExpr:
		w.expr(x.X)
		w.visit(x.Sel)
	case *syntax.CompositeLit:
		w.compositeLit(x, litType{})
	case *syntax.FuncLit:
		f := valueFrame{mode: [2]frameMode{frameValue, frameValue}, at: x.Pos(), via: "the function literal"}
		if w.callees[x] {
			f.mode = [2]frameMode{framePass, framePass}
		}
		closed := w.argRows[x]
		if writesErrors(x.Type) || closed[0] {
			f.mode[0] = frameStop
		}
		if x.Type.Needs != nil || closed[1] {
			f.mode[1] = frameStop
		}
		w.frame(f, func() {
			w.push()
			w.params(x.Type, nil)
			w.visit(x.Body)
			w.list(x.Body.List)
			w.pop()
		})
	case *syntax.CallExpr:
		if id, ok := x.Fun.(*syntax.Ident); ok && id.Name == "with" {
			// The with construct: its first argument is a wire literal or a
			// named wire (wire.go withArg), never a value reference.
			w.visit(id)
			for i, a := range x.Args {
				if i > 0 || x.Trailing == nil {
					// Without a block it is an error, and the arguments are
					// checked as values (wire.go with, silentArgs).
					w.expr(a)
					continue
				}
				if wid, ok := syntax.Expr(a).(*syntax.Ident); ok {
					w.name(wid)
					continue
				}
				if p, ok := a.(*syntax.ParenExpr); ok {
					if wid, ok := p.X.(*syntax.Ident); ok {
						w.visit(p)
						w.name(wid)
						continue
					}
				}
				w.expr(a)
			}
			w.block(x.Trailing)
		} else {
			w.callee(x.Fun)
			w.expr(x.Fun)
			w.selfCallee(x.Fun)
			sig := w.calleeSig(x.Fun)
			spread := x.Ellipsis.IsValid()
			for i, a := range x.Args {
				if lit, ok := unparen(a).(*syntax.FuncLit); ok && sig != nil {
					w.argRows[lit] = closedRows(paramType(sig, i, spread))
				}
				w.expr(a)
			}
			if x.Trailing != nil {
				f := valueFrame{mode: [2]frameMode{frameValue, frameValue}, at: x.Trailing.Pos(), via: "the trailing block"}
				if sig != nil {
					closed := closedRows(paramType(sig, len(x.Args), spread))
					for k := range closed {
						if closed[k] {
							f.mode[k] = frameStop
						}
					}
				}
				w.frame(f, func() { w.block(x.Trailing) })
			}
		}
	case *syntax.GoExpr:
		w.frame(valueFrame{mode: [2]frameMode{frameValue, framePass}, at: x.Pos(), via: "the go task"}, func() { w.goBody(x) })
	case *syntax.CatchExpr:
		w.expr(x.X)
		w.visit(x.Bind)
		for _, arm := range x.Arms {
			w.visit(arm)
			w.push()
			if x.Bind != nil {
				w.bind(x.Bind.Name)
			}
			for _, t := range arm.Types {
				w.typ(t)
			}
			w.bindPatterns(arm.Types)
			w.list(arm.Body)
			w.pop()
		}
	case *syntax.WireLit:
		w.wireBody(x.Body)
	case *syntax.ParenExpr:
		w.expr(x.X)
	case *syntax.IndexExpr:
		w.expr(x.X)
		w.exprs(x.Indices)
	case *syntax.SliceExpr:
		w.expr(x.X)
		w.expr(x.Low)
		w.expr(x.High)
		w.expr(x.Max)
	case *syntax.TypeGuard:
		w.expr(x.X)
	case *syntax.StarExpr:
		w.expr(x.X)
	case *syntax.UnaryExpr:
		w.expr(x.X)
	case *syntax.BinaryExpr:
		w.expr(x.X)
		w.expr(x.Y)
	case *syntax.TryExpr:
		w.expr(x.X)
	case *syntax.TupleExpr:
		w.exprs(x.Elts)
	case *syntax.KeyValueExpr:
		w.expr(x.Key)
		w.expr(x.Value)
	case *syntax.InterpolatedString:
		for _, t := range x.Texts {
			w.visit(t)
		}
		w.exprs(x.Exprs)
	case *syntax.BasicLit:
	default:
		// A type in a value position (a conversion's type, an array or
		// map type, a function type): no value reference.
		w.typ(x)
	}
}

// wireBody visits a wire literal's entries: a key is a capability name or
// an embedded wire, a value is an expression.
func (w *refWalker) wireBody(b *syntax.WireBody) {
	if b == nil {
		return
	}
	w.visit(b)
	for _, e := range b.Entries {
		w.visit(e)
		w.typ(e.Key)
		for _, k := range e.Keys {
			w.typ(k)
		}
		w.expr(e.Value)
	}
}

// goBody visits what a go expression runs: a call or a block.
func (w *refWalker) goBody(g *syntax.GoExpr) {
	if g.Call != nil {
		w.expr(g.Call)
	}
	w.block(g.Block)
}

// writesErrors reports whether a function type writes its error row.
func writesErrors(ft *syntax.FuncType) bool {
	if ft == nil || ft.Results == nil {
		return false
	}
	for _, r := range ft.Results.List {
		if _, ok := r.Type.(*syntax.ErrorRow); ok {
			return true
		}
	}
	return false
}

// block visits a trailing block or a go block: a frame with no parameter.
func (w *refWalker) block(b *syntax.BlockStmt) {
	if b == nil {
		return
	}
	w.visit(b)
	w.push()
	w.list(b.List)
	w.pop()
}

func (w *refWalker) list(list []syntax.Stmt) {
	for _, s := range list {
		w.stmt(s)
	}
}

func (w *refWalker) stmt(s syntax.Stmt) {
	if s == nil {
		return
	}
	w.visit(s)
	switch s := s.(type) {
	case *syntax.DeclStmt:
		w.visit(s.Decl)
		for _, spec := range s.Decl.Specs {
			w.visit(spec)
			w.typ(spec.Type)
			w.exprs(spec.Values)
			for i, n := range spec.Names {
				w.visit(n)
				b := &localBinding{rt: w.writtenType(spec.Type)}
				if spec.Type == nil && len(spec.Values) == len(spec.Names) {
					b = w.initBinding(spec.Values[i])
				}
				w.bindAs(n.Name, b)
			}
		}
	case *syntax.LabeledStmt:
		w.visit(s.Label)
		w.stmt(s.Stmt)
	case *syntax.ExprStmt:
		if g, ok := unparen(s.X).(*syntax.GoExpr); ok {
			// A go statement discards its task: no frame.
			for x := s.X; x != syntax.Expr(g); x = x.(*syntax.ParenExpr).X {
				w.visit(x)
			}
			w.visit(g)
			w.goBody(g)
			break
		}
		w.expr(s.X)
	case *syntax.SendStmt:
		w.expr(s.Chan)
		w.expr(s.Value)
	case *syntax.IncDecStmt:
		w.expr(s.X)
	case *syntax.AssignStmt:
		w.exprs(s.Rhs)
		if s.Tok == syntax.DEFINE {
			for i, l := range s.Lhs {
				if id, ok := l.(*syntax.Ident); ok {
					w.visit(id)
					b := &localBinding{}
					if len(s.Lhs) == len(s.Rhs) {
						b = w.initBinding(s.Rhs[i])
					}
					w.bindAs(id.Name, b)
					continue
				}
				w.expr(l)
			}
		} else {
			w.exprs(s.Lhs)
		}
	case *syntax.DeferStmt:
		w.stmt(s.Stmt)
	case *syntax.ReturnStmt:
		w.exprs(s.Results)
	case *syntax.BranchStmt:
		w.visit(s.Label)
	case *syntax.BlockStmt:
		w.push()
		w.list(s.List)
		w.pop()
	case *syntax.IfStmt:
		w.push()
		w.stmt(s.Init)
		w.expr(s.Cond)
		w.stmt(s.Body)
		w.stmt(s.Else)
		w.pop()
	case *syntax.ForStmt:
		w.push()
		w.stmt(s.Init)
		w.expr(s.Cond)
		w.stmt(s.Post)
		w.stmt(s.Body)
		w.pop()
	case *syntax.RangeStmt:
		w.push()
		w.expr(s.X)
		for _, l := range []syntax.Expr{s.Key, s.Value} {
			if l == nil {
				continue
			}
			if id, ok := l.(*syntax.Ident); ok && s.Tok == syntax.DEFINE {
				w.visit(id)
				w.bind(id.Name)
				continue
			}
			w.expr(l)
		}
		w.stmt(s.Body)
		w.pop()
	case *syntax.SwitchStmt:
		w.push()
		w.stmt(s.Init)
		w.expr(s.Tag)
		for _, cl := range s.Clauses {
			w.visit(cl)
			w.exprs(cl.List)
			w.push()
			w.list(cl.Body)
			w.pop()
		}
		w.pop()
	case *syntax.TypeSwitchStmt:
		w.push()
		w.stmt(s.Init)
		for _, g := range s.Subjects {
			w.expr(g)
		}
		for _, b := range s.Binds {
			w.visit(b)
		}
		for _, cl := range s.Clauses {
			w.visit(cl)
			for _, t := range cl.List {
				w.typ(t)
			}
			w.push()
			for _, b := range s.Binds {
				if b.Name != "_" {
					w.bind(b.Name)
				}
			}
			w.bindPatterns(cl.List)
			w.list(cl.Body)
			w.pop()
		}
		w.pop()
	case *syntax.SelectStmt:
		for _, cl := range s.Clauses {
			w.visit(cl)
			w.push()
			w.stmt(cl.Comm)
			w.list(cl.Body)
			w.pop()
		}
	case *syntax.EmptyStmt:
	}
}
