package compiler

// Machine declaration checking and canonical machine plans.
//
// A `machine` declaration binds an initial state and ordinary named step,
// entry and completion functions. Checking owns three things no library could
// (docs/specs/state-machines.md#construct-admission-gate):
//
//   - the static Stay same-tag rule (EF139), keyed on the bundled
//     effra/machines Step identity and on binder identity. The declaration
//     names the current state: the first parameter of step and complete;
//   - the nominal home of the step/completion rows; and
//   - the plan identity that tooling projects through a bounded,
//     non-executing walk of the bound functions.
//
// Behavior stays in the functions. Ordinary checking already proves match
// exhaustiveness (every state/event pair has a policy), purity of pure steps
// and row preservation through helpers. Capabilities that need later units are
// refused with EF141 rather than passed through: entry work (typed recipe
// values, payload-aware recovery, entry ownership) and machine values (actor
// protocols, spawn and the owned runtime).

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
)

const (
	machineStayCode      = "EF139"
	machineContractCode  = "EF140"
	machineAdmissionCode = "EF141"
	machineBudgetCode    = "EF142"
)

const machinesStepTemplate = "template:effra/machines:module:Step"

const (
	maxMachineDeclarations = 256
	maxMachineHelperDepth  = 4
	// maxMachineWork bounds one declaration's walk: visited expressions, Stay
	// argument paths and helper bodies. Exhausting it refuses the plan.
	maxMachineWork        = 1 << 16
	maxMachineTransitions = 4096
	// maxMachineModuleWork bounds all declarations of a module together, so
	// many declarations over one wide enum cannot multiply plan output.
	maxMachineModuleWork = 1 << 20
)

// Step decisions as published in plans and graphs. "any" is a conservative
// decision produced by a Step value the bounded walk cannot see into.
const (
	machineDecisionGo     = "go"
	machineDecisionStay   = "stay"
	machineDecisionIgnore = "ignore"
	machineDecisionReject = "reject"
	machineDecisionDone   = "done"
	machineDecisionAny    = "any"
)

// Target precision of a transition. Literal targets are constructor tags the
// walk saw; current targets re-enter or keep the narrowed current state;
// conservative targets admit every state because the value is dynamic.
const (
	machineTargetLiteral      = "literal"
	machineTargetCurrent      = "current"
	machineTargetConservative = "conservative"
)

// MachinePlan is the canonical, provider-independent fact set of one checked
// machine declaration. It is static: it never executes a step, entry or
// completion function, and it claims no reachability, liveness or current
// enabledness. Transitions are possible edges from a bounded local walk.
type MachinePlan struct {
	ID string `json:"id"`
	// Ordinal is the declaration's position in its module; it prefixes the
	// plan's transition IDs.
	Ordinal          int                      `json:"ordinal"`
	Name             string                   `json:"name"`
	Span             Span                     `json:"span"`
	State            MachineDataRef           `json:"state"`
	Event            MachineDataRef           `json:"event"`
	Output           ValueType                `json:"output"`
	Initial          MachineInitial           `json:"initial"`
	Functions        []MachineFunction        `json:"functions"`
	Transitions      []MachineTransition      `json:"transitions"`
	States           []MachineStateFacts      `json:"states"`
	TerminalFailures []MachineRowContribution `json:"terminalFailures"`
	Requirements     []MachineRowContribution `json:"requirements"`
	Terminal         bool                     `json:"terminal"`
	Evidence         string                   `json:"evidence"`
	Limitations      []string                 `json:"limitations"`
}

// MachineDataRef names a nominal state, event or outcome enum.
type MachineDataRef struct {
	Name     string    `json:"name"`
	Identity string    `json:"identity"`
	Variants []string  `json:"variants"`
	Type     ValueType `json:"type"`
}

type MachineInitial struct {
	Variant string `json:"variant"`
	Span    Span   `json:"span"`
}

// MachineFunction is one bound function and its checked contract.
type MachineFunction struct {
	Role     string    `json:"role"`
	Name     string    `json:"name"`
	Identity string    `json:"identity"`
	Span     Span      `json:"span"`
	Kind     string    `json:"kind"`
	Failures []string  `json:"failures"`
	Services []string  `json:"requirements"`
	Contract ValueType `json:"contract"`
}

// MachineTransition is one possible decision. From and On are the state and
// trigger variants for which the decision is visible (match narrowing), not an
// expansion of every history. Ignore and Reject keep the state; Done publishes
// output. Guarded marks a decision beneath a data-dependent condition; in an
// effect function that condition may be an effectful decision.
type MachineTransition struct {
	ID        string   `json:"id"`
	Role      string   `json:"role"`
	From      []string `json:"from"`
	On        []string `json:"on"`
	Decision  string   `json:"decision"`
	Targets   []string `json:"targets"`
	Precision string   `json:"precision"`
	Guarded   bool     `json:"guarded"`
	Via       []string `json:"via,omitempty"`
	Span      Span     `json:"span"`
}

// MachineStateFacts summarizes one state. Enterable is false only when no
// possible edge path from the initial state reaches it in this conservative
// graph, which over-approximates edges; true never proves reachability. A
// state restored from a snapshot can still start anywhere.
type MachineStateFacts struct {
	Variant   string `json:"variant"`
	Initial   bool   `json:"initial"`
	Enterable bool   `json:"enterable"`
}

// MachineRowContribution is one failure or requirement and the bound roles
// contributing it. Spawn charging is a later actor-runtime obligation.
type MachineRowContribution struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

