package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

type ValueType struct {
	Success string  `json:"success"` // compatibility projection of Type
	Type    TypeRef `json:"type"`    // canonical identity projection
	// Contract is the complete checked value identity. Type remains the
	// success/result accessor used by compatibility clients.
	Contract        TypeRef              `json:"contract,omitempty"`
	Identity        string               `json:"identity,omitempty"`
	Effect          bool                 `json:"effect"`
	Errors          []string             `json:"failures"`
	Services        []string             `json:"requirements"`
	FailureRow      string               `json:"failureRow,omitempty"`
	ServiceRow      string               `json:"serviceRow,omitempty"`
	Callable        *CallableType        `json:"callable,omitempty"`
	Application     *ApplicationIdentity `json:"application,omitempty"`
	Evaluation      EvaluationRows       `json:"-"`
	Ownership       []OwnershipFact      `json:"ownership,omitempty"`
	Captures        []OwnershipFact      `json:"captures,omitempty"`
	Child           []OwnershipFact      `json:"childOwnership,omitempty"`
	ProjectionError string               `json:"projectionError,omitempty"`
}

// EvaluationRows describe requirements incurred while evaluating an
// expression. They are intentionally separate from the deferred rows on
// ValueType: constructing a recipe is pure with respect to its recipe rows,
// while run/argument evaluation contributes to the enclosing computation.
type EvaluationRows struct {
	Failures     []string `json:"failures,omitempty"`
	Requirements []string `json:"requirements,omitempty"`
}

type CallableType struct {
	TypeParameters   []TemplateParameterView `json:"typeParameters,omitempty"`
	CallbackPolicies []CallbackPolicy        `json:"callbackPolicies,omitempty"`
	RowParameters    []RowParameter          `json:"rowParameters,omitempty"`
	ID               string                  `json:"id"`
	Signature        string                  `json:"signature"`
	Kind             string                  `json:"kind"`
	Parameters       []Param                 `json:"parameters"`
	Result           TypeRef                 `json:"result"`
	Failures         []string                `json:"failures"`
	Requirements     []string                `json:"requirements"`
	FailureRow       string                  `json:"failureRow,omitempty"`
	ServiceRow       string                  `json:"serviceRow,omitempty"`
}

// ApplicationIdentity is the checked identity of one function application.
// Arguments are canonical type identities; the full expression tree remains
// available through source spans and the checked node, rather than being
// duplicated in every inspection response.
type ApplicationIdentity struct {
	CallbackPolicies []CallbackPolicy `json:"callbackPolicies,omitempty"`
	RowArguments     []RowArgument    `json:"rowArguments,omitempty"`
	ID               string           `json:"id"`
	Callee           string           `json:"callee"`
	Arguments        []TypeRef        `json:"arguments"`
	Result           TypeRef          `json:"result"`
	// ProducedResult retains the factory's canonical return type when an
	// explicit function return contract safely re-contracts its fresh data value.
	ProducedResult *TypeRef `json:"producedResult,omitempty"`
}

type CallbackPolicy struct {
	Parameter             int      `json:"parameter"`
	Kind                  string   `json:"kind"`
	PropagateRequirements bool     `json:"propagateRequirements"`
	AbsorbedFailures      []string `json:"absorbedFailures,omitempty"`
	FailureRow            string   `json:"failureRow,omitempty"`
}

func newApplicationIdentity(callee string, arguments []TypeRef, result TypeRef, span Span) ApplicationIdentity {
	var key strings.Builder
	key.WriteString(callee)
	key.WriteByte('(')
	for _, argument := range arguments {
		key.WriteString(argument.ID)
		key.WriteByte(',')
	}
	key.WriteByte(')')
	key.WriteByte('@')
	key.WriteString(fmt.Sprint(span.Offset))
	sum := sha256.Sum256([]byte(key.String()))
	return ApplicationIdentity{ID: "application:" + hex.EncodeToString(sum[:8]), Callee: callee, Arguments: append([]TypeRef{}, arguments...), Result: result}
}

type TypeNode struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name,omitempty"`
	Declaration string   `json:"declaration,omitempty"`
	Mode        string   `json:"callableKind,omitempty"`
	Args        []string `json:"args,omitempty"`
	Result      string   `json:"result,omitempty"`
	FailureRow  string   `json:"failureRow,omitempty"`
	ServiceRow  string   `json:"serviceRow,omitempty"`
}

type RowNode struct {
	ID         string         `json:"id"`
	Labels     []string       `json:"labels,omitempty"`
	Parameters []RowParameter `json:"parameters,omitempty"`
}

type RowParameter struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Declaration string `json:"declaration"`
	Span        Span   `json:"span"`
}

type RowArgument struct {
	Parameter RowParameter `json:"parameter"`
	Row       string       `json:"row"`
}

// ownershipOwnerKind is the owner atom of a fact (design §3.1). The region
// string remains part of the inspection contract; the kind keeps an owner
// bound by a layer's executor apart from a value already owned by a lexical
// region.
//
//   - Parameter: Param(p, π), the owners of caller argument p at input path π
//     (Region "parameter:p", source π).
//   - Exec: Exec(i), owned by the executor of layer i of the enclosing
//     occurrence (see owner_layers.go).
//   - Lexical: owned by a lexical region (Region "scope:N", "invocation").
//   - Child, Timeout, Scope, Provision: Closed(k, site), owned by an owner
//     which has closed. Scope and Provision appear only on failure payloads
//     which outlived their region (design §5.4).
//   - Relation: Rel(ρ), the owners of a deferred callback result.
//   - Unknown: no proven owner; with potentialOwner it is ⊤.
type ownershipOwnerKind uint8

const (
	ownershipOwnerUnknown ownershipOwnerKind = iota
	ownershipOwnerParameter
	ownershipOwnerExec
	ownershipOwnerLexical
	ownershipOwnerChild
	ownershipOwnerTimeout
	ownershipOwnerRelation
	ownershipOwnerScope
	ownershipOwnerProvision
)

// closedOwnerKind reports the owner kinds which have completed cleanup by the
// time a value carrying them is observable.
func closedOwnerKind(kind ownershipOwnerKind) bool {
	return kind == ownershipOwnerChild || kind == ownershipOwnerTimeout || kind == ownershipOwnerScope || kind == ownershipOwnerProvision
}

// OwnershipFact is the bounded ownership evidence carried by a checked value.
// A fact is deliberately explicit about uncertainty: the compiler rejects a
// value when it proves that the value belongs to a closing scope or when
// bounded analysis retains potential ownership after exhausting its budget.
// Foreign values and summaries outside this model stay unknown; unknown can
// also represent bounded analysis that did not establish a complete proof.
type OwnershipFact struct {
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Region string `json:"region,omitempty"`
	Origin string `json:"origin,omitempty"`

	// source is the path relative to a parameter which supplied this fact. It
	// is kept out of the inspection schema because it is an implementation
	// detail of function-summary instantiation. Without it, a helper returning
	// pair.outer would have to conservatively retain pair.inner as well.
	source string
	// sourceSet distinguishes a parameter-relative root path (the valid empty
	// path) from a fact that has no parameter source.
	sourceSet bool
	ownerKind ownershipOwnerKind
	// potentialOwner marks bounded evidence which may contain an owned path that
	// the representation could not retain. It may be attached to a wildcard or
	// to a concrete path: the latter records that the selected path itself is
	// incomplete, while a wildcard records an incompleteness outside the paths
	// retained explicitly. It is internal so the public contract still reports
	// bounded evidence while escape checks diagnose exhausted analysis instead
	// of treating it as safe.
	potentialOwner bool
	// remainder marks uncertainty outside retained exact paths. Projection may
	// discharge this marker at an independently complete selected terminal;
	// wildcard alternatives which may include that terminal remain live.
	remainder bool
	// remainderExclusions records exact paths retained in the same abstract
	// alternative. A remainder covers every other path, so a join cannot use a
	// sibling fact from another alternative to discharge it.
	remainderExclusions string
	callbackRelation    *callbackResultRelation
	// exec is the level of an Exec owner: the executor of layer exec of the
	// occurrence carrying the fact. Region renders it as "exec:<level>".
	exec int
	// layer is the recipe layer whose held set a captures fact belongs to.
	// Ownership facts describe the innermost success and keep layer 0.
	layer int
	// relationEnv is the owner vector of a Relation fact: the owner bound to
	// each layer of the deferred invocation (see owner_layers.go).
	relationEnv string
}

