package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func readRecipeExample(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

// Two unrelated consumers share the bundled generic declarations. Building a
// container runs nothing; every run of a stored recipe executes it again.
func TestRecipeConsumersExecuteLazilyAndIndependentlyOnGoAndJS(t *testing.T) {
	for _, fixture := range []struct{ example, output string }{
		{"recipes-retry.ef", "constructed\nprobe flaky\nprobe stable\nprobe flaky\nprobe stable\nflaky failed; stable ok\nflaky failed; stable ok\n"},
		{"recipes-queue.ef", "queued\ndeliver a\ndeliver bad\ndeliver a\nsent a, rejected bad, late deferred late, sent a\n"},
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

func TestRecipeStoredInDataRunsOncePerExecution(t *testing.T) {
	source := readRecipeExample(t, "recipes-queue.ef")
	output := runJSForTarget(t, "js", source, `
let calls = 0;
const console = { log: message => Effect.sync(() => { calls++; }) };
const recipe = __ef_function_delivery("a");
const job = __ef_function_enqueue({ prepare: __ef_function_delivery }, "a");
if (calls !== 0) throw new Error("construction executed a recipe");
const first = await Effect.runPromise(Effect.provideService(job.task, __ef_service_Console, console));
if (calls !== 1 || first !== "sent a") throw new Error("first run: " + calls);
await Effect.runPromise(Effect.provideService(job.task, __ef_service_Console, console));
await Effect.runPromise(Effect.provideService(recipe, __ef_service_Console, console));
if (calls !== 3) throw new Error("runs shared or skipped work: " + calls);
console.log === undefined;
globalThis.console.log("counted");
`)
	if output != "counted\n" {
		t.Fatal(output)
	}
}

func TestRecipeTypesShareCanonicalIdentityAndPreserveRows(t *testing.T) {
	source := readRecipeExample(t, "recipes-queue.ef")
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	symbols := map[string]Symbol{}
	for _, symbol := range r.Symbols {
		symbols[symbol.Name] = symbol
	}
	declared := symbols["delivery"].Contract.Type
	constructed := symbols["deliver"].Actual.Contract
	if declared.Kind != "recipe" || declared.ID == "" || declared.ID != constructed.ID {
		t.Fatalf("declared and constructed recipes differ: %+v %+v", declared, constructed)
	}
	if declared.Result != "t:dc0347e1f7f5c4d9" || declared.FailureRow == "" || declared.ServiceRow == "" {
		t.Fatalf("recipe identity lost its explicit rows: %+v", declared)
	}
	if !slices.Equal(symbols["admit"].Contract.Type.ArgIDs, []string{declared.ID, "t:dc0347e1f7f5c4d9"}) {
		t.Fatalf("Result application lost recipe identity: %+v", symbols["admit"].Contract.Type)
	}
	info, err := r.TypeAt(strings.Index(source, "task.catch"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Type.Contract.ID != declared.ID || !slices.Equal(info.Type.Errors, []string{"Rejected"}) || !slices.Equal(info.Type.Services, []string{"Console"}) {
		t.Fatalf("enum payload erased recipe rows: %+v", info.Type)
	}
	retry := readRecipeExample(t, "recipes-retry.ef")
	rr := Compile(retry)
	info, err = rr.TypeAt(strings.Index(retry, "work.catch"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Type.Contract.Kind != "recipe" || !slices.Equal(info.Type.Errors, []string{"Transient"}) || !slices.Equal(info.Type.Services, []string{"Console"}) {
		t.Fatalf("record field erased recipe rows: %+v", info.Type)
	}
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	checkStrictTypeScript(t, declaration, `import type { Job, Worker, ConsoleRequirement } from "./generated.mjs";
import type { Effect } from "effect";
type Expected = Effect.Effect<string, { readonly _tag: "Rejected" }, ConsoleRequirement>;
declare const job: Job<string>;
declare const worker: Worker;
const prepared: Expected = worker.prepare("a");
if (job._tag === "template:module:file:module:Job.Ready") {
  const task: Expected = job.task;
  // @ts-expect-error a stored recipe keeps its service row
  const erased: Effect.Effect<string, { readonly _tag: "Rejected" }, never> = job.task;
  // @ts-expect-error a stored recipe keeps its failure row
  const absorbed: Effect.Effect<string, never, ConsoleRequirement> = job.task;
  void task; void erased; void absorbed;
}
void prepared;
`)
}

const recipeRowDeclarations = `import Data "effra/data"
error A
error B
record Slot<T: type> {
    task: Effect<T, { A }, { Console }>
}
effect fn ab() -> string raises { A, B } {
    fail B
}
effect fn plain() -> string {
    "p"
}
`

func TestRecipeAssignabilityAndRowRefusals(t *testing.T) {
	for _, test := range []struct{ name, source, code, message string }{
		{"rows widen into a declared slot", `effect fn main() -> void {
    let s = Slot { task: plain() }
    void
}`, "", ""},
		{"undeclared failure refused at construction", `effect fn main() -> void {
    let s = Slot { task: ab() }
    void
}`, "EF127", "constructor field task must be Effect<string, {A}, {Console}>"},
		{"stored failure must be declared", `effect fn go(s: Slot<string>) -> string uses { Console } {
    run s.task
}
effect fn main() -> void {
    void
}`, "EF107", "undeclared failures: A"},
		{"stored service must be declared", `effect fn go(s: Slot<string>) -> string raises { A } {
    run s.task
}
effect fn main() -> void {
    void
}`, "EF108", "missing service requirements: Console"},
		{"applications hold recipes invariantly", `fn keep(o: Data.Option<Effect<string, { A, B }>>) -> string {
    "k"
}
effect fn main() -> void {
    let o = Data.Option<Effect<string, { A }>>.None {}
    let k = keep(o)
    void
}`, "EF106", "argument must be Data.Option<Effect<string, {A, B}>>"},
		{"returned failure rows are upper bounds", `fn widen(t: Effect<string, { A }>) -> Effect<string, { A, B }> {
    t
}
fn narrow(t: Effect<string, { A, B }>) -> Effect<string, { A }> {
    t
}
effect fn main() -> void {
    void
}`, "EF106", "body returns Effect<string, {A, B}>; expected Effect<string, {A}>"},
		{"service rows cannot be dropped", `fn drop(t: Effect<string, {}, { Console }>) -> Effect<string> {
    t
}
effect fn main() -> void {
    void
}`, "EF106", "body returns Effect<string, {}, {Console}>; expected Effect<string>"},
		{"discarded stored recipe", `effect fn go(s: Slot<string>) -> string raises { A } uses { Console } {
    s.task
    "x"
}
effect fn main() -> void {
    void
}`, "EF105", "unused lazy effect"},
		{"ambiguous instantiation", `effect fn main() -> void {
    let o = Data.Option.None {}
    void
}`, "EF127", "type parameter T requires a complete explicit constructor argument"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(recipeRowDeclarations + test.source)
			if test.code == "" {
				if !r.Checked {
					t.Fatal(r.Diagnostics)
				}
				return
			}
			if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != test.code || !strings.Contains(r.Diagnostics[0].Message, test.message) {
				t.Fatalf("want exactly %s %q, got %+v", test.code, test.message, r.Diagnostics)
			}
		})
	}
}

const recipeOwnershipHolder = `import Data "effra/data"
record Holder<T: type> {
    task: Effect<T, { IoError }, { Files }>
}
record Opener {
    open: Effect<File, { IoError }>
}
fn wrap(t: Effect<string, { IoError }, { Files }>) -> Holder<string> {
    Holder { task: t }
}
`

func TestRecipeHeldHandlesFollowContainerOwnership(t *testing.T) {
	const escape = "value owned by closing scope cannot escape"
	for _, test := range []struct{ name, source, message string }{
		{"inner handle in generic record", `effect fn leak() -> Holder<string> raises { IoError } uses { Files } {
    scope {
        let file = run Files.openRead("x").provide<Files>(LiveFiles)
        Holder { task: Files.readText(file) }
    }
}`, escape},
		{"inner handle through helper", `effect fn leak() -> Holder<string> raises { IoError } uses { Files } {
    scope {
        let file = run Files.openRead("x").provide<Files>(LiveFiles)
        wrap(Files.readText(file))
    }
}`, escape},
		{"inner handle in bundled Option", `effect fn leak() -> Data.Option<Effect<string, { IoError }, { Files }>> raises { IoError } uses { Files } {
    scope {
        let file = run Files.openRead("x").provide<Files>(LiveFiles)
        Data.Option.Some { value: Files.readText(file) }
    }
}`, escape},
		{"borrowed handle accepted", `effect fn keep(file: File) -> Holder<string> {
    scope {
        Holder { task: Files.readText(file) }
    }
}`, ""},
		{"retained constructor result accepted", `effect fn open() -> string raises { IoError } uses { Files } {
    let o = Opener { open: Files.openRead("x").provide<Files>(LiveFiles) }
    let f = run o.open
    run Files.readText(f)
}`, ""},
		{"retained constructor result stays scoped", `effect fn open() -> string raises { IoError } uses { Files } {
    let o = Opener { open: Files.openRead("x").provide<Files>(LiveFiles) }
    let f = scope {
        run o.open
    }
    run Files.readText(f)
}`, escape},
		{"recipe factory result stays scoped", `effect fn opener() -> Effect<File, { IoError }> {
    Files.openRead("x").provide<Files>(LiveFiles)
}
effect fn open() -> string raises { IoError } uses { Files } {
    let r = run opener()
    let f = scope {
        run r
    }
    run Files.readText(f)
}`, escape},
		// A recipe read from a parameter has Param layer evidence (design
		// §5.1): its result is a handle it holds or one its execution
		// acquired, so the runner owns it.
		{"parameter recipe result owned by its runner", `effect fn use(o: Opener) -> string raises { IoError } uses { Files } {
    let f = run o.open
    run Files.readText(f)
}`, ""},
		{"parameter recipe result stays scoped", `effect fn use(o: Opener) -> string raises { IoError } uses { Files } {
    let f = scope {
        run o.open
    }
    run Files.readText(f)
}`, escape},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(recipeOwnershipHolder + test.source + "\neffect fn main() -> void {\n    void\n}")
			if test.message == "" {
				if !r.Checked {
					t.Fatal(r.Diagnostics)
				}
				return
			}
			if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF123" || !strings.Contains(r.Diagnostics[0].Message, test.message) {
				t.Fatalf("want EF123 %q, got %+v", test.message, r.Diagnostics)
			}
		})
	}
}

func TestRecipeRecursiveLayouts(t *testing.T) {
	const stream = `record Stream {
    head: string;
    tail: Effect<Stream>
}
effect fn unfold(label: string) -> Stream {
    Stream {
        head: label,
        tail: unfold(label + "+")
    }
}
effect fn main() -> void {
    let s = run unfold("a")
    let t = run s.tail
    let u = run t.tail
    run Console.log(s.head + " " + t.head + " " + u.head).provide<Console>(Stdout)
}`
	// A recipe is a lazy indirection, so a nominal record may refer to itself
	// through one.
	runGenericDataNative(t, stream, "a a+ a++\n")
	if output := runJSForTarget(t, "js", stream, `await Effect.runPromise(__ef_function_main());`); output != "a a+ a++\n" {
		t.Fatal(output)
	}
	for _, test := range []struct{ source, code, message string }{
		{`record Bad { label: string; again: Bad }`, "EF119", "recursive data layout is unsupported"},
		{`record Stream<T: type> { head: T; tail: Effect<Stream<T>> }`, "EF127", "recursive or excessive generic data layout"},
	} {
		r := Compile(test.source + " effect fn main() -> void { void }")
		if r.Checked || !slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool {
			return d.Code == test.code && strings.Contains(d.Message, test.message)
		}) {
			t.Fatalf("%s: want %s, got %+v", test.source, test.code, r.Diagnostics)
		}
	}
}

func recipeDAGSource(depth int) string {
	var source strings.Builder
	source.WriteString("import Data \"effra/data\"\nerror Lost\nrecord Leaf<T: type> { value: T }\n")
	previous := "Leaf"
	for i := 0; i < depth; i++ {
		name := fmt.Sprintf("Layer%d", i)
		fmt.Fprintf(&source, "record %s<T: type> { now: Effect<%s<T>, { Lost }, { Console }>; later: Data.Option<Effect<%s<T>, { Lost }, { Console }>> }\n", name, previous, previous)
		previous = name
	}
	fmt.Fprintf(&source, "fn keep(value: %s<string>) -> %s<string> { value }\neffect fn main() -> void { void }", previous, previous)
	return source.String()
}

// Each layer names its predecessor twice, so the layout has 2^depth paths.
// Canonical recipe and application nodes are shared, so admitted work and the
// published projection grow linearly. Past the publication budget the checked
// source keeps its validity but publishes no partial type authority.
func TestRecipeSharedDAGStaysLinearAndBounded(t *testing.T) {
	usage := map[int]ProjectionUsage{}
	for _, depth := range []int{16, 32, 64} {
		r := Compile(recipeDAGSource(depth))
		if !r.Checked || !r.TypeProjectionComplete {
			t.Fatalf("depth %d refused: %+v", depth, r.Diagnostics)
		}
		usage[depth] = r.TypeProjectionUsage
	}
	for _, pair := range [][2]int{{16, 32}, {32, 64}} {
		small, large := usage[pair[0]], usage[pair[1]]
		if large.Nodes <= small.Nodes || large.Nodes-small.Nodes > small.Nodes+16 || large.Edges-small.Edges > small.Edges+32 {
			t.Fatalf("shared recipe DAG grew superlinearly: %+v", usage)
		}
	}
	r := Compile(recipeDAGSource(400))
	if !r.Checked || r.TypeProjectionComplete {
		t.Fatalf("exhausted publication budget was not refused: checked=%v %+v", r.Checked, r.Diagnostics)
	}
	response := r.CheckResponse()
	for _, key := range []string{"symbols", "types", "rows", "declarations"} {
		if response[key] != nil {
			t.Fatalf("refused publication retained %s authority", key)
		}
	}
}

func TestRecipeTypeFormattingRoundTrip(t *testing.T) {
	const source = `import Data "effra/data"
error A error B
record Slot<T: type> { now: Effect<T,{B,A},{Console}>; plain: Effect<T>; failing: Effect<T,{A}>; later: Data.Option<Effect<T,{A},{Console}>> }
fn keep(slot: Slot<string>) -> Effect<string,{A,B},{Console}> { slot.now }
effect fn main() -> void { void }`
	formatted, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"now: Effect<T, { B, A }, { Console }>;",
		"plain: Effect<T>;",
		"failing: Effect<T, { A }>;",
		"later: Data.Option<Effect<T, { A }, { Console }>>",
		"fn keep(slot: Slot<string>) -> Effect<string, { A, B }, { Console }> {",
	} {
		if !strings.Contains(formatted.Text, want) {
			t.Fatalf("missing %q in\n%s", want, formatted.Text)
		}
	}
	second, err := FormatSource(formatted.Text)
	if err != nil || second.Text != formatted.Text {
		t.Fatalf("recipe formatting is not idempotent: %v\n%s", err, second.Text)
	}
	before, after := Compile(source), Compile(formatted.Text)
	if !before.Checked || !after.Checked {
		t.Fatal(before.Diagnostics, after.Diagnostics)
	}
	if before.Symbols[0].Contract.Type.ID != after.Symbols[0].Contract.Type.ID || before.Symbols[0].Contract.Type.Kind != "recipe" {
		t.Fatalf("formatting changed recipe identity: %+v %+v", before.Symbols[0].Contract.Type, after.Symbols[0].Contract.Type)
	}
	for _, example := range []string{"recipes-retry.ef", "recipes-queue.ef"} {
		text := readRecipeExample(t, example)
		if formatted, err := FormatSource(text); err != nil || formatted.Text != text {
			t.Fatalf("%s is not canonically formatted: %v", example, err)
		}
	}
}

// A recipe value is transported by the handles it holds. What running it
// returns stays on the recipe until it actually runs, so forwarding or
// storing an unopened managed-result recipe is safe while running one inside
// a closing scope, or escaping an acquired handle it captured, is not.
func TestRecipeOwnerBoundaryTransportsHeldFacts(t *testing.T) {
	// Each row declares only its own helpers, so a row is red or green by its
	// transport alone rather than by a shared fixture declaration.
	const opener = `record Opener {
    open: Effect<File, { IoError }>
}
`
	const forwarding = `fn forward(t: Effect<File, { IoError }>) -> Effect<File, { IoError }> {
    t
}
fn pass(t: Effect<File, { IoError }>) -> Effect<File, { IoError }> {
    forward(t)
}
`
	const makePure = opener + `fn makePure() -> Opener {
    Opener { open: Files.openRead("x").provide<Files>(LiveFiles) }
}
`
	const make = opener + `effect fn make() -> Opener {
    Opener { open: Files.openRead("x").provide<Files>(LiveFiles) }
}
`
	const readIt = `effect fn readIt(file: File) -> string raises { IoError } {
    run Files.readText(file).provide<Files>(LiveFiles)
}
`
	for _, test := range []struct{ name, declarations, main, code string }{
		{"unopened forwarding", forwarding, `let o = forward(Files.openRead("x").provide<Files>(LiveFiles))
    let p = pass(o)
    void`, ""},
		{"direct construction in a scope", opener, `let o = scope { Opener { open: Files.openRead("x").provide<Files>(LiveFiles) } }
    void`, ""},
		{"pure factory in a scope", makePure, `let o = scope { makePure() }
    void`, ""},
		{"effectful factory in a scope", make, `let o = scope { run make() }
    void`, ""},
		{"running the stored recipe in a closing scope", make, `let f = scope {
        let o = run make()
        run o.open
    }
    void`, "EF123"},
		{"captured acquired handle escapes", readIt, `let r = scope {
        let file = run Files.openRead("x").provide<Files>(LiveFiles)
        readIt(file)
    }
    void`, "EF123"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := CompileFor(test.declarations+"effect fn main() -> void raises { IoError } {\n    "+test.main+"\n}\n", "go")
			if test.code == "" && !r.Checked {
				t.Fatalf("refused safe transport: %+v", r.Diagnostics)
			}
			if test.code != "" && (r.Checked || !hasCode(r, test.code)) {
				t.Fatalf("want %s: %+v", test.code, r.Diagnostics)
			}
		})
	}
}

