package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	jsEffectImportLine = regexp.MustCompile(`(?m)^import \{ ([A-Za-z, ]+) \} from 'effect';$`)
	jsEffectUse        = regexp.MustCompile(`\b(Context|Clock|Duration|Effect|Fiber|Scope|Scheduler|Exit|Cause|Option|Queue)\.`)
	jsGeneratedName    = regexp.MustCompile(`__ef_[A-Za-z0-9_]+`)
	jsGeneratedBinding = regexp.MustCompile(`\b(?:const|let|class) (__ef_[A-Za-z0-9_]+)`)
	jsValueExport      = regexp.MustCompile(`export \{ ?[A-Za-z0-9_]+ as ([A-Za-z0-9_]+) ?\};`)
)

// jsClosureDefects is a static oracle over one emitted module: it imports
// exactly the `effect` names it uses, and every generated name it references
// is bound in the module. Parameters (__ef_local_, __ef_capture_) are bound
// by their functions.
func jsClosureDefects(js string) []string {
	defects := []string{}
	imported := []string{}
	for _, match := range jsEffectImportLine.FindAllStringSubmatch(js, -1) {
		imported = append(imported, strings.Split(match[1], ", ")...)
	}
	body := jsEffectImportLine.ReplaceAllString(js, "")
	used := []string{}
	for _, match := range jsEffectUse.FindAllStringSubmatch(body, -1) {
		if !slices.Contains(used, match[1]) {
			used = append(used, match[1])
		}
	}
	slices.Sort(imported)
	slices.Sort(used)
	if !slices.Equal(imported, used) {
		defects = append(defects, "effect imports "+strings.Join(imported, ",")+" but uses "+strings.Join(used, ","))
	}
	bound := map[string]bool{}
	for _, match := range jsGeneratedBinding.FindAllStringSubmatch(js, -1) {
		bound[match[1]] = true
	}
	for _, name := range jsGeneratedName.FindAllString(js, -1) {
		if !bound[name] && !strings.HasPrefix(name, "__ef_local_") && !strings.HasPrefix(name, "__ef_capture_") {
			defects = append(defects, "unbound "+name)
			bound[name] = true
		}
	}
	return defects
}

func jsValueExports(text string) []string {
	names := []string{}
	for _, match := range jsValueExport.FindAllStringSubmatch(text, -1) {
		names = append(names, match[1])
	}
	slices.Sort(names)
	return names
}

func checkJSModule(t *testing.T, label, js, declarations string) {
	t.Helper()
	if defects := jsClosureDefects(js); len(defects) > 0 {
		t.Fatalf("%s: %s\n%s", label, strings.Join(defects, "; "), js)
	}
	if exports, declared := jsValueExports(js), jsValueExports(declarations); !slices.Equal(exports, declared) {
		t.Fatalf("%s: JavaScript exports %v but declarations export %v", label, exports, declared)
	}
}

// writeJSModule places modules under dist/ so Node resolves the pinned
// `effect` package from the repository's node_modules.
func writeJSModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := filepath.Join("..", "..")
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Join(root, "dist"), "js-emission-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runNode(t *testing.T, dir, file string) (string, error) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node is required for JavaScript emission tests")
	}
	// dist/ may be a symbolic link. Name the module by its path through the
	// link so Node resolves `effect` from this repository's node_modules.
	main, err := filepath.Abs(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(node, "--preserve-symlinks", "--preserve-symlinks-main", main)
	output, err := command.CombinedOutput()
	return string(output), err
}

// Every JavaScript-capable authored example emits entry, library and test
// modules that close over their own prelude and export exactly what their
// declarations export.
func TestJSModulesCloseOverTheirSelectedPrelude(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.ef")
	if err != nil {
		t.Fatal(err)
	}
	emitted := 0
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		r := CompileAt(string(source), "js", "../../examples")
		if !r.Checked || r.Program.GoOnly {
			continue
		}
		name := filepath.Base(path)
		js, declarations, err := r.Emit(false)
		if err != nil {
			t.Fatal(name, err)
		}
		checkJSModule(t, name+" library", js, declarations)
		emitted++
		if r.Entry() == nil {
			js, declarations, err := r.Emit(true)
			if err != nil {
				t.Fatal(name, err)
			}
			checkJSModule(t, name+" entry", js, declarations)
		}
		if _, err := r.Tests(); err == nil {
			js, declarations, err := r.EmitJSTests()
			if err != nil {
				t.Fatal(name, err)
			}
			checkJSModule(t, name+" tests", js, declarations)
		}
	}
	if emitted == 0 {
		t.Fatal("no JavaScript-capable examples")
	}
}

