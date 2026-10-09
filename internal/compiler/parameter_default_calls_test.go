package compiler

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// defaultCallsSource declares callees whose omitted parameters have constant
// defaults: an ordinary function, a compiler-distributed import, a service
// operation, a function whose only parameter is defaulted and a
// row-polymorphic effect function whose failure row is substituted. The
// callers are substituted so that one program can omit what the other spells.
func defaultCallsSource(suffix string, callers [8]string) string {
	return fmt.Sprintf(`import Fns "effra/functions"
fn render(value: string, suffix: string = %q, count: i64 = 7, loud: bool = false) -> string {
    if loud { value + suffix } else { value }
}
service Greeter {
    effect fn greet(name: string, punctuation: string = "!") -> string
}
impl PlainGreeter for Greeter {
    effect fn greet(name: string, punctuation: string) -> string { "Hi " + name + punctuation }
}
fn count(value: i64 = 5) -> i64 { value }
effect fn shout(text: string) -> string { text + "!" }
effect fn apply<E: raises>(callback: effect fn(string) -> string raises { E }, input: string = "seed") -> string raises { E } {
    run callback(input)
}
fn a() -> string { %s }
fn b() -> string { %s }
fn c() -> string { %s }
fn d() -> string { %s }
fn e() -> string { %s }
effect fn f() -> string uses { Greeter } { %s }
fn g() -> i64 { %s }
effect fn h() -> string { %s }
effect fn main() -> string {
    let greeting = run f().provide<Greeter>(PlainGreeter)
    let numbered = if g() == 5 { "5" } else { "?" }
    a() + "|" + b() + "|" + c() + "|" + d() + "|" + e() + "|" + greeting + "|" + numbered + "|" + run h()
}
`, suffix, callers[0], callers[1], callers[2], callers[3], callers[4], callers[5], callers[6], callers[7])
}

var omittedDefaultCallers = [8]string{
	`render("a")`,
	`render("b", loud: true)`,
	`render(loud: true, value: "c")`,
	`"d" |> render(count: 9, loud: true)`,
	`Fns.suffixed("e")`,
	`run Greeter.greet("ada")`,
	`count()`,
	`run apply(shout)`,
}

// explicitDefaultCallers write each omitted constant in its own parameter
// slot: positionally, or by label in parameter order before the first
// authored label that binds a later parameter.
var explicitDefaultCallers = [8]string{
	`render("a", "!", 7, false)`,
	`render("b", "!", 7, true)`,
	`render(suffix: "!", count: 7, loud: true, value: "c")`,
	`"d" |> render("!", 9, true)`,
	`Fns.suffixed("e", "!")`,
	`run Greeter.greet("ada", "!")`,
	`count(5)`,
	`run apply(shout, "seed")`,
}

const defaultCallsOutput = "a|b!|c!|d!|e!|Hi ada!|5|seed!\n"

func TestOmittedArgumentsCompileToTheExplicitConstantCall(t *testing.T) {
	omitted := defaultCallsSource("!", omittedDefaultCallers)
	explicit := defaultCallsSource("!", explicitDefaultCallers)
	omittedOutput := compilePipeOutput(t, omitted, ".", nil)
	for key, artifact := range omittedOutput {
		if artifact.Status == pipeRejected {
			t.Fatalf("%s rejected the omitted-argument program: %v", key, artifact.Codes)
		}
	}
	for _, key := range []string{"js/entry", "js/library", "js/library.d.mts", "go/build"} {
		if omittedOutput[key].Status != pipeEmitted {
			t.Fatalf("%s was not emitted: %+v", key, omittedOutput[key])
		}
	}
	explicitOutput := compilePipeOutput(t, explicit, ".", nil)
	if !reflect.DeepEqual(omittedOutput, explicitOutput) {
		for key := range omittedOutput {
			if !reflect.DeepEqual(omittedOutput[key], explicitOutput[key]) {
				t.Errorf("%s differs between omitted and explicit constant arguments:\n--- omitted\n%s\n--- explicit\n%s", key, omittedOutput[key].Text, explicitOutput[key].Text)
			}
		}
		t.FailNow()
	}
	runOnEveryHost(t, omitted, defaultCallsOutput)

	// The imported callee keeps its declaration identity and owns the
	// default: the omitting call is an application of that identity to the
	// full parameter vector.
	checked := CompileFor(omitted, "go")
	found := false
	for _, expression := range expressionsOf(checked.Program) {
		f := expression.ResolvedFunction
		if expression.Kind != "call" || f == nil || f.Module != "effra/functions" {
			continue
		}
		application := expression.checked.application
		found = application != nil && application.Callee == f.Identity && len(application.Arguments) == 2 && len(expression.Args) == 1 && len(expression.parameterArguments()) == 2 && f.Params[1].DefaultValue != nil && f.Params[1].DefaultValue.Value == "!"
	}
	if !found {
		t.Fatal("the omitting call to the imported callee is not an application of its declaration identity to both parameters")
	}

	// Controls: the comparison sees the constant's value and its slot. A
	// different explicit constant, or the constants passed after the authored
	// labels through a reordering adapter, is a different call.
	changed := explicitDefaultCallers
	changed[0] = `render("a", "?", 7, false)`
	if reflect.DeepEqual(omittedOutput, compilePipeOutput(t, defaultCallsSource("!", changed), ".", nil)) {
		t.Fatal("equivalence did not observe a different explicit constant")
	}
	trailing := explicitDefaultCallers
	trailing[1] = `render("b", loud: true, suffix: "!", count: 7)`
	trailingOutput := compilePipeOutput(t, defaultCallsSource("!", trailing), ".", nil)
	if reflect.DeepEqual(omittedOutput, trailingOutput) || !strings.Contains(trailingOutput["js/library"].Text, "__ef_argument_") {
		t.Fatal("equivalence did not distinguish constants in their slots from a trailing-label adapter")
	}
}

