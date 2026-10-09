package compiler

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Resource closure as a class. Machine analysis builds every program-sized
// value through charged builders, so what it allocates stays within a constant
// factor of the units it charged, whatever the program. The property test
// below generates adversarial machines (wide enums, long names, deep helpers,
// wide rows, wide match arms, many declarations, deep payloads) and measures
// each module's analysis between the analysis probe's brackets: heap bytes
// (runtime.MemStats.TotalAlloc) and allocation count (Mallocs) against the
// charged units. A new allocation site that escapes the budget fails it as a
// class, without a hand-built counterexample.

// Bounds per charged unit, plus a fixed allowance for per-module setup.
const (
	machineBytesPerUnit  = 512
	machineAllocsPerUnit = 8
	machineBaseBytes     = 64 << 10
	machineBaseAllocs    = 512
)

type machineMeasure struct {
	units          int
	bytes, mallocs uint64
}

// measureMachineAnalysis compiles source and returns one measure per machine
// analysis the compile ran.
func measureMachineAnalysis(t *testing.T, source string) (*Result, []machineMeasure) {
	t.Helper()
	var measures []machineMeasure
	var before runtime.MemStats
	machineAnalysisProbe = func(done bool, units int) {
		if !done {
			runtime.ReadMemStats(&before)
			return
		}
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		measures = append(measures, machineMeasure{units: units, bytes: after.TotalAlloc - before.TotalAlloc, mallocs: after.Mallocs - before.Mallocs})
	}
	defer func() { machineAnalysisProbe = nil }()
	r := Compile(source)
	return r, measures
}

func assertMachineAllocationCharged(t *testing.T, label string, measures []machineMeasure) {
	t.Helper()
	if len(measures) == 0 {
		t.Fatalf("%s: machine analysis never ran", label)
	}
	for _, m := range measures {
		if testing.Verbose() && m.units > 0 {
			t.Logf("%s: units=%d bytes=%d (%.1f/unit) mallocs=%d (%.2f/unit)", label[:min(len(label), 160)], m.units, m.bytes, float64(m.bytes)/float64(m.units), m.mallocs, float64(m.mallocs)/float64(m.units))
		}
		if m.bytes > uint64(machineBytesPerUnit*m.units+machineBaseBytes) || m.mallocs > uint64(machineAllocsPerUnit*m.units+machineBaseAllocs) {
			t.Fatalf("%s: analysis allocated %d bytes in %d allocations for %d charged units (bounds %d bytes/unit, %d allocations/unit)", label, m.bytes, m.mallocs, m.units, machineBytesPerUnit, machineAllocsPerUnit)
		}
	}
}

// machineShape is one generated adversarial machine module.
type machineShape struct {
	states, events int // enum widths
	name           int // extra bytes on machine, enum, helper and field names
	machineName    int // extra bytes on machine declaration names only
	variantName    int // extra bytes on every variant name
	helpers, tails int // Step helper chain depth and if-tails per helper
	distinct       int // distinct helpers at the last level (0 means one)
	wideArm        bool
	keepPaths      bool // Go targets keep the state through a helper chain
	keeps          int  // depth of that chain (0 means max(1, helpers))
	literalStay    bool // tails Stay with a literal state (EF139 under wide arms)
	failures       int  // failures declared by an effect step
	declarations   int  // machine declarations sharing the step
	payload        int  // depth of the output record chain
}

func (s machineShape) String() string {
	return fmt.Sprintf("states=%d events=%d name=%d machineName=%d variantName=%d helpers=%d tails=%d distinct=%d wideArm=%t keepPaths=%t keeps=%d literalStay=%t failures=%d declarations=%d payload=%d",
		s.states, s.events, s.name, s.machineName, s.variantName, s.helpers, s.tails, s.distinct, s.wideArm, s.keepPaths, s.keeps, s.literalStay, s.failures, s.declarations, s.payload)
}

// nestedTails renders count tails selected by n as nested ifs.
func nestedTails(count int, tail func(int) string) string {
	var b strings.Builder
	for i := 0; i < count-1; i++ {
		fmt.Fprintf(&b, "    if n == %d { %s } else {\n", i, tail(i))
	}
	fmt.Fprintf(&b, "    %s\n%s", tail(count-1), strings.Repeat("    }\n", count-1))
	return b.String()
}

