package compiler

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// recoverFailure checks `recipe.recover<E>(handler)`. The handler is an
// ordinary callable value which receives the declared E payload. Recovery
// eliminates exactly E from the recipe's carried failure row and adds the
// handler's own success, failure and service contract. It constructs a new
// recipe; neither the original recipe nor the handler runs here.
//
// Only a solitary typed E failure reaches the handler at run time. Defects,
// interruption and composite causes (including cleanup failures) propagate
// unchanged, as for the pure catch fallback.
func (c *checker) recoverFailure(e *Expr, env localEnv, inEffect bool) checkedExpression {
	t := c.expr(e.Left, env, inEffect)
	if c.abstractRow(t.failureRow()) {
		c.diagnostic("EF125", "recovery of an abstract row requires an unsupported row difference constraint", e.Span)
	}
	// The handler operand is read through its summary, whose pending is
	// joined below (design §5.5); it is not stored in a callable contract.
	outer := c.handlerOperand
	c.handlerOperand = e.Right
	handler := c.expr(e.Right, env, false)
	c.handlerOperand = outer
	c.expect(t.isEffect(), "EF105", "recover requires an Effect value", e.Span, t)
	payload := invalidTypeID
	if _, exists := c.program.Errors[e.Name]; !exists {
		c.diagnostic("EF102", "unknown failure "+e.Name, e.Span)
	} else if slices.Contains(builtinErrors(), e.Name) {
		c.diagnostic("EF106", "recover binds a declared error payload; handle builtin failure "+e.Name+" with catch", e.Span)
	} else {
		payload = c.canonicalRef(typeRef(e.Name))
		c.expect(c.hasRow(t, true, e.Name), "EF107", "effect does not admit failure "+e.Name, e.Span, t)
	}
	node := handler.node()
	if handler.isEffect() || node == nil || node.Kind != "callable" {
		c.expect(false, "EF106", "recovery handler must be a function value accepting "+e.Name, e.Right.Span, handler)
		return t
	}
	if len(node.Args) != 1 || (payload != invalidTypeID && !c.assignable(payload, node.Args[0], 0)) {
		c.diagnostic("EF106", "recovery handler must accept exactly the "+e.Name+" payload", e.Right.Span)
	}
	if result := c.node(node.Result); (result == nil || result.Kind != "never") && !c.assignable(node.Result, t.resultID(), 0) {
		c.diagnostic("EF106", "recovery handler must return "+c.displayTypeID(t.resultID()), e.Right.Span)
	}
	// One binder-aware rule (design §5.5). The handler receives the payload
	// of E raised by the recovered layer, with that layer's evidence: Exec(0)
	// is the failing execution, which is still running when the handler is
	// invoked (effect.go Recover).
	t = c.completeFailures(t)
	argument := c.payloadOccurrence(payload, layerPayloads(t.failures, 0)[e.Name])
	handled := c.handlerResult(handler.callableEvidence, argument, node.Result, node.FailureRow, node.Mode == "effect", e.Span)
	// Dispatch: the handler dereferences exactly the handles it uses. A
	// payload owned by a closed owner is refused only when the handler
	// uses it; Param and Rel obligations are deferred to the enclosing
	// summary.
	if uses := expandRelations(handled.uses); hasOwnedClosed(uses) || hasPotentialOwner(uses) {
		c.reportOwnership("recovery handler uses a failure payload owned by a closed scope", e.Span, ownershipRoots(uses, ""))
	}
	// The recovered recipe publishes either the original success or the
	// handler's invocation result. Join both complete result occurrences
	// (ownership, held captures and field evidence) and both capture sets:
	// dropping either branch would turn an acquisition into a false borrowed
	// proof. The handler runs when the recovered layer executes, so the
	// handles it uses stay obligations of that layer, except the failing
	// execution's own acquisitions (free_L discharges Exec(0)).
	t.setOwnership(mergeFacts(t.ownershipFacts(), handled.ownership))
	t.setCaptures(mergeFacts(mergeFacts(mergeFacts(t.captureFacts(), heldFacts(handler, 0)), handled.captures), freeRecoveredLayer(handled.uses)))
	if handled.succeeds {
		t.fields = c.joinKnownFields(t.resultID(), t.fields, handled.fields, e.Span)
	}
	// A callable produced by the handler has no named-callee evidence.
	t.callableEvidence = joinCallableEvidence(t.callableEvidence, callableEvidence{})
	t.callableDecl = nil
	// The handled failure leaves the recovered layer; the handler's own
	// failures are raised by that layer's execution. The handler's pending
	// children are instantiated at this call, as a call's are, so they are
	// distinct from the recovered recipe's own children.
	failures := c.handledRow(t, e.Name)
	t.failures = mergeFailures(handleWith(withLayerPayloads(t.failures, 0, withoutLabel(layerPayloads(t.failures, 0), e.Name)), e.Name, slices.Collect(maps.Keys(layerPayloads(handled.failures, 0)))), atCall(handled.failures, e.Span.Offset))
	services := c.rowLabels(t.serviceRow())
	if node.Mode == "effect" {
		failures = union(failures, c.rowLabels(node.FailureRow))
		services = union(services, c.rowLabels(node.ServiceRow))
	}
	t.value = c.recontractRows(t, c.internRow(failures), c.internRow(services))
	return c.completeHandled(t, e.Name)
}

// handledRow is the failure row of a recipe once recover or catch handles
// label: the label leaves the row unless a forked child may still raise it
// where an owner closes, which no handler around the layer observes (design
// §5.3 rule 5).
func (c *checker) handledRow(t checkedExpression, label string) []string {
	labels := c.rowLabels(t.failureRow())
	if slices.Contains(pendingLabels(t.failures, 0), label) {
		return labels
	}
	return remove(labels, label)
}

