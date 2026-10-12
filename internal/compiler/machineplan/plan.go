// Package machineplan analyses checked machine declarations into canonical,
// provider-independent machine plans.
//
// A `machine` declaration binds an initial state and ordinary named step,
// entry and completion functions. Analysis owns three things no library could
// (docs/specs/state-machines.md#construct-admission-gate):
//
//   - the static `stay` same-tag rule (EF139), keyed on decision syntax
//     (`enter`, `stay`, `ignore`, `reject`, `done` over the predeclared
//     decision<S, O>) and on binder identity. The declaration names the
//     current state: the first parameter of step and complete;
//   - the nominal home of the step/completion rows; and
//   - the plan identity that tooling projects through a bounded,
//     non-executing walk of the bound functions.
//
// Behavior stays in the functions. Ordinary checking already proves match
// exhaustiveness, purity of pure steps and row preservation through helpers.
// Capabilities that need later units are refused with EF141 rather than passed
// through.
//
// The package is the analysis phase of three. The checker prepares an
// immutable Input (checked initials, lowered bodies, resolved and interned
// types, layouts, displays and projections), Analyse runs over that Input
// alone, and the checker commits the returned plans and diagnostics. The
// package cannot import the checker, so the Go compiler enforces that analysis
// never reaches the checker's arena, intern maps or shared diagnostics.
package machineplan

const (
	StayCode      = "EF139"
	ContractCode  = "EF140"
	AdmissionCode = "EF141"
	BudgetCode    = "EF142"
)

// Binding kinds, as the parser spells them.
const (
	BindInitial  = "initial"
	BindStep     = "step"
	BindEnter    = "enter"
	BindComplete = "complete"
)

const (
	MaxDeclarations = 256
	MaxHelperDepth  = 4
	// MaxWork bounds one declaration's analysis: visited expressions, `stay`
	// argument paths, helper bodies and the text and lists it builds.
	// Exhausting it refuses the plan.
	MaxWork        = 1 << 16
	MaxTransitions = 4096
	// MaxModuleWork bounds all declarations of a module together, so many
	// declarations over one wide enum cannot multiply plan output.
	MaxModuleWork = 1 << 20
)

// Decisions as published in plans and graphs: each is the source word of its
// decision form, so source, plan and graph use one word per concept. "any"
// is a conservative decision produced by a decision value the bounded walk
// cannot see into.
const (
	DecisionEnter  = "enter"
	DecisionStay   = "stay"
	DecisionIgnore = "ignore"
	DecisionReject = "reject"
	DecisionDone   = "done"
	DecisionAny    = "any"
)

// Target precision of a transition. Literal targets are constructor tags the
// walk saw; current targets re-enter or keep the narrowed current state;
// conservative targets admit every state because the value is dynamic.
const (
	TargetLiteral      = "literal"
	TargetCurrent      = "current"
	TargetConservative = "conservative"
)

const Evidence = "checked-static-machine-plan"

// Limitations are published with every plan and machine graph view.
var Limitations = []string{
	"possible edges from a bounded local walk; not reachability, liveness or current enabledness",
	"inspection never executes a step, entry or completion function",
	"entry work, spawn, actor handles and runtime snapshots are not admitted in this profile",
	"flat states only; hierarchy, parallel regions, history and delayed transitions are unsupported",
	"transition IDs (declaration ordinal and index) and spans are scoped to the semantic revision",
}

// Span is a source range, UTF-8 byte based and scoped to a semantic revision.
// It has the checker's Span layout, so the two convert directly.
type Span struct {
	Offset int `json:"offset"`
	Length int `json:"length"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Diagnostic is one analysis finding, returned as a value. The checker
// appends it to its own diagnostics after analysis, in order.
type Diagnostic struct {
	Code    string
	Message string
	Span    Span
}

// Plan is the canonical, provider-independent fact set of one checked
// machine declaration. It is static: it never executes a step, entry or
// completion function, and it claims no reachability, liveness or current
// enabledness. Transitions are possible edges from a bounded local walk. V is
// the checker's projected value type, which analysis copies but never reads.
type Plan[V any] struct {
	ID string `json:"id"`
	// Ordinal is the declaration's position in its module; it prefixes the
	// plan's transition IDs.
	Ordinal          int                `json:"ordinal"`
	Name             string             `json:"name"`
	Span             Span               `json:"span"`
	State            DataRef[V]         `json:"state"`
	Event            DataRef[V]         `json:"event"`
	Output           V                  `json:"output"`
	Initial          Initial            `json:"initial"`
	Functions        []BoundFunction[V] `json:"functions"`
	Transitions      []Transition       `json:"transitions"`
	States           []StateFacts       `json:"states"`
	TerminalFailures []RowContribution  `json:"terminalFailures"`
	Requirements     []RowContribution  `json:"requirements"`
	Terminal         bool               `json:"terminal"`
	Evidence         string             `json:"evidence"`
	Limitations      []string           `json:"limitations"`
}

// DataRef names a nominal state, event or outcome enum.
type DataRef[V any] struct {
	Name     string   `json:"name"`
	Identity string   `json:"identity"`
	Variants []string `json:"variants"`
	Type     V        `json:"type"`
}

type Initial struct {
	Variant string `json:"variant"`
	Span    Span   `json:"span"`
}

// BoundFunction is one bound function and its checked contract.
type BoundFunction[V any] struct {
	Role     string   `json:"role"`
	Name     string   `json:"name"`
	Identity string   `json:"identity"`
	Span     Span     `json:"span"`
	Kind     string   `json:"kind"`
	Failures []string `json:"failures"`
	Services []string `json:"requirements"`
	Contract V        `json:"contract"`
}

// Transition is one possible decision. From and On are the state and trigger
// variants for which the decision is visible (match narrowing), not an
// expansion of every history. Ignore and Reject keep the state; Done publishes
// output. Guarded marks a decision beneath a data-dependent condition; in an
// effect function that condition may be an effectful decision.
type Transition struct {
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

// StateFacts summarizes one state. Enterable is false only when no possible
// edge path from the initial state reaches it in this conservative graph,
// which over-approximates edges; true never proves reachability. A state
// restored from a snapshot can still start anywhere.
type StateFacts struct {
	Variant   string `json:"variant"`
	Initial   bool   `json:"initial"`
	Enterable bool   `json:"enterable"`
}

// RowContribution is one failure or requirement and the bound roles
// contributing it. Spawn charging is a later actor-runtime obligation.
type RowContribution struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}
