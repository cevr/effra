package compiler

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// Preparation is checker work outside the analysis window, and the checker has
// no allocation budget of its own: what bounds it is the mirrored analysis
// budget. Every layout and display it builds is charged by the analysis cost
// model first, and the rest of what it does (lowering the declarations and the
// bodies the walk reads, the type table, projections of published values) is
// linear in the source. So each module's preparation must allocate at most
// preparationBytesPerUnit per unit its mirrored charges accepted, plus
// preparationBytesPerSourceByte per source byte, plus a fixed allowance. The
// accepted units never exceed the module's analysis budget, so the bound is
// linear in the source. Every measured compile checks it
// (measureMachineAnalysisAfter), the seed sweep included.
const (
	preparationBytesPerUnit       = 512
	preparationBytesPerSourceByte = 64
	preparationBaseBytes          = 64 << 10
)

type preparationMeasure struct {
	bytes uint64
	units int
}

func (m preparationMeasure) bound(sourceBytes int) uint64 {
	return uint64(preparationBytesPerUnit*m.units + preparationBytesPerSourceByte*sourceBytes + preparationBaseBytes)
}

func assertMachinePreparationCharged(t *testing.T, sourceBytes int, m preparationMeasure) {
	t.Helper()
	if bound := m.bound(sourceBytes); m.bytes > bound {
		t.Errorf("preparation allocated %d bytes for %d accepted units and %d source bytes (bound %d: %d bytes/unit, %d bytes/source byte)", m.bytes, m.units, sourceBytes, bound, preparationBytesPerUnit, preparationBytesPerSourceByte)
	}
}

// measureMachinePreparation compiles source and returns what each module's
// preparation allocated and the units its charges accepted.
func measureMachinePreparation(t *testing.T, source string) (*Result, []preparationMeasure) {
	t.Helper()
	var before runtime.MemStats
	var measures []preparationMeasure
	machinePreparationProbe = func(done bool, units int) {
		if !done {
			runtime.ReadMemStats(&before)
			return
		}
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		measures = append(measures, preparationMeasure{bytes: after.TotalAlloc - before.TotalAlloc, units: units})
	}
	defer func() { machinePreparationProbe = nil }()
	return Compile(source), measures
}

// machineLayoutTextSource is the round-4 review's N2 shape: k distinct
// instantiations of Outer<T>, whose F declared fields are Inner<T> with an
// N-byte name. Instantiating Outer<Rk> rebuilds F interning keys that carry
// the name, so preparing all of them would build k·F·N bytes from a source of
// about F·N bytes.
func machineLayoutTextSource(k, fields, nameBytes int) string {
	var b strings.Builder
	inner := "I" + strings.Repeat("n", nameBytes)
	b.WriteString("import Machines \"effra/machines\"\n")
	fmt.Fprintf(&b, "record %s<T: type> { value: T }\nrecord Outer<T: type> {", inner)
	for i := 0; i < fields; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " a%d: %s<T>", i, inner)
	}
	b.WriteString(" }\n")
	for i := 0; i < k; i++ {
		fmt.Fprintf(&b, "record R%d { x: i64 }\n", i)
	}
	b.WriteString("record Payload {")
	for i := 0; i < k; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " o%d: Outer<R%d>", i, i)
	}
	b.WriteString(" }\nenum S { A }\nenum E { P }\n")
	b.WriteString("fn step(state: S, event: E) -> Machines.Step<S, Payload> { Machines.Step<S, Payload>.Ignore {} }\nmachine M { initial S.A {}; step step }\n")
	return b.String()
}

// Round-4 review M1: the layout's key-text charge is what keeps preparation
// from rebuilding k·F·N bytes of interning keys. Without it, k=50, F=16,
// N=65,536 prepared all 50 instantiations (about 234 times the source).
func TestMachinePreparationAllocationIsCharged(t *testing.T) {
	for _, c := range []struct{ k, fields, name int }{
		{50, 16, 1 << 16},
		{200, 64, 1 << 14},
		{50, 1, 1 << 16},
		{50, 4, 1 << 14},
	} {
		source := machineLayoutTextSource(c.k, c.fields, c.name)
		r, measures := measureMachinePreparation(t, source)
		// The imported Machines module is prepared too, with nothing to prepare.
		if len(measures) == 0 {
			t.Fatalf("k=%d F=%d N=%d: nothing prepared", c.k, c.fields, c.name)
		}
		for _, m := range measures {
			if bound := m.bound(len(source)); m.bytes > bound {
				t.Errorf("k=%d F=%d N=%d: preparation allocated %d bytes for %d accepted units and %d source bytes (bound %d)", c.k, c.fields, c.name, m.bytes, m.units, len(source), bound)
			}
		}
		if codes := machineCodes(r); len(codes) != 1 || codes[0] != machineBudgetCode {
			t.Errorf("k=%d F=%d N=%d: codes = %v", c.k, c.fields, c.name, codes)
		}
	}
}
