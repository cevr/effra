package compiler

import (
	"fmt"
	"slices"
)

type GraphNode struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Name     string     `json:"name"`
	Source   string     `json:"source,omitempty"`
	Span     Span       `json:"span"`
	Contract *ValueType `json:"contract,omitempty"`
	Incoming []string   `json:"incoming,omitempty"`
}
type GraphEdge struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Kind    string `json:"kind"`
	Service string `json:"service,omitempty"`
	Span    Span   `json:"span"`
}
type DependencyGraph struct {
	ProducerMetadata
	ProducerIdentity       string                 `json:"producerIdentity"`
	Sources                []SourceInfo           `json:"sources"`
	BundledBindings        []BundledBinding       `json:"bundledBindings"`
	BundledInterfaces      []BundledInterfaceInfo `json:"bundledInterfaces"`
	SchemaVersion          int                    `json:"schemaVersion"`
	Revision               string                 `json:"revision"`
	Target                 string                 `json:"target"`
	Nodes                  []GraphNode            `json:"nodes"`
	Edges                  []GraphEdge            `json:"edges"`
	Layers                 []LayerPlan            `json:"layers,omitempty"`
	Types                  []TypeNode             `json:"types,omitempty"`
	Rows                   []RowNode              `json:"rows,omitempty"`
	Declarations           []Declaration          `json:"declarations,omitempty"`
	TypeProjectionLimits   ProjectionLimits       `json:"typeProjectionLimits"`
	TypeProjectionUsage    ProjectionUsage        `json:"typeProjectionUsage"`
	TypeProjectionComplete bool                   `json:"typeProjectionComplete"`
	TypeProjectionError    string                 `json:"typeProjectionError,omitempty"`
	Limitations            []string               `json:"limitations"`
}

type providerBinding struct {
	id     string
	recipe bool
}

const maxGraphNodes = 1000
const maxGraphEdges = 2000

// dependencyContract is the unprojected contract of one dependency fact.
// Projection is the expensive, budgeted part of publication, so the walker
// hands it to its sink instead of projecting every fact it visits.
type dependencyContract struct {
	checked *checkedExpression
	// value is the contract the compatibility graph publishes. For functions
	// it is the symbol's whole-source publication, which a source over the
	// publication budget truncates; selected views project checked instead.
	value *ValueType
	// materialized marks a provider value produced by `run`: it is reusable
	// and has no unresolved row of its own.
	materialized bool
}

// dependencyFactSink receives the dependency facts of one checked file in
// traversal order. A sink returning false stops the walk.
type dependencyFactSink interface {
	node(id, kind, name string, span Span, contract dependencyContract) bool
	source(id, source string)
	edge(GraphEdge) bool
}

