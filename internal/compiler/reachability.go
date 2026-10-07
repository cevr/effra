package compiler

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	rt "effra.local/prototype/runtime/effra"
)

// maxApplicationPlanWork bounds one application closure. Every admitted
// requirement, visited expression and expanded canonical type node costs one
// unit. Declarations and type nodes are admitted once, so shared call and
// type DAGs cost their size rather than their path count.
const maxApplicationPlanWork = 1 << 20

// applicationPlanExhaustedCode refuses a closure that exceeds its work bound.
// No partial plan is returned: an incomplete closure could drop behavior.
const applicationPlanExhaustedCode = "EF136"

// ApplicationRequirementKind classifies one retained checked identity.
type ApplicationRequirementKind string

const (
	RequiresFunction      ApplicationRequirementKind = "function"
	RequiresCallableValue ApplicationRequirementKind = "callable-value"
	RequiresDynamicCall   ApplicationRequirementKind = "dynamic-call"
	RequiresService       ApplicationRequirementKind = "service"
	RequiresOperation     ApplicationRequirementKind = "operation"
	RequiresProvider      ApplicationRequirementKind = "provider"
	RequiresLayer         ApplicationRequirementKind = "layer"
	RequiresLayerNode     ApplicationRequirementKind = "layer-node"
	RequiresDeclaration   ApplicationRequirementKind = "declaration"
	RequiresForeign       ApplicationRequirementKind = "foreign"
	RequiresGoImport      ApplicationRequirementKind = "go-import"
	// RequiresGoInitialization is a declared foreign Go package whose
	// initialization the application runs, independently of reachable calls.
	// Its identity is the resolved package path.
	RequiresGoInitialization ApplicationRequirementKind = "go-initialization"
	RequiresHelper           ApplicationRequirementKind = "helper"
	RequiresRuntimeModule    ApplicationRequirementKind = "runtime-module"
)

// ApplicationRequirement is one retained identity. Via names the identity
// whose checked body, contract or plan first required it; roots have no Via.
// Reason classifies that checked edge.
type ApplicationRequirement struct {
	Kind     ApplicationRequirementKind `json:"kind"`
	Identity string                     `json:"identity"`
	Via      string                     `json:"via,omitempty"`
	Reason   string                     `json:"reason"`
}

// ApplicationPlan is the deterministic emission closure of one concrete
// application entry mode. It is computed from checked identities only: it
// never consults graph or projection output, and it never mutates the
// checker. Every source declaration has already been checked; the plan only
// selects what an emitted application must retain.
//
// A dynamic call site invokes a callable value. Such values originate only
// at checked function-value references, so the callable-value requirements
// are the finite conservative target set of every dynamic call in the plan,
// and each of those functions is itself retained.
type ApplicationPlan struct {
	Mode         GoGenerationMode         `json:"mode"`
	Target       string                   `json:"target"`
	Revision     string                   `json:"revision"`
	Requirements []ApplicationRequirement `json:"requirements"`
	Work         int                      `json:"work"`
	WorkLimit    int                      `json:"workLimit"`
	// goImports holds the import aliases retained code qualifies, and
	// namedGoPackages their packages; see goImportLowering.
	goImports       map[string]bool
	namedGoPackages map[string]bool
}

// GoImportLowering is the import form that retains one declared foreign Go
// package in generated code.
type GoImportLowering string

const (
	// GoImportNamed: retained code qualifies the package through an alias,
	// and that named import also initializes the package.
	GoImportNamed GoImportLowering = "named"
	// GoImportBlank: no retained code names the package, so one blank import
	// keeps its initialization.
	GoImportBlank GoImportLowering = "blank"
)

// ApplicationPlanError is an explicit refusal with a stable diagnostic code.
type ApplicationPlanError struct {
	Code    string
	Message string
}

func (e *ApplicationPlanError) Error() string { return e.Code + ": " + e.Message }

// Diagnostic projects the refusal into the compiler diagnostic shape. The
// closure has no single source location, so it carries the zero span.
func (e *ApplicationPlanError) Diagnostic() Diagnostic {
	return Diagnostic{Code: e.Code, Message: e.Message}
}

// Requires reports whether the plan retains identity as kind.
func (p *ApplicationPlan) Requires(kind ApplicationRequirementKind, identity string) bool {
	_, found := p.find(kind, identity)
	return found
}

