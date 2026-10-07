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
	generated, err := r.EmitGo()
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
	if err := WriteRuntime(dir); err != nil {
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
	generated, err := r.EmitGo()
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
	if err := WriteRuntime(dir); err != nil {
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
