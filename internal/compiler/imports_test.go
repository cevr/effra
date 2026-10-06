package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGoImportsUseNativeDeclarationsAndDetectDependencyChanges(t *testing.T) {
	workspace := t.TempDir()
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/app\n\ngo 1.27\nrequire example.test/dependency v0.0.0\nreplace example.test/dependency => ./dependency\n")
	if err := os.Mkdir(filepath.Join(workspace, "dependency"), 0700); err != nil {
		t.Fatal(err)
	}
	write("dependency/go.mod", "module example.test/dependency\n\ngo 1.27\n")
	write("dependency/value.go", "package dependency\nfunc Value(s string) string { return s }\n")
	if err := os.Mkdir(filepath.Join(workspace, "sdk"), 0700); err != nil {
		t.Fatal(err)
	}
	write("sdk/sdk.go", "package sdk\nimport \"example.test/dependency\"\nfunc Lookup(name string)(string,error){return dependency.Value(name),nil}\n")
	source := `import go sdk "example.test/app/sdk"
effect fn main() -> string throws {GoError} {run sdk.Lookup("native").orFail().provide<Foreign>(Host)}`
	r := CompileAt(source, "go", workspace)
	if !r.Checked || len(r.Bindings) != 1 || r.Bindings[0].Signature != "func(name string) (string, error)" {
		t.Fatalf("foreign declaration: %+v %+v", r.Diagnostics, r.Bindings)
	}
	code, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	generated := t.TempDir()
	if err = WriteRuntime(generated); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(generated, "go.mod"), r.ModuleFile(), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(generated, "main.go"), []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runGoCommand(generated, "run", ".")
	if err != nil || string(output) != "native\n" {
		t.Fatalf("module-aware native build: %v %s", err, output)
	}
	write("sdk/sdk.go", "package sdk\nfunc Lookup(name string)(bool,error){return true,nil}\n")
	changed := CompileAt(source, "go", workspace)
	if changed.Checked || !hasCode(changed, "EF106") || changed.Revision == r.Revision {
		t.Fatalf("dependency type change not detected: %+v", changed)
	}
}

func TestGoImportGuardrails(t *testing.T) {
	for _, tc := range []struct{ source, code string }{
		{`import go strings "strings" effect fn main() -> string {run strings.ToUpper("x")}`, "EF108"},
		{`import go strconv "strconv" effect fn main() -> bool {run strconv.ParseBool("x").orFail().provide<Foreign>(Host)}`, "EF107"},
		{`import go strings "strings" effect fn main() -> () {let pending = strings.NewReplacer("x","y"); ()}`, "EF112"},
	} {
		r := Compile(tc.source)
		if r.Checked || !hasCode(r, tc.code) {
			t.Fatalf("expected %s: %+v", tc.code, r.Diagnostics)
		}
	}
	js := CompileFor(`import go strings "strings" effect fn main() -> () {()}`, "js")
	if js.Checked || !hasCode(js, "EF110") || js.Timings.ImportMicros != 0 {
		t.Fatal("JS check attempted a Go import")
	}
}

func TestImportedPartialValuesAndContextForwarding(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	source := `import go sdk "effra.local/prototype/examples/sdk"
effect fn main() -> string throws {GoError} {
 let partial = run sdk.Lookup("missing").provide<Foreign>(Host)
 let timed = run sdk.Lookup("slow").orFail().timeout(1).catch<Timeout>("done").provide<Foreign>(Host).provide<Scheduler>(LiveScheduler)
 partial.value + ":" + timed
}`
	r := CompileAt(source, "go", root)
	if !r.Checked || len(r.Bindings) != 1 || !r.Bindings[0].Context || r.Bindings[0].Cancellation != "cooperative" {
		t.Fatalf("context binding: %+v %+v", r.Diagnostics, r.Bindings)
	}
	code, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{"main.go": []byte(code), "go.mod": r.ModuleFile()} {
		if err = os.WriteFile(filepath.Join(dir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "run", "-race", ".")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != "partial:done\n" {
		t.Fatalf("imported SDK: %v %s", err, output)
	}
}

func TestBindingMetadataRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	for name, contents := range map[string]string{
		"go.mod":              "module example.test/contracts\n\ngo 1.27\n",
		"effra.bindings.json": `{"strings.ToUpper":{"cancellaton":"cooperative"}}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := CompileAt(`import go strings "strings" effect fn main() -> string {run strings.ToUpper("x").provide<Foreign>(Host)}`, "go", dir)
	if r.Checked || !hasCode(r, "EF111") {
		t.Fatalf("misspelled behavior assertion accepted: %+v", r.Diagnostics)
	}
}

func TestContextForwardingUsesPackageIdentity(t *testing.T) {
	dir := t.TempDir()
	for name, contents := range map[string]string{
		"go.mod":              "module example.test/context\n\ngo 1.27\n",
		"context.go":          "package context\ntype Context struct{}\nfunc Lookup(ctx Context) string { return \"bad\" }\n",
		"effra.bindings.json": `{"example.test/context.Lookup":{"context":"fiber","cancellation":"cooperative"}}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := CompileAt(`import go host "example.test/context" effect fn main() -> string {run host.Lookup().provide<Foreign>(Host)}`, "go", dir)
	if r.Checked || !hasCode(r, "EF112") {
		t.Fatalf("unrelated context.Context accepted: %+v", r.Diagnostics)
	}
}
