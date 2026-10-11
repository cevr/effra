package compiler

import (
	"errors"
	"flag"
	"fmt"
	"maps"
	"math/rand"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
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
	return measureMachineAnalysisAfter(t, source, nil)
}

// measureMachineAnalysisAfter is measureMachineAnalysis with setup applied to
// the checker just before each window opens. The verifier setup returns, if
// any, runs as the window closes and reports what the window changed.
func measureMachineAnalysisAfter(t *testing.T, source string, setup func(*checker) func(*checker) string) (*Result, []machineMeasure) {
	t.Helper()
	var measures []machineMeasure
	var before runtime.MemStats
	var verify func(*checker) string
	machineAnalysisProbe = func(c *checker, done bool, units int) {
		if !done {
			if setup != nil {
				verify = setup(c)
			}
			runtime.ReadMemStats(&before)
			return
		}
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		measures = append(measures, machineMeasure{units: units, bytes: after.TotalAlloc - before.TotalAlloc, mallocs: after.Mallocs - before.Mallocs})
		if verify != nil {
			if changed := verify(c); changed != "" {
				t.Errorf("the analysis window changed checker state: %s", changed)
			}
		}
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
	failureName    int  // extra bytes on every failure name
	failBody       bool // the effect step body only fails: no transition
	lets           int  // let bindings after the step's first statement
	generic        bool // output Outer<Arg> through generic records
	genericName    int  // extra bytes on the generic inner record's name
	genericFields  int  // Outer<T>'s fields of type Inner<T> (0 means one)
	genericArg     int  // depth of a 7-ary tree of distinct records as Arg (0 means i64)
	recipe         int  // Effect<...> layers around the output (refused, EF141)
	recipeName     int  // extra bytes on the record the recipe output wraps
	records        int  // unrelated one-field records in the program
	initialArg     int  // the initial spells Presence<tree of this depth> explicitly (0 means a plain initial)
	// Checker state before machines: the analysis window must not depend on it.
	errors int  // ordinary type errors reported before the machines
	clip   bool // put checker slices at capacity and maps at their size as the window opens
}

func (s machineShape) String() string {
	return fmt.Sprintf("states=%d events=%d name=%d machineName=%d variantName=%d helpers=%d tails=%d distinct=%d wideArm=%t keepPaths=%t keeps=%d literalStay=%t failures=%d declarations=%d payload=%d failureName=%d failBody=%t lets=%d generic=%t genericName=%d genericFields=%d genericArg=%d recipe=%d recipeName=%d records=%d initialArg=%d errors=%d clip=%t",
		s.states, s.events, s.name, s.machineName, s.variantName, s.helpers, s.tails, s.distinct, s.wideArm, s.keepPaths, s.keeps, s.literalStay, s.failures, s.declarations, s.payload, s.failureName, s.failBody, s.lets, s.generic, s.genericName, s.genericFields, s.genericArg, s.recipe, s.recipeName, s.records, s.initialArg, s.errors, s.clip)
}

// genericArgument declares the records of a 7-ary tree of the given depth
// over distinct leaf records, and returns the tree as a type: P<P<R0, …>, …>.
// Every node is distinct: depth 3 has 57 applications and 343 records.
func genericArgument(b *strings.Builder, prefix string, depth int) string {
	const branch = 7
	if depth == 0 {
		return "i64"
	}
	params := make([]string, branch)
	for i := range params {
		params[i] = fmt.Sprintf("T%d: type", i)
	}
	fmt.Fprintf(b, "record %sP<%s> { x: T0 }\n", prefix, strings.Join(params, ", "))
	leaves := 0
	var tree func(int) string
	tree = func(level int) string {
		if level == depth {
			fmt.Fprintf(b, "record %sR%d { x: i64 }\n", prefix, leaves)
			leaves++
			return fmt.Sprintf("%sR%d", prefix, leaves-1)
		}
		children := make([]string, branch)
		for i := range children {
			children[i] = tree(level + 1)
		}
		return prefix + "P<" + strings.Join(children, ", ") + ">"
	}
	return tree(0)
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
	failure := func(i int) string { return fmt.Sprintf("F%d%s", i, strings.Repeat("f", s.failureName)) }
	for i := 0; i < s.failures; i++ {
		fmt.Fprintf(&b, "error %s\n", failure(i))
	}
	for i := 0; i < s.records; i++ {
		fmt.Fprintf(&b, "record Rec%d { x: i64 }\n", i)
	}
	for i := 0; i < s.errors; i++ {
		fmt.Fprintf(&b, "fn ordinaryError%d() -> i64 { true }\n", i)
	}
	// An explicit generic initial: S.Vi { x: Presence<tree>.None {} }, with Vi
	// matched wherever the step matches every variant.
	initial, extra := fmt.Sprintf("%s.%s {}", enum, variant(0)), ""
	if s.initialArg > 0 {
		tree := genericArgument(&b, "I", s.initialArg)
		b.WriteString("enum Presence<T: type> { None; Some { value: T } }\n")
		initial = fmt.Sprintf("%s.Vi { x: Presence<%s>.None {} }", enum, tree)
		extra = fmt.Sprintf(" Vi { x: Presence<%s> }", tree)
	}
	if s.recipe > 0 {
		leaf := long("R", s.recipeName)
		fmt.Fprintf(&b, "record %s { x: i64 }\n", leaf)
		output = strings.Repeat("Effect<", s.recipe) + leaf + strings.Repeat(">", s.recipe)
	} else if s.generic {
		inner := long("L", s.genericName)
		fmt.Fprintf(&b, "record %s<T: type> { value: T }\nrecord Outer<T: type> { inner: %s<T>", inner, inner)
		for i := 1; i < s.genericFields; i++ {
			fmt.Fprintf(&b, ", a%d: %s<T>", i, inner)
		}
		b.WriteString(" }\n")
		output = "Outer<" + genericArgument(&b, "", s.genericArg) + ">"
	} else if s.payload > 0 {
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
	b.WriteString(extra)
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
			labels[i] = failure(i)
		}
		signature = "effect " + signature + " raises { " + strings.Join(labels, ", ") + " }"
		if s.failBody {
			first = "fail " + failure(0)
		}
	}
	n := ""
	if s.helpers == 0 {
		n = "    let n = 0\n"
	}
	body := "    " + first + "\n"
	if s.wideArm && s.states > 1 {
		most := make([]string, s.states-1)
		for i := range most {
			most[i] = enum + "." + variant(i)
		}
		if s.initialArg > 0 {
			most = append(most, enum+".Vi")
		}
		body = fmt.Sprintf("    match state {\n        %s => %s\n        %s.%s => %s.Ignore {}\n    }\n", strings.Join(most, " | "), first, enum, variant(s.states-1), step)
	}
	// Statements after the first: the walk reaches them after any refusal
	// the first statement caused.
	if s.lets > 0 {
		var lets strings.Builder
		for i := 0; i < s.lets; i++ {
			fmt.Fprintf(&lets, "    let l%d = %d\n", i, i)
		}
		body += lets.String() + "    " + step + ".Ignore {}\n"
	}
	fmt.Fprintf(&b, "%s {\n%s%s}\n", signature, n, body)
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
		if s.initialArg > 0 {
			alternatives = append(alternatives, enum+".Vi")
		}
		last := ""
		if s.states > 1 {
			last = fmt.Sprintf("        %s.%s => state\n", enum, variant(s.states-1))
		}
		fmt.Fprintf(&b, "fn %s(state: %s) -> %s {\n    match state {\n        %s => state\n%s    }\n}\n", long("wide", s.name), enum, enum, strings.Join(alternatives, " | "), last)
	}
	for d := 0; d < max(1, s.declarations); d++ {
		fmt.Fprintf(&b, "machine %s%d { initial %s; step step }\n", long("M", s.name+s.machineName), d, initial)
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
		failureName:  pick(0, 0, 64, 4096, 1<<17),
		failBody:     rng.Intn(4) == 0,
		lets:         pick(0, 0, 100, 5000),
		generic:      rng.Intn(4) == 0,
		genericName:  pick(0, 64, 4096, 1<<17),
		// Structured arguments across many fields: substitution validates
		// the argument once per field.
		genericFields: pick(0, 0, 64, 3000),
		genericArg:    pick(0, 0, 2, 3),
		recipe:        pick(0, 0, 0, 1, 30, 60),
		recipeName:    pick(0, 64, 4096, 32767),
		// Program-wide checker work and explicit generic initials, which the
		// checker resolves before analysis starts.
		records:    pick(0, 0, 0, 0, 0, 0, 2000, 2000, 20000),
		initialArg: pick(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 2, 2, 3),
	}
	if s.states <= 40 {
		s.variantName = pick(0, 64, 4096)
	}
	// Keep generated sources in the tens of megabytes: the cost under test is
	// analysis, not lexing.
	if s.failures*s.failureName > 2<<20 {
		s.failureName = 64
	}
	// The checker resolves an explicit generic initial superlinearly in its
	// tree (a checker cost outside the analysis): keep its compile quick.
	switch s.initialArg {
	case 2:
		s.declarations = min(s.declarations, 64)
	case 3:
		s.declarations = 1
	}
	s.machineName = min(s.machineName, (16<<20)/s.declarations)
	// The parser nests at most 64 levels: helper tails and Effect<…> layers
	// share them.
	if s.recipe > 0 {
		s.recipe = max(1, min(s.recipe, 56-2*s.tails))
	}
	// Checker state before machines, drawn last so the axes above draw the
	// same shapes per seed as before: ordinary diagnostics, and every checker
	// slice at capacity and map at its size as the window opens.
	s.errors = pick(0, 0, 0, 1000, 20000)
	s.clip = rng.Intn(3) == 0
	return s
}

