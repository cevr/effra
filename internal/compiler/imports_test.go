package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
effect fn main() -> string raises {GoError} {run sdk.Lookup("native").orFail().provide<Foreign>(Host)}`
	r := CompileAt(source, "go", workspace)
	if !r.Checked || len(r.Bindings) != 1 || r.Bindings[0].Signature != "func(name string) (string, error)" {
		t.Fatalf("foreign declaration: %+v %+v", r.Diagnostics, r.Bindings)
	}
	code, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	generated := t.TempDir()
	if err = application.WriteRuntime(generated); err != nil {
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
		{`import go strings "strings" effect fn main() -> void {let pending = strings.NewReplacer("x","y"); void}`, "EF112"},
	} {
		r := Compile(tc.source)
		if r.Checked || !hasCode(r, tc.code) {
			t.Fatalf("expected %s: %+v", tc.code, r.Diagnostics)
		}
	}
	// JavaScript refuses even an uncalled Go import before host loading,
	// and emits nothing for the refused result.
	js := CompileFor(`import go strings "strings" effect fn main() -> void {void}`, "js")
	if js.Checked || !hasCode(js, "EF110") || js.Timings.ImportMicros != 0 {
		t.Fatal("JS check attempted a Go import")
	}
	if _, _, err := js.Emit(true); err == nil {
		t.Fatal("JavaScript emitted a program with a Go import")
	}
}

func TestImportedPartialValuesAndContextForwarding(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	source := `import go sdk "effra.local/prototype/examples/sdk"
effect fn main() -> string raises {GoError} {
 let partial = run sdk.Lookup("missing").provide<Foreign>(Host)
 let timed = run sdk.Lookup("slow").orFail().timeout(1).catch<Timeout>("done").provide<Foreign>(Host).provide<Scheduler>(LiveScheduler)
 partial.value + ":" + timed
}`
	r := CompileAt(source, "go", root)
	if !r.Checked || len(r.Bindings) != 1 || !r.Bindings[0].Context || r.Bindings[0].Cancellation != "cooperative" {
		t.Fatalf("context binding: %+v %+v", r.Diagnostics, r.Bindings)
	}
	code, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = application.WriteRuntime(dir); err != nil {
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

// Every effra.bindings.json key must name a declaration: a function or a
// method's go/types full name, resolved in the package the key names, which
// source need not import. An unknown key is refused when metadata loads, one
// sorted diagnostic per key, with near misses from the same package.
func TestBindingMetadataRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/keys\n\ngo 1.27\n")
	write("keys.go", `package keys

type Client struct{}

func (c *Client) Lookup() string { return "lookup" }
func (Client) Name() string     { return "name" }

func Fetch() string { return "fetch" }

type shared struct{}

func (shared) Shared() string { return "shared" }

type Wrapped struct{ shared }

type Reader interface{ Read() string }

type Box[T any] struct{}

func (*Box[T]) Get() string { return "get" }

type Boxed struct{ Box[int64] }
`)
	source := `import go keys "example.test/keys"
effect fn main() -> string {
    run keys.Fetch().provide<Foreign>(Host)
}`
	known := []string{
		"example.test/keys.Fetch", "(*example.test/keys.Client).Lookup", "(example.test/keys.Client).Name",
		"(example.test/keys.shared).Shared", "(example.test/keys.Reader).Read", "(*example.test/keys.Box[T]).Get",
		"(error).Error", "strings.ToUpper",
	}
	contract := func(keys []string) string {
		entries := make([]string, len(keys))
		for i, key := range keys {
			entries[i] = strconv.Quote(key) + `:{"cancellation":"unknown"}`
		}
		return "{" + strings.Join(entries, ",") + "}"
	}
	write("effra.bindings.json", contract(known))
	if r := CompileAt(source, "go", dir); !r.Checked {
		t.Fatalf("declared keys refused: %+v", r.Diagnostics)
	}
	write("effra.bindings.json", contract([]string{
		"example.test/keys.Fecth", "(example.test/keys.Client).Lookup", "example.test/keys.Client",
		"example.test/missing.Fetch", "Fetch", "(example.test/keys.Wrapped).Shared", "(*example.test/keys.Boxed).Get",
		"(*example.test/keys.Box[int64]).Get",
	}))
	r := CompileAt(source, "go", dir)
	want := []string{
		`effra.bindings.json key "(*example.test/keys.Box[int64]).Get" matches no Go function or method; near: "(*example.test/keys.Box[T]).Get"`,
		`effra.bindings.json key "(*example.test/keys.Boxed).Get" matches no Go function or method; near: "(*example.test/keys.Box[T]).Get"`,
		`effra.bindings.json key "(example.test/keys.Client).Lookup" matches no Go function or method; near: "(*example.test/keys.Client).Lookup"`,
		`effra.bindings.json key "(example.test/keys.Wrapped).Shared" matches no Go function or method; near: "(example.test/keys.shared).Shared"`,
		`effra.bindings.json key "Fetch" is not a go/types full name such as "path.Func" or "(*path.Type).Method"`,
		`effra.bindings.json key "example.test/keys.Client" matches no Go function or method`,
		`effra.bindings.json key "example.test/keys.Fecth" matches no Go function or method; near: "example.test/keys.Fetch"`,
		`effra.bindings.json key "example.test/missing.Fetch" names package example.test/missing, which does not load: `,
	}
	if r.Checked || len(r.Diagnostics) != len(want) {
		t.Fatalf("unknown keys: %+v", r.Diagnostics)
	}
	for i, diagnostic := range r.Diagnostics {
		if diagnostic.Code != "EF111" || !strings.HasPrefix(diagnostic.Message, want[i]) || (!strings.HasSuffix(want[i], ": ") && diagnostic.Message != want[i]) {
			t.Fatalf("diagnostic %d: %+v, want %q", i, diagnostic, want[i])
		}
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