// TypeRef is the canonical semantic identity used by checking, emission and
// inspection. Success remains a stable rendered string for existing clients;
// TypeRef prevents the compiler from growing another string-encoded type
// grammar as nominal application data is added. Kind is intentionally open:
// provider, opaque-handle and ownership wrappers can be added without
// changing the public contract shape, with Args carrying nested identities.
type TypeRef struct {
	ID          string `json:"ref,omitempty"`
	Kind        string `json:"kind"`
	Name        string `json:"name,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Declaration string `json:"declaration,omitempty"`
	// Args is a one-hop in-process compatibility view. The serialized `args`
	// field and Result.Types table carry the complete shared graph by reference,
	// so repeated children never trigger recursive DAG expansion at a boundary.
	Args       []TypeRef `json:"-"`
	ArgIDs     []string  `json:"args,omitempty"`
	Result     string    `json:"result,omitempty"`
	FailureRow string    `json:"failureRow,omitempty"`
	ServiceRow string    `json:"serviceRow,omitempty"`
}

type semanticTypeNode struct {
	ID          TypeID
	Kind        string
	Name        string
	Declaration string
	Mode        string
	Args        []TypeID
	Result      TypeID
	FailureRow  RowID
	ServiceRow  RowID
}

type TypeID uint32
type RowID uint32

const invalidTypeID TypeID = 0
const emptyRowID RowID = 0

// SemanticSchemaVersion 9 publishes recipe contracts without constructor
// parameters and per-path owner evidence (owners and evidence grade) on
// checked values.
const SemanticSchemaVersion = 9

const voidTypeName = "void"

type Contribution struct {
	Kind  string   `json:"kind"`
	Names []string `json:"names"`
	Span  Span     `json:"span"`
}
type Symbol struct {
	Name          string         `json:"name"`
	Source        string         `json:"source,omitempty"`
	Identity      string         `json:"identity,omitempty"`
	Params        []Param        `json:"parameters"`
	Contract      ValueType      `json:"contract"`
	Actual        ValueType      `json:"bodyContract"`
	Span          Span           `json:"span"`
	Contributions []Contribution `json:"contributions"`
}
type Timings struct {
	ImportMicros int64 `json:"importMicros"`
	ParseMicros  int64 `json:"parseMicros"`
	CheckMicros  int64 `json:"checkMicros"`
	TotalMicros  int64 `json:"totalMicros"`
}
type Result struct {
	producerMetadata       ProducerMetadata
	BundledInterfaces      []BundledInterfaceInfo `json:"bundledInterfaces,omitempty"`
	Sources                []SourceInfo           `json:"sources,omitempty"`
	BundledBindings        []BundledBinding       `json:"bundledBindings,omitempty"`
	ProducerIdentity       string                 `json:"producerIdentity,omitempty"`
	ModuleSum              []byte                 `json:"-"`
	Bindings               []Binding              `json:"bindings,omitempty"`
	SchemaVersion          int                    `json:"schemaVersion"`
	Revision               string                 `json:"revision"`
	Target                 string                 `json:"target"`
	Checked                bool                   `json:"checked"`
	Diagnostics            []Diagnostic           `json:"diagnostics"`
	Symbols                []Symbol               `json:"symbols"`
	Layers                 []LayerPlan            `json:"layers,omitempty"`
	Declarations           []Declaration          `json:"declarations,omitempty"`
	Codecs                 []CodecInspection      `json:"codecs,omitempty"`
	CodecPlans             []*CodecPlan           `json:"codecPlans,omitempty"`
	Types                  []TypeNode             `json:"types,omitempty"`
	Rows                   []RowNode              `json:"rows,omitempty"`
	TypeProjectionBudget   int                    `json:"typeProjectionBudget"`
	TypeProjectionLimits   ProjectionLimits       `json:"typeProjectionLimits"`
	TypeProjectionUsage    ProjectionUsage        `json:"typeProjectionUsage,omitempty"`
	TypeProjectionComplete bool                   `json:"typeProjectionComplete"`
	TypeProjectionError    string                 `json:"typeProjectionError,omitempty"`
	Timings                Timings                `json:"timings"`
	Program                *Program               `json:"-"`
	// comments are the source's comments as lexed, kept whether or not
	// the source parsed, so lint suppressions survive a syntax fault.
	comments             []Comment
	sourceBytes          int
	facts                map[*Expr]ExpressionFacts
	lexical              *lexicalFacts
	canonical            *canonicalSnapshot
	checkedProviders     map[string]*Provider
	checkedServices      map[string]*Service
	projector            *checker
	checkedSymbols       map[string]checkedSymbol
	checkedFunctions     map[*Function]checkedSymbol
	checkedProviderRoots map[*Provider]checkedExpression
	publicationRefused   bool
	publicationUsage     ProjectionUsage
}

type checkedSymbol struct {
	contract      checkedExpression
	body          checkedExpression
	declaration   *Function
	contributions []Contribution
}

type ExpressionFacts struct {
	Checked    checkedExpression
	Type       ValueType
	Evaluation EvaluationRows
	Executed   EvaluationRows
}

// checkedExpression is the checker-owned expression fact. ValueType is only
// produced from this record at public/query boundaries; expression checking
// never mutates compatibility fields to establish semantic relations.
type checkedExpression struct {
	fields     map[string]checkedExpression
	value      CheckedValue
	evaluation ExpressionEvaluation
	executed   ExpressionEvaluation
	child      []OwnershipFact
	// failures is the payload evidence of the failures of each recipe layer
	// (or of a Fiber's child); see failure_evidence.go.
	failures failureEvidence
	// callableDecl is projection metadata only. Its type and row facts are
	// never read for checking; those facts come from value's canonical node.
	callableDecl     *Function
	application      *ApplicationIdentity
	identity         string
	callableEvidence callableEvidence
	lexicalBinding   string
	// forks is the fork instances a Fiber occurrence may denote (design
	// §5.3); join and interrupt observe a child only through a Fiber which
	// denotes exactly one instance.
	forks []string
	// observes is the fork instances executing a recipe's layer 0 observes
	// before each exit that completes it: join and interrupt, kept by
	// recover, catch, orFail and service provision around them.
	observes []string
	// kills is the fork instances evaluating the expression observes before
	// its success exit; exitKills is what it observes before each of its
	// typed-failure exits. Both compose along evaluation order
	// (fork_observations.go); the enclosing block reads them.
	kills     []string
	exitKills []string
}

func (e checkedExpression) contractID() TypeID      { return e.value.contractID() }
func (e checkedExpression) valueID() TypeID         { return e.value.valueID() }
func (e checkedExpression) resultID() TypeID        { return e.value.resultID() }
func (e checkedExpression) node() *semanticTypeNode { return e.value.node() }
func (e checkedExpression) kind() checkedValueKind  { return e.value.kind() }
func (e checkedExpression) failureRow() RowID       { return e.value.failureRow() }
func (e checkedExpression) serviceRow() RowID       { return e.value.serviceRow() }
func (e checkedExpression) isEffect() bool {
	return e.kind() == checkedRecipeValue || e.kind() == checkedProviderRecipeValue
}
func (e checkedExpression) ownershipFacts() []OwnershipFact { return e.value.ownershipFacts() }
func (e checkedExpression) captureFacts() []OwnershipFact   { return e.value.captureFacts() }
func (e *checkedExpression) setOwnership(facts []OwnershipFact) {
	e.value = e.value.withOccurrenceFacts(facts, e.captureFacts())
}
func (e *checkedExpression) setCaptures(facts []OwnershipFact) {
	e.value = e.value.withOccurrenceFacts(e.ownershipFacts(), facts)
}

func (e checkedExpression) clone() checkedExpression {
	copy := e
	copy.fields = cloneFieldOccurrences(e.fields)
	copy.child = cloneFacts(e.child)
	copy.failures = cloneFailures(e.failures)
	if e.application != nil {
		application := *e.application
		application.Arguments = append([]TypeRef{}, e.application.Arguments...)
		application.Result.Args = append([]TypeRef{}, e.application.Result.Args...)
		application.Result.ArgIDs = append([]string{}, e.application.Result.ArgIDs...)
		if e.application.ProducedResult != nil {
			produced := *e.application.ProducedResult
			produced.Args = append([]TypeRef{}, produced.Args...)
			produced.ArgIDs = append([]string{}, produced.ArgIDs...)
			application.ProducedResult = &produced
		}
		copy.application = &application
	}
	return copy
}

type checker struct {
	// handlerOperand is the operand of the recover being checked: a named
	// function there is instantiated by handlerResult, so its pending
	// children do not reach a callable contract.
	handlerOperand          *Expr
	program                 *Program
	result                  *Result
	functions               map[string]*Function
	services                map[string]*Service
	providers               map[string]*Provider
	layers                  map[string]*LayerPlan
	layerBudget             *layerAssemblyBudget
	layerProviders          map[*LayerEntry]*Provider
	records                 map[string]*Record
	enums                   map[string]*Enum
	errors                  map[string]*ErrorDecl
	constants               map[string]*Constant
	constantsByModule       map[string]map[string]*Constant
	typeIntern              map[string]*semanticTypeNode
	typeNodes               []*semanticTypeNode
	rowIntern               map[string]RowID
	rows                    []RowNode
	values                  *checkedValueArena
	nextTypeID              TypeID
	nextRowID               RowID
	recordFacts             bool
	typePublicIDs           map[TypeID]string
	typePublicToID          map[string]TypeID
	declarationFingerprints map[string]string
	reasons                 []Contribution
	region                  string
	// reportedOwnership holds the EF123 roots already reported (see
	// reportOwnership); reportingFunction scopes roots without a source
	// offset to the function being checked.
	reportedOwnership map[reportedOwnershipRoot]bool
	// ownershipReports lists each emitted EF123 with the roots it reported
	// first, in emission order.
	ownershipReports    []ownershipReport
	reportingFunction   *Function
	suppressDiagnostics bool
	// source is the checked text, used to place suggested edits.
	source            string
	publicationBytes  int
	rowContext        map[string]RowParameter
	rowDefinitions    map[string]RowParameter
	callbackRelations map[string]*callbackResultRelation
	functionModule    string
	admittedSummaries map[string]interfaceSummary
	templates         map[string]*Record
	typeContext       map[string]TemplateParameter
	variableOwners    map[string]TemplateParameter
	lexicalOwner      *Function
	host              *hostState
	// hostCallee is the member expression currently checked as a callee, so
	// a member of a host value there selects a Go method.
	hostCallee *Expr
	// nonzero counts, per local binding, the enclosing if branches whose
	// condition proves the binding is not zero. Bindings are immutable, so
	// the proof holds for every use inside the branch (see divisorProof).
	nonzero map[*localBinding]int
}

const maxTypeProjectionNodes = 4096

func (c *checker) checkedData(name string) checkedExpression {
	return c.checkedDataID(c.canonicalRef(typeRef(name)), nil, nil)
}

func (c *checker) checkedDataID(id TypeID, ownership, captures []OwnershipFact) checkedExpression {
	if id == invalidTypeID {
		id = c.canonicalRef(typeRef("invalid"))
	}
	node := c.node(id)
	if node != nil && node.Kind == "provider" {
		return checkedExpression{value: c.values.provider(id, ownership, captures)}
	}
	if node != nil && (node.Kind == "callable" || node.Kind == "recipe" || node.Kind == "providerRecipe" || node.Kind == "fiber") {
		return checkedExpression{value: c.values.occurrence(id, ownership, captures)}
	}
	return checkedExpression{value: c.values.data(id, ownership, captures)}
}

func (c *checker) checkedProvider(p *Provider, recipe bool) checkedExpression {
	providerID := c.canonicalRef(providerTypeRef(p))
	if recipe {
		failure := c.internRow(nil)
		service := c.internRow(p.Services)
		return checkedExpression{value: c.values.providerRecipe(providerID, failure, service, nil, nil)}
	}
	return checkedExpression{value: c.values.provider(providerID, nil, nil)}
}

func (c *checker) checkedFunction(f *Function, declaration, recipe bool) checkedExpression {
	result := f.returnID
	if result == invalidTypeID {
		result = c.canonicalRef(typeRef(f.Return))
	}
	if result == invalidTypeID {
		result = c.canonicalRef(typeRef("invalid"))
	}
	parameters := c.functionParameterTypeIDs(f)
	kind := checkedPureCallable
	if f.Effect {
		kind = checkedEffectCallable
	}
	failure, service := emptyRowID, emptyRowID
	if f.Effect {
		if f.signatureChecked {
			failure, service = f.failureID, f.serviceID
		} else {
			failure = c.internRow(f.Errors)
			service = c.internRow(f.Services)
		}
	}
	var checked CheckedValue
	switch {
	case declaration:
		checked = c.values.callable(result, parameters, kind, failure, service, f.Ownership, f.Captures)
	case recipe && f.Effect:
		checked = c.values.recipe(result, kind, failure, service, f.Ownership, f.Captures)
	default:
		checked = c.values.occurrence(result, f.Ownership, f.Captures)
	}
	resultExpression := checkedExpression{value: checked, identity: f.Identity}
	if declaration {
		resultExpression.callableDecl = f
		resultExpression.callableEvidence = namedCallableEvidence(f)
	}
	return resultExpression
}

func (c *checker) functionParameterTypeIDs(f *Function) []TypeID {
	parameters := make([]TypeID, 0, len(f.Params))
	for i := range f.Params {
		id := f.Params[i].typeID
		if id == invalidTypeID {
			ref := f.Params[i].TypeRef
			if ref.Kind == "" {
				ref = typeRef(f.Params[i].Type)
			}
			id = c.canonicalRef(ref)
		}
		if id == invalidTypeID {
			id = c.canonicalRef(typeRef("invalid"))
		}
		parameters = append(parameters, id)
	}
	return parameters
}

func (c *checker) ownershipPaths(typeName, prefix string, seen map[string]bool) []string {
	return c.ownershipPathsID(c.canonicalRef(typeRef(typeName)), prefix)
}

func (c *checker) ownershipPathsID(root TypeID, prefix string) []string {
	// A shared record graph can have exponentially many leaf paths. First memoize
	// whether each structural type can contain a managed handle; then enumerate
	// only handle-bearing branches under a total structural visit budget. The
	// wildcard keeps the fact that a handle exists somewhere in the shape when
	// the bounded representation cannot retain every path.
	const maxPaths = 64
	const maxVisits = 256
	const maxContainsVisits = 256
	containsMemo := map[TypeID]bool{}
	containsVisiting := map[TypeID]bool{}
	containsVisits := 0
	var containsHandle func(TypeID) bool
	containsHandle = func(name TypeID) bool {
		n := c.node(name)
		if n == nil || n.Kind == "type-variable" || n.Name == "File" || n.Kind == "fiber" || n.Kind == "recipe" {
			// A stored recipe may hold managed handles captured from its
			// arguments; it is an opaque leaf for ownership paths.
			return true
		}
		if known, ok := containsMemo[name]; ok {
			return known
		}
		if containsVisits >= maxContainsVisits {
			// A budget overflow means the checker cannot prove that this shape is
			// handle-free. Treat it as handle-bearing and let the path walk add a
			// bounded wildcard instead of spending unbounded time on structure.
			containsMemo[name] = true
			return true
		}
		containsVisits++
		if containsVisiting[name] {
			return false
		}
		containsVisiting[name] = true
		found := false
		c.walkDataFields(name, func(_ string, field Field) {
			if !found {
				found = containsHandle(field.typeID)
			}
		})
		delete(containsVisiting, name)
		containsMemo[name] = found
		return found
	}
	if !containsHandle(root) {
		return nil
	}
	paths := make([]string, 0, maxPaths)
	truncated := false
	visits := 0
	seen := map[TypeID]bool{}
	var visit func(TypeID, string)
	visit = func(name TypeID, path string) {
		if len(paths) >= maxPaths || visits >= maxVisits {
			truncated = true
			return
		}
		visits++
		n := c.node(name)
		if n == nil || n.Kind == "type-variable" {
			truncated = true
			return
		}
		if n.Name == "File" || n.Kind == "fiber" || n.Kind == "recipe" {
			paths = append(paths, path)
			return
		}
		if !containsHandle(name) || seen[name] {
			return
		}
		seen[name] = true
		defer delete(seen, name)
		c.walkDataFields(name, func(fieldPath string, field Field) {
			if truncated {
				return
			}
			if path != "" {
				fieldPath = path + "." + fieldPath
			}
			visit(field.typeID, fieldPath)
		})
	}
	visit(root, prefix)
	if truncated {
		paths = append(paths, "*")
	}
	return paths
}

func (c *checker) unknownOwnership(typeName string) []OwnershipFact {
	return c.unknownOwnershipID(c.canonicalRef(typeRef(typeName)))
}

func (c *checker) unknownOwnershipID(id TypeID) []OwnershipFact {
	facts := []OwnershipFact{}
	for _, path := range c.ownershipPathsID(id, "") {
		fact := OwnershipFact{Path: path, Status: "unknown", Origin: "unknown"}
		if isWildcardPath(path) {
			fact.Origin = "bounded"
			fact.potentialOwner = true
			fact.remainder = true
		}
		facts = append(facts, fact)
	}
	for i := range facts {
		if isWildcardPath(facts[i].Path) {
			facts[i].remainderExclusions = remainderExclusionsForFacts(facts)
		}
	}
	return normalizeFacts(facts)
}

func (c *checker) borrowedOwnership(typeName, region string) []OwnershipFact {
	return c.borrowedOwnershipID(c.canonicalRef(typeRef(typeName)), region)
}

func (c *checker) borrowedOwnershipID(id TypeID, region string) []OwnershipFact {
	facts := []OwnershipFact{}
	for _, path := range c.ownershipPathsID(id, "") {
		fact := OwnershipFact{Path: path, Status: "borrowed", Region: region, Origin: "parameter", source: path, sourceSet: true, ownerKind: ownershipOwnerParameter}
		if isWildcardPath(path) {
			fact.Origin = "bounded"
			fact.potentialOwner = true
			fact.remainder = true
		}
		facts = append(facts, fact)
	}
	for i := range facts {
		if isWildcardPath(facts[i].Path) {
			facts[i].remainderExclusions = remainderExclusionsForFacts(facts)
		}
	}
	return normalizeFacts(facts)
}

func cloneFacts(facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	return append([]OwnershipFact{}, facts...)
}

func wildcardPathPrefix(path string) (string, bool) {
	if path == "*" {
		return "", true
	}
	if strings.HasSuffix(path, ".*") {
		return strings.TrimSuffix(path, ".*"), true
	}
	return "", false
}

func isWildcardPath(path string) bool {
	_, wildcard := wildcardPathPrefix(path)
	return wildcard
}

const ownershipRemainderSeparator = "\x00"

func prependOwnershipPath(prefix, path string) string {
	if prefix == "" {
		return path
	}
	if path == "" {
		return prefix
	}
	if path == "*" {
		return prefix + ".*"
	}
	return prefix + "." + path
}

func encodeRemainderExclusions(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	paths = append([]string{}, paths...)
	slices.Sort(paths)
	unique := paths[:0]
	for _, path := range paths {
		if len(unique) == 0 || unique[len(unique)-1] != path {
			unique = append(unique, path)
		}
	}
	return strings.Join(unique, ownershipRemainderSeparator)
}

func decodeRemainderExclusions(encoded string) []string {
	if encoded == "" {
		return nil
	}
	return strings.Split(encoded, ownershipRemainderSeparator)
}

func remainderExcludes(fact OwnershipFact, path string) bool {
	for _, excluded := range decodeRemainderExclusions(fact.remainderExclusions) {
		if excluded == path {
			return true
		}
		base, wildcard := wildcardPathPrefix(excluded)
		if wildcard && (base == "" || pathWithinPrefix(path, base)) {
			return true
		}
	}
	return false
}

func projectRemainderExclusions(encoded, selected string) string {
	paths := decodeRemainderExclusions(encoded)
	if len(paths) == 0 {
		return ""
	}
	rebased := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == selected {
			rebased = append(rebased, "")
			continue
		}
		if strings.HasPrefix(path, selected+".") {
			rebased = append(rebased, strings.TrimPrefix(path, selected+"."))
			continue
		}
		base, wildcard := wildcardPathPrefix(path)
		if !wildcard {
			continue
		}
		if base == "" || base == selected || strings.HasPrefix(selected, base+".") {
			if base == selected {
				rebased = append(rebased, "*")
			} else if base == "" {
				rebased = append(rebased, "*")
			} else {
				rebased = append(rebased, "*")
			}
			continue
		}
		if strings.HasPrefix(base, selected+".") {
			rebased = append(rebased, strings.TrimPrefix(base, selected+".")+".*")
		}
	}
	return encodeRemainderExclusions(rebased)
}

func intersectRemainderExclusions(a, b string) string {
	if a == "" || b == "" {
		return ""
	}
	left := decodeRemainderExclusions(a)
	right := decodeRemainderExclusions(b)
	seen := make(map[string]bool, len(right))
	for _, path := range right {
		seen[path] = true
	}
	common := make([]string, 0, min(len(left), len(right)))
	for _, path := range left {
		if seen[path] {
			common = append(common, path)
		}
	}
	return encodeRemainderExclusions(common)
}

func remainderExclusionsForFacts(facts []OwnershipFact) string {
	paths := make([]string, 0, len(facts))
	for _, fact := range facts {
		if fact.Status != "borrowed" || fact.potentialOwner || fact.remainder {
			continue
		}
		paths = append(paths, fact.Path)
	}
	return encodeRemainderExclusions(paths)
}

// ownershipFactMayOwnPath reports whether one fact can still account for an
// owned value at path. A complete borrowed fact from a parameter is still a
// deferred source relation: the caller may instantiate that parameter with an
// owned value. Only a complete borrowed fact whose owner is already resolved
// outside a parameter can prove that the path is foreign; every other status
// remains possible evidence unless a remainder explicitly excludes that path.
func borrowedFactHasDeferredSource(fact OwnershipFact) bool {
	return fact.Status == "borrowed" && !fact.potentialOwner && !fact.remainder && strings.HasPrefix(fact.Region, "parameter:")
}

func completeBorrowedCoverage(fact OwnershipFact) bool {
	// A wildcard is complete only when its constructor proved that every
	// descendant shares the same borrowed provenance. An arbitrary wildcard is
	// already bounded uncertainty and must remain possible ownership.
	if fact.Status != "borrowed" || fact.potentialOwner || fact.remainder {
		return false
	}
	return !isWildcardPath(fact.Path) || fact.Origin == "bounded-all-borrowed"
}

func ownershipFactMayOwnPath(fact OwnershipFact, path string) bool {
	if isWildcardPath(fact.Path) {
		base, _ := wildcardPathPrefix(fact.Path)
		if base != "" && !pathWithinPrefix(path, base) {
			return false
		}
	} else if fact.Path != path {
		return false
	}
	if fact.remainder && remainderExcludes(fact, path) {
		return false
	}
	if completeBorrowedCoverage(fact) && !borrowedFactHasDeferredSource(fact) {
		return false
	}
	return true
}

func wildcardIntersectionBase(left, right string) (string, bool) {
	leftBase, leftWildcard := wildcardPathPrefix(left)
	rightBase, rightWildcard := wildcardPathPrefix(right)
	if !leftWildcard || !rightWildcard {
		return "", false
	}
	if leftBase == "" {
		return rightBase, true
	}
	if rightBase == "" {
		return leftBase, true
	}
	if pathWithinPrefix(leftBase, rightBase) {
		return leftBase, true
	}
	if pathWithinPrefix(rightBase, leftBase) {
		return rightBase, true
	}
	return "", false
}

func remainderExcludesWildcardIntersection(fact OwnershipFact, target string) bool {
	intersection, ok := wildcardIntersectionBase(fact.Path, target)
	if !ok {
		return false
	}
	for _, exclusion := range decodeRemainderExclusions(fact.remainderExclusions) {
		base, wildcard := wildcardPathPrefix(exclusion)
		if wildcard && (base == "" || pathWithinPrefix(intersection, base)) {
			return true
		}
	}
	return false
}

func ownershipFactMayOwnTarget(fact OwnershipFact, target string) bool {
	if !isWildcardPath(target) {
		return ownershipFactMayOwnPath(fact, target)
	}
	if !isWildcardPath(fact.Path) {
		base, _ := wildcardPathPrefix(target)
		if base != "" && !pathWithinPrefix(fact.Path, base) {
			return false
		}
		return ownershipFactMayOwnPath(fact, fact.Path)
	}
	if _, ok := wildcardIntersectionBase(fact.Path, target); !ok {
		return false
	}
	if fact.remainder && remainderExcludesWildcardIntersection(fact, target) {
		return false
	}
	if completeBorrowedCoverage(fact) && !borrowedFactHasDeferredSource(fact) {
		return false
	}
	return true
}

func omittedOwnershipMayOwnTarget(allFacts, retained []OwnershipFact, target string) bool {
	for _, fact := range allFacts {
		if slices.Contains(retained, fact) {
			continue
		}
		if ownershipFactMayOwnTarget(fact, target) {
			return true
		}
	}
	return false
}

// remainderExclusionsForRetainedFacts only discharges a generated remainder
// at a retained borrowed terminal when every omitted alternative is outside
// that terminal. In particular, a potential wildcard which overlaps the
// terminal keeps the generated remainder live; a disjoint wildcard or a
// remainder which explicitly excludes the terminal does not.
func remainderExclusionsForRetainedFacts(retained, allFacts []OwnershipFact) string {
	paths := make([]string, 0, len(retained))
	for _, fact := range retained {
		if fact.Status != "borrowed" || fact.potentialOwner || fact.remainder {
			continue
		}
		if omittedOwnershipMayOwnTarget(allFacts, retained, fact.Path) {
			continue
		}
		paths = append(paths, fact.Path)
	}
	return encodeRemainderExclusions(paths)
}

func completeWildcardCovers(wildcard OwnershipFact, path string) bool {
	if !isWildcardPath(wildcard.Path) || (wildcard.Origin != "bounded-all-owned" && wildcard.Origin != "bounded-all-borrowed") {
		return false
	}
	base, _ := wildcardPathPrefix(wildcard.Path)
	return base == "" || path == base || strings.HasPrefix(path, base+".")
}

// projectWildcardPath rebases an uncertain subtree through one member
// projection. A descendant prefix is kept until the selected terminal path is
// complete; one retained descendant does not prove the whole subtree.
func projectWildcardPath(fact OwnershipFact, selected string, terminal bool) (rebased string, canDrop bool, ok bool) {
	base, wildcard := wildcardPathPrefix(fact.Path)
	if !wildcard {
		return "", false, false
	}
	if base == "" {
		return "*", fact.remainder && terminal && remainderExcludes(fact, selected), true
	}
	if base == selected {
		return "*", fact.remainder && terminal && remainderExcludes(fact, selected), true
	}
	if strings.HasPrefix(base, selected+".") {
		return strings.TrimPrefix(base, selected+".") + ".*", false, true
	}
	if strings.HasPrefix(selected, base+".") {
		return "*", fact.remainder && terminal && remainderExcludes(fact, selected), true
	}
	return "", false, false
}

func normalizeProjectedWildcard(fact OwnershipFact) OwnershipFact {
	completeOwned := fact.Status == "owned" && fact.Origin == "bounded-all-owned"
	completeBorrowed := fact.Status == "borrowed" && fact.Origin == "bounded-all-borrowed"
	if !completeOwned && !completeBorrowed {
		// A projected wildcard is an incomplete subtree unless its constructor
		// explicitly proved that every descendant had the same provenance. Keep
		// the parameter-relative source while converting it to scoped potential;
		// substitution must not turn an omitted owner into ordinary unknown.
		fact.Status = "unknown"
		fact.Origin = "bounded"
		fact.potentialOwner = true
	}
	return fact
}

func prependFacts(prefix string, facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		copy := fact
		if prefix != "" {
			copy.Path = prependOwnershipPath(prefix, copy.Path)
			paths := decodeRemainderExclusions(copy.remainderExclusions)
			for i, path := range paths {
				paths[i] = prependOwnershipPath(prefix, path)
			}
			copy.remainderExclusions = encodeRemainderExclusions(paths)
		}
		out = append(out, copy)
	}
	return normalizeFacts(out)
}

func projectFacts(facts []OwnershipFact, field string) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	prefix := field + "."
	exact := false
	inexact := false
	descendant := false
	for _, fact := range facts {
		if fact.Path == field {
			if fact.Status == "owned" || fact.Status == "borrowed" {
				exact = true
			} else {
				inexact = true
			}
		} else if strings.HasPrefix(fact.Path, prefix) {
			descendant = true
		}
	}
	terminal := exact && !inexact && !descendant
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		if rebased, canDrop, ok := projectWildcardPath(fact, field, terminal); ok {
			if canDrop {
				continue
			}
			copy := fact
			copy.Path = rebased
			copy.remainderExclusions = projectRemainderExclusions(fact.remainderExclusions, field)
			copy = normalizeProjectedWildcard(copy)
			out = append(out, copy)
			continue
		}
		if fact.Path == field {
			copy := fact
			copy.Path = ""
			if !copy.sourceSet {
				copy.source = field
				copy.sourceSet = true
			}
			out = append(out, copy)
			continue
		}
		if strings.HasPrefix(fact.Path, prefix) {
			copy := fact
			copy.Path = strings.TrimPrefix(fact.Path, prefix)
			if !copy.sourceSet {
				copy.source = fact.Path
				copy.sourceSet = true
			}
			out = append(out, copy)
		}
	}
	return normalizeFacts(out)
}

func projectVariantFacts(facts []OwnershipFact, variant, field string) []OwnershipFact {
	prefix := variant + "." + field
	exact := false
	inexact := false
	descendant := false
	for _, fact := range facts {
		if fact.Path == prefix {
			if fact.Status == "owned" || fact.Status == "borrowed" {
				exact = true
			} else {
				inexact = true
			}
		} else if strings.HasPrefix(fact.Path, prefix+".") {
			descendant = true
		}
	}
	terminal := exact && !inexact && !descendant
	projected := make([]OwnershipFact, 0)
	for _, fact := range facts {
		if rebased, canDrop, ok := projectWildcardPath(fact, prefix, terminal); ok {
			if canDrop {
				continue
			}
			copy := fact
			copy.Path = rebased
			copy.remainderExclusions = projectRemainderExclusions(fact.remainderExclusions, prefix)
			copy = normalizeProjectedWildcard(copy)
			projected = append(projected, copy)
			continue
		}
		path := fact.Path
		if path == prefix {
			copy := fact
			copy.Path = ""
			if !copy.sourceSet {
				copy.source = prefix
				copy.sourceSet = true
			}
			projected = append(projected, copy)
		} else if strings.HasPrefix(path, prefix+".") {
			copy := fact
			copy.Path = strings.TrimPrefix(path, prefix+".")
			if !copy.sourceSet {
				copy.source = path
				copy.sourceSet = true
			}
			projected = append(projected, copy)
		}
	}
	return normalizeFacts(projected)
}

// mergeFacts is the join of two alternatives' facts: per path, the union of
// their owner sets (design §3.2). An owner proven by either alternative stays
// in the join, so a conditional escape is checked against every owner.
func mergeFacts(a, b []OwnershipFact) []OwnershipFact {
	if len(a) == 0 {
		return cloneFacts(b)
	}
	if len(b) == 0 {
		return cloneFacts(a)
	}
	return normalizeFacts(slices.Concat(a, b))
}

func conservativeCycleFacts(observed ...[]OwnershipFact) []OwnershipFact {
	owned := make([]OwnershipFact, 0)
	for _, facts := range observed {
		for _, fact := range facts {
			if fact.Status != "owned" {
				continue
			}
			fact.Path = "*"
			fact.Origin = "bounded"
			fact.potentialOwner = true
			owned = append(owned, fact)
		}
	}
	if len(owned) == 0 {
		return []OwnershipFact{{Path: "*", Status: "unknown", Origin: "bounded", potentialOwner: true}}
	}
	return normalizeFacts(owned)
}

func ownershipPathMatches(argumentPath, sourcePath string) bool {
	// A bounded wildcard means that a handle exists somewhere below the
	// argument. It must remain eligible for a helper's parameter-relative
	// summary; dropping it would turn a proven owned path into an unsafe
	// unknown result.
	if argumentPath == sourcePath || argumentPath == "*" || sourcePath == "*" {
		return true
	}
	if sourcePrefix, sourceWildcard := wildcardPathPrefix(sourcePath); sourceWildcard && sourcePrefix != "" {
		if argumentPath == sourcePrefix || strings.HasPrefix(argumentPath, sourcePrefix+".") {
			return true
		}
	}
	if argumentPrefix, argumentWildcard := wildcardPathPrefix(argumentPath); argumentWildcard && argumentPrefix != "" {
		if sourcePath == argumentPrefix || strings.HasPrefix(sourcePath, argumentPrefix+".") {
			return true
		}
	}
	return false
}

func preservesCompleteWildcardArgument(summary, argument OwnershipFact) bool {
	// A parameter-relative wildcard can preserve its complete argument evidence
	// through a helper boundary. The returned path may be nested, so only the
	// source and both complete wildcard proofs determine whether this is safe.
	if !isWildcardPath(summary.Path) || summary.potentialOwner || summary.remainder ||
		summary.remainderExclusions != "" || !summary.sourceSet || summary.source != "*" ||
		argument.Path != "*" || argument.potentialOwner || argument.remainder ||
		argument.remainderExclusions != "" {
		return false
	}
	return (argument.Status == "owned" && argument.Origin == "bounded-all-owned") ||
		(argument.Status == "borrowed" && argument.Origin == "bounded-all-borrowed")
}

func instantiateFacts(facts []OwnershipFact, params []Param, args []ValueType) []OwnershipFact {
	ownership := make([][]OwnershipFact, len(args))
	for i := range args {
		ownership[i] = args[i].Ownership
	}
	return instantiateOwnershipFacts(facts, params, ownership)
}

func instantiateCheckedFacts(facts []OwnershipFact, params []Param, args []checkedExpression) []OwnershipFact {
	ownership := make([][]OwnershipFact, len(args))
	for i := range args {
		// Parameter-relative summary facts describe handles held by the
		// argument. For a recipe argument those are its captures; its result
		// facts are never parameter-relative.
		ownership[i] = heldFacts(args[i], 0)
	}
	return instantiateOwnershipFacts(facts, params, ownership)
}

func instantiateOwnershipFacts(facts []OwnershipFact, params []Param, args [][]OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		matched := false
		for i, param := range params {
			if fact.Region != "parameter:"+param.Name && fact.Region != "parameter:*" {
				continue
			}
			matched = true
			if i >= len(args) || len(args[i]) == 0 {
				out = append(out, OwnershipFact{Path: fact.Path, Status: "unknown", Origin: "helper", layer: fact.layer})
				continue
			}
			hasConcreteSource := slices.ContainsFunc(args[i], func(argument OwnershipFact) bool {
				return !isWildcardPath(argument.Path) && argument.Path == fact.source
			})
			for _, argument := range args[i] {
				if hasConcreteSource && isWildcardPath(argument.Path) && canSkipWildcardForSource(argument, fact.source) {
					continue
				}
				if !ownershipPathMatches(argument.Path, fact.source) {
					continue
				}
				copy := argument
				copy.layer = fact.layer
				copy.potentialOwner = copy.potentialOwner || fact.potentialOwner
				copy.remainder = fact.remainder
				copy.remainderExclusions = fact.remainderExclusions
				preserveWildcard := preservesCompleteWildcardArgument(fact, argument)
				if isWildcardPath(fact.source) || isWildcardPath(argument.Path) {
					// A wildcard source or argument is evidence about an
					// unspecified descendant. Preserve complete whole-value
					// wildcard evidence, but do not turn an ambiguous projected
					// field into a certain owned fact.
					if !preserveWildcard {
						copy.Status = "unknown"
						copy.Origin = "bounded"
						copy.potentialOwner = true
						if !fact.remainder {
							copy.remainder = false
							copy.remainderExclusions = ""
						}
					}
				}
				// The summary path is relative to the returned value. The
				// argument contributes status/region, not another path prefix;
				// projection and payload construction already recorded the
				// returned shape before this call boundary.
				copy.Path = fact.Path
				copy.Origin = "helper"
				if preserveWildcard {
					copy.Origin = argument.Origin
				}
				out = append(out, copy)
			}
			if !slices.ContainsFunc(args[i], func(argument OwnershipFact) bool {
				return ownershipPathMatches(argument.Path, fact.source)
			}) {
				out = append(out, OwnershipFact{Path: fact.Path, Status: "unknown", Origin: "helper", layer: fact.layer})
			}
		}
		if !matched {
			// Every other owner atom (Exec, Lexical, Closed) is already an
			// owner of the call's own occurrence.
			out = append(out, fact)
		}
	}
	return normalizeFacts(out)
}

func canSkipWildcardForSource(fact OwnershipFact, source string) bool {
	if !isWildcardPath(fact.Path) {
		return false
	}
	if fact.remainder {
		return remainderExcludes(fact, source)
	}
	return fact.Status == "borrowed" && !fact.potentialOwner && fact.Origin == "bounded-all-borrowed"
}

// ownerIdentity is the owner atom of a fact (design §3.1): two facts of
// one path with the same identity describe the same owner and merge, while
// distinct owners stay distinct members of the path's owner set. The
// parameter selector (source) is part of the Param atom (B2).
func ownerIdentity(fact OwnershipFact) string {
	relation := ""
	if fact.callbackRelation != nil {
		relation = fact.callbackRelation.key
	}
	return fmt.Sprintf("%q\x00%q\x00%d\x00%d\x00%q\x00%t\x00%q\x00%q", fact.Path, fact.Status, fact.ownerKind, fact.layer, fact.Region, fact.sourceSet, fact.source, relation) + "\x00" + fact.relationEnv
}

// mergeKey groups the facts normalizeFacts merges. An unknown wildcard is
// one ⊤ marker per path and layer whatever owner it names.
func mergeKey(fact OwnershipFact) string {
	if isWildcardPath(fact.Path) && fact.Status == "unknown" {
		return fmt.Sprintf("wildcard\x00%q\x00%d", fact.Path, fact.layer)
	}
	return ownerIdentity(fact)
}

// mergeSameOwner merges two facts naming one owner. Provenance is the
// minimum origin, so the merge is commutative, associative and idempotent.
func mergeSameOwner(a, b OwnershipFact) OwnershipFact {
	if a == b {
		return a
	}
	if isWildcardPath(a.Path) && a.Status == "unknown" {
		return mergePotentialWildcard(a, b)
	}
	merged := a
	if b.Origin < merged.Origin {
		merged.Origin = b.Origin
	}
	merged.potentialOwner = a.potentialOwner || b.potentialOwner
	if a.remainder != b.remainder || a.remainderExclusions != b.remainderExclusions {
		merged.remainder = false
		merged.remainderExclusions = ""
	}
	return merged
}

type ownershipProvenanceKey struct {
	region    string
	sourceSet bool
	ownerKind ownershipOwnerKind
}

type ownershipFrontierNode struct {
	exact    []OwnershipFact
	wildcard []OwnershipFact
	children map[string]*ownershipFrontierNode
}

func ownershipProvenance(fact OwnershipFact) ownershipProvenanceKey {
	return ownershipProvenanceKey{
		region:    fact.Region,
		sourceSet: fact.sourceSet,
		ownerKind: fact.ownerKind,
	}
}

func ownershipFrontierMarker(key ownershipProvenanceKey, prefix string) OwnershipFact {
	marker := OwnershipFact{
		Path:      "*",
		Status:    "borrowed",
		Region:    key.region,
		Origin:    "bounded-all-borrowed",
		sourceSet: key.sourceSet,
		ownerKind: key.ownerKind,
	}
	if prefix != "" {
		marker.Path = prefix + ".*"
	}
	if key.sourceSet {
		marker.source = "*"
	}
	return marker
}

func addOwnershipFrontierFact(root *ownershipFrontierNode, fact OwnershipFact) {
	base, wildcard := wildcardPathPrefix(fact.Path)
	if wildcard {
		fact.Path = base
	} else {
		base = fact.Path
	}
	segments := []string{}
	if base != "" {
		segments = strings.Split(base, ".")
	}
	node := root
	for _, segment := range segments {
		if node.children == nil {
			node.children = map[string]*ownershipFrontierNode{}
		}
		child := node.children[segment]
		if child == nil {
			child = &ownershipFrontierNode{}
			node.children[segment] = child
		}
		node = child
	}
	if wildcard {
		node.wildcard = append(node.wildcard, fact)
	} else {
		node.exact = append(node.exact, fact)
	}
}

func ownershipFrontierKeys(node *ownershipFrontierNode) map[ownershipProvenanceKey]bool {
	keys := map[ownershipProvenanceKey]bool{}
	for _, fact := range append(append([]OwnershipFact{}, node.exact...), node.wildcard...) {
		keys[ownershipProvenance(fact)] = true
	}
	for _, child := range node.children {
		for key := range ownershipFrontierKeys(child) {
			keys[key] = true
		}
	}
	return keys
}

func ownershipFrontierSingleKey(node *ownershipFrontierNode) (ownershipProvenanceKey, bool) {
	keys := ownershipFrontierKeys(node)
	if len(keys) != 1 {
		return ownershipProvenanceKey{}, false
	}
	for key := range keys {
		return key, true
	}
	return ownershipProvenanceKey{}, false
}

func ownershipFrontierCanCollapse(node *ownershipFrontierNode) bool {
	if len(node.wildcard) > 0 || len(node.children) <= 1 {
		return true
	}
	for _, child := range node.children {
		if len(child.children) > 0 || len(child.wildcard) > 0 {
			return false
		}
	}
	return true
}

func collectOwnershipFrontier(node *ownershipFrontierNode, prefix string) ([]OwnershipFact, bool) {
	if key, ok := ownershipFrontierSingleKey(node); ok {
		if prefix == "" {
			if ownershipFrontierCanCollapse(node) && (len(node.children) > 0 || len(node.wildcard) > 0) {
				return []OwnershipFact{ownershipFrontierMarker(key, "")}, true
			}
			if len(node.exact) == 1 {
				return cloneFacts(node.exact), true
			}
		} else if ownershipFrontierCanCollapse(node) && (len(node.children) > 0 || len(node.wildcard) > 0) {
			return []OwnershipFact{ownershipFrontierMarker(key, prefix)}, true
		} else if len(node.exact) > 0 {
			return cloneFacts(node.exact), true
		}
	}

	if len(node.wildcard) > 0 {
		// A wildcard already covers every descendant. Keeping it beside a
		// different provenance key would make one source stand in for another.
		return nil, false
	}
	out := cloneFacts(node.exact)
	children := make([]string, 0, len(node.children))
	for name := range node.children {
		children = append(children, name)
	}
	slices.Sort(children)
	for _, name := range children {
		childPrefix := name
		if prefix != "" {
			childPrefix = prefix + "." + name
		}
		child, ok := collectOwnershipFrontier(node.children[name], childPrefix)
		if !ok {
			return nil, false
		}
		out = append(out, child...)
	}
	return out, true
}

// compactBorrowedFacts retains complete terminal facts and replaces a
// homogeneous descendant set with a provenance-preserving prefix frontier.
// A wildcard source means any descendant of the same parameter, rather than
// an arbitrary borrowed value from another parameter.
func compactBorrowedFacts(facts []OwnershipFact, maxFacts int) ([]OwnershipFact, bool) {
	root := &ownershipFrontierNode{}
	for _, fact := range facts {
		if fact.Status != "borrowed" || fact.potentialOwner {
			return nil, false
		}
		if isWildcardPath(fact.Path) && fact.Origin != "bounded-all-borrowed" {
			return nil, false
		}
		addOwnershipFrontierFact(root, fact)
	}
	frontier, ok := collectOwnershipFrontier(root, "")
	if !ok || len(frontier) > maxFacts {
		return nil, false
	}
	return normalizeFacts(frontier), true
}

func compactOwnedFacts(facts []OwnershipFact, maxFacts int) ([]OwnershipFact, bool) {
	if len(facts) <= maxFacts {
		return nil, false
	}
	region := ""
	ownerKind := ownershipOwnerUnknown
	for i, fact := range facts {
		if fact.Status != "owned" || fact.potentialOwner {
			return nil, false
		}
		if i == 0 {
			region = fact.Region
			ownerKind = fact.ownerKind
			continue
		}
		if fact.Region != region {
			region = "*"
		}
		if fact.ownerKind != ownerKind || fact.Region != region {
			// The fact budget is exhausted: distinct owners widen to ⊤.
			region = "*"
			ownerKind = ownershipOwnerUnknown
		}
	}
	return []OwnershipFact{{Path: "*", Status: "owned", Region: region, Origin: "bounded-all-owned", ownerKind: ownerKind}}, true
}

func wildcardBaseSegments(path string) ([]string, bool) {
	base, wildcard := wildcardPathPrefix(path)
	if !wildcard {
		return nil, false
	}
	if base == "" {
		return nil, true
	}
	return strings.Split(base, "."), true
}

func commonWildcardPrefix(paths [][]string) []string {
	if len(paths) == 0 {
		return nil
	}
	common := append([]string{}, paths[0]...)
	for _, path := range paths[1:] {
		limit := min(len(common), len(path))
		for i := 0; i < limit; i++ {
			if common[i] != path[i] {
				limit = i
				break
			}
		}
		common = common[:limit]
		if len(common) == 0 {
			return nil
		}
	}
	return common
}

func pathWithinPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+".")
}

func coalesceOwnedWildcards(facts []OwnershipFact) []OwnershipFact {
	groups := map[string][]int{}
	for i, fact := range facts {
		if fact.Status != "owned" || fact.Origin != "bounded-all-owned" || fact.potentialOwner {
			continue
		}
		base, wildcard := wildcardPathPrefix(fact.Path)
		if !wildcard || base == "" {
			continue
		}
		key := fmt.Sprintf("%s\x00%d", fact.Region, fact.ownerKind)
		groups[key] = append(groups[key], i)
	}
	if len(groups) == 0 {
		return facts
	}
	removed := make(map[int]bool)
	added := []OwnershipFact{}
	for _, indexes := range groups {
		if len(indexes) < 2 {
			continue
		}
		bases := make([][]string, 0, len(indexes))
		for _, index := range indexes {
			base, _ := wildcardBaseSegments(facts[index].Path)
			bases = append(bases, base)
		}
		common := commonWildcardPrefix(bases)
		if len(common) == 0 {
			continue
		}
		prefix := strings.Join(common, ".")
		conflict := false
		for i, fact := range facts {
			if slices.Contains(indexes, i) {
				continue
			}
			if pathWithinPrefix(fact.Path, prefix) {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		marker := facts[indexes[0]]
		marker.Path = prefix + ".*"
		added = append(added, marker)
		for _, index := range indexes {
			removed[index] = true
		}
	}
	if len(removed) == 0 {
		return facts
	}
	result := make([]OwnershipFact, 0, len(facts)-len(removed)+len(added))
	for i, fact := range facts {
		if !removed[i] {
			result = append(result, fact)
		}
	}
	result = append(result, added...)
	slices.SortStableFunc(result, func(a, b OwnershipFact) int {
		if a.Path != b.Path {
			return strings.Compare(a.Path, b.Path)
		}
		return strings.Compare(a.Region, b.Region)
	})
	return result
}

func ownershipPathDepth(path string) int {
	if isWildcardPath(path) {
		return 1 << 30
	}
	if path == "" {
		return 0
	}
	return strings.Count(path, ".") + 1
}

func ownershipPathCoveredByIncomplete(path string, incomplete map[string]bool) bool {
	for prefix := range incomplete {
		base, wildcard := wildcardPathPrefix(prefix)
		if wildcard && (base == "" || pathWithinPrefix(path, base)) {
			return true
		}
	}
	return false
}

func boundedBorrowedWithPotential(facts []OwnershipFact, incomplete map[string]bool, maxFacts int) []OwnershipFact {
	ownedCoverage := make([]OwnershipFact, 0)
	for _, fact := range facts {
		if fact.Status == "owned" && fact.Origin == "bounded-all-owned" && !fact.potentialOwner {
			ownedCoverage = append(ownedCoverage, fact)
		}
	}
	// Parameter-relative borrowed facts are deferred source relations, not
	// proof of foreign ownership. Keep complete relations in the bounded
	// representation before ordinary borrowed terminals so substitution can
	// still inspect the caller's ownership evidence.
	deferredCoverage := make([]OwnershipFact, 0)
	for _, fact := range facts {
		if !borrowedFactHasDeferredSource(fact) || !completeBorrowedCoverage(fact) {
			continue
		}
		deferredCoverage = append(deferredCoverage, fact)
	}
	retained := append(append([]OwnershipFact{}, ownedCoverage...), deferredCoverage...)
	if len(retained) >= maxFacts {
		return []OwnershipFact{{Path: "*", Status: "unknown", Origin: "bounded", potentialOwner: true}}
	}
	candidates := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		if fact.Status != "borrowed" || fact.potentialOwner || incomplete[fact.Path] || slices.Contains(retained, fact) {
			continue
		}
		candidates = append(candidates, fact)
	}
	slices.SortStableFunc(candidates, func(a, b OwnershipFact) int {
		if depth := ownershipPathDepth(a.Path) - ownershipPathDepth(b.Path); depth != 0 {
			return depth
		}
		return strings.Compare(a.Path, b.Path)
	})
	available := maxFacts - len(retained) - 1
	if available < 0 {
		available = 0
	}
	if len(candidates) > available {
		candidates = candidates[:available]
	}
	bounded := append(append([]OwnershipFact{}, retained...), candidates...)
	bounded = append(bounded, OwnershipFact{
		Path:                "*",
		Status:              "unknown",
		Region:              "*",
		Origin:              "bounded",
		potentialOwner:      true,
		remainder:           true,
		remainderExclusions: remainderExclusionsForRetainedFacts(bounded, facts),
	})
	return normalizeFacts(bounded)
}

func incompleteWildcard(fact OwnershipFact) bool {
	if !isWildcardPath(fact.Path) {
		return false
	}
	return fact.potentialOwner || (fact.Origin != "bounded-all-owned" && fact.Origin != "bounded-all-borrowed")
}

func mergePotentialWildcard(a, b OwnershipFact) OwnershipFact {
	merged := a
	if a.callbackRelation != b.callbackRelation {
		merged.callbackRelation = nil
	}
	merged.Status = "unknown"
	merged.Origin = "bounded"
	merged.potentialOwner = true
	if merged.remainder && b.remainder {
		merged.remainderExclusions = intersectRemainderExclusions(merged.remainderExclusions, b.remainderExclusions)
	} else {
		merged.remainder = false
		merged.remainderExclusions = ""
	}
	if merged.Region != b.Region || merged.ownerKind != b.ownerKind {
		merged.Region = "*"
		merged.ownerKind = ownershipOwnerUnknown
	}
	if merged.sourceSet != b.sourceSet || (merged.sourceSet && merged.source != b.source) {
		merged.source = ""
		merged.sourceSet = false
	}
	return merged
}

// retainIncompleteWildcards turns every non-complete wildcard into an
// explicit scoped potential marker. The marker covers only its own prefix;
// unrelated sibling facts remain available for the bounded representation.
// This is the widening boundary for path truncation and projected uncertainty.
func retainIncompleteWildcards(facts []OwnershipFact) []OwnershipFact {
	markers := map[string]OwnershipFact{}
	for _, fact := range facts {
		if !incompleteWildcard(fact) {
			continue
		}
		if previous, ok := markers[fact.Path]; ok {
			markers[fact.Path] = mergePotentialWildcard(previous, fact)
			continue
		}
		fact.Status = "unknown"
		fact.Origin = "bounded"
		fact.potentialOwner = true
		markers[fact.Path] = fact
	}
	if len(markers) == 0 {
		return facts
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		if incompleteWildcard(fact) {
			continue
		}
		covered := false
		for _, marker := range markers {
			if marker.remainder {
				continue
			}
			base, _ := wildcardPathPrefix(marker.Path)
			if base == "" || pathWithinPrefix(fact.Path, base) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, fact)
		}
	}
	for _, marker := range markers {
		out = append(out, marker)
	}
	slices.SortStableFunc(out, compareOwnershipFacts)
	return out
}

func compareOwnershipFacts(a, b OwnershipFact) int {
	if a.callbackRelation != b.callbackRelation {
		if a.callbackRelation == nil {
			return -1
		}
		if b.callbackRelation == nil {
			return 1
		}
		if order := strings.Compare(a.callbackRelation.key, b.callbackRelation.key); order != 0 {
			return order
		}
	}
	if a.Path != b.Path {
		return strings.Compare(a.Path, b.Path)
	}
	if a.Status != b.Status {
		return strings.Compare(a.Status, b.Status)
	}
	if a.Region != b.Region {
		return strings.Compare(a.Region, b.Region)
	}
	if a.Origin != b.Origin {
		return strings.Compare(a.Origin, b.Origin)
	}
	if a.sourceSet != b.sourceSet {
		if !a.sourceSet {
			return -1
		}
		return 1
	}
	if a.source != b.source {
		return strings.Compare(a.source, b.source)
	}
	if a.ownerKind < b.ownerKind {
		return -1
	}
	if a.ownerKind > b.ownerKind {
		return 1
	}
	if a.potentialOwner != b.potentialOwner {
		if !a.potentialOwner {
			return -1
		}
		return 1
	}
	if a.remainder != b.remainder {
		if !a.remainder {
			return -1
		}
		return 1
	}
	if a.remainderExclusions != b.remainderExclusions {
		return strings.Compare(a.remainderExclusions, b.remainderExclusions)
	}
	if a.layer != b.layer {
		return a.layer - b.layer
	}
	if a.exec != b.exec {
		return a.exec - b.exec
	}
	return strings.Compare(a.relationEnv, b.relationEnv)
}

func normalizeFacts(facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	if layered := slices.ContainsFunc(facts, func(fact OwnershipFact) bool { return fact.layer != facts[0].layer }); layered {
		// Each held layer is its own evidence set; budgets and wildcard
		// coalescing never mix two layers.
		layers := map[int][]OwnershipFact{}
		for _, fact := range facts {
			layers[fact.layer] = append(layers[fact.layer], fact)
		}
		var out []OwnershipFact
		for _, layer := range slices.Sorted(maps.Keys(layers)) {
			out = append(out, normalizeFacts(layers[layer])...)
		}
		return out
	}
	out := append([]OwnershipFact{}, facts...)
	slices.SortStableFunc(out, compareOwnershipFacts)
	result := make([]OwnershipFact, 0, len(out))
	merged := map[string]int{}
	for _, fact := range out {
		key := mergeKey(fact)
		if at, ok := merged[key]; ok {
			result[at] = mergeSameOwner(result[at], fact)
			continue
		}
		merged[key] = len(result)
		result = append(result, fact)
	}
	slices.SortStableFunc(result, compareOwnershipFacts)
	result = coalesceOwnedWildcards(result)
	result = retainIncompleteWildcards(result)
	const maxFacts = 64
	if len(result) > maxFacts {
		allOwned := true
		allBorrowed := true
		for _, fact := range result {
			if fact.Status != "owned" {
				allOwned = false
			}
			if fact.Status != "borrowed" {
				allBorrowed = false
			}
		}
		if allOwned {
			if bounded, ok := compactOwnedFacts(result, maxFacts); ok {
				return bounded
			}
		}
		if allBorrowed {
			if bounded, ok := compactBorrowedFacts(result, maxFacts); ok {
				return bounded
			}
		}
		// A path with several alternatives is indivisible evidence. Retaining a
		// borrowed fact for a selected path while dropping its owned sibling
		// would let projection mistake an incomplete path for a complete one.
		// Keep every fact in such a group before spending the remaining budget on
		// independent paths. This keeps the bounded representation useful for
		// safe siblings without turning truncation into a fabricated proof.
		groups := make(map[string][]OwnershipFact)
		incomplete := make(map[string]bool)
		incompleteCount := 0
		for _, fact := range result {
			groups[fact.Path] = append(groups[fact.Path], fact)
		}
		for path, group := range groups {
			if len(group) > 1 || slices.ContainsFunc(group, func(fact OwnershipFact) bool {
				return fact.potentialOwner || (isWildcardPath(fact.Path) && (fact.Origin == "bounded-all-owned" || fact.Origin == "bounded-all-borrowed"))
			}) {
				incomplete[path] = true
				incompleteCount += len(group)
			}
		}
		if incompleteCount >= maxFacts {
			// No bounded representation can retain every alternative for the
			// selected path. Preserve independently complete borrowed terminals
			// when there is room; the top marker still rejects every omitted path.
			return boundedBorrowedWithPotential(result, incomplete, maxFacts)
		}

		bounded := make([]OwnershipFact, 0, maxFacts)
		for _, fact := range result {
			if incomplete[fact.Path] {
				bounded = append(bounded, fact)
			}
		}
		if len(bounded) >= maxFacts {
			return boundedBorrowedWithPotential(result, incomplete, maxFacts)
		}

		// Reserve a marker when a discarded fact could itself be owned or could
		// carry unresolved ownership. A retained concrete borrowed sibling may
		// still discharge the marker during projection; a discarded selected
		// path has only the marker and is rejected conservatively.
		potentialNeeded := false
		candidateCount := 0
		for _, fact := range result {
			if incomplete[fact.Path] || (isWildcardPath(fact.Path) && (fact.Origin == "bounded-all-owned" || fact.Origin == "bounded-all-borrowed")) {
				continue
			}
			if slices.ContainsFunc(result, func(candidate OwnershipFact) bool {
				// A complete bounded subtree already accounts for every fact
				// below its prefix, so retaining another global potential marker
				// would incorrectly poison unrelated sibling projections.
				return candidate != fact && completeWildcardCovers(candidate, fact.Path)
			}) {
				continue
			}
			candidateCount++
			switch {
			case fact.Status == "owned", fact.Status == "unknown", fact.potentialOwner, allBorrowed && fact.Status == "borrowed":
				potentialNeeded = true
			}
		}
		available := maxFacts - len(bounded)
		if candidateCount > available {
			potentialNeeded = true
		}
		if potentialNeeded {
			available--
		}
		if available < 0 {
			return boundedBorrowedWithPotential(result, incomplete, maxFacts)
		}

		// Keep known borrowed paths first so an independently safe sibling stays
		// observable even when owned siblings consume most of the budget. The
		// retained incomplete groups above always win over these priorities.
		for _, status := range []string{"borrowed", "owned", "unknown"} {
			candidates := make([]OwnershipFact, 0, len(result))
			for _, fact := range result {
				if incomplete[fact.Path] || fact.Status != status {
					continue
				}
				candidates = append(candidates, fact)
			}
			slices.SortStableFunc(candidates, func(a, b OwnershipFact) int {
				aDeferred := borrowedFactHasDeferredSource(a)
				bDeferred := borrowedFactHasDeferredSource(b)
				if aDeferred != bDeferred {
					if aDeferred {
						return -1
					}
					return 1
				}
				aCovered := ownershipPathCoveredByIncomplete(a.Path, incomplete)
				bCovered := ownershipPathCoveredByIncomplete(b.Path, incomplete)
				if aCovered != bCovered {
					if !aCovered {
						return -1
					}
					return 1
				}
				if depth := ownershipPathDepth(a.Path) - ownershipPathDepth(b.Path); depth != 0 {
					return depth
				}
				return strings.Compare(a.Path, b.Path)
			})
			for _, fact := range candidates {
				if available == 0 {
					break
				}
				bounded = append(bounded, fact)
				available--
			}
		}
		if potentialNeeded {
			bounded = append(bounded, OwnershipFact{
				Path:                "*",
				Status:              "unknown",
				Region:              "*",
				Origin:              "bounded",
				potentialOwner:      true,
				remainder:           true,
				remainderExclusions: remainderExclusionsForRetainedFacts(bounded, result),
			})
		}
		return normalizeFacts(bounded)
	}
	return result
}

func hasOwnedFact(facts []OwnershipFact, region string) bool {
	for _, fact := range facts {
		if fact.Status == "owned" && (fact.Region == "*" || fact.ownerKind == ownershipOwnerUnknown || (fact.ownerKind == ownershipOwnerLexical && fact.Region == region)) {
			return true
		}
	}
	return false
}

func hasOwnedClosed(facts []OwnershipFact) bool {
	for _, fact := range facts {
		if fact.Status == "owned" && closedOwnerKind(fact.ownerKind) {
			return true
		}
	}
	return false
}

func hasPotentialOwner(facts []OwnershipFact) bool {
	for _, fact := range facts {
		if fact.potentialOwner {
			return true
		}
	}
	return false
}

func (c *checker) withRegion(region string, fn func() checkedExpression) checkedExpression {
	previous := c.region
	c.region = region
	defer func() { c.region = previous }()
	return fn()
}

// typeRef is the legacy source/display parser used only at compatibility
// boundaries. The checker immediately resolves its kind/name/children into
// numeric canonical nodes; these legacy IDs never participate in checking.
func typeRef(name string) TypeRef {
	var ref TypeRef
	switch {
	case name == "":
		return ref
	case name == "string", name == "bool", name == "i64", name == "bytes", name == voidTypeName:
		ref = TypeRef{Kind: "primitive", Name: name}
	case name == "File", name == "Latch", builtinCallbacks[name].Result != "":
		ref = TypeRef{Kind: "opaque", Name: name}
	case strings.HasPrefix(name, "Fiber:"):
		ref = TypeRef{Kind: "fiber", Args: []TypeRef{typeRef(strings.TrimPrefix(name, "Fiber:"))}}
	case strings.HasPrefix(name, "GoResult:"):
		ref = TypeRef{Kind: "goResult", Args: []TypeRef{typeRef(strings.TrimPrefix(name, "GoResult:"))}}
	case strings.HasPrefix(name, "provider:"):
		ref = TypeRef{Kind: "provider", Name: strings.TrimPrefix(name, "provider:")}
	case name == "never":
		ref = TypeRef{Kind: "never"}
	case name == "invalid":
		ref = TypeRef{Kind: "invalid"}
	default:
		ref = TypeRef{Kind: "named", Name: name}
	}
	sum := sha256.Sum256([]byte(name))
	ref.ID = "legacy:" + hex.EncodeToString(sum[:8])
	for _, arg := range ref.Args {
		ref.ArgIDs = append(ref.ArgIDs, arg.ID)
	}
	return ref
}
func providerTypeRef(p *Provider) TypeRef {
	ref := typeRef("provider:" + p.Service)
	ref.Declaration = "provider:" + currentModuleIdentity + ":" + p.Name
	return ref
}

func serviceIdentity(name string) string { return "service:" + currentModuleIdentity + ":" + name }

func providerContract(p *Provider) ValueType {
	// This compatibility shim preserves the compact checked provider view.
	// Graph publication projects the retained numeric recipe roots directly.
	return publicValue(p.Contract)
}
func normalized(names []string) []string {
	out := append([]string{}, names...)
	slices.Sort(out)
	return slices.Compact(out)
}
func union(a, b []string) []string { return normalized(append(append([]string{}, a...), b...)) }
func remove(a []string, n string) []string {
	out := []string{}
	for _, v := range a {
		if v != n {
			out = append(out, v)
		}
	}
	return out
}
func difference(a, b []string) []string {
	out := []string{}
	for _, v := range a {
		if !slices.Contains(b, v) {
			out = append(out, v)
		}
	}
	return out
}
func Compile(source string) *Result { return CompileFor(source, "go") }
func CompileFor(source, target string) *Result {
	return CompileAt(source, target, ".")
}
func CompileAt(source, target, dir string) *Result {
	r, program := parseSource(source, target)
	if program == nil {
		return r
	}
	return checkParsed(r, program, dir, source)
}

// parseSource is the parse phase of CompileAt. It returns the result and the
// parsed program, or a nil program when the target is unsupported or the source
// has syntax diagnostics: the result is then final.
func parseSource(source, target string) (*Result, *Program) {
	start := time.Now()
	hash := sha256.Sum256([]byte(source))
	r := &Result{SchemaVersion: SemanticSchemaVersion, Revision: hex.EncodeToString(hash[:]), Target: target, Diagnostics: []Diagnostic{}, Symbols: []Symbol{}, TypeProjectionBudget: maxTypeProjectionNodes, TypeProjectionLimits: defaultProjectionLimits, facts: map[*Expr]ExpressionFacts{}, sourceBytes: len(source)}
	if target != "go" && target != "js" {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF110", Message: "unsupported target " + target})
		return r, nil
	}
	program, _, comments, diagnostics := parseSyntax(source)
	r.comments = comments
	r.Timings.ParseMicros = time.Since(start).Microseconds()
	if len(diagnostics) > 0 {
		r.Diagnostics = diagnostics
		r.Timings.TotalMicros = r.Timings.ParseMicros
		return r, nil
	}
	return r, program
}

// checkParsed is the check phase of CompileAt: imports, checking and the
// semantic projection of the program parseSource returned. source is the text
// the program was parsed from.
func checkParsed(r *Result, program *Program, dir, source string) *Result {
	start := time.Now()
	r.Program = program
	r.lexical = captureOriginalSyntax(program)
	r.loadImports(dir)
	r.loadBundledImports(source)
	c := newChecker(program, r)
	c.source = source
	checkStart := time.Now()
	c.check()
	c.observeDeclarationSyntax()
	c.publishTypeNodes()
	r.Timings.CheckMicros = time.Since(checkStart).Microseconds()
	r.Timings.TotalMicros = r.Timings.ParseMicros + time.Since(start).Microseconds()
	r.Checked = len(r.Diagnostics) == 0
	return r
}

func newChecker(program *Program, r *Result) *checker {
	c := &checker{
		program:                 program,
		result:                  r,
		functions:               map[string]*Function{},
		services:                map[string]*Service{},
		providers:               map[string]*Provider{},
		records:                 map[string]*Record{},
		enums:                   map[string]*Enum{},
		errors:                  map[string]*ErrorDecl{},
		constants:               map[string]*Constant{},
		constantsByModule:       map[string]map[string]*Constant{},
		typeIntern:              map[string]*semanticTypeNode{},
		rowIntern:               map[string]RowID{},
		nextTypeID:              1,
		nextRowID:               1,
		typePublicIDs:           map[TypeID]string{},
		typePublicToID:          map[string]TypeID{},
		declarationFingerprints: map[string]string{},
		region:                  "invocation",
	}
	c.values = newCheckedValueArena(c)
	resolveBindings(program)
	addBuiltinData(program)
	program.semantic = c
	r.checkedSymbols = map[string]checkedSymbol{}
	r.checkedFunctions = map[*Function]checkedSymbol{}
	r.checkedProviderRoots = map[*Provider]checkedExpression{}
	return c
}

func (c *checker) publishTypeNodes() {
	c.result.projector = c
	c.result.checkedProviders = c.providers
	c.result.checkedServices = c.services
	c.result.canonical = c.canonicalSnapshot()
	projection := c.result.ProjectAllTypes()
	c.result.Types = projection.Types
	c.result.Rows = projection.Rows
	c.result.TypeProjectionUsage = projection.Usage
	c.result.TypeProjectionComplete = projection.Complete
	c.result.TypeProjectionError = projection.Error
}

// pipedArgumentMismatch words the rejection of a piped value. The pipe fills
// the first parameter, so a recipe that reached an ordinary parameter is most
// likely a run the author meant to pipe past; a step of a longer chain is
// anchored at its own |> so the failing step is the one marked. Only wording
// and anchors depend on the pipe.
func (c *checker) pipedArgumentMismatch(call, piped *Expr, value checkedExpression, message string) (string, Span) {
	callee := expressionName(call.Left)
	if value.isEffect() {
		recipe := "..."
		if piped.Kind == "call" {
			recipe = expressionName(piped.Left) + "(...)"
		}
		return message + "; |> binds inside run, so to pipe the result write (run " + recipe + ") |> " + callee + "()", piped.Span
	}
	if piped.Kind == "call" {
		return message + "; piped from " + expressionName(piped.Left) + "(...): " + c.displayTypeID(value.valueID()), call.PipeSpan
	}
	return message, piped.Span
}

func (c *checker) diagnostic(code, message string, span Span) {
	if c.suppressDiagnostics {
		return
	}
	c.result.Diagnostics = append(c.result.Diagnostics, Diagnostic{Code: code, Message: message, Span: span})
}

// scopeResultRecipe reports a recipe in the final position of a scope. The
// scope does not run it; the suggestion wraps the complete expression in one
// explicit run, which the edited program must still pass on recheck.
func (c *checker) scopeResultRecipe(e *Expr) {
	span := e.Extent
	if span.Length == 0 {
		span = e.Span
	}
	diagnostic := Diagnostic{Code: "EF105", Message: "scope result is an unexecuted recipe; use run here to execute it before the scope closes", Span: span}
	if end, ok := c.sourceSpan(span.Offset + span.Length); ok && span.Length > 0 {
		diagnostic.Suggestions = []Suggestion{{Message: "Execute this recipe inside the scope", Edits: []SourceEdit{
			{Span: Span{Offset: span.Offset, Line: span.Line, Column: span.Column}, NewText: "run ("},
			{Span: end, NewText: ")"},
		}}}
	}
	if !c.suppressDiagnostics {
		c.result.Diagnostics = append(c.result.Diagnostics, diagnostic)
	}
}

// sourceSpan is the empty span at a byte offset of the checked source, with
// the lexer's 1-based line and byte column.
func (c *checker) sourceSpan(offset int) (Span, bool) {
	if offset < 0 || offset > len(c.source) {
		return Span{}, false
	}
	prefix := c.source[:offset]
	return Span{Offset: offset, Line: 1 + strings.Count(prefix, "\n"), Column: offset - strings.LastIndex(prefix, "\n")}, true
}

// registerService finalizes every service operation as a checked callable.
// Builtins and source services use the same owner identity so downstream
// semantic consumers do not need a separate intrinsic-service escape hatch.
func (c *checker) registerService(s *Service) {
	for _, f := range s.Methods {
		f.Owner = "service:" + s.Name
		f.Identity = c.declarationIdentity("function", f.Owner, f.Name)
	}
	c.services[s.Name] = s
}

func (c *checker) check() {
	c.checkTemplates()
	names := map[string]bool{}
	for _, s := range builtinServicesFor(c.program) {
		c.registerService(s)
		names[s.Name] = true
	}
	for _, p := range builtinProvidersFor(c.program) {
		c.providers[p.Name] = p
		names[p.Name] = true
	}
	for _, name := range builtinErrors() {
		names[name] = true
		c.errors[name] = &ErrorDecl{Name: name}
	}
	claim := func(name string, span Span) {
		if names[name] {
			c.diagnostic("EF101", "duplicate declaration "+name, span)
		}
		names[name] = true
	}
	claimData := func(name string, span Span) {
		switch name {
		case "string", "bool", "i64", "bytes", "File", "Latch", "Fiber", "Context", "Effect", "Scope", "Exit", "Cause", "never", "invalid":
			c.diagnostic("EF101", "reserved data declaration "+name, span)
		}
		if _, callback := c.builtinCallback(name); callback {
			c.diagnostic("EF101", "reserved data declaration "+name, span)
		}
		claim(name, span)
	}
	for _, imp := range c.program.Imports {
		claim(imp.Alias, imp.Span)
	}
	for _, imp := range c.program.BundledImports {
		claim(imp.Alias, imp.Span)
	}
	c.registerConstants(claim)
	errors := make([]string, 0, len(c.program.Errors))
	for name := range c.program.Errors {
		errors = append(errors, name)
	}
	slices.Sort(errors)
	for _, name := range errors {
		claimData(name, c.program.Errors[name])
	}
	for _, name := range builtinErrors() {
		c.program.Errors[name] = Span{}
	}
	c.admitBundledFailures(claim)
	for _, decl := range c.program.ErrorDecls {
		if c.errors[decl.Name] == nil {
			c.errors[decl.Name] = decl
		}
		c.result.Declarations = append(c.result.Declarations, Declaration{Kind: "error", Name: decl.Name, Identity: c.declarationIdentity("error", "module", decl.Name), Fields: decl.Fields, Span: decl.Span})
	}
	for _, record := range c.program.Records {
		claimData(record.Name, record.Span)
		c.records[record.Name] = record
		if len(record.Parameters) > 0 {
			continue
		}
		c.result.Declarations = append(c.result.Declarations, Declaration{Kind: "record", Source: record.SourceID, Name: record.Name, Identity: c.declarationIdentity("record", "module", record.Name), Fields: record.Fields, Span: record.Span})
	}
	for _, enum := range c.program.Enums {
		claimData(enum.Name, enum.Span)
		c.enums[enum.Name] = enum
		if len(enum.Parameters) > 0 {
			continue
		}
		c.result.Declarations = append(c.result.Declarations, Declaration{Kind: "enum", Source: enum.SourceID, Name: enum.Name, Identity: c.declarationIdentity("enum", "module", enum.Name), Variants: enum.Variants, Span: enum.Span})
	}
	for _, decl := range c.program.ErrorDecls {
		for i := range decl.Fields {
			decl.Fields[i].TypeRef = c.typeRef(decl.Fields[i].Type)
			decl.Fields[i].typeID = c.canonicalRef(decl.Fields[i].TypeRef)
			c.bindSourceSyntax(decl.Fields[i].sourceType, decl.Fields[i].typeID)
		}
	}
	for _, record := range c.program.Records {
		if len(record.Parameters) > 0 {
			continue
		}
		for i := range record.Fields {
			record.Fields[i].TypeRef = c.typeRef(record.Fields[i].Type)
			record.Fields[i].typeID = c.canonicalRef(record.Fields[i].TypeRef)
			c.bindSourceSyntax(record.Fields[i].sourceType, record.Fields[i].typeID)
		}
	}
	for _, enum := range c.program.Enums {
		if len(enum.Parameters) > 0 {
			continue
		}
		for i := range enum.Variants {
			for j := range enum.Variants[i].Fields {
				enum.Variants[i].Fields[j].TypeRef = c.typeRef(enum.Variants[i].Fields[j].Type)
				enum.Variants[i].Fields[j].typeID = c.canonicalRef(enum.Variants[i].Fields[j].TypeRef)
				c.bindSourceSyntax(enum.Variants[i].Fields[j].sourceType, enum.Variants[i].Fields[j].typeID)
			}
		}
	}
	c.resolveDataTemplateLayouts()
	for _, decl := range c.program.ErrorDecls {
		c.validateFields(decl.Fields, decl.Name+"Error", true)
	}
	for _, record := range c.program.Records {
		if len(record.Parameters) > 0 {
			continue
		}
		c.validateFields(record.Fields, record.Name, false)
	}
	for _, enum := range c.program.Enums {
		variants := map[string]bool{}
		for _, variant := range enum.Variants {
			if variants[variant.Name] {
				c.diagnostic("EF101", "duplicate variant "+variant.Name+" in "+enum.Name, variant.Span)
			}
			variants[variant.Name] = true
			if len(enum.Parameters) == 0 {
				c.validateFields(variant.Fields, enum.Name+"."+variant.Name, true)
			}
		}
	}
	c.validateDataLayouts()
	previousRecordFacts := c.recordFacts
	c.recordFacts = true
	c.checkConstants()
	c.recordFacts = previousRecordFacts
	slices.SortStableFunc(c.result.Declarations, func(a, b Declaration) int {
		if offset := a.Span.Offset - b.Span.Offset; offset != 0 {
			return offset
		}
		// Distributed sources retain independent offsets. Equal local spans
		// must not expose map/import traversal order across CLI and MCP.
		return strings.Compare(a.Identity, b.Identity)
	})
	for _, s := range c.program.Services {
		claim(s.Name, s.Span)
		c.registerService(s)
	}
	for _, p := range c.program.Providers {
		for _, f := range p.Methods {
			f.Owner = "provider:" + p.Name
			f.Identity = c.declarationIdentity("function", f.Owner, f.Name)
		}
		claim(p.Name, p.Span)
		c.providers[p.Name] = p
	}
	for _, f := range c.program.Functions {
		f.Module = currentModuleIdentity
		f.SourceID = "source:user"
		f.Owner = "module"
		f.Identity = c.declarationIdentity("function", f.Owner, f.Name)
		claim(f.Name, f.Span)
		c.functions[f.Name] = f
	}
	for _, f := range c.program.BundledFunctions {
		f.Owner = "module"
	}
	for _, layer := range c.program.Layers {
		claim(layer.Name, layer.Span)
	}
	// Builtin operations resolve their signatures like declared ones, so the
	// application plan retains the builtin data their types name.
	for _, s := range builtinServicesFor(c.program) {
		for _, f := range c.services[s.Name].Methods {
			c.signature(f)
		}
	}
	c.deriveCodecs(claim)
	for _, s := range c.program.Services {
		methods := map[string]bool{}
		for _, f := range s.Methods {
			if methods[f.Name] {
				c.diagnostic("EF101", "duplicate method "+f.Name, f.Span)
			}
			methods[f.Name] = true
			c.signature(f)
			c.bindSignatureParameters(f)
			for _, label := range f.Services {
				if !c.rowParameterDeclared(f, label, "uses") {
					c.diagnostic("EF103", "service operation uses may name only its row parameters; "+label+" is a fixed requirement", f.Span)
				}
			}
		}
	}
	for _, f := range c.program.checkedFunctions() {
		c.signature(f)
	}
	if !c.program.interfaceProducer && len(c.program.BundledFunctions)+len(c.program.BundledTemplates) > 0 {
		if err := c.admitBundledSummaries(); err != nil {
			c.diagnostic("EF126", "bundled interface refused: "+err.Error(), Span{})
			return
		}
	}
	for _, p := range c.program.Providers {
		if c.services[p.Service] != nil {
			c.providerSignature(p)
		}
	}
	c.checkLayers()
	if len(c.program.checkedFunctions()) > 0 {
		c.prepareFunctionSummaries()
	}
	for _, p := range c.program.Providers {
		s, exists := c.services[p.Service]
		if !exists {
			c.diagnostic("EF102", "unknown service "+p.Service, p.Span)
			continue
		}
		c.observeReference(p, p.ServiceSpan, lexicalTarget{kind: "service", service: s})
		methods := map[string]*Function{}
		for _, f := range p.Methods {
			if methods[f.Name] != nil {
				c.diagnostic("EF101", "duplicate implementation method "+f.Name, f.Span)
			}
			methods[f.Name] = f
			c.signature(f)
			for _, required := range normalized(f.Services) {
				if !slices.Contains(normalized(p.Services), required) && !c.rowParameterDeclared(f, required, "uses") {
					c.diagnostic("EF103", "provider method "+p.Name+"."+f.Name+" captures undeclared service "+required, f.Span)
				}
			}
			c.providerFunction(p, f)
			// Callers dispatch the service method to whichever provider is
			// in scope: its contract cannot carry a pending child failure.
			c.requireObservedChildren(f, f.Span)
		}
		for _, want := range s.Methods {
			got := methods[want.Name]
			if got == nil {
				c.diagnostic("EF104", "provider "+p.Name+" is missing method "+want.Name, p.Span)
				continue
			}
			if !c.implementsOperation(got, want) {
				c.diagnostic("EF104", "implementation does not satisfy service method "+s.Name+"."+want.Name, got.Span)
			}
		}
		for _, f := range p.Methods {
			found := false
			for _, want := range s.Methods {
				found = found || want.Name == f.Name
			}
			if !found {
				c.diagnostic("EF104", "unexpected implementation method "+f.Name, f.Span)
			}
		}
	}
	for _, f := range c.program.checkedFunctions() {
		c.function(f, true)
	}
	for module, admitted := range c.admittedSummaries {
		functions := []*Function{}
		for _, f := range c.program.BundledFunctions {
			if f.Module == module {
				functions = append(functions, f)
			}
		}
		actual, err := exportInterfaceSummary(c, module, admitted.SourceInput, functions)
		if err != nil || actual.ContentHash != admitted.ContentHash {
			c.diagnostic("EF126", "distributed implementation disagrees with admitted interface summary", Span{})
		} else {
			c.result.BundledInterfaces = append(c.result.BundledInterfaces, BundledInterfaceInfo{Module: module, InterfaceSchema: admitted.InterfaceSchema, OwnershipSchema: admitted.OwnershipSchema, InterfaceHash: admitted.ContentHash, SourceInput: admitted.SourceInput, Producer: admitted.Producer, ImplementationHash: actual.Implementation})
		}
	}
	slices.SortFunc(c.result.BundledInterfaces, func(a, b BundledInterfaceInfo) int { return strings.Compare(a.Module, b.Module) })
	c.publishCodecs()
	c.validateJSDeclarationNames()
}

// implementsOperation compares an implementation method with its operation up
// to row-parameter renaming: the parameters correspond by position and kind,
// the implementation may raise less than the operation and use no row
// parameter the operation does not bind.
func (c *checker) implementsOperation(got, want *Function) bool {
	if !got.Effect || len(got.Params) != len(want.Params) || len(got.RowParameters) != len(want.RowParameters) {
		return false
	}
	bindings := map[string][]string{}
	for i, p := range got.RowParameters {
		if p.Kind != want.RowParameters[i].Kind {
			return false
		}
		bindings[p.ID] = []string{want.RowParameters[i].ID}
	}
	rename := func(id TypeID) TypeID {
		if len(bindings) == 0 {
			return id
		}
		return c.substituteCanonical(id, nil, bindings)
	}
	if rename(got.returnID) != want.returnID {
		return false
	}
	for i := range got.Params {
		// A required choice is part of the operation's call contract, so an
		// implementation must keep each parameter's choice marker.
		if rename(got.Params[i].typeID) != want.Params[i].typeID || got.Params[i].RequiredChoice != want.Params[i].RequiredChoice {
			return false
		}
	}
	if len(difference(c.rowLabels(c.instantiateRow(got.failureID, bindings)), c.rowLabels(want.failureID))) > 0 {
		return false
	}
	for _, label := range c.rowLabels(c.instantiateRow(got.serviceID, bindings)) {
		if _, abstract := c.rowDefinitions[label]; abstract && !slices.Contains(c.rowLabels(want.serviceID), label) {
			return false
		}
	}
	return true
}

// prepareFunctionSummaries computes bounded ownership summaries before the
// diagnostic-producing pass. Calls may refer to a helper declared later in a
// module. A dependency-ordered pass computes acyclic helpers once; only the
// residual cyclic portion uses a fixed point. This keeps unrelated functions
// independent instead of rescanning every function for every declaration.
func (c *checker) prepareFunctionSummaries() {
	previous := c.suppressDiagnostics
	c.suppressDiagnostics = true

	functions := c.program.checkedFunctions()
	known := make(map[string]*Function, len(functions))
	for _, f := range functions {
		if f.Module == currentModuleIdentity {
			known[f.Name] = f
		}
	}
	for alias, bindings := range c.program.BundledBindings {
		for member, f := range bindings {
			known[alias+"."+member] = f
		}
	}
	for name, bindings := range c.program.DerivedBindings {
		for member, f := range bindings {
			known[name+"."+member] = f
		}
	}
	dependents := make(map[*Function][]*Function, len(functions))
	remaining := make(map[*Function]int, len(functions))
	for _, caller := range functions {
		callerKnown := known
		if caller.Module != currentModuleIdentity {
			callerKnown = map[string]*Function{}
			for _, f := range c.program.BundledFunctions {
				if f.Module == caller.Module {
					callerKnown[f.Name] = f
				}
			}
		}
		deps := map[*Function]bool{}
		collectFunctionDependencies(caller.Body, callerKnown, deps)
		remaining[caller] = len(deps)
		for callee := range deps {
			dependents[callee] = append(dependents[callee], caller)
		}
	}

	processed := map[*Function]bool{}
	ready := make([]*Function, 0, len(functions))
	for _, f := range functions {
		if remaining[f] == 0 {
			ready = append(ready, f)
		}
	}
	for head := 0; head < len(ready); head++ {
		f := ready[head]
		c.function(f, false)
		processed[f] = true
		for _, caller := range dependents[f] {
			remaining[caller]--
			if remaining[caller] == 0 {
				ready = append(ready, caller)
			}
		}
	}

	// Recursive groups have no acyclic order. Process the residual graph as a
	// local worklist and revisit a member only when one of its dependencies
	// changes. The summaries are bounded, and the iteration cap keeps a
	// non-monotone summary from monopolizing checking. Facts observed before a
	// cap are retained conservatively so a proven owned result cannot become
	// unknown merely because convergence was inconclusive.
	cycle := make([]*Function, 0)
	cycleSet := map[*Function]bool{}
	for _, f := range functions {
		if !processed[f] {
			cycle = append(cycle, f)
			cycleSet[f] = true
		}
	}
	queue := append([]*Function{}, cycle...)
	queued := map[*Function]bool{}
	for _, f := range queue {
		queued[f] = true
	}
	observedOwnership := map[*Function][][]OwnershipFact{}
	observedCaptures := map[*Function][][]OwnershipFact{}
	// Failure payload evidence ascends from ⊥ like the other summary
	// slots (design §5.2 F18): a member not yet summarized raises its
	// failures with proven-empty payloads, not unknown ones.
	for _, f := range cycle {
		if f.failures == nil {
			f.failures = c.bottomFailures(f)
		}
	}
	maxIterations := len(cycle) * 8
	if maxIterations < 32 {
		maxIterations = 32
	}
	iterations := 0
	for head := 0; head < len(queue) && iterations < maxIterations; head++ {
		f := queue[head]
		queued[f] = false
		beforeOwnership, beforeCaptures := cloneFacts(f.Ownership), cloneFacts(f.Captures)
		beforeFailures := cloneFailures(f.failures)
		beforeCallable := f.returnCallableEvidence
		c.function(f, false)
		observedOwnership[f] = append(observedOwnership[f], beforeOwnership, cloneFacts(f.Ownership))
		observedCaptures[f] = append(observedCaptures[f], beforeCaptures, cloneFacts(f.Captures))
		iterations++
		changed := !slices.Equal(beforeOwnership, f.Ownership) || !slices.Equal(beforeCaptures, f.Captures) || !equalFailures(beforeFailures, f.failures) || beforeCallable != f.returnCallableEvidence
		if !changed {
			continue
		}
		for _, caller := range dependents[f] {
			if !cycleSet[caller] || queued[caller] {
				continue
			}
			queue = append(queue, caller)
			queued[caller] = true
		}
	}
	if iterations >= maxIterations && len(queue) > iterations {
		for _, f := range cycle {
			f.Ownership = conservativeCycleFacts(observedOwnership[f]...)
			f.Captures = conservativeCycleFacts(observedCaptures[f]...)
			// Inconclusive payload evidence is unknown (fail closed): every
			// failure may also come from an unobserved child.
			f.failures = c.topPending(f)
			f.returnCallableEvidence = callableEvidence{unresolved: true}
		}
	}
	c.suppressDiagnostics = previous
}

func collectFunctionDependencies(block *Block, known map[string]*Function, out map[*Function]bool) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectFunctionDependenciesExpr(statement.Value, known, out)
		collectFunctionDependenciesExpr(statement.Payload, known, out)
	}
}

func collectFunctionDependenciesExpr(e *Expr, known map[string]*Function, out map[*Function]bool) {
	if e == nil {
		return
	}
	if e.Kind == "name" && e.binding == nil {
		if f := known[e.Name]; f != nil {
			out[f] = true
		}
	}
	if e.Kind == "member" && e.Left != nil && e.Left.Kind == "name" && e.Left.binding == nil {
		if f := known[e.Left.Name+"."+e.Name]; f != nil {
			out[f] = true
		}
	}
	// Named call arguments are retained in both Args and Fields for source
	// inspection, with the same child pointers in each view. Use the shared
	// traversal seam so summary preparation remains linear in the syntax tree.
	forEachExprChild(e, func(child *Expr) {
		collectFunctionDependenciesExpr(child, known, out)
	})
	for _, arm := range e.Arms {
		collectFunctionDependencies(arm.Body, known, out)
	}
	collectFunctionDependencies(e.Then, known, out)
	collectFunctionDependencies(e.Else, known, out)
}

// providerSignature checks the explicit constructor boundary. Constructor
// parameters are ordinary values; captured services are checked as a distinct
// requirement row and never inferred from an ambient context.
func (c *checker) providerSignature(p *Provider) {
	names := map[string]bool{}
	for i := range p.Params {
		param := &p.Params[i]
		if !c.typeKnown(param.Type) && !c.absentName(absentInType, param.Type, param.TypeSpan) {
			c.diagnostic("EF102", "unknown or unsupported provider configuration type "+param.Type, param.Span)
		}
		param.TypeRef = c.typeRef(param.Type)
		param.typeID = c.canonicalRef(param.TypeRef)
		c.bindSourceSyntax(param.sourceType, param.typeID)
		if names[param.Name] {
			c.diagnostic("EF101", "duplicate provider configuration parameter "+param.Name, param.Span)
		}
		names[param.Name] = true
	}
	for _, name := range normalized(p.Services) {
		if _, exists := c.services[name]; !exists {
			c.diagnostic("EF102", "unknown service "+name, p.Span)
		}
	}
	checked := c.checkedProvider(p, true)
	c.result.checkedProviderRoots[p] = checked
	p.Contract = c.projectCheckedBase(checked)
}

// providerFunction checks a method with the constructor's captured
// requirements in scope. A method may repeat a captured requirement with
// `uses { ... }` for local clarity, but it can never widen the constructor's
// row. The service operation contract remains the service declaration's
// contract, which has no captured construction row. The body is checked
// against canonical identities: the method's declared row (f.serviceID, whose
// row parameters are the method's own binders) plus the constructor's concrete
// captures. Captures are never re-resolved under the method's binders, so a
// binder spelled like a capture cannot widen or hide either.
func (c *checker) providerFunction(p *Provider, f *Function) {
	previousFacts := c.recordFacts
	c.recordFacts = true
	c.functionWithLocals(f, false, p.Params, normalized(p.Services))
	c.recordFacts = previousFacts
}

func (c *checker) validateJSDeclarationNames() {
	if c.result.Target != "js" {
		return
	}
	reserved := map[string]bool{
		"as": true, "asserts": true, "async": true, "await": true, "break": true,
		"case": true, "catch": true, "class": true, "const": true, "continue": true,
		"debugger": true, "default": true, "delete": true, "do": true, "else": true,
		"enum": true, "export": true, "extends": true, "finally": true, "for": true,
		"false": true, "from": true, "function": true, "get": true, "if": true, "implements": true,
		"import": true, "in": true, "infer": true, "instanceof": true, "interface": true,
		"is": true, "keyof": true, "let": true, "module": true, "namespace": true,
		"new": true, "null": true, "number": true, "object": true, "of": true, "package": true, "private": true, "protected": true,
		"public": true, "readonly": true, "return": true, "satisfies": true, "set": true,
		"static": true, "string": true, "super": true, "switch": true, "symbol": true, "throw": true, "this": true, "true": true, "try": true,
		"type": true, "typeof": true, "undefined": true, "unknown": true, "using": true, "var": true,
		"any": true, "bigint": true, "boolean": true, "never": true, "void": true, "while": true, "with": true, "yield": true,
	}
	validateIdentifier := func(name, owner string, span Span) {
		if reserved[name] {
			c.diagnostic("EF110", "JavaScript declaration name "+name+" is reserved by TypeScript for "+owner, span)
		}
	}
	occupied := map[string]string{}
	claim := func(name, owner string, span Span) {
		if previous, exists := occupied[name]; exists {
			c.diagnostic("EF110", "JavaScript declaration name "+name+" collides between "+previous+" and "+owner, span)
			return
		}
		occupied[name] = owner
	}
	for _, declaration := range c.result.Declarations {
		if declaration.Kind == "error" {
			// The raw error name is exported as its payload type alias.
			validateIdentifier(declaration.Name, "error payload type", declaration.Span)
			validateIdentifier(declaration.Name+"Error", "error payload declaration", declaration.Span)
		} else {
			validateIdentifier(declaration.Name, "data declaration", declaration.Span)
		}
		claim(declaration.Name, "data declaration", declaration.Span)
	}
	for _, declaration := range c.result.Declarations {
		if declaration.Kind == "error" {
			claim(declaration.Name+"Error", "error payload declaration", declaration.Span)
		}
	}
	services := append(append([]*Service{}, builtinServicesFor(c.program)...), c.program.Services...)
	for _, service := range services {
		validateIdentifier(service.Name, "service export", service.Span)
		claim(service.Name+"Requirement", "service requirement declaration", service.Span)
		claim(service.Name+"Provider", "service provider declaration", service.Span)
	}
	for _, provider := range c.program.Providers {
		validateIdentifier(provider.Name, "provider export", provider.Span)
	}
	for _, function := range c.program.Functions {
		validateIdentifier(function.Name, "function export", function.Span)
	}
	for _, codec := range c.program.Codecs {
		validateIdentifier(codec.Name, "codec witness export", codec.Span)
	}
}
func (c *checker) signature(f *Function) {
	previousTypes, previousModule := c.typeContext, c.functionModule
	c.typeContext, c.functionModule = c.templateContext(f.TypeParameters, f.Identity), f.Module
	defer func() { c.typeContext, c.functionModule = previousTypes, previousModule }()
	if len(f.TypeParameters) > 0 && operationOwner(f.Owner) {
		c.diagnostic("EF125", "type parameters are unsupported on service operations and implementation methods; only row parameters are admitted", f.Span)
	} else if len(f.TypeParameters) > 0 && (f.Module == "" || f.Module == currentModuleIdentity) {
		c.diagnostic("EF127", "user generic functions are unavailable; use an explicit compiler-distributed declaration", f.Span)
	}
	previous := c.rowContext
	c.rowContext = c.functionRows(f)
	defer func() { c.rowContext = previous }()
	valid := func(t string, span, typeSpan Span, known func(string) bool) {
		if c.requiresTemplateArguments(t) {
			c.diagnostic("EF127", "generic type "+t+" requires complete application arguments", span)
		} else if !known(t) && !c.absentName(absentInType, t, typeSpan) {
			message := "unknown or unsupported value type " + t
			if t == "unit" {
				message += "; use void for no-value results"
			}
			c.diagnostic("EF102", message, span)
		}
	}
	valid(f.Return, f.Span, f.ReturnSpan, c.typeKnown)
	f.returnID = c.canonicalRef(typeRef(f.Return))
	c.bindSourceSyntax(f.returnType, f.returnID)
	names := map[string]bool{}
	for i := range f.Params {
		p := &f.Params[i]
		valid(p.Type, p.Span, p.TypeSpan, c.parameterTypeKnown)
		p.TypeRef = c.typeRef(p.Type)
		p.typeID = c.canonicalRef(p.TypeRef)
		c.bindSourceSyntax(p.sourceType, p.typeID)
		c.checkParameterContract(f, p)
		if names[p.Name] {
			c.diagnostic("EF101", "duplicate parameter "+p.Name, p.Span)
		}
		names[p.Name] = true
	}
	for _, name := range normalized(f.Errors) {
		if c.rowParameter(name, "raises") {
			continue
		}
		if _, exists := c.program.Errors[name]; !exists {
			c.diagnostic("EF102", "unknown failure "+name, f.Span)
		}
	}
	for _, name := range normalized(f.Services) {
		if c.rowParameter(name, "uses") {
			continue
		}
		if _, exists := c.services[name]; !exists {
			c.diagnostic("EF102", "unknown service "+name, f.Span)
		}
	}
	if !f.Effect && (len(f.Errors) > 0 || len(f.Services) > 0) {
		c.diagnostic("EF103", "ordinary functions cannot declare effect rows", f.Span)
	}
	f.failureID = c.internRow(c.sourceRow(f.Errors, "raises"))
	f.serviceID = c.internRow(c.sourceRow(f.Services, "uses"))
	f.signatureChecked = true
	c.validateRowInference(f)
}
func (c *checker) typeKnown(name string) bool {
	if _, known := c.typeContext[name]; known {
		return true
	}
	if c.program != nil && c.program.typeExpressions[name] != nil {
		return c.sourceCallableKnown(c.program.typeExpressions[name])
	}
	if c.requiresTemplateArguments(name) {
		return false
	}
	switch name {
	case "string", "bool", voidTypeName, "i64", "File", "Latch", "bytes":
		return true
	}
	if _, callback := c.builtinCallback(name); callback {
		return true
	}
	if c.records[name] != nil || c.enums[name] != nil {
		return true
	}
	return c.hostAnnotation(name) != invalidTypeID
}

// parameterTypeKnown admits a declared error as its nominal payload type, in
// addition to ordinary values, where a function receives one: a recovery
// handler parameter. Errors remain unavailable as stored or returned success
// values, and builtin failures have no source payload declaration.
func (c *checker) parameterTypeKnown(name string) bool {
	return c.typeKnown(name) || c.errors[name] != nil && !slices.Contains(builtinErrors(), name)
}

func (c *checker) requiresTemplateArguments(name string) bool {
	template := c.templateByName(name)
	return template != nil && len(template.Parameters) > 0
}
func (c *checker) typeNodeID(id TypeID) string {
	if id == invalidTypeID {
		return ""
	}
	if c.typePublicIDs == nil {
		c.typePublicIDs = map[TypeID]string{}
	}
	if c.typePublicToID == nil {
		c.typePublicToID = map[string]TypeID{}
	}
	if publicID, ok := c.typePublicIDs[id]; ok {
		return publicID
	}
	node := c.node(id)
	if node == nil {
		return ""
	}
	var publicID func(TypeID, map[TypeID]bool) string
	publicID = func(current TypeID, visiting map[TypeID]bool) string {
		if cached, ok := c.typePublicIDs[current]; ok {
			return cached
		}
		if visiting[current] {
			return "cycle"
		}
		currentNode := c.node(current)
		if currentNode == nil {
			return "invalid"
		}
		visiting[current] = true
		var key strings.Builder
		key.WriteString(currentNode.Kind)
		key.WriteByte(':')
		key.WriteString(currentNode.Name)
		if currentNode.Declaration != "" {
			key.WriteString("#")
			key.WriteString(currentNode.Declaration)
		}
		if currentNode.Mode != "" {
			key.WriteString("~")
			key.WriteString(currentNode.Mode)
		}
		for _, arg := range currentNode.Args {
			key.WriteByte('[')
			key.WriteString(publicID(arg, visiting))
			key.WriteByte(']')
		}
		if currentNode.Result != invalidTypeID {
			key.WriteString("->")
			key.WriteString(publicID(currentNode.Result, visiting))
		}
		if currentNode.FailureRow != emptyRowID {
			key.WriteString("!")
			key.WriteString(rowIdentityKey(c.rowLabels(currentNode.FailureRow)))
		}
		if currentNode.ServiceRow != emptyRowID {
			key.WriteString("?")
			key.WriteString(rowIdentityKey(c.rowLabels(currentNode.ServiceRow)))
		}
		delete(visiting, current)
		sum := sha256.Sum256([]byte(key.String()))
		prefix := "t:"
		if currentNode.Kind == "callable" || currentNode.Kind == "recipe" || currentNode.Kind == "providerRecipe" {
			prefix = "v:"
		}
		result := prefix + hex.EncodeToString(sum[:8])
		c.typePublicIDs[current] = result
		c.typePublicToID[result] = current
		return result
	}
	return publicID(id, map[TypeID]bool{})
}

func rowNodeIDForLabels(labels []string) string {
	sum := sha256.Sum256([]byte(rowIdentityKey(labels)))
	return "r:" + hex.EncodeToString(sum[:8])
}

func (c *checker) rowNodeID(id RowID) string {
	if id == emptyRowID {
		return ""
	}
	return rowNodeIDForLabels(c.rowLabels(id))
}

func (c *checker) node(id TypeID) *semanticTypeNode {
	if id == invalidTypeID || int(id) > len(c.typeNodes) {
		return nil
	}
	return c.typeNodes[id-1]
}

func (c *checker) rowLabels(id RowID) []string {
	return append([]string(nil), c.retainedRowLabels(id)...)
}

func (c *checker) retainedRowLabels(id RowID) []string {
	if id == emptyRowID || int(id) > len(c.rows) {
		return nil
	}
	return c.rows[id-1].Labels
}

func rowIdentityKey(labels []string) string {
	return strings.Join(normalized(labels), "\x00")
}

func (c *checker) internRow(labels []string) RowID {
	labels = normalized(labels)
	if len(labels) == 0 {
		return emptyRowID
	}
	key := rowIdentityKey(labels)
	if id, ok := c.rowIntern[key]; ok {
		return id
	}
	id := c.nextRowID
	c.nextRowID++
	c.rowIntern[key] = id
	row := RowNode{ID: rowNodeIDForLabels(labels), Labels: append([]string{}, labels...)}
	for _, label := range labels {
		if parameter, ok := c.rowDefinitions[label]; ok {
			row.Parameters = append(row.Parameters, parameter)
		}
	}
	c.rows = append(c.rows, row)
	return id
}

func (c *checker) canonicalRef(ref TypeRef) TypeID {
	if ref.Scope != "" && c.result != nil && ref.Scope != c.result.Revision {
		return invalidTypeID
	}
	// Canonical projections are admitted by their same-arena public ID. The
	// visible kind/name and one-hop Args are inspection projections only; using
	// them to rebuild a nested reference would silently truncate a shared DAG.
	// Legacy source/display refs are the sole compatibility exception.
	if ref.ID != "" && !strings.HasPrefix(ref.ID, "legacy:") {
		if id, ok := c.typePublicToID[ref.ID]; ok {
			return id
		}
		return invalidTypeID
	}
	if parameter, known := c.typeContext[ref.Name]; known {
		return parameter.typeID
	}
	if c.program != nil && c.program.typeExpressions[ref.Name] != nil {
		return c.sourceCallable(c.program.typeExpressions[ref.Name])
	}
	if callback, ok := c.builtinCallback(ref.Name); ok {
		parameterID, resultID := c.canonicalRef(typeRef(callback.Parameter)), c.canonicalRef(typeRef(callback.Result))
		if parameterID == invalidTypeID || resultID == invalidTypeID {
			return invalidTypeID
		}
		return c.internContract("callable", "effect", resultID, []TypeID{parameterID}, emptyRowID, emptyRowID)
	}
	argIDs := []TypeID(nil)
	if !strings.HasPrefix(ref.ID, "legacy:") {
		argIDs = make([]TypeID, len(ref.ArgIDs))
		for i, argID := range ref.ArgIDs {
			id, ok := c.typePublicToID[argID]
			if !ok {
				return invalidTypeID
			}
			argIDs[i] = id
		}
	}
	if ref.Kind == "" {
		if ref.Name == "" {
			return invalidTypeID
		}
		ref = typeRef(ref.Name)
	}
	if _, reserved := builtinCallbacks[ref.Name]; reserved && ref.Kind == "opaque" {
		// typeRef spells every callback name opaque without knowing the
		// program. Where builtinCallback does not admit it, the name is a
		// user declaration and resolves like any other named type.
		if _, admitted := c.builtinCallback(ref.Name); !admitted {
			ref.Kind = "named"
		}
	}
	if ref.Kind == "named" {
		switch {
		case c.records[ref.Name] != nil:
			ref.Kind = "record"
		case c.enums[ref.Name] != nil:
			ref.Kind = "enum"
		case c.errors[ref.Name] != nil:
			ref.Kind = "error"
		}
	}
	// A nominal reference is meaningful only when its declaration is admitted
	// by this checker. Reconstructing an unknown record/error/enum/provider from
	// its display fields would create a phantom type which happens to look like
	// a source declaration but has no declaration authority.
	switch ref.Kind {
	case "record":
		if c.records[ref.Name] == nil || len(c.records[ref.Name].Parameters) > 0 {
			return invalidTypeID
		}
	case "enum":
		if c.enums[ref.Name] == nil || len(c.enums[ref.Name].Parameters) > 0 {
			return invalidTypeID
		}
	case "error":
		if c.errors[ref.Name] == nil {
			return invalidTypeID
		}
	case "provider":
		known := false
		for _, provider := range c.providers {
			if provider != nil && provider.Service == ref.Name {
				known = true
				break
			}
		}
		if !known {
			return invalidTypeID
		}
	case "primitive":
		if !slices.Contains([]string{"string", "bool", "i64", "bytes", voidTypeName}, ref.Name) {
			return invalidTypeID
		}
	case "opaque":
		if _, callback := c.builtinCallback(ref.Name); !callback && !slices.Contains([]string{"File", "Latch"}, ref.Name) {
			return invalidTypeID
		}
	case "named":
		return c.hostAnnotation(ref.Name)
	}
	args := argIDs
	if len(args) == 0 && len(ref.Args) > 0 {
		args = make([]TypeID, len(ref.Args))
		for i := range ref.Args {
			args[i] = c.canonicalRef(ref.Args[i])
			if args[i] == invalidTypeID {
				return invalidTypeID
			}
		}
	}
	declaration := ref.Declaration
	if declaration == "" {
		declaration = c.declarationQualifier(ref.Kind, ref.Name)
	}
	return c.internTypeWithDeclaration(ref.Kind, ref.Name, args, declaration)
}

func (c *checker) internType(kind, name string, args []TypeID) TypeID {
	return c.internTypeWithDeclaration(kind, name, args, c.declarationQualifier(kind, name))
}

const currentModuleIdentity = "module:file"

// declarationQualifier is intentionally bounded: nominal identity uses the
// current file sentinel, declaration owner/name, and a shallow declaration
// fingerprint. It never recursively expands a type graph or uses the mutable
// semantic revision as an identity component.
func (c *checker) declarationQualifier(kind, name string) string {
	if kind != "record" && kind != "enum" && kind != "error" && kind != "provider" {
		return ""
	}
	fingerprint := c.declarationFingerprint(kind, name)
	return kind + ":" + currentModuleIdentity + ":" + name + ":" + fingerprint
}

func (c *checker) declarationIdentity(kind, owner, name string) string {
	return kind + ":" + currentModuleIdentity + ":" + owner + ":" + name
}

func (c *checker) declarationFingerprint(kind, name string) string {
	key := kind + "\x00" + name
	if c.declarationFingerprints == nil {
		c.declarationFingerprints = map[string]string{}
	}
	if fingerprint, ok := c.declarationFingerprints[key]; ok {
		return fingerprint
	}
	var b strings.Builder
	b.WriteString(kind)
	b.WriteByte(':')
	b.WriteString(name)
	appendField := func(field Field) {
		b.WriteByte('|')
		b.WriteString(field.Name)
		b.WriteByte(':')
		b.WriteString(field.Type)
	}
	switch kind {
	case "record":
		if declaration := c.records[name]; declaration != nil {
			for _, field := range declaration.Fields {
				appendField(field)
			}
		}
	case "enum":
		if declaration := c.enums[name]; declaration != nil {
			for _, variant := range declaration.Variants {
				b.WriteByte('|')
				b.WriteString(variant.Name)
				for _, field := range variant.Fields {
					appendField(field)
				}
			}
		}
	case "error":
		if declaration := c.errors[name]; declaration != nil {
			for _, field := range declaration.Fields {
				appendField(field)
			}
		}
	case "provider":
		if declaration := c.providers[name]; declaration != nil {
			b.WriteByte('|')
			b.WriteString(declaration.Service)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	fingerprint := hex.EncodeToString(sum[:8])
	c.declarationFingerprints[key] = fingerprint
	return fingerprint
}

func semanticNodeKey(kind, name, declaration, mode string, args []TypeID, result TypeID, failure, service RowID) string {
	// Every checked expression interns its type, so the key is built with
	// appends rather than fmt; the spelling is unchanged.
	key := make([]byte, 0, 72+len(kind)+len(name)+len(declaration)+len(mode)+4*len(args))
	writeString := func(label, value string) {
		key = append(key, label...)
		key = strconv.AppendInt(key, int64(len(value)), 10)
		key = append(key, ':')
		key = append(key, value...)
		key = append(key, ';')
	}
	writeString("kind", kind)
	writeString("name", name)
	writeString("decl", declaration)
	writeString("mode", mode)
	key = append(key, "args"...)
	key = strconv.AppendInt(key, int64(len(args)), 10)
	key = append(key, ':')
	for _, arg := range args {
		key = strconv.AppendUint(key, uint64(arg), 10)
		key = append(key, ',')
	}
	key = append(key, ";result"...)
	key = strconv.AppendUint(key, uint64(result), 10)
	key = append(key, ";failure"...)
	key = strconv.AppendUint(key, uint64(failure), 10)
	key = append(key, ";service"...)
	key = strconv.AppendUint(key, uint64(service), 10)
	key = append(key, ';')
	return string(key)
}

// internSemanticNode is the sole structural interner for data types, value
// contracts, and row-carrying contracts. Constructors may choose different
// semantic fields, but they cannot create a duplicate node for the same full
// structure through a second key namespace.
func (c *checker) internSemanticNode(kind, name, declaration, mode string, args []TypeID, result TypeID, failure, service RowID) TypeID {
	key := semanticNodeKey(kind, name, declaration, mode, args, result, failure, service)
	return c.internSemanticNodeWithKey(key, kind, name, declaration, mode, args, result, failure, service)
}

func (c *checker) internSemanticNodeWithKey(key, kind, name, declaration, mode string, args []TypeID, result TypeID, failure, service RowID) TypeID {
	if node, ok := c.typeIntern[key]; ok {
		return node.ID
	}
	id := c.nextTypeID
	c.nextTypeID++
	node := &semanticTypeNode{
		ID:          id,
		Kind:        kind,
		Name:        name,
		Declaration: declaration,
		Mode:        mode,
		Args:        append([]TypeID{}, args...),
		Result:      result,
		FailureRow:  failure,
		ServiceRow:  service,
	}
	c.typeIntern[key] = node
	c.typeNodes = append(c.typeNodes, node)
	return id
}

func (c *checker) internTypeWithDeclaration(kind, name string, args []TypeID, declaration string) TypeID {
	return c.internSemanticNode(kind, name, declaration, "", args, invalidTypeID, emptyRowID, emptyRowID)
}

// refForID preserves the legacy one-hop Args shape for existing tooling. ArgIDs
// and Result.Types remain the authoritative complete graph, so this function
// does not recursively clone grandchildren or repeated DAG branches.
func (c *checker) refForID(id TypeID, depth int) TypeRef {
	if id == invalidTypeID {
		return TypeRef{Kind: "invalid"}
	}
	node := c.node(id)
	if node == nil {
		return TypeRef{Kind: "invalid"}
	}
	ref := TypeRef{ID: c.typeNodeID(id), Kind: node.Kind, Name: node.Name, Declaration: node.Declaration}
	if c.result != nil {
		ref.Scope = c.result.Revision
	}
	for _, arg := range node.Args {
		ref.ArgIDs = append(ref.ArgIDs, c.typeNodeID(arg))
		if depth == 0 {
			ref.Args = append(ref.Args, c.shallowRefForID(arg))
		}
	}
	if node.Result != invalidTypeID {
		ref.Result = c.typeNodeID(node.Result)
	}
	if node.FailureRow != emptyRowID {
		ref.FailureRow = c.rowNodeID(node.FailureRow)
	}
	if node.ServiceRow != emptyRowID {
		ref.ServiceRow = c.rowNodeID(node.ServiceRow)
	}
	return ref
}

func (c *checker) shallowRefForID(id TypeID) TypeRef {
	if id == invalidTypeID {
		return TypeRef{Kind: "invalid"}
	}
	node := c.node(id)
	if node == nil {
		return TypeRef{Kind: "invalid"}
	}
	ref := TypeRef{ID: c.typeNodeID(id), Kind: node.Kind, Name: node.Name, Declaration: node.Declaration}
	if c.result != nil {
		ref.Scope = c.result.Revision
	}
	return ref
}

func (c *checker) ref(id TypeID) TypeRef { return c.refForID(id, 0) }

func (c *checker) displayTypeID(id TypeID) string {
	node := c.node(id)
	if node == nil {
		return "invalid"
	}
	switch node.Kind {
	case "never":
		return "never"
	case "invalid":
		return "invalid"
	case "fiber":
		if len(node.Args) == 1 {
			return "Fiber:" + c.displayTypeID(node.Args[0])
		}
	case "goResult":
		if len(node.Args) == 1 {
			return "GoResult:" + c.displayTypeID(node.Args[0])
		}
	case "goValues":
		components := make([]string, len(node.Args))
		for i, arg := range node.Args {
			components[i] = c.displayTypeID(arg)
		}
		return "(" + strings.Join(components, ", ") + ")"
	case "provider":
		return "provider:" + node.Name
	case "recipe":
		return recipeDisplay(c.displayTypeID(node.Result), c.rowLabels(node.FailureRow), c.rowLabels(node.ServiceRow))
	}
	return node.Name
}

func (c *checker) projectEvaluation(e ExpressionEvaluation) EvaluationRows {
	return EvaluationRows{Failures: c.retainedRowLabels(e.failureRowID()), Requirements: c.retainedRowLabels(e.serviceRowID())}
}

func (c *checker) projectChecked(e checkedExpression) ValueType {
	v := c.projectCheckedBase(e)
	if _, err := c.checkedCompatibilitySize(e, v, defaultProjectionLimits.CompatibilityBytes); err != nil {
		v.ProjectionError = err.Error()
		return v
	}
	if e.callableDecl != nil {
		callable := callableIdentity(c, e.callableDecl)
		callable.Failures = c.rowLabels(e.failureRow())
		callable.Requirements = c.rowLabels(e.serviceRow())
		callable.Result = c.ref(e.resultID())
		callable.Signature = c.typeNodeID(e.contractID())
		v.Callable = callable
	}
	if e.application != nil {
		application := *e.application
		application.Arguments = append([]TypeRef{}, e.application.Arguments...)
		application.Result.Args = append([]TypeRef{}, e.application.Result.Args...)
		application.Result.ArgIDs = append([]string{}, e.application.Result.ArgIDs...)
		if e.application.ProducedResult != nil {
			produced := *e.application.ProducedResult
			produced.Args = append([]TypeRef{}, produced.Args...)
			produced.ArgIDs = append([]string{}, produced.ArgIDs...)
			application.ProducedResult = &produced
		}
		v.Application = &application
	}
	shapeID := e.resultID()
	if e.kind() == checkedFiberValue {
		shapeID = e.contractID()
	}
	v.Type = c.ref(shapeID)
	v.Contract = c.ref(e.contractID())
	v.Errors = c.rowLabels(e.failureRow())
	v.Services = c.rowLabels(e.serviceRow())
	v.Evaluation = EvaluationRows{Failures: c.rowLabels(e.evaluation.failureRowID()), Requirements: c.rowLabels(e.evaluation.serviceRowID())}
	v.Child = cloneFacts(e.child)
	return v
}

// projectCheckedBase is the compact compatibility view used by internal lint,
// graph classification and ownership consumers. Complete expanded metadata is
// materialized only for bounded selected queries or declaration publication.
func (c *checker) projectCheckedBase(e checkedExpression) ValueType {
	resultID := e.resultID()
	contractID := e.contractID()
	if resultID == invalidTypeID || contractID == invalidTypeID {
		return c.projectCheckedBase(c.checkedData("invalid"))
	}
	contract := c.identityRef(contractID)
	shapeID := resultID
	if e.kind() == checkedFiberValue {
		shapeID = contractID
	}
	successID := resultID
	if e.kind() == checkedFiberValue {
		successID = contractID
	}
	v := ValueType{
		Success: c.displayTypeID(successID),
		// Type is the value shape used by compatibility clients. A Fiber keeps
		// its wrapper shape; recipes expose their eventual result shape while
		// Contract carries the complete deferred contract.
		Type:     c.identityRef(shapeID),
		Contract: contract,
		Identity: e.identity,
		// Compatibility clients see an effect callable's mode even when it is
		// carried through a parameter or field. Execution still requires the
		// separate canonical recipe category used by isEffect.
		Effect:     e.isEffect() || (e.node() != nil && e.node().Kind == "callable" && e.node().Mode == "effect"),
		Errors:     c.retainedRowLabels(e.failureRow()),
		Services:   c.retainedRowLabels(e.serviceRow()),
		FailureRow: c.rowNodeID(e.failureRow()),
		ServiceRow: c.rowNodeID(e.serviceRow()),
		Evaluation: c.projectEvaluation(e.evaluation),
		Ownership:  e.ownershipFacts(),
		Captures:   e.captureFacts(),
		Child:      e.child,
	}
	if v.Identity == "" {
		v.Identity = c.typeNodeID(contractID)
	}
	return v
}

func (c *checker) identityRef(id TypeID) TypeRef {
	n := c.node(id)
	if n == nil {
		return TypeRef{Kind: "invalid"}
	}
	return TypeRef{ID: c.typeNodeID(id), Kind: n.Kind, Name: n.Name, Declaration: n.Declaration, Scope: c.result.Revision}
}

func (c *checker) recontractRows(e checkedExpression, failure, service RowID) CheckedValue {
	node := e.node()
	if node == nil {
		return e.value
	}
	switch node.Kind {
	case "recipe":
		return c.values.recipe(node.Result, e.value.callableKind(), failure, service, e.ownershipFacts(), e.captureFacts())
	case "providerRecipe":
		return c.values.providerRecipe(node.Result, failure, service, e.ownershipFacts(), e.captureFacts())
	case "callable":
		return c.values.callable(node.Result, append([]TypeID{}, node.Args...), e.value.callableKind(), failure, service, e.ownershipFacts(), e.captureFacts())
	case "fiber":
		return c.values.fiber(e.resultID(), failure, e.ownershipFacts(), e.captureFacts())
	case "provider":
		return c.values.provider(e.contractID(), e.ownershipFacts(), e.captureFacts())
	default:
		if failure == emptyRowID && service == emptyRowID {
			return c.values.data(e.resultID(), e.ownershipFacts(), e.captureFacts())
		}
		return c.values.recipe(e.resultID(), checkedEffectCallable, failure, service, e.ownershipFacts(), e.captureFacts())
	}
}

func (c *checker) invocationContract(e checkedExpression, effect bool) checkedExpression {
	if !effect {
		return e
	}
	// A returned callable carries its own signature and rows. Evaluation of
	// the factory is a separate contract whose result is that complete value,
	// not the result of invoking the returned callable.
	if node := e.node(); node != nil && node.Kind == "callable" {
		failure, service := e.evaluation.failureRowID(), e.evaluation.serviceRowID()
		if failure != emptyRowID || service != emptyRowID {
			e.value = c.values.recipe(e.valueID(), checkedEffectCallable, failure, service, e.ownershipFacts(), e.captureFacts())
			// Named callable metadata describes the returned value, not this
			// enclosing invocation. Evidence remains available for substitution.
			e.callableDecl = nil
			e.identity = ""
			e.application = nil
		}
		return e
	}
	if node := e.node(); node != nil && node.Kind == "recipe" {
		// A returned recipe is data. Executing the enclosing function yields
		// that complete recipe; its own rows are not the function body's rows.
		failure, service := e.evaluation.failureRowID(), e.evaluation.serviceRowID()
		e.value = c.values.recipe(e.valueID(), checkedEffectCallable, failure, service, e.ownershipFacts(), e.captureFacts())
		e.callableDecl = nil
		e.identity = ""
		e.application = nil
		return e
	}
	e.value = c.recontractRows(e, e.evaluation.failureRowID(), e.evaluation.serviceRowID())
	return e
}

// singleFailureFieldType is the declared field of a one-field failure, used
// by the `fail Error(value)` shorthand.
func (c *checker) singleFailureFieldType(name string) TypeID {
	if decl := c.errors[name]; decl != nil && len(decl.Fields) == 1 {
		return decl.Fields[0].typeID
	}
	return invalidTypeID
}

// recipeType reports whether id is a declared recipe value contract.
func (c *checker) recipeType(id TypeID) bool {
	node := c.node(id)
	return node != nil && node.Kind == "recipe"
}

func (c *checker) typeRef(name string) TypeRef {
	// Source declarations retain identity references. Structural argument
	// arrays belong to an admitted public projection, not to signature checks.
	return c.identityRef(c.canonicalRef(typeRef(name)))
}

func (c *checker) sameType(actual checkedExpression, expected string) bool {
	expectedID := c.canonicalRef(typeRef(expected))
	return c.assignable(actual.valueID(), expectedID, 0)
}

// handlerCompatible is the one narrow source compatibility relation for the
// builtin callback parameters of Http operations. It accepts an effectful
// callable whose actual source signature is exactly the callback's; it does
// not turn the callback into a general function type or erase its own rows.
func (c *checker) handlerCompatible(actual checkedExpression, expected string) bool {
	callback, ok := c.builtinCallback(expected)
	if !ok {
		return false
	}
	node := actual.node()
	if node == nil || node.Kind != "callable" || node.Mode != "effect" || len(node.Args) != 1 {
		return false
	}
	return node.Args[0] == c.canonicalRef(typeRef(callback.Parameter)) && node.Result == c.canonicalRef(typeRef(callback.Result))
}

func (c *checker) sameValues(actual, expected checkedExpression) bool {
	return c.sameContract(actual, expected)
}

func (c *checker) sameResultType(actual, expected checkedExpression) bool {
	return actual.valueID() == expected.resultID() || c.isNeverValue(actual)
}

// expect reports a failed consumer check unless one of its operands is
// already invalid, whose own diagnostic is then the only report. It returns
// whether the check passed. Consumer checks go through expect so a new
// consumer cannot cascade (lane E2 R5 design §7.2).
func (c *checker) expect(ok bool, code, message string, span Span, operands ...checkedExpression) bool {
	if !ok && !c.poisoned(operands...) {
		c.diagnostic(code, message, span)
	}
	return ok
}

// poisoned reports an operand whose type is the already-reported invalid
// type. Like Gleam's typed invalid expression, it is accepted silently by
// every later check that depends on it, so one rejected expression yields
// one diagnostic; checks independent of it still report.
func (c *checker) poisoned(values ...checkedExpression) bool {
	for _, value := range values {
		if c.isKind(value, "invalid") {
			return true
		}
	}
	return false
}

func (c *checker) isNeverValue(value checkedExpression) bool {
	node := value.node()
	return node != nil && node.Kind == "never"
}

func (c *checker) isNeverResult(value checkedExpression) bool {
	node := value.node()
	if node == nil {
		return false
	}
	if node.Kind == "callable" || node.Kind == "recipe" || node.Kind == "providerRecipe" {
		result := c.node(node.Result)
		return result != nil && result.Kind == "never"
	}
	return node.Kind == "never"
}

// sameContract compares complete checked values. Result compatibility is a
// separate relation because a recipe and its eventual result can share a
// success type while carrying entirely different rows and execution state.
func (c *checker) sameContract(actual, expected checkedExpression) bool {
	if c.isNeverResult(actual) || c.isNeverResult(expected) || c.poisoned(actual, expected) {
		return true
	}
	if actual.valueID() == expected.valueID() {
		return true
	}
	left, right := actual.node(), expected.node()
	if left == nil || right == nil || left.Kind != right.Kind {
		return false
	}
	// Deferred recipes carry rows that are joined at control-flow boundaries.
	// Their result and callable shape must agree; row differences are retained
	// by joinContractRows instead of rejecting an already admitted branch.
	switch left.Kind {
	case "recipe", "providerRecipe", "callable":
		return left.Mode == right.Mode && left.Result == right.Result && slices.Equal(left.Args, right.Args)
	case "fiber":
		// Failure rows are carried by a Fiber handle and can be joined at a
		// branch boundary. Its result shape must remain identical; a Fiber is
		// never admitted as the result value itself.
		return slices.Equal(left.Args, right.Args) && left.ServiceRow == right.ServiceRow
	default:
		return false
	}
}

func (c *checker) joinContractRows(base, other checkedExpression) checkedExpression {
	base.callableEvidence = joinCallableEvidence(base.callableEvidence, other.callableEvidence)
	if base.callableDecl != other.callableDecl {
		base.callableDecl = nil
	}
	if !c.sameContract(base, other) {
		return base
	}
	node := base.node()
	if node == nil || (node.Kind != "recipe" && node.Kind != "providerRecipe" && node.Kind != "callable" && node.Kind != "fiber") {
		return base
	}
	failure := c.internRow(union(c.rowLabels(base.failureRow()), c.rowLabels(other.failureRow())))
	service := c.internRow(union(c.rowLabels(base.serviceRow()), c.rowLabels(other.serviceRow())))
	base.value = c.recontractRows(base, failure, service)
	return base
}

func (c *checker) isKind(t checkedExpression, kind string) bool {
	node := t.node()
	if node != nil && node.Kind == kind {
		return true
	}
	result := c.resultNode(t)
	return result != nil && result.Kind == kind
}

// resultNode is the explicit result relation for deferred callable values.
// Fibers intentionally do not participate: their result type is available
// only through join/run, while the handle remains a distinct value contract.
func (c *checker) resultNode(t checkedExpression) *semanticTypeNode {
	node := t.node()
	if node == nil {
		return nil
	}
	if node.Kind != "callable" && node.Kind != "recipe" && node.Kind != "providerRecipe" {
		return node
	}
	if node.Result == invalidTypeID {
		return nil
	}
	return c.node(node.Result)
}

func (c *checker) namedType(t checkedExpression) string {
	node := t.node()
	if node == nil {
		return ""
	}
	if node.Kind == "record" || node.Kind == "enum" || node.Kind == "named" {
		return node.Name
	}
	return ""
}

func (c *checker) hasRow(t checkedExpression, failure bool, label string) bool {
	id := c.carriedRowID(t, failure)
	return slices.Contains(c.rowLabels(id), label)
}

func (c *checker) carriedRowID(t checkedExpression, failure bool) RowID {
	if failure {
		return t.failureRow()
	}
	return t.serviceRow()
}

func (c *checker) rowDifference(actual, allowed []string) []string {
	actualID := c.internRow(actual)
	allowedID := c.internRow(allowed)
	return difference(c.rowLabels(actualID), c.rowLabels(allowedID))
}

func (c *checker) internContract(kind, mode string, result TypeID, args []TypeID, failure, service RowID) TypeID {
	name := kind
	key := semanticNodeKey(kind, "", "", mode, args, result, failure, service)
	if node, ok := c.typeIntern[key]; ok {
		return node.ID
	}
	if kind == "callable" {
		publicKey := "callable\x00" + mode + "\x00" + c.typeNodeID(result)
		for _, arg := range args {
			publicKey += "\x00" + c.typeNodeID(arg)
		}
		publicKey += "\x00!" + rowIdentityKey(c.rowLabels(failure)) + "\x00?" + rowIdentityKey(c.rowLabels(service))
		sum := sha256.Sum256([]byte(publicKey))
		name = "signature:" + hex.EncodeToString(sum[:8])
	}
	return c.internSemanticNodeWithKey(key, kind, name, "", mode, args, result, failure, service)
}

func (c *checker) internTypeWithRows(kind, name string, args []TypeID, failure, service RowID) TypeID {
	return c.internSemanticNode(kind, name, "", "", args, invalidTypeID, failure, service)
}

// publicValue defensively copies a boundary projection. Semantic relations
// never consume this value; they operate on CheckedValue/ExpressionEvaluation
// and project only once the query or public declaration crosses the boundary.
func publicValue(t ValueType) ValueType {
	cloneStrings := func(values []string) []string {
		if values == nil {
			return nil
		}
		return append([]string{}, values...)
	}
	t.Errors = cloneStrings(t.Errors)
	t.Services = cloneStrings(t.Services)
	t.Ownership = cloneFacts(t.Ownership)
	t.Captures = cloneFacts(t.Captures)
	t.Child = cloneFacts(t.Child)
	t.Evaluation = EvaluationRows{
		Failures:     cloneStrings(t.Evaluation.Failures),
		Requirements: cloneStrings(t.Evaluation.Requirements),
	}
	if t.Callable != nil {
		callable := *t.Callable
		callable.Parameters = publicParams(callable.Parameters)
		callable.Failures = cloneStrings(callable.Failures)
		callable.Requirements = cloneStrings(callable.Requirements)
		t.Callable = &callable
	}
	if t.Application != nil {
		application := *t.Application
		application.Arguments = append([]TypeRef{}, application.Arguments...)
		t.Application = &application
	}
	return t
}

func publicParams(params []Param) []Param {
	params = append([]Param{}, params...)
	for i := range params {
		params[i].defaultExpr = nil
		params[i].requiredSpan = Span{}
		params[i].defaultSpan = Span{}
		params[i].typeID = invalidTypeID
		params[i].sourceType = nil
		if params[i].DefaultValue != nil {
			value := *params[i].DefaultValue
			params[i].DefaultValue = &value
		}
	}
	return params
}

func (c *checker) validateFields(fields []Field, owner string, reserveTag bool) {
	names := map[string]bool{}
	for _, field := range fields {
		if names[field.Name] {
			c.diagnostic("EF101", "duplicate field "+field.Name+" in "+owner, field.Span)
		}
		names[field.Name] = true
		if field.Name == "_tag" && reserveTag {
			c.diagnostic("EF120", "_tag is reserved for closed variant/error discriminators", field.Span)
		}
		if c.requiresTemplateArguments(field.Type) {
			c.diagnostic("EF127", "generic type "+field.Type+" requires complete application arguments", field.Span)
		} else if !c.typeKnown(field.Type) && !c.absentName(absentInType, field.Type, field.TypeSpan) {
			c.diagnostic("EF102", "unknown or unsupported field type "+field.Type, field.Span)
		}
	}
}
func (c *checker) validateDataLayouts() {
	state := map[string]int{}
	reported := map[string]bool{}
	var visit func(string, []string)
	visit = func(name string, path []string) {
		if state[name] == 1 {
			cycle := append(path, name)
			key := strings.Join(cycle, "->")
			if !reported[key] {
				reported[key] = true
				span := Span{}
				if record := c.records[name]; record != nil {
					span = record.Span
				} else if enum := c.enums[name]; enum != nil {
					span = enum.Span
				} else if failure := c.errors[name]; failure != nil {
					span = failure.Span
				}
				c.diagnostic("EF119", "recursive data layout is unsupported: "+key, span)
			}
			return
		}
		if state[name] == 2 {
			return
		}
		state[name] = 1
		fields := []Field{}
		if record := c.records[name]; record != nil {
			fields = record.Fields
		} else if enum := c.enums[name]; enum != nil {
			for _, variant := range enum.Variants {
				fields = append(fields, variant.Fields...)
			}
		} else if failure := c.errors[name]; failure != nil {
			fields = failure.Fields
		}
		for _, field := range fields {
			if c.records[field.Type] != nil || c.enums[field.Type] != nil || c.errors[field.Type] != nil {
				visit(field.Type, append(path, name))
			}
		}
		state[name] = 2
	}
	for _, record := range c.program.Records {
		visit(record.Name, nil)
	}
	for _, enum := range c.program.Enums {
		visit(enum.Name, nil)
	}
	for _, failure := range c.program.ErrorDecls {
		visit(failure.Name, nil)
	}
}
func (c *checker) function(f *Function, record bool) {
	if !record && !c.program.interfaceProducer && c.admittedSummaries[f.Module].Module != "" {
		return
	}
	c.functionWithLocals(f, record, nil, nil)
}

func (c *checker) publishRecontractedExpression(expr *Expr, result checkedExpression) {
	if expr == nil {
		return
	}
	facts, exists := c.result.facts[expr]
	if !exists {
		return
	}
	checked := facts.Checked.clone()
	checked.value = result.value
	checked.fields = cloneFieldOccurrences(result.fields)
	checked.callableEvidence = result.callableEvidence
	checked.application = result.clone().application
	checked.identity = result.identity
	// Re-contracting changes the checked value and its projected fields. The
	// expression's own evaluation and execution facts remain those computed at
	// its source occurrence; earlier block statements stay on the function body.
	expr.checked = checked.clone()
	expr.Type = c.projectCheckedBase(checked)
	expr.Identity = checked.identity
	facts.Checked = checked.clone()
	facts.Type = expr.Type
	c.result.facts[expr] = facts
}

func (c *checker) functionWithLocals(f *Function, record bool, locals []Param, capturedServices []string) {
	previousLexicalOwner := c.lexicalOwner
	c.lexicalOwner = nil
	if c.result.lexical != nil {
		if _, original := c.result.lexical.functions[f]; original {
			c.lexicalOwner = f
		}
	}
	defer func() { c.lexicalOwner = previousLexicalOwner }()
	previousTypes := c.typeContext
	c.typeContext = c.templateContext(f.TypeParameters, f.Identity)
	defer func() { c.typeContext = previousTypes }()
	previousModule := c.functionModule
	c.functionModule = f.Module
	defer func() { c.functionModule = previousModule }()
	previousRows := c.rowContext
	c.rowContext = c.functionRows(f)
	defer func() { c.rowContext = previousRows }()
	env := localEnv{}
	for _, p := range locals {
		parameter := c.checkedDataID(p.typeID, nil, nil)
		parameter.setOwnership(c.borrowedOwnershipID(parameter.valueID(), "parameter:"+p.Name))
		if c.lexicalOwner != nil {
			parameter = c.bindLocal("configuration", p.Name, p.Span, p.Extent, c.result.lexical.parameters[p.Span.Offset], parameter)
		}
		env[p.binding] = parameter
	}
	for _, p := range f.Params {
		parameter := c.checkedDataID(p.typeID, nil, nil)
		parameter.fields = c.parameterFields(f, p)
		if node := parameter.node(); node != nil && node.Kind == "callable" {
			parameter.callableEvidence = callableEvidence{parameter: f, parameterName: p.Name}
		}
		parameter.setOwnership(c.borrowedOwnershipID(p.typeID, "parameter:"+p.Name))
		if c.recipeType(p.typeID) {
			// The caller's recipe holds borrowed handles; what executing it
			// returns is unresolved inside this function.
			parameter = c.recipeOccurrence(parameter, false, p.typeID, parameter.ownershipFacts())
		}
		if c.lexicalOwner != nil {
			parameter = c.bindLocal("parameter", p.Name, p.Span, p.Extent, c.result.lexical.parameters[p.Span.Offset], parameter)
			c.attachParameterFacts(parameter.lexicalBinding, p)
		}
		env[p.binding] = parameter
	}
	c.reasons = []Contribution{}
	previousReporting := c.reportingFunction
	c.reportingFunction = f
	defer func() { c.reportingFunction = previousReporting }()
	previousFacts := c.recordFacts
	c.recordFacts = record || previousFacts
	var returnedExpr *Expr
	if f.Body != nil && len(f.Body.Statements) > 0 {
		last := f.Body.Statements[len(f.Body.Statements)-1]
		if last.Kind == "expr" {
			returnedExpr = last.Value
		}
	}
	actual := c.withRegion("invocation", func() checkedExpression { return c.blockReturning(f.Body, env, f.Effect, f.returnID) })
	c.recordFacts = previousFacts
	compatible := c.assignable(actual.valueID(), f.returnID, 0)
	if !compatible && !actual.isEffect() {
		if recontracted, ok := c.recontractApplication(actual, f.returnID, returnedExpr); ok {
			actual, compatible = recontracted, true
			c.publishRecontractedExpression(returnedExpr, actual)
		}
	}
	// An invalid body result was already diagnosed where it arose.
	if !c.isKind(actual, "never") && !c.isKind(actual, "invalid") && (!compatible || (actual.isEffect() && !c.recipeType(f.returnID))) {
		c.diagnostic("EF106", fmt.Sprintf("body returns %s; expected %s", c.displayChecked(actual), f.Return), f.Span)
	}
	// The end of the body is an exit: a child still unobserved there is
	// pending on the call layer, and its failures are in the body's row
	// (design §5.3 rules 1 and 5). An effect function invocation is not an
	// owner, so the caller's owner raises them.
	actual.evaluation = c.exitAll(actual.evaluation, nil)
	if missing := c.rowDifference(c.rowLabels(actual.evaluation.failureRowID()), c.rowLabels(f.failureID)); len(missing) > 0 {
		if slices.Contains(missing, compositeCause) {
			missing = remove(missing, compositeCause)
			c.diagnostic("EF107", "undeclared failures: "+compositeCause+"; an owner may close over a failing child together with a failure of its body, or over two failing children, and the runtime raises a composite cause which no handler matches (design §5.3 rule 6): join or interrupt the child first", f.Span)
		}
		if len(missing) > 0 {
			c.diagnostic("EF107", "undeclared failures: "+strings.Join(missing, ", "), f.Span)
		}
	}
	if missing := c.rowDifference(c.rowLabels(actual.evaluation.serviceRowID()), union(c.rowLabels(f.serviceID), capturedServices)); len(missing) > 0 {
		c.diagnostic("EF108", "missing service requirements: "+strings.Join(missing, ", "), f.Span)
		if !c.suppressDiagnostics {
			diagnostic := &c.result.Diagnostics[len(c.result.Diagnostics)-1]
			for _, contribution := range c.reasons {
				if contribution.Kind == "layer-construction-input" && len(contribution.Names) == 1 && slices.Contains(missing, contribution.Names[0]) && len(diagnostic.Related) < maxLayerRelatedLocations {
					diagnostic.Related = append(diagnostic.Related, RelatedLocation{Message: "layer construction requires " + contribution.Names[0], Span: contribution.Span})
				}
			}
		}
	}
	f.returnsNever = c.isKind(actual, "never")
	actual = c.invocationContract(c.completeFailures(actual), f.Effect)
	if f.Identity == "" {
		f.Identity = "function:" + f.Name
	}
	summary := c.summaryOccurrence(actual, f.Effect)
	f.Ownership = summary.ownershipFacts()
	f.Captures = summary.captureFacts()
	f.failures = c.declaredFailures(summary.failures, c.failureLayerRows(actual.valueID()), c.callFailureRows(f.returnID, f.Effect, f.failureID))
	f.returnCallableEvidence = actual.callableEvidence
	f.returnFields = cloneFieldOccurrences(summary.fields)
	declared := c.checkedFunction(f, true, false)
	declared.identity = f.Identity
	c.result.checkedFunctions[f] = checkedSymbol{contract: declared, body: actual, declaration: f}
	if record {
		checked := checkedSymbol{contract: declared, body: actual, declaration: f, contributions: c.reasons}
		c.result.checkedSymbols[f.Identity] = checked
		prototype := Symbol{
			Name:          f.Name,
			Source:        f.SourceID,
			Identity:      f.Identity,
			Params:        f.Params,
			Contract:      c.projectCheckedBase(declared),
			Actual:        c.projectCheckedBase(actual),
			Span:          f.Span,
			Contributions: c.reasons,
		}
		if f.Module != "" && f.Module != currentModuleIdentity {
			prototype.Name = f.Module + "." + f.Name
		}
		var size int
		var err error
		if !c.result.publicationRefused {
			size, err = c.checkedSymbolSize(prototype, checked, defaultProjectionLimits.CompatibilityBytes-c.publicationBytes)
		}
		if err != nil || c.result.publicationRefused {
			if !c.result.publicationRefused {
				c.result.publicationUsage.CompatibilityBytes = c.publicationBytes + size
			}
			c.result.publicationRefused = true
			prototype.Contract.ProjectionError = "whole-source compatibility projection exceeds limits"
			prototype.Actual.ProjectionError = prototype.Contract.ProjectionError
		} else {
			c.publicationBytes += size
			prototype.Contract = c.projectChecked(declared)
			prototype.Actual = c.projectChecked(actual)
			prototype.Params = publicParams(f.Params)
		}
		// The declaration and public symbol share this immutable boundary view.
		// Checking and emission continue to use the retained numeric facts.
		f.Contract, f.Actual = prototype.Contract, prototype.Actual
		c.result.Symbols = append(c.result.Symbols, prototype)
	} else {
		f.Contract = c.projectCheckedBase(declared)
		f.Actual = c.projectCheckedBase(actual)
	}
}

func callableIdentity(c *checker, f *Function) *CallableType {
	parameters := publicParams(f.Params)
	for i := range parameters {
		parameters[i].TypeRef = c.ref(f.Params[i].typeID)
	}
	kind := "pure"
	if f.Effect {
		kind = "effect"
	}
	identity := f.Identity
	if identity == "" {
		identity = "function:" + f.Name
	}
	view := &CallableType{ID: identity, Kind: kind, Parameters: parameters, Result: c.ref(f.returnID), Failures: normalized(f.Errors), Requirements: normalized(f.Services), RowParameters: append([]RowParameter(nil), f.RowParameters...), CallbackPolicies: append([]CallbackPolicy(nil), f.CallbackPolicies...)}
	for _, p := range f.TypeParameters {
		view.TypeParameters = append(view.TypeParameters, TemplateParameterView{Name: p.Name, Kind: p.Kind, Identity: p.Identity, Variable: c.ref(p.typeID), typeID: p.typeID})
	}
	return view
}
func (c *checker) displayChecked(t checkedExpression) string {
	if t.isEffect() {
		return recipeDisplay(c.displayTypeID(t.resultID()), c.rowLabels(t.failureRow()), c.rowLabels(t.serviceRow()))
	}
	return c.displayTypeID(t.resultID())
}
func clone(env localEnv) localEnv {
	copy := localEnv{}
	for n, t := range env {
		copy[n] = t.clone()
	}
	return copy
}
func fieldsFor(c *checker, typeName, variantName string) ([]Field, bool) {
	if variantName == "" {
		if record := c.records[typeName]; record != nil {
			return record.Fields, true
		}
		return nil, false
	}
	if enum := c.enums[typeName]; enum != nil {
		for _, variant := range enum.Variants {
			if variant.Name == variantName {
				return variant.Fields, true
			}
		}
	}
	return nil, false
}
func fieldsMap(fields []Field) map[string]Field {
	result := make(map[string]Field, len(fields))
	for _, field := range fields {
		result[field.Name] = field
	}
	return result
}
func sortedBindingNames(bindings map[string]string) []string {
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
func (c *checker) payload(e *Expr, fields []Field, env localEnv, span Span) []OwnershipFact {
	declared := fieldsMap(fields)
	seen := map[string]bool{}
	ownership := []OwnershipFact{}
	for _, field := range e.Fields {
		want, exists := declared[field.Name]
		if !exists {
			c.diagnostic("EF114", "unknown payload field "+field.Name, field.Span)
			continue
		}
		if seen[field.Name] {
			c.diagnostic("EF114", "duplicate payload field "+field.Name, field.Span)
		}
		seen[field.Name] = true
		got := c.expr(field.Value, env, false)
		// A recipe is admitted only by a declared recipe field; assignability
		// refuses it in every other slot.
		if !c.sameType(got, want.Type) {
			c.diagnostic("EF115", "payload field "+field.Name+" must be "+want.Type, field.Span)
		}
		ownership = append(ownership, prependFacts(field.Name, heldFacts(got, 0))...)
	}
	for _, field := range fields {
		if !seen[field.Name] {
			c.diagnostic("EF114", "missing payload field "+field.Name, span)
		}
	}
	return normalizeFacts(ownership)
}

func unionEvaluation(a, b EvaluationRows) EvaluationRows {
	return EvaluationRows{Failures: union(a.Failures, b.Failures), Requirements: union(a.Requirements, b.Requirements)}
}

func (c *checker) childEvaluation(e *Expr) ExpressionEvaluation {
	result := c.evaluation(emptyRowID, emptyRowID)
	forEachExprChild(e, func(child *Expr) {
		if child != nil {
			result = c.unionEvaluationFacts(result, child.checked.evaluation)
		}
	})
	return result
}

// requireObservedChildren refuses a function used as a callable value when
// its call may leave a forked child unobserved (design §5.3). A callable or
// service method type promises that its row is raised by its invocation:
// recover and catch around an invocation of an unknown callee handle it.
// A pending failure is raised later, by the owner that closes after the
// call, so no contract can carry it.
func (c *checker) requireObservedChildren(f *Function, span Span) {
	labels := anyPendingLabels(f.failures)
	for _, field := range f.returnFields {
		labels = union(labels, carriedPending(field, 1))
	}
	slices.Sort(labels)
	if len(labels) == 0 {
		return
	}
	c.diagnostic("EF107", f.Name+" may leave forked child failures unobserved ("+strings.Join(labels, ", ")+"); a callable or service contract cannot carry them, so join or interrupt the child first", span)
}

// blockResult says what consumes a block's final expression, which decides
// how a recipe there is diagnosed. It never executes the recipe.
type blockResult int

const (
	// blockStatement discards the final value; a recipe there is unused.
	blockStatement blockResult = iota
	// blockSelected makes the final value the block's value: a branch arm or
	// a declared recipe-result slot selects the recipe without running it.
	blockSelected
	// blockScope publishes the final value from a closing scope, which
	// admits no unexecuted recipe.
	blockScope
)

func (c *checker) block(b *Block, env localEnv, effect bool) checkedExpression {
	return c.blockValue(b, env, effect, blockStatement)
}

// branchBlock checks an if or match arm. Its final expression is the value of
// the enclosing branch expression, so a recipe there is selected rather than
// discarded; the statement or slot that consumes the branch decides whether
// that value is used.
func (c *checker) branchBlock(b *Block, env localEnv, effect bool) checkedExpression {
	return c.blockValue(b, env, effect, blockSelected)
}

// blockReturning checks a block whose final expression initializes result.
// A recipe in that position is a returned value, not a discarded lazy
// effect, only when the declared slot is itself a recipe contract.
func (c *checker) blockReturning(b *Block, env localEnv, effect bool, result TypeID) checkedExpression {
	if c.recipeType(result) {
		return c.blockValue(b, env, effect, blockSelected)
	}
	return c.blockValue(b, env, effect, blockStatement)
}

func (c *checker) blockValue(b *Block, env localEnv, effect bool, result blockResult) checkedExpression {
	out := c.checkedData(voidTypeName)
	env = clone(env)
	terminated := false
	// Fork observation is flow-sensitive (design §5.3 rule 2): a child
	// forked by an earlier statement is pending at every exit a later
	// statement may take before a join or interrupt observes it. killed is
	// what the statements so far observed; guaranteed is what was observed
	// before every exit taken so far.
	var killed, guaranteed []string
	exited := false
	exit := func(observed []string) {
		if exited {
			guaranteed = intersectStrings(guaranteed, observed)
		} else {
			guaranteed, exited = append([]string{}, observed...), true
		}
	}
	for index, s := range b.Statements {
		if terminated {
			c.diagnostic("EF109", "unreachable statement after fail", s.Span)
		}
		if s.Kind == "fail" {
			if !effect {
				c.diagnostic("EF105", "fail is only valid inside effect functions", s.Span)
			}
			if _, exists := c.program.Errors[s.Name]; !exists {
				c.diagnostic("EF102", "unknown failure "+s.Name, s.Span)
			} else if decl := c.errors[s.Name]; decl != nil {
				c.observeReference(s, s.NameSpan, lexicalTarget{kind: "error", failure: decl})
			}
			// The payload keeps the owners of the handles it carries (design
			// §5.2 F1): the failing execution's owners stay attached, and a
			// handler is checked against the handles it dereferences.
			var payloadFacts []OwnershipFact
			if s.Payload != nil {
				if s.Payload.Kind == "payload" {
					fields := []Field(nil)
					if decl := c.errors[s.Name]; decl != nil {
						fields = decl.Fields
						c.observeFieldLabels(s.Payload, s.Payload.Fields, fields, lexicalTarget{failure: decl})
					}
					payloadFacts = c.payload(s.Payload, fields, env, s.Payload.Span)
					for _, field := range s.Payload.Fields {
						c.refuseCarriedPending(field.Value.checked, field.Span)
					}
				} else {
					payload := c.refuseCarriedPending(c.expr(s.Payload, env, false), s.Payload.Span)
					if decl := c.errors[s.Name]; decl != nil && len(decl.Fields) == 1 {
						payloadFacts = prependFacts(decl.Fields[0].Name, heldFacts(payload, 0))
					}
					if payload.isEffect() && !c.recipeType(c.singleFailureFieldType(s.Name)) {
						c.diagnostic("EF105", "failure payload must be pure", s.Payload.Span)
					}
					if decl := c.errors[s.Name]; decl != nil {
						if len(decl.Fields) != 1 || !c.sameType(payload, decl.Fields[0].Type) {
							c.diagnostic("EF115", "failure payload for "+s.Name+" must match its declared fields", s.Payload.Span)
						} else {
							s.Payload = &Expr{Kind: "payload", Fields: []FieldValue{{Name: decl.Fields[0].Name, Value: s.Payload, Span: s.Payload.Span}}, Span: s.Payload.Span}
						}
					}
				}
			} else if decl := c.errors[s.Name]; decl != nil {
				// The shorthand `fail Error` and `fail Error()` still need to
				// satisfy every declared payload field.
				payloadFacts = c.payload(&Expr{Kind: "payload", Span: s.Span}, decl.Fields, env, s.Span)
			}
			// fail is an exit: every child still unobserved is pending.
			previousEvaluation := c.exitAll(out.evaluation, []string{s.Name})
			exit(killed)
			out = c.checkedData("never")
			out.evaluation = c.unionEvaluationFacts(previousEvaluation, c.evaluationWith(c.internRow([]string{s.Name}), emptyRowID, map[string][]OwnershipFact{s.Name: payloadFacts}, nil))
			terminated = true
			c.reasons = append(c.reasons, Contribution{"failure", []string{s.Name}, s.Span})
			continue
		}
		t := c.expr(s.Value, env, effect)
		// A recipe value escapes with the handles it holds; what running it
		// returns stays deferred on the recipe until it actually runs.
		// A selected tail flows into its enclosing branch or recipe value,
		// where the joined value is checked once.
		selectedTail := result == blockSelected && index == len(b.Statements)-1
		if escaping := expandRelations(heldFacts(t, 0)); s.Kind != "let" && !selectedTail && (hasPotentialOwner(escaping) || hasOwnedClosed(escaping)) {
			c.reportOwnership("value owned by a closing scope cannot escape", s.Span, ownershipRoots(escaping, ""))
		}
		current := out.evaluation
		if len(c.rowLabels(t.executed.failureRowID())) > 0 {
			// The statement may exit: the children forked before it and
			// not observed before that exit stay pending.
			crossing, observed := current.live, killed
			crossing, observed = killForks(crossing, t.exitKills), unionStrings(killed, t.exitKills)
			current = c.exitPending(current, crossing, c.rowLabels(t.executed.failureRowID()))
			exit(observed)
		}
		current.live = killForks(current.live, t.kills)
		killed = unionStrings(killed, t.kills)
		out.evaluation = c.unionEvaluationFacts(current, t.executed)
		if s.Kind == "let" {
			if s.binding.rebinds != nil {
				c.diagnostic("EF101", "duplicate local "+s.Name, s.Span)
			}
			bound := t.clone()
			if c.lexicalOwner != nil {
				bound = c.bindLocal("let", s.Name, s.NameSpan, s.Extent, c.result.lexical.statements[s], bound)
			}
			env[s.binding] = bound
			previousEvaluation := out.evaluation
			out = c.checkedData(voidTypeName)
			out.evaluation = previousEvaluation
		} else {
			last := index == len(b.Statements)-1
			switch {
			case !t.isEffect() || last && result == blockSelected:
			case last && result == blockScope:
				c.scopeResultRecipe(s.Value)
			default:
				c.diagnostic("EF105", "unused lazy effect; execute with run or bind it with let", s.Span)
			}
			previousEvaluation := out.evaluation
			out = t.clone()
			out.evaluation = c.unionEvaluationFacts(previousEvaluation, t.executed)
		}
	}
	// The success exit: the block exports what it observed before every
	// exit.
	exit(killed)
	out.kills, out.exitKills = guaranteed, guaranteed
	return out
}

// Effect values carry deferred rows. Only run (and executed branch bodies)
// contribute to the enclosing computation. The rows are computed once by
// expr and retained on the checked node; this accessor is deliberately a
// projection rather than a second subtree walk.
func (c *checker) expr(e *Expr, env localEnv, inEffect bool) checkedExpression {
	t := c.checkedData("invalid")
	if e.constructorType != nil {
		c.diagnostic("EF127", "explicit application syntax requires a data constructor", e.Span)
	}
	switch e.Kind {
	case "integer":
		// Ordinary literals and signed constant literals must fit i64; the
		// unsigned minimum magnitude fits only directly under unary minus.
		if !isI64IntegerLiteral(e.Text) && !(e.allowMinLiteral && isMinI64Literal(e.Text)) {
			c.diagnostic("EF001", "integer exceeds i64 range", e.Span)
			break
		}
		t = c.checkedData("i64")
	case "string":
		t = c.checkedData("string")
	case "bool":
		t = c.checkedData("bool")
	case "void":
		t = c.checkedData(voidTypeName)
	case "name":
		if e.binding != nil {
			v, bound := env[e.binding]
			if !bound {
				// The binder was rejected by its own diagnostic.
				break
			}
			c.observeLocalUse(e, v)
			t = v.clone()
			// A local read observes a carried value contract; it does not replay
			// evaluation work performed by the initializer.
			t.evaluation = c.evaluation(emptyRowID, emptyRowID)
			t.executed = t.evaluation
			e.Text = "local"
		} else if p, exists := c.providers[e.Name]; exists {
			c.observeReference(e, e.Span, lexicalTarget{kind: "provider", provider: p})
			if len(p.Params) > 0 || len(p.Services) > 0 {
				c.diagnostic("EF104", "provider "+p.Name+" requires explicit construction", e.Span)
				t = c.checkedData("invalid")
				break
			}
			t = c.checkedProvider(p, false)
			if p.Service == "Files" || p.Service == "Runtime" || p.Service == "Foreign" {
				c.requireGo(e.Span, "native provider "+p.Service)
			}
			e.Text = "provider"
		} else if f := c.namedFunction(e.Name); f != nil {
			if len(f.RowParameters) > 0 || len(f.TypeParameters) > 0 {
				c.diagnostic("EF125", "row-polymorphic functions require direct application; first-class polymorphic values are unsupported", e.Span)
			}
			t = c.checkedFunction(f, true, false)
			if e != c.handlerOperand {
				c.requireObservedChildren(f, e.Span)
			}
			t.setOwnership(nil)
			e.ResolvedFunction = f
			e.Text = "function"
			c.observeReference(e, e.Span, lexicalTarget{kind: "function", function: f})
		} else if !c.absentName(absentInValue, e.Name, e.Span) {
			c.diagnostic("EF102", "unknown value "+e.Name, e.Span)
		}
	case "call":
		if data, ok := c.dataCall(e, env, inEffect); ok {
			t = data
			break
		}
		if converted, ok := c.hostConversion(e, env, inEffect); ok {
			t = converted
			break
		}
		if c.foreignCall(e, env, inEffect) {
			t = e.checked
			break
		}
		if c.fiberCall(e, env, inEffect) {
			t = e.checked
			break
		}
		if callback, ok := c.callableCall(e, env, inEffect); ok {
			t = callback
			break
		}
		if e.Left.Kind == "name" {
			if provider := c.providers[e.Left.Name]; provider != nil {
				c.observeReference(e.Left, e.Left.Span, lexicalTarget{kind: "provider", provider: provider})
				if len(provider.Params) == 0 && len(provider.Services) == 0 {
					c.diagnostic("EF105", "provider "+provider.Name+" is a value and cannot be called", e.Span)
					t = c.checkedData("invalid")
					break
				}
				order, bound := c.bindCallArguments(e, provider.Params, "provider "+provider.Name+" expects "+fmt.Sprint(len(provider.Params))+" configuration arguments", lexicalTarget{provider: provider})
				argumentTypes := make([]checkedExpression, len(e.Args))
				if !bound {
					// A rejected binding keeps the provider's declared contract:
					// each parameter starts at its declared type.
					argumentTypes = make([]checkedExpression, len(provider.Params))
					for i, param := range provider.Params {
						argumentTypes[i] = c.checkedDataID(param.typeID, nil, nil)
					}
				}
				for source, arg := range e.Args {
					got := c.refuseCarriedPending(c.expr(arg, env, false), arg.Span)
					i := order[source]
					if i < 0 && !bound {
						continue
					}
					if i < 0 {
						i = source
					}
					argumentTypes[i] = got
					if i < len(provider.Params) && (got.isEffect() || !c.assignable(got.valueID(), provider.Params[i].typeID, 0)) {
						c.diagnostic("EF106", "provider configuration argument must be "+provider.Params[i].Type, arg.Span)
					}
				}
				t = c.checkedProvider(provider, true)
				for i, param := range provider.Params {
					if i < len(argumentTypes) {
						t.setCaptures(append(t.captureFacts(), prependFacts("capture:"+param.Name, argumentTypes[i].ownershipFacts())...))
					}
				}
				t.setCaptures(normalizeFacts(t.captureFacts()))
				e.Text = "provider-constructor"
				break
			}
		}
		var f *Function
		var service *Service
		serviceName := ""
		unresolvedName := false
		if e.Left.Kind == "name" {
			f = c.namedFunction(e.Left.Name)
			shadow := e.Left.binding != nil
			if shadow {
				c.diagnostic("EF103", "calling local values is not supported in this prototype", e.Span)
				f = nil
			}
			unresolvedName = f == nil && !shadow
		} else if e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
			key := e.Left.Left.Name
			if key == "Files" || key == "Runtime" {
				c.requireGo(e.Span, "native service "+key)
			}
			if e.Left.Left.binding != nil {
				c.diagnostic("EF103", "a local shadows service "+key, e.Span)
			} else if imported := c.program.memberFunction(e.Left); imported != nil {
				f = imported
			} else if s := c.services[key]; s != nil {
				for _, m := range s.Methods {
					if m.Name == e.Left.Name {
						f = m
						service, serviceName = s, key
						break
					}
				}
			}
		}
		if f == nil {
			if !unresolvedName || !c.absentName(absentInValue, e.Left.Name, e.Left.Span) {
				c.diagnostic("EF102", "unknown function or service method", e.Span)
			}
			for _, a := range e.Args {
				c.expr(a, env, inEffect)
			}
			break
		}
		e.ResolvedFunction = f
		switch {
		case service != nil:
			c.observeReference(e.Left, e.Left.Span, lexicalTarget{kind: "operation", function: f, service: service})
			c.observeReference(e.Left.Left, e.Left.Left.Span, lexicalTarget{kind: "service", service: service})
		case e.Left.Kind == "member":
			c.observeReference(e.Left, e.Left.Span, lexicalTarget{kind: "function", function: f})
			c.observeModuleAlias(e.Left.Left, e.Left.Left)
		default:
			c.observeReference(e.Left, e.Left.Span, lexicalTarget{kind: "function", function: f})
		}
		// Arguments are checked in source order and recorded by parameter.
		order, bound := c.bindCallArguments(e, f.Params, "incorrect argument count", lexicalTarget{function: f, service: service})
		argumentTypes := make([]checkedExpression, len(e.boundArguments()))
		t = c.checkedFunction(f, false, true)
		var callbackPolicies []CallbackPolicy
		parameterIDs := c.functionParameterTypeIDs(f)
		if !bound {
			// A rejected binding keeps the callee's declared contract: each
			// parameter starts at its declared type, so a parameter left
			// without an argument cascades no further diagnostics.
			argumentTypes = make([]checkedExpression, len(f.Params))
			for i, id := range parameterIDs {
				argumentTypes[i] = c.checkedDataID(id, nil, nil)
			}
		}
		failureRow := t.failureRow()
		serviceLabels := c.rowLabels(t.serviceRow())
		if serviceName != "" {
			serviceLabels = union(serviceLabels, []string{serviceName})
		}
		for source, a := range e.Args {
			arg := c.refuseCarriedPending(c.refuseClosedArgument(c.expr(a, env, inEffect), a.Span), a.Span)
			i := order[source]
			if i < 0 && !bound {
				// The rejected binding already diagnosed this argument.
				continue
			}
			if i < 0 {
				i = source
			}
			argumentTypes[i] = arg
			handlerArgument := false
			for _, policy := range f.CallbackPolicies {
				if policy.Parameter != i || i >= len(f.Params) || !c.handlerCompatible(arg, f.Params[i].Type) {
					continue
				}
				handlerArgument = true
				if policy.PropagateRequirements {
					serviceLabels = union(serviceLabels, c.rowLabels(arg.serviceRow()))
				}
				policy.AbsorbedFailures = c.rowLabels(arg.failureRow())
				policy.FailureRow = c.rowNodeID(arg.failureRow())
				callbackPolicies = append(callbackPolicies, policy)
			}
			if i < len(f.Params) && len(f.RowParameters) == 0 && len(f.TypeParameters) == 0 && !handlerArgument && !c.assignable(arg.valueID(), parameterIDs[i], 0) {
				message, span := "argument must be "+f.Params[i].Type, a.Span
				if source == 0 && e.PipeSpan.Length > 0 {
					message, span = c.pipedArgumentMismatch(e, a, arg, message)
				}
				c.diagnostic("EF106", message, span)
			}
		}
		// An omitted parameter's constant default is an ordinary argument of
		// the call from here on: generic and row inference, ownership and the
		// application identity see the same vector as the explicit spelling.
		for parameter, argument := range c.checkDefaultArguments(e) {
			argumentTypes[parameter] = argument
		}
		// The application identity lists handler policies by parameter, however
		// the call spells its labels.
		slices.SortStableFunc(callbackPolicies, func(a, b CallbackPolicy) int { return a.Parameter - b.Parameter })
		resultID := f.returnID
		if resultID == invalidTypeID {
			resultID = t.resultID()
		}
		var rowArguments []RowArgument
		typeBindings := map[TypeID]TypeID{}
		if len(f.TypeParameters) > 0 {
			typeBindings = c.inferTypeArguments(f, argumentTypes, e.Span)
		}
		rowBindings := map[string][]string{}
		if len(f.RowParameters) > 0 {
			bindings := c.inferRows(f, argumentTypes, e.Span, typeBindings)
			rowBindings = bindings
			for i, id := range parameterIDs {
				parameterIDs[i] = c.substituteCanonical(id, typeBindings, bindings)
			}
			resultID = c.substituteCanonical(f.returnID, typeBindings, bindings)
			failureRow = c.instantiateRow(f.failureID, bindings)
			serviceLabels = c.rowLabels(c.instantiateRow(f.serviceID, bindings))
			if serviceName != "" {
				serviceLabels = union(serviceLabels, []string{serviceName})
			}
			for _, parameter := range f.RowParameters {
				rowArguments = append(rowArguments, RowArgument{Parameter: parameter, Row: c.rowNodeID(c.internRow(bindings[parameter.ID]))})
			}
		}
		if len(f.TypeParameters) > 0 && len(f.RowParameters) == 0 {
			for i, id := range parameterIDs {
				parameterIDs[i] = c.substituteCanonical(id, typeBindings, nil)
			}
			resultID = c.substituteCanonical(f.returnID, typeBindings, nil)
		}
		if resultID == invalidTypeID {
			c.diagnostic("EF127", "template substitution unavailable or exceeds budget", e.Span)
			break
		}
		if len(f.Ownership) > 0 {
			t.setOwnership(c.instantiateCallbackFacts(f.Ownership, f, argumentTypes, 0))
		}
		if len(f.Captures) > 0 {
			t.setCaptures(c.instantiateCallbackFacts(f.Captures, f, argumentTypes, 0))
		}
		// The callee's failure payloads, instantiated (design §5.2 F3).
		t.failures = c.instantiateFailures(f.failures, f, argumentTypes, 0)
		if serviceName != "" {
			operation := c.operationResult(resultID, argumentTypes, f.Effect, failureRow)
			t.setOwnership(operation.ownership)
			t.setCaptures(operation.captures)
			t.failures = operation.failures
		}
		if f.Effect {
			t.value = c.values.recipe(resultID, checkedEffectCallable, failureRow, c.internRow(serviceLabels), t.ownershipFacts(), t.captureFacts())
		} else {
			t.value = c.values.occurrence(resultID, t.ownershipFacts(), t.captureFacts())
		}
		t.callableEvidence = substituteCallableEvidence(f.returnCallableEvidence, f, argumentTypes)
		t.fields = c.instantiateFieldOccurrences(f.returnFields, f, argumentTypes, typeBindings, rowBindings, 0)
		owners, captures := c.containerFieldFacts(t.ownershipFacts(), t.captureFacts(), t.fields, c.recipeDepth(t.valueID()))
		t.setOwnership(owners)
		t.setCaptures(captures)
		if f.Effect && serviceName != "" {
			// A service operation dispatches to whichever provider is in
			// scope, an unknown callee: executing it uses every argument it
			// was given (design §4.2). A summarized callee's call layer uses
			// exactly the handles its body dereferences, instantiated above.
			for i, argument := range argumentTypes {
				if i < len(f.Params) {
					t.setCaptures(append(t.captureFacts(), prependFacts("capture:"+f.Params[i].Name, heldFacts(argument, 0))...))
				}
			}
			t.setCaptures(normalizeFacts(t.captureFacts()))
		}
		argumentRefs := make([]TypeRef, 0, len(argumentTypes))
		for _, argument := range argumentTypes {
			argumentRefs = append(argumentRefs, c.identityRef(argument.valueID()))
		}
		callee := e.Left.Name
		if f.Module != "" && f.Module != currentModuleIdentity {
			callee = f.Identity
		} else if e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
			callee = e.Left.Left.Name + "." + e.Left.Name
		}
		application := newApplicationIdentity(callee, argumentRefs, c.ref(resultID), e.Span)
		application.CallbackPolicies = callbackPolicies
		application.RowArguments = rowArguments
		t.application = &application
		t.identity = application.ID
	case "member":
		if e.Left != nil && e.Left.Kind == "name" {
			if e.Left.binding == nil {
				if f := c.program.memberFunction(e); f != nil {
					if len(f.RowParameters) > 0 || len(f.TypeParameters) > 0 {
						c.diagnostic("EF125", "row-polymorphic functions require direct application; first-class polymorphic values are unsupported", e.Span)
					}
					t = c.checkedFunction(f, true, false)
					c.requireObservedChildren(f, e.Span)
					t.setOwnership(nil)
					e.ResolvedFunction = f
					e.Text = "function"
					c.observeReference(e, e.Span, lexicalTarget{kind: "function", function: f})
					c.observeModuleAlias(e.Left, e.Left)
					break
				}
			}
		}
		inner := c.expr(e.Left, env, inEffect)
		if selected, ok := c.hostSelection(e, inner); ok {
			t = selected
			break
		}
		if inner.isEffect() {
			c.diagnostic("EF106", "field access requires an executed value", e.Span)
			break
		}
		if fields, ok := c.applicationFields(inner.resultID()); ok {
			for _, field := range fields {
				if field.Name == e.Name {
					t = c.projectFieldOccurrence(inner, field)
					e.Text = "field"
					c.observeReference(e, e.Span, lexicalTarget{kind: "field", data: c.templates[c.node(inner.resultID()).Declaration], field: field.Name})
					break
				}
			}
			if c.isKind(t, "invalid") {
				c.diagnostic("EF114", "unknown applied field "+e.Name, e.Span)
			}
			break
		}
		fields, ok := fieldsFor(c, c.namedType(inner), "")
		if node := inner.node(); !ok && node != nil && node.Kind == "error" && c.errors[node.Name] != nil {
			fields, ok = c.errors[node.Name].Fields, true
		}
		if ok {
			for _, field := range fields {
				if field.Name == e.Name {
					t = c.projectFieldOccurrence(inner, field)
					if len(t.ownershipFacts()) == 0 && !c.recipeType(field.typeID) {
						t.setOwnership(c.unknownOwnershipID(field.typeID))
					}
					e.Text = "field"
					c.observeReference(e, e.Span, lexicalTarget{kind: "field", data: c.records[c.namedType(inner)], field: field.Name})
					break
				}
			}
			if !c.isKind(t, "invalid") {
				break
			}
			c.diagnostic("EF114", "unknown field "+e.Name+" on "+c.displayTypeID(inner.resultID()), e.Span)
			break
		}
		if c.poisoned(inner) {
			// Already reported; the field of an invalid value is invalid.
			break
		}
		t = c.goResultField(inner, e)
	case "orFail":
		t = c.expr(e.Left, env, inEffect)
		if !c.expect(t.isEffect() && c.isKind(t, "goResult"), "EF106", "orFail requires an Effect returning GoResult", e.Span, t) {
			break
		}
		if node := c.resultNode(t); node != nil && len(node.Args) == 1 {
			failure := c.internRow(union(c.rowLabels(t.failureRow()), []string{"GoError"}))
			result := checkedExpression{value: c.values.recipe(node.Args[0], checkedEffectCallable, failure, t.serviceRow(), t.ownershipFacts(), t.captureFacts())}
			result.evaluation = t.evaluation
			result.executed = t.executed
			// GoError carries no handle (design §5.2 F8).
			result.failures = t.failures
			result.observes = t.observes
			t = result
		}
	case "scope":
		if !inEffect {
			c.diagnostic("EF105", "scope requires an effect function", e.Span)
		}
		scopeRegion := fmt.Sprintf("scope:%d", e.Span.Offset)
		t = c.withRegion(scopeRegion, func() checkedExpression { return c.blockValue(e.Then, env, inEffect, blockScope) })
		// The scope edge (design §5.4 F14): a failure payload the scope owns
		// leaves it owned by the closed scope, refused only where a handler
		// dereferences it, and the obligations its body met inside the
		// scope are discharged.
		// The scope owns the children its body forked: it raises their
		// failures when it closes (design §5.3 rule 4, F14).
		kills := t.kills
		closing := c.dischargeEvaluation(t.evaluation)
		closing.payloads = mapPayloadFacts(closing.payloads, func(facts []OwnershipFact) []OwnershipFact {
			return c.closeRegionFacts(facts, scopeRegion, ownershipOwnerScope)
		})
		closing.uses = withoutRegionUses(closing.uses, scopeRegion)
		t.evaluation = closing
		t = c.closeRegionFailures(t, scopeRegion, ownershipOwnerScope)
		if escaping := expandRelations(slices.Concat(t.ownershipFacts(), t.captureFacts(), storedRecipeEvidence(t))); hasPotentialOwner(escaping) || hasOwnedFact(escaping, scopeRegion) {
			c.reportOwnership("value owned by closing scope cannot escape", e.Span, ownershipRoots(escaping, scopeRegion))
		}
		if t.isEffect() {
			// The unexecuted tail was reported at its own span. The scope has
			// no admitted result, so consumers must not report it again as a
			// discarded effect or a mismatched type.
			rejected := c.checkedData("invalid")
			rejected.evaluation, rejected.executed = t.evaluation, t.executed
			t = rejected
		}
		// The scope exports the observations its body guarantees.
		t.kills, t.exitKills = kills, kills
	case "fork":
		inner := c.expr(e.Left, env, inEffect)
		if !inEffect {
			c.diagnostic("EF105", "fork requires an Effect inside an effect function", e.Span)
		} else {
			c.expect(inner.isEffect(), "EF105", "fork requires an Effect inside an effect function", e.Span, inner)
		}
		if inner.isEffect() {
			c.requireOpenHeldCaptures(inner, e.Span)
		}
		childRegion := fmt.Sprintf("child:%d", e.Span.Offset)
		forkKey := fmt.Sprintf("fork:%d", e.Span.Offset)
		// The child executes its recipe under its own owner, which closes
		// before a joined recipe can observe the child's result. This owner is
		// distinct from the owner of the Fiber handle returned to the parent.
		// The child's scope is the owner of its own forks: their pending
		// failures are raised by the child (design §5.4, fork edge).
		var child, captures []OwnershipFact
		var childFailures map[string][]OwnershipFact
		var resultFailures failureEvidence
		if inner.isEffect() {
			inner = c.completeFailures(inner)
			// A child fiber's owner raises the grandchildren the child left
			// unobserved after its body, as failures of the child. An
			// interrupt which cancels the running child abandons them
			// (cancellation-aborted close), so none is retained for it.
			inner = c.dischargeOccurrence(inner)
			// join returns the child's result by its type alone.
			c.refuseCarriedPending(inner, e.Span)
			bound, raised, _ := c.executeLayer(inner, closedOwner(ownershipOwnerChild, childRegion))
			child, captures, childFailures = bound.ownershipFacts(), bound.captureFacts(), raised
			// A joined recipe's later layers are the child's result layers,
			// numbered under the join's binder.
			result := c.checkedDataID(inner.resultID(), nil, nil)
			result.failures = bound.failures
			resultFailures = c.liftOccurrence(result).failures
		}
		fiberFailure := inner.failureRow()
		t = checkedExpression{value: c.values.fiber(inner.resultID(), fiberFailure, []OwnershipFact{{Status: "owned", Region: c.region, Origin: "fork", ownerKind: ownershipOwnerLexical}}, captures)}
		t.child = child
		t.forks = []string{forkKey}
		// The child's failures are owned by the closed child owner (design
		// §5.2 F11); the Fiber carries them, numbered like its joined
		// recipe, for join and interrupt.
		t.failures = withLayerPayloads(resultFailures, 0, childFailures)
		// The fork charges no row (design §5.3 rule 1): the child's failures
		// are live on the forking body until a join or interrupt observes
		// the child, and pending once an exit may leave it unobserved.
		forked := c.evaluationWith(emptyRowID, inner.serviceRow(), nil, heldAt(inner.captureFacts(), 0))
		for _, label := range c.rowLabels(inner.failureRow()) {
			if forked.live == nil {
				forked.live = failureEvidence{}
			}
			forked.live[failureKey{label: label, pending: true, fork: forkKey}] = cloneFacts(childFailures[label])
		}
		t.evaluation = c.unionEvaluationFacts(inner.evaluation, forked)
		for i := range t.child {
			if t.child[i].Region == childRegion && t.child[i].Status == "owned" {
				t.child[i].Origin = "child-acquisition"
			}
		}
		c.reasons = append(c.reasons, Contribution{"owned-child", c.rowLabels(inner.failureRow()), e.Span})
	case "hostAssert":
		t = c.hostAssert(e, env, inEffect)
	case "timeout":
		duration := c.expr(e.Right, env, inEffect)
		timeoutRegion := fmt.Sprintf("timeout:%d", e.Span.Offset)
		// Recipe construction and eager arguments execute in the caller. Only
		// deferred or symbolic invocation results are materialized under the
		// fresh timeout owner; rebinding an already materialized borrow here
		// would reject a valid caller-owned value.
		inner := c.expr(e.Left, env, inEffect)
		t = inner
		durationValid := !duration.isEffect() && c.sameType(duration, "i64")
		if !t.isEffect() || !durationValid {
			c.expect((t.isEffect() || c.poisoned(t)) && durationValid, "EF106", "timeout requires an Effect and an i64 millisecond duration", e.Span, duration)
			// Invalid operands retain their value contract and caller evaluation;
			// only an admitted recipe can acquire timeout-owned facts and rows.
			break
		}
		// Layer 0 executes under the timeout owner, which has closed by the
		// time its success is observable: close(L, timeout).
		// The timeout child owns the children its layer forks (design §5.4):
		// it raises their failures, and a timed-out join observes nothing.
		// A deadline which wins abandons the timed computation, so Timeout is
		// raised alone; work which wins closes the owner normally and raises
		// its unobserved children.
		t = c.completeFailures(t)
		t = c.dischargeOccurrence(t)
		t.observes = nil
		t = c.closeOccurrence(t, closedOwner(ownershipOwnerTimeout, timeoutRegion))
		t.value = c.recontractRows(t, c.internRow(union(c.rowLabels(t.failureRow()), []string{"Timeout"})), c.internRow(union(c.rowLabels(t.serviceRow()), []string{"Scheduler"})))
	case "run":
		inner := c.expr(e.Left, env, inEffect)
		if !inEffect {
			c.diagnostic("EF105", "run is only valid inside effect functions", e.Span)
		}
		if c.expect(inner.isEffect(), "EF105", "run requires an Effect value", e.Span, inner) {
			c.requireOpenHeldCaptures(inner, e.Span)
		}
		t = inner.clone()
		t.value = c.checkedDataID(inner.resultID(), nil, nil).value
		t.application = nil
		t.identity = ""
		// exec(L, Lexical(region)): the current region owns what layer 0
		// acquires, and a produced recipe keeps its own layers. The body
		// raises layer 0's failures with their payload evidence and uses
		// the handles layer 0 holds (design §4.2, §5.2 F2).
		var payloads map[string][]OwnershipFact
		var pending failureEvidence
		if inner.isEffect() {
			executed, raised, unobserved := c.executeLayer(inner, lexicalOwner(c.region))
			t.setOwnership(executed.ownershipFacts())
			t.setCaptures(executed.captureFacts())
			t.fields = executed.fields
			t.failures = executed.failures
			payloads, pending = raised, unobserved
		}
		t.observes, t.forks = nil, nil
		executedLayer := c.evaluationWith(inner.failureRow(), inner.serviceRow(), payloads, heldAt(inner.captureFacts(), 0))
		executedLayer.pending = atCall(pending, e.Span.Offset)
		t.evaluation = c.unionEvaluationFacts(inner.evaluation, executedLayer)
		// Building the recipe evaluates its operands first, which may
		// observe children or exit; executing a join or interrupt then
		// observes its child before both of its exits.
		t.observe(sequenceObservations(c.observationOf(inner), observation{kills: inner.observes, exitKills: inner.observes, exits: len(c.rowLabels(inner.failureRow())) > 0}))
		if len(c.rowLabels(inner.failureRow())) > 0 {
			c.reasons = append(c.reasons, Contribution{"failure", c.rowLabels(inner.failureRow()), e.Span})
		}
		if len(c.rowLabels(inner.serviceRow())) > 0 {
			c.reasons = append(c.reasons, Contribution{"requirement", c.rowLabels(inner.serviceRow()), e.Span})
		}
	case "provideLayer":
		t = c.provideLayer(e, env, inEffect)
	case "codec":
		t = c.codecOperation(e, env)
	case "intrinsic":
		t = c.intrinsicOperation(e, env)
	case "provide":
		t = c.expr(e.Left, env, inEffect)
		if c.abstractRow(t.serviceRow()) {
			c.diagnostic("EF125", "provision of an abstract row requires an unsupported row difference constraint", e.Span)
		}
		provider := c.expr(e.Right, env, inEffect)
		c.expect(t.isEffect(), "EF105", "provide requires an Effect value", e.Span, t)
		if service := c.services[e.Name]; service == nil {
			c.diagnostic("EF102", "unknown service "+e.Name, e.Span)
		} else {
			c.observeReference(e, e.NameSpan, lexicalTarget{kind: "service", service: service})
		}
		providerNode := provider.node()
		c.expect(providerNode != nil && providerNode.Kind == "provider" && providerNode.Name == e.Name && !provider.isEffect(), "EF104", "provider must implement "+e.Name, e.Right.Span, provider)
		t.setCaptures(normalizeFacts(append(t.captureFacts(), provider.captureFacts()...)))
		serviceLabels := remove(c.rowLabels(t.serviceRow()), e.Name)
		t.value = c.recontractRows(t, t.failureRow(), c.internRow(serviceLabels))
	case "catch":
		t = c.expr(e.Left, env, inEffect)
		if c.abstractRow(t.failureRow()) {
			c.diagnostic("EF125", "recovery of an abstract row requires an unsupported row difference constraint", e.Span)
		}
		fallback := c.expr(e.Right, env, false)
		c.expect(t.isEffect(), "EF105", "catch requires an Effect value", e.Span, t)
		if _, exists := c.program.Errors[e.Name]; !exists {
			c.diagnostic("EF102", "unknown failure "+e.Name, e.Span)
		} else {
			c.observeReference(e, e.NameSpan, lexicalTarget{kind: "error", failure: c.errors[e.Name]})
			c.expect(c.hasRow(t, true, e.Name), "EF107", "effect does not admit failure "+e.Name, e.Span, t)
		}
		c.expect(!fallback.isEffect() && c.sameResultType(fallback, t), "EF106", "prototype catch fallback must be a pure "+c.displayTypeID(t.resultID()), e.Right.Span, t, fallback)
		// Recovery can publish the fallback value on the handled-failure
		// branch. Preserve both its returned ownership and any provider
		// captures; dropping either branch turns a closed-owner escape into a
		// false safe result. The fallback is the success of the recovered
		// layer, so it joins under that layer's binder. A payload catch
		// discards is never dereferenced (design §5.5).
		if t.isEffect() && !c.poisoned(fallback) {
			lifted := c.liftOccurrence(fallback)
			lifted.value = c.values.recipe(fallback.valueID(), checkedEffectCallable, emptyRowID, emptyRowID, lifted.ownershipFacts(), lifted.captureFacts())
			fallback = lifted
		}
		observes := t.observes
		t = c.joinOccurrence(t, fallback, e.Span)
		t.observes = observes
		// The fallback handles every ordinary raise of the label.
		t.failures = handleWith(withLayerPayloads(t.failures, 0, withoutLabel(layerPayloads(t.failures, 0), e.Name)), e.Name, nil)
		t.value = c.recontractRows(t, c.internRow(c.handledRow(t, e.Name)), t.serviceRow())
		t = c.completeHandled(t, e.Name)
	case "recover":
		t = c.recoverFailure(e, env, inEffect)
	case "construct":
		t = c.construct(e, env, inEffect)
	case "match":
		t = c.match(e, env, inEffect)
	case "unary":
		operand := c.expr(e.Left, env, inEffect)
		node := operand.node()
		valid := e.Name == "-" && !operand.isEffect() && node != nil && node.Kind == "primitive" && node.Name == "i64"
		if !valid {
			c.expect(false, "EF106", "unary - requires an i64 value", e.Span, operand)
			t = c.checkedData("invalid")
		} else {
			t = c.checkedData("i64")
		}
	case "binary":
		left, right := c.expr(e.Left, env, inEffect), c.expr(e.Right, env, inEffect)
		leftNode, rightNode := left.node(), right.node()
		valid := !left.isEffect() && !right.isEffect() && c.sameValues(left, right) && leftNode != nil && rightNode != nil && leftNode.Kind == "primitive" && rightNode.Kind == "primitive"
		if valid {
			switch e.Name {
			case "==":
				valid = leftNode.Name == "string" || leftNode.Name == "bool" || leftNode.Name == "i64"
			case "+":
				valid = leftNode.Name == "string" || leftNode.Name == "i64"
			case "-", "*", "/", "%", "<", "<=", ">", ">=":
				valid = leftNode.Name == "i64"
			default:
				valid = false
			}
		}
		if c.expect(valid, "EF106", "operator requires matching primitive values; + accepts strings or i64, and -, *, /, % and ordered comparisons accept i64", e.Span, left, right) && (e.Name == "/" || e.Name == "%") && !c.divisorProof(e.Right) {
			c.diagnostic("EF150", divisorRefusal(e), e.Right.Span)
		}
		if valid {
			if e.Name == "<" || e.Name == "<=" || e.Name == ">" || e.Name == ">=" {
				t = c.checkedData("bool")
			} else {
				t = c.checkedDataID(left.valueID(), nil, nil)
			}
		} else {
			t = c.checkedData("invalid")
		}
		if e.Name == "==" {
			t = c.checkedData("bool")
		}
	case "if":
		condition := c.expr(e.Left, env, inEffect)
		conditionNode := condition.node()
		c.expect(!condition.isEffect() && conditionNode != nil && conditionNode.Kind == "primitive" && conditionNode.Name == "bool", "EF106", "if condition must be bool", e.Left.Span, condition)
		thenProof, elseProof := nonzeroBranchProofs(e.Left)
		a := c.withNonzero(thenProof, func() checkedExpression { return c.branchBlock(e.Then, env, inEffect) })
		b := c.withNonzero(elseProof, func() checkedExpression { return c.branchBlock(e.Else, env, inEffect) })
		// The arms are alternatives: the children one forks never exist
		// together with the other's.
		a.evaluation = c.alternativeEvaluation(a.evaluation, e.Span.Offset, 0)
		b.evaluation = c.alternativeEvaluation(b.evaluation, e.Span.Offset, 1)
		// A never branch contributes no value and an already-reported invalid
		// branch takes the other branch's contract, as unification would.
		if c.isKind(a, "never") || (c.poisoned(a) && !c.isKind(b, "never")) {
			t = b
		} else if c.isKind(b, "never") || c.poisoned(b) {
			t = a
		} else {
			t = c.joinOccurrence(a, b, e.Span)
			if !c.sameValues(a, b) || a.isEffect() != b.isEffect() {
				c.diagnostic("EF106", "if branches must return the same type", e.Span)
			} else {
				t = c.joinContractRows(t, b)
			}
		}
		t.evaluation = c.unionEvaluationFacts(condition.evaluation, c.unionEvaluationFacts(a.evaluation, b.evaluation))
		// The condition evaluates, then either arm runs: only what both
		// arms observe is observed, and a failing condition exits before
		// either arm.
		t.observe(sequenceObservations(c.observationOf(condition), alternativeObservations(c.observationOf(a), c.observationOf(b))))
	default:
		c.diagnostic("EF103", "unsupported expression "+e.Kind, e.Span)
	}
	// Every checked expression owns one evaluation summary. Child summaries are
	// shared through this node instead of being recomputed by each consumer.
	t.evaluation = c.unionEvaluationFacts(t.evaluation, c.childEvaluation(e))
	switch e.Kind {
	case "run", "scope", "if", "match":
		// These compose their own observations above.
	default:
		t.observe(c.childObservation(e))
	}
	if t.callableDecl == nil && t.application == nil {
		t.identity = c.typeNodeID(t.contractID())
	}
	if e.Kind == "run" || e.Kind == "if" || e.Kind == "match" || e.Kind == "scope" || e.Kind == "fork" {
		t.executed = t.evaluation
	} else if e.Kind == "construct" || e.Kind == "payload" {
		// Constructor payloads are required to be pure. Keep their invalid child
		// evaluation facts available for tooling without admitting those rows
		// into the enclosing body contract after the purity diagnostic.
		t.executed = c.evaluation(emptyRowID, emptyRowID)
	} else {
		t.executed = t.evaluation
	}
	e.checked = t.clone()
	e.Type = c.projectCheckedBase(t)
	e.Evaluation = c.projectEvaluation(t.evaluation)
	e.Identity = t.identity
	if e.Kind == "run" || e.Kind == "if" || e.Kind == "match" || e.Kind == "scope" || e.Kind == "fork" {
		e.Executed = c.projectEvaluation(t.executed)
	} else if e.Kind == "construct" || e.Kind == "payload" {
		e.Executed = EvaluationRows{}
	} else {
		e.Executed = e.Evaluation
	}
	if c.recordFacts {
		c.result.facts[e] = ExpressionFacts{Checked: t.clone(), Type: e.Type, Evaluation: e.Evaluation, Executed: e.Executed}
	}
	return t
}

func (c *checker) dataCall(e *Expr, env localEnv, inEffect bool) (checkedExpression, bool) {
	if e.Left == nil {
		return checkedExpression{}, false
	}
	if value, ok := c.templateConstruct(e, env, inEffect); ok {
		return value, true
	}
	if e.Left.constructorType != nil {
		c.diagnostic("EF127", "explicit application syntax requires a data constructor", e.Span)
		return c.checkedData("invalid"), true
	}
	typeName, variantName := "", ""
	switch e.Left.Kind {
	case "name":
		typeName = e.Left.Name
	case "member":
		if e.Left.Left.Kind != "name" {
			return checkedExpression{}, false
		}
		typeName, variantName = e.Left.Left.Name, e.Left.Name
	default:
		return checkedExpression{}, false
	}
	if variantName == "" && c.errors[typeName] != nil {
		c.diagnostic("EF102", "error declarations are failure payloads, not success values", e.Span)
		return c.checkedData("invalid"), true
	}
	fields, ok := fieldsFor(c, typeName, variantName)
	if !ok || (variantName == "" && c.enums[typeName] != nil) {
		if variantName != "" && c.enums[typeName] == nil {
			return checkedExpression{}, false
		}
		if variantName == "" && c.records[typeName] == nil {
			return checkedExpression{}, false
		}
	}
	if variantName != "" {
		enum := c.enums[typeName]
		if enum == nil {
			return checkedExpression{}, false
		}
		found := false
		for _, variant := range enum.Variants {
			found = found || variant.Name == variantName
		}
		if !found {
			c.diagnostic("EF116", "unknown variant "+typeName+"."+variantName, e.Span)
			return c.checkedData("invalid"), true
		}
	}
	owner := c.observeDataHead(e.Left, typeName, variantName)
	if len(e.Fields) > 0 {
		if len(e.Fields) != len(e.Args) {
			c.diagnostic("EF122", "constructor arguments cannot mix named and positional forms", e.Span)
			for _, arg := range e.Args {
				c.expr(arg, env, false)
			}
			e.Text = "data"
			return c.checkedData("invalid"), true
		}
		c.observeFieldLabels(e, e.Fields, fields, owner)
		payloadExpr := &Expr{Kind: "payload", Fields: e.Fields, Span: e.Span}
		ownership := c.payload(payloadExpr, fields, env, e.Span)
		e.Text = "data"
		result := c.checkedDataID(c.canonicalRef(typeRef(typeName)), nil, nil)
		if variantName != "" {
			result.setOwnership(prependFacts(variantName, ownership))
		} else {
			result.setOwnership(ownership)
		}
		c.retainDataPayload(&result, e.Fields, variantName)
		return result, true
	}
	if len(e.Args) != len(fields) {
		c.diagnostic("EF115", "constructor "+typeName+" expects "+fmt.Sprint(len(fields))+" payload fields", e.Span)
	}
	argumentTypes := make([]checkedExpression, len(e.Args))
	for i, arg := range e.Args {
		got := c.expr(arg, env, false)
		argumentTypes[i] = got
		if i < len(fields) && !c.sameType(got, fields[i].Type) {
			c.diagnostic("EF115", "payload field "+fields[i].Name+" must be "+fields[i].Type, arg.Span)
		}
	}
	e.Text = "data"
	e.Fields = make([]FieldValue, 0, len(e.Args))
	for i, arg := range e.Args {
		if i < len(fields) {
			e.Fields = append(e.Fields, FieldValue{Name: fields[i].Name, Value: arg, Span: arg.Span})
		}
	}
	result := c.checkedDataID(c.canonicalRef(typeRef(typeName)), nil, nil)
	ownership := []OwnershipFact{}
	for i, got := range argumentTypes {
		if i < len(fields) {
			ownership = append(ownership, prependFacts(fields[i].Name, heldFacts(got, 0))...)
		}
	}
	if variantName != "" {
		result.setOwnership(prependFacts(variantName, ownership))
	} else {
		result.setOwnership(normalizeFacts(ownership))
	}
	c.retainDataPayload(&result, e.Fields, variantName)
	return result, true
}

func (c *checker) construct(e *Expr, env localEnv, inEffect bool) checkedExpression {
	if value, ok := c.templateConstruct(e, env, inEffect); ok {
		return value
	}
	if e.Left == nil {
		return c.checkedData("invalid")
	}
	typeName, variantName := "", ""
	if e.Left.Kind == "name" {
		typeName = e.Left.Name
	} else if e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
		typeName, variantName = e.Left.Left.Name, e.Left.Name
	} else {
		c.diagnostic("EF114", "invalid data constructor", e.Span)
		return c.checkedData("invalid")
	}
	if variantName == "" && c.errors[typeName] != nil {
		c.diagnostic("EF102", "error declarations are failure payloads, not success values", e.Span)
		return c.checkedData("invalid")
	}
	fields, ok := fieldsFor(c, typeName, variantName)
	if !ok {
		if variantName != "" && c.enums[typeName] != nil {
			c.diagnostic("EF116", "unknown variant "+typeName+"."+variantName, e.Span)
		} else if variantName != "" || !c.absentName(absentInBlock, typeName, e.Left.Span) {
			c.diagnostic("EF102", "unknown data declaration "+typeName, e.Span)
		}
		return c.checkedData("invalid")
	}
	if variantName != "" {
		if c.enums[typeName] == nil {
			c.diagnostic("EF116", typeName+" is not a closed enum", e.Span)
			return c.checkedData("invalid")
		}
	}
	c.observeFieldLabels(e, e.Fields, fields, c.observeDataHead(e.Left, typeName, variantName))
	ownership := c.payload(e, fields, env, e.Span)
	result := c.checkedData(typeName)
	if variantName != "" {
		result.setOwnership(prependFacts(variantName, ownership))
	} else {
		result.setOwnership(ownership)
	}
	c.retainDataPayload(&result, e.Fields, variantName)
	return result
}

// observeDataHead records the record, enum and variant tokens of an admitted
// non-generic constructor head and returns the owner of its payload fields.
func (c *checker) observeDataHead(head *Expr, typeName, variantName string) lexicalTarget {
	if variantName == "" {
		record := c.records[typeName]
		c.observeReference(head, head.Span, lexicalTarget{kind: "record", data: record})
		return lexicalTarget{data: record}
	}
	enum := c.enums[typeName]
	c.observeReference(head.Left, head.Left.Span, lexicalTarget{kind: "enum", data: enum})
	c.observeReference(head, head.Span, lexicalTarget{kind: "variant", data: enum, variant: variantName})
	return lexicalTarget{data: enum, variant: variantName}
}

// observePattern records the enum and variant segments of a pattern the
// checker admitted against enum. A three-segment pattern is alias-qualified.
func (c *checker) observePattern(pattern *MatchPattern, enum *Enum) {
	segments := pattern.Segments
	if len(segments) == 3 && c.result.lexical != nil {
		// templateByName resolved the same alias-qualified owner name.
		alias, _, _ := strings.Cut(pattern.TypeName, ".")
		if item := c.result.lexical.aliases[alias]; item != nil {
			c.observeReference(pattern, segments[0], lexicalTarget{kind: "module", module: item})
		}
		segments = segments[1:]
	}
	if len(segments) == 0 {
		return
	}
	c.observeReference(pattern, segments[0], lexicalTarget{kind: enum.Kind, data: enum})
	if len(segments) == 2 && slices.ContainsFunc(enum.Variants, func(v Variant) bool { return v.Name == pattern.VariantName }) {
		c.observeReference(pattern, segments[1], lexicalTarget{kind: "variant", data: enum, variant: pattern.VariantName})
		for _, name := range pattern.Names {
			// An unaliased field token is the binding's own declaration.
			if name.FieldSpan != name.NameSpan {
				c.observeReference(pattern, name.FieldSpan, lexicalTarget{kind: "field", data: enum, variant: pattern.VariantName, field: name.Field})
			}
		}
	}
}
func (r *Result) Find(name string) *Symbol {
	identity := ""
	if alias, member, ok := strings.Cut(name, "."); ok && r.Program != nil {
		if f := r.Program.BundledBindings[alias][member]; f != nil {
			identity = f.Identity
		}
	}
	for i := range r.Symbols {
		if r.Symbols[i].Name == name || identity != "" && r.Symbols[i].Identity == identity {
			if r.projector != nil && r.Symbols[i].Contract.ProjectionError != "" {
				checked := r.checkedSymbols[r.Symbols[i].Identity]
				if _, err := r.projector.checkedSymbolSize(r.Symbols[i], checked, r.projectionLimits().CompatibilityBytes); err == nil {
					selected := r.Symbols[i]
					selected.Contract = r.projector.projectChecked(checked.contract)
					selected.Actual = r.projector.projectChecked(checked.body)
					selected.Params = publicParams(checked.declaration.Params)
					return &selected
				}
			}
			return &r.Symbols[i]
		}
	}
	return nil
}
func (r *Result) FindDeclaration(name string) *Declaration {
	identity := name
	if alias, member, qualified := strings.Cut(name, "."); qualified && r.Program != nil {
		if template := r.Program.BundledTypeBindings[alias][member]; template != nil {
			identity = template.Identity
		}
	}
	for i := range r.Declarations {
		if r.Declarations[i].Identity == identity {
			return &r.Declarations[i]
		}
	}
	for i := range r.Declarations {
		if r.Declarations[i].Name == name && r.Declarations[i].Source == "" {
			return &r.Declarations[i]
		}
	}
	return nil
}
func (r *Result) Entry() error {
	if !r.Checked {
		return fmt.Errorf("source has diagnostics")
	}
	main := r.Find("main")
	if main == nil {
		return fmt.Errorf("entry requires an effect fn main")
	}
	if !main.Contract.Effect || len(main.Params) > 0 {
		return fmt.Errorf("main must be an effect function with no parameters")
	}
	if len(main.Contract.Services) > 0 {
		return fmt.Errorf("main has unprovided services: %s", strings.Join(main.Contract.Services, ", "))
	}
	return nil
}

func (c *checker) fiberCall(e *Expr, env localEnv, inEffect bool) bool {
	if e.Left.Kind != "member" || e.Left.Left.Kind != "name" {
		return false
	}
	inner, exists := env[e.Left.Left.binding]
	if !exists || !c.isKind(inner, "fiber") {
		return false
	}
	// Resolve the receiver through the ordinary local-read owner so its
	// checked fact and lexical binding observation match other environment uses.
	inner = c.expr(e.Left.Left, env, inEffect)
	if len(e.Args) != 0 {
		c.diagnostic("EF106", "fiber operations take no arguments", e.Span)
	}
	node := inner.node()
	if node == nil || len(node.Args) != 1 {
		return false
	}
	resultID := node.Args[0]
	failureRow := inner.failureRow()
	// Every fiber operation is a recipe holding the Fiber handle. A joined
	// recipe's success is the child's result: its evidence (already owned by
	// the closed child owner) moves under the joined recipe's binder.
	fiber := heldFacts(inner, 0)
	// join and interrupt raise the child's failures, owned by the closed
	// child owner (design §5.2 F11, F12): interrupt re-raises a child cause
	// which is not an interruption (fiber.go Interrupt).
	joined := c.completeFailures(inner).failures
	childFailures := layerPayloads(joined, 0)
	t := checkedExpression{value: c.values.recipe(resultID, checkedEffectCallable, failureRow, emptyRowID, nil, fiber)}
	switch e.Left.Name {
	case "join":
		result := c.liftOccurrence(c.checkedDataID(resultID, cloneFacts(inner.child), inner.captureFacts()))
		t.setOwnership(result.ownershipFacts())
		t.setCaptures(normalizeFacts(append(cloneFacts(fiber), result.captureFacts()...)))
		t.failures = joined
	case "interrupt":
		t = checkedExpression{value: c.values.recipe(c.canonicalRef(typeRef(voidTypeName)), checkedEffectCallable, failureRow, emptyRowID, nil, fiber)}
		t.failures = withLayerPayloads(nil, 0, childFailures)
	case "cancel":
		t = checkedExpression{value: c.values.recipe(c.canonicalRef(typeRef(voidTypeName)), checkedEffectCallable, emptyRowID, emptyRowID, nil, fiber)}
	default:
		c.diagnostic("EF102", "unknown fiber operation "+e.Left.Name, e.Span)
	}
	// join and interrupt wait for the child and mark it observed before
	// either of their exits (design §5.3 rule 2). A Fiber which may denote
	// several fork instances observes none of them; cancel never waits.
	if (e.Left.Name == "join" || e.Left.Name == "interrupt") && len(inner.forks) == 1 {
		t.observes = []string{inner.forks[0]}
	}
	e.Text = "fiber"
	e.checked = t.clone()
	e.Type = c.projectChecked(t)
	return true
}

func (c *checker) requireGo(span Span, feature string) {
	c.program.GoOnly = true
	if c.result.Target != "go" {
		c.diagnostic("EF110", feature+" is currently implemented only for Go", span)
	}
}

// builtinCallback returns a builtin callback contract only where the program
// admits the contract it belongs to. HttpHandler is part of the Http contract
// with HttpRequest and HttpReply, which it names: without a reference to Http
// it is an ordinary unknown type and a free name.
func (c *checker) builtinCallback(name string) (struct{ Parameter, Result string }, bool) {
	return c.program.callback(name)
}
