package compiler

import (
	"go/ast"
	goformat "go/format"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const concreteVoidAndGenericSource = `
record Holder<T: type> { callback: fn() -> T }
fn finish() -> void { void }
fn selected() -> Holder<void> { Holder<void> { callback: finish } }
fn invokeDirect(callback: fn() -> void) -> void { callback() }
fn invoke(holder: Holder<void>) -> void { holder.callback() }
fn caller() -> void { invokeDirect(finish); invoke(selected()); void }
effect fn main() -> void { caller(); void }
`

func generatedGoFunction(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == name {
			return function
		}
	}
	t.Fatalf("generated function %s not found", name)
	return nil
}

func formatGoNode(t *testing.T, node ast.Node) string {
	t.Helper()
	var output strings.Builder
	if err := goformat.Node(&output, gotoken.NewFileSet(), node); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestConcreteVoidGoDeclarationsAndGenericCarrierBridge(t *testing.T) {
	r := CompileFor(concreteVoidAndGenericSource, "go")
	if !r.Checked {
		t.Fatalf("void ABI source rejected: %+v", r.Diagnostics)
	}
	generated, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "generated.go", generated, 0)
	if err != nil {
		t.Fatalf("generated Go did not parse: %v\n%s", err, generated)
	}

	finish := generatedGoFunction(t, file, "efFunction_finish")
	if finish.Type.Results != nil {
		t.Fatalf("concrete pure void function retained a Go result: %s", formatGoNode(t, finish.Type))
	}
	invoke := generatedGoFunction(t, file, "efFunction_invokeDirect")
	if invoke.Type.Results != nil || invoke.Type.Params == nil || len(invoke.Type.Params.List) != 1 {
		t.Fatalf("concrete callback declaration retained a result carrier: %s", formatGoNode(t, invoke.Type))
	}
	callbackType, ok := invoke.Type.Params.List[0].Type.(*ast.FuncType)
	if !ok || callbackType.Results != nil {
		t.Fatalf("void callback parameter was not a no-result Go function: %s", formatGoNode(t, invoke.Type.Params.List[0].Type))
	}
	main := generatedGoFunction(t, file, "efFunction_main")
	if main.Type.Results == nil || !strings.Contains(formatGoNode(t, main.Type), "efEffect[struct{}]") {
		t.Fatalf("effectful void function lost its runtime carrier: %s", formatGoNode(t, main.Type))
	}
	var holder *ast.StructType
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok.String() != "type" {
			continue
		}
		for _, specification := range gen.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if ok && typeSpec.Name.Name == "efTemplate_Holder" {
				holder, _ = typeSpec.Type.(*ast.StructType)
			}
		}
	}
	if holder == nil || len(holder.Fields.List) != 1 {
		t.Fatalf("generic callable carrier declaration was not emitted: %s", generated)
	}
	genericCallback, ok := holder.Fields.List[0].Type.(*ast.FuncType)
	if !ok || genericCallback.Results == nil || len(genericCallback.Results.List) != 1 {
		t.Fatalf("generic callable field was eagerly specialized to no-result: %s", formatGoNode(t, holder.Fields.List[0].Type))
	}

	if !strings.Contains(generated, "func() struct{}") || !strings.Contains(generated, "efTemp1 := efFunction_finish") {
		t.Fatalf("generic void callback bridge was not emitted: %s", generated)
	}
	if strings.Contains(generated, "func efFunction_finish() struct{}") || strings.Contains(generated, "callback func() struct{}") {
		t.Fatalf("concrete void ABI regressed to a struct carrier: %s", generated)
	}

	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module effra.generated\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(generated), 0644); err != nil {
		t.Fatal(err)
	}
	probe := `package main

import "testing"

func TestConcreteVoidExecution(t *testing.T) {
	calls := 0
	efFunction_invokeDirect(func() { calls++ })
	if calls != 1 {
		t.Fatalf("no-result callback was not invoked exactly once: %d", calls)
	}
	efFunction_invoke(efFunction_selected())
}
`
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(probe), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := runGoCommand(dir, "test", "-race", "."); err != nil {
		t.Fatalf("generated concrete void ABI did not compile and execute: %v\n%s\n%s", err, output, generated)
	}
	binary := filepath.Join(dir, "native")
	if output, err := runGoCommand(dir, "build", "-o", binary, "."); err != nil {
		t.Fatalf("generated concrete void ABI did not build: %v\n%s\n%s", err, output, generated)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("generated concrete void executable failed: %v\n%s", err, output)
	}
}

