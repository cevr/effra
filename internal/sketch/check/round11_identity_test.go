package check

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// structural is the structural identity the interned ids must agree with
// (the checker's identical before class E): a named type by its origin and
// identical type arguments, a row by its set, a signature by its parameters,
// results and rows, an interface by method names, a struct by field names
// and types, a union and a machine only by the object.
func structural(a, b Type) bool {
	if a == b {
		return true
	}
	switch a := a.(type) {
	case *Named:
		b, ok := b.(*Named)
		if !ok || a.origin() != b.origin() || len(a.TArgs) != len(b.TArgs) {
			return false
		}
		for i := range a.TArgs {
			if !structural(a.TArgs[i], b.TArgs[i]) {
				return false
			}
		}
		return true
	case *RowArg:
		b, ok := b.(*RowArg)
		return ok && structuralRow(a.Row, b.Row)
	case *Slice:
		b, ok := b.(*Slice)
		return ok && structural(a.Elem, b.Elem)
	case *Array:
		b, ok := b.(*Array)
		return ok && a.Len == b.Len && structural(a.Elem, b.Elem)
	case *Map:
		b, ok := b.(*Map)
		return ok && structural(a.Key, b.Key) && structural(a.Elem, b.Elem)
	case *Chan:
		b, ok := b.(*Chan)
		return ok && a.Dir == b.Dir && structural(a.Elem, b.Elem)
	case *Pointer:
		b, ok := b.(*Pointer)
		return ok && structural(a.Elem, b.Elem)
	case *Signature:
		b, ok := b.(*Signature)
		if !ok || len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) || a.Variadic != b.Variadic {
			return false
		}
		for i := range a.Params {
			if !structural(a.Params[i].Type, b.Params[i].Type) {
				return false
			}
		}
		for i := range a.Results {
			if !structural(a.Results[i], b.Results[i]) {
				return false
			}
		}
		return structuralRow(a.Errors, b.Errors) && structuralRow(a.Uses, b.Uses)
	case *Interface:
		b, ok := b.(*Interface)
		if !ok || a.Comparable != b.Comparable || len(a.Methods) != len(b.Methods) {
			return false
		}
		for _, m := range a.Methods {
			o := b.method(m.Name)
			if o == nil || !structural(m.Sig, o.Sig) {
				return false
			}
		}
		return true
	case *Struct:
		b, ok := b.(*Struct)
		if !ok || len(a.Fields) != len(b.Fields) {
			return false
		}
		for i := range a.Fields {
			if a.Fields[i].Name != b.Fields[i].Name || !structural(a.Fields[i].Type, b.Fields[i].Type) {
				return false
			}
		}
		return true
	case *Tuple:
		b, ok := b.(*Tuple)
		if !ok || len(a.Types) != len(b.Types) {
			return false
		}
		for i := range a.Types {
			if !structural(a.Types[i], b.Types[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// structuralRow compares two rows as sets, element by element.
func structuralRow(a, b *Row) bool {
	in := func(x *Row, t Type) bool {
		for _, e := range x.Elems() {
			if structural(e, t) {
				return true
			}
		}
		return false
	}
	if len(a.Elems()) != len(b.Elems()) {
		return false
	}
	for _, e := range a.Elems() {
		if !in(b, e) {
			return false
		}
	}
	return true
}

// collectTypes gathers every type the check recorded and their subterms.
func collectTypes(c *checker) []Type {
	seen := map[Type]bool{}
	var out []Type
	var walk func(t Type)
	walk = func(t Type) {
		if t == nil || seen[t] {
			return
		}
		seen[t] = true
		out = append(out, t)
		switch t := t.(type) {
		case *Named:
			for _, a := range t.TArgs {
				walk(a)
			}
		case *RowArg:
			for _, e := range t.Row.Elems() {
				walk(e)
			}
		case *Slice:
			walk(t.Elem)
		case *Array:
			walk(t.Elem)
		case *Map:
			walk(t.Key)
			walk(t.Elem)
		case *Chan:
			walk(t.Elem)
		case *Pointer:
			walk(t.Elem)
		case *Tuple:
			for _, x := range t.Types {
				walk(x)
			}
		case *Struct:
			for _, f := range t.Fields {
				walk(f.Type)
			}
		case *Interface:
			for _, m := range t.Methods {
				walk(m.Sig)
			}
		case *Signature:
			for _, p := range t.Params {
				walk(p.Type)
			}
			for _, r := range t.Results {
				walk(r)
			}
			for _, e := range t.Errors.Elems() {
				walk(e)
			}
			for _, e := range t.Uses.Elems() {
				walk(e)
			}
		}
	}
	for _, t := range c.pkg.Info.Types {
		walk(t)
	}
	for _, s := range c.pkg.Info.Literals {
		walk(s)
	}
	for _, o := range c.pkg.Info.Uses {
		if f, ok := o.(*Func); ok && f.Sig != nil {
			walk(f.Sig)
		}
	}
	for _, inst := range c.instance {
		walk(inst)
		walk(under(inst))
	}
	return out
}

const identitySource = `package main

type Fault struct{}
type Other struct{}
type G[T any] struct{ v T }
type Pair[A any, B any] struct {
	a A
	b B
}
type Box[T any] struct{ f func(T) error[Fault | Other] }
type Shape interface{ V[int] | V[string] }
type V[T any] struct{}
type Store interface{ Get(k string) (int, error[Fault]) }
type Log interface{ Put(s string) }

func a(x G[int], y G[string], z G[int], p Pair[int, G[int]], q Pair[G[int], int]) {}
func b(f func(int) error[Fault | Other], g func(int) error[Other | Fault], h func(int) error[Fault]) {}
func c(s struct{ X int; Y string }, t struct{ X int; Y string }, u struct{ Y string; X int }) {}
func d(i interface{ M(); N(int) }, j interface{ N(int); M() }, k interface{ M() }) {}
func e(m map[string][]G[int], n map[string][]G[int], o [3]chan<- int, p [3]chan int, q <-chan int) {}
func f(w Box[int], v Box[int], u Box[string]) {}
func g(x Shape) int {
	switch x.(type) {
	case V[int]:
		return 1
	default:
		return 0
	}
}
func h() needs Store, Log {}
func k() needs Log, Store {}
func l(fn func() needs Store, gn func() needs Store, hn func() needs Log) {}
`

// TestIdentityMatchesStructure: equal interned ids exactly when the types
// are structurally identical, over every type and subterm of a source with
// generic instances, rows in both orders, permuted interface methods and
// struct fields, channel directions and capability rows, and of every
// fixture.
func TestIdentityMatchesStructure(t *testing.T) {
	sources := map[string]string{"identity": identitySource}
	for _, dir := range []string{"positive", "spec", "negative"} {
		files, _ := filepath.Glob(filepath.Join("testdata", "fixtures", dir, "*.ef"))
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			sources[dir+"/"+filepath.Base(file)] = string(data)
		}
	}
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs, same := 0, 0
	for _, name := range names {
		c, diags := checkFor(sources[name])
		if c == nil {
			t.Fatalf("%s: %v", name, diags)
		}
		if name == "identity" && len(diags) > 0 {
			t.Fatalf("identity source: %v", diags)
		}
		types := collectTypes(c)
		if len(types) > 400 {
			types = types[:400]
		}
		for i, x := range types {
			for _, y := range types[i:] {
				pairs++
				want, have := structural(x, y), c.identical(x, y)
				if want {
					same++
				}
				if want != have {
					t.Errorf("%s: %s and %s: structural %v, interned ids %v", name, x, y, want, have)
				}
			}
		}
	}
	t.Logf("%d sources, %d pairs, %d identical", len(names), pairs, same)
	if same == pairs {
		t.Fatal("no distinct pair compared")
	}
}

// TestTypeStringOnlyPrints: typeString builds a whole type's text, so it is
// for printing only (class E): no comparison, cache or index of the checker
// may key on it. A call outside the printing functions below, or any use of
// typeString as a value, fails.
func TestTypeStringOnlyPrints(t *testing.T) {
	allowed := map[string]bool{
		"typeString": true, // its own recursion
		"Row.list":   true, // the print order of a row's elements
	}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	calls := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			name := "package scope"
			if fd, ok := decl.(*ast.FuncDecl); ok {
				name = funcName(fd)
			}
			callee := map[*ast.Ident]bool{}
			ast.Inspect(decl, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "typeString" {
						callee[id] = true
					}
				}
				return true
			})
			ast.Inspect(decl, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || id.Name != "typeString" {
					return true
				}
				if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name == id {
					return true // the declaration itself
				}
				switch {
				case !callee[id]:
					t.Errorf("%s: %s uses typeString as a value", fset.Position(id.Pos()), name)
				case !allowed[name]:
					t.Errorf("%s: %s calls typeString; compare, cache and index by interned id (intern.go)", fset.Position(id.Pos()), name)
				default:
					calls++
				}
				return true
			})
		}
	}
	if calls == 0 {
		t.Fatal("no typeString call found: the scan reads the wrong files")
	}
}

