package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExplicitTestProvidersUseTheHarnessAcrossTargets(t *testing.T) {
	source := `effect fn explicitSleep() -> () uses {Clock} {
    run Clock.sleep(20)
}

effect fn test_explicit_virtual_providers() -> () raises {AssertionFailed} uses {Assert, Scheduler} {
    let child = fork explicitSleep().provide<Clock>(TestClock).provide<Scheduler>(TestScheduler)
    run Scheduler.awaitRegistration()
    run Scheduler.advance(20)
    run child.join()
    run Assert.check(true, "explicit test providers share the harness scheduler")
}

effect fn test_advance_admits_unstarted_fork() -> () raises {AssertionFailed} uses {Assert, Scheduler} {
    let child = fork explicitSleep().provide<Clock>(TestClock).provide<Scheduler>(TestScheduler)
    run Scheduler.advance(20)
    run child.join()
    run Assert.check(true, "adjust waits for an admitted fork to register its timer")
}
`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	goSource, err := r.EmitGoTests()
	if err != nil {
		t.Fatal(err)
	}
	goDir := t.TempDir()
	if err = WriteRuntime(goDir); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{
		"go.mod":  []byte(r.ModuleFile()),
		"main.go": []byte(goSource),
	} {
		if err = os.WriteFile(filepath.Join(goDir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	goOutput, err := runWithWatchdog(goDir, 15*time.Second, "go", "run", ".")
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
	root := filepath.Join("..", "..")
	if err = os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	jsDir, err := os.MkdirTemp(filepath.Join(root, "dist"), "explicit-scheduler-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(jsDir) })
	jsPath := filepath.Join(jsDir, "scheduler.tests.mjs")
	if err = os.WriteFile(jsPath, []byte(jsSource), 0600); err != nil {
		t.Fatal(err)
	}
	jsOutput, err := runWithWatchdog(jsDir, 15*time.Second, bun, jsPath)
	if err != nil || !strings.Contains(string(jsOutput), `"passed":true`) {
		t.Fatalf("generated JS explicit test providers: %v\n%s", err, jsOutput)
	}
}

func TestExplicitTestProvidersRejectLiveExecutionWithoutHarness(t *testing.T) {
	source := `effect fn main() -> () {
    run Clock.sleep(1).provide<Clock>(TestClock)
}
`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	goSource, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	goDir := t.TempDir()
	if err = WriteRuntime(goDir); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{
		"go.mod":  []byte(r.ModuleFile()),
		"main.go": []byte(goSource),
	} {
		if err = os.WriteFile(filepath.Join(goDir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	goOutput, err := runWithWatchdog(goDir, 15*time.Second, "go", "run", ".")
	if err == nil || !strings.Contains(string(goOutput), "test clock requires an active test scheduler") {
		t.Fatalf("Go TestClock escaped its harness: %v\n%s", err, goOutput)
	}
	jsOutput := runJS(t, source, `const exit=await Effect.runPromiseExit(__ef_function_main());if(exit._tag!=="Failure"||!exit.cause.reasons.some(reason=>reason._tag==="Die"&&String(reason.defect).includes("test clock requires the ef test harness")))throw new Error("JS TestClock escaped its harness "+JSON.stringify(exit));`)
	if jsOutput != "" {
		t.Fatalf("JS TestClock escaped its harness: %s", jsOutput)
	}
}

// Keep the generated continuation large enough to exercise scheduler handoffs
// while bounding Go 1.27's compile time for one very large straight-line body.
func TestSchedulerDrainsLongManagedContinuationAcrossTargets(t *testing.T) {
	source := `effect fn longContinuation(latch: Latch) -> () uses {Clock, Sync} {
    run Clock.sleep(20)
` + strings.Repeat("    run Sync.signal(latch)\n", 1024) + `    run Clock.sleep(30)
}

effect fn test_scheduler_drains_long_continuation() -> () raises {AssertionFailed} uses {Assert, Clock, Scheduler, Sync} {
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
	goSource, err := r.EmitGoTests()
	if err != nil {
		t.Fatal(err)
	}
	goDir := t.TempDir()
	if err = WriteRuntime(goDir); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{
		"go.mod":  []byte(r.ModuleFile()),
		"main.go": []byte(goSource),
	} {
		if err = os.WriteFile(filepath.Join(goDir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	goOutput, err := runWithWatchdog(goDir, 60*time.Second, "go", "run", ".")
	if err != nil || !strings.Contains(string(goOutput), `"passed":true`) {
		t.Fatalf("generated Go scheduler test: %v\n%s", err, goOutput)
	}

	jsSource, _, err := r.EmitJSTests()
	if err != nil {
		t.Fatal(err)
	}
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for generated scheduler tests")
	}
	root := filepath.Join("..", "..")
	if err = os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	jsDir, err := os.MkdirTemp(filepath.Join(root, "dist"), "causal-scheduler-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(jsDir) })
	jsPath := filepath.Join(jsDir, "scheduler.tests.mjs")
	if err = os.WriteFile(jsPath, []byte(jsSource), 0600); err != nil {
		t.Fatal(err)
	}
	jsOutput, err := runWithWatchdog(jsDir, 30*time.Second, bun, jsPath)
	if err != nil || !strings.Contains(string(jsOutput), `"passed":true`) {
		t.Fatalf("generated JS scheduler test: %v\n%s", err, jsOutput)
	}
}

func TestSchedulerAuthorityIsProviderDrivenAcrossTargets(t *testing.T) {
	source := `impl Immediate for Scheduler {
    effect fn sleep(milliseconds: i64) -> () { () }
    effect fn advance(milliseconds: i64) -> () { () }
    effect fn awaitRegistration() -> () { () }
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
			goSource, err := r.EmitGo()
			if err != nil {
				t.Fatal(err)
			}
			goDir := t.TempDir()
			if err = WriteRuntime(goDir); err != nil {
				t.Fatal(err)
			}
			for fileName, contents := range map[string][]byte{"go.mod": []byte(r.ModuleFile()), "main.go": []byte(goSource)} {
				if err = os.WriteFile(filepath.Join(goDir, fileName), contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			goOutput, err := runWithWatchdog(goDir, 10*time.Second, "go", "run", ".")
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
    effect fn sleep(milliseconds: i64) -> () { run Clock.sleep(2147483648).provide<Clock>(LiveClock) }
    effect fn advance(milliseconds: i64) -> () { () }
    effect fn awaitRegistration() -> () { () }
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
	goSource, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	goDir := t.TempDir()
	if err = WriteRuntime(goDir); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{"go.mod": []byte(r.ModuleFile()), "main.go": []byte(goSource)} {
		if err = os.WriteFile(filepath.Join(goDir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	goOutput, err := runWithWatchdog(goDir, 10*time.Second, "go", "run", ".")
	if err == nil || !strings.Contains(string(goOutput), "invalid millisecond duration") || strings.Contains(string(goOutput), "timed out") {
		t.Fatalf("generated Go scheduler defect was rewritten: %v\n%s", err, goOutput)
	}
	jsOutput := runJS(t, source, `const exit=await Effect.runPromiseExit(__ef_function_main());if(exit._tag!=="Failure"||!exit.cause.reasons.some(reason=>reason._tag==="Die")||exit.cause.reasons.some(reason=>reason._tag==="Fail"&&reason.error?._tag==="Timeout"))throw new Error("scheduler defect was rewritten "+JSON.stringify(exit));
let timerStarted;const started=new Promise(resolve=>timerStarted=resolve);const cleanupDefect={sleep:()=>Effect.acquireUseRelease(Effect.sync(()=>timerStarted()),()=>Effect.never,()=>Effect.die(new Error("timer cleanup defect"))),advance:()=>Effect.die(new Error("unused")),awaitRegistration:()=>Effect.die(new Error("unused"))};const work=Effect.promise(()=>started.then(()=>"completed"));const workWinner=await Effect.runPromiseExit(Effect.provideService(__ef_timeout(work,500),__ef_service_Scheduler,cleanupDefect));if(workWinner._tag!=="Failure"||!workWinner.cause.reasons.some(reason=>reason._tag==="Die"&&String(reason.defect).includes("timer cleanup defect")))throw new Error("timer cleanup defect was discarded "+JSON.stringify(workWinner));`)
	if jsOutput != "" {
		t.Fatalf("generated JS scheduler defect: %s", jsOutput)
	}
}

func runWithWatchdog(dir string, timeout time.Duration, command string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
}
