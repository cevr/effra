package compiler

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Two unrelated callers recover through ordinary named handlers: a stored
// job recipe becomes an Outcome, and a codec decode failure becomes a domain
// failure through an effectful handler with its own service.
func TestRecoveryCallersRunOnGoAndJS(t *testing.T) {
	for _, fixture := range []struct{ example, output string }{
		{"recovery-outcome.ef", "constructed\nsettling bad\nattempt bad\nsettling bad\nattempt bad\nsettling ok\nattempt ok\nsettling busy\nattempt busy\nfailed bad refused; failed bad refused; done ok; busy busy\n"},
		{"recovery-codec.ef", "audit malformed forty\nage 42; not an age: forty\n"},
	} {
		t.Run(fixture.example, func(t *testing.T) {
			source := readRecipeExample(t, fixture.example)
			runGenericDataNative(t, source, fixture.output)
			if output := runJSForTarget(t, "js", source, `await Effect.runPromise(__ef_function_main());`); output != fixture.output {
				t.Fatalf("JS execution:\n%s", output)
			}
		})
	}
}

// Recovering a stored recipe constructs a new recipe. Neither the work nor
// the handler runs until that recipe runs, and each run executes both again.
func TestRecoveryOfStoredRecipeRunsOncePerExecution(t *testing.T) {
	source := readRecipeExample(t, "recovery-outcome.ef")
	output := runJSForTarget(t, "js", source, `
let calls = 0;
const console = { log: message => Effect.sync(() => { calls++; }) };
const job = { label: "bad", work: __ef_function_attempt("bad") };
const settled = __ef_recover(job.work, "Rejected", payload => Effect.sync(() => __ef_function_rejected(payload)));
if (calls !== 0) throw new Error("recovery executed its recipe");
const first = await Effect.runPromise(Effect.provideService(settled, __ef_service_Console, console));
const second = await Effect.runPromise(Effect.provideService(settled, __ef_service_Console, console));
if (calls !== 2 || first.reason !== "bad refused" || second.reason !== "bad refused") throw new Error("runs shared or skipped work: " + calls);
globalThis.console.log("counted");
`)
	if output != "counted\n" {
		t.Fatal(output)
	}
}

const recoveryDeclarations = `error Rejected {
    reason: string
}
error Busy
error Lost
service Audit {
    effect fn note(message: string) -> void
}
effect fn work() -> string raises { Rejected, Busy } {
    fail Busy
}
effect fn quiet() -> string {
    "q"
}
fn reason(failure: Rejected) -> string {
    failure.reason
}
effect fn escalate(failure: Rejected) -> string raises { Lost } uses { Audit } {
    run Audit.note(failure.reason)
    fail Lost
}
effect fn again(failure: Rejected) -> string raises { Rejected } {
    fail Rejected {
        reason: failure.reason + " again"
    }
}
`

func TestRecoveryRowAlgebraIsExact(t *testing.T) {
	for _, test := range []struct {
		name, expression   string
		failures, services []string
	}{
		{"pure handler removes only the handled label", `work().recover<Rejected>(reason)`, []string{"Busy"}, nil},
		{"effectful handler adds its rows", `work().recover<Rejected>(escalate)`, []string{"Busy", "Lost"}, []string{"Audit"}},
		{"handler may raise the handled label again", `work().recover<Rejected>(again)`, []string{"Busy", "Rejected"}, nil},
		{"sequential recovery composes", `work().recover<Rejected>(escalate).catch<Busy>("busy")`, []string{"Lost"}, []string{"Audit"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := recoveryDeclarations + "effect fn main() -> void {\n    let recipe = " + test.expression + "\n    void\n}\n"
			r := Compile(source)
			if !r.Checked {
				t.Fatal(r.Diagnostics)
			}
			info, err := r.TypeAt(strings.LastIndex(source, ").") + 2)
			if err != nil {
				t.Fatal(err)
			}
			if info.Type.Contract.Kind != "recipe" || !slices.Equal(info.Type.Errors, test.failures) || !slices.Equal(info.Type.Services, test.services) {
				t.Fatalf("recovered rows: %+v", info.Type)
			}
		})
	}
}

