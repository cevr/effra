package check

import (
	"sort"
	"strconv"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
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
// matchLen is the length of the keyword a totality diagnostic spans: the
// decided parser builds every type switch from `match` (#196).
const matchLen = syntax.Pos(len("match"))

func (c *checker) typeSwitch(s *syntax.TypeSwitchStmt) {
	if s.Init != nil || s.Binds != nil || s.Define.IsValid() {
		c.undecided(s, "a match header with an init statement or a binding")
		return
	}
	done := c.openScope()
	defer done()
	n := len(s.Subjects)
	subj := make([]Type, n)
	unions := make([]*switchUnion, n)
	valid := true
	for i, g := range s.Subjects {
		op := c.valueExpr(g.X, nil)
		if op.invalid() {
			valid = false
			continue
		}
		u := c.subjectUnion(op.typ)
		if u == nil {
			c.errorf(g, CodeType, "a type switch's subject must be a union, not %s (rows.md, Totality 1)", op.typ)
			valid = false
			continue
		}
		subj[i], unions[i] = op.typ, u
	}
	pop := c.push(brSwitch)
	defer pop()
	if valid {
		c.match(s, &Match{Subjects: subj, Arms: len(s.Clauses)})
	}
	sizes := make([]int, n)
	for i, u := range unions {
		if u != nil {
			sizes[i] = u.len()
		}
	}
	// Resolving the cases is charged to the switch's own step counter,
	// against the solvers' budget (round 9 class C): past it the switch is
	// S103, so no uncounted pass runs before the solvers.
	resolve := &lookupSteps{ops: &c.variantOps}
	// The case rows, resolved once, in source order: rowOf[clause][item]
	// is nil for a case that does not resolve (reported).
	hasDefault := false
	patternsOK := valid
	rowOf := make([][][]int, len(s.Clauses))
	var all [][]int
	for ci, cl := range s.Clauses {
		if cl.List == nil {
			if hasDefault {
				c.errorf(cl, CodeType, "a switch has at most one default")
			}
			hasDefault = true
		}
		rowOf[ci] = make([][]int, len(cl.List))
		for ii, item := range cl.List {
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
			var elems []*Pattern
			for j, e := range elts {
				if !valid {
					ok = false
					break
				}
				core, name := asParts(e)
				if name != nil && len(cl.List) > 1 {
					// A binding in a case with several patterns must
					// bind in each of them; check-v1 decides it (196p).
					c.notYet(e)
					ok, patternsOK = false, false
					continue
				}
				idx := c.caseVariant(core, unions[j], subj[j], resolve)
				if idx == variantOver {
					resolve.over = true
					ok = false
					patternsOK = false
					break
				}
				if idx == -2 {
					ok = false
					patternsOK = false
					continue
				}
				row[j] = idx
				p := &Pattern{Kind: PatWildcard}
				if idx >= 0 {
					p = &Pattern{Kind: PatTypeTest, Type: unions[j].at(idx)}
				}
				c.pattern(core, p)
				if name != nil {
					p = c.asPattern(e, p, name)
				}
				elems = append(elems, p)
			}
			if ok {
				rowOf[ci][ii] = row
				all = append(all, row)
				if n > 1 {
					c.pattern(item, &Pattern{Kind: PatTuple, Elems: elems})
				}
			}
		}
	}
	// The solvers see the columns in source order: the solver is
	// deterministic for a given source, and reordering the subjects may
	// change whether a switch near the budget is refused, never a proved
	// verdict (D16 as amended in round 5). Reachability follows the cases'
	// order: a case is unreachable when the cases before it cover it.
	useful := newMatrixSolver(sizes)
	var matrix [][]int
	for ci, cl := range s.Clauses {
		for ii, item := range cl.List {
			row := rowOf[ci][ii]
			if row == nil || resolve.over { // past the budget: refused below, no case verdict
				continue
			}
			if u, ok := useful.caseUseful(matrix, row); ok && !u {
				c.errorf(item, CodeType, "this case is already covered by the cases before it (unreachable)")
				continue
			}
			matrix = useful.addRow(matrix, row)
		}
		inner := c.openScope()
		asGroups := c.bindAsPatterns(cl.List, n, rowOf[ci], unions, subj)
		c.stmtList(cl.Body, false)
		c.unusedAs(asGroups)
		inner()
	}
	// Totality is decided over the maximal case rows by a solver of its
	// own, so it does not depend on which cases reachability judged first.
	// A switch with default needs no proof: its cost stays linear in the
	// subjects and cases (round-4 M4).
	total := newMatrixSolver(sizes)
	var set [][]int
	if valid && patternsOK && !hasDefault {
		for _, row := range all {
			set = total.addMaximal(set, row)
		}
	}
	w, missing, ok := total.total(set)
	if c.silent == 0 {
		c.totalitySteps = append(c.totalitySteps, total.steps)
		c.usefulSteps = append(c.usefulSteps, useful.steps)
	}
	if useful.over {
		// A case whose reachability is unproven is never accepted.
		c.errorAt(s.Switch, s.Switch+matchLen, CodeNotTotal, "too many cases to prove which cases of this switch are reachable; split it (rows.md, Totality 5)")
		c.totalSwitch[s] = true
		return
	}
	if resolve.over {
		// Resolving the cases ran past the budget (round 9 class C): the
		// switch is refused like a solver past its budget, never accepted
		// unproven.
		c.errorAt(s.Switch, s.Switch+matchLen, CodeNotTotal, "too many cases to prove this switch total; split it or add default (rows.md, Totality 5)")
		c.totalSwitch[s] = true
		return
	}
	if !valid || !patternsOK || hasDefault {
		// Already reported, or total by default: the switch counts as
		// total, so no missing return or totality diagnostic follows (D3).
		c.totalSwitch[s] = true
		return
	}
	switch {
	case !ok:
		c.errorAt(s.Switch, s.Switch+matchLen, CodeNotTotal, "too many cases to prove this switch total; split it or add default (rows.md, Totality 5)")
	case missing:
		parts := make([]string, n)
		for j, idx := range w {
			parts[j] = unions[j].at(idx).String()
		}
		what := parts[0]
		if n > 1 {
			what = "(" + strings.Join(parts, ", ") + ")"
		}
		c.errorAt(s.Switch, s.Switch+matchLen, CodeNotTotal, "this type switch is not total: no case handles %s; name every variant or add default (rows.md, Totality 2)", what)
	}
	// Reported or proved: the switch counts as total for the flow, so no
	// missing-return diagnostic follows from an S103 (D3; round-2 N1).
	c.totalSwitch[s] = true
}

// totalityBudget bounds the steps of each of a switch's two solvers, one for
// its cases' reachability and one for its totality (D16 as amended in rounds 3
// and 5). A step is one residual matrix the solver splits, weighted by its
// number of rows, or one row scanned when a case specializes or joins the
// matrix, so the bound holds all the superlinear work to a fixed amount.
// Deciding totality over several subjects is NP-complete in the worst case
// (a case row can encode a clause, so totality encodes unsatisfiability;
// Maranget 2007, Sekar et al.), so no exact checker is linear on every
// input. A switch whose proof needs more steps is refused (S103, "too many
// cases"), never accepted unproven. The budget is deterministic for a
// given source: steps, not time. Like rustc's limit it depends on the
// order of the subjects, so reordering them may change whether a switch
// near the budget is refused, never a proved verdict. Every switch over at
// most 6 subjects of at most 3 variants stays far below it (at most 3^6 =
// 729 residual matrices; TestTotalityPermutations holds the odd cycles of
// 31 subjects under an eighth of it).
//
// Prior art: rustc's pattern_complexity_limit, GHC's -fmax-pmcheck-models.
const totalityBudget = 200_000

// cell is one constructor of a case row: the variant ctor of subject col.
// A row lists only its constructor cells, sorted by column; a column absent
// from the row is a wildcard there.
type cell struct{ col, ctor int }

type solverRow []cell

// matrixSolver decides usefulness and totality over one switch's case
// matrix (Maranget, "Warnings for pattern matching", 2007), with:
//
//   - column choice by Maranget's necessity heuristic, taken over the
//     shortest rows first: the column needed (a constructor there) by the
//     most of the rows with the fewest constructors. A row with one
//     constructor left is unit, as in DPLL: splitting its column covers or
//     drops it at once, so forced choices are followed first;
//   - memoization of residual matrices under a canonical form that does not
//     depend on column order: columns keep their subject's identity, a row
//     is its set of constructor cells, and the matrix is the sorted set of
//     its rows with its remaining columns, so two orders of splitting that
//     reach the same residual question share one answer;
//   - the cheap shortcuts of OCaml's Parmatch: a row with no constructor
//     left covers everything; equal rows collapse; the case matrix keeps no
//     row a later case subsumes (addRow);
//   - a step budget (totalityBudget).
type matrixSolver struct {
	sizes []int
	memo  map[string]missingResult
	steps int
	over  bool // the budget ran out: nothing more is proved
}

type missingResult struct {
	witness map[int]int // column → variant of a value no row matches
	found   bool
}

func newMatrixSolver(sizes []int) *matrixSolver {
	return &matrixSolver{sizes: sizes, memo: map[string]missingResult{}}
}

// toRows converts full case rows (a variant index or -1 per subject) into
// solver rows.
func toRows(rows [][]int) []solverRow {
	out := make([]solverRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, toRow(r))
	}
	return out
}