// Requirement returns the retained entry for identity, including its first
// checked provenance.
func (p *ApplicationPlan) Requirement(kind ApplicationRequirementKind, identity string) (ApplicationRequirement, bool) {
	index, found := p.find(kind, identity)
	if !found {
		return ApplicationRequirement{}, false
	}
	return p.Requirements[index], true
}

// Identities lists the retained identities of one kind in plan order.
func (p *ApplicationPlan) Identities(kind ApplicationRequirementKind) []string {
	identities := []string{}
	for _, requirement := range p.Requirements {
		if requirement.Kind == kind {
			identities = append(identities, requirement.Identity)
		}
	}
	return identities
}

// RuntimeModules lists the native runtime catalog roots the application
// requires. Catalog dependencies are closed by rt.SelectSources.
func (p *ApplicationPlan) RuntimeModules() []rt.RuntimeModule {
	modules := []rt.RuntimeModule{}
	for _, identity := range p.Identities(RequiresRuntimeModule) {
		modules = append(modules, rt.RuntimeModule(identity))
	}
	return modules
}

// RuntimeModuleClosure lists the roots closed over the runtime catalog's
// declared dependencies, in identity order.
func (p *ApplicationPlan) RuntimeModuleClosure() ([]rt.RuntimeModule, error) {
	return rt.SelectModules(p.RuntimeModules()...)
}

// RuntimeSources selects the native runtime source closure of the plan.
func (p *ApplicationPlan) RuntimeSources() (map[string][]byte, error) {
	return rt.SelectSources(p.RuntimeModules()...)
}

// includesGoImport reports whether retained code qualifies a package through
// the import declared with alias, so emission writes that named import.
func (p *ApplicationPlan) includesGoImport(alias string) bool { return p.goImports[alias] }

// goImportLowering is the plan's import form for the package of one
// initialization root. Emission and inspection both read it, so a reference
// that names a package also suppresses its blank import.
func (p *ApplicationPlan) goImportLowering(path string) GoImportLowering {
	if p.namedGoPackages[path] {
		return GoImportNamed
	}
	return GoImportBlank
}

func (p *ApplicationPlan) find(kind ApplicationRequirementKind, identity string) (int, bool) {
	return slices.BinarySearchFunc(p.Requirements, ApplicationRequirement{Kind: kind, Identity: identity}, compareApplicationRequirements)
}

func compareApplicationRequirements(a, b ApplicationRequirement) int {
	if order := strings.Compare(string(a.Kind), string(b.Kind)); order != 0 {
		return order
	}
	return strings.Compare(a.Identity, b.Identity)
}

// ApplicationInspection summarizes one native entry mode's plan for CLI and
// MCP inspection without emitting it. Runtime modules are closed over the
// catalog's dependencies. A refused plan carries its diagnostic instead.
type ApplicationInspection struct {
	Mode           GoGenerationMode                   `json:"mode"`
	Complete       bool                               `json:"complete"`
	RuntimeModules []rt.RuntimeModule                 `json:"runtimeModules,omitempty"`
	Requirements   map[ApplicationRequirementKind]int `json:"requirements,omitempty"`
	// GoInitialization lists the declared foreign Go packages the application
	// initializes, in package order. It reports no binding a retained call
	// does not use.
	GoInitialization []GoInitializationInspection `json:"goInitialization,omitempty"`
	Work             int                          `json:"work"`
	WorkLimit        int                          `json:"workLimit"`
	Diagnostics      []Diagnostic                 `json:"diagnostics,omitempty"`
	Error            string                       `json:"error,omitempty"`
}

// GoInitializationInspection is one declared foreign Go package the
// application initializes: the import declarations that root it and the
// plan's import lowering for it.
type GoInitializationInspection struct {
	Package      string           `json:"package"`
	Declarations []GoImport       `json:"declarations"`
	Lowering     GoImportLowering `json:"lowering"`
}

// goInitialization projects the plan's initialization roots with the
// declarations that root them and their planned lowering.
func (r *Result) goInitialization(plan *ApplicationPlan) []GoInitializationInspection {
	packages := []GoInitializationInspection{}
	for _, path := range plan.Identities(RequiresGoInitialization) {
		entry := GoInitializationInspection{Package: path, Declarations: []GoImport{}, Lowering: plan.goImportLowering(path)}
		for _, imported := range r.Program.Imports {
			if imported.Path == path {
				entry.Declarations = append(entry.Declarations, imported)
			}
		}
		packages = append(packages, entry)
	}
	return packages
}