func TestRecoveryRefusals(t *testing.T) {
	for _, test := range []struct{ name, body, code, message string }{
		{"wrong payload type", `fn other(failure: Busy) -> string {
    "b"
}
effect fn main() -> string raises { Busy } {
    run work().recover<Rejected>(other)
}`, "EF106", "recovery handler must accept exactly the Rejected payload"},
		{"wrong result type", `fn count(failure: Rejected) -> i64 {
    1
}
effect fn main() -> string raises { Busy } {
    run work().recover<Rejected>(count)
}`, "EF106", "recovery handler must return string"},
		{"extra handler parameter", `fn two(failure: Rejected, label: string) -> string {
    label
}
effect fn main() -> string raises { Busy } {
    run work().recover<Rejected>(two)
}`, "EF106", "recovery handler must accept exactly the Rejected payload"},
		{"handler is not a function", `effect fn main() -> string raises { Busy } {
    run work().recover<Rejected>("fallback")
}`, "EF106", "recovery handler must be a function value accepting Rejected"},
		{"unknown failure", `effect fn main() -> string raises { Rejected, Busy } {
    run work().recover<Missing>(reason)
}`, "EF102", "unknown failure Missing"},
		{"failure not admitted by the recipe", `effect fn main() -> string {
    run quiet().recover<Rejected>(reason)
}`, "EF107", "effect does not admit failure Rejected"},
		{"builtin failures have no declared payload", `fn late(failure: Rejected) -> string {
    "late"
}
effect fn main() -> string raises { Rejected, Busy } uses { Scheduler } {
    run work().timeout(1).recover<Timeout>(late)
}`, "EF106", "recover binds a declared error payload; handle builtin failure Timeout with catch"},
		{"unhandled failure stays declared", `effect fn main() -> string {
    run work().recover<Rejected>(reason)
}`, "EF107", "undeclared failures: Busy"},
		{"handler failure must be declared", `effect fn main() -> string raises { Busy } uses { Audit } {
    run work().recover<Rejected>(escalate)
}`, "EF107", "undeclared failures: Lost"},
		{"handler service must be declared", `effect fn main() -> string raises { Busy, Lost } {
    run work().recover<Rejected>(escalate)
}`, "EF108", "missing service requirements: Audit"},
		{"unknown payload field", `fn nope(failure: Rejected) -> string {
    failure.missing
}
effect fn main() -> void {
    void
}`, "EF114", "unknown field missing on Rejected"},
		{"a payload is not a returned success value", `fn wrap(failure: Rejected) -> Rejected {
    failure
}
effect fn main() -> void {
    void
}`, "EF102", "unknown or unsupported value type Rejected"},
		{"recover requires a recipe", `effect fn main() -> void {
    let value = reason.recover<Rejected>(reason)
    void
}`, "EF105", "recover requires an Effect value"},
		{"abstract rows cannot be subtracted", `effect fn guard<E: raises>(task: effect fn(string) -> string raises { E, Rejected }) -> string raises { E } {
    run task("x").recover<Rejected>(reason)
}
effect fn main() -> void {
    void
}`, "EF125", "recovery of an abstract row requires an unsupported row difference constraint"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(recoveryDeclarations + test.body)
			if r.Checked {
				t.Fatal("accepted invalid recovery")
			}
			found := false
			for _, diagnostic := range r.Diagnostics {
				found = found || diagnostic.Code == test.code && strings.Contains(diagnostic.Message, test.message)
			}
			if !found {
				t.Fatalf("want %s %q: %+v", test.code, test.message, r.Diagnostics)
			}
		})
	}
}

