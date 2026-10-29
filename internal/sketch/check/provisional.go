package check

import "ergo.local/prototype/internal/sketch/syntax"

// Undiagnosed values and typing's record of callback frames (round 9
// review M1; round 10 ruling). An undiagnosed value is one typing treats
// as invalid, with no diagnostic and no cascade, although no diagnostic of
// its own explains it:
//
//   - in the silent pass whose record class G reads, while a cyclic
//     component's rows are still being inferred, a type that typing would
//     build from a row is provisional: the instance a bare generic catch
//     arm takes from the caught row (catchType) may still arrive (rowWaits);
//   - in the reporting pass, a reference to a member class G refuses is
//     invalid so that nothing cascades from its pinned row (groupRef); its
//     diagnostic is the member's S108, which reportGroup decides last.
//
// A local bound to an undiagnosed value is undiagnosed (Var.undiagnosed),
// and each read of an undiagnosed value counts (undiagSeq). A callback
// frame passed to a call that does not check it (an invalid callee, an
// invalid argument, an arity error) is recorded after the call
// (skipFrames): invalid-diagnosed when the call read no undiagnosed value,
// refused-by-class-G in the reporting pass when it did, and provisional
// otherwise. So typing's record of a frame has four states (frameState).
//
// Invalid-diagnosed is the positive verdict: typing skipped the frame for
// a reason it reports and that holds whatever the rows (a refused import,
// S104, S109, the arm's own error). Every other skipped frame, whatever
// the route, keeps the class G uses inside it. In the silent pass this
// makes the pin conservative; in the reporting pass, reportGroup drops a
// use only when its frame is invalid-diagnosed, so a refusal never
// cascades and a class G refusal is never dropped (rows.md, Capability
// rows 6). A staged or unknown result is never evidence that a frame is
// not checked.
//
// Prior art: Hindley-Milner checkers keep an unsolved type a flexible
// variable and poison a type only on a reported error (Elm, Roc); go/types
// records a type once it is final; Gleam checks a group's uses on its
// solved types.

// A frameState is typing's record of a callback frame in one body check.
type frameState uint8

const (
	// frameProvisional: typing did not check the frame, with no positive
	// reason: the frame keeps its uses.
	frameProvisional frameState = iota
	// frameChecked: typing checked the frame.
	frameChecked
	// frameInvalid: typing skipped the frame for a diagnosed reason that
	// does not depend on a row still being inferred (invalid-diagnosed).
	frameInvalid
	// frameRefused: the reporting pass skipped the frame because its call
	// reads a member class G refuses (refused-by-class-G): the frame keeps
	// its uses.
	frameRefused
)

func (s frameState) String() string {
	switch s {
	case frameChecked:
		return "checked"
	case frameInvalid:
		return "invalid-diagnosed"
	case frameRefused:
		return "refused-by-class-G"
	}
	return "provisional"
}

// rowWaits reports whether a type built from a row now waits on a row
// still being inferred, and counts the undiagnosed read when it does. A
// rule that builds a type from a row asks it before it reports the row
// as lacking the type.
func (c *checker) rowWaits() bool {
	if c.rowsOpen == 0 {
		return false
	}
	c.undiagSeq++
	return true
}

// readUndiagnosed counts a read of an undiagnosed value.
func (c *checker) readUndiagnosed() { c.undiagSeq++ }

// readLocal counts a read of v when v is undiagnosed.
func (c *checker) readLocal(v *Var) {
	if v.undiagnosed {
		c.undiagSeq++
	}
}

// bindLocal marks v undiagnosed when its type is invalid and the current
// statement has read an undiagnosed value: the value it is bound to may
// come from that read. It returns v.
func (c *checker) bindLocal(v *Var) *Var {
	if isInvalid(v.Type) && c.undiagSeq != c.stmtMark {
		v.undiagnosed = true
	}
	return v
}

// skipFrames records, after the call x is checked, each callback frame of
// x (a function-literal argument or the trailing block) that typing did
// not check: invalid-diagnosed when the call read no undiagnosed value;
// else refused-by-class-G in the reporting pass, and provisional (no
// record) in a silent one.
func (c *checker) skipFrames(x *syntax.CallExpr) {
	if c.curGen == 0 {
		return
	}
	rec := c.frameInv
	if c.undiagSeq != c.callMark {
		if c.silent > 0 {
			return
		}
		rec = c.frameRef
	}
	skip := func(n syntax.Node) {
		if c.frameGen[n] != c.curGen {
			rec[n] = c.curGen
		}
	}
	for _, a := range x.Args {
		if fl, ok := a.(*syntax.FuncLit); ok {
			skip(fl)
		}
	}
	if x.Trailing != nil {
		skip(x.Trailing)
	}
}

// frameStateAt is typing's record of the callback frame n in the body
// check gen.
func (c *checker) frameStateAt(n syntax.Node, gen int) frameState {
	switch {
	case gen == 0:
		return frameProvisional
	case c.frameGen[n] == gen:
		return frameChecked
	case c.frameInv[n] == gen:
		return frameInvalid
	case c.frameRef[n] == gen:
		return frameRefused
	}
	return frameProvisional
}

// chainState is the state of g's frame in the body check gen: a frame
// inside one typing did not enter takes that frame's state. memo holds the
// states decided so far, so each guard node is decided once; trace, in a
// test build, receives the first verdict read for each frame.
func (c *checker) chainState(g *guardNode, gen int, memo map[*guardNode]frameState, trace map[syntax.Node]frameVerdict) frameState {
	var chain []*guardNode
	n := g
	for ; n != nil; n = n.up {
		if _, ok := memo[n]; ok {
			break
		}
		chain = append(chain, n)
	}
	st := frameChecked
	if n != nil {
		st = memo[n]
	}
	for i := len(chain) - 1; i >= 0; i-- {
		f := chain[i]
		if st == frameChecked {
			st = c.frameStateAt(f.frame, gen)
		}
		if trace != nil {
			if _, seen := trace[f.frame]; !seen {
				trace[f.frame] = frameVerdict{state: st, gen: gen}
			}
		}
		memo[f] = st
	}
	return st
}