const branchRecipeSource = `error Late
error Lost
enum Choice {
    First
    Second
}
effect fn tick(label: string) -> string raises { Late } uses { Console } {
    run Console.log("tick " + label)
    label
}
effect fn tock(label: string) -> string raises { Lost } uses { Console } {
    run Console.log("tock " + label)
    label
}
fn pick(flag: bool) -> Effect<string, { Late, Lost }, { Console }> {
    if flag {
        tick("if")
    } else {
        tock("if")
    }
}
fn choose(choice: Choice) -> Effect<string, { Late, Lost }, { Console }> {
    match choice {
        Choice.First => tick("match"),
        Choice.Second => tock("match")
    }
}
effect fn main() -> void {
    let selected = pick(true)
    let matched = choose(Choice.Second {})
    run Console.log("selected").provide<Console>(Stdout)
    let first = run selected.provide<Console>(Stdout).catch<Late>("late").catch<Lost>("lost")
    let second = run selected.provide<Console>(Stdout).catch<Late>("late").catch<Lost>("lost")
    let third = run matched.provide<Console>(Stdout).catch<Late>("late").catch<Lost>("lost")
    run Console.log(first + second + third).provide<Console>(Stdout)
}
`

// A branch selects a recipe value without running it; each explicit run
// executes the selected recipe again.
func TestRecipeBranchResultsRunOnlyWhenRunOnGoAndJS(t *testing.T) {
	const expected = "selected\ntick if\ntick if\ntock match\nififmatch\n"
	runGenericDataNative(t, branchRecipeSource, expected)
	if output := runJSForTarget(t, "js", branchRecipeSource, `await Effect.runPromise(__ef_function_main());`); output != expected {
		t.Fatalf("JS execution:\n%s", output)
	}
}

