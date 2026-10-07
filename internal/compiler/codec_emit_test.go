package compiler

import (
	goparser "go/parser"
	gotoken "go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

const codecEmissionTypes = `import Json "effra/json"

record User {
    id: i64
    name: string
}

record Archive {
    owner: User
    sealed: bool
}

enum Event {
    Created { user: User }
    Closed
}

derive userJson = Json.codec<User>(maxBodyBytes: 4096, maxDepth: 4)
derive userMirror = Json.codec<User>(maxBodyBytes: 4096, maxDepth: 4)
derive eventJson = Json.codec<Event>
derive archiveJson = Json.codec<Archive>
`

// codecUsingProgram executes userJson in both directions and eventJson's
// encode only; archiveJson is declared and never executed.
const codecUsingProgram = codecEmissionTypes + `
effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    let user = run userJson.decode("{\"id\":\"7\",\"name\":\"Ada\"}")
    let closed = Event.Closed {}
    run userJson.encode(user) + run eventJson.encode(closed)
}
`

const codecIdleProgram = codecEmissionTypes + `
effect fn main() -> string {
    "ok"
}
`

func codecPlanByRoot(t *testing.T, r *Result, root string) *CodecPlan {
	t.Helper()
	for _, plan := range r.CodecPlans {
		if plan.Nodes[plan.Root].ID == dataIdentity(map[bool]string{true: "enum", false: "record"}[root == "Event"], root) {
			return plan
		}
	}
	t.Fatalf("no plan rooted at %s", root)
	return nil
}

// The application plan retains a codec plan, its nominal declarations, the
// codec runtime module and the lowering helpers only through an executed
// direction, and the emitted Go references exactly the selected modules.
func TestCodecPlansAreSelectedOnlyThroughExecutedDirections(t *testing.T) {
	r, plan := checkedApplicationPlan(t, codecUsingProgram, GoGenerationBuild)
	user, event, archive := codecPlanByRoot(t, r, "User"), codecPlanByRoot(t, r, "Event"), codecPlanByRoot(t, r, "Archive")
	if len(r.CodecPlans) != 3 {
		t.Fatalf("identical witnesses must share one plan: %d plans", len(r.CodecPlans))
	}
	requirePlanned(t, plan, RequiresCodecPlan, user.ID, event.ID)
	requireUnplanned(t, plan, RequiresCodecPlan, archive.ID)
	requirePlanned(t, plan, RequiresFunction, symbolIdentity(t, r, "userJson.decode"), symbolIdentity(t, r, "userJson.encode"), symbolIdentity(t, r, "eventJson.encode"))
	requireUnplanned(t, plan, RequiresFunction, symbolIdentity(t, r, "eventJson.decode"), symbolIdentity(t, r, "archiveJson.decode"), symbolIdentity(t, r, "archiveJson.encode"), symbolIdentity(t, r, "userMirror.decode"))
	requirePlanned(t, plan, RequiresDeclaration, dataIdentity("record", "User"), dataIdentity("enum", "Event"))
	requireUnplanned(t, plan, RequiresDeclaration, dataIdentity("record", "Archive"))
	requirePlanned(t, plan, RequiresHelper, codecLoweringHelper)
	if requirement, found := plan.Requirement(RequiresRuntimeModule, string(rt.RuntimeModuleCodec)); !found || requirement.Via != user.ID || requirement.Reason != "codec-plan" {
		t.Fatalf("codec module provenance: %+v %v", requirement, found)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "main.go", goSource, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtimeReferenceModules(t, runtimeDeclarationModules(t), file); !maps.Equal(got, nonCoreModules(plan.RuntimeModules()...)) || !got[rt.RuntimeModuleCodec] {
		t.Fatalf("emitted runtime references %v, planned %v", got, plan.RuntimeModules())
	}
	sources, err := application.Plan.RuntimeSources()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := sources["codec_json.go"]; !found {
		t.Fatalf("application runtime omits the codec engine: %v", slices.Sorted(maps.Keys(sources)))
	}
	for _, want := range []string{"var " + goCodecPlanName(user), "var " + goCodecPlanName(event), "func efCodecConstruct_User(", "func efCodecProject_Event("} {
		if strings.Count(goSource, want) != 1 {
			t.Fatalf("Go emission has %d of %q", strings.Count(goSource, want), want)
		}
	}
	for _, unwanted := range []string{goCodecPlanName(archive), "efCodecConstruct_Archive", "efCodecFunction_archiveJson"} {
		if strings.Contains(goSource, unwanted) {
			t.Fatalf("Go emission retains unexecuted %s", unwanted)
		}
	}
}

// Declared but unexecuted witnesses retain nothing: the native application
// is the minimal one, without the codec module, helpers or plans.
func TestUnexecutedCodecsOmitTheCodecModule(t *testing.T) {
	r, plan := checkedApplicationPlan(t, codecIdleProgram, GoGenerationBuild)
	if len(plan.Identities(RequiresCodecPlan)) != 0 || plan.Requires(RequiresHelper, codecLoweringHelper) || plan.Requires(RequiresRuntimeModule, string(rt.RuntimeModuleCodec)) {
		t.Fatalf("idle codecs retained: %+v", plan.Requirements)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(goSource, "efCodec") || strings.Contains(goSource, "er.Codec") {
		t.Fatal("idle codecs emitted Go codec code")
	}
	sources, err := application.Plan.RuntimeSources()
	if err != nil {
		t.Fatal(err)
	}
	for _, unused := range []string{"codec.go", "codec_json.go"} {
		if _, found := sources[unused]; found {
			t.Fatalf("idle codecs selected %s", unused)
		}
	}
}

// A JS module exports every witness as public surface, so it carries the
// engine exactly once when it derives any codec and never otherwise.
func TestJSCodecEngineIsEmittedOnceWhenDerived(t *testing.T) {
	engine := "const __ef_codecCompile = "
	for _, tc := range []struct {
		source string
		count  int
	}{
		{codecUsingProgram, 1},
		{codecIdleProgram, 1},
		{"effect fn main() -> string {\n    \"ok\"\n}\n", 0},
		{"import Json \"effra/json\"\neffect fn main() -> string {\n    \"ok\"\n}\n", 0},
	} {
		r := Compile(tc.source)
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		js, decl, err := r.Emit(false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(js, engine) != tc.count || strings.Count(js, "const __ef_codecDecode = ") != tc.count {
			t.Fatalf("engine emitted %d times, want %d", strings.Count(js, engine), tc.count)
		}
		if tc.count == 0 {
			continue
		}
		if strings.Count(js, "__ef_codecCompile({") != 3 {
			t.Fatalf("JS must compile one codec per shared plan")
		}
		if !strings.Contains(decl, `declare const __ef_codec_witness_userJson: { readonly decode: (arg_input: string) => Effect.Effect<User, { readonly _tag: "JsonDecodeFailure" }, never>; readonly encode: (arg_value: User) => Effect.Effect<string, { readonly _tag: "JsonEncodeFailure" }, never> };`+"\nexport { __ef_codec_witness_userJson as userJson };\n") {
			t.Fatalf("witness declaration:\n%s", decl)
		}
		if strings.Contains(js, "as userJson.decode") || strings.Contains(decl, "as userJson.decode") {
			t.Fatal("direction functions must be exported through their witness")
		}
	}
}

// Emission is a function of the checked program: repeated emission of both
// targets is byte-identical.
func TestCodecEmissionIsDeterministic(t *testing.T) {
	emit := func() (string, string) {
		r := Compile(codecUsingProgram)
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		goSource, _, err := emitGoApplication(r, GoGenerationBuild)
		if err != nil {
			t.Fatal(err)
		}
		js, _, err := r.Emit(false)
		if err != nil {
			t.Fatal(err)
		}
		return goSource, js
	}
	goFirst, jsFirst := emit()
	for range 3 {
		if goAgain, jsAgain := emit(); goAgain != goFirst || jsAgain != jsFirst {
			t.Fatal("codec emission is not deterministic")
		}
	}
}

// The exported witness is a frozen object whose directions are ordinary
// Effect functions with the direction's tagged failure, as a host consumer
// importing the module sees it.
func TestJSCodecWitnessExports(t *testing.T) {
	output := runJSConsumer(t, codecIdleProgram, `import { Effect } from "effect";
import { userJson, eventJson } from "./generated.mjs";
if (!Object.isFrozen(userJson)) throw new Error("witness is not frozen");
const user = await Effect.runPromise(userJson.decode('{"name":"Ada","id":"-1"}'));
if (user.id !== -1n || user.name !== "Ada") throw new Error("decoded " + JSON.stringify(user, (_, v) => typeof v === "bigint" ? v.toString() : v));
const encoded = await Effect.runPromise(eventJson.encode({ _tag: "Event.Created", user }));
if (encoded !== '{"_tag":"Created","user":{"id":"-1","name":"Ada"}}') throw new Error("encoded " + encoded);
const failure = await Effect.runPromise(Effect.flip(userJson.decode('{"id":"1"}')));
if (failure._tag !== "JsonDecodeFailure" || failure.message !== 'codec decode: missing at ["name"]' || failure.issue.reason !== "missing") throw new Error("failure " + JSON.stringify(failure));
const illFormed = await Effect.runPromise(Effect.flip(userJson.decode('"\ud800"')));
if (illFormed.message !== "codec decode: invalid-unicode") throw new Error("ill-formed " + JSON.stringify(illFormed));
`)
	if output != "" {
		t.Fatalf("unexpected JS output: %s", output)
	}
}

// Direction functions are ordinary function values: bound to a local, passed
// to a typed parameter and forwarded through a bundled callback function,
// with the same result on both targets.
func TestDerivedDirectionsAreOrdinaryFunctionValues(t *testing.T) {
	source := `import Json "effra/json"
import Fns "effra/functions"

derive textJson = Json.codec<string>

effect fn apply(decode: effect fn(string) -> string raises { JsonDecodeFailure }, input: string) -> string raises { JsonDecodeFailure } {
    run decode(input)
}

effect fn main() -> string raises { JsonDecodeFailure } {
    let decode = textJson.decode
    let direct = run decode("\"a\"")
    let passed = run apply(textJson.decode, "\"b\"")
    let forwarded = run Fns.call(textJson.decode, "\"c\"")
    direct + passed + forwarded
}
`
	if output := runJS(t, source, `if (await Effect.runPromise(__ef_function_main()) !== "abc") throw new Error("wrong JS result");`); output != "" {
		t.Fatalf("unexpected JS output: %s", output)
	}
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	application, err := r.GoApplication(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := runGoCommand(writeGoApplication(t, r, application), "run", "."); err != nil || string(output) != "abc\n" {
		t.Fatalf("Go result: %v\n%s", err, output)
	}
}

// runJSConsumer emits source as generated.mjs beside a host consumer module
// and runs the consumer under Bun, so it sees only the module's exports.
func runJSConsumer(t *testing.T, source, consumer string) string {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for backend conformance tests")
	}
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	module, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("..", "..", "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Join("..", "..", "dist"), "conformance-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for file, text := range map[string]string{"generated.mjs": module, "consumer.mjs": consumer} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	output, err := exec.Command(bun, filepath.Join(dir, "consumer.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("runtime: %v\n%s", err, output)
	}
	return string(output)
}

// A witness's JS binding is generated and its source name is only an export
// alias, as for ordinary functions. A witness named after a host global, the
// Effect import or a generated binding therefore checks, runs as it does on
// Go, and is imported by host code under its source name.
func TestJSCodecWitnessNamesDoNotShadowModuleBindings(t *testing.T) {
	for _, name := range []string{"Object", "TextEncoder", "Effect", "__ef_codecText"} {
		t.Run(name, func(t *testing.T) {
			source := `import Json "effra/json"

derive ` + name + ` = Json.codec<string>

effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    run ` + name + `.encode(run ` + name + `.decode("\"ok\""))
}
`
			r := Compile(source)
			if !r.Checked {
				t.Fatal(r.Diagnostics)
			}
			application, err := r.GoApplication(GoGenerationBuild)
			if err != nil {
				t.Fatal(err)
			}
			if output, err := runGoCommand(writeGoApplication(t, r, application), "run", "."); err != nil || string(output) != "\"ok\"\n" {
				t.Fatalf("Go result: %v\n%s", err, output)
			}
			output := runJSConsumer(t, source, `import { Effect } from "effect";
import { `+name+` as witness, main } from "./generated.mjs";
if (!Object.isFrozen(witness)) throw new Error("witness is not frozen");
const decoded = await Effect.runPromise(witness.decode('"x"'));
const encoded = await Effect.runPromise(witness.encode(decoded));
const ran = await Effect.runPromise(main());
console.log(decoded + " " + encoded + " " + ran);
`)
			if output != "x \"x\" \"ok\"\n" {
				t.Fatalf("JS result: %s", output)
			}
			_, declaration, err := r.Emit(false)
			if err != nil {
				t.Fatal(err)
			}
			binding := "__ef_codec_witness_" + name
			if !strings.Contains(declaration, "declare const "+binding+": {") || !strings.Contains(declaration, "export { "+binding+" as "+name+" };") {
				t.Fatalf("witness declaration must alias its generated binding:\n%s", declaration)
			}
		})
	}
}
