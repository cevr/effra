package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func checkedGraphSource(t *testing.T, source, target string) *Result {
	t.Helper()
	r := CompileAt(source, target, ".")
	if !r.Checked {
		t.Fatalf("fixture is not checked: %v", r.Diagnostics)
	}
	return r
}

func checkedGraphExample(t *testing.T, name, target string) *Result {
	t.Helper()
	file := filepath.Join("..", "..", "examples", name+".ef")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	r := CompileAt(string(raw), target, filepath.Dir(file))
	if !r.Checked {
		t.Fatalf("%s is not checked: %v", name, r.Diagnostics)
	}
	return r
}

func graphRequest(t *testing.T, fields map[string]any) GraphRequest {
	t.Helper()
	request, err := ParseGraphRequest(fields)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func graphView(t *testing.T, r *Result, fields map[string]any) *GraphView {
	t.Helper()
	view, err := r.GraphView(graphRequest(t, fields))
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func requireGraphRefusal(t *testing.T, err error, code string) {
	t.Helper()
	var refusal *GraphRefusal
	if !errors.As(err, &refusal) || refusal.Code != code {
		t.Fatalf("expected %s refusal, got %v", code, err)
	}
}

func viewNode(view *GraphView, id string) *GraphViewNode {
	for i := range view.Nodes {
		if view.Nodes[i].ID == id {
			return &view.Nodes[i]
		}
	}
	return nil
}

func viewEdges(view *GraphView, relation string) []GraphViewEdge {
	edges := []GraphViewEdge{}
	for _, edge := range view.Edges {
		if edge.Data.Effra.Relation == relation {
			edges = append(edges, edge)
		}
	}
	return edges
}

// The dependency view and the frozen legacy graph serialize one fact owner:
// identical node identities, contracts and relation occurrences.
func TestGraphViewDependencySerializesLegacyFacts(t *testing.T) {
	for _, name := range []string{"workflow", "layers", "callables-factory", "latest-task"} {
		for _, target := range []string{"go", "js"} {
			r := checkedGraphExample(t, name, target)
			legacy, err := r.Graph()
			if err != nil {
				t.Fatal(err)
			}
			view := graphView(t, r, map[string]any{"kind": "dependency"})
			if len(view.Nodes) != len(legacy.Nodes) {
				t.Fatalf("%s/%s: view has %d nodes, legacy %d", name, target, len(view.Nodes), len(legacy.Nodes))
			}
			for _, node := range legacy.Nodes {
				published := viewNode(view, node.ID)
				if published == nil || published.Data.Effra.Kind != node.Kind || published.Data.Effra.Source != node.Source || !reflect.DeepEqual(published.Data.Effra.Contract, node.Contract) {
					t.Fatalf("%s/%s: node %s differs from the legacy fact", name, target, node.ID)
				}
			}
			occurrences := map[[3]string]int{}
			for _, edge := range legacy.Edges {
				occurrences[[3]string{edge.From, edge.To, edge.Kind}]++
			}
			for _, edge := range view.Edges {
				occurrences[[3]string{edge.SourceID, edge.TargetID, edge.Data.Effra.Relation}]--
			}
			for key, count := range occurrences {
				if count != 0 {
					t.Fatalf("%s/%s: relation %v differs by %d", name, target, key, count)
				}
			}
			if !reflect.DeepEqual(view.Data.Effra.Facts.Types, nonNilTypes(legacy.Types)) {
				t.Fatalf("%s/%s: whole-kind type closure differs from the legacy closure", name, target)
			}
			if view.Data.Effra.Completeness.Scope != "kind" || view.Data.Effra.Completeness.FactNodes != len(view.Nodes) {
				t.Fatalf("%s/%s: whole-kind completeness %+v", name, target, view.Data.Effra.Completeness)
			}
		}
	}
}

func TestGraphTupleIDsAreInjective(t *testing.T) {
	pairs := [][2][]string{
		{{"a|b", "c"}, {"a", "b|c"}},
		{{"a%7Cb"}, {"a|b"}},
		{{"", "x"}, {"x", ""}},
	}
	for _, pair := range pairs {
		if graphTupleID("edge", pair[0]...) == graphTupleID("edge", pair[1]...) {
			t.Fatalf("tuple IDs collide: %q %q", pair[0], pair[1])
		}
	}
}

// Parallel relations between the same endpoints keep distinct semantic IDs;
// the IDs do not depend on traversal or insertion order.
func TestGraphViewEdgeIDsAreSemantic(t *testing.T) {
	source := `service Users { effect fn get() -> string }
impl Live for Users { effect fn get() -> string { "u" } }
effect fn main() -> string uses { Users } {
  let a = run Users.get()
  let b = run Users.get()
  a + b
}`
	r := checkedGraphSource(t, source, "go")
	view := graphView(t, r, map[string]any{"kind": "dependency", "edgeKinds": []any{"calls"}})
	calls := viewEdges(view, "calls")
	if len(calls) != 2 || calls[0].TargetID != calls[1].TargetID || calls[0].ID == calls[1].ID {
		t.Fatalf("parallel calls were merged or misidentified: %+v", calls)
	}
	for _, edge := range calls {
		at := edge.Data.Effra.Span
		want := graphTupleID("edge", "calls", edge.SourceID, edge.TargetID, "Users", fmt.Sprint(at.Offset), fmt.Sprint(at.Length))
		if edge.ID != want {
			t.Fatalf("edge ID %q is not its semantic tuple %q", edge.ID, want)
		}
	}
	again := graphView(t, checkedGraphSource(t, source, "go"), map[string]any{"kind": "dependency", "edgeKinds": []any{"calls"}})
	first, _ := json.Marshal(view)
	second, _ := json.Marshal(again)
	if string(first) != string(second) {
		t.Fatal("graph view encoding is not deterministic")
	}
}

func wideGraphSource(functions int) string {
	var source strings.Builder
	source.WriteString("service Store { effect fn get() -> string }\n")
	for i := 0; i < functions; i++ {
		fmt.Fprintf(&source, "fn f%d(value: string) -> string { value }\n", i)
	}
	source.WriteString("effect fn main() -> string uses { Store } { let seed = run Store.get()\n f0(seed) }\n")
	return source.String()
}

// A focused query selects from fact topology before materializing contracts
// or type tables, so it succeeds where the whole graph is refused.
func TestGraphViewFocusSelectsBeforeMaterialization(t *testing.T) {
	r := checkedGraphSource(t, wideGraphSource(1100), "go")
	if _, err := r.Graph(); err == nil {
		t.Fatal("whole legacy graph should exceed its node limit")
	}
	_, err := r.GraphView(graphRequest(t, map[string]any{"kind": "dependency"}))
	requireGraphRefusal(t, err, GraphRefusalNodeLimit)
	view := graphView(t, r, map[string]any{"focus": "function:main", "depth": 2, "direction": "outgoing"})
	completeness := view.Data.Effra.Completeness
	if completeness.FactNodes <= maxGraphNodes || completeness.Scope != "selection" || !completeness.Complete {
		t.Fatalf("focused completeness lost the full fact totals: %+v", completeness)
	}
	if view.Data.Effra.Usage.MaterializedNodes != len(view.Nodes) || len(view.Nodes) > 10 {
		t.Fatalf("focused view materialized %d nodes for %d published", view.Data.Effra.Usage.MaterializedNodes, len(view.Nodes))
	}
	if viewNode(view, "function:f0") == nil || viewNode(view, "service:Store") == nil || viewNode(view, "function:f1") != nil {
		t.Fatalf("focused neighborhood is wrong: %v", view.Nodes)
	}
	if view.InitialNodeID != "function:main" {
		t.Fatalf("focus is not the initial node: %q", view.InitialNodeID)
	}
}

func TestGraphViewDirectionDepthAndRelations(t *testing.T) {
	r := checkedGraphExample(t, "workflow", "go")
	outgoing := graphView(t, r, map[string]any{"focus": "function:welcome", "direction": "outgoing", "depth": 1})
	for _, edge := range outgoing.Edges {
		if viewNode(outgoing, edge.SourceID) == nil || viewNode(outgoing, edge.TargetID) == nil {
			t.Fatal("selected view published a dangling edge")
		}
	}
	if len(viewEdges(outgoing, "requires")) == 0 || viewNode(outgoing, "service:Directory") == nil {
		t.Fatal("outgoing selection lost the declared requirement")
	}
	incoming := graphView(t, r, map[string]any{"focus": "function:welcome", "direction": "incoming", "depth": 1})
	for _, node := range incoming.Nodes {
		if node.ID == "service:Directory" {
			t.Fatal("incoming selection followed an outgoing relation")
		}
	}
	requires := graphView(t, r, map[string]any{"focus": "function:welcome", "edgeKinds": []any{"requires"}, "depth": 3})
	for _, edge := range requires.Edges {
		if edge.Data.Effra.Relation != "requires" {
			t.Fatalf("relation filter published %s", edge.Data.Effra.Relation)
		}
	}
	shallow := graphView(t, r, map[string]any{"focus": "function:main", "direction": "outgoing", "depth": 1})
	if len(shallow.Data.Effra.Completeness.Frontier) == 0 {
		t.Fatal("a depth-limited selection must report its frontier")
	}
	for _, id := range shallow.Data.Effra.Completeness.Frontier {
		if node := viewNode(shallow, id); node == nil || !node.Data.Effra.Frontier {
			t.Fatalf("frontier node %s is not marked", id)
		}
	}
	zero := graphView(t, r, map[string]any{"focus": "function:main", "depth": 0})
	if len(zero.Nodes) != 1 || len(zero.Edges) != 0 {
		t.Fatalf("depth 0 selects only the focus: %d nodes", len(zero.Nodes))
	}
}

func syntheticDependencyFacts() *graphFacts {
	facts := newGraphFacts(GraphKindDependency, defaultGraphViewLimits)
	for _, id := range []string{"A", "G", "H1", "H2", "H3", "B"} {
		facts.addNode(GraphViewNode{ID: id, Label: id, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "function"}}}, dependencyContract{})
	}
	for _, edge := range [][3]string{{"G", "H1", "contains"}, {"G", "H2", "contains"}, {"G", "H3", "contains"}, {"A", "H1", "references"}, {"H2", "B", "calls"}} {
		facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", edge[2], edge[0], edge[1]), SourceID: edge[0], TargetID: edge[1], Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: edge[2]}}})
	}
	return facts
}