func TestRecipeBranchResultsJoinRowsAndKeepDiscardRefusal(t *testing.T) {
	r := Compile(branchRecipeSource)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	for _, anchor := range []string{"if flag", "match choice"} {
		info, err := r.TypeAt(strings.Index(branchRecipeSource, anchor))
		if err != nil {
			t.Fatal(err)
		}
		if info.Type.Contract.Kind != "recipe" || !slices.Equal(info.Type.Errors, []string{"Late", "Lost"}) || !slices.Equal(info.Type.Services, []string{"Console"}) {
			t.Fatalf("%s branch rows not joined: %+v", anchor, info.Type)
		}
	}
	declarations := branchRecipeSource[:strings.Index(branchRecipeSource, "fn pick")]
	for _, test := range []struct{ name, source, code, message string }{
		{"joined row exceeds a narrower declared row", `fn narrow(flag: bool) -> Effect<string, { Late }, { Console }> {
    if flag {
        tick("a")
    } else {
        tock("b")
    }
}
effect fn main() -> void {
    void
}`, "EF106", "body returns Effect<string, {Late, Lost}, {Console}>; expected Effect<string, {Late}, {Console}>"},
		{"branch-selected recipe keeps its captured owner", `effect fn readIt(file: File) -> string raises { IoError } {
    run Files.readText(file).provide<Files>(LiveFiles)
}
effect fn main() -> void raises { IoError } {
    let r = scope {
        let file = run Files.openRead("x").provide<Files>(LiveFiles)
        if true {
            readIt(file)
        } else {
            readIt(file)
        }
    }
    void
}`, "EF123", "value owned by closing scope cannot escape"},
		{"discarded branch recipe", `effect fn main() -> void uses { Console } {
    if true {
        tick("a")
    } else {
        tock("b")
    }
    void
}`, "EF105", "unused lazy effect"},
		{"branch recipe is not an executed result", `effect fn main() -> string uses { Console } {
    if true {
        tick("a")
    } else {
        tock("b")
    }
}`, "EF105", "unused lazy effect"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(declarations + test.source)
			found := false
			for _, diagnostic := range r.Diagnostics {
				found = found || diagnostic.Code == test.code && strings.Contains(diagnostic.Message, test.message)
			}
			if r.Checked || !found {
				t.Fatalf("want %s %q: %+v", test.code, test.message, r.Diagnostics)
			}
		})
	}
}

