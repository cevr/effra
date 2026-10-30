package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -record-durations runs its packages under the memory budget, as the gate
// runs its steps: a measured package whose test passes the step budget is
// stopped, and the durations file is not rewritten.
func TestBudgetRecordDurations(t *testing.T) {
	t.Setenv(mechanismVariable, "rss")
	t.Setenv(stepMemoryVariable, "256M")
	t.Setenv(gateMemoryVariable, "1G")
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/alloc\n\ngo 1.24\n")
	write(t, root, "alloc_test.go", `package alloc

import (
	"runtime"
	"testing"
	"time"
)

func TestAllocate(t *testing.T) {
	var chunks [][]byte
	for range 64 {
		chunk := make([]byte, 16<<20)
		for i := 0; i < len(chunk); i += 4096 {
			chunk[i] = 1
		}
		chunks = append(chunks, chunk)
	}
	time.Sleep(10 * time.Second)
	runtime.KeepAlive(chunks)
}
`)
	saved := slowPackages
	slowPackages = []string{"."}
	t.Cleanup(func() { slowPackages = saved })
	var out strings.Builder
	g := &gate{root: root}
	err := g.recordDurations(&out)
	t.Logf("output %q, error %v", out.String(), err)
	if err == nil || !strings.Contains(err.Error(), "go test -json .: memory budget exceeded (256M)") {
		t.Errorf("error %v: want the package stopped for its budget", err)
	}
	if !strings.Contains(out.String(), "gate: memory budget: RSS watch 256M per step, 1G per gate") {
		t.Errorf("the run does not name its budget: %q", out.String())
	}
	if _, statErr := os.Stat(filepath.Join(root, durationsFile)); !os.IsNotExist(statErr) {
		t.Errorf("the durations file was written (%v)", statErr)
	}
}
