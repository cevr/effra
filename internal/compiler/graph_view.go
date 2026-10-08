package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"effra.local/prototype/internal/producer"
	rt "effra.local/prototype/runtime/effra"
)

// GraphViewVersion versions the GraphView wire contract independently of the
// semantic schema and of any renderer.
const GraphViewVersion = 1

// GraphView is GraphViewV1: one selected, compiler-owned projection of
// checked facts in the structural shape of a Stately Graph. All Effra
// semantics live under data.effra; diagrams render this view and never
// re-derive facts.
type GraphView struct {
	ID            string          `json:"id"`
	Mode          string          `json:"mode"`
	InitialNodeID string          `json:"initialNodeId,omitempty"`
	Nodes         []GraphViewNode `json:"nodes"`
	Edges         []GraphViewEdge `json:"edges"`
	Data          GraphViewData   `json:"data"`
}

type GraphViewData struct {
	Effra GraphViewFacts `json:"effra"`
}

type GraphViewNode struct {
	Type     string            `json:"type"`
	ID       string            `json:"id"`
	ParentID string            `json:"parentId,omitempty"`
	Label    string            `json:"label,omitempty"`
	Ports    []GraphViewPort   `json:"ports,omitempty"`
	Data     GraphViewNodeData `json:"data"`
}

// GraphViewPort is a node-local boundary name. Stage 1 projections publish
// none; the validator still closes every edge port over its endpoint.
type GraphViewPort struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Label     string `json:"label,omitempty"`
	Data      any    `json:"data"`
}

type GraphViewNodeData struct {
	Effra GraphNodeFacts `json:"effra"`
}

type GraphViewEdge struct {
	Type       string            `json:"type"`
	ID         string            `json:"id"`
	SourceID   string            `json:"sourceId"`
	TargetID   string            `json:"targetId"`
	SourcePort string            `json:"sourcePort,omitempty"`
	TargetPort string            `json:"targetPort,omitempty"`
	Label      string            `json:"label,omitempty"`
	Data       GraphViewEdgeData `json:"data"`
}

type GraphViewEdgeData struct {
	Effra GraphEdgeFacts `json:"effra"`
}

// GraphNodeFacts are the checked facts of one published node. Each kind fills
// only the fields that have meaning for it.
type GraphNodeFacts struct {
	Kind      string                 `json:"kind"`
	Name      string                 `json:"name,omitempty"`
	Source    string                 `json:"source,omitempty"`
	Span      *Span                  `json:"span,omitempty"`
	Contract  *ValueType             `json:"contract,omitempty"`
	Layer     *GraphLayerFacts       `json:"layer,omitempty"`
	Binding   *GraphBindingFacts     `json:"binding,omitempty"`
	Retention *GraphRetentionFacts   `json:"retention,omitempty"`
	Collapsed *GraphCollapsedFacts   `json:"collapsed,omitempty"`
	Frontier  bool                   `json:"frontier,omitempty"`
	Selection *GraphLayerSelectFacts `json:"selection,omitempty"`
}

// GraphEdgeFacts identify one checked relation occurrence.
type GraphEdgeFacts struct {
	Relation     string                 `json:"relation"`
	Service      string                 `json:"service,omitempty"`
	Span         *Span                  `json:"span,omitempty"`
	Plan         string                 `json:"plan,omitempty"`
	Selection    *GraphLayerSelectFacts `json:"selection,omitempty"`
	Reason       string                 `json:"reason,omitempty"`
	FirstWitness bool                   `json:"firstWitness,omitempty"`
}

// GraphCollapsedFacts records the members a collapse hid. Hidden members are
// presentation omissions: their IDs remain canonical, no summary edge stands
// in for them, and a published edge always joins two published facts.
type GraphCollapsedFacts struct {
	HiddenMembers []string `json:"hiddenMembers"`
	HiddenEdges   int      `json:"hiddenEdges"`
}

type GraphViewFacts struct {
	GraphViewVersion int                   `json:"graphViewVersion"`
	Kind             GraphKind             `json:"kind"`
	Producer         producer.Identity     `json:"producer,omitzero"`
	Snapshot         SnapshotQualification `json:"snapshot"`
	ProducerIdentity string                `json:"producerIdentity"`
	Selection        GraphSelection        `json:"selection"`
	Completeness     GraphCompleteness     `json:"completeness"`
	Facts            GraphFactTables       `json:"facts"`
	Limits           GraphViewLimits       `json:"limits"`
	Usage            GraphViewUsage        `json:"usage"`
	Limitations      []string              `json:"limitations"`
}

// GraphSelection is the normalized request that produced a view.
type GraphSelection struct {
	Target    string         `json:"target"`
	Focus     string         `json:"focus,omitempty"`
	Depth     *int           `json:"depth,omitempty"`
	Direction GraphDirection `json:"direction,omitempty"`
	EdgeKinds []string       `json:"edgeKinds"`
	Collapse  []string       `json:"collapse"`
	Mode      string         `json:"mode,omitempty"`
}

// GraphCompleteness separates an intentional selection from a refusal: a
// successful view is complete for its scope, and the fact totals are exact.
type GraphCompleteness struct {
	Scope          string   `json:"scope"`
	Complete       bool     `json:"complete"`
	Semantics      string   `json:"semantics"`
	FactNodes      int      `json:"factNodes"`
	FactEdges      int      `json:"factEdges"`
	SelectedNodes  int      `json:"selectedNodes"`
	SelectedEdges  int      `json:"selectedEdges"`
	PublishedNodes int      `json:"publishedNodes"`
	PublishedEdges int      `json:"publishedEdges"`
	Frontier       []string `json:"frontier"`
}

type GraphFactTables struct {
	Sources      []SourceInfo  `json:"sources"`
	Types        []TypeNode    `json:"types"`
	Rows         []RowNode     `json:"rows"`
	Declarations []Declaration `json:"declarations"`
}

// GraphViewLimits are the declared graph view budgets. Nodes and edges bound
// the published view; Work bounds fact enumeration plus selection; Depth
// bounds a focus traversal; RenderBytes bounds one diagram; ResponseBytes
// bounds the encoded view.
type GraphViewLimits struct {
	Nodes          int              `json:"nodes"`
	Edges          int              `json:"edges"`
	Work           int              `json:"work"`
	Depth          int              `json:"depth"`
	RenderBytes    int              `json:"renderBytes"`
	ResponseBytes  int              `json:"responseBytes"`
	TypeProjection ProjectionLimits `json:"typeProjection"`
}

type GraphViewUsage struct {
	Nodes             int             `json:"nodes"`
	Edges             int             `json:"edges"`
	Work              int             `json:"work"`
	MaterializedNodes int             `json:"materializedNodes"`
	TypeProjection    ProjectionUsage `json:"typeProjection"`
}

var defaultGraphViewLimits = GraphViewLimits{
	Nodes:         maxGraphNodes,
	Edges:         maxGraphEdges,
	Work:          1 << 18,
	Depth:         64,
	RenderBytes:   maxTypeProjectionResponseBytes,
	ResponseBytes: maxTypeProjectionResponseBytes,
}

type GraphKind string

const (
	GraphKindDependency  GraphKind = "dependency"
	GraphKindLayers      GraphKind = "layers"
	GraphKindApplication GraphKind = "application"
	GraphKindMachine     GraphKind = "machine"
	GraphKindActor       GraphKind = "actor"
)

type GraphFormat string

const (
	GraphFormatJSON    GraphFormat = "json"
	GraphFormatMermaid GraphFormat = "mermaid"
	GraphFormatDOT     GraphFormat = "dot"
	GraphFormatHTML    GraphFormat = "html"
)

