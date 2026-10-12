package machineplan

import (
	"runtime"
	"testing"
)

// The compiler's allocation property test holds each analysis window to these
// bounds per credited unit (machines_alloc_test.go).
const (
	propertyBytesPerUnit = 512
	propertyBaseBytes    = 64 << 10
)

var sink any

func testCheck() *check[struct{}] {
	in := &Input[struct{}]{Module: "module:test"}
	return &check[struct{}]{in: in, machine: &Machine{Name: "M"}, seen: map[reportKey]bool{}, shared: &budget[struct{}]{in: in}}
}

func allocatedBytes(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// Round-3 m1: a refused charge is credited only up to the room it found plus
// one, never with everything it asked for.
func TestRefusedChargeCreditsOnlyItsRoom(t *testing.T) {
	m := testCheck()
	if !m.charge(MaxWork - 10) {
		t.Fatal("a charge within the budget was refused")
	}
	if m.shared.credit != MaxWork-10 {
		t.Fatalf("accepted charge credited %d", m.shared.credit)
	}
	if m.charge(1 << 30) {
		t.Fatal("a charge beyond the budget was accepted")
	}
	// 11 units for the refused charge, then its EF142 diagnostic's slot.
	if want := MaxWork - 10 + 11 + diagnosticUnits; m.shared.credit != want {
		t.Fatalf("refused charge credited %d, want %d", m.shared.credit, want)
	}
	// The module budget bounds the room too.
	n := testCheck()
	n.shared.work = MaxModuleWork - 3
	if n.charge(100) {
		t.Fatal("a charge beyond the module budget was accepted")
	}
	if want := 4 + diagnosticUnits; n.shared.credit != want {
		t.Fatalf("module-refused charge credited %d, want %d", n.shared.credit, want)
	}
}

// What the accounting is for: a builder that allocated before a refused
// charge exceeds the per-unit bound under this credit, while crediting the
// whole refused request would have excused it. A real builder charges first,
// so the property sweep finds no such window (the request's evidence).
func TestAllocateThenRefuseIsDetected(t *testing.T) {
	m := testCheck()
	m.charge(MaxWork - 10)
	const size = 1 << 20
	before := m.shared.credit
	bytes := allocatedBytes(func() {
		sink = make([]byte, size)
		m.charge(TextUnits(size))
	})
	credited := m.shared.credit - before
	if bound := uint64(propertyBytesPerUnit*credited + propertyBaseBytes); bytes <= bound {
		t.Fatalf("allocate-then-refuse of %d bytes stays within %d bytes for %d credited units", bytes, bound, credited)
	}
	if excused := uint64(propertyBytesPerUnit*TextUnits(size) + propertyBaseBytes); bytes > excused {
		t.Fatalf("the full request (%d units) would not have excused %d bytes either; the probe is too large", TextUnits(size), bytes)
	}
}

// Round-3 m2: declarationMapBytes is derived from the swiss-map layout. Each
// map it lists allocates at most its estimate up to the first group in the
// runtime this builds with.
func TestDeclarationMapBytesCoverTheRuntime(t *testing.T) {
	makers := map[string]func(){
		"reports seen":     func() { m := map[reportKey]bool{}; m[reportKey{}] = true; sink = m },
		"transition keys":  func() { m := map[string]bool{}; m["k"] = true; sink = m },
		"helper ordinals":  func() { m := map[string]int{}; m["h"] = 1; sink = m },
		"enum tables":      func() { m := map[*Enum]map[string]*set{}; m[nil] = nil; sink = m },
		"variant indexes":  func() { m := map[string]int{}; m["V"] = 1; sink = m },
		"narrowing tables": func() { m := map[string]*set{}; m[""] = nil; sink = m },
		"refinements":      func() { m := map[refinement]*set{}; m[refinement{}] = nil; sink = m },
		"subsets":          func() { m := map[[2]*set]bool{}; m[[2]*set{}] = true; sink = m },
		"role scopes":      func() { m := map[*Local]*Expr{}; m[nil] = nil; sink = m },
		"payload visits":   func() { m := map[TypeRef]bool{}; m[1] = true; sink = m },
	}
	total := 0
	for _, entry := range declarationMaps {
		build, ok := makers[entry.name]
		if !ok {
			t.Fatalf("no measurement for %s", entry.name)
		}
		const runs = 1000
		runtime.GC()
		measured := allocatedBytes(func() {
			for range runs {
				build()
			}
		}) / runs
		if estimate := uint64(mapBytes(entry.slot)); measured > estimate {
			t.Errorf("%s: a map allocates %d bytes up to its first group; the estimate is %d", entry.name, measured, estimate)
		}
		total += entry.count * mapBytes(entry.slot)
	}
	if total != declarationMapBytes {
		t.Fatalf("declarationMapBytes = %d, the table sums to %d", declarationMapBytes, total)
	}
	t.Logf("declarationMapBytes = %d; DeclarationUnits = %d", declarationMapBytes, DeclarationUnits(PlanBaseBytes))
}
