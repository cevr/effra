package check

import (
	"sort"

	"ergo.local/prototype/internal/sketch/syntax"
)

// requirementKeyword is the spelling of a capability (requirement) row in
// source. The keyword is the only place the checker names it, so a rename of
// the keyword (#217) touches this constant and the syntax nodes only.
const requirementKeyword = "needs"

// Object is a named entity: a type, a function, a variable, a constant, a
// package, a builtin or a wire.
type Object interface {
	name() string
	pos() syntax.Pos
}

// TypeName is a declared or predeclared type.
type TypeName struct {
	Name string
	Pkg  *Package
	Pos  syntax.Pos
	Type Type
	Decl *syntax.TypeDecl
	file *fileInfo
	id   int
}

// Func is a function or a method.
type Func struct {
	Name  string
	Pkg   *Package
	Pos   syntax.Pos
	Decl  *syntax.FuncDecl
	Recv  *Named // the receiver's type, for a method
	Ptr   bool   // a *T receiver
	Sig   *Signature
	file  *fileInfo
	state int // signature: 0 none, 1 resolving, 2 done

	// Inferred rows: true when the row is computed from the body
	// (rows.md, Capability rows 6; rows-and-constraints §7 amendment 1).
	inferErrors, inferUses bool
	errVar, useVar         *RowVar // the inferred rows' variables (class F)
	called                 bool
}

// Var is a variable: a parameter, a local or a package-level var.
type Var struct {
	Name    string
	Type    Type
	Pos     syntax.Pos
	used    bool
	param   bool
	pkg     bool
	group   *switchGroup // a type-switch or catch binding: used if any arm reads it
	bindRow bool         // a capability-row binding (needs X): an entry of the frame, not a value
}

// Const is a predeclared constant (true, false).
type Const struct {
	Name string
	Type Type
	Val  bool
}

// PkgName is an imported package's name in a file.
type PkgName struct {
	Name string
	Pos  syntax.Pos
	Pkg  *Package
	used bool
	spec *syntax.ImportSpec
	// broken marks an import that failed (reported at the import): its
	// name is bound to an empty package, so a use of it adds nothing (D3).
	broken bool
}

// Builtin is a predeclared function such as len or append, or the with
// construct.
type Builtin struct{ Name string }

// capBinding is a written capability-row binding in a function's frame: the
// bare name of a capability the row lists (rows-and-constraints §7
// amendment 3).
type capBinding struct {
	tn  *TypeName
	at  syntax.Pos
	who string
}

func (o *capBinding) name() string    { return o.tn.Name }
func (o *capBinding) pos() syntax.Pos { return o.at }

// Wire is a named wire.
type Wire struct {
	Name  string
	Pos   syntax.Pos
	Decl  *syntax.WireDecl
	file  *fileInfo
	binds *Row // capabilities the wire binds; nil until resolved
	own   *Row // the wire's own entries (wireSummaries)
	// unknown marks a wire that embeds, directly or not, a wire that does
	// not exist (reported at the embedding): what it binds is unknown, so
	// a with of it discharges everything (D3; M18).
	unknown bool
	pkg     *Package
}

func (o *TypeName) name() string { return o.Name }
func (o *Func) name() string     { return o.Name }
func (o *Var) name() string      { return o.Name }
func (o *Const) name() string    { return o.Name }
func (o *PkgName) name() string  { return o.Name }
func (o *Builtin) name() string  { return o.Name }
func (o *Wire) name() string     { return o.Name }

func (o *TypeName) pos() syntax.Pos { return o.Pos }
func (o *Func) pos() syntax.Pos     { return o.Pos }
func (o *Var) pos() syntax.Pos      { return o.Pos }
func (o *Const) pos() syntax.Pos    { return syntax.NoPos }
func (o *PkgName) pos() syntax.Pos  { return o.Pos }
func (o *Builtin) pos() syntax.Pos  { return syntax.NoPos }
func (o *Wire) pos() syntax.Pos     { return o.Pos }

// switchGroup ties the per-arm variables of one type-switch or catch
// binding: the binding is read when any arm reads it (rows.md, Totality 1).
type switchGroup struct{ used bool }

// Scope is a lexical frame.
type Scope struct {
	parent *Scope
	names  map[string]Object
	// frame marks a function body's frame: its parameters, its written
	// capability-row bindings and its top-level locals
	// (rows-and-constraints §7 amendment 3).
	frame bool
	// barrier marks a with block, a trailing block or a bare block: return,
	// break and continue may not cross it (grammar.md, Statements 5).
	barrier bool
}

func newScope(parent *Scope) *Scope { return &Scope{parent: parent, names: map[string]Object{}} }

func (s *Scope) lookup(name string) (Object, *Scope) {
	for sc := s; sc != nil; sc = sc.parent {
		if o, ok := sc.names[name]; ok {
			return o, sc
		}
	}
	return nil, nil
}

func (s *Scope) insert(name string, o Object) {
	if name == "_" {
		return
	}
	s.names[name] = o
}

// Package is a checked package: its scope, its declarations and the types
// and rows the checker computed.
type Package struct {
	Path  string
	Name  string
	Scope *Scope
	// Wires is the wire namespace: wires are named only by with and by
	// wire embedding, so they do not collide with types (fixture with-name).
	Wires map[string]*Wire
	Funcs []*Func
	Types []*TypeName
	// Info holds the type of every checked expression and the rows of every
	// function and literal.
	Info *Info
	// ErrorTypes are the struct types this package names in an error row
	// (types.md, Error types 1); returning a value of one is a failure.
	ErrorTypes map[*Named]bool

	qualify bool // print its types with the package name
	files   []*fileInfo
}

// Info records the checker's results for later passes (#197, #50).
type Info struct {
	Types    map[syntax.Expr]Type
	Uses     map[*syntax.Ident]Object
	Literals map[syntax.Node]*Signature // function literals and trailing blocks, with their rows
}

// Exported returns the package's exported objects, sorted by name: its
// summary for importers (grammar.md, Visibility 2).
func (p *Package) Exported() []Object {
	var out []Object
	for name, o := range p.Scope.names {
		if isExported(name) {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name() < out[j].name() })
	return out
}

type fileInfo struct {
	syntax  *syntax.File
	index   int
	scope   *Scope // the file's import names
	imports []*PkgName
}

// methods returns the method set of the origin of n, keyed by name.
func (n *Named) method(name string) *Func {
	o := n.origin()
	if o.methods == nil {
		return nil
	}
	return o.methods[name]
}

func (n *Named) addMethod(f *Func) bool {
	if n.methods == nil {
		n.methods = map[string]*Func{}
	}
	if _, dup := n.methods[f.Name]; dup {
		return false
	}
	n.methods[f.Name] = f
	n.mlist = append(n.mlist, f)
	return true
}
