package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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

// When go list fails to list the packages that keys name outside the import
// closure, whether the command fails or its output does not decode, each such
// key is refused with that failure, every other key is still checked, and no
// refused key reaches a binding: Lookup keeps its explicit context parameter.
func TestBindingMetadataChecksEveryKeyWhenListingFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake go command is a shell script")
	}
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	dir, bin := t.TempDir(), t.TempDir()
	write := func(name, contents string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(contents), mode); err != nil {
			t.Fatal(err)
		}
	}
	// The fake go fails only the listing of key packages; every other command
	// runs the real toolchain.
	write(filepath.Join(bin, "go"), `#!/bin/sh
printf '%s\n' "$*" >> "$EFFRA_FAKE_GO_LOG"
if [ "$1" = list ] && [ "$2" = -e ]; then
	case "$EFFRA_FAKE_GO_LIST" in
	fail) echo "simulated go list failure" >&2; exit 1 ;;
	garbage) echo '{"ImportPath":'; exit 0 ;;
	esac
fi
exec "$EFFRA_REAL_GO" "$@"
`, 0o700)
	write(filepath.Join(dir, "go.mod"), "module example.test/listing\n\ngo 1.27\n", 0o600)
	write(filepath.Join(dir, "listing.go"), `package listing

import "context"

type value struct{}

func (value) Lookup(context.Context, string) (string, error) { return "value", nil }

func First() interface {
	Lookup(context.Context, string) (string, error)
} {
	return value{}
}
`, 0o600)
	write(filepath.Join(dir, "other", "other.go"), "package other\n\nfunc F() string { return \"other\" }\n", 0o600)
	write(filepath.Join(dir, "effra.bindings.json"), `{"(interface).Lookup":{"context":"fiber","cancellation":"cooperative"},
 "example.test/listing.Fecth":{"cancellation":"unknown"},
 "example.test/listing/other.F":{"cancellation":"unknown"}}`, 0o600)
	log := filepath.Join(bin, "go.log")
	t.Setenv("EFFRA_REAL_GO", realGo)
	t.Setenv("EFFRA_FAKE_GO_LOG", log)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct{ mode, failure string }{
		{"fail", "go list: exit status 1: simulated go list failure"},
		{"garbage", "go list: unexpected EOF"},
	} {
		t.Setenv("EFFRA_FAKE_GO_LIST", tc.mode)
		if err := os.WriteFile(log, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		r := CompileAt(`import go l "example.test/listing"
import Data "effra/data"
effect fn main() -> string {
    match run l.First().provide<Foreign>(Host) {
        Data.Option.None => "none",
        Data.Option.Some { value: first } => {
            let looked = run first.Lookup("one").provide<Foreign>(Host)
            "looked"
        }
    }
}`, "go", dir)
		calls, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(strings.Split(string(calls), "\n"), "list -e -deps -export -json -- example.test/listing/other") {
			t.Fatalf("%s: key packages were not listed: %q", tc.mode, calls)
		}
		want := []string{
			`EF111 effra.bindings.json key "(interface).Lookup" names a method of an unnamed interface`,
			`EF111 effra.bindings.json key "example.test/listing.Fecth" matches no Go function or method`,
			`EF111 effra.bindings.json key "example.test/listing/other.F" names package example.test/listing/other, which does not load: ` + tc.failure,
			`EF106 incorrect Go argument count`,
		}
		if r.Checked || len(r.Diagnostics) < len(want) {
			t.Fatalf("%s: %+v", tc.mode, r.Diagnostics)
		}
		for i, prefix := range want {
			if got := r.Diagnostics[i].Code + " " + r.Diagnostics[i].Message; !strings.HasPrefix(got, prefix) {
				t.Fatalf("%s: diagnostic %d is %q, want prefix %q", tc.mode, i, got, prefix)
			}
		}
		for _, binding := range r.Bindings {
			if binding.Context || binding.Cancellation == "cooperative" {
				t.Fatalf("%s: refused key applied to %s", tc.mode, binding.Symbol)
			}
		}
	}
}

// Metadata is validated in key order, so the first invalid entry, and the
// error a load reports, are the same on every load.
func TestBindingMetadataValidationOrderIsStable(t *testing.T) {
	dir := t.TempDir()
	for name, contents := range map[string]string{
		"go.mod":              "module example.test/order\n\ngo 1.27\n",
		"effra.bindings.json": `{"example.test/order.MissingB":{"cancellation":"invalid"},"example.test/order.MissingA":{"context":"invalid"},"example.test/order.MissingC":{"context":"invalid"}}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for range 32 {
		if _, _, err := loadContracts(dir); err == nil || err.Error() != "unsupported context contract for example.test/order.MissingA" {
			t.Fatalf("first metadata error: %v", err)
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
