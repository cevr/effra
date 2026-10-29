package check

import (
	"sort"
	"strconv"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
)

// The usefulness solver of match (196p; rows.md, Totality 2 and 5): totality
// and reachability over the case matrix, never by enumerating the values.
//
// Prior art: Maranget's usefulness algorithm, as in OCaml's Parmatch, Rust
// and Gleam's exhaustiveness checker. A pattern is a constructor at a
// column with sub-patterns at the column's child columns: a union's
// variants (their fields are the children), bool's two constants, a
// tuple's or a record's one shape (its elements or fields), and the
// constants of an integer, rune or string type, which never complete
// their column. Specializing a column by a constructor replaces its cell
// by the constructor's sub-patterns, so nested patterns need no other
// rule.

// matchLen is the length of the keyword a totality diagnostic spans: the
// decided parser builds every match from `match` (#196).
const matchLen = syntax.Pos(len("match"))

// totalityBudget bounds the steps of each of a match's two solvers, one for
// its cases' reachability and one for its totality (D16 as amended in rounds 3
// and 5). A step is one residual matrix the solver splits, weighted by its
// number of rows, or one row scanned when a case specializes or joins the
// matrix, so the bound holds all the superlinear work to a fixed amount.
// Deciding totality over several subjects is NP-complete in the worst case
// (a case row can encode a clause, so totality encodes unsatisfiability;
// Maranget 2007, Sekar et al.), so no exact checker is linear on every
// input. A match whose proof needs more steps is refused (S103, "too many
// cases"), never accepted unproven. The budget is deterministic for a
// given source: steps, not time. Like rustc's limit it depends on the
// order of the subjects, so reordering them may change whether a match
// near the budget is refused, never a proved verdict. Every match over at
// most 6 subjects of at most 3 variants stays far below it (at most 3^6 =
// 729 residual matrices; TestTotalityPermutations holds the odd cycles of
// 31 subjects under an eighth of it).
//
// Prior art: rustc's pattern_complexity_limit, GHC's -fmax-pmcheck-models.
const totalityBudget = 200_000

// ctorOther is a witness constructor of a column whose constructors never
// complete it (constants): a value no listed constant equals.
const ctorOther = -1

// cell is one constructor cell of a case row: the pattern pat (an interned
// patNode) at column col. A row lists only its constructor cells, sorted
// by column; a column absent from the row is a wildcard there.
type cell struct{ col, pat int }

type solverRow []cell

// patNode is an interned pattern: its constructor at its column and the
// cells of its sub-patterns at the column's child columns (wildcards
// omitted), sorted by column.
type patNode struct {
	ctor int
	kids solverRow
}

// column is one column of a match's matrices: a subject, or a child of a
// constructor at a parent column (a field of a variant or record, an
// element of a tuple). size is its number of constructors, or -1 when
// they never complete it (constants) or it has none (only wildcards).
type column struct {
	size                int
	parent, ctor, field int
	consts              map[string]int // a constant column's constructors, by exact value
	label               func(w witness, col int) string
	children            map[int][]int // ctor → its child columns, in field order
}

// matrixSolver decides usefulness and totality over one match's case
// matrix (Maranget, "Warnings for pattern matching", 2007), with:
//
//   - column choice by Maranget's necessity heuristic, taken over the
//     shortest rows first: the column needed (a constructor there) by the
//     most of the rows with the fewest constructors. A row with one
//     constructor left is unit, as in DPLL: splitting its column covers or
//     drops it at once, so forced choices are followed first;
//   - memoization of residual matrices under a canonical form that does not
//     depend on column order: columns keep their identity, a row is its set
//     of constructor cells, and the matrix is the sorted set of its rows
//     with its remaining columns, so two orders of splitting that reach the
//     same residual question share one answer;
//   - the cheap shortcuts of OCaml's Parmatch: a row with no constructor
//     left covers everything; equal rows collapse; the case matrix keeps no
//     row a later case subsumes (rowSet.addRow);
//   - one pass that splits a complete column into every constructor's
//     residual (so a union of n variants matched by n arms costs O(n));
//   - a step budget (totalityBudget).
type matrixSolver struct {
	*patSpace
	memo  map[string]missingResult
	steps int
	over  bool // the budget ran out: nothing more is proved
}

