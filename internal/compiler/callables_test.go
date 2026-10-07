package compiler

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This source boundary protects ordinary named callback composition. The base
// compiler rejects the function-type grammar and cannot call local values;
// HTTP callback tests cover only the builtin transport shape, not this contract.
func TestOrdinaryPureCallbackComposition(t *testing.T) {
	source := `
record State { name: string }
fn advance(step: fn(State, string) -> State, state: State, event: string) -> State {
    step(state, event)
}

fn renamed(state: State, event: string) -> State { State { name: event } }
effect fn main() -> string {
    let next = advance(renamed, State { name: "old" }, "new")
    next.name
}`
	for _, target := range []string{"go", "js"} {
		r := CompileFor(source, target)
		if !r.Checked {
			t.Fatalf("%s ordinary callback rejected: %+v", target, r.Diagnostics)
		}
	}
	r := Compile(source)
	generated, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"go.mod": "module effra.generated\n\ngo 1.27\n", "main.go": generated} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(dir, "run", "."); err != nil || string(output) != "new\n" {
		t.Fatalf("native callback execution: %v %s", err, output)
	}
	if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "new\n" {
		t.Fatalf("JS callback execution: %s", output)
	}
}

func TestCallableTimeoutRequiresARecipeAndValidDuration(t *testing.T) {
	prefix := `fn keep(x:string)->string{x}
fn apply(cb:fn(string)->string)->string{cb("ok")}
effect fn callback(x:string)->string{x}
effect fn pureEmpty()->(fn(string)->string){keep}
`
	for _, test := range []struct{ name, body string }{
		{"pure name", `effect fn main()->void{keep.timeout(10);void}`},
		{"pure alias", `effect fn main()->void{let f=keep;f.timeout(10);void}`},
		{"pure parameter", `fn misuse(f:fn(string)->string)->void{f.timeout(10);void} effect fn main()->void{void}`},
		{"pure argument", `effect fn main()->void{apply(keep.timeout(10));void}`},
		{"effect callback", `effect fn main()->void{callback.timeout(10);void}`},
		{"pure invalid duration", `effect fn main()->void{keep.timeout("wrong");void}`},
		{"recipe invalid duration", `effect fn main()->void{pureEmpty().timeout("wrong");void}`},
		{"recipe deferred duration", `effect fn main()->void{pureEmpty().timeout(pureEmpty());void}`},
	} {
		for _, target := range []string{"go", "js"} {
			t.Run(test.name+"/"+target, func(t *testing.T) {
				r := CompileFor(prefix+test.body, target)
				if r.Checked || !hasCode(r, "EF106") {
					t.Fatalf("invalid timeout admitted: %+v", r.Diagnostics)
				}
			})
		}
	}
	// A validity refusal must still account for eager child evaluation. The
	// duration is the wrong value type, but its failures/services still execute.
	invalidDuration := prefix + `error Missing
effect fn duration()->string raises {Missing} uses {Clock}{run Clock.sleep(1);if true {fail Missing} else {"wrong"}}
effect fn main()->void{pureEmpty().timeout(run duration());void}`
	for _, target := range []string{"go", "js"} {
		r := CompileFor(invalidDuration, target)
		if r.Checked || !hasCode(r, "EF106") || !hasCode(r, "EF107") || !hasCode(r, "EF108") {
			t.Fatalf("invalid duration evaluation erased: %+v", r.Diagnostics)
		}
	}
	source := prefix + `effect fn main()->string{let f=run pureEmpty().timeout(1000).catch<Timeout>(keep).provide<Scheduler>(LiveScheduler);f("ok")}`
	for _, target := range []string{"go", "js"} {
		r := CompileFor(source, target)
		if !r.Checked {
			t.Fatalf("valid callable recipe: %+v", r.Diagnostics)
		}
		info, err := r.TypeAt(strings.Index(source, "pureEmpty().timeout"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Type.Contract.Kind != "recipe" || info.Type.Type.Kind != "callable" {
			t.Fatalf("timeout stripped callable result: %+v", info.Type)
		}
	}
}

func TestFiniteCallbackRowsAreInstantiatedFromArguments(t *testing.T) {
	source := `error Missing error Broken
service Directory { effect fn get(key: string) -> string raises {Missing} }
service Audit { effect fn note(key: string) -> string raises {Broken} }
effect fn first(key: string) -> string raises {Missing} uses {Directory} { run Directory.get(key) }
effect fn second(key: string) -> string raises {Broken} uses {Audit} { run Audit.note(key) }
effect fn chain<E: raises, R: uses>(
    a: effect fn(string) -> string raises {E} uses {R},
    b: effect fn(string) -> string raises {E} uses {R}, key: string
) -> string raises {E} uses {R} { let next = run a(key); run b(next) }
effect fn request() -> string raises {Missing, Broken} uses {Directory, Audit} { run chain(first, second, "42") }
effect fn main() -> void { void }`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	info, err := r.TypeAt(strings.Index(source, `chain(first`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(info.Type.Errors, []string{"Broken", "Missing"}) || !slices.Equal(info.Type.Services, []string{"Audit", "Directory"}) {
		t.Fatalf("argument-driven union lost: %+v", info.Type)
	}
	if len(info.ExecutedFailures) > 0 || len(info.ExecutedRequirements) > 0 {
		t.Fatal("constructing callback recipe executed it")
	}
	if len(info.Type.Application.RowArguments) != 2 {
		t.Fatalf("row bindings missing: %+v", info.Type.Application)
	}
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(declaration, `__ef_row_0_E extends { readonly _tag: string }`) || !strings.Contains(declaration, `Effect.Effect<string, __ef_row_0_E | __ef_row_1_E, __ef_row_0_R | __ef_row_1_R>`) {
		t.Fatalf("row declaration erased: %s", declaration)
	}
	t.Run("strict TypeScript consumer", func(t *testing.T) {
		checkStrictTypeScript(t, declaration, `import { chain, first, second } from "./generated.mjs";
import type { Effect } from "effect";
import type { DirectoryRequirement, AuditRequirement } from "./generated.mjs";
const recipe = chain(first,second,"42");
type Expected = Effect.Effect<string, {readonly _tag:"Missing"}|{readonly _tag:"Broken"}, DirectoryRequirement|AuditRequirement>;
const rows: Expected = recipe;
declare const expected: Expected;
const reciprocal: typeof recipe = expected;
// @ts-expect-error Both callback rows must remain visible.
const erased: Effect.Effect<string, never, never> = recipe;
// @ts-expect-error Pure callbacks are not lifted implicitly.
chain((x: string) => x, second, "42");
void rows; void reciprocal; void erased;`)
	})
	projection := r.ProjectSymbol(r.Find("chain"))
	if !projection.Complete {
		t.Fatal(projection.Error)
	}
	definitions := map[string]RowParameter{}
	for _, row := range projection.Rows {
		for _, parameter := range row.Parameters {
			definitions[parameter.ID] = parameter
		}
	}
	for _, parameter := range r.Find("chain").Contract.Callable.RowParameters {
		if definitions[parameter.ID] != parameter {
			t.Fatalf("undefined generic row parameter: %+v", parameter)
		}
	}
	for _, boundary := range []struct {
		name string
		used int
		set  func(int)
	}{
		{"row labels", projection.Usage.RowLabels, func(n int) { r.TypeProjectionLimits.RowLabels = n }},
		{"row names", projection.Usage.NameBytes, func(n int) { r.TypeProjectionLimits.NameBytes = n }},
		{"compatibility metadata", projection.Usage.CompatibilityBytes, func(n int) { r.TypeProjectionLimits.CompatibilityBytes = n }},
		{"complete response", projection.Usage.ResponseBytes, func(n int) { r.TypeProjectionLimits.ResponseBytes = n }},
	} {
		t.Run("generic row budget "+boundary.name, func(t *testing.T) {
			r.TypeProjectionLimits = defaultProjectionLimits
			boundary.set(boundary.used)
			if p := r.ProjectSymbol(r.Find("chain")); !p.Complete {
				t.Fatal(p.Error)
			}
			boundary.set(boundary.used - 1)
			if p := r.ProjectSymbol(r.Find("chain")); p.Complete || len(p.Types) > 0 || len(p.Rows) > 0 {
				t.Fatalf("partial authority returned: %+v", p)
			}
		})
	}
	r.TypeProjectionLimits = defaultProjectionLimits
	formatted, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if result := Compile(formatted.Text); !result.Checked {
		t.Fatalf("row formatter changed admission: %+v", result.Diagnostics)
	}
	for _, test := range []struct{ name, old, new, code string }{
		{"missing callback failure", `request() -> string raises {Missing, Broken}`, `request() -> string raises {Missing}`, "EF107"},
		{"missing callback service", `request() -> string raises {Missing, Broken} uses {Directory, Audit}`, `request() -> string raises {Missing, Broken} uses {Directory}`, "EF108"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(strings.Replace(source, test.old, test.new, 1))
			found := false
			for _, d := range r.Diagnostics {
				found = found || d.Code == test.code
			}
			if r.Checked || !found {
				t.Fatalf("missing precise %s: %+v", test.code, r.Diagnostics)
			}
		})
	}
}

func TestClosedCallbackRowsAndVariance(t *testing.T) {
	for _, test := range []struct {
		name, callback, expected string
		checked                  bool
	}{
		{"narrower rows", `effect fn operation(x: string) -> string { x }`, `effect fn(string) -> string raises {A} uses {Users}`, true},
		{"wider failure", `effect fn operation(x: string) -> string raises {A} { x }`, `effect fn(string) -> string`, false},
		{"wider service", `effect fn operation(x: string) -> string uses {Users} { run Users.get(x) }`, `effect fn(string) -> string`, false},
		{"parameter mismatch", `effect fn operation(x: bool) -> string { "ok" }`, `effect fn(string) -> string`, false},
		{"result mismatch", `effect fn operation(x: string) -> bool { true }`, `effect fn(string) -> string`, false},
		{"pure is distinct", `fn operation(x: string) -> string { x }`, `effect fn(string) -> string`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `error A service Users { effect fn get(x: string) -> string } ` + test.callback + ` fn store(cb: ` + test.expected + `) -> void { void } effect fn main() -> void { store(operation) }`
			r := Compile(source)
			if r.Checked != test.checked {
				t.Fatalf("checked=%v, diagnostics=%+v", r.Checked, r.Diagnostics)
			}
			if !test.checked {
				found := false
				for _, d := range r.Diagnostics {
					found = found || d.Code == "EF106"
				}
				if !found {
					t.Fatalf("expected callable contract mismatch, got %+v", r.Diagnostics)
				}
			}
		})
	}
}

func TestFiniteRowSolverDiagnosesUnsupportedConstraints(t *testing.T) {
	for _, source := range []string{
		`effect fn invoke<E: raises, F: raises>(cb: effect fn(string) -> string raises {E,F}) -> string raises {E,F} {run cb("x")} effect fn main()->void{void}`,
		`effect fn invoke<E: raises>(cb: effect fn(string) -> string) -> string raises {E} {run cb("x")} effect fn value(x:string)->string{x} effect fn main()->void{let pending=invoke(value);void}`,
		`error Missing effect fn invoke<E: raises>(cb: effect fn(string)->string raises {E}) -> string raises {E} {run cb("x").catch<Missing>("fallback")} effect fn main()->void{void}`,
		`service Users {effect fn get()->string} impl Memory for Users {effect fn get()->string{"x"}} effect fn invoke<R: uses>(cb: effect fn(string)->string uses {R}) -> string uses {R} {run cb("x").provide<Users>(Memory)} effect fn main()->void{void}`,
	} {
		r := Compile(source)
		found := false
		for _, d := range r.Diagnostics {
			found = found || d.Code == "EF125"
		}
		if r.Checked || !found {
			t.Fatalf("unsupported row constraint admitted: %+v", r.Diagnostics)
		}
	}
}

func TestRowForwardingRecoveryAndProvisionAcrossTargets(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "examples", "callables-service.ef"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(fixture)
	for _, target := range []string{"go", "js"} {
		r := CompileFor(source, target)
		if !r.Checked {
			t.Fatalf("%s: %+v", target, r.Diagnostics)
		}
	}
	r := Compile(source)
	generated, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"go.mod": string(r.ModuleFile()), "main.go": generated} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(dir, "run", "."); err != nil || string(output) != "name:42:logged\n" {
		t.Fatalf("native row forwarding: %v %s", err, output)
	}
	if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "name:42:logged\n" {
		t.Fatalf("JS row forwarding: %s", output)
	}
}

func TestCallableContractsInRecordsAndProvidersAcrossTargets(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "examples", "callables-state.ef"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(fixture)
	for _, target := range []string{"go", "js"} {
		r := CompileFor(source, target)
		if !r.Checked {
			t.Fatalf("%s placement: %+v", target, r.Diagnostics)
		}
	}
	r := Compile(source)
	generated, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"go.mod": string(r.ModuleFile()), "main.go": generated} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(dir, "run", "."); err != nil || string(output) != "record:provider\n" {
		t.Fatalf("native callback placement: %v %s", err, output)
	}
	if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "record:provider\n" {
		t.Fatalf("JS callback placement: %s", output)
	}
}
