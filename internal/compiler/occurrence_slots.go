package compiler

import (
	"reflect"
	"slices"
)

// An expression occurrence is a tree of evidence slots. Every field of
// checkedExpression, and of the evidence types reachable from it, belongs to
// exactly one class:
//
//   - evidence: ownership facts and the occurrences that carry them. Every
//     rewrite of an occurrence (summary abstraction, execution binding,
//     instantiation) visits these slots through mapOccurrenceFacts, and every
//     branch join combines them through joinOccurrence.
//   - rows: evaluation rows, combined by row union.
//   - metadata: projection and identity data. A join keeps equal values and
//     drops unequal ones; rewrites leave it unchanged.
//
// TestEvidenceSchemaIsClassified walks every type reachable from
// checkedExpression and requires each field to appear here, so a new slot
// cannot be forgotten by a join or a rewrite as `child` and `fields` were.
type occurrenceSlotClass uint8

const (
	occurrenceEvidence occurrenceSlotClass = iota + 1
	occurrenceRows
	occurrenceMetadata
)

// occurrenceSlots classifies the fields of the evidence types. A metadata
// field's type is not descended into by the schema test.
var occurrenceSlots = map[string]occurrenceSlotClass{
	"checkedExpression.fields":           occurrenceEvidence,
	"checkedExpression.value":            occurrenceEvidence,
	"checkedExpression.child":            occurrenceEvidence,
	"checkedExpression.failures":         occurrenceEvidence,
	"checkedExpression.callableEvidence": occurrenceEvidence,
	"checkedExpression.evaluation":       occurrenceRows,
	"checkedExpression.executed":         occurrenceRows,
	"checkedExpression.callableDecl":     occurrenceMetadata,
	"checkedExpression.application":      occurrenceMetadata,
	"checkedExpression.identity":         occurrenceMetadata,
	"checkedExpression.lexicalBinding":   occurrenceMetadata,
	"checkedExpression.forks":            occurrenceEvidence,
	"checkedExpression.observes":         occurrenceEvidence,
	"checkedExpression.kills":            occurrenceRows,
	"checkedExpression.exitKills":        occurrenceRows,

	"CheckedValue.arena":     occurrenceMetadata,
	"CheckedValue.contract":  occurrenceMetadata,
	"CheckedValue.ownership": occurrenceEvidence,
	"CheckedValue.captures":  occurrenceEvidence,

	"ExpressionEvaluation.failureRow": occurrenceRows,
	"ExpressionEvaluation.serviceRow": occurrenceRows,
	// The payloads and uses an evaluation incurs belong to the evaluating
	// body, like its rows: they are combined by union and consumed at the
	// body's own edges (scope, function), never rewritten with a value.
	"ExpressionEvaluation.payloads": occurrenceRows,
	"ExpressionEvaluation.uses":     occurrenceRows,
	"ExpressionEvaluation.pending":  occurrenceRows,
	"ExpressionEvaluation.live":     occurrenceRows,

	"failureKey.layer":   occurrenceEvidence,
	"failureKey.label":   occurrenceEvidence,
	"failureKey.pending": occurrenceEvidence,
	"failureKey.fork":    occurrenceEvidence,
	"failureKey.with":    occurrenceEvidence,
	"failureKey.alt":     occurrenceEvidence,

	"OwnershipFact.Path":                occurrenceEvidence,
	"OwnershipFact.Status":              occurrenceEvidence,
	"OwnershipFact.Region":              occurrenceEvidence,
	"OwnershipFact.Origin":              occurrenceMetadata,
	"OwnershipFact.source":              occurrenceEvidence,
	"OwnershipFact.sourceSet":           occurrenceEvidence,
	"OwnershipFact.ownerKind":           occurrenceEvidence,
	"OwnershipFact.potentialOwner":      occurrenceEvidence,
	"OwnershipFact.remainder":           occurrenceEvidence,
	"OwnershipFact.remainderExclusions": occurrenceEvidence,
	"OwnershipFact.callbackRelation":    occurrenceEvidence,
	"OwnershipFact.exec":                occurrenceEvidence,
	"OwnershipFact.layer":               occurrenceEvidence,
	"OwnershipFact.relationEnv":         occurrenceEvidence,

	// A callback-result relation is resolved by instantiateCallbackFacts;
	// its argument occurrences are evidence of the deferred invocation.
	"callbackResultRelation.key":       occurrenceMetadata,
	"callbackResultRelation.callee":    occurrenceEvidence,
	"callbackResultRelation.arguments": occurrenceEvidence,
	"callbackResultRelation.result":    occurrenceMetadata,
	"callbackResultRelation.path":      occurrenceEvidence,
	"callbackResultRelation.held":      occurrenceEvidence,
	"callbackResultRelation.effect":    occurrenceEvidence,
	"callbackResultRelation.failure":   occurrenceEvidence,

	"callableEvidence.callees":       occurrenceMetadata,
	"callableEvidence.count":         occurrenceEvidence,
	"callableEvidence.unresolved":    occurrenceEvidence,
	"callableEvidence.parameter":     occurrenceMetadata,
	"callableEvidence.parameterName": occurrenceEvidence,
	"callableEvidence.parameterPath": occurrenceEvidence,
}

// occurrenceFactSlot names the fact-carrying slots of one occurrence.
type occurrenceFactSlot uint8

const (
	// ownershipSlot is what the occurrence's value (or, for a recipe, its
	// execution) produces.
	ownershipSlot occurrenceFactSlot = iota
	// capturesSlot is what the occurrence already holds.
	capturesSlot
	// childSlot is the result evidence of a Fiber's child.
	childSlot
	// failuresSlot is the payload evidence of one failure of one layer.
	failuresSlot
)

