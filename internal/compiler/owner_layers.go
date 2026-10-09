package compiler

import (
	"strconv"
	"strings"
)

// Recipe owner evidence is indexed by layer (design §3-§4). A recipe
// occurrence of type Effect^D<X> is a chain of layers L0 ... L(D-1). Each
// layer is a binder whose owner is chosen by whichever operation executes it:
// run binds it to the current region, timeout and fork to their own closing
// owners, and a layer provision to the provision.
//
// The chain is stored flattened on the occurrence's slots:
//
//   - a captures fact carries layer d: it belongs to the held set of layer
//     d, the handles that executing layer d uses. Layer D holds what the
//     innermost value itself holds (a provider's captures, for instance);
//   - ownership facts and the field table describe the innermost value X;
//   - an Exec owner names the executor of layer exec of the occurrence.
//     A recipe stored in a field of X numbers its own layers from D upwards,
//     so each level names one binder of the whole occurrence.
//
// Executing layer 0 (exec) substitutes its owner for Exec(0), drops the
// layer-0 held set after the caller checked it, and shifts every remaining
// level down by one. Closing layer 0 (close) substitutes a closed owner for
// Exec(0) and keeps the binder. The function edge (abstract) turns the
// body's own region into Exec(0) of the call layer and shifts every level
// up. Environment occurrences in a body are closed terms: they carry no
// Exec owner of their own level.

// recipeDepth is the number of recipe layers of a value contract.
func (c *checker) recipeDepth(id TypeID) int {
	depth := 0
	for range 256 {
		node := c.node(id)
		if node == nil {
			return depth
		}
		switch node.Kind {
		case "recipe":
			depth++
			id = node.Result
		case "providerRecipe":
			return depth + 1
		default:
			return depth
		}
	}
	return depth
}

func execRegion(level int) string { return "exec:" + strconv.Itoa(level) }

// execOwner is the owner atom Exec(level).
func execOwner(level int) OwnershipFact {
	return OwnershipFact{Status: "owned", Region: execRegion(level), ownerKind: ownershipOwnerExec, exec: level}
}

// lexicalOwner is the owner atom Lexical(region).
func lexicalOwner(region string) OwnershipFact {
	return OwnershipFact{Status: "owned", Region: region, ownerKind: ownershipOwnerLexical}
}

// closedOwner is the owner atom Closed(kind, site) of a closed timeout or
// child owner whose region is "timeout:N" or "child:N".
func closedOwner(kind ownershipOwnerKind, region string) OwnershipFact {
	return OwnershipFact{Status: "owned", Region: region, ownerKind: kind}
}

// withOwner replaces the owner atom of fact, keeping its path, layer and
// provenance.
func withOwner(fact, owner OwnershipFact) OwnershipFact {
	fact.Status = owner.Status
	fact.Region = owner.Region
	fact.ownerKind = owner.ownerKind
	fact.exec = owner.exec
	fact.source, fact.sourceSet = owner.source, owner.sourceSet
	if owner.potentialOwner {
		fact.potentialOwner = true
	}
	return fact
}

// ownerRewrite is one substitution of owner atoms over every slot of an
// occurrence: level gives the atom replacing Exec(level), and a Lexical
// owner of region (when set) becomes regionOwner.
type ownerRewrite struct {
	level       func(int) OwnershipFact
	region      string
	regionOwner OwnershipFact
}

// executeRewrite is exec(·, owner): Exec(0) := owner, and the remaining
// levels shift down.
func executeRewrite(owner OwnershipFact) ownerRewrite {
	return ownerRewrite{level: func(level int) OwnershipFact {
		if level == 0 {
			return owner
		}
		return execOwner(level - 1)
	}}
}

// closeRewrite is close(·, owner): Exec(0) := owner, and the binder stays.
func closeRewrite(owner OwnershipFact) ownerRewrite {
	return ownerRewrite{level: func(level int) OwnershipFact {
		if level == 0 {
			return owner
		}
		return execOwner(level)
	}}
}

// liftRewrite places an occurrence under one more binder.
func liftRewrite() ownerRewrite {
	return ownerRewrite{level: func(level int) OwnershipFact { return execOwner(level + 1) }}
}

// abstractRewrite is abstract(·, region) at an effect function edge: the
// body's region becomes Exec(0) of the call layer under one more binder.
func abstractRewrite(region string) ownerRewrite {
	r := liftRewrite()
	r.region, r.regionOwner = region, execOwner(0)
	return r
}

func (r ownerRewrite) owner(fact OwnershipFact) OwnershipFact {
	switch {
	case fact.ownerKind == ownershipOwnerExec:
		return withOwner(fact, r.level(fact.exec))
	case r.region != "" && fact.ownerKind == ownershipOwnerLexical && fact.Region == r.region:
		return withOwner(fact, r.regionOwner)
	}
	return fact
}