// measure compiles the shape and measures its analysis windows, with checker
// state clipped first when the shape asks.
func (s machineShape) measure(t *testing.T) (*Result, []machineMeasure) {
	t.Helper()
	if s.clip {
		return measureMachineAnalysisAfter(t, s.source(), clipCheckerState)
	}
	return measureMachineAnalysis(t, s.source())
}

// clipCheckerState puts the checker's shared state at its worst for the
// analysis window: every arena and the diagnostics at len == cap, so one more
// element copies the whole slice, and every intern map rebuilt at exactly its
// size, so one more entry regrows it. Analysis reads none of it; a window
// that touched it would pay for the whole program. The verifier it returns
// reports any of that state the window changed, whatever the allocation
// bound would have excused.
func clipCheckerState(c *checker) func(*checker) string {
	c.result.Diagnostics = slices.Clip(c.result.Diagnostics)
	c.typeNodes = slices.Clip(c.typeNodes)
	c.rows = slices.Clip(c.rows)
	c.reasons = slices.Clip(c.reasons)
	c.ownershipReports = slices.Clip(c.ownershipReports)
	c.typeIntern = refit(c.typeIntern)
	c.rowIntern = refit(c.rowIntern)
	c.typePublicIDs = refit(c.typePublicIDs)
	c.typePublicToID = refit(c.typePublicToID)
	c.declarationFingerprints = refit(c.declarationFingerprints)
	if lexical := c.result.lexical; lexical != nil {
		lexical.references = refit(lexical.references)
	}
	before := checkerStateSizes(c)
	return func(c *checker) string {
		after := checkerStateSizes(c)
		for name, size := range before {
			if after[name] != size {
				return fmt.Sprintf("%s from %d to %d", name, size, after[name])
			}
		}
		return ""
	}
}