const machinePlanEvidence = "checked-static-machine-plan"

var machinePlanLimitations = []string{
	"possible edges from a bounded local walk; not reachability, liveness or current enabledness",
	"inspection never executes a step, entry or completion function",
	"entry work, spawn, actor handles and runtime snapshots are not admitted in this profile",
	"flat states only; hierarchy, parallel regions, history and delayed transitions are unsupported",
	"transition IDs (declaration ordinal and index) and spans are scoped to the semantic revision",
}

func machineID(name string) string { return "machine:" + currentModuleIdentity + ":" + name }

// machineNamed finds a declaration by name; the profile bounds the count.
func (c *checker) machineNamed(name string) *Machine {
	for _, machine := range c.program.Machines {
		if machine.Name == name {
			return machine
		}
	}
	return nil
}

// machineValueRefusal reports a machine name used as a value. A machine is a
// declaration; spawning a machine-backed actor needs checked actor protocols
// and the owned machine runtime, so the reference is refused, not lowered.
func (c *checker) machineValueRefusal(e *Expr, machine *Machine) checkedExpression {
	c.diagnostic(machineAdmissionCode, "machine "+machine.Name+" is a declaration, not a value; spawning or addressing a machine-backed actor is not yet admitted: it needs checked actor protocols and the owned machine runtime", e.Span)
	return c.checkedData("invalid")
}

// machineCheck holds one declaration's analysis. Every program-sized value it
// builds goes through the charged builders in machines_alloc.go.
type machineCheck struct {
	c       *checker
	machine *Machine
	ordinal int
	plan    *MachinePlan
	state   TypeID
	enum    *Enum
	output  TypeID
	work    int
	failed  bool
	refused bool
	seen    map[machineReport]bool
	// role-scoped trigger enum while walking step (event) or complete (outcome).
	role    string
	trigger *Enum
	// roleFunction is the step or complete function being walked.
	roleFunction *Function
	// transitionKeys deduplicates published transitions by a compact key of
	// narrowing ordinals, span and helper ordinals, never by source text.
	transitionKeys map[string]bool
	// shared is the module-wide budget and projection memo.
	shared *machineBudget
	// indexes maps each enum's variant names to declaration positions.
	indexes map[*Enum]map[string]int
	// viaIndex numbers helper names for transition keys.
	viaIndex map[string]int
	// sets interns canonical narrowings per enum; setCount numbers them.
	sets     map[*Enum]map[string]*machineSet
	setCount int
	// refinements and subsets memoize narrowing operations.
	refinements map[machineRefinement]*machineSet
	subsets     map[[2]*machineSet]bool
}

// machineReport deduplicates diagnostics without copying their text.
type machineReport struct {
	code, message  string
	offset, length int
}

// checkMachines runs after every function body is checked, so bound bodies
// carry their checked facts. A declaration with any diagnostic publishes no
// plan.
func (c *checker) checkMachines() {
	if len(c.program.Machines) > maxMachineDeclarations {
		c.diagnostic(machineBudgetCode, fmt.Sprintf("machine profile exceeds %d declarations", maxMachineDeclarations), c.program.Machines[maxMachineDeclarations].Span)
		return
	}
	c.result.Machines = []MachinePlan{}
	shared := &machineBudget{}
	if machineAnalysisProbe != nil {
		machineAnalysisProbe(false, 0)
		defer func() { machineAnalysisProbe(true, shared.work) }()
	}
	for ordinal, machine := range c.program.Machines {
		check := &machineCheck{c: c, machine: machine, ordinal: ordinal, seen: map[machineReport]bool{}, shared: shared}
		if plan := check.run(); plan != nil {
			c.result.Machines = append(c.result.Machines, *plan)
		}
	}
	slices.SortFunc(c.result.Machines, func(a, b MachinePlan) int { return strings.Compare(a.ID, b.ID) })
}

// report records a diagnostic. After an analysis refusal only EF142 is
// reported for the declaration: the refusal already rejects the source, and a
// check the budget cut short is not evidence of a contract violation.
func (m *machineCheck) report(code, message string, span Span) {
	m.failed = true
	if m.refused && code != machineBudgetCode {
		return
	}
	key := machineReport{code: code, message: message, offset: span.Offset, length: span.Length}
	if m.seen[key] {
		return
	}
	m.seen[key] = true
	m.c.diagnostic(code, message, span)
}

// spend charges one unit of the declaration's analysis budget. Exhaustion is a
// refusal, never a fallback: it reports EF142 once and withholds the plan, so a
// caller that stops early on false never admits what it did not inspect.
func (m *machineCheck) spend() bool { return m.charge(1) }

func (m *machineCheck) charge(units int) bool {
	if m.refused {
		return false
	}
	m.work += units
	m.shared.work += units
	if m.shared.work > maxMachineModuleWork {
		m.refused = true
		if m.shared.exhausted {
			// Reported once for the module; this declaration is withheld too.
			m.failed = true
			return false
		}
		m.shared.exhausted = true
		m.report(machineBudgetCode, fmt.Sprintf("machine declarations exceed their shared %d-unit analysis budget; no plan is published", maxMachineModuleWork), m.machine.Span)
		return false
	}
	if m.work > maxMachineWork {
		m.refused = true
		m.report(machineBudgetCode, fmt.Sprintf("machine %s analysis exceeds its %d-unit budget; no plan is published", m.machine.Name, maxMachineWork), m.machine.Span)
		return false
	}
	return true
}

