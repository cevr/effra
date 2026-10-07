package compiler

import (
	"fmt"
	"slices"
	"strings"
)

const maxLayerNodes = 1000
const maxLayerEdges = 10000
const maxLayerDeclarations = 256
const maxLayerAssemblyMetadata = 100000
const maxLayerRelatedLocations = 64

// One budget accounts for every layer in the checked file. Identity
// deduplication bounds nodes; this also bounds repeated merge work, copied
// provenance, rows and diagnostics before they allocate retained metadata.
type layerAssemblyBudget struct {
	used    int
	refused bool
}

func (c *checker) layerWork(count int, span Span) bool {
	if c.layerBudget == nil {
		return true
	}
	if c.layerBudget.refused {
		return false
	}
	if count < 0 || count > maxLayerAssemblyMetadata-c.layerBudget.used {
		if !c.layerBudget.refused {
			c.layerBudget.refused = true
			c.diagnostic("EF133", "layer assembly exceeds its 100000 work and metadata budget", span)
		}
		return false
	}
	c.layerBudget.used += count
	return true
}

// LayerPlan is the complete selected graph, including hidden bindings. Output
// projection never prunes construction. These are static facts, not runtime
// acquisition receipts or package-loader support.
type LayerPlan struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	Span                 Span        `json:"span"`
	Provides             []string    `json:"provides"`
	InferredProvides     []string    `json:"inferredProvides"`
	Failures             []string    `json:"failures"`
	Requirements         []string    `json:"requirements"`
	DeclaredProvides     *[]string   `json:"declaredProvides,omitempty"`
	DeclaredFailures     *[]string   `json:"declaredFailures,omitempty"`
	DeclaredRequirements *[]string   `json:"declaredRequirements,omitempty"`
	Nodes                []LayerNode `json:"nodes"`
	Merges               []LayerSite `json:"merges"`
	ConstructionPaths    []LayerPath `json:"constructionPaths"`
	Owner                string      `json:"plannedOwner"`
	Evidence             string      `json:"evidence"`
	ChildFailurePolicy   string      `json:"childFailurePolicy"`
	selected             map[string]*layerSelection
	order                []string
}
type LayerSite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Span Span   `json:"span"`
}
type LayerNode struct {
	ID                     string          `json:"id"`
	Service                string          `json:"service"`
	ServiceIdentity        string          `json:"serviceIdentity"`
	Implementation         string          `json:"implementation"`
	ImplementationIdentity string          `json:"implementationIdentity"`
	Span                   Span            `json:"span"`
	SelectionSpan          Span            `json:"selectionSpan"`
	Public                 bool            `json:"public"`
	Occurrences            []LayerSite     `json:"occurrences"`
	Replacements           []LayerSite     `json:"replacements"`
	Dependencies           []string        `json:"dependencies"`
	Incoming               []string        `json:"incoming"`
	Requirements           []string        `json:"constructionRequirements"`
	Failures               []string        `json:"constructionFailures"`
	Constructor            ValueType       `json:"constructor"`
	Parameters             []Param         `json:"configurationParameters,omitempty"`
	Arguments              []LayerArgument `json:"configurationArguments,omitempty"`
	Owner                  string          `json:"plannedOwner"`
}
type LayerArgument struct {
	Span Span      `json:"span"`
	Type ValueType `json:"type"`
}
type LayerPath struct {
	Nodes []string `json:"nodes"`
	Input string   `json:"input,omitempty"`
	Span  Span     `json:"span"`
}
type layerSelection struct {
	id           string
	service      string
	origin       *LayerEntry
	effective    *LayerEntry
	provider     *Provider
	public       bool
	occurrences  []LayerSite
	replacements []LayerSite
}

func layerID(name string) string { return "layer:" + currentModuleIdentity + ":" + name }
func layerBindingID(layer *Layer, entry *LayerEntry) string {
	return fmt.Sprintf("%s:binding:%s:%d", layerID(layer.Name), entry.Name, entry.Span.Offset)
}
func layerSelectionSite(layer *Layer, entry *LayerEntry) LayerSite {
	return LayerSite{ID: fmt.Sprintf("%s:%s:%d", layerID(layer.Name), entry.Kind, entry.Span.Offset), Name: layer.Name, Span: entry.Span}
}

