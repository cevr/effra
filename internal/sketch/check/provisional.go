package check

import "ergo.local/prototype/internal/sketch/syntax"

// Provisional values (round 9 review M1). While a cyclic component's rows
// are still being inferred (the silent pass whose record class G reads), a
// type that typing would build from a row, such as the instance a bare
// generic catch arm takes from the caught row (catchType), is unknown, not
// invalid: the row may still grow to hold it. Typing treats such a value
// like an invalid one, with no diagnostic and no cascade, but it keeps the
// difference:
//
//   - a local bound to it is provisional (Var.provisional), and each read
//     of a provisional local counts (provSeq);
//   - a callback frame passed to a call that does not check it (an
//     invalid callee, an invalid argument, an arity error) is invalid only
//     when the call read no provisional value (callMark).
//
// So typing's record of a callback frame has three states (frameState).
// Invalid is the positive verdict: typing skipped the frame for a reason
// it diagnoses and that holds whatever the rows (a refused import, S104,
// S109), and only an invalid frame carries no row. Every other frame that
// typing did not check, whatever the route, is provisional and keeps the
// class G uses inside it, so the pin is conservative (rows.md, Capability
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
	// does not depend on a row still being inferred.
	frameInvalid
)

func (s frameState) String() string {
	switch s {
	case frameChecked:
		return "checked"
	case frameInvalid:
		return "invalid"
	}
	return "provisional"
}

// rowWaits reports whether a type built from a row now waits on a row
// still being inferred, and counts the provisional read when it does. A
// rule that builds a type from a row asks it before it reports the row
// as lacking the type.
func (c *checker) rowWaits() bool {
	if c.rowsOpen == 0 {
		return false
	}
	c.provSeq++
	return true
}

// readLocal counts a read of v when v is provisional.
func (c *checker) readLocal(v *Var) {
	if v.provisional {
		c.provSeq++
	}
}

// bindLocal marks v provisional when its type is invalid and the current
// statement has read a provisional value: the value it is bound to may
// come from that read. It returns v.
func (c *checker) bindLocal(v *Var) *Var {
	if isInvalid(v.Type) && c.provSeq != c.stmtMark {
		v.provisional = true
	}
	return v
}

// skipFrames records, after the call x is checked, each callback frame of
// x (a function-literal argument or the trailing block) that typing did
// not check: invalid when the call read no provisional value, else left
// provisional (no record).
func (c *checker) skipFrames(x *syntax.CallExpr) {
	if c.provSeq != c.callMark || c.curGen == 0 {
		return
	}
	skip := func(n syntax.Node) {
		if c.frameGen[n] != c.curGen {
			c.frameInv[n] = c.curGen
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
	}
	return frameProvisional
}
