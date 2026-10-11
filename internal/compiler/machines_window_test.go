package compiler

import (
	"fmt"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler/machineplan"
)

// Round-3 review M1: the analysis window must not depend on checker state
// built before it. With ordinary diagnostics filling c.result.Diagnostics to
// its capacity, the machine's first diagnostic used to regrow the whole slice
// inside the window, and a fresh layout instantiation regrew a full type
// arena. Analysis now reads only its prepared input and records diagnostics in
// its own buffer; the checker commits them after the window, so what the
// window allocates is what the analysis charged.

const machineWindowPrelude = "import Machines \"effra/machines\"\nenum S { A }\nenum E { P }\nfn step(state: S, event: E) -> Machines.Step<S, void> { Machines.Step<S, void>.Ignore {} }\n"

// ordinaryErrorsThenStepless is errors ordinary type errors, then a machine
// with no step binding (EF140).
func ordinaryErrorsThenStepless(errors int) string {
	var b strings.Builder
	b.WriteString(machineWindowPrelude)
	for i := 0; i < errors; i++ {
		fmt.Fprintf(&b, "fn e%d() -> i64 { true }\n", i)
	}
	b.WriteString("machine M { initial S.A {} }\n")
	return b.String()
}

// The reviewer's construction: ordinary errors until the diagnostics are at
// len == cap as the window opens, then one machine diagnostic.
func TestMachineWindowIgnoresDiagnosticsAtCapacity(t *testing.T) {
	var length, capacity int
	observe := func(c *checker) func(*checker) string {
		length, capacity = len(c.result.Diagnostics), cap(c.result.Diagnostics)
		return func(c *checker) string {
			if len(c.result.Diagnostics) != length {
				return fmt.Sprintf("diagnostics from %d to %d", length, len(c.result.Diagnostics))
			}
			return ""
		}
	}
	n := 20000
	for range 16 {
		measureMachineAnalysisAfter(t, ordinaryErrorsThenStepless(n), observe)
		if length == capacity {
			break
		}
		n += capacity - length
	}
	if length != capacity || length < 20000 {
		t.Fatalf("diagnostics at the window: len %d cap %d", length, capacity)
	}
	r, measures := measureMachineAnalysisAfter(t, ordinaryErrorsThenStepless(n), observe)
	assertMachineAllocationCharged(t, fmt.Sprintf("%d diagnostics at capacity", length), measures)
	// The machine's diagnostic is committed after the window, last and once.
	if len(r.Diagnostics) != length+1 {
		t.Fatalf("%d diagnostics, want %d", len(r.Diagnostics), length+1)
	}
	if last := r.Diagnostics[len(r.Diagnostics)-1]; last.Code != machineContractCode || !strings.Contains(last.Message, "requires a step binding") {
		t.Fatalf("last diagnostic = %s %s", last.Code, last.Message)
	}
}

// Deterministic forms: every checker slice at capacity and every intern map
// at its size as the window opens (clipCheckerState), over the shapes whose
// analysis used to touch them: diagnostics, a fresh generic instantiation
// beside a large type arena (L<i64> through Outer<i64>), a step-binding
// reference after many ordinary references, and plain plans.
func TestMachineWindowIgnoresCheckerStateAtCapacity(t *testing.T) {
	references := func(n int) string {
		var b strings.Builder
		b.WriteString(machineWindowPrelude + "fn base() -> i64 { 1 }\nfn refs() -> i64 {\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "    let r%d = base()\n", i)
		}
		b.WriteString("    1\n}\nmachine M { initial S.A {}; step step }\n")
		return b.String()
	}
	cases := []struct {
		name   string
		source string
		plans  int
	}{
		{"ordinary errors then a step-less machine", ordinaryErrorsThenStepless(1000), 0},
		{"generic instantiation beside a large arena", machineShape{states: 1, events: 1, generic: true, records: 2000}.source(), 1},
		{"step reference after ordinary references", references(2000), 1},
		{"helpers and rows over many declarations", machineShape{states: 3, events: 2, helpers: 2, tails: 7, failures: 3, declarations: 64}.source(), 64},
	}
	for _, c := range cases {
		r, measures := measureMachineAnalysisAfter(t, c.source, clipCheckerState)
		assertMachineAllocationCharged(t, c.name, measures)
		if len(r.Machines) != c.plans {
			t.Fatalf("%s: %d plans, want %d: %v", c.name, len(r.Machines), c.plans, machineCodes(r))
		}
	}
}

