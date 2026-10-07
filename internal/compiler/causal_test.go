package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExplicitTestProvidersUseTheHarnessAcrossTargets(t *testing.T) {
	source := `effect fn explicitSleep() -> void uses {Clock} {
    run Clock.sleep(36000000)
}

effect fn test_explicit_virtual_providers() -> void raises {AssertionFailed} uses {Assert, Scheduler} {
    let child = fork explicitSleep().provide<Clock>(TestClock).provide<Scheduler>(TestScheduler)
    run Scheduler.awaitRegistration()
    run Scheduler.advance(36000000)
    run child.join()
    run Assert.check(true, "explicit test providers share the harness scheduler")
}

effect fn test_advance_admits_unstarted_fork() -> void raises {AssertionFailed} uses {Assert, Scheduler} {
    let child = fork explicitSleep().provide<Clock>(TestClock).provide<Scheduler>(TestScheduler)
    run Scheduler.advance(36000000)
    run child.join()
    run Assert.check(true, "adjust waits for an admitted fork to register its timer")
}
`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	goOutput, err := runWithWatchdog(buildGeneratedGo(t, r, GoGenerationTest))
	if err != nil || !strings.Contains(string(goOutput), `"passed":true`) {
		t.Fatalf("generated Go explicit test providers: %v\n%s", err, goOutput)
	}

	jsSource, _, err := r.EmitJSTests()
	if err != nil {
		t.Fatal(err)
	}
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for generated scheduler tests")
	}
	jsOutput, err := runWithWatchdog(bun, writeGeneratedJS(t, "explicit-scheduler-", jsSource))
	if err != nil || !strings.Contains(string(jsOutput), `"passed":true`) {
		t.Fatalf("generated JS explicit test providers: %v\n%s", err, jsOutput)
	}
}

func TestExplicitTestProvidersRejectLiveExecutionWithoutHarness(t *testing.T) {
	source := `effect fn main() -> void {
    run Clock.sleep(1).provide<Clock>(TestClock)
}
`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	goOutput, err := runWithWatchdog(buildGeneratedGo(t, r, GoGenerationBuild))
	if err == nil || !strings.Contains(string(goOutput), "test clock requires an active test scheduler") {
		t.Fatalf("Go TestClock escaped its harness: %v\n%s", err, goOutput)
	}
	jsOutput := runJS(t, source, `const exit=await Effect.runPromiseExit(__ef_function_main());if(exit._tag!=="Failure"||!exit.cause.reasons.some(reason=>reason._tag==="Die"&&String(reason.defect).includes("test clock requires the ef test harness")))throw new Error("JS TestClock escaped its harness "+JSON.stringify(exit));`)
	if jsOutput != "" {
		t.Fatalf("JS TestClock escaped its harness: %s", jsOutput)
	}
}

// jsHarnessYieldBudget is the per-turn operation budget of the JS test
// harness's Effect scheduler (prelude/test-harness.mjs shouldYield). A managed
// fiber that exceeds it is preempted and requeued on the harness dispatcher.
const jsHarnessYieldBudget = 2048

// jsYieldBudget finds the harness's yield comparison independently of the
// names around it.
var jsYieldBudget = regexp.MustCompile(`\.currentOpCount>=(\d+)`)

