package compiler

import (
	"errors"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const initProbePackage = "effra.fixture/initprobe/registry"

// writeInitProbeModule writes a local Go module whose registry package and
// its transitive dependency report their package initialization on stdout.
// Effra sources compiled against this directory resolve it as the main
// module, so the generated module replaces it by directory.
func writeInitProbeModule(t *testing.T) string {
	t.Helper()
	return writeGoModule(t, map[string]string{
		"go.mod": "module effra.fixture/initprobe\n\ngo 1.27\n",
		"dependency/dependency.go": `package dependency

import "fmt"

var Ready = announce("dependency-init")

func announce(text string) bool { fmt.Println(text); return true }
`,
		"registry/registry.go": `package registry

import (
	"fmt"

	"effra.fixture/initprobe/dependency"
)

func init() {
	if !dependency.Ready {
		panic("dependency initialized after its importer")
	}
	fmt.Println("registry-init")
}

func Value() int64 { fmt.Println("foreign-call"); return 7 }
`,
		// Go refuses both packages as imports of the generated program,
		// although go list loads them as command-line packages.
		"internal/hidden/hidden.go": "package hidden\n\nfunc Value() int64 { return 1 }\n",
		"cmd/tool/main.go":          "package main\n\nfunc Value() int64 { return 1 }\n\nfunc main() {}\n",
	})
}

func writeGoModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// emittedImportSpecs lists the generated file's import specs as name/path
// pairs; an unnamed import has an empty name.
func emittedImportSpecs(t *testing.T, code []byte) [][2]string {
	t.Helper()
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "main.go", code, goparser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	specs := [][2]string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}
		specs = append(specs, [2]string{name, path})
	}
	return specs
}

func importsOf(specs [][2]string, path string) []string {
	names := []string{}
	for _, spec := range specs {
		if spec[1] == path {
			names = append(names, spec[0])
		}
	}
	return names
}

func generatedFunctionNames(t *testing.T, code []byte) map[string]bool {
	t.Helper()
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "main.go", code, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, declaration := range file.Decls {
		if f, ok := declaration.(*ast.FuncDecl); ok {
			names[f.Name.Name] = true
		}
	}
	return names
}

// buildAndRunInitProbe compiles source against the probe module, lowers one
// native mode, builds the generated module and returns its stdout.
func buildAndRunInitProbe(t *testing.T, module, source string, mode GoGenerationMode) (*Result, *GoApplication, string) {
	t.Helper()
	r := CompileAt(source, "go", module)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	application, err := r.GoApplication(mode)
	if err != nil {
		t.Fatal(err)
	}
	directory := writeGoApplication(t, r, application)
	binary := filepath.Join(directory, "application")
	if output, err := runGoCommand(directory, "build", "-mod=readonly", "-o", binary, "."); err != nil {
		t.Fatalf("generated application did not build: %v\n%s\n%s", err, output, application.Main)
	}
	command := exec.Command(binary)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		t.Fatalf("generated application failed: %v\n%s", err, output)
	}
	return r, application, string(output)
}

const unreachableForeignCaller = `import go registry "effra.fixture/initprobe/registry"
effect fn unused() -> i64 uses { Foreign } {
    run registry.Value()
}
effect fn main() -> void {
    void
}
`

func TestDeclaredForeignImportInitializesWithoutReachableCaller(t *testing.T) {
	module := writeInitProbeModule(t)
	for name, source := range map[string]string{
		"unreachable caller": unreachableForeignCaller,
		"no caller": `import go registry "effra.fixture/initprobe/registry"
effect fn main() -> void {
    void
}
`,
	} {
		t.Run(name, func(t *testing.T) {
			r, application, output := buildAndRunInitProbe(t, module, source, GoGenerationBuild)
			if output != "dependency-init\nregistry-init\n" {
				t.Fatalf("declared import initialization = %q", output)
			}
			if names := importsOf(emittedImportSpecs(t, application.Main), initProbePackage); len(names) != 1 || names[0] != "_" {
				t.Fatalf("an initialization-only package needs exactly one blank import: %v", names)
			}
			plan, err := r.ApplicationPlan(GoGenerationBuild)
			if err != nil {
				t.Fatal(err)
			}
			requireProvenance(t, plan, RequiresGoInitialization, initProbePackage, "", "declared-foreign-import")
			requireUnplanned(t, plan, RequiresGoImport, initProbePackage)
			requireUnplanned(t, plan, RequiresForeign, "go:"+initProbePackage+".Value")
			requireUnplanned(t, plan, RequiresService, serviceIdentity("Foreign"))
			requireUnplanned(t, plan, RequiresHelper, "foreign")
			if r.Find("unused") != nil {
				requireUnplanned(t, plan, RequiresFunction, symbolIdentity(t, r, "unused"))
				if generatedFunctionNames(t, application.Main)["efFunction_unused"] {
					t.Fatal("initialization retained the dead foreign caller")
				}
			}
		})
	}
}

