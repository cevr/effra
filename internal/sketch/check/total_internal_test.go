package check

import (
	"math/rand"
	"testing"
)

// TestMatrixSolverAgreesWithEnumeration cross-checks the pruned,
// memoized solver (M8) against brute-force enumeration of every value
// vector, over random small case matrices: pruning and memoization must
// change no answer.
func TestMatrixSolverAgreesWithEnumeration(t *testing.T) {
	rng := rand.New(rand.NewSource(196))
	for trial := 0; trial < 20000; trial++ {
		cols := 1 + rng.Intn(4)
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
		s := newMatrixSolver()
		var matrix, all [][]int
		for k := rng.Intn(7); k > 0; k-- {
			q := randRow()
			want := enumUseful(all, q, sizes)
			if got := s.caseUseful(matrix, q, sizes); got != want {
				t.Fatalf("trial %d: useful(%v, %v, sizes %v) = %v, enumeration says %v", trial, all, q, sizes, got, want)
			}
			if want {
				matrix = addRow(matrix, q)
				all = append(all, q)
			}
		}
		w, ok := s.missing(matrix, sizes)
		wantOK := enumUseful(all, wild(cols), sizes)
		if ok != wantOK {
			t.Fatalf("trial %d: missing(%v, sizes %v) ok = %v, enumeration says %v", trial, all, sizes, ok, wantOK)
		}
		if ok && matchesAny(all, w) {
			t.Fatalf("trial %d: witness %v is matched by %v", trial, w, all)
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

func matchesAny(rows [][]int, v []int) bool {
	for _, r := range rows {
		if subsumes(r, v) {
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
			return subsumes(q, v) && !matchesAny(rows, v)
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
