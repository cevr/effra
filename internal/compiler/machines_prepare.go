package compiler

// The prepare phase of machine analysis. The checker builds the immutable
// machineplan.Input before analysis starts: it lowers the declarations and
// every module function body the walk can read, checks the initial values,
// and resolves the layouts, displays and projections analysis will read,
// interning whatever that needs. This is ordinary checker work, outside the
// analysis window; it grows the checker's arenas as any checking does.
//
// Layouts and displays are prepared by mirroring the analysis order with the
// same cost model (machineplan.Costs) and the same per-declaration and module
// limits. Preparation charges a subset of what analysis charges at every
// point of that order, so whatever analysis can afford is prepared, and
// analysis refuses (EF142) whatever is not. The checker has no allocation
// budget of its own, so the mirror is what keeps preparation bounded: a
// generic payload that would instantiate exponentially, or rebuild a long
// key once per instantiation, is refused by its cost before the checker
// builds it (machines_prepare_test.go measures this). The input copies the
// slices it would otherwise share with the checker (row labels, a call's
// label binding), so nothing analysis holds aliases checker memory.
//
// Work over data the module shares is done once per module, not once per
// declaration that reads it: each canonical row is copied once, and each type
// whose reachable payload has been walked and found admissible is not walked
// again. A declaration that reuses either pays a fixed charge for it.

import (
	"math"
	"slices"

	"effra.local/prototype/internal/compiler/machineplan"
)

type machineInput = machineplan.Input[ValueType]

type machinePrep struct {
	c     *checker
	in    *machineInput
	costs machineplan.Costs

	functions map[*Function]*machineplan.Function
	bodies    []*Function
	locals    map[*localBinding]*machineplan.Local
	enums     map[*Enum]*machineplan.Enum
	arms      map[*MatchArm]*machineplan.Arm

	// layoutCosts memoizes each layout's cost, so a layout refused by one
	// declaration is not costed again by the next.
	layoutCosts map[machineplan.TypeRef]layoutCost

	// rows holds the input's one copy of each canonical row, shared by every
	// type node and initial that carries it; displayedRows are the rows an
	// initial's display has already paid for.
	rows          map[RowID][]string
	displayedRows map[RowID]bool

	// clean holds the types whose whole reachable payload a walk has finished
	// and found admissible, with every layout on the way prepared.
	clean map[TypeID]bool

	// work and moduleWork mirror the analysis budgets for the declaration
	// being prepared and for the module; stopped is set once the declaration
	// would exceed either, and nothing more is prepared for it.
	work, moduleWork int
	stopped          bool
}

// layoutCost is a memoized layout cost; data is false when the type has no layout.
type layoutCost struct {
	units int
	data  bool
}

func ref(id TypeID) machineplan.TypeRef { return machineplan.TypeRef(id) }

// prepareMachines builds the analysis input. initials are the checked initial
// values, by declaration ordinal. It also returns the units its mirrored
// charges accepted across the module.
func (c *checker) prepareMachines(initials []checkedExpression) (*machineInput, int) {
	p := &machinePrep{
		c: c,
		in: &machineInput{
			Module:    currentModuleIdentity,
			Machines:  make([]*machineplan.Machine, len(c.program.Machines)),
			Types:     machineplan.Types{Nodes: map[machineplan.TypeRef]*machineplan.TypeNode{}, Templates: map[string]*machineplan.Template{}, Layouts: map[machineplan.TypeRef]*machineplan.Layout{}, Displays: map[machineplan.TypeRef]string{}},
			Data:      map[machineplan.TypeRef]machineplan.Projection[ValueType]{},
			Contracts: map[*machineplan.Function]machineplan.Projection[ValueType]{},
		},
		functions:     map[*Function]*machineplan.Function{},
		locals:        map[*localBinding]*machineplan.Local{},
		enums:         map[*Enum]*machineplan.Enum{},
		arms:          map[*MatchArm]*machineplan.Arm{},
		layoutCosts:   map[machineplan.TypeRef]layoutCost{},
		rows:          map[RowID][]string{},
		displayedRows: map[RowID]bool{},
		clean:         map[TypeID]bool{},
	}
	for ordinal, machine := range c.program.Machines {
		p.in.Machines[ordinal] = p.declaration(machine, initials[ordinal])
	}
	// Bodies are lowered after the declarations, once each, so recursion and
	// helpers shared by many declarations cost one lowering.
	for len(p.bodies) > 0 {
		f := p.bodies[len(p.bodies)-1]
		p.bodies = p.bodies[:len(p.bodies)-1]
		p.functions[f].Body = p.block(f.Body)
	}
	return p.in, p.moduleWork
}

