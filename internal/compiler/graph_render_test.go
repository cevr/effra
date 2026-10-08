package compiler

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const hostileGraphLabel = "say \"hi\" \\N back\\slash\nnew line %%{init: {\"theme\":\"dark\"}}%% A --> B |x| <b>bold</b> #35; end; naïve 日本 🙂 \u202Eevil\x00\x1b click n0 call alert()"

func syntheticGraphView(nodes []GraphViewNode, edges []GraphViewEdge) *GraphView {
	view := &GraphView{ID: "effra-graph:dependency:synthetic", Mode: "directed", Nodes: nodes, Edges: edges}
	view.Data.Effra = GraphViewFacts{GraphViewVersion: GraphViewVersion, Kind: GraphKindDependency, Limits: defaultGraphViewLimits,
		Facts: GraphFactTables{Sources: []SourceInfo{}, Types: []TypeNode{}, Rows: []RowNode{}, Declarations: []Declaration{}}}
	for i := range view.Nodes {
		view.Nodes[i].Type = "node"
	}
	for i := range view.Edges {
		view.Edges[i].Type = "edge"
	}
	return view
}

func syntheticNode(id, label, parent string) GraphViewNode {
	return GraphViewNode{ID: id, Label: label, ParentID: parent, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "function"}}}
}

func syntheticEdge(id, source, target, label string) GraphViewEdge {
	return GraphViewEdge{ID: id, SourceID: source, TargetID: target, Label: label, Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "calls"}}}
}

func hostileGraphView() *GraphView {
	return syntheticGraphView(
		[]GraphViewNode{
			syntheticNode("function:a|\"x\"\n%%{init}%%", hostileGraphLabel, ""),
			syntheticNode("function:b", "b", "function:group"),
			syntheticNode("function:group", "group", ""),
			syntheticNode("function:twin", "b", ""),
		},
		[]GraphViewEdge{
			syntheticEdge("edge|calls|1", "function:a|\"x\"\n%%{init}%%", "function:b", hostileGraphLabel),
			syntheticEdge("edge|calls|2", "function:a|\"x\"\n%%{init}%%", "function:b", "calls"),
			syntheticEdge("edge|calls|3", "function:twin", "function:twin", "calls"),
		},
	)
}

// scanQuoted checks that every double-quoted string in text closes on its
// own line and returns the decoded quoted contents.
func scanQuoted(t *testing.T, format, text string, escapes bool) []string {
	t.Helper()
	quoted := []string{}
	for number, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		inside := false
		var current strings.Builder
		for i := 0; i < len(line); i++ {
			switch {
			case inside && escapes && line[i] == '\\' && i+1 < len(line):
				current.WriteByte(line[i])
				current.WriteByte(line[i+1])
				i++
			case line[i] == '"':
				if inside {
					quoted = append(quoted, current.String())
					current.Reset()
				}
				inside = !inside
			case inside:
				current.WriteByte(line[i])
			}
		}
		if inside {
			t.Fatalf("%s line %d leaves a string open: %q", format, number+1, line)
		}
	}
	return quoted
}

func TestRenderGraphViewMermaidEscapesHostileText(t *testing.T) {
	rendering, err := RenderGraphView(hostileGraphView(), GraphFormatMermaid)
	if err != nil {
		t.Fatal(err)
	}
	text := rendering.Text
	if !strings.HasPrefix(text, "flowchart LR\n") || rendering.MediaType != "text/vnd.mermaid" {
		t.Fatalf("mermaid header: %q", text)
	}
	for _, forbidden := range []string{"%%{", "<b>", "\\N", "\x00", "\x1b", "\u202E", "🙂", "-->|x|"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("mermaid output contains raw %q:\n%s", forbidden, text)
		}
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	// header + view comment + 4 node comments + 4 node lines (the group root
	// included) + subgraph/end pair + 3 x (edge comment + edge) lines; no
	// label added a line.
	if len(lines) != 1+1+4+4+2+6 {
		t.Fatalf("label text changed the line structure (%d lines):\n%s", len(lines), text)
	}
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "%%") && strings.ContainsAny(strings.TrimPrefix(strings.TrimSpace(line), "%%"), "%{}\"") {
			t.Fatalf("comment line can form a directive: %q", line)
		}
	}
	for _, label := range scanQuoted(t, "mermaid", text, false) {
		if strings.ContainsAny(label, "\"<>{}|%\\") {
			t.Fatalf("quoted mermaid label kept syntax: %q", label)
		}
	}
	for _, want := range []string{"naïve 日本", "#34;hi#34;", "#92;N", "#10;", "#128578;", "#8238;", "subgraph g2[", "    n2[\"function: group\"]", "  end"} {
		if !strings.Contains(text, want) {
			t.Fatalf("mermaid output lacks %q:\n%s", want, text)
		}
	}
}

