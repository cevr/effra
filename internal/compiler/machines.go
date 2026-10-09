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
	"reflect"
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
	ID               string                   `json:"id"`
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
	"transition IDs and spans are scoped to the semantic revision",
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

// machineCheck holds one declaration's analysis.
type machineCheck struct {
	c       *checker
	machine *Machine
	plan    *MachinePlan
	state   TypeID
	enum    *Enum
	output  TypeID
	work    int
	failed  bool
	refused bool
	seen    map[string]bool
	// role-scoped trigger enum while walking step (event) or complete (outcome).
	role    string
	trigger *Enum
	// roleFunction is the step or complete function being walked.
	roleFunction *Function
	// transitionKeys deduplicates published transitions by a compact key of
	// variant indices and ordinals, never by joined source text.
	transitionKeys map[string]bool
	// shared is the module-wide budget every declaration also charges.
	shared *machineBudget
	// indexes maps each enum's variant names to declaration positions.
	indexes map[*Enum]map[string]int
	// viaIndex numbers helper names for transition keys.
	viaIndex map[string]int
}

type machineBudget struct {
	work      int
	exhausted bool
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
	for _, machine := range c.program.Machines {
		check := &machineCheck{c: c, machine: machine, seen: map[string]bool{}, shared: shared}
		if plan := check.run(); plan != nil {
			c.result.Machines = append(c.result.Machines, *plan)
		}
	}
	slices.SortFunc(c.result.Machines, func(a, b MachinePlan) int { return strings.Compare(a.ID, b.ID) })
}

func (m *machineCheck) report(code, message string, span Span) {
	m.failed = true
	if m.refused && code != machineBudgetCode {
		// A check that stopped because the budget ran out is not evidence of
		// a contract violation; the budget refusal is the diagnostic.
		return
	}
	key := code + "\x00" + message + "\x00" + fmt.Sprint(span.Offset, ":", span.Length)
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
		m.report(machineContractCode, "machine "+binding.Kind+" names unknown module function "+binding.Name, binding.NameSpan)
		return nil
	}
	m.c.observeReference(m.machine, binding.NameSpan, lexicalTarget{kind: "function", function: f})
	if len(f.RowParameters) > 0 || len(f.TypeParameters) > 0 {
		m.report(machineContractCode, "machine "+binding.Kind+" "+f.Name+" must be a monomorphic function; row and type parameters are unsupported", binding.NameSpan)
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
	enum, _, ok := m.c.checkedVariants(id)
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
		m.report(machineContractCode, "machine "+machine.Name+" requires a step binding", machine.Span)
	}
	if initialBinding == nil {
		m.report(machineContractCode, "machine "+machine.Name+" requires an initial state", machine.Span)
	}
	if enterBinding != nil {
		m.report(machineAdmissionCode, "machine entry work is not yet admitted: enter returns an optional deferred Outcome recipe, which needs typed recipe values, payload-aware recovery of work failures and checked entry ownership; declare the machine without enter and complete until those land", enterBinding.Span)
	}
	if completeBinding != nil && enterBinding == nil {
		m.report(machineContractCode, "machine complete consumes the outcome of entry work, so it requires an enter binding", completeBinding.Span)
	}
	m.plan = &MachinePlan{ID: machineID(machine.Name), Name: machine.Name, Span: machine.Span, Functions: []MachineFunction{}, Transitions: []MachineTransition{}, States: []MachineStateFacts{}, TerminalFailures: []MachineRowContribution{}, Requirements: []MachineRowContribution{}, Evidence: machinePlanEvidence, Limitations: append([]string{}, machinePlanLimitations...)}
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
		m.report(machineContractCode, fmt.Sprintf("machine %s %s must take (state, %s) parameters; it declares %d", role, f.Name, triggerWord, len(f.Params)), at)
		return nil
	}
	state, output, ok := m.stepArguments(f.returnID)
	if !ok {
		m.report(machineContractCode, "machine "+role+" "+f.Name+" must return the bundled effra/machines Step<State, Output>; it returns "+m.c.displayTypeID(f.returnID), at)
		return nil
	}
	if f.Params[0].typeID != state {
		m.report(machineContractCode, "machine "+role+" "+f.Name+" takes the current state first: its first parameter is "+f.Params[0].Type+" but it returns Step<"+m.c.displayTypeID(state)+", …>", f.Params[0].Span)
		return nil
	}
	enum := m.userEnum(state)
	if enum == nil {
		m.report(machineContractCode, "machine state "+m.c.displayTypeID(state)+" must be a closed, non-generic enum declared in this module", f.Params[0].Span)
		return nil
	}
	trigger := m.userEnum(f.Params[1].typeID)
	if trigger == nil {
		m.report(machineContractCode, "machine "+triggerWord+" "+f.Params[1].Type+" must be a closed, non-generic enum declared in this module", f.Params[1].Span)
		return nil
	}
	if trigger == enum {
		m.report(machineContractCode, "machine "+triggerWord+" and state must be distinct types; "+trigger.Name+" is both", f.Params[1].Span)
		return nil
	}
	if role == machineStep {
		m.state, m.enum, m.output = state, enum, output
		if refusal := m.dataRefusal(state, enum.Name, map[TypeID]bool{}); refusal != "" {
			m.report(machineAdmissionCode, refusal, f.Params[0].Span)
		}
		if refusal := m.dataRefusal(f.Params[1].typeID, trigger.Name, map[TypeID]bool{}); refusal != "" {
			m.report(machineAdmissionCode, refusal, f.Params[1].Span)
		}
		if refusal := m.dataRefusal(output, "output "+m.c.displayTypeID(output), map[TypeID]bool{}); refusal != "" {
			m.report(machineAdmissionCode, refusal, at)
		}
		return trigger
	}
	if state != m.state {
		m.report(machineContractCode, "machine complete "+f.Name+" must use the step state "+m.enum.Name+"; it uses "+enum.Name, f.Params[0].Span)
		return nil
	}
	if output != m.output {
		m.report(machineContractCode, "machine complete "+f.Name+" must publish the step output "+m.c.displayTypeID(m.output)+"; it publishes "+m.c.displayTypeID(output), at)
		return nil
	}
	if event != nil && trigger == event {
		m.report(machineContractCode, "machine outcome and event must be distinct types so public sends cannot forge work completion; "+trigger.Name+" is both", f.Params[1].Span)
		return nil
	}
	return trigger
}

