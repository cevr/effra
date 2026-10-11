package machineplan

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// sharedPath is the package that holds the read-only row view.
const sharedPath = "effra.local/prototype/internal/compiler/machineplan/shared"

// The analysis boundary is structural. The package imports only these
// standard packages and the row view, so it cannot name the checker, its
// arenas, its intern maps or its diagnostics; the Go compiler enforces it. It
// does not import unsafe, so it cannot forge a pointer into checker memory,
// and it uses reflect only to read type sizes (reflect.TypeFor), never to
// reach a value.
var allowedImports = []string{"encoding/binary", "fmt", "reflect", "slices", "strconv", "strings", "unicode/utf8", sharedPath}

// sharedImports are what the row view may import: nothing that reaches
// another package's state, and neither reflect nor unsafe.
var sharedImports = []string{"iter", "strings"}

// allowedReflect is the only reflect identifier the package may name.
const allowedReflect = "TypeFor"

func TestAnalysisImportsNoCheckerState(t *testing.T) {
	for _, dir := range []struct {
		path    string
		allowed []string
		least   int
	}{{".", allowedImports, 4}, {"shared", sharedImports, 1}} {
		entries, err := os.ReadDir(dir.path)
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
			name = filepath.Join(dir.path, name)
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
				if !slices.Contains(dir.allowed, path) {
					t.Errorf("%s imports %s; it may import only %v", name, path, dir.allowed)
				}
				// The identifier check above sees reflect only by its own name.
				if path == "reflect" && spec.Name != nil {
					t.Errorf("%s imports reflect as %s; analysis imports it only under its own name", name, spec.Name.Name)
				}
			}
		}
		if files < dir.least {
			t.Fatalf("found %d package files in %s", files, dir.path)
		}
	}
}

// The input and the result are plain data: no function, channel, interface or
// unsafe pointer can carry a path back to checker state, and every named type
// they reach is this package's, the row view's or a predeclared one. V, the
// checker's value type, is opaque to analysis; struct{} stands in for it here.
func TestInputAndResultArePlainData(t *testing.T) {
	for _, root := range []reflect.Type{reflect.TypeFor[Input[struct{}]](), reflect.TypeFor[Result[struct{}]]()} {
		seen := map[reflect.Type]bool{}
		var visit func(path string, typ reflect.Type)
		visit = func(path string, typ reflect.Type) {
			if seen[typ] {
				return
			}
			seen[typ] = true
			if pkg := typ.PkgPath(); pkg != "" && pkg != reflect.TypeFor[TypeRef]().PkgPath() && pkg != sharedPath {
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

// What holds for the input's rows. They are the checker's interned labels,
// shared rather than copied (the input would otherwise hold a copy of every
// distinct row, which the source does not bound). Analysis sees each as a Row,
// a shared.Row: its slice is an unexported field of package shared, and
// shared exposes only the reads and copies below, none of which writes the
// labels or returns their slice. So no code in this package can write a row
// label or append into a row's backing array, whatever alias, conversion or
// container it goes through: the Go compiler refuses it. TestSharedRowIsReadOnly
// pins the view's shape, and TestRowWritesDoNotCompile builds probes that try.
// The checker side, that nothing writes an interned label after internRow, is
// held by convention and checked by the window's row digest
// (clipCheckerState in the compiler's tests).
var rowMethods = []string{"All", "AppendTo", "At", "Bytes", "Join", "Len"}

func TestSharedRowIsReadOnly(t *testing.T) {
	row := reflect.TypeFor[Row]()
	if row.PkgPath() != sharedPath || row.Kind() != reflect.Struct || row.NumField() != 1 {
		t.Fatalf("Row is %s %s with %d fields; want a one-field struct of %s", row.PkgPath(), row.Kind(), row.NumField(), sharedPath)
	}
	if field := row.Field(0); field.IsExported() || field.Type != reflect.TypeFor[[]string]() {
		t.Fatalf("Row's field is %s %s; want an unexported []string", field.Name, field.Type)
	}
	var methods []string
	for i := range row.NumMethod() {
		methods = append(methods, row.Method(i).Name)
	}
	if !slices.Equal(methods, rowMethods) {
		t.Fatalf("Row's methods are %v; want %v (a new method must not return or write the labels)", methods, rowMethods)
	}
	if pointer := reflect.TypeFor[*Row](); pointer.NumMethod() != len(rowMethods) {
		t.Fatalf("*Row has %d methods; Row's methods all take a value", pointer.NumMethod())
	}
}

// rowProbes are files that try to write a shared row, each with the compiler
// error that must refuse it. G1-G3 are the writes that got past round 7's
// syntactic check; the last probe reads through the view and must compile, so
// a refusal above is the type's doing, not the harness's.
var rowProbes = []struct {
	name, source, refusal string
}{
	{"G1 append into a row's backing array", `func probe(f *Function) { _ = append(f.Failures[:0], "x") }`, "cannot slice"},
	{"G2 conversion-wrapped sort", `func probe(f *Function) { sort.Sort(sort.Reverse(sort.StringSlice(f.Failures))) }`, "cannot convert"},
	{"G3 write through an array of rows", `func probe(n *TypeNode) {
	for _, row := range [2][]string{n.Failures, n.Services} {
		row[0] = "x"
	}
}`, "cannot use"},
	{"conversion to a slice", `func probe(n *TypeNode) { slices.Sort([]string(n.Services)) }`, "cannot convert"},
	{"index assignment", `func probe(v *InitialValue) { v.Failures[0] = "x" }`, "cannot index"},
	{"unexported field", `func probe(n *TypeNode) { n.Failures.labels[0] = "x" }`, "labels"},
	{"read through the view", `func probe(n *TypeNode) (int, []string, string) {
	total := n.Failures.Len() + n.Services.Bytes()
	for label := range n.Failures.All() {
		total += len(label)
	}
	copied := n.Services.AppendTo(nil)
	slices.Sort(copied)
	return total, copied, n.Failures.Join(", ") + n.Failures.At(0)
}`, ""},
}

func TestRowWritesDoNotCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the package once per probe")
	}
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range rowProbes {
		t.Run(probe.name, func(t *testing.T) {
			t.Parallel()
			scratch := t.TempDir()
			source := filepath.Join(scratch, "probe.go")
			text := "package machineplan\n\nimport (\n\t\"slices\"\n\t\"sort\"\n)\n\nvar _ = slices.Sort[[]string]\nvar _ = sort.Sort\n\n" + probe.source + "\n"
			if err := os.WriteFile(source, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			overlay, err := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(dir, "zz_row_probe.go"): source}})
			if err != nil {
				t.Fatal(err)
			}
			overlayPath := filepath.Join(scratch, "overlay.json")
			if err := os.WriteFile(overlayPath, overlay, 0o644); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("go", "build", "-overlay="+overlayPath, ".")
			command.Dir = dir
			output, err := command.CombinedOutput()
			switch {
			case probe.refusal == "" && err != nil:
				t.Fatalf("the read probe does not compile: %v\n%s", err, output)
			case probe.refusal == "" && err == nil:
			case err == nil:
				t.Fatalf("the probe compiled; a Row must refuse it:\n%s", probe.source)
			// The compiler reports the overlay's source path. The read probe
			// shows that the file's header compiles.
			case !strings.Contains(string(output), source+":") || !strings.Contains(string(output), probe.refusal):
				t.Fatalf("the probe failed, but not with %q in the probe:\n%s", probe.refusal, output)
			}
		})
	}
}