// Pruning keeps behavior: each JavaScript-capable example's entry prints
// what the same program prints with the complete prelude.
func TestJSEntryBehavesLikeTheCompletePrelude(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.ef")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// A transport example serves until interrupted; scripts/
		// http_transport_smoke.py drives it over sockets instead.
		if filepath.Base(path) == "http-transport.ef" {
			continue
		}
		r := CompileAt(string(source), "js", "../../examples")
		if !r.Checked || r.Program.GoOnly || r.Entry() != nil {
			continue
		}
		entry, _, err := r.Emit(true)
		if err != nil {
			t.Fatal(err)
		}
		library, _, err := r.Emit(false)
		if err != nil {
			t.Fatal(err)
		}
		complete := withCompletePrelude(library)
		runner := entry[strings.LastIndex(entry, "const __ef_signal = new AbortController()"):]
		dir := writeJSModule(t, map[string]string{"entry.mjs": entry, "complete.mjs": complete + runner})
		pruned, prunedErr := runNode(t, dir, "entry.mjs")
		full, fullErr := runNode(t, dir, "complete.mjs")
		if (prunedErr == nil) != (fullErr == nil) || pruned != full {
			t.Fatalf("%s: pruned entry diverged\npruned (%v):\n%s\ncomplete (%v):\n%s", filepath.Base(path), prunedErr, pruned, fullErr, full)
		}
		if len(entry) >= len(complete+runner) {
			t.Fatalf("%s: entry retained the complete prelude", filepath.Base(path))
		}
	}
}

const jsSurfaceSource = `error Late
service Greeter {
    effect fn greet(name: string) -> string
}
service Ledger {
    effect fn total() -> i64
}
impl Loud for Greeter {
    effect fn greet(name: string) -> string {
        "HELLO " + name
    }
}
impl Fixed for Ledger {
    effect fn total() -> i64 {
        7
    }
}
record Receipt {
    total: i64
}
fn shout(name: string) -> string {
    name + "!"
}
effect fn quick() -> string {
    "quick"
}
effect fn slow() -> string uses { Clock } {
    run Clock.sleep(10000);
    "late"
}
effect fn audit() -> Receipt raises { Late } uses { Ledger, Clock, Scheduler } {
    scope {
        let child = fork quick()
        let text = run child.join()
        let deadline = run slow().timeout(1).catch<Timeout>("timed out")
        let total = run Ledger.total()
        Receipt { total: total }
    }
}
effect fn main() -> void {
    let text = run Greeter.greet(shout("ada")).provide<Greeter>(Loud)
    run Console.log(text).provide<Console>(Stdout)
}
`