// The recovered recipe publishes either its original result or the
// handler's invocation result. A borrowed input stays borrowed only when the
// handler cannot acquire; an acquiring handler cannot escape a closing scope.
func TestRecoveryJoinsBorrowedAndAcquiredResults(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, test := range []struct {
			name, handler string
			valid         bool
		}{
			{"failing handler keeps the borrowed proof", "refuse", true},
			{"acquiring handler result is owned by the scope", "reopen", false},
		} {
			t.Run(fmt.Sprintf("%s/reverse=%t", test.name, reverse), func(t *testing.T) {
				declarations := []string{
					`error Missing`,
					`effect fn choose(file: File) -> File raises { Missing } { if true { fail Missing } else { file } }`,
					`effect fn refuse(failure: Missing) -> File raises { IoError } { fail IoError }`,
					`effect fn reopen(failure: Missing) -> File raises { IoError } { run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles) }`,
					`effect fn outer(file: File) -> File raises { IoError } { scope { run choose(file).recover<Missing>(` + test.handler + `) } }`,
					`effect fn main() -> void { void }`,
				}
				if reverse {
					slices.Reverse(declarations)
				}
				r := Compile(strings.Join(declarations, "\n"))
				if r.Checked != test.valid || (!test.valid && !hasCode(r, "EF123")) {
					t.Fatalf("recovered ownership: %+v", r.Diagnostics)
				}
				if test.valid {
					symbol := r.Find("outer")
					if len(symbol.Actual.Ownership) != 1 || symbol.Actual.Ownership[0].Status != "borrowed" {
						t.Fatalf("borrowed recovery lost proof: %+v", symbol.Actual)
					}
				}
			})
		}
	}
}

// Cancellation that arrives while the handler runs interrupts the handler:
// the recovered recipe never publishes the handler's success.
func TestRecoveryHandlerObservesCancellationOnGoAndJS(t *testing.T) {
	source := `error Rejected {
    reason: string
}
effect fn work() -> string raises { Rejected } {
    fail Rejected {
        reason: "slow"
    }
}
effect fn wait(failure: Rejected) -> string uses { Clock, Console } {
    run Console.log("recovering " + failure.reason)
    run Clock.sleep(10000)
    "recovered late"
}
effect fn main() -> void {
    let result = run work().recover<Rejected>(wait).timeout(20).catch<Timeout>("timed out")
        .provide<Clock>(LiveClock).provide<Console>(Stdout).provide<Scheduler>(LiveScheduler)
    run Console.log(result).provide<Console>(Stdout)
}
`
	expected := "recovering slow\ntimed out\n"
	runGenericDataNative(t, source, expected)
	if output := runJSForTarget(t, "js", source, `await Effect.runPromise(__ef_function_main());`); output != expected {
		t.Fatalf("JS execution:\n%s", output)
	}
}

// The JS recovery helper keeps Effra's solitary-failure boundary rather than
// Effect.catchTag's first-failure selection: composite causes, defects and
// interruption never reach the handler.
func TestRecoveryJSKeepsCompositeCauses(t *testing.T) {
	output := runJS(t, readRecipeExample(t, "recovery-outcome.ef"), `
const failure = { _tag: "Rejected", reason: "payload" }, defect = { message: "cleanup defect" };
let seen = [];
const handler = payload => Effect.sync(() => { seen.push(payload); return "recovered"; });
const matched = await Effect.runPromiseExit(__ef_recover(Effect.fail(failure), "Rejected", handler));
if (!Exit.isSuccess(matched) || matched.value !== "recovered" || seen.length !== 1 || seen[0] !== failure) throw new Error("payload not delivered intact");
const composite = await Effect.runPromiseExit(__ef_recover(Effect.failCause(Cause.combine(Cause.fail(failure), Cause.die(defect))), "Rejected", handler));
if (!Exit.isFailure(composite) || composite.cause.reasons.length !== 2 || seen.length !== 1) throw new Error("composite cause recovered");
const other = await Effect.runPromiseExit(__ef_recover(Effect.fail({ _tag: "Busy" }), "Rejected", handler));
if (!Exit.isFailure(other) || other.cause.reasons[0].error._tag !== "Busy" || seen.length !== 1) throw new Error("unselected failure changed");
const died = await Effect.runPromiseExit(__ef_recover(Effect.die(defect), "Rejected", handler));
if (!Exit.isFailure(died) || died.cause.reasons[0].defect !== defect || seen.length !== 1) throw new Error("defect recovered");
const interrupted = await Effect.runPromiseExit(__ef_recover(Effect.interrupt, "Rejected", handler));
if (!Exit.isFailure(interrupted) || !Cause.hasInterruptsOnly(interrupted.cause) || seen.length !== 1) throw new Error("interruption recovered");
const raised = await Effect.runPromiseExit(__ef_recover(Effect.fail(failure), "Rejected", () => Effect.fail({ _tag: "Lost" })));
if (!Exit.isFailure(raised) || raised.cause.reasons[0].error._tag !== "Lost") throw new Error("handler failure lost");
console.log("recovery cause controls");
`)
	if output != "recovery cause controls\n" {
		t.Fatal(output)
	}
}