// declaration lowers one declaration and prepares, in analysis order, the
// layouts, displays and projections its analysis reads.
func (p *machinePrep) declaration(machine *Machine, initial checkedExpression) *machineplan.Machine {
	c := p.c
	out := &machineplan.Machine{Name: machine.Name, Span: machineplan.Span(machine.Span), Bindings: make([]*machineplan.Binding, len(machine.Bindings))}
	for i, binding := range machine.Bindings {
		out.Bindings[i] = &machineplan.Binding{Kind: binding.Kind, Span: machineplan.Span(binding.Span), Name: binding.Name, NameSpan: machineplan.Span(binding.NameSpan), Value: p.expr(binding.Value)}
	}
	out.Initial = machineplan.InitialValue{Effect: initial.isEffect(), Value: p.typeRef(initial.valueID()), Result: p.typeRef(initial.resultID())}
	if out.Initial.Effect {
		out.Initial.Failures = p.row(initial.failureRow())
		out.Initial.Services = p.row(initial.serviceRow())
	}
	// The step and complete names resolve to module functions; each resolved
	// name is a lexical reference to its function.
	bound := func(kind string) (*Function, *machineplan.Function) {
		binding := machine.binding(kind)
		if binding == nil {
			return nil, nil
		}
		f := c.functions[binding.Name]
		if f == nil || f.Module != currentModuleIdentity {
			return nil, nil
		}
		c.observeReference(machine, binding.NameSpan, lexicalTarget{kind: "function", function: f})
		lowered := p.function(f)
		for i, candidate := range machine.Bindings {
			if candidate == binding {
				out.Bindings[i].Function = lowered
			}
		}
		if lowered.Generic {
			return nil, nil
		}
		return f, lowered
	}
	step, stepIR := bound(machineStep)
	complete, completeIR := bound(machineComplete)

	p.work, p.stopped = 0, false
	state, output, enum, event := p.signature(step, invalidTypeID, invalidTypeID, nil, nil)
	if enum == nil {
		return out
	}
	if binding := machine.binding(machineInitial); binding != nil && binding.Value != nil {
		if out.Initial.Effect || initial.valueID() != state {
			p.display(initial.resultID())
			if out.Initial.Effect {
				p.displayRows(initial.failureRow(), initial.serviceRow())
			}
		}
	}
	if complete != nil {
		if _, _, outcome, _ := p.signature(complete, state, output, enum, event); outcome == nil {
			return out
		}
	}
	if p.stopped {
		return out
	}
	p.data(state)
	p.data(step.Params[1].typeID)
	p.data(output)
	p.contract(step, stepIR)
	if complete != nil {
		p.contract(complete, completeIR)
	}
	return out
}

// signature mirrors the analysis' decision-signature check of step (enum nil)
// or complete (enum the step state enum). It returns the state, output, state
// enum and trigger enum when the signature passes.
func (p *machinePrep) signature(f *Function, stepState, stepOutput TypeID, stepEnum, event *machineplan.Enum) (TypeID, TypeID, *machineplan.Enum, *machineplan.Enum) {
	if f == nil || len(f.Params) != 2 {
		return invalidTypeID, invalidTypeID, nil, nil
	}
	state, output, ok := p.stepArguments(f.returnID)
	if !ok {
		p.display(f.returnID)
		return invalidTypeID, invalidTypeID, nil, nil
	}
	if f.Params[0].typeID != state {
		p.display(state)
		return invalidTypeID, invalidTypeID, nil, nil
	}
	enum := p.userEnum(state)
	if enum == nil {
		p.display(state)
		return invalidTypeID, invalidTypeID, nil, nil
	}
	trigger := p.userEnum(f.Params[1].typeID)
	if trigger == nil || trigger == enum {
		return invalidTypeID, invalidTypeID, nil, nil
	}
	if stepEnum == nil {
		p.payload(state)
		p.payload(f.Params[1].typeID)
		p.display(output)
		p.payload(output)
		return state, output, enum, trigger
	}
	if state != stepState {
		return invalidTypeID, invalidTypeID, nil, nil
	}
	if output != stepOutput {
		p.display(stepOutput)
		p.display(output)
		return invalidTypeID, invalidTypeID, nil, nil
	}
	if event != nil && trigger == event {
		return invalidTypeID, invalidTypeID, nil, nil
	}
	return state, output, enum, trigger
}