// completeHandled completes a recovered recipe's evidence. A handled label
// which stays in the row only as pending has no ordinary raise left unless
// the handler raises it again: its ordinary payload is proven empty.
func (c *checker) completeHandled(t checkedExpression, label string) checkedExpression {
	if slices.Contains(pendingLabels(t.failures, 0), label) {
		if _, raised := t.failures[raisedKey(0, label)]; !raised {
			t.failures = cloneFailures(t.failures)
			t.failures[raisedKey(0, label)] = nil
		}
	}
	return c.completeFailures(t)
}

// withoutLabel is payloads without label's entry.
func withoutLabel(payloads map[string][]OwnershipFact, label string) map[string][]OwnershipFact {
	out := clonePayloads(payloads)
	delete(out, label)
	return out
}

// freeRecoveredLayer is free_L (design §5.5): the handles a recovery
// handler uses, as obligations of the recovered layer. The failing
// execution's own acquisitions, Exec(0), are open while the handler runs
// inside it; every other obligation stays on the layer and is checked where
// the layer executes.
func freeRecoveredLayer(uses []OwnershipFact) []OwnershipFact {
	var out []OwnershipFact
	for _, fact := range uses {
		if fact.ownerKind == ownershipOwnerExec && fact.exec == 0 {
			continue
		}
		fact.layer = 0
		out = append(out, fact)
	}
	return normalizeFacts(out)
}

// handledResult is one recovery handler invocation, numbered like the
// recovered recipe: the handler runs inside the failing execution, so its
// result is that execution's success and its failures are that execution's
// failures. uses is what an effect handler's own execution dereferences,
// with the payload substituted. A handler proven never to succeed has
// succeeds false and contributes no success.
type handledResult struct {
	ownership []OwnershipFact
	captures  []OwnershipFact
	uses      []OwnershipFact
	failures  failureEvidence
	fields    map[string]checkedExpression
	succeeds  bool
}

// handlerResult summarizes one invocation of a recovery handler. An effect
// handler's invocation layer is the recovered recipe's layer 0; a pure
// handler's result is that layer's success, one binder deeper than the
// handler's own numbering.
func (c *checker) handlerResult(evidence callableEvidence, argument checkedExpression, result TypeID, callRow RowID, effect bool, span Span) handledResult {
	layers := c.invocationLayers(result, effect)
	shift := 0
	if !effect {
		shift = 1
	}
	env := make([]OwnershipFact, layers+1)
	for i := range env {
		env[i] = execOwner(i + shift)
	}
	invoked := c.invocationEvidence(evidence, []checkedExpression{argument}, result, effect, callRow, env, 0)
	captures := shiftHeldLayers(invoked.captures, shift)
	out := handledResult{ownership: invoked.ownership, fields: invoked.fields, failures: shiftFailures(invoked.failures, shift), succeeds: true}
	if effect {
		out.uses = heldAt(captures, 0)
		if !summarizedCallees(evidence, effect) {
			// An unknown handler uses the payload it is given (§4.2).
			out.uses = mergeFacts(out.uses, collapseRecipeFacts(heldFacts(argument, 0)))
		}
		var produced []OwnershipFact
		for _, fact := range captures {
			if fact.layer > 0 {
				produced = append(produced, fact)
			}
		}
		captures = normalizeFacts(produced)
	}
	out.captures = captures
	if !evidence.unresolved && evidence.count > 0 && evidence.parameter == nil {
		out.succeeds = slices.ContainsFunc(evidence.callees[:evidence.count], func(f *Function) bool { return !f.returnsNever })
	}
	return out
}

// joinKnownFields joins two field evidence maps for values of one layout.
// Missing evidence on either side may denote any value, so the join is
// missing too rather than the other side's narrower evidence.
func (c *checker) joinKnownFields(layout TypeID, a, b map[string]checkedExpression, span Span) map[string]checkedExpression {
	if a == nil || b == nil {
		return nil
	}
	left, right := c.checkedDataID(layout, nil, nil), c.checkedDataID(layout, nil, nil)
	left.fields, right.fields = a, b
	return c.joinExpressionFields(left, right, span, 0, new(int))
}

// recoverFailure lowers recovery to efRecover. The recipe and handler values
// are evaluated in source order when the recovered recipe is constructed; the
// handler is invoked only inside the runtime recovery path, so a pure handler
// panic becomes a defect of that recipe.
func (g *goEmitter) recoverFailure(e *Expr, effect bool, ret string, out *strings.Builder) string {
	left := g.expr(e.Left, effect, ret, out)
	program := g.temp()
	out.WriteString(program + " := " + left + "\n")
	right := g.expr(e.Right, false, ret, out)
	handler := g.temp()
	out.WriteString(handler + " := " + right + "\n")
	valueType := g.resultType(e)
	invoke := "return " + handler + "(efPayload)"
	if node := e.Right.checked.node(); node == nil || node.Mode != "effect" {
		value := handler + "(efPayload)"
		if node != nil && canonicalVoidType(g.program.semantic, node.Result) {
			value = "struct{}{}"
			invoke = handler + "(efPayload)\n"
		} else {
			invoke = ""
		}
		invoke = "return func(efContext) efExit[" + valueType + "] {\n" + invoke + "return er.Succeed[" + valueType + "](" + value + ")\n}"
	}
	return "efRecover(" + program + ", " + strconv.Quote(e.Name) + ", func(efPayload efType_" + goIdent(e.Name) + ") efEffect[" + valueType + "] {\n" + invoke + "\n})"
}
