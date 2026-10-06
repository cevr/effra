package compiler

import (
	"slices"
	"testing"
)

func newCheckedValueTestChecker() *checker {
	c := &checker{
		typeIntern:     map[string]*semanticTypeNode{},
		typePublicIDs:  map[TypeID]string{},
		typePublicToID: map[string]TypeID{},
		rowIntern:      map[string]RowID{},
		nextTypeID:     1,
		nextRowID:      1,
	}
	c.values = newCheckedValueArena(c)
	return c
}

func TestCheckedValueConstructorsUseOneCanonicalTypeArena(t *testing.T) {
	c := newCheckedValueTestChecker()
	arena := c.values
	result := c.internType("primitive", "string", nil)
	parameter := c.internType("primitive", "i64", nil)
	provider := c.internTypeWithDeclaration("provider", "Users", nil, "provider:module:file:Users")
	otherProvider := c.internTypeWithDeclaration("provider", "Users", nil, "provider:module:file:Other")
	failure := c.internRow([]string{"MissingUser"})
	service := c.internRow([]string{"Users"})
	ownership := []OwnershipFact{{Path: "file", Status: "borrowed", Region: "parameter:file", Origin: "parameter"}}
	captures := []OwnershipFact{{Path: "child", Status: "owned", Region: "invocation", Origin: "invocation-result"}}

	data := arena.data(result, ownership, captures)
	pure := arena.callable(result, []TypeID{parameter}, checkedPureCallable, emptyRowID, emptyRowID, ownership, captures)
	effect := arena.callable(result, []TypeID{parameter}, checkedEffectCallable, failure, service, ownership, captures)
	recipe := arena.recipe(result, []TypeID{parameter}, checkedEffectCallable, failure, service, ownership, captures)
	fiber := arena.fiber(result, failure, ownership, captures)
	providerRecipe := arena.providerRecipe(provider, failure, service, ownership, captures)
	otherProviderRecipe := arena.providerRecipe(otherProvider, failure, service, ownership, captures)
	providerValue := arena.provider(provider, ownership, captures)

	cases := []struct {
		name       string
		value      CheckedValue
		kind       checkedValueKind
		callable   checkedCallableKind
		result     TypeID
		failureRow RowID
		serviceRow RowID
	}{
		{name: "data", value: data, kind: checkedDataValue, callable: checkedNonCallable, result: result},
		{name: "pure callable", value: pure, kind: checkedCallableValue, callable: checkedPureCallable, result: result},
		{name: "effect callable", value: effect, kind: checkedCallableValue, callable: checkedEffectCallable, result: result, failureRow: failure, serviceRow: service},
		{name: "recipe", value: recipe, kind: checkedRecipeValue, callable: checkedEffectCallable, result: result, failureRow: failure, serviceRow: service},
		{name: "fiber", value: fiber, kind: checkedFiberValue, callable: checkedNonCallable, result: result, failureRow: failure},
		{name: "provider recipe", value: providerRecipe, kind: checkedProviderRecipeValue, callable: checkedEffectCallable, result: provider, failureRow: failure, serviceRow: service},
		{name: "provider", value: providerValue, kind: checkedProviderValue, callable: checkedNonCallable, result: provider},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value.kind() != tc.kind || tc.value.callableKind() != tc.callable {
				t.Fatalf("kind = (%d, %d), want (%d, %d)", tc.value.kind(), tc.value.callableKind(), tc.kind, tc.callable)
			}
			if tc.value.resultID() != tc.result || tc.value.failureRow() != tc.failureRow || tc.value.serviceRow() != tc.serviceRow {
				t.Fatalf("canonical accessors = result %d, rows (%d, %d); want result %d, rows (%d, %d)", tc.value.resultID(), tc.value.failureRow(), tc.value.serviceRow(), tc.result, tc.failureRow, tc.serviceRow)
			}
			if tc.value.node() != c.node(tc.value.contractID()) {
				t.Fatal("checked value does not point into the checker's semantic node arena")
			}
			if !slices.Equal(tc.value.ownershipFacts(), ownership) || !slices.Equal(tc.value.captureFacts(), captures) {
				t.Fatalf("constructor lost occurrence facts: ownership=%+v captures=%+v", tc.value.ownershipFacts(), tc.value.captureFacts())
			}
		})
	}

	// Repeated construction is structural interning, while any complete
	// contract change receives a different canonical ID.
	equivalent := arena.callable(result, []TypeID{parameter}, checkedEffectCallable, failure, service, nil, nil)
	if equivalent.contractID() != effect.contractID() {
		t.Fatalf("equivalent callable contracts were not interned: %d != %d", equivalent.contractID(), effect.contractID())
	}
	otherFailure := c.internRow([]string{"OtherFailure"})
	if arena.callable(result, []TypeID{parameter}, checkedEffectCallable, otherFailure, service, nil, nil).contractID() == effect.contractID() {
		t.Fatal("callable contracts with different failure rows shared an ID")
	}
	if arena.callable(result, nil, checkedEffectCallable, failure, service, nil, nil).contractID() == effect.contractID() {
		t.Fatal("callable contracts with different parameter IDs shared an ID")
	}
	if arena.recipe(result, []TypeID{parameter}, checkedEffectCallable, failure, service, nil, nil).contractID() == effect.contractID() {
		t.Fatal("callable and recipe contracts shared an ID")
	}
	if otherProviderRecipe.contractID() == providerRecipe.contractID() || arena.provider(otherProvider, nil, nil).contractID() == providerValue.contractID() {
		t.Fatal("provider declaration identity was omitted from the canonical contract")
	}

	// Recipe construction is pure evaluation even though executing the recipe
	// carries the effect callable's rows.
	evaluation := c.evaluation(emptyRowID, emptyRowID)
	if evaluation.failureRowID() != emptyRowID || evaluation.serviceRowID() != emptyRowID {
		t.Fatalf("recipe construction incurred execution rows: %+v", evaluation)
	}
}