func TestReachableForeignCallKeepsOneNamedImport(t *testing.T) {
	module := writeInitProbeModule(t)
	source := strings.Replace(unreachableForeignCaller, `effect fn main() -> void {
    void
}`, `effect fn main() -> i64 {
    run unused().provide<Foreign>(Host)
}`, 1)
	r, application, output := buildAndRunInitProbe(t, module, source, GoGenerationBuild)
	if !strings.HasPrefix(output, "dependency-init\nregistry-init\nforeign-call\n") {
		t.Fatalf("initialization must precede the entry: %q", output)
	}
	if names := importsOf(emittedImportSpecs(t, application.Main), initProbePackage); len(names) != 1 || names[0] != "efGo_registry" {
		t.Fatalf("a referenced package keeps its named import and no blank duplicate: %v", names)
	}
	plan, err := r.ApplicationPlan(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	// Both explanations are retained: the declaration roots initialization
	// and the reachable call requires the named import.
	requireProvenance(t, plan, RequiresGoInitialization, initProbePackage, "", "declared-foreign-import")
	requireProvenance(t, plan, RequiresGoImport, initProbePackage, "go:"+initProbePackage+".Value", "foreign-call")
}

func TestAliasedForeignImportsInitializeTheirPackageOnce(t *testing.T) {
	module := writeInitProbeModule(t)
	twoAliases := `import go first "effra.fixture/initprobe/registry"
import go second "effra.fixture/initprobe/registry"
effect fn viaSecond() -> i64 uses { Foreign } {
    run second.Value()
}
effect fn main() -> void {
    void
}
`
	_, application, output := buildAndRunInitProbe(t, module, twoAliases, GoGenerationBuild)
	if output != "dependency-init\nregistry-init\n" {
		t.Fatalf("aliased imports initialization = %q", output)
	}
	if names := importsOf(emittedImportSpecs(t, application.Main), initProbePackage); len(names) != 1 || names[0] != "_" {
		t.Fatalf("aliases of one package need one blank import: %v", names)
	}
	used := strings.Replace(twoAliases, `effect fn main() -> void {
    void
}`, `effect fn main() -> i64 {
    run viaSecond().provide<Foreign>(Host)
}`, 1)
	_, application, output = buildAndRunInitProbe(t, module, used, GoGenerationBuild)
	if output != "dependency-init\nregistry-init\nforeign-call\n7\n" {
		t.Fatalf("aliased imports with one use = %q", output)
	}
	if names := importsOf(emittedImportSpecs(t, application.Main), initProbePackage); len(names) != 1 || names[0] != "efGo_second" {
		t.Fatalf("only the referenced alias is emitted, without a blank duplicate: %v", names)
	}
}

func TestRemovedForeignImportLosesItsInitializationRoot(t *testing.T) {
	module := writeInitProbeModule(t)
	r, application, output := buildAndRunInitProbe(t, module, "effect fn main() -> void {\n    void\n}\n", GoGenerationBuild)
	if output != "" {
		t.Fatalf("a source without the import still initialized it: %q", output)
	}
	if names := importsOf(emittedImportSpecs(t, application.Main), initProbePackage); len(names) != 0 {
		t.Fatalf("stale import of a removed declaration: %v", names)
	}
	plan, err := r.ApplicationPlan(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if roots := plan.Identities(RequiresGoInitialization); len(roots) != 0 {
		t.Fatalf("initialization roots without a declaration: %v", roots)
	}
}

func TestDeclaredForeignImportInitializesBeforeTestCases(t *testing.T) {
	module := writeInitProbeModule(t)
	source := `import go registry "effra.fixture/initprobe/registry"
effect fn unused() -> i64 uses { Foreign } {
    run registry.Value()
}
effect fn test_runs() -> void raises { AssertionFailed } uses { Assert } {
    run Assert.check(true, "runs")
}
`
	r := CompileAt(source, "go", module)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	// Admission without --live refuses before any generated program exists.
	if err := r.TestMode(false); err == nil || !strings.Contains(err.Error(), "--live") {
		t.Fatalf("Go imports must require test --live: %v", err)
	}
	if err := r.TestMode(true); err != nil {
		t.Fatal(err)
	}
	_, application, output := buildAndRunInitProbe(t, module, source, GoGenerationTest)
	if !strings.HasPrefix(output, "dependency-init\nregistry-init\n") || strings.Contains(output, "foreign-call") {
		t.Fatalf("initialization must precede the harness without calling the dead binding: %q", output)
	}
	if names := importsOf(emittedImportSpecs(t, application.Main), initProbePackage); len(names) != 1 || names[0] != "_" {
		t.Fatalf("test application import = %v", names)
	}
}

func TestDeclaredForeignImportRefusesJavaScriptBeforeHostLoading(t *testing.T) {
	module := writeInitProbeModule(t)
	r := CompileAt("import go registry \"effra.fixture/initprobe/registry\"\neffect fn main() -> void {\n    void\n}\n", "js", module)
	if r.Checked || !hasCode(r, "EF110") || r.Timings.ImportMicros != 0 {
		t.Fatalf("JavaScript must refuse an uncalled Go import before host loading: checked=%v micros=%d %v", r.Checked, r.Timings.ImportMicros, r.Diagnostics)
	}
	if _, _, err := r.Emit(true); err == nil {
		t.Fatal("JavaScript emitted a program with a Go import")
	}
}

func TestUnresolvedUncalledForeignImportIsRefused(t *testing.T) {
	module := writeInitProbeModule(t)
	r := CompileAt("import go missing \"effra.fixture/initprobe/missing\"\neffect fn main() -> void {\n    void\n}\n", "go", module)
	if r.Checked || !hasCode(r, "EF111") {
		t.Fatalf("an unresolved uncalled import must not be admitted: %v", r.Diagnostics)
	}
}

// TestDeclaredForeignImportsMustBeImportableByTheGeneratedProgram applies
// Go's import rules for the generated main package at admission: every
// declared import is emitted, so a package Go refuses to import is an EF111
// at its declaration whether or not a call names it.
func TestDeclaredForeignImportsMustBeImportableByTheGeneratedProgram(t *testing.T) {
	module := writeInitProbeModule(t)
	// An internal element is visible below its parent. A module path that
	// starts with internal has the empty parent, so its registry package is
	// visible to every importer, the generated program included, while its
	// own internal/hidden package is not.
	visible := writeGoModule(t, map[string]string{
		"go.mod":                    "module internal/initprobe\n\ngo 1.27\n",
		"registry/registry.go":      "package registry\n\nimport \"fmt\"\n\nfunc init() { fmt.Println(\"visible-init\") }\n\nfunc Value() int64 { fmt.Println(\"foreign-call\"); return 7 }\n",
		"internal/hidden/hidden.go": "package hidden\n\nfunc Value() int64 { return 1 }\n",
	})
	callers := map[string]string{
		"no caller":        "effect fn main() -> void {\n    void\n}\n",
		"dead caller":      "effect fn unused() -> i64 uses { Foreign } {\n    run probe.Value()\n}\neffect fn main() -> void {\n    void\n}\n",
		"reachable caller": "effect fn main() -> i64 {\n    run probe.Value().provide<Foreign>(Host)\n}\n",
	}
	for _, refused := range []struct{ name, module, path string }{
		{"program package", module, "effra.fixture/initprobe/cmd/tool"},
		{"internal package of the source module", module, "effra.fixture/initprobe/internal/hidden"},
		{"final internal element", visible, "internal/initprobe/internal/hidden"},
	} {
		for caller, body := range callers {
			t.Run(refused.name+"/"+caller, func(t *testing.T) {
				r := CompileAt("import go probe "+strconv.Quote(refused.path)+"\n"+body, "go", refused.module)
				if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF111" || len(r.Program.Imports) != 1 || r.Diagnostics[0].Span != r.Program.Imports[0].Span {
					t.Fatalf("a package the generated program cannot import must be refused at its declaration only: %+v", r.Diagnostics)
				}
				if inspections := r.ApplicationInspections(); len(inspections) != 0 {
					t.Fatalf("a refused import reported application facts: %+v", inspections)
				}
			})
		}
	}
	for name, path := range map[string]string{
		"standard internal package": "internal/abi",
		"vendored standard package": "vendor/golang.org/x/net/dns/dnsmessage",
	} {
		t.Run(name, func(t *testing.T) {
			r := CompileAt("import go probe "+strconv.Quote(path)+"\n"+callers["no caller"], "go", module)
			if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF111" || r.Diagnostics[0].Span != r.Program.Imports[0].Span {
				t.Fatalf("a standard package hidden from the generated program must be refused: %+v", r.Diagnostics)
			}
		})
	}
	// Positive control: Go's rule, not a substring match, decides.
	for caller, want := range map[string]string{"no caller": "visible-init\n", "reachable caller": "visible-init\nforeign-call\n7\n"} {
		t.Run("visible internal package/"+caller, func(t *testing.T) {
			_, _, output := buildAndRunInitProbe(t, visible, "import go probe \"internal/initprobe/registry\"\n"+callers[caller], GoGenerationBuild)
			if output != want {
				t.Fatalf("visible internal package output = %q", output)
			}
		})
	}
}

func TestInitializationRootsAreChargedToThePlanBudget(t *testing.T) {
	module := writeInitProbeModule(t)
	r := CompileAt(unreachableForeignCaller, "go", module)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	plan, err := r.ApplicationPlan(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.applicationPlan(GoGenerationBuild, plan.Work); err != nil {
		t.Fatalf("exact budget refused: %v", err)
	}
	refused, err := r.applicationPlan(GoGenerationBuild, plan.Work-1)
	var refusal *ApplicationPlanError
	if refused != nil || err == nil || !errors.As(err, &refusal) || refusal.Code != applicationPlanExhaustedCode {
		t.Fatalf("one unit short must refuse without a partial plan: %v %v", refused, err)
	}
}