// Recipe branches whose success is an enum join variant occurrences by the
// success layout, so disjoint alternatives from each arm stay admissible.
const branchVariantSource = `enum Outcome {
    Done { value: string }
    Failed { reason: string }
}
enum Choice {
    First
    Second
}
effect fn done() -> Outcome uses { Console } {
    run Console.log("done ran")
    Outcome.Done { value: "ok" }
}
effect fn failed() -> Outcome uses { Console } {
    run Console.log("failed ran")
    Outcome.Failed { reason: "bad" }
}
fn pick(flag: bool) -> Effect<Outcome, {}, { Console }> {
    if flag { done() } else { failed() }
}
fn choose(choice: Choice) -> Effect<Outcome, {}, { Console }> {
    match choice {
        Choice.First => done(),
        Choice.Second => failed()
    }
}
fn describe(outcome: Outcome) -> string {
    match outcome {
        Outcome.Done { value } => "done " + value,
        Outcome.Failed { reason } => "failed " + reason
    }
}
effect fn main() -> void {
    let selected = pick(false)
    let matched = choose(Choice.First {})
    run Console.log("selected").provide<Console>(Stdout)
    let first = run selected.provide<Console>(Stdout)
    let second = run matched.provide<Console>(Stdout)
    run Console.log(describe(first) + "/" + describe(second)).provide<Console>(Stdout)
}
`

