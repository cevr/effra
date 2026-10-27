package check

import (
	"strings"

	"effra.local/prototype/internal/sketch/syntax"
)

// typeSwitch checks a single- or multi-subject type switch over unions. Each
// case type is a variant of its subject's union (S108); a case that earlier
// cases already cover is S108 (unreachable); the switch is total, by a
// usefulness check over the case matrix, or has default (S103; rows.md,
// Totality 2 and 5); each binding is read in some case or is _ (S110).
//
// Prior art: Maranget's usefulness algorithm, as in OCaml's Parmatch, Rust
// and Gleam's exhaustiveness checker: totality is decided over the case
// matrix, never by enumerating the product of the subjects.
func (c *checker) typeSwitch(s *syntax.TypeSwitchStmt, label string) {
	done := c.openScope()
	defer done()
	if s.Init != nil {
		c.stmt(s.Init)
	}
	n := len(s.Subjects)
	subj := make([]Type, n)
	unions := make([]*Union, n)
	valid := true
	for i, g := range s.Subjects {
		op := c.valueExpr(g.X, nil)
		if op.invalid() {
			valid = false
			continue
		}
		u := asUnion(op.typ)
		if u == nil {
			c.errorf(g, CodeType, "a type switch's subject must be a union, not %s (rows.md, Totality 1)", op.typ)
			valid = false
			continue
		}
		subj[i], unions[i] = op.typ, u
	}
	groups := make([]*switchGroup, len(s.Binds))
	for i, b := range s.Binds {
		if b.Name != "_" {
			groups[i] = &switchGroup{}
		}
	}
	pop := c.push(brSwitch, label)
	defer pop()
	sizes := make([]int, n)
	for i, u := range unions {
		if u != nil {
			sizes[i] = len(u.Variants)
		}
	}
	var matrix [][]int
	hasDefault := false
	patternsOK := valid
	for _, cl := range s.Clauses {
		var tuples [][]int
		if cl.List == nil {
			if hasDefault {
				c.errorf(cl, CodeType, "a switch has at most one default")
			}
			hasDefault = true
		}
		for _, item := range cl.List {
			var elts []syntax.Expr
			if n == 1 {
				elts = []syntax.Expr{item}
			} else if tup, ok := item.(*syntax.TupleExpr); ok && len(tup.Elts) == n {
				elts = tup.Elts
			} else {
				c.errorf(item, CodeType, "a case of a %d-subject switch is a tuple of %d types", n, n)
				patternsOK = false
				continue
			}
			row := make([]int, n)
			ok := true
			for j, e := range elts {
				if !valid {
					ok = false
					break
				}
				idx := c.caseVariant(e, unions[j], subj[j])
				if idx == -2 {
					ok = false
					patternsOK = false
					continue
				}
				row[j] = idx
			}
			if !ok {
				continue
			}
			if !useful(matrix, row, sizes) {
				c.errorf(item, CodeType, "this case is already covered by the cases before it (unreachable)")
				continue
			}
			matrix = append(matrix, row)
			tuples = append(tuples, row)
		}
		inner := c.openScope()
		for j, b := range s.Binds {
			if groups[j] == nil || j >= n {
				continue
			}
			bt := subj[j]
			if bt == nil {
				bt = invalid
			} else if len(tuples) > 0 {
				first := tuples[0][j]
				same := first >= 0
				for _, t := range tuples[1:] {
					if t[j] != first {
						same = false
					}
				}
				if same && len(tuples) > 0 && cl.List != nil {
					bt = unions[j].Variants[first]
				}
			}
			v := &Var{Name: b.Name, Type: bt, Pos: b.Pos(), group: groups[j], used: true}
			c.fn.scope.insert(b.Name, v)
		}
		c.stmtList(cl.Body, false)
		inner()
	}
	for j, g := range groups {
		if g != nil && !g.used {
			c.errorf(s.Binds[j], CodeUnused, "the type-switch binding %s is read in no case; write _ (rows.md, Totality 1)", s.Binds[j].Name)
		}
	}
	if !valid || !patternsOK {
		// Already reported: the switch counts as total, so no missing
		// return or totality diagnostic follows from it (D3).
		c.totalSwitch[s] = true
		return
	}
	if hasDefault {
		c.totalSwitch[s] = true
		return
	}
	if w, ok := missing(matrix, sizes); ok {
		parts := make([]string, len(w))
		for j, idx := range w {
			parts[j] = unions[j].Variants[idx].String()
		}
		what := parts[0]
		if n > 1 {
			what = "(" + strings.Join(parts, ", ") + ")"
		}
		c.errorAt(s.Switch, s.Switch+6, CodeNotTotal, "this type switch is not total: no case handles %s; name every variant or add default (rows.md, Totality 2)", what)
		return
	}
	c.totalSwitch[s] = true
}

