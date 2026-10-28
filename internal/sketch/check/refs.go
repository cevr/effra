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
// arm), a wire key and a named wire, and a composite literal's identifier
// key, which is a field name or a map key depending on the literal's type.
// A map key never needs an edge: an edge orders a callee's rows before its
// caller, and a key is not called; the key's var still reaches the
// initialization order through typing.
//
// References that need types (a method through a selector, a function of
// another package) are no edge: a method and an imported function spell
// their rows.
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
// site). A with block and a go statement are no frame.
type refWalker struct {
	c      *checker
	scopes []map[string]bool
	refs   map[any]bool
	// values holds the class G value uses: for each package function with
	// an inferred row, which of its rows (errors, capabilities) the body
	// uses as a value.
	values  map[*Func][2]bool
	frames  []valueFrame
	callees map[syntax.Node]bool // callees of calls: identifiers and function literals
	// Test builds only (refWalk set): the resolution of each walked
	// identifier (a package object or nil) and every node visited.
	idents  map[*syntax.Ident]any
	visited map[syntax.Node]bool
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
// its mode for errors and for capabilities.
type valueFrame [2]frameMode

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
				w.bind(n.Name)
			}
		}
	}
	w.params(d.Type)
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
	w := &refWalker{c: c, refs: map[any]bool{}, values: map[*Func][2]bool{}, callees: map[syntax.Node]bool{}}
	if refTrace != nil {
		w.idents = map[*syntax.Ident]any{}
		w.visited = map[syntax.Node]bool{}
	}
	return w
}

func (w *refWalker) push()            { w.scopes = append(w.scopes, map[string]bool{}) }
func (w *refWalker) pop()             { w.scopes = w.scopes[:len(w.scopes)-1] }
func (w *refWalker) bind(name string) { w.scopes[len(w.scopes)-1][name] = true }

func (w *refWalker) visit(n syntax.Node) {
	if w.visited != nil && n != nil {
		w.visited[n] = true
	}
}

// params binds a function type's parameter names and the bare names of its
// written capability row (bindRequirements), and visits the signature.
func (w *refWalker) params(ft *syntax.FuncType) {
	if ft == nil {
		return
	}
	if ft.Params != nil {
		for _, field := range ft.Params.List {
			for _, n := range field.Names {
				if n.Name != "_" {
					w.bind(n.Name)
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
		w.valueUse(f, w.callees[x])
	}
	if w.idents != nil {
		w.idents[x] = o
	}
}

// valueUse records the class G value uses of a reference to f: called or
// not (see refWalker).
func (w *refWalker) valueUse(f *Func, called bool) {
	uses := w.values[f]
	for k := range uses {
		if !called {
			uses[k] = true
			continue
		}
		for i := len(w.frames) - 1; i >= 0; i-- {
			if m := w.frames[i][k]; m != framePass {
				uses[k] = uses[k] || m == frameValue
				break
			}
		}
	}
	w.values[f] = uses
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

func (w *refWalker) local(name string) bool {
	for i := len(w.scopes) - 1; i >= 0; i-- {
		if w.scopes[i][name] {
			return true
		}
	}
	return false
}

// typ visits a type expression: no identifier in it is a value.
func (w *refWalker) typ(n syntax.Node) {
	if n == nil {
		return
	}
	syntax.Inspect(n, func(x syntax.Node) bool {
		w.visit(x)
		return true
	})
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
		w.typ(x.Type)
		for _, e := range x.Elts {
			if kv, ok := e.(*syntax.KeyValueExpr); ok {
				w.visit(kv)
				if id, ok := kv.Key.(*syntax.Ident); ok {
					w.visit(id) // a field name or a map key: see refWalker
				} else {
					w.expr(kv.Key)
				}
				w.expr(kv.Value)
				continue
			}
			w.expr(e)
		}
	case *syntax.FuncLit:
		f := valueFrame{frameValue, frameValue}
		if w.callees[x] {
			f = valueFrame{framePass, framePass}
		}
		if writesErrors(x.Type) {
			f[0] = frameStop
		}
		if x.Type.Needs != nil {
			f[1] = frameStop
		}
		w.frame(f, func() {
			w.push()
			w.params(x.Type)
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
				if i > 0 {
					w.expr(a)
					continue
				}
				if wid, ok := syntax.Expr(a).(*syntax.Ident); ok {
					w.visit(wid)
					continue
				}
				if p, ok := a.(*syntax.ParenExpr); ok {
					if wid, ok := p.X.(*syntax.Ident); ok {
						w.visit(p)
						w.visit(wid)
						continue
					}
				}
				w.expr(a)
			}
			w.block(x.Trailing)
		} else {
			w.callee(x.Fun)
			w.expr(x.Fun)
			w.exprs(x.Args)
			if x.Trailing != nil {
				w.frame(valueFrame{frameValue, frameValue}, func() { w.block(x.Trailing) })
			}
		}
	case *syntax.GoExpr:
		w.frame(valueFrame{frameValue, framePass}, func() { w.goBody(x) })
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
			for _, n := range spec.Names {
				w.visit(n)
				w.bind(n.Name)
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
			for _, l := range s.Lhs {
				if id, ok := l.(*syntax.Ident); ok {
					w.visit(id)
					w.bind(id.Name)
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