// ApplicationInspections plans every native entry mode the checked source
// declares: an effect main, and test cases when present. Source that declares
// neither has no native application to inspect.
func (r *Result) ApplicationInspections() []ApplicationInspection {
	inspections := []ApplicationInspection{}
	if r == nil || !r.Checked || r.Target != "go" {
		return inspections
	}
	if r.Entry() == nil {
		inspections = append(inspections, r.inspectApplication(GoGenerationBuild, maxApplicationPlanWork))
	}
	if _, err := r.Tests(); err == nil {
		inspections = append(inspections, r.inspectApplication(GoGenerationTest, maxApplicationPlanWork))
	}
	return inspections
}

func (r *Result) inspectApplication(mode GoGenerationMode, limit int) ApplicationInspection {
	inspection := ApplicationInspection{Mode: mode, WorkLimit: limit}
	plan, err := r.applicationPlan(mode, limit)
	var refusal *ApplicationPlanError
	if errors.As(err, &refusal) {
		inspection.Diagnostics = []Diagnostic{refusal.Diagnostic()}
		return inspection
	}
	if err == nil {
		inspection.RuntimeModules, err = plan.RuntimeModuleClosure()
	}
	if err != nil {
		inspection.RuntimeModules = nil
		inspection.Error = err.Error()
		return inspection
	}
	inspection.Complete = true
	inspection.Work = plan.Work
	inspection.Requirements = map[ApplicationRequirementKind]int{}
	for _, requirement := range plan.Requirements {
		inspection.Requirements[requirement.Kind]++
	}
	inspection.GoInitialization = r.goInitialization(plan)
	return inspection
}

// ApplicationPlan computes the emission closure for a concrete generated
// entry mode: GoGenerationBuild roots the checked effect main, and
// GoGenerationTest roots the selected test cases plus the harness fixture
// providers bound to every case. JavaScript library exports are a separate
// public surface and are not application roots.
func (r *Result) ApplicationPlan(mode GoGenerationMode) (*ApplicationPlan, error) {
	return r.applicationPlan(mode, maxApplicationPlanWork)
}

// LibrarySurface roots a JavaScript library module at its exported
// surface. A library is not an application: it has no entry or harness, and
// its roots never widen an entry closure. It shares the planner, so library
// emission selects through the same checked edges as an entry.
const LibrarySurface GoGenerationMode = "library"

// LibraryPlan computes the closure of every value a library module exports.
// ApplicationPlan refuses LibrarySurface: only this entry point roots it.
func (r *Result) LibraryPlan() (*ApplicationPlan, error) {
	if r == nil || !r.Checked || r.Program == nil || r.Program.semantic == nil {
		return nil, fmt.Errorf("library plans require checked source")
	}
	planner := newApplicationPlanner(r, LibrarySurface, maxApplicationPlanWork)
	planner.librarySurface()
	return planner.finish()
}

func (r *Result) applicationPlan(mode GoGenerationMode, limit int) (*ApplicationPlan, error) {
	if r == nil || !r.Checked || r.Program == nil || r.Program.semantic == nil {
		return nil, fmt.Errorf("application plans require checked source")
	}
	if err := validateGenerationMode(mode); err != nil {
		return nil, err
	}
	planner := newApplicationPlanner(r, mode, limit)
	switch mode {
	case GoGenerationBuild:
		if err := r.Entry(); err != nil {
			return nil, err
		}
		planner.root(r.Find("main"), "entry")
	case GoGenerationTest:
		tests, err := r.Tests()
		if err != nil {
			return nil, err
		}
		for _, test := range tests {
			planner.root(test, "test")
		}
		for _, name := range testHarnessProviders {
			planner.provider(r.checkedProviders[name], "", "test-harness")
		}
	}
	planner.goInitialization()
	planner.runtimeModule(rt.RuntimeModuleCore, "", "native-entry")
	return planner.finish()
}

// finish drains the queued expansions and returns the plan in canonical
// requirement order, or the planner's refusal.
func (p *applicationPlanner) finish() (*ApplicationPlan, error) {
	p.drain()
	if p.err != nil {
		return nil, p.err
	}
	plan := p.plan
	slices.SortFunc(plan.Requirements, compareApplicationRequirements)
	if _, err := plan.RuntimeSources(); err != nil {
		return nil, fmt.Errorf("application runtime selection: %w", err)
	}
	return plan, nil
}

