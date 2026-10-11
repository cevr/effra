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