// The entry lowers only the effect main's application plan: an unused
// service, provider, function, helper or import never appears, and the
// module runs under Node.
func TestJSEntryLowersOnlyItsApplicationPlan(t *testing.T) {
	r := CompileFor(jsSurfaceSource, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	plan, err := r.ApplicationPlan(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	entry, declarations, err := r.Emit(true)
	if err != nil {
		t.Fatal(err)
	}
	checkJSModule(t, "entry", entry, declarations)
	if !strings.Contains(entry, "import { Context, Effect, Fiber, Scope, Exit, Cause, Option } from 'effect';\n") {
		t.Fatalf("entry imports are not the closure of its lowering:\n%s", entry)
	}
	for _, absent := range []string{"audit", "quick", "slow", "Fixed", "__ef_service_Ledger", "__ef_fork", "__ef_join", "__ef_timeout", "__ef_catch", "__ef_provideLayer", "__ef_provider_LiveClock", "__ef_provider_TestSync", "__ef_latch", "__ef_test_harness"} {
		if strings.Contains(entry, absent) || strings.Contains(declarations, "declare const "+absent) {
			t.Fatalf("entry retained %s outside its plan:\n%s", absent, entry)
		}
	}
	for _, f := range r.Program.Functions {
		if exported := strings.Contains(entry, "export { "+f.jsEmissionName()+" as "+f.Name+" };"); exported != plan.Requires(RequiresFunction, f.Identity) {
			t.Fatalf("entry export of %s disagrees with its plan", f.Name)
		}
	}
	// Type-only declarations stay complete, so retained signatures and
	// consumers may still name any module type.
	for _, kept := range []string{"export interface Receipt", "export interface LateError", "export interface LedgerRequirement", "export type LedgerProvider"} {
		if !strings.Contains(declarations, kept) {
			t.Fatalf("entry declarations dropped type-only %q", kept)
		}
	}
	dir := writeJSModule(t, map[string]string{"entry.mjs": entry})
	if output, err := runNode(t, dir, "entry.mjs"); err != nil || output != "HELLO ada!\n" {
		t.Fatalf("entry run: %v\n%s", err, output)
	}
}

// A library retains every export with its helpers, and nothing an export
// does not reference. A consumer runs it under Node and type-checks it under
// strict TypeScript.
func TestJSLibraryRetainsEveryExport(t *testing.T) {
	r := CompileFor(jsSurfaceSource, "js")
	library, declarations, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	checkJSModule(t, "library", library, declarations)
	for _, name := range []string{"shout", "quick", "slow", "audit", "main", "Loud", "Fixed", "Greeter", "Ledger", "Console", "Stdout", "TestClock"} {
		if !slices.Contains(jsValueExports(library), name) {
			t.Fatalf("library does not export %s:\n%s", name, library)
		}
	}
	for _, present := range []string{"const __ef_fork", "const __ef_join", "const __ef_timeout", "const __ef_catch", "const __ef_call"} {
		if !strings.Contains(library, present) {
			t.Fatalf("library dropped %s referenced by an export", present)
		}
	}
	for _, absent := range []string{"__ef_provideLayer", "__ef_interrupt", "__ef_makeTestHarness", "Queue", "Duration"} {
		if strings.Contains(library, absent) {
			t.Fatalf("library retained unreferenced %s", absent)
		}
	}
	consumer := `import { shout, audit, Fixed, Ledger } from "./library.mjs";
import { Effect } from "effect";
const receipt = await Effect.runPromise(audit().pipe(Effect.provideService(Ledger, Fixed)));
console.log(shout("lib"), receipt.total);
`
	dir := writeJSModule(t, map[string]string{"library.mjs": library, "consumer.mjs": consumer})
	if output, err := runNode(t, dir, "consumer.mjs"); err == nil || !strings.Contains(output, "Clock") {
		// audit needs Clock and Scheduler, so the unprovided run must fail
		// on a missing service rather than on an unbound helper.
		t.Fatalf("library consumer without Clock: %v\n%s", err, output)
	}
	consumer = `import { shout, audit, Fixed, Ledger, Clock, LiveClock, Scheduler, LiveScheduler } from "./library.mjs";
import { Effect } from "effect";
const program = audit().pipe(Effect.provideService(Ledger, Fixed), Effect.provideService(Clock, LiveClock), Effect.provideService(Scheduler, LiveScheduler));
const receipt = await Effect.runPromise(program);
console.log(shout("lib"), String(receipt.total));
`
	dir = writeJSModule(t, map[string]string{"library.mjs": library, "consumer.mjs": consumer})
	if output, err := runNode(t, dir, "consumer.mjs"); err != nil || output != "lib! 7\n" {
		t.Fatalf("library consumer: %v\n%s", err, output)
	}
	checkStrictTypeScript(t, declarations, `import { shout, audit, Fixed } from "./generated.mjs";
import type { Receipt, LateError, LedgerProvider } from "./generated.mjs";
const name: string = shout("ts");
const provider: LedgerProvider = Fixed;
const receipt = audit();
type Success = typeof receipt extends import("effect").Effect.Effect<infer A, infer E, infer _R> ? [A, E] : never;
const proof: Success extends [Receipt, LateError] ? true : false = true;
// @ts-expect-error a structural lookalike lacks the nominal brand.
const forged: Receipt = { total: 1n };
void name; void provider; void proof; void forged;
`)
	_, entryDeclarations, err := r.Emit(true)
	if err != nil {
		t.Fatal(err)
	}
	checkStrictTypeScript(t, entryDeclarations, `import { main, shout } from "./generated.mjs";
import type { Receipt } from "./generated.mjs";
const run: import("effect").Effect.Effect<void, never, never> = main();
const name: string = shout("ts");
// @ts-expect-error a pruned value is not exported by the entry.
import { audit } from "./generated.mjs";
// @ts-expect-error a structural lookalike lacks the nominal brand.
const forged: Receipt = { total: 1n };
void run; void name; void audit; void forged;
`)
}

// JavaScript has no foreign module bindings. A Go import refuses JavaScript
// output even when the entry never reaches it, so no host initialization
// is dropped by pruning.
func TestJSRefusesForeignImportsWhetherOrNotReachable(t *testing.T) {
	source := `import go strings "strings"

effect fn main() -> void {
    run Console.log("ok").provide<Console>(Stdout)
}
`
	if r := CompileFor(source, "js"); r.Checked || !slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool { return d.Code == "EF110" }) {
		t.Fatalf("JavaScript target accepted an unused Go import: %+v", r.Diagnostics)
	}
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	for _, entry := range []bool{true, false} {
		if _, _, err := r.Emit(entry); err == nil || !strings.Contains(err.Error(), "only for Go") {
			t.Fatalf("Emit(%v) of a foreign-importing program: %v", entry, err)
		}
	}
}

// A deadline forks its work internally, so an entry that times out without
// forking anything itself still selects the fork helper through the timeout
// chunk's declared edge.
func TestJSEntryClosesHelperEdgesItDoesNotNameItself(t *testing.T) {
	r := CompileFor(`effect fn slow() -> string uses { Clock } {
    run Clock.sleep(10000);
    "late"
}
effect fn main() -> void {
    let text = run slow().timeout(1).catch<Timeout>("timed out")
        .provide<Clock>(LiveClock).provide<Scheduler>(LiveScheduler)
    run Console.log(text).provide<Console>(Stdout)
}
`, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	plan, err := r.ApplicationPlan(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Requires(RequiresHelper, "fork") || !plan.Requires(RequiresHelper, "timeout") {
		t.Fatalf("fixture must time out without forking: %v", plan.Identities(RequiresHelper))
	}
	entry, declarations, err := r.Emit(true)
	if err != nil {
		t.Fatal(err)
	}
	checkJSModule(t, "timeout entry", entry, declarations)
	dir := writeJSModule(t, map[string]string{"entry.mjs": entry})
	if output, err := runNode(t, dir, "entry.mjs"); err != nil || output != "timed out\n" {
		t.Fatalf("timeout entry run: %v\n%s", err, output)
	}
}