type applicationRequirementKey struct {
	kind     ApplicationRequirementKind
	identity string
}

// applicationWork is one admitted declaration whose checked body, contract
// or plan has not been expanded yet. Exactly one pointer is set.
type applicationWork struct {
	function  *Function
	provider  *Provider
	service   *Service
	layer     *LayerPlan
	data      *DataDeclaration
	errorDecl *ErrorDecl
	identity  string
}

type applicationPlanner struct {
	r        *Result
	c        *checker
	plan     *ApplicationPlan
	admitted map[applicationRequirementKey]bool
	types    map[TypeID]bool
	queue    []applicationWork
	err      error

	providerDeclarations map[string]*Provider
	operationServices    map[*Function]*Service
	sourceErrors         map[*ErrorDecl]bool
	goImports            map[string]GoImport
}

func newApplicationPlanner(r *Result, mode GoGenerationMode, limit int) *applicationPlanner {
	c := r.Program.semantic
	planner := &applicationPlanner{
		r:                    r,
		c:                    c,
		plan:                 &ApplicationPlan{Mode: mode, Target: r.Target, Revision: r.Revision, Requirements: []ApplicationRequirement{}, WorkLimit: limit, goImports: map[string]bool{}, namedGoPackages: map[string]bool{}},
		admitted:             map[applicationRequirementKey]bool{},
		types:                map[TypeID]bool{},
		providerDeclarations: map[string]*Provider{},
		operationServices:    map[*Function]*Service{},
		sourceErrors:         map[*ErrorDecl]bool{},
		goImports:            map[string]GoImport{},
	}
	// These indexes read the checker's admitted declaration tables. Canonical
	// provider nodes carry the provider declaration identity; service
	// operations are resolved by declaration pointer, never by spelling.
	for _, provider := range c.providers {
		planner.providerDeclarations[providerTypeRef(provider).Declaration] = provider
	}
	for _, service := range c.services {
		for _, operation := range service.Methods {
			planner.operationServices[operation] = service
		}
	}
	for _, declaration := range r.Program.ErrorDecls {
		planner.sourceErrors[declaration] = true
	}
	for _, imported := range r.Program.Imports {
		planner.goImports[imported.Alias] = imported
	}
	return planner
}

func (p *applicationPlanner) spend() bool {
	if p.err != nil {
		return false
	}
	if p.plan.Work >= p.plan.WorkLimit {
		p.err = &ApplicationPlanError{Code: applicationPlanExhaustedCode, Message: "application reachability exceeds its " + strconv.Itoa(p.plan.WorkLimit) + " work budget"}
		return false
	}
	p.plan.Work++
	return true
}

// require admits identity once. It returns true only on first admission, so
// callers queue each declaration's expansion exactly once.
func (p *applicationPlanner) require(kind ApplicationRequirementKind, identity, via, reason string) bool {
	key := applicationRequirementKey{kind, identity}
	if p.admitted[key] || !p.spend() {
		return false
	}
	p.admitted[key] = true
	p.plan.Requirements = append(p.plan.Requirements, ApplicationRequirement{Kind: kind, Identity: identity, Via: via, Reason: reason})
	return true
}

func (p *applicationPlanner) root(symbol *Symbol, reason string) {
	if symbol == nil {
		p.err = fmt.Errorf("application root is unavailable")
		return
	}
	checked, ok := p.r.checkedSymbols[symbol.Identity]
	if !ok || checked.declaration == nil {
		p.err = fmt.Errorf("application root %s has no checked declaration", symbol.Name)
		return
	}
	p.function(checked.declaration, "", reason)
}

// librarySurface roots every value a JavaScript library exports: the
// module's functions and providers, the builtin service tags and the builtin
// providers with a JavaScript implementation. Data, error and template
// declarations are type-only in JavaScript and need no value root.
func (p *applicationPlanner) librarySurface() {
	for _, f := range p.r.Program.checkedFunctions() {
		if f.Module == currentModuleIdentity {
			p.function(f, "", "export")
		}
	}
	for _, s := range append(builtins(), p.r.Program.Services...) {
		p.service(p.c.services[s.Name], "", "export")
	}
	for _, provider := range builtinProviders() {
		if jsExportsBuiltinProvider(provider.Name) {
			p.provider(p.r.checkedProviders[provider.Name], "", "export")
		}
	}
	for _, provider := range p.r.Program.Providers {
		p.provider(provider, "", "export")
	}
}

