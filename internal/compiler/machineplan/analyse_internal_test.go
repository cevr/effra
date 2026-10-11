package machineplan

import (
	"strings"
	"testing"
)

// Transition keys are narrowing and helper ordinals: their size never follows
// the length of an identifier or the width of a narrowing.
func TestTransitionKeysIgnoreIdentifierLength(t *testing.T) {
	m := &check[struct{}]{role: BindStep}
	long := strings.Repeat("h", 1<<20)
	wide := &set{id: 7, positions: make([]int32, 1<<16)}
	key := m.transitionKey(Span{Offset: 1 << 20, Length: 9}, DecisionGo, TargetLiteral, true, wide, &set{id: 2}, nil, []string{long, long + "x", long})
	if len(key) > 64 {
		t.Fatalf("key for three 1 MiB helper names is %d bytes", len(key))
	}
}