// The unrelated-H1/H2 counterexample: contracting G would fabricate A -> G -> B.
// Collapse keeps boundary members visible and publishes only fact edges.
func TestGraphCollapseNeverFabricatesAPath(t *testing.T) {
	facts := syntheticDependencyFacts()
	selection, err := facts.selectGraph(GraphRequest{Kind: GraphKindDependency, Collapse: []string{"G"}}, defaultGraphViewLimits)
	if err != nil {
		t.Fatal(err)
	}
	original := map[string]bool{}
	for _, fact := range facts.edges {
		original[fact.edge.ID] = true
	}
	adjacent := map[string][]string{}
	for _, index := range selection.edges {
		edge := facts.edges[index].edge
		if !original[edge.ID] || !selection.nodes[edge.SourceID] || !selection.nodes[edge.TargetID] {
			t.Fatalf("collapse published a non-fact or dangling edge %s", edge.ID)
		}
		adjacent[edge.SourceID] = append(adjacent[edge.SourceID], edge.TargetID)
	}
	reached := map[string]bool{"A": true}
	queue := []string{"A"}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, target := range adjacent[next] {
			if !reached[target] {
				reached[target] = true
				queue = append(queue, target)
			}
		}
	}
	if reached["B"] {
		t.Fatal("collapse fabricated a path from A to B")
	}
	if selection.parents["H1"] != "G" || selection.parents["H2"] != "G" || selection.nodes["H3"] {
		t.Fatalf("boundary members must stay visible under G and interior members hide: %+v", selection)
	}
	if !slices.Equal(selection.hidden["G"], []string{"H3"}) || selection.hiddenN["G"] != 1 {
		t.Fatalf("hidden membership was not recorded: %v %v", selection.hidden, selection.hiddenN)
	}
}

