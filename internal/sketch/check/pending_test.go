package check_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// A pendingEntry is one line of testdata/v1-pending.txt: the stage that
// checks the input and why the 196a checker cannot.
type pendingEntry struct {
	stage, reason string
}

// pendingStageNames are the stages an input may wait for (merge round step
// 4, and lower-v1 for the std modules, round 6b1 answers).
var pendingStageNames = map[string]bool{
	"196m": true, "196p": true, "196r": true, "196v": true, "196l": true, "196x": true, "lower-v1": true,
}

var pendingOnce struct {
	sync.Once
	entries map[string]pendingEntry
	err     error
}

// pendingStages reads testdata/v1-pending.txt: each quarantined input, by
// its id (a slash path under testdata, or Test/case), with its stage and
// reason.
func pendingStages(t *testing.T) map[string]pendingEntry {
	t.Helper()
	pendingOnce.Do(func() {
		data, err := os.ReadFile(filepath.Join("testdata", "v1-pending.txt"))
		if err != nil {
			pendingOnce.err = err
			return
		}
		pendingOnce.entries = map[string]pendingEntry{}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) != 3 || cols[0] == "" || strings.TrimSpace(cols[2]) == "" || !pendingStageNames[cols[1]] {
				pendingOnce.err = fmt.Errorf("line %d: want id TAB stage TAB reason, with a stage of %v: %q", i+1, pendingStageNames, line)
				return
			}
			pendingOnce.entries[cols[0]] = pendingEntry{stage: cols[1], reason: cols[2]}
		}
	})
	if pendingOnce.err != nil {
		t.Fatalf("testdata/v1-pending.txt: %v", pendingOnce.err)
	}
	return pendingOnce.entries
}

// isPendingFixture reports whether a quarantine id is a fixture path.
func isPendingFixture(id string) bool { return strings.HasSuffix(id, ".ergo") }

// skipPending skips a verdict test of a quarantined fixture, naming the
// stage that checks it. file is the path the loader read, under testdata.
func skipPending(t *testing.T, file string) {
	t.Helper()
	rel, err := filepath.Rel("testdata", file)
	if err != nil {
		return
	}
	if e, ok := pendingStages(t)[filepath.ToSlash(rel)]; ok {
		t.Skipf("pending stage %s: %s (testdata/v1-pending.txt)", e.stage, e.reason)
	}
}

// pendingCase returns the quarantine entry of the test case id
// (Test/case), if it is listed.
func pendingCase(t *testing.T, id string) (pendingEntry, bool) {
	t.Helper()
	e, ok := pendingStages(t)[id]
	return e, ok
}

// TestPendingStages: every quarantined fixture exists, the checker reads it
// without a panic, and it is still S109 naming its stage; every quarantined
// test case names a case that exists (TestReviewCases checks its S109).
// When the stage lands the S109 goes, and the tests ask for the line to go
// too.
func TestPendingStages(t *testing.T) {
	entries := pendingStages(t)
	if len(entries) == 0 {
		t.Fatal("testdata/v1-pending.txt lists no input")
	}
	reviewIDs := map[string]bool{}
	for _, c := range reviewCases {
		reviewIDs["TestReviewCases/"+c.finding+"/"+c.name] = true
	}
	for id, e := range entries {
		t.Run(id, func(t *testing.T) {
			if !isPendingFixture(id) {
				if !reviewIDs[id] {
					t.Fatalf("no test case %s", id)
				}
				return
			}
			data, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(id)))
			if err != nil {
				t.Fatal(err)
			}
			_, diags := check.CheckSource(string(data))
			for _, d := range diags {
				if d.Code == check.CodeOutside && strings.Contains(d.Message, e.stage) {
					return
				}
			}
			t.Fatalf("no S109 naming %s; if the stage landed, unquarantine it: %v", e.stage, diags)
		})
	}
}
