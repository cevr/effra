package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// Derived codec directions are checked module functions. Their member syntax
// must not make the dependency collector drop the call fact, for either the
// direct or pipe invocation form.
func TestGraphViewDependencyIncludesCheckedDerivedCodecCalls(t *testing.T) {
	source := `import Json "effra/json"
record Parcel { id: i64, label: string }
derive parcelJson = Json.codec<Parcel>(maxBodyBytes: 256, maxDepth: 1)
effect fn normalize(value: Parcel) -> Parcel { value }
effect fn direct(body: string) -> Parcel raises { JsonDecodeFailure } {
  run parcelJson.decode(body)
}
effect fn transfer(body: string) -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
  let decoded = run body |> parcelJson.decode()
  let normalized = run decoded |> normalize()
  run normalized |> parcelJson.encode()
}
effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
  let decoded = run direct("{\"id\":\"1\",\"label\":\"a\"}")
  run transfer("{\"id\":\"1\",\"label\":\"a\"}")
}`
	want := map[string]int{
		"function:direct":            1,
		"function:transfer":          1,
		"function:normalize":         1,
		"function:parcelJson.decode": 2,
		"function:parcelJson.encode": 1,
	}
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := checkedGraphSource(t, source, target)
			view := graphView(t, r, map[string]any{"kind": "dependency", "edgeKinds": []any{"calls"}})
			counts := map[string]int{}
			for _, edge := range viewEdges(view, "calls") {
				if viewNode(view, edge.SourceID) == nil || viewNode(view, edge.TargetID) == nil {
					t.Fatalf("call edge has an unresolved endpoint: %+v", edge)
				}
				counts[edge.TargetID]++
			}
			for target, count := range want {
				if counts[target] != count {
					t.Fatalf("call topology for %s: got %d occurrences, want %d (all=%v)", target, counts[target], count, counts)
				}
			}
			for _, target := range []string{"function:parcelJson.decode", "function:parcelJson.encode"} {
				node := viewNode(view, target)
				if node == nil || node.Data.Effra.Contract == nil || node.Data.Effra.Source == "" || node.Data.Effra.Span == nil {
					t.Fatalf("derived codec node lost checked contract/source/span closure: %+v", node)
				}
			}
		})
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
// or type tables, so it succeeds where the whole graph is refused. Focus
// bounds materialization, not enumeration: the whole topology is still
// enumerated and charged against the work limit.
func TestGraphViewFocusBoundsMaterializationNotEnumeration(t *testing.T) {
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
	usage := view.Data.Effra.Usage
	if usage.Work < completeness.FactNodes+completeness.FactEdges {
		t.Fatalf("focused work %d does not cover enumerating %d fact nodes and %d fact edges", usage.Work, completeness.FactNodes, completeness.FactEdges)
	}
	limits := defaultGraphViewLimits
	limits.Work = usage.Work
	request := graphRequest(t, map[string]any{"focus": "function:main", "depth": 2, "direction": "outgoing"})
	request.limits = &limits
	if _, err := r.GraphView(request); err != nil {
		t.Fatalf("focused view refused at its exact work: %v", err)
	}
	limits.Work--
	_, err = r.GraphView(request)
	requireGraphRefusal(t, err, GraphRefusalWorkLimit)
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
		// Every integer-valued depth meets the hop limit before narrowing,
		// however it arrives: an int, an MCP float64 or a JSON number.
		{map[string]any{"focus": "x", "depth": 3000000000}, GraphRefusalDepthLimit},
		{map[string]any{"focus": "x", "depth": float64(3000000000)}, GraphRefusalDepthLimit},
		{map[string]any{"focus": "x", "depth": 1e300}, GraphRefusalDepthLimit},
		{map[string]any{"focus": "x", "depth": json.Number("99999999999999999999")}, GraphRefusalDepthLimit},
		{map[string]any{"focus": "x", "depth": math.Inf(1)}, GraphRefusalInvocation},
		{map[string]any{"focus": "x", "depth": math.NaN()}, GraphRefusalInvocation},
		{map[string]any{"focus": "x", "depth": -1e300}, GraphRefusalInvocation},
		{map[string]any{"focus": "x", "depth": json.Number("2.5")}, GraphRefusalInvocation},
		{map[string]any{"focus": "x", "depth": json.Number("1e400")}, GraphRefusalInvocation},
		{map[string]any{"focus": ""}, GraphRefusalInvocation},
		{map[string]any{"edgeKinds": []any{"retains"}}, GraphRefusalEdgeKind},
		{map[string]any{"edgeKinds": []any{}}, GraphRefusalInvocation},
		{map[string]any{"edgeKinds": "calls"}, GraphRefusalInvocation},
		{map[string]any{"kind": "layers", "collapse": []any{"x"}}, GraphRefusalIncompatible},
		{map[string]any{"mode": "build"}, GraphRefusalIncompatible},
		{map[string]any{"target": "go"}, GraphRefusalInvocation},
	} {
		t.Run(fmt.Sprintf("%v", test.fields), func(t *testing.T) {
			_, err := ParseGraphRequest(test.fields)
			requireGraphRefusal(t, err, test.code)
		})
	}
	// A refused depth is printed as the integer it decoded to, never in
	// exponent form, whichever transport carried it.
	for _, depth := range []any{3000000000, float64(3000000000), json.Number("3000000000"), json.Number("3e9")} {
		_, err := ParseGraphRequest(map[string]any{"focus": "x", "depth": depth})
		requireGraphRefusal(t, err, GraphRefusalDepthLimit)
		if want := "EFGRAPH_DEPTH_LIMIT: depth 3000000000 exceeds the 64-hop limit"; err.Error() != want {
			t.Fatalf("depth %#v refusal is %q, want %q", depth, err, want)
		}
	}
	for _, raw := range []string{`1e400`, `{"depth":1e400}`, `2.0`} {
		decoded, err := DecodeGraphJSON([]byte(raw))
		if err != nil {
			t.Fatalf("%s did not decode: %v", raw, err)
		}
		if fields, ok := decoded.(map[string]any); ok {
			decoded = fields["depth"]
		}
		if _, ok := decoded.(json.Number); !ok {
			t.Fatalf("%s decoded to %T, not json.Number", raw, decoded)
		}
	}
	for _, raw := range []string{`2 3`, `{"depth":2}x`, ``} {
		if _, err := DecodeGraphJSON([]byte(raw)); err == nil {
			t.Fatalf("%q decoded as one JSON value", raw)
		}
	}
	for _, depth := range []any{2, 2.0, json.Number("2"), json.Number("2.0")} {
		request, err := ParseGraphRequest(map[string]any{"focus": "x", "depth": depth})
		if err != nil || request.Depth == nil || *request.Depth != 2 {
			t.Fatalf("integer-valued depth %#v was not admitted as 2: %v", depth, err)
		}
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
		"stray hidden frontier": func(v *GraphView) {
			v.Nodes[0].Data.Effra.Collapsed = &GraphCollapsedFacts{HiddenMembers: []string{"missing"}, HiddenFrontier: []string{"other"}}
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
	// Closure nodes are untraversed references: a closure node with a
	// relation, on the frontier, or miscounted makes a valid view fail.
	layers := graphView(t, checkedGraphSource(t, openLayerSource, "go"), map[string]any{"kind": "layers", "focus": layerID("Fixture"), "direction": "outgoing", "depth": 1})
	if err := ValidateGraphView(layers); err != nil {
		t.Fatal(err)
	}
	closure := slices.IndexFunc(layers.Nodes, func(node GraphViewNode) bool { return node.Data.Effra.Closure })
	if closure < 0 {
		t.Fatal("fixture has no closure node")
	}
	for name, mutate := range map[string]func(*GraphView){
		"closure node with a relation": func(v *GraphView) {
			endpoint := viewNode(v, v.Edges[0].SourceID)
			endpoint.Data.Effra.Closure = true
			v.Data.Effra.Completeness.ClosureNodes++
		},
		"closure node marked frontier": func(v *GraphView) { v.Nodes[closure].Data.Effra.Frontier = true },
		"closure node listed on the frontier": func(v *GraphView) {
			v.Data.Effra.Completeness.Frontier = append(v.Data.Effra.Completeness.Frontier, v.Nodes[closure].ID)
		},
		"closure count above the marked nodes": func(v *GraphView) { v.Data.Effra.Completeness.ClosureNodes++ },
		"closure mark without its count":       func(v *GraphView) { v.Nodes[closure].Data.Effra.Closure = false },
	} {
		encoded, _ := json.Marshal(layers)
		var broken GraphView
		if err := json.Unmarshal(encoded, &broken); err != nil {
			t.Fatal(err)
		}
		mutate(&broken)
		if err := ValidateGraphView(&broken); err == nil {
			t.Errorf("validator accepted %s", name)
		}
	}
}

func layerBindingNode(t *testing.T, view *GraphView, plan, service string) string {
	t.Helper()
	for _, node := range view.Nodes {
		if node.Data.Effra.Kind == "layer-binding" && node.Data.Effra.Binding.Service == service && strings.HasPrefix(node.ID, plan+":binding:") {
			return node.ID
		}
	}
	t.Fatalf("no %s binding of %s", service, plan)
	return ""
}

// encodedNodeReferences lists every string in encoded metadata that is the
// ID of a fact node: the references a consumer would try to resolve.
func encodedNodeReferences(value any, facts map[string]bool) []string {
	references := []string{}
	switch value := value.(type) {
	case string:
		if facts[value] {
			references = append(references, value)
		}
	case []any:
		for _, item := range value {
			references = append(references, encodedNodeReferences(item, facts)...)
		}
	case map[string]any:
		for _, item := range value {
			references = append(references, encodedNodeReferences(item, facts)...)
		}
	}
	return references
}

// requireReferenceClosure checks that a view publishes every node its edges
// name in metadata, that each closure node is such a reference outside the
// traversal (no incident edge, never frontier), and that the accounting
// separates closure from selection. The expected references come from the
// encoded edge metadata, not from the projector's reference helper: any
// string in an edge's data that is the ID of a node of the kind's whole view
// names that node, so a reference kind the helper omits still fails here.
func requireReferenceClosure(t *testing.T, whole, view *GraphView) []string {
	t.Helper()
	facts := map[string]bool{}
	for _, node := range whole.Nodes {
		facts[node.ID] = true
	}
	referenced, incident := map[string]bool{}, map[string]bool{}
	for _, edge := range view.Edges {
		incident[edge.SourceID], incident[edge.TargetID] = true, true
		encoded, err := json.Marshal(edge.Data)
		if err != nil {
			t.Fatal(err)
		}
		var data any
		if err := json.Unmarshal(encoded, &data); err != nil {
			t.Fatal(err)
		}
		for _, id := range encodedNodeReferences(data, facts) {
			if viewNode(view, id) == nil {
				t.Fatalf("edge %s references unpublished node %s", edge.ID, id)
			}
			referenced[id] = true
		}
	}
	closure := []string{}
	for _, node := range view.Nodes {
		if !node.Data.Effra.Closure {
			continue
		}
		if !referenced[node.ID] || incident[node.ID] || node.Data.Effra.Frontier || slices.Contains(view.Data.Effra.Completeness.Frontier, node.ID) {
			t.Fatalf("closure node %s is not an untraversed reference", node.ID)
		}
		closure = append(closure, node.ID)
	}
	completeness := view.Data.Effra.Completeness
	if completeness.ClosureNodes != len(closure) || completeness.PublishedNodes != len(view.Nodes) || completeness.SelectedNodes != len(view.Nodes)-len(closure) {
		t.Fatalf("closure accounting is wrong: %+v with %d closure nodes", completeness, len(closure))
	}
	return closure
}

// Focused layers views select edges whose metadata names plans and
// replacement sites outside the traversal. Those nodes are published as
// closure, so the views validate without widening the requested boundary.
func TestGraphViewFocusedLayersPublishReferencedClosure(t *testing.T) {
	r := checkedGraphExample(t, "layers", "go")
	whole := graphView(t, r, map[string]any{"kind": "layers"})
	account := layerBindingNode(t, whole, layerID("Accounts"), "Account")
	replacement := ""
	for _, node := range whole.Nodes {
		if node.Data.Effra.Kind == "layer-replacement" && strings.HasPrefix(node.ID, layerID("Fixture")+":replace:") {
			replacement = node.ID
		}
	}
	for _, test := range []struct {
		name    string
		fields  map[string]any
		closure string
	}{
		{"replacement", map[string]any{"focus": layerID("Fixture"), "direction": "outgoing", "depth": 1}, replacement},
		{"plan-qualified depends-on", map[string]any{"focus": account, "direction": "outgoing", "depth": 1, "edgeKinds": []any{"depends-on"}}, layerID("Accounts")},
		{"binding neighborhood", map[string]any{"focus": account, "depth": 1}, replacement},
	} {
		test.fields["kind"] = "layers"
		view, err := r.GraphView(graphRequest(t, test.fields))
		if err != nil {
			t.Fatalf("%s: focused layers view refused: %v", test.name, err)
		}
		closure := requireReferenceClosure(t, whole, view)
		if !slices.Contains(closure, test.closure) {
			t.Fatalf("%s: %s is not published as closure: %v", test.name, test.closure, closure)
		}
	}
}

// Every focused view of every kind on every checked example validates: each
// node as the focus, in each direction, over every relation, and for the
// kinds whose edges name nodes in metadata, over each single relation.
func TestGraphViewFocusedViewsValidateOnEveryExample(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.ef")
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	focused := 0
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{"go", "js"} {
			r := CompileAt(string(raw), target, filepath.Dir(file))
			if !r.Checked {
				continue
			}
			for _, kind := range []GraphKind{GraphKindDependency, GraphKindLayers, GraphKindApplication} {
				whole, err := r.GraphView(graphRequest(t, map[string]any{"kind": string(kind)}))
				if err != nil {
					continue
				}
				relations := [][]any{nil}
				for _, relation := range graphKindRelations[kind] {
					if kind != GraphKindDependency {
						relations = append(relations, []any{relation})
					}
				}
				for _, node := range whole.Nodes {
					for _, direction := range []string{"outgoing", "incoming", "both"} {
						for _, edgeKinds := range relations {
							fields := map[string]any{"kind": string(kind), "focus": node.ID, "direction": direction, "depth": 1}
							if edgeKinds != nil {
								fields["edgeKinds"] = edgeKinds
							}
							view, err := r.GraphView(graphRequest(t, fields))
							if err != nil {
								t.Fatalf("%s %s %v: %v", filepath.Base(file), target, fields, err)
							}
							requireReferenceClosure(t, whole, view)
							focused++
						}
					}
				}
			}
		}
	}
	if focused == 0 {
		t.Fatal("no focused view was checked")
	}
}