// voidGoBoundaryPrograms are admitted programs whose void completions and
// declared callable layouts meet at distinct Go lowering boundaries. Each one
// must build and execute natively, and execute identically on JavaScript.
var voidGoBoundaryPrograms = []struct{ name, source string }{
	{"empty-body", `fn empty() -> void {}
effect fn main() -> void { empty(); void }`},
	{"let-final-body", `fn lastLet() -> void { let x = 1 }
effect fn main() -> void { lastLet(); void }`},
	{"stored-void-tail", `fn stored(value: void) -> void { value }
effect fn main() -> void { stored(void); void }`},
	{"if-call-arm", `fn finish() -> void { void }
fn choose(flag: bool) -> void { if flag { finish() } else { void } }
effect fn main() -> void { choose(true); choose(false); void }`},
	{"if-literal-arms", `fn choose(flag: bool) -> void { if flag { void } else { void } }
effect fn main() -> void { choose(true); choose(false); void }`},
	{"exhaustive-match-arms", `enum Mode { Run; Skip }
fn finish() -> void { void }
fn choose(mode: Mode) -> void { match mode { Mode.Run => finish(); Mode.Skip => void } }
effect fn main() -> void { choose(Mode.Run()); choose(Mode.Skip()); void }`},
	{"nested-callable-result", `fn finish() -> void { void }
fn factory() -> fn() -> void { finish }
fn use(make: fn() -> fn() -> void) -> void {
    let done = make();
    done()
}
effect fn main() -> void { use(factory); void }`},
	{"record-field-extraction", `record Holder<T: type> { callback: fn() -> T }
fn finish() -> void { void }
fn selected() -> Holder<void> { Holder<void> { callback: finish } }
fn extract(h: Holder<void>) -> fn() -> void { h.callback }
effect fn main() -> void {
    let done = extract(selected());
    done();
    void
}`},
	{"enum-match-extraction", `enum Hooks<T: type> { Some { callback: fn() -> T }; Empty }
fn skip() -> void {}
fn pick(hooks: Hooks<void>) -> fn() -> void { match hooks { Hooks.Some { callback } => callback; Hooks.Empty => skip } }
effect fn main() -> void { let done = pick(Hooks<void>.Empty {}); done(); void }`},
	{"enum-payload-construction", `enum Hooks<T: type> { Some { callback: fn() -> T } }
fn finish() -> void { void }
fn selected() -> Hooks<void> {
    Hooks<void>.Some { callback: finish }
}
effect fn main() -> void { let h = selected(); void }`},
	{"nested-callback-parameter", `record Runner<T: type> { run: fn(fn() -> T) -> void }
fn invoke(cb: fn() -> void) -> void { cb() }
fn selected() -> Runner<void> { Runner<void> { run: invoke } }
effect fn main() -> void { let r = selected(); void }`},
	{"nested-returned-callable", `record Factory<T: type> { make: fn() -> fn() -> T }
fn finish() -> void { void }
fn factory() -> fn() -> void { finish }
fn selected() -> Factory<void> { Factory<void> { make: factory } }
effect fn main() -> void { let x = selected(); void }`},
	{"recipe-returned-callable", `record Deferred<T: type> { make: effect fn() -> fn() -> T }
fn finish() -> void { void }
effect fn make() -> fn() -> void { finish }
fn selected() -> Deferred<void> { Deferred<void> { make: make } }
effect fn main() -> void {
    let deferred = selected();
    let done = run deferred.make();
    done();
    void
}`},
	{"nested-enum-application-construction", `enum Action<F: callable fn() -> A, A: type> { Some { operation: F } }
record Envelope<T: type> { action: Action<fn() -> T, T> }
fn finish() -> void { void }
fn make() -> Envelope<void> {
    Envelope<void> {
        action: Action<fn() -> void, void>.Some { operation: finish }
    }
}
effect fn main() -> void { let e = make(); void }`},
	{"nested-enum-application-extraction", `enum Action<F: callable fn() -> A, A: type> { Some { operation: F } }
record Envelope<T: type> { action: Action<fn() -> T, T> }
fn finish() -> void { void }
fn make() -> Envelope<void> { Envelope<void> { action: Action<fn() -> void, void>.Some { operation: finish } } }
fn unwrap(e: Envelope<void>) -> Action<fn() -> void, void> { e.action }
fn perform(a: Action<fn() -> void, void>) -> void { match a { Action.Some { operation } => operation() } }
effect fn main() -> void { perform(unwrap(make())); void }`},
	{"nested-record-application", `record Call<F: callable fn() -> A, A: type> { operation: F }
record Envelope<T: type> { call: Call<fn() -> T, T> }
fn finish() -> void { void }
fn make() -> Envelope<void> { Envelope<void> { call: Call<fn() -> void, void> { operation: finish } } }
fn unwrap(e: Envelope<void>) -> Call<fn() -> void, void> { e.call }
effect fn main() -> void { let e = make(); e.call.operation(); let c = unwrap(e); c.operation(); void }`},
	{"nested-application-option-result-transport", `import Data "effra/data"
enum Action<F: callable fn() -> A, A: type> { Some { operation: F } }
record Envelope<T: type> { action: Action<fn() -> T, T> }
fn finish() -> void { void }
fn make() -> Envelope<void> { Envelope<void> { action: Action<fn() -> void, void>.Some { operation: finish } } }
fn wrap() -> Data.Option<Envelope<void>> { Data.Option.Some { value: make() } }
fn settle() -> Data.Result<Envelope<void>, string> { Data.Result<Envelope<void>, string>.Ok { value: make() } }
fn perform(a: Action<fn() -> void, void>) -> void { match a { Action.Some { operation } => operation() } }
effect fn main() -> void {
    match wrap() { Data.Option.Some { value } => perform(value.action); Data.Option.None => void };
    match settle() { Data.Result.Ok { value } => perform(value.action); Data.Result.Err { error } => void };
    void
}`},
}

