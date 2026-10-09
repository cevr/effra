package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const closedDataSource = `record User { id: string, name: string }
enum RunState {
 Idle
 Running { runId: string }
 Done { code: i64 }
}
error Invalid { message: string }
fn label(state: RunState) -> string {
 match state {
  RunState.Idle => "idle"
  RunState.Running { runId } => "running " + runId
  RunState.Done { code } => "done"
 }
}
effect fn interpret(state: RunState) -> string raises {Invalid} {
 match state {
  RunState.Idle => "ok"
  RunState.Running { runId } => fail Invalid { message: runId }
  RunState.Done { code } => "done"
 }
}
effect fn failWithPayload() -> string raises {Invalid} { fail Invalid { message: "bad" } }
effect fn main() -> string raises {Invalid} {
 let user = User { id: "u1", name: "Ada" }
 let state = RunState.Running { runId: "42" }
 // Host probes call these failure paths directly.
 let probeFailure = failWithPayload
 let probeInterpret = interpret
 if user.name == "Ada" { label(state) } else { "wrong user" }
}`

func TestClosedDataContractsAndExhaustiveness(t *testing.T) {
	r := Compile(closedDataSource)
	if !r.Checked {
		t.Fatalf("closed data should check: %+v", r.Diagnostics)
	}
	if len(r.Declarations) != 3 {
		t.Fatalf("declarations: %+v", r.Declarations)
	}
	state := r.Find("label")
	if state == nil || state.Contract.Success != "string" || state.Contract.Type.Kind != "primitive" {
		t.Fatalf("label contract: %+v", state)
	}
	interpret := r.Find("interpret")
	if interpret == nil || !slices.Contains(interpret.Actual.Errors, "Invalid") {
		t.Fatalf("match branch failure row lost: %+v", interpret)
	}
	var enumDecl *Declaration
	for i := range r.Declarations {
		if r.Declarations[i].Kind == "enum" {
			enumDecl = &r.Declarations[i]
		}
	}
	if enumDecl == nil || len(enumDecl.Variants) != 3 || len(enumDecl.Variants[1].Fields) != 1 || enumDecl.Variants[1].Fields[0].TypeRef.Kind != "primitive" {
		t.Fatalf("enum declaration: %+v", r.Declarations)
	}
	info, err := r.TypeAt(strings.Index(closedDataSource, `"running "`))
	if err != nil || info.Type.Success != "string" {
		t.Fatalf("match-arm type query lost: %+v %v", info, err)
	}
	missing := strings.Replace(closedDataSource, "  RunState.Done { code } => \"done\"\n", "", 1)
	if bad := Compile(missing); bad.Checked || !hasCode(bad, "EF117") {
		t.Fatalf("missing arm admitted: %+v", bad.Diagnostics)
	}
	wrongPayload := strings.Replace(closedDataSource, `RunState.Running { runId: "42" }`, `RunState.Running { code: 42 }`, 1)
	if bad := Compile(wrongPayload); bad.Checked || !hasCode(bad, "EF114") {
		t.Fatalf("wrong payload admitted: %+v", bad.Diagnostics)
	}
}

func TestClosedDataDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{"duplicate arm", `enum State { Idle Running } fn main(state: State) -> string { match state { State.Idle => "a" State.Idle => "b" State.Running => "c" } }`, "EF117"},
		{"unknown arm", `enum State { Idle } fn main(state: State) -> string { match state { State.Missing => "x" } }`, "EF116"},
		{"extra payload", `record User { id: string } fn main() -> string { User { id: "x", extra: "y" } "x" }`, "EF114"},
		{"missing payload", `record User { id: string, name: string } fn main() -> string { User { id: "x" } "x" }`, "EF114"},
		{"other variant field", `enum State { Idle Running { runId: string } } fn main(state: State) -> string { match state { State.Idle { runId } => "x" State.Running { runId } => runId } }`, "EF114"},
		{"catch all", `enum State { Idle } fn main(state: State) -> string { match state { _ => "x" } }`, "EF118"},
		{"nominal identity", `record A { id: string } record B { id: string } fn take(value: A) -> string { value.id } fn main() -> string { take(B { id: "x" }) }`, "EF106"},
		{"recursive layout", `record Node { next: Node } fn main() -> string { "x" }`, "EF119"},
		{"reserved variant discriminator", `enum State { Ready { _tag: string } } fn main() -> string { "x" }`, "EF120"},
		{"reserved error discriminator", `error Invalid { _tag: string } fn main() -> string { "x" }`, "EF120"},
		{"reserved data name", `record File { path: string } fn main() -> string { "x" }`, "EF101"},
		{"reserved latch handle", `record Latch { state: string } effect fn wait(latch: Latch) -> void uses {Sync} { run Sync.await(latch) } effect fn main() -> void { void }`, "EF101"},
		{"error is not a success value", `error Invalid { message: string } record Envelope { failure: Invalid } fn main() -> string { "x" }`, "EF102"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(tc.source)
			if r.Checked || !hasCode(r, tc.code) {
				t.Fatalf("expected %s: %+v", tc.code, r.Diagnostics)
			}
		})
	}
}

