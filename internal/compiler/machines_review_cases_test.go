package compiler

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The reviewers' allocation probes from rounds 1 and 2, as in-tree cases.
// Each program's machine analysis must stay within the per-unit bound
// (assertMachineAllocationCharged); the GraphView and projection probes are
// cases of machines_boundaries_test.go, and the generated axes they found
// are drawn by randomMachineShape.

// Round 1: Outer<T> has `fields` fields of type L<T>; the output is
// Outer<P<…>> over a 7-ary tree of distinct records, so every field
// substitution walks the argument.
func reviewTemplateArgumentSource(fields, depth int) string {
	const branch = 7
	var b strings.Builder
	leaves := 1
	for i := 0; i < depth; i++ {
		leaves *= branch
	}
	for i := 0; i < leaves; i++ {
		fmt.Fprintf(&b, "record R%d { x: i64 }\n", i)
	}
	params := make([]string, branch)
	for i := range params {
		params[i] = fmt.Sprintf("T%d: type", i)
	}
	fmt.Fprintf(&b, "record P<%s> { x: T0 }\n", strings.Join(params, ", "))
	next := 0
	var tree func(int) string
	tree = func(level int) string {
		if level == depth {
			next++
			return fmt.Sprintf("R%d", next-1)
		}
		children := make([]string, branch)
		for i := range children {
			children[i] = tree(level + 1)
		}
		return "P<" + strings.Join(children, ", ") + ">"
	}
	b.WriteString("record L<T: type> { value: T }\nrecord Outer<T: type> {")
	for i := 0; i < fields; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " a%d: L<T>", i)
	}
	b.WriteString(" }\n")
	out := "Outer<" + tree(0) + ">"
	b.WriteString("enum S { A }\nenum E { Ping }\n")
	fmt.Fprintf(&b, "fn step(state: S, event: E) -> decision<S, %s> {\n    ignore\n}\n", out)
	b.WriteString("machine M { initial S.A {}; step step }\n")
	return b.String()
}

// reviewOutputSource is one machine whose step outputs output.
func reviewOutputSource(declarations, output string) string {
	return fmt.Sprintf("%senum S { A }\nenum E { Ping }\nfn step(state: S, event: E) -> decision<S, %s> { ignore }\nmachine M { initial S.A {}; step step }\n", declarations, output)
}

// Round 2: an initial value spelling Presence<tree> explicitly, checked by
// the ordinary checker before analysis starts.
func reviewInitialSource(depth, declarations int) string {
	var b strings.Builder
	tree := genericArgument(&b, "", depth)
	b.WriteString("enum Presence<T: type> { None; Some { value: T } }\n")
	fmt.Fprintf(&b, "enum S { V0 { x: Presence<%s> }; V1 }\nenum E { Ping }\n", tree)
	b.WriteString("fn step(state: S, event: E) -> decision<S, void> { ignore }\n")
	for d := 0; d < declarations; d++ {
		fmt.Fprintf(&b, "machine M%d { initial S.V0 { x: Presence<%s>.None {} }; step step }\n", d, tree)
	}
	return b.String()
}

// Round 2: many unrelated records, whose every type lookup the checker's
// initial check pays for.
func reviewManyRecordsSource(records, declarations int, payload bool) string {
	var b strings.Builder
	for i := 0; i < max(1, records); i++ {
		fmt.Fprintf(&b, "record R%d { x: i64 }\n", i)
	}
	b.WriteString("enum S { V0 { x: R0 }; V1 }\nenum E { Ping }\n")
	b.WriteString("fn step(state: S, event: E) -> decision<S, void> { ignore }\n")
	initial := "S.V1 {}"
	if payload {
		initial = "S.V0 { x: R0 { x: 1 } }"
	}
	for d := 0; d < declarations; d++ {
		fmt.Fprintf(&b, "machine M%d { initial %s; step step }\n", d, initial)
	}
	return b.String()
}

