package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// Callable flow evidence is independent from ownership of its eventual result.
type callableEvidence struct {
	callees       [8]*Function
	count         int
	parameter     *Function
	parameterName string
	parameterPath string
	unresolved    bool
}

type callbackResultRelation struct {
	key       string
	callee    callableEvidence
	arguments []checkedExpression
	result    TypeID
	path      string
}

func namedCallableEvidence(f *Function) callableEvidence {
	var evidence callableEvidence
	evidence.callees[0] = f
	evidence.count = 1
	return evidence
}

func joinCallableEvidence(a, b callableEvidence) callableEvidence {
	if a == b {
		return a
	}
	if a.parameter != nil || b.parameter != nil || a.unresolved || b.unresolved || a.count == 0 || b.count == 0 {
		return callableEvidence{unresolved: true}
	}
	result := a
	for _, f := range b.callees[:b.count] {
		if slices.Contains(result.callees[:result.count], f) {
			continue
		}
		if result.count == len(result.callees) {
			return callableEvidence{unresolved: true}
		}
		result.callees[result.count] = f
		result.count++
	}
	slices.SortFunc(result.callees[:result.count], func(a, b *Function) int { return strings.Compare(a.Identity, b.Identity) })
	return result
}

func substituteCallableEvidence(evidence callableEvidence, f *Function, arguments []checkedExpression) callableEvidence {
	if evidence.parameter != f {
		return evidence
	}
	for i, p := range f.Params {
		if p.Name == evidence.parameterName && i < len(arguments) {
			if value, known := fieldOccurrence(arguments[i], evidence.parameterPath); known {
				return value.callableEvidence
			}
			return callableEvidence{unresolved: true}
		}
	}
	return callableEvidence{unresolved: true}
}

// Interning retained semantic evidence, rather than occurrence addresses,
// makes recursive summary convergence and relation storage explicitly finite.
func (c *checker) callbackRelation(callee callableEvidence, arguments []checkedExpression, result TypeID, path string) *callbackResultRelation {
	var key strings.Builder
	writeEvidence := func(e callableEvidence) {
		if e.parameter != nil {
			key.WriteString(e.parameter.Identity)
			key.WriteByte(':')
			key.WriteString(e.parameterName)
			key.WriteByte(':')
			key.WriteString(e.parameterPath)
		}
		for _, f := range e.callees[:e.count] {
			key.WriteString(f.Identity)
			key.WriteByte(',')
		}
		fmt.Fprintf(&key, "/%t;", e.unresolved)
	}
	writeEvidence(callee)
	fmt.Fprintf(&key, "%d:%s;", result, path)
	writeFacts := func(facts []OwnershipFact) {
		fmt.Fprintf(&key, "facts:%d;", len(facts))
		for _, fact := range facts {
			fmt.Fprintf(&key, "%q:%q:%q:%q:%q:%t:%d:%t:%t:%q;", fact.Path, fact.Status, fact.Region, fact.Origin, fact.source, fact.sourceSet, fact.ownerKind, fact.potentialOwner, fact.remainder, fact.remainderExclusions)
			if fact.callbackRelation != nil {
				key.WriteString(fact.callbackRelation.key)
			}
		}
	}
	visits := 0
	var writeOccurrence func(checkedExpression, int) bool
	writeOccurrence = func(argument checkedExpression, depth int) bool {
		visits++
		if depth > 32 || visits > 4096 || key.Len() > 1<<20 {
			return false
		}
		fmt.Fprintf(&key, "%d;", argument.valueID())
		writeEvidence(argument.callableEvidence)
		writeFacts(argument.ownershipFacts())
		writeFacts(argument.captureFacts())
		writeFacts(argument.child)
		fmt.Fprintf(&key, "rows:%d:%d:%d:%d;", argument.evaluation.failureRowID(), argument.evaluation.serviceRowID(), argument.executed.failureRowID(), argument.executed.serviceRowID())
		names := []string{}
		for name := range argument.fields {
			names = append(names, name)
		}
		slices.Sort(names)
		fmt.Fprintf(&key, "fields:%d;", len(names))
		for _, name := range names {
			fmt.Fprintf(&key, "%q;", name)
			if !writeOccurrence(argument.fields[name], depth+1) {
				return false
			}
		}
		return key.Len() <= 1<<20
	}
	for _, argument := range arguments {
		if !writeOccurrence(argument, 0) {
			return nil
		}
	}
	sum := sha256.Sum256([]byte(key.String()))
	id := hex.EncodeToString(sum[:])
	if relation := c.callbackRelations[id]; relation != nil {
		return relation
	}
	if len(c.callbackRelations) >= 4096 {
		return nil
	}
	if c.callbackRelations == nil {
		c.callbackRelations = map[string]*callbackResultRelation{}
	}
	relation := &callbackResultRelation{key: id, callee: callee, arguments: append([]checkedExpression{}, arguments...), result: result, path: path}
	c.callbackRelations[id] = relation
	return relation
}