func (s machineShape) source() string {
	long := func(base string, extra int) string { return base + strings.Repeat("x", extra) }
	enum, event := long("S", s.name), long("E", s.name)
	variant := func(i int) string { return fmt.Sprintf("V%d%s", i, strings.Repeat("v", s.variantName)) }
	output := "void"
	var b strings.Builder
	b.WriteString("import Machines \"effra/machines\"\n")
	for i := 0; i < s.failures; i++ {
		fmt.Fprintf(&b, "error F%d\n", i)
	}
	if s.payload > 0 {
		field := long("f", s.name)
		fmt.Fprintf(&b, "record P%d { leaf: i64 }\n", s.payload)
		for i := s.payload - 1; i >= 0; i-- {
			fmt.Fprintf(&b, "record P%d { %s: P%d  other%s: P%d }\n", i, field, i+1, field, i+1)
		}
		output = "P0"
	}
	step := fmt.Sprintf("Machines.Step<%s, %s>", enum, output)
	fmt.Fprintf(&b, "enum %s {", enum)
	for i := 0; i < s.states; i++ {
		b.WriteString(" " + variant(i))
	}
	fmt.Fprintf(&b, " }\nenum %s {", event)
	for i := 0; i < s.events; i++ {
		fmt.Fprintf(&b, " P%d", i)
	}
	b.WriteString(" }\n")
	helper := func(i int) string { return fmt.Sprintf("%s%d", long("h", s.name), i) }
	keep := func(i int) string { return fmt.Sprintf("%s%d", long("k", s.name), i) }
	leaf := func(i int) string {
		switch {
		case s.keepPaths && i%2 == 0:
			return fmt.Sprintf("%s.Go(%s(state, n))", step, keep(0))
		case s.literalStay && i%3 == 0:
			return fmt.Sprintf("%s.Stay(%s.%s {})", step, enum, variant(i%s.states))
		}
		switch i % 4 {
		case 0:
			return step + ".Ignore {}"
		case 1:
			return step + ".Stay(state)"
		case 2:
			return fmt.Sprintf("%s.Go(%s.%s {})", step, enum, variant(i%s.states))
		}
		return step + ".Reject {}"
	}
	first := leaf(0)
	if s.helpers > 0 {
		first = helper(0) + "(state, event, 0)"
	}
	signature := fmt.Sprintf("fn step(state: %s, event: %s) -> %s", enum, event, step)
	if s.failures > 0 {
		labels := make([]string, s.failures)
		for i := range labels {
			labels[i] = fmt.Sprintf("F%d", i)
		}
		signature = "effect " + signature + " raises { " + strings.Join(labels, ", ") + " }"
	}
	n := ""
	if s.helpers == 0 {
		n = "    let n = 0\n"
	}
	if s.wideArm && s.states > 1 {
		most := make([]string, s.states-1)
		for i := range most {
			most[i] = enum + "." + variant(i)
		}
		fmt.Fprintf(&b, "%s {\n%s    match state {\n        %s => %s\n        %s.%s => %s.Ignore {}\n    }\n}\n", signature, n, strings.Join(most, " | "), first, enum, variant(s.states-1), step)
	} else {
		fmt.Fprintf(&b, "%s {\n%s    %s\n}\n", signature, n, first)
	}
	// The last level has distinct copies, so tails reaching different copies
	// are distinct transitions rather than one deduplicated span.
	copies := max(1, s.distinct)
	for h := 0; h < s.helpers; h++ {
		instances := 1
		if h == s.helpers-1 {
			instances = copies
		}
		for c := 0; c < instances; c++ {
			name := helper(h)
			if h == s.helpers-1 && c > 0 {
				name = fmt.Sprintf("%s_%d", helper(h), c)
			}
			fmt.Fprintf(&b, "fn %s(state: %s, event: %s, n: i64) -> %s {\n", name, enum, event, step)
			b.WriteString(nestedTails(s.tails, func(i int) string {
				if h < s.helpers-1 {
					next := helper(h + 1)
					if h+1 == s.helpers-1 && i%copies > 0 {
						next = fmt.Sprintf("%s_%d", next, i%copies)
					}
					return next + "(state, event, n)"
				}
				return leaf(i)
			}))
			b.WriteString("}\n")
		}
	}
	if s.keepPaths {
		depth := max(1, s.helpers)
		if s.keeps > 0 {
			depth = s.keeps
		}
		for k := 0; k < depth; k++ {
			fmt.Fprintf(&b, "fn %s(state: %s, n: i64) -> %s {\n", keep(k), enum, enum)
			b.WriteString(nestedTails(s.tails, func(int) string {
				if k < depth-1 {
					return keep(k+1) + "(state, n)"
				}
				return long("wide", s.name) + "(state)"
			}))
			b.WriteString("}\n")
		}
		alternatives := make([]string, max(1, s.states-1))
		for i := range alternatives {
			alternatives[i] = enum + "." + variant(i)
		}
		last := ""
		if s.states > 1 {
			last = fmt.Sprintf("        %s.%s => state\n", enum, variant(s.states-1))
		}
		fmt.Fprintf(&b, "fn %s(state: %s) -> %s {\n    match state {\n        %s => state\n%s    }\n}\n", long("wide", s.name), enum, enum, strings.Join(alternatives, " | "), last)
	}
	for d := 0; d < max(1, s.declarations); d++ {
		fmt.Fprintf(&b, "machine %s%d { initial %s.%s {}; step step }\n", long("M", s.name+s.machineName), d, enum, variant(0))
	}
	return b.String()
}