func TestClosedDataParserDisambiguatesControlBraces(t *testing.T) {
	source := `enum State { Idle Running { id: string } }
effect fn main() -> string {
 let State = State.Idle()
 if true { "if-ok" } else { "if-bad" }
}
effect fn localIf() -> string {
 let State = true
 if State { "if-ok" } else { "if-bad" }
}
effect fn localMatch() -> string {
 let State = State.Idle()
 match State {
  State.Idle => "idle"
  State.Running { id } => id
 }
}
effect fn fromConstructor() -> string {
 match State.Running { id: "x" } {
  State.Idle => "idle"
  State.Running { id } => id
 }
}
effect fn fromEmptyConstructor() -> string {
 match State.Idle {} {
  State.Idle => "idle"
  State.Running { id } => id
 }
}`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("control and match braces should parse: %+v", r.Diagnostics)
	}
	if _, _, err := r.Emit(false); err != nil {
		t.Fatalf("JS lowering rejected disambiguated braces: %v", err)
	}
	if _, err := r.EmitGo(); err != nil {
		t.Fatalf("Go lowering rejected disambiguated braces: %v", err)
	}
}

func TestClosedDataComputedKeysPreserveProtoFields(t *testing.T) {
	source := `record Data { __proto__: string }
effect fn main() -> string { let value = Data { __proto__: "safe" } value.__proto__ }`
	output := runJS(t, source, `if (await Effect.runPromise(__ef_function_main()) !== "safe") throw new Error("computed __proto__ field was lost");`)
	if output != "" {
		t.Fatalf("unexpected output: %s", output)
	}
	r := Compile(source)
	if _, err := r.EmitGo(); err != nil {
		t.Fatalf("Go lowering rejected computed field: %v", err)
	}
}

func TestClosedDataRecordTagFieldIsNotReserved(t *testing.T) {
	r := Compile(`record ErrorDetails { _tag: string } effect fn main() -> string { let details = ErrorDetails { _tag: "data" } details._tag }`)
	if !r.Checked {
		t.Fatalf("record _tag should remain ordinary data: %+v", r.Diagnostics)
	}
}

func TestClosedDataRejectsJavaScriptDeclarationCollisions(t *testing.T) {
	for _, source := range []string{
		`service Foo { effect fn get() -> string } record FooRequirement { value: string } effect fn main() -> string { "x" }`,
		`error Invalid { message: string } record InvalidError { value: string } effect fn main() -> string { "x" }`,
		`record class { name: string } effect fn main() -> string { let r = class { name: "ok" } r.name }`,
	} {
		r := CompileFor(source, "js")
		if r.Checked || !hasCode(r, "EF110") {
			t.Fatalf("JavaScript declaration collision was admitted: %+v", r.Diagnostics)
		}
	}
}

func TestClosedDataLoweringIsAvailable(t *testing.T) {
	r := Compile(closedDataSource)
	if !r.Checked {
		t.Fatalf("closed data should check: %+v", r.Diagnostics)
	}
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatalf("JS lowering rejected closed data: %v", err)
	}
	if !strings.Contains(declaration, "__ef_brand_User") || !strings.Contains(declaration, `readonly _tag: "RunState.Running"`) || !strings.Contains(declaration, "message: string") {
		t.Fatalf("JS declarations lost nominal payload metadata: %s", declaration)
	}
	if _, err := r.EmitGo(); err != nil {
		t.Fatalf("Go lowering rejected closed data: %v", err)
	}
}

