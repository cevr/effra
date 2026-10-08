package compiler

import (
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hostTypesImport = `import go host "effra.local/prototype/examples/hosttypes"
import go strconv "strconv"
import Data "effra/data"
`

const hostTypesEntry = `
effect fn main() -> void {
    run program().provide<Foreign>(Host).provide<Console>(Stdout)
}`

func compileHostTypes(t *testing.T, source string) *Result {
	t.Helper()
	return CompileAt(hostTypesImport+source+hostTypesEntry, "go", "../..")
}

func runGeneratedGo(t *testing.T, r *Result) string {
	t.Helper()
	code, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	return runGoModule(t, r, application, map[string][]byte{"main.go": []byte(code)})
}

// runGoModule runs generated sources as one native module beside the runtime.
func runGoModule(t *testing.T, r *Result, application *GoApplication, files map[string][]byte) string {
	t.Helper()
	output, err := execGoModule(t, application, r, files)
	if err != nil {
		t.Fatalf("generated program: %v\n%s", err, output)
	}
	return string(output)
}

// execGoModule runs generated sources and returns the process outcome.
func execGoModule(t *testing.T, application *GoApplication, r *Result, files map[string][]byte) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = r.ModuleFile()
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return runGoCommand(dir, "run", ".")
}

// The authored example is an ordinary program over a local Go package: nominal
// host values, nullable pointer/interface/slice results, typed-nil errors and
// a three-value tuple all execute natively without nil in source.
func TestHostTypesExampleExecutesNatively(t *testing.T) {
	source, err := os.ReadFile("../../examples/host-types.ef")
	if err != nil {
		t.Fatal(err)
	}
	r := CompileAt(string(source), "go", "../..")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	want := strings.Join([]string{
		"4,0",
		"3 absent",
		"nil interface; typed-nil square; square",
		"7 with typed-nil *Problem",
		"8 no error",
		"is missing; typed-nil error failed",
		"ab 3 found; abc 3 missing",
		"nil slice; 0",
		"nil bytes; empty; alias",
		"native buffer",
		`4; 3 read=0 writeTo=1 write=1 readFrom=0 "abc"; *Problem nil`,
		"ab c false; 2 short=true he; a,b open=0 readDir=1",
		"42",
	}, "\n") + "\n"
	if output := runGeneratedGo(t, r); output != want {
		t.Fatalf("host types program:\n%s\nwant:\n%s", output, want)
	}
}