// A continuation is long when it outlives one scheduler turn, and one
// adjustment must drain it before committing its target.
//   - JS: the fiber crosses the harness yield budget and is requeued
//     mid-continuation; adjustment must run the requeued work. Every run spends
//     at least one fiber operation, so the budget is crossed under any lowering.
//   - Go: a managed fiber keeps its turn while it runs; adjustment must wait out
//     that active window rather than race it. With the wait removed, 4096
//     signals still passed in about one run in three; 65536 failed all 60.
//
// The signals are spread over small nested functions. As one straight-line
// body, 1024 signals already took the Go build about a minute.
func TestSchedulerDrainsLongManagedContinuationAcrossTargets(t *testing.T) {
	const fanOut = 16
	source := `effect fn signal16(latch: Latch) -> void uses {Sync} {
` + strings.Repeat("    run Sync.signal(latch)\n", fanOut) + `}

effect fn signal256(latch: Latch) -> void uses {Sync} {
` + strings.Repeat("    run signal16(latch)\n", fanOut) + `}

effect fn signal4096(latch: Latch) -> void uses {Sync} {
` + strings.Repeat("    run signal256(latch)\n", fanOut) + `}

effect fn signal65536(latch: Latch) -> void uses {Sync} {
` + strings.Repeat("    run signal4096(latch)\n", fanOut) + `}

effect fn longContinuation(latch: Latch) -> void uses {Clock, Sync} {
    run Clock.sleep(20)
    run signal65536(latch)
    run Clock.sleep(30)
}

effect fn test_scheduler_drains_long_continuation() -> void raises {AssertionFailed} uses {Assert, Clock, Scheduler, Sync} {
    let latch = run Sync.latch()
    let child = fork longContinuation(latch)
    run Scheduler.awaitRegistration()
    run Scheduler.advance(50)
    run Sync.await(latch)
    run child.join()
    run Assert.check(true, "one adjustment drains a long managed continuation")
}
`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	goOutput, err := runWithWatchdog(buildGeneratedGo(t, r, GoGenerationTest))
	if err != nil || !strings.Contains(string(goOutput), `"passed":true`) {
		t.Fatalf("generated Go scheduler test: %v\n%s", err, goOutput)
	}

	jsSource, _, err := r.EmitJSTests()
	if err != nil {
		t.Fatal(err)
	}
	if budgets := jsYieldBudget.FindAllStringSubmatch(jsSource, -1); len(budgets) != 1 || budgets[0][1] != strconv.Itoa(jsHarnessYieldBudget) {
		t.Fatalf("generated JS tests no longer yield at %d operations (found %q); re-derive the continuation length", jsHarnessYieldBudget, budgets)
	}
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for generated scheduler tests")
	}
	jsOutput, err := runWithWatchdog(bun, writeGeneratedJS(t, "causal-scheduler-", jsSource))
	if err != nil || !strings.Contains(string(jsOutput), `"passed":true`) {
		t.Fatalf("generated JS scheduler test: %v\n%s", err, jsOutput)
	}
}

func TestSchedulerAuthorityIsProviderDrivenAcrossTargets(t *testing.T) {
	source := `impl Immediate for Scheduler {
    effect fn sleep(milliseconds: i64) -> void { void }
    effect fn advance(milliseconds: i64) -> void { void }
    effect fn awaitRegistration() -> void { void }
}
effect fn work() -> string uses {Clock} {
    run Clock.sleep(20)
    "completed"
}
effect fn main() -> string {
    run work().timeout(500).catch<Timeout>("timed out")
        .provide<Clock>(LiveClock).provide<Scheduler>(Immediate)
}
	`
	for name, variant := range map[string]string{
		"scheduler-after-clock":  source,
		"scheduler-before-clock": strings.Replace(source, ".provide<Clock>(LiveClock).provide<Scheduler>(Immediate)", ".provide<Scheduler>(Immediate).provide<Clock>(LiveClock)", 1),
	} {
		t.Run(name, func(t *testing.T) {
			r := CompileFor(variant, "go")
			if !r.Checked {
				t.Fatal(r.Diagnostics)
			}
			goOutput, err := runWithWatchdog(buildGeneratedGo(t, r, GoGenerationBuild))
			if err != nil || string(goOutput) != "timed out\n" {
				t.Fatalf("generated Go custom scheduler: %v\n%s", err, goOutput)
			}
			jsOutput := runJS(t, variant, `if (await Effect.runPromise(__ef_function_main()) !== "timed out") throw new Error("custom scheduler was ignored");`)
			if jsOutput != "" {
				t.Fatalf("generated JS custom scheduler: %s", jsOutput)
			}
		})
	}
}

