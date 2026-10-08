// Package lint is the public Go SDK for Effra custom lint rules.
//
// A rule reads a versioned, read-only Snapshot of canonical compiler facts
// and reports findings. Facts carry resolved declaration identities, nominal
// failure and requirement rows, callable identities and original source
// spans; they never ask a rule to reconstruct imports, shadowing or effect
// contracts from identifier spelling. The runner, not the rule, owns
// configuration, severity, ordering, limits and projection to diagnostics.
//
// This package deliberately imports nothing from the compiler: a rule pack
// depends on the fact schema, not on checker internals.
package lint

import "slices"

// FactSchemaName and FactSchemaVersion identify the snapshot wire contract.
// Additive optional fields keep the version; any change in the meaning or
// shape of an existing field increments it.
const (
	FactSchemaName    = "effra.lint.facts"
	FactSchemaVersion = 1
)

// Family names one independently available group of facts. A rule declares
// the families it requires; a family absent from Snapshot.Families is
// unavailable and the rule is skipped rather than passed.
type Family string

const (
	// FamilyDeclarations: services, providers and failures with identities.
	FamilyDeclarations Family = "declarations"
	// FamilyCallables: source functions, service operations and provider
	// methods with declared and checked body rows plus row contributions.
	FamilyCallables Family = "callables"
	// FamilyCalls: resolved call edges from source callables.
	FamilyCalls Family = "calls"
	// FamilyProvisions: provision edges, direct or through a layer plan, with
	// the resolved provider identity.
	FamilyProvisions Family = "provisions"
	// FamilyBindings: checked lexical bindings and their resolved uses.
	FamilyBindings Family = "bindings"
	// FamilyImports: Go import declarations and whether a checked foreign
	// call uses them.
	FamilyImports Family = "imports"
)

// Families lists every family of FactSchemaVersion in canonical order.
func Families() []Family {
	return []Family{FamilyDeclarations, FamilyCallables, FamilyCalls, FamilyProvisions, FamilyBindings, FamilyImports}
}

func knownFamily(family Family) bool { return slices.Contains(Families(), family) }

// Unavailable reasons. A rule requiring an unavailable family is skipped
// with the family's reason.
const (
	ReasonUncheckedSource = "unchecked-source"
	ReasonFactBudget      = "fact-budget-exhausted"
	ReasonNotRequested    = "not-requested"
)

// Span is an original-source location: a UTF-8 byte offset and length plus
// the 1-based line and column of its start, as reported by the compiler.
type Span struct {
	Offset int `json:"offset"`
	Length int `json:"length"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Schema identifies the fact schema of a snapshot.
type Schema struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// Semantic is the compiler's qualified semantic snapshot. Revision is the
// source/import digest; Producer and ReuseScope identify the analysis
// producer. Lint options and rule packs are deliberately not part of it: they
// qualify an AnalysisIdentity, not the program's types.
type Semantic struct {
	SchemaVersion int    `json:"schemaVersion"`
	Revision      string `json:"revision"`
	Target        string `json:"target"`
	Producer      string `json:"producer,omitempty"`
	ReuseScope    string `json:"reuseScope,omitempty"`
}

// Source describes the analysed text. Bytes bounds every finding span.
type Source struct {
	URI   string `json:"uri,omitempty"`
	Bytes int    `json:"bytes"`
}

// UnavailableFamily explains why a family was not produced.
type UnavailableFamily struct {
	Family Family `json:"family"`
	Reason string `json:"reason"`
}

// Snapshot is one read-only fact projection of a compiled source. Every
// slice is deterministically ordered; equal sources compiled by equal
// producers yield byte-identical JSON.
type Snapshot struct {
	Schema      Schema              `json:"schema"`
	Semantic    Semantic            `json:"semantic"`
	Source      Source              `json:"source"`
	Checked     bool                `json:"checked"`
	Families    []Family            `json:"families"`
	Unavailable []UnavailableFamily `json:"unavailable,omitempty"`

	Services   []Service   `json:"services,omitempty"`
	Providers  []Provider  `json:"providers,omitempty"`
	Failures   []Failure   `json:"failures,omitempty"`
	Callables  []Callable  `json:"callables,omitempty"`
	Calls      []Call      `json:"calls,omitempty"`
	Provisions []Provision `json:"provisions,omitempty"`
	Bindings   []Binding   `json:"bindings,omitempty"`
	Imports    []Import    `json:"imports,omitempty"`
}

// Service is a nominal service declaration. Builtin declarations have no
// source span.
type Service struct {
	Identity   string   `json:"identity"`
	Name       string   `json:"name"`
	Builtin    bool     `json:"builtin,omitempty"`
	Span       *Span    `json:"span,omitempty"`
	Operations []string `json:"operations,omitempty"`
}

// Provider is a target provider declaration implementing Service. Requires
// lists construction-captured service identities; Configured reports that
// construction takes configuration arguments.
type Provider struct {
	Identity   string   `json:"identity"`
	Name       string   `json:"name"`
	Service    string   `json:"service"`
	Builtin    bool     `json:"builtin,omitempty"`
	Configured bool     `json:"configured,omitempty"`
	Requires   []string `json:"requires,omitempty"`
	Span       *Span    `json:"span,omitempty"`
}

// Failure is a nominal failure declaration; rows refer to its Identity.
type Failure struct {
	Identity string `json:"identity"`
	Name     string `json:"name"`
	Builtin  bool   `json:"builtin,omitempty"`
	Span     *Span  `json:"span,omitempty"`
}

// Rows is a checked failure/requirement row pair. Members are nominal
// failure and service identities; Parameters are abstract row parameter
// identities of a row-polymorphic declaration.
type Rows struct {
	Failures     []string `json:"failures,omitempty"`
	Requirements []string `json:"requirements,omitempty"`
	Parameters   []string `json:"parameters,omitempty"`
}

// Callable kinds.
const (
	CallableFunction       = "function"
	CallableOperation      = "operation"
	CallableProviderMethod = "provider-method"
)

// Callable is a checked source callable. Declared is its contract; Body is
// the checked row of its body after recovery and provision, including rows
// forwarded from helpers. Owner is the service or provider identity of an
// operation or method.
type Callable struct {
	Identity      string         `json:"identity"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	Owner         string         `json:"owner,omitempty"`
	Effect        bool           `json:"effect"`
	Span          Span           `json:"span"`
	Extent        Span           `json:"extent"`
	Declared      Rows           `json:"declared"`
	Body          *Rows          `json:"body,omitempty"`
	Contributions []Contribution `json:"contributions,omitempty"`
}