// rewriteFacts applies r to every owner atom of facts, including the owner
// vector and the argument occurrences of a deferred relation.
func (c *checker) rewriteFacts(facts []OwnershipFact, r ownerRewrite) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := cloneFacts(facts)
	for i := range out {
		if out[i].ownerKind != ownershipOwnerRelation {
			out[i] = r.owner(out[i])
			continue
		}
		env := decodeOwnerEnv(out[i].relationEnv)
		for j := range env {
			env[j] = r.owner(env[j])
		}
		out[i].relationEnv = encodeOwnerEnv(env)
		if relation := out[i].callbackRelation; relation != nil {
			arguments := make([]checkedExpression, len(relation.arguments))
			for j, argument := range relation.arguments {
				arguments[j] = c.rewriteOccurrence(argument, r)
			}
			out[i].callbackRelation = c.callbackRelation(relation.callee, arguments, relation.result, relation.path, relation.held, relation.effect, relation.failure)
			if out[i].callbackRelation == nil {
				out[i] = budgetTop(out[i])
			}
		}
	}
	return normalizeFacts(out)
}

// budgetTop is ⊤ for one path: evidence the bounded relation store could
// not retain.
func budgetTop(fact OwnershipFact) OwnershipFact {
	return OwnershipFact{Path: fact.Path, Status: "unknown", Origin: "callback-result-budget", potentialOwner: true, layer: fact.layer}
}

// rewriteOccurrence applies r to every fact slot of an occurrence and of its
// field occurrences. Captures keep their layers.
func (c *checker) rewriteOccurrence(e checkedExpression, r ownerRewrite) checkedExpression {
	return mapOccurrenceFacts(e, func(_ checkedExpression, _ occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact {
		return c.rewriteFacts(facts, r)
	})
}

// shiftHeldLayers moves the held layers of an occurrence's own captures by
// delta and drops the layers which fall below zero (an executed layer).
func shiftHeldLayers(facts []OwnershipFact, delta int) []OwnershipFact {
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		fact.layer += delta
		if fact.layer < 0 {
			continue
		}
		out = append(out, fact)
	}
	return normalizeFacts(out)
}

// heldAt is the held set of layer d.
func heldAt(facts []OwnershipFact, layer int) []OwnershipFact {
	var out []OwnershipFact
	for _, fact := range facts {
		if fact.layer == layer {
			out = append(out, fact)
		}
	}
	return out
}

// executeOccurrence is exec(L, owner) on a recipe occurrence whose layer-0
// held set the caller has checked. A provider recipe keeps that held set:
// the provider it produces holds the handles its construction captured.
func (c *checker) executeOccurrence(e checkedExpression, owner OwnershipFact) checkedExpression {
	out, _, _ := c.executeLayer(e, owner)
	return out
}

// executeLayer is executeOccurrence which also returns the payload evidence
// of the failures the executed layer raises, owned through owner (design
// §5.2 F2), and the failures of forked children pending on it (§5.3 rule
// 3): both become failures of the executing body.
func (c *checker) executeLayer(e checkedExpression, owner OwnershipFact) (checkedExpression, map[string][]OwnershipFact, failureEvidence) {
	keepHeld := c.isKind(e, "provider")
	held := heldAt(e.captureFacts(), 0)
	out := c.rewriteOccurrence(c.completeFailures(e), executeRewrite(owner))
	payloads := layerPayloads(out.failures, 0)
	pending := layerPending(out.failures, 0)
	out.failures = shiftFailures(out.failures, -1)
	out.observes = nil
	out.setCaptures(shiftHeldLayers(out.captureFacts(), -1))
	if keepHeld {
		out.setCaptures(normalizeFacts(append(out.captureFacts(), held...)))
	}
	return out, payloads, pending
}

// closeOccurrence is close(L, owner): layer 0 executes under a closing
// owner which has closed by the time its success is observable.
func (c *checker) closeOccurrence(e checkedExpression, owner OwnershipFact) checkedExpression {
	return c.rewriteOccurrence(e, closeRewrite(owner))
}

// liftOccurrence places a value occurrence under one more binder: the value
// becomes the success of a layer whose own held set is empty.
func (c *checker) liftOccurrence(e checkedExpression) checkedExpression {
	out := c.rewriteOccurrence(c.completeFailures(e), liftRewrite())
	out.setCaptures(shiftHeldLayers(out.captureFacts(), 1))
	out.failures = shiftFailures(out.failures, 1)
	out.observes = nil
	return out
}

// abstractOccurrence is abstract(Occ, region) at an effect function edge.
func (c *checker) abstractOccurrence(e checkedExpression, region string) checkedExpression {
	out := c.rewriteOccurrence(e, abstractRewrite(region))
	out.setCaptures(shiftHeldLayers(out.captureFacts(), 1))
	out.failures = forgetForks(shiftFailures(out.failures, 1))
	out.observes, out.forks = nil, nil
	return out
}

