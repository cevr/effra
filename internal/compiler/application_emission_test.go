package compiler

import (
	"bufio"
	"errors"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

// emitGoApplication lowers one native entry mode and returns its generated
// main with the application whose plan selects the runtime to write. Host
// roots declare the generated declarations a Go probe calls directly.
func emitGoApplication(r *Result, mode GoGenerationMode, hostRoots ...applicationHostRoot) (string, *GoApplication, error) {
	application, err := r.goApplication(mode, hostRoots...)
	if err != nil {
		return "", nil, err
	}
	return string(application.Main), application, nil
}

func hostFunction(name string) applicationHostRoot {
	return applicationHostRoot{kind: RequiresFunction, name: name}
}

func hostHelper(name string) applicationHostRoot {
	return applicationHostRoot{kind: RequiresHelper, name: name}
}

// emittedGoDeclarations lists the generated file's top-level declaration
// names and import paths in source order.
func emittedGoDeclarations(t *testing.T, code []byte) ([]string, []string) {
	t.Helper()
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "main.go", code, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			names = append(names, declaration.Name.Name)
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				if spec, ok := spec.(*ast.TypeSpec); ok {
					names = append(names, spec.Name.Name)
				}
			}
		}
	}
	imports := []string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		imports = append(imports, path)
	}
	slices.Sort(imports)
	return names, imports
}

// writeGoApplication writes a complete generated module for application.
func writeGoApplication(t *testing.T, r *Result, application *GoApplication) string {
	t.Helper()
	directory := t.TempDir()
	if err := application.WriteRuntime(directory); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"go.mod": r.ModuleFile(), "go.sum": r.ModuleSum, "main.go": application.Main} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

const selectiveEmissionSource = `import go strings "strings"
import go strconv "strconv"
record Greeting {
    text: string
}
record Unused {
    name: string
}
enum Mode {
    On
    Off
}
error Broken
error Missing
service Store {
    effect fn read() -> string raises { Broken }
}
service Audit {
    effect fn note(entry: string) -> void
}
impl Memory for Store {
    effect fn read() -> string raises { Broken } {
        fail Broken
    }
}
impl Quiet for Audit {
    effect fn note(entry: string) -> void {
        void
    }
}
layer Stores {
    Store = Memory
}
layer Audits {
    Audit = Quiet
}
fn helper(value: Unused) -> string {
    value.name
}
effect fn shout(text: string) -> string uses { Foreign } {
    run strings.ToUpper(text)
}
effect fn parse(text: string) -> bool raises { GoError } uses { Foreign } {
    run strconv.ParseBool(text).orFail()
}
effect fn read() -> string raises { Broken } uses { Store } {
    run Store.read()
}
effect fn main() -> string {
    let greeting = Greeting {
        text: "recovered"
    }
    let parsed = run parse("true").provide<Foreign>(Host).catch<GoError>(false)
    run read().provide(Stores).catch<Broken>(greeting.text)
}
`