func (c *checker) layerDiagnostic(code, message string, span Span, related ...RelatedLocation) {
	if !c.suppressDiagnostics {
		if len(related) > maxLayerRelatedLocations {
			related = related[:maxLayerRelatedLocations]
		}
		if !c.layerWork(1+len(related), span) {
			return
		}
		c.result.Diagnostics = append(c.result.Diagnostics, Diagnostic{Code: code, Message: message, Span: span, Related: related})
	}
}

// Layer declarations are processed once in dependency order. Each merge
// unions interned binding identities; a shared diamond never recursively
// expands the declarations of its ancestors.
func (c *checker) checkLayers() {
	c.layers = map[string]*LayerPlan{}
	c.layerBudget = &layerAssemblyBudget{}
	c.layerProviders = map[*LayerEntry]*Provider{}
	if len(c.program.Layers) > maxLayerDeclarations {
		c.layerDiagnostic("EF133", "static layer profile exceeds 256 declarations", c.program.Layers[maxLayerDeclarations].Span)
		return
	}
	declarations := map[string]*Layer{}
	for _, layer := range c.program.Layers {
		if !c.layerWork(1+len(layer.Entries), layer.Span) {
			return
		}
		declarations[layer.Name] = layer
	}
	remaining := map[string]int{}
	dependents := map[string][]string{}
	for _, layer := range c.program.Layers {
		deps := map[string]bool{}
		for _, entry := range layer.Entries {
			if entry.Kind != "merge" {
				continue
			}
			if declarations[entry.Name] == nil {
				c.layerDiagnostic("EF102", "unknown layer "+entry.Name, entry.Span)
				continue
			}
			deps[entry.Name] = true
		}
		remaining[layer.Name] = len(deps)
		for dep := range deps {
			dependents[dep] = append(dependents[dep], layer.Name)
		}
	}
	ready := []string{}
	for _, layer := range c.program.Layers {
		if remaining[layer.Name] == 0 {
			ready = append(ready, layer.Name)
		}
	}
	for head := 0; head < len(ready); head++ {
		if c.layerBudget.refused {
			return
		}
		name := ready[head]
		c.layers[name] = c.assembleLayer(declarations[name])
		for _, dependent := range dependents[name] {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	for _, layer := range c.program.Layers {
		if c.layers[layer.Name] == nil {
			related := []RelatedLocation{}
			for _, entry := range layer.Entries {
				if entry.Kind == "merge" && remaining[entry.Name] > 0 && len(related) < maxLayerRelatedLocations {
					related = append(related, RelatedLocation{Message: "unresolved construction merge " + entry.Name, Span: entry.Span})
				}
			}
			c.layerDiagnostic("EF132", "cyclic layer merge involving "+layer.Name, layer.Span, related...)
		}
	}
	c.result.Layers = []LayerPlan{}
	for _, layer := range c.program.Layers {
		if plan := c.layers[layer.Name]; plan != nil {
			c.result.Layers = append(c.result.Layers, *plan)
		}
	}
}

func cloneSelection(node *layerSelection) *layerSelection {
	copy := *node
	copy.occurrences = append([]LayerSite{}, node.occurrences...)
	copy.replacements = append([]LayerSite{}, node.replacements...)
	return &copy
}

func (c *checker) assembleLayer(layer *Layer) *LayerPlan {
	plan := &LayerPlan{ID: layerID(layer.Name), Name: layer.Name, Span: layer.Span,
		Provides: []string{}, InferredProvides: []string{}, Failures: []string{}, Requirements: []string{}, Nodes: []LayerNode{}, Merges: []LayerSite{}, ConstructionPaths: []LayerPath{},
		Owner: "provision-build", Evidence: "checked-static-plan; acquisition and cleanup require runtime execution", ChildFailurePolicy: "observed at node owner closure", selected: map[string]*layerSelection{}}
	// Retained identity bytes share the existing cumulative work/metadata
	// budget. This also bounds every emitted NewPlan, including empty graphs.
	if !c.layerWork(len(plan.ID), layer.Span) {
		return plan
	}
	if len(layer.Entries) > maxLayerEdges {
		c.layerDiagnostic("EF133", "layer exceeds 10000 composition entries", layer.Span)
		return plan
	}
	variants := map[string]map[*LayerEntry]*layerSelection{}
	outer := map[string]*LayerEntry{}
	for _, entry := range layer.Entries {
		if entry.Kind == "replace" {
			if previous := outer[entry.Name]; previous != nil {
				c.layerDiagnostic("EF131", "two replacements for "+entry.Name+" in "+layer.Name, entry.Span, RelatedLocation{Message: "previous replacement", Span: previous.Span})
			}
			outer[entry.Name] = entry
		}
	}
	add := func(node *layerSelection, occurrence *LayerSite) bool {
		occurrences := len(node.occurrences)
		if occurrence != nil {
			occurrences = 1
		}
		if !c.layerWork(1+occurrences+len(node.replacements), layer.Span) {
			return false
		}
		if variants[node.id] == nil {
			variants[node.id] = map[*LayerEntry]*layerSelection{}
		}
		if existing := variants[node.id][node.effective]; existing != nil {
			existing.public = existing.public || node.public
			if occurrence != nil {
				existing.occurrences = append(existing.occurrences, *occurrence)
			} else {
				existing.occurrences = append(existing.occurrences, node.occurrences...)
			}
		} else {
			copy := *node
			if occurrence != nil {
				copy.occurrences = []LayerSite{*occurrence}
			} else {
				copy.occurrences = append([]LayerSite{}, node.occurrences...)
			}
			copy.replacements = append([]LayerSite{}, node.replacements...)
			variants[node.id][node.effective] = &copy
		}
		return true
	}
	for _, entry := range layer.Entries {
		switch entry.Kind {
		case "binding":
			provider := c.layerProvider(entry)
			if provider != nil {
				if !add(&layerSelection{id: layerBindingID(layer, entry), service: entry.Name, origin: entry, effective: entry, provider: provider, public: true, occurrences: []LayerSite{layerSelectionSite(layer, entry)}}, nil) {
					return plan
				}
			}
		case "merge":
			if !c.layerWork(1, entry.Span) {
				return plan
			}
			plan.Merges = append(plan.Merges, LayerSite{ID: layerID(entry.Name), Name: entry.Name, Span: entry.Span})
			if merged := c.layers[entry.Name]; merged != nil {
				occurrence := layerSelectionSite(layer, entry)
				for _, id := range merged.order {
					if !add(merged.selected[id], &occurrence) {
						return plan
					}
				}
			}
		}
		if len(variants) > maxLayerNodes {
			c.layerDiagnostic("EF133", "layer exceeds 1000 selected nodes", layer.Span)
			return plan
		}
	}
	ids := make([]string, 0, len(variants))
	for id := range variants {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	services := map[string]*layerSelection{}
	for _, id := range ids {
		alternatives := variants[id]
		if !c.layerWork(len(alternatives), layer.Span) {
			return plan
		}
		var node *layerSelection
		// Order diagnostic paths by source position rather than map iteration.
		choices := make([]*layerSelection, 0, len(alternatives))
		for _, choice := range alternatives {
			choices = append(choices, choice)
		}
		slices.SortFunc(choices, func(a, b *layerSelection) int { return a.effective.Span.Offset - b.effective.Span.Offset })
		if !c.layerWork(1+len(choices[0].occurrences)+len(choices[0].replacements), layer.Span) {
			return plan
		}
		node = cloneSelection(choices[0])
		for _, choice := range choices[1:] {
			if !c.layerWork(len(choice.occurrences), layer.Span) {
				return plan
			}
			node.public = node.public || choice.public
			node.occurrences = append(node.occurrences, choice.occurrences...)
		}
		if replacement := outer[node.service]; replacement != nil {
			if !c.layerWork(1, replacement.Span) {
				return plan
			}
			if provider := c.layerProvider(replacement); provider != nil {
				node.provider = provider
				node.effective = replacement
				node.replacements = append(node.replacements, layerSelectionSite(layer, replacement))
			}
		} else if len(choices) > 1 {
			related := []RelatedLocation{}
			for _, choice := range choices[:min(len(choices), maxLayerRelatedLocations)] {
				related = append(related, RelatedLocation{Message: "conflicting effective selection of " + node.service, Span: choice.effective.Span})
			}
			c.layerDiagnostic("EF131", "sibling layers select conflicting replacements of "+node.service+"; replace explicitly in "+layer.Name, layer.Span, related...)
		}
		if previous := services[node.service]; previous != nil && previous.id != node.id {
			c.layerDiagnostic("EF130", "distinct bindings both provide "+node.service, node.origin.Span, RelatedLocation{Message: "other binding " + previous.id, Span: previous.origin.Span})
		}
		services[node.service] = node
		plan.selected[id] = node
		plan.order = append(plan.order, id)
		if node.public {
			plan.InferredProvides = append(plan.InferredProvides, node.service)
		}
	}
	for _, replacement := range layer.Entries {
		if replacement.Kind == "replace" && services[replacement.Name] == nil {
			c.layerDiagnostic("EF131", "replacement has no selected binding for "+replacement.Name, replacement.Span)
		}
	}
	plan.InferredProvides = normalized(plan.InferredProvides)
	plan.Provides = append([]string{}, plan.InferredProvides...)
	if layer.DeclaredProvides {
		declared := normalized(layer.Provides)
		plan.DeclaredProvides = &declared
		for _, name := range declared {
			if c.services[name] == nil {
				c.layerDiagnostic("EF102", "unknown service "+name, layer.Span)
			}
			if !slices.Contains(plan.InferredProvides, name) {
				c.layerDiagnostic("EF134", "provides annotation cannot expose unavailable or hidden service "+name, layer.Span)
			}
		}
		plan.Provides = declared
	}
	edges := map[string][]string{}
	incoming := map[string][]string{}
	edgeCount := 0
	for _, id := range plan.order {
		node := plan.selected[id]
		if !c.layerWork(len(node.provider.Services)*3, node.effective.Span) {
			return plan
		}
		node.public = slices.Contains(plan.Provides, node.service) && node.public
		for _, required := range normalized(node.provider.Services) {
			edgeCount++
			if edgeCount > maxLayerEdges {
				c.layerDiagnostic("EF133", "layer exceeds 10000 construction edges", layer.Span)
				return plan
			}
			if dependency := services[required]; dependency != nil {
				edges[id] = append(edges[id], dependency.id)
				incoming[dependency.id] = append(incoming[dependency.id], id)
			} else {
				plan.Requirements = append(plan.Requirements, required)
				plan.ConstructionPaths = append(plan.ConstructionPaths, LayerPath{Nodes: []string{id}, Input: required, Span: node.effective.Span})
			}
		}
	}
	plan.Requirements = normalized(plan.Requirements)
	if layer.DeclaredServices {
		declared := normalized(layer.Services)
		plan.DeclaredRequirements = &declared
		for _, name := range declared {
			if c.services[name] == nil {
				c.layerDiagnostic("EF102", "unknown service "+name, layer.Span)
			}
		}
		if extra := difference(plan.Requirements, declared); len(extra) > 0 {
			c.layerDiagnostic("EF134", "layer construction exceeds uses annotation: "+strings.Join(extra, ", "), layer.Span)
		}
	}
	if layer.DeclaredErrors {
		declared := normalized(layer.Errors)
		plan.DeclaredFailures = &declared
		for _, name := range declared {
			if c.errors[name] == nil {
				c.layerDiagnostic("EF102", "unknown failure "+name, layer.Span)
			}
		}
	}
	// Dependency-first Kahn traversal is also the emitter's checked order.
	counts := map[string]int{}
	ready := []string{}
	for _, id := range plan.order {
		counts[id] = len(edges[id])
		if counts[id] == 0 {
			ready = append(ready, id)
		}
	}
	for head := 0; head < len(ready); head++ {
		for _, consumer := range incoming[ready[head]] {
			counts[consumer]--
			if counts[consumer] == 0 {
				ready = append(ready, consumer)
			}
		}
	}
	if len(ready) != len(plan.order) {
		related := []RelatedLocation{}
		for _, id := range plan.order {
			if counts[id] > 0 && len(related) < maxLayerRelatedLocations {
				node := plan.selected[id]
				related = append(related, RelatedLocation{Message: node.service + " constructed by " + node.provider.Name, Span: node.effective.Span})
			}
		}
		c.layerDiagnostic("EF132", "visible construction cycle in layer "+layer.Name, layer.Span, related...)
	} else {
		plan.order = ready
	}
	for _, id := range ids {
		node := plan.selected[id]
		if !c.layerWork(1+len(id)+len(node.replacements)+len(edges[id])+len(incoming[id])+len(node.provider.Services), node.effective.Span) {
			return plan
		}
		// The node/dependency unit charges above cover NewPlan's per-entry
		// counts; emitted NodeSource.Module is empty in this static profile.
		for _, dependency := range edges[id] {
			if !c.layerWork(len(dependency), node.effective.Span) {
				return plan
			}
		}
		constructor := c.projectCheckedBase(c.checkedProvider(node.provider, true))
		arguments := []LayerArgument{}
		for _, arg := range node.effective.Value.Args {
			arguments = append(arguments, LayerArgument{Span: arg.Span, Type: arg.Type})
		}
		plan.Nodes = append(plan.Nodes, LayerNode{ID: id, Service: node.service, ServiceIdentity: serviceIdentity(node.service), Implementation: node.provider.Name, ImplementationIdentity: providerTypeRef(node.provider).Declaration,
			Span: node.origin.Span, SelectionSpan: node.effective.Span, Public: node.public, Occurrences: node.occurrences, Replacements: append([]LayerSite{}, node.replacements...), Dependencies: normalized(edges[id]), Incoming: normalized(incoming[id]), Requirements: normalized(node.provider.Services), Failures: []string{}, Constructor: constructor, Parameters: publicParams(node.provider.Params), Arguments: arguments, Owner: "provision-build/node:" + id})
	}
	return plan
}

func (c *checker) layerProvider(entry *LayerEntry) *Provider {
	if provider, checked := c.layerProviders[entry]; checked {
		return provider
	}
	provider := c.checkLayerProvider(entry)
	c.layerProviders[entry] = provider
	return provider
}

func (c *checker) checkLayerProvider(entry *LayerEntry) *Provider {
	if c.services[entry.Name] == nil {
		c.layerDiagnostic("EF102", "unknown service "+entry.Name, entry.Span)
		return nil
	}
	expr := entry.Value
	name := ""
	var args []*Expr
	if expr.Kind == "name" {
		name = expr.Name
	} else if expr.Kind == "call" && expr.Left.Kind == "name" {
		name = expr.Left.Name
		args = expr.Args
	} else {
		c.layerDiagnostic("EF135", "static layer bindings require an ordinary pure implementation constructor; dynamic and fallible factories are unsupported", expr.Span)
		return nil
	}
	provider := c.providers[name]
	if provider == nil {
		c.layerDiagnostic("EF135", "static layers select impl constructors; effect factories are unsupported", expr.Span)
		return nil
	}
	if provider.Service != entry.Name {
		c.layerDiagnostic("EF104", "implementation "+name+" does not implement "+entry.Name, expr.Span)
		return nil
	}
	if len(args) != len(provider.Params) {
		c.layerDiagnostic("EF106", "incorrect layer constructor argument count for "+name, expr.Span)
	}
	if len(expr.Fields) > 0 {
		c.layerDiagnostic("EF135", "named implementation configuration arguments are unsupported; use positional arguments", expr.Span)
	}
	for i, arg := range args {
		if !c.staticLayerArgument(arg) {
			c.layerDiagnostic("EF135", "layer configuration currently admits only pure literal, record and enum values", arg.Span)
			continue
		}
		previous := c.recordFacts
		c.recordFacts = true
		checked := c.expr(arg, localEnv{}, false)
		c.recordFacts = previous
		if i < len(provider.Params) && (checked.isEffect() || !c.assignable(checked.valueID(), provider.Params[i].typeID, 0)) {
			c.layerDiagnostic("EF106", "layer constructor argument must be "+provider.Params[i].Type, arg.Span)
		}
		if len(c.rowLabels(checked.evaluation.failureRowID())) > 0 || len(c.rowLabels(checked.evaluation.serviceRowID())) > 0 {
			c.layerDiagnostic("EF135", "layer configuration must be pure", arg.Span)
		}
	}
	if provider.Service == "Files" || provider.Service == "Runtime" || provider.Service == "Foreign" {
		c.requireGo(expr.Span, "native provider "+provider.Service)
	}
	return provider
}

func (c *checker) staticLayerArgument(expr *Expr) bool {
	if !c.layerWork(1, expr.Span) {
		return false
	}
	switch expr.Kind {
	case "string", "integer", "bool", "void":
		return true
	case "construct":
		for _, field := range expr.Fields {
			if !c.staticLayerArgument(field.Value) {
				return false
			}
		}
		return true
	case "member":
		return expr.Left != nil && expr.Left.Kind == "name" && c.enums[expr.Left.Name] != nil
	case "call":
		if expr.Left == nil || !(expr.Left.Kind == "name" || (expr.Left.Kind == "member" && expr.Left.Left != nil && expr.Left.Left.Kind == "name")) {
			return false
		}
		if expr.Left.Kind == "name" && c.records[expr.Left.Name] == nil {
			return false
		}
		if expr.Left.Kind == "member" && c.enums[expr.Left.Left.Name] == nil {
			return false
		}
		// The canonical checker identifies data constructors; calls to functions
		// are excluded below after checking their declaration kind.
		for _, arg := range expr.Args {
			if !c.staticLayerArgument(arg) {
				return false
			}
		}
		return true
	}
	return false
}

func (r *Result) FindLayer(name string) *LayerPlan {
	for i := range r.Layers {
		if r.Layers[i].Name == name {
			return &r.Layers[i]
		}
	}
	return nil
}

func (r *Result) LayerInspection(name, file string) (map[string]any, error) {
	if !r.Checked {
		return nil, fmt.Errorf("layer inspection requires checked source")
	}
	plan := r.FindLayer(name)
	if plan == nil {
		return nil, fmt.Errorf("unknown layer %s", name)
	}
	projection := r.ProjectAllTypes()
	if !projection.Complete {
		return nil, fmt.Errorf("type projection unavailable: %s", projection.Error)
	}
	response := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "file": file, "target": r.Target, "checked": r.Checked, "layer": plan, "types": projection.Types, "rows": projection.Rows, "declarations": r.ProjectionDeclarations(projection), "typeProjectionComplete": true}
	r.AddSourceInputs(response)
	usage, err := r.ValidateProjectionResponse(projection, response)
	if err != nil {
		return nil, err
	}
	response["typeProjectionUsage"] = usage
	return response, nil
}

func (c *checker) provideLayer(e *Expr, env localEnv, inEffect bool) checkedExpression {
	program := c.expr(e.Left, env, inEffect)
	if !program.isEffect() {
		c.layerDiagnostic("EF105", "layer provision requires an Effect value", e.Span)
		return program
	}
	if e.binding != nil {
		c.layerDiagnostic("EF135", "static layer provision requires an unshadowed layer declaration", e.Span)
		return program
	}
	plan := c.layers[e.Name]
	if plan == nil {
		c.layerDiagnostic("EF102", "unknown or unchecked layer "+e.Name, e.Span)
		return program
	}
	e.layerPlan = plan
	if c.abstractRow(program.serviceRow()) {
		c.layerDiagnostic("EF125", "layer provision of an abstract row requires an unsupported row difference constraint", e.Span)
	}
	// This profile admits pure constructors only, so preserve the complete
	// program failure row, including any ordinary row parameter identity.
	remaining := difference(c.rowLabels(program.serviceRow()), plan.Provides)
	program.value = c.recontractRows(program, program.failureRow(), c.internRow(union(remaining, plan.Requirements)))
	region := fmt.Sprintf("provision:%d", e.Span.Offset)
	if len(program.ownershipFacts()) == 0 {
		program.setOwnership(c.unknownOwnership(c.displayTypeID(program.resultID())))
	}
	program.setOwnership(materializeExecutionFacts(program.ownershipFacts(), region, ownershipOwnerLexical))
	program.setCaptures(materializeExecutionFacts(program.captureFacts(), region, ownershipOwnerLexical))
	for _, facts := range [][]OwnershipFact{program.ownershipFacts(), program.captureFacts()} {
		uncertain := false
		for _, fact := range facts {
			uncertain = uncertain || fact.Status == "unknown"
		}
		if uncertain || hasPotentialOwner(facts) || hasOwnedFact(facts, region) || hasOwnedClosed(facts) {
			c.layerDiagnostic("EF123", "value owned by the closing layer provision or with unresolved ownership cannot escape", e.Span)
		}
	}
	for _, path := range plan.ConstructionPaths {
		c.reasons = append(c.reasons, Contribution{Kind: "layer-construction-input", Names: []string{path.Input}, Span: path.Span})
	}
	c.reasons = append(c.reasons, Contribution{Kind: "layer-provision", Names: plan.Provides, Span: e.Span})
	return program
}