type GraphDirection string

const (
	GraphOutgoing GraphDirection = "outgoing"
	GraphIncoming GraphDirection = "incoming"
	GraphBoth     GraphDirection = "both"
)

// GraphRefusal is an explicit graph request refusal with a stable code. A
// refusal never carries a partial or empty successful graph.
type GraphRefusal struct {
	Code    string
	Message string
}

func (e *GraphRefusal) Error() string { return e.Code + ": " + e.Message }

// Invocation reports whether the request itself was malformed, before any
// source fact was consulted.
func (e *GraphRefusal) Invocation() bool { return graphInvocationRefusals[e.Code] }

const (
	GraphRefusalInvocation        = "EFGRAPH_INVOCATION"
	GraphRefusalKind              = "EFGRAPH_KIND"
	GraphRefusalKindUnavailable   = "EFGRAPH_KIND_UNAVAILABLE"
	GraphRefusalFormat            = "EFGRAPH_FORMAT"
	GraphRefusalFormatUnavailable = "EFGRAPH_FORMAT_UNAVAILABLE"
	GraphRefusalIncompatible      = "EFGRAPH_INCOMPATIBLE"
	GraphRefusalEdgeKind          = "EFGRAPH_EDGE_KIND"
	GraphRefusalMode              = "EFGRAPH_MODE"
	GraphRefusalUnchecked         = "EFGRAPH_UNCHECKED"
	GraphRefusalTarget            = "EFGRAPH_TARGET"
	GraphRefusalFocus             = "EFGRAPH_FOCUS"
	GraphRefusalCollapse          = "EFGRAPH_COLLAPSE"
	GraphRefusalPlan              = "EFGRAPH_PLAN"
	GraphRefusalNodeLimit         = "EFGRAPH_NODE_LIMIT"
	GraphRefusalEdgeLimit         = "EFGRAPH_EDGE_LIMIT"
	GraphRefusalWorkLimit         = "EFGRAPH_WORK_LIMIT"
	GraphRefusalDepthLimit        = "EFGRAPH_DEPTH_LIMIT"
	GraphRefusalTypeLimit         = "EFGRAPH_TYPE_LIMIT"
	GraphRefusalRenderLimit       = "EFGRAPH_RENDER_LIMIT"
	GraphRefusalResponseLimit     = "EFGRAPH_RESPONSE_LIMIT"
	GraphRefusalInvalidView       = "EFGRAPH_INVALID_VIEW"
)

var graphInvocationRefusals = map[string]bool{
	GraphRefusalInvocation: true, GraphRefusalKind: true, GraphRefusalKindUnavailable: true,
	GraphRefusalFormat: true, GraphRefusalFormatUnavailable: true, GraphRefusalIncompatible: true,
	GraphRefusalEdgeKind: true, GraphRefusalMode: true, GraphRefusalDepthLimit: true,
}

func graphRefusal(code, format string, args ...any) error {
	return &GraphRefusal{Code: code, Message: fmt.Sprintf(format, args...)}
}

// GraphRequest is one validated graph view request. Adapters build it only
// through ParseGraphRequest so CLI and MCP share admission and refusals.
type GraphRequest struct {
	Kind      GraphKind
	Format    GraphFormat
	Focus     string
	Depth     *int
	Direction GraphDirection
	EdgeKinds []string
	Collapse  []string
	Mode      string
	limits    *GraphViewLimits
}

// GraphOption is one graph selection option, advertised identically by the
// CLI flag parser and the MCP project.graph schema.
type GraphOption struct {
	Field       string
	Flag        string
	Repeatable  bool
	Values      []string
	Description string
}

const maxGraphOptionItems = 64
const maxGraphOptionBytes = 1024

var graphOptions = []GraphOption{
	{Field: "kind", Flag: "--kind", Values: []string{"dependency", "layers", "application"}, Description: "Graph view kind; selecting kind or format requests GraphViewV1"},
	{Field: "format", Flag: "--format", Values: []string{"json", "mermaid", "dot"}, Description: "Rendering of the one selected view; json is the complete interchange"},
	{Field: "focus", Flag: "--focus", Description: "Exact node ID to select around; requires a checked node of the kind"},
	{Field: "depth", Flag: "--depth", Description: "Hops from the focus; requires focus; default 1"},
	{Field: "direction", Flag: "--direction", Values: []string{"outgoing", "incoming", "both"}, Description: "Traversal direction from the focus; requires focus; default both"},
	{Field: "edgeKinds", Flag: "--edge-kind", Repeatable: true, Description: "Relations to traverse and publish; defaults to every relation of the kind"},
	{Field: "collapse", Flag: "--collapse", Repeatable: true, Description: "Containment roots whose interior members are hidden; boundary members stay visible"},
	{Field: "mode", Flag: "--mode", Values: []string{"build", "test"}, Description: "Application entry mode; only with kind application; default build"},
}

// GraphOptions lists the graph selection options in their advertised order.
func GraphOptions() []GraphOption {
	options := make([]GraphOption, len(graphOptions))
	copy(options, graphOptions)
	return options
}

// GraphOptionField maps a CLI flag to its shared request field.
func GraphOptionField(flag string) (GraphOption, bool) {
	for _, option := range graphOptions {
		if option.Flag == flag {
			return option, true
		}
	}
	return GraphOption{}, false
}

// IsGraphOptionField reports whether field is a graph selection argument.
func IsGraphOptionField(field string) bool {
	for _, option := range graphOptions {
		if option.Field == field {
			return true
		}
	}
	return false
}