func (p *machinePrep) stepArguments(id TypeID) (TypeID, TypeID, bool) {
	n := p.c.node(id)
	if n == nil || n.Kind != "application" || n.Declaration != machineplan.StepTemplate || len(n.Args) != 2 {
		return invalidTypeID, invalidTypeID, false
	}
	return n.Args[0], n.Args[1], true
}

func (p *machinePrep) userEnum(id TypeID) *machineplan.Enum {
	n := p.c.node(id)
	if n == nil || n.Kind != "enum" {
		return nil
	}
	layout := p.layout(id)
	if layout == nil || !layout.Sum || layout.Enum == nil || layout.Enum.Generic || len(layout.Enum.Variants) == 0 {
		return nil
	}
	return layout.Enum
}

// charge mirrors one analysis charge. It reports false, and stops the
// declaration, when analysis would refuse here.
func (p *machinePrep) charge(units int) bool {
	if p.stopped {
		return false
	}
	if p.work+units > machineplan.MaxWork || p.moduleWork+units > machineplan.MaxModuleWork {
		p.stopped = true
		return false
	}
	p.work += units
	p.moduleWork += units
	return true
}

// payload follows the analysis' payload walk from one root, in the same order
// and with the same visited set, preparing each layout it reaches and the
// display of the first refused type. It charges what the analysis walk charges
// for each type it walks: a unit for the visit, two per record field and four
// per variant field.
//
// A type the module has already found clean (prepWalk) is not walked again:
// the walk stops there for one unit, where analysis' visit of the same type
// charges at least that much and walks on through layouts already prepared.
// So the charges stay a subset of analysis, and preparation walks each type
// once per module however many roots and declarations reach it.
func (p *machinePrep) payload(root TypeID) {
	w := prepWalk{p: p, order: map[TypeID]int{}, onStack: map[TypeID]bool{}}
	if behavior, at, refused, _ := w.visit(root); refused && !behavior {
		p.display(at)
	}
}

// prepWalk is one payload walk. A type is clean once the walk has finished
// everything reachable from it without a refusal and within the budget. On a
// cycle that holds only when the whole cycle is finished, so cleanliness is
// decided per strongly connected component, by Tarjan's algorithm: order is
// the walk's visited set and each type's discovery index, and the stack holds
// the types of components not yet finished. A walk that ends at a refusal or
// at the budget marks nothing it has not finished.
type prepWalk struct {
	p       *machinePrep
	order   map[TypeID]int
	stack   []TypeID
	onStack map[TypeID]bool
}

// unlinked is the low link of a type that reaches no unfinished component.
const unlinked = math.MaxInt

