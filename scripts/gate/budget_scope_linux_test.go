package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestBudgetSystemdScope runs the budget cases under the systemd scope, the
// mechanism of Linux hosts with a user service manager. Each case is a gate
// re-executed as the gate itself is (scopeArguments, scopeEnvironment): a
// helper process, this test binary, in a fresh scope capped at the case's
// gate budget.
func TestBudgetSystemdScope(t *testing.T) {
	if _, err := exec.LookPath("systemd-run"); err != nil {
		t.Skip("no systemd-run: the RSS watch tests cover this host")
	}
	for _, c := range budgetCases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(stepMemoryVariable, c.step)
			t.Setenv(gateMemoryVariable, c.gate)
			budget, err := currentBudget()
			if err != nil {
				t.Fatal(err)
			}
			unit := scopeUnit()
			probe := exec.Command("systemd-run", scopeArguments("", budget, []string{"true"})...)
			probe.Env = scopeEnvironment("")
			if output, err := probe.CombinedOutput(); err != nil {
				t.Skipf("no systemd user scope here (%v: %s): the RSS watch tests cover this host", err, firstLine(output))
			}
			cache := t.TempDir()
			helper := exec.Command("systemd-run", scopeArguments(unit, budget, []string{os.Args[0], "-test.run=^TestBudgetScopeHelper$", "-test.count=1"})...)
			helper.Env = append(scopeEnvironment(unit), "GATE_BUDGET_CASE="+c.name, "GATE_BUDGET_ROOT="+budgetFixture(t), "GATE_BUDGET_CACHE="+cache)
			var output bytes.Buffer
			helper.Stdout, helper.Stderr = &output, &output
			helper.Run()
			text := output.String()
			t.Logf("helper output:\n%s", text)
			_, exit, ok := strings.Cut(text, "gate-exit=")
			var code int
			if _, err := fmt.Sscanf(exit, "%d", &code); !ok || err != nil {
				t.Fatalf("the helper gate did not finish:\n%s", text)
			}
			if !strings.Contains(text, "gate: memory budget: systemd scope "+c.step+" per step, "+c.gate+" per gate\n") {
				t.Errorf("the helper gate does not name the systemd scope and its budget")
			}
			c.check(t, code, text, receiptsIn(t, cache))
		})
	}
}

// TestBudgetScopeHelper is the gate TestBudgetSystemdScope starts in a
// systemd scope: it runs one case's steps and prints the gate's output and
// exit code.
func TestBudgetScopeHelper(t *testing.T) {
	name := os.Getenv("GATE_BUDGET_CASE")
	if name == "" {
		t.Skip("run by TestBudgetSystemdScope")
	}
	for _, c := range budgetCases {
		if c.name == name {
			var out bytes.Buffer
			g := &gate{root: os.Getenv("GATE_BUDGET_ROOT"), cacheDir: os.Getenv("GATE_BUDGET_CACHE"), steps: c.steps(), scope: os.Getenv(scopeVariable)}
			code := g.run(&out)
			fmt.Printf("%sgate-exit=%d\n", out.String(), code)
			return
		}
	}
	t.Fatalf("no budget case %q", name)
}