// boundFunction resolves a step/enter/complete name to a module function.
func (m *machineCheck) boundFunction(binding *MachineBinding) *Function {
	f := m.c.functions[binding.Name]
	if f == nil || f.Module != currentModuleIdentity {
		m.reportf(machineContractCode, binding.NameSpan, "machine %s names unknown module function %s", binding.Kind, binding.Name)
		return nil
	}
	m.c.observeReference(m.machine, binding.NameSpan, lexicalTarget{kind: "function", function: f})
	if len(f.RowParameters) > 0 || len(f.TypeParameters) > 0 {
		m.reportf(machineContractCode, binding.NameSpan, "machine %s %s must be a monomorphic function; row and type parameters are unsupported", binding.Kind, f.Name)
		return nil
	}
	return f
}

// stepArguments returns S and O when id applies the bundled Step template.
func (m *machineCheck) stepArguments(id TypeID) (TypeID, TypeID, bool) {
	n := m.c.node(id)
	if n == nil || n.Kind != "application" || n.Declaration != machinesStepTemplate || len(n.Args) != 2 {
		return invalidTypeID, invalidTypeID, false
	}
	return n.Args[0], n.Args[1], true
}

// userEnum returns the closed, first-order user enum named by id.
func (m *machineCheck) userEnum(id TypeID) *Enum {
	n := m.c.node(id)
	if n == nil || n.Kind != "enum" {
		return nil
	}
	enum, _, ok := m.variantsOf(id)
	if !ok || enum == nil || len(enum.Parameters) > 0 || len(enum.Variants) == 0 {
		return nil
	}
	return enum
}

func (m *machineCheck) run() *MachinePlan {
	if m.shared.exhausted {
		// The module refusal is already reported; no further plan publishes.
		return nil
	}
	machine := m.machine
	stepBinding := machine.binding(machineStep)
	initialBinding := machine.binding(machineInitial)
	enterBinding := machine.binding(machineEnter)
	completeBinding := machine.binding(machineComplete)
	if stepBinding == nil {
		m.reportf(machineContractCode, machine.Span, "machine %s requires a step binding", machine.Name)
	}
	if initialBinding == nil {
		m.reportf(machineContractCode, machine.Span, "machine %s requires an initial state", machine.Name)
	}
	if enterBinding != nil {
		m.reportf(machineAdmissionCode, enterBinding.Span, "machine entry work is not yet admitted: enter returns an optional deferred Outcome recipe, which needs typed recipe values, payload-aware recovery of work failures and checked entry ownership; declare the machine without enter and complete until those land")
	}
	if completeBinding != nil && enterBinding == nil {
		m.reportf(machineContractCode, completeBinding.Span, "machine complete consumes the outcome of entry work, so it requires an enter binding")
	}
	id, ok := m.text("machine:", currentModuleIdentity, ":", machine.Name)
	if !ok {
		return nil
	}
	m.plan = &MachinePlan{ID: id, Ordinal: m.ordinal, Name: machine.Name, Span: machine.Span, Functions: []MachineFunction{}, Transitions: []MachineTransition{}, States: []MachineStateFacts{}, TerminalFailures: []MachineRowContribution{}, Requirements: []MachineRowContribution{}, Evidence: machinePlanEvidence, Limitations: append([]string{}, machinePlanLimitations...)}
	var step *Function
	var event *Enum
	if stepBinding != nil {
		step = m.boundFunction(stepBinding)
	}
	if step != nil {
		event = m.decisionSignature(step, stepBinding, machineStep, nil)
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
		outcome = m.decisionSignature(complete, completeBinding, machineComplete, event)
	}
	if step != nil && event != nil {
		m.walkRole(step, machineStep, event)
	}
	if complete != nil && outcome != nil {
		m.walkRole(complete, machineComplete, outcome)
	}
	if m.failed || m.refused || step == nil || event == nil || m.enum == nil {
		return nil
	}
	if !m.finishPlan(step, event, complete) {
		return nil
	}
	return m.plan
}