func TestRecoveryFormattingRoundTrip(t *testing.T) {
	source := "effect fn main() -> string raises { Busy } {\n    run work( ).recover < Rejected > ( reason )\n}\n"
	formatted, err := FormatSource(recoveryDeclarations + source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(formatted.Text, "run work().recover<Rejected>(reason)\n") {
		t.Fatalf("recovery formatting:\n%s", formatted.Text)
	}
	again, err := FormatSource(formatted.Text)
	if err != nil || again.Changed {
		t.Fatalf("recovery formatting is not idempotent: %v\n%s", err, again.Text)
	}
	if r := Compile(formatted.Text); !r.Checked {
		t.Fatal(r.Diagnostics)
	}
}

// A declared error name is also its payload type in TypeScript declarations.
func TestRecoveryHandlerDeclarationsTypeCheck(t *testing.T) {
	r := Compile(readRecipeExample(t, "recovery-outcome.ef"))
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	checkStrictTypeScript(t, declaration, `import type { Rejected, RejectedError, Outcome } from "./generated.mjs";
import { rejected } from "./generated.mjs";
declare const payload: RejectedError;
const outcome: Outcome = rejected(payload);
const same: Rejected = payload;
// @ts-expect-error a handler receives only the declared payload
rejected({ _tag: "Busy" });
void outcome; void same;
`)
}

// Handlers are ordinary callable values: named, stored in a record field or
// passed as a parameter, including a pure void handler.
func TestRecoveryHandlerPlacementsRunOnGoAndJS(t *testing.T) {
	source := `error Rejected {
    reason: string
}
record Policy {
    handler: fn(Rejected) -> void
}
effect fn work() -> void raises { Rejected } {
    fail Rejected {
        reason: "x"
    }
}
fn ignore(failure: Rejected) -> void {
    void
}
effect fn guard(handler: fn(Rejected) -> void) -> void {
    run work().recover<Rejected>(handler)
}
effect fn main() -> void {
    let policy = Policy {
        handler: ignore
    }
    run work().recover<Rejected>(policy.handler)
    run guard(ignore)
    run work().recover<Rejected>(ignore)
    run Console.log("void handled").provide<Console>(Stdout)
}
`
	runGenericDataNative(t, source, "void handled\n")
	if output := runJSForTarget(t, "js", source, `await Effect.runPromise(__ef_function_main());`); output != "void handled\n" {
		t.Fatalf("JS execution:\n%s", output)
	}
}

// A recovered recipe publishes either the original success or the handler's
// invocation result, so a stored recipe field of the recovered value joins
// both occurrences. An unresolved handler leaves the field evidence missing.
func TestRecoveryJoinsStoredFieldOccurrences(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, test := range []struct {
			name, handler, parameter string
			valid                    bool
		}{
			{"failing stored handler keeps the borrowed proof", "refuse", "", true},
			{"acquiring stored handler is owned by the scope", "reopen", "", false},
			{"unresolved handler leaves field evidence missing", "handler", ", handler: fn(Missing) -> Opener", false},
			{"never-succeeding handler keeps the original evidence", "fails", "", true},
		} {
			t.Run(fmt.Sprintf("%s/reverse=%t", test.name, reverse), func(t *testing.T) {
				declarations := []string{
					`error Missing`,
					`record Opener { open: Effect<File, { IoError }> }`,
					`effect fn borrowed(file: File) -> File { file }`,
					`effect fn absent() -> File raises { IoError } { fail IoError }`,
					`effect fn original(file: File) -> Opener raises { Missing } { if true { fail Missing } else { Opener { open: borrowed(file) } } }`,
					`fn refuse(failure: Missing) -> Opener { Opener { open: absent() } }`,
					`effect fn fails(failure: Missing) -> Opener raises { IoError } { fail IoError }`,
					`fn reopen(failure: Missing) -> Opener { Opener { open: Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles) } }`,
					`effect fn leak(file: File` + test.parameter + `) -> File raises { IoError } { scope { let opener = run original(file).recover<Missing>(` + test.handler + `)
    run opener.open } }`,
					`effect fn main() -> void { void }`,
				}
				if reverse {
					slices.Reverse(declarations)
				}
				r := Compile(strings.Join(declarations, "\n"))
				if r.Checked != test.valid || (!test.valid && !hasCode(r, "EF123")) {
					t.Fatalf("recovered field ownership: %+v", r.Diagnostics)
				}
			})
		}
	}
}

