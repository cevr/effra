package machineplan

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Result is one module's analysis: the published plans, sorted by ID, and the
// diagnostics in report order.
type Result[V any] struct {
	Plans       []Plan[V]
	Diagnostics []Diagnostic
	// Units is the work the allocation property test credits across the
	// module: accepted charges, refused charges up to the room they found
	// plus one, and recorded diagnostics (budget.credit).
	Units int
}

// Analyse checks every declaration of in and plans the ones that pass. It
// reads only in and writes only its result: the checker commits the
// diagnostics, in order, after analysis returns. in.PlanBytes must be the
// size of a Plan[V]; anything less than PlanBaseBytes is a caller error.
func Analyse[V any](in *Input[V]) Result[V] {
	if in.PlanBytes < PlanBaseBytes {
		panic(fmt.Sprintf("machineplan: Input.PlanBytes is %d, less than a Plan's %d bytes", in.PlanBytes, PlanBaseBytes))
	}
	shared := &budget[V]{in: in}
	plans := make([]*Plan[V], 0, len(in.Machines))
	for ordinal, machine := range in.Machines {
		check := &check[V]{in: in, machine: machine, ordinal: ordinal, seen: map[reportKey]bool{}, shared: shared}
		if plan := check.run(); plan != nil {
			plans = append(plans, plan)
		}
	}
	// Every published plan paid for its slot (DeclarationUnits).
	out := make([]Plan[V], len(plans))
	for i, plan := range plans {
		out[i] = *plan
	}
	slices.SortFunc(out, func(a, b Plan[V]) int { return strings.Compare(a.ID, b.ID) })
	return Result[V]{Plans: out, Diagnostics: shared.diagnostics, Units: shared.credit}
}

// check holds one declaration's analysis. Every program-sized value it builds
// goes through the charged builders in alloc.go.
type check[V any] struct {
	in      *Input[V]
	machine *Machine
	ordinal int
	plan    *Plan[V]
	state   TypeRef
	enum    *Enum
	output  TypeRef
	work    int
	failed  bool
	refused bool
	seen    map[reportKey]bool
	// role-scoped trigger enum while walking step (event) or complete (outcome).
	role    string
	trigger *Enum
	// roleFunction is the step or complete function being walked.
	roleFunction *Function
	// transitionKeys deduplicates published transitions by a compact key of
	// narrowing ordinals, span and helper ordinals, never by source text.
	transitionKeys map[string]bool
	// shared is the module-wide budget and projection memo.
	shared *budget[V]
	// indexes maps each enum's variant names to declaration positions.
	indexes map[*Enum]map[string]int
	// viaIndex numbers helper names for transition keys.
	viaIndex map[string]int
	// sets interns canonical narrowings per enum; setCount numbers them.
	sets     map[*Enum]map[string]*set
	setCount int
	// refinements and subsets memoize narrowing operations.
	refinements map[refinement]*set
	subsets     map[[2]*set]bool
}

// reportKey deduplicates diagnostics without copying their text.
type reportKey struct {
	code, message  string
	offset, length int
}

// diagnosticUnits is what recording one diagnostic allocates: its slot in the
// result buffer, which doubles as it grows, and its deduplication entry.
const diagnosticUnits = (2*diagnosticBytes + 2*(reportKeyBytes+1) + textUnit - 1) / textUnit

// report records a diagnostic in the analysis' own buffer. After an analysis
// refusal only EF142 is reported for the declaration: the refusal already
// rejects the source, and a check the budget cut short is not evidence of a
// contract violation.
//
// Recording is counted, not gated: the EF142 refusal that exhausts the budget
// must itself be recorded, so its slot cannot wait on the budget. Its message
// is therefore built uncharged, and quotes the machine name through
// quotedName, so its size is fixed whatever the program. Every other
// diagnostic's message was charged before it was built, so the diagnostics a
// declaration records never outnumber the units it paid.
func (m *check[V]) report(code, message string, span Span) {
	m.failed = true
	if m.refused && code != BudgetCode {
		return
	}
	key := reportKey{code: code, message: message, offset: span.Offset, length: span.Length}
	if m.seen[key] {
		return
	}
	m.seen[key] = true
	m.work += diagnosticUnits
	m.shared.work += diagnosticUnits
	m.shared.credit += diagnosticUnits
	m.shared.diagnostics = append(m.shared.diagnostics, Diagnostic{Code: code, Message: message, Span: span})
}

// maxQuotedName is the most of a machine name an uncharged message quotes.
const maxQuotedName = 64

// quotedName is the machine name as an uncharged message quotes it: whole up
// to maxQuotedName bytes, otherwise its first maxQuotedName bytes (cut at a
// rune boundary), an ellipsis and the name's length. A refusal message is
// built after the budget is gone, so it must not copy a program-sized name.
func quotedName(name string) string {
	if len(name) <= maxQuotedName {
		return name
	}
	cut := maxQuotedName
	for cut > 0 && !utf8.RuneStart(name[cut]) {
		cut--
	}
	return fmt.Sprintf("%s… (%d bytes)", name[:cut], len(name))
}

// spend charges one unit of the declaration's analysis budget. Exhaustion is a
// refusal, never a fallback: it reports EF142 once and withholds the plan, so a
// caller that stops early on false never admits what it did not inspect.
func (m *check[V]) spend() bool { return m.charge(1) }

