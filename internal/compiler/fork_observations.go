package compiler

import "strconv"

// Observation of forked children is composed along evaluation order (design
// §5.3 rule 2). An expression observes a fork instance when evaluating it
// completes a join or interrupt of that child, and a child counts as observed
// at an exit only if the observation completed before the exit was taken.
// Every checked expression carries two sets: kills, observed before its
// success exit, and exitKills, observed before each of its typed-failure
// exits. A block reads them to remove observed children from the live set,
// and an expression made of eagerly evaluated parts (a call and its
// arguments, an operand pair, a scrutinee and its arms) composes the sets of
// its parts here, so a join nested in an argument or a match arm counts
// exactly as a join at statement level does.

// observation is what evaluating one part of an expression observes.
type observation struct {
	kills     []string
	exitKills []string
	// exits reports whether the part may take a typed-failure exit.
	exits bool
}

// observationOf is the observation of a checked part.
func (c *checker) observationOf(e checkedExpression) observation {
	return observation{kills: e.kills, exitKills: e.exitKills, exits: len(c.rowLabels(e.evaluation.failureRowID())) > 0}
}

// sequenceObservations composes parts evaluated in order. The success exit
// follows every part, so it has observed all of their kills. A failure exit
// of part i has observed the kills of the parts before it and the part's own
// exit kills; the composite guarantees only what every exit has observed,
// which is why an earlier part which fails first leaves a later join unseen.
func sequenceObservations(parts ...observation) observation {
	var out observation
	var done, exitKills []string
	for _, part := range parts {
		if part.exits {
			observed := unionStrings(done, part.exitKills)
			if out.exits {
				exitKills = intersectStrings(exitKills, observed)
			} else {
				exitKills, out.exits = append([]string{}, observed...), true
			}
		}
		done = unionStrings(done, part.kills)
	}
	out.kills = done
	out.exitKills = done
	if out.exits {
		out.exitKills = exitKills
	}
	return out
}

// alternativeObservations composes branches of which exactly one evaluates:
// only what every branch observes is observed, at each of its exits.
func alternativeObservations(parts ...observation) observation {
	var out observation
	for i, part := range parts {
		if i == 0 {
			out.kills = append([]string{}, part.kills...)
		} else {
			out.kills = intersectStrings(out.kills, part.kills)
		}
		if !part.exits {
			continue
		}
		if out.exits {
			out.exitKills = intersectStrings(out.exitKills, part.exitKills)
		} else {
			out.exitKills, out.exits = append([]string{}, part.exitKills...), true
		}
	}
	if !out.exits {
		out.exitKills = out.kills
	}
	return out
}

// observe records o as the observation of the expression.
func (e *checkedExpression) observe(o observation) {
	e.kills, e.exitKills = o.kills, o.exitKills
}

// childObservation is the observation of an expression whose children all
// evaluate eagerly, in source order.
func (c *checker) childObservation(e *Expr) observation {
	var parts []observation
	forEachExprChild(e, func(child *Expr) {
		parts = append(parts, c.observationOf(child.checked))
	})
	return sequenceObservations(parts...)
}

// alternativeEvaluation tags the children an arm leaves live or pending as
// belonging to arm of the branch at offset (failureKey.alt).
func (c *checker) alternativeEvaluation(e ExpressionEvaluation, offset, arm int) ExpressionEvaluation {
	segment := strconv.Itoa(offset) + "." + strconv.Itoa(arm)
	e.live, e.pending = tagAlternative(e.live, segment), tagAlternative(e.pending, segment)
	return e
}
