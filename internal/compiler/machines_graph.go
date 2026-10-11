package compiler

import (
	"slices"
	"strings"
)

// GraphKindMachine projects checked machine plans: one node per declaration,
// its states and terminal output, and one edge per possible decision. The
// projection reads MachinePlan only; it never re-derives transitions and never
// executes a step.

var machineGraphRelations = []string{"any", "contains", "done", "go", "ignore", "initial", "reject", "stay"}

// GraphMachineFacts are the declaration-level facts of one machine node.
type GraphMachineFacts struct {
	State            MachineDataRef           `json:"state"`
	Event            MachineDataRef           `json:"event"`
	Output           TypeRef                  `json:"output"`
	Initial          string                   `json:"initial"`
	Functions        []GraphMachineFunction   `json:"functions"`
	TerminalFailures []MachineRowContribution `json:"terminalFailures"`
	Requirements     []MachineRowContribution `json:"requirements"`
	Evidence         string                   `json:"evidence"`
}

// GraphMachineFunction is one bound function; its contract is published in
// the view's type tables through the node's projected values.
type GraphMachineFunction struct {
	Role     string   `json:"role"`
	Name     string   `json:"name"`
	Identity string   `json:"identity"`
	Kind     string   `json:"kind"`
	Failures []string `json:"failures"`
	Services []string `json:"requirements"`
}

type GraphMachineStateFacts struct {
	Variant   string `json:"variant"`
	Initial   bool   `json:"initial"`
	Enterable bool   `json:"enterable"`
}

// GraphMachineTransitionFacts identify the plan transition an edge projects.
type GraphMachineTransitionFacts struct {
	Transition string   `json:"transition"`
	Role       string   `json:"role"`
	On         []string `json:"on"`
	Precision  string   `json:"precision"`
	Guarded    bool     `json:"guarded"`
	Via        []string `json:"via,omitempty"`
}

// machineGraphID builds a generated graph identity after charging its bytes
// to the view's work. Plan IDs carry the machine's source name and every state
// node and edge identity repeats one, so identities are charged by length, not
// only by count.
func machineGraphID(facts *graphFacts, prefix string, parts ...string) (string, bool) {
	n := len(prefix)
	for _, part := range parts {
		n += 1 + len(part) + 2*(strings.Count(part, "%")+strings.Count(part, "|"))
	}
	if !facts.spend(machineTextUnits(n)) {
		return "", false
	}
	return graphTupleID(prefix, parts...), true
}

func (r *Result) machineGraphFacts(limits GraphViewLimits) (*graphFacts, error) {
	facts := newGraphFacts(GraphKindMachine, limits)
	facts.semantics = machinePlanEvidence
	facts.limitations = append([]string{}, machinePlanLimitations...)
	plans := append([]MachinePlan{}, r.Machines...)
	slices.SortFunc(plans, func(a, b MachinePlan) int { return strings.Compare(a.ID, b.ID) })
	for i := range plans {
		if !machineGraphPlan(facts, &plans[i]) {
			return nil, facts.err
		}
	}
	if facts.err != nil {
		return nil, facts.err
	}
	return facts, nil
}