func (m *check[V]) charge(units int) bool {
	if m.refused {
		return false
	}
	// A refused charge allocates nothing it asked for, so it is credited only
	// up to the room it found plus one: whatever was allocated before it is
	// measured against what the analysis could afford.
	room := max(min(MaxWork-m.work, MaxModuleWork-m.shared.work), 0)
	m.shared.credit += min(units, room+1)
	m.work += units
	m.shared.work += units
	if m.shared.work > MaxModuleWork {
		m.refused = true
		if m.shared.exhausted {
			// Reported once for the module; this declaration is withheld too.
			m.failed = true
			return false
		}
		m.shared.exhausted = true
		m.report(BudgetCode, fmt.Sprintf("machine declarations exceed their shared %d-unit analysis budget; no plan is published", MaxModuleWork), m.machine.Span)
		return false
	}
	if m.work > MaxWork {
		m.refused = true
		m.report(BudgetCode, fmt.Sprintf("machine %s analysis exceeds its %d-unit budget; no plan is published", quotedName(m.machine.Name), MaxWork), m.machine.Span)
		return false
	}
	return true
}

// unprepared refuses the declaration when analysis reaches a fact the checker
// did not prepare. The checker prepares every layout, display and projection
// analysis can afford, so this is a refusal at the budget, never a guess.
func (m *check[V]) unprepared() {
	if m.refused {
		return
	}
	m.refused = true
	m.report(BudgetCode, fmt.Sprintf("machine %s analysis reaches a type the checker did not prepare within the analysis budget; no plan is published", quotedName(m.machine.Name)), m.machine.Span)
}

func (m *check[V]) binding(kind string) *Binding {
	for _, binding := range m.machine.Bindings {
		if binding.Kind == kind {
			return binding
		}
	}
	return nil
}

// boundFunction resolves a step/enter/complete name to a module function.
func (m *check[V]) boundFunction(binding *Binding) *Function {
	f := binding.Function
	if f == nil {
		m.reportf(ContractCode, binding.NameSpan, "machine %s names unknown module function %s", binding.Kind, binding.Name)
		return nil
	}
	if f.Generic {
		m.reportf(ContractCode, binding.NameSpan, "machine %s %s must be a monomorphic function; row and type parameters are unsupported", binding.Kind, f.Name)
		return nil
	}
	return f
}

// stepArguments returns S and O when id is the predeclared decision<S, O>.
func (m *check[V]) stepArguments(id TypeRef) (TypeRef, TypeRef, bool) {
	n := m.in.Types.Nodes[id]
	if n == nil || n.Kind != "application" || !n.Decision || len(n.Args) != 2 {
		return 0, 0, false
	}
	return n.Args[0], n.Args[1], true
}

// userEnum returns the closed, first-order user enum named by id.
func (m *check[V]) userEnum(id TypeRef) *Enum {
	n := m.in.Types.Nodes[id]
	if n == nil || n.Kind != "enum" {
		return nil
	}
	enum, _, ok := m.variantsOf(id)
	if !ok || enum == nil || enum.Generic || len(enum.Variants) == 0 {
		return nil
	}
	return enum
}

func (m *check[V]) run() *Plan[V] {
	if m.shared.exhausted {
		// The module refusal is already reported; no further plan publishes.
		return nil
	}
	// The declaration's fixed state is charged before the plan exists.
	if !m.charge(DeclarationUnits(m.in.PlanBytes)) {
		return nil
	}
	machine := m.machine
	stepBinding := m.binding(BindStep)
	initialBinding := m.binding(BindInitial)
	enterBinding := m.binding(BindEnter)
	completeBinding := m.binding(BindComplete)
	if stepBinding == nil {
		m.reportf(ContractCode, machine.Span, "machine %s requires a step binding", machine.Name)
	}
	if initialBinding == nil {
		m.reportf(ContractCode, machine.Span, "machine %s requires an initial state", machine.Name)
	}
	if enterBinding != nil {
		m.reportf(AdmissionCode, enterBinding.Span, "machine entry work is not yet admitted: enter returns an optional deferred Outcome recipe, which needs typed recipe values, payload-aware recovery of work failures and checked entry ownership; declare the machine without enter and complete until those land")
	}
	if completeBinding != nil && enterBinding == nil {
		m.reportf(ContractCode, completeBinding.Span, "machine complete consumes the outcome of entry work, so it requires an enter binding")
	}
	id, ok := m.text("machine:", m.in.Module, ":", machine.Name)
	if !ok {
		return nil
	}
	m.plan = &Plan[V]{ID: id, Ordinal: m.ordinal, Name: machine.Name, Span: machine.Span, Functions: []BoundFunction[V]{}, Transitions: []Transition{}, States: []StateFacts{}, TerminalFailures: []RowContribution{}, Requirements: []RowContribution{}, Evidence: Evidence, Limitations: append([]string{}, Limitations...)}
	var step *Function
	var event *Enum
	if stepBinding != nil {
		step = m.boundFunction(stepBinding)
	}
	if step != nil {
		event = m.decisionSignature(step, stepBinding, BindStep, nil)
	}
	if initialBinding != nil && m.enum != nil {
		m.checkInitial(initialBinding)
	}
	var complete *Function
	var outcome *Enum
	if completeBinding != nil {
		complete = m.boundFunction(completeBinding)
	}
	if complete != nil && m.enum != nil {
		outcome = m.decisionSignature(complete, completeBinding, BindComplete, event)
	}
	if step != nil && event != nil {
		m.walkRole(step, BindStep, event)
	}
	if complete != nil && outcome != nil {
		m.walkRole(complete, BindComplete, outcome)
	}
	if m.failed || m.refused || step == nil || event == nil || m.enum == nil {
		return nil
	}
	if !m.finishPlan(step, event, complete) {
		return nil
	}
	return m.plan
}

