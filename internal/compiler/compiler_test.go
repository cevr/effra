package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func hasCode(r *Result, code string) bool {
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}
func TestGuardrails(t *testing.T) {
	cases := []struct{ name, source, code string }{
		{"missing service", `effect fn main() -> () { run Console.log("x") }`, "EF108"},
		{"unhandled error", `error Bad effect fn bad() -> string raises {Bad} { fail Bad } effect fn main() -> string { run bad() }`, "EF107"},
		{"pure execution", `fn main() -> () { run Console.log("x") }`, "EF105"},
		{"unused lazy effect", `effect fn main() -> () { Console.log("x") }`, "EF105"},
		{"wrong argument", `effect fn main() -> () { run Console.log(true).provide<Console>(Stdout) }`, "EF106"},
		{"wrong provider", `service Users { effect fn get() -> string } effect fn x() -> string { run Users.get().provide<Users>(Stdout) }`, "EF104"},
		{"incomplete provider", `service Users { effect fn get() -> string } impl Empty for Users { }`, "EF104"},
		{"wrong implementation", `service Users { effect fn get() -> string } impl Broken for Users { effect fn get() -> bool { true } }`, "EF104"},
		{"unknown failure", `effect fn main() -> string { fail Unknown }`, "EF102"},
		{"recover absent tag", `error Bad effect fn a() -> string { "x" } effect fn main() -> string { run a().catch<Bad>("y") }`, "EF107"},
		{"recovery executes effect", `error Bad effect fn a() -> string raises {Bad} { fail Bad } effect fn b() -> string { "y" } effect fn main() -> string { run a().catch<Bad>(run b()) }`, "EF105"},
		{"nested argument execution", `error Bad effect fn a() -> string raises {Bad} { fail Bad } effect fn b(x: string) -> string { x } effect fn main() -> string { run b(run a()) }`, "EF107"},
		{"executed conditional", `effect fn x() -> () { let branch = if true { run Console.log("x") } else { () }; () }`, "EF108"},
		{"declaration collision", `error Users service Users { }`, "EF101"},
		{"local function shadow", `fn f() -> string { "x" } fn x() -> string { let f = "y" f() }`, "EF103"},
		{"bad source", `effect fn x() -> string { "unterminated }`, "EF001"},
		{"unsupported number", `fn x() -> u64 { 42 }`, "EF102"},
		{"scope local escaped", `effect fn x() -> string { scope { let hidden = "x"; () }; hidden }`, "EF102"},
		{"unobserved child failure", `error Bad effect fn child() -> () raises {Bad} { fail Bad } effect fn main() -> () { let child = fork child(); () }`, "EF107"},
		{"timeout failure", `effect fn child() -> () { () } effect fn main() -> () { run child().timeout(1) }`, "EF107"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(tc.source)
			if r.Checked || !hasCode(r, tc.code) {
				t.Fatalf("expected %s: %+v", tc.code, r.Diagnostics)
			}
			if _, _, err := r.Emit(false); err == nil {
				t.Fatal("invalid source emitted")
			}
		})
	}
}
func TestContractsAndEntry(t *testing.T) {
	source := `error Missing error Broken
 service Users { effect fn get() -> string raises {Missing, Broken} }
 impl Memory for Users { effect fn get() -> string { "Ada" } }
 effect fn greeting() -> string raises {Broken, Missing} uses {Users} { run Users.get() }
 effect fn recovered() -> string raises {Broken} uses {Users} { run greeting().catch<Missing>("unknown") }
 effect fn main() -> string raises {Broken} { run recovered().provide<Users>(Memory) }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("%+v", r.Diagnostics)
	}
	symbol := r.Find("recovered")
	if !slices.Equal(symbol.Contract.Errors, []string{"Broken"}) || !slices.Equal(symbol.Actual.Services, []string{"Users"}) {
		t.Fatalf("wrong contract: %+v", symbol)
	}
	if err := r.Entry(); err != nil {
		t.Fatal(err)
	}
	original := Compile(source)
	if original.Revision != r.Revision {
		t.Fatal("unstable revision")
	}
	edited := Compile(source + "\n// edited\n")
	if edited.Revision == r.Revision {
		t.Fatal("stale revision")
	}
	js, decl, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	again, _, _ := original.Emit(false)
	if js != again {
		t.Fatal("nondeterministic emission")
	}
	if !strings.Contains(decl, `Effect.Effect<string, { readonly _tag: "Broken" }, UsersRequirement>`) {
		t.Fatal(decl)
	}
	missing := Compile(`effect fn main() -> () uses {Console} { run Console.log("x") }`)
	if !missing.Checked || missing.Entry() == nil {
		t.Fatal("entry accepted an unprovided service")
	}
	pure := Compile(`fn main() -> string { "x" }`)
	if pure.Entry() == nil {
		t.Fatal("pure entry accepted")
	}
}

func TestRaisesSyntaxPreservesRowsAcrossTargets(t *testing.T) {
	source := `error Missing error Broken
service Users { effect fn get(id: string) -> string raises {Missing, Broken} }
impl Memory for Users { effect fn get(id: string) -> string raises {Missing, Broken} { "Ada" } }
effect fn greeting(id: string) -> string raises {Missing, Broken} uses {Users} { run Users.get(id) }
effect fn recovered(id: string) -> string raises {Broken} uses {Users} { run greeting(id).catch<Missing>("unknown") }
effect fn main() -> string raises {Broken} { run recovered("42").provide<Users>(Memory) }`
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := CompileFor(source, target)
			if !r.Checked {
				t.Fatalf("raises source did not check: %+v", r.Diagnostics)
			}
			if symbol := r.Find("recovered"); symbol == nil || !slices.Equal(symbol.Contract.Errors, []string{"Broken"}) || !slices.Equal(symbol.Actual.Services, []string{"Users"}) {
				t.Fatalf("raises rows changed: %+v", r.Find("recovered"))
			}
			if err := r.Entry(); err != nil {
				t.Fatal(err)
			}
			if target == "go" {
				if _, err := r.EmitGo(); err != nil {
					t.Fatal(err)
				}
			} else if _, _, err := r.Emit(false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestThrowsKeywordReportsMigrationDiagnostic(t *testing.T) {
	r := Compile(`error Bad effect fn main() -> string throws {Bad} { fail Bad }`)
	if r.Checked || len(r.Diagnostics) != 1 {
		t.Fatalf("old spelling should be rejected once: %+v", r.Diagnostics)
	}
	diagnostic := r.Diagnostics[0]
	if diagnostic.Code != "EF002" || !strings.Contains(diagnostic.Message, "replaced by `raises`") {
		t.Fatalf("wrong migration diagnostic: %+v", diagnostic)
	}
	if diagnostic.Span.Length != len("throws") || diagnostic.Span.Line != 1 || diagnostic.Span.Column <= 1 {
		t.Fatalf("diagnostic does not point at old keyword: %+v", diagnostic.Span)
	}
}
func TestDeferredRows(t *testing.T) {
	r := Compile(`effect fn main() -> () { let pending = Console.log("never executed"); () }`)
	if !r.Checked || len(r.Find("main").Actual.Services) != 0 {
		t.Fatalf("constructing a recipe must not require execution: %+v", r.Diagnostics)
	}
}
func runJS(t *testing.T, source, assertions string) string {
	return runJSForTarget(t, "js", source, assertions)
}

func runJSForTarget(t *testing.T, target, source, assertions string) string {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for backend conformance tests")
	}
	r := CompileFor(source, target)
	if !r.Checked {
		t.Fatalf("%s target: %+v", target, r.Diagnostics)
	}
	js, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..")
	if err = os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Join(root, "dist"), "conformance-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "probe.mjs")
	if err = os.WriteFile(path, []byte(js+"\n"+assertions), 0644); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(bun, path).CombinedOutput()
	if err != nil {
		t.Fatalf("runtime: %v\n%s\n%s", err, output, js)
	}
	return string(output)
}
func TestBackendLazinessRecoveryAndFailure(t *testing.T) {
	source := `error Missing error Broken
 service Users { effect fn get(id: string) -> string raises {Missing, Broken} }
 impl Memory for Users {
  effect fn get(id: string) -> string raises {Missing, Broken} {
   if id == "42" { "Ada" } else { if id == "broken" { fail Broken } else { fail Missing } }
  }
 }
 effect fn greeting(id: string) -> string raises {Missing, Broken} uses {Users} { let user = run Users.get(id) "Hi " + user }
 effect fn recovered(id: string) -> string raises {Broken} { run greeting(id).provide<Users>(Memory).catch<Missing>("unknown") }
 effect fn nested() -> string { run recovered(run recovered("42").catch<Broken>("bad")).catch<Broken>("bad") }`
	output := runJS(t, source, `
let calls = 0;
const pending = Effect.provideService(__ef_function_greeting("42"), __ef_service_Users, {
 get: id => Effect.sync(() => { calls++; return "Ada"; })
});
if (calls !== 0) throw new Error("eager construction");
if (await Effect.runPromise(pending) !== "Hi Ada") throw new Error("wrong result");
await Effect.runPromise(pending);
if (calls !== 2) throw new Error("program not replayable");
if (await Effect.runPromise(__ef_function_recovered("missing")) !== "unknown") throw new Error("recovery failed");
if (await Effect.runPromise(__ef_function_recovered("42")) !== "Hi Ada") throw new Error("success changed");
const other = await Effect.runPromiseExit(__ef_function_recovered("broken"));
if (other._tag !== "Failure" || !other.cause.reasons.some(reason => reason._tag === "Fail" && reason.error._tag === "Broken")) throw new Error("unhandled typed failure disappeared");
await Effect.runPromise(__ef_function_nested());
console.log("conformance: passed");
`)
	if !strings.Contains(output, "conformance: passed") {
		t.Fatal(output)
	}
}
func TestExample(t *testing.T) {
	source, err := os.ReadFile("../../examples/main.ef")
	if err != nil {
		t.Fatal(err)
	}
	output := runJS(t, string(source), `await Effect.runPromise(__ef_function_main());`)
	if output != "Hello, Ada\nUnknown user\n" {
		t.Fatal(output)
	}
}
func BenchmarkCompile10KLines(b *testing.B) {
	var source strings.Builder
	for i := 0; i < 2000; i++ {
		source.WriteString("effect fn f" + fmtInt(i) + "() -> string\nraises {}\nuses {}\n{\n\"value\" }\n")
	}
	text := source.String()
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r := Compile(text); !r.Checked {
			b.Fatal(r.Diagnostics)
		}
	}
}
func fmtInt(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func FuzzCompiler(f *testing.F) {
	for _, source := range []string{"", "effect fn main() -> string { \"hello\" }", "service S { effect fn get() -> () }", "fn missing() -> string {", "error E effect fn main() -> string raises {E} { fail E }"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 10000 {
			t.Skip()
		}
		r := Compile(source)
		if r.Checked {
			if _, _, err := r.Emit(false); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestNestingBound(t *testing.T) {
	source := "fn deeplyNested() -> string { " + strings.Repeat("(", 300) + "\"x\"" + strings.Repeat(")", 300) + " }"
	r := Compile(source)
	if r.Checked || !hasCode(r, "EF002") {
		t.Fatal("excessive nesting was admitted")
	}
}