func TestRenderGraphViewDOTEscapesHostileText(t *testing.T) {
	rendering, err := RenderGraphView(hostileGraphView(), GraphFormatDOT)
	if err != nil {
		t.Fatal(err)
	}
	text := rendering.Text
	if !strings.HasPrefix(text, "digraph \"effra-graph:dependency:synthetic\" {\n") || !strings.HasSuffix(text, "}\n") || rendering.MediaType != "text/vnd.graphviz" {
		t.Fatalf("dot frame: %q", text)
	}
	for _, label := range scanQuoted(t, "dot", text, true) {
		for i := 0; i < len(label); i++ {
			if label[i] == '\\' {
				i++
				if i == len(label) || !strings.ContainsRune(`\"n`, rune(label[i])) {
					t.Fatalf("dot string has an escString sequence: %q", label)
				}
			}
		}
	}
	for _, want := range []string{`say \"hi\" \\N back\\slash\nnew line`, `\\u202E`, `\\u0000`, `\\u001B`, "naïve 日本 🙂", "subgraph cluster_n2 {", `id="edge|calls|1"`, `id="edge|calls|2"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("dot output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "strict") {
		t.Fatal("a strict digraph would merge parallel edges")
	}
}

func TestRenderGraphViewKeepsParallelEdgesAndIsDeterministic(t *testing.T) {
	r := checkedGraphExample(t, "workflow", "go")
	view := graphView(t, r, map[string]any{"kind": "dependency", "collapse": []any{"function:main"}})
	for _, format := range []GraphFormat{GraphFormatMermaid, GraphFormatDOT} {
		first, err := RenderGraphView(view, format)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(view)
		random := rand.New(rand.NewSource(7))
		for trial := 0; trial < 8; trial++ {
			var permuted GraphView
			if err := json.Unmarshal(encoded, &permuted); err != nil {
				t.Fatal(err)
			}
			random.Shuffle(len(permuted.Nodes), func(i, j int) { permuted.Nodes[i], permuted.Nodes[j] = permuted.Nodes[j], permuted.Nodes[i] })
			random.Shuffle(len(permuted.Edges), func(i, j int) { permuted.Edges[i], permuted.Edges[j] = permuted.Edges[j], permuted.Edges[i] })
			again, err := RenderGraphView(&permuted, format)
			if err != nil || again.Text != first.Text {
				t.Fatalf("%s rendering depends on input order: %v", format, err)
			}
		}
		parallel, err := RenderGraphView(hostileGraphView(), format)
		if err != nil {
			t.Fatal(err)
		}
		edges := 0
		for _, line := range strings.Split(parallel.Text, "\n") {
			if strings.Contains(line, "n0 -->") || strings.Contains(line, "n0 -> n1") {
				edges++
			}
		}
		if edges != 2 {
			t.Fatalf("%s merged parallel edges:\n%s", format, parallel.Text)
		}
		selfLoop := map[GraphFormat]string{GraphFormatMermaid: "n3 -->|\"calls\"| n3", GraphFormatDOT: "n3 -> n3 ["}[format]
		twins := map[GraphFormat][]string{GraphFormatMermaid: {"n1[\"function: b\"]", "n3[\"function: b\"]"}, GraphFormatDOT: {"n1 [label=\"function: b\"", "n3 [label=\"function: b\""}}[format]
		if !strings.Contains(parallel.Text, selfLoop) || !strings.Contains(parallel.Text, twins[0]) || !strings.Contains(parallel.Text, twins[1]) {
			t.Fatalf("%s lost a self-loop or merged equal labels:\n%s", format, parallel.Text)
		}
	}
	_, err := RenderGraphView(view, GraphFormatHTML)
	requireGraphRefusal(t, err, GraphRefusalFormat)
	broken := hostileGraphView()
	broken.Edges[0].TargetID = "missing"
	_, err = RenderGraphView(broken, GraphFormatDOT)
	requireGraphRefusal(t, err, GraphRefusalInvalidView)
}

// The payload budget charges both MCP copies of the payload: structured and
// escaped text. Exactly that many bytes are admitted; one fewer is refused.
func TestGraphViewPayloadExactResponseLimit(t *testing.T) {
	for _, format := range []GraphFormat{GraphFormatJSON, GraphFormatMermaid, GraphFormatDOT} {
		view := hostileGraphView()
		payload, _, err := GraphViewPayload(view, format)
		if err != nil {
			t.Fatal(err)
		}
		structured, _ := encodedSize(payload, 1<<30)
		escaped, _ := encodedSizeMode(payload, 1<<30, true)
		// The limit is published inside the view, so iterate to its fixed point.
		for limit := structured + escaped; ; {
			view.Data.Effra.Limits.ResponseBytes = limit
			fitted, _, err := GraphViewPayload(view, format)
			if err != nil {
				t.Fatalf("%s exact response limit refused: %v", format, err)
			}
			structured, _ = encodedSize(fitted, 1<<30)
			escaped, _ = encodedSizeMode(fitted, 1<<30, true)
			if structured+escaped == limit {
				break
			}
			limit = structured + escaped
		}
		view.Data.Effra.Limits.ResponseBytes--
		_, _, err = GraphViewPayload(view, format)
		requireGraphRefusal(t, err, GraphRefusalResponseLimit)
	}
}

func TestRenderGraphViewExactRenderLimit(t *testing.T) {
	for _, format := range []GraphFormat{GraphFormatMermaid, GraphFormatDOT} {
		view := hostileGraphView()
		rendering, err := RenderGraphView(view, format)
		if err != nil {
			t.Fatal(err)
		}
		view.Data.Effra.Limits.RenderBytes = len(rendering.Text)
		if _, err := RenderGraphView(view, format); err != nil {
			t.Fatalf("%s exact render limit refused: %v", format, err)
		}
		view.Data.Effra.Limits.RenderBytes--
		_, err = RenderGraphView(view, format)
		requireGraphRefusal(t, err, GraphRefusalRenderLimit)
	}
}

// graphRenderingFixtures pin rendered bytes for the shapes renderers must
// keep: a recipe alias beside two materializations, a layer diamond with a
// hidden shared binding and an outer replacement, an open construction input,
// native test roots, stored callbacks, a collapsed root that keeps its own
// relations beside a boundary member and hidden interior members, and a
// focused view with closure references. Renderer changes are deliberate:
// regenerate these files only together with GraphRendererVersion review.
var graphRenderingFixtures = []struct {
	name, file, source, target string
	fields                     map[string]any
}{
	{name: "provider-recipes-dependency.mmd", source: legacyGraphFixtures[5].source, fields: map[string]any{"format": "mermaid"}},
	{name: "layer-diamond-layers.dot", source: legacyGraphFixtures[6].source, fields: map[string]any{"kind": "layers", "format": "dot"}},
	{name: "open-input-layers.mmd", source: openLayerSource, fields: map[string]any{"kind": "layers", "format": "mermaid"}},
	{name: "testing-application-test.mmd", file: "../../examples/testing.ef", fields: map[string]any{"kind": "application", "mode": "test", "format": "mermaid"}},
	{name: "stored-callback-application.dot", source: applicationStoredCallbackSource, fields: map[string]any{"kind": "application", "format": "dot"}},
	{name: "collapsed-root-dependency.mmd", source: collapsedRootSource, fields: map[string]any{"format": "mermaid", "focus": "function:helper", "depth": 2, "collapse": []any{"function:helper"}}},
	{name: "collapsed-root-dependency.dot", source: collapsedRootSource, fields: map[string]any{"format": "dot", "focus": "function:helper", "depth": 2, "collapse": []any{"function:helper"}}},
	{name: "closure-layers.mmd", source: openLayerSource, fields: map[string]any{"kind": "layers", "format": "mermaid", "focus": layerID("Fixture"), "direction": "outgoing", "depth": 1}},
	{name: "closure-layers.dot", source: openLayerSource, fields: map[string]any{"kind": "layers", "format": "dot", "focus": layerID("Fixture"), "direction": "outgoing", "depth": 1}},
}

// collapsedRootSource has a collapse root with incoming and outgoing
// relations of its own and a member that calls out of the group.
const collapsedRootSource = `service Users { effect fn get() -> string }
impl Live for Users { effect fn get() -> string { "u" } }
effect fn helper() -> string uses { Users } { run Users.get() }
effect fn main() -> string uses { Users } { run helper() }
`

// A collapsed root is a node inside its group in both formats: its own
// relations attach to the root, and the group box has a separate ID.
func TestRenderGraphViewKeepsTheCollapsedRootANode(t *testing.T) {
	r := checkedGraphSource(t, collapsedRootSource, "go")
	view := graphView(t, r, map[string]any{"collapse": []any{"function:helper"}})
	layout := newGraphRenderLayout(view)
	root := layout.ids["function:helper"]
	box := "g" + strings.TrimPrefix(root, "n")
	if len(layout.children["function:helper"]) == 0 {
		t.Fatal("fixture has no visible boundary member under the root")
	}
	rootEdges := 0
	for _, edge := range view.Edges {
		if edge.SourceID == "function:helper" || edge.TargetID == "function:helper" {
			rootEdges++
		}
	}
	if rootEdges == 0 {
		t.Fatal("fixture root has no relation of its own")
	}
	group := map[GraphFormat]string{GraphFormatMermaid: "subgraph " + box + "[", GraphFormatDOT: "subgraph cluster_" + root + " {"}
	node := map[GraphFormat]string{GraphFormatMermaid: root + "[", GraphFormatDOT: root + " [label="}
	edge := map[GraphFormat]string{GraphFormatMermaid: " -->|", GraphFormatDOT: " -> "}
	for _, format := range []GraphFormat{GraphFormatMermaid, GraphFormatDOT} {
		rendering, err := RenderGraphView(view, format)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(rendering.Text, "\n")
		start := slices.IndexFunc(lines, func(line string) bool {
			return strings.TrimSpace(line) == strings.TrimSpace(group[format]) || strings.HasPrefix(strings.TrimSpace(line), group[format])
		})
		if start < 0 || !slices.ContainsFunc(lines[start+1:min(start+3, len(lines))], func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), node[format]) }) {
			t.Fatalf("%s does not emit the collapsed root as a node inside its group:\n%s", format, rendering.Text)
		}
		attached := 0
		for _, line := range lines {
			if !strings.Contains(line, edge[format]) {
				continue
			}
			fields := strings.Fields(line)
			if fields[0] == box || fields[len(fields)-1] == box {
				t.Fatalf("%s attaches a relation to the group box: %q", format, line)
			}
			if fields[0] == root || fields[2] == root || fields[len(fields)-1] == root {
				attached++
			}
		}
		if attached != rootEdges {
			t.Fatalf("%s attaches %d of the root's %d relations to the root node:\n%s", format, attached, rootEdges, rendering.Text)
		}
	}
}

// Closure nodes are references published without traversal. Both formats
// mark exactly those nodes with the closure class, Mermaid through classDef
// and DOT through a dashed style, so a reader never takes them for traversal
// results; a view without closure emits no class at all.
func TestRenderGraphViewMarksClosureNodes(t *testing.T) {
	r := checkedGraphSource(t, openLayerSource, "go")
	view := graphView(t, r, map[string]any{"kind": "layers", "focus": layerID("Fixture"), "direction": "outgoing", "depth": 1})
	layout := newGraphRenderLayout(view)
	closure, plain := []string{}, []string{}
	for _, node := range layout.nodes {
		if node.Data.Effra.Closure {
			closure = append(closure, layout.ids[node.ID])
		} else {
			plain = append(plain, layout.ids[node.ID])
		}
	}
	if len(closure) == 0 || view.Data.Effra.Completeness.ClosureNodes != len(closure) {
		t.Fatalf("fixture has no closure nodes: %+v", view.Data.Effra.Completeness)
	}
	mermaid, err := RenderGraphView(view, GraphFormatMermaid)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(mermaid.Text, "\n")
	if !slices.Contains(lines, "  classDef closure stroke-dasharray:5 5") || !slices.Contains(lines, "  class "+strings.Join(closure, ",")+" closure") {
		t.Fatalf("mermaid does not class exactly the closure nodes %v:\n%s", closure, mermaid.Text)
	}
	dot, err := RenderGraphView(view, GraphFormatDOT)
	if err != nil {
		t.Fatal(err)
	}
	marked := func(id string) bool {
		return slices.ContainsFunc(strings.Split(dot.Text, "\n"), func(line string) bool {
			return strings.HasPrefix(strings.TrimSpace(line), id+" [") && strings.HasSuffix(line, `, style=dashed, class="closure"];`)
		})
	}
	for _, id := range closure {
		if !marked(id) {
			t.Fatalf("dot does not mark closure node %s:\n%s", id, dot.Text)
		}
	}
	for _, id := range plain {
		if marked(id) {
			t.Fatalf("dot marks traversed node %s as closure:\n%s", id, dot.Text)
		}
	}
	for _, rendering := range []*GraphRendering{mermaid, dot} {
		if !slices.ContainsFunc(rendering.Losses, func(loss string) bool { return strings.Contains(loss, "closure") }) {
			t.Fatalf("%s losses do not name closure: %v", rendering.Format, rendering.Losses)
		}
	}
	whole := graphView(t, r, map[string]any{"kind": "layers"})
	for _, format := range []GraphFormat{GraphFormatMermaid, GraphFormatDOT} {
		rendering, err := RenderGraphView(whole, format)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rendering.Text, "closure") {
			t.Fatalf("%s marks closure in a view without closure nodes:\n%s", format, rendering.Text)
		}
	}
}

func TestGraphRenderingGoldens(t *testing.T) {
	for _, fixture := range graphRenderingFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			source, dir := fixture.source, "."
			if fixture.file != "" {
				raw, err := os.ReadFile(fixture.file)
				if err != nil {
					t.Fatal(err)
				}
				source, dir = string(raw), filepath.Dir(fixture.file)
			}
			r := CompileAt(source, "go", dir)
			if !r.Checked {
				t.Fatalf("fixture is not checked: %v", r.Diagnostics)
			}
			request := graphRequest(t, fixture.fields)
			view, err := r.GraphView(request)
			if err != nil {
				t.Fatal(err)
			}
			_, rendering, err := GraphViewPayload(view, request.Format)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", "graph", "views", fixture.name))
			if err != nil {
				t.Fatal(err)
			}
			if rendering.Text != string(expected) {
				t.Fatalf("rendering changed for %s:\n%s", fixture.name, rendering.Text)
			}
		})
	}
}

// Every checked example yields a valid view of every kind in every diagram
// format, or one of the documented capability refusals.
func TestGraphViewsCoverEveryExample(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.ef")
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	views := 0
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
			for _, kind := range []string{"dependency", "layers", "application"} {
				view, err := r.GraphView(graphRequest(t, map[string]any{"kind": kind}))
				var refusal *GraphRefusal
				if errors.As(err, &refusal) && kind == "application" && (refusal.Code == GraphRefusalTarget && target == "js" || refusal.Code == GraphRefusalPlan) {
					continue
				}
				if err != nil {
					t.Fatalf("%s %s %s: %v", filepath.Base(file), target, kind, err)
				}
				views++
				for _, format := range []GraphFormat{GraphFormatMermaid, GraphFormatDOT} {
					if _, _, err := GraphViewPayload(view, format); err != nil {
						t.Fatalf("%s %s %s %s: %v", filepath.Base(file), target, kind, format, err)
					}
				}
			}
		}
	}
	if views < 2*len(files) {
		t.Fatalf("only %d views over %d examples", views, len(files))
	}
}