// checkerStateSizes is the length of every piece of checker state
// clipCheckerState puts at capacity.
func checkerStateSizes(c *checker) map[string]int {
	sizes := map[string]int{
		"diagnostics":              len(c.result.Diagnostics),
		"type arena":               len(c.typeNodes),
		"row arena":                len(c.rows),
		"reasons":                  len(c.reasons),
		"ownership reports":        len(c.ownershipReports),
		"type intern map":          len(c.typeIntern),
		"row intern map":           len(c.rowIntern),
		"type public IDs":          len(c.typePublicIDs),
		"type public ID keys":      len(c.typePublicToID),
		"declaration fingerprints": len(c.declarationFingerprints),
	}
	if lexical := c.result.lexical; lexical != nil {
		sizes["lexical references"] = len(lexical.references)
	}
	return sizes
}

// refit copies m into a map sized for exactly its entries.
func refit[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return nil
	}
	out := make(map[K]V, len(m))
	maps.Copy(out, m)
	return out
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
		// The round-5 and later residuals have dedicated regressions below;
		// the generator draws their axes.
	}
	request, err := ParseGraphRequest(map[string]any{"kind": "machine"})
	if err != nil {
		t.Fatal(err)
	}
	warmMachineBoundaries(t, request)
	for i, shape := range shapes {
		_, measures := shape.measure(t)
		assertMachineAllocationCharged(t, fmt.Sprintf("shape %d (%s)", i, shape), measures)
	}
	// The sweep: one generated shape per seed, measured at the analysis and
	// at every public boundary its plans reach.
	var worst machineWorst
	admitted, refused := 0, 0
	for k := 0; k < *machineSeeds; k++ {
		seed := *machineSeedStart + int64(k)
		shape := machineSeedShape(seed)
		label := fmt.Sprintf("seed %d (%s)", seed, shape)
		started := time.Now()
		r, measures := shape.measure(t)
		if testing.Verbose() {
			t.Logf("seed %d compiled in %s: %d plans, codes %v", seed, time.Since(started).Round(time.Millisecond), len(r.Machines), slices.Compact(machineCodes(r)))
		}
		worst.observe(label, measures)
		assertMachineAllocationCharged(t, label, measures)
		if len(r.Machines) > 0 {
			admitted++
			worst.observe(label+" boundaries", assertMachineBoundariesCharged(t, label, shape, r, measures, request))
		}
		if slices.Contains(machineCodes(r), machineBudgetCode) {
			refused++
		}
	}
	t.Logf("%d seeds from %d: worst %.1f B/unit at %s", *machineSeeds, *machineSeedStart, worst.ratio, worst.label)
	// The generator must exercise both sides of the budget.
	if *machineSeeds >= 64 && (admitted == 0 || refused == 0) {
		t.Fatalf("generated shapes admitted %d and refused %d modules", admitted, refused)
	}
}