func TestGoApplicationEmitsExactlyPlannedDeclarations(t *testing.T) {
	r := Compile(selectiveEmissionSource)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	application, err := r.GoApplication(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	names, imports := emittedGoDeclarations(t, application.Main)
	want := []string{
		"efExit", "efEffect", "efToRuntime", "efFromRuntime", "efCatch",
		"efType_Broken", "efType_Greeting",
		"efContext",
		"efService_Foreign", "efProvide_Foreign",
		"efService_Store", "efProvide_Store", "efCall_Store_read",
		"efProvider_Host", "efProvider_Memory",
		"efLayerState_Stores", "efLayerOutputs_Stores", "efLayer_Stores",
		"efFunction_parse", "efFunction_read", "efFunction_main",
		"main",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("emitted declarations =\n%v\nwant\n%v", names, want)
	}
	if want := []string{"context", "effra.generated/runtime", "fmt", "os", "os/signal", "strconv", "syscall"}; !slices.Equal(imports, want) {
		t.Fatalf("emitted imports = %v, want %v", imports, want)
	}
	modules, err := application.Plan.RuntimeModuleClosure()
	if err != nil {
		t.Fatal(err)
	}
	if want := []rt.RuntimeModule{rt.RuntimeModuleCore, rt.RuntimeModuleInterop, rt.RuntimeModuleLayers}; !slices.Equal(modules, want) {
		t.Fatalf("runtime modules = %v, want %v", modules, want)
	}
	again, err := Compile(selectiveEmissionSource).GoApplication(GoGenerationBuild)
	if err != nil || string(again.Main) != string(application.Main) {
		t.Fatalf("fresh compilation emitted a different application: %v", err)
	}

	directory := writeGoApplication(t, r, application)
	if got, want := runtimeDirectoryFiles(t, directory), []string{"effect.go", "fiber.go", "interop.go", "layers.go", "managed.go", "scheduler.go", "scope.go"}; !slices.Equal(got, want) {
		t.Fatalf("written runtime = %v, want %v", got, want)
	}
	if output, err := runGoCommand(directory, "run", "."); err != nil || string(output) != "recovered\n" {
		t.Fatalf("selected application did not run: %v\n%s", err, output)
	}
}

func runtimeDirectoryFiles(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(directory, "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestGoTestApplicationRetainsHarnessFixtures(t *testing.T) {
	r := Compile(`effect fn main() -> string {
    "ok"
}
effect fn test_assertion() -> void raises { AssertionFailed } uses { Assert } {
    run Assert.check(true, "holds")
}
`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	build, err := r.GoApplication(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	test, err := r.GoApplication(GoGenerationTest)
	if err != nil {
		t.Fatal(err)
	}
	buildNames, buildImports := emittedGoDeclarations(t, build.Main)
	testNames, testImports := emittedGoDeclarations(t, test.Main)
	if slices.Contains(buildImports, "encoding/json") || !slices.Contains(testImports, "encoding/json") {
		t.Fatalf("encoding/json must be test-only: build=%v test=%v", buildImports, testImports)
	}
	for _, name := range testHarnessProviders {
		provider := r.checkedProviders[name]
		for _, declaration := range []string{"efProvider_" + name, "efService_" + provider.Service} {
			if !slices.Contains(testNames, declaration) {
				t.Fatalf("test application omitted harness declaration %s: %v", declaration, testNames)
			}
			if slices.Contains(buildNames, declaration) {
				t.Fatalf("ordinary application retained harness declaration %s", declaration)
			}
		}
	}
	modules, err := test.Plan.RuntimeModuleClosure()
	if err != nil {
		t.Fatal(err)
	}
	if want := []rt.RuntimeModule{rt.RuntimeModuleCore, rt.RuntimeModuleSync}; !slices.Equal(modules, want) {
		t.Fatalf("test runtime modules = %v, want %v", modules, want)
	}
	directory := writeGoApplication(t, r, test)
	if output, err := runGoCommand(directory, "run", "."); err != nil || !strings.Contains(string(output), `"passed":true`) {
		t.Fatalf("test application did not pass: %v\n%s", err, output)
	}
}

// Every authored example builds against exactly its plan's runtime selection.
// The Go compiler is the coherence oracle: a declaration referring to an
// unselected runtime module cannot build.
func TestAuthoredExamplesBuildAgainstSelectedRuntime(t *testing.T) {
	list, err := os.Open("../../scripts/authored_examples.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer list.Close()
	scanner := bufio.NewScanner(list)
	built := 0
	for scanner.Scan() {
		path := "../../" + scanner.Text()
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		r := CompileAt(string(source), "go", filepath.Dir(path))
		if !r.Checked {
			continue
		}
		modes := []GoGenerationMode{}
		if r.Entry() == nil {
			modes = append(modes, GoGenerationBuild)
		}
		if _, err := r.Tests(); err == nil {
			modes = append(modes, GoGenerationTest)
		}
		for _, mode := range modes {
			built++
			t.Run(filepath.Base(path)+"/"+string(mode), func(t *testing.T) {
				t.Parallel()
				application, err := r.GoApplication(mode)
				if err != nil {
					t.Fatal(err)
				}
				selected, err := application.RuntimeSources()
				if err != nil {
					t.Fatal(err)
				}
				directory := writeGoApplication(t, r, application)
				if got, want := runtimeDirectoryFiles(t, directory), slices.Sorted(maps.Keys(selected)); !slices.Equal(got, want) {
					t.Fatalf("written runtime = %v, want %v", got, want)
				}
				if output, err := runGoCommand(directory, "build", "-mod=readonly", "-o", filepath.Join(directory, "application"), "."); err != nil {
					t.Fatalf("selected application did not build: %v\n%s", err, output)
				}
			})
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if built == 0 {
		t.Fatal("no authored example declares a native application")
	}
}

func TestApplicationPlanRefusalIsADiagnosticAndEmitsNothing(t *testing.T) {
	r := Compile(applicationDAGSource(4))
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, err := r.applicationPlan(GoGenerationBuild, 3)
	var refusal *ApplicationPlanError
	if !errors.As(err, &refusal) || refusal.Diagnostic().Code != applicationPlanExhaustedCode {
		t.Fatalf("exhausted plan = %v", err)
	}
	if _, err := r.goApplication(GoGenerationBuild); err != nil {
		t.Fatalf("complete application refused: %v", err)
	}
}

func TestGoApplicationWriteRuntimeRefusesUnselectedFiles(t *testing.T) {
	r := Compile(`effect fn main() -> string {
    "minimal"
}
`)
	application, err := r.GoApplication(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	stale := filepath.Join(directory, "runtime", "http.go")
	if err := os.MkdirAll(filepath.Dir(stale), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("package runtime\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := application.WriteRuntime(directory); err == nil || !strings.Contains(err.Error(), "unselected http.go") {
		t.Fatalf("stale runtime source was admitted: %v", err)
	}
	if data, err := os.ReadFile(stale); err != nil || string(data) != "package runtime\n" {
		t.Fatalf("refusal changed the unknown file: %v %q", err, data)
	}
}

func TestGoSourceSnapshotRequiresThisResultsApplication(t *testing.T) {
	source := `effect fn main() -> string {
    "minimal"
}
`
	first, second := Compile(source), Compile(source)
	application, err := first.GoApplication(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.GoSourceSnapshot("/origin/main.ef", application); err == nil {
		t.Fatal("snapshot accepted an application planned from another result")
	}
	snapshot, err := first.GoSourceSnapshot("/origin/main.ef", application)
	if err != nil {
		t.Fatal(err)
	}
	core, err := rt.SelectSources(rt.RuntimeModuleCore)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mode != GoGenerationBuild || !slices.Equal(slices.Sorted(maps.Keys(snapshot.Runtime)), slices.Sorted(maps.Keys(core))) {
		t.Fatalf("snapshot mode=%s runtime=%v", snapshot.Mode, slices.Sorted(maps.Keys(snapshot.Runtime)))
	}
}