// GraphOptionSchema returns the JSON-schema properties of the graph options.
func GraphOptionSchema() map[string]any {
	properties := map[string]any{}
	for _, option := range graphOptions {
		var property map[string]any
		switch {
		case option.Repeatable:
			property = map[string]any{"type": "array", "maxItems": maxGraphOptionItems, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": maxGraphOptionBytes}}
		case option.Field == "depth":
			property = map[string]any{"type": "integer", "minimum": 0, "maximum": defaultGraphViewLimits.Depth}
		case len(option.Values) > 0:
			property = map[string]any{"type": "string", "enum": append([]string{}, option.Values...)}
		default:
			property = map[string]any{"type": "string", "minLength": 1, "maxLength": maxGraphOptionBytes}
		}
		property["description"] = option.Description
		properties[option.Field] = property
	}
	return properties
}

// graphKindRelations is the closed relation vocabulary of each kind.
var graphKindRelations = map[GraphKind][]string{
	GraphKindDependency:  {"adapts", "calls", "constructs", "contains", "implements", "materializes", "originates", "provides", "provides-layer", "references", "requires"},
	GraphKindLayers:      {"consumes-input", "depends-on", "merges", "replaces", "requires-input", "selects"},
	GraphKindApplication: {"retains", "runtime-requires"},
}

// graphKindContainment names the forest relation collapse groups follow. A
// kind without one (layers share bindings as a DAG) refuses collapse.
var graphKindContainment = map[GraphKind]string{
	GraphKindDependency:  "contains",
	GraphKindApplication: "retains",
}

// graphApplicationModes maps the public entry modes, named after the `ef
// build` and `ef test` commands, to their checked generation modes.
var graphApplicationModes = map[string]GoGenerationMode{"build": GoGenerationBuild, "test": GoGenerationTest}

// ParseGraphRequest admits graph options decoded from MCP JSON or from CLI
// flags. Every rejection is a GraphRefusal shared by both adapters.
func ParseGraphRequest(fields map[string]any) (GraphRequest, error) {
	request := GraphRequest{Kind: GraphKindDependency, Format: GraphFormatJSON}
	for field := range fields {
		if !IsGraphOptionField(field) {
			return request, graphRefusal(GraphRefusalInvocation, "unknown graph option %s", field)
		}
	}
	text := func(field string) (string, bool, error) {
		value, present := fields[field]
		if !present {
			return "", false, nil
		}
		s, ok := value.(string)
		if !ok || s == "" || len(s) > maxGraphOptionBytes {
			return "", true, graphRefusal(GraphRefusalInvocation, "%s must be a non-empty string of at most %d bytes", field, maxGraphOptionBytes)
		}
		return s, true, nil
	}
	list := func(field string) ([]string, error) {
		value, present := fields[field]
		if !present {
			return nil, nil
		}
		var items []string
		switch value := value.(type) {
		case []string:
			items = append(items, value...)
		case []any:
			for _, item := range value {
				s, ok := item.(string)
				if !ok {
					return nil, graphRefusal(GraphRefusalInvocation, "%s must be an array of non-empty strings", field)
				}
				items = append(items, s)
			}
		default:
			return nil, graphRefusal(GraphRefusalInvocation, "%s must be an array of non-empty strings", field)
		}
		if len(items) == 0 || len(items) > maxGraphOptionItems {
			return nil, graphRefusal(GraphRefusalInvocation, "%s must list 1 to %d items", field, maxGraphOptionItems)
		}
		for _, item := range items {
			if item == "" || len(item) > maxGraphOptionBytes {
				return nil, graphRefusal(GraphRefusalInvocation, "%s must be an array of non-empty strings", field)
			}
		}
		slices.Sort(items)
		return slices.Compact(items), nil
	}
	if kind, present, err := text("kind"); err != nil {
		return request, err
	} else if present {
		switch GraphKind(kind) {
		case GraphKindDependency, GraphKindLayers, GraphKindApplication:
			request.Kind = GraphKind(kind)
		case GraphKindMachine, GraphKindActor:
			return request, graphRefusal(GraphRefusalKindUnavailable, "graph kind %s is not implemented; its checked plan owner has not landed", kind)
		default:
			return request, graphRefusal(GraphRefusalKind, "unknown graph kind %q; use dependency, layers or application", kind)
		}
	}
	if format, present, err := text("format"); err != nil {
		return request, err
	} else if present {
		switch GraphFormat(format) {
		case GraphFormatJSON, GraphFormatMermaid, GraphFormatDOT:
			request.Format = GraphFormat(format)
		case GraphFormatHTML:
			return request, graphRefusal(GraphRefusalFormatUnavailable, "graph format html is not implemented; use json, mermaid or dot")
		default:
			return request, graphRefusal(GraphRefusalFormat, "unknown graph format %q; use json, mermaid or dot", format)
		}
	}
	focus, hasFocus, err := text("focus")
	if err != nil {
		return request, err
	}
	request.Focus = focus
	if value, present := fields["depth"]; present {
		depth, ok := graphInteger(value)
		if !ok || depth < 0 {
			return request, graphRefusal(GraphRefusalInvocation, "depth must be a non-negative integer")
		}
		if !hasFocus {
			return request, graphRefusal(GraphRefusalIncompatible, "depth requires focus")
		}
		if depth > defaultGraphViewLimits.Depth {
			return request, graphRefusal(GraphRefusalDepthLimit, "depth %d exceeds the %d-hop limit", depth, defaultGraphViewLimits.Depth)
		}
		request.Depth = &depth
	}
	if direction, present, err := text("direction"); err != nil {
		return request, err
	} else if present {
		switch GraphDirection(direction) {
		case GraphOutgoing, GraphIncoming, GraphBoth:
		default:
			return request, graphRefusal(GraphRefusalInvocation, "unknown direction %q; use outgoing, incoming or both", direction)
		}
		if !hasFocus {
			return request, graphRefusal(GraphRefusalIncompatible, "direction requires focus")
		}
		request.Direction = GraphDirection(direction)
	}
	if hasFocus && request.Direction == "" {
		request.Direction = GraphBoth
	}
	if hasFocus && request.Depth == nil {
		depth := 1
		request.Depth = &depth
	}
	if request.EdgeKinds, err = list("edgeKinds"); err != nil {
		return request, err
	}
	for _, relation := range request.EdgeKinds {
		if !slices.Contains(graphKindRelations[request.Kind], relation) {
			return request, graphRefusal(GraphRefusalEdgeKind, "unknown %s relation %q; use %s", request.Kind, relation, strings.Join(graphKindRelations[request.Kind], ", "))
		}
	}
	if request.Collapse, err = list("collapse"); err != nil {
		return request, err
	}
	if len(request.Collapse) > 0 && graphKindContainment[request.Kind] == "" {
		return request, graphRefusal(GraphRefusalCollapse, "graph kind %s has no containment groups to collapse; shared bindings are not owned by one parent", request.Kind)
	}
	if mode, present, err := text("mode"); err != nil {
		return request, err
	} else if present {
		if request.Kind != GraphKindApplication {
			return request, graphRefusal(GraphRefusalIncompatible, "mode applies only to kind application")
		}
		if _, known := graphApplicationModes[mode]; !known {
			return request, graphRefusal(GraphRefusalMode, "unknown application mode %q; use build or test", mode)
		}
		request.Mode = mode
	}
	if request.Kind == GraphKindApplication && request.Mode == "" {
		request.Mode = "build"
	}
	return request, nil
}

func graphInteger(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, true
	case float64:
		if value != math.Trunc(value) || value < math.MinInt32 || value > math.MaxInt32 {
			return 0, false
		}
		return int(value), true
	case json.Number:
		n, err := strconv.Atoi(string(value))
		return n, err == nil
	}
	return 0, false
}

func (request GraphRequest) viewLimits(r *Result) GraphViewLimits {
	limits := defaultGraphViewLimits
	if request.limits != nil {
		limits = *request.limits
	}
	limits.TypeProjection = r.projectionLimits()
	return limits
}

