package check

// Poisoned error rows (rows.md, Capability rows 6; round 12 review).
//
// When reportGroup drops a member's class G S108 for its error row, the
// member keeps its pinned row, which its body exceeds and no diagnostic
// explains. The pinned row is a subset of the real row, and so is every
// row that joins it: joins and the rest of a catch are monotone. So a
// poisoned row is a lower bound, and the row checks split in two:
//
//   - a check that reports presence (an effect in the row that nothing
//     handles: Catch 4, an undeclared error) is sound on a subset, because
//     the effect is really there. It always runs;
//   - a check that reports absence ("catch on an expression that cannot
//     fail", "T is not in the caught row", "try on an expression that
//     cannot fail": Catch 1 and 2, Error rows 6) needs the whole row. It
//     is silent where the row it reads holds poison.
//
// Poison starts only at a member whose error-row S108 was dropped
// (Func.poisoned). It follows row flow: a row read at a call holds the
// rows of the functions whose variables the callee's row holds, and of the
// function literal it calls (rowTaint). A row escapes into its target with
// that taint (escape), so a function, or a function literal, is poisoned
// only when a poisoned row reaches its own inferred row (Func.flow,
// closePoison). A total catch discharges its row, so it stops the flow; a
// partial catch passes its rest on with the taint.
//
// The decision is made with the converged rows: a cyclic component's flow
// is recorded by its converge checks, which read every member's row, and
// the dropped S108s are known only after its reporting pass. So an absence
// check in the reporting pass whose row reads a member of the component
// is deferred, and reported after reportGroup unless the member it reads
// is poisoned by then (flushDeferred). Nothing is silenced in advance.
//
// Prior art: Elm and Roc poison a type variable on a failed unification,
// and the poison moves only along unification, so an unrelated conflict
// still reports; Merlin types a broken node with a fresh variable, so only
// the expressions that depend on it go quiet; TypeScript's error type
// silences only the expressions it flows into.

// rowTaint is the set of inferred functions whose error rows a row was
// built from, kept only for those that can hold poison: a poisoned
// function, or a member of the component being checked, whose poison is
// decided at the end of its check. Treat it as immutable once built.
type rowTaint []*Func

// poisonState is the verdict of a row read for an absence check.
type poisonState uint8

const (
	poisonNone    poisonState = iota // the row is the whole row
	poisonPending                    // decided after reportGroup (deferred)
	poisonHeld                       // the row is a lower bound: silent
)

// deferredDiag is an absence check of the reporting pass, reported after
// reportGroup unless its row then holds poison.
type deferredDiag struct {
	d     Diagnostic
	taint rowTaint
}

// canPoison reports whether f's error row may hold poison now or when its
// component is decided.
func (c *checker) canPoison(f *Func) bool {
	return f != nil && f.inferErrors && (f.poisoned || c.group[f])
}

// callTaint is the taint of the error row a call of sig reads: the
// functions whose row variables the row holds, and the flow of the
// function literal sig belongs to (sigTaint).
func (c *checker) callTaint(sig *Signature) rowTaint {
	var out rowTaint
	if r := sig.Errors; r != nil && r.vars {
		for _, e := range r.Elems() {
			if v, ok := e.(*RowVar); ok && c.canPoison(v.root().Fn) {
				out = out.with(v.root().Fn)
			}
		}
	}
	for _, f := range c.sigTaint[sig] {
		if c.canPoison(f) {
			out = out.with(f)
		}
	}
	return out
}

// with returns ts with f added once.
func (ts rowTaint) with(f *Func) rowTaint {
	for _, g := range ts {
		if g == f {
			return ts
		}
	}
	return append(ts[:len(ts):len(ts)], f)
}

// flowInto records that a row with taint ts escapes into target t's
// inferred error row (escape).
func (t *target) flowInto(ts rowTaint) {
	for _, f := range ts {
		if t.flowSeen[f] {
			continue
		}
		if t.flowSeen == nil {
			t.flowSeen = map[*Func]bool{}
		}
		t.flowSeen[f] = true
		t.flow = append(t.flow, f)
	}
}

// poisonOf decides a row read with taint ts: held when a function in it
// is poisoned, pending in the reporting pass when it reads a member of
// the component being checked, whose poison is decided after reportGroup.
func (c *checker) poisonOf(ts rowTaint) poisonState {
	pending := false
	for _, f := range ts {
		if f.poisoned {
			return poisonHeld
		}
		if c.group[f] {
			pending = true
		}
	}
	if pending && c.silent == 0 {
		return poisonPending
	}
	return poisonNone
}

// absence runs an absence check whose row has taint ts (Catch 1 and 2,
// Error rows 6). It returns false, and reports nothing, when the row holds
// poison. Otherwise it calls report, deferring its diagnostic while the
// row's poison is undecided, and returns true: the check fired.
func (c *checker) absence(ts rowTaint, report func()) bool {
	switch c.poisonOf(ts) {
	case poisonHeld:
		return false
	case poisonPending:
		saved := c.deferring
		c.deferring = ts
		report()
		c.deferring = saved
		return true
	}
	report()
	return true
}

// litFlow keeps the flow of a function literal's or block's inferred error
// row by its signature, so a call of it reads the poison its body read
// (callTaint). A declared or expected row holds no poison.
func (c *checker) litFlow(sig *Signature, t *target) {
	if t.declErrors == nil && len(t.flow) > 0 {
		c.sigTaint[sig] = t.flow
	}
}

// recordFlow keeps the flow of f's inferred error row after a check of its
// body. A cyclic component's flow is its converge checks': its reporting
// pass refuses the references to a refused member, so that member's row
// reaches no row there, although the converged rows hold it.
func (c *checker) recordFlow(f *Func, t *target) {
	if !f.inferErrors || c.silent == 0 && c.flowFrozen {
		return
	}
	f.flow = t.flow
}

// closePoison poisons each inferred member of scc that a poisoned row
// reaches, through the members' flows: a member whose flow holds a
// poisoned function, and each member whose flow holds such a member.
func (c *checker) closePoison(scc []*bodyNode) {
	readers := map[*Func][]*Func{}
	var queue []*Func
	for _, n := range scc {
		f := n.fn
		if f == nil || !f.inferErrors {
			continue
		}
		for _, g := range f.flow {
			if c.group[g] {
				readers[g] = append(readers[g], f)
			}
			if g.poisoned && !f.poisoned {
				f.poisoned = true
			}
		}
		if f.poisoned {
			queue = append(queue, f)
		}
	}
	for len(queue) > 0 {
		g := queue[0]
		queue = queue[1:]
		for _, f := range readers[g] {
			if !f.poisoned {
				f.poisoned = true
				queue = append(queue, f)
			}
		}
	}
}

// flushDeferred reports each deferred absence check whose row holds no
// poison now that its component is decided, and drops the rest.
func (c *checker) flushDeferred() {
	list := c.deferred
	c.deferred = nil
	for _, dd := range list {
		if c.poisonOf(dd.taint) == poisonHeld {
			continue
		}
		c.emit(dd.d)
	}
}
