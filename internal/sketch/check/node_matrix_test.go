package check_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"effra.local/prototype/internal/sketch/check"
	"effra.local/prototype/internal/sketch/syntax"
)

// nodePrelude declares what the matrix programs share. Its names are
// exported or bodyless, so none of them is unused.
const nodePrelude = `package main
type A struct{}
type B struct{}
type U interface{ A | B }
type S struct{ X int }
func G(x int) {}
func Work(x int) int { return x }
func Fail(x int) error[A]
`

// nodeMatrix gives every expression, statement and declaration node kind of
// the syntax package a program in which a mistake sits inside a node of that
// kind, and the one diagnostic that mistake must produce: its code and the
// source text of its span. A checker that skipped the kind, or passed it to
// the host unchecked, would lose that diagnostic (spec Risk 1; round-1
// review T1).
var nodeMatrix = []struct {
	kind, src, code, span string
}{
	// Expressions.
	{"ArrayType", `func F(x [2]Nope) {}`, "S104", "Nope"},
	{"BasicLit", `func F() int8 { return 300 }`, "S108", "300"},
	{"BinaryExpr", `func F() int { return 1 + nope }`, "S104", "nope"},
	{"CallExpr", `func F() { G(nope) }`, "S104", "nope"},
	{"CatchExpr", `func F() { Fail(1) catch { case A: G(nope) } }`, "S104", "nope"},
	{"ChanType", `func F(c chan Nope) {}`, "S104", "Nope"},
	{"CompositeLit", `func F() S { return S{nope} }`, "S104", "nope"},
	{"Ellipsis", `func F(xs ...Nope) {}`, "S104", "Nope"},
	{"ErrorRow", `func F() error[Nope]`, "S104", "Nope"},
	{"FuncLit", `func F() { _ = func() { G(nope) } }`, "S104", "nope"},
	{"FuncType", `func F(f func(Nope)) {}`, "S104", "Nope"},
	{"GoExpr", `func F() int { t := go Work(nope); return t.Wait() }`, "S104", "nope"},
	{"Ident", `func F() int { return nope }`, "S104", "nope"},
	{"IndexExpr", `func F(xs []int) int { return xs[nope] }`, "S104", "nope"},
	{"InterfaceType", `type I interface{ M(Nope) }`, "S104", "Nope"},
	{"KeyValueExpr", `func F() S { return S{X: nope} }`, "S104", "nope"},
	{"MachineType", `type M machine[Nope, U] { initial A{}; step Step }
func Step(s U, e U) Decision[U, int] { return Ignore{} }`, "S104", "Nope"},
	{"MapType", `func F(m map[string]Nope) {}`, "S104", "Nope"},
	{"ParenExpr", `func F() int { return (nope) }`, "S104", "nope"},
	{"SelectorExpr", `func F(s S) int { return s.Nope }`, "S104", "Nope"},
	{"SliceExpr", `func F(xs []int) []int { return xs[nope:] }`, "S104", "nope"},
	{"StarExpr", `func F(p *Nope) {}`, "S104", "Nope"},
	{"StructType", `type T struct{ X Nope }`, "S104", "Nope"},
	{"TryExpr", `func F() error[A] { try Fail(nope); return A{} }`, "S104", "nope"},
	{"TupleExpr", `func F(x, y U) { switch _, _ := x.(type), y.(type) { case (A, Nope): default: } }`, "S104", "Nope"},
	{"TypeGuard", `func F() { switch nope.(type) { default: } }`, "S104", "nope"},
	{"UnaryExpr", `func F() int { return -nope }`, "S104", "nope"},
	{"WireLit", `func F() { with(wire{ Nope = S{} }) { G(1) } }`, "S104", "Nope"},
	// Statements.
	{"AssignStmt", `func F() int { x := 1; x = nope; return x }`, "S104", "nope"},
	{"BlockStmt", `func F() { { G(nope) } }`, "S104", "nope"},
	{"BranchStmt", `func F() { for { break nope } }`, "S104", "nope"},
	{"DeclStmt", `func F() int { var x int = nope; return x }`, "S104", "nope"},
	{"DeferStmt", `func F() { defer G(nope) }`, "S104", "nope"},
	{"EmptyStmt", `func F() { L: ; }`, "S110", "L"},
	{"ExprStmt", `func F() { G(nope) }`, "S104", "nope"},
	{"ForStmt", `func F() { for i := 0; i < nope; i++ { G(i) } }`, "S104", "nope"},
	{"IfStmt", `func F() { if nope { G(1) } }`, "S104", "nope"},
	{"IncDecStmt", `func F() { nope++ }`, "S104", "nope"},
	{"LabeledStmt", `func F() { L: for { G(nope); break L } }`, "S104", "nope"},
	{"RangeStmt", `func F() { for range nope { G(1) } }`, "S104", "nope"},
	{"ReturnStmt", `func F() int { return nope }`, "S104", "nope"},
	{"SelectStmt", `func F(ch chan int) { select { case v := <-ch: G(v + nope) } }`, "S104", "nope"},
	{"SendStmt", `func F(ch chan int) { ch <- nope }`, "S104", "nope"},
	{"SwitchStmt", `func F(x int) { switch x { case nope: G(1) } }`, "S104", "nope"},
	{"TypeSwitchStmt", `func F(x U) { switch x.(type) { case A: G(nope); case B: G(1) } }`, "S104", "nope"},
	// Declarations.
	{"FuncDecl", `func F(x Nope) {}`, "S104", "Nope"},
	{"ImportDecl", `import "nope"`, "S104", `"nope"`},
	{"TypeDecl", `type T interface{ Nope }`, "S104", "Nope"},
	{"VarDecl", `var v int = nope`, "S104", "nope"},
	{"WireDecl", `wire W { Nope = S{} }`, "S104", "Nope"},
}

