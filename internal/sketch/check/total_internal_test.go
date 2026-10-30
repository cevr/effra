package check

import (
	"math/rand"
	"testing"
)

// TestMatrixSolverAgreesWithEnumeration cross-checks the solver (column
// choice, canonical memo, pruning, the indexed case matrix) against
// brute-force enumeration of every value vector, over random small case
// matrices (up to 6 subjects of up to 3 variants): no answer changes, and
// no small case reaches the budget.
func TestMatrixSolverAgreesWithEnumeration(t *testing.T) {
	rng := rand.New(rand.NewSource(196))
	for trial := 0; trial < 20000; trial++ {
		cols := 1 + rng.Intn(6)
		sizes := make([]int, cols)
		for j := range sizes {
			sizes[j] = 1 + rng.Intn(3)
		}
		randRow := func() []int {
			r := make([]int, cols)
			for j := range r {
				if rng.Intn(3) == 0 {
					r[j] = -1
				} else {
					r[j] = rng.Intn(sizes[j])
				}
			}
			return r
		}
		s := newMatrixSolver(sizes)
		matrix := newRowSet(s)
		var all [][]int
		for k := rng.Intn(9); k > 0; k-- {
			q := randRow()
			want := enumUseful(all, q, sizes)
			got, ok := s.caseUseful(matrix, s.row(q))
			if !ok {
				t.Fatalf("trial %d: a small case reached the budget", trial)
			}
			if got != want {
				t.Fatalf("trial %d: useful(%v, %v, sizes %v) = %v, enumeration says %v", trial, all, q, sizes, got, want)
			}
			if want {
				matrix.addRow(s.row(q))
				all = append(all, q)
			}
		}
		total := s.patSpace.solver()
		set := newRowSet(total)
		for _, r := range all {
			set.addMaximal(s.row(r))
		}
		w, ok, proved := total.total(set.all())
		if !proved {
			t.Fatalf("trial %d: a small switch reached the budget", trial)
		}
		wantOK := enumUseful(all, wild(cols), sizes)
		if ok != wantOK {
			t.Fatalf("trial %d: missing(%v, sizes %v) ok = %v, enumeration says %v", trial, all, sizes, ok, wantOK)
		}
		if ok {
			v := make([]int, cols)
			for j := range v {
				v[j] = w[j] // a column absent from the witness takes any value: 0
			}
			if matchesAny(all, v) {
				t.Fatalf("trial %d: witness %v is matched by %v", trial, v, all)
			}
		}
	}
}

func wild(n int) []int {
	r := make([]int, n)
	for i := range r {
		r[i] = -1
	}
	return r
}

// covers reports whether full row a matches every value full row b does.
func covers(a, b []int) bool {
	for i := range a {
		if a[i] >= 0 && a[i] != b[i] {
			return false
		}
	}
	return true
}

func matchesAny(rows [][]int, v []int) bool {
	for _, r := range rows {
		if covers(r, v) {
			return true
		}
	}
	return false
}

// enumUseful reports whether some value vector matches q and no row.
func enumUseful(rows [][]int, q []int, sizes []int) bool {
	v := make([]int, len(sizes))
	var walk func(j int) bool
	walk = func(j int) bool {
		if j == len(sizes) {
			return covers(q, v) && !matchesAny(rows, v)
		}
		for c := 0; c < sizes[j]; c++ {
			v[j] = c
			if walk(j + 1) {
				return true
			}
		}
		return false
	}
	return walk(0)
}