func TestRecipeBranchEnumSuccessJoinsByLayout(t *testing.T) {
	r := Compile(branchVariantSource)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	expected := "selected\nfailed ran\ndone ran\nfailed bad/done ok\n"
	runGenericDataNative(t, branchVariantSource, expected)
	if output := runJSForTarget(t, "js", branchVariantSource, `await Effect.runPromise(__ef_function_main());`); output != expected {
		t.Fatalf("JS branch enum output = %q", output)
	}
}

func TestRecipeBranchEnumMissingEvidenceStaysConservative(t *testing.T) {
	prefix := `enum Held {
    Open { file: File }
    Empty
}
effect fn borrow(file: File) -> Held { Held.Open { file: file } }
effect fn nothing() -> Held { Held.Empty {} }
`
	known := prefix + `fn known(flag: bool, file: File) -> Effect<Held> {
    if flag { borrow(file) } else { nothing() }
}
effect fn use(file: File) -> Held { run known(true, file) }
effect fn main() -> void { void }
`
	if r := Compile(known); !r.Checked {
		t.Fatalf("both arms initialize the managed enum: %v", r.Diagnostics)
	}
	// A parameter recipe can produce any alternative, so the join must not
	// narrow to the initialized arm's variants: its Param layer evidence
	// (design §5.1) owns every variant path by the runner, so an acquisition
	// through the parameter arm still cannot escape a closing scope.
	unknown := prefix + `effect fn fresh() -> Held raises { IoError } uses { Files } {
    let g = run Files.openRead("x").provide<Files>(LiveFiles)
    Held.Open { file: g }
}
fn unknown(flag: bool, file: File, other: Effect<Held, { IoError }, { Files }>) -> Effect<Held, { IoError }, { Files }> {
    if flag { borrow(file) } else { other }
}
effect fn leak(file: File) -> Held raises { IoError } uses { Files } {
    scope { run unknown(false, file, fresh()) }
}
effect fn main() -> void { void }
`
	if r := Compile(unknown); r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF123" {
		t.Fatalf("missing branch evidence must stay conservative: %+v", r.Diagnostics)
	}
	r := Compile(unknown)
	symbol := r.Find("unknown")
	if symbol == nil || !slices.ContainsFunc(symbol.Contract.Ownership, func(fact OwnershipFact) bool {
		return fact.Path == "Open.file" && fact.ownerKind == ownershipOwnerExec
	}) {
		t.Fatalf("the parameter arm's owners were narrowed away: %+v", symbol)
	}
}