// randomMachineShape draws one shape. Sizes stay small enough to compile
// quickly while still crossing the declaration and module budgets.
func randomMachineShape(rng *rand.Rand) machineShape {
	pick := func(values ...int) int { return values[rng.Intn(len(values))] }
	s := machineShape{
		states:       pick(2, 3, 40, 400, 1500),
		events:       pick(1, 2, 50, 600),
		name:         pick(0, 0, 64, 4096, 1<<16),
		machineName:  pick(0, 0, 1<<16, 1<<20),
		helpers:      pick(0, 1, 2, 3, 4, 5),
		tails:        pick(1, 2, 7, 20),
		distinct:     pick(0, 0, 3, 20),
		wideArm:      rng.Intn(2) == 0,
		keepPaths:    rng.Intn(3) == 0,
		literalStay:  rng.Intn(3) == 0,
		failures:     pick(0, 0, 3, 300, 4000),
		declarations: pick(1, 1, 4, 64, 256),
		payload:      pick(0, 0, 3, 30),
	}
	if s.states <= 40 {
		s.variantName = pick(0, 64, 4096)
	}
	return s
}

func TestMachineAnalysisAllocationIsCharged(t *testing.T) {
	shapes := []machineShape{
		// The three round-4 residuals at their reviewed shapes, scaled to
		// compile quickly: narrowings refined on every path, a long machine
		// name over thousands of transitions, and wide rows shared by many
		// declarations.
		{states: 4000, events: 1, tails: 20, wideArm: true, keepPaths: true, keeps: 3},
		{states: 2, events: 1, machineName: 1 << 20, helpers: 2, tails: 20, distinct: 20},
		{states: 2, events: 1, failures: 4000, declarations: 256},
		// Long variant and enum names under wide literal-Stay diagnostics.
		{states: 40, events: 2, name: 4096, variantName: 4096, helpers: 4, tails: 7, wideArm: true, literalStay: true},
		// Deep payload with long field names across every declaration.
		{states: 2, events: 1, name: 4096, payload: 30, declarations: 256},
		// Wide events and states over the transition limit.
		{states: 2000, events: 600, helpers: 3, tails: 20, wideArm: true},
	}
	seed := int64(46)
	if text := os.Getenv("EFFRA_MACHINE_ALLOC_SEED"); text != "" {
		seed, _ = strconv.ParseInt(text, 10, 64)
	}
	rng := rand.New(rand.NewSource(seed))
	for len(shapes) < 36 {
		shapes = append(shapes, randomMachineShape(rng))
	}
	admitted, refused := 0, 0
	for i, shape := range shapes {
		r, measures := measureMachineAnalysis(t, shape.source())
		assertMachineAllocationCharged(t, fmt.Sprintf("shape %d (%s)", i, shape), measures)
		if len(r.Machines) > 0 {
			admitted++
		}
		if slices.Contains(machineCodes(r), machineBudgetCode) {
			refused++
		}
	}
	// The generator must exercise both sides of the budget.
	if admitted == 0 || refused == 0 {
		t.Fatalf("generated shapes admitted %d and refused %d modules", admitted, refused)
	}
}

// Round-4 residual 1: refining a wide narrowing on every path. Narrowings are
// canonical and refinements memoized, so 8,000 paths through three helpers of
// 20 tails into one wide re-match share one narrowing instead of each copying
// 3,999 entries before any charge.
func TestMachineNarrowingsAreSharedAcrossPaths(t *testing.T) {
	shape := machineShape{states: 4000, events: 1, tails: 20, wideArm: true, keepPaths: true, keeps: 3}
	r, measures := measureMachineAnalysis(t, shape.source())
	assertMachineAllocationCharged(t, "shared narrowings", measures)
	if measures[len(measures)-1].bytes > 64<<20 {
		t.Fatalf("8,000 equal narrowings allocated %d bytes", measures[len(measures)-1].bytes)
	}
	if codes := machineCodes(r); len(codes) > 0 && !slices.Equal(codes, []string{machineBudgetCode}) {
		t.Fatalf("codes = %v", codes)
	}
	// 400 such paths fit the budget: every one keeps the step's own
	// narrowing, so the Go decision enters exactly those 3,999 states.
	small := machineShape{states: 4000, events: 1, tails: 20, wideArm: true, keepPaths: true, keeps: 2}
	r, measures = measureMachineAnalysis(t, small.source())
	assertMachineAllocationCharged(t, "400 shared narrowings", measures)
	if len(r.Machines) != 1 || !slices.ContainsFunc(r.Machines[0].Transitions, func(tr MachineTransition) bool {
		return tr.Decision == machineDecisionGo && tr.Precision == machineTargetCurrent && len(tr.Targets) == 3999
	}) {
		t.Fatalf("400 shared narrowings: codes %v", machineCodes(r))
	}
}

