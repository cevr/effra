package compiler

import (
	"slices"
	"strings"
)

// Typed recipe values carry layer-indexed ownership evidence (see
// owner_layers.go): the held set of each layer, the handles executing that
// layer uses, and the evidence of the innermost value its last layer
// produces. A container holding a recipe holds the handles of every held
// set which the recipe has not acquired itself, so they become the
// container's ownership at the field path.

// requireOpenHeldCaptures refuses executing (run, fork) a recipe whose
// layer-0 held handles are not provably open where it executes: no closed
// owner and no ⊤ evidence. Deeper layers are checked when they execute.
func (c *checker) requireOpenHeldCaptures(recipe checkedExpression, span Span) {
	if held := expandRelations(heldAt(recipe.captureFacts(), 0)); hasOwnedClosed(held) || hasPotentialOwner(held) {
		c.reportOwnership("recipe holds a value owned by a closed scope and cannot be executed", span, ownershipRoots(held, ""))
	}
}

// refuseClosedArgument refuses passing a value whose held handles are not
// provably open. The argument keeps its evidence: a recipe or value built
// from it still carries the closed owner, and reportOwnership reports that
// owner once.
func (c *checker) refuseClosedArgument(argument checkedExpression, span Span) checkedExpression {
	if passed := expandRelations(heldFacts(argument, 0)); hasPotentialOwner(passed) || hasOwnedClosed(passed) {
		c.reportOwnership("value owned by a closing scope cannot be used", span, ownershipRoots(passed, ""))
	}
	return argument
}

// refuseCarriedPending refuses passing a value that may leave a forked
// child unobserved (design §5.3) where its occurrence evidence ends: an
// argument, whose callee sees only its parameter type, a provider
// configuration, or a failure payload, whose handler sees only the error
// type. A recipe type promises that executing it raises its row; a pending
// failure is raised later, by the owner that closes after the execution,
// so recovering the recipe where its evidence is gone would not handle it.
// Running, binding, branching on or returning the recipe keeps its
// evidence.
func (c *checker) refuseCarriedPending(value checkedExpression, span Span) checkedExpression {
	if labels := carriedPending(value, 0); len(labels) > 0 {
		c.diagnostic("EF107", "recipe may leave forked child failures unobserved ("+strings.Join(labels, ", ")+"); a parameter, payload or joined result type cannot carry them, so join or interrupt the child first", span)
	}
	return value
}

// carriedPending is the labels pending on any layer of value or of a
// recipe one of its fields holds.
func carriedPending(value checkedExpression, depth int) []string {
	labels := anyPendingLabels(value.failures)
	if depth > 32 {
		// Field occurrences are built at most this deep.
		return labels
	}
	for _, field := range value.fields {
		labels = union(labels, carriedPending(field, depth+1))
	}
	slices.Sort(labels)
	return labels
}

// ownershipRoot is the closing owner behind an unsafe fact. kind is closed (a
// closed child or timeout owner), closing (the owner an escape check closes)
// or potential (incomplete evidence materialized in region).
type ownershipRoot struct{ kind, region string }

// reportedOwnershipRoot scopes a root. A region with a source offset
// ("scope:8930") names one owner of its module; any other region
// ("invocation", "*") is local to the function being checked.
type reportedOwnershipRoot struct {
	module   string
	function *Function
	root     ownershipRoot
}

// ownershipReport is one emitted EF123, the roots it reported first and every
// root its check named. A report is a distinct fault only if its check named
// a root no earlier report had named (design §7.1); distinctOwnershipReports
// verifies that from the full root sets, independently of the filter which
// computes roots.
type ownershipReport struct {
	span    Span
	roots   []reportedOwnershipRoot
	checked []reportedOwnershipRoot
}

// distinctOwnershipReports returns the indices of the reports whose check
// named no root beyond those of the earlier reports.
func distinctOwnershipReports(reports []ownershipReport) (repeated []int) {
	named := map[reportedOwnershipRoot]bool{}
	for i, report := range reports {
		added := false
		for _, root := range report.checked {
			if !named[root] {
				added = true
			}
			named[root] = true
		}
		if !added && len(report.checked) > 0 {
			repeated = append(repeated, i)
		}
	}
	return repeated
}

// ownershipRoots names the roots of facts. closing is the region an escape
// check closes; a use check passes "" and is violated only by closed and
// potential owners.
func ownershipRoots(facts []OwnershipFact, closing string) []ownershipRoot {
	var roots []ownershipRoot
	for _, fact := range facts {
		switch {
		case fact.potentialOwner:
			roots = append(roots, ownershipRoot{"potential", fact.Region})
		case fact.Status == "owned" && closedOwnerKind(fact.ownerKind):
			roots = append(roots, ownershipRoot{"closed", fact.Region})
		case closing != "" && (fact.Status == "unknown" || fact.Status == "owned" && (fact.Region == "*" || fact.ownerKind == ownershipOwnerUnknown || fact.Region == closing)):
			roots = append(roots, ownershipRoot{"closing", closing})
		}
	}
	return roots
}

// reportOwnership emits one EF123 per root (design §7.1): a check whose roots
// were all reported already is silent. Evidence is never dropped after a
// report, so an independent fault still reports at its own check.
func (c *checker) reportOwnership(message string, span Span, roots []ownershipRoot) {
	c.reportOwnershipWith(message, span, roots, c.diagnostic)
}