// The sweep's seeds: -machine.seeds=N draws N shapes, one per seed from
// -machine.seed, for a deeper local sweep than the default.
var (
	machineSeeds     = flag.Int("machine.seeds", 64, "generated machine shapes the allocation sweep measures, one per seed")
	machineSeedStart = flag.Int64("machine.seed", 1, "first seed of the machine allocation sweep")
)

// machineSeedShape is the shape one seed draws.
func machineSeedShape(seed int64) machineShape {
	return randomMachineShape(rand.New(rand.NewSource(seed)))
}

// machineWorst records the highest bytes per charged unit a sweep measured.
type machineWorst struct {
	ratio float64
	label string
}

func (w *machineWorst) observe(label string, measures []machineMeasure) {
	for _, m := range measures {
		if m.units == 0 {
			continue
		}
		if ratio := float64(m.bytes) / float64(m.units); ratio > w.ratio {
			w.ratio, w.label = ratio, label
		}
	}
}

func machineUnits(measures []machineMeasure) int {
	units := 0
	for _, m := range measures {
		units += m.units
	}
	return units
}

// warmMachineBoundaries runs every boundary once, so one-time reflection
// caches are not measured as a shape's cost.
func warmMachineBoundaries(t *testing.T, request GraphRequest) {
	t.Helper()
	r := Compile(machineShape{states: 2, events: 1, failures: 1}.source())
	r.CheckResponse()
	for _, plan := range r.Machines {
		_, _ = r.MachineInspection(plan.Name, "machines.ef")
	}
	if _, err := r.GraphView(request); err != nil {
		t.Fatal(err)
	}
}