func toRow(r []int) solverRow {
	var row solverRow
	for j, p := range r {
		if p >= 0 {
			row = append(row, cell{j, p})
		}
	}
	return row
}

// caseUseful reports whether case q matches a value the earlier cases do
// not. It specializes the matrix by q's constructors, so what remains is a
// totality question over q's wildcard columns, answered with the shared
// memo. ok is false when the budget ran out.
func (s *matrixSolver) caseUseful(rows [][]int, q []int) (useful, ok bool) {
	// Specializing the matrix scans its rows: counted, so the cases'
	// quadratic work stays under the budget (round 5: nothing superlinear
	// runs outside the counters).
	if !s.charge(len(rows)) {
		return false, false
	}
	var residual []solverRow
	for _, r := range rows {
		row, keep := specializeBy(toRow(r), q)
		if keep {
			residual = append(residual, row)
		}
	}
	var cols []int
	for j, p := range q {
		if p < 0 {
			cols = append(cols, j)
		}
	}
	res := s.missing(residual, cols)
	return res.found, !s.over
}

// specializeBy keeps a row that is compatible with q's constructors and
// drops the cells q fixes.
func specializeBy(row solverRow, q []int) (solverRow, bool) {
	var out solverRow
	for _, c := range row {
		switch {
		case q[c.col] < 0:
			out = append(out, c)
		case q[c.col] != c.ctor:
			return nil, false
		}
	}
	return out, true
}