func TestOmittedArgumentsUseTheCalleesCurrentDefault(t *testing.T) {
	// The default belongs to the callee's declaration. Changing it changes
	// every omitting caller's emitted call and result, and nothing else.
	before := compilePipeOutput(t, defaultCallsSource("!", omittedDefaultCallers), ".", nil)
	after := compilePipeOutput(t, defaultCallsSource("?", omittedDefaultCallers), ".", nil)
	if !strings.Contains(before["js/library"].Text, `__ef_function_render("b", "!", 7n, true)`) || !strings.Contains(after["js/library"].Text, `__ef_function_render("b", "?", 7n, true)`) {
		t.Fatalf("omitted call did not lower the callee's current default:\n%s", after["js/library"].Text)
	}
	if strings.Contains(after["js/library"].Text, `"b", "!"`) || strings.Contains(after["go/build"].Text, `"!", int64(7)`) {
		t.Fatal("a stale default survived the callee's contract change")
	}
	runOnEveryHost(t, defaultCallsSource("?", omittedDefaultCallers), "a|b?|c?|d?|e!|Hi ada!|5|seed!\n")
	explicitBefore := compilePipeOutput(t, defaultCallsSource("!", explicitDefaultCallers), ".", nil)
	explicitAfter := compilePipeOutput(t, defaultCallsSource("?", explicitDefaultCallers), ".", nil)
	// A default is call-site sugar: the callee's emitted body and ABI do not
	// mention it, so programs that spell every argument compile identically.
	if !reflect.DeepEqual(explicitBefore, explicitAfter) || reflect.DeepEqual(before, after) {
		t.Fatal("a default change reached explicit callers or missed omitting callers")
	}
}

func TestOmittedArgumentsKeepAuthoredEvaluationOrder(t *testing.T) {
	// Each authored argument runs once, in source order, whatever parameter
	// its label binds; the omitted constant evaluates nothing.
	source := `error First
error Second
effect fn first() -> string raises { First } { fail First }
effect fn second() -> string raises { Second } { fail Second }
fn join(a: string, sep: string = "-", b: string) -> string { a + sep + b }
effect fn order() -> string raises { First, Second } {
    join(b: run second(), a: run first())
}
effect fn ok() -> string { join(b: "y", a: "x") }
effect fn main() -> string {
    let failed = run order().catch<Second>("second ran first").catch<First>("first ran first")
    failed + "|" + run ok()
}
`
	runOnEveryHost(t, source, "second ran first|x-y\n")
	r := CompileFor(source, "js")
	js, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(js, "__ef_function_second()") != 1 || strings.Count(js, "__ef_function_first()") != 1 {
		t.Fatalf("an authored argument was not emitted exactly once:\n%s", js)
	}
	if !strings.Contains(js, `((__ef_argument_0, __ef_argument_1, __ef_argument_2) => __ef_function_join(__ef_argument_2, __ef_argument_0, __ef_argument_1))("-", "y", "x")`) {
		t.Fatalf("the reordered call did not keep authored order with the constant in its slot:\n%s", js)
	}
}

func TestNegativeAndMinimumDefaultsAreDirectConstants(t *testing.T) {
	source := `fn shift(value: i64, by: i64 = -3) -> i64 { value + by }
fn floor(value: i64 = -9223372036854775808) -> i64 { value }
effect fn main() -> string {
    if shift(10) == 7 { if floor() == shift(-9223372036854775805) { "ok" } else { "bad floor" } } else { "bad shift" }
}
`
	runOnEveryHost(t, source, "ok\n")
	js, _, err := CompileFor(source, "js").Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "__ef_function_shift(10n, -3n)") || !strings.Contains(js, "__ef_function_floor(-9223372036854775808n)") {
		t.Fatalf("negative defaults were not emitted as direct signed literals:\n%s", js)
	}
	native, _, err := emitGoApplication(CompileFor(source, "go"), GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(native, "int64(-3)") || !strings.Contains(native, "int64(-9223372036854775808)") {
		t.Fatalf("negative defaults were not emitted as direct Go constants:\n%s", native)
	}
}

