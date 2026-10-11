package compiler

// Machine declaration checking and canonical machine plans.
//
// A `machine` declaration binds an initial state and ordinary named step,
// entry and completion functions. Checking owns three things no library could
// (docs/specs/state-machines.md#construct-admission-gate): the static Stay
// same-tag rule (EF139), the nominal home of the step/completion rows, and the
// plan identity tooling projects through a bounded, non-executing walk.
//
// The checker runs machine analysis in three phases:
//
//   - prepare (machines_prepare.go) is checker work: it checks the initial
//     values, lowers the declarations and the function bodies the walk can
//     read, and resolves and interns the types, layouts, displays and
//     projections analysis reads, into an immutable machineplan.Input;
//   - analyse (package machineplan) reads only that input and returns plans
//     and diagnostics. It cannot import the checker, so it cannot reach the
//     checker's arenas, intern maps or diagnostics; and
//   - commit appends the analysis diagnostics in order and stores the plans.
//
// Behavior stays in the functions. Ordinary checking already proves match
// exhaustiveness (every state/event pair has a policy), purity of pure steps
// and row preservation through helpers. Capabilities that need later units are
// refused with EF141 rather than passed through: entry work (typed recipe
// values, payload-aware recovery, entry ownership) and machine values (actor
// protocols, spawn and the owned runtime).

import (
	"fmt"

	"effra.local/prototype/internal/compiler/machineplan"
)

const (
	machineStayCode      = machineplan.StayCode
	machineContractCode  = machineplan.ContractCode
	machineAdmissionCode = machineplan.AdmissionCode
	machineBudgetCode    = machineplan.BudgetCode
)

const (
	maxMachineDeclarations = machineplan.MaxDeclarations
	maxMachineHelperDepth  = machineplan.MaxHelperDepth
	maxMachineWork         = machineplan.MaxWork
	maxMachineTransitions  = machineplan.MaxTransitions
	maxMachineModuleWork   = machineplan.MaxModuleWork
)

const (
	machineDecisionGo     = machineplan.DecisionGo
	machineDecisionStay   = machineplan.DecisionStay
	machineDecisionIgnore = machineplan.DecisionIgnore
	machineDecisionReject = machineplan.DecisionReject
	machineDecisionDone   = machineplan.DecisionDone
	machineDecisionAny    = machineplan.DecisionAny
)

const (
	machineTargetLiteral      = machineplan.TargetLiteral
	machineTargetCurrent      = machineplan.TargetCurrent
	machineTargetConservative = machineplan.TargetConservative
)

const machinePlanEvidence = machineplan.Evidence

var machinePlanLimitations = machineplan.Limitations

func machineTextUnits(bytes int) int { return machineplan.TextUnits(bytes) }

// MachinePlan is the canonical, provider-independent fact set of one checked
// machine declaration (machineplan.Plan), with the checker's projected value
// type.
type (
	MachinePlan            = machineplan.Plan[ValueType]
	MachineDataRef         = machineplan.DataRef[ValueType]
	MachineInitial         = machineplan.Initial
	MachineFunction        = machineplan.BoundFunction[ValueType]
	MachineTransition      = machineplan.Transition
	MachineStateFacts      = machineplan.StateFacts
	MachineRowContribution = machineplan.RowContribution
)

// machineAnalysisProbe, when a test sets it, brackets one module's machine
// analysis so the test can compare measured allocation with the units the
// analysis charged. It is nil in production.
var machineAnalysisProbe func(done bool, units int)

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

// checkMachines runs after every function body is checked, so bound bodies
// carry their checked facts. A declaration with any analysis diagnostic
// publishes no plan.
func (c *checker) checkMachines() {
	if len(c.program.Machines) > maxMachineDeclarations {
		c.diagnostic(machineBudgetCode, fmt.Sprintf("machine profile exceeds %d declarations", maxMachineDeclarations), c.program.Machines[maxMachineDeclarations].Span)
		return
	}
	c.result.Machines = []MachinePlan{}
	// An initial value is an ordinary expression: it is checked here, in the
	// checking pass that checks function bodies, before machine analysis
	// starts. Analysis reads the checked value and charges only its own work.
	initials := make([]checkedExpression, len(c.program.Machines))
	for ordinal, machine := range c.program.Machines {
		if binding := machine.binding(machineInitial); binding != nil && binding.Value != nil {
			previous := c.recordFacts
			c.recordFacts = true
			initials[ordinal] = c.expr(binding.Value, localEnv{}, false)
			c.recordFacts = previous
		}
	}
	// Prepare: checker work, outside the analysis window.
	in := c.prepareMachines(initials)
	// Analyse: the measured window reads only in.
	if machineAnalysisProbe != nil {
		machineAnalysisProbe(false, 0)
	}
	result := machineplan.Analyse(in)
	if machineAnalysisProbe != nil {
		machineAnalysisProbe(true, result.Units)
	}
	// Commit: the analysis diagnostics in report order, then the plans.
	for _, d := range result.Diagnostics {
		c.diagnostic(d.Code, d.Message, Span(d.Span))
	}
	c.result.Machines = result.Plans
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