// decisionSignature checks (S, Trigger) -> decision<S, O> for step and complete.
// The first parameter is the current state by declaration, never by a
// "the only S-typed parameter" heuristic. It returns the trigger enum.
func (m *check[V]) decisionSignature(f *Function, binding *Binding, role string, event *Enum) *Enum {
	at := binding.NameSpan
	triggerWord := "event"
	if role == BindComplete {
		triggerWord = "outcome"
	}
	if len(f.Params) != 2 {
		m.reportf(ContractCode, at, "machine %s %s must take (state, %s) parameters; it declares %d", role, f.Name, triggerWord, len(f.Params))
		return nil
	}
	state, output, ok := m.stepArguments(f.Return)
	if !ok {
		m.reportf(ContractCode, at, "machine %s %s must return decision<State, Output>; it returns %s", role, f.Name, m.display(f.Return))
		return nil
	}
	if f.Params[0].Type != state {
		m.reportf(ContractCode, f.Params[0].Span, "machine %s %s takes the current state first: its first parameter is %s but it returns decision<%s, …>", role, f.Name, f.Params[0].Source, m.display(state))
		return nil
	}
	enum := m.userEnum(state)
	if enum == nil {
		m.reportf(ContractCode, f.Params[0].Span, "machine state %s must be a closed, non-generic enum declared in this module", m.display(state))
		return nil
	}
	trigger := m.userEnum(f.Params[1].Type)
	if trigger == nil {
		m.reportf(ContractCode, f.Params[1].Span, "machine %s %s must be a closed, non-generic enum declared in this module", triggerWord, f.Params[1].Source)
		return nil
	}
	if trigger == enum {
		m.reportf(ContractCode, f.Params[1].Span, "machine %s and state must be distinct types; %s is both", triggerWord, trigger.Name)
		return nil
	}
	if role == BindStep {
		m.state, m.enum, m.output = state, enum, output
		m.dataRefusal(state, enum.Name, f.Params[0].Span)
		m.dataRefusal(f.Params[1].Type, trigger.Name, f.Params[1].Span)
		if label, ok := m.text("output ", m.display(output)); ok {
			m.dataRefusal(output, label, at)
		}
		return trigger
	}
	if state != m.state {
		m.reportf(ContractCode, f.Params[0].Span, "machine complete %s must use the step state %s; it uses %s", f.Name, m.enum.Name, enum.Name)
		return nil
	}
	if output != m.output {
		m.reportf(ContractCode, at, "machine complete %s must publish the step output %s; it publishes %s", f.Name, m.display(m.output), m.display(output))
		return nil
	}
	if event != nil && trigger == event {
		m.reportf(ContractCode, f.Params[1].Span, "machine outcome and event must be distinct types so public sends cannot forge work completion; %s is both", trigger.Name)
		return nil
	}
	return trigger
}

// dataRefusal is the conservative payload rule until the portable ownership
// checker owns machine data: state, event, outcome and output values are
// closed first-order data. Callables, recipes, providers, fibers and host or
// runtime handles are refused (EF141) rather than admitted unchecked.
//
// It is a reachability search from one root with a visited set that is never
// cleared, so every reachable type is inspected exactly once whatever its
// nesting depth or sharing. Each type and each field costs budget, and the
// field path is kept as a trail of labels, built into text only for the
// refusal; exhaustion refuses the declaration with EF142 instead of admitting
// the rest. The checker prepares the same walk's layouts in the same order
// (machinePrep.payload), so analysis reads them in place.
func (m *check[V]) dataRefusal(root TypeRef, label string, at Span) {
	walk := &payloadWalk[V]{m: m, visited: map[TypeRef]bool{}, trail: []string{label}}
	behavior, id, refused := walk.visit(root)
	if !refused {
		return
	}
	path, ok := m.text(walk.trail...)
	if !ok {
		return
	}
	if behavior {
		m.reportf(AdmissionCode, at, "machine data %s carries a function or effect recipe; machine state, event and output values are not yet admitted to carry behavior: portable machine payloads need checked actor ownership", path)
		return
	}
	m.reportf(AdmissionCode, at, "machine data %s carries %s; machine state, event and output values are not yet admitted to carry host, provider or runtime handles: portable machine payloads need checked actor ownership", path, m.display(id))
}

type payloadWalk[V any] struct {
	m       *check[V]
	visited map[TypeRef]bool
	trail   []string
}

// visit reports the first refused type reachable from id: whether it carries
// behavior, and its type. On a refusal the trail holds its field path.
func (w *payloadWalk[V]) visit(id TypeRef) (behavior bool, at TypeRef, refused bool) {
	m := w.m
	if w.visited[id] || !m.spend() {
		return false, 0, false
	}
	w.visited[id] = true
	n := m.in.Types.Nodes[id]
	if n == nil {
		return false, 0, false
	}
	switch n.Kind {
	case "primitive", "never", "invalid":
		return false, 0, false
	case "enum", "record", "application":
		if fields, ok := m.fieldsOf(id); ok {
			if !m.charge(2 * len(fields)) {
				return false, 0, false
			}
			for _, field := range fields {
				w.trail = append(w.trail, ".", field.Name)
				if behavior, at, refused := w.visit(field.Type); refused {
					return behavior, at, true
				}
				w.trail = w.trail[:len(w.trail)-2]
			}
			return false, 0, false
		}
		if _, variants, ok := m.variantsOf(id); ok {
			// A variant without fields allocates nothing and reaches no type;
			// only fields are charged.
			for _, variant := range variants {
				if len(variant.Fields) > 0 && !m.charge(4*len(variant.Fields)) {
					return false, 0, false
				}
				for _, field := range variant.Fields {
					w.trail = append(w.trail, ".", variant.Name, ".", field.Name)
					if behavior, at, refused := w.visit(field.Type); refused {
						return behavior, at, true
					}
					w.trail = w.trail[:len(w.trail)-4]
				}
			}
		}
		return false, 0, false
	case "callable", "callable-shape", "recipe", "providerRecipe":
		return true, id, true
	default:
		return false, id, true
	}
}