// visit reports the first refused type reachable from id, as the analysis
// walk does, and the lowest discovery index id reaches on the stack.
func (w *prepWalk) visit(id TypeID) (behavior bool, at TypeID, refused bool, low int) {
	p := w.p
	if index, seen := w.order[id]; seen {
		if w.onStack[id] {
			return false, invalidTypeID, false, index
		}
		return false, invalidTypeID, false, unlinked
	}
	if !p.charge(1) {
		return false, invalidTypeID, false, unlinked
	}
	index := len(w.order)
	w.order[id] = index
	if p.clean[id] {
		return false, invalidTypeID, false, unlinked
	}
	n := p.c.node(id)
	if n == nil {
		return false, invalidTypeID, false, unlinked
	}
	switch n.Kind {
	case "primitive", "never", "invalid":
		return false, invalidTypeID, false, unlinked
	case "callable", "callable-shape", "recipe", "providerRecipe":
		return true, id, true, unlinked
	case "enum", "record", "application":
	default:
		return false, id, true, unlinked
	}
	layout := p.layout(id)
	if layout == nil {
		// No data layout, or the budget stopped the declaration.
		return false, invalidTypeID, false, unlinked
	}
	w.stack = append(w.stack, id)
	w.onStack[id] = true
	low = index
	walk := func(fields []machineplan.Field) (bool, TypeID, bool) {
		for _, field := range fields {
			behavior, at, refused, below := w.visit(TypeID(field.Type))
			if refused {
				return behavior, at, true
			}
			if p.stopped {
				break
			}
			low = min(low, below)
		}
		return false, invalidTypeID, false
	}
	if layout.Record {
		if !p.charge(2 * len(layout.Fields)) {
			return false, invalidTypeID, false, low
		}
		if behavior, at, refused := walk(layout.Fields); refused {
			return behavior, at, true, low
		}
	} else if layout.Sum {
		for _, variant := range layout.Variants {
			if len(variant.Fields) > 0 && !p.charge(4*len(variant.Fields)) {
				return false, invalidTypeID, false, low
			}
			if behavior, at, refused := walk(variant.Fields); refused {
				return behavior, at, true, low
			}
		}
	}
	if p.stopped || low < index {
		return false, invalidTypeID, false, low
	}
	// id roots a finished component: everything it reaches is admissible.
	for {
		top := w.stack[len(w.stack)-1]
		w.stack = w.stack[:len(w.stack)-1]
		delete(w.onStack, top)
		p.clean[top] = true
		if top == id {
			return false, invalidTypeID, false, unlinked
		}
	}
}

// layout prepares the fields or variants of a data type once per module, when
// analysis can afford their cost.
func (p *machinePrep) layout(id TypeID) *machineplan.Layout {
	key := p.typeRef(id)
	if layout, ok := p.in.Types.Layouts[key]; ok {
		return layout
	}
	// A stopped declaration prepares nothing more; the cost of a layout one
	// declaration could not afford is kept for the next.
	if p.stopped {
		return nil
	}
	cost, ok := p.layoutCosts[key]
	if !ok {
		cost.units, cost.data = p.costs.LayoutUnits(&p.in.Types, key)
		p.layoutCosts[key] = cost
	}
	if !cost.data || !p.charge(cost.units) {
		return nil
	}
	c := p.c
	n := c.node(id)
	layout := &machineplan.Layout{}
	if n.Kind != "enum" {
		var fields []Field
		fields, layout.Record = c.checkedFields(id)
		layout.Fields = p.fields(fields)
	}
	if !layout.Record && n.Kind != "record" {
		var enum *Enum
		var variants []Variant
		enum, variants, layout.Sum = c.checkedVariants(id)
		layout.Enum = p.enum(enum)
		layout.Variants = make([]machineplan.Variant, len(variants))
		for i, variant := range variants {
			layout.Variants[i] = machineplan.Variant{Name: variant.Name, Fields: p.fields(variant.Fields)}
		}
	}
	p.in.Types.Layouts[key] = layout
	return layout
}

func (p *machinePrep) fields(fields []Field) []machineplan.Field {
	out := make([]machineplan.Field, len(fields))
	for i, field := range fields {
		out[i] = machineplan.Field{Name: field.Name, Type: p.typeRef(field.typeID)}
	}
	return out
}

func (p *machinePrep) enum(enum *Enum) *machineplan.Enum {
	if enum == nil {
		return nil
	}
	if lowered, ok := p.enums[enum]; ok {
		return lowered
	}
	lowered := &machineplan.Enum{Name: enum.Name, Variants: make([]string, len(enum.Variants)), Generic: len(enum.Parameters) > 0, Qualifier: p.c.declarationQualifier("enum", enum.Name)}
	for i, variant := range enum.Variants {
		lowered.Variants[i] = variant.Name
	}
	p.enums[enum] = lowered
	return lowered
}

// row is the input's copy of a canonical row, made the first time the module
// reads it and shared from then on. The checker interned the row before
// preparation, building at least the labels' bytes, so one copy per distinct
// row is a fraction of what the checker already did for it.
func (p *machinePrep) row(id RowID) []string {
	if labels, ok := p.rows[id]; ok {
		return labels
	}
	labels := slices.Clone(p.c.retainedRowLabels(id))
	p.rows[id] = labels
	return labels
}

