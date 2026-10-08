package lint

import (
	"context"
	"strings"
	"testing"
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
