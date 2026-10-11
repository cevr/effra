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

// sharedPayloadShape is the round-5 review's M1 shape: records R0…R{n-1}, a
// record W with one field of each, and declarations whose state, event and
// output all reach W. With distinct, each declaration has its own state and
// event enums and step, so every declaration walks from new roots into the
// same W; otherwise all declarations share one step and its three roots.
// variantFields adds that many i64 fields beside w in the event's variant.
type sharedPayloadShape struct {
	records, declarations, variantFields int
	distinct                             bool
}

func (s sharedPayloadShape) source() string {
	var b strings.Builder
	b.WriteString("import Machines \"effra/machines\"\n")
	for i := 0; i < s.records; i++ {
		fmt.Fprintf(&b, "record R%d { x: i64 }\n", i)
	}
	b.WriteString("record W {")
	for i := 0; i < s.records; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " f%d: R%d", i, i)
	}
	b.WriteString(" }\n")
	extra := ""
	for i := 0; i < s.variantFields; i++ {
		extra += fmt.Sprintf(", y%d: i64", i)
	}
	steps := 1
	if s.distinct {
		steps = s.declarations
	}
	for i := 0; i < steps; i++ {
		fmt.Fprintf(&b, "enum S%d { A { w: W } B }\nenum E%d { P { w: W%s } Q }\n", i, i, extra)
		fmt.Fprintf(&b, "fn step%d(state: S%d, event: E%d) -> Machines.Step<S%d, W> { Machines.Step<S%d, W>.Ignore {} }\n", i, i, i, i, i)
	}
	for i := 0; i < s.declarations; i++ {
		step := 0
		if s.distinct {
			step = i
		}
		fmt.Fprintf(&b, "machine M%d { initial S%d.B {}; step step%d }\n", i, step, step)
	}
	return b.String()
}

// effectInitialSource is the round-5 review's M2 shape: declarations whose
// initial is an effect raising labels errors. Analysis refuses each (EF140)
// and displays the initial's type, rows included.
func effectInitialSource(labels, declarations int) string {
	var b strings.Builder
	b.WriteString("import Machines \"effra/machines\"\n")
	for i := 0; i < labels; i++ {
		fmt.Fprintf(&b, "error E%d\n", i)
	}
	b.WriteString("enum S { A }\nenum E { P }\neffect fn start() -> S raises {")
	for i := 0; i < labels; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " E%d", i)
	}
	b.WriteString(" } { S.A {} }\n")
	b.WriteString("fn step(state: S, event: E) -> Machines.Step<S, void> { Machines.Step<S, void>.Ignore {} }\n")
	for i := 0; i < declarations; i++ {
		fmt.Fprintf(&b, "machine M%d { initial start(); step step }\n", i)
	}
	return b.String()
}

// prepareModule compiles source and returns its module's preparation: the
// last one, after the imported Machines module's.
func prepareModule(t *testing.T, source string) (*Result, preparationMeasure) {
	t.Helper()
	r, measures := measureMachinePreparation(t, source)
	if len(measures) == 0 {
		t.Fatal("nothing prepared")
	}
	m := measures[len(measures)-1]
	assertMachinePreparationCharged(t, len(source), m)
	return r, m
}

// Round-5 review M1 and M2: work over data the module shares is done once per
// module. Without that, each of 256 declarations walked W's fields again for
// nothing (at 5,000 records, 119.9 MB against a 20.4 MB bound) and copied an
// initial's row again (at 5,000 labels, 21.4 MB against 6.7 MB). Here the
// bound holds, and a declaration's preparation work does not grow with the
// shared data: the units 255 more declarations add are the same over 2,000
// records or labels as over 10. (2,000 records keeps the ordinary checker's
// own cost on W, which is not preparation, to a few seconds.)
func TestMachinePreparationSharesModuleWork(t *testing.T) {
	added := func(source func(size, declarations int) string, size int) int {
		_, one := prepareModule(t, source(size, 1))
		r, many := prepareModule(t, source(size, 256))
		if t.Failed() {
			t.Fatalf("size %d: codes %v", size, machineCodes(r))
		}
		return many.units - one.units
	}
	for _, c := range []struct {
		name   string
		size   int
		source func(size, declarations int) string
	}{
		{"shared roots", 2000, func(n, d int) string { return sharedPayloadShape{records: n, declarations: d}.source() }},
		{"distinct roots", 2000, func(n, d int) string {
			return sharedPayloadShape{records: n, declarations: d, distinct: true}.source()
		}},
		{"effectful initials", 5000, effectInitialSource},
		{"effectful initials", 20000, effectInitialSource},
	} {
		small, large := added(c.source, 10), added(c.source, c.size)
		if small != large {
			t.Errorf("%s: 255 more declarations add %d preparation units over 10 shared types or labels and %d over %d", c.name, small, large, c.size)
		}
	}
}

// The walk's charges, pinned by the units they add. Every number is exact, so
// dropping or changing any of them fails here:
//   - one more record Ri (R10 beside R0…R9) adds 8: its layout (1 + ⌈(3+64)/64⌉
//     = 3), its visit (1), its field in W (2) and its own field x (2). W's
//     layout and display do not depend on its fields, and the event and output
//     walks stop at W, clean, for one unit each.
//   - one more i64 field in the event's variant adds 5: four for the variant
//     field and one for visiting i64 in that walk.
//   - a second declaration on the same step adds 3: one unit for each of its
//     three roots, which are clean.
func TestMachinePreparationWalkChargesArePinned(t *testing.T) {
	units := func(s sharedPayloadShape) int {
		r, m := prepareModule(t, s.source())
		if len(r.Machines) != s.declarations {
			t.Fatalf("%+v: %d plans: %v", s, len(r.Machines), machineCodes(r))
		}
		return m.units
	}
	base := units(sharedPayloadShape{records: 10, declarations: 1})
	for _, c := range []struct {
		name  string
		shape sharedPayloadShape
		want  int
	}{
		{"one more record", sharedPayloadShape{records: 11, declarations: 1}, 8},
		{"one more variant field", sharedPayloadShape{records: 10, declarations: 1, variantFields: 1}, 5},
		{"one more declaration", sharedPayloadShape{records: 10, declarations: 2}, 3},
	} {
		if got := units(c.shape) - base; got != c.want {
			t.Errorf("%s adds %d preparation units, want %d", c.name, got, c.want)
		}
	}
}

// The first effectful initial to display a row pays the row's bytes; the
// labels E0…E4999 are 23,890 bytes, ⌈23,890/64⌉ = 374 units, against 1 for
// E0, E1 and E2 (6 bytes).
func TestMachinePreparationRowChargeIsPinned(t *testing.T) {
	_, small := prepareModule(t, effectInitialSource(3, 1))
	_, large := prepareModule(t, effectInitialSource(5000, 1))
	if got := large.units - small.units; got != 373 {
		t.Errorf("5,000 labels add %d preparation units over 3, want 373", got)
	}
}