// displayRows mirrors what analysis charges to show an effectful initial's
// rows in its display: it joins each row as charged text, then charges the
// display's own text. The first initial of the module to show a row pays the
// row's bytes, which the join charges at least; a later one shares the copy
// for one unit, which the display text exceeds.
func (p *machinePrep) displayRows(rows ...RowID) {
	units := 0
	for _, id := range rows {
		if p.displayedRows[id] {
			continue
		}
		bytes := 0
		for _, label := range p.row(id) {
			bytes += len(label)
		}
		units += machineplan.TextUnits(bytes)
	}
	if !p.charge(max(units, 1)) {
		return
	}
	for _, id := range rows {
		p.displayedRows[id] = true
	}
}

// display prepares the checker's display of a type once per module, when
// analysis can afford building it.
func (p *machinePrep) display(id TypeID) {
	key := p.typeRef(id)
	if _, ok := p.in.Types.Displays[key]; ok {
		return
	}
	if !p.charge(p.costs.DisplayUnits(&p.in.Types, key)) {
		return
	}
	p.in.Types.Displays[key] = p.c.displayTypeID(id)
}

// projection is the checker's compact projection of a checked value, made
// only when its contract and result are published and its success type
// displays as a retained name.
func (p *machinePrep) projection(e checkedExpression) machineplan.Projection[ValueType] {
	c := p.c
	out := machineplan.Projection[ValueType]{IdentityBytes: len(e.identity)}
	success := e.resultID()
	if e.kind() == checkedFiberValue {
		success = e.contractID()
	}
	if n := c.node(success); n != nil {
		switch n.Kind {
		case "fiber", "goResult", "goValues", "provider", "recipe":
			return out
		}
	}
	_, contract := c.typePublicIDs[e.contractID()]
	_, result := c.typePublicIDs[e.resultID()]
	if !contract || !result {
		return out
	}
	out.Published = true
	out.Value = c.projectCheckedBase(e)
	return out
}

func (p *machinePrep) data(id TypeID) {
	key := p.typeRef(id)
	if _, ok := p.in.Data[key]; ok {
		return
	}
	p.in.Data[key] = p.projection(p.c.checkedDataID(id, nil, nil))
}

func (p *machinePrep) contract(f *Function, lowered *machineplan.Function) {
	if _, ok := p.in.Contracts[lowered]; ok {
		return
	}
	p.in.Contracts[lowered] = p.projection(p.c.checkedFunction(f, true, false))
}

// typeRef records id's node in the type table, with everything the cost model
// reads from it: its arguments and result, and for an application its
// template and the template's declared field types, transitively.
func (p *machinePrep) typeRef(id TypeID) machineplan.TypeRef {
	c, types := p.c, &p.in.Types
	work := []TypeID{id}
	for len(work) > 0 {
		next := work[len(work)-1]
		work = work[:len(work)-1]
		if next == invalidTypeID || types.Nodes[ref(next)] != nil {
			continue
		}
		n := c.node(next)
		if n == nil {
			continue
		}
		node := &machineplan.TypeNode{Kind: n.Kind, Name: n.Name, Declaration: n.Declaration, Mode: n.Mode, Args: make([]machineplan.TypeRef, len(n.Args)), Result: ref(n.Result), Failures: p.row(n.FailureRow), Services: p.row(n.ServiceRow)}
		for i, arg := range n.Args {
			node.Args[i] = ref(arg)
		}
		types.Nodes[ref(next)] = node
		work = append(work, n.Args...)
		work = append(work, n.Result)
		if n.Kind != "application" {
			continue
		}
		if _, ok := types.Templates[n.Declaration]; ok {
			continue
		}
		template := c.templates[n.Declaration]
		if template == nil {
			continue
		}
		lowered := &machineplan.Template{Params: make([]machineplan.TemplateParam, len(template.Parameters)), Fields: make([]machineplan.TypeRef, len(template.Fields)), Variants: make([][]machineplan.TypeRef, len(template.Variants))}
		for i, parameter := range template.Parameters {
			lowered.Params[i] = machineplan.TemplateParam{Type: parameter.Kind == "type", Constraint: -1}
			if parameter.Kind != "type" && parameter.Constraint != nil {
				lowered.Params[i].Constraint = len(parameter.Constraint.Parameters)
			}
		}
		for i, field := range template.Fields {
			lowered.Fields[i] = ref(field.typeID)
			work = append(work, field.typeID)
		}
		for i, variant := range template.Variants {
			lowered.Variants[i] = make([]machineplan.TypeRef, len(variant.Fields))
			for j, field := range variant.Fields {
				lowered.Variants[i][j] = ref(field.typeID)
				work = append(work, field.typeID)
			}
		}
		types.Templates[n.Declaration] = lowered
	}
	return ref(id)
}