// Round-4 residual 2: transition IDs are positional, so a long machine name is
// stored once in the plan instead of being copied into every transition ID.
func TestMachineTransitionIDsDoNotCopyTheMachineName(t *testing.T) {
	shape := machineShape{states: 2, events: 1, machineName: 1 << 20, helpers: 2, tails: 20, distinct: 20}
	r, measures := measureMachineAnalysis(t, shape.source())
	assertMachineAllocationCharged(t, "long machine name", measures)
	if len(r.Machines) != 1 {
		t.Fatalf("long machine name refused: %v", machineCodes(r))
	}
	plan := r.Machines[0]
	if len(plan.Transitions) != 400 || plan.Ordinal != 0 || len(plan.Name) < 1<<20 {
		t.Fatalf("plan: %d transitions, ordinal %d", len(plan.Transitions), plan.Ordinal)
	}
	for i, transition := range plan.Transitions {
		if want := fmt.Sprintf("0:transition:%d", i); transition.ID != want {
			t.Fatalf("transition %d ID = %.40q, want %s", i, transition.ID, want)
		}
	}
}

// Round-4 residual 3: wide declared rows. A step declaring 4,000 failures and
// bound by 256 declarations is projected once for the module and its
// contributions shared, instead of copying every row into every plan.
func TestMachineWideRowsAreProjectedOncePerModule(t *testing.T) {
	shape := machineShape{states: 2, events: 1, failures: 4000, declarations: 256}
	r, measures := measureMachineAnalysis(t, shape.source())
	assertMachineAllocationCharged(t, "wide rows", measures)
	if len(r.Machines) != 256 {
		t.Fatalf("%d of 256 declarations planned: %v", len(r.Machines), machineCodes(r))
	}
	first, last := r.Machines[0], r.Machines[255]
	if len(first.TerminalFailures) != 4000 || &first.TerminalFailures[0] != &last.TerminalFailures[0] || &first.Functions[0].Failures[0] != &last.Functions[0].Failures[0] {
		t.Fatal("wide rows are not shared across plans")
	}
	// Rows wider than one declaration's budget are refused before they are
	// copied.
	wide := machineShape{states: 2, events: 1, failures: maxMachineWork / 4, declarations: 1}
	r, measures = measureMachineAnalysis(t, wide.source())
	assertMachineAllocationCharged(t, "rows over budget", measures)
	if !slices.Equal(machineCodes(r), []string{machineBudgetCode}) {
		t.Fatalf("rows over budget codes = %v", machineCodes(r))
	}
}

// The machine graph view repeats the plan ID in every state node and edge
// identity, so it charges identities by bytes: a 1 MiB machine name over 400
// transitions is refused at the work limit after a bounded allocation instead
// of copying the name into every edge.
func TestMachineGraphChargesIdentityBytes(t *testing.T) {
	shape := machineShape{states: 2, events: 1, machineName: 1 << 20, helpers: 2, tails: 20, distinct: 20}
	r := Compile(shape.source())
	if len(r.Machines) != 1 {
		t.Fatalf("long machine name refused: %v", machineCodes(r))
	}
	facts := newGraphFacts(GraphKindMachine, defaultGraphViewLimits)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	admitted := machineGraphPlan(facts, &r.Machines[0])
	runtime.ReadMemStats(&after)
	if admitted || facts.err == nil || !strings.Contains(facts.err.Error(), string(GraphRefusalWorkLimit)) {
		t.Fatalf("1 MiB identities admitted: %v", facts.err)
	}
	if bytes := after.TotalAlloc - before.TotalAlloc; bytes > uint64(machineBytesPerUnit*defaultGraphViewLimits.Work) {
		t.Fatalf("graph allocated %d bytes for at most %d work units", bytes, defaultGraphViewLimits.Work)
	}
	// Ordinary names still project the same plan shape.
	small := machineShape{states: 2, events: 1, helpers: 2, tails: 20, distinct: 20}
	request, err := ParseGraphRequest(map[string]any{"kind": "machine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(small.source()).GraphView(request); err != nil {
		t.Fatalf("ordinary graph refused: %v", err)
	}
}