// TestNodeMatrix: each node kind's program contains a node of that kind,
// and checking it yields exactly the expected diagnostic, at the mistake
// inside that node. The matrix covers every node kind ast.go declares.
func TestNodeMatrix(t *testing.T) {
	covered := map[string]bool{}
	for _, m := range nodeMatrix {
		covered[m.kind] = true
		t.Run(m.kind, func(t *testing.T) {
			src := nodePrelude + m.src + "\n"
			if strings.HasPrefix(m.src, "import ") {
				// Imports come before every other declaration.
				src = strings.Replace(nodePrelude, "package main\n", "package main\n"+m.src+"\n", 1)
			}
			file, err := syntax.Parse(src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			kinds := map[string]bool{}
			collectNodeKinds(reflect.ValueOf(file), kinds)
			if !kinds[m.kind] {
				t.Fatalf("the program has no %s node", m.kind)
			}
			_, diags := check.CheckSource(src)
			if len(diags) != 1 {
				t.Fatalf("want one %s at %q, got %s", m.code, m.span, describe(diags))
			}
			d := diags[0]
			if got := src[d.Offset : d.Offset+d.Length]; d.Code != m.code || got != m.span {
				t.Fatalf("want %s at %q, got %s at %q", m.code, m.span, describe(diags), got)
			}
		})
	}
	var missing []string
	for _, k := range syntaxNodeKinds(t) {
		if !covered[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("node kinds without a matrix row: %s", strings.Join(missing, ", "))
	}
}

// syntaxNodeKinds lists the expression, statement and declaration node
// types of the syntax package, from ast.go.
func syntaxNodeKinds(t *testing.T) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join("..", "syntax", "ast.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		switch fn.Name.Name {
		case "exprNode", "stmtNode", "declNode":
		default:
			continue
		}
		if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
			out = append(out, star.X.(*ast.Ident).Name)
		}
	}
	if len(out) < 40 {
		t.Fatalf("found only %d node kinds", len(out))
	}
	return out
}

// collectNodeKinds records the type names of the syntax nodes reachable
// from v.
func collectNodeKinds(v reflect.Value, out map[string]bool) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		if v.Elem().Kind() == reflect.Struct {
			name := v.Elem().Type().Name()
			if out[name+"@"+itoa(v.Pointer())] {
				return
			}
			out[name+"@"+itoa(v.Pointer())] = true
			out[name] = true
		}
		collectNodeKinds(v.Elem(), out)
	case reflect.Interface:
		if !v.IsNil() {
			collectNodeKinds(v.Elem(), out)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				collectNodeKinds(v.Field(i), out)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			collectNodeKinds(v.Index(i), out)
		}
	}
}

func itoa(p uintptr) string {
	const digits = "0123456789abcdef"
	if p == 0 {
		return "0"
	}
	var b []byte
	for p > 0 {
		b = append([]byte{digits[p%16]}, b...)
		p /= 16
	}
	return string(b)
}
