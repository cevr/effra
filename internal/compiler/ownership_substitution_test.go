package compiler

import (
	"fmt"
	"testing"
)

func TestOwnershipSubstitutionRetainsOmittedParameterAlternatives(t *testing.T) {
	parameterWide := []OwnershipFact{{
		Path:      "*",
		Status:    "borrowed",
		Origin:    "bounded-all-borrowed",
		Region:    "parameter:a",
		source:    "*",
		sourceSet: true,
		ownerKind: ownershipOwnerParameter,
	}}
	parameterLeaf := []OwnershipFact{{
		Path:      "z",
		Status:    "borrowed",
		Origin:    "parameter",
		Region:    "parameter:b",
		sourceSet: true,
		ownerKind: ownershipOwnerParameter,
	}}
	for i := 0; i < 63; i++ {
		parameterLeaf = append(parameterLeaf, OwnershipFact{
			Path:           fmt.Sprintf("x%d.*", i),
			Status:         "unknown",
			Origin:         "bounded",
			potentialOwner: true,
			remainder:      true,
		})
	}
	parameterLeaf = normalizeFacts(parameterLeaf)

	params := []Param{{Name: "a"}, {Name: "b"}}
	ownedArgument := []ValueType{{Ownership: []OwnershipFact{{
		Path:      "*",
		Status:    "owned",
		Origin:    "bounded-all-owned",
		Region:    "scope:inner",
		ownerKind: ownershipOwnerLexical,
	}}}, {Ownership: []OwnershipFact{{
		Status:    "borrowed",
		Origin:    "parameter",
		Region:    "parameter:outer",
		sourceSet: true,
		ownerKind: ownershipOwnerParameter,
	}}}}
	borrowedArguments := []ValueType{{Ownership: []OwnershipFact{{
		Path:      "*",
		Status:    "borrowed",
		Origin:    "bounded-all-borrowed",
		Region:    "parameter:outer",
		source:    "*",
		sourceSet: true,
		ownerKind: ownershipOwnerParameter,
	}}}, {Ownership: []OwnershipFact{{
		Status:    "borrowed",
		Origin:    "parameter",
		Region:    "parameter:outer",
		sourceSet: true,
		ownerKind: ownershipOwnerParameter,
	}}}}
	unsafe := func(facts []OwnershipFact) bool {
		return hasOwnedFact(facts, "scope:inner") || hasPotentialOwner(facts)
	}

	if !unsafe(instantiateFacts(projectFacts(parameterWide, "z"), params, ownedArgument)) {
		t.Fatal("owned parameter argument control lost its owner")
	}
	if unsafe(instantiateFacts(projectFacts(parameterLeaf, "z"), params, ownedArgument)) {
		t.Fatal("independently borrowed parameter control became unsafe")
	}
	if unsafe(instantiateFacts(projectFacts(parameterLeaf, "z"), params, borrowedArguments)) {
		t.Fatal("borrowed arguments should remain an accepted control")
	}

	joined := [][]OwnershipFact{
		mergeFacts(parameterWide, parameterLeaf),
		mergeFacts(parameterLeaf, parameterWide),
		mergeFacts(mergeFacts(parameterWide, parameterLeaf[:32]), parameterLeaf[32:]),
		mergeFacts(mergeFacts(parameterLeaf[:32], parameterWide), parameterLeaf[32:]),
	}
	for i, summary := range joined {
		projected := projectFacts(summary, "z")
		if !unsafe(instantiateFacts(projected, params, ownedArgument)) {
			t.Errorf("join %d dropped the omitted parameter:a owner: summary=%#v actual=%#v", i, projected, instantiateFacts(projected, params, ownedArgument))
		}
		if unsafe(instantiateFacts(projected, params, borrowedArguments)) {
			t.Errorf("join %d rejected the borrowed-argument control: summary=%#v actual=%#v", i, projected, instantiateFacts(projected, params, borrowedArguments))
		}
	}
}

func TestOwnershipSubstitutionPreservesExactParameterSourceAlternatives(t *testing.T) {
	left := []OwnershipFact{{
		Path:      "z",
		Status:    "borrowed",
		Origin:    "parameter",
		Region:    "parameter:a",
		source:    "z",
		sourceSet: true,
		ownerKind: ownershipOwnerParameter,
	}}
	right := []OwnershipFact{{
		Path:      "z",
		Status:    "borrowed",
		Origin:    "parameter",
		Region:    "parameter:b",
		source:    "",
		sourceSet: true,
		ownerKind: ownershipOwnerParameter,
	}}
	params := []Param{{Name: "a"}, {Name: "b"}}
	args := []ValueType{{Ownership: []OwnershipFact{{
		Path:      "z",
		Status:    "owned",
		Origin:    "parameter",
		Region:    "scope:inner",
		ownerKind: ownershipOwnerLexical,
	}}}, {Ownership: []OwnershipFact{{
		Status:    "borrowed",
		Origin:    "parameter",
		Region:    "parameter:outer",
		sourceSet: true,
		ownerKind: ownershipOwnerParameter,
	}}}}
	for i, summary := range [][]OwnershipFact{
		mergeFacts(left, right),
		mergeFacts(right, left),
	} {
		actual := instantiateFacts(projectFacts(summary, "z"), params, args)
		if !hasOwnedFact(actual, "scope:inner") {
			t.Errorf("join %d lost exact parameter:a source: summary=%#v actual=%#v", i, summary, actual)
		}
	}
}
