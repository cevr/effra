package compiler

import (
	"slices"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

// Every provenance witness is typed: a requirement's (ViaKind, Via) names a
// retained requirement or a declared origin, never a bare identity string.
func TestApplicationPlanTypesEveryWitness(t *testing.T) {
	plans := []*ApplicationPlan{}
	for _, source := range []string{applicationLayerSource, applicationHigherOrderSource, applicationStoredCallbackSource, applicationForeignSource} {
		_, plan := checkedApplicationPlan(t, source, GoGenerationBuild)
		plans = append(plans, plan)
	}
	r, build := checkedApplicationPlan(t, applicationModeSource, GoGenerationBuild)
	test, err := r.ApplicationPlan(GoGenerationTest)
	if err != nil {
		t.Fatal(err)
	}
	plans = append(plans, build, test)
	for _, file := range []string{"layers.ef", "callables-state.ef"} {
		_, plan := exampleApplicationPlan(t, file, GoGenerationBuild)
		plans = append(plans, plan)
	}
	for _, plan := range plans {
		witnesses := map[applicationRequirementKey]bool{}
		for _, requirement := range plan.Requirements {
			witnesses[applicationRequirementKey{requirement.Kind, requirement.Identity}] = true
		}
		for _, origin := range plan.Origins {
			if origin.Kind != OriginProviderMethod || origin.ViaKind != RequiresProvider || !plan.Requires(origin.ViaKind, origin.Via) {
				t.Fatalf("origin %+v is not a provider operation of a retained provider", origin)
			}
			witnesses[applicationRequirementKey{origin.Kind, origin.Identity}] = true
		}
		for _, requirement := range plan.Requirements {
			if (requirement.Via == "") != (requirement.ViaKind == "") {
				t.Fatalf("requirement %+v has an untyped witness", requirement)
			}
			if requirement.Via != "" && !witnesses[applicationRequirementKey{requirement.ViaKind, requirement.Via}] {
				t.Fatalf("requirement %+v names an unretained witness", requirement)
			}
		}
	}
}

func applicationView(t *testing.T, r *Result, mode string, fields map[string]any) *GraphView {
	t.Helper()
	request := map[string]any{"kind": "application", "mode": mode}
	for key, value := range fields {
		request[key] = value
	}
	return graphView(t, r, request)
}

func retainedNode(view *GraphView, kind ApplicationRequirementKind, identity string) *GraphViewNode {
	return viewNode(view, applicationNodeID(kind, identity))
}

// The application view is the plan: one node per retained requirement and
// origin, one first-witness edge into every non-root requirement.
func TestGraphViewApplicationProjectsPlanByMode(t *testing.T) {
	r := checkedGraphSource(t, applicationModeSource, "go")
	for _, mode := range []GoGenerationMode{GoGenerationBuild, GoGenerationTest} {
		plan, err := r.ApplicationPlan(mode)
		if err != nil {
			t.Fatal(err)
		}
		public := map[GoGenerationMode]string{GoGenerationBuild: "build", GoGenerationTest: "test"}[mode]
		view := applicationView(t, r, public, nil)
		incoming := map[string]int{}
		for _, edge := range viewEdges(view, "retains") {
			if !edge.Data.Effra.FirstWitness {
				t.Fatalf("retains edge %s is not a first witness", edge.ID)
			}
			incoming[edge.TargetID]++
		}
		for _, requirement := range plan.Requirements {
			node := retainedNode(view, requirement.Kind, requirement.Identity)
			if node == nil || node.Data.Effra.Retention == nil || !node.Data.Effra.Retention.Retained || node.Data.Effra.Retention.Reason != requirement.Reason {
				t.Fatalf("%s: requirement %+v is not published", mode, requirement)
			}
			want := 1
			if requirement.Via == "" {
				want = 0
			}
			if incoming[node.ID] != want {
				t.Fatalf("%s: requirement %+v has %d witnesses", mode, requirement, incoming[node.ID])
			}
		}
		closure, _ := plan.RuntimeModuleClosure()
		published := len(plan.Requirements) + len(plan.Origins)
		for _, module := range closure {
			if !plan.Requires(RequiresRuntimeModule, string(module)) {
				published++
			}
		}
		if len(view.Nodes) != published || view.Data.Effra.Selection.Mode != public {
			t.Fatalf("%s: view has %d nodes for %d plan facts", mode, len(view.Nodes), published)
		}
	}
	build := applicationView(t, r, "build", nil)
	test := applicationView(t, r, "test", nil)
	main, testGreeting := symbolIdentity(t, r, "main"), symbolIdentity(t, r, "test_greeting")
	if retainedNode(build, RequiresFunction, main) == nil || retainedNode(build, RequiresFunction, testGreeting) != nil {
		t.Fatal("build view is not rooted at the effect main")
	}
	if retainedNode(test, RequiresFunction, testGreeting) == nil || retainedNode(test, RequiresFunction, main) != nil {
		t.Fatal("test view is not rooted at the test cases")
	}
	if retainedNode(test, RequiresRuntimeModule, string(rt.RuntimeModuleSync)) == nil {
		t.Fatal("test view lacks the harness runtime module")
	}
	if build.ID == test.ID {
		t.Fatal("build and test views share an identity")
	}
}

func TestGraphViewApplicationShowsHiddenNodesAndReplacements(t *testing.T) {
	r, plan := checkedApplicationPlan(t, applicationLayerSource, GoGenerationBuild)
	view := applicationView(t, r, "build", nil)
	var hidden, public *GraphViewNode
	for _, identity := range plan.Identities(RequiresLayerNode) {
		node := retainedNode(view, RequiresLayerNode, identity)
		if node == nil || node.Data.Effra.Selection == nil || node.Data.Effra.Binding == nil {
			t.Fatalf("layer node %s lacks its checked selection", identity)
		}
		if node.Data.Effra.Selection.Public {
			public = node
		} else {
			hidden = node
		}
	}
	if hidden == nil || public == nil || hidden.Data.Effra.Retention.Reason != "hidden-layer-node" {
		t.Fatal("hidden selected layer node is not visible as hidden")
	}
	if len(hidden.Data.Effra.Selection.Replacements) != 1 || hidden.Data.Effra.Selection.Implementation != "FixtureStore" {
		t.Fatalf("effective replacement is not published: %+v", hidden.Data.Effra.Selection)
	}
	replacement := ""
	for _, edge := range viewEdges(view, "retains") {
		if edge.SourceID == hidden.ID && edge.Data.Effra.Reason == "layer-replacement" {
			replacement = edge.TargetID
		}
	}
	if replacement != applicationNodeID(RequiresProvider, providerIdentity(t, r, "FixtureStore")) {
		t.Fatalf("replacement provider witness = %q", replacement)
	}
	if retainedNode(view, RequiresProvider, providerIdentity(t, r, "LiveStore")) != nil {
		t.Fatal("replaced provider is published as retained")
	}
	layers := applicationNodeID(RequiresRuntimeModule, string(rt.RuntimeModuleLayers))
	core := applicationNodeID(RequiresRuntimeModule, string(rt.RuntimeModuleCore))
	found := false
	for _, edge := range viewEdges(view, "runtime-requires") {
		found = found || edge.SourceID == layers && edge.TargetID == core
	}
	if !found {
		t.Fatal("runtime catalog closure edge layers -> core is missing")
	}
	if len(view.Data.Effra.Facts.Types) == 0 {
		t.Fatal("layer node constructor types are not in the fact closure")
	}
	for _, edge := range viewEdges(view, "retains") {
		if source := viewNode(view, edge.SourceID); source.Data.Effra.Kind == string(OriginProviderMethod) {
			return
		}
	}
	t.Fatal("no provider operation origin witnesses a requirement")
}

func TestGraphViewApplicationShowsCallbacks(t *testing.T) {
	r, plan := checkedApplicationPlan(t, applicationStoredCallbackSource, GoGenerationBuild)
	view := applicationView(t, r, "build", nil)
	renamed := symbolIdentity(t, r, "renamed")
	if node := retainedNode(view, RequiresCallableValue, renamed); node == nil || node.Data.Effra.Retention.Reason != "function-value" {
		t.Fatal("callable value target is not published")
	}
	for _, identity := range plan.Identities(RequiresDynamicCall) {
		requirement, _ := plan.Requirement(RequiresDynamicCall, identity)
		edge := graphTupleID("edge", "retains", applicationNodeID(requirement.ViaKind, requirement.Via), applicationNodeID(RequiresDynamicCall, identity))
		if !slices.ContainsFunc(view.Edges, func(candidate GraphViewEdge) bool { return candidate.ID == edge }) {
			t.Fatalf("dynamic call %s lacks its owner witness", identity)
		}
	}
	focused := applicationView(t, r, "build", map[string]any{"focus": applicationNodeID(RequiresCallableValue, renamed), "direction": "incoming", "depth": 1})
	if len(focused.Nodes) != 2 {
		t.Fatalf("callable focus selects its single first witness, got %d nodes", len(focused.Nodes))
	}
	if !slices.Contains(view.Data.Effra.Limitations, "callable-value requirements are the conservative target set of every dynamic call") {
		t.Fatal("conservative callable limitation is not declared")
	}
}

func TestGraphViewApplicationRefusals(t *testing.T) {
	js := checkedGraphSource(t, applicationModeSource, "js")
	_, err := js.GraphView(graphRequest(t, map[string]any{"kind": "application"}))
	requireGraphRefusal(t, err, GraphRefusalTarget)
	noTests := checkedGraphSource(t, applicationLayerSource, "go")
	_, err = noTests.GraphView(graphRequest(t, map[string]any{"kind": "application", "mode": "test"}))
	requireGraphRefusal(t, err, GraphRefusalPlan)
	_, err = ParseGraphRequest(map[string]any{"kind": "application", "mode": "run"})
	requireGraphRefusal(t, err, GraphRefusalMode)
	_, err = ParseGraphRequest(map[string]any{"kind": "layers", "mode": "build"})
	requireGraphRefusal(t, err, GraphRefusalIncompatible)
	request := graphRequest(t, map[string]any{"kind": "application"})
	if request.Mode != "build" {
		t.Fatalf("application mode defaults to build, got %q", request.Mode)
	}
	collapsed := applicationView(t, noTests, "build", map[string]any{"collapse": []any{applicationNodeID(RequiresLayer, layerID("Fixture"))}})
	root := viewNode(collapsed, applicationNodeID(RequiresLayer, layerID("Fixture")))
	if root == nil || root.Data.Effra.Collapsed == nil {
		t.Fatal("application collapse follows the retains forest")
	}
}