// patSpace holds a match's columns and interned patterns, shared by its
// two solvers (reachability and totality), which keep their own memo and
// step count.
type patSpace struct {
	cols   []column
	child  map[[3]int]int // (parent column, constructor, field) → child column
	pats   []patNode
	patKey map[string]int
}

func newPatSpace() *patSpace {
	return &patSpace{child: map[[3]int]int{}, patKey: map[string]int{}}
}

// solver returns a fresh solver over the space.
func (sp *patSpace) solver() *matrixSolver {
	return &matrixSolver{patSpace: sp, memo: map[string]missingResult{}}
}

type missingResult struct {
	witness witness // column → constructor of a value no row matches
	found   bool
}

// witness maps a column to the constructor of a missing value there.
type witness map[int]int

// newMatrixSolver returns a solver over flat subject columns of the given
// sizes (the tests' constructor rows).
func newMatrixSolver(sizes []int) *matrixSolver {
	sp := newPatSpace()
	for _, n := range sizes {
		sp.addColumn(column{size: n})
	}
	return sp.solver()
}

// subjects lists the subject columns.
func (s *patSpace) subjects() []int {
	var out []int
	for j, col := range s.cols {
		if col.parent < 0 {
			out = append(out, j)
		}
	}
	return out
}

// addColumn adds a subject column of size n and returns it.
func (s *patSpace) addColumn(col column) int {
	col.parent = -1
	s.cols = append(s.cols, col)
	return len(s.cols) - 1
}

// childColumn returns the child column of constructor ctor at parent for
// its field (or element) field, made on first use by mk.
func (s *patSpace) childColumn(parent, ctor, field int, mk func() column) int {
	k := [3]int{parent, ctor, field}
	if j, ok := s.child[k]; ok {
		return j
	}
	col := mk()
	col.parent, col.ctor, col.field = parent, ctor, field
	s.cols = append(s.cols, col)
	j := len(s.cols) - 1
	s.child[k] = j
	p := &s.cols[parent]
	if p.children == nil {
		p.children = map[int][]int{}
	}
	kids := append(p.children[ctor], j)
	sort.Slice(kids, func(a, b int) bool { return s.cols[kids[a]].field < s.cols[kids[b]].field })
	p.children[ctor] = kids
	return j
}

// node interns the pattern ctor(kids); kids are sorted by column.
func (s *patSpace) node(ctor int, kids solverRow) int {
	var b []byte
	b = strconv.AppendInt(b, int64(ctor), 10)
	if len(kids) > 0 {
		b = append(b, '(')
		for i, k := range kids {
			if i > 0 {
				b = append(b, ',')
			}
			b = strconv.AppendInt(b, int64(k.col), 10)
			b = append(b, ':')
			b = strconv.AppendInt(b, int64(k.pat), 10)
		}
		b = append(b, ')')
	}
	key := string(b)
	if id, ok := s.patKey[key]; ok {
		return id
	}
	s.pats = append(s.pats, patNode{ctor: ctor, kids: kids})
	s.patKey[key] = len(s.pats) - 1
	return len(s.pats) - 1
}

// constCtor returns the constructor of a constant at a constant column.
func (s *patSpace) constCtor(col int, exact string) int {
	c := &s.cols[col]
	if c.consts == nil {
		c.consts = map[string]int{}
	}
	if k, ok := c.consts[exact]; ok {
		return k
	}
	k := len(c.consts)
	c.consts[exact] = k
	return k
}

// row converts a full row of constructors (a constructor or -1 for a
// wildcard per subject column) into a solver row of patterns with no
// sub-patterns.
func (s *patSpace) row(r []int) solverRow {
	var out solverRow
	for j, p := range r {
		if p >= 0 {
			out = append(out, cell{j, s.node(p, nil)})
		}
	}
	return out
}