// TestIdentityEpoch: the rows of a signature under inference change after
// its identity was built. An identity that depends on it holds for one
// epoch; the next epoch rebuilds it from the new rows. Identities that
// depend on no such signature stay memoized.
func TestIdentityEpoch(t *testing.T) {
	c, diags := checkFor("package main\ntype Fault struct{}\ntype G[T any] struct{}\nfunc main() {}\n")
	if len(diags) > 0 {
		t.Fatal(diags)
	}
	fault := c.pkg.Scope.names["Fault"].(*TypeName).Type
	g := c.pkg.Scope.names["G"].(*TypeName).Type.(*Named)
	sig := &Signature{Errors: newRow(), Uses: newRow(), inferred: true}
	box := c.instantiate(g, []Type{sig})
	before := c.types.id(box)
	plain := c.instantiate(g, []Type{Typ[Int]})
	plainID := c.types.id(plain)

	sig.Errors = c.types.row(fault)
	if c.types.id(box) != before {
		t.Fatal("an identity changed inside its epoch")
	}
	c.types.changed()
	after := c.types.id(box)
	if after == before {
		t.Fatal("the identity of G[func() error[Fault]] kept the rows of the previous epoch")
	}
	want := c.instantiate(g, []Type{&Signature{Errors: c.types.row(fault), Uses: newRow()}})
	if c.types.id(want) != after {
		t.Fatal("the rebuilt identity differs from a fresh G[func() error[Fault]]")
	}
	if c.types.id(plain) != plainID {
		t.Fatal("a stable identity changed with the epoch")
	}
}

// funcName is Recv.Name for a method, Name for a function.
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	recv := fd.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	if id, ok := recv.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}
