package compiler

import (
	"fmt"
	"testing"
)

// The public boundaries project plans after analysis: CheckResponse,
// MachineInspection and the machine GraphView. Each is measured on the
// compiled result and again on a copy whose plans keep only their identity;
// what the plans add must stay within the same per-unit bound as the units
// that paid for it (assertMachineBoundariesCharged). The sweep in
// TestMachineAnalysisAllocationIsCharged runs this measurement over every
// generated shape that publishes a plan; these are the fixed shapes,
// including the reviewers' from rounds 1 and 2.
func TestMachineProjectionAllocationIsCharged(t *testing.T) {
	shapes := []machineShape{
		machineLongFailureLabels,
		machineGenericPayloadLayouts,
		{states: 2, events: 1, failures: 4000, declarations: 256},
		{states: 2, events: 1, name: 4096, payload: 30, declarations: 256},
		{states: 40, events: 2, name: 4096, variantName: 4096, helpers: 2, tails: 7, wideArm: true},
		{states: 4000, events: 1, tails: 20, wideArm: true, keepPaths: true, keeps: 2},
		{states: 2, events: 1, machineName: 1 << 16, helpers: 2, tails: 20, distinct: 20, declarations: 4},
		{states: 2, events: 1},
		{states: 3, events: 2, helpers: 1, tails: 2},
	}
	request, err := ParseGraphRequest(map[string]any{"kind": "machine"})
	if err != nil {
		t.Fatal(err)
	}
	warmMachineBoundaries(t, request)
	projected := 0
	for i, shape := range shapes {
		r, measures := shape.measure(t)
		if len(r.Machines) == 0 {
			continue
		}
		projected++
		assertMachineBoundariesCharged(t, fmt.Sprintf("shape %d (%s)", i, shape), shape, r, measures, request)
	}
	if projected == 0 {
		t.Fatal("no shape published a plan")
	}
}

// Round-2 review M3: 128 declarations whose effect step only fails, so no
// plan has a transition and each node's fixed copies are its whole cost.
var machineGraphFailBodyPlans = machineShape{states: 2, events: 1, failures: 3, failBody: true, declarations: 128}

// The machine GraphView is paid for by the analysis units plus the graph work
// its own budget charged (measureMachineGraphView). The first cases assert
// their outcome; the rest are the reviewers' GraphView shapes, held to the
// bound whatever their outcome. Validation and the response check serialise
// nothing, so a published view and a response refusal stay within the bound,
// and a plan with no transitions pays for its node's fixed copies.
func TestMachineGraphViewAllocationIsCharged(t *testing.T) {
	cases := []struct {
		shape   machineShape
		refusal string
	}{
		{machineShape{states: 2, events: 1}, ""},
		{machineShape{states: 2, events: 1, failures: 300}, ""},
		{machineShape{states: 2, events: 1, failures: 3, failureName: 4096}, ""},
		{machineShape{states: 2, events: 1, name: 4096, payload: 3}, ""},
		{machineShape{states: 1, events: 1, generic: true, genericName: 4096, declarations: 4}, ""},
		{machineShape{states: 2, events: 1, failures: 3, declarations: 64}, ""},
		{machineShape{states: 400, events: 1, tails: 2, wideArm: true}, ""},
		{machineGraphFailBodyPlans, ""},
		// Found by the round-3 sweep (seed 872): 300 failure labels of 66 bytes
		// publish 300 error types, each checked against every declaration.
		{machineShape{states: 2, events: 50, name: 4096, variantName: 64, tails: 2, keepPaths: true, failures: 300, declarations: 1, failureName: 64, generic: true, genericFields: 64, recipeName: 64, records: 2000}, ""},
		{machineShape{states: 40, events: 2, helpers: 2, tails: 7, wideArm: true}, GraphRefusalEdgeLimit},
		{machineShape{states: 2, events: 1, machineName: 4096, helpers: 2, tails: 20, distinct: 20}, GraphRefusalResponseLimit},
		{machineShape{states: 4000, events: 1, tails: 20, wideArm: true, keepPaths: true, keeps: 2}, GraphRefusalWorkLimit},
	}
	reviewed := []machineShape{
		// Round 1.
		{states: 2, events: 1, name: 1024, payload: 30, declarations: 16},
		{states: 2, events: 1, failures: 4000, declarations: 256},
		{states: 2, events: 1, name: 4096, payload: 30, declarations: 256},
		{states: 40, events: 2, name: 4096, variantName: 4096, helpers: 2, tails: 7, wideArm: true},
		{states: 2, events: 1, machineName: 1 << 16, helpers: 2, tails: 20, distinct: 20, declarations: 4},
		// Round 2: many declarations.
		{states: 2, events: 1, failures: 3, declarations: 128},
		{states: 2, events: 1, failures: 3, declarations: 256},
		{states: 2, events: 1, failures: 1, declarations: 256},
		{states: 2, events: 1, failures: 10, declarations: 128},
		{states: 2, events: 1, failures: 30, declarations: 64},
		{states: 3, events: 2, failures: 3, declarations: 128},
		{states: 2, events: 1, declarations: 256},
		{states: 2, events: 1, failures: 3, helpers: 1, tails: 2, declarations: 128},
		{states: 2, events: 1, failures: 3, name: 16, declarations: 128},
		{states: 2, events: 1, failures: 3, failureName: 16, declarations: 128},
		{states: 2, events: 1, failures: 3, payload: 3, declarations: 128},
		// Round 2: the random probe's fixed shapes.
		{states: 2, events: 1, failures: 300, failureName: 4096, declarations: 4},
		{states: 40, events: 2, name: 4096, variantName: 4096, helpers: 1, tails: 2},
		{states: 2, events: 1, name: 4096, payload: 30, declarations: 4},
		{states: 1, events: 1, generic: true, genericFields: 64, genericArg: 2, declarations: 16},
		{states: 400, events: 50, tails: 2, wideArm: true, declarations: 2},
		{states: 2, events: 1, machineName: 1 << 16, declarations: 64},
		{states: 3, events: 600, helpers: 1, tails: 2},
	}
	request, err := ParseGraphRequest(map[string]any{"kind": "machine"})
	if err != nil {
		t.Fatal(err)
	}
	warmMachineBoundaries(t, request)
	for i, c := range cases {
		label := fmt.Sprintf("case %d (%s)", i, c.shape)
		r, measures := c.shape.measure(t)
		if len(r.Machines) == 0 {
			t.Fatalf("%s: no plan: %v", label, machineCodes(r))
		}
		_, err := measureMachineGraphView(t, label, r, machineUnits(measures), request)
		if c.refusal == "" {
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			continue
		}
		requireGraphRefusal(t, err, c.refusal)
	}
	graphed := 0
	for i, shape := range reviewed {
		r, measures := shape.measure(t)
		if len(r.Machines) == 0 {
			continue
		}
		graphed++
		_, _ = measureMachineGraphView(t, fmt.Sprintf("reviewed shape %d (%s)", i, shape), r, machineUnits(measures), request)
	}
	if graphed == 0 {
		t.Fatal("no reviewed shape published a plan")
	}
}
