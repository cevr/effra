package compiler

import "fmt"

// checkedValueKind describes the semantic category of a complete value
// contract. The structural node itself is owned by checker.typeIntern; this
// enum is derived from that node and is never a second type authority.
type checkedValueKind uint8

const (
	checkedDataValue checkedValueKind = iota
	checkedCallableValue
	checkedRecipeValue
	checkedFiberValue
	checkedProviderRecipeValue
	checkedProviderValue
)

// checkedCallableKind is part of a callable contract. Pure and effectful
// callables have different admissibility rules and must not be represented by
// a display flag that can drift from the canonical semantic node.
type checkedCallableKind uint8

const (
	checkedNonCallable checkedCallableKind = iota
	checkedPureCallable
	checkedEffectCallable
)

func (k checkedCallableKind) mode() string {
	switch k {
	case checkedPureCallable:
		return "pure"
	case checkedEffectCallable:
		return "effect"
	default:
		return ""
	}
}

func callableKindForMode(mode string) checkedCallableKind {
	switch mode {
	case "pure":
		return checkedPureCallable
	case "effect":
		return checkedEffectCallable
	default:
		return checkedNonCallable
	}
}

// CheckedValue is an occurrence of one canonical semantic contract. The
// contract ID points into the checker's existing semantic type arena. Only
// ownership/capture facts vary per occurrence; they are copied at both input
// and accessor boundaries so callers cannot mutate a checked value.
type CheckedValue struct {
	arena     *checkedValueArena
	contract  TypeID
	ownership []OwnershipFact
	captures  []OwnershipFact
}

func (v CheckedValue) contractID() TypeID { return v.contract }

func (v CheckedValue) node() *semanticTypeNode {
	if v.arena == nil || v.arena.checker == nil {
		return nil
	}
	return v.arena.checker.node(v.contract)
}

func (v CheckedValue) resultID() TypeID {
	node := v.node()
	if node == nil {
		return invalidTypeID
	}
	switch node.Kind {
	case "callable", "recipe", "providerRecipe":
		return node.Result
	case "fiber":
		if len(node.Args) == 1 {
			return node.Args[0]
		}
	}
	return v.contract
}

func (v CheckedValue) kind() checkedValueKind {
	node := v.node()
	if node == nil {
		return checkedDataValue
	}
	switch node.Kind {
	case "callable":
		return checkedCallableValue
	case "recipe":
		return checkedRecipeValue
	case "fiber":
		return checkedFiberValue
	case "providerRecipe":
		return checkedProviderRecipeValue
	case "provider":
		return checkedProviderValue
	default:
		return checkedDataValue
	}
}

func (v CheckedValue) callableKind() checkedCallableKind {
	node := v.node()
	if node == nil {
		return checkedNonCallable
	}
	return callableKindForMode(node.Mode)
}

func (v CheckedValue) failureRow() RowID {
	if node := v.node(); node != nil {
		return node.FailureRow
	}
	return emptyRowID
}

func (v CheckedValue) serviceRow() RowID {
	if node := v.node(); node != nil {
		return node.ServiceRow
	}
	return emptyRowID
}

func (v CheckedValue) ownershipFacts() []OwnershipFact { return cloneFacts(v.ownership) }
func (v CheckedValue) captureFacts() []OwnershipFact   { return cloneFacts(v.captures) }

func (v CheckedValue) withOccurrenceFacts(ownership, captures []OwnershipFact) CheckedValue {
	if v.arena == nil {
		return CheckedValue{contract: v.contract, ownership: cloneFacts(ownership), captures: cloneFacts(captures)}
	}
	return v.arena.occurrence(v.contract, ownership, captures)
}

// checkedValueArena is an adapter over checker.typeIntern. It has no private
// node map: every constructor below interns the complete structural contract
// into the same semanticTypeNode arena used by checking, emission, and query.
type checkedValueArena struct {
	checker *checker
}

func newCheckedValueArena(c *checker) *checkedValueArena {
	return &checkedValueArena{checker: c}
}

func (a *checkedValueArena) requireType(id TypeID) *semanticTypeNode {
	if a == nil || a.checker == nil {
		panic("checked value arena is not attached to a checker")
	}
	node := a.checker.node(id)
	if node == nil {
		panic(fmt.Sprintf("checked value constructor received unknown type ID %d (nodes=%d)", id, len(a.checker.typeNodes)))
	}
	return node
}

func (a *checkedValueArena) requireRow(id RowID) {
	if id == emptyRowID {
		return
	}
	if a == nil || a.checker == nil || int(id) > len(a.checker.rows) {
		panic("checked value constructor received an unknown row ID")
	}
}

