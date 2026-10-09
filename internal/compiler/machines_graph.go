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

func machineStateNodeID(plan *MachinePlan, variant string) string {
	return graphTupleID("machine-state", plan.ID, variant)
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
	span := plan.Span
	functions := []GraphMachineFunction{}
	values := []ValueType{plan.State.Type, plan.Event.Type, plan.Output}
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
		id := machineStateNodeID(plan, state.Variant)
		if facts.addNode(GraphViewNode{ID: id, Label: state.Variant, Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "machine-state", Name: state.Variant, MachineState: &GraphMachineStateFacts{Variant: state.Variant, Initial: state.Initial, Enterable: state.Enterable}}}}, dependencyContract{}) == nil {
			return false
		}
		if !facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "contains", plan.ID, id), SourceID: plan.ID, TargetID: id, Label: "contains", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "contains", Plan: plan.ID}}}) {
			return false
		}
	}
	initialSpan := plan.Initial.Span
	initial := machineStateNodeID(plan, plan.Initial.Variant)
	if !facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", "initial", plan.ID, initial), SourceID: plan.ID, TargetID: initial, Label: "initial", Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: "initial", Span: &initialSpan, Plan: plan.ID}}}) {
		return false
	}
	output := graphTupleID("machine-output", plan.ID)
	if plan.Terminal && facts.addNode(GraphViewNode{ID: output, Label: "output", Data: GraphViewNodeData{Effra: GraphNodeFacts{Kind: "machine-output", Name: plan.Name}}}, dependencyContract{}) == nil {
		return false
	}
	for _, t := range plan.Transitions {
		at := t.Span
		transition := &GraphMachineTransitionFacts{Transition: t.ID, Role: t.Role, On: t.On, Precision: t.Precision, Guarded: t.Guarded, Via: t.Via}
		label := t.Decision + ": " + strings.Join(t.On, " | ")
		edge := func(relation, source, target string) bool {
			return facts.addEdge(GraphViewEdge{ID: graphTupleID("edge", relation, source, target, t.ID), SourceID: source, TargetID: target, Label: label, Data: GraphViewEdgeData{Effra: GraphEdgeFacts{Relation: relation, Span: &at, Plan: plan.ID, Transition: transition}}})
		}
		for _, from := range t.From {
			source := machineStateNodeID(plan, from)
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
					if !edge(t.Decision, source, machineStateNodeID(plan, target)) {
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