// machineGraphPlan admits one plan's facts. It stops at the first refused
// admission: the From x Targets expansion of a wide conservative transition is
// quadratic in the state count, so continuing past the work limit would do
// unbounded work for a view that is already refused.
func machineGraphPlan(facts *graphFacts, plan *MachinePlan) bool {
	// The node's fixed copies (its facts, three values and each bound
	// function's record and contract) are sized once. They are a constant per
	// plan and per function, within what the plan's analysis charged for its
	// declaration (machineplan DeclarationUnits) and its functions.
	span := Span(plan.Span)
	functions := make([]GraphMachineFunction, 0, len(plan.Functions))
	values := make([]ValueType, 0, 3+len(plan.Functions))
	values = append(values, plan.State.Type, plan.Event.Type, plan.Output)
	for _, f := range plan.Functions {
		functions = append(functions, GraphMachineFunction{Role: f.Role, Name: f.Name, Identity: f.Identity, Kind: f.Kind, Failures: f.Failures, Services: f.Services})
		values = append(values, f.Contract)
	}
	if facts.addNode(GraphViewNode{ID: plan.ID, Label: plan.Name, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "machine", Name: plan.Name, Span: &span, Machine: &GraphMachineFacts{
		State: plan.State, Event: plan.Event, Output: plan.Output.Type, Initial: plan.Initial.Variant, Functions: functions,
		TerminalFailures: plan.TerminalFailures, Requirements: plan.Requirements, Evidence: plan.Evidence,
	}}}}, dependencyContract{}, values...) == nil {
		return false
	}
	for _, state := range plan.States {
		id, ok := machineGraphID(facts, "machine-state", plan.ID, state.Variant)
		if !ok {
			return false
		}
		if facts.addNode(GraphViewNode{ID: id, Label: state.Variant, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "machine-state", Name: state.Variant, MachineState: &GraphMachineStateFacts{Variant: state.Variant, Initial: state.Initial, Enterable: state.Enterable}}}}, dependencyContract{}) == nil {
			return false
		}
		edgeID, ok := machineGraphID(facts, "edge", "contains", plan.ID, id)
		if !ok || !facts.addEdge(GraphViewEdge{ID: edgeID, SourceID: plan.ID, TargetID: id, Label: "contains", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "contains", Plan: plan.ID}}}) {
			return false
		}
	}
	initialSpan := Span(plan.Initial.Span)
	initial, ok := machineGraphID(facts, "machine-state", plan.ID, plan.Initial.Variant)
	if !ok {
		return false
	}
	initialEdge, ok := machineGraphID(facts, "edge", "initial", plan.ID, initial)
	if !ok || !facts.addEdge(GraphViewEdge{ID: initialEdge, SourceID: plan.ID, TargetID: initial, Label: "initial", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "initial", Span: &initialSpan, Plan: plan.ID}}}) {
		return false
	}
	output, ok := machineGraphID(facts, "machine-output", plan.ID)
	if !ok {
		return false
	}
	if plan.Terminal && facts.addNode(GraphViewNode{ID: output, Label: "output", Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "machine-output", Name: plan.Name}}}, dependencyContract{}) == nil {
		return false
	}
	for _, t := range plan.Transitions {
		at := Span(t.Span)
		transition := &GraphMachineTransitionFacts{Transition: t.ID, Role: t.Role, On: t.On, Precision: t.Precision, Guarded: t.Guarded, Via: t.Via}
		labelBytes := len(t.Decision) + 2
		for _, on := range t.On {
			labelBytes += len(on) + 3
		}
		if !facts.spend(machineTextUnits(labelBytes)) {
			return false
		}
		label := t.Decision + ": " + strings.Join(t.On, " | ")
		edge := func(relation, source, target string) bool {
			id, ok := machineGraphID(facts, "edge", relation, source, target, t.ID)
			return ok && facts.addEdge(GraphViewEdge{ID: id, SourceID: source, TargetID: target, Label: label, Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: relation, Span: &at, Plan: plan.ID, Transition: transition}}})
		}
		for _, from := range t.From {
			source, ok := machineGraphID(facts, "machine-state", plan.ID, from)
			if !ok {
				return false
			}
			switch t.Decision {
			case machineDecisionIgnore, machineDecisionReject:
				if !edge(t.Decision, source, source) {
					return false
				}
			case machineDecisionDone:
				if !edge(t.Decision, source, output) {
					return false
				}
			default:
				for _, target := range t.Targets {
					id, ok := machineGraphID(facts, "machine-state", plan.ID, target)
					if !ok || !edge(t.Decision, source, id) {
						return false
					}
				}
				if t.Decision == machineDecisionAny && !edge(t.Decision, source, output) {
					return false
				}
			}
		}
	}
	return true
}