func (a *checkedValueArena) occurrence(contract TypeID, ownership, captures []OwnershipFact) CheckedValue {
	a.requireType(contract)
	return CheckedValue{arena: a, contract: contract, ownership: cloneFacts(ownership), captures: cloneFacts(captures)}
}

func (a *checkedValueArena) validateCallableRows(kind checkedCallableKind, failure, service RowID) {
	a.requireRow(failure)
	a.requireRow(service)
	switch kind {
	case checkedPureCallable:
		if failure != emptyRowID || service != emptyRowID {
			panic("pure callable cannot carry failure or service rows")
		}
	case checkedEffectCallable:
		// Effect rows are incurred when the callable runs. Constructing a recipe
		// remains an empty ExpressionEvaluation and does not union these rows.
	default:
		panic("callable constructor requires pure or effect callable kind")
	}
}

func (a *checkedValueArena) data(result TypeID, ownership, captures []OwnershipFact) CheckedValue {
	node := a.requireType(result)
	switch node.Kind {
	case "callable", "recipe", "providerRecipe", "fiber", "provider":
		panic("data constructor received a value-contract node")
	}
	return a.occurrence(result, ownership, captures)
}

func (a *checkedValueArena) callable(result TypeID, parameters []TypeID, kind checkedCallableKind, failure, service RowID, ownership, captures []OwnershipFact) CheckedValue {
	a.requireType(result)
	for _, parameter := range parameters {
		a.requireType(parameter)
	}
	a.validateCallableRows(kind, failure, service)
	contract := a.checker.internContract("callable", kind.mode(), result, parameters, failure, service)
	return a.occurrence(contract, ownership, captures)
}

func (a *checkedValueArena) recipe(result TypeID, parameters []TypeID, kind checkedCallableKind, failure, service RowID, ownership, captures []OwnershipFact) CheckedValue {
	a.requireType(result)
	for _, parameter := range parameters {
		a.requireType(parameter)
	}
	a.validateCallableRows(kind, failure, service)
	contract := a.checker.internContract("recipe", kind.mode(), result, parameters, failure, service)
	return a.occurrence(contract, ownership, captures)
}

func (a *checkedValueArena) fiber(result TypeID, failure RowID, ownership, captures []OwnershipFact) CheckedValue {
	a.requireType(result)
	a.requireRow(failure)
	contract := a.checker.internTypeWithRows("fiber", "", []TypeID{result}, failure, emptyRowID)
	return a.occurrence(contract, ownership, captures)
}

func (a *checkedValueArena) providerRecipe(provider TypeID, failure, service RowID, ownership, captures []OwnershipFact) CheckedValue {
	node := a.requireType(provider)
	if node.Kind != "provider" {
		panic("provider recipe constructor requires a provider type")
	}
	a.requireRow(failure)
	a.requireRow(service)
	contract := a.checker.internContract("providerRecipe", checkedEffectCallable.mode(), provider, nil, failure, service)
	return a.occurrence(contract, ownership, captures)
}

func (a *checkedValueArena) provider(provider TypeID, ownership, captures []OwnershipFact) CheckedValue {
	node := a.requireType(provider)
	if node.Kind != "provider" {
		panic("provider constructor requires a provider type")
	}
	return a.occurrence(provider, ownership, captures)
}

// ExpressionEvaluation stores only canonical row IDs. It is deliberately
// separate from CheckedValue: constructing a recipe does not incur its rows,
// while run/fail/branch expressions can contribute rows to their own fact.
type ExpressionEvaluation struct {
	failureRow RowID
	serviceRow RowID
}

func (e ExpressionEvaluation) failureRowID() RowID { return e.failureRow }
func (e ExpressionEvaluation) serviceRowID() RowID { return e.serviceRow }

func (c *checker) evaluation(failureRow, serviceRow RowID) ExpressionEvaluation {
	if failureRow != emptyRowID && int(failureRow) > len(c.rows) {
		panic("expression evaluation received an unknown failure row")
	}
	if serviceRow != emptyRowID && int(serviceRow) > len(c.rows) {
		panic("expression evaluation received an unknown service row")
	}
	return ExpressionEvaluation{failureRow: failureRow, serviceRow: serviceRow}
}

func (c *checker) evaluationFromLabels(failures, services []string) ExpressionEvaluation {
	return c.evaluation(c.internRow(failures), c.internRow(services))
}

func (c *checker) unionEvaluationFacts(a, b ExpressionEvaluation) ExpressionEvaluation {
	return c.evaluation(
		c.internRow(union(c.rowLabels(a.failureRow), c.rowLabels(b.failureRow))),
		c.internRow(union(c.rowLabels(a.serviceRow), c.rowLabels(b.serviceRow))),
	)
}