func (c *checker) reportOwnershipWith(message string, span Span, roots []ownershipRoot, emit func(code, message string, span Span)) {
	keys := make([]reportedOwnershipRoot, 0, len(roots))
	for _, root := range roots {
		keys = append(keys, c.scopedOwnershipRoot(root))
	}
	var first []reportedOwnershipRoot
	for _, key := range keys {
		if !c.ownershipRootReported(key) && !slices.Contains(first, key) {
			first = append(first, key)
		}
	}
	fresh := len(keys) == 0 || len(first) > 0
	if fresh {
		emit("EF123", message, span)
	}
	if c.suppressDiagnostics {
		return
	}
	if fresh {
		c.ownershipReports = append(c.ownershipReports, ownershipReport{span: span, roots: first, checked: slices.Clone(keys)})
	}
	if c.reportedOwnership == nil {
		c.reportedOwnership = map[reportedOwnershipRoot]bool{}
	}
	for _, key := range keys {
		c.reportedOwnership[key] = true
	}
}

func (c *checker) scopedOwnershipRoot(root ownershipRoot) reportedOwnershipRoot {
	if sourceOffsetRegion(root.region) {
		return reportedOwnershipRoot{module: c.functionModule, root: root}
	}
	return reportedOwnershipRoot{module: c.functionModule, function: c.reportingFunction, root: root}
}

// sourceOffsetRegion reports whether region names one source construct
// ("scope:8930", "child:12", "provision:7").
func sourceOffsetRegion(region string) bool {
	_, offset, found := strings.Cut(region, ":")
	if !found || offset == "" {
		return false
	}
	for _, digit := range offset {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

// ownershipRootReported treats a joined region "*" (an unknown choice among
// alternatives) as the same root as any reported owner of its kind in the
// same function or module: the join cannot name which alternative closed, so
// it adds no new owner.
func (c *checker) ownershipRootReported(key reportedOwnershipRoot) bool {
	if c.reportedOwnership[key] {
		return true
	}
	if key.root.region != "*" {
		joined := reportedOwnershipRoot{module: c.functionModule, function: c.reportingFunction, root: ownershipRoot{key.root.kind, "*"}}
		return c.reportedOwnership[joined]
	}
	for reported := range c.reportedOwnership {
		if reported.root.kind == key.root.kind && reported.module == key.module && (reported.function == nil || reported.function == key.function) {
			return true
		}
	}
	return false
}

// heldFacts is the evidence a value contributes to a container or a
// parameter-relative summary that stores it. base is the level at which the
// value sits in the occurrence carrying the container: a recipe's own layers
// start there, so an Exec owner at or above base is an acquisition the
// recipe has not made yet and nobody holds now.
func heldFacts(value checkedExpression, base int) []OwnershipFact {
	if node := value.node(); node == nil || node.Kind != "recipe" {
		return value.ownershipFacts()
	}
	var held []OwnershipFact
	for _, fact := range value.captureFacts() {
		if fact.ownerKind == ownershipOwnerRelation {
			for _, owner := range relationOwners(fact) {
				if owner.ownerKind != ownershipOwnerExec || owner.exec < base {
					held = append(held, owner)
				}
			}
			continue
		}
		if fact.ownerKind == ownershipOwnerExec && fact.exec >= base {
			continue
		}
		held = append(held, fact)
	}
	return collapseRecipeFacts(held)
}

func collapseRecipeFacts(facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		if isWildcardPath(fact.Path) && fact.remainder {
			// Uncertainty about some captured descendant is uncertainty about
			// the recipe as a whole.
			fact.potentialOwner = true
		}
		fact.Path = ""
		fact.layer = 0
		fact.remainder = false
		fact.remainderExclusions = ""
		out = append(out, fact)
	}
	return normalizeFacts(out)
}

// opaqueRecipe is the evidence of a recipe without a retained occurrence: a
// recipe parameter, or one read from a data position. Its layer-0 held set is
// held, what the parameter or container holds at its path. Its later layers
// and its innermost value hold or return only those handles or handles one of
// its executed layers acquired (design §5.1: a parameter recipe has Param
// layer evidence; only a recipe with unknown held handles is ⊤).
//
// Its failure payloads are bounded the same way: a payload of layer d holds
// what the recipe holds or what layers 0 ... d acquired.
func (c *checker) opaqueRecipe(id TypeID, held []OwnershipFact) checkedExpression {
	held = collapseRecipeFacts(held)
	layers := c.recipeDepth(id)
	opaque := c.opaqueLayers(held, c.innermostValue(id), identityOwnerEnv(layers), recipeResultOrigin, c.failureLayerRows(id))
	out := c.checkedDataID(id, nil, nil)
	out.value = c.values.occurrence(id, opaque.ownership, normalizeFacts(append(cloneFacts(held), opaque.captures...)))
	out.failures = opaque.failures
	return out
}

const recipeResultOrigin = "recipe-result"

// recipeOccurrence rebuilds a recipe read out of a container. A retained
// occurrence keeps its own layers; otherwise the recipe is opaque and holds
// what the container holds at its path.
func (c *checker) recipeOccurrence(value checkedExpression, retained bool, typeID TypeID, held []OwnershipFact) checkedExpression {
	if retained {
		value.value = c.values.occurrence(typeID, value.ownershipFacts(), value.captureFacts())
		return value
	}
	opaque := c.opaqueRecipe(typeID, held)
	value.value = opaque.value
	value.failures = opaque.failures
	return value
}