// Round-2 review M1: the initial binding is checked in the checking pass,
// outside the analysis window, so neither many records nor an explicit
// generic initial reaches the window.
func TestMachineInitialCheckIsOutsideTheAnalysis(t *testing.T) {
	cases := []struct {
		label  string
		source string
		plans  int
	}{
		{"20,000 records, 64 plain initials", reviewManyRecordsSource(20000, 64, false), 64},
		{"20,000 records, 64 initials with a record payload", reviewManyRecordsSource(20000, 64, true), 64},
		{"20,000 records, one initial", reviewManyRecordsSource(20000, 1, false), 1},
		{"20,000 records, one initial with a record payload", reviewManyRecordsSource(20000, 1, true), 1},
		{"50,000 records, 256 plain initials", reviewManyRecordsSource(50000, 256, false), 256},
		{"Presence<57-node tree> initial, 1 declaration", reviewInitialSource(2, 1), 1},
		{"Presence<57-node tree> initial, 64 declarations", reviewInitialSource(2, 64), 64},
		{"Presence<400-node tree> initial, 1 declaration", reviewInitialSource(3, 1), 1},
		{"Presence<400-node tree> initial, 16 declarations", reviewInitialSource(3, 16), 16},
	}
	for _, c := range cases {
		r, measures := measureMachineAnalysis(t, c.source)
		assertMachineAllocationCharged(t, c.label, measures)
		if len(r.Machines) != c.plans {
			t.Fatalf("%s: %d of %d plans: %v", c.label, len(r.Machines), c.plans, machineCodes(r))
		}
	}
}

// Round-2 review M2: seed 26, shape 18 of the round-2 generator (256
// declarations of a failing step with no transitions) and the neighbours that
// isolate its axes. Each declaration's fixed state is charged before its plan
// is allocated (machineplan DeclarationUnits).
var machineSeed26Shape18 = machineShape{states: 2, events: 2, name: 64, helpers: 3, tails: 7, distinct: 20, failures: 3, declarations: 256, failureName: 64, failBody: true, genericName: 131072, genericFields: 64, genericArg: 2, recipeName: 64}

func TestMachineDeclarationStateIsCharged(t *testing.T) {
	variants := []struct {
		label string
		edit  func(*machineShape)
	}{
		{"exact", func(*machineShape) {}},
		{"failBody=false", func(s *machineShape) { s.failBody = false }},
		{"declarations=64", func(s *machineShape) { s.declarations = 64 }},
		{"declarations=1", func(s *machineShape) { s.declarations = 1 }},
		{"no helpers", func(s *machineShape) { s.helpers, s.tails, s.distinct = 0, 0, 0 }},
		{"short names", func(s *machineShape) { s.name, s.failureName = 0, 0 }},
		{"distinct=0", func(s *machineShape) { s.distinct = 0 }},
	}
	for _, v := range variants {
		shape := machineSeed26Shape18
		v.edit(&shape)
		_, measures := shape.measure(t)
		assertMachineAllocationCharged(t, "seed 26 shape 18, "+v.label, measures)
	}
	// The cheapest declarations: no step binding, so each allocates its plan
	// and analysis record and charges little else; and the smallest complete
	// machine. Without the fixed charge, 256 step-less declarations allocate
	// about 4 KB each against about 5 units.
	for _, step := range []bool{false, true} {
		var b strings.Builder
		b.WriteString("enum S { A }\nenum E { P }\nfn step(state: S, event: E) -> decision<S, void> { ignore }\n")
		for d := 0; d < maxMachineDeclarations; d++ {
			binding := ""
			if step {
				binding = "; step step"
			}
			fmt.Fprintf(&b, "machine M%d { initial S.A {}%s }\n", d, binding)
		}
		r, measures := measureMachineAnalysis(t, b.String())
		assertMachineAllocationCharged(t, fmt.Sprintf("256 minimal declarations (step %t)", step), measures)
		if want := map[bool]int{false: 0, true: maxMachineDeclarations}[step]; len(r.Machines) != want {
			t.Fatalf("256 minimal declarations (step %t): %d plans, want %d", step, len(r.Machines), want)
		}
	}
}