func TestGraphViewCollapsePublishesOnlyFactEdges(t *testing.T) {
	r := checkedGraphExample(t, "workflow", "go")
	full := graphView(t, r, map[string]any{"kind": "dependency"})
	known := map[string]bool{}
	for _, edge := range full.Edges {
		known[edge.ID] = true
	}
	collapsed := graphView(t, r, map[string]any{"kind": "dependency", "collapse": []any{"function:main"}})
	root := viewNode(collapsed, "function:main")
	if root == nil || root.Data.Effra.Collapsed == nil || len(root.Data.Effra.Collapsed.HiddenMembers) == 0 {
		t.Fatal("collapse hid no interior member")
	}
	for _, member := range root.Data.Effra.Collapsed.HiddenMembers {
		if viewNode(collapsed, member) != nil || viewNode(full, member) == nil {
			t.Fatalf("hidden member %s is published or not a fact", member)
		}
	}
	for _, edge := range collapsed.Edges {
		if !known[edge.ID] {
			t.Fatalf("collapse published summary edge %s", edge.ID)
		}
	}
	completeness := collapsed.Data.Effra.Completeness
	if completeness.SelectedNodes != len(full.Nodes) || completeness.PublishedNodes != len(collapsed.Nodes) {
		t.Fatalf("collapse accounting is wrong: %+v", completeness)
	}
	_, err := r.GraphView(graphRequest(t, map[string]any{"collapse": []any{"service:Directory"}}))
	requireGraphRefusal(t, err, GraphRefusalCollapse)
	_, err = r.GraphView(graphRequest(t, map[string]any{"collapse": []any{"function:main", "missing"}}))
	requireGraphRefusal(t, err, GraphRefusalCollapse)
}