func TestVoidGoBoundaryProgramsExecuteOnBothTargets(t *testing.T) {
	for _, program := range voidGoBoundaryPrograms {
		t.Run(program.name, func(t *testing.T) {
			t.Parallel()
			runGenericDataNative(t, program.source, "")
			if output := runJSForTarget(t, "js", program.source, `await Effect.runPromise(__ef_function_main());`); output != "" {
				t.Fatalf("void JavaScript program was not silent: %q", output)
			}
		})
	}
}

// voidGoAdapterSource moves instrumented concrete callbacks into and out of
// generic declared callable layouts. The native probe counts invocations to
// prove each adapter captures its callable once and invokes it lazily.
const voidGoAdapterSource = `record Holder<T: type> { callback: fn() -> T }
enum Hooks<T: type> { Some { callback: fn() -> T }; Empty }
record Runner<T: type> { run: fn(fn() -> T) -> void }
record Factory<T: type> { make: fn() -> fn() -> T }
fn skip() -> void {}
fn hold(callback: fn() -> void) -> Holder<void> { Holder<void> { callback: callback } }
fn rehold(holder: Holder<void>) -> Holder<void> { Holder<void> { callback: holder.callback } }
fn release(holder: Holder<void>) -> fn() -> void { holder.callback }
fn hook(callback: fn() -> void) -> Hooks<void> { Hooks<void>.Some { callback: callback } }
fn unhook(hooks: Hooks<void>) -> fn() -> void { match hooks { Hooks.Some { callback } => callback; Hooks.Empty => skip } }
fn runner(execute: fn(fn() -> void) -> void) -> Runner<void> { Runner<void> { run: execute } }
fn runWith(selected: Runner<void>, callback: fn() -> void) -> void { selected.run(callback) }
fn factory(make: fn() -> fn() -> void) -> Factory<void> { Factory<void> { make: make } }
fn produce(selected: Factory<void>) -> fn() -> void { selected.make() }
effect fn main() -> void { void }
`