// total decides whether the case rows cover every value of the subjects.
// It returns a witness the rows miss (a variant per subject), and ok false
// when the budget ran out.
func (s *matrixSolver) total(rows [][]int) (witness []int, missing, ok bool) {
	// A subject of no variant (its variants were all reported unknown) has
	// no value, so no value vector exists and none is missing (D3).
	for _, n := range s.sizes {
		if n == 0 {
			return nil, false, true
		}
	}
	cols := make([]int, len(s.sizes))
	for j := range cols {
		cols[j] = j
	}
	res := s.missing(toRows(rows), cols)
	if s.over {
		return nil, false, false
	}
	if !res.found {
		return nil, false, true
	}
	w := make([]int, len(s.sizes))
	for j, v := range res.witness {
		w[j] = v
	}
	return w, true, true
}

// missing finds a value of the columns cols that no row matches.
func (s *matrixSolver) missing(rows []solverRow, cols []int) missingResult {
	for _, r := range rows {
		if len(r) == 0 {
			return missingResult{} // a row of wildcards covers everything
		}
	}
	if len(rows) == 0 {
		return missingResult{witness: map[int]int{}, found: true}
	}
	if s.over {
		return missingResult{}
	}
	rows, key := canonical(rows, cols)
	if r, ok := s.memo[key]; ok {
		return r
	}
	if !s.charge(len(rows)) {
		return missingResult{}
	}
	res := s.split(rows, cols)
	if !s.over {
		s.memo[key] = res
	}
	return res
}

// charge counts work of the given weight against the budget and reports
// whether the budget still holds.
func (s *matrixSolver) charge(w int) bool {
	if s.over {
		return false
	}
	s.steps += w
	if s.steps > totalityBudget {
		s.over = true
		return false
	}
	return true
}

// addRow is addRow with its scan of the matrix counted. Past the budget
// nothing more is proved, so the matrix stops growing.
func (s *matrixSolver) addRow(rows [][]int, row []int) [][]int {
	if !s.charge(len(rows)) {
		return rows
	}
	return addRow(rows, row)
}

// addMaximal is addMaximal with its scan of the set counted.
func (s *matrixSolver) addMaximal(rows [][]int, row []int) [][]int {
	if !s.charge(len(rows)) {
		return rows
	}
	return addMaximal(rows, row)
}

