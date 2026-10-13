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
	key := m.transitionKey(Span{Offset: 1 << 20, Length: 9}, DecisionEnter, TargetLiteral, true, wide, &set{id: 2}, nil, []string{long, long + "x", long})
	if len(key) > 64 {
		t.Fatalf("key for three 1 MiB helper names is %d bytes", len(key))
	}
}

// The checker projects only published values, and analysis' payload walk
// refuses (EF141) whatever would be unpublished first, so no program reaches
// this guard; it is defence in depth. A projection the checker marked
// unpublished is refused (EF142) with no value.
func TestUnpublishedProjectionIsRefused(t *testing.T) {
	in := &Input[int]{Module: "m"}
	fresh := func() *check[int] {
		return &check[int]{in: in, machine: &Machine{Name: "M"}, seen: map[reportKey]bool{}, shared: &budget[int]{in: in}}
	}
	m := fresh()
	if value, ok := m.project(Projection[int]{Published: false, Value: 7}, true); ok || value != 0 {
		t.Fatalf("unpublished projection = %d %t", value, ok)
	}
	if !m.refused || len(m.shared.diagnostics) != 1 || m.shared.diagnostics[0].Code != BudgetCode {
		t.Fatalf("refused %t, diagnostics %v", m.refused, m.shared.diagnostics)
	}
	if value, ok := fresh().project(Projection[int]{Published: true, Value: 7}, true); !ok || value != 7 {
		t.Fatalf("published projection = %d %t", value, ok)
	}
}
