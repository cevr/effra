package sketch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
)

// Revision is the source revision of a sketch file: the SHA-256 of its bytes
// in hex, the same digest `ef` reports as a revision. A definition ID is
// meaningful only with the revision it was computed from; a trace header
// carries it once.
func Revision(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:])
}

// Def kinds.
const (
	DefType       = "type"
	DefCapability = "capability"
	DefOperation  = "operation" // a capability method
	DefFunc       = "func"
	DefMethod     = "method" // a provider method
	DefWire       = "wire"
	DefWireLit    = "wire literal"
	DefTimeout    = "timeout block"
	DefHost       = "host"
)

// Def is one definition of the checked model.
type Def struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Site is "<file>:<line>:<col>" of the declaration, "" for a host
	// definition.
	Site string `json:"site,omitempty"`
	// In is the definition that encloses a block, "" for a declaration.
	In string `json:"in,omitempty"`
	// Errors is the error row of a function, method, operation or block.
	Errors []string `json:"errors,omitempty"`
	// Spelling is the source head of a block, such as timeout.After(100ms).
	Spelling string `json:"spelling,omitempty"`
}

// Point is one place where emitted code raises a trace event itself: a
// capability call, the interruption point before it, a wire build or a catch
// arm. A recorded event of these kinds must match a point of the model.
type Point struct {
	Kind string // call, interrupt, construct or handler
	Def  string // the callee, the enclosing definition, the wire or the handled error
	Site string
	In   string // the enclosing definition
}

// Model is the definition table of one checked file: every definition ID with
// its site, and every point at which emitted code raises a trace event. The
// emitter and `sketchc trace` both derive it from the checked declarations, so
// no ID is written by hand and the same source always gives the same IDs.
type Model struct {
	Package string
	File    string // base name, as in //line directives and sites
	Defs    []Def  // source order, host definitions last
	Points  []Point

	byID   map[string]int
	blocks map[Expr]string     // a timeout.After call or a wire literal -> its definition ID
	points map[[3]string]Point // (kind, def, site) -> point
}

// Def returns the definition with id.
func (m *Model) Def(id string) (Def, bool) {
	i, ok := m.byID[id]
	if !ok {
		return Def{}, false
	}
	return m.Defs[i], true
}

// HasPoint reports whether emitted code can raise an event of kind for def at
// site.
func (m *Model) HasPoint(kind, def, site string) bool {
	_, ok := m.points[[3]string{kind, def, site}]
	return ok
}

// PointAt returns the point of kind for def at site.
func (m *Model) PointAt(kind, def, site string) (Point, bool) {
	p, ok := m.points[[3]string{kind, def, site}]
	return p, ok
}

// Block returns the definition ID of a timeout block or a wire literal.
func (m *Model) Block(x Expr) string { return m.blocks[x] }

// hostDefs are the definitions the prelude and the timeout package provide.
var hostDefs = []Def{
	{ID: "host.Console", Kind: DefHost},
	{ID: "host.Console.Println", Kind: DefHost},
	{ID: "host.Scheduler", Kind: DefHost},
	{ID: "host.Scheduler.Sleep", Kind: DefHost},
	{ID: "host.LiveScheduler", Kind: DefHost},
	{ID: "timeout.After", Kind: DefHost},
	{ID: "timeout.Expired", Kind: DefHost},
}

// hostCaps are the prelude capabilities, whose definitions are host.<Name>.
var hostCaps = map[string]bool{"Console": true, "Scheduler": true}

// NewModel derives the definition table of a checked file.
//
// A declaration's ID is <package>.<Name>, a method's <package>.<Type>.<Method>,
// and a block's <enclosing ID>/<construct>#<n>, where n counts that construct
// in the enclosing definition in source order. IDs therefore survive edits
// elsewhere in the file and formatting; the revision pins them to one source.
func NewModel(c *Checked) *Model {
	m := &Model{
		Package: c.File.Package,
		File:    filepath.Base(c.File.Name),
		byID:    map[string]int{},
		blocks:  map[Expr]string{},
		points:  map[[3]string]Point{},
	}
	add := func(d Def) {
		if _, dup := m.byID[d.ID]; dup {
			return
		}
		m.byID[d.ID] = len(m.Defs)
		m.Defs = append(m.Defs, d)
	}
	for _, d := range c.File.Types {
		switch d.Kind {
		case kindCapability:
			add(Def{ID: m.id(d.Name), Kind: DefCapability, Site: m.site(d.Pos)})
			for _, op := range d.Methods {
				add(Def{ID: m.id(d.Name + "." + op.Name), Kind: DefOperation, Site: m.site(op.Pos), Errors: errorNames(m, op.Errors)})
			}
		default:
			add(Def{ID: m.id(d.Name), Kind: DefType, Site: m.site(d.Pos)})
		}
	}
	for _, fn := range c.File.Funcs {
		d := Def{ID: m.funcID(fn), Kind: DefFunc, Site: m.site(fn.Pos), Errors: errorNames(m, fn.Errors)}
		if fn.Recv != nil {
			d.Kind = DefMethod
		}
		add(d)
	}
	for _, w := range c.File.Wires {
		add(Def{ID: m.id(w.Name), Kind: DefWire, Site: m.site(w.Pos)})
	}
	for _, fn := range c.File.Funcs {
		w := &defWalker{m: m, c: c, add: add, in: m.funcID(fn), counts: map[string]int{}}
		w.stmts(fn.Body.Stmts)
	}
	for _, d := range hostDefs {
		add(d)
	}
	return m
}

