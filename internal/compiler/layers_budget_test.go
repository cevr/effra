package compiler

import (
	"fmt"
	"strings"
	"testing"
)

func repeatedLayerSource(merges, plans int) string {
	var source strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&source, "service S%d { effect fn value() -> string } impl P%d for S%d { effect fn value() -> string { \"value\" } }\n", i, i, i)
	}
	source.WriteString("layer Shared {\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&source, "S%d = P%d\n", i, i)
	}
	source.WriteString("}\n")
	for i := 0; i < plans; i++ {
		fmt.Fprintf(&source, "layer App%d {\n", i)
		for j := 0; j < merges; j++ {
			source.WriteString("merge Shared\n")
		}
		source.WriteString("}\n")
	}
	return source.String()
}

func TestLayerAssemblyBudgetRejectsRepeatedMetadataBeforeExpansion(t *testing.T) {
	// Find the real default compile boundary, keeping the nominal graph fixed.
	// One additional reference must cause a source refusal, rather than a
	// partially checked graph or a serialization-time allocation explosion.
	low, high := 1, 1000
	if r := Compile(repeatedLayerSource(low, 1)); !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if r := Compile(repeatedLayerSource(high, 1)); r.Checked || !hasCode(r, "EF133") {
		t.Fatal("over-budget control unexpectedly admitted", r.Diagnostics)
	}
	for high-low > 1 {
		middle := (low + high) / 2
		if Compile(repeatedLayerSource(middle, 1)).Checked {
			low = middle
		} else {
			high = middle
		}
	}
	under := Compile(repeatedLayerSource(low, 1))
	if !under.Checked {
		t.Fatal(under.Diagnostics)
	}
	plan := under.FindLayer("App0")
	if len(plan.Nodes) != 50 || len(plan.Nodes[0].Occurrences) != low {
		t.Fatalf("under-budget graph lost identities/provenance: %d/%d", len(plan.Nodes), len(plan.Nodes[0].Occurrences))
	}
	over := Compile(repeatedLayerSource(high, 1))
	if over.Checked || !hasCode(over, "EF133") || over.projector.layerBudget.used > maxLayerAssemblyMetadata {
		t.Fatalf("over-budget admission: %+v", over.Diagnostics)
	}
	response := over.CheckResponse()
	if response["typeProjectionComplete"] != false || response["layers"] != nil {
		t.Fatal("refused graph published authoritative metadata", response)
	}
	if _, err := over.LayerInspection("Shared", "test.ef"); err == nil {
		t.Fatal("refused program allowed inspection")
	}
	t.Logf("50-node graph: %d repeated references accepted; %d refused; metadata units before refusal %d", low, high, over.projector.layerBudget.used)
}

func TestLayerAssemblyBudgetIsSharedAcrossDeclarations(t *testing.T) {
	if r := Compile(repeatedLayerSource(100, 1)); !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	r := Compile(repeatedLayerSource(100, 20))
	if r.Checked || !hasCode(r, "EF133") || r.projector.layerBudget.used > maxLayerAssemblyMetadata {
		t.Fatalf("cumulative layer metadata escaped budget: %+v", r.Diagnostics)
	}
}

func TestLayerCycleRelatedLocationsAreBounded(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&source, "service S%d { effect fn value() -> string } impl P%d for S%d uses {S%d} { effect fn value() -> string { \"x\" } }\n", i, i, i, (i+1)%100)
	}
	source.WriteString("layer Cyclic {\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&source, "S%d = P%d\n", i, i)
	}
	source.WriteString("}\n")
	r := Compile(source.String())
	if r.Checked || !hasCode(r, "EF132") {
		t.Fatal(r.Diagnostics)
	}
	for _, diagnostic := range r.Diagnostics {
		if len(diagnostic.Related) > maxLayerRelatedLocations {
			t.Fatal("unbounded related source paths", len(diagnostic.Related))
		}
	}
}