// Collapse never erases a depth cut: a root whose hidden members were on
// the frontier stands on the frontier for them and lists them.
func TestGraphViewCollapseKeepsTheDepthFrontier(t *testing.T) {
	r := checkedGraphExample(t, "workflow", "go")
	fields := map[string]any{"focus": "function:main", "depth": 1, "direction": "outgoing"}
	plain := graphView(t, r, fields)
	if len(plain.Data.Effra.Completeness.Frontier) == 0 {
		t.Fatal("fixture has no depth frontier")
	}
	fields["collapse"] = []any{"function:main"}
	collapsed := graphView(t, r, fields)
	root := viewNode(collapsed, "function:main")
	if root == nil || root.Data.Effra.Collapsed == nil || len(root.Data.Effra.Collapsed.HiddenMembers) == 0 {
		t.Fatal("collapse hid no frontier member")
	}
	frontier := collapsed.Data.Effra.Completeness.Frontier
	for _, id := range plain.Data.Effra.Completeness.Frontier {
		switch {
		case viewNode(collapsed, id) != nil:
			if !slices.Contains(frontier, id) {
				t.Fatalf("visible frontier member %s left the frontier", id)
			}
		case !slices.Contains(root.Data.Effra.Collapsed.HiddenFrontier, id) || !slices.Contains(root.Data.Effra.Collapsed.HiddenMembers, id):
			t.Fatalf("hidden frontier member %s is not listed on its root", id)
		}
	}
	if !slices.Contains(frontier, root.ID) || !root.Data.Effra.Frontier {
		t.Fatalf("collapse root does not stand on the frontier for its hidden members: %v", frontier)
	}
	for _, id := range frontier {
		if node := viewNode(collapsed, id); node == nil || !node.Data.Effra.Frontier {
			t.Fatalf("frontier node %s is not a published frontier node", id)
		}
	}
}