// abstractFacts applies the function-edge abstraction to facts whose slot
// has no held layers of its own (the facts of a field occurrence).
func (c *checker) abstractFacts(facts []OwnershipFact, region string) []OwnershipFact {
	return c.rewriteFacts(facts, abstractRewrite(region))
}

// Owner vectors of relations are encoded as text so OwnershipFact stays a
// comparable value. Entry j is the owner bound to layer j of the deferred
// invocation; the final entry is the tail: the level of the occurrence
// carrying the relation which the first layer past the invocation (a recipe
// stored in its innermost value) maps to.
func encodeOwnerEnv(env []OwnershipFact) string {
	var b strings.Builder
	for i, owner := range env {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(strconv.Itoa(int(owner.ownerKind)))
		b.WriteByte('=')
		b.WriteString(owner.Region)
	}
	return b.String()
}

func decodeOwnerEnv(encoded string) []OwnershipFact {
	if encoded == "" {
		return nil
	}
	parts := strings.Split(encoded, ";")
	env := make([]OwnershipFact, 0, len(parts))
	for _, part := range parts {
		kind, region, _ := strings.Cut(part, "=")
		k, _ := strconv.Atoi(kind)
		owner := OwnershipFact{Status: "owned", Region: region, ownerKind: ownershipOwnerKind(k)}
		if owner.ownerKind == ownershipOwnerExec {
			owner.exec, _ = strconv.Atoi(strings.TrimPrefix(region, "exec:"))
		}
		env = append(env, owner)
	}
	return env
}

// identityOwnerEnv is the owner vector of an invocation with layers layers
// carried by an occurrence numbered like the invocation itself.
func identityOwnerEnv(layers int) []OwnershipFact {
	env := make([]OwnershipFact, layers+1)
	for i := range env {
		env[i] = execOwner(i)
	}
	return env
}

// envRewrite maps the levels of an invocation's own evidence to the owners
// of the occurrence which carries it.
func envRewrite(env []OwnershipFact) ownerRewrite {
	return ownerRewrite{level: func(level int) OwnershipFact {
		layers := len(env) - 1
		if level < layers {
			return env[level]
		}
		tail := env[layers]
		if tail.ownerKind != ownershipOwnerExec {
			return OwnershipFact{Status: "unknown", Origin: "callback-result-budget", potentialOwner: true}
		}
		return execOwner(tail.exec + level - layers)
	}}
}

// summaryOccurrence is the function edge (design §4.3 abstract): an effect
// function's body result becomes the success of the call layer, so the
// body's own region becomes Exec(0) and every level and held layer moves
// under that binder. A pure function adds no layer, and its body owns no
// region of its own.
//
// The call layer itself (design §4.2, §5.2):
//
//   - holds the body's dereference obligations which outlive the body:
//     Param and Rel atoms and handles of enclosing regions. An obligation
//     on the body's own region was checked open where its layer executed,
//     and a closed or unknown one was reported there;
//   - raises the failures the body evaluation incurs, with their payload
//     evidence abstracted like the result, and carries the failures of
//     children the body may leave unobserved as pending.
//
// The body occurrence's own payload evidence must be complete for its own
// contract before invocationContract recontracts it (completeFailures).
func (c *checker) summaryOccurrence(body checkedExpression, effect bool) checkedExpression {
	if !effect {
		return body
	}
	out := c.abstractOccurrence(body, "invocation")
	var uses []OwnershipFact
	for _, fact := range body.evaluation.uses {
		switch {
		case fact.ownerKind == ownershipOwnerLexical && fact.Region == "invocation":
		case fact.potentialOwner, fact.Status == "owned" && closedOwnerKind(fact.ownerKind):
		case fact.ownerKind == ownershipOwnerExec:
		default:
			fact.layer = 0
			uses = append(uses, fact)
		}
	}
	if len(uses) > 0 {
		out.setCaptures(normalizeFacts(append(out.captureFacts(), c.abstractFacts(uses, "invocation")...)))
	}
	payloads := c.completeEvaluation(body.evaluation).payloads
	out.failures = withLayerPayloads(out.failures, 0, mapPayloadFacts(payloads, func(facts []OwnershipFact) []OwnershipFact {
		return c.abstractFacts(facts, "invocation")
	}))
	// The children the body may leave unobserved at an exit are pending on
	// the call layer; no caller can name them (design §5.3).
	pending := forgetForks(mapFailureFacts(body.evaluation.pending, func(facts []OwnershipFact) []OwnershipFact {
		return c.abstractFacts(facts, "invocation")
	}))
	out.failures = mergeFailures(out.failures, pending)
	return out
}