// walkDependencyFacts is the single owner of the static dependency fact
// family: lexical construction, declared requirements, provision boundaries
// and calls. The legacy graph and every graph view serialize these facts;
// neither re-derives them.
func (r *Result) walkDependencyFacts(sink dependencyFactSink) {
	nodes := map[string]bool{}
	stopped := false
	add := func(id, kind, name string, span Span, contract dependencyContract) {
		if stopped {
			return
		}
		if !sink.node(id, kind, name, span, contract) {
			stopped = true
			return
		}
		nodes[id] = true
	}
	edge := func(from, to, kind, service string, span Span) {
		if !stopped && !sink.edge(GraphEdge{from, to, kind, service, span}) {
			stopped = true
		}
	}
	requires := func(from, requirement string, span Span, source string) {
		if parameter, abstract := r.projector.rowDefinitions[requirement]; abstract {
			add(requirement, "row-parameter", parameter.Name, parameter.Span, dependencyContract{})
			if !stopped {
				sink.source(requirement, source)
			}
			edge(from, requirement, "requires", "", span)
		} else {
			edge(from, "service:"+requirement, "requires", requirement, span)
		}
	}
	serviceNames := make([]string, 0, len(r.checkedServices))
	for name := range r.checkedServices {
		serviceNames = append(serviceNames, name)
	}
	slices.Sort(serviceNames)
	for _, name := range serviceNames {
		s := r.checkedServices[name]
		add("service:"+s.Name, "service", s.Name, s.Span, dependencyContract{})
	}
	providerNames := make([]string, 0, len(r.checkedProviders))
	for name := range r.checkedProviders {
		providerNames = append(providerNames, name)
	}
	slices.Sort(providerNames)
	for _, name := range providerNames {
		p := r.checkedProviders[name]
		var constructor dependencyContract
		if providerConstructed(p) {
			root := r.checkedProviderRoots[p]
			constructor.checked = &root
		}
		add("provider:"+p.Name, "provider", p.Name, p.Span, constructor)
		edge("provider:"+p.Name, "service:"+p.Service, "implements", p.Service, p.Span)
		for _, requirement := range normalized(p.Services) {
			edge("provider:"+p.Name, "service:"+requirement, "requires", requirement, p.Span)
		}
	}
	for _, s := range r.Symbols {
		contract := s.Contract
		declared := r.checkedSymbols[s.Identity].contract
		add("function:"+s.Name, "function", s.Name, s.Span, dependencyContract{value: &contract, checked: &declared})
		if !stopped {
			sink.source("function:"+s.Name, s.Source)
		}
		for _, req := range s.Contract.Services {
			requires("function:"+s.Name, req, s.Span, s.Source)
		}
	}
	for _, plan := range r.Layers {
		add(plan.ID, "layer-plan", plan.Name, plan.Span, dependencyContract{})
	}
	providerOrigins := map[*Expr]providerBinding{}
	var block func(*Block, string, map[string]providerBinding)
	var expr func(*Expr, string, map[string]providerBinding) string
	expr = func(e *Expr, owner string, locals map[string]providerBinding) string {
		if e == nil || stopped {
			return ""
		}
		id := owner
		if e.Type.Effect || e.Kind == "run" || e.Kind == "scope" || e.Kind == "fork" {
			id = fmt.Sprintf("expression:%s:%d:%s", owner, e.Span.Offset, e.Kind)
			add(id, e.Kind, e.Name, e.Span, dependencyContract{checked: &e.checked})
			edge(owner, id, "contains", "", e.Span)
			for _, req := range e.Type.Services {
				requires(id, req, e.Span, "source:user")
			}
		}
		left := ""
		leftSet := false
		forEachExprChild(e, func(child *Expr) {
			childID := expr(child, id, locals)
			if child == e.Left && !leftSet {
				left = childID
				leftSet = true
			}
		})
		for _, arm := range e.Arms {
			block(arm.Body, id, cloneStringMap(locals))
		}
		block(e.Then, id, cloneStringMap(locals))
		block(e.Else, id, cloneStringMap(locals))
		if e.Kind == "call" && e.Text == "provider-constructor" {
			recipe := fmt.Sprintf("provider-recipe:%s:%d", owner, e.Span.Offset)
			add(recipe, "provider-recipe", e.Left.Name, e.Span, dependencyContract{checked: &e.checked})
			providerOrigins[e] = providerBinding{id: recipe, recipe: true}
			edge(id, recipe, "constructs", e.Left.Name, e.Span)
			edge(recipe, "provider:"+e.Left.Name, "originates", "", e.Span)
		}
		if e.Kind == "run" && e.Type.Type.Kind == "provider" {
			if recipe, ok := providerOrigin(e.Left, locals, providerOrigins, nodes); ok && recipe.recipe {
				provider := "provider-value:" + id
				// The value materialized by `run` is reusable and has no
				// unresolved row of its own. Each run gets its own identity,
				// even when it executes the same lazy constructor recipe.
				add(provider, "provider-value", e.Type.Type.Name, e.Span, dependencyContract{checked: &e.checked, materialized: true})
				providerOrigins[e] = providerBinding{id: provider}
				edge(id, provider, "materializes", e.Type.Type.Name, e.Span)
				edge(provider, recipe.id, "originates", "", e.Span)
			}
		}
		if e.Kind == "provide" {
			edge(id, left, "adapts", "", e.Span)
			binding, ok := providerOrigin(e.Right, locals, providerOrigins, nodes)
			provider := binding.id
			if !ok {
				provider = fmt.Sprintf("provider-value:%s:%d", owner, e.Right.Span.Offset)
				add(provider, "provider-value", e.Right.Name, e.Right.Span, dependencyContract{checked: &e.Right.checked})
			}
			edge(id, provider, "provides", e.Name, e.Span)
		}
		if e.Kind == "provideLayer" && e.layerPlan != nil {
			edge(id, left, "adapts", "", e.Span)
			edge(id, e.layerPlan.ID, "provides-layer", e.Name, e.Span)
		}
		if e.Kind == "call" {
			if f := e.ResolvedFunction; f != nil {
				if f.Module != "" && f.Module != currentModuleIdentity {
					edge(id, "function:"+f.Module+"."+f.Name, "calls", "", e.Span)
				}
				if f.Owner == "module" && e.Left.Kind == "name" && nodes["function:"+f.Name] {
					edge(id, "function:"+f.Name, "calls", "", e.Span)
				}
				if e.Left.Kind == "member" && e.Left.Left.Kind == "name" && f.Owner == "service:"+e.Left.Left.Name && nodes["service:"+e.Left.Left.Name] {
					edge(id, "service:"+e.Left.Left.Name, "calls", e.Left.Left.Name, e.Span)
				}
			}
		}
		if f := e.ResolvedFunction; f != nil && e.Kind != "call" {
			name := f.Name
			if f.Module != "" && f.Module != currentModuleIdentity {
				name = f.Module + "." + f.Name
			}
			if nodes["function:"+name] {
				edge(id, "function:"+name, "references", "", e.Span)
			}
		}
		return id
	}
	block = func(b *Block, owner string, locals map[string]providerBinding) {
		if b != nil {
			for _, s := range b.Statements {
				expr(s.Value, owner, locals)
				expr(s.Payload, owner, locals)
				if s.Kind == "let" {
					if origin, ok := providerOrigin(s.Value, locals, providerOrigins, nodes); ok {
						locals[s.Name] = origin
					} else {
						delete(locals, s.Name)
					}
				}
			}
		}
	}
	for _, f := range r.Program.Functions {
		block(f.Body, "function:"+f.Name, map[string]providerBinding{})
	}
	for _, p := range r.Program.Providers {
		for _, f := range p.Methods {
			id := "provider-method:" + p.Name + "." + f.Name
			contract := r.checkedFunctions[f].contract
			add(id, "provider-method", p.Name+"."+f.Name, f.Span, dependencyContract{checked: &contract})
			edge("provider:"+p.Name, id, "contains", "", f.Span)
			block(f.Body, id, map[string]providerBinding{})
		}
	}
}