func TestCheckedValueRejectsInvalidCallableRowsBeforeInterning(t *testing.T) {
	c := newCheckedValueTestChecker()
	arena := c.values
	result := c.internType("primitive", "string", nil)
	failure := c.internRow([]string{"Failure"})
	service := c.internRow([]string{"Users"})
	before := len(c.typeNodes)

	for _, tc := range []struct {
		name    string
		failure RowID
		service RowID
	}{
		{name: "failure", failure: failure},
		{name: "service", service: service},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mustPanic(t, func() {
				arena.callable(result, nil, checkedPureCallable, tc.failure, tc.service, nil, nil)
			})
			if len(c.typeNodes) != before {
				t.Fatalf("invalid pure callable was interned: nodes before=%d after=%d", before, len(c.typeNodes))
			}
		})
	}
	mustPanic(t, func() {
		arena.callable(result, nil, checkedNonCallable, emptyRowID, emptyRowID, nil, nil)
	})
}

func TestCheckedValueFiberUsesTheSameStructuralNodeAsTheTypeArena(t *testing.T) {
	c := newCheckedValueTestChecker()
	result := c.internType("primitive", "string", nil)
	rowlessType := c.internType("fiber", "", []TypeID{result})
	rowlessValue := c.values.fiber(result, emptyRowID, nil, nil)
	if rowlessValue.contractID() != rowlessType {
		t.Fatalf("rowless fiber constructor created a duplicate node: value=%d type=%d", rowlessValue.contractID(), rowlessType)
	}
	failure := c.internRow([]string{"Failure"})
	value := c.values.fiber(result, failure, nil, nil)
	typeNode := c.internTypeWithRows("fiber", "", []TypeID{result}, failure, emptyRowID)
	if value.contractID() != typeNode {
		t.Fatalf("fiber constructor created an overlay node: value=%d type=%d", value.contractID(), typeNode)
	}
	if c.typeNodeID(value.contractID()) != c.typeNodeID(typeNode) {
		t.Fatalf("equivalent fiber contracts have different public IDs: %q != %q", c.typeNodeID(value.contractID()), c.typeNodeID(typeNode))
	}
	if got := c.canonicalRef(TypeRef{Kind: "record", Name: "Phantom"}); got != invalidTypeID {
		t.Fatalf("unknown nominal declaration was admitted: %d", got)
	}
}

func TestCheckedValueSnapshotsOccurrenceFacts(t *testing.T) {
	c := newCheckedValueTestChecker()
	result := c.internType("primitive", "string", nil)
	inputOwnership := []OwnershipFact{{Path: "file", Status: "borrowed", Origin: "parameter"}}
	inputCaptures := []OwnershipFact{{Path: "child", Status: "owned", Origin: "invocation-result"}}
	value := c.values.data(result, inputOwnership, inputCaptures)

	inputOwnership[0].Path = "mutated-input"
	inputCaptures[0].Path = "mutated-input"
	ownership := value.ownershipFacts()
	captures := value.captureFacts()
	ownership[0].Path = "mutated-accessor"
	captures[0].Path = "mutated-accessor"
	if value.ownershipFacts()[0].Path != "file" || value.captureFacts()[0].Path != "child" {
		t.Fatalf("occurrence facts were mutable: ownership=%+v captures=%+v", value.ownershipFacts(), value.captureFacts())
	}
}

func TestExpressionEvaluationUsesCanonicalRowIDs(t *testing.T) {
	c := newCheckedValueTestChecker()
	left := c.evaluationFromLabels([]string{"FailureA"}, []string{"Users"})
	right := c.evaluationFromLabels([]string{"FailureB", "FailureA"}, []string{"Orders"})
	joined := c.unionEvaluationFacts(left, right)

	if joined.failureRowID() == emptyRowID || joined.serviceRowID() == emptyRowID {
		t.Fatalf("evaluation did not retain canonical row IDs: %+v", joined)
	}
	if got := c.rowLabels(joined.failureRowID()); !slices.Equal(got, []string{"FailureA", "FailureB"}) {
		t.Fatalf("failure row labels = %v", got)
	}
	if got := c.rowLabels(joined.serviceRowID()); !slices.Equal(got, []string{"Orders", "Users"}) {
		t.Fatalf("service row labels = %v", got)
	}

	reordered := c.evaluationFromLabels([]string{"FailureB", "FailureA"}, []string{"Users", "Orders"})
	if reordered.failureRowID() != joined.failureRowID() || reordered.serviceRowID() != joined.serviceRowID() {
		t.Fatalf("row IDs were not canonical: reordered=%+v joined=%+v", reordered, joined)
	}
	if empty := c.evaluation(emptyRowID, emptyRowID); empty.failureRowID() != emptyRowID || empty.serviceRowID() != emptyRowID {
		t.Fatalf("empty evaluation rows changed: %+v", empty)
	}
}

func mustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