// Round-2 review M4: seed 8, shape 27 of the round-2 generator, which found
// the initial check's checker work in the analysis window.
var machineSeed8Shape27 = machineShape{states: 400, events: 600, name: 4096, tails: 20, distinct: 3, failures: 300, declarations: 64, payload: 3, failBody: true, recipe: 1}

func TestMachineSeed8Shape27IsCharged(t *testing.T) {
	_, measures := measureMachineAnalysis(t, machineSeed8Shape27.source())
	assertMachineAllocationCharged(t, "seed 8 shape 27", measures)
}

// The remaining round-1 and round-2 analysis probes.
func TestMachineReviewedAnalysisProbesAreCharged(t *testing.T) {
	type probe struct {
		label  string
		source string
	}
	var probes []probe
	probes = append(probes, probe{"round 1: 3,000 fields over a 400-node argument", reviewTemplateArgumentSource(3000, 3)})
	for _, depth := range []int{1, 30, 60} {
		name := "R" + strings.Repeat("x", 32767)
		output := strings.Repeat("Effect<", depth) + name + strings.Repeat(">", depth)
		probes = append(probes, probe{fmt.Sprintf("round 1: %d recipe layers over a 32,768-byte name", depth), reviewOutputSource("record "+name+" { x: i64 }\n", output)})
	}
	// Nested recipes whose every layer carries a row of long failure labels.
	for _, c := range [][3]int{{10, 10, 1024}, {30, 4, 4096}, {50, 2, 4096}, {20, 60, 64}} {
		depth, labels, width := c[0], c[1], c[2]
		var declarations strings.Builder
		names := make([]string, labels)
		for i := range names {
			names[i] = fmt.Sprintf("F%d%s", i, strings.Repeat("f", width))
			fmt.Fprintf(&declarations, "error %s\n", names[i])
		}
		declarations.WriteString("record R { x: i64 }\n")
		output := "R"
		for i := 0; i < depth; i++ {
			output = "Effect<" + output + ", {" + strings.Join(names, ", ") + "}>"
		}
		probes = append(probes, probe{fmt.Sprintf("round 2: %d recipe layers of %d %d-byte failures", depth, labels, width), reviewOutputSource(declarations.String(), output)})
	}
	// A nested recipe as a template argument, over many fields.
	for _, c := range [][3]int{{50, 64, 32767}, {50, 1, 32767}, {20, 3000, 4096}} {
		depth, fields, width := c[0], c[1], c[2]
		leaf := "R" + strings.Repeat("x", width)
		var declarations strings.Builder
		fmt.Fprintf(&declarations, "record %s { x: i64 }\nrecord L<T: type> { value: T }\nrecord Outer<T: type> { a0: L<T>", leaf)
		for i := 1; i < fields; i++ {
			fmt.Fprintf(&declarations, ", a%d: L<T>", i)
		}
		declarations.WriteString(" }\n")
		output := "Outer<" + strings.Repeat("Effect<", depth) + leaf + strings.Repeat(">", depth) + ">"
		probes = append(probes, probe{fmt.Sprintf("round 2: Outer<%d recipe layers> over %d fields", depth, fields), reviewOutputSource(declarations.String(), output)})
	}
	for _, p := range probes {
		r, measures := measureMachineAnalysis(t, p.source)
		assertMachineAllocationCharged(t, p.label, measures)
		if len(r.Machines) == 0 && !slices.ContainsFunc(machineCodes(r), func(code string) bool { return code == machineBudgetCode || code == machineAdmissionCode }) {
			t.Fatalf("%s: neither planned nor refused by machine analysis: %v", p.label, machineCodes(r))
		}
	}
}
