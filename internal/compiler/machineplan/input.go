package machineplan

// The analysis input. The checker builds it before analysis starts, as
// ordinary checker work under the checker's own limits, and analysis only
// reads it. Nothing here refers back to the checker: function bodies are
// lowered to the expression shape the walk reads, and every type the analysis
// can name is in Types with its node, and, where analysis asks for them, its
// layout and display. A layout, display or projection the checker did not
// prepare (because preparing it would have exceeded what analysis could
// afford) is refused with EF142 when analysis reaches it, never guessed.

import "effra.local/prototype/internal/compiler/machineplan/shared"

// Row is a failure or service row: the checker's interned labels, shared
// read-only. Analysis can read and copy a row but cannot write it: the field
// is unexported, and the package imports neither reflect nor unsafe
// (boundary_test.go).
type Row = shared.Row

// TypeRef is the checker's canonical type ID. 0 is invalid. Analysis compares
// refs and looks them up in Types; it never creates one.
type TypeRef uint32

// Input is one module's machine declarations and everything their analysis
// reads.
type Input[V any] struct {
	// Module is the current module identity used in plan and data IDs.
	Module   string
	Machines []*Machine
	Types    Types
	// Data holds the projections of state, event and output types, and
	// Contracts those of bound functions.
	Data      map[TypeRef]Projection[V]
	Contracts map[*Function]Projection[V]
	// PlanBytes is the size of a Plan[V], which holds V by value. Analysis
	// imports no reflect or unsafe, so the caller, which knows V, measures it;
	// each declaration's fixed charge (DeclarationUnits) includes it.
	PlanBytes int
}

// Machine is one declaration.
type Machine struct {
	Name     string
	Span     Span
	Bindings []*Binding
	// Initial is the initial binding's value as the checker checked it before
	// analysis.
	Initial InitialValue
}

// Binding is one `initial`, `step`, `enter` or `complete` line in source
// order. Initial carries a value; the others name a module function, which
// Function resolves when it is a function of this module.
type Binding struct {
	Kind     string
	Span     Span
	Name     string
	NameSpan Span
	Value    *Expr
	Function *Function
}

// InitialValue is the checked initial expression's type facts.
type InitialValue struct {
	Effect   bool
	Value    TypeRef
	Result   TypeRef
	Failures Row
	Services Row
}

// Function is a module function the walk can read: a bound function or a
// helper reachable from one.
type Function struct {
	Name     string
	Identity string
	Span     Span
	// Generic is set when the function declares row or type parameters.
	Generic bool
	Effect  bool
	Params  []Param
	Return  TypeRef
	Body    *Block
	// Failures and Services are the function's failure and service rows,
	// normalized, for an effect function.
	Failures Row
	Services Row
}

type Param struct {
	Type    TypeRef
	Span    Span
	Source  string // the parameter's type as written
	Binding *Local
}

// Local is a local binder; analysis compares binders by identity.
type Local struct{ _ byte }

type Block struct {
	Statements []*Statement
}

type Statement struct {
	Kind    string
	Value   *Expr
	Payload *Expr
	Binding *Local
}

// Expr is a checked expression as the walk reads it.
type Expr struct {
	Kind string
	Name string
	Text string
	Span Span
	Left *Expr
	// Right is kept only so Children visits what the checker's child walk
	// visits.
	Right              *Expr
	Args               []*Expr
	ArgumentParameters []int
	Fields             []FieldValue
	Arms               []*Arm
	Then, Else         *Block
	// Binding is the local binder a name resolves to.
	Binding *Local
	// Resolved is set when the checker resolved a function for the
	// expression; Function is that function when it is a module function
	// with a body.
	Resolved bool
	Function *Function
	// StepTemplate is set when the expression constructs the bundled Step.
	StepTemplate bool
	// Value is the checked value's type.
	Value TypeRef
}

type FieldValue struct {
	Name  string
	Value *Expr
}

// Arm holds one pattern cell per match subject; a cell lists the variant
// names of its alternatives.
type Arm struct {
	Patterns [][]string
	Body     *Block
}

// Children visits each expression-valued child once, in the checker's child
// order: Left, Right, Args and, except for a call with arguments, Fields.
func (e *Expr) Children(visit func(*Expr)) {
	if e == nil {
		return
	}
	if e.Left != nil {
		visit(e.Left)
	}
	if e.Right != nil {
		visit(e.Right)
	}
	for _, argument := range e.Args {
		if argument != nil {
			visit(argument)
		}
	}
	if e.Kind == "call" && len(e.Args) > 0 {
		return
	}
	for _, field := range e.Fields {
		if field.Value != nil {
			visit(field.Value)
		}
	}
}

// Enum is a nominal enum or enum template.
type Enum struct {
	Name     string
	Variants []string
	Generic  bool
	// Qualifier is the declaration identity published in data refs; the
	// checker prepares it for state and event enums.
	Qualifier string
}

// Types is the prepared type table.
type Types struct {
	Nodes     map[TypeRef]*TypeNode
	Templates map[string]*Template
	Layouts   map[TypeRef]*Layout
	Displays  map[TypeRef]string
}

// TypeNode is one canonical type node with its row labels.
type TypeNode struct {
	Kind        string
	Name        string
	Declaration string
	Mode        string
	Args        []TypeRef
	Result      TypeRef
	Failures    Row
	Services    Row
}

// Template is a generic data declaration: its parameters and declared field
// types (0 where the checker left a field unresolved).
type Template struct {
	Params   []TemplateParam
	Fields   []TypeRef
	Variants [][]TypeRef
}

type TemplateParam struct {
	Type bool
	// Constraint is the number of callable-constraint parameters, or -1.
	Constraint int
}

// Layout is the resolved fields or variants of one data type.
type Layout struct {
	Enum     *Enum
	Fields   []Field
	Variants []Variant
	Record   bool
	Sum      bool
}

type Field struct {
	Name string
	Type TypeRef
}

type Variant struct {
	Name   string
	Fields []Field
}

// Projection is the checker's compact projection of one checked value.
// Published is false when the value's contract or result has no public ID, or
// its success type displays by building text; analysis then refuses rather
// than project it.
type Projection[V any] struct {
	IdentityBytes int
	Published     bool
	Value         V
}