const openLayerSource = `service Store { effect fn label() -> string }
service Account { effect fn label() -> string }
impl Memory(label: string) for Store { effect fn label() -> string { label } }
impl AccountLive for Account uses { Store } { effect fn label() -> string { run Store.label() } }
layer Accounts { Account = AccountLive }
layer Shared { Store = Memory("live") }
layer App { merge Shared, Accounts }
layer Fixture { merge App; replace Store = Memory("fixture") }
effect fn main() -> string { run Account.label().provide(Fixture) }
`

// The layer view shows open construction inputs as explicit boundary nodes
// and each plan's effective replacement on its selection, keeping a shared
// binding one canonical node.
func TestGraphViewLayersShowOpenInputsAndReplacements(t *testing.T) {
	r := checkedGraphSource(t, openLayerSource, "go")
	view := graphView(t, r, map[string]any{"kind": "layers"})
	accounts := layerID("Accounts")
	input := graphTupleID("layer-input", accounts, "Store")
	if node := viewNode(view, input); node == nil || node.Data.Effra.Kind != "layer-input" {
		t.Fatal("open layer input is not an explicit boundary node")
	}
	consumed := false
	for _, edge := range viewEdges(view, "consumes-input") {
		consumed = consumed || edge.TargetID == input && strings.HasPrefix(edge.SourceID, accounts+":binding:Account")
	}
	if !consumed || len(viewEdges(view, "requires-input")) != 1 {
		t.Fatal("open input lacks its plan and consumer relations")
	}
	store := ""
	for _, node := range view.Nodes {
		if node.Data.Effra.Kind == "layer-binding" && node.Data.Effra.Binding.Service == "Store" {
			if store != "" {
				t.Fatal("shared binding is not one canonical node")
			}
			store = node.ID
		}
	}
	selections := map[string]*GraphLayerSelectFacts{}
	for _, edge := range viewEdges(view, "selects") {
		if edge.TargetID == store {
			selections[edge.SourceID] = edge.Data.Effra.Selection
		}
	}
	fixture, shared := selections[layerID("Fixture")], selections[layerID("Shared")]
	if fixture == nil || shared == nil || !fixture.Public || !shared.Public || len(fixture.Replacements) != 1 || len(shared.Replacements) != 0 {
		t.Fatalf("per-plan selection facts are wrong: fixture=%+v shared=%+v", fixture, shared)
	}
	if fixture.Arguments[0].Span == shared.Arguments[0].Span {
		t.Fatal("effective replacement configuration was not published")
	}
	replaced := false
	for _, edge := range viewEdges(view, "replaces") {
		replaced = replaced || edge.SourceID == fixture.Replacements[0].ID && edge.TargetID == store && edge.Data.Effra.Plan == layerID("Fixture")
	}
	if !replaced || viewNode(view, fixture.Replacements[0].ID) == nil {
		t.Fatal("replacement site is not a published fact")
	}
	if len(view.Data.Effra.Facts.Types) == 0 {
		t.Fatal("constructor and configuration types are not in the fact closure")
	}
	_, err := r.GraphView(graphRequest(t, map[string]any{"kind": "layers", "focus": "missing"}))
	requireGraphRefusal(t, err, GraphRefusalFocus)
}

