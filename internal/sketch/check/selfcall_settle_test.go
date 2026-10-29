package check_test

import (
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// TestSelfCallSettleRounds: a call x.f(…) that typing from empty rows does
// not resolve (a receiver whose type depends on a row) is resolved by the
// dependency worklist of settleWaiting, which types each waiting body once
// the bodies it reaches have settled. With the first typing skipped, the
// worklist alone resolves every case to the same verdict, and every
// call the reporting pass resolves was resolved before the bodies were
// ordered (RefWalkProblems).
func TestSelfCallSettleRounds(t *testing.T) {
	defer check.SelfSettleOnly()()
	checkEdgeCases(t, typedEdgeCases)
	for _, tc := range typedEdgeCases {
		if p := check.RefWalkProblems(tc.src); len(p) > 0 {
			t.Errorf("%s: %s", tc.name, strings.Join(p, "; "))
		}
	}
}
