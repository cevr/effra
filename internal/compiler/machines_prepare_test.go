package compiler

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Preparation is checker work outside the analysis window, and the checker has
// no allocation budget of its own: what bounds it is the mirrored analysis
// budget. Every layout and display it builds is charged by the analysis cost
// model first, and the rest of what it does (lowering the declarations and the
// bodies the walk reads, the type table, projections of published values) is
// linear in the source. The type table is linear because it shares the
// checker's interned rows instead of copying them: rows are not linear in the
// source (TestMachinePreparationSharesInternedRows). So each module's
// preparation must allocate at most
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
	b.WriteString("fn step(state: S, event: E) -> decision<S, Payload> { ignore }\nmachine M { initial S.A {}; step step }\n")
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
		fmt.Fprintf(&b, "fn step%d(state: S%d, event: E%d) -> decision<S%d, W> { ignore }\n", i, i, i, i)
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
	b.WriteString("fn step(state: S, event: E) -> decision<S, void> { ignore }\n")
	for i := 0; i < declarations; i++ {
		fmt.Fprintf(&b, "machine M%d { initial start(); step step }\n", i)
	}
	return b.String()
}

// prepareModule compiles source and returns its module's preparation: the
// last one.
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

// catchChainSource is the round-6 review's m1 shape: all() raises n errors and
// start()'s body catches them one at a time, so the chain's n expressions
// carry n distinct rows of n, n-1, …, 1 labels, n²/2 labels from O(n) source
// bytes. The initial calls start(), so its body is lowered and its types
// prepared; analysis then refuses the effectful initial (EF140).
func catchChainSource(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "error G%d\n", i)
	}
	b.WriteString("enum S { A }\nenum E { P }\neffect fn all() -> i64 raises {")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " G%d", i)
	}
	b.WriteString(" } { 1 }\neffect fn start() -> S {\n    let x = run all()")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, ".catch<G%d>(0)", i)
	}
	b.WriteString("\n    S.A {}\n}\n")
	b.WriteString("fn step(state: S, event: E) -> decision<S, void> { ignore }\n")
	b.WriteString("machine M { initial start(); step step }\n")
	return b.String()
}

// Round-6 review m1: the input shares the checker's interned rows. A copy per
// distinct row is quadratic in this source (at 500 errors, 2.6 MB against a
// 1.1 MB bound, and 16× the bound at 4,000), while sharing keeps preparation
// within its linear bound. The checker's own cost on the chain is quadratic
// too, and is not preparation's.
func TestMachinePreparationSharesInternedRows(t *testing.T) {
	r, _ := prepareModule(t, catchChainSource(500))
	if codes := machineCodes(r); len(codes) != 1 || codes[0] != "EF140" {
		t.Errorf("codes %v, want [EF140]", codes)
	}
}

// wideRecordSource declares, for each name, a record of records records
// whose own fields are fields i64s each.
func wideRecordSource(b *strings.Builder, name string, records, fields int) {
	for i := 0; i < records; i++ {
		fmt.Fprintf(b, "record %sq%d {", name, i)
		for j := 0; j < fields; j++ {
			if j > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(b, " x%d: i64", j)
		}
		b.WriteString(" }\n")
	}
	fmt.Fprintf(b, "record %s {", name)
	for i := 0; i < records; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(b, " f%d: %sq%d", i, name, i)
	}
	b.WriteString(" }\n")
}

// machinesOver declares one machine per state, each with its own state and
// event enums; state is the fields of the state's first variant.
func machinesOver(b *strings.Builder, states ...string) {
	for i, state := range states {
		fmt.Fprintf(b, "enum S%d { A { %s } B }\nenum E%d { P Q }\n", i, state, i)
		fmt.Fprintf(b, "fn step%d(state: S%d, event: E%d) -> decision<S%d, void> { ignore }\n", i, i, i, i)
		fmt.Fprintf(b, "machine M%d { initial S%d.B {}; step step%d }\n", i, i, i)
	}
}

// diagnosticSummary is each diagnostic's code and message, in order.
func diagnosticSummary(r *Result) []string {
	out := make([]string, len(r.Diagnostics))
	for i, d := range r.Diagnostics {
		out[i] = d.Code + " " + d.Message
	}
	return out
}

// Round-6 review m2: the clean set's two soundness guards, each witnessed by
// the plan or refusal it protects. Only two declarations each: all of them
// are prepared before any is analysed, so a third that walked the same types
// would prepare what the first skipped and hide the fault.
//
//   - A walk the budget stops marks nothing clean. M0's state carries P0 and
//     P1, about 40,000 units each, so its walk stops inside P1; M1 carries P1
//     alone and is admitted. Marking P1 clean when the walk stopped would
//     stop M1's preparation at P1 for one unit, and analysis would refuse M1
//     for a fact the checker did not prepare.
//   - A type is clean only once its whole strongly connected component is.
//     B and A are mutually recursive (EF119); M0 walks P, then A, then B and
//     back to A, which is still on the walk's stack, and the budget stops it
//     inside H, after B has returned. M1 carries B; its walk must reach A's
//     function field and refuse it (EF141). A per-type memo would mark B
//     clean when its frame returned, and M1 would reach H's unprepared
//     layouts instead.
func TestMachinePreparationCleanSetIsSound(t *testing.T) {
	var stop strings.Builder
	wideRecordSource(&stop, "P0", 600, 30)
	wideRecordSource(&stop, "P1", 600, 30)
	machinesOver(&stop, "p0: P0, p1: P1", "p1: P1")
	var cycle strings.Builder
	wideRecordSource(&cycle, "P", 800, 30)
	wideRecordSource(&cycle, "H", 400, 30)
	cycle.WriteString("enum B { Y { a: A } N }\nenum A { X { b: B, h: H, f: fn() -> i64 } N }\n")
	machinesOver(&cycle, "p: P, a: A", "b: B")
	for _, c := range []struct {
		name   string
		source string
		plans  []string
		want   []string
	}{
		{"budget stop", stop.String(), []string{"M1"}, []string{
			"EF142 machine M0 analysis exceeds its 65536-unit budget; no plan is published",
		}},
		{"component", cycle.String(), nil, []string{
			"EF119 recursive data layout is unsupported: B->A->B",
			"EF127 recursive or excessive field occurrence layout",
			"EF127 recursive or excessive field occurrence layout",
			"EF142 machine M0 analysis exceeds its 65536-unit budget; no plan is published",
			"EF141 machine data S1.A.b.Y.a.X.f carries a function or effect recipe; machine state, event and output values are not yet admitted to carry behavior: portable machine payloads need checked actor ownership",
		}},
	} {
		r, _ := prepareModule(t, c.source)
		var plans []string
		for _, m := range r.Machines {
			plans = append(plans, m.Name)
		}
		got := diagnosticSummary(r)
		if !slices.Equal(plans, c.plans) || !slices.Equal(got, c.want) {
			t.Errorf("%s: plans %v, diagnostics %q; want plans %v, diagnostics %q", c.name, plans, got, c.plans, c.want)
		}
	}
}
