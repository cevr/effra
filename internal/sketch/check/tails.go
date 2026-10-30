package check

import "ergo.local/prototype/internal/sketch/syntax"

// functionBody checks a declared function or literal's result. A final
// expression is an implicit return; a void body discards its success value.
// Other final statements still use termination analysis. Branch expressions
// add their contextual result checking separately (D-C2).
func (c *checker) functionBody(body *syntax.BlockStmt, name string) {
	list := body.List
	if len(list) == 0 {
		c.missingBodyResult(body, name)
		return
	}
	last, tail := list[len(list)-1].(*syntax.ExprStmt)
	if !tail {
		c.stmtList(list, false)
		if !c.terminatesList(list) {
			c.missingBodyResult(body, name)
		}
		return
	}
	c.stmtList(list[:len(list)-1], false)
	results := c.fn.t.results
	var hint Type
	if len(results) == 1 {
		hint = results[0]
	}
	op := c.expr(last.X, hint)
	if op.invalid() {
		return
	}
	switch op.mode {
	case modeValue, modeConst, modeNoValue:
	default:
		c.needValue(&op)
		if op.invalid() {
			return
		}
	}
	fallible := op.fallible()
	if fallible {
		c.escape(last.X, op.errs)
		op.errs = nil
	}
	if op.mode == modeValue {
		if row := c.errorValueRow(op.typ); row != nil {
			c.escape(last.X, row)
			c.tail(last, op.typ)
			return
		}
	}
	if len(results) == 0 {
		if fallible {
			c.tail(last, NoValue)
		}
		return
	}
	if op.mode == modeNoValue {
		c.missingBodyResult(body, name)
		return
	}
	if len(results) == 1 {
		c.assign(&op, results[0], "the body's tail value")
		c.tail(last, results[0])
	} else {
		c.returnTuple(last.X, op, results)
		c.tail(last, op.typ)
	}
}

func (c *checker) missingBodyResult(body *syntax.BlockStmt, name string) {
	if len(c.fn.t.results) > 0 {
		c.errorAt(body.Rbrace, body.Rbrace+1, CodeMissingRet, "%s can reach the end of its body without returning a value (rows.md, Totality 3)", name)
	}
}