// machineBoundaries are the public projections of checked plans.
var machineBoundaries = []struct {
	name    string
	project func(*Result)
}{
	{"CheckResponse", func(r *Result) { r.CheckResponse() }},
	{"MachineInspection", func(r *Result) {
		for _, plan := range r.Machines {
			_, _ = r.MachineInspection(plan.Name, "machines.ef")
		}
	}},
}

// assertMachineBoundariesCharged measures what each public boundary
// allocates for r's plans beyond what it allocates for their identities, and
// holds it to the per-unit bound against the analysis units plus any graph
// work the boundary charged itself. Each boundary runs on a fresh compile
// (the first on r), so no boundary inherits another's warm caches, and each
// identity fixture is built outside the measured window.
func assertMachineBoundariesCharged(t *testing.T, label string, shape machineShape, r *Result, measures []machineMeasure, request GraphRequest) []machineMeasure {
	t.Helper()
	var out []machineMeasure
	for i, boundary := range machineBoundaries {
		if i > 0 {
			r, measures = shape.measure(t)
		}
		fullBytes, fullMallocs := allocated(func() { boundary.project(r) })
		identity := identityPlans(r)
		baseBytes, baseMallocs := allocated(func() { boundary.project(identity) })
		m := allocationDelta(machineUnits(measures), fullBytes, fullMallocs, baseBytes, baseMallocs)
		assertMachineAllocationCharged(t, boundary.name+" of "+label, []machineMeasure{m})
		out = append(out, m)
	}
	r, measures = shape.measure(t)
	m, _ := measureMachineGraphView(t, label, r, machineUnits(measures), request)
	return append(out, m)
}

// measureMachineGraphView measures r's machine GraphView against its
// identity fixture and asserts the bound, whatever the outcome. The credit
// is the work the view charged: a published view's Usage.Work; a work-limit
// refusal's limit, which is exactly what it charged; any other refusal, the
// work of the same view with every size limit lifted, since its facts were
// built before the refusal.
func measureMachineGraphView(t *testing.T, label string, r *Result, units int, request GraphRequest) (machineMeasure, error) {
	t.Helper()
	var view *GraphView
	var err error
	fullBytes, fullMallocs := allocated(func() { view, err = r.GraphView(request) })
	limits := request.viewLimits(r)
	credit := 0
	switch {
	case err == nil:
		credit = view.Data.Effra.Usage.Work
	case graphRefusalCode(err) == GraphRefusalWorkLimit:
		credit = limits.Work
	case graphRefusalCode(err) == GraphRefusalUnchecked:
		// Refused before any fact is built: nothing is credited.
	default:
		lifted := limits
		lifted.Nodes, lifted.Edges, lifted.ResponseBytes = 1<<30, 1<<30, 1<<30
		liftedRequest := request
		liftedRequest.limits = &lifted
		copied := *r
		copied.TypeProjectionLimits = ProjectionLimits{Nodes: 1 << 30, Edges: 1 << 30, RowLabels: 1 << 30, NameBytes: 1 << 30, CompatibilityBytes: 1 << 30, ResponseBytes: 1 << 30}
		liftedView, liftedErr := copied.GraphView(liftedRequest)
		switch {
		case liftedErr == nil:
			credit = liftedView.Data.Effra.Usage.Work
		case graphRefusalCode(liftedErr) == GraphRefusalWorkLimit:
			credit = limits.Work
		default:
			t.Fatalf("GraphView of %s: refused %v, and with its size limits lifted %v", label, err, liftedErr)
		}
	}
	identity := identityPlans(r)
	baseBytes, baseMallocs := allocated(func() { _, _ = identity.GraphView(request) })
	m := allocationDelta(units+credit, fullBytes, fullMallocs, baseBytes, baseMallocs)
	assertMachineAllocationCharged(t, "GraphView of "+label, []machineMeasure{m})
	return m, err
}

