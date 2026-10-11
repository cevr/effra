package machineplan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The analysis boundary is structural. The package imports only these
// standard packages, so it cannot name the checker, its arenas, its intern
// maps or its diagnostics; the Go compiler enforces it. It does not import
// unsafe, so it cannot forge a pointer into checker memory, and it uses reflect
// only to read type sizes (reflect.TypeFor), never to reach a value.
var allowedImports = []string{"encoding/binary", "fmt", "reflect", "slices", "strconv", "strings", "unicode/utf8"}

// allowedReflect is the only reflect identifier the package may name.
const allowedReflect = "TypeFor"

func TestAnalysisImportsNoCheckerState(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok {
				if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "reflect" && selector.Sel.Name != allowedReflect {
					t.Errorf("%s uses reflect.%s; analysis may use only reflect.%s", name, selector.Sel.Name, allowedReflect)
				}
			}
			return true
		})
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if !slices.Contains(allowedImports, path) {
				t.Errorf("%s imports %s; analysis may import only %v", name, path, allowedImports)
			}
			// The identifier check above sees reflect only by its own name.
			if path == "reflect" && spec.Name != nil {
				t.Errorf("%s imports reflect as %s; analysis imports it only under its own name", name, spec.Name.Name)
			}
		}
	}
	if files < 4 {
		t.Fatalf("found %d package files", files)
	}
}

// The input and the result are plain data: no function, channel, interface or
// unsafe pointer can carry a path back to checker state, and every named type
// they reach is this package's or a predeclared one. V, the checker's value
// type, is opaque to analysis; struct{} stands in for it here.
func TestInputAndResultArePlainData(t *testing.T) {
	for _, root := range []reflect.Type{reflect.TypeFor[Input[struct{}]](), reflect.TypeFor[Result[struct{}]]()} {
		seen := map[reflect.Type]bool{}
		var visit func(path string, typ reflect.Type)
		visit = func(path string, typ reflect.Type) {
			if seen[typ] {
				return
			}
			seen[typ] = true
			if pkg := typ.PkgPath(); pkg != "" && pkg != reflect.TypeFor[TypeRef]().PkgPath() {
				t.Errorf("%s reaches %s from package %s", path, typ, pkg)
			}
			switch typ.Kind() {
			case reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer, reflect.Uintptr:
				t.Errorf("%s is a %s", path, typ.Kind())
			case reflect.Pointer, reflect.Slice, reflect.Array:
				visit(path+"[]", typ.Elem())
			case reflect.Map:
				visit(path+"{key}", typ.Key())
				visit(path+"{}", typ.Elem())
			case reflect.Struct:
				for i := range typ.NumField() {
					field := typ.Field(i)
					visit(path+"."+field.Name, field.Type)
				}
			}
		}
		visit(root.Name(), root)
	}
}

// rowFields name the input's row labels. They are the checker's interned
// rows, shared read-only rather than copied (the input would otherwise hold
// a copy of every distinct row, which the source does not bound), so analysis
// may read them and copy them but never write an element. Their capacity is
// clipped, so an append copies. This check is syntactic: it sees writes
// through a row field, not through a local alias of one, and the window's
// row digest (clipCheckerState) covers what it cannot see.
var rowFields = []string{"Failures", "Services"}

// rowWriters are the calls that write into their first argument.
var rowWriters = []string{"Sort", "Strings", "Stable", "SortFunc", "SortStableFunc", "Reverse", "Compact", "CompactFunc", "Delete", "DeleteFunc", "Insert", "Replace"}

func TestAnalysisWritesNoInputRow(t *testing.T) {
	isRow := func(expr ast.Expr) bool {
		for {
			switch e := expr.(type) {
			case *ast.ParenExpr:
				expr = e.X
			case *ast.SliceExpr:
				expr = e.X
			case *ast.SelectorExpr:
				return slices.Contains(rowFields, e.Sel.Name)
			default:
				return false
			}
		}
	}
	element := func(expr ast.Expr) bool {
		index, ok := expr.(*ast.IndexExpr)
		return ok && isRow(index.X)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			var written bool
			switch n := node.(type) {
			case *ast.AssignStmt:
				written = slices.ContainsFunc(n.Lhs, element)
			case *ast.IncDecStmt:
				written = element(n.X)
			case *ast.UnaryExpr:
				written = n.Op == token.AND && element(n.X)
			case *ast.CallExpr:
				if len(n.Args) > 0 && isRow(n.Args[0]) {
					switch fun := n.Fun.(type) {
					case *ast.Ident:
						written = fun.Name == "copy" || fun.Name == "clear"
					case *ast.SelectorExpr:
						pkg, ok := fun.X.(*ast.Ident)
						written = ok && (pkg.Name == "slices" || pkg.Name == "sort") && slices.Contains(rowWriters, fun.Sel.Name)
					}
				}
			}
			if written {
				t.Errorf("%s writes into a shared input row", fset.Position(node.Pos()))
			}
			return true
		})
	}
}