func (c *checker) callbackResultOwnership(callee callableEvidence, arguments []checkedExpression, result TypeID, depth int) []OwnershipFact {
	if depth < 32 && !callee.unresolved && callee.count > 0 {
		var facts []OwnershipFact
		for _, f := range callee.callees[:callee.count] {
			facts = mergeFacts(facts, c.instantiateCallbackFacts(f.Ownership, f, arguments, depth+1))
		}
		return facts
	}
	facts := c.unknownOwnership(c.displayTypeID(result))
	for i := range facts {
		facts[i].potentialOwner = true
		facts[i].Origin = "callback-result"
		facts[i].ownerKind = ownershipOwnerCallbackResult
		if depth < 32 && !callee.unresolved && callee.parameter != nil {
			facts[i].callbackRelation = c.callbackRelation(callee, arguments, result, facts[i].Path)
		}
	}
	return facts
}

func (c *checker) instantiateCallbackFacts(facts []OwnershipFact, f *Function, arguments []checkedExpression, depth int) []OwnershipFact {
	var out []OwnershipFact
	for _, fact := range facts {
		relation := fact.callbackRelation
		if relation != nil && depth >= 32 {
			fact.callbackRelation = nil
			fact.potentialOwner = true
			fact.Origin = "callback-result-budget"
		}
		if relation == nil || depth >= 32 {
			out = append(out, instantiateCheckedFacts([]OwnershipFact{fact}, f.Params, arguments)...)
			continue
		}
		bound := make([]checkedExpression, len(relation.arguments))
		for i, argument := range relation.arguments {
			bound[i] = argument.clone()
			bound[i].callableEvidence = substituteCallableEvidence(argument.callableEvidence, f, arguments)
			bound[i].setOwnership(c.instantiateCallbackFacts(argument.ownershipFacts(), f, arguments, depth+1))
		}
		callee := substituteCallableEvidence(relation.callee, f, arguments)
		resolved := c.callbackResultOwnership(callee, bound, relation.result, depth+1)
		resolved = materializeExecutionFacts(resolved, fact.Region, fact.ownerKind)
		matched := false
		for _, value := range resolved {
			if !ownershipPathMatches(value.Path, relation.path) {
				continue
			}
			value.Path = fact.Path
			out = append(out, value)
			matched = true
		}
		if !matched {
			fact.callbackRelation = nil
			out = append(out, fact)
		}
	}
	return normalizeFacts(out)
}

// Module helpers retain obligations for their caller. Closing nested scopes
// and unresolved record/foreign callbacks cannot discharge them.
func (c *checker) unsafePotentialOwner(facts []OwnershipFact) bool {
	for _, fact := range facts {
		deferred := fact.ownerKind == ownershipOwnerCallbackResult || fact.ownerKind == ownershipOwnerInvocationResult || (fact.ownerKind == ownershipOwnerLexical && fact.Region == "invocation")
		if fact.potentialOwner && (c.region != "invocation" || fact.callbackRelation == nil || !deferred) {
			return true
		}
	}
	return false
}
