package compiler

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// GraphRendererVersion versions the Mermaid and DOT encodings independently
// of GraphViewV1.
const GraphRendererVersion = 1

// GraphRendering is one diagram of exactly one selected GraphView. It is a
// lossy presentation: the view remains the complete interchange.
type GraphRendering struct {
	Format          GraphFormat `json:"format"`
	MediaType       string      `json:"mediaType"`
	RendererVersion int         `json:"rendererVersion"`
	Text            string      `json:"text"`
	Losses          []string    `json:"losses"`
}

var graphRenderingLosses = []string{
	"data.effra facts (contracts, rows, spans, selections, frontier and limits) are not rendered; use format json",
	"node and edge IDs appear as comments or id attributes; renderer IDs n0..nN and e0..eN are positional",
	"collapsed members are omitted; their counts appear in the collapse root label",
	"closure nodes are references published without traversal; they carry the closure class (dashed), not a label marker, and have no relations of their own",
}

// graphClosureClass is the class both formats give closure nodes: a Mermaid
// classDef and class assignment, and a DOT dashed style with the same class.
const graphClosureClass = "closure"

// GraphViewRendered is the project.graph payload of a diagram format: the
// complete view beside its one rendering.
type GraphViewRendered struct {
	View      *GraphView      `json:"view"`
	Rendering *GraphRendering `json:"rendering"`
}

// GraphViewPayload is the one adapter seam for CLI and MCP: the view itself
// for json, or the view with its rendering for mermaid and dot. The payload
// is charged as MCP transmits it, structured and again as escaped text,
// against the view's response budget, so both adapters refuse the same
// oversized request with the same code.
func GraphViewPayload(view *GraphView, format GraphFormat) (any, *GraphRendering, error) {
	var payload any = view
	var rendering *GraphRendering
	if format != GraphFormatJSON {
		rendered, err := RenderGraphView(view, format)
		if err != nil {
			return nil, nil, err
		}
		rendering = rendered
		payload = GraphViewRendered{View: view, Rendering: rendered}
	}
	limit := view.Data.Effra.Limits.ResponseBytes
	raw, err := encodedSize(payload, limit)
	if err == nil {
		_, err = encodedSizeMode(payload, limit-raw, true)
	}
	if err != nil {
		return nil, nil, graphRefusal(GraphRefusalResponseLimit, "graph %s response exceeds %d bytes; select with focus, depth or edge kinds", format, view.Data.Effra.Limits.ResponseBytes)
	}
	return payload, rendering, nil
}

// RenderGraphView renders one validated view. Renderer IDs, label escaping
// and ordering are owned here once per format; renderers never consult
// compiler facts beyond the view.
func RenderGraphView(view *GraphView, format GraphFormat) (*GraphRendering, error) {
	if err := ValidateGraphView(view); err != nil {
		return nil, graphRefusal(GraphRefusalInvalidView, "%v", err)
	}
	layout := newGraphRenderLayout(view)
	var text string
	var mediaType string
	switch format {
	case GraphFormatMermaid:
		text, mediaType = layout.mermaid(), "text/vnd.mermaid"
	case GraphFormatDOT:
		text, mediaType = layout.dot(), "text/vnd.graphviz"
	default:
		return nil, graphRefusal(GraphRefusalFormat, "graph format %q has no renderer; use mermaid or dot", format)
	}
	if limit := view.Data.Effra.Limits.RenderBytes; len(text) > limit {
		return nil, graphRefusal(GraphRefusalRenderLimit, "%s rendering is %d bytes; limit is %d; select with focus, depth or edge kinds", format, len(text), limit)
	}
	return &GraphRendering{Format: format, MediaType: mediaType, RendererVersion: GraphRendererVersion, Text: text, Losses: append([]string{}, graphRenderingLosses...)}, nil
}

// graphRenderLayout is the format-independent rendering order: nodes and
// edges sorted by canonical ID, positional renderer IDs, and the containment
// forest. Input order never reaches the output.
type graphRenderLayout struct {
	view     *GraphView
	nodes    []*GraphViewNode
	edges    []*GraphViewEdge
	ids      map[string]string
	children map[string][]*GraphViewNode
	roots    []*GraphViewNode
	closure  []string
}

func newGraphRenderLayout(view *GraphView) *graphRenderLayout {
	layout := &graphRenderLayout{view: view, ids: map[string]string{}, children: map[string][]*GraphViewNode{}}
	for i := range view.Nodes {
		layout.nodes = append(layout.nodes, &view.Nodes[i])
	}
	slices.SortFunc(layout.nodes, func(a, b *GraphViewNode) int { return strings.Compare(a.ID, b.ID) })
	for i, node := range layout.nodes {
		layout.ids[node.ID] = fmt.Sprintf("n%d", i)
		if node.Data.Effra.Closure {
			layout.closure = append(layout.closure, layout.ids[node.ID])
		}
		if node.ParentID == "" {
			layout.roots = append(layout.roots, node)
		} else {
			layout.children[node.ParentID] = append(layout.children[node.ParentID], node)
		}
	}
	for i := range view.Edges {
		layout.edges = append(layout.edges, &view.Edges[i])
	}
	slices.SortFunc(layout.edges, func(a, b *GraphViewEdge) int { return strings.Compare(a.ID, b.ID) })
	return layout
}

