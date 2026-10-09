package compiler

// These classes describe the fields of the current expression evidence schema:
// evidence carries ownership or deferred-invocation facts, rows describe
// evaluation effects, and metadata records projection or identity data.
// TestEvidenceSchemaIsClassified audits that every reachable field is listed
// and every listed field exists. Metadata terminates the audit's traversal.
// This classification does not establish coverage by runtime rewrites or joins.
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
	"checkedExpression.callableEvidence": occurrenceEvidence,
	"checkedExpression.evaluation":       occurrenceRows,
	"checkedExpression.executed":         occurrenceRows,
	"checkedExpression.callableDecl":     occurrenceMetadata,
	"checkedExpression.application":      occurrenceMetadata,
	"checkedExpression.identity":         occurrenceMetadata,
	"checkedExpression.lexicalBinding":   occurrenceMetadata,

	"CheckedValue.arena":     occurrenceMetadata,
	"CheckedValue.contract":  occurrenceMetadata,
	"CheckedValue.ownership": occurrenceEvidence,
	"CheckedValue.captures":  occurrenceEvidence,

	"ExpressionEvaluation.failureRow": occurrenceRows,
	"ExpressionEvaluation.serviceRow": occurrenceRows,

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

	// A callback-result relation is resolved by instantiateCallbackFacts;
	// its argument occurrences are evidence of the deferred invocation.
	"callbackResultRelation.key":       occurrenceMetadata,
	"callbackResultRelation.callee":    occurrenceEvidence,
	"callbackResultRelation.arguments": occurrenceEvidence,
	"callbackResultRelation.result":    occurrenceMetadata,
	"callbackResultRelation.path":      occurrenceEvidence,

	"callableEvidence.callees":       occurrenceMetadata,
	"callableEvidence.count":         occurrenceEvidence,
	"callableEvidence.unresolved":    occurrenceEvidence,
	"callableEvidence.parameter":     occurrenceMetadata,
	"callableEvidence.parameterName": occurrenceEvidence,
	"callableEvidence.parameterPath": occurrenceEvidence,
}
