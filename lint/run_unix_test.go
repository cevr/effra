//go:build unix

package lint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Process-group lifecycle tests: they observe pack and descendant pids, so
// they build on Unix only. Portable protocol tests stay in run_test.go.

// processGone polls until pid no longer exists. A killed descendant is
// reparented and reaped by init; a pid that stays is an orphan.
func processGone(pid int) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
	}
	return false
}

// readPIDs waits for the pack and descendant pids the fixture recorded.
func readPIDs(t *testing.T, path string) (int, int) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fields := strings.Fields(string(data))
		pack, _ := strconv.Atoi(fields[0])
		child, _ := strconv.Atoi(fields[1])
		return pack, child
	}
	t.Fatal("fixture did not record its pids")
	return 0, 0
}

// waitForPIDs returns the recorded pids after checking both are alive, so a
// reaping test cannot pass vacuously.
func waitForPIDs(t *testing.T, path string) (int, int) {
	t.Helper()
	pack, child := readPIDs(t, path)
	if syscall.Kill(pack, 0) != nil || syscall.Kill(child, 0) != nil {
		t.Errorf("fixture processes %d/%d were not alive", pack, child)
	}
	return pack, child
}

func TestRunTimeoutKillsAndReapsTheProcessGroup(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	done := make(chan Report, 1)
	run := fixtureRunner(t, "hang", pids)
	go func() { done <- run(context.Background(), Limits{Timeout: 2 * time.Second}) }()
	// The recorded pids are the startup handshake: the bound is measured
	// from a running pack, not from a launch the scheduler may delay.
	pack, child := waitForPIDs(t, pids)
	started := time.Now()
	report := <-done
	assertPackFailure(t, report, FailureTimeout, "did not exit within 2s")
	if elapsed := time.Since(started); elapsed > 7*time.Second || report.Failure.Exit != "signal: killed" {
		t.Fatalf("timeout took %s after startup: %+v", elapsed, report.Failure)
	}
	// The pack was reaped by Run; the descendant shared its group and its
	// stdout, so it was killed with it.
	if !processGone(pack) || !processGone(child) {
		t.Fatalf("pack %d or descendant %d outlived the run", pack, child)
	}
}

func TestRunCancellationKillsAndReapsTheProcessGroup(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Report, 1)
	run := fixtureRunner(t, "hang", pids)
	go func() { done <- run(ctx, Limits{Timeout: time.Minute}) }()
	pack, child := waitForPIDs(t, pids)
	cancel()
	select {
	case report := <-done:
		assertPackFailure(t, report, FailureCancelled, "context canceled")
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation did not stop the run")
	}
	if !processGone(pack) || !processGone(child) {
		t.Fatalf("pack %d or descendant %d outlived the cancelled run", pack, child)
	}
	// An already cancelled analysis starts nothing.
	assertPackFailure(t, runFixture(t, ctx, Limits{}, "crash"), FailureCancelled, "context canceled")
}

func TestRunKillsDescendantsLeftAfterASuccessfulPack(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	report := runFixture(t, context.Background(), Limits{}, "orphan", pids)
	if !report.Complete || !reflect.DeepEqual(report.Findings, []ReportedFinding{renameFinding}) {
		t.Fatalf("%+v", report)
	}
	data, err := os.ReadFile(pids)
	if err != nil {
		t.Fatal(err)
	}
	child, _ := strconv.Atoi(strings.Fields(string(data))[1])
	if !processGone(child) {
		syscall.Kill(child, syscall.SIGKILL)
		t.Fatalf("descendant %d outlived a successful pack", child)
	}
}

// A descendant that leaves the process group cannot be killed by the
// runner; holding the pack's stdout open must still end the run within the
// output grace period, as a failure.
func TestRunBoundsOutputHeldByAnEscapedDescendant(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	run := fixtureRunner(t, "escape", pids)
	start := time.Now()
	report := run(context.Background(), Limits{})
	data, _ := os.ReadFile(pids)
	if fields := strings.Fields(string(data)); len(fields) == 2 {
		child, _ := strconv.Atoi(fields[1])
		syscall.Kill(child, syscall.SIGKILL)
	}
	assertPackFailure(t, report, FailureMalformed, "stream did not end after the frame")
	if elapsed := time.Since(start); elapsed > outputGrace+5*time.Second {
		t.Fatalf("escaped descendant held the run for %s", elapsed)
	}
}

// A descendant that leaves the process group holding only the pack's stdin,
// unread, must not hold the run: the request is larger than any pipe buffer,
// so the runner's write blocks until the grace period ends it.
func TestRunBoundsTheRequestWriteHeldByAnEscapedDescendant(t *testing.T) {
	registry, err := NewRegistry(testBuiltins, fixtureManifest(t, "escape-stdin", filepath.Join(t.TempDir(), "pids")))
	if err != nil {
		t.Fatal(err)
	}
	pids := registry.packs[0].Executable.Args[1]
	configuration := configure(t, registry, `{"version":1}`)
	snapshot := testSnapshot()
	for i := range 20_000 {
		snapshot.Callables = append(snapshot.Callables, Callable{Identity: fmt.Sprint("function:padding", i), Name: fmt.Sprint("padding", i), Kind: CallableFunction})
	}
	done := make(chan Report, 1)
	start := time.Now()
	go func() {
		report, err := Run(context.Background(), configuration, "fixture", snapshot, RunOptions{})
		if err != nil {
			t.Error(err)
		}
		done <- report
	}()
	// The pack exits at once; its descendant is the process under test.
	_, child := readPIDs(t, pids)
	t.Cleanup(func() { syscall.Kill(child, syscall.SIGKILL) })
	if syscall.Kill(child, 0) != nil {
		t.Fatalf("descendant %d was not alive", child)
	}
	select {
	case report := <-done:
		assertPackFailure(t, report, FailureMalformed, "no frame")
		if elapsed := time.Since(start); elapsed > outputGrace+5*time.Second {
			t.Fatalf("escaped stdin held the run for %s", elapsed)
		}
	case <-time.After(outputGrace + 10*time.Second):
		// Release the blocked write so the run goroutine can finish.
		syscall.Kill(child, syscall.SIGKILL)
		<-done
		t.Fatal("an escaped descendant holding stdin held the run past the grace period")
	}
	// The bound held while the descendant still held stdin.
	if syscall.Kill(child, 0) != nil {
		t.Fatalf("descendant %d was gone before the run returned; the bound is unproven", child)
	}
}