// A payload handle reaches the handler with failure-payload provenance: it is
// owned by whichever owner runs the recovered recipe, so a payload-derived
// File cannot escape a closing scope, while a caller that keeps it inside
// its own owner can still use it.
func TestRecoveryPayloadHandlesAreOwnedByTheRunner(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "..", "examples", "fixture.txt"))
	if err != nil {
		t.Fatal(err)
	}
	path := strconv.Quote(fixture)
	prefix := `error WithFile { file: File }
effect fn reject(file: File) -> File raises { WithFile } {
    fail WithFile { file: file }
}
`
	for _, reverse := range []bool{false, true} {
		for _, handler := range []string{
			`fn retrieve(failure: WithFile) -> File { failure.file }`,
			`effect fn retrieve(failure: WithFile) -> File { failure.file }`,
		} {
			t.Run(fmt.Sprintf("%s/reverse=%t", handler, reverse), func(t *testing.T) {
				declarations := []string{handler, `effect fn escape() -> File raises { IoError } {
    scope {
        let file = run Files.openRead(` + path + `).provide<Files>(LiveFiles)
        run reject(file).recover<WithFile>(retrieve)
    }
}`, `effect fn main() -> void { void }`}
				if reverse {
					slices.Reverse(declarations)
				}
				r := Compile(prefix + strings.Join(declarations, "\n"))
				if r.Checked || !hasCode(r, "EF123") {
					t.Fatalf("payload handle escaped its scope: %+v", r.Diagnostics)
				}
			})
		}
	}
	safe := prefix + `fn retrieve(failure: WithFile) -> File { failure.file }
effect fn read(file: File) -> string raises { IoError } {
    let recovered = run reject(file).recover<WithFile>(retrieve)
    run Files.readText(recovered).provide<Files>(LiveFiles)
}
effect fn main() -> void raises { IoError } {
    scope {
        let file = run Files.openRead(` + path + `).provide<Files>(LiveFiles)
        let text = run read(file)
        run Console.log(text).provide<Console>(Stdout)
    }
}
`
	if r := Compile(safe); !r.Checked {
		t.Fatalf("caller-owned payload handle refused: %+v", r.Diagnostics)
	}
	runGenericDataNative(t, safe, "scoped file read complete\n\n")
}

// The raw error name is exported as the payload type alias, so a reserved
// TypeScript word is refused for JS while a near-reserved spelling declares.
func TestRecoveryPayloadAliasNamesAreValidTypeScript(t *testing.T) {
	for _, name := range []string{"class", "type", "from", "await"} {
		source := "error " + name + " { reason: string }\neffect fn main() -> void { void }\n"
		if r := CompileFor(source, "go"); !r.Checked {
			t.Fatalf("Go target refused error %s: %+v", name, r.Diagnostics)
		}
		r := CompileFor(source, "js")
		if r.Checked || !hasDiagnosticMessage(r, "JavaScript declaration name "+name+" is reserved by TypeScript for error payload type") {
			t.Fatalf("reserved error alias %s: %+v", name, r.Diagnostics)
		}
	}
	source := `error Class { reason: string }
error String { reason: string }
fn explain(failure: Class) -> string { failure.reason }
fn describe(failure: String) -> string { failure.reason }
effect fn main() -> void { void }
`
	r := CompileFor(source, "js")
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err, r.Diagnostics)
	}
	checkStrictTypeScript(t, declaration, `import type { Class, String } from "./generated.mjs";
import { explain, describe } from "./generated.mjs";
declare const klass: Class;
declare const text: String;
const reasons: string = explain(klass) + describe(text);
// @ts-expect-error payload aliases stay nominal by tag
explain(text);
void reasons;
`)
}