// Round-4 review M2: the EF142 that exhausts a declaration's budget is
// recorded uncharged, after the budget is gone, so its message must not copy
// a program-sized name. A 40 MiB machine name refuses its plan-ID charge, and
// the refusal used to quote the whole name: 83.9 MB in the window for 65,541
// units.
func TestMachineRefusalQuotesABoundedName(t *testing.T) {
	size := 40 << 20
	name := "M" + strings.Repeat("x", size)
	r, measures := measureMachineAnalysis(t, machineWindowPrelude+"machine "+name+" { initial S.A {}; step step }\n")
	assertMachineAllocationCharged(t, "40 MiB machine name", measures)
	if codes := machineCodes(r); len(codes) != 1 || codes[0] != machineBudgetCode {
		t.Fatalf("codes = %v", codes)
	}
	message := r.Diagnostics[len(r.Diagnostics)-1].Message
	want := fmt.Sprintf("machine %s… (%d bytes) analysis exceeds", name[:64], len(name))
	if !strings.HasPrefix(message, want) || len(message) > 256 {
		t.Fatalf("refusal message (%d bytes) = %.200q", len(message), message)
	}
}

// Round-4 review m1: each declaration's fixed state is charged at its
// measured size (machineplan.DeclarationUnits), which since b07b96c1 is 171
// units for the checker's value type, up from about 114. A fixed charge sets
// how many declarations a module admits, so the limit is pinned: 256
// declarations of 800 states each publish exactly 209 plans before the shared
// 1,048,576-unit budget refuses the rest (211 at the earlier charge).
func TestMachineModuleAdmissionFollowsTheFixedCharge(t *testing.T) {
	if units := machineplan.DeclarationUnits[ValueType](); units != 171 {
		t.Errorf("a declaration's fixed charge is %d units, want 171", units)
	}
	r, measures := machineShape{states: 800, events: 1, tails: 2, wideArm: true, declarations: 256}.measure(t)
	assertMachineAllocationCharged(t, "256 declarations of 800 states", measures)
	if len(r.Machines) != 209 {
		t.Errorf("%d of 256 declarations admitted, want 209", len(r.Machines))
	}
	if codes := machineCodes(r); len(codes) != 1 || codes[0] != machineBudgetCode || !strings.Contains(r.Diagnostics[len(r.Diagnostics)-1].Message, "shared 1048576-unit") {
		t.Errorf("codes = %v", codes)
	}
}

// Preparation is checker work outside the window, bounded by mirroring the
// analysis budget: a layout or display analysis could never afford is refused
// before the checker builds it (measureMachineAnalysis checks every prepared
// one). 3,000 fields over a 400-node template argument would instantiate an
// exponential layout, and a display copied through 56 recipe layers around a
// 65,535-byte name costs more than a declaration's budget; both are refused.
func TestMachinePreparationStaysWithinTheAnalysisBudget(t *testing.T) {
	for _, c := range []struct {
		name  string
		shape machineShape
	}{
		{"template argument walks", machineTemplateArgumentWalks},
		{"nested recipe display over the budget", machineShape{states: 1, events: 1, recipe: 56, recipeName: 65535}},
	} {
		r, measures := c.shape.measure(t)
		assertMachineAllocationCharged(t, c.name, measures)
		if codes := machineCodes(r); len(codes) != 1 || codes[0] != machineBudgetCode {
			t.Fatalf("%s: codes = %v", c.name, codes)
		}
	}
}
