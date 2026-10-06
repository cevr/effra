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

func TestSchedulerDrainsLongManagedContinuationAcrossTargets(t *testing.T) {
	source := `effect fn longContinuation(latch: Latch) -> () uses {Clock, Sync} {
    run Clock.sleep(20)
` + strings.Repeat("    run Sync.signal(latch)\n", 2050) + `    run Clock.sleep(30)
}

effect fn test_scheduler_drains_long_continuation() -> () throws {AssertionFailed} uses {Assert, Clock, Scheduler, Sync} {
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