// checkInitial admits a literal State constructor with a static payload. The
// plan identity records its tag; evaluating it constructs data only.
func (m *check[V]) checkInitial(binding *Binding) {
	value := binding.Value
	// The checker checked the value before analysis; comparing its type
	// allocates nothing, and the diagnostic display and the literal walk below
	// charge their own work.
	if value == nil || !m.spend() {
		return
	}
	checked := m.machine.Initial
	if checked.Effect || checked.Value != m.state {
		m.reportf(ContractCode, value.Span, "machine initial state must be a %s value; found %s", m.enum.Name, m.displayInitial(checked))
		return
	}
	variant, ok := m.literalVariant(value)
	if !ok || !m.staticValue(value) {
		m.reportf(ContractCode, value.Span, "machine initial state must be a literal %s constructor with a static payload", m.enum.Name)
		return
	}
	m.plan.Initial = Initial{Variant: variant, Span: value.Span}
}

// displayInitial is the checker's display of the checked initial value, built
// from charged text.
func (m *check[V]) displayInitial(checked InitialValue) string {
	if !checked.Effect {
		return m.display(checked.Result)
	}
	failures, _ := m.joinRow(", ", checked.Failures)
	services, _ := m.joinRow(", ", checked.Services)
	text, _ := m.text("Effect<", m.display(checked.Result), ", {", failures, "}, {", services, "}>")
	return text
}

// staticValue admits literal data only. Nesting is bounded by the parsed
// expression and charged to the budget; exhaustion refuses with EF142.
func (m *check[V]) staticValue(e *Expr) bool {
	if e == nil || !m.spend() {
		return false
	}
	switch e.Kind {
	case "string", "integer", "bool", "void":
		return true
	case "unary":
		return e.Left != nil && e.Left.Kind == "integer"
	case "construct":
		for _, field := range e.Fields {
			if !m.staticValue(field.Value) {
				return false
			}
		}
		return !e.Resolved
	case "call":
		if e.Text != "data" {
			return false
		}
		for _, field := range e.Fields {
			if !m.staticValue(field.Value) {
				return false
			}
		}
		return true
	}
	return false
}

// literalVariant reports the State variant a data constructor names.
func (m *check[V]) literalVariant(e *Expr) (string, bool) {
	if e == nil || e.Left == nil || e.Left.Kind != "member" || e.Value != m.state {
		return "", false
	}
	if e.Kind == "construct" || (e.Kind == "call" && e.Text == "data") {
		return e.Left.Name, true
	}
	return "", false
}

// stepDecision reports a decision form (`enter e`, `stay e`, `ignore`,
// `reject`, `done e`) for this machine's state: its word and its next or
// output operand. The form is syntax, so no library declaration is consulted.
func (m *check[V]) stepDecision(e *Expr) (string, *Expr, bool) {
	if e == nil || e.Kind != "decision" {
		return "", nil, false
	}
	state, _, ok := m.stepArguments(e.Value)
	if !ok || state != m.state {
		return "", nil, false
	}
	var argument *Expr
	for _, field := range e.Fields {
		if field.Name == "next" || field.Name == "output" {
			argument = field.Value
		}
	}
	return e.Name, argument, true
}

// narrowing is the narrowing of the current state and trigger: the variants
// each may have here. A nil set is unnarrowed.
type narrowing struct {
	states, triggers *set
}

// visit is the context one function body is walked under: the narrowing of
// the current state and trigger, and which parameters receive them. The `stay`
// rule's verdict depends on all three, so a recursive call is reused only when
// an active visit of the same function covers its context.
type visit struct {
	function         *Function
	states, triggers *set
	bound            string
}

// covers reports whether checking under v already discharges w. A non-nil
// state narrowing is checked strictly, and narrowing further only removes
// alternatives, so a non-nil superset covers a non-nil subset. nil
// (unnarrowed) is lenient for `stay` (the runtime defect applies) yet refines to
// every alternative a match names, so it covers only itself. Triggers only
// decide which arms are reachable, so nil (every trigger) covers any trigger
// narrowing.
func (m *check[V]) covers(v, w visit) bool {
	if v.function != w.function || v.bound != w.bound {
		return false
	}
	if v.states == nil || w.states == nil {
		if v.states != nil || w.states != nil {
			return false
		}
	} else if !m.subset(w.states, v.states) {
		return false
	}
	if v.triggers == nil {
		return true
	}
	return w.triggers != nil && m.subset(w.triggers, v.triggers)
}

// visitOf builds the visit context of f, charged by its parameter count.
func (m *check[V]) visitOf(f *Function, s *scope, n narrowing) (visit, bool) {
	if !m.charge(1 + len(f.Params)) {
		return visit{}, false
	}
	bound := make([]byte, len(f.Params))
	for index, parameter := range f.Params {
		bound[index] = '-'
		switch {
		case parameter.Binding != nil && s.states[parameter.Binding] && s.triggers[parameter.Binding]:
			bound[index] = 'B'
		case parameter.Binding != nil && s.states[parameter.Binding]:
			bound[index] = 'S'
		case parameter.Binding != nil && s.triggers[parameter.Binding]:
			bound[index] = 'T'
		}
	}
	return visit{function: f, states: n.states, triggers: n.triggers, bound: string(bound)}, true
}

// scope is one function body read under the machine: the function, which
// binders denote the current state and trigger, the let aliases seen, the
// visits in progress along this chain, the helper chain and the caller site
// that reports a helper's findings.
type scope struct {
	function *Function
	states   map[*Local]bool
	triggers map[*Local]bool
	lets     map[*Local]*Expr
	roots    map[*Local]*Local
	active   []visit
	via      []string
	site     Span
}

// reentry decides a call to a function already being walked on this chain.
// It reports covered when an active visit's context discharges the new one,
// so the recursion adds nothing the rule could reject; otherwise the body is
// walked again under the narrower context. Contexts are finite subsets, and
// a covered context is never re-walked, so the chain terminates; each walk is
// charged, and the helper depth bounds the chain.
func (m *check[V]) reentry(s *scope, v visit) bool {
	return slices.ContainsFunc(s.active, func(active visit) bool { return m.covers(active, v) })
}