// Two timeouts close two owners: each first use reports its own root, and the
// second use of the first handle repeats a reported root and stays silent.
func TestOwnershipReportsEachRootOnce(t *testing.T) {
	r := Compile(`effect fn leak() -> string raises { IoError, Timeout } uses { Files, Scheduler } {
    let f = run Files.openRead("x").provide<Files>(LiveFiles).timeout(5000)
    let g = run Files.openRead("y").provide<Files>(LiveFiles).timeout(5000)
    let a = run Files.readText(f)
    let b = run Files.readText(f)
    run Files.readText(g)
}
effect fn main() -> void {
    void
}`)
	if len(r.Diagnostics) != 2 || r.Diagnostics[0].Span.Line != 4 || r.Diagnostics[1].Span.Line != 6 {
		t.Fatalf("want one EF123 per closed owner at lines 4 and 6, got %+v", r.Diagnostics)
	}
	reports := r.projector.ownershipReports
	if len(reports) != 2 || len(reports[0].roots) != 1 || len(reports[1].roots) != 1 || reports[0].roots[0] == reports[1].roots[0] {
		t.Fatalf("want two reports naming distinct roots, got %+v", reports)
	}
	for _, report := range reports {
		if root := report.roots[0].root; root.kind != "closed" || !strings.HasPrefix(root.region, "timeout:") {
			t.Fatalf("want a closed timeout root, got %+v", root)
		}
	}
}