func (m *Model) id(name string) string { return m.Package + "." + name }

func (m *Model) funcID(fn *FuncDecl) string {
	if fn.Recv != nil {
		return m.id(fn.Recv.Type + "." + fn.Name)
	}
	return m.id(fn.Name)
}

// site formats a position as a trace site.
func (m *Model) site(p Pos) string { return fmt.Sprintf("%s:%d:%d", m.File, p.Line, p.Col) }

// ErrorID is the definition ID of an error type named in a row or a catch.
func (m *Model) ErrorID(name string) string {
	if name == expiredType {
		return expiredType
	}
	return m.id(name)
}

// CapOpID is the definition ID of a capability operation.
func (m *Model) CapOpID(capName, op string) string {
	if hostCaps[capName] {
		return "host." + capName + "." + op
	}
	return m.id(capName + "." + op)
}

func errorNames(m *Model, qs []QName) []string {
	var out []string
	for _, q := range qs {
		if q.Name != "" {
			out = append(out, m.ErrorID(q.String()))
		}
	}
	return out
}

// callSite is where a call expression starts: its receiver or its name.
func callSite(x *Call) Pos {
	switch f := x.Fun.(type) {
	case *Selector:
		return f.X.exprPos()
	case *Ident:
		return f.Pos
	}
	return x.Pos
}

// defWalker visits one function body in source order.
type defWalker struct {
	m      *Model
	c      *Checked
	add    func(Def)
	in     string // the enclosing definition: the function or the innermost block
	counts map[string]int
}

func (w *defWalker) point(kind, def string, p Pos) {
	pt := Point{Kind: kind, Def: def, Site: w.m.site(p), In: w.in}
	if key := [3]string{kind, def, pt.Site}; w.m.points[key] == (Point{}) {
		w.m.points[key] = pt
		w.m.Points = append(w.m.Points, pt)
	}
}

func (w *defWalker) block(construct string, x Expr, p Pos, errs []string, spelling string) string {
	w.counts[construct]++
	id := fmt.Sprintf("%s/%s#%d", w.in, construct, w.counts[construct])
	w.m.blocks[x] = id
	w.add(Def{ID: id, Kind: map[string]string{"timeout.After": DefTimeout, "wire": DefWireLit}[construct], Site: w.m.site(p), In: w.in, Errors: errs, Spelling: spelling})
	return id
}

func (w *defWalker) stmts(list []Stmt) {
	for _, s := range list {
		switch s := s.(type) {
		case *DefineStmt:
			w.expr(s.Value)
		case *ReturnStmt:
			if s.Value != nil {
				w.expr(s.Value)
			}
		case *IfStmt:
			w.expr(s.Cond)
			w.stmts(s.Then.Stmts)
			if s.Else != nil {
				w.stmts(s.Else.Stmts)
			}
		case *TypeSwitchStmt:
			w.expr(s.Subject)
			for _, cs := range s.Cases {
				w.stmts(cs.Body.Stmts)
			}
		case *ExprStmt:
			w.expr(s.X)
		}
	}
}

func (w *defWalker) expr(x Expr) {
	switch x := x.(type) {
	case *Selector:
		w.expr(x.X)
	case *Binary:
		w.expr(x.L)
		w.expr(x.R)
	case *CompositeLit:
		for _, el := range x.Elems {
			w.expr(el.Value)
		}
	case *TryExpr:
		w.expr(x.X)
	case *CatchExpr:
		w.expr(x.X)
		for _, arm := range x.Arms {
			for _, q := range arm.Types {
				w.point("handler", w.m.ErrorID(q.String()), arm.Pos)
			}
			w.expr(arm.Value)
		}
	case *Call:
		w.call(x)
	}
}

func (w *defWalker) call(x *Call) {
	switch fun := x.Fun.(type) {
	case *Ident:
		if fun.Name == "with" {
			if len(x.Args) == 1 {
				switch wl := x.Args[0].(type) {
				case *Ident:
					w.point("construct", w.m.id(wl.Name), fun.Pos)
				case *WireLit:
					for _, en := range wl.Entries {
						w.expr(en.Value)
					}
					if len(wl.Entries) > 0 {
						w.point("construct", w.block("wire", wl, wl.Pos, nil, ""), fun.Pos)
					}
				}
			}
			if x.Block != nil {
				w.stmts(x.Block.Stmts)
			}
			return
		}
		for _, a := range x.Args {
			w.expr(a)
		}
	case *Selector:
		for _, a := range x.Args {
			w.expr(a)
		}
		if w.c.Afters[x] {
			in := w.c.Info[x]
			var errs []string
			for _, e := range in.errs {
				errs = append(errs, w.m.ErrorID(e))
			}
			spelling := "timeout.After"
			if len(x.Args) == 1 {
				if lit, ok := x.Args[0].(*BasicLit); ok {
					spelling += "(" + lit.Text + ")"
				}
			}
			id := w.block("timeout.After", x, callSite(x), errs, spelling)
			outer := w.in
			w.in = id
			w.stmts(x.Block.Stmts)
			w.in = outer
			return
		}
		recv, ok := fun.X.(*Ident)
		if !ok {
			return
		}
		if d := w.c.Types[recv.Name]; d != nil && d.Kind == kindCapability {
			p := callSite(x)
			w.point("call", w.m.CapOpID(recv.Name, fun.Sel), p)
			w.point("interrupt", w.in, p)
		}
	}
}