func (p *applicationPlanner) drain() {
	for head := 0; head < len(p.queue) && p.err == nil; head++ {
		work := p.queue[head]
		switch {
		case work.function != nil:
			p.expandFunction(work.function, work.identity)
		case work.provider != nil:
			p.expandProvider(work.provider, work.identity)
		case work.service != nil:
			p.expandService(work.service, work.identity)
		case work.layer != nil:
			p.expandLayer(work.layer)
		case work.data != nil:
			p.expandData(work.data, work.identity)
		case work.errorDecl != nil:
			p.fields(work.errorDecl.Fields, work.identity)
		}
	}
}

func (p *applicationPlanner) function(f *Function, via, reason string) {
	if f == nil {
		return
	}
	if p.require(RequiresFunction, f.Identity, via, reason) {
		p.queue = append(p.queue, applicationWork{function: f, identity: f.Identity})
	}
}

func (p *applicationPlanner) expandFunction(f *Function, identity string) {
	p.signature(f, identity)
	p.block(f.Body, identity)
}

func (p *applicationPlanner) signature(f *Function, via string) {
	for _, parameter := range f.Params {
		p.typeID(parameter.typeID, via)
	}
	p.typeID(f.returnID, via)
}

func (p *applicationPlanner) service(s *Service, via, reason string) {
	if s == nil {
		return
	}
	identity := serviceIdentity(s.Name)
	if p.require(RequiresService, identity, via, reason) {
		p.queue = append(p.queue, applicationWork{service: s, identity: identity})
	}
}

// expandService retains the complete operation table: a provider value is
// one record of every operation, so a service type needs every signature.
func (p *applicationPlanner) expandService(s *Service, identity string) {
	for _, module := range s.native {
		p.runtimeModule(module, identity, "service-runtime")
	}
	for _, operation := range s.Methods {
		p.signature(operation, identity)
	}
}

func (p *applicationPlanner) operation(f *Function, via string) {
	s := p.operationServices[f]
	p.require(RequiresOperation, f.Identity, via, "operation-call")
	p.service(s, f.Identity, "operation")
}

func (p *applicationPlanner) provider(provider *Provider, via, reason string) {
	if provider == nil {
		return
	}
	identity := providerTypeRef(provider).Declaration
	if p.require(RequiresProvider, identity, via, reason) {
		p.queue = append(p.queue, applicationWork{provider: provider, identity: identity})
	}
}

// expandProvider retains the implemented service, captured construction
// services, configuration types and every operation body. A materialized
// provider value carries all of its operations, whichever ones are called.
func (p *applicationPlanner) expandProvider(provider *Provider, identity string) {
	p.service(p.c.services[provider.Service], identity, "implements")
	for _, captured := range normalized(provider.Services) {
		p.service(p.c.services[captured], identity, "provider-capture")
	}
	for _, parameter := range provider.Params {
		p.typeID(parameter.typeID, identity)
	}
	for _, module := range provider.native {
		p.runtimeModule(module, identity, "provider-runtime")
	}
	for _, method := range provider.Methods {
		p.signature(method, method.Identity)
		p.block(method.Body, method.Identity)
	}
}

// checkedProvider resolves a provider value or construction recipe through
// its canonical provider node.
func (p *applicationPlanner) checkedProvider(e *Expr, via, reason string) {
	node := e.checked.node()
	if node != nil && node.Kind == "providerRecipe" {
		node = p.c.node(node.Result)
	}
	if node == nil || node.Kind != "provider" {
		p.err = fmt.Errorf("application provider at offset %d has no checked provider identity", e.Span.Offset)
		return
	}
	provider := p.providerDeclarations[node.Declaration]
	if provider == nil {
		p.err = fmt.Errorf("application provider %s is not an admitted declaration", node.Declaration)
		return
	}
	p.provider(provider, via, reason)
}

func (p *applicationPlanner) layer(plan *LayerPlan, via string) {
	if plan == nil {
		p.err = fmt.Errorf("application layer provision has no checked plan")
		return
	}
	if p.require(RequiresLayer, plan.ID, via, "layer-provision") {
		p.queue = append(p.queue, applicationWork{layer: plan, identity: plan.ID})
	}
}