// TestDistinctOwnershipReportsHasARedControl: the distinct-roots predicate
// fails for a report which names only roots an earlier report named, so the
// matrix check is not satisfied by construction.
func TestDistinctOwnershipReportsHasARedControl(t *testing.T) {
	r := Compile(`effect fn leak() -> string raises { IoError, Timeout } uses { Files, Scheduler } {
    let f = run Files.openRead("x").provide<Files>(LiveFiles).timeout(5000)
    let g = run Files.openRead("y").provide<Files>(LiveFiles).timeout(5000)
    let a = run Files.readText(f)
    run Files.readText(g)
}
effect fn main() -> void {
    void
}`)
	reports := r.projector.ownershipReports
	if len(reports) != 2 || len(distinctOwnershipReports(reports)) != 0 {
		t.Fatalf("want two distinct reports, got %+v", reports)
	}
	if repeated := distinctOwnershipReports([]ownershipReport{reports[0], reports[1], reports[0]}); len(repeated) != 1 || repeated[0] != 2 {
		t.Fatalf("a report repeating earlier roots must be flagged, got %v", repeated)
	}
	if repeated := distinctOwnershipReports([]ownershipReport{reports[0], reports[0]}); len(repeated) != 1 {
		t.Fatalf("a duplicated report must be flagged, got %v", repeated)
	}
}