// graphTupleID encodes an ordered tuple injectively: each component escapes
// the separator and the escape character before joining.
func graphTupleID(prefix string, parts ...string) string {
	var b strings.Builder
	b.WriteString(prefix)
	for _, part := range parts {
		b.WriteByte('|')
		for _, r := range part {
			switch r {
			case '%':
				b.WriteString("%25")
			case '|':
				b.WriteString("%7C")
			default:
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// graphFactNode is one checked fact before publication. Contract projection
// is deferred until the node is selected.
type graphFactNode struct {
	node     GraphViewNode
	contract dependencyContract
	values   []ValueType
}

type graphFactEdge struct {
	edge   GraphViewEdge
	values []ValueType
}

// graphFacts is the complete topology of one kind. It carries identities,
// relations and provenance but no projected contract or type table.
type graphFacts struct {
	kind        GraphKind
	nodes       map[string]*graphFactNode
	edges       []graphFactEdge
	edgeIDs     map[string]bool
	limitations []string
	semantics   string
	work        int
	workLimit   int
	err         error
}

func newGraphFacts(kind GraphKind, limits GraphViewLimits) *graphFacts {
	return &graphFacts{kind: kind, nodes: map[string]*graphFactNode{}, edgeIDs: map[string]bool{}, workLimit: limits.Work}
}

func (f *graphFacts) spend(units int) bool {
	if f.err != nil {
		return false
	}
	if units > f.workLimit-f.work {
		f.err = graphRefusal(GraphRefusalWorkLimit, "graph view work exceeds its %d-unit limit", f.workLimit)
		return false
	}
	f.work += units
	return true
}

// addNode admits a fact once; the first admission owns the identity.
func (f *graphFacts) addNode(node GraphViewNode, contract dependencyContract, values ...ValueType) *graphFactNode {
	if !f.spend(1) {
		return nil
	}
	if existing := f.nodes[node.ID]; existing != nil {
		return existing
	}
	node.Type = "node"
	fact := &graphFactNode{node: node, contract: contract, values: values}
	f.nodes[node.ID] = fact
	return fact
}

func (f *graphFacts) addEdge(edge GraphViewEdge, values ...ValueType) bool {
	if !f.spend(1) {
		return false
	}
	if f.edgeIDs[edge.ID] {
		return true
	}
	edge.Type = "edge"
	f.edgeIDs[edge.ID] = true
	f.edges = append(f.edges, graphFactEdge{edge: edge, values: values})
	return true
}

// dependencyFactCollector admits the dependency walk into graph facts without
// projecting any contract.
type dependencyFactCollector struct{ facts *graphFacts }

func (c dependencyFactCollector) node(id, kind, name string, span Span, contract dependencyContract) bool {
	at := span
	return c.facts.addNode(GraphViewNode{ID: id, Label: name, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: kind, Name: name, Span: &at}}}, contract) != nil
}

func (c dependencyFactCollector) source(id, source string) {
	if node := c.facts.nodes[id]; node != nil {
		node.node.Data.Effra.Source = source
	}
}

func (c dependencyFactCollector) edge(relationship GraphEdge) bool {
	at := relationship.Span
	id := graphTupleID("edge", relationship.Kind, relationship.From, relationship.To, relationship.Service, strconv.Itoa(at.Offset), strconv.Itoa(at.Length))
	return c.facts.addEdge(GraphViewEdge{ID: id, SourceID: relationship.From, TargetID: relationship.To, Label: relationship.Kind, Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: relationship.Kind, Service: relationship.Service, Span: &at}}})
}

func (r *Result) dependencyGraphFacts(limits GraphViewLimits) (*graphFacts, error) {
	facts := newGraphFacts(GraphKindDependency, limits)
	facts.semantics = "checked-static-dependencies"
	facts.limitations = []string{
		"single-file static graph; includes deferred recipe construction, not execution order",
		"ordinary provider recipes have explicit value identities without general memoized acquisition",
		"node IDs containing offsets are scoped to the semantic revision",
	}
	r.walkDependencyFacts(dependencyFactCollector{facts})
	if facts.err != nil {
		return nil, facts.err
	}
	return facts, nil
}

// layerGraphFacts projects every checked LayerPlan: plans, canonical shared
// bindings, per-plan selections with their effective implementation,
// replacement sites, merges and open construction inputs.
func (r *Result) layerGraphFacts(limits GraphViewLimits) (*graphFacts, error) {
	facts := newGraphFacts(GraphKindLayers, limits)
	facts.semantics = "checked-static-layer-plans"
	facts.limitations = []string{
		"checked static layer plans; acquisition and cleanup require runtime execution",
		"shared bindings are one canonical node; per-plan selection facts live on selects edges",
		"source layer effect factories, startup effects and dynamic plans are unsupported",
		"node IDs containing offsets are scoped to the semantic revision",
	}
	plans := append([]LayerPlan{}, r.Layers...)
	slices.SortFunc(plans, func(a, b LayerPlan) int { return strings.Compare(a.ID, b.ID) })
	for i := range plans {
		plan := &plans[i]
		span := plan.Span
		facts.addNode(GraphViewNode{ID: plan.ID, Label: plan.Name, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "layer", Name: plan.Name, Span: &span, Layer: &GraphLayerFacts{
			Provides: plan.Provides, InferredProvides: plan.InferredProvides, Failures: plan.Failures, Requirements: plan.Requirements,
			DeclaredProvides: plan.DeclaredProvides, DeclaredFailures: plan.DeclaredFailures, DeclaredRequirements: plan.DeclaredRequirements,
			Owner: plan.Owner, Evidence: plan.Evidence, ChildFailurePolicy: plan.ChildFailurePolicy,
		}}}}, dependencyContract{})
	}
	for i := range plans {
		plan := &plans[i]
		for _, merge := range plan.Merges {
			at := merge.Span
			facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "merges", plan.ID, merge.ID, strconv.Itoa(at.Offset)), SourceID: plan.ID, TargetID: merge.ID, Label: "merges", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "merges", Span: &at, Plan: plan.ID}}})
		}
		for _, node := range plan.Nodes {
			bindingSpan := node.Span
			facts.addNode(GraphViewNode{ID: node.ID, Label: node.Service, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "layer-binding", Name: node.Service, Span: &bindingSpan, Binding: &GraphBindingFacts{Service: node.Service, ServiceIdentity: node.ServiceIdentity}}}}, dependencyContract{})
			selection := &GraphLayerSelectFacts{
				Public: node.Public, Implementation: node.Implementation, ImplementationIdentity: node.ImplementationIdentity,
				SelectionSpan: node.SelectionSpan, Occurrences: node.Occurrences, Replacements: node.Replacements,
				ConstructionRequirements: node.Requirements, ConstructionFailures: node.Failures,
				Constructor: node.Constructor, Parameters: node.Parameters, Arguments: node.Arguments, Owner: node.Owner,
			}
			facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "selects", plan.ID, node.ID), SourceID: plan.ID, TargetID: node.ID, Label: "selects", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "selects", Plan: plan.ID, Selection: selection}}}, layerNodeContracts(node)...)
			for _, dependency := range node.Dependencies {
				facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "depends-on", node.ID, dependency, plan.ID), SourceID: node.ID, TargetID: dependency, Label: "depends-on", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "depends-on", Plan: plan.ID}}})
			}
			for _, replacement := range node.Replacements {
				at := replacement.Span
				facts.addNode(GraphViewNode{ID: replacement.ID, Label: "replace " + node.Service, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "layer-replacement", Name: replacement.Name, Span: &at}}}, dependencyContract{})
				facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "replaces", replacement.ID, node.ID, plan.ID), SourceID: replacement.ID, TargetID: node.ID, Label: "replaces", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "replaces", Span: &at, Plan: plan.ID}}})
			}
		}
		for _, input := range plan.Requirements {
			id := graphTupleID("layer-input", plan.ID, input)
			facts.addNode(GraphViewNode{ID: id, Label: input, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "layer-input", Name: input}}}, dependencyContract{})
			facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "requires-input", plan.ID, id), SourceID: plan.ID, TargetID: id, Label: "requires-input", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "requires-input", Service: input, Plan: plan.ID}}})
		}
		for _, path := range plan.ConstructionPaths {
			at := path.Span
			input := graphTupleID("layer-input", plan.ID, path.Input)
			for _, consumer := range path.Nodes {
				facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "consumes-input", consumer, input, strconv.Itoa(at.Offset)), SourceID: consumer, TargetID: input, Label: "consumes-input", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "consumes-input", Service: path.Input, Span: &at, Plan: plan.ID}}})
			}
		}
	}
	if facts.err != nil {
		return nil, facts.err
	}
	return facts, nil
}

// GraphLayerFacts are the plan-level contracts of one layer.
type GraphLayerFacts struct {
	Provides             []string  `json:"provides"`
	InferredProvides     []string  `json:"inferredProvides"`
	Failures             []string  `json:"failures"`
	Requirements         []string  `json:"requirements"`
	DeclaredProvides     *[]string `json:"declaredProvides,omitempty"`
	DeclaredFailures     *[]string `json:"declaredFailures,omitempty"`
	DeclaredRequirements *[]string `json:"declaredRequirements,omitempty"`
	Owner                string    `json:"plannedOwner"`
	Evidence             string    `json:"evidence"`
	ChildFailurePolicy   string    `json:"childFailurePolicy"`
}