func graphRefusalCode(err error) string {
	var refusal *GraphRefusal
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	return ""
}

// Round-4 residual 1: refining a wide narrowing on every path. Narrowings are
// canonical and refinements memoized, so 8,000 paths through three helpers of
// 20 tails into one wide re-match share one narrowing instead of each copying
// 3,999 entries before any charge.
func TestMachineNarrowingsAreSharedAcrossPaths(t *testing.T) {
	shape := machineShape{states: 4000, events: 1, tails: 20, wideArm: true, keepPaths: true, keeps: 3}
	r, measures := shape.measure(t)
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
	r, measures := shape.measure(t)
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
	r, measures := shape.measure(t)
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

// allocated measures the heap bytes and allocations of f.
func allocated(f func()) (uint64, uint64) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs
}

// identityPlans is r with every plan reduced to its identity: what a boundary
// allocates for r beyond identityPlans(r) is what the plans' contents add.
func identityPlans(r *Result) *Result {
	identity := *r
	identity.Machines = make([]MachinePlan, len(r.Machines))
	for j, plan := range r.Machines {
		identity.Machines[j] = MachinePlan{ID: plan.ID, Ordinal: plan.Ordinal, Name: plan.Name, Span: plan.Span, Functions: []MachineFunction{}, Transitions: []MachineTransition{}, States: []MachineStateFacts{}, TerminalFailures: []MachineRowContribution{}, Requirements: []MachineRowContribution{}, Evidence: plan.Evidence, Limitations: plan.Limitations}
	}
	return &identity
}

func allocationDelta(units int, fullBytes, fullMallocs, baseBytes, baseMallocs uint64) machineMeasure {
	measure := machineMeasure{units: units}
	if fullBytes > baseBytes {
		measure.bytes = fullBytes - baseBytes
	}
	if fullMallocs > baseMallocs {
		measure.mallocs = fullMallocs - baseMallocs
	}
	return measure
}

// The round-5 residuals at their reviewed shapes.
var (
	// A five-helper chain forwarding the state refuses with EF142 inside the
	// first statement; 20,000 let bindings follow it.
	machineRefusalThenLets = machineShape{states: 1, events: 1, helpers: 5, tails: 1, lets: 20000}
	// Two 131,072-byte failure labels declared by a step that only fails.
	machineLongFailureLabels = machineShape{states: 1, events: 1, failures: 2, failureName: 1 << 17, failBody: true}
	// Outer<i64> through a generic record with a 131,072-byte name, the
	// output of 256 declarations.
	machineGenericPayloadLayouts = machineShape{states: 1, events: 1, generic: true, genericName: 1 << 17, declarations: 256}
	// Outer<P<…>> over a 400-node argument tree, with 3,000 fields of type
	// L<T>: substitution validates the argument once per field.
	machineTemplateArgumentWalks = machineShape{states: 1, events: 1, generic: true, genericFields: 3000, genericArg: 3}
	// Effect<…> nested 60 deep around a 32,768-byte record name: every layer
	// of the display copies the text below it.
	machineNestedRecipeDisplay = machineShape{states: 1, events: 1, recipe: 60, recipeName: 32767}
)

// Round-5 residual 1: a refusal ends the block. The walker charges each
// statement's scope entry before inserting it and stops at the refusal, so
// the bindings after it are neither walked nor retained.
func TestMachineRefusalStopsTheBlock(t *testing.T) {
	r, measures := measureMachineAnalysis(t, machineRefusalThenLets.source())
	assertMachineAllocationCharged(t, "refusal then lets", measures)
	if !slices.Equal(machineCodes(r), []string{machineBudgetCode}) {
		t.Fatalf("codes = %v", machineCodes(r))
	}
}