// TestNamedHandlerPendingIsInstantiated pins design §5.5: a recovered recipe's
// pending is L.pending joined with the instantiated pending of the named
// handler. A handler which forks a failing child is admitted where the
// enclosing function declares the child's failure
// (testdata/ownership_sweep/i2_handler_declared.ef executes safe), refused by
// the ordinary undeclared-failure rule where nothing declares it
// (i1_handler), and refused as a composite cause where its child meets
// another failure (hh1, hh2) or with the recovered recipe's own child (hh3,
// hh4). Any other use of the function as a callable value keeps the contract
// guard (h2_effcallable).
func TestNamedHandlerPendingIsInstantiated(t *testing.T) {
	compile := func(name string) *Result {
		source, err := os.ReadFile(filepath.Join("testdata", "ownership_sweep", name))
		if err != nil {
			t.Fatal(err)
		}
		return Compile(string(source))
	}
	if r := compile("i2_handler_declared.ef"); !r.Checked || len(r.Diagnostics) != 0 {
		t.Fatalf("a declared handler child must be admitted, got checked=%v %+v", r.Checked, r.Diagnostics)
	}
	r := compile("i1_handler.ef")
	if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF107" || r.Diagnostics[0].Message != "undeclared failures: Boom" {
		t.Fatalf("i1_handler must be refused by the undeclared failure alone, got checked=%v %+v", r.Checked, r.Diagnostics)
	}
	for _, name := range []string{"hh1_handler_then_fail.ef", "hh2_handler_twice.ef", "hh3_handler_child_alias.ef", "hh4_nested_recovery.ef"} {
		r := compile(name)
		var composite bool
		for _, d := range r.Diagnostics {
			composite = composite || (d.Code == "EF107" && strings.Contains(d.Message, "composite cause"))
		}
		if r.Checked || !composite {
			t.Fatalf("%s must be refused as a composite cause, got checked=%v %+v", name, r.Checked, r.Diagnostics)
		}
	}
	r = compile("h2_effcallable.ef")
	var guard bool
	for _, d := range r.Diagnostics {
		guard = guard || strings.Contains(d.Message, "stage may leave forked child failures unobserved")
	}
	if r.Checked || !guard {
		t.Fatalf("a pending function passed as an ordinary callable must keep the guard, got checked=%v %+v", r.Checked, r.Diagnostics)
	}
}