const voidGoAdapterProbe = `package main

import "testing"

func TestVoidLayoutAdapterCounts(t *testing.T) {
	calls := 0
	count := func() { calls++ }
	holder := efFunction_hold(count)
	recaptured := efFunction_rehold(holder)
	released := efFunction_release(recaptured)
	if calls != 0 {
		t.Fatalf("record adapters invoked the callback while capturing it: %d", calls)
	}
	recaptured.EfField_8_callback()
	released()
	if calls != 2 {
		t.Fatalf("record adapters did not invoke the callback exactly once per call: %d", calls)
	}

	calls = 0
	unhooked := efFunction_unhook(efFunction_hook(count))
	if calls != 0 {
		t.Fatalf("enum adapters invoked the callback while capturing it: %d", calls)
	}
	unhooked()
	unhooked()
	if calls != 2 {
		t.Fatalf("enum adapters did not invoke the callback exactly once per call: %d", calls)
	}

	runs, callbacks := 0, 0
	selected := efFunction_runner(func(callback func()) { runs++; callback() })
	if runs != 0 {
		t.Fatalf("nested parameter adapter invoked its runner while capturing it: %d", runs)
	}
	efFunction_runWith(selected, func() { callbacks++ })
	if runs != 1 || callbacks != 1 {
		t.Fatalf("nested parameter adapter changed invocation counts: runs=%d callbacks=%d", runs, callbacks)
	}

	makes, finishes := 0, 0
	made := efFunction_factory(func() func() { makes++; return func() { finishes++ } })
	if makes != 0 {
		t.Fatalf("returned-callable adapter invoked its factory while capturing it: %d", makes)
	}
	produced := efFunction_produce(made)
	if makes != 1 || finishes != 0 {
		t.Fatalf("returned-callable adapter changed factory counts: makes=%d finishes=%d", makes, finishes)
	}
	produced()
	produced()
	if makes != 1 || finishes != 2 {
		t.Fatalf("returned-callable adapter changed result counts: makes=%d finishes=%d", makes, finishes)
	}
}
`

const voidJSAdapterProbe = `
let calls = 0;
const count = () => { calls++; };
const recaptured = __ef_function_rehold(__ef_function_hold(count));
const released = __ef_function_release(recaptured);
if (calls !== 0) throw new Error("record capture invoked " + calls);
recaptured.callback();
released();
if (calls !== 2) throw new Error("record calls " + calls);
calls = 0;
const unhooked = __ef_function_unhook(__ef_function_hook(count));
if (calls !== 0) throw new Error("enum capture invoked " + calls);
unhooked();
unhooked();
if (calls !== 2) throw new Error("enum calls " + calls);
let runs = 0, callbacks = 0;
__ef_function_runWith(__ef_function_runner((callback) => { runs++; callback(); }), () => { callbacks++; });
if (runs !== 1 || callbacks !== 1) throw new Error("runner " + runs + "/" + callbacks);
let makes = 0, finishes = 0;
const produced = __ef_function_produce(__ef_function_factory(() => { makes++; return () => { finishes++; }; }));
produced();
produced();
if (makes !== 1 || finishes !== 2) throw new Error("factory " + makes + "/" + finishes);
console.log("counted");
`

