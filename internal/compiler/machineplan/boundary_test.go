package machineplan

import (
	"encoding/json"
	"fmt"
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
// arenas, its intern maps or its diagnostics; the Go compiler enforces it.
// Neither it nor the row view imports reflect or unsafe, the two packages
// that can reach memory past Go's export and type rules: unsafe by pointer,
// reflect through a Value's UnsafePointer or settable fields. The fixed sizes
// analysis charges are counted in words and checked against reflect by a
// test (sizes_test.go), and the size of a Plan[V] comes in Input.PlanBytes.
var allowedImports = []string{"encoding/binary", "fmt", "slices", "strconv", "strings", "unicode/utf8", sharedPath}

// sharedImports are what the row view may import.
var sharedImports = []string{"iter", "strings"}

// forbiddenImports reach memory past the export rule; no allowlist may hold
// them.
var forbiddenImports = []string{"reflect", "unsafe"}

// importViolations lists what source, a file of the given directory's
// package, imports beyond allowed.
func importViolations(name string, source any, allowed []string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		switch {
		case slices.Contains(forbiddenImports, path):
			violations = append(violations, fmt.Sprintf("%s imports %s, which reaches memory past the export rule", name, path))
		case !slices.Contains(allowed, path):
			violations = append(violations, fmt.Sprintf("%s imports %s; it may import only %v", name, path, allowed))
		}
	}
	return violations, nil
}

func TestAnalysisImportsNoCheckerState(t *testing.T) {
	for _, list := range [][]string{allowedImports, sharedImports} {
		for _, path := range forbiddenImports {
			if slices.Contains(list, path) {
				t.Fatalf("%s is in an allowlist", path)
			}
		}
	}
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
			// Assembly, C or object files could write memory without an import.
			if !entry.IsDir() && !strings.HasSuffix(name, ".go") {
				t.Errorf("%s holds %s; the package holds only Go files", dir.path, name)
			}
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			files++
			violations, err := importViolations(filepath.Join(dir.path, name), nil, dir.allowed)
			if err != nil {
				t.Fatal(err)
			}
			for _, violation := range violations {
				t.Error(violation)
			}
		}
		if files < dir.least {
			t.Fatalf("found %d package files in %s", files, dir.path)
		}
	}
}

// reviewR8ReflectWrite is review round 8's control, verbatim: it writes a
// shared row's label naming only reflect.TypeFor, reaching a reflect.Value
// through a method chain and its UnsafePointer, with no import of unsafe. It
// compiles; the import rule is what refuses it.
const reviewR8ReflectWrite = `package machineplan

import "reflect"

var reviewR8Stash Row

type reviewR8Getter func(reviewR8Getter) Row

func (reviewR8Getter) Get() Row { return reviewR8Stash }

func reviewR8One[T any](x T) []T { return []T{x} }

func reviewR8Write(r Row) {
	if r.Len() == 0 {
		return
	}
	reviewR8Stash = r
	fn := reflect.TypeFor[reviewR8Getter]().Method(0).Func
	receiver := fn.Convert(reflect.TypeFor[reviewR8Getter]())
	got := fn.Call(reviewR8One(receiver))[0]
	data := got.Field(0).UnsafePointer()
	*(*string)(data) = "written"
}
`

func TestBoundaryRefusesTheReflectWrite(t *testing.T) {
	for _, probe := range []struct {
		name, source, refused string
	}{
		{"review r8 reflect write", reviewR8ReflectWrite, "imports reflect"},
		{"renamed reflect", "package machineplan\n\nimport r \"reflect\"\n\nvar _ = r.TypeFor[int]\n", "imports reflect"},
		{"unsafe", "package machineplan\n\nimport \"unsafe\"\n\nvar _ = unsafe.Sizeof(0)\n", "imports unsafe"},
	} {
		for _, dir := range []struct {
			path    string
			allowed []string
		}{{".", allowedImports}, {"shared", sharedImports}} {
			violations, err := importViolations(filepath.Join(dir.path, "zz_probe.go"), probe.source, dir.allowed)
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) != 1 || !strings.Contains(violations[0], probe.refused) {
				t.Errorf("%s in %s: violations %q, want one that %s", probe.name, dir.path, violations, probe.refused)
			}
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
// labels or returns their slice. Go code reaches another package's unexported
// field only through reflect or unsafe (or assembly, cgo or linkname, which
// need a non-Go file, import "C" or import unsafe), and the import and file
// rules above keep all of them out of this package and of shared. So the
// package's non-test code cannot write a row label or append into a row's
// backing array, whatever alias, conversion or container it goes through: a
// direct write does not compile (TestRowWritesDoNotCompile), and the reflect
// route fails the import rule (TestBoundaryRefusesTheReflectWrite).
// TestSharedRowIsReadOnly pins the view's shape. Test files may import
// reflect; they are not analysis. The checker side, that nothing writes an
// interned label after internRow, is held by convention and checked by the
// window's row digest (clipCheckerState in the compiler's tests).
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