func TestClosedDataGoVariantNamesDoNotCollide(t *testing.T) {
	source := `enum AB { C }
enum A { BC }
effect fn main() -> string {
    let other = AB.C()
    match A.BC() { A.BC => "ok" }
}`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("collision fixture should check: %+v", r.Diagnostics)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(goSource, "type efTypeV_2_AB_1_C struct") || !strings.Contains(goSource, "type efTypeV_1_A_2_BC struct") {
		t.Fatalf("qualified enum types missing: %s", goSource)
	}
	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module effra.generated\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := runGoCommand(dir, "build", "."); err != nil {
		t.Fatalf("generated Go enum names collided: %v\n%s", err, output)
	}
}

func TestClosedDataRunsOnBothBackends(t *testing.T) {
	r := Compile(closedDataSource)
	if !r.Checked {
		t.Fatalf("closed data should check: %+v", r.Diagnostics)
	}
	output := runJS(t, closedDataSource, `if (await Effect.runPromise(__ef_function_main()) !== "running 42") throw new Error("wrong JS result"); const failed = await Effect.runPromiseExit(__ef_function_failWithPayload()); const interpreted = await Effect.runPromiseExit(__ef_function_interpret({ _tag: "RunState.Running", runId: "branch" })); if (failed._tag !== "Failure" || failed.cause.reasons[0].error.message !== "bad" || interpreted.cause.reasons[0].error.message !== "branch") throw new Error("payload failure was lost"); console.log("closed-data: passed");`)
	if !strings.Contains(output, "closed-data: passed") {
		t.Fatal(output)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module effra.generated\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	goAssertions := `package main
import("testing"; er "effra.generated/runtime")
func TestClosedDataPayload(t *testing.T) { er.Run(func(fc *er.FiberContext) er.Exit[struct{}] { failed := efFunction_failWithPayload()(efContext{Runtime: fc}); interpreted := efFunction_interpret(efTypeV_8_RunState_7_Running{EfField_5_runId:"branch"})(efContext{Runtime: fc}); for _, exit := range []er.Exit[string]{failed, interpreted} { if exit.Failure == nil { t.Fatal("failure missing") }; payload, ok := exit.Failure.Payload.(efType_Invalid); if !ok || (payload.EfField_7_message != "bad" && payload.EfField_7_message != "branch") { t.Fatalf("payload lost: %#v", exit.Failure.Payload) } }; return er.Succeed(struct{}{}) }) }
`
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(goAssertions), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := runGoCommand(dir, "test", "-race", "."); err != nil {
		t.Fatalf("generated Go payload: %v\n%s", err, output)
	}
	binary := filepath.Join(dir, "closed-data")
	if output, err := runGoCommand(dir, "build", "-o", binary, "."); err != nil {
		t.Fatalf("generated Go: %v\n%s\n%s", err, output, goSource)
	}
	goOutput, err := exec.Command(binary).CombinedOutput()
	if err != nil || string(goOutput) != "running 42\n" {
		t.Fatalf("wrong Go result: %v\n%s", err, goOutput)
	}
}

func TestClosedDataMatchRowsAccumulateAcrossAllBranches(t *testing.T) {
	for name, source := range map[string]string{
		"failure first": `error Bad enum State { Ready Done } effect fn main() -> string raises {Bad} { match State.Ready() { State.Ready => fail Bad State.Done => "ok" } }`,
		"failure last":  `error Bad enum State { Ready Done } effect fn main() -> string raises {Bad} { match State.Ready() { State.Ready => "ok" State.Done => fail Bad } }`,
		"all failures":  `error Bad error Other enum State { Ready Done } effect fn main() -> string raises {Bad, Other} { match State.Ready() { State.Ready => fail Bad State.Done => fail Other } }`,
	} {
		t.Run(name, func(t *testing.T) {
			r := Compile(source)
			if !r.Checked {
				t.Fatalf("match rows should be admitted: %+v", r.Diagnostics)
			}
			main := r.Find("main")
			expected := map[string][]string{"failure first": {"Bad"}, "failure last": {"Bad"}, "all failures": {"Bad", "Other"}}[name]
			if main == nil || !slices.Equal(main.Actual.Errors, expected) {
				t.Fatalf("match rows lost: %+v", main)
			}
		})
	}

	withService := `error Bad service Users { effect fn get() -> string } enum State { Ready Done } effect fn main() -> string raises {Bad} uses {Users} { match State.Ready() { State.Ready => { run Users.get() fail Bad } State.Done => "ok" } }`
	r := Compile(withService)
	if !r.Checked {
		t.Fatalf("failure and service rows should survive a match: %+v", r.Diagnostics)
	}
	main := r.Find("main")
	if main == nil || !slices.Equal(main.Actual.Errors, []string{"Bad"}) || !slices.Equal(main.Actual.Services, []string{"Users"}) {
		t.Fatalf("match rows lost: %+v", main)
	}
}

func TestClosedDataFieldAccessRequiresExecution(t *testing.T) {
	lazy := Compile(`record R { name: string } effect fn get() -> R { R { name: "ok" } } effect fn main() -> string { get().name }`)
	if lazy.Checked || !hasCode(lazy, "EF106") {
		t.Fatalf("lazy record field access was admitted: %+v", lazy.Diagnostics)
	}
	ready := `record R { name: string } effect fn loadRecord() -> R { R { name: "ok" } } effect fn main() -> string { (run loadRecord()).name }`
	if output := runJS(t, ready, `if (await Effect.runPromise(__ef_function_main()) !== "ok") throw new Error("executed record field access failed");`); output != "" {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestClosedDataRequiredFailurePayloadCannotBeOmitted(t *testing.T) {
	for _, source := range []string{
		`error Bad { message: string } effect fn main() -> string raises {Bad} { fail Bad }`,
		`error Bad { message: string } effect fn main() -> string raises {Bad} { fail Bad() }`,
	} {
		r := Compile(source)
		if r.Checked || !hasCode(r, "EF114") {
			t.Fatalf("missing structured failure payload was admitted: %+v", r.Diagnostics)
		}
	}
	if r := Compile(`error Bad effect fn main() -> string raises {Bad} { fail Bad }`); !r.Checked {
		t.Fatalf("payload-free errors should remain valid: %+v", r.Diagnostics)
	}
}

func TestClosedDataGoFieldNamesAreInjective(t *testing.T) {
	source := `record R { id: string, Id: string } effect fn main() -> string { let r = R { id: "one", Id: "two" } r.id }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("case-distinct fields should check: %+v", r.Diagnostics)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module effra.generated\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "field-case")
	if output, err := runGoCommand(dir, "build", "-o", binary, "."); err != nil {
		t.Fatalf("generated Go fields collided: %v\n%s\n%s", err, output, goSource)
	}
	output, err := exec.Command(binary).CombinedOutput()
	if err != nil || string(output) != "one\n" {
		t.Fatalf("wrong Go field result: %v\n%s", err, output)
	}
}

func TestClosedDataRejectsDuplicatePatternBindings(t *testing.T) {
	duplicateAlias := Compile(`enum State { Ready { name: string, count: i64 } } effect fn main() -> i64 { match State.Ready("ok", 1) { State.Ready { name: value, count: value } => value } }`)
	if duplicateAlias.Checked || !hasCode(duplicateAlias, "EF121") {
		t.Fatalf("duplicate pattern aliases were admitted: %+v", duplicateAlias.Diagnostics)
	}
	duplicateField := Compile(`enum State { Ready { name: string } } effect fn main() -> string { match State.Ready("ok") { State.Ready { name: first, name: second } => second } }`)
	if duplicateField.Checked || !hasCode(duplicateField, "EF002") {
		t.Fatalf("duplicate pattern fields were admitted: %+v", duplicateField.Diagnostics)
	}
	shadowed := Compile(`enum State { Ready { name: string } } fn show(name: string, state: State) -> string { match state { State.Ready { name: name } => name } } effect fn main() -> string { show("outer", State.Ready("inner")) }`)
	if !shadowed.Checked {
		t.Fatalf("a single pattern binder may shadow an outer local: %+v", shadowed.Diagnostics)
	}
	discarded := Compile(`enum State { Ready { name: string, count: i64 } } fn show(state: State) -> string { match state { State.Ready { name: _, count: _ } => "ok" } } effect fn main() -> string { show(State.Ready("inner", 1)) }`)
	if !discarded.Checked {
		t.Fatalf("repeated discard bindings should remain valid: %+v", discarded.Diagnostics)
	}
}

func TestClosedDataRejectsMixedConstructorArguments(t *testing.T) {
	for _, source := range []string{
		`record R { name: string } effect fn main() -> string { let r = R(name: "ok", missing()) r.name }`,
		`record R { name: string } effect fn main() -> string { let r = R(missing(), name: "ok") r.name }`,
	} {
		r := Compile(source)
		if r.Checked || !hasCode(r, "EF122") || !hasCode(r, "EF102") {
			t.Fatalf("mixed constructor arguments were not fully checked: %+v", r.Diagnostics)
		}
	}
}

func TestClosedDataFailurePayloadPreservesProto(t *testing.T) {
	source := `error Bad { __proto__: string } effect fn main() -> string raises {Bad} { fail Bad { __proto__: "safe" } }`
	output := runJS(t, source, `const exit = await Effect.runPromiseExit(__ef_function_main()); const error = exit.cause.reasons[0].error; if (!Object.hasOwn(error, "__proto__") || error.__proto__ !== "safe") throw new Error("structured __proto__ payload was lost");`)
	if output != "" {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestClosedDataEmptyEnumHasStrictConsumerDeclaration(t *testing.T) {
	r := CompileFor(`enum Empty {} effect fn main() -> string { "ok" }`, "js")
	if !r.Checked {
		t.Fatalf("empty enums should have an honest uninhabited declaration: %+v", r.Diagnostics)
	}
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(declaration, "export type Empty = never;") {
		t.Fatalf("empty enum declaration is not uninhabited: %s", declaration)
	}
	checkStrictTypeScript(t, declaration, `import type { Empty } from "./generated.d.mts"; type Proof = Empty extends never ? true : false; const proof: Proof = true; void proof;`)
}

func TestClosedDataNestedTypeRefsAreCanonical(t *testing.T) {
	source := `record R { a: string } effect fn get() -> R { R { a: "ok" } } effect fn main() -> string { let child = fork get() let r = run child.join() r.a }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("nested TypeRef fixture should check: %+v", r.Diagnostics)
	}
	info, err := r.TypeAt(strings.Index(source, "fork"))
	if err != nil || len(info.Type.Type.Args) != 1 || info.Type.Type.Args[0].Kind != "record" || info.Type.Type.Args[0].Name != "R" {
		t.Fatalf("nested TypeRef lost nominal identity: %+v %v", info, err)
	}
}

func TestClosedDataControlConstructorShorthand(t *testing.T) {
	source := `enum State { Ready { value: string } } effect fn main() -> string { let value = "ok" match State.Ready { value } { State.Ready { value } => value } }`
	if r := Compile(source); !r.Checked {
		t.Fatalf("control constructor shorthand should parse and check: %+v", r.Diagnostics)
	}
}

func TestClosedDataMultiFieldControlConstructorShorthand(t *testing.T) {
	source := `enum State { Ready { first: string, second: string } } effect fn main() -> string { let first = "a" let second = "b" match State.Ready { first, second } { State.Ready { first, second } => first + second } }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("multi-field control constructor shorthand should parse and check: %+v", r.Diagnostics)
	}
	output := runJS(t, source, `if (await Effect.runPromise(__ef_function_main()) !== "ab") throw new Error("wrong shorthand result");`)
	if output != "" {
		t.Fatalf("unexpected JS output: %s", output)
	}
}

func TestClosedDataEmptyEnumEliminationBuildsOnGo(t *testing.T) {
	source := `enum Empty {}
fn absurd(value: Empty) -> string { match value {} }
effect fn effectAbsurd(value: Empty) -> string { match value {} }
effect fn main() -> string {
 // Both empty matches are retained, so the native build lowers them.
 let keepPure = absurd
 let keepEffect = effectAbsurd
 "ok"
}`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("empty enum elimination should check: %+v", r.Diagnostics)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(r.ModuleFile()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSource), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := runGoCommand(dir, "build", "."); err != nil {
		t.Fatalf("empty enum Go build: %v\n%s\n%s", err, output, goSource)
	}
	binary := filepath.Join(dir, "empty-enum")
	if output, err := runGoCommand(dir, "build", "-o", binary, "."); err != nil {
		t.Fatalf("empty enum native build: %v\n%s", err, output)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil || string(output) != "ok\n" {
		t.Fatalf("empty enum native run: %v\n%s", err, output)
	}
}

func checkStrictTypeScript(t *testing.T, declaration, consumer string) {
	t.Helper()
	// The compiler runs in a scratch directory outside the repository, so the
	// local TypeScript candidate must not be relative to the package.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := jsModuleDir(t, "strict-ts-")
	declarationPath := filepath.Join(dir, "generated.d.mts")
	consumerPath := filepath.Join(dir, "consumer.mts")
	if err := os.WriteFile(declarationPath, []byte(declaration), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(consumerPath, []byte(consumer), 0644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--noEmit", "--strict", "--module", "NodeNext", "--moduleResolution", "NodeNext", "--target", "ES2022", "--lib", "ES2022,DOM,ESNext.Disposable", consumerPath}
	var command *exec.Cmd
	if tsc, err := exec.LookPath("tsc"); err == nil {
		command = exec.Command(tsc, args...)
	} else {
		candidates := []string{
			filepath.Join(root, "node_modules", "typescript", "lib", "tsc.js"),
			"/Users/cvr/Developer/personal/gent/node_modules/typescript/lib/tsc.js",
		}
		for _, candidate := range candidates {
			if _, statErr := os.Stat(candidate); statErr == nil {
				node, nodeErr := exec.LookPath("node")
				if nodeErr != nil {
					t.Skip("TypeScript compiler is available only as a Node script, but Node is unavailable")
				}
				command = exec.Command(node, append([]string{candidate}, args...)...)
				break
			}
		}
	}
	if command == nil {
		t.Skip("TypeScript compiler unavailable")
	}
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("strict TypeScript consumer rejected generated declarations: %v\n%s\n%s", err, output, declaration)
	}
}

func TestDelimitedConstructorsInControlHeaders(t *testing.T) {
	// Parentheses and call arguments delimit their contents, so a payload-free
	// constructor needs no following body brace to be recognized there.
	r := Compile(`enum Light { Red; Green }
fn same(a: Light, b: Light) -> bool { true }
fn f(light: Light) -> string { if same(Light.Red {}, light) { "a" } else { "b" } }
fn g() -> string { match (Light.Red {}) { Light.Red => "r"; Light.Green => "g" } }`)
	if !r.Checked {
		t.Fatal("delimited constructor in a control header refused", r.Diagnostics)
	}
}

func TestConstructorSubjectsBeforeSubjectComma(t *testing.T) {
	// A subject comma follows only a constructor payload, never a control
	// body, so empty and shorthand payloads need no parentheses there.
	source := `enum Bit { Zero; One }
enum Item { Value { x: i64 }; Pair { x: i64, y: i64 } }
fn pick() -> string {
 match Bit.Zero {}, Bit.One {} {
  Bit.Zero, Bit.Zero | Bit.One => "zero"
  Bit.One, Bit.Zero | Bit.One => "one"
 }
}
fn single(x: i64) -> string {
 match Item.Value { x }, Bit.One {} {
  Item.Value { x: n } | Item.Pair { x: n }, Bit.Zero | Bit.One => if n == x { "x" } else { "other" }
 }
}
fn pair(x: i64, y: i64) -> string {
 match Bit.One {}, Item.Pair { x, y }, Bit.Zero {} {
  Bit.Zero | Bit.One, Item.Value { x: n } | Item.Pair { x: n }, Bit.Zero | Bit.One => if n == x { "x" } else { "other" }
 }
}
fn guarded(flag: bool, x: Bit, y: Bit) -> string {
 match if flag { x } else { y }, x {
  Bit.Zero | Bit.One, Bit.Zero | Bit.One => if flag { "flag" } else { "plain" }
 }
}
effect fn main() -> string { pick() + ";" + single(1) + ";" + pair(2, 3) + ";" + guarded(true, Bit.Zero {}, Bit.One {}) }`
	runGenericDataNative(t, source, "zero;x;x;flag\n")
	if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "zero;x;x;flag\n" {
		t.Fatalf("JS constructor subjects: %q", output)
	}
	first, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FormatSource(first.Text)
	if err != nil || second.Changed || second.Text != first.Text {
		t.Fatalf("formatting is not idempotent: %v\n%s\n---\n%s", err, first.Text, second.Text)
	}
	if r := Compile(first.Text); !r.Checked {
		t.Fatalf("formatted constructor subjects no longer check: %+v\n%s", r.Diagnostics, first.Text)
	}
}

func TestConstructorPayloadFormsBeforeSubjectComma(t *testing.T) {
	// Every payload spelling fieldValues admits keeps its constructor reading
	// before a subject comma, bare or parenthesized.
	const declarations = `enum Bit { Zero; One }
enum Item { Value { x: i64 } }
enum Pair { Both { x: i64, y: i64 } }
enum Wrap { Item { inner: i64, other: Pair } }
`
	forms := []struct{ subject, arm string }{
		{"Bit.Zero {}", "Bit.Zero | Bit.One"},
		{"Item.Value { x }", "Item.Value { x: a }"},
		{"Item.Value { x, }", "Item.Value { x: a }"},
		{"Item.Value { x; }", "Item.Value { x: a }"},
		{"Pair.Both { x, y }", "Pair.Both { y: a }"},
		{"Pair.Both { x; y }", "Pair.Both { y: a }"},
		{"Pair.Both { x, y, }", "Pair.Both { y: a }"},
		{"Pair.Both { x; y; }", "Pair.Both { y: a }"},
		{"Pair.Both { x, y: y }", "Pair.Both { y: a }"},
		{"Pair.Both { x y }", "Pair.Both { y: a }"},
		{"Pair.Both { x y: y }", "Pair.Both { y: a }"},
		{"Pair.Both { x: x, y }", "Pair.Both { y: a }"},
		{"Wrap.Item { inner: x, other: Pair.Both { x, y, } }", "Wrap.Item { inner: a }"},
	}
	var source, calls strings.Builder
	source.WriteString(declarations)
	for index, form := range forms {
		for parenthesized, subject := range []string{form.subject, "(" + form.subject + ")"} {
			name := fmt.Sprintf("f%d_%d", index, parenthesized)
			result := `"ok"`
			if strings.Contains(form.arm, "a }") {
				result = `if a == 0 { "bad" } else { "ok" }`
			}
			function := fmt.Sprintf("fn %s(x: i64, y: i64) -> string {\n match %s, Bit.One {} {\n  %s, Bit.Zero | Bit.One => %s\n }\n}\n", name, subject, form.arm, result)
			for _, target := range []string{"go", "js"} {
				if r := CompileFor(declarations+function, target); !r.Checked {
					t.Errorf("%s: %s refused: %+v", target, subject, r.Diagnostics)
				}
			}
			source.WriteString(function)
			if calls.Len() > 0 {
				calls.WriteString(` + ";" + `)
			}
			fmt.Fprintf(&calls, "%s(1, 2)", name)
		}
	}
	// Body protection: an if body and a nested match body before a subject
	// comma, and a shorthand look-alike body after an if header, stay bodies.
	source.WriteString(`fn bodies(flag: bool, x: Bit, y: Bit) -> string {
 match if flag { x } else { y }, match x { Bit.Zero => Bit.One {}; Bit.One => Bit.Zero {} } {
  Bit.Zero | Bit.One, Bit.Zero => if flag { "flag" } else { "plain" }
  Bit.Zero | Bit.One, Bit.One => "one"
 }
}
`)
	fmt.Fprintf(&source, "effect fn main() -> string { %s + \";\" + bodies(true, Bit.One {}, Bit.Zero {}) }", calls.String())
	if t.Failed() {
		return
	}
	want := strings.Repeat("ok;", 2*len(forms)) + "flag\n"
	runGenericDataNative(t, source.String(), want)
	if output := runJS(t, source.String(), `console.log(await Effect.runPromise(__ef_function_main()));`); output != want {
		t.Fatalf("JS constructor payload forms: %q", output)
	}
	first, err := FormatSource(source.String())
	if err != nil {
		t.Fatal(err)
	}
	second, err := FormatSource(first.Text)
	if err != nil || second.Changed || second.Text != first.Text {
		t.Fatalf("formatting is not idempotent: %v\n%s\n---\n%s", err, first.Text, second.Text)
	}
	if r := Compile(first.Text); !r.Checked {
		t.Fatalf("formatted payload forms no longer check: %+v\n%s", r.Diagnostics, first.Text)
	}
}