// legacyGraphSink is the compatibility serialization of the dependency facts:
// the frozen schema-7 `ef graph` result with its whole-file node, edge and
// compatibility-metadata budgets charged in traversal order.
type legacyGraphSink struct {
	r             *Result
	g             *DependencyGraph
	nodes         map[string]bool
	metadataBytes int
	err           error
}

func (s *legacyGraphSink) project(contract dependencyContract) (*ValueType, error) {
	switch {
	case contract.value != nil:
		t := *contract.value
		return &t, nil
	case contract.checked != nil:
		checked := *contract.checked
		base := s.r.projector.projectCheckedBase(checked)
		if _, err := s.r.projector.checkedCompatibilitySize(checked, base, s.r.projectionLimits().CompatibilityBytes-s.metadataBytes); err != nil {
			return nil, err
		}
		t := s.r.projector.projectChecked(checked)
		if contract.materialized {
			t.Effect = false
			t.Errors = nil
			t.Services = nil
		}
		return &t, nil
	}
	return nil, nil
}

func (s *legacyGraphSink) node(id, kind, name string, span Span, contract dependencyContract) bool {
	if s.err != nil {
		return false
	}
	projected, err := s.project(contract)
	if err != nil {
		s.err = err
		return false
	}
	if s.nodes[id] {
		return true
	}
	if len(s.g.Nodes) >= maxGraphNodes {
		s.err = fmt.Errorf("dependency graph exceeds %d nodes", maxGraphNodes)
		return false
	}
	wire := GraphNode{ID: id, Kind: kind, Name: name, Span: span, Contract: projected}
	size, err := encodedSize(wire, s.r.projectionLimits().CompatibilityBytes-s.metadataBytes)
	s.metadataBytes += size
	if err != nil {
		s.err = err
		return false
	}
	s.nodes[id] = true
	s.g.Nodes = append(s.g.Nodes, wire)
	return true
}

func (s *legacyGraphSink) source(id, source string) {
	for i := len(s.g.Nodes) - 1; i >= 0; i-- {
		if s.g.Nodes[i].ID == id {
			s.g.Nodes[i].Source = source
			return
		}
	}
}

func (s *legacyGraphSink) edge(relationship GraphEdge) bool {
	if s.err != nil {
		return false
	}
	if len(s.g.Edges) >= maxGraphEdges {
		s.err = fmt.Errorf("dependency graph exceeds %d edges", maxGraphEdges)
		return false
	}
	s.g.Edges = append(s.g.Edges, relationship)
	return true
}