func (m *check[V]) walkRole(f *Function, role string, trigger *Enum) {
	if f.Body == nil || len(f.Params) != 2 {
		return
	}
	m.role, m.trigger = role, trigger
	m.roleFunction = f
	s := &scope{function: f, states: map[*Local]bool{}, triggers: map[*Local]bool{}, lets: map[*Local]*Expr{}, roots: map[*Local]*Local{}}
	if b := f.Params[0].Binding; b != nil {
		s.states[b] = true
	}
	if b := f.Params[1].Binding; b != nil {
		s.triggers[b] = true
	}
	v, ok := m.visitOf(f, s, narrowing{})
	if !ok {
		return
	}
	s.active = []visit{v}
	m.block(s, f.Body, narrowing{}, true, false)
}

func (m *check[V]) block(s *scope, b *Block, n narrowing, tail, guarded bool) {
	if b == nil {
		return
	}
	for index, statement := range b.Statements {
		// Each statement pays for its scope entry before it is inserted, and a
		// refusal ends the block: nothing after it is walked or retained.
		if !m.spend() {
			return
		}
		if statement.Kind == "let" && statement.Value != nil && statement.Binding != nil {
			s.lets[statement.Binding] = statement.Value
		}
		last := index == len(b.Statements)-1
		m.expr(s, statement.Value, n, tail && last && statement.Kind == "expr", guarded)
		m.expr(s, statement.Payload, n, false, guarded)
	}
}

// denotes reports whether e names a binder in set, through any chain of let
// aliases. Alias chains have no depth cutoff: each binder resolves once per
// scope to its root (memoized in s.roots), each resolution step costs one
// budget unit, and exhaustion refuses the declaration with EF142. A cutoff that
// answered "unrelated" would silently drop narrowing and admit a wrong-tag `stay`.
func (m *check[V]) denotes(set map[*Local]bool, s *scope, e *Expr) bool {
	if e == nil || e.Kind != "name" || e.Binding == nil {
		return false
	}
	root := m.aliasRoot(s, e.Binding)
	return root != nil && set[root]
}

// aliasRoot follows let aliases from b to the binder they name. It returns nil
// once the budget is exhausted (already refused).
func (m *check[V]) aliasRoot(s *scope, b *Local) *Local {
	if root, ok := s.roots[b]; ok {
		return root
	}
	chain, onChain := []*Local{}, map[*Local]bool{}
	root := b
	for {
		if cached, ok := s.roots[root]; ok {
			root = cached
			break
		}
		value, ok := s.lets[root]
		if !ok || value == nil || value.Kind != "name" || value.Binding == nil || onChain[root] {
			break
		}
		if !m.charge(3) {
			return nil
		}
		chain, onChain[root] = append(chain, root), true
		root = value.Binding
	}
	for _, alias := range chain {
		s.roots[alias] = root
	}
	return root
}

// arm intersects the narrowing with every cell whose subject is the current
// state or trigger. exact is false when a subject is other data, which makes
// the arm a data-dependent guard. A refused refinement is unreachable.
func (m *check[V]) arm(s *scope, match *Expr, arm *Arm, n narrowing) (narrowing, bool, bool) {
	out, exact := n, true
	for index, subject := range match.Args {
		if index >= len(arm.Patterns) {
			continue
		}
		switch {
		case m.denotes(s.states, s, subject):
			states, ok := m.refine(m.enum, out.states, arm, index)
			if !ok || len(states.positions) == 0 {
				return out, exact, false
			}
			out.states = states
		case m.denotes(s.triggers, s, subject):
			triggers, ok := m.refine(m.trigger, out.triggers, arm, index)
			if !ok || len(triggers.positions) == 0 {
				return out, exact, false
			}
			out.triggers = triggers
		default:
			exact = false
		}
	}
	return out, exact, true
}

// expr visits every expression for the `stay` rule and, in tail position,
// records the transition decision the expression produces.
func (m *check[V]) expr(s *scope, e *Expr, n narrowing, tail, guarded bool) {
	if e == nil || !m.spend() {
		return
	}
	if variant, argument, ok := m.stepDecision(e); ok {
		if variant == "stay" {
			m.checkStay(s, e, argument, n)
		}
		for _, field := range e.Fields {
			m.expr(s, field.Value, n, false, guarded)
		}
		if tail {
			m.decision(s, e, variant, argument, n, guarded)
		}
		return
	}
	switch e.Kind {
	case "match":
		for _, subject := range e.Args {
			m.expr(s, subject, n, false, guarded)
		}
		for _, arm := range e.Arms {
			if m.refused {
				return
			}
			narrowed, exact, reachable := m.arm(s, e, arm, n)
			if reachable {
				m.block(s, arm.Body, narrowed, tail, guarded || !exact)
			}
		}
		return
	case "if":
		m.expr(s, e.Left, n, false, guarded)
		m.block(s, e.Then, n, tail, true)
		m.block(s, e.Else, n, tail, true)
		return
	case "scope":
		m.block(s, e.Then, n, tail, guarded)
		return
	case "run":
		m.expr(s, e.Left, n, tail, guarded)
		return
	case "call":
		for _, argument := range e.Args {
			m.expr(s, argument, n, false, guarded)
		}
		if m.follow(s, e, n, tail, guarded) {
			return
		}
		if tail {
			m.conservative(s, e, n, guarded)
		}
		return
	case "name":
		if tail {
			if value, ok := s.lets[e.Binding]; ok && e.Binding != nil {
				m.expr(s, value, n, true, guarded)
				return
			}
			m.conservative(s, e, n, guarded)
		}
		return
	}
	e.Children(func(child *Expr) { m.expr(s, child, n, false, guarded) })
	m.block(s, e.Then, n, false, guarded)
	m.block(s, e.Else, n, false, guarded)
	if tail {
		m.conservative(s, e, n, guarded)
	}
}