func TestGraphRequestRefusals(t *testing.T) {
	for _, test := range []struct {
		fields map[string]any
		code   string
	}{
		{map[string]any{"kind": "graph"}, GraphRefusalKind},
		{map[string]any{"kind": "machine"}, GraphRefusalKindUnavailable},
		{map[string]any{"kind": "actor"}, GraphRefusalKindUnavailable},
		{map[string]any{"kind": 3}, GraphRefusalInvocation},
		{map[string]any{"format": "svg"}, GraphRefusalFormat},
		{map[string]any{"format": "html"}, GraphRefusalFormatUnavailable},
		{map[string]any{"depth": 1}, GraphRefusalIncompatible},
		{map[string]any{"direction": "both"}, GraphRefusalIncompatible},
		{map[string]any{"focus": "x", "direction": "sideways"}, GraphRefusalInvocation},
		{map[string]any{"focus": "x", "depth": -1}, GraphRefusalInvocation},
		{map[string]any{"focus": "x", "depth": 1.5}, GraphRefusalInvocation},
		{map[string]any{"focus": "x", "depth": "2"}, GraphRefusalInvocation},
		{map[string]any{"focus": "x", "depth": 65}, GraphRefusalDepthLimit},
		{map[string]any{"focus": ""}, GraphRefusalInvocation},
		{map[string]any{"edgeKinds": []any{"retains"}}, GraphRefusalEdgeKind},
		{map[string]any{"edgeKinds": []any{}}, GraphRefusalInvocation},
		{map[string]any{"edgeKinds": "calls"}, GraphRefusalInvocation},
		{map[string]any{"kind": "layers", "collapse": []any{"x"}}, GraphRefusalCollapse},
		{map[string]any{"mode": "build"}, GraphRefusalIncompatible},
		{map[string]any{"target": "go"}, GraphRefusalInvocation},
	} {
		_, err := ParseGraphRequest(test.fields)
		requireGraphRefusal(t, err, test.code)
	}
	unchecked := CompileAt(`fn nope() -> missing {}`, "go", ".")
	_, err := unchecked.GraphView(graphRequest(t, map[string]any{"kind": "dependency"}))
	requireGraphRefusal(t, err, GraphRefusalUnchecked)
	r := checkedGraphExample(t, "workflow", "go")
	_, err = r.GraphView(graphRequest(t, map[string]any{"focus": "function:absent"}))
	requireGraphRefusal(t, err, GraphRefusalFocus)
	// Focus is an exact canonical ID: a display name or renderer ID never
	// resolves to a node.
	for _, alias := range []string{"welcome", "n0", "function: welcome"} {
		_, err = r.GraphView(graphRequest(t, map[string]any{"focus": alias}))
		requireGraphRefusal(t, err, GraphRefusalFocus)
	}
}

// Each declared budget admits its exact usage and refuses one unit less.
func TestGraphViewExactLimits(t *testing.T) {
	r := checkedGraphExample(t, "workflow", "go")
	base := graphRequest(t, map[string]any{"focus": "function:main", "depth": 2})
	view, err := r.GraphView(base)
	if err != nil {
		t.Fatal(err)
	}
	usage := view.Data.Effra.Usage
	for _, test := range []struct {
		name  string
		limit func(*GraphViewLimits)
		less  func(*GraphViewLimits)
		code  string
	}{
		{"nodes", func(l *GraphViewLimits) { l.Nodes = usage.Nodes }, func(l *GraphViewLimits) { l.Nodes = usage.Nodes - 1 }, GraphRefusalNodeLimit},
		{"edges", func(l *GraphViewLimits) { l.Edges = usage.Edges }, func(l *GraphViewLimits) { l.Edges = usage.Edges - 1 }, GraphRefusalEdgeLimit},
		{"work", func(l *GraphViewLimits) { l.Work = usage.Work }, func(l *GraphViewLimits) { l.Work = usage.Work - 1 }, GraphRefusalWorkLimit},
		{"depth", func(l *GraphViewLimits) { l.Depth = 2 }, func(l *GraphViewLimits) { l.Depth = 1 }, GraphRefusalDepthLimit},
	} {
		exact, less := defaultGraphViewLimits, defaultGraphViewLimits
		test.limit(&exact)
		test.less(&less)
		request := base
		request.limits = &exact
		if _, err := r.GraphView(request); err != nil {
			t.Fatalf("%s exact limit refused: %v", test.name, err)
		}
		request.limits = &less
		_, err := r.GraphView(request)
		requireGraphRefusal(t, err, test.code)
	}
	// The view publishes its own limits, so the exact response budget is the
	// fixed point of encoding the view under that budget.
	exact, less := defaultGraphViewLimits, defaultGraphViewLimits
	request := base
	for exact.ResponseBytes = 0; ; {
		request.limits = &exact
		fitted := view
		if exact.ResponseBytes > 0 {
			if fitted, err = r.GraphView(request); err != nil {
				t.Fatalf("response exact limit refused: %v", err)
			}
		}
		encoded, err := json.Marshal(fitted)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) == exact.ResponseBytes {
			break
		}
		exact.ResponseBytes = len(encoded)
	}
	less.ResponseBytes = exact.ResponseBytes - 1
	request.limits = &less
	_, err = r.GraphView(request)
	requireGraphRefusal(t, err, GraphRefusalResponseLimit)
	typed := checkedGraphExample(t, "workflow", "go")
	typed.TypeProjectionLimits = defaultProjectionLimits
	typed.TypeProjectionLimits.Nodes = usage.TypeProjection.Nodes
	if _, err := typed.GraphView(base); err != nil {
		t.Fatalf("type exact limit refused: %v", err)
	}
	typed.TypeProjectionLimits.Nodes = usage.TypeProjection.Nodes - 1
	_, err = typed.GraphView(base)
	requireGraphRefusal(t, err, GraphRefusalTypeLimit)
}

