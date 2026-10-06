package compiler

import (
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
effect fn interpret(state: RunState) -> string throws {Invalid} {
 match state {
  RunState.Idle => "ok"
  RunState.Running { runId } => fail Invalid { message: runId }
  RunState.Done { code } => "done"
 }
}
effect fn failWithPayload() -> string throws {Invalid} { fail Invalid { message: "bad" } }
effect fn main() -> string throws {Invalid} {
 let user = User { id: "u1", name: "Ada" }
 let state = RunState.Running { runId: "42" }
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
effect fn main() -> string { match A.BC() { A.BC => "ok" } }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("collision fixture should check: %+v", r.Diagnostics)
	}
	goSource, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(goSource, "type efTypeV_2_AB_1_C struct") || !strings.Contains(goSource, "type efTypeV_1_A_2_BC struct") {
		t.Fatalf("qualified enum types missing: %s", goSource)
	}
	dir := t.TempDir()
	if err := WriteRuntime(dir); err != nil {
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
	goSource, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := WriteRuntime(dir); err != nil {
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
func TestClosedDataPayload(t *testing.T) { er.Run(func(fc *er.FiberContext) er.Exit[struct{}] { failed := efFunction_failWithPayload()(efContext{Runtime: fc}); interpreted := efFunction_interpret(efTypeV_8_RunState_7_Running{RunId:"branch"})(efContext{Runtime: fc}); for _, exit := range []er.Exit[string]{failed, interpreted} { if exit.Failure == nil { t.Fatal("failure missing") }; payload, ok := exit.Failure.Payload.(efType_Invalid); if !ok || (payload.Message != "bad" && payload.Message != "branch") { t.Fatalf("payload lost: %#v", exit.Failure.Payload) } }; return er.Succeed(struct{}{}) }) }
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