// GraphBindingFacts identify one canonical binding shared across plans.
type GraphBindingFacts struct {
	Service         string `json:"service"`
	ServiceIdentity string `json:"serviceIdentity"`
}

// GraphLayerSelectFacts are one plan's selection of a binding: visibility,
// effective (possibly replacement) implementation and construction facts.
type GraphLayerSelectFacts struct {
	Public                   bool            `json:"public"`
	Implementation           string          `json:"implementation"`
	ImplementationIdentity   string          `json:"implementationIdentity"`
	SelectionSpan            Span            `json:"selectionSpan"`
	Occurrences              []LayerSite     `json:"occurrences"`
	Replacements             []LayerSite     `json:"replacements"`
	ConstructionRequirements []string        `json:"constructionRequirements"`
	ConstructionFailures     []string        `json:"constructionFailures"`
	Constructor              ValueType       `json:"constructor"`
	Parameters               []Param         `json:"configurationParameters,omitempty"`
	Arguments                []LayerArgument `json:"configurationArguments,omitempty"`
	Owner                    string          `json:"plannedOwner"`
}

// GraphRetentionFacts identify one retained application requirement or a
// typed provenance origin that is not itself a requirement.
type GraphRetentionFacts struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
	Reason   string `json:"reason,omitempty"`
	Retained bool   `json:"retained"`
}

// graphSelection is the selected subgraph before publication.
type graphSelection struct {
	nodes    map[string]bool
	edges    []int
	frontier []string
	parents  map[string]string
	hidden   map[string][]string
	hiddenN  map[string]int
}

// selectGraph applies focus, depth, direction and relation selection to the
// fact topology, then collapse. Nothing is projected here.
func (f *graphFacts) selectGraph(request GraphRequest, limits GraphViewLimits) (*graphSelection, error) {
	allowed := map[string]bool{}
	for _, relation := range request.EdgeKinds {
		allowed[relation] = true
	}
	included := func(edge GraphViewEdge) bool {
		return len(allowed) == 0 || allowed[edge.Data.Effra.Relation]
	}
	for _, fact := range f.edges {
		if f.nodes[fact.edge.SourceID] == nil || f.nodes[fact.edge.TargetID] == nil {
			return nil, graphRefusal(GraphRefusalInvalidView, "fact edge %s has an unpublished endpoint", fact.edge.ID)
		}
	}
	selection := &graphSelection{nodes: map[string]bool{}, parents: map[string]string{}, hidden: map[string][]string{}, hiddenN: map[string]int{}}
	if request.Focus == "" {
		for id := range f.nodes {
			selection.nodes[id] = true
		}
	} else {
		if f.nodes[request.Focus] == nil {
			return nil, graphRefusal(GraphRefusalFocus, "unknown %s graph node %q", f.kind, request.Focus)
		}
		depth := 1
		if request.Depth != nil {
			depth = *request.Depth
		}
		if depth > limits.Depth {
			return nil, graphRefusal(GraphRefusalDepthLimit, "depth %d exceeds the %d-hop limit", depth, limits.Depth)
		}
		outgoing, incoming := map[string][]int{}, map[string][]int{}
		for index, fact := range f.edges {
			if !included(fact.edge) {
				continue
			}
			if !f.spend(1) {
				return nil, f.err
			}
			outgoing[fact.edge.SourceID] = append(outgoing[fact.edge.SourceID], index)
			incoming[fact.edge.TargetID] = append(incoming[fact.edge.TargetID], index)
		}
		neighbors := func(id string, visit func(string)) {
			if request.Direction != GraphIncoming {
				for _, index := range outgoing[id] {
					visit(f.edges[index].edge.TargetID)
				}
			}
			if request.Direction != GraphOutgoing {
				for _, index := range incoming[id] {
					visit(f.edges[index].edge.SourceID)
				}
			}
		}
		selection.nodes[request.Focus] = true
		layer := []string{request.Focus}
		for hop := 0; hop < depth && len(layer) > 0; hop++ {
			next := []string{}
			for _, id := range layer {
				neighbors(id, func(neighbor string) {
					if f.spend(1) && !selection.nodes[neighbor] {
						selection.nodes[neighbor] = true
						next = append(next, neighbor)
					}
				})
			}
			if f.err != nil {
				return nil, f.err
			}
			layer = next
		}
		for _, id := range layer {
			cut := false
			neighbors(id, func(neighbor string) { cut = cut || !selection.nodes[neighbor] })
			if cut {
				selection.frontier = append(selection.frontier, id)
			}
		}
		slices.Sort(selection.frontier)
	}
	for index, fact := range f.edges {
		if included(fact.edge) && selection.nodes[fact.edge.SourceID] && selection.nodes[fact.edge.TargetID] {
			selection.edges = append(selection.edges, index)
		}
	}
	if err := f.collapse(request, selection); err != nil {
		return nil, err
	}
	return selection, nil
}

// collapse hides the interior members of each containment group. A member
// with any selected relation outside its group stays published as a child of
// the group root, so every published edge is an original fact edge and no
// path through the group is fabricated.
func (f *graphFacts) collapse(request GraphRequest, selection *graphSelection) error {
	if len(request.Collapse) == 0 {
		return nil
	}
	containment := graphKindContainment[f.kind]
	children := map[string][]string{}
	for _, fact := range f.edges {
		if fact.edge.Data.Effra.Relation == containment && selection.nodes[fact.edge.SourceID] && selection.nodes[fact.edge.TargetID] {
			children[fact.edge.SourceID] = append(children[fact.edge.SourceID], fact.edge.TargetID)
		}
	}
	group := map[string]string{}
	for _, root := range request.Collapse {
		if !selection.nodes[root] {
			return graphRefusal(GraphRefusalCollapse, "collapse node %q is not in the selected view", root)
		}
		members := []string{}
		queue := append([]string{}, children[root]...)
		for len(queue) > 0 {
			member := queue[0]
			queue = queue[1:]
			if !f.spend(1) {
				return f.err
			}
			if owner, claimed := group[member]; claimed || member == root {
				if claimed && owner == root {
					continue
				}
				return graphRefusal(GraphRefusalCollapse, "collapse groups %q and %q overlap", owner, root)
			}
			group[member] = root
			members = append(members, member)
			queue = append(queue, children[member]...)
		}
		if len(members) == 0 {
			return graphRefusal(GraphRefusalCollapse, "node %q has no selected %s members to collapse", root, containment)
		}
	}
	for _, root := range request.Collapse {
		if owner, nested := group[root]; nested {
			return graphRefusal(GraphRefusalCollapse, "collapse groups %q and %q overlap", owner, root)
		}
	}
	boundary := map[string]bool{}
	for _, index := range selection.edges {
		edge := f.edges[index].edge
		source, target := group[edge.SourceID], group[edge.TargetID]
		sourceRoot, targetRoot := source, target
		if sourceRoot == "" {
			sourceRoot = edge.SourceID
		}
		if targetRoot == "" {
			targetRoot = edge.TargetID
		}
		if sourceRoot == targetRoot {
			continue
		}
		if source != "" {
			boundary[edge.SourceID] = true
		}
		if target != "" {
			boundary[edge.TargetID] = true
		}
	}
	if request.Focus != "" && group[request.Focus] != "" {
		boundary[request.Focus] = true
	}
	for member, root := range group {
		if boundary[member] {
			selection.parents[member] = root
			continue
		}
		delete(selection.nodes, member)
		selection.hidden[root] = append(selection.hidden[root], member)
	}
	kept := selection.edges[:0]
	for _, index := range selection.edges {
		edge := f.edges[index].edge
		if selection.nodes[edge.SourceID] && selection.nodes[edge.TargetID] {
			kept = append(kept, index)
			continue
		}
		root := group[edge.SourceID]
		if selection.nodes[edge.SourceID] {
			root = group[edge.TargetID]
		}
		selection.hiddenN[root]++
	}
	selection.edges = kept
	frontier := selection.frontier[:0]
	for _, id := range selection.frontier {
		if selection.nodes[id] {
			frontier = append(frontier, id)
		}
	}
	selection.frontier = frontier
	for root := range selection.hidden {
		slices.Sort(selection.hidden[root])
	}
	return nil
}