// The validator is a causal control: removing one referenced fact, or
// publishing one dangling relation, makes a valid view fail.
func TestValidateGraphViewRejectsBrokenClosure(t *testing.T) {
	r := checkedGraphExample(t, "workflow", "go")
	valid := graphView(t, r, map[string]any{"focus": "function:welcome", "depth": 2})
	if err := ValidateGraphView(valid); err != nil {
		t.Fatal(err)
	}
	clone := func() *GraphView {
		encoded, _ := json.Marshal(valid)
		var copy GraphView
		if err := json.Unmarshal(encoded, &copy); err != nil {
			t.Fatal(err)
		}
		return &copy
	}
	referencedType, referencedRow := "", ""
	for _, node := range valid.Nodes {
		if contract := node.Data.Effra.Contract; contract != nil && referencedType == "" {
			referencedType = contract.Type.ID
		}
	}
	for _, node := range valid.Data.Effra.Facts.Types {
		if referencedRow == "" && node.FailureRow != "" {
			referencedRow = node.FailureRow
		}
	}
	if referencedType == "" || referencedRow == "" {
		t.Fatal("fixture lacks a referenced type and row")
	}
	without := func(v *GraphView, id string) {
		v.Data.Effra.Facts.Types = slices.DeleteFunc(v.Data.Effra.Facts.Types, func(node TypeNode) bool { return node.ID == id })
		v.Data.Effra.Facts.Rows = slices.DeleteFunc(v.Data.Effra.Facts.Rows, func(row RowNode) bool { return row.ID == id })
	}
	for name, mutate := range map[string]func(*GraphView){
		"missing type":     func(v *GraphView) { without(v, referencedType) },
		"missing row":      func(v *GraphView) { without(v, referencedRow) },
		"missing decl":     func(v *GraphView) { v.Data.Effra.Facts.Declarations = nil },
		"missing source":   func(v *GraphView) { v.Data.Effra.Facts.Sources = nil },
		"dangling edge":    func(v *GraphView) { v.Edges[0].TargetID = "missing" },
		"duplicate node":   func(v *GraphView) { v.Nodes = append(v.Nodes, v.Nodes[0]) },
		"duplicate edge":   func(v *GraphView) { v.Edges = append(v.Edges, v.Edges[0]) },
		"unknown parent":   func(v *GraphView) { v.Nodes[0].ParentID = "missing" },
		"cyclic parents":   func(v *GraphView) { v.Nodes[0].ParentID, v.Nodes[1].ParentID = v.Nodes[1].ID, v.Nodes[0].ID },
		"unknown initial":  func(v *GraphView) { v.InitialNodeID = "missing" },
		"unknown port":     func(v *GraphView) { v.Edges[0].SourcePort = "out" },
		"foreign relation": func(v *GraphView) { v.Edges[0].Data.Effra.Relation = "selects" },
		"hidden published": func(v *GraphView) {
			v.Nodes[0].Data.Effra.Collapsed = &GraphCollapsedFacts{HiddenMembers: []string{v.Nodes[1].ID}}
		},
		"untyped node":      func(v *GraphView) { v.Nodes[0].Type = "vertex" },
		"wrong view header": func(v *GraphView) { v.Data.Effra.GraphViewVersion = 2 },
	} {
		broken := clone()
		mutate(broken)
		if err := ValidateGraphView(broken); err == nil {
			t.Fatalf("validator accepted %s", name)
		}
	}
}