// dataRefusal is the conservative payload rule until the portable ownership
// checker owns machine data: state, event, outcome and output values are
// closed first-order data. Callables, recipes, providers, fibers and host or
// runtime handles are refused rather than admitted unchecked.
//
// It is a reachability search from one root with a visited set that is never
// cleared, so every reachable type is inspected exactly once whatever its
// nesting depth or sharing. Each type costs one budget unit; exhaustion reports
// EF142 and refuses the declaration instead of admitting the rest.
func (m *machineCheck) dataRefusal(id TypeID, path string, visited map[TypeID]bool) string {
	if visited[id] || !m.spend() {
		return ""
	}
	visited[id] = true
	n := m.c.node(id)
	if n == nil {
		return ""
	}
	switch n.Kind {
	case "primitive", "never", "invalid":
		return ""
	case "enum", "record", "application":
		refusal := ""
		m.c.walkDataFields(id, func(name string, field Field) {
			if refusal == "" {
				refusal = m.dataRefusal(field.typeID, path+"."+name, visited)
			}
		})
		return refusal
	case "callable", "callable-shape", "recipe", "providerRecipe":
		return "machine data " + path + " carries a function or effect recipe; machine state, event and output values are not yet admitted to carry behavior: portable machine payloads need checked actor ownership"
	default:
		return "machine data " + path + " carries " + m.c.displayTypeID(id) + "; machine state, event and output values are not yet admitted to carry host, provider or runtime handles: portable machine payloads need checked actor ownership"
	}
}