// mapOccurrenceFacts rewrites every fact slot of an occurrence and of its
// field occurrences with one function. Field tables are shared and
// immutable, so the rewrite copies the tables it changes.
//
// A record graph shares field tables (`R { left: v, right: v }`), so its
// leaf paths can be exponential in its size. Every rewrite is a function of
// the facts of one slot, so one table rewrites to one table: the walk
// rewrites each shared table once and the result shares it the same way.
func mapOccurrenceFacts(e checkedExpression, rewrite func(occurrence checkedExpression, slot occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact) checkedExpression {
	return mapSharedOccurrenceFacts(e, rewrite, map[uintptr]map[string]checkedExpression{})
}

func mapSharedOccurrenceFacts(e checkedExpression, rewrite func(occurrence checkedExpression, slot occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact, tables map[uintptr]map[string]checkedExpression) checkedExpression {
	out := mapOccurrenceSlotFacts(e, rewrite)
	out.fields = mapSharedFieldFacts(e.fields, rewrite, tables)
	return out
}

// mapSharedFieldFacts rewrites one field table, reusing the rewrite of a
// table this walk has already visited. The input tables stay reachable for
// the whole walk, so their identities are not reused.
func mapSharedFieldFacts(fields map[string]checkedExpression, rewrite func(occurrence checkedExpression, slot occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact, tables map[uintptr]map[string]checkedExpression) map[string]checkedExpression {
	if fields == nil {
		return nil
	}
	identity := reflect.ValueOf(fields).Pointer()
	if out, seen := tables[identity]; seen {
		return out
	}
	out := make(map[string]checkedExpression, len(fields))
	for name, field := range fields {
		out[name] = mapSharedOccurrenceFacts(field, rewrite, tables)
	}
	tables[identity] = out
	return out
}

// mapOccurrenceSlotFacts rewrites the fact slots of one occurrence and
// leaves its field table untouched: the walk of a caller which visits the
// fields itself (a bounded substitution, which also rewrites each field's
// contract).
func mapOccurrenceSlotFacts(e checkedExpression, rewrite func(occurrence checkedExpression, slot occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact) checkedExpression {
	out := e
	out.value = e.value.withOccurrenceFacts(rewrite(e, ownershipSlot, e.ownershipFacts()), rewrite(e, capturesSlot, e.captureFacts()))
	if len(e.child) > 0 {
		out.child = rewrite(e, childSlot, cloneFacts(e.child))
	}
	if len(e.failures) > 0 {
		out.failures = mapFailureFacts(e.failures, func(facts []OwnershipFact) []OwnershipFact {
			return rewrite(e, failuresSlot, facts)
		})
	}
	return out
}

// mapFieldFacts rewrites only the field occurrences of e.
func mapFieldFacts(fields map[string]checkedExpression, rewrite func(occurrence checkedExpression, slot occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact) map[string]checkedExpression {
	return mapSharedFieldFacts(fields, rewrite, map[uintptr]map[string]checkedExpression{})
}

// joinOccurrence joins the evidence and metadata slots of two alternatives
// with the same value contract. Contract rows are joined by
// joinContractRows; evaluation rows belong to the enclosing expression.
func (c *checker) joinOccurrence(a, b checkedExpression, span Span) checkedExpression {
	return c.joinOccurrenceAt(a, b, span, 0, new(int))
}

// joinOccurrenceAt is joinOccurrence for an occurrence at depth of a field
// table whose joins share one budget. It is the only join of an occurrence:
// a stored field is joined by the same call as the value that holds it, so
// every slot of every alternative survives at every depth.
func (c *checker) joinOccurrenceAt(a, b checkedExpression, span Span, depth int, nodes *int) checkedExpression {
	t := c.joinOccurrenceSlots(a, b)
	t.fields = c.joinExpressionFields(a, b, span, depth, nodes)
	return t
}

// joinOccurrenceSlots joins every slot of one occurrence except its field
// table, whose join is bounded by the caller's field budget. Payload
// evidence is completed for each alternative's own rows first, so a failure
// one alternative raises without evidence stays unknown in the join.
func (c *checker) joinOccurrenceSlots(a, b checkedExpression) checkedExpression {
	a, b = c.completeFailures(a), c.completeFailures(b)
	t := a.clone()
	t.failures = mergeFailures(a.failures, b.failures)
	t.setOwnership(mergeFacts(a.ownershipFacts(), b.ownershipFacts()))
	t.setCaptures(mergeFacts(a.captureFacts(), b.captureFacts()))
	t.child = mergeFacts(a.child, b.child)
	t.callableEvidence = joinCallableEvidence(a.callableEvidence, b.callableEvidence)
	// A joined Fiber may denote either alternative's child; a joined recipe
	// observes only what both alternatives observe.
	t.forks = unionStrings(a.forks, b.forks)
	t.observes = intersectStrings(a.observes, b.observes)
	if a.callableDecl != b.callableDecl {
		t.callableDecl = nil
	}
	if a.lexicalBinding != b.lexicalBinding {
		t.lexicalBinding = ""
	}
	return t
}

func unionStrings(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	out := append([]string{}, a...)
	for _, item := range b {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	slices.Sort(out)
	return out
}

func intersectStrings(a, b []string) []string {
	var out []string
	for _, item := range a {
		if slices.Contains(b, item) {
			out = append(out, item)
		}
	}
	return out
}