// Contribution records why a body row contains members: a failure raised or
// executed, a requirement executed, an owned child, or a layer provision.
type Contribution struct {
	Kind    string   `json:"kind"`
	Members []string `json:"members"`
	Span    Span     `json:"span"`
}

// Call kinds.
const (
	CallFunction            = "function"
	CallOperation           = "operation"
	CallForeign             = "foreign"
	CallDynamic             = "dynamic"
	CallFiber               = "fiber"
	CallProviderConstructor = "provider-constructor"
)

// Call is one checked application inside Caller. Callee is the resolved
// identity; a dynamic call has none because its target is a callable value.
type Call struct {
	Caller string `json:"caller"`
	Callee string `json:"callee,omitempty"`
	Kind   string `json:"kind"`
	Span   Span   `json:"span"`
}

// Provision kinds.
const (
	ProvisionDirect = "provide"
	ProvisionLayer  = "layer"
)

// Provision is one provision edge inside Callable. Provider is the resolved
// provider declaration identity, whatever local value spelled it. A layer
// provision has one edge per selected layer node, with Layer and the node's
// Selection span. Receiver is the requirement row of the effect receiving
// a direct provision.
type Provision struct {
	Callable  string   `json:"callable"`
	Kind      string   `json:"kind"`
	Service   string   `json:"service"`
	Provider  string   `json:"provider"`
	Layer     string   `json:"layer,omitempty"`
	Span      Span     `json:"span"`
	Selection *Span    `json:"selection,omitempty"`
	Receiver  []string `json:"receiver,omitempty"`
}

// Binding is one checked lexical binding. Uses are the original spans of
// expressions the checker resolved to this binding.
type Binding struct {
	Identity string `json:"identity"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Callable string `json:"callable"`
	Span     Span   `json:"span"`
	Effect   bool   `json:"effect"`
	Uses     []Span `json:"uses,omitempty"`
}

// Import is one Go import declaration.
type Import struct {
	Alias string `json:"alias"`
	Path  string `json:"path"`
	Used  bool   `json:"used"`
	Span  Span   `json:"span"`
}

// Has reports whether family was produced.
func (s *Snapshot) Has(family Family) bool { return slices.Contains(s.Families, family) }

// UnavailableReason reports why family is unavailable, or "" when present.
func (s *Snapshot) UnavailableReason(family Family) string {
	if s.Has(family) {
		return ""
	}
	for _, unavailable := range s.Unavailable {
		if unavailable.Family == family {
			return unavailable.Reason
		}
	}
	return ReasonNotRequested
}

// Callable returns the callable with identity.
func (s *Snapshot) Callable(identity string) *Callable {
	for i := range s.Callables {
		if s.Callables[i].Identity == identity {
			return &s.Callables[i]
		}
	}
	return nil
}

// Service returns the service with identity.
func (s *Snapshot) Service(identity string) *Service {
	for i := range s.Services {
		if s.Services[i].Identity == identity {
			return &s.Services[i]
		}
	}
	return nil
}

// Declaration-name lookups select a declaration a user configured by name.
// Top-level names are unique declarations, so these never interpret a
// use-site spelling: rules then compare resolved identities only.

// FunctionNamed returns the top-level function declared as name.
func (s *Snapshot) FunctionNamed(name string) *Callable {
	for i := range s.Callables {
		if s.Callables[i].Kind == CallableFunction && s.Callables[i].Name == name {
			return &s.Callables[i]
		}
	}
	return nil
}

// ProviderNamed returns the provider declared as name.
func (s *Snapshot) ProviderNamed(name string) *Provider {
	for i := range s.Providers {
		if s.Providers[i].Name == name {
			return &s.Providers[i]
		}
	}
	return nil
}

// FailureNamed returns the failure declared as name.
func (s *Snapshot) FailureNamed(name string) *Failure {
	for i := range s.Failures {
		if s.Failures[i].Name == name {
			return &s.Failures[i]
		}
	}
	return nil
}