func TestVoidGoCallableLayoutAdaptersCaptureOnceAndInvokeLazily(t *testing.T) {
	r := CompileFor(voidGoAdapterSource, "go")
	if !r.Checked {
		t.Fatalf("adapter source rejected: %+v", r.Diagnostics)
	}
	// The Go probe calls these functions directly; main does not reach them.
	generated, application, err := emitGoApplication(r, GoGenerationBuild, hostFunction("hold"), hostFunction("rehold"), hostFunction("release"), hostFunction("hook"), hostFunction("unhook"), hostFunction("runner"), hostFunction("runWith"), hostFunction("factory"), hostFunction("produce"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "generated.go", generated, 0)
	if err != nil {
		t.Fatalf("generated Go did not parse: %v\n%s", err, generated)
	}
	// A callable returning a no-result callable keeps its own result.
	factory := generatedGoFunction(t, file, "efFunction_factory")
	if factory.Type.Params == nil || len(factory.Type.Params.List) != 1 {
		t.Fatalf("factory parameter list changed: %s", formatGoNode(t, factory.Type))
	}
	maker, ok := factory.Type.Params.List[0].Type.(*ast.FuncType)
	if !ok || maker.Results == nil || len(maker.Results.List) != 1 {
		t.Fatalf("nested void callable erased its enclosing result: %s", formatGoNode(t, factory.Type.Params.List[0].Type))
	}
	if produced, ok := maker.Results.List[0].Type.(*ast.FuncType); !ok || produced.Results != nil {
		t.Fatalf("returned void callable retained a result carrier: %s", formatGoNode(t, maker))
	}

	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"go.mod": "module effra.generated\n\ngo 1.27\n", "main.go": generated, "main_test.go": voidGoAdapterProbe} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(dir, "test", "."); err != nil {
		t.Fatalf("generated callable layout adapters failed: %v\n%s\n%s", err, output, generated)
	}
	if output := runJSForTarget(t, "js", voidGoAdapterSource, voidJSAdapterProbe); output != "counted\n" {
		t.Fatalf("JavaScript callback counts changed: %q", output)
	}
}

// voidGoNestedApplicationSource stores concrete callbacks inside generic data
// whose Go instantiation differs between the declared field layout and the
// use site, in both a record and an enum inner carrier.
const voidGoNestedApplicationSource = `record Call<F: callable fn() -> A, A: type> { operation: F }
enum Action<F: callable fn() -> A, A: type> { Some { operation: F }; Idle }
record Envelope<T: type> { call: Call<fn() -> T, T>, action: Action<fn() -> T, T> }
fn envelop(callback: fn() -> void) -> Envelope<void> {
    Envelope<void> { call: Call<fn() -> void, void> { operation: callback }, action: Action<fn() -> void, void>.Some { operation: callback } }
}
fn reenvelop(e: Envelope<void>) -> Envelope<void> { Envelope<void> { call: e.call, action: e.action } }
fn call(e: Envelope<void>) -> Call<fn() -> void, void> { e.call }
fn action(e: Envelope<void>) -> Action<fn() -> void, void> { e.action }
fn perform(a: Action<fn() -> void, void>) -> void { match a { Action.Some { operation } => operation(); Action.Idle => void } }
fn fire(e: Envelope<void>) -> void { e.call.operation() }
effect fn main() -> void { void }
`

const voidGoNestedApplicationProbe = `package main

import "testing"

func TestVoidNestedApplicationAdapterCounts(t *testing.T) {
	calls := 0
	envelope := efFunction_reenvelop(efFunction_envelop(func() { calls++ }))
	call := efFunction_call(envelope)
	action := efFunction_action(envelope)
	if calls != 0 {
		t.Fatalf("nested application adapters invoked the callback while converting it: %d", calls)
	}
	call.EfField_9_operation()
	efFunction_perform(action)
	efFunction_fire(envelope)
	if calls != 3 {
		t.Fatalf("nested application adapters did not invoke the callback exactly once per call: %d", calls)
	}
}
`

const voidJSNestedApplicationProbe = `
let calls = 0;
const envelope = __ef_function_reenvelop(__ef_function_envelop(() => { calls++; }));
const call = __ef_function_call(envelope);
const action = __ef_function_action(envelope);
if (calls !== 0) throw new Error("nested capture invoked " + calls);
call.operation();
__ef_function_perform(action);
__ef_function_fire(envelope);
if (calls !== 3) throw new Error("nested calls " + calls);
console.log("counted");
`