// follow reads a decision-returning helper under the caller's narrowing. The
// helper parameters that receive the current state or trigger denote them
// inside its body, so a helper whose S parameter is some other state is not
// misjudged. It reports false when the call is not a followable helper.
func (m *check[V]) follow(s *scope, call *Expr, n narrowing, tail, guarded bool) bool {
	callee := call.Function
	if callee == nil || callee.Body == nil {
		return false
	}
	state, _, ok := m.stepArguments(callee.Return)
	if !ok || state != m.state {
		return false
	}
	inner, ok := m.helperScope(s, callee, call)
	if !ok {
		return true
	}
	v, ok := m.visitOf(callee, inner, n)
	if !ok {
		return true
	}
	if m.reentry(s, v) {
		// Recursion in a covered context: the caller records the
		// conservative decision for this call.
		return false
	}
	if len(s.via) >= MaxHelperDepth {
		return m.helperDepthExhausted(s, call, n)
	}
	inner.active = append(slices.Clone(s.active), v)
	m.block(inner, callee.Body, n, tail, guarded)
	return true
}

// helperDepthExhausted decides a followable helper call beyond the helper
// depth. When the current state is narrowed here, or the call passes the
// current state, the helper could produce a `stay` the rule must judge, so the
// declaration is refused with EF142 rather than admitted unread. Otherwise
// every `stay` inside it is unnarrowed, which the rule leaves to the runtime
// defect, and the conservative decision the caller records is exact. It
// reports whether the call was refused.
func (m *check[V]) helperDepthExhausted(s *scope, call *Expr, n narrowing) bool {
	carries := n.states != nil
	for _, argument := range call.Args {
		carries = carries || m.denotes(s.states, s, argument)
	}
	if !carries {
		return false
	}
	at := call.Span
	if s.site.Length > 0 {
		at = s.site
	}
	if chain, ok := m.join(" -> ", append(slices.Clone(s.via), call.Function.Name)); ok {
		if message, ok := m.textf("machine %s cannot check `stay` through helper %s: the chain exceeds %d helpers while the current state is narrowed or passed; no plan is published", m.machine.Name, chain, MaxHelperDepth); ok {
			m.report(BudgetCode, message, at)
		}
	}
	// The refusal ends the declaration's analysis.
	m.failed, m.refused = true, true
	return true
}

// helperScope binds a helper's parameters for one call, charged by the call's
// width.
func (m *check[V]) helperScope(s *scope, callee *Function, call *Expr) (*scope, bool) {
	if !m.charge(4 + len(call.Args) + len(s.via)) {
		return nil, false
	}
	site := s.site
	if len(s.via) == 0 {
		site = call.Span
	}
	inner := &scope{function: callee, states: map[*Local]bool{}, triggers: map[*Local]bool{}, lets: map[*Local]*Expr{}, roots: map[*Local]*Local{}, via: append(slices.Clone(s.via), callee.Name), site: site}
	for index, argument := range call.Args {
		parameter := index
		if index < len(call.ArgumentParameters) {
			parameter = call.ArgumentParameters[index]
		}
		if parameter < 0 || parameter >= len(callee.Params) || callee.Params[parameter].Binding == nil {
			continue
		}
		if m.denotes(s.states, s, argument) {
			inner.states[callee.Params[parameter].Binding] = true
		}
		if m.denotes(s.triggers, s, argument) {
			inner.triggers[callee.Params[parameter].Binding] = true
		}
	}
	return inner, true
}

// statePath is one visible way a State value is produced: a literal
// constructor (variant is its position, or -1), the unchanged current state,
// or a dynamic value.
type statePath struct {
	variant  int32
	same     bool
	narrowed *set
	span     Span
	via      []string
}

// addPath appends one path after charging it, so the paths a walk retains
// never outnumber the units it paid.
func (m *check[V]) addPath(out []statePath, path statePath) []statePath {
	if !m.spend() {
		return out
	}
	return append(out, path)
}

// paths appends the visible ways e produces a State value to out. A refusal
// stops collection; the declaration is then withheld.
func (m *check[V]) paths(s *scope, e *Expr, n narrowing, out []statePath) []statePath {
	if e == nil {
		return m.addPath(out, statePath{variant: -1})
	}
	if !m.spend() {
		return out
	}
	if m.denotes(s.states, s, e) {
		return m.addPath(out, statePath{variant: -1, same: true, narrowed: n.states, span: e.Span, via: s.via})
	}
	if name, ok := m.literalVariant(e); ok {
		variant, ok := m.position(m.enum, name)
		if !ok {
			return out
		}
		return m.addPath(out, statePath{variant: variant, narrowed: n.states, span: e.Span, via: s.via})
	}
	switch e.Kind {
	case "name":
		if value, ok := s.lets[e.Binding]; ok && e.Binding != nil {
			return m.paths(s, value, n, out)
		}
	case "if":
		out = m.blockPaths(s, e.Then, n, out)
		return m.blockPaths(s, e.Else, n, out)
	case "match":
		for _, arm := range e.Arms {
			if m.refused {
				return out
			}
			narrowed, _, reachable := m.arm(s, e, arm, n)
			if reachable {
				out = m.blockPaths(s, arm.Body, narrowed, out)
			}
		}
		return out
	case "scope":
		return m.blockPaths(s, e.Then, n, out)
	case "call":
		callee := e.Function
		if callee != nil && callee.Body != nil && callee.Return == m.state && !callee.Effect {
			inner, ok := m.helperScope(s, callee, e)
			if !ok {
				return out
			}
			v, ok := m.visitOf(callee, inner, n)
			if !ok {
				return out
			}
			if m.reentry(s, v) {
				return m.addPath(out, statePath{variant: -1, span: e.Span, via: s.via})
			}
			if len(s.via) >= MaxHelperDepth {
				m.helperDepthExhausted(s, e, n)
				return m.addPath(out, statePath{variant: -1, span: e.Span, via: s.via})
			}
			inner.active = append(slices.Clone(s.active), v)
			return m.blockPaths(inner, callee.Body, n, out)
		}
	}
	return m.addPath(out, statePath{variant: -1, span: e.Span, via: s.via})
}

