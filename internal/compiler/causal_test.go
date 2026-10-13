package compiler

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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

// The scheduler examples run on both targets, and every discovered case must
// pass on each, in source order.
//
// Scheduler.awaitRegistration has one meaning on both targets: it returns at
// quiescence, when no managed fiber is running or runnable. The interrupt
// after a barrier must find the work already complete, so a barrier that
// returned early fails the join, and a barrier waiting for a new registration
// hangs until the watchdog. The required cases cover:
//   - quiescence.ef, work that registers no new timer: the #80 repro, a latch
//     signalled before the fork, a pure fork, a completion that cancels its
//     timeout timer, a nested fork, and mixed sibling forks;
//   - quiescence-wakes.ef, wakes in flight: wakes by cancellation, which carry
//     no wake reservation on Go (an interrupted sleeper or latch waiter and a
//     closed scope), and a signalled waiter that has not run yet (readiness
//     then registration, with and without the barrier before advance, and a
//     resumed waiter that resumes another and advances); and interrupting a
//     fiber parked at the barrier, which must be an ordinary interruption,
//     never a defect;
//   - latch-handoff.ef, the latch handoff (#142): a signal makes its waiters
//     runnable and returns, the signaller keeps its turn, and each waiter
//     resumes on its own turn. Interrupting the signaller after its signal
//     cannot affect a waiter, and a signalled waiter interrupted before it
//     runs is interrupted. A waiter resumed inside its signaller's turn fails
//     the interrupt-after-signal cases.
//
// The cancellation cases are timing-dependent on Go; the receipts for #80
// record their failure rates without the fix at high counts.
func TestSchedulerExamplesAcrossTargets(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for generated scheduler tests")
	}
	examples := []struct {
		file     string
		required []string
	}{
		{file: "causal.ef"},
		{file: "quiescence.ef", required: []string{
			"test_barrier_after_immediately_completed_timeout",
			"test_barrier_after_latch_signalled_before_fork",
			"test_barrier_after_pure_fork_then_interrupt",
			"test_barrier_after_completion_cancels_timeout",
			"test_barrier_after_nested_fork",
			"test_barrier_settles_mixed_sibling_forks",
		}},
		{file: "quiescence-wakes.ef", required: []string{
			"test_barrier_after_interrupting_a_sleeper",
			"test_barrier_after_interrupting_a_latch_waiter",
			"test_barrier_after_scope_close",
			"test_barrier_after_readiness_then_advance",
			"test_advance_after_readiness",
			"test_barrier_after_nested_resumption",
			"test_interrupting_a_barrier_waiter",
		}},
		{file: "latch-handoff.ef", required: []string{
			"test_interrupting_the_signaller_after_signal",
			"test_signal_before_wait",
			"test_two_waiters",
			"test_interrupting_the_signaller_while_the_waiter_is_at_the_barrier",
			"test_signal_inside_a_scope_that_closes_after",
			"test_interrupting_a_signalled_waiter_before_it_runs",
			"test_scope_close_interrupts_a_signalled_waiter_before_it_runs",
		}},
	}
	for _, example := range examples {
		t.Run(example.file, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("..", "..", "examples", example.file))
			if err != nil {
				t.Fatal(err)
			}
			r := CompileAt(string(source), "go", filepath.Join("..", "..", "examples"))
			if !r.Checked {
				t.Fatal(r.Diagnostics)
			}
			tests, err := r.Tests()
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, test := range tests {
				names = append(names, test.Name)
			}
			for _, name := range example.required {
				if !slices.Contains(names, name) {
					t.Fatalf("examples/%s lost case %s (cases: %v)", example.file, name, names)
				}
			}
			jsSource, _, err := r.EmitJSTests()
			if err != nil {
				t.Fatal(err)
			}
			runs := map[string]func() ([]byte, error){
				"go": func() ([]byte, error) { return runWithWatchdog(buildGeneratedGo(t, r, GoGenerationTest)) },
				"js": func() ([]byte, error) {
					return runWithWatchdog(bun, writeGeneratedJS(t, "scheduler-example-", jsSource))
				},
			}
			for _, target := range []string{"go", "js"} {
				output, err := runs[target]()
				if err != nil {
					t.Fatalf("%s suite: %v\n%s", target, err, output)
				}
				lines := strings.Split(strings.TrimSpace(string(output)), "\n")
				var report struct {
					Passed bool `json:"passed"`
					Tests  []struct {
						Name   string `json:"name"`
						Passed bool   `json:"passed"`
					} `json:"tests"`
				}
				if err := json.Unmarshal([]byte(lines[len(lines)-1]), &report); err != nil {
					t.Fatalf("%s suite report: %v\n%s", target, err, output)
				}
				reported := []string{}
				for _, test := range report.Tests {
					if !test.Passed {
						t.Errorf("%s: %s failed\n%s", target, test.Name, output)
					}
					reported = append(reported, test.Name)
				}
				if !report.Passed || !slices.Equal(reported, names) {
					t.Fatalf("%s suite: passed=%v reported %v, want %v\n%s", target, report.Passed, reported, names, output)
				}
			}
		})
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
	if err == nil || !strings.Contains(string(goOutput), "test clock requires the ef test harness") {
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

// writeGeneratedJS places a generated test module where Bun resolves the
// pinned effect package.
func writeGeneratedJS(t *testing.T, prefix, source string) string {
	t.Helper()
	dir := jsModuleDir(t, prefix)
	path := filepath.Join(dir, "scheduler.tests.mjs")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
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
