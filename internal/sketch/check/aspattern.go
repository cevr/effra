package check

import (
	"unicode"
	"unicode/utf8"

	"ergo.local/prototype/internal/sketch/syntax"
)

// The `as` patterns of #196 (ninth round): `case T as v:` in a match or a
// catch arm matches T and binds the matched value, with T's type, to v. It
// replaces Go's switch binding `switch v := x.(type)` and the old catch
// binding `catch e { … }`, one binding per arm. This file checks the
// top-level form, alone in its case; a nested `as` and an `as` in a case
// with several patterns are check-v1's (196p), S109 until then.

// asParts splits `P as name` into P and name; any other pattern is itself
// with no name.
func asParts(e syntax.Expr) (syntax.Expr, *syntax.Ident) {
	if a, ok := e.(*syntax.AsPattern); ok {
		return a.X, a.Name
	}
	return e, nil
}

// asBinding is one `as` name of an arm, with the group that records a read.
type asBinding struct {
	name  *syntax.Ident
	group *switchGroup
}

// bindAsPatterns declares, in the current scope, the names that the `as`
// patterns of a match case bind. A case binds only when it has one
// pattern; rows[0] is its resolved row (nil when it did not resolve). A
// binding takes its variant's type, or the subject's type under `_`.
func (c *checker) bindAsPatterns(list []syntax.Expr, n int, rows [][]int, unions []*switchUnion, subj []Type) []asBinding {
	if len(list) != 1 {
		return nil
	}
	elts := []syntax.Expr{list[0]}
	if n > 1 {
		tup, ok := list[0].(*syntax.TupleExpr)
		if !ok || len(tup.Elts) != n {
			return nil
		}
		elts = tup.Elts
	}
	var row []int
	if len(rows) == 1 {
		row = rows[0]
	}
	var out []asBinding
	for j, e := range elts {
		core, name := asParts(e)
		c.declareRecordNames(core)
		if name == nil {
			continue
		}
		t := Type(invalid)
		switch {
		case row == nil || subj[j] == nil:
		case row[j] >= 0:
			t = unions[j].at(row[j])
		default:
			t = subj[j]
		}
		if b, ok := c.declareAs(name, t); ok {
			out = append(out, b)
		}
	}
	return out
}

// declareAs declares one `as` name with type t; `_` binds nothing.
func (c *checker) declareAs(name *syntax.Ident, t Type) (asBinding, bool) {
	if name.Name == "_" {
		return asBinding{}, false
	}
	g := &switchGroup{}
	c.fn.scope.insert(name.Name, &Var{Name: name.Name, Type: t, Pos: name.Pos(), group: g, used: true})
	return asBinding{name: name, group: g}, true
}

// declareRecordNames declares, with no type, each lower-case name that a
// record pattern binds. The pattern itself is S109 (196p); the names keep
// its arm from cascading into unknown-name errors.
func (c *checker) declareRecordNames(p syntax.Expr) {
	if _, ok := p.(*syntax.CompositeLit); !ok {
		return
	}
	syntax.Inspect(p, func(n syntax.Node) bool {
		var id *syntax.Ident
		switch n := n.(type) {
		case *syntax.KeyValueExpr:
			id, _ = n.Value.(*syntax.Ident)
		case *syntax.CompositeLit:
			for _, e := range n.Elts {
				if x, ok := e.(*syntax.Ident); ok {
					id = x
					c.declareUntyped(id)
				}
			}
			return true
		case *syntax.AsPattern:
			id = n.Name
		}
		c.declareUntyped(id)
		return true
	})
}

// declareUntyped declares a pattern name with the invalid type, so reads
// of it report nothing further.
func (c *checker) declareUntyped(id *syntax.Ident) {
	if id == nil || id.Name == "_" {
		return
	}
	if r, _ := utf8.DecodeRuneInString(id.Name); !unicode.IsLower(r) {
		return
	}
	c.fn.scope.insert(id.Name, &Var{Name: id.Name, Type: invalid, Pos: id.Pos(), used: true})
}

// catchAll reports whether a catch arm's lone name binds the whole failure
// (`case err:`, rows.md Catch 7): a lower-case name that names no type.
func (c *checker) catchAll(id *syntax.Ident) bool {
	if r, _ := utf8.DecodeRuneInString(id.Name); !unicode.IsLower(r) {
		return false
	}
	return c.lookupType(id.Name, c.localTypeCtx()) == nil
}

// unusedAs reports each `as` name that its arm never reads (S110), as the
// old switch binding was reported (rows.md, Totality 1).
func (c *checker) unusedAs(list []asBinding) {
	for _, b := range list {
		if !b.group.used {
			c.errorf(b.name, CodeUnused, "the binding %s is read nowhere in its arm; drop `as %s` (rows.md, Totality 1)", b.name.Name, b.name.Name)
		}
	}
}