// graphNodeLabel is the display text of one node: its kind and label, or the
// kind at its source position when the fact is anonymous.
func graphNodeLabel(node *GraphViewNode) string {
	kind, label := node.Data.Effra.Kind, node.Label
	switch {
	case label == "" && node.Data.Effra.Span != nil && node.Data.Effra.Span.Line > 0:
		label = fmt.Sprintf("%s @ %d:%d", kind, node.Data.Effra.Span.Line, node.Data.Effra.Span.Column)
	case label == "":
		label = kind
	case kind != "" && kind != label:
		label = kind + ": " + label
	}
	if collapsed := node.Data.Effra.Collapsed; collapsed != nil {
		label += fmt.Sprintf(" (+%d hidden members, %d hidden edges)", len(collapsed.HiddenMembers), collapsed.HiddenEdges)
	}
	return label
}

func graphEdgeLabel(edge *GraphViewEdge) string {
	if edge.Label != "" {
		return edge.Label
	}
	return edge.Data.Effra.Relation
}

// mermaidText escapes text for a quoted Mermaid label or a comment. Only
// letters, digits, space and inert punctuation pass through; every other rune,
// including quotes, `#`, `;`, `|`, `%`, braces, angle brackets, backslashes,
// newlines and controls, becomes a numeric entity, so text can neither close
// its string nor start a directive, comment, link or HTML tag.
func mermaidText(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == ' ' || unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_.,:/()[]+*=!?'@$^~", r):
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "#%d;", r)
		}
	}
	return b.String()
}

func (layout *graphRenderLayout) mermaid() string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	fmt.Fprintf(&b, "%%%% effra graph view %s\n", mermaidText(layout.view.ID))
	for _, node := range layout.nodes {
		fmt.Fprintf(&b, "%%%% %s: %s\n", layout.ids[node.ID], mermaidText(node.ID))
	}
	if len(layout.closure) > 0 {
		fmt.Fprintf(&b, "  classDef %s stroke-dasharray:5 5\n", graphClosureClass)
	}
	var emit func(node *GraphViewNode, indent string)
	emit = func(node *GraphViewNode, indent string) {
		id, label := layout.ids[node.ID], mermaidText(graphNodeLabel(node))
		children := layout.children[node.ID]
		if len(children) == 0 {
			fmt.Fprintf(&b, "%s%s[\"%s\"]\n", indent, id, label)
			return
		}
		// The group gets its own ID so the root stays a node inside it, as
		// in DOT: the root's own relations attach to the root, not the box.
		fmt.Fprintf(&b, "%ssubgraph g%s[\"%s\"]\n%s  %s[\"%s\"]\n", indent, strings.TrimPrefix(id, "n"), label, indent, id, label)
		for _, child := range children {
			emit(child, indent+"  ")
		}
		fmt.Fprintf(&b, "%send\n", indent)
	}
	for _, node := range layout.roots {
		emit(node, "  ")
	}
	for i, edge := range layout.edges {
		fmt.Fprintf(&b, "  %%%% e%d: %s\n", i, mermaidText(edge.ID))
		fmt.Fprintf(&b, "  %s -->|\"%s\"| %s\n", layout.ids[edge.SourceID], mermaidText(graphEdgeLabel(edge)), layout.ids[edge.TargetID])
	}
	if len(layout.closure) > 0 {
		fmt.Fprintf(&b, "  class %s %s\n", strings.Join(layout.closure, ","), graphClosureClass)
	}
	return b.String()
}

// dotText escapes text for a DOT double-quoted string. Backslash is doubled
// first so Graphviz escString sequences (\N, \G, \l, ...) cannot form; a
// quote is escaped, newline becomes the centered line break, and every other
// control, format (bidi) or separator rune becomes a visible \\uXXXX escape.
func dotText(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp):
			fmt.Fprintf(&b, `\\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (layout *graphRenderLayout) dot() string {
	var b strings.Builder
	fmt.Fprintf(&b, "digraph \"%s\" {\n  rankdir=LR;\n", dotText(layout.view.ID))
	var emit func(node *GraphViewNode, indent string)
	emit = func(node *GraphViewNode, indent string) {
		id := layout.ids[node.ID]
		attributes := fmt.Sprintf("label=\"%s\", id=\"%s\"", dotText(graphNodeLabel(node)), dotText(node.ID))
		if node.Data.Effra.Closure {
			attributes += fmt.Sprintf(", style=dashed, class=\"%s\"", graphClosureClass)
		}
		attributes = "[" + attributes + "]"
		children := layout.children[node.ID]
		if len(children) == 0 {
			fmt.Fprintf(&b, "%s%s %s;\n", indent, id, attributes)
			return
		}
		fmt.Fprintf(&b, "%ssubgraph cluster_%s {\n%s  label=\"%s\";\n%s  %s %s;\n", indent, id, indent, dotText(graphNodeLabel(node)), indent, id, attributes)
		for _, child := range children {
			emit(child, indent+"  ")
		}
		fmt.Fprintf(&b, "%s}\n", indent)
	}
	for _, node := range layout.roots {
		emit(node, "  ")
	}
	for i, edge := range layout.edges {
		fmt.Fprintf(&b, "  %s -> %s [label=\"%s\", id=\"%s\", class=\"e%d\"];\n", layout.ids[edge.SourceID], layout.ids[edge.TargetID], dotText(graphEdgeLabel(edge)), dotText(edge.ID), i)
	}
	b.WriteString("}\n")
	return b.String()
}