func (m *check[V]) blockPaths(s *scope, b *Block, n narrowing, out []statePath) []statePath {
	if b == nil || len(b.Statements) == 0 {
		return m.addPath(out, statePath{variant: -1})
	}
	if !m.charge(len(b.Statements)) {
		return out
	}
	for _, statement := range b.Statements {
		if statement.Kind == "let" && statement.Value != nil && statement.Binding != nil {
			s.lets[statement.Binding] = statement.Value
		}
	}
	last := b.Statements[len(b.Statements)-1]
	if last.Kind != "expr" {
		return out
	}
	return m.paths(s, last.Value, n, out)
}

// checkStay is the static same-tag rule. Inside an arm narrowing the current
// state to one variant, a literal `stay` argument must construct that variant;
// an arm admitting several variants accepts only the unchanged state. An
// unnarrowed or dynamic value keeps the runtime defect.
func (m *check[V]) checkStay(s *scope, e, argument *Expr, n narrowing) {
	for _, path := range m.paths(s, argument, n, nil) {
		if path.variant < 0 || path.narrowed == nil || len(path.narrowed.positions) == 1 && path.narrowed.positions[0] == path.variant {
			continue
		}
		enum := m.enum.Name
		variant := m.enum.Variants[path.variant]
		qualified, ok := m.qualified(path.narrowed)
		if !ok {
			return
		}
		format := "`stay` keeps the current entry, so it cannot change the state tag: this arm narrows the state to %s but `stay` constructs %s.%s; use `enter`%s"
		if path.narrowed.admits(path.variant) {
			format = "`stay` keeps the current entry, so it cannot change the state tag: this arm admits %s, so the literal %s.%s changes the tag for another alternative; split the arm or `stay` with the current state%s"
		}
		at, helper := e.Span, ""
		if len(path.via) > 0 {
			chain, ok := m.join(" -> ", path.via)
			if !ok {
				return
			}
			if helper, ok = m.text(" (through helper ", chain, ")"); !ok {
				return
			}
			// A `stay` inside a helper reports at the caller's call of it; one in
			// the step body itself, reached again through recursion, reports
			// at the `stay`.
			if s.site.Length > 0 && s.function != m.roleFunction {
				at = s.site
			}
		}
		m.reportf(StayCode, at, format, qualified, enum, variant, helper)
	}
}

// qualified lists a narrowing as `Enum.Variant | …`, charged before it is
// built.
func (m *check[V]) qualified(narrowed *set) (string, bool) {
	n := 0
	for _, i := range narrowed.positions {
		n += len(m.enum.Name) + 1 + len(m.enum.Variants[i]) + 3
	}
	if !m.charge(TextUnits(n)) {
		return "", false
	}
	var b strings.Builder
	b.Grow(n)
	for j, i := range narrowed.positions {
		if j > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(m.enum.Name)
		b.WriteByte('.')
		b.WriteString(m.enum.Variants[i])
	}
	return b.String(), true
}

// transition records one possible decision. targets is a narrowing of the
// state: nil means every state, the empty set no target state.
func (m *check[V]) transition(s *scope, span Span, n narrowing, decision string, targets *set, precision string, guarded bool) {
	if len(m.plan.Transitions) >= MaxTransitions {
		if !m.refused {
			m.report(BudgetCode, fmt.Sprintf("machine %s exceeds %d possible transitions; no plan is published", quotedName(m.machine.Name), MaxTransitions), m.machine.Span)
			m.refused = true
		}
		return
	}
	// A transition's From, On, Targets and Via lists are plan output.
	// Charging them before they are allocated bounds plan size, the
	// enterable search and the graph projection.
	if !m.charge(2 + targets.size(m.enum) + n.states.size(m.enum) + n.triggers.size(m.trigger) + len(s.via)) {
		return
	}
	key := m.transitionKey(span, decision, precision, guarded, n.states, n.triggers, targets, s.via)
	if m.transitionKeys == nil {
		m.transitionKeys = map[string]bool{}
	}
	if m.transitionKeys[key] {
		return
	}
	m.transitionKeys[key] = true
	m.plan.Transitions = append(m.plan.Transitions, Transition{ID: m.transitionID(len(m.plan.Transitions)), Role: m.role, From: n.states.names(m.enum), On: n.triggers.names(m.trigger), Decision: decision, Targets: targets.names(m.enum), Precision: precision, Guarded: guarded, Via: slices.Clone(s.via), Span: span})
}

// transitionKey is a compact canonical identity: the role, decision and
// precision are fixed words, the span is two integers, each narrowing is its
// interned ordinal and each helper its ordinal. Its size never follows the
// length of a source identifier or the width of a narrowing.
func (m *check[V]) transitionKey(span Span, decision, precision string, guarded bool, from, on, to *set, via []string) string {
	key := make([]byte, 0, 48+2*len(via))
	key = append(key, m.role...)
	key = append(key, 0)
	key = append(key, decision...)
	key = append(key, 0)
	key = append(key, precision...)
	key = append(key, 0)
	if guarded {
		key = append(key, 1)
	} else {
		key = append(key, 0)
	}
	key = binary.AppendUvarint(key, uint64(span.Offset))
	key = binary.AppendUvarint(key, uint64(span.Length))
	for _, set := range []*set{from, on, to} {
		key = binary.AppendUvarint(key, uint64(set.ordinal()))
	}
	if m.viaIndex == nil {
		m.viaIndex = map[string]int{}
	}
	key = binary.AppendUvarint(key, uint64(len(via)))
	for _, name := range via {
		ordinal, ok := m.viaIndex[name]
		if !ok {
			ordinal = len(m.viaIndex)
			m.viaIndex[name] = ordinal
		}
		key = binary.AppendUvarint(key, uint64(ordinal))
	}
	return string(key)
}