// GraphView projects one selected view of the checked facts of request.Kind.
// Selection runs over fact topology before any contract or type table is
// materialized, so a focused request succeeds where the whole view would
// exceed its limits.
func (r *Result) GraphView(request GraphRequest) (*GraphView, error) {
	if r == nil || !r.Checked {
		return nil, graphRefusal(GraphRefusalUnchecked, "graph views require checked source")
	}
	limits := request.viewLimits(r)
	var facts *graphFacts
	var err error
	switch request.Kind {
	case GraphKindDependency:
		facts, err = r.dependencyGraphFacts(limits)
	case GraphKindLayers:
		facts, err = r.layerGraphFacts(limits)
	case GraphKindApplication:
		facts, err = r.applicationGraphFacts(request, limits)
	default:
		return nil, graphRefusal(GraphRefusalKind, "unknown graph kind %q", request.Kind)
	}
	if err != nil {
		return nil, err
	}
	selection, err := facts.selectGraph(request, limits)
	if err != nil {
		return nil, err
	}
	nodeIDs := make([]string, 0, len(selection.nodes))
	for id := range selection.nodes {
		nodeIDs = append(nodeIDs, id)
	}
	slices.Sort(nodeIDs)
	if len(nodeIDs) > limits.Nodes {
		return nil, graphRefusal(GraphRefusalNodeLimit, "graph view selects %d nodes; limit is %d; select with focus, depth or edge kinds", len(nodeIDs), limits.Nodes)
	}
	if len(selection.edges) > limits.Edges {
		return nil, graphRefusal(GraphRefusalEdgeLimit, "graph view selects %d edges; limit is %d; select with focus, depth or edge kinds", len(selection.edges), limits.Edges)
	}
	view := &GraphView{Mode: "directed", InitialNodeID: request.Focus, Nodes: make([]GraphViewNode, 0, len(nodeIDs)), Edges: make([]GraphViewEdge, 0, len(selection.edges))}
	contracts := []ValueType{}
	compatibility := 0
	for _, id := range nodeIDs {
		fact := facts.nodes[id]
		node := fact.node
		node.ParentID = selection.parents[id]
		if hidden := selection.hidden[id]; len(hidden) > 0 {
			node.Data.Effra.Collapsed = &GraphCollapsedFacts{HiddenMembers: hidden, HiddenEdges: selection.hiddenN[id]}
		}
		node.Data.Effra.Frontier = slices.Contains(selection.frontier, id)
		contract, size, err := r.materializeDependencyContract(fact.contract, limits.TypeProjection.CompatibilityBytes-compatibility)
		if err != nil {
			return nil, graphRefusal(GraphRefusalTypeLimit, "graph view contract metadata: %v", err)
		}
		compatibility += size
		if contract != nil {
			node.Data.Effra.Contract = contract
			contracts = append(contracts, *contract)
		}
		contracts = append(contracts, fact.values...)
		view.Nodes = append(view.Nodes, node)
	}
	for _, index := range selection.edges {
		fact := facts.edges[index]
		view.Edges = append(view.Edges, fact.edge)
		contracts = append(contracts, fact.values...)
	}
	slices.SortFunc(view.Edges, func(a, b GraphViewEdge) int { return strings.Compare(a.ID, b.ID) })
	projection := r.ProjectValues(contracts)
	if !projection.Complete {
		return nil, graphRefusal(GraphRefusalTypeLimit, "graph view type projection unavailable: %s", projection.Error)
	}
	snapshot := r.producerMetadata.Snapshot
	snapshot.SchemaVersion, snapshot.Revision, snapshot.Target = r.SchemaVersion, r.Revision, r.Target
	selected := GraphSelection{Target: r.Target, Focus: request.Focus, Depth: request.Depth, Direction: request.Direction, EdgeKinds: append([]string{}, request.EdgeKinds...), Collapse: append([]string{}, request.Collapse...), Mode: request.Mode}
	scope := "kind"
	if request.Focus != "" || len(request.EdgeKinds) > 0 {
		scope = "selection"
	}
	if selection.frontier == nil {
		selection.frontier = []string{}
	}
	view.Data.Effra = GraphViewFacts{
		GraphViewVersion: GraphViewVersion,
		Kind:             request.Kind,
		Producer:         r.producerMetadata.Producer,
		Snapshot:         snapshot,
		ProducerIdentity: r.ProducerIdentity,
		Selection:        selected,
		Completeness: GraphCompleteness{
			Scope: scope, Complete: true, Semantics: facts.semantics,
			FactNodes: len(facts.nodes), FactEdges: len(facts.edges),
			SelectedNodes: len(nodeIDs) + hiddenCount(selection), SelectedEdges: len(selection.edges) + hiddenEdgeCount(selection),
			PublishedNodes: len(view.Nodes), PublishedEdges: len(view.Edges), Frontier: selection.frontier,
		},
		Facts: GraphFactTables{
			Sources: append([]SourceInfo{}, r.Sources...), Types: nonNilTypes(projection.Types), Rows: nonNilRows(projection.Rows),
			Declarations: nonNilDeclarations(r.ProjectionDeclarations(projection)),
		},
		Limits:      limits,
		Usage:       GraphViewUsage{Nodes: len(view.Nodes), Edges: len(view.Edges), Work: facts.work, MaterializedNodes: len(view.Nodes), TypeProjection: projection.Usage},
		Limitations: append([]string{}, facts.limitations...),
	}
	view.ID = graphViewID(view.Data.Effra)
	if err := ValidateGraphView(view); err != nil {
		return nil, graphRefusal(GraphRefusalInvalidView, "%v", err)
	}
	if _, err := encodedSize(view, limits.ResponseBytes); err != nil {
		return nil, graphRefusal(GraphRefusalResponseLimit, "graph view response: %v", err)
	}
	return view, nil
}

func hiddenCount(selection *graphSelection) int {
	total := 0
	for _, members := range selection.hidden {
		total += len(members)
	}
	return total
}

func hiddenEdgeCount(selection *graphSelection) int {
	total := 0
	for _, count := range selection.hiddenN {
		total += count
	}
	return total
}

func nonNilTypes(values []TypeNode) []TypeNode {
	if values == nil {
		return []TypeNode{}
	}
	return values
}

func nonNilRows(values []RowNode) []RowNode {
	if values == nil {
		return []RowNode{}
	}
	return values
}

func nonNilDeclarations(values []Declaration) []Declaration {
	if values == nil {
		return []Declaration{}
	}
	return values
}

// materializeDependencyContract projects one selected fact's contract under
// the remaining compatibility-metadata budget.
func (r *Result) materializeDependencyContract(contract dependencyContract, remaining int) (*ValueType, int, error) {
	switch {
	case contract.checked != nil:
		checked := *contract.checked
		base := r.projector.projectCheckedBase(checked)
		size, err := r.projector.checkedCompatibilitySize(checked, base, remaining)
		if err != nil {
			return nil, 0, err
		}
		t := r.projector.projectChecked(checked)
		if contract.materialized {
			t.Effect = false
			t.Errors = nil
			t.Services = nil
		}
		return &t, size, nil
	case contract.value != nil:
		t := *contract.value
		size, err := encodedSize(t, remaining)
		return &t, size, err
	}
	return nil, 0, nil
}