// A handler that never succeeds contributes rows but no successful value, so
// a caller-owned File in the original's stored recipe still crosses the
// recovery and is read after the scope.
func TestRecoveryNeverSucceedingHandlerKeepsCallerOwnedResult(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "..", "examples", "fixture.txt"))
	if err != nil {
		t.Fatal(err)
	}
	source := `error Missing
record Opener { open: Effect<File, { IoError }> }
effect fn borrowed(file: File) -> File { file }
effect fn original(file: File) -> Opener raises { Missing } {
    if false { fail Missing } else { Opener { open: borrowed(file) } }
}
effect fn fails(failure: Missing) -> Opener raises { IoError } { fail IoError }
effect fn use(file: File) -> File raises { IoError } {
    scope {
        let box = run original(file).recover<Missing>(fails)
        run box.open
    }
}
effect fn main() -> void raises { IoError } {
    let file = run Files.openRead(` + strconv.Quote(fixture) + `).provide<Files>(LiveFiles)
    let returned = run use(file)
    run Console.log(run Files.readText(returned).provide<Files>(LiveFiles)).provide<Console>(Stdout)
}
`
	runGenericDataNative(t, source, "scoped file read complete\n\n")
}

// A handler may return a recipe that captures the payload File. That
// recipe holds the File, owned by whichever owner runs the recovery: it
// cannot leave that owner's closing scope, while running it there and
// returning its string is safe. The recipe's eventual result is separate.
func TestRecoveryProducedRecipeHoldsPayloadCaptures(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "..", "examples", "fixture.txt"))
	if err != nil {
		t.Fatal(err)
	}
	prefix := `error WithFile { file: File }
record Reader { task: Effect<string, { IoError }> }
effect fn readFile(file: File) -> string raises { IoError } {
    run Files.readText(file).provide<Files>(LiveFiles)
}
effect fn rejected(file: File) -> Effect<string, { IoError }> raises { WithFile } {
    fail WithFile { file: file }
}
service Work {
    effect fn job() -> Effect<string, { IoError }> raises { WithFile, IoError }
}
impl LiveWork for Work {
    effect fn job() -> Effect<string, { IoError }> raises { WithFile, IoError } {
        let file = run Files.openRead(` + strconv.Quote(fixture) + `).provide<Files>(LiveFiles)
        run rejected(file)
    }
}
`
	type handlerCase struct{ name, declaration, parameter, argument string }
	for _, handler := range []handlerCase{
		{"pure", `fn retrieve(failure: WithFile) -> Effect<string, { IoError }> { readFile(failure.file) }`, "", ""},
		{"effect", `effect fn retrieve(failure: WithFile) -> Effect<string, { IoError }> { readFile(failure.file) }`, "", ""},
		{"parameter", `fn retrieve(failure: WithFile) -> Effect<string, { IoError }> { readFile(failure.file) }`, "handler: fn(WithFile) -> Effect<string, { IoError }>", "retrieve"},
	} {
		local := "retrieve"
		if handler.parameter != "" {
			local = "handler"
		}
		recovered := "run Work.job().provide<Work>(LiveWork).recover<WithFile>(" + local + ")"
		escape := `effect fn escape(` + handler.parameter + `) -> Reader raises { IoError } {
    scope {
        let task = ` + recovered + `
        Reader { task: task }
    }
}`
		consume := `effect fn consume(` + handler.parameter + `) -> string raises { IoError } {
    scope {
        let task = ` + recovered + `
        run task
    }
}`
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%t", handler.name, reverse), func(t *testing.T) {
				compile := func(body string) *Result {
					declarations := []string{handler.declaration, body, `effect fn main() -> void { void }`}
					if reverse {
						slices.Reverse(declarations)
					}
					return Compile(prefix + strings.Join(declarations, "\n"))
				}
				if r := compile(escape); r.Checked || !hasCode(r, "EF123") {
					t.Fatalf("payload-capturing recipe escaped its scope: %+v", r.Diagnostics)
				}
				if r := compile(consume); !r.Checked {
					t.Fatalf("in-scope consumer refused: %+v", r.Diagnostics)
				}
			})
		}
		t.Run(handler.name+"/runs", func(t *testing.T) {
			main := `effect fn main() -> void raises { IoError } {
    run Console.log(run consume(` + handler.argument + `)).provide<Console>(Stdout)
}
`
			runGenericDataNative(t, prefix+handler.declaration+"\n"+consume+"\n"+main, "scoped file read complete\n\n")
		})
	}
}