func (r solverRow) at(col int) (int, bool) {
	for _, c := range r {
		if c.col == col {
			return c.pat, true
		}
	}
	return 0, false
}

// replace returns r with its cell at col replaced by kids.
func (r solverRow) replace(col int, kids solverRow) solverRow {
	out := make(solverRow, 0, len(r)-1+len(kids))
	for _, c := range r {
		if c.col != col {
			out = append(out, c)
		}
	}
	if len(kids) == 0 {
		return out
	}
	out = append(out, kids...)
	sort.Slice(out, func(a, b int) bool { return out[a].col < out[b].col })
	return out
}

// subsumesPat reports whether every value pattern b matches, a matches.
func (s *patSpace) subsumesPat(a, b int) bool {
	if a == b {
		return true
	}
	pa, pb := s.pats[a], s.pats[b]
	if pa.ctor != pb.ctor {
		return false
	}
	return s.subsumes(pa.kids, pb.kids)
}

// subsumes reports whether every value row b matches, row a matches: each
// cell of a has a cell of b at its column that it subsumes.
func (s *patSpace) subsumes(a, b solverRow) bool {
	for _, ca := range a {
		pb, ok := b.at(ca.col)
		if !ok || !s.subsumesPat(ca.pat, pb) {
			return false
		}
	}
	return true
}

// compatible reports whether r may match a value q matches: at each
// column of q, r is a wildcard or has q's constructor.
func (s *patSpace) compatible(r, q solverRow) bool {
	for _, cq := range q {
		if p, ok := r.at(cq.col); ok && s.pats[p].ctor != s.pats[cq.pat].ctor {
			return false
		}
	}
	return true
}

// rowSet is a case matrix with its rows indexed by their constructor cells,
// so a case finds the rows it may meet without scanning the others: a
// union of n variants matched by n arms costs O(n), not O(n²). Every scan
// of a candidate is charged to the solver.
type rowSet struct {
	s       *matrixSolver
	rows    []solverRow
	dead    []bool // dropped: a later row subsumed it
	byCell  map[[2]int][]int
	byFirst map[[2]int][]int
	lacking map[int][]int // subject column → rows with no cell there
	empty   []int         // rows with no cell: they cover everything
	nsubj   []int
}

func newRowSet(s *matrixSolver) *rowSet {
	return &rowSet{s: s, byCell: map[[2]int][]int{}, byFirst: map[[2]int][]int{}, lacking: map[int][]int{}, nsubj: s.subjects()}
}

func (m *rowSet) key(c cell) [2]int { return [2]int{c.col, m.s.pats[c.pat].ctor} }

// alive filters an index list in place, dropping the rows a later row
// subsumed, and charges the scan; false when the budget ran out.
func (m *rowSet) alive(list *[]int) ([]int, bool) {
	if !m.s.charge(len(*list)) {
		return nil, false
	}
	out := (*list)[:0]
	for _, i := range *list {
		if !m.dead[i] {
			out = append(out, i)
		}
	}
	*list = out
	return out, true
}

// all returns the rows in source order.
func (m *rowSet) all() []solverRow {
	var out []solverRow
	for i, r := range m.rows {
		if !m.dead[i] {
			out = append(out, r)
		}
	}
	return out
}

func (m *rowSet) add(r solverRow) {
	i := len(m.rows)
	m.rows = append(m.rows, r)
	m.dead = append(m.dead, false)
	if len(r) == 0 {
		m.empty = append(m.empty, i)
	} else {
		k := m.key(r[0])
		m.byFirst[k] = append(m.byFirst[k], i)
	}
	for _, c := range r {
		k := m.key(c)
		m.byCell[k] = append(m.byCell[k], i)
	}
	for _, j := range m.nsubj {
		if _, ok := r.at(j); !ok {
			m.lacking[j] = append(m.lacking[j], i)
		}
	}
}