func TestSchedulerTimerFailureIsPreservedAcrossTargets(t *testing.T) {
	source := `impl Broken for Scheduler {
    effect fn sleep(milliseconds: i64) -> void { run Clock.sleep(2147483648).provide<Clock>(LiveClock) }
    effect fn advance(milliseconds: i64) -> void { void }
    effect fn awaitRegistration() -> void { void }
}
effect fn work() -> string uses {Clock} {
    run Clock.sleep(20)
    "completed"
}
effect fn main() -> string {
    run work().timeout(500).catch<Timeout>("timed out")
        .provide<Clock>(LiveClock).provide<Scheduler>(Broken)
}
`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	goOutput, err := runWithWatchdog(buildGeneratedGo(t, r, GoGenerationBuild))
	if err == nil || !strings.Contains(string(goOutput), "invalid millisecond duration") || strings.Contains(string(goOutput), "timed out") {
		t.Fatalf("generated Go scheduler defect was rewritten: %v\n%s", err, goOutput)
	}
	jsOutput := runJS(t, source, `const exit=await Effect.runPromiseExit(__ef_function_main());if(exit._tag!=="Failure"||!exit.cause.reasons.some(reason=>reason._tag==="Die")||exit.cause.reasons.some(reason=>reason._tag==="Fail"&&reason.error?._tag==="Timeout"))throw new Error("scheduler defect was rewritten "+JSON.stringify(exit));
let timerStarted;const started=new Promise(resolve=>timerStarted=resolve);const cleanupDefect={sleep:()=>Effect.acquireUseRelease(Effect.sync(()=>timerStarted()),()=>Effect.never,()=>Effect.die(new Error("timer cleanup defect"))),advance:()=>Effect.die(new Error("unused")),awaitRegistration:()=>Effect.die(new Error("unused"))};const work=Effect.promise(()=>started.then(()=>"completed"));const workWinner=await Effect.runPromiseExit(Effect.provideService(__ef_timeout(work,500),__ef_service_Scheduler,cleanupDefect));if(workWinner._tag!=="Failure"||!workWinner.cause.reasons.some(reason=>reason._tag==="Die"&&String(reason.defect).includes("timer cleanup defect")))throw new Error("timer cleanup defect was discarded "+JSON.stringify(workWinner));`)
	if jsOutput != "" {
		t.Fatalf("generated JS scheduler defect: %s", jsOutput)
	}
}

// generatedRunWatchdog bounds one execution of an already-built generated
// program. A healthy program exits within milliseconds on Go and within about
// a second on Bun, module loading included, even on a saturated host; a
// deadlocked one never exits. The bound only separates the two, so no
// compilation runs under it.
const generatedRunWatchdog = 15 * time.Second

// buildGeneratedGo writes r's Go application into a fresh module and builds
// its executable.
func buildGeneratedGo(t *testing.T, r *Result, mode GoGenerationMode) string {
	t.Helper()
	goSource, application, err := emitGoApplication(r, mode)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{"go.mod": r.ModuleFile(), "main.go": []byte(goSource)} {
		if err = os.WriteFile(filepath.Join(dir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return buildGoModule(t, dir)
}

// buildGoModule builds the generated module in dir and returns its executable.
// The build is bounded by the go test timeout rather than the run watchdog:
// its duration belongs to the host and the Go toolchain, not to the behavior
// the run observes.
func buildGoModule(t *testing.T, dir string) string {
	t.Helper()
	binary := filepath.Join(dir, "program")
	if output, err := runGoCommand(dir, "build", "-o", binary, "."); err != nil {
		t.Fatalf("generated Go did not build: %v\n%s", err, output)
	}
	return binary
}

// writeGeneratedJS places a generated test module under dist/, where Bun
// resolves the pinned effect package.
func writeGeneratedJS(t *testing.T, prefix, source string) string {
	t.Helper()
	root := filepath.Join("..", "..")
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Join(root, "dist"), prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "scheduler.tests.mjs")
	if err = os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runWithWatchdog(command string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), generatedRunWatchdog)
	defer cancel()
	output, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
}