// function lowers a module function's signature now and queues its body.
func (p *machinePrep) function(f *Function) *machineplan.Function {
	if lowered, ok := p.functions[f]; ok {
		return lowered
	}
	lowered := &machineplan.Function{Name: f.Name, Identity: f.Identity, Span: machineplan.Span(f.Span), Generic: len(f.RowParameters) > 0 || len(f.TypeParameters) > 0, Effect: f.Effect, Params: make([]machineplan.Param, len(f.Params)), Return: p.typeRef(f.returnID)}
	for i, parameter := range f.Params {
		lowered.Params[i] = machineplan.Param{Type: p.typeRef(parameter.typeID), Span: machineplan.Span(parameter.Span), Source: parameter.Type, Binding: p.local(parameter.binding)}
	}
	if f.Effect {
		lowered.Failures = normalized(p.c.retainedRowLabels(f.failureID))
		lowered.Services = normalized(p.c.retainedRowLabels(f.serviceID))
	}
	p.functions[f] = lowered
	if f.Body != nil {
		p.bodies = append(p.bodies, f)
	}
	return lowered
}

func (p *machinePrep) local(b *localBinding) *machineplan.Local {
	if b == nil {
		return nil
	}
	if lowered, ok := p.locals[b]; ok {
		return lowered
	}
	lowered := &machineplan.Local{}
	p.locals[b] = lowered
	return lowered
}

func (p *machinePrep) block(b *Block) *machineplan.Block {
	if b == nil {
		return nil
	}
	out := &machineplan.Block{Statements: make([]*machineplan.Statement, len(b.Statements))}
	for i, statement := range b.Statements {
		out.Statements[i] = &machineplan.Statement{Kind: statement.Kind, Value: p.expr(statement.Value), Payload: p.expr(statement.Payload), Binding: p.local(statement.binding)}
	}
	return out
}

// expr lowers a checked expression to the shape the walk reads. A call's
// function is kept when it is a module function, whose body the walk may
// follow.
func (p *machinePrep) expr(e *Expr) *machineplan.Expr {
	if e == nil {
		return nil
	}
	out := &machineplan.Expr{Kind: e.Kind, Name: e.Name, Text: e.Text, Span: machineplan.Span(e.Span), Left: p.expr(e.Left), Right: p.expr(e.Right), ArgumentParameters: slices.Clone(e.ArgumentParameters), Then: p.block(e.Then), Else: p.block(e.Else), Binding: p.local(e.binding), Resolved: e.ResolvedFunction != nil, StepTemplate: e.ResolvedTemplate != nil && e.ResolvedTemplate.Identity == machineplan.StepTemplate}
	out.Value = p.typeRef(e.checked.valueID())
	if f := e.ResolvedFunction; f != nil && f.Module == currentModuleIdentity {
		out.Function = p.function(f)
	}
	if len(e.Args) > 0 {
		out.Args = make([]*machineplan.Expr, len(e.Args))
		for i, argument := range e.Args {
			out.Args[i] = p.expr(argument)
		}
	}
	if len(e.Fields) > 0 {
		out.Fields = make([]machineplan.FieldValue, len(e.Fields))
		for i, field := range e.Fields {
			out.Fields[i] = machineplan.FieldValue{Name: field.Name, Value: p.expr(field.Value)}
		}
	}
	if len(e.Arms) > 0 {
		out.Arms = make([]*machineplan.Arm, len(e.Arms))
		for i, arm := range e.Arms {
			out.Arms[i] = p.arm(arm)
		}
	}
	return out
}

func (p *machinePrep) arm(arm *MatchArm) *machineplan.Arm {
	if lowered, ok := p.arms[arm]; ok {
		return lowered
	}
	lowered := &machineplan.Arm{Patterns: make([][]string, len(arm.Patterns)), Body: p.block(arm.Body)}
	for i, cell := range arm.Patterns {
		lowered.Patterns[i] = make([]string, len(cell))
		for j, pattern := range cell {
			lowered.Patterns[i][j] = pattern.VariantName
		}
	}
	p.arms[arm] = lowered
	return lowered
}