// compatibleRows returns the rows that may meet q (s.compatible), in
// source order; false when the budget ran out.
func (m *rowSet) compatibleRows(q solverRow) ([]solverRow, bool) {
	if len(q) == 0 {
		rows := m.all()
		return rows, m.s.charge(len(rows))
	}
	// The cell of q with the fewest candidates: the rows with its
	// constructor and the rows with no cell at its column.
	best, bestN := -1, -1
	for i, c := range q {
		n := len(m.byCell[m.key(c)]) + len(m.lacking[c.col])
		if best < 0 || n < bestN {
			best, bestN = i, n
		}
	}
	bc := m.key(q[best])
	l0 := m.byCell[bc]
	with, ok := m.alive(&l0)
	m.byCell[bc] = with
	if !ok {
		return nil, false
	}
	l := m.lacking[q[best].col]
	without, ok := m.alive(&l)
	if !ok {
		return nil, false
	}
	m.lacking[q[best].col] = without
	idx := mergeSorted(with, without)
	var out []solverRow
	for _, i := range idx {
		if r := m.rows[i]; m.s.compatible(r, q) {
			out = append(out, r)
		}
	}
	return out, true
}

func mergeSorted(a, b []int) []int {
	out := make([]int, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j == len(b) || i < len(a) && a[i] < b[j]:
			out = append(out, a[i])
			i++
		default:
			out = append(out, b[j])
			j++
		}
	}
	return out
}

// addRow appends a useful case row and drops the rows it subsumes, so the
// matrix never holds a subsumed row. Dropping one changes neither
// usefulness nor totality: every value it matches, the subsuming row
// matches too. Past the budget nothing more is proved, so the matrix
// stops growing.
func (m *rowSet) addRow(q solverRow) {
	if len(q) == 0 {
		if !m.s.charge(len(m.rows)) {
			return
		}
		for i := range m.rows {
			m.dead[i] = true
		}
	} else {
		k := m.key(q[0])
		l := m.byCell[k]
		cand, ok := m.alive(&l)
		m.byCell[k] = cand
		if !ok {
			return
		}
		for _, i := range cand {
			if m.s.subsumes(q, m.rows[i]) {
				m.dead[i] = true
			}
		}
	}
	m.add(q)
}

// addMaximal adds q to a set of maximal rows: nothing when a row of the
// set already subsumes it, else q in place of the rows it subsumes. The
// result is the maximal rows of everything added, in any order of adding.
func (m *rowSet) addMaximal(q solverRow) {
	// A row that subsumes q has each of its cells in q: its first cell is
	// one of q's, or it has none.
	e, ok := m.alive(&m.empty)
	if !ok {
		return
	}
	if len(e) > 0 {
		return
	}
	for _, c := range q {
		k := m.key(c)
		l := m.byFirst[k]
		cand, ok := m.alive(&l)
		m.byFirst[k] = cand
		if !ok {
			return
		}
		for _, i := range cand {
			if m.s.subsumes(m.rows[i], q) {
				return
			}
		}
	}
	m.addRow(q)
}

// caseUseful reports whether case q matches a value the rows of m do not.
// It specializes the rows q may meet by q's constructors, so what remains
// is a usefulness question over q's sub-patterns, and at the end a
// totality question over q's wildcard columns, answered with the shared
// memo. ok is false when the budget ran out.
func (s *matrixSolver) caseUseful(m *rowSet, q solverRow) (useful, ok bool) {
	rows, ok := m.compatibleRows(q)
	if !ok {
		return false, false
	}
	res := s.useful(rows, q, s.subjects(), true)
	return res, !s.over
}

