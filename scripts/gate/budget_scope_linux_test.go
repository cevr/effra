package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// The systemd scope tests run each case as a gate re-executed as the gate
// itself is (scopeArguments, scopeEnvironment): a helper process, this test
// binary, in a fresh scope capped at the case's gate budget. The scopes run
// in a slice of this test run's own (testSlice), below ergo-gates.slice, so
// the host budget a case sets bounds only its own gates, and the real
// gates' bound also holds over them. The helpers leave the cgroup of the
// step that runs this test, if a gate runs it: their own small budgets and
// the gates' slice bound them.

// testSlice is the slice of this test run's gate scopes. When the test
// ends, its helpers have ended, and the slice's MemoryMax, which they set,
// is reverted and the empty slice stopped.
func testSlice(t *testing.T) string {
	t.Helper()
	slice := fmt.Sprintf("%s-test-%d.slice", strings.TrimSuffix(gateSlice, ".slice"), os.Getpid())
	t.Cleanup(func() {
		for _, action := range []string{"revert", "stop"} {
			command := exec.Command("systemctl", "--user", action, slice)
			command.Env = scopeEnvironment("")
			if output, err := command.CombinedOutput(); err != nil {
				t.Logf("systemctl --user %s %s: %v: %s", action, slice, err, output)
			}
		}
	})
	return slice
}

// scopeHelper is one helper gate, set up to run.
type scopeHelper struct {
	command *exec.Cmd
	output  bytes.Buffer
	cache   string
}

// startableScope skips the test where no systemd user scope can start.
func startableScope(t *testing.T, slice string, budget memoryBudget) {
	t.Helper()
	if _, err := exec.LookPath("systemd-run"); err != nil {
		t.Skip("no systemd-run: the RSS watch tests cover this host")
	}
	probe := exec.Command("systemd-run", scopeArguments(slice, "", budget, []string{"true"})...)
	probe.Env = scopeEnvironment("")
	if output, err := probe.CombinedOutput(); err != nil {
		t.Skipf("no systemd user scope here (%v: %s): the RSS watch tests cover this host", err, firstLine(output))
	}
}

// newScopeHelper prepares a helper gate in slice that runs the named case
// under the budget the environment sets.
func newScopeHelper(t *testing.T, slice, name string, budget memoryBudget) *scopeHelper {
	t.Helper()
	unit := scopeUnit()
	h := &scopeHelper{cache: t.TempDir()}
	h.command = exec.Command("systemd-run", scopeArguments(slice, unit, budget, []string{os.Args[0], "-test.run=^TestBudgetScopeHelper$", "-test.count=1"})...)
	h.command.Env = append(scopeEnvironment(unit), "GATE_BUDGET_CASE="+name, "GATE_BUDGET_ROOT="+budgetFixture(t), "GATE_BUDGET_CACHE="+h.cache)
	h.command.Stdout, h.command.Stderr = &h.output, &h.output
	return h
}

// result is the helper gate's exit code and output.
func (h *scopeHelper) result(t *testing.T) (int, string) {
	t.Helper()
	text := h.output.String()
	t.Logf("helper output:\n%s", text)
	_, exit, ok := strings.Cut(text, "gate-exit=")
	var code int
	if _, err := fmt.Sscanf(exit, "%d", &code); !ok || err != nil {
		t.Fatalf("the helper gate did not finish:\n%s", text)
	}
	return code, text
}

// TestBudgetSystemdScope runs the budget cases under the systemd scope, the
// mechanism of Linux hosts with a user service manager.
func TestBudgetSystemdScope(t *testing.T) {
	slice := testSlice(t)
	for _, c := range budgetCases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(stepMemoryVariable, c.step)
			t.Setenv(gateMemoryVariable, c.gate)
			t.Setenv(hostMemoryVariable, "4G")
			budget, err := currentBudget()
			if err != nil {
				t.Fatal(err)
			}
			startableScope(t, slice, budget)
			h := newScopeHelper(t, slice, c.name, budget)
			h.command.Run()
			code, text := h.result(t)
			if !strings.Contains(text, "gate: memory budget: systemd scope "+c.step+" per step, "+c.gate+" per gate, 4G across gates in "+slice+"\n") {
				t.Errorf("the helper gate does not name the systemd scope and its budgets")
			}
			c.check(t, scopeMechanism, code, text, receiptsIn(t, h.cache))
		})
	}
}

// hostCase is a gate whose step holds 448 MiB, under its step and gate
// budgets of 512M; TestBudgetHostSlice runs two at once under a host budget
// of 768M.
var hostCase = budgetCase{
	name: "host", step: "512M", gate: "512M",
	steps: func() []*Step { return []*Step{allocatorStep("alloc", 448, 3*time.Second)} },
}