// caseVariant resolves a case type to its variant index in the union, -1
// for _, or -2 on an error (reported).
func (c *checker) caseVariant(e syntax.Expr, u *Union, subj Type) int {
	if id, ok := e.(*syntax.Ident); ok && id.Name == "_" {
		return -1
	}
	t := c.typExpr0(e, c.localTypeCtx())
	if isInvalid(t) {
		return -2
	}
	if n, ok := t.(*Named); ok && n.Orig == nil && len(n.TParams) > 0 {
		for i, v := range u.Variants {
			if vn, ok := v.(*Named); ok && vn.origin() == n {
				return i
			}
		}
		c.errorf(e, CodeType, "%s is not a variant of %s (rows.md, Totality 1)", n.Obj.Name, subj)
		return -2
	}
	for i, v := range u.Variants {
		if identical(v, t) {
			return i
		}
	}
	c.errorf(e, CodeType, "%s is not a variant of %s (rows.md, Totality 1)", t, subj)
	return -2
}

// useful reports whether the pattern row q matches a value that no row of
// the matrix matches (Maranget, "Warnings for pattern matching", 2007). A
// pattern element is a variant index or -1 for _.
func useful(rows [][]int, q []int, sizes []int) bool {
	if len(q) == 0 {
		return len(rows) == 0
	}
	if q[0] >= 0 {
		return useful(specialize(rows, q[0]), q[1:], sizes[1:])
	}
	present := map[int]bool{}
	for _, r := range rows {
		if r[0] >= 0 {
			present[r[0]] = true
		}
	}
	if len(present) == sizes[0] {
		for ctor := 0; ctor < sizes[0]; ctor++ {
			if useful(specialize(rows, ctor), q[1:], sizes[1:]) {
				return true
			}
		}
		return false
	}
	return useful(defaults(rows), q[1:], sizes[1:])
}

// missing returns a value vector that no row matches, if one exists.
func missing(rows [][]int, sizes []int) ([]int, bool) {
	if len(sizes) == 0 {
		if len(rows) == 0 {
			return []int{}, true
		}
		return nil, false
	}
	present := map[int]bool{}
	for _, r := range rows {
		if r[0] >= 0 {
			present[r[0]] = true
		}
	}
	if len(present) == sizes[0] {
		for ctor := 0; ctor < sizes[0]; ctor++ {
			if w, ok := missing(specialize(rows, ctor), sizes[1:]); ok {
				return append([]int{ctor}, w...), true
			}
		}
		return nil, false
	}
	w, ok := missing(defaults(rows), sizes[1:])
	if !ok {
		return nil, false
	}
	for ctor := 0; ctor < sizes[0]; ctor++ {
		if !present[ctor] {
			return append([]int{ctor}, w...), true
		}
	}
	return nil, false
}

func specialize(rows [][]int, ctor int) [][]int {
	var out [][]int
	for _, r := range rows {
		if r[0] == ctor || r[0] < 0 {
			out = append(out, r[1:])
		}
	}
	return out
}

func defaults(rows [][]int) [][]int {
	var out [][]int
	for _, r := range rows {
		if r[0] < 0 {
			out = append(out, r[1:])
		}
	}
	return out
}