// Graph describes lexical construction and declared dependency edges. It is not
// a runtime allocation graph or a claim about execution order or layer sharing.
// It is the frozen legacy serialization; GraphView publishes selected views of
// the same facts.
func (r *Result) Graph() (*DependencyGraph, error) {
	if !r.Checked {
		return nil, fmt.Errorf("dependency graphs require checked source")
	}
	if len(r.checkedServices)+len(r.checkedProviders)+len(r.Symbols) > maxGraphNodes {
		return nil, fmt.Errorf("dependency graph exceeds %d nodes; use selected inspection", maxGraphNodes)
	}
	g := &DependencyGraph{SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target, Nodes: []GraphNode{}, Edges: []GraphEdge{}, Limitations: []string{"single-file static graph; includes deferred recipe construction, not execution order", "checked static layers share selected nodes within each provision; ordinary provider recipes have explicit value identities without general memoized acquisition", "source layer effect factories, startup effects and dynamic plans are unsupported", "node IDs containing offsets are scoped to the semantic revision"}}
	g.ProducerIdentity, g.Sources = r.ProducerIdentity, append([]SourceInfo{}, r.Sources...)
	g.ProducerMetadata = r.producerMetadata
	g.BundledBindings = append([]BundledBinding{}, r.BundledBindings...)
	g.BundledInterfaces = append([]BundledInterfaceInfo{}, r.BundledInterfaces...)
	g.Layers = append([]LayerPlan{}, r.Layers...)
	sink := &legacyGraphSink{r: r, g: g, nodes: map[string]bool{}}
	r.walkDependencyFacts(sink)
	if sink.err != nil {
		return nil, sink.err
	}
	incoming := map[string][]string{}
	for _, relationship := range g.Edges {
		if sink.nodes[relationship.To] {
			incoming[relationship.To] = append(incoming[relationship.To], relationship.From)
		}
	}
	slices.SortFunc(g.Nodes, func(a, b GraphNode) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	for i := range g.Nodes {
		relationships := incoming[g.Nodes[i].ID]
		slices.Sort(relationships)
		g.Nodes[i].Incoming = slices.Compact(relationships)
	}
	contracts := make([]ValueType, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		if node.Contract != nil {
			contracts = append(contracts, *node.Contract)
		}
	}
	contracts = append(contracts, layerPlanContracts(g.Layers)...)
	projection := r.ProjectValues(contracts)
	if !projection.Complete {
		return nil, fmt.Errorf("type projection unavailable: %s", projection.Error)
	}
	g.Types = projection.Types
	g.Rows = projection.Rows
	g.Declarations = r.ProjectionDeclarations(projection)
	g.TypeProjectionLimits = projection.Limits
	g.TypeProjectionUsage = projection.Usage
	g.TypeProjectionComplete = projection.Complete
	g.TypeProjectionError = projection.Error
	if projection.Complete {
		if usage, err := r.ValidateProjectionResponse(projection, g); err != nil {
			return nil, err
		} else {
			g.TypeProjectionUsage = usage
		}
	}
	return g, nil
}

// layerPlanContracts lists the canonical roots a layer plan publishes:
// effective constructors, configuration parameters and argument types.
func layerPlanContracts(plans []LayerPlan) []ValueType {
	contracts := []ValueType{}
	for _, layer := range plans {
		for _, node := range layer.Nodes {
			contracts = append(contracts, layerNodeContracts(node)...)
		}
	}
	return contracts
}

func layerNodeContracts(node LayerNode) []ValueType {
	contracts := []ValueType{node.Constructor}
	for _, parameter := range node.Parameters {
		contracts = append(contracts, ValueType{Type: parameter.TypeRef})
	}
	for _, argument := range node.Arguments {
		contracts = append(contracts, argument.Type)
	}
	return contracts
}

func cloneStringMap(values map[string]providerBinding) map[string]providerBinding {
	clone := map[string]providerBinding{}
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func providerOrigin(e *Expr, locals map[string]providerBinding, origins map[*Expr]providerBinding, nodes map[string]bool) (providerBinding, bool) {
	if e == nil {
		return providerBinding{}, false
	}
	if origin, ok := origins[e]; ok {
		return origin, true
	}
	switch e.Kind {
	case "name":
		if e.Text == "local" {
			origin, ok := locals[e.Name]
			return origin, ok
		}
		if e.Text == "provider" && nodes["provider:"+e.Name] {
			return providerBinding{id: "provider:" + e.Name}, true
		}
	case "provide", "catch":
		return providerOrigin(e.Left, locals, origins, nodes)
	}
	return providerBinding{}, false
}