// checkInitial admits a literal State constructor with a static payload. The
// plan identity records its tag; evaluating it constructs data only.
func (m *machineCheck) checkInitial(binding *MachineBinding) {
	value := binding.Value
	previous := m.c.recordFacts
	m.c.recordFacts = true
	checked := m.c.expr(value, localEnv{}, false)
	m.c.recordFacts = previous
	if checked.isEffect() || checked.valueID() != m.state {
		m.report(machineContractCode, "machine initial state must be a "+m.enum.Name+" value; found "+m.c.displayChecked(checked), value.Span)
		return
	}
	variant, ok := m.literalVariant(value)
	if !ok || !m.staticValue(value) {
		m.report(machineContractCode, "machine initial state must be a literal "+m.enum.Name+" constructor with a static payload", value.Span)
		return
	}
	m.plan.Initial = MachineInitial{Variant: variant, Span: value.Span}
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

// machineVariants is a narrowing: the variants a subject may have here. nil means
// unnarrowed, every declared variant.
type machineVariants map[string]bool

func (v machineVariants) admits(variant string) bool { return v == nil || v[variant] }

func (v machineVariants) refine(cell []*MatchPattern) machineVariants {
	out := machineVariants{}
	for _, pattern := range cell {
		if v.admits(pattern.VariantName) {
			out[pattern.VariantName] = true
		}
	}
	return out
}

// positions lists the set as declaration positions in order; nil lists
// every variant. A non-nil set costs its own size, never the enum's: names
// resolve through an index built (and charged) once per enum.
func (m *machineCheck) positions(v machineVariants, enum *Enum) []int {
	if v == nil {
		out := make([]int, len(enum.Variants))
		for i := range out {
			out[i] = i
		}
		return out
	}
	index := m.variantIndex(enum)
	out := make([]int, 0, len(v))
	for name := range v {
		if i, ok := index[name]; ok {
			out = append(out, i)
		}
	}
	slices.Sort(out)
	return out
}

// order lists the set's variant names in declaration order.
func (m *machineCheck) order(v machineVariants, enum *Enum) []string {
	return machineNames(m.positions(v, enum), enum)
}

func machineNames(positions []int, enum *Enum) []string {
	out := make([]string, len(positions))
	for j, i := range positions {
		out[j] = enum.Variants[i].Name
	}
	return out
}

func (m *machineCheck) variantIndex(enum *Enum) map[string]int {
	if index, ok := m.indexes[enum]; ok {
		return index
	}
	if !m.charge(len(enum.Variants)) {
		return nil
	}
	index := make(map[string]int, len(enum.Variants))
	for i, variant := range enum.Variants {
		index[variant.Name] = i
	}
	if m.indexes == nil {
		m.indexes = map[*Enum]map[string]int{}
	}
	m.indexes[enum] = index
	return index
}

// size is the length of ordered(enum) without allocating it.
func (v machineVariants) size(enum *Enum) int {
	if v == nil {
		return len(enum.Variants)
	}
	return len(v)
}

// covers reports whether a Stay check under v implies the same check under
// w. A non-nil narrowing is checked strictly, and narrowing further only
// removes alternatives, so a non-nil superset covers a non-nil subset. nil
// (unnarrowed) is lenient for Stay (the runtime defect applies) yet refines
// to every alternative a match names, so it covers only itself.
func (v machineVariants) covers(w machineVariants) bool {
	if v == nil || w == nil {
		return v == nil && w == nil
	}
	for variant := range w {
		if !v[variant] {
			return false
		}
	}
	return true
}

type machineNarrowing struct {
	states, triggers machineVariants
}

// machineVisit is the context one function body is walked under: the
// narrowing of the current state and trigger, and which parameters receive
// them. The Stay rule's verdict depends on all three, so a recursive call is
// reused only when an active visit of the same function covers its context.
type machineVisit struct {
	function         *Function
	states, triggers machineVariants
	bound            string
}

// covers reports whether checking under v already discharges w. Triggers
// only decide which arms are reachable, so nil (every trigger) covers any
// trigger narrowing; states follow machineVariants.covers.
func (v machineVisit) covers(w machineVisit) bool {
	if v.function != w.function || v.bound != w.bound || !v.states.covers(w.states) {
		return false
	}
	if v.triggers == nil {
		return true
	}
	if w.triggers == nil {
		return false
	}
	for trigger := range w.triggers {
		if !v.triggers[trigger] {
			return false
		}
	}
	return true
}

func machineVisitOf(f *Function, scope *machineScope, n machineNarrowing) machineVisit {
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
	return machineVisit{function: f, states: n.states, triggers: n.triggers, bound: string(bound)}
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
func (s *machineScope) reentry(visit machineVisit) bool {
	return slices.ContainsFunc(s.active, func(active machineVisit) bool { return active.covers(visit) })
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
	scope.active = []machineVisit{machineVisitOf(f, scope, machineNarrowing{})}
	m.block(scope, f.Body, machineNarrowing{}, true, false)
}

func (m *machineCheck) block(s *machineScope, b *Block, n machineNarrowing, tail, guarded bool) {
	if b == nil {
		return
	}
	for index, statement := range b.Statements {
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
		if !m.spend() {
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
// the arm a data-dependent guard.
func (m *machineCheck) arm(s *machineScope, match *Expr, arm *MatchArm, n machineNarrowing) (machineNarrowing, bool, bool) {
	out, exact := n, true
	for index, subject := range match.Args {
		if index >= len(arm.Patterns) {
			continue
		}
		switch {
		case m.denotes(s.states, s, subject):
			out.states = out.states.refine(arm.Patterns[index])
			if len(out.states) == 0 {
				return out, exact, false
			}
		case m.denotes(s.triggers, s, subject):
			out.triggers = out.triggers.refine(arm.Patterns[index])
			if len(out.triggers) == 0 {
				return out, exact, false
			}
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
	inner := m.helperScope(s, callee, call)
	visit := machineVisitOf(callee, inner, n)
	if s.reentry(visit) {
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
	chain := append(slices.Clone(s.via), call.ResolvedFunction.Name)
	at := call.Span
	if s.site.Length > 0 {
		at = s.site
	}
	m.report(machineBudgetCode, fmt.Sprintf("machine %s cannot check Step.Stay through helper %s: the chain exceeds %d helpers while the current state is narrowed or passed; no plan is published", m.machine.Name, strings.Join(chain, " -> "), maxMachineHelperDepth), at)
	return true
}

func (m *machineCheck) helperScope(s *machineScope, callee *Function, call *Expr) *machineScope {
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
	return inner
}

// statePath is one visible way a State value is produced: a literal
// constructor tag, the unchanged current state, or a dynamic value.
type statePath struct {
	variant  string
	same     bool
	narrowed machineVariants
	span     Span
	via      []string
}

func (m *machineCheck) paths(s *machineScope, e *Expr, n machineNarrowing) []statePath {
	if e == nil || !m.spend() {
		return []statePath{{}}
	}
	if m.denotes(s.states, s, e) {
		return []statePath{{same: true, narrowed: n.states, span: e.Span, via: s.via}}
	}
	if variant, ok := m.literalVariant(e); ok {
		return []statePath{{variant: variant, narrowed: n.states, span: e.Span, via: s.via}}
	}
	switch e.Kind {
	case "name":
		if value, ok := s.lets[e.binding]; ok && e.binding != nil {
			return m.paths(s, value, n)
		}
	case "if":
		return append(m.blockPaths(s, e.Then, n), m.blockPaths(s, e.Else, n)...)
	case "match":
		out := []statePath{}
		for _, arm := range e.Arms {
			narrowed, _, reachable := m.arm(s, e, arm, n)
			if reachable {
				out = append(out, m.blockPaths(s, arm.Body, narrowed)...)
			}
		}
		return out
	case "scope":
		return m.blockPaths(s, e.Then, n)
	case "call":
		callee := e.ResolvedFunction
		if callee != nil && callee.Body != nil && callee.Module == currentModuleIdentity && callee.returnID == m.state && !callee.Effect {
			inner := m.helperScope(s, callee, e)
			visit := machineVisitOf(callee, inner, n)
			if s.reentry(visit) {
				return []statePath{{span: e.Span, via: s.via}}
			}
			if len(s.via) >= maxMachineHelperDepth {
				m.helperDepthExhausted(s, e, n)
				return []statePath{{span: e.Span, via: s.via}}
			}
			inner.active = append(slices.Clone(s.active), visit)
			return m.blockPaths(inner, callee.Body, n)
		}
	}
	return []statePath{{span: e.Span, via: s.via}}
}

func (m *machineCheck) blockPaths(s *machineScope, b *Block, n machineNarrowing) []statePath {
	if b == nil || len(b.Statements) == 0 {
		return []statePath{{}}
	}
	for _, statement := range b.Statements {
		if statement.Kind == "let" && statement.Value != nil && statement.binding != nil {
			s.lets[statement.binding] = statement.Value
		}
	}
	last := b.Statements[len(b.Statements)-1]
	if last.Kind != "expr" {
		return nil
	}
	return m.paths(s, last.Value, n)
}

// checkStay is the static same-tag rule. Inside an arm narrowing the current
// state to one variant, a literal Stay argument must construct that variant;
// an arm admitting several variants accepts only the unchanged state. An
// unnarrowed or dynamic value keeps the runtime defect.
func (m *machineCheck) checkStay(s *machineScope, e, argument *Expr, n machineNarrowing) {
	for _, path := range m.paths(s, argument, n) {
		if path.variant == "" || path.narrowed == nil || len(path.narrowed) == 1 && path.narrowed[path.variant] {
			continue
		}
		// The message lists the narrowing; charge it before building it.
		if !m.charge(path.narrowed.size(m.enum)) {
			return
		}
		enum := m.enum.Name
		qualified := m.order(path.narrowed, m.enum)
		for i := range qualified {
			qualified[i] = enum + "." + qualified[i]
		}
		message := fmt.Sprintf("Step.Stay keeps the current entry, so it cannot change the state tag: this arm narrows the state to %s but Stay constructs %s.%s; use Step.Go", strings.Join(qualified, " | "), enum, path.variant)
		if path.narrowed.admits(path.variant) {
			message = fmt.Sprintf("Step.Stay keeps the current entry, so it cannot change the state tag: this arm admits %s, so the literal %s.%s changes the tag for another alternative; split the arm or use Stay with the current state", strings.Join(qualified, " | "), enum, path.variant)
		}
		at := e.Span
		if len(path.via) > 0 {
			message += " (through helper " + strings.Join(path.via, " -> ") + ")"
			// A Stay inside a helper reports at the caller's call of it; one in
			// the step body itself, reached again through recursion, reports
			// at the Stay.
			if s.site.Length > 0 && s.function != m.roleFunction {
				at = s.site
			}
		}
		m.report(machineStayCode, message, at)
	}
}

// transition records one possible decision. targets is a variant set: nil
// means every state, an empty set means no target state.
func (m *machineCheck) transition(s *machineScope, span Span, n machineNarrowing, decision string, targets machineVariants, precision string, guarded bool) {
	if len(m.plan.Transitions) >= maxMachineTransitions {
		if !m.refused {
			m.refused = true
			m.report(machineBudgetCode, fmt.Sprintf("machine %s exceeds %d possible transitions; no plan is published", m.machine.Name, maxMachineTransitions), m.machine.Span)
		}
		return
	}
	// A transition's From, On, Targets and Via lists are plan output.
	// Charging them before they are allocated bounds plan size, the
	// enterable search and the graph projection.
	if !m.charge(targets.size(m.enum) + n.states.size(m.enum) + n.triggers.size(m.trigger) + len(s.via)) {
		return
	}
	from, on, to := m.positions(n.states, m.enum), m.positions(n.triggers, m.trigger), m.positions(targets, m.enum)
	key := m.transitionKey(span, decision, precision, guarded, from, on, to, s.via)
	if m.transitionKeys == nil {
		m.transitionKeys = map[string]bool{}
	}
	if m.transitionKeys[key] {
		return
	}
	m.transitionKeys[key] = true
	t := MachineTransition{Role: m.role, From: machineNames(from, m.enum), On: machineNames(on, m.trigger), Decision: decision, Targets: machineNames(to, m.enum), Precision: precision, Guarded: guarded, Via: slices.Clone(s.via), Span: span}
	t.ID = fmt.Sprintf("%s:transition:%d", m.plan.ID, len(m.plan.Transitions))
	m.plan.Transitions = append(m.plan.Transitions, t)
}

// transitionKey is a compact canonical identity: the role, decision and
// precision are fixed words, the span is two integers, and every list is
// variant positions or helper ordinals. Its size follows the charged list
// lengths, never the length of a source identifier.
func (m *machineCheck) transitionKey(span Span, decision, precision string, guarded bool, from, on, to []int, via []string) string {
	key := make([]byte, 0, 32+2*(len(from)+len(on)+len(to)+len(via)))
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
	for _, list := range [][]int{from, on, to} {
		key = binary.AppendUvarint(key, uint64(len(list)))
		for _, i := range list {
			key = binary.AppendUvarint(key, uint64(i))
		}
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
		targets, precision := m.targets(s, argument, n)
		m.transition(s, e.Span, n, machineDecisionGo, targets, precision, guarded)
	case "Stay":
		m.transition(s, e.Span, n, machineDecisionStay, n.states, machineTargetCurrent, guarded)
	case "Ignore":
		m.transition(s, e.Span, n, machineDecisionIgnore, machineVariants{}, machineTargetLiteral, guarded)
	case "Reject":
		m.transition(s, e.Span, n, machineDecisionReject, machineVariants{}, machineTargetLiteral, guarded)
	case "Done":
		m.transition(s, e.Span, n, machineDecisionDone, machineVariants{}, machineTargetLiteral, guarded)
	}
}

// targets resolves the states a Go decision enters, as a set (nil means
// every state). Paths that keep the current state share their narrowing
// with the caller, so each distinct narrowing is enumerated once, and each
// enumeration is charged before it runs.
func (m *machineCheck) targets(s *machineScope, argument *Expr, n machineNarrowing) (machineVariants, string) {
	set, precision, every := machineVariants{}, machineTargetLiteral, false
	enumerated := map[uintptr]bool{}
	for _, path := range m.paths(s, argument, n) {
		switch {
		case path.variant != "":
			set[path.variant] = true
		case path.same:
			if precision == machineTargetLiteral {
				precision = machineTargetCurrent
			}
			if path.narrowed == nil {
				every = true
				continue
			}
			identity := reflect.ValueOf(path.narrowed).Pointer()
			if every || enumerated[identity] {
				continue
			}
			enumerated[identity] = true
			if !m.charge(len(path.narrowed)) {
				return machineVariants{}, precision
			}
			for variant := range path.narrowed {
				set[variant] = true
			}
		default:
			return nil, machineTargetConservative
		}
	}
	if every {
		return nil, precision
	}
	return set, precision
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

func (m *machineCheck) function(role string, f *Function) MachineFunction {
	kind := "pure"
	failures, services := []string{}, []string{}
	if f.Effect {
		kind = "effect"
		failures = normalized(m.c.rowLabels(f.failureID))
		services = normalized(m.c.rowLabels(f.serviceID))
	}
	return MachineFunction{Role: role, Name: f.Name, Identity: f.Identity, Span: f.Span, Kind: kind, Failures: failures, Services: services, Contract: m.c.projectCheckedBase(m.c.checkedFunction(f, true, false))}
}

func (m *machineCheck) dataRef(id TypeID, enum *Enum) MachineDataRef {
	return MachineDataRef{Name: enum.Name, Identity: m.c.declarationQualifier("enum", enum.Name), Variants: m.order(nil, enum), Type: m.c.projectCheckedBase(m.c.checkedDataID(id, nil, nil))}
}

// finishPlan completes a checked plan. Its state and event variant lists and
// per-state facts are charged before they are allocated; a refusal withholds
// the plan.
func (m *machineCheck) finishPlan(step *Function, event *Enum, complete *Function) bool {
	if !m.charge(2*len(m.enum.Variants) + len(event.Variants)) {
		return false
	}
	plan := m.plan
	plan.State = m.dataRef(m.state, m.enum)
	plan.Event = m.dataRef(step.Params[1].typeID, event)
	plan.Output = m.c.projectCheckedBase(m.c.checkedDataID(m.output, nil, nil))
	plan.Functions = append(plan.Functions, m.function(machineStep, step))
	if complete != nil {
		plan.Functions = append(plan.Functions, m.function(machineComplete, complete))
	}
	failures, services := map[string][]string{}, map[string][]string{}
	for _, f := range plan.Functions {
		for _, name := range f.Failures {
			failures[name] = append(failures[name], f.Role)
		}
		for _, name := range f.Services {
			services[name] = append(services[name], f.Role)
		}
	}
	plan.TerminalFailures = machineContributions(failures)
	plan.Requirements = machineContributions(services)
	// Enterable states: a worklist over the transitions indexed by source, so
	// the work is linear in the charged plan size.
	bySource := map[string][]int{}
	for index, t := range plan.Transitions {
		if t.Decision == machineDecisionDone || t.Decision == machineDecisionAny {
			plan.Terminal = true
		}
		if t.Decision != machineDecisionGo && t.Decision != machineDecisionStay && t.Decision != machineDecisionAny {
			continue
		}
		for _, from := range t.From {
			bySource[from] = append(bySource[from], index)
		}
	}
	enterable := machineVariants{plan.Initial.Variant: true}
	fired := make([]bool, len(plan.Transitions))
	for work := []string{plan.Initial.Variant}; len(work) > 0; {
		state := work[len(work)-1]
		work = work[:len(work)-1]
		for _, index := range bySource[state] {
			if fired[index] {
				continue
			}
			fired[index] = true
			for _, target := range plan.Transitions[index].Targets {
				if !enterable[target] {
					enterable[target] = true
					work = append(work, target)
				}
			}
		}
	}
	for _, variant := range m.enum.Variants {
		plan.States = append(plan.States, MachineStateFacts{Variant: variant.Name, Initial: variant.Name == plan.Initial.Variant, Enterable: enterable[variant.Name]})
	}
	return true
}

func machineContributions(rows map[string][]string) []MachineRowContribution {
	out := []MachineRowContribution{}
	for name, roles := range rows {
		out = append(out, MachineRowContribution{Name: name, Roles: normalized(roles)})
	}
	slices.SortFunc(out, func(a, b MachineRowContribution) int { return strings.Compare(a.Name, b.Name) })
	return out
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