func (m *check[V]) decision(s *scope, e *Expr, variant string, argument *Expr, n narrowing, guarded bool) {
	switch variant {
	case "enter":
		if targets, precision, ok := m.targets(s, argument, n); ok {
			m.transition(s, e.Span, n, DecisionEnter, targets, precision, guarded)
		}
	case "stay":
		m.transition(s, e.Span, n, DecisionStay, n.states, TargetCurrent, guarded)
	case "ignore", "reject", "done":
		decision := DecisionDone
		switch variant {
		case "ignore":
			decision = DecisionIgnore
		case "reject":
			decision = DecisionReject
		}
		if none, ok := m.empty(m.enum); ok {
			m.transition(s, e.Span, n, decision, none, TargetLiteral, guarded)
		}
	}
}

// targets resolves the states a `enter` decision enters, as a narrowing (nil means
// every state). Paths that keep the current state carry canonical narrowings,
// so equal narrowings are enumerated once, and each distinct enumeration is
// charged before it runs.
func (m *check[V]) targets(s *scope, argument *Expr, n narrowing) (*set, string, bool) {
	precision, every := TargetLiteral, false
	positions := []int32{}
	enumerated := map[*set]bool{}
	for _, path := range m.paths(s, argument, n, nil) {
		switch {
		case path.variant >= 0:
			positions = append(positions, path.variant)
		case path.same:
			if precision == TargetLiteral {
				precision = TargetCurrent
			}
			if path.narrowed == nil {
				every = true
				continue
			}
			if every || enumerated[path.narrowed] {
				continue
			}
			if !m.charge(1 + len(path.narrowed.positions)) {
				return nil, precision, false
			}
			enumerated[path.narrowed] = true
			positions = append(positions, path.narrowed.positions...)
		default:
			return nil, TargetConservative, !m.refused
		}
	}
	if m.refused {
		return nil, precision, false
	}
	if every {
		return nil, precision, true
	}
	slices.Sort(positions)
	interned, ok := m.intern(m.enum, slices.Compact(positions))
	return interned, precision, ok
}

// conservative records a decision value the walk cannot see into: any decision to
// any state.
func (m *check[V]) conservative(s *scope, e *Expr, n narrowing, guarded bool) {
	state, _, ok := m.stepArguments(e.Value)
	if !ok || state != m.state {
		return
	}
	m.transition(s, e.Span, n, DecisionAny, nil, TargetConservative, guarded)
}

// finishPlan completes a checked plan. Module-wide projections are memoized
// and charged once; the plan's own state facts and enterable search are
// charged before they are allocated. A refusal withholds the plan.
func (m *check[V]) finishPlan(step *Function, event *Enum, complete *Function) bool {
	plan := m.plan
	var ok bool
	if plan.State, ok = m.dataRef(m.state, m.enum); !ok {
		return false
	}
	if plan.Event, ok = m.dataRef(step.Params[1].Type, event); !ok {
		return false
	}
	if plan.Output, ok = m.value(m.output); !ok {
		return false
	}
	stepFacts, ok := m.function(BindStep, step)
	if !ok {
		return false
	}
	plan.Functions = append(plan.Functions, stepFacts)
	if complete != nil {
		completeFacts, ok := m.function(BindComplete, complete)
		if !ok {
			return false
		}
		plan.Functions = append(plan.Functions, completeFacts)
	}
	rows, ok := m.rows(step, complete, plan.Functions)
	if !ok {
		return false
	}
	plan.TerminalFailures, plan.Requirements = rows.failures, rows.requirements
	// Enterable states: a worklist over the transitions indexed by source
	// position. The index entries follow the From lists each transition
	// already paid for; the per-state facts and marks are charged here.
	if !m.charge(1 + 2*len(m.enum.Variants) + len(plan.Transitions)) {
		return false
	}
	initial := int32(slices.Index(m.enum.Variants, plan.Initial.Variant))
	enterable := make([]bool, len(m.enum.Variants))
	enterable[initial] = true
	if len(plan.Transitions) > 0 {
		index := m.variantIndex(m.enum)
		if index == nil {
			return false
		}
		bySource := map[int32][]int{}
		for i, t := range plan.Transitions {
			if t.Decision == DecisionDone || t.Decision == DecisionAny {
				plan.Terminal = true
			}
			if t.Decision != DecisionEnter && t.Decision != DecisionStay && t.Decision != DecisionAny {
				continue
			}
			for _, from := range t.From {
				source := int32(index[from])
				bySource[source] = append(bySource[source], i)
			}
		}
		fired := make([]bool, len(plan.Transitions))
		for work := []int32{initial}; len(work) > 0; {
			state := work[len(work)-1]
			work = work[:len(work)-1]
			for _, i := range bySource[state] {
				if fired[i] {
					continue
				}
				fired[i] = true
				for _, target := range plan.Transitions[i].Targets {
					if position := int32(index[target]); !enterable[position] {
						enterable[position] = true
						work = append(work, position)
					}
				}
			}
		}
	}
	plan.States = make([]StateFacts, len(m.enum.Variants))
	for i, variant := range m.enum.Variants {
		plan.States[i] = StateFacts{Variant: variant, Initial: int32(i) == initial, Enterable: enterable[i]}
	}
	return true
}