// useful is U(rows, q) over the columns cols: rows are compatible with q
// when known is set.
func (s *matrixSolver) useful(rows []solverRow, q solverRow, cols []int, known bool) bool {
	for len(q) > 0 {
		if s.over {
			return false
		}
		if !known {
			if !s.charge(len(rows)) {
				return false
			}
			kept := rows[:0:0]
			for _, r := range rows {
				if s.compatible(r, q) {
					kept = append(kept, r)
				}
			}
			rows = kept
		}
		known = false
		// Specialize the rows and q by q's constructors.
		var next solverRow
		fixed := map[int]bool{}
		for _, cq := range q {
			fixed[cq.col] = true
			next = append(next, s.pats[cq.pat].kids...)
		}
		residual := make([]solverRow, 0, len(rows))
		for _, r := range rows {
			out := make(solverRow, 0, len(r))
			for _, c := range r {
				if fixed[c.col] {
					out = append(out, s.pats[c.pat].kids...)
				} else {
					out = append(out, c)
				}
			}
			sort.Slice(out, func(a, b int) bool { return out[a].col < out[b].col })
			residual = append(residual, out)
		}
		var rest []int
		for _, c := range cols {
			if !fixed[c] {
				rest = append(rest, c)
			}
		}
		for _, cq := range q {
			rest = append(rest, s.cols[cq.col].children[s.pats[cq.pat].ctor]...)
		}
		sort.Slice(next, func(a, b int) bool { return next[a].col < next[b].col })
		rows, q, cols = residual, next, rest
	}
	return s.missing(rows, cols).found
}

// total decides whether the case rows cover every value of the subjects.
// It returns a witness the rows miss, and ok false when the budget ran
// out.
func (s *matrixSolver) total(rows []solverRow) (w witness, missing, ok bool) {
	cols := s.subjects()
	// A subject of no variant (its variants were all reported unknown) has
	// no value, so no value vector exists and none is missing (D3).
	for _, j := range cols {
		if s.cols[j].size == 0 {
			return nil, false, true
		}
	}
	res := s.missing(rows, cols)
	if s.over {
		return nil, false, false
	}
	if !res.found {
		return nil, false, true
	}
	return res.witness, true, true
}

// missing finds a value of the columns cols that no row matches.
func (s *matrixSolver) missing(rows []solverRow, cols []int) missingResult {
	for _, r := range rows {
		if len(r) == 0 {
			return missingResult{} // a row of wildcards covers everything
		}
	}
	if len(rows) == 0 {
		return missingResult{witness: witness{}, found: true}
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
		if p, ok := r.at(col); ok {
			present[s.pats[p].ctor] = true
		}
	}
	size := s.cols[col].size
	if size >= 0 && len(present) == size {
		// Every constructor appears: one pass puts each row in the
		// residual of its constructor, a wildcard row in every one.
		buckets := make([][]solverRow, size)
		for _, r := range rows {
			if p, ok := r.at(col); ok {
				k := s.pats[p].ctor
				buckets[k] = append(buckets[k], r.replace(col, s.pats[p].kids))
				continue
			}
			for k := range buckets {
				buckets[k] = append(buckets[k], r)
			}
		}
		for ctor := 0; ctor < size; ctor++ {
			sub := rest
			if kids := s.cols[col].children[ctor]; len(kids) > 0 {
				sub = append(append([]int(nil), rest...), kids...)
			}
			res := s.missing(buckets[ctor], sub)
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
	// with a constructor no row names here, is missing from the whole.
	res := s.missing(defaultCol(rows, col), rest)
	if !res.found {
		return res
	}
	if size < 0 {
		return withCell(res, col, ctorOther)
	}
	for ctor := 0; ctor < size; ctor++ {
		if !present[ctor] {
			return withCell(res, col, ctor)
		}
	}
	return missingResult{}
}

func withCell(res missingResult, col, ctor int) missingResult {
	w := make(witness, len(res.witness)+1)
	for k, v := range res.witness {
		w[k] = v
	}
	w[col] = ctor
	return missingResult{witness: w, found: true}
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

func defaultCol(rows []solverRow, col int) []solverRow {
	var out []solverRow
	for _, r := range rows {
		if _, ok := r.at(col); !ok {
			out = append(out, r)
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
			b = strconv.AppendInt(b, int64(c.pat), 10)
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

// witnessText prints the value a witness names at column col: a variant
// with the fields the witness constrains, a tuple, a constant, or `_`.
func (s *patSpace) witnessText(w witness, col int) string {
	if s.cols[col].label != nil {
		return s.cols[col].label(w, col)
	}
	return "_"
}