// split chooses a column and decides the matrix by its constructors.
func (s *matrixSolver) split(rows []solverRow, cols []int) missingResult {
	col := chooseColumn(rows)
	rest := make([]int, 0, len(cols)-1)
	for _, c := range cols {
		if c != col {
			rest = append(rest, c)
		}
	}
	present := map[int]bool{}
	for _, r := range rows {
		if v, ok := r.at(col); ok {
			present[v] = true
		}
	}
	if len(present) == s.sizes[col] {
		for ctor := 0; ctor < s.sizes[col]; ctor++ {
			res := s.missing(specializeCol(rows, col, ctor), rest)
			if res.found {
				return withCell(res, col, ctor)
			}
			if s.over {
				return missingResult{}
			}
		}
		return missingResult{}
	}
	// An incomplete column: a value missing from the default matrix,
	// with a variant no row names here, is missing from the whole.
	res := s.missing(defaultCol(rows, col), rest)
	if !res.found {
		return res
	}
	for ctor := 0; ctor < s.sizes[col]; ctor++ {
		if !present[ctor] {
			return withCell(res, col, ctor)
		}
	}
	return missingResult{}
}

func withCell(res missingResult, col, ctor int) missingResult {
	w := make(map[int]int, len(res.witness)+1)
	for k, v := range res.witness {
		w[k] = v
	}
	w[col] = ctor
	return missingResult{witness: w, found: true}
}

func (r solverRow) at(col int) (int, bool) {
	for _, c := range r {
		if c.col == col {
			return c.ctor, true
		}
	}
	return 0, false
}

// chooseColumn applies the necessity heuristic over the shortest rows: of
// the rows with the fewest constructors, the column most of them need; a
// tie goes to the lowest column, so the choice is deterministic.
func chooseColumn(rows []solverRow) int {
	shortest := -1
	for _, r := range rows {
		if shortest < 0 || len(r) < shortest {
			shortest = len(r)
		}
	}
	need := map[int]int{}
	for _, r := range rows {
		if len(r) != shortest {
			continue
		}
		for _, c := range r {
			need[c.col]++
		}
	}
	best, bestN := -1, -1
	for col, n := range need {
		if n > bestN || n == bestN && col < best {
			best, bestN = col, n
		}
	}
	return best
}

func specializeCol(rows []solverRow, col, ctor int) []solverRow {
	var out []solverRow
	for _, r := range rows {
		v, ok := r.at(col)
		switch {
		case !ok:
			out = append(out, r)
		case v == ctor:
			out = append(out, r.without(col))
		}
	}
	return out
}

func defaultCol(rows []solverRow, col int) []solverRow {
	var out []solverRow
	for _, r := range rows {
		if _, ok := r.at(col); !ok {
			out = append(out, r)
		}
	}
	return out
}

func (r solverRow) without(col int) solverRow {
	out := make(solverRow, 0, len(r)-1)
	for _, c := range r {
		if c.col != col {
			out = append(out, c)
		}
	}
	return out
}

// canonical sorts and deduplicates the rows and returns them with a key
// that names the residual question independently of column order: the
// remaining columns as a set and the rows as a set of cell sets.
func canonical(rows []solverRow, cols []int) ([]solverRow, string) {
	keys := make([]string, len(rows))
	byKey := make(map[string]solverRow, len(rows))
	for i, r := range rows {
		var b []byte
		for k, c := range r {
			if k > 0 {
				b = append(b, ',')
			}
			b = strconv.AppendInt(b, int64(c.col), 10)
			b = append(b, ':')
			b = strconv.AppendInt(b, int64(c.ctor), 10)
		}
		keys[i] = string(b)
		byKey[keys[i]] = r
	}
	sort.Strings(keys)
	sortedCols := append([]int(nil), cols...)
	sort.Ints(sortedCols)
	var b strings.Builder
	for i, c := range sortedCols {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(c))
	}
	out := make([]solverRow, 0, len(keys))
	prev := ""
	for i, k := range keys {
		if i > 0 && k == prev {
			continue
		}
		prev = k
		b.WriteByte('|')
		b.WriteString(k)
		out = append(out, byKey[k])
	}
	return out, b.String()
}

// subsumes reports whether every value row b matches also matches row a
// (full rows, -1 for a wildcard).
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

// addMaximal adds row to a set of maximal rows: nothing when a row of the
// set already subsumes it, else the row in place of those it subsumes. The
// result is the maximal rows of everything added, in any order of adding.
func addMaximal(rows [][]int, row []int) [][]int {
	for _, r := range rows {
		if subsumes(r, row) {
			return rows
		}
	}
	return addRow(rows, row)
}
