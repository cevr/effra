package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
)

const initProbeRegistry = "effra.fixture/initprobe/registry"

const initProbeApplication = `import go registry "effra.fixture/initprobe/registry"
effect fn unused() -> i64 uses { Foreign } {
    run registry.Value()
}
effect fn main() -> void {
    void
}
`

const initProbeTests = `import go registry "effra.fixture/initprobe/registry"
effect fn unused() -> i64 uses { Foreign } {
    run registry.Value()
}
effect fn test_runs() -> void raises { AssertionFailed } uses { Assert } {
    run Assert.check(true, "runs")
}
`

// writeInitProbeRoot writes a Go module whose registry package and its
// dependency print during package initialization. Effra sources placed in
// the root resolve it as their main module.
func writeInitProbeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":                   "module effra.fixture/initprobe\n\ngo 1.27\n",
		"dependency/dependency.go": "package dependency\n\nimport \"fmt\"\n\nvar Ready = announce(\"dependency-init\")\n\nfunc announce(text string) bool { fmt.Println(text); return true }\n",
		"registry/registry.go":     "package registry\n\nimport (\n\t\"fmt\"\n\n\t\"effra.fixture/initprobe/dependency\"\n)\n\nfunc init() {\n\tif !dependency.Ready {\n\t\tpanic(\"dependency initialized after its importer\")\n\t}\n\tfmt.Println(\"registry-init\")\n}\n\nfunc Value() int64 { fmt.Println(\"foreign-call\"); return 7 }\n",
		"app/main.ef":              initProbeApplication,
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCLIInitializesDeclaredForeignImportsWithoutReachableCallers(t *testing.T) {
	binary := buildTestCLI(t)
	root := writeInitProbeRoot(t)
	source := filepath.Join(root, "app", "main.ef")

	// Inspection reports the initialization-only package with no binding,
	// and a package a reachable call names as its named import.
	live := filepath.Join(root, "app", "live.ef")
	liveSource := strings.Replace(initProbeApplication, "effect fn main() -> void {\n    void\n}", "effect fn main() -> i64 {\n    run unused().provide<Foreign>(Host)\n}", 1)
	if err := os.WriteFile(live, []byte(liveSource), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, source, lowering string
		foreign                int
	}{{source, initProbeApplication, "blank", 0}, {live, liveSource, "named", 1}} {
		stdout, stderr, code := runTestCLIDir(t, binary, root, "", "check", tc.path)
		var report struct {
			Checked      bool                             `json:"checked"`
			Applications []compiler.ApplicationInspection `json:"applications"`
		}
		if err := json.Unmarshal(stdout, &report); err != nil || code != 0 || !report.Checked {
			t.Fatalf("check: code=%d err=%v stdout=%q stderr=%q", code, err, stdout, stderr)
		}
		if len(report.Applications) != 1 {
			t.Fatalf("applications = %+v", report.Applications)
		}
		inspection := report.Applications[0]
		if len(inspection.GoInitialization) != 1 || inspection.GoInitialization[0].Package != initProbeRegistry || inspection.GoInitialization[0].Lowering != tc.lowering ||
			len(inspection.GoInitialization[0].Declarations) != 1 || inspection.GoInitialization[0].Declarations[0].Alias != "registry" {
			t.Fatalf("initialization inspection = %+v, want %s lowering", inspection.GoInitialization, tc.lowering)
		}
		if inspection.Requirements[compiler.RequiresGoInitialization] != 1 || inspection.Requirements[compiler.RequiresForeign] != tc.foreign || inspection.Requirements[compiler.RequiresGoImport] != tc.foreign {
			t.Fatalf("initialization requirements = %v", inspection.Requirements)
		}
		if want := compiler.CompileAt(tc.source, "go", filepath.Dir(tc.path)).ApplicationInspections(); !reflect.DeepEqual(report.Applications, want) {
			t.Fatalf("CLI inspection differs from the compiler's:\n%+v\n%+v", report.Applications, want)
		}
	}

	build := func(directory, output string) string {
		t.Helper()
		stdout, stderr, code := runTestCLIDir(t, binary, directory, "", "build", source, "-o", output)
		if code != 0 {
			t.Fatalf("build failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		run := exec.Command(filepath.Join(directory, output))
		output_, err := run.Output()
		if err != nil {
			t.Fatalf("built application failed: %v %q", err, output_)
		}
		return string(output_)
	}
	if output := build(root, filepath.Join("dist", "app")); output != "dependency-init\nregistry-init\n" {
		t.Fatalf("declared import initialization = %q", output)
	}
	applications := generatedApplicationDirectories(t, filepath.Join(root, "dist", "go", "apps"))
	if len(applications) != 1 {
		t.Fatalf("applications = %v", applications)
	}
	application := applications[0]

	// The same origin drops the import: its root goes, and no stale import
	// survives in the reconciled generation.
	if err := os.WriteFile(source, []byte("effect fn main() -> void {\n    void\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := generationCommits(t, application)
	if output := build(root, filepath.Join("dist", "app-shrunk")); output != "" {
		t.Fatalf("removed import still initialized: %q", output)
	}
	shrunkCommit, shrunkGeneration := newGeneration(t, application, before)
	if imports := generationImports(t, shrunkGeneration); slices.Contains(imports, initProbeRegistry) {
		t.Fatalf("stale foreign import after removal: %v", imports)
	}
	fresh := t.TempDir()
	if output := build(fresh, filepath.Join("dist", "app-shrunk")); output != "" {
		t.Fatalf("fresh no-import build initialized: %q", output)
	}
	freshApplications := generatedApplicationDirectories(t, filepath.Join(fresh, "dist", "go", "apps"))
	if len(freshApplications) != 1 {
		t.Fatalf("fresh applications = %v", freshApplications)
	}
	freshGeneration, found := generationCommits(t, freshApplications[0])[shrunkCommit]
	if !found {
		t.Fatalf("fresh no-import build published a different generation than the rebuild")
	}
	if got, want := generationBytes(t, freshGeneration), generationBytes(t, shrunkGeneration); !maps.EqualFunc(got, want, bytes.Equal) {
		t.Fatal("rebuilt no-import generation differs from a fresh one")
	}
}

func TestCLITestsInitializeDeclaredForeignImportsOnlyWhenLive(t *testing.T) {
	binary := buildTestCLI(t)
	root := writeInitProbeRoot(t)
	source := filepath.Join(root, "app", "main_test.ef")
	if err := os.WriteFile(source, []byte(initProbeTests), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runTestCLIDir(t, binary, root, "", "test", source)
	if code == 0 || !strings.Contains(string(stderr), "--live") || strings.Contains(string(stdout)+string(stderr), "registry-init") {
		t.Fatalf("tests with Go imports must be refused without --live and before execution: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runTestCLIDir(t, binary, root, "", "test", source, "--live")
	var report struct {
		Passed bool   `json:"passed"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(stdout, &report); err != nil || code != 0 || !report.Passed {
		t.Fatalf("live tests: code=%d err=%v stdout=%q stderr=%q", code, err, stdout, stderr)
	}
	if report.Output != "dependency-init\nregistry-init\n" {
		t.Fatalf("initialization must run once before the cases without calling the dead binding: %q", report.Output)
	}
}

// An uncalled import of a package the generated program cannot import is
// refused by check and build before any Go build runs.
func TestCLIRefusesForeignImportsTheGeneratedProgramCannotImport(t *testing.T) {
	binary := buildTestCLI(t)
	root := writeInitProbeRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "internal", "hidden"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "hidden", "hidden.go"), []byte("package hidden\n\nfunc Value() int64 { return 1 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "app", "main.ef")
	if err := os.WriteFile(source, []byte("import go hidden \"effra.fixture/initprobe/internal/hidden\"\neffect fn main() -> void {\n    void\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"check", "build"} {
		stdout, stderr, code := runTestCLIDir(t, binary, root, "", command, source)
		var report struct {
			Checked      bool                             `json:"checked"`
			Diagnostics  []compiler.Diagnostic            `json:"diagnostics"`
			Applications []compiler.ApplicationInspection `json:"applications"`
		}
		if err := json.Unmarshal(stdout, &report); err != nil {
			t.Fatalf("%s: no JSON report: %v stdout=%q stderr=%q", command, err, stdout, stderr)
		}
		if code == 0 || report.Checked || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "EF111" || report.Diagnostics[0].Span.Line != 1 || len(report.Applications) != 0 {
			t.Fatalf("%s must refuse the import at its declaration: code=%d %+v stderr=%q", command, code, report, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "dist")); !os.IsNotExist(err) {
		t.Fatalf("refused build published output: %v", err)
	}
}
