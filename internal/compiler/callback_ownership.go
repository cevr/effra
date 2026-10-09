package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
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

// callbackResultRelation is the deferred relation ρ of a Rel owner (design
// §3.1): the owners of one slot of the occurrence produced by invoking callee
// on arguments. result is the callee's declared result, effect its mode, and
// the slot is the held set of invocation layer held (held >= 1), the
// innermost value at path (held < 0), or, when failure is set, the payload
// of failure raised by invocation layer held at path (design §5.2 F4).
type callbackResultRelation struct {
	key       string
	callee    callableEvidence
	arguments []checkedExpression
	result    TypeID
	path      string
	held      int
	effect    bool
	failure   string
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
func (c *checker) callbackRelation(callee callableEvidence, arguments []checkedExpression, result TypeID, path string, held int, effect bool, failure string) *callbackResultRelation {
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
	fmt.Fprintf(&key, "%d:%s:%d:%t:%q;", result, path, held, effect, failure)
	writeFacts := func(facts []OwnershipFact) {
		fmt.Fprintf(&key, "facts:%d;", len(facts))
		for _, fact := range facts {
			fmt.Fprintf(&key, "%q:%q:%q:%q:%q:%t:%d:%t:%t:%q:%d:%d:%q;", fact.Path, fact.Status, fact.Region, fact.Origin, fact.source, fact.sourceSet, fact.ownerKind, fact.potentialOwner, fact.remainder, fact.remainderExclusions, fact.exec, fact.layer, fact.relationEnv)
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
		fmt.Fprintf(&key, "failures:%d;", len(argument.failures))
		for _, k := range sortedFailureKeys(argument.failures) {
			fmt.Fprintf(&key, "%d:%q;", k.layer, k.label)
			writeFacts(argument.failures[k])
		}
		fmt.Fprintf(&key, "rows:%d:%d:%d:%d;", argument.evaluation.failureRowID(), argument.evaluation.serviceRowID(), argument.executed.failureRowID(), argument.executed.serviceRowID())
		for _, evaluation := range []ExpressionEvaluation{argument.evaluation, argument.executed} {
			labels := slices.Sorted(maps.Keys(evaluation.payloads))
			fmt.Fprintf(&key, "payloads:%d;", len(labels))
			for _, label := range labels {
				fmt.Fprintf(&key, "%q;", label)
				writeFacts(evaluation.payloads[label])
			}
			writeFacts(evaluation.uses)
		}
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
	relation := &callbackResultRelation{key: id, callee: callee, arguments: append([]checkedExpression{}, arguments...), result: result, path: path, held: held, effect: effect, failure: failure}
	c.callbackRelations[id] = relation
	return relation
}

// invocation is the layered evidence of one invocation of a callable: its
// innermost value's ownership and fields, the held sets of the layers it
// produces and the payloads of their failures. Invocation layer 0 is the
// callee's own execution for an effect callee, and the produced recipe's
// first layer for a pure one. A summarized callee's layer-0 held set is the
// handles its body dereferences; callers of an unknown callee add the
// arguments it was given (design §4.2).
type invocation struct {
	ownership []OwnershipFact
	captures  []OwnershipFact
	failures  failureEvidence
	fields    map[string]checkedExpression
	evidence  callableEvidence
}

// invocationLayers is the number of layers of the occurrence produced by
// invoking a callable with the given result and mode.
func (c *checker) invocationLayers(result TypeID, effect bool) int {
	if effect {
		return c.recipeDepth(result) + 1
	}
	return c.recipeDepth(result)
}

// innermostValue is the value a chain of recipe layers finally produces.
func (c *checker) innermostValue(id TypeID) TypeID {
	for range 256 {
		node := c.node(id)
		if node == nil || (node.Kind != "recipe" && node.Kind != "providerRecipe") {
			return id
		}
		id = node.Result
	}
	return id
}

// invocationEvidence is the evidence of invoking callee on arguments, with
// the invocation's levels mapped through env to the owners of the
// occurrence which carries the result (identityOwnerEnv for a fresh call).
//
//   - A named, non-generic callee instantiates its summary.
//   - A callee read from a parameter of the function being checked is
//     deferred as Rel atoms, resolved when a caller substitutes the callee.
//   - Any other callee is opaque. Following language-abstractions.md, it
//     holds whatever it was given and, for an effect callee, whatever the
//     executors of its layers acquired.
//
// callRow is the callee's own failure row (for an effect callee), which
// bounds the failures an opaque invocation's call layer raises.
func (c *checker) invocationEvidence(callee callableEvidence, arguments []checkedExpression, result TypeID, effect bool, callRow RowID, env []OwnershipFact, depth int) invocation {
	switch {
	case callee.parameter != nil && !callee.unresolved:
		return c.relationInvocation(callee, arguments, result, effect, callRow, env)
	case callee.unresolved || callee.count == 0:
		return c.opaqueInvocation(arguments, result, effect, callRow, env)
	}
	var out invocation
	var failures failureEvidence
	var uses []OwnershipFact
	succeeding := 0
	for _, f := range callee.callees[:callee.count] {
		var one invocation
		if len(f.TypeParameters) > 0 || len(f.RowParameters) > 0 || f.Effect != effect {
			one = c.opaqueInvocation(arguments, result, effect, callRow, env)
		} else {
			one = c.summaryInvocation(f, arguments, env, depth)
		}
		// A callee which never succeeds still executes its call layer: it
		// uses the handles that layer dereferences and raises its failures.
		failures = mergeFailures(failures, one.failures)
		if effect {
			uses = mergeFacts(uses, heldAt(one.captures, 0))
		}
		if f.returnsNever {
			continue
		}
		if succeeding == 0 {
			out = one
		} else {
			out.ownership = mergeFacts(out.ownership, one.ownership)
			out.captures = mergeFacts(out.captures, one.captures)
			out.fields = c.joinKnownFields(c.innermostValue(result), out.fields, one.fields, Span{})
			out.evidence = joinCallableEvidence(out.evidence, one.evidence)
		}
		succeeding++
	}
	out.failures = failures
	out.captures = mergeFacts(out.captures, uses)
	return out
}

// summaryInvocation instantiates f's summary. The summary's own levels are
// mapped through env first; the arguments are occurrences of the carrying
// occurrence already, so Param substitution happens after the mapping.
func (c *checker) summaryInvocation(f *Function, arguments []checkedExpression, env []OwnershipFact, depth int) invocation {
	r := envRewrite(env)
	fields := mapFieldFacts(f.returnFields, func(_ checkedExpression, _ occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact {
		return c.rewriteFacts(facts, r)
	})
	out := invocation{
		ownership: c.instantiateCallbackFacts(c.rewriteFacts(f.Ownership, r), f, arguments, depth),
		captures:  c.instantiateCallbackFacts(c.rewriteFacts(f.Captures, r), f, arguments, depth),
		failures: c.completeFailureRows(c.instantiateFailures(mapFailureFacts(f.failures, func(facts []OwnershipFact) []OwnershipFact {
			return c.rewriteFacts(facts, r)
		}), f, arguments, depth), c.callFailureRows(f.returnID, f.Effect, f.failureID)),
		fields:   c.instantiateFieldOccurrences(fields, f, arguments, nil, nil, depth),
		evidence: substituteCallableEvidence(f.returnCallableEvidence, f, arguments),
	}
	out.ownership, out.captures = c.containerFieldFacts(out.ownership, out.captures, out.fields, envTail(env))
	return out
}

// envTail is the level of the carrying occurrence which holds the
// invocation's innermost value.
func envTail(env []OwnershipFact) int {
	if len(env) == 0 {
		return 0
	}
	return env[len(env)-1].exec
}

// containerFieldFacts adds the evidence of a returned container's field
// occurrences to its path facts. A stored recipe contributes the handles it
// holds, not the result of a run that has not happened; the container's
// innermost value sits at level base, so a recipe's own layers start there.
func (c *checker) containerFieldFacts(ownership, captures []OwnershipFact, fields map[string]checkedExpression, base int) ([]OwnershipFact, []OwnershipFact) {
	if len(fields) == 0 {
		return ownership, captures
	}
	owners, held := cloneFacts(ownership), cloneFacts(captures)
	for name, value := range fields {
		owners = append(owners, prependFacts(name, heldFacts(value, base))...)
		if !value.isEffect() {
			for _, fact := range prependFacts(name, value.captureFacts()) {
				fact.layer += base
				held = append(held, fact)
			}
		}
	}
	return normalizeFacts(owners), normalizeFacts(held)
}

// relationInvocation defers an invocation of a callable parameter: every
// handle-carrying slot of the invocation, failure payloads included (design
// §5.2 F4), is a Rel atom over env.
func (c *checker) relationInvocation(callee callableEvidence, arguments []checkedExpression, result TypeID, effect bool, callRow RowID, env []OwnershipFact) invocation {
	var out invocation
	encoded := encodeOwnerEnv(env)
	for layer, labels := range c.callFailureRows(result, effect, callRow) {
		for _, label := range labels {
			payload, ok := c.failurePayloadType(label)
			if !ok {
				continue
			}
			var facts []OwnershipFact
			for _, path := range c.ownershipPathsID(payload, "") {
				relation := c.callbackRelation(callee, arguments, result, path, layer, effect, label)
				fact := OwnershipFact{Path: path, Status: "unknown", Origin: "callback-result", ownerKind: ownershipOwnerRelation, callbackRelation: relation, relationEnv: encoded}
				if relation == nil {
					fact = budgetTop(fact)
				}
				facts = append(facts, fact)
			}
			if out.failures == nil {
				out.failures = failureEvidence{}
			}
			out.failures[raisedKey(layer, label)] = normalizeFacts(facts)
		}
	}
	atom := func(path string, held int) OwnershipFact {
		layer := 0
		if held > 0 {
			layer = held
		}
		relation := c.callbackRelation(callee, arguments, result, path, held, effect, "")
		fact := OwnershipFact{Path: path, Status: "unknown", Origin: "callback-result", ownerKind: ownershipOwnerRelation, callbackRelation: relation, relationEnv: encoded, layer: layer}
		if relation == nil {
			return budgetTop(fact)
		}
		return fact
	}
	for held := 1; held < len(env)-1; held++ {
		out.captures = append(out.captures, atom("", held))
	}
	for _, fact := range c.unknownOwnershipID(c.innermostValue(result)) {
		if fact.potentialOwner {
			out.ownership = append(out.ownership, fact)
			continue
		}
		out.ownership = append(out.ownership, atom(fact.Path, -1))
	}
	out.ownership, out.captures = normalizeFacts(out.ownership), normalizeFacts(out.captures)
	return out
}

// opaqueInvocation is the conservative evidence of an invocation whose
// callee has no summary: each produced layer and the innermost value may
// hold any handle the arguments hold, and any handle acquired by the
// executor of an enclosing invocation layer.
func (c *checker) opaqueInvocation(arguments []checkedExpression, result TypeID, effect bool, callRow RowID, env []OwnershipFact) invocation {
	return c.opaqueProducer(arguments, result, effect, callRow, env, opaqueProducerOrigin)
}

// opaqueProducer is opaqueInvocation with the provenance label of the
// acquisitions it assumes.
func (c *checker) opaqueProducer(arguments []checkedExpression, result TypeID, effect bool, callRow RowID, env []OwnershipFact, origin string) invocation {
	return c.opaqueLayers(givenFacts(arguments), result, env, origin, c.callFailureRows(result, effect, callRow))
}

// givenFacts is what a set of arguments holds, collapsed to its roots.
func givenFacts(arguments []checkedExpression) []OwnershipFact {
	var given []OwnershipFact
	for _, argument := range arguments {
		given = append(given, collapseRecipeFacts(heldFacts(argument, 0))...)
	}
	return given
}

// opaqueLayers is the conservative evidence of layers whose producer is not
// known (language-abstractions.md, recipe ownership): every handle a layer
// holds or returns was either given to it (given) or acquired by one of the
// layers executed so far, so it is owned by given or by the executor env[j]
// of such a layer.
//
// rows is the failure row of each layer: a payload of layer d holds a handle
// given to it or acquired by the executor of one of layers 0 ... d.
func (c *checker) opaqueLayers(given []OwnershipFact, result TypeID, env []OwnershipFact, origin string, rows [][]string) invocation {
	acquired := func(path string, below int, layer int) []OwnershipFact {
		var out []OwnershipFact
		for _, fact := range given {
			fact.Path, fact.layer = path, layer
			out = append(out, fact)
		}
		for level := 0; level < below && level < len(env)-1; level++ {
			out = append(out, withOwner(OwnershipFact{Path: path, Origin: origin, layer: layer}, env[level]))
		}
		return out
	}
	var out invocation
	layers := len(env) - 1
	for held := 1; held < layers; held++ {
		out.captures = append(out.captures, acquired("", held, held)...)
	}
	for _, fact := range c.unknownOwnershipID(c.innermostValue(result)) {
		if fact.potentialOwner {
			out.ownership = append(out.ownership, fact)
			continue
		}
		owners := acquired(fact.Path, layers, 0)
		if len(owners) == 0 {
			// A handle no argument holds and no layer acquired is still a
			// value of a handle type: nothing bounds its owner.
			owners = []OwnershipFact{{Path: fact.Path, Status: "unknown", Origin: origin, potentialOwner: true}}
		}
		out.ownership = append(out.ownership, owners...)
	}
	out.ownership, out.captures = normalizeFacts(out.ownership), normalizeFacts(out.captures)
	out.failures = c.opaqueFailures(given, rows, env, origin)
	return out
}

// opaqueFailures is the conservative payload evidence of the failures of
// layers whose producer is not known (design §5.2 F4, F5).
func (c *checker) opaqueFailures(given []OwnershipFact, rows [][]string, env []OwnershipFact, origin string) failureEvidence {
	var out failureEvidence
	for layer, labels := range rows {
		for _, label := range labels {
			payload, ok := c.failurePayloadType(label)
			if !ok {
				continue
			}
			var facts []OwnershipFact
			for _, path := range c.ownershipPathsID(payload, "") {
				var owners []OwnershipFact
				for _, fact := range given {
					fact.Path, fact.layer = path, 0
					owners = append(owners, fact)
				}
				for level := 0; level <= layer && level < len(env)-1; level++ {
					owners = append(owners, withOwner(OwnershipFact{Path: path, Origin: origin}, env[level]))
				}
				if len(owners) == 0 {
					owners = []OwnershipFact{{Path: path, Status: "unknown", Origin: origin, potentialOwner: true}}
				}
				facts = append(facts, owners...)
			}
			if out == nil {
				out = failureEvidence{}
			}
			out[raisedKey(layer, label)] = normalizeFacts(facts)
		}
	}
	return out
}

const opaqueProducerOrigin = "opaque-producer"

const operationResultOrigin = "operation-result"

// operationResult is the evidence of a service operation's result. The
// operation runs in whichever provider is in scope, so it is an opaque
// producer (design F5): a service name is not an acquisition proof, and the
// result is labelled as an operation result rather than an acquisition.
func (c *checker) operationResult(result TypeID, arguments []checkedExpression, effect bool, callRow RowID) invocation {
	layers := c.invocationLayers(result, effect)
	return c.opaqueProducer(arguments, result, effect, callRow, identityOwnerEnv(layers), operationResultOrigin)
}

// instantiateFailures substitutes a callee's parameters in its failure
// payload evidence, keeping every entry.
func (c *checker) instantiateFailures(failures failureEvidence, f *Function, arguments []checkedExpression, depth int) failureEvidence {
	return mapFailureFacts(failures, func(facts []OwnershipFact) []OwnershipFact {
		return c.instantiateCallbackFacts(facts, f, arguments, depth)
	})
}

// instantiateCallbackFacts substitutes a callee's parameters in its summary
// facts. A Rel atom whose callee becomes known is resolved here.
func (c *checker) instantiateCallbackFacts(facts []OwnershipFact, f *Function, arguments []checkedExpression, depth int) []OwnershipFact {
	var out []OwnershipFact
	for _, fact := range facts {
		relation := fact.callbackRelation
		if fact.ownerKind != ownershipOwnerRelation || relation == nil {
			out = append(out, instantiateCheckedFacts([]OwnershipFact{fact}, f.Params, arguments)...)
			continue
		}
		if depth >= 32 {
			out = append(out, budgetTop(fact))
			continue
		}
		bound := make([]checkedExpression, len(relation.arguments))
		for i, argument := range relation.arguments {
			bound[i] = mapOccurrenceFacts(argument, func(_ checkedExpression, _ occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact {
				return c.instantiateCallbackFacts(facts, f, arguments, depth+1)
			})
			bound[i].callableEvidence = substituteCallableEvidence(argument.callableEvidence, f, arguments)
		}
		callee := substituteCallableEvidence(relation.callee, f, arguments)
		out = append(out, c.resolveRelation(fact, callee, bound, depth+1)...)
	}
	return normalizeFacts(out)
}

// resolveRelation is the evidence of one Rel atom once its callee is
// substituted: the selected slot of the invocation's evidence, whose levels
// map through the atom's owner vector.
func (c *checker) resolveRelation(fact OwnershipFact, callee callableEvidence, arguments []checkedExpression, depth int) []OwnershipFact {
	relation := fact.callbackRelation
	env := decodeOwnerEnv(fact.relationEnv)
	if len(env) == 0 {
		return []OwnershipFact{budgetTop(fact)}
	}
	if callee.parameter != nil && !callee.unresolved {
		rebuilt := c.callbackRelation(callee, arguments, relation.result, relation.path, relation.held, relation.effect, relation.failure)
		if rebuilt == nil {
			return []OwnershipFact{budgetTop(fact)}
		}
		fact.callbackRelation = rebuilt
		return []OwnershipFact{fact}
	}
	evidence := c.invocationEvidence(callee, arguments, relation.result, relation.effect, emptyRowID, env, depth)
	var out []OwnershipFact
	if relation.failure != "" {
		payload, ok := evidence.failures[raisedKey(relation.held, relation.failure)]
		if !ok {
			// The resolved callee raises the failure without evidence.
			top := budgetTop(fact)
			top.Origin = "callback-result"
			return []OwnershipFact{top}
		}
		for _, resolved := range payload {
			if !ownershipPathMatches(resolved.Path, relation.path) {
				continue
			}
			resolved.Path, resolved.layer = fact.Path, fact.layer
			out = append(out, resolved)
		}
		return out
	}
	if relation.held >= 0 {
		for _, resolved := range evidence.captures {
			if resolved.layer != relation.held {
				continue
			}
			resolved.Path, resolved.layer = fact.Path, fact.layer
			out = append(out, resolved)
		}
		return out
	}
	for _, resolved := range evidence.ownership {
		if !ownershipPathMatches(resolved.Path, relation.path) {
			continue
		}
		resolved.Path, resolved.layer = fact.Path, fact.layer
		out = append(out, resolved)
	}
	if len(out) == 0 {
		top := budgetTop(fact)
		top.Origin = "callback-result"
		out = append(out, top)
	}
	return out
}

// relationOwners is the conservative owner set of a Rel atom used by the
// predicates: the handles its invocation was given and the owners bound to
// its executed layers. A layer still unexecuted (an Exec entry) is open by
// deferral: the enclosing summary carries the obligation.
func relationOwners(fact OwnershipFact) []OwnershipFact {
	relation := fact.callbackRelation
	if relation == nil {
		return []OwnershipFact{budgetTop(fact)}
	}
	var out []OwnershipFact
	for _, argument := range relation.arguments {
		for _, owner := range expandRelations(collapseRecipeFacts(heldFacts(argument, 0))) {
			owner.Path, owner.layer = fact.Path, fact.layer
			out = append(out, owner)
		}
	}
	env := decodeOwnerEnv(fact.relationEnv)
	limit := len(env) - 1
	reached := relation.held
	if relation.failure != "" {
		// A payload of layer held may be acquired by that layer itself.
		reached++
	}
	if relation.held >= 0 && reached < limit {
		limit = reached
	}
	for level := 0; level < limit; level++ {
		if env[level].ownerKind == ownershipOwnerExec {
			continue
		}
		out = append(out, withOwner(OwnershipFact{Path: fact.Path, Origin: "callback-result", layer: fact.layer}, env[level]))
	}
	return out
}

// expandRelations replaces every Rel atom by its conservative owners.
func expandRelations(facts []OwnershipFact) []OwnershipFact {
	if !slices.ContainsFunc(facts, func(fact OwnershipFact) bool { return fact.ownerKind == ownershipOwnerRelation }) {
		return facts
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		if fact.ownerKind == ownershipOwnerRelation {
			out = append(out, relationOwners(fact)...)
			continue
		}
		out = append(out, fact)
	}
	return out
}