// graphViewID identifies a view by kind and normalized selection within its
// qualified snapshot. It names the view; it is not a freshness proof.
func graphViewID(facts GraphViewFacts) string {
	identity, _ := json.Marshal(struct {
		Version          int                   `json:"version"`
		Kind             GraphKind             `json:"kind"`
		Snapshot         SnapshotQualification `json:"snapshot"`
		ProducerIdentity string                `json:"producerIdentity"`
		Selection        GraphSelection        `json:"selection"`
	}{GraphViewVersion, facts.Kind, facts.Snapshot, facts.ProducerIdentity, facts.Selection})
	sum := sha256.Sum256(identity)
	return "effra-graph:" + string(facts.Kind) + ":" + hex.EncodeToString(sum[:16])
}

// ValidateGraphView checks a view's structural closure (unique IDs, existing
// endpoints, parents, initial node and ports, acyclic containment) and its
// semantic closure (every type, row, declaration, source and plan reference
// resolves within the view's own facts).
func ValidateGraphView(view *GraphView) error {
	if view == nil {
		return fmt.Errorf("graph view is absent")
	}
	effra := view.Data.Effra
	if view.ID == "" || view.Mode != "directed" || effra.GraphViewVersion != GraphViewVersion {
		return fmt.Errorf("graph view header is not GraphViewV1")
	}
	if _, known := graphKindRelations[effra.Kind]; !known {
		return fmt.Errorf("graph view kind %q is unknown", effra.Kind)
	}
	nodes := map[string]*GraphViewNode{}
	for i := range view.Nodes {
		node := &view.Nodes[i]
		if node.Type != "node" || node.ID == "" {
			return fmt.Errorf("graph node %d is not a typed node", i)
		}
		if nodes[node.ID] != nil {
			return fmt.Errorf("duplicate graph node %s", node.ID)
		}
		nodes[node.ID] = node
		ports := map[string]bool{}
		for _, port := range node.Ports {
			if port.Name == "" || ports[port.Name] {
				return fmt.Errorf("graph node %s has a duplicate or empty port", node.ID)
			}
			ports[port.Name] = true
		}
	}
	for _, node := range view.Nodes {
		seen := map[string]bool{node.ID: true}
		for parent := node.ParentID; parent != ""; parent = nodes[parent].ParentID {
			if nodes[parent] == nil {
				return fmt.Errorf("graph node %s has unpublished parent %s", node.ID, parent)
			}
			if seen[parent] {
				return fmt.Errorf("graph node %s has cyclic parents", node.ID)
			}
			seen[parent] = true
		}
		if collapsed := node.Data.Effra.Collapsed; collapsed != nil {
			for _, member := range collapsed.HiddenMembers {
				if nodes[member] != nil {
					return fmt.Errorf("hidden member %s of %s is also published", member, node.ID)
				}
			}
		}
	}
	if view.InitialNodeID != "" && nodes[view.InitialNodeID] == nil {
		return fmt.Errorf("initial node %s is not published", view.InitialNodeID)
	}
	relations := graphKindRelations[effra.Kind]
	edges := map[string]bool{}
	for _, edge := range view.Edges {
		if edge.Type != "edge" || edge.ID == "" || edges[edge.ID] {
			return fmt.Errorf("graph edge %q is untyped, empty or duplicated", edge.ID)
		}
		edges[edge.ID] = true
		source, target := nodes[edge.SourceID], nodes[edge.TargetID]
		if source == nil || target == nil {
			return fmt.Errorf("graph edge %s has an unpublished endpoint", edge.ID)
		}
		if !portExists(source, edge.SourcePort) || !portExists(target, edge.TargetPort) {
			return fmt.Errorf("graph edge %s names an unknown port", edge.ID)
		}
		if !slices.Contains(relations, edge.Data.Effra.Relation) {
			return fmt.Errorf("graph edge %s has relation %q outside kind %s", edge.ID, edge.Data.Effra.Relation, effra.Kind)
		}
		if plan := edge.Data.Effra.Plan; plan != "" && effra.Kind == GraphKindLayers && nodes[plan] == nil {
			return fmt.Errorf("graph edge %s references unpublished plan %s", edge.ID, plan)
		}
		if selection := edge.Data.Effra.Selection; selection != nil {
			for _, replacement := range selection.Replacements {
				if nodes[replacement.ID] == nil {
					return fmt.Errorf("graph edge %s references unpublished replacement %s", edge.ID, replacement.ID)
				}
			}
		}
	}
	return validateGraphViewReferences(view)
}

// qualifiedDeclarationPublished resolves a data declaration qualifier
// (kind:module:name:fingerprint) against the published declaration table.
// Provider qualifiers name checked providers, not data declarations; their
// facts are the provider and binding nodes themselves.
func qualifiedDeclarationPublished(qualifier string, declarations []Declaration) bool {
	kind, _, _ := strings.Cut(qualifier, ":")
	switch kind {
	case "record", "enum", "error":
	default:
		return true
	}
	cut := strings.LastIndexByte(qualifier, ':')
	if cut < 0 {
		return false
	}
	for _, declaration := range declarations {
		if declaration.Kind == kind && strings.HasSuffix(qualifier[:cut], ":"+declaration.Name) {
			return true
		}
	}
	return false
}

func portExists(node *GraphViewNode, port string) bool {
	if port == "" {
		return true
	}
	for _, candidate := range node.Ports {
		if candidate.Name == port {
			return true
		}
	}
	return false
}

// validateGraphViewReferences resolves every published reference against the
// view's own fact tables, over the encoded interchange itself.
func validateGraphViewReferences(view *GraphView) error {
	encoded, err := json.Marshal(view)
	if err != nil {
		return err
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return err
	}
	facts := view.Data.Effra.Facts
	types, rows, declarations, sources, parameters := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, node := range facts.Types {
		types[node.ID] = true
	}
	for _, row := range facts.Rows {
		rows[row.ID] = true
		for _, parameter := range row.Parameters {
			parameters[parameter.ID] = true
		}
	}
	for _, declaration := range facts.Declarations {
		declarations[declaration.Identity] = true
	}
	for _, source := range facts.Sources {
		sources[source.ID] = true
	}
	for _, node := range facts.Types {
		if node.Declaration != "" && !declarations[node.Declaration] && !qualifiedDeclarationPublished(node.Declaration, facts.Declarations) {
			return fmt.Errorf("type %s references unpublished declaration %s", node.ID, node.Declaration)
		}
	}
	var walk func(key string, value any) error
	walk = func(key string, value any) error {
		switch value := value.(type) {
		case []any:
			for _, item := range value {
				if err := walk(key, item); err != nil {
					return err
				}
			}
			if key == "args" {
				for _, item := range value {
					if id, ok := item.(string); ok && !types[id] {
						return fmt.Errorf("unpublished type reference args=%q", id)
					}
				}
			}
			if key == "labels" {
				for _, item := range value {
					if id, ok := item.(string); ok && strings.HasPrefix(id, "row-parameter:") && !parameters[id] {
						return fmt.Errorf("unpublished row parameter %q", id)
					}
				}
			}
		case map[string]any:
			for child, item := range value {
				if err := walk(child, item); err != nil {
					return err
				}
			}
		case string:
			if value == "" {
				return nil
			}
			switch key {
			case "ref", "signature":
				if !types[value] {
					return fmt.Errorf("unpublished type reference %s=%q", key, value)
				}
			case "failureRow", "serviceRow", "row":
				if !rows[value] {
					return fmt.Errorf("unpublished row reference %s=%q", key, value)
				}
			case "result":
				if !types[value] {
					return fmt.Errorf("unpublished type reference result=%q", value)
				}
			}
		}
		return nil
	}
	data := wire["data"].(map[string]any)["effra"].(map[string]any)
	tables := data["facts"].(map[string]any)
	if err := walk("types", tables["types"]); err != nil {
		return err
	}
	if err := walk("rows", tables["rows"]); err != nil {
		return err
	}
	for _, collection := range []string{"nodes", "edges"} {
		items, _ := wire[collection].([]any)
		for _, item := range items {
			entity := item.(map[string]any)["data"].(map[string]any)["effra"].(map[string]any)
			if source, ok := entity["source"].(string); ok && source != "" && !sources[source] {
				return fmt.Errorf("unpublished source reference %q", source)
			}
			if err := walk(collection, entity); err != nil {
				return err
			}
		}
	}
	return nil
}

