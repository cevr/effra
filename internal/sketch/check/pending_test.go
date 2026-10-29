package check_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

var pendingOnce struct {
	sync.Once
	stages map[string]string
	err    error
}

// pendingStages reads testdata/v1-pending.txt: each quarantined fixture,
// by its slash path under testdata, and the stage that checks it.
func pendingStages(t *testing.T) map[string]string {
	t.Helper()
	pendingOnce.Do(func() {
		data, err := os.ReadFile(filepath.Join("testdata", "v1-pending.txt"))
		if err != nil {
			pendingOnce.err = err
			return
		}
		pendingOnce.stages = map[string]string{}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			path, stage, ok := strings.Cut(line, " ")
			if !ok {
				pendingOnce.err = os.ErrInvalid
				return
			}
			pendingOnce.stages[path] = strings.TrimSpace(stage)
		}
	})
	if pendingOnce.err != nil {
		t.Fatalf("testdata/v1-pending.txt: %v", pendingOnce.err)
	}
	return pendingOnce.stages
}

// skipPending skips a verdict test of a quarantined fixture, naming the
// stage that checks it. file is the path the loader read, under testdata.
func skipPending(t *testing.T, file string) {
	t.Helper()
	rel, err := filepath.Rel("testdata", file)
	if err != nil {
		return
	}
	if stage, ok := pendingStages(t)[filepath.ToSlash(rel)]; ok {
		t.Skipf("pending stage %s (testdata/v1-pending.txt)", stage)
	}
}

// TestPendingStages: every quarantined fixture exists, the checker reads
// it without a panic, and it is still S109 naming its stage. When the
// stage lands the S109 goes, and this test asks for the line to go too.
func TestPendingStages(t *testing.T) {
	stages := pendingStages(t)
	if len(stages) == 0 {
		t.Fatal("testdata/v1-pending.txt lists no fixture")
	}
	for path, stage := range stages {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			_, diags := check.CheckSource(string(data))
			for _, d := range diags {
				if d.Code == check.CodeOutside && strings.Contains(d.Message, stage) {
					return
				}
			}
			t.Fatalf("no S109 naming %s; if the stage landed, unquarantine it: %v", stage, diags)
		})
	}
}
