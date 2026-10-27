package check

import (
	"slices"
	"sort"
)

// canonicalColumns orders the columns of a case matrix by structure, so
// the totality solver's search, its steps and its verdicts do not depend on
// the order the switch writes its subjects in (round-3 M2; D16 as amended:
// a budget invariant under column and row order). It returns perm, where
// perm[k] is the original column at canonical position k.
//
// A column's key is never its index. The first key is the column's
// variant count and the multiset of its cells (how many rows hold each
// variant and how many a wildcard). Then colour refinement, as in the
// Weisfeiler-Leman graph test: a row's colour is the sorted multiset of
// its cells' (column colour, variant) pairs, and a column's next colour is
// its colour with the sorted multiset of (row colour, cell) over the rows,
// until the colours stop splitting. Columns still tied are individualised
// one at a time, the first of the smallest tied class getting a colour of
// its own, and refinement runs again. Columns that refinement cannot tell
// apart are, in every family the tests generate, interchangeable by a
// symmetry of the matrix, so the choice among them changes no step;
// a canonical form for every matrix would be graph canonisation.
//
// Prior art: colour refinement and individualisation-refinement (McKay's
// nauty) for canonical forms; the rows are ranked the same way.
func canonicalColumns(rows [][]int, sizes []int) []int {
	n := len(sizes)
	perm := make([]int, n)
	for j := range perm {
		perm[j] = j
	}
	if n <= 1 {
		return perm
	}
	// colour[j] is column j's colour, a rank among the distinct keys.
	colour := make([]int, n)
	keys := make([][]int, n)
	for j := range n {
		k := make([]int, sizes[j]+2)
		k[0] = sizes[j]
		keys[j] = k
	}
	// cells[i] lists row i's variant cells, so refinement is linear in
	// the cells, not in rows times columns.
	type cell struct{ col, v int }
	cells := make([][]cell, len(rows))
	for i, r := range rows {
		for j, v := range r {
			keys[j][v+2]++ // -1 (wildcard) counts at index 1
			if v >= 0 {
				cells[i] = append(cells[i], cell{j, v})
			}
		}
	}
	classes := rank(keys, colour)
	rowColour := make([]int, len(rows))
	refine := func() {
		for {
			rkeys := make([][]int, len(rows))
			for i := range rows {
				k := make([]int, 0, len(cells[i]))
				for _, c := range cells[i] {
					k = append(k, colour[c.col]*(1<<20)+c.v)
				}
				slices.Sort(k)
				rkeys[i] = k
			}
			rank(rkeys, rowColour)
			// A column lists its variant cells only: the colours of its
			// wildcard rows are all rows' colours less these, so they add
			// nothing, and the work stays linear in the cells.
			ckeys := make([][]int, n)
			for j := range n {
				ckeys[j] = []int{colour[j]}
			}
			for i := range rows {
				for _, c := range cells[i] {
					ckeys[c.col] = append(ckeys[c.col], rowColour[i]*(1<<20)+c.v)
				}
			}
			for j := range n {
				slices.Sort(ckeys[j][1:])
			}
			next := rank(ckeys, colour)
			if next == classes {
				return
			}
			classes = next
		}
	}
	refine()
	for classes < n {
		// Individualise the first column of the smallest tied colour.
		count := map[int]int{}
		for _, col := range colour {
			count[col]++
		}
		tied := -1
		for col, k := range count {
			if k > 1 && (tied < 0 || col < tied) {
				tied = col
			}
		}
		pick := -1
		for j, col := range colour {
			if col == tied {
				pick = j
				break
			}
		}
		keys := make([][]int, n)
		for j := range n {
			keys[j] = []int{colour[j], 0}
		}
		keys[pick][1] = -1
		classes = rank(keys, colour)
		refine()
	}
	sort.SliceStable(perm, func(a, b int) bool { return colour[perm[a]] < colour[perm[b]] })
	return perm
}

// rank writes into out the rank of each key among the distinct keys, in
// lexicographic order, and returns the number of distinct keys.
func rank(keys [][]int, out []int) int {
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return slices.Compare(keys[idx[a]], keys[idx[b]]) < 0 })
	r := -1
	for i, k := range idx {
		if i == 0 || slices.Compare(keys[idx[i-1]], keys[k]) != 0 {
			r++
		}
		out[k] = r
	}
	return r + 1
}