// decisionSignature checks (S, Trigger) -> Step<S, O> for step and complete.
// The first parameter is the current state by declaration, never by a
// "the only S-typed parameter" heuristic. It returns the trigger enum.
func (m *machineCheck) decisionSignature(f *Function, binding *MachineBinding, role string, event *Enum) *Enum {
	at := binding.NameSpan
	triggerWord := "event"
	if role == machineComplete {
		triggerWord = "outcome"
	}
	if len(f.Params) != 2 {
		m.reportf(machineContractCode, at, "machine %s %s must take (state, %s) parameters; it declares %d", role, f.Name, triggerWord, len(f.Params))
		return nil
	}
	state, output, ok := m.stepArguments(f.returnID)
	if !ok {
		m.reportf(machineContractCode, at, "machine %s %s must return the bundled effra/machines Step<State, Output>; it returns %s", role, f.Name, m.display(f.returnID))
		return nil
	}
	if f.Params[0].typeID != state {
		m.reportf(machineContractCode, f.Params[0].Span, "machine %s %s takes the current state first: its first parameter is %s but it returns Step<%s, …>", role, f.Name, f.Params[0].Type, m.display(state))
		return nil
	}
	enum := m.userEnum(state)
	if enum == nil {
		m.reportf(machineContractCode, f.Params[0].Span, "machine state %s must be a closed, non-generic enum declared in this module", m.display(state))
		return nil
	}
	trigger := m.userEnum(f.Params[1].typeID)
	if trigger == nil {
		m.reportf(machineContractCode, f.Params[1].Span, "machine %s %s must be a closed, non-generic enum declared in this module", triggerWord, f.Params[1].Type)
		return nil
	}
	if trigger == enum {
		m.reportf(machineContractCode, f.Params[1].Span, "machine %s and state must be distinct types; %s is both", triggerWord, trigger.Name)
		return nil
	}
	if role == machineStep {
		m.state, m.enum, m.output = state, enum, output
		m.dataRefusal(state, enum.Name, f.Params[0].Span)
		m.dataRefusal(f.Params[1].typeID, trigger.Name, f.Params[1].Span)
		if label, ok := m.text("output ", m.display(output)); ok {
			m.dataRefusal(output, label, at)
		}
		return trigger
	}
	if state != m.state {
		m.reportf(machineContractCode, f.Params[0].Span, "machine complete %s must use the step state %s; it uses %s", f.Name, m.enum.Name, enum.Name)
		return nil
	}
	if output != m.output {
		m.reportf(machineContractCode, at, "machine complete %s must publish the step output %s; it publishes %s", f.Name, m.display(m.output), m.display(output))
		return nil
	}
	if event != nil && trigger == event {
		m.reportf(machineContractCode, f.Params[1].Span, "machine outcome and event must be distinct types so public sends cannot forge work completion; %s is both", trigger.Name)
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
// the rest.
func (m *machineCheck) dataRefusal(root TypeID, label string, at Span) {
	walk := &machinePayloadWalk{m: m, visited: map[TypeID]bool{}, trail: []string{label}}
	behavior, id, refused := walk.visit(root)
	if !refused {
		return
	}
	path, ok := m.text(walk.trail...)
	if !ok {
		return
	}
	if behavior {
		m.reportf(machineAdmissionCode, at, "machine data %s carries a function or effect recipe; machine state, event and output values are not yet admitted to carry behavior: portable machine payloads need checked actor ownership", path)
		return
	}
	m.reportf(machineAdmissionCode, at, "machine data %s carries %s; machine state, event and output values are not yet admitted to carry host, provider or runtime handles: portable machine payloads need checked actor ownership", path, m.display(id))
}

type machinePayloadWalk struct {
	m       *machineCheck
	visited map[TypeID]bool
	trail   []string
}

// visit reports the first refused type reachable from id: whether it carries
// behavior, and its type. On a refusal the trail holds its field path.
func (w *machinePayloadWalk) visit(id TypeID) (behavior bool, at TypeID, refused bool) {
	m := w.m
	if w.visited[id] || !m.spend() {
		return false, invalidTypeID, false
	}
	w.visited[id] = true
	n := m.c.node(id)
	if n == nil {
		return false, invalidTypeID, false
	}
	switch n.Kind {
	case "primitive", "never", "invalid":
		return false, invalidTypeID, false
	case "enum", "record", "application":
		if fields, ok := m.fieldsOf(id); ok {
			if !m.charge(2 * len(fields)) {
				return false, invalidTypeID, false
			}
			for _, field := range fields {
				w.trail = append(w.trail, ".", field.Name)
				if behavior, at, refused := w.visit(field.typeID); refused {
					return behavior, at, true
				}
				w.trail = w.trail[:len(w.trail)-2]
			}
			return false, invalidTypeID, false
		}
		if _, variants, ok := m.variantsOf(id); ok {
			// A variant without fields allocates nothing and reaches no type;
			// only fields are charged.
			for _, variant := range variants {
				if len(variant.Fields) > 0 && !m.charge(4*len(variant.Fields)) {
					return false, invalidTypeID, false
				}
				for _, field := range variant.Fields {
					w.trail = append(w.trail, ".", variant.Name, ".", field.Name)
					if behavior, at, refused := w.visit(field.typeID); refused {
						return behavior, at, true
					}
					w.trail = w.trail[:len(w.trail)-4]
				}
			}
		}
		return false, invalidTypeID, false
	case "callable", "callable-shape", "recipe", "providerRecipe":
		return true, id, true
	default:
		return false, id, true
	}
}

// checkInitial admits a literal State constructor with a static payload. The
// plan identity records its tag; evaluating it constructs data only.
func (m *machineCheck) checkInitial(binding *MachineBinding) {
	value := binding.Value
	// Checking the initial expression allocates in proportion to its source.
	if !m.charge(1 + machineTextUnits(value.Span.Length)) {
		return
	}
	previous := m.c.recordFacts
	m.c.recordFacts = true
	checked := m.c.expr(value, localEnv{}, false)
	m.c.recordFacts = previous
	if checked.isEffect() || checked.valueID() != m.state {
		m.reportf(machineContractCode, value.Span, "machine initial state must be a %s value; found %s", m.enum.Name, m.displayChecked(checked))
		return
	}
	variant, ok := m.literalVariant(value)
	if !ok || !m.staticValue(value) {
		m.reportf(machineContractCode, value.Span, "machine initial state must be a literal %s constructor with a static payload", m.enum.Name)
		return
	}
	m.plan.Initial = MachineInitial{Variant: variant, Span: value.Span}
}

// displayChecked is the checker's display of a checked value, built from
// charged text.
func (m *machineCheck) displayChecked(checked checkedExpression) string {
	if !checked.isEffect() {
		return m.display(checked.resultID())
	}
	failures, _ := m.join(", ", m.c.retainedRowLabels(checked.failureRow()))
	services, _ := m.join(", ", m.c.retainedRowLabels(checked.serviceRow()))
	text, _ := m.text("Effect<", m.display(checked.resultID()), ", {", failures, "}, {", services, "}>")
	return text
}

// staticValue admits literal data only. Nesting is bounded by the parsed
// expression and charged to the budget; exhaustion refuses with EF142.
func (m *machineCheck) staticValue(e *Expr) bool {
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
		return e.ResolvedFunction == nil
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
func (m *machineCheck) literalVariant(e *Expr) (string, bool) {
	if e == nil || e.Left == nil || e.Left.Kind != "member" || e.checked.valueID() != m.state {
		return "", false
	}
	if e.Kind == "construct" || (e.Kind == "call" && e.Text == "data") {
		return e.Left.Name, true
	}
	return "", false
}

// stepDecision reports a constructor of the bundled Step for this machine's
// state: its variant and its next/output argument. Identity comes from the
// resolved template, never from the spelling or the import alias.
func (m *machineCheck) stepDecision(e *Expr) (string, *Expr, bool) {
	if e == nil || e.Left == nil || e.Left.Kind != "member" || e.ResolvedTemplate == nil || e.ResolvedTemplate.Identity != machinesStepTemplate {
		return "", nil, false
	}
	if e.Kind != "construct" && !(e.Kind == "call" && e.Text == "data") {
		return "", nil, false
	}
	state, _, ok := m.stepArguments(e.checked.valueID())
	if !ok || state != m.state {
		return "", nil, false
	}
	var argument *Expr
	for _, field := range e.Fields {
		if field.Name == "next" || field.Name == "output" {
			argument = field.Value
		}
	}
	return e.Left.Name, argument, true
}

// machineNarrowing is the narrowing of the current state and trigger: the
// variants each may have here. A nil set is unnarrowed.
type machineNarrowing struct {
	states, triggers *machineSet
}

// machineVisit is the context one function body is walked under: the
// narrowing of the current state and trigger, and which parameters receive
// them. The Stay rule's verdict depends on all three, so a recursive call is
// reused only when an active visit of the same function covers its context.
type machineVisit struct {
	function         *Function
	states, triggers *machineSet
	bound            string
}

// covers reports whether checking under v already discharges w. A non-nil
// state narrowing is checked strictly, and narrowing further only removes
// alternatives, so a non-nil superset covers a non-nil subset. nil
// (unnarrowed) is lenient for Stay (the runtime defect applies) yet refines to
// every alternative a match names, so it covers only itself. Triggers only
// decide which arms are reachable, so nil (every trigger) covers any trigger
// narrowing.
func (m *machineCheck) covers(v, w machineVisit) bool {
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
func (m *machineCheck) visitOf(f *Function, scope *machineScope, n machineNarrowing) (machineVisit, bool) {
	if !m.charge(1 + len(f.Params)) {
		return machineVisit{}, false
	}
	bound := make([]byte, len(f.Params))
	for index, parameter := range f.Params {
		bound[index] = '-'
		switch {
		case parameter.binding != nil && scope.states[parameter.binding] && scope.triggers[parameter.binding]:
			bound[index] = 'B'
		case parameter.binding != nil && scope.states[parameter.binding]:
			bound[index] = 'S'
		case parameter.binding != nil && scope.triggers[parameter.binding]:
			bound[index] = 'T'
		}
	}
	return machineVisit{function: f, states: n.states, triggers: n.triggers, bound: string(bound)}, true
}

// machineScope is one function body read under the machine: the function,
// which binders denote the current state and trigger, the let aliases seen,
// the visits in progress along this chain, the helper chain and the caller
// site that reports a helper's findings.
type machineScope struct {
	function *Function
	states   map[*localBinding]bool
	triggers map[*localBinding]bool
	lets     map[*localBinding]*Expr
	roots    map[*localBinding]*localBinding
	active   []machineVisit
	via      []string
	site     Span
}

// reentry decides a call to a function already being walked on this chain.
// It reports covered when an active visit's context discharges the new one,
// so the recursion adds nothing the rule could reject; otherwise the body is
// walked again under the narrower context. Contexts are finite subsets, and
// a covered context is never re-walked, so the chain terminates; each walk is
// charged, and the helper depth bounds the chain.
func (m *machineCheck) reentry(s *machineScope, visit machineVisit) bool {
	return slices.ContainsFunc(s.active, func(active machineVisit) bool { return m.covers(active, visit) })
}

func (m *machineCheck) walkRole(f *Function, role string, trigger *Enum) {
	if f.Body == nil || len(f.Params) != 2 {
		return
	}
	m.role, m.trigger = role, trigger
	m.roleFunction = f
	scope := &machineScope{function: f, states: map[*localBinding]bool{}, triggers: map[*localBinding]bool{}, lets: map[*localBinding]*Expr{}, roots: map[*localBinding]*localBinding{}}
	if b := f.Params[0].binding; b != nil {
		scope.states[b] = true
	}
	if b := f.Params[1].binding; b != nil {
		scope.triggers[b] = true
	}
	visit, ok := m.visitOf(f, scope, machineNarrowing{})
	if !ok {
		return
	}
	scope.active = []machineVisit{visit}
	m.block(scope, f.Body, machineNarrowing{}, true, false)
}

func (m *machineCheck) block(s *machineScope, b *Block, n machineNarrowing, tail, guarded bool) {
	if b == nil {
		return
	}
	for index, statement := range b.Statements {
		// Each statement pays for its scope entry before it is inserted, and a
		// refusal ends the block: nothing after it is walked or retained.
		if !m.spend() {
			return
		}
		if statement.Kind == "let" && statement.Value != nil && statement.binding != nil {
			s.lets[statement.binding] = statement.Value
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
// answered "unrelated" would silently drop narrowing and admit a wrong-tag Stay.
func (m *machineCheck) denotes(set map[*localBinding]bool, s *machineScope, e *Expr) bool {
	if e == nil || e.Kind != "name" || e.binding == nil {
		return false
	}
	root := m.aliasRoot(s, e.binding)
	return root != nil && set[root]
}

// aliasRoot follows let aliases from b to the binder they name. It returns nil
// once the budget is exhausted (already refused).
func (m *machineCheck) aliasRoot(s *machineScope, b *localBinding) *localBinding {
	if root, ok := s.roots[b]; ok {
		return root
	}
	chain, onChain := []*localBinding{}, map[*localBinding]bool{}
	root := b
	for {
		if cached, ok := s.roots[root]; ok {
			root = cached
			break
		}
		value, ok := s.lets[root]
		if !ok || value == nil || value.Kind != "name" || value.binding == nil || onChain[root] {
			break
		}
		if !m.charge(3) {
			return nil
		}
		chain, onChain[root] = append(chain, root), true
		root = value.binding
	}
	for _, alias := range chain {
		s.roots[alias] = root
	}
	return root
}

// arm intersects the narrowing with every cell whose subject is the current
// state or trigger. exact is false when a subject is other data, which makes
// the arm a data-dependent guard. A refused refinement is unreachable.
func (m *machineCheck) arm(s *machineScope, match *Expr, arm *MatchArm, n machineNarrowing) (machineNarrowing, bool, bool) {
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

// expr visits every expression for the Stay rule and, in tail position,
// records the transition decision the expression produces.
func (m *machineCheck) expr(s *machineScope, e *Expr, n machineNarrowing, tail, guarded bool) {
	if e == nil || !m.spend() {
		return
	}
	if variant, argument, ok := m.stepDecision(e); ok {
		if variant == "Stay" {
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
			if value, ok := s.lets[e.binding]; ok && e.binding != nil {
				m.expr(s, value, n, true, guarded)
				return
			}
			m.conservative(s, e, n, guarded)
		}
		return
	}
	forEachExprChild(e, func(child *Expr) { m.expr(s, child, n, false, guarded) })
	m.block(s, e.Then, n, false, guarded)
	m.block(s, e.Else, n, false, guarded)
	if tail {
		m.conservative(s, e, n, guarded)
	}
}

// follow reads a Step-returning helper under the caller's narrowing. The
// helper parameters that receive the current state or trigger denote them
// inside its body, so a helper whose S parameter is some other state is not
// misjudged. It reports false when the call is not a followable helper.
func (m *machineCheck) follow(s *machineScope, call *Expr, n machineNarrowing, tail, guarded bool) bool {
	callee := call.ResolvedFunction
	if callee == nil || callee.Body == nil || callee.Module != currentModuleIdentity {
		return false
	}
	state, _, ok := m.stepArguments(callee.returnID)
	if !ok || state != m.state {
		return false
	}
	inner, ok := m.helperScope(s, callee, call)
	if !ok {
		return true
	}
	visit, ok := m.visitOf(callee, inner, n)
	if !ok {
		return true
	}
	if m.reentry(s, visit) {
		// Recursion in a covered context: the caller records the
		// conservative decision for this call.
		return false
	}
	if len(s.via) >= maxMachineHelperDepth {
		return m.helperDepthExhausted(s, call, n)
	}
	inner.active = append(slices.Clone(s.active), visit)
	m.block(inner, callee.Body, n, tail, guarded)
	return true
}

// helperDepthExhausted decides a followable helper call beyond the helper
// depth. When the current state is narrowed here, or the call passes the
// current state, the helper could produce a Stay the rule must judge, so the
// declaration is refused with EF142 rather than admitted unread. Otherwise
// every Stay inside it is unnarrowed, which the rule leaves to the runtime
// defect, and the conservative decision the caller records is exact. It
// reports whether the call was refused.
func (m *machineCheck) helperDepthExhausted(s *machineScope, call *Expr, n machineNarrowing) bool {
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
	if chain, ok := m.join(" -> ", append(slices.Clone(s.via), call.ResolvedFunction.Name)); ok {
		if message, ok := m.textf("machine %s cannot check Step.Stay through helper %s: the chain exceeds %d helpers while the current state is narrowed or passed; no plan is published", m.machine.Name, chain, maxMachineHelperDepth); ok {
			m.report(machineBudgetCode, message, at)
		}
	}
	// The refusal ends the declaration's analysis.
	m.failed, m.refused = true, true
	return true
}

// helperScope binds a helper's parameters for one call, charged by the call's
// width.
func (m *machineCheck) helperScope(s *machineScope, callee *Function, call *Expr) (*machineScope, bool) {
	if !m.charge(4 + len(call.Args) + len(s.via)) {
		return nil, false
	}
	site := s.site
	if len(s.via) == 0 {
		site = call.Span
	}
	inner := &machineScope{function: callee, states: map[*localBinding]bool{}, triggers: map[*localBinding]bool{}, lets: map[*localBinding]*Expr{}, roots: map[*localBinding]*localBinding{}, via: append(slices.Clone(s.via), callee.Name), site: site}
	for index, argument := range call.Args {
		parameter := index
		if index < len(call.ArgumentParameters) {
			parameter = call.ArgumentParameters[index]
		}
		if parameter < 0 || parameter >= len(callee.Params) || callee.Params[parameter].binding == nil {
			continue
		}
		if m.denotes(s.states, s, argument) {
			inner.states[callee.Params[parameter].binding] = true
		}
		if m.denotes(s.triggers, s, argument) {
			inner.triggers[callee.Params[parameter].binding] = true
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
	narrowed *machineSet
	span     Span
	via      []string
}

// addPath appends one path after charging it, so the paths a walk retains
// never outnumber the units it paid.
func (m *machineCheck) addPath(out []statePath, path statePath) []statePath {
	if !m.spend() {
		return out
	}
	return append(out, path)
}

// paths appends the visible ways e produces a State value to out. A refusal
// stops collection; the declaration is then withheld.
func (m *machineCheck) paths(s *machineScope, e *Expr, n machineNarrowing, out []statePath) []statePath {
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
		if value, ok := s.lets[e.binding]; ok && e.binding != nil {
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
		callee := e.ResolvedFunction
		if callee != nil && callee.Body != nil && callee.Module == currentModuleIdentity && callee.returnID == m.state && !callee.Effect {
			inner, ok := m.helperScope(s, callee, e)
			if !ok {
				return out
			}
			visit, ok := m.visitOf(callee, inner, n)
			if !ok {
				return out
			}
			if m.reentry(s, visit) {
				return m.addPath(out, statePath{variant: -1, span: e.Span, via: s.via})
			}
			if len(s.via) >= maxMachineHelperDepth {
				m.helperDepthExhausted(s, e, n)
				return m.addPath(out, statePath{variant: -1, span: e.Span, via: s.via})
			}
			inner.active = append(slices.Clone(s.active), visit)
			return m.blockPaths(inner, callee.Body, n, out)
		}
	}
	return m.addPath(out, statePath{variant: -1, span: e.Span, via: s.via})
}

func (m *machineCheck) blockPaths(s *machineScope, b *Block, n machineNarrowing, out []statePath) []statePath {
	if b == nil || len(b.Statements) == 0 {
		return m.addPath(out, statePath{variant: -1})
	}
	if !m.charge(len(b.Statements)) {
		return out
	}
	for _, statement := range b.Statements {
		if statement.Kind == "let" && statement.Value != nil && statement.binding != nil {
			s.lets[statement.binding] = statement.Value
		}
	}
	last := b.Statements[len(b.Statements)-1]
	if last.Kind != "expr" {
		return out
	}
	return m.paths(s, last.Value, n, out)
}

// checkStay is the static same-tag rule. Inside an arm narrowing the current
// state to one variant, a literal Stay argument must construct that variant;
// an arm admitting several variants accepts only the unchanged state. An
// unnarrowed or dynamic value keeps the runtime defect.
func (m *machineCheck) checkStay(s *machineScope, e, argument *Expr, n machineNarrowing) {
	for _, path := range m.paths(s, argument, n, nil) {
		if path.variant < 0 || path.narrowed == nil || len(path.narrowed.positions) == 1 && path.narrowed.positions[0] == path.variant {
			continue
		}
		enum := m.enum.Name
		variant := m.enum.Variants[path.variant].Name
		qualified, ok := m.qualified(path.narrowed)
		if !ok {
			return
		}
		format := "Step.Stay keeps the current entry, so it cannot change the state tag: this arm narrows the state to %s but Stay constructs %s.%s; use Step.Go%s"
		if path.narrowed.admits(path.variant) {
			format = "Step.Stay keeps the current entry, so it cannot change the state tag: this arm admits %s, so the literal %s.%s changes the tag for another alternative; split the arm or use Stay with the current state%s"
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
			// A Stay inside a helper reports at the caller's call of it; one in
			// the step body itself, reached again through recursion, reports
			// at the Stay.
			if s.site.Length > 0 && s.function != m.roleFunction {
				at = s.site
			}
		}
		m.reportf(machineStayCode, at, format, qualified, enum, variant, helper)
	}
}

// qualified lists a narrowing as `Enum.Variant | …`, charged before it is
// built.
func (m *machineCheck) qualified(set *machineSet) (string, bool) {
	n := 0
	for _, i := range set.positions {
		n += len(m.enum.Name) + 1 + len(m.enum.Variants[i].Name) + 3
	}
	if !m.charge(machineTextUnits(n)) {
		return "", false
	}
	var b strings.Builder
	b.Grow(n)
	for j, i := range set.positions {
		if j > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(m.enum.Name)
		b.WriteByte('.')
		b.WriteString(m.enum.Variants[i].Name)
	}
	return b.String(), true
}

// transition records one possible decision. targets is a narrowing of the
// state: nil means every state, the empty set no target state.
func (m *machineCheck) transition(s *machineScope, span Span, n machineNarrowing, decision string, targets *machineSet, precision string, guarded bool) {
	if len(m.plan.Transitions) >= maxMachineTransitions {
		if !m.refused {
			m.report(machineBudgetCode, fmt.Sprintf("machine %s exceeds %d possible transitions; no plan is published", m.machine.Name, maxMachineTransitions), m.machine.Span)
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
	m.plan.Transitions = append(m.plan.Transitions, MachineTransition{ID: m.transitionID(len(m.plan.Transitions)), Role: m.role, From: n.states.names(m.enum), On: n.triggers.names(m.trigger), Decision: decision, Targets: targets.names(m.enum), Precision: precision, Guarded: guarded, Via: slices.Clone(s.via), Span: span})
}

// transitionKey is a compact canonical identity: the role, decision and
// precision are fixed words, the span is two integers, each narrowing is its
// interned ordinal and each helper its ordinal. Its size never follows the
// length of a source identifier or the width of a narrowing.
func (m *machineCheck) transitionKey(span Span, decision, precision string, guarded bool, from, on, to *machineSet, via []string) string {
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
	for _, set := range []*machineSet{from, on, to} {
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

func (m *machineCheck) decision(s *machineScope, e *Expr, variant string, argument *Expr, n machineNarrowing, guarded bool) {
	switch variant {
	case "Go":
		if targets, precision, ok := m.targets(s, argument, n); ok {
			m.transition(s, e.Span, n, machineDecisionGo, targets, precision, guarded)
		}
	case "Stay":
		m.transition(s, e.Span, n, machineDecisionStay, n.states, machineTargetCurrent, guarded)
	case "Ignore", "Reject", "Done":
		decision := machineDecisionDone
		switch variant {
		case "Ignore":
			decision = machineDecisionIgnore
		case "Reject":
			decision = machineDecisionReject
		}
		if none, ok := m.empty(m.enum); ok {
			m.transition(s, e.Span, n, decision, none, machineTargetLiteral, guarded)
		}
	}
}

// targets resolves the states a Go decision enters, as a narrowing (nil means
// every state). Paths that keep the current state carry canonical narrowings,
// so equal narrowings are enumerated once, and each distinct enumeration is
// charged before it runs.
func (m *machineCheck) targets(s *machineScope, argument *Expr, n machineNarrowing) (*machineSet, string, bool) {
	precision, every := machineTargetLiteral, false
	positions := []int32{}
	enumerated := map[*machineSet]bool{}
	for _, path := range m.paths(s, argument, n, nil) {
		switch {
		case path.variant >= 0:
			positions = append(positions, path.variant)
		case path.same:
			if precision == machineTargetLiteral {
				precision = machineTargetCurrent
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
			return nil, machineTargetConservative, !m.refused
		}
	}
	if m.refused {
		return nil, precision, false
	}
	if every {
		return nil, precision, true
	}
	slices.Sort(positions)
	set, ok := m.intern(m.enum, slices.Compact(positions))
	return set, precision, ok
}

// conservative records a Step value the walk cannot see into: any decision to
// any state.
func (m *machineCheck) conservative(s *machineScope, e *Expr, n machineNarrowing, guarded bool) {
	state, _, ok := m.stepArguments(e.checked.valueID())
	if !ok || state != m.state {
		return
	}
	m.transition(s, e.Span, n, machineDecisionAny, nil, machineTargetConservative, guarded)
}

// finishPlan completes a checked plan. Module-wide projections are memoized
// and charged once; the plan's own state facts and enterable search are
// charged before they are allocated. A refusal withholds the plan.
func (m *machineCheck) finishPlan(step *Function, event *Enum, complete *Function) bool {
	plan := m.plan
	var ok bool
	if plan.State, ok = m.dataRef(m.state, m.enum); !ok {
		return false
	}
	if plan.Event, ok = m.dataRef(step.Params[1].typeID, event); !ok {
		return false
	}
	if plan.Output, ok = m.value(m.output); !ok {
		return false
	}
	stepFacts, ok := m.function(machineStep, step)
	if !ok {
		return false
	}
	plan.Functions = append(plan.Functions, stepFacts)
	if complete != nil {
		completeFacts, ok := m.function(machineComplete, complete)
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
	initial := int32(slices.IndexFunc(m.enum.Variants, func(v Variant) bool { return v.Name == plan.Initial.Variant }))
	enterable := make([]bool, len(m.enum.Variants))
	enterable[initial] = true
	if len(plan.Transitions) > 0 {
		index := m.variantIndex(m.enum)
		if index == nil {
			return false
		}
		bySource := map[int32][]int{}
		for i, t := range plan.Transitions {
			if t.Decision == machineDecisionDone || t.Decision == machineDecisionAny {
				plan.Terminal = true
			}
			if t.Decision != machineDecisionGo && t.Decision != machineDecisionStay && t.Decision != machineDecisionAny {
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
	plan.States = make([]MachineStateFacts, len(m.enum.Variants))
	for i, variant := range m.enum.Variants {
		plan.States[i] = MachineStateFacts{Variant: variant.Name, Initial: int32(i) == initial, Enterable: enterable[i]}
	}
	return true
}

// FindMachine returns the checked plan of a machine declaration. A program
// with any diagnostic has no checked plan.
func (r *Result) FindMachine(name string) *MachinePlan {
	if r == nil || !r.Checked {
		return nil
	}
	for i := range r.Machines {
		if r.Machines[i].Name == name {
			return &r.Machines[i]
		}
	}
	return nil
}

// MachineInspection is the shared CLI/MCP projection of one canonical plan.
func (r *Result) MachineInspection(name, file string) (map[string]any, error) {
	if !r.Checked {
		return nil, fmt.Errorf("machine inspection requires checked source")
	}
	plan := r.FindMachine(name)
	if plan == nil {
		return nil, fmt.Errorf("unknown machine %s", name)
	}
	projection := r.ProjectAllTypes()
	if !projection.Complete {
		return nil, fmt.Errorf("type projection unavailable: %s", projection.Error)
	}
	response := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "file": file, "target": r.Target, "checked": r.Checked, "machine": plan, "types": projection.Types, "rows": projection.Rows, "declarations": r.ProjectionDeclarations(projection), "typeProjectionComplete": true}
	r.AddSourceInputs(response)
	usage, err := r.ValidateProjectionResponse(projection, response)
	if err != nil {
		return nil, err
	}
	response["typeProjectionUsage"] = usage
	return response, nil
}