// A typed-nil error is still an error: GoResult reports it, keeps the partial
// value, and hands the original interface back to native code. A nil error is
// absent. Both are observed only through checked alternatives.
func TestTypedNilErrorRetainsIdentityAndPartialResult(t *testing.T) {
	r := compileHostTypes(t, `effect fn observe(typed: bool) -> string uses { Foreign } {
    let result = if typed { run host.Typed() } else { run host.Untyped() }
    let flag = if result.hasError { "error" } else { "success" }
    let native = match result.error {
        Data.Option.None => "absent",
        Data.Option.Some { value: native } => run host.DescribeError(native)
    }
    flag + ":" + run strconv.Itoa(result.value) + ":" + native
}
effect fn program() -> void uses { Console, Foreign } {
    run Console.log(run observe(true))
    run Console.log(run observe(false))
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "error:7:typed-nil *Problem\nsuccess:8:absent\n" {
		t.Fatalf("typed-nil error: %q", output)
	}
}

// Every native result component survives an error, and orFail retains the
// whole tuple as the failure's partial value rather than discarding it.
func TestMultiValueResultPreservesPartialComponents(t *testing.T) {
	r := compileHostTypes(t, `effect fn parts(text: string) -> string uses { Foreign } {
    let split = run host.Split(text)
    let found = if split.value.v2 { "true" } else { "false" }
    split.value.v0 + "|" + run strconv.Itoa(split.value.v1) + "|" + found + "|" + if split.hasError { "error" } else { "ok" }
}
effect fn strict(text: string) -> string raises { GoError } uses { Foreign } {
    let split = run host.Split(text).orFail()
    split.v0
}
effect fn program() -> void uses { Console, Foreign } {
    run Console.log(run parts("key:value"))
    run Console.log(run parts("bare"))
    run Console.log(run strict("bare").catch<GoError>("failed"))
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "key|5|true|ok\nbare|4|false|error\nfailed\n" {
		t.Fatalf("multi-value result: %q", output)
	}
	var split Binding
	for _, binding := range r.Bindings {
		if binding.Symbol == "host.Split" {
			split = binding
		}
	}
	if split.Return != "(string, int, bool)" || !split.HasError || len(split.HostResults) != 4 || split.HostResults[3].Adaptation != hostAdaptError {
		t.Fatalf("complete tuple inspection: %+v", split)
	}
	// Source cannot inspect a failure payload, so a native entry point runs the
	// compiled strict function and reads GoError directly: the partial value is
	// the complete tuple and the error is Go's original sentinel.
	code, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(code, "\nfunc main() {") {
		t.Fatal("generated program has no main entry")
	}
	code = strings.Replace(code, "\nfunc main() {", "\nfunc efGeneratedMain() {", 1)
	output := runGoModule(t, r, application, map[string][]byte{"main.go": []byte(code), "harness.go": []byte(`package main

import (
	"context"
	"fmt"

	er "effra.generated/runtime"
	host "effra.local/prototype/examples/hosttypes"
)

func main() {
	exit := er.RunContext(context.Background(), func(fc *er.FiberContext) er.Exit[string] {
		return efProvide_Foreign(efFunction_strict("bare"), efProvider_Host())(efContext{Runtime: fc})
	})
	payload := exit.Failure.Payload.(er.GoError)
	partial := payload.Partial.(struct {
		V0 string
		V1 int
		V2 bool
	})
	fmt.Printf("%s %s|%d|%t %t\n", exit.Failure.Tag, partial.V0, partial.V1, partial.V2, payload.Err == host.ErrNoSeparator)
}
`)})
	if output != "GoError bare|4|false true\n" {
		t.Fatalf("GoError partial payload: %q", output)
	}
}

// Nullable native values adapt to Option in both directions of presence; a
// present value is passed back to Go as the original native object.
func TestNullablePointerAdaptsThroughOption(t *testing.T) {
	r := compileHostTypes(t, `effect fn count(name: string) -> string uses { Foreign } {
    match run host.Find(name) {
        Data.Option.None => "none",
        Data.Option.Some { value: counter } => run strconv.Itoa(run host.Count(counter))
    }
}
effect fn program() -> void uses { Console, Foreign } {
    run Console.log(run count("known") + "/" + run count("other"))
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "3/none\n" {
		t.Fatalf("nullable pointer: %q", output)
	}
	var pointer, option *TypeNode
	for i := range r.Types {
		node := &r.Types[i]
		if node.Kind == "host" && node.Declaration == "go:*effra.local/prototype/examples/hosttypes.Counter" {
			pointer = node
		}
		if node.Kind == "application" && node.Declaration == hostOptionTemplate {
			option = node
		}
	}
	if pointer == nil || pointer.Name != "*host.Counter" || len(pointer.Args) != 1 || option == nil || len(option.Args) != 1 || option.Args[0] != pointer.ID {
		t.Fatalf("canonical host facts: pointer=%+v option=%+v", pointer, option)
	}
}

// A native byte slice is a bytes payload only when present: nil is None and a
// present empty slice reaches Go again as empty, also through an alias. The
// adapted result cannot stand in for bytes, so nil never hides inside Some.
func TestNativeByteSliceAdaptsThroughOption(t *testing.T) {
	r := compileHostTypes(t, `effect fn raw(present: bool) -> string uses { Foreign } {
    match run host.Bytes(present) {
        Data.Option.None => "none",
        Data.Option.Some { value: data } => run host.BytesClass(data)
    }
}
effect fn program() -> void uses { Console, Foreign } {
    let aliased = match run host.RawText("") {
        Data.Option.None => "none",
        Data.Option.Some { value: data } => run host.BytesClass(data)
    }
    run Console.log(run raw(false) + "/" + run raw(true) + "/" + aliased)
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "none/empty/empty\n" {
		t.Fatalf("native bytes: %q", output)
	}
	results := map[string]HostComponent{}
	for _, binding := range r.Bindings {
		if len(binding.HostResults) > 0 {
			results[binding.Symbol] = binding.HostResults[0]
		}
		if binding.Symbol == "host.BytesClass" && (binding.HostParameters[0].Type != "bytes" || binding.HostParameters[0].Adaptation != hostAdaptPresent) {
			t.Fatalf("bytes parameter: %+v", binding.HostParameters)
		}
	}
	for _, symbol := range []string{"host.Bytes", "host.RawText"} {
		if got := results[symbol]; got != (HostComponent{Native: "[]uint8", Type: "Option<bytes>", Adaptation: hostAdaptOption}) {
			t.Fatalf("%s result: %+v", symbol, got)
		}
	}
	for _, body := range []string{
		`run host.BytesClass(run host.Bytes(false))`,
		`run host.BytesClass(match Data.Option.Some { value: run host.Bytes(false) } {
        Data.Option.None => run host.Bytes(true),
        Data.Option.Some { value } => value
    })`,
	} {
		r := compileHostTypes(t, "effect fn program() -> void uses { Console, Foreign } {\n    run Console.log("+body+")\n}")
		if r.Checked || !hasCode(r, "EF106") {
			t.Fatalf("absent bytes reached a bytes parameter: %s\n%v", body, r.Diagnostics)
		}
	}
}

func TestHostTypeAdmissionDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, body, code, message string }{
		{"fixed width", `run host.Narrow(1)`, "EF112", "native scalar int32 has no admitted Effra width"},
		{"float", `run host.Ratio()`, "EF112", "native scalar float64"},
		{"generic", `run host.Identity("x")`, "EF112", "generic Go functions are unsupported"},
		{"variadic", `run host.Join("a", "b")`, "EF112", "variadic Go functions are unsupported"},
		{"i64 is not int", `run strconv.Itoa(4 + 0)`, "EF106", "Go argument must be int"},
		{"portable int literal", `run strconv.Itoa(3000000000)`, "EF106", "exceeds the portable range of Go int"},
		{"unchecked option", `run host.Count(run host.Find("known"))`, "EF106", "Go argument must be"},
		{"nominal mismatch", `run host.Count(run host.Origin())`, "EF106", "Go argument must be *host.Counter"},
		{"tuple field", `run host.Split("a")
    let probe = split.value.v3`, "EF102", "v0 through v2"},
		{"nil", `nil`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := compileHostTypes(t, "effect fn program() -> void uses { Console, Foreign } {\n    let split = "+tc.body+"\n    void\n}")
			if r.Checked {
				t.Fatalf("accepted %s", tc.body)
			}
			if tc.code != "" && !hasCode(r, tc.code) {
				t.Fatalf("expected %s: %+v", tc.code, r.Diagnostics)
			}
			if tc.message != "" && !strings.Contains(diagnosticText(r), tc.message) {
				t.Fatalf("expected %q: %+v", tc.message, r.Diagnostics)
			}
		})
	}
}

func diagnosticText(r *Result) string {
	var text strings.Builder
	for _, d := range r.Diagnostics {
		text.WriteString(d.Message + "\n")
	}
	return text.String()
}

// Host identity is the declaring package path and name, independent of the
// import alias and of a same-named type elsewhere. Types generated code
// cannot name are refused with their reason.
func TestHostTypeNominalIdentityAndUnnameableTypes(t *testing.T) {
	workspace := t.TempDir()
	files := map[string]string{
		"go.mod":                    "module example.test/app\n\ngo 1.27\n",
		"left/left.go":              "package left\ntype Point struct{ X int64 }\nfunc Origin() Point { return Point{} }\nfunc Show(p Point) string { return \"left\" }\n",
		"right/right.go":            "package right\ntype Point struct{ X int64 }\nfunc Show(p Point) string { return \"right\" }\n",
		"left/hidden/internal/x.go": "package internal\ntype Secret struct{}\n",
		"left/hidden/hidden.go":     "package hidden\nimport \"example.test/app/left/hidden/internal\"\ntype private struct{}\nfunc Private() private { return private{} }\nfunc Secret() internal.Secret { return internal.Secret{} }\n",
	}
	for name, contents := range files {
		path := filepath.Join(workspace, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	imports := `import go left "example.test/app/left"
import go again "example.test/app/left"
import go right "example.test/app/right"
import go hidden "example.test/app/left/hidden"
`
	accepted := CompileAt(imports+`fn keep(point: again.Point) -> left.Point { point }
effect fn program() -> void uses { Console, Foreign } { run Console.log(run left.Show(keep(run left.Origin()))) }`+hostTypesEntry, "go", workspace)
	if !accepted.Checked {
		t.Fatalf("aliases of one package must share host identity: %+v", accepted.Diagnostics)
	}
	if output := runGeneratedGo(t, accepted); output != "left\n" {
		t.Fatalf("aliased host type program: %q", output)
	}
	for _, tc := range []struct{ body, message string }{
		{`run right.Show(run left.Origin())`, "Go argument must be right.Point"},
		{`run hidden.Private()`, "unexported host type"},
		{`run hidden.Secret()`, "generated code cannot import"},
	} {
		r := CompileAt(imports+"effect fn program() -> void uses { Console, Foreign } {\n    let split = "+tc.body+"\n    void\n}"+hostTypesEntry, "go", workspace)
		if r.Checked || !strings.Contains(diagnosticText(r), tc.message) {
			t.Fatalf("%s: expected %q: %+v", tc.body, tc.message, r.Diagnostics)
		}
	}
}

// Go-only host types never reach the JavaScript target: the program is
// refused before any Go tool runs, rather than emitted with unknown types.
func TestHostTypesRefuseJavaScriptBeforeGoTools(t *testing.T) {
	source, err := os.ReadFile("../../examples/host-types.ef")
	if err != nil {
		t.Fatal(err)
	}
	r := CompileAt(string(source), "js", "../..")
	if r.Checked || !hasCode(r, "EF110") || r.Timings.ImportMicros != 0 || len(r.Bindings) != 0 {
		t.Fatalf("JS target admitted Go host types: checked=%v import=%d %+v", r.Checked, r.Timings.ImportMicros, r.Diagnostics)
	}
}

// The application plan roots the packages declaring reachable host types and
// the bundled Option they adapt through, separately from call imports.
func TestApplicationPlanRetainsHostTypePackages(t *testing.T) {
	r, plan := exampleApplicationPlan(t, "host-types.ef", GoGenerationBuild)
	counter := "go:*effra.local/prototype/examples/hosttypes.Counter"
	requirePlanned(t, plan, RequiresHostType, counter, "go:effra.local/prototype/examples/hosttypes.Shape", "go:error", "go:int", "go:[]string")
	requirePlanned(t, plan, RequiresGoImport, "effra.local/prototype/examples/hosttypes")
	// bytes is never imported by source; only a reachable native type names it.
	requireProvenance(t, plan, RequiresGoImport, "bytes", "go:bytes.Buffer", "host-type")
	requirePlanned(t, plan, RequiresDeclaration, hostOptionTemplate)
	requirePlanned(t, plan, RequiresForeign, "go:strconv.Itoa", "go:io.Copy", "go:(*effra.local/prototype/examples/hosttypes.Counter).Increment", "go:(*bytes.Buffer).String")
	requirePlanned(t, plan, RequiresHostType, "go:*effra.local/prototype/examples/hosttypes.Source", "go:*effra.local/prototype/examples/hosttypes.Problem")
	requireUnplanned(t, plan, RequiresForeign, "go:effra.local/prototype/examples/hosttypes.Narrow")
	requireUnplanned(t, plan, RequiresHostType, "go:effra.local/prototype/examples/hosttypes.Square")
	if !strings.Contains(plan.Revision, r.Revision) {
		t.Fatalf("plan revision %s does not match %s", plan.Revision, r.Revision)
	}
}

// Native pointer, slice, map, error, any and int annotations use Go's own
// spelling, which is also what inspection displays, so a reported host type
// round-trips into source.
func TestHostTypeAnnotationSpellings(t *testing.T) {
	r := compileHostTypes(t, `fn counters(values: []*host.Counter) -> []*host.Counter {
    values
}
fn table(counts: map[string]int) -> map[string]int {
    counts
}
fn names(listed: []string) -> []string {
    listed
}
effect fn count(counter: *host.Counter) -> int uses { Foreign } {
    run host.Count(counter)
}
effect fn describe(err: error) -> string uses { Foreign } {
    run host.DescribeError(err)
}
effect fn kind(value: any, size: int) -> string uses { Foreign } {
    run host.DynamicType(value) + " " + run strconv.Itoa(size)
}
effect fn program() -> void uses { Console, Foreign } {
    match run host.Find("known") {
        Data.Option.None => void,
        Data.Option.Some { value: counter } => run Console.log(run strconv.Itoa(run count(counter)))
    }
    match run host.Counts() {
        Data.Option.None => void,
        Data.Option.Some { value: counts } => match run host.Boxed() {
            Data.Option.None => void,
            Data.Option.Some { value: boxed } => run Console.log(run kind(boxed, run host.CountOf(table(counts), "b")))
        }
    }
    let typed = run host.Typed()
    match typed.error {
        Data.Option.None => void,
        Data.Option.Some { value: native } => run Console.log(run describe(native))
    }
    match run host.Names(true) {
        Data.Option.None => void,
        Data.Option.Some { value: listed } => run Console.log(run strconv.Itoa(run host.NameCount(names(listed))))
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "3\nstring 2\ntyped-nil *Problem\n0\n" {
		t.Fatalf("annotated host values: %q", output)
	}
	displays := map[string]bool{}
	for _, binding := range r.Bindings {
		for _, component := range binding.HostParameters {
			displays[component.Type] = true
		}
	}
	for _, display := range []string{"*host.Counter", "map[string]int", "[]string", "error", "any", "int"} {
		if !displays[display] {
			t.Fatalf("binding display %q missing: %v", display, displays)
		}
	}
	formatted, err := FormatSource(hostTypesImport + "fn f(a:*host.Counter,b:[]string,c:map[string][]*host.Counter) -> []*host.Counter {\n    b\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(formatted.Text, "fn f(a: *host.Counter, b: []string, c: map[string][]*host.Counter) -> []*host.Counter {") {
		t.Fatalf("formatted host spellings: %q", formatted.Text)
	}
	for _, tc := range []struct{ name, source string }{
		{"unknown element", `fn f(values: []host.Missing) -> void {
    void
}`},
		{"incomparable key", `fn f(values: map[[]string]int) -> void {
    void
}`},
		{"unadmitted element", `fn f(values: []host.Join) -> void {
    void
}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := compileHostTypes(t, tc.source+"\neffect fn program() -> void uses { Console, Foreign } {\n    void\n}")
			if r.Checked {
				t.Fatalf("accepted %s", tc.source)
			}
		})
	}
	for _, annotation := range []string{"*Point", "[]string", "error", "any", "int"} {
		r := CompileAt("record Point { x: i64 }\nfn f(value: "+annotation+") -> void {\n    void\n}\neffect fn main() -> void {\n    void\n}", "go", "../..")
		if r.Checked {
			t.Fatalf("%s admitted without a Go import", annotation)
		}
	}
}

// A host type's declaring package is spelled through the plan's named import
// of it, exactly like a call's alias: a declared import of that package is
// not also given a blank initialization import, and inspection reports the
// package as named.
func TestHostTypePackageIsNamedThroughThePlan(t *testing.T) {
	r := compileHostTypes(t, `import go bytes "bytes"
effect fn program() -> void uses { Console, Foreign } {
    match run host.NewBuffer("native") {
        Data.Option.None => void,
        Data.Option.Some { value: buffer } => run Console.log(run buffer.String())
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	names := importsOf(emittedImportSpecs(t, application.Main), "bytes")
	if len(names) != 1 || !strings.HasPrefix(names[0], "efGoType_") {
		t.Fatalf("a package named by a host type needs its named import and no blank duplicate: %v", names)
	}
	for _, inspection := range r.ApplicationInspections() {
		for _, init := range inspection.GoInitialization {
			if init.Package == "bytes" && init.Lowering != "named" {
				t.Fatalf("inspection reports %q for a package a host type names", init.Lowering)
			}
		}
	}
}

// One rule decides whether generated code may import a package: a host type
// is admitted only from packages goImportRefusal admits, so a declared import
// and a host type's package cannot disagree. cmd/go's vendor rule applies to
// a "vendor" path element that has a parent directory below it, not to a
// package whose final element is "vendor".
func TestHostPackageImportabilityIsGoImportRefusal(t *testing.T) {
	for _, test := range []struct {
		path, name string
		importable bool
	}{
		{"example.test/sdk", "sdk", true},
		{"example.test/sdk/vendor", "vendor", true},
		{"example.test/vendorish/sdk", "sdk", true},
		{"example.test/sdk/vendor/dep", "dep", false},
		{"vendor/golang.org/x/net/dns/dnsmessage", "dnsmessage", false},
		{"example.test/sdk/internal/dep", "dep", false},
		{"example.test/sdk/internal", "internal", false},
		{"internal/abi", "abi", false},
		{"example.test/cmd/tool", "main", false},
		{"strings", "strings", true},
	} {
		// The table is the oracle: it states the Go command's rule, not
		// what goImportRefusal currently answers.
		if got := hostPackageImportable(types.NewPackage(test.path, test.name)); got != test.importable {
			t.Errorf("%s: hostPackageImportable=%v want importable=%v", test.path, got, test.importable)
		}
	}
}