// Round-5 residual 2: projecting a function reuses its rows' retained
// canonical IDs and charges the contract's text, labels included, before the
// checker builds its public ID.
func TestMachineLongFailureLabelsAreCharged(t *testing.T) {
	r, measures := measureMachineAnalysis(t, machineLongFailureLabels.source())
	assertMachineAllocationCharged(t, "long failure labels", measures)
	// The labels are shared and their row ID retained, so the analysis never
	// copies one, whatever it charged for them.
	if bytes := measures[len(measures)-1].bytes; bytes >= 1<<17 {
		t.Fatalf("analysis allocated %d bytes: a 131,072-byte label was copied", bytes)
	}
	plan := r.FindMachine(fmt.Sprintf("M%d", 0))
	if plan == nil || len(plan.Transitions) != 0 || len(plan.TerminalFailures) != 2 {
		t.Fatalf("zero-transition failing plan: %v", machineCodes(r))
	}
	if contract := plan.Functions[0].Contract; contract.FailureRow == "" || contract.FailureRow != rowNodeIDForLabels(contract.Errors) {
		t.Fatalf("failure row ID %q is not the canonical ID of its labels", contract.FailureRow)
	}
}

// Round-5 residual 3: an application's layout is resolved once per module,
// charged by the interning keys substitution builds for its declared field
// types, so 256 declarations do not rebuild a 131,072-byte key each.
func TestMachineGenericLayoutsAreChargedAndShared(t *testing.T) {
	r, measures := measureMachineAnalysis(t, machineGenericPayloadLayouts.source())
	assertMachineAllocationCharged(t, "generic payload layouts", measures)
	if len(r.Machines) != 256 {
		t.Fatalf("%d of 256 declarations planned: %v", len(r.Machines), machineCodes(r))
	}
	// One declaration: the shared layout's substituted key text is the only
	// large allocation, with no other declaration's charges to cover it.
	single := machineGenericPayloadLayouts
	single.declarations = 1
	r, measures = measureMachineAnalysis(t, single.source())
	assertMachineAllocationCharged(t, "one generic payload layout", measures)
	if len(r.Machines) != 1 {
		t.Fatalf("one generic payload layout refused: %v", machineCodes(r))
	}
}

// Round-1 review M1: substitution validates the template argument afresh for
// every field (templateDataArgument builds a memo over the argument's nodes),
// so the layout charges the argument's distinct nodes once per field before
// the checker instantiates them. 3,000 fields over a 400-node argument are
// refused before the walks; a modest layout is still planned.
func TestMachineTemplateArgumentWalksAreCharged(t *testing.T) {
	r, measures := measureMachineAnalysis(t, machineTemplateArgumentWalks.source())
	assertMachineAllocationCharged(t, "template argument walks", measures)
	if !slices.Equal(machineCodes(r), []string{machineBudgetCode}) {
		t.Fatalf("codes = %v", machineCodes(r))
	}
	modest := machineShape{states: 1, events: 1, generic: true, genericFields: 64, genericArg: 2}
	r, measures = measureMachineAnalysis(t, modest.source())
	assertMachineAllocationCharged(t, "modest template argument walks", measures)
	if len(r.Machines) != 1 {
		t.Fatalf("64 fields over a 57-node argument refused: %v", machineCodes(r))
	}
}

// Round-1 review M3: a recipe output is refused (EF141) after its display is
// built for the diagnostic, and every Effect<…> layer copies the display
// below it. The display is charged by that build cost before it is built, so
// 60 layers over a 32,768-byte name pay for the copies and are still
// reported.
func TestMachineNestedRecipeDisplayIsCharged(t *testing.T) {
	r, measures := measureMachineAnalysis(t, machineNestedRecipeDisplay.source())
	assertMachineAllocationCharged(t, "nested recipe display", measures)
	if !slices.Equal(machineCodes(r), []string{machineAdmissionCode}) {
		t.Fatalf("codes = %v", machineCodes(r))
	}
}