func applicationNodeID(kind ApplicationRequirementKind, identity string) string {
	return graphTupleID("retained", string(kind), identity)
}

// applicationGraphFacts projects one native application plan: each retained
// requirement with its limited first-witness provenance, provider operation
// origins, hidden selected layer nodes with their effective replacements,
// and the runtime catalog closure. It plans; it never builds or emits.
func (r *Result) applicationGraphFacts(request GraphRequest, limits GraphViewLimits) (*graphFacts, error) {
	if r.Target != "go" {
		return nil, graphRefusal(GraphRefusalTarget, "application graphs plan native Go applications; target %s has no application plan", r.Target)
	}
	plan, err := r.ApplicationPlan(graphApplicationModes[request.Mode])
	if err != nil {
		return nil, graphRefusal(GraphRefusalPlan, "%s application plan unavailable: %v", request.Mode, err)
	}
	facts := newGraphFacts(GraphKindApplication, limits)
	facts.semantics = "checked-application-plan-first-witness"
	facts.limitations = []string{
		"native Go application plan for one entry mode; computed without build, emission or execution",
		"provenance is the first checked witness of each requirement, not every path that retains it",
		"callable-value requirements are the conservative target set of every dynamic call",
		"a retained requirement is a planned emission obligation; the view makes no JavaScript retention or bundler tree-shaking claim",
		"runtime-closure nodes are catalog dependencies the selected runtime roots close over",
	}
	layers := map[string]*LayerPlan{}
	for i := range r.Layers {
		layers[r.Layers[i].ID] = &r.Layers[i]
	}
	for _, requirement := range plan.Requirements {
		facts.addNode(GraphViewNode{ID: applicationNodeID(requirement.Kind, requirement.Identity), Label: requirement.Identity, Data: GraphViewNodeData{Effra: GraphNodeFacts{
			Kind: string(requirement.Kind), Name: requirement.Identity,
			Retention: &GraphRetentionFacts{Kind: string(requirement.Kind), Identity: requirement.Identity, Reason: requirement.Reason, Retained: true},
		}}}, dependencyContract{})
	}
	for _, origin := range plan.Origins {
		facts.addNode(GraphViewNode{ID: applicationNodeID(origin.Kind, origin.Identity), Label: origin.Identity, Data: GraphViewNodeData{Effra: GraphNodeFacts{
			Kind: string(origin.Kind), Name: origin.Identity,
			Retention: &GraphRetentionFacts{Kind: string(origin.Kind), Identity: origin.Identity},
		}}}, dependencyContract{})
	}
	retains := func(viaKind ApplicationRequirementKind, via string, kind ApplicationRequirementKind, identity, reason string) error {
		source, target := applicationNodeID(viaKind, via), applicationNodeID(kind, identity)
		if facts.nodes[source] == nil {
			return graphRefusal(GraphRefusalInvalidView, "application requirement %s %s has unretained witness %s %s", kind, identity, viaKind, via)
		}
		facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "retains", source, target), SourceID: source, TargetID: target, Label: reason, Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "retains", Reason: reason, FirstWitness: true}}})
		return nil
	}
	for _, requirement := range plan.Requirements {
		if requirement.Via == "" {
			continue
		}
		if err := retains(requirement.ViaKind, requirement.Via, requirement.Kind, requirement.Identity, requirement.Reason); err != nil {
			return nil, err
		}
		if requirement.Kind != RequiresLayerNode {
			continue
		}
		layer := layers[requirement.Via]
		node := layerPlanNode(layer, requirement.Identity)
		if node == nil {
			return nil, graphRefusal(GraphRefusalInvalidView, "retained layer node %s has no checked plan selection", requirement.Identity)
		}
		fact := facts.nodes[applicationNodeID(requirement.Kind, requirement.Identity)]
		fact.node.Label = node.Service
		fact.node.Data.Effra.Binding = &GraphBindingFacts{Service: node.Service, ServiceIdentity: node.ServiceIdentity}
		fact.node.Data.Effra.Selection = &GraphLayerSelectFacts{
			Public: node.Public, Implementation: node.Implementation, ImplementationIdentity: node.ImplementationIdentity,
			SelectionSpan: node.SelectionSpan, Occurrences: node.Occurrences, Replacements: node.Replacements,
			ConstructionRequirements: node.Requirements, ConstructionFailures: node.Failures,
			Constructor: node.Constructor, Parameters: node.Parameters, Arguments: node.Arguments, Owner: node.Owner,
		}
		fact.values = layerNodeContracts(*node)
	}
	for _, origin := range plan.Origins {
		if err := retains(origin.ViaKind, origin.Via, origin.Kind, origin.Identity, string(origin.Kind)); err != nil {
			return nil, err
		}
	}
	closure, err := plan.RuntimeModuleClosure()
	if err != nil {
		return nil, graphRefusal(GraphRefusalPlan, "application runtime closure: %v", err)
	}
	for _, module := range closure {
		id := applicationNodeID(RequiresRuntimeModule, string(module))
		if facts.nodes[id] == nil {
			facts.addNode(GraphViewNode{ID: graphTupleID("runtime-closure", string(module)), Label: string(module), Data: GraphViewNodeData{Effra: GraphNodeFacts{
				Kind: "runtime-closure", Name: string(module),
				Retention: &GraphRetentionFacts{Kind: string(RequiresRuntimeModule), Identity: string(module), Reason: "catalog-dependency", Retained: true},
			}}}, dependencyContract{})
		}
	}
	moduleNode := func(module rt.RuntimeModule) string {
		if id := applicationNodeID(RequiresRuntimeModule, string(module)); facts.nodes[id] != nil {
			return id
		}
		return graphTupleID("runtime-closure", string(module))
	}
	for _, module := range closure {
		dependencies, err := rt.ModuleDependencies(module)
		if err != nil {
			return nil, graphRefusal(GraphRefusalPlan, "application runtime closure: %v", err)
		}
		for _, dependency := range dependencies {
			source, target := moduleNode(module), moduleNode(dependency)
			facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "runtime-requires", source, target), SourceID: source, TargetID: target, Label: "runtime-requires", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "runtime-requires"}}})
		}
	}
	if facts.err != nil {
		return nil, facts.err
	}
	return facts, nil
}

func layerPlanNode(plan *LayerPlan, id string) *LayerNode {
	if plan == nil {
		return nil
	}
	for i := range plan.Nodes {
		if plan.Nodes[i].ID == id {
			return &plan.Nodes[i]
		}
	}
	return nil
}
