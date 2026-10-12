// Package shared holds the read-only view machine analysis has of a
// checker-interned row.
//
// The checker interns each row once and never writes its labels again, so the
// analysis input shares them instead of copying a row per type and function
// that carries it (a copy per distinct row is not bounded by the source). A
// Row keeps the labels in an unexported field of this package, and nothing
// here returns that slice or writes it. Code outside the package can read a
// row and copy it; the Go compiler refuses a slice, conversion, index
// assignment or append on a Row. Only reflect or unsafe could reach the field,
// and machineplan's boundary test keeps both out of machineplan and of this
// package.
package shared

import (
	"iter"
	"strings"
)

// Row is a row's labels, sorted and distinct, shared read-only.
type Row struct {
	labels []string
}

// Of shares labels as a Row. The caller must not write labels afterwards;
// the checker passes its interned rows, which it writes only when it interns
// them.
func Of(labels []string) Row { return Row{labels: labels} }

// Len is the number of labels.
func (r Row) Len() int { return len(r.labels) }

// At is the i-th label.
func (r Row) At(i int) string { return r.labels[i] }

// All yields the labels in order.
func (r Row) All() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, label := range r.labels {
			if !yield(label) {
				return
			}
		}
	}
}

// Bytes is the labels' total length.
func (r Row) Bytes() int {
	n := 0
	for _, label := range r.labels {
		n += len(label)
	}
	return n
}

// Join is the joined string: the labels joined by separator. For a single
// label it is that label's own, immutable, string.
func (r Row) Join(separator string) string { return strings.Join(r.labels, separator) }

// AppendTo appends a copy of the labels to dst.
func (r Row) AppendTo(dst []string) []string { return append(dst, r.labels...) }
