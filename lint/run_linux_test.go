package lint

import (
	"context"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// The memory limit is RLIMIT_DATA, enforced on Linux only.
func TestRunLimitsPackMemory(t *testing.T) {
	if report := runFixture(t, context.Background(), Limits{}, "memory"); !report.Complete || report.Findings[0].Message != "held 768 MiB" {
		t.Fatalf("768 MiB within the default limit: %+v", report)
	}
	report := runFixture(t, context.Background(), Limits{MaxMemoryBytes: 128 << 20}, "memory")
	assertPackFailure(t, report, FailureCrashed, "exited unsuccessfully")
	if !strings.Contains(report.Failure.Stderr, "out of memory") && !strings.Contains(report.Failure.Stderr, "cannot allocate memory") {
		t.Fatalf("memory failure evidence: %+v", report.Failure)
	}
}

// The pack runs under the configured limit itself, both soft and hard.
func TestRunAppliesTheMemoryLimitToThePack(t *testing.T) {
	report := runFixture(t, context.Background(), Limits{MaxMemoryBytes: 300 << 20}, "limits")
	if !report.Complete || len(report.Findings) != 1 || report.Findings[0].Message != "Max data size 314572800 314572800 bytes" {
		t.Fatalf("pack limit: %+v %+v", report, report.Failure)
	}
}

// awaitExit blocks until the child pid has exited, without reaping it
// (waitid with WNOWAIT): the exit is observed, never raced.
func awaitExit(t *testing.T, pid int) {
	t.Helper()
	const pPID = 1
	var info [128]byte // siginfo_t
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, pPID, uintptr(pid), uintptr(unsafe.Pointer(&info)), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			t.Errorf("waitid %d: %v", pid, errno)
		}
		return
	}
}

// A pack that exits before its limit is applied is a zombie until the
// runner reaps it, so the limit still lands on that same process and the
// runner reports the pack's own outcome. The seam holds the runner until
// the pack has exited, so every iteration takes the exit-before-limit path.
func TestRunReportsAPackThatExitsBeforeItsLimit(t *testing.T) {
	exited := 0
	packStarted = func(pid int) {
		awaitExit(t, pid)
		exited++
	}
	t.Cleanup(func() { packStarted = func(int) {} })
	run := fixtureRunner(t, "exit-at-once")
	for i := range 50 {
		report := run(context.Background(), Limits{})
		assertPackFailure(t, report, FailureCrashed, "exited unsuccessfully")
		if report.Failure.Exit != "exit status 4" {
			t.Fatalf("iteration %d: %+v", i, report.Failure)
		}
	}
	if exited != 50 {
		t.Fatalf("the seam saw %d of 50 exits", exited)
	}
}
