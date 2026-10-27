package check

import (
	"strconv"
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
	solver := newMatrixSolver()
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
			if !solver.caseUseful(matrix, row, sizes) {
				c.errorf(item, CodeType, "this case is already covered by the cases before it (unreachable)")
				continue
			}
			matrix = addRow(matrix, row)
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
	if w, ok := solver.missing(matrix, sizes); ok {
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

// matrixSolver decides usefulness and totality over one switch's case
// matrix (Maranget, "Warnings for pattern matching", 2007). A pattern
// element is a variant index or -1 for _.
//
// Plain Maranget recursion splits on every complete column and can take
// time exponential in the number of subjects (round-1 review, M8: 21 binary
// subjects took 4.9 s). The solver prunes as OCaml's Parmatch does: the case
// matrix drops every row a later case subsumes; a submatrix holding a row of
// wildcards answers at once; equal rows collapse; a run of fixed query
// columns specializes without branching; and equal submatrices are answered
// once, memoized by their rows.
type matrixSolver struct {
	usefulMemo  map[string]bool
	missingMemo map[string]missingResult
}

type missingResult struct {
	witness []int
	ok      bool
}

func newMatrixSolver() *matrixSolver {
	return &matrixSolver{usefulMemo: map[string]bool{}, missingMemo: map[string]missingResult{}}
}

// caseUseful reports whether a case row is useful against the cases before
// it. Usefulness does not depend on the order of the columns, so the row's
// fixed columns move to the front: they specialize the matrix without
// branching, and what remains is a totality question over the wildcard
// columns, which the totality memo shares across the cases.
func (s *matrixSolver) caseUseful(rows [][]int, q []int, sizes []int) bool {
	order := make([]int, 0, len(q))
	for j, p := range q {
		if p >= 0 {
			order = append(order, j)
		}
	}
	for j, p := range q {
		if p < 0 {
			order = append(order, j)
		}
	}
	permute := func(r []int) []int {
		out := make([]int, len(order))
		for i, j := range order {
			out[i] = r[j]
		}
		return out
	}
	prows := make([][]int, len(rows))
	for i, r := range rows {
		prows[i] = permute(r)
	}
	return s.useful(prows, permute(q), permute(sizes))
}

// useful reports whether the pattern row q matches a value that no row of
// the matrix matches. A run of fixed columns specializes without branching;
// once the rest of q is wildcards, q is useful exactly when the matrix is not
// total, so the query shares the totality memo (as Parmatch reduces
// exhaustiveness to the usefulness of a wildcard row).
func (s *matrixSolver) useful(rows [][]int, q []int, sizes []int) bool {
	for {
		if len(rows) == 0 {
			return true
		}
		if allWild(q) {
			_, ok := s.missing(rows, sizes)
			return ok
		}
		if q[0] < 0 {
			break
		}
		rows, q, sizes = specialize(rows, q[0]), q[1:], sizes[1:]
	}
	rows, key := reduce(rows, q, sizes)
	if allWild(rows[0]) {
		return false
	}
	if v, ok := s.usefulMemo[key]; ok {
		return v
	}
	v := s.useful0(rows, q, sizes)
	s.usefulMemo[key] = v
	return v
}

// useful0 splits a wildcard column of q over the column's constructors.
func (s *matrixSolver) useful0(rows [][]int, q []int, sizes []int) bool {
	present := presentCtors(rows)
	if len(present) == sizes[0] {
		for ctor := 0; ctor < sizes[0]; ctor++ {
			if s.useful(specialize(rows, ctor), q[1:], sizes[1:]) {
				return true
			}
		}
		return false
	}
	return s.useful(defaults(rows), q[1:], sizes[1:])
}

// missing returns a value vector that no row matches, if one exists.
func (s *matrixSolver) missing(rows [][]int, sizes []int) ([]int, bool) {
	if len(sizes) == 0 {
		if len(rows) == 0 {
			return []int{}, true
		}
		return nil, false
	}
	rows, key := reduce(rows, nil, sizes)
	if len(rows) > 0 && allWild(rows[0]) {
		return nil, false
	}
	if r, ok := s.missingMemo[key]; ok {
		return r.witness, r.ok
	}
	w, ok := s.missing0(rows, sizes)
	s.missingMemo[key] = missingResult{w, ok}
	return w, ok
}

func (s *matrixSolver) missing0(rows [][]int, sizes []int) ([]int, bool) {
	present := presentCtors(rows)
	if len(present) == sizes[0] {
		for ctor := 0; ctor < sizes[0]; ctor++ {
			if w, ok := s.missing(specialize(rows, ctor), sizes[1:]); ok {
				return append([]int{ctor}, w...), true
			}
		}
		return nil, false
	}
	w, ok := s.missing(defaults(rows), sizes[1:])
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

func presentCtors(rows [][]int) map[int]bool {
	present := map[int]bool{}
	for _, r := range rows {
		if r[0] >= 0 {
			present[r[0]] = true
		}
	}
	return present
}

func allWild(r []int) bool {
	for _, p := range r {
		if p >= 0 {
			return false
		}
	}
	return true
}

// subsumes reports whether every value row b matches also matches row a.
func subsumes(a, b []int) bool {
	for i := range a {
		if a[i] >= 0 && a[i] != b[i] {
			return false
		}
	}
	return true
}

// addRow appends a useful case row to the matrix and drops the earlier rows
// it subsumes, so the case matrix never holds a subsumed row. Dropping one
// changes neither usefulness nor totality: every value it matches, the
// subsuming row matches too. The new row is useful, so no earlier row
// subsumes it.
func addRow(rows [][]int, row []int) [][]int {
	out := rows[:0:0]
	for _, r := range rows {
		if !subsumes(row, r) {
			out = append(out, r)
		}
	}
	return append(out, row)
}

// reduce prepares a submatrix for a branch: a row of wildcards covers every
// value and stands for the whole matrix; equal rows, which specialization
// makes of rows that differed only in a consumed column, collapse to the
// first. It returns the reduced rows with their memo key, built in the same
// pass. The key states the whole subproblem (the column sizes, the query
// and the rows), so a hit always answers the same question; rows keep their
// order, so subproblems that order-preserving steps make equal meet with
// equal keys.
func reduce(rows [][]int, q []int, sizes []int) ([][]int, string) {
	for _, r := range rows {
		if allWild(r) {
			rows = [][]int{r}
			break
		}
	}
	var b []byte
	b = appendRow(b, sizes)
	b = append(b, '|')
	b = appendRow(b, q)
	seen := make(map[string]bool, len(rows))
	out := rows[:0:0]
	var rk []byte
	for _, r := range rows {
		rk = appendRow(rk[:0], r)
		if seen[string(rk)] {
			continue
		}
		seen[string(rk)] = true
		out = append(out, r)
		b = append(b, '|')
		b = append(b, rk...)
	}
	return out, string(b)
}

func appendRow(b []byte, r []int) []byte {
	for i, p := range r {
		if i > 0 {
			b = append(b, ',')
		}
		if p < 0 {
			b = append(b, '_')
			continue
		}
		b = strconv.AppendInt(b, int64(p), 10)
	}
	return b
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