func TestVoidGoNestedApplicationAdaptersPreserveIdentityAndCounts(t *testing.T) {
	r := CompileFor(voidGoNestedApplicationSource, "go")
	if !r.Checked {
		t.Fatalf("nested application source rejected: %+v", r.Diagnostics)
	}
	// The Go probe calls these functions directly; main does not reach them.
	generated, application, err := emitGoApplication(r, GoGenerationBuild, hostFunction("envelop"), hostFunction("reenvelop"), hostFunction("call"), hostFunction("action"), hostFunction("perform"), hostFunction("fire"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "generated.go", generated, 0)
	if err != nil {
		t.Fatalf("generated Go did not parse: %v\n%s", err, generated)
	}
	// The conversion keeps the nominal declarations: the extracted value is
	// the concrete instantiation, never an interface{} or a structural copy.
	for name, want := range map[string]string{"efFunction_call": "efTemplate_Call[func(), struct{}]", "efFunction_action": "efTemplate_Action[func(), struct{}]"} {
		function := generatedGoFunction(t, file, name)
		if function.Type.Results == nil || len(function.Type.Results.List) != 1 || formatGoNode(t, function.Type.Results.List[0].Type) != want {
			t.Fatalf("%s result changed: %s", name, formatGoNode(t, function.Type))
		}
	}
	if strings.Contains(generated, "any(") || strings.Contains(generated, "interface{}(") {
		t.Fatalf("nested application adapter erased the nominal type: %s", generated)
	}

	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"go.mod": "module effra.generated\n\ngo 1.27\n", "main.go": generated, "main_test.go": voidGoNestedApplicationProbe} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(dir, "test", "."); err != nil {
		t.Fatalf("generated nested application adapters failed: %v\n%s\n%s", err, output, generated)
	}
	if output := runJSForTarget(t, "js", voidGoNestedApplicationSource, voidJSNestedApplicationProbe); output != "counted\n" {
		t.Fatalf("JavaScript nested application counts changed: %q", output)
	}
}

// providerConfigurationPrograms capture generic configuration values whose
// Go representation comes from their checked application, including one
// nested inside another application's type argument.
var providerConfigurationPrograms = []struct{ name, source, output string }{
	{"void-holder", `record Holder<T: type> { callback: fn() -> T }
service Task { effect fn execute() -> void }
fn finish() -> void { void }
impl Configured(holder: Holder<void>) for Task {
    effect fn execute() -> void { holder.callback() }
}
effect fn main() -> void {
    let task = run Configured(Holder<void> { callback: finish });
    run Task.execute().provide<Task>(task);
    void
}`, ""},
	{"string-holder", `record Holder<T: type> { callback: fn() -> T }
service Task { effect fn execute() -> string }
fn label() -> string { "configured" }
impl Configured(holder: Holder<string>) for Task {
    effect fn execute() -> string { holder.callback() }
}
effect fn main() -> string {
    let task = run Configured(Holder<string> { callback: label });
    run Task.execute().provide<Task>(task)
}`, "configured"},
	{"nested-application", `enum Action<F: callable fn() -> A, A: type> { Some { operation: F } }
record Envelope<T: type> { action: Action<fn() -> T, T> }
service Task { effect fn execute() -> void }
fn finish() -> void { void }
impl Configured(envelope: Envelope<void>) for Task {
    effect fn execute() -> void { match envelope.action { Action.Some { operation } => operation() } }
}
effect fn main() -> void {
    let task = run Configured(Envelope<void> { action: Action<fn() -> void, void>.Some { operation: finish } });
    run Task.execute().provide<Task>(task);
    void
}`, ""},
}

func TestProviderConfigurationKeepsCheckedGenericTypeAcrossTargets(t *testing.T) {
	for _, program := range providerConfigurationPrograms {
		t.Run(program.name, func(t *testing.T) {
			t.Parallel()
			expected := ""
			if program.output != "" {
				expected = program.output + "\n"
			}
			runGenericDataNative(t, program.source, expected)
			if output := runJSForTarget(t, "js", program.source, `const value = await Effect.runPromise(__ef_function_main()); if (value !== undefined) console.log(value);`); output != expected {
				t.Fatalf("JavaScript provider configuration output changed: %q", output)
			}
		})
	}
}