// expandLayer retains every selected node of a provided plan, public or
// hidden, with its effective (possibly replacement) implementation and
// configuration. Replaced selections are not part of the plan.
func (p *applicationPlanner) expandLayer(plan *LayerPlan) {
	p.runtimeModule(rt.RuntimeModuleLayers, plan.ID, "layer-provision")
	for _, node := range plan.Nodes {
		reason := "layer-node"
		if !node.Public {
			reason = "hidden-layer-node"
		}
		p.require(RequiresLayerNode, node.ID, plan.ID, reason)
		selection := plan.selected[node.ID]
		if selection == nil || selection.provider == nil || selection.effective == nil {
			p.err = fmt.Errorf("layer node %s has no checked selection", node.ID)
			return
		}
		binding := "layer-binding"
		if len(selection.replacements) > 0 {
			binding = "layer-replacement"
		}
		p.provider(selection.provider, node.ID, binding)
		p.service(p.c.services[node.Service], node.ID, "layer-node")
		for _, argument := range selection.effective.Value.Args {
			p.expr(argument, node.ID)
		}
	}
	for _, provided := range plan.Provides {
		p.service(p.c.services[provided], plan.ID, "layer-output")
	}
}

func (p *applicationPlanner) runtimeModule(module rt.RuntimeModule, via, reason string) {
	p.require(RequiresRuntimeModule, string(module), via, reason)
}

func (p *applicationPlanner) helper(name, via string) {
	p.require(RequiresHelper, name, via, "lowering")
}

func (p *applicationPlanner) data(declaration *DataDeclaration, identity, via string) {
	if declaration == nil {
		return
	}
	if p.require(RequiresDeclaration, identity, via, "type") {
		p.queue = append(p.queue, applicationWork{data: declaration, identity: identity})
	}
}

func (p *applicationPlanner) expandData(declaration *DataDeclaration, identity string) {
	p.fields(declaration.Fields, identity)
	for _, variant := range declaration.Variants {
		p.fields(variant.Fields, identity)
	}
}

func (p *applicationPlanner) fields(fields []Field, via string) {
	for _, field := range fields {
		p.typeID(field.typeID, via)
	}
}

// failure retains a source error declaration raised by a reachable fail
// statement. Builtin failures have no source declaration to emit.
func (p *applicationPlanner) failure(name, via string) {
	declaration := p.c.errors[name]
	if declaration == nil || !p.sourceErrors[declaration] {
		return
	}
	identity := p.c.declarationIdentity("error", "module", declaration.Name)
	if p.require(RequiresDeclaration, identity, via, "fail") {
		p.queue = append(p.queue, applicationWork{errorDecl: declaration, identity: identity})
	}
}

// typeID expands one canonical type node once. Nominal data is admitted as a
// declaration and expanded from its declared fields, so recursive data does
// not recurse here.
func (p *applicationPlanner) typeID(id TypeID, via string) {
	if id == invalidTypeID || p.types[id] || p.err != nil {
		return
	}
	node := p.c.node(id)
	if node == nil {
		return
	}
	if !p.spend() {
		return
	}
	p.types[id] = true
	if module, ok := typeRuntimeModule(node); ok {
		p.runtimeModule(module, via, "type")
	}
	switch node.Kind {
	case "record":
		p.data(p.c.records[node.Name], p.c.declarationIdentity("record", "module", node.Name), via)
	case "enum":
		p.data(p.c.enums[node.Name], p.c.declarationIdentity("enum", "module", node.Name), via)
	case "error":
		if declaration := p.c.errors[node.Name]; declaration != nil && p.sourceErrors[declaration] {
			identity := p.c.declarationIdentity("error", "module", node.Name)
			if p.require(RequiresDeclaration, identity, via, "type") {
				p.queue = append(p.queue, applicationWork{errorDecl: declaration, identity: identity})
			}
		}
	case "application":
		if template := p.c.templates[node.Declaration]; template != nil {
			p.data(template, template.Identity, via)
		}
	case "provider":
		p.service(p.c.services[node.Name], via, "provider-type")
	}
	for _, argument := range node.Args {
		p.typeID(argument, via)
	}
	p.typeID(node.Result, via)
}

// typeRuntimeModule names the non-core native runtime module declaring the
// emitted Go representation of one canonical node itself. Wrapped nodes are
// reached through the node's arguments and result.
func typeRuntimeModule(node *semanticTypeNode) (rt.RuntimeModule, bool) {
	switch node.Kind {
	case "opaque":
		switch node.Name {
		case "File":
			return rt.RuntimeModuleFiles, true
		case "Latch":
			return rt.RuntimeModuleSync, true
		}
	case "goResult":
		return rt.RuntimeModuleInterop, true
	}
	return "", false
}