func TestOmittedArgumentRefusals(t *testing.T) {
	declarations := `fn render(value: string, suffix: string = "!") -> string { value + suffix }
fn marked(required key: string, suffix: string = "!") -> string { key + suffix }
fn gap(a: i64 = 1, b: i64) -> i64 { b }
fn applyOne(callback: fn(string) -> string) -> string { callback("x") }
`
	for _, refusal := range []struct{ name, body, message string }{
		{"required value omitted", `fn caller() -> string { render() }`, "missing argument for parameter value"},
		{"required choice omitted", `fn caller() -> string { marked(suffix: "?") }`, "missing argument for parameter key"},
		{"positional cannot skip to a later parameter", `fn caller() -> i64 { gap(5) }`, "missing argument for parameter b"},
		{"too many arguments", `fn caller() -> string { render("a", "b", "c") }`, "incorrect argument count"},
		{"unknown label", `fn caller() -> string { render("a", sufix: "?") }`, "unknown argument label sufix"},
		{"duplicate label", `fn caller() -> string { render(value: "a", value: "b") }`, "duplicate argument label value"},
		{"callable value keeps full arity", `fn caller() -> string { let shorter = render
    shorter("a") }`, "incorrect callback argument count"},
		{"function value is not eta-expanded", `fn caller() -> string { applyOne(render) }`, "argument must be"},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			for _, target := range []string{"go", "js"} {
				r := CompileFor(declarations+refusal.body, target)
				found := false
				for _, d := range r.Diagnostics {
					found = found || d.Code == "EF106" && strings.Contains(d.Message, refusal.message)
				}
				if r.Checked || !found {
					t.Fatalf("%s: want EF106 %q, got %+v", target, refusal.message, r.Diagnostics)
				}
			}
		})
	}
}

func TestOmittedArgumentsKeepTheFullRequiredTypeScriptABI(t *testing.T) {
	r := CompileFor(defaultCallsSource("!", omittedDefaultCallers), "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, declarations, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(declarations, "__ef_function_render: (arg_value: string, arg_suffix: string, arg_count: bigint, arg_loud: boolean) => string;") {
		t.Fatalf("callers that omit arguments changed the callee's declared ABI:\n%s", declarations)
	}
	checkStrictTypeScript(t, declarations, `import { render } from "./generated.mjs";
const full: string = render("a", "!", 7n, false);
// @ts-expect-error Call-site defaults are Effra sugar, not optional JavaScript parameters.
render("a");
// @ts-expect-error Omitting a trailing argument is not admitted either.
render("a", "!", 7n);
// @ts-expect-error undefined is not a default marker.
render("a", undefined, 7n, false);
void full;`)
}

const defaultedTailSource = `fn walk(n: i64, step: i64 = 1, acc: i64 = 0) -> i64 {
    if n == 0 {
        acc
    } else {
        if n == 5 {
            walk(n - 1, step: 3, acc: acc + step)
        } else {
            walk(acc: acc + step, n: n - 1)
        }
    }
}
effect fn main() -> string {
    if walk(1000000) == 1000002 { "ok" } else { "bad" }
}
`

func TestDefaultedSelfTailCallsLoopAndResetOmittedSlots(t *testing.T) {
	// The omitted step resets to its constant on every iteration: only the
	// iteration after n == 5 adds 3. A million iterations also overflow a
	// V8 stack unless both self calls, defaulted and labelled, are loops.
	r := CompileFor(defaultedTailSource, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	walk := r.Find("walk").function(t, r)
	if lowerTail(walk) == nil {
		t.Fatal("defaulted, labelled self-tail call was not planned as a loop")
	}
	runOnEveryHost(t, defaultedTailSource, "ok\n")
	js, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(js, "while (true)") != 1 || strings.Count(js, "__ef_function_walk(") != 1+strings.Count(js, "__ef_function_walk(1000000n)") {
		t.Fatalf("a defaulted self-tail call stayed recursive:\n%s", js)
	}
	reset := "const __ef_next_step = 1n;"
	if strings.Count(js, reset) != 1 {
		t.Fatalf("the omitted slot was not reassigned from its constant:\n%s", js)
	}
	// Causal control: keeping the previous iteration's value in the omitted
	// slot changes the result.
	sticky := strings.Replace(js, reset, "const __ef_next_step = __ef_arg_step;", 1)
	assertion := fmt.Sprintf(`if (await Effect.runPromise(%s()) !== "ok") throw new Error("defaulted tail result changed");`, r.Find("main").function(t, r).jsEmissionName())
	if output, err := runJSHostModule(t, "node", js, assertion); err != nil {
		t.Fatalf("defaulted tail module failed: %v\n%s", err, output)
	}
	if output, err := runJSHostModule(t, "node", sticky, assertion); err == nil || !strings.Contains(output, "defaulted tail result changed") {
		t.Fatalf("sticky omitted-slot mutant did not fail at the intended assertion: %v\n%s", err, output)
	}
	native, _, err := emitGoApplication(CompileFor(defaultedTailSource, "go"), GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(native, "for {") != 1 {
		t.Fatalf("expected one native loop:\n%s", native)
	}
}
