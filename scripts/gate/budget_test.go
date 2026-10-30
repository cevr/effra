package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The memory budget, tested through the gate's run as a caller sees it:
// the environment sets the budgets, and steps run this test binary as an
// allocator (TestBudgetAllocator). This file names no gate internals, so it
// also runs against a gate without a budget, where it fails.

// TestBudgetAllocator is a step's process: run with two arguments, it
// allocates and touches that many MiB, prints "allocated", and holds the
// memory for the given duration.
func TestBudgetAllocator(t *testing.T) {
	if flag.NArg() != 2 {
		t.Skip("a budget test runs it as a step")
	}
	mib, err := strconv.Atoi(flag.Arg(0))
	if err != nil {
		t.Fatal(err)
	}
	hold, err := time.ParseDuration(flag.Arg(1))
	if err != nil {
		t.Fatal(err)
	}
	var chunks [][]byte
	for range mib / 16 {
		chunk := make([]byte, 16<<20)
		for i := 0; i < len(chunk); i += 4096 {
			chunk[i] = 1
		}
		chunks = append(chunks, chunk)
	}
	fmt.Printf("allocated %d MiB\n", mib)
	time.Sleep(hold)
	runtime.KeepAlive(chunks)
}

// allocatorStep allocates mib MiB and holds it. The shell around the
// allocator exits 0 even when the allocator is killed, so a verdict cannot
// come from the exit status alone. Its result is cacheable, so a test can
// see that a budget failure is never recorded.
func allocatorStep(name string, mib int, hold time.Duration) *Step {
	return &Step{
		Name:   name,
		Argv:   []string{"sh", "-c", `"$0" -test.run='^TestBudgetAllocator$' -test.count=1 "$1" "$2"; echo "allocator exited $?"`, os.Args[0], strconv.Itoa(mib), hold.String()},
		Inputs: &Inputs{Paths: []string{"src/" + name + ".txt"}},
	}
}

// budgetCase is one gate run under a budget, checked on every mechanism.
type budgetCase struct {
	name, step, gate string
	steps            func() []*Step
	check            func(t *testing.T, code int, out string, receipts []string)
}

var budgetCases = []budgetCase{
	{
		// The acceptance case: a 2 GB allocation under a 512M step budget is
		// stopped, the gate goes on, reports it and records no pass.
		name: "over", step: "512M", gate: "2G",
		steps: func() []*Step {
			return []*Step{allocatorStep("alloc", 2048, 10*time.Second), {Name: "independent", Argv: []string{"sh", "-c", "echo independent ran"}}}
		},
		check: func(t *testing.T, code int, out string, receipts []string) {
			wantAll(t, out, "== alloc: FAILED", "gate: alloc: memory budget exceeded (512M)\n", "== independent: ok", "independent ran", "gate: FAILED in")
			if code != 1 || strings.Contains(out, "allocated 2048 MiB") || len(receipts) != 0 {
				t.Errorf("exit %d, receipts %v: want 1, no completed allocation and no receipt:\n%s", code, receipts, out)
			}
		},
	},
	{
		// A step under its budget passes, and the gate reports its peak.
		name: "under", step: "512M", gate: "2G",
		steps: func() []*Step { return []*Step{allocatorStep("alloc", 64, time.Second)} },
		check: func(t *testing.T, code int, out string, receipts []string) {
			wantAll(t, out, "allocated 64 MiB", "allocator exited 0")
			if code != 0 || !strings.Contains(out, "== alloc: ok (") || !strings.Contains(out, ", peak ") || len(receipts) != 1 {
				t.Errorf("exit %d, receipts %v: want a pass with its peak, recorded once:\n%s", code, receipts, out)
			}
		},
	},
	{
		// Two steps, each under the step budget, together pass the gate
		// budget: the gate stops the largest, so its steps never hold more
		// than the gate budget together. The other usually passes; under the
		// systemd scope the kernel may stop it too, when the first one's
		// memory is not yet freed as the second one grows.
		name: "gate", step: "512M", gate: "768M",
		steps: func() []*Step {
			return []*Step{allocatorStep("a", 448, 3*time.Second), allocatorStep("b", 448, 3*time.Second)}
		},
		check: func(t *testing.T, code int, out string, receipts []string) {
			stopped := 0
			for _, name := range []string{"a", "b"} {
				if strings.Contains(out, "gate: "+name+": gate memory budget exceeded (768M)") {
					stopped++
				} else if !strings.Contains(out, "== "+name+": ok") {
					t.Errorf("step %s neither passed nor was stopped for the gate budget", name)
				}
			}
			if code != 1 || stopped == 0 || len(receipts) != 2-stopped {
				t.Errorf("exit %d, %d steps stopped for the gate budget, receipts %v: want 1, at least 1, and one per passed step:\n%s", code, stopped, receipts, out)
			}
		},
	},
}

func wantAll(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// receiptsIn lists the passes recorded in a gate cache directory.
func receiptsIn(t *testing.T, cache string) []string {
	t.Helper()
	receipts, err := filepath.Glob(filepath.Join(cache, "*", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return receipts
}

// budgetFixture is a fixture with the inputs the allocator steps declare.
func budgetFixture(t *testing.T) string {
	root := fixture(t)
	for _, name := range []string{"alloc", "a", "b"} {
		write(t, root, "src/"+name+".txt", name+"\n")
	}
	return root
}

// TestBudgetRSSWatch runs the cases under the RSS watch, the mechanism of
// darwin and of hosts without a usable systemd scope, forced here so it
// runs on Linux too.
func TestBudgetRSSWatch(t *testing.T) {
	t.Setenv("ERGO_GATE_MEMORY_MECHANISM", "rss")
	for _, c := range budgetCases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("ERGO_GATE_STEP_MEMORY", c.step)
			t.Setenv("ERGO_GATE_MEMORY", c.gate)
			cache := t.TempDir()
			code, out := runGate(t, budgetFixture(t), cache, c.steps())
			t.Logf("gate exit %d:\n%s", code, out)
			if !strings.Contains(out, "gate: memory budget: RSS watch "+c.step+" per step, "+c.gate+" per gate (forced by ERGO_GATE_MEMORY_MECHANISM=rss)\n") {
				t.Errorf("the gate does not name the RSS watch and its budget:\n%s", out)
			}
			c.check(t, code, out, receiptsIn(t, cache))
		})
	}
}