func (p *applicationPlanner) block(b *Block, owner string) {
	if b == nil {
		return
	}
	for _, statement := range b.Statements {
		if statement.Kind == "fail" {
			p.failure(statement.Name, owner)
		}
		p.expr(statement.Value, owner)
		p.expr(statement.Payload, owner)
	}
}

// expr follows the checker's resolution of one expression: its Text
// classification, resolved declaration, layer plan and canonical value node.
// A same-spelled local or field never selects a declaration.
func (p *applicationPlanner) expr(e *Expr, owner string) {
	if e == nil || !p.spend() {
		return
	}
	if e.checked.value.arena != nil {
		p.typeID(e.checked.valueID(), owner)
	}
	switch e.Kind {
	case "name":
		switch e.Text {
		case "function":
			p.callableValue(e.ResolvedFunction, owner)
		case "provider":
			p.checkedProvider(e, owner, "provider-value")
		}
	case "member":
		if e.Text == "function" {
			p.callableValue(e.ResolvedFunction, owner)
		}
	case "call":
		switch e.Text {
		case "callable":
			p.require(RequiresDynamicCall, owner+"@"+strconv.Itoa(e.Span.Offset), owner, "callable-invocation")
		case "foreign":
			p.foreign(e, owner)
		case "fiber":
			p.helper("fiber."+e.Left.Name, owner)
		case "provider-constructor":
			p.checkedProvider(e, owner, "provider-constructor")
		case "data":
		default:
			if f := e.ResolvedFunction; f != nil {
				if p.operationServices[f] != nil {
					p.operation(f, owner)
				} else {
					p.function(f, owner, "call")
				}
			}
		}
	case "scope", "fork", "catch", "orFail":
		p.helper(e.Kind, owner)
	case "timeout":
		// The timeout lowering reads the Scheduler from the effect context.
		p.helper(e.Kind, owner)
		p.service(p.c.services["Scheduler"], owner, "timeout")
	case "provide":
		if node := e.Right.checked.node(); node != nil && node.Kind == "provider" {
			p.service(p.c.services[node.Name], owner, "provision")
		}
	case "provideLayer":
		p.layer(e.layerPlan, owner)
	}
	forEachExprChild(e, func(child *Expr) { p.expr(child, owner) })
	p.block(e.Then, owner)
	p.block(e.Else, owner)
	for _, arm := range e.Arms {
		p.block(arm.Body, owner)
	}
}

func (p *applicationPlanner) callableValue(f *Function, owner string) {
	if f == nil {
		return
	}
	p.require(RequiresCallableValue, f.Identity, owner, "function-value")
	p.function(f, owner, "function-value")
}

// goInitialization roots the package initialization of every explicit
// foreign Go import. An import declares a runtime initialization dependency
// as well as host declarations, so it is retained whether or not a reachable
// call names it. Aliases of one package share its identity. Initialization
// alone retains no function, binding, Foreign capability or helper.
func (p *applicationPlanner) goInitialization() {
	for _, imported := range p.r.Program.Imports {
		p.require(RequiresGoInitialization, imported.Path, "", "declared-foreign-import")
	}
}

// namedGoImport records that retained code qualifies imported's package
// through its alias: emission writes that named import, and the package
// needs no blank import for its initialization.
func (p *applicationPlanner) namedGoImport(imported GoImport) {
	p.plan.goImports[imported.Alias] = true
	p.plan.namedGoPackages[imported.Path] = true
}

// foreign retains one checked host binding, its import declaration and the
// Foreign capability the lowering reads from the effect context.
func (p *applicationPlanner) foreign(e *Expr, owner string) {
	binding, ok := p.r.Program.Bindings[e.Name]
	if !ok {
		p.err = fmt.Errorf("application foreign call at offset %d has no checked binding", e.Span.Offset)
		return
	}
	imported, ok := p.goImports[binding.alias]
	if !ok {
		p.err = fmt.Errorf("application foreign binding %s has no import declaration", binding.Symbol)
		return
	}
	identity := "go:" + binding.Package + "." + binding.member
	p.require(RequiresForeign, identity, owner, "foreign-call")
	p.require(RequiresGoImport, imported.Path, identity, "foreign-call")
	p.namedGoImport(imported)
	p.helper("foreign", owner)
	p.service(p.c.services["Foreign"], owner, "foreign-call")
}