// TestBudgetHostSlice runs two gates, each under its own budgets, that
// together pass the host budget: the kernel stops the larger, so the gates
// never hold more than the host budget together, and the gate stopped says
// so.
func TestBudgetHostSlice(t *testing.T) {
	slice := testSlice(t)
	t.Setenv(stepMemoryVariable, hostCase.step)
	t.Setenv(gateMemoryVariable, hostCase.gate)
	t.Setenv(hostMemoryVariable, "768M")
	budget, err := currentBudget()
	if err != nil {
		t.Fatal(err)
	}
	startableScope(t, slice, budget)
	helpers := []*scopeHelper{newScopeHelper(t, slice, hostCase.name, budget), newScopeHelper(t, slice, hostCase.name, budget)}
	var running sync.WaitGroup
	for _, h := range helpers {
		running.Go(func() { h.command.Run() })
	}
	running.Wait()
	stopped := 0
	for _, h := range helpers {
		code, text := h.result(t)
		wantAll(t, text, "gate: memory budget: systemd scope 512M per step, 512M per gate, 768M across gates in "+slice+"\n")
		switch {
		case strings.Contains(text, "gate: alloc: host memory budget exceeded (768M)"):
			stopped++
			if code != 1 || len(receiptsIn(t, h.cache)) != 0 {
				t.Errorf("a gate stopped for the host budget exited %d with receipts: want 1 and none", code)
			}
		case code != 0 || !strings.Contains(text, "== alloc: ok"):
			t.Errorf("a gate neither passed nor was stopped for the host budget:\n%s", text)
		}
	}
	if stopped == 0 {
		t.Error("no gate was stopped for the host budget")
	}
}

// TestBudgetScopeHelper is the gate the scope tests start in a systemd
// scope: it runs one case's steps and prints the gate's output and exit
// code. Like the gate, it keeps the scope's unit from its steps.
func TestBudgetScopeHelper(t *testing.T) {
	name := os.Getenv("GATE_BUDGET_CASE")
	if name == "" {
		t.Skip("run by the scope tests")
	}
	scope := os.Getenv(scopeVariable)
	os.Unsetenv(scopeVariable)
	for _, c := range append(budgetCases, hostCase) {
		if c.name == name {
			var out bytes.Buffer
			g := &gate{root: os.Getenv("GATE_BUDGET_ROOT"), cacheDir: os.Getenv("GATE_BUDGET_CACHE"), steps: c.steps(), scope: scope}
			code := g.run(&out)
			fmt.Printf("%sgate-exit=%d\n", out.String(), code)
			return
		}
	}
	t.Fatalf("no budget case %q", name)
}

// TestBudgetNestedHelper starts a gate inside a step of the helper gate, as
// main does, and prints where it runs. A gate that opened its own scope
// re-executes this test in it, which then says so.
func TestBudgetNestedHelper(t *testing.T) {
	if os.Getenv("GATE_BUDGET_NESTED") == "" {
		t.Skip("run as a step by the nested budget case")
	}
	if os.Getenv(scopeVariable) != "" {
		fmt.Println("re-executed in a scope")
		return
	}
	note := enterScope(memoryBudget{step: 256 << 20, gate: 512 << 20, host: 512 << 20})
	membership, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("nested note: %s\nnested cgroup: %s", note, membership)
}

// A process is in an outer gate's step when its cgroup is a step-N child of
// an ergo-gate-*.scope; the gate's own leaf and other cgroups are not steps.
func TestBudgetOuterGateStep(t *testing.T) {
	scope := "/user.slice/user-1000.slice/user@1000.service/ergo.slice/ergo-gates.slice/ergo-gate-12-ab12cd34.scope"
	for _, c := range []struct {
		membership string
		nested     bool
	}{
		{"0::" + scope + "/step-7\n", true},
		{"0::" + scope + "/step-7/inner\n", true},
		{"0::" + scope + "/gate\n", false},
		{"0::" + scope + "\n", false},
		{"0::/user.slice/user-1000.slice/session-3.scope\n", false},
		{"0::/user.slice/ergo-gate-1.service/step-1\n", false},
		{"1:name=systemd:" + scope + "/step-7\n", false},
		{"", false},
	} {
		if _, nested := outerGateStep(c.membership); nested != c.nested {
			t.Errorf("outerGateStep(%q) = %t, want %t", c.membership, nested, c.nested)
		}
	}
}
