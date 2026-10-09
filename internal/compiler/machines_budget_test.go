package compiler

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Exhausting a depth or budget in the machine checker refuses the declaration
// (EF142) or reaches the exact verdict; it never classifies what it did not
// inspect as admitted. These controls sit on both sides of each former cutoff.

func aliasedStaySource(aliases int, stay string) string {
	var b strings.Builder
	b.WriteString(`import Machines "effra/machines"
enum S { Idle  Active { n: i64 } }
enum E { Ping }
fn step(state: S, event: E) -> Machines.Step<S, void> {
    let a0 = state
`)
	for i := 1; i < aliases; i++ {
		fmt.Fprintf(&b, "    let a%d = a%d\n", i, i-1)
	}
	fmt.Fprintf(&b, `    match a%d, event {
        S.Idle, E.Ping => Machines.Step<S, void>.Stay(%s)
        S.Active, E.Ping => Machines.Step<S, void>.Stay(state)
    }
}
machine M { initial S.Idle {}; step step }
`, aliases-1, stay)
	return b.String()
}

func TestMachineStayFollowsAliasChainsOfAnyLength(t *testing.T) {
	for _, aliases := range []int{1, 16, 17, 64} {
		wrong := Compile(aliasedStaySource(aliases, "S.Active { n: 1 }"))
		if got := machineCodes(wrong); !slices.Equal(got, []string{"EF139"}) || wrong.FindMachine("M") != nil {
			t.Fatalf("%d aliases: wrong-tag Stay codes = %v: %+v", aliases, got, wrong.Diagnostics)
		}
		same := Compile(aliasedStaySource(aliases, "S.Idle {}"))
		plan := same.FindMachine("M")
		if plan == nil {
			t.Fatalf("%d aliases: same-tag Stay refused: %+v", aliases, same.Diagnostics)
		}
		// The alias still narrows: the Idle arm's Stay is a literal-precision
		// transition from Idle only.
		if !slices.ContainsFunc(plan.Transitions, func(tr MachineTransition) bool {
			return tr.Decision == "stay" && slices.Equal(tr.From, []string{"Idle"})
		}) {
			t.Fatalf("%d aliases: narrowing lost: %+v", aliases, plan.Transitions)
		}
	}
}

func nestedOutputSource(depth int, leaf string) string {
	var b strings.Builder
	b.WriteString(`import Machines "effra/machines"
enum S { Idle }
enum E { Ping }
`)
	fmt.Fprintf(&b, "record R0 { leaf: %s }\n", leaf)
	for i := 1; i <= depth; i++ {
		fmt.Fprintf(&b, "record R%d { inner: R%d }\n", i, i-1)
	}
	fmt.Fprintf(&b, `fn step(state: S, event: E) -> Machines.Step<S, R%d> {
    Machines.Step<S, R%d>.Ignore {}
}
machine M { initial S.Idle {}; step step }
`, depth, depth)
	return b.String()
}

func TestMachinePayloadAdmissionHasNoDepthCutoff(t *testing.T) {
	for _, depth := range []int{1, 32, 33, 80} {
		behavior := Compile(nestedOutputSource(depth, "fn() -> i64"))
		if got := machineCodes(behavior); !slices.Equal(got, []string{"EF141"}) || behavior.FindMachine("M") != nil {
			t.Fatalf("depth %d: callable output codes = %v: %+v", depth, got, behavior.Diagnostics)
		}
		if !strings.Contains(behavior.Diagnostics[0].Message, ".inner.leaf carries a function") {
			t.Fatalf("depth %d: refusal does not name the path: %s", depth, behavior.Diagnostics[0].Message)
		}
		data := Compile(nestedOutputSource(depth, "i64"))
		if data.FindMachine("M") == nil {
			t.Fatalf("depth %d: data-only output refused: %+v", depth, data.Diagnostics)
		}
	}
}

// A shared diamond is inspected once per type, so wide sharing costs linear
// work and still finds a callable at the bottom.
func TestMachinePayloadAdmissionIsLinearInSharedTypes(t *testing.T) {
	source := func(leaf string) string {
		var b strings.Builder
		b.WriteString("import Machines \"effra/machines\"\nenum S { Idle }\nenum E { Ping }\n")
		fmt.Fprintf(&b, "record D0 { leaf: %s }\n", leaf)
		for i := 1; i <= 40; i++ {
			fmt.Fprintf(&b, "record D%d { left: D%d  right: D%d }\n", i, i-1, i-1)
		}
		b.WriteString("fn step(state: S, event: E) -> Machines.Step<S, D40> { Machines.Step<S, D40>.Ignore {} }\nmachine M { initial S.Idle {}; step step }\n")
		return b.String()
	}
	if r := Compile(source("i64")); r.FindMachine("M") == nil {
		t.Fatalf("2^40 paths of data refused: %+v", r.Diagnostics)
	}
	if r := Compile(source("fn() -> i64")); !slices.Equal(machineCodes(r), []string{"EF141"}) {
		t.Fatalf("shared callable admitted: %+v", r.Diagnostics)
	}
}

func helperChainSource(helpers int, narrowed bool, bottom string) string {
	var b strings.Builder
	b.WriteString(`import Machines "effra/machines"
enum S { Idle  Active }
enum E { Ping }
`)
	if narrowed {
		b.WriteString(`fn step(state: S, event: E) -> Machines.Step<S, void> {
    match state {
        S.Idle => h1(state)
        S.Active => Machines.Step<S, void>.Ignore {}
    }
}
`)
		for i := 1; i < helpers; i++ {
			fmt.Fprintf(&b, "fn h%d(state: S) -> Machines.Step<S, void> { h%d(state) }\n", i, i+1)
		}
		fmt.Fprintf(&b, "fn h%d(state: S) -> Machines.Step<S, void> { %s }\n", helpers, bottom)
	} else {
		b.WriteString("fn step(state: S, event: E) -> Machines.Step<S, void> { h1(1) }\n")
		for i := 1; i < helpers; i++ {
			fmt.Fprintf(&b, "fn h%d(n: i64) -> Machines.Step<S, void> { h%d(n) }\n", i, i+1)
		}
		fmt.Fprintf(&b, "fn h%d(n: i64) -> Machines.Step<S, void> { %s }\n", helpers, bottom)
	}
	b.WriteString("machine M { initial S.Idle {}; step step }\n")
	return b.String()
}

func TestMachineHelperDepthRefusesInsteadOfAdmitting(t *testing.T) {
	wrongStay := "Machines.Step<S, void>.Stay(S.Active {})"
	// Within the depth the wrong-tag Stay at the bottom is found.
	if r := Compile(helperChainSource(maxMachineHelperDepth, true, wrongStay)); !slices.Equal(machineCodes(r), []string{"EF139"}) {
		t.Fatalf("in-depth helper chain: %+v", r.Diagnostics)
	}
	// One helper deeper, under narrowing, the checker cannot judge the Stay and
	// refuses the declaration rather than admitting it unread.
	r := Compile(helperChainSource(maxMachineHelperDepth+1, true, wrongStay))
	if got := machineCodes(r); !slices.Equal(got, []string{"EF142"}) || r.FindMachine("M") != nil {
		t.Fatalf("over-depth narrowed chain codes = %v: %+v", got, r.Diagnostics)
	}
	if !strings.Contains(r.Diagnostics[0].Message, "h1 -> h2 -> h3 -> h4 -> h5") {
		t.Fatalf("refusal does not name the chain: %s", r.Diagnostics[0].Message)
	}
	// An unnarrowed chain that never receives the state makes every Stay
	// dynamic, so the conservative decision is exact and the plan publishes.
	plan := Compile(helperChainSource(maxMachineHelperDepth+1, false, "Machines.Step<S, void>.Stay(S.Active {})")).FindMachine("M")
	if plan == nil || len(plan.Transitions) != 1 || plan.Transitions[0].Decision != "any" || plan.Transitions[0].Precision != machineTargetConservative {
		t.Fatalf("unnarrowed over-depth chain: %+v", plan)
	}
}

func wideMachineSource(variants int) string {
	var b strings.Builder
	b.WriteString("import Machines \"effra/machines\"\nenum W {")
	for i := 0; i < variants; i++ {
		fmt.Fprintf(&b, " V%d", i)
	}
	b.WriteString(" }\nenum E { Ping }\nfn spin(state: W, event: E) -> Machines.Step<W, void> { spin(state, event) }\nmachine Wide { initial W.V0 {}; step spin }\n")
	return b.String()
}

// One conservative transition over n states projects n*n edges. The graph
// stops at the first refused admission instead of visiting every pair.
func TestMachineGraphStopsAtTheWorkLimit(t *testing.T) {
	r := Compile(wideMachineSource(1000))
	plan := r.FindMachine("Wide")
	if plan == nil || len(plan.Transitions) != 1 || len(plan.Transitions[0].From) != 1000 || len(plan.Transitions[0].Targets) != 1000 {
		t.Fatalf("wide plan: %+v", r.Diagnostics)
	}
	facts := newGraphFacts(GraphKindMachine, defaultGraphViewLimits)
	if machineGraphPlan(facts, plan) || facts.err == nil {
		t.Fatal("a million-edge expansion was admitted")
	}
	if facts.refusals != 1 || facts.work > defaultGraphViewLimits.Work {
		t.Fatalf("expansion continued past the limit: %d refused admissions, work %d", facts.refusals, facts.work)
	}
	request, err := ParseGraphRequest(map[string]any{"kind": "machine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.GraphView(request); err == nil || !strings.Contains(err.Error(), string(GraphRefusalWorkLimit)) {
		t.Fatalf("graph view error = %v", err)
	}
}

// The plan's own size is charged: each transition lists its From and Targets,
// so a declaration whose plan would outgrow the budget is refused with EF142.
// Over 2000 states one conservative tail costs 4000 units: 10 tails fit in the
// 65536-unit budget and 20 do not.
func wideTailsSource(tails int) string {
	var b strings.Builder
	b.WriteString("import Machines \"effra/machines\"\nenum W {")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, " V%d", i)
	}
	b.WriteString(" }\nenum E { Ping }\nfn spin(state: W, event: E, n: i64) -> Machines.Step<W, void> {\n")
	for i := 0; i < tails-1; i++ {
		fmt.Fprintf(&b, "    if n == %d { spin(state, event, %d) } else {\n", i, i)
	}
	b.WriteString("    spin(state, event, 0)\n")
	b.WriteString(strings.Repeat("    }\n", tails-1))
	b.WriteString("}\nfn step(state: W, event: E) -> Machines.Step<W, void> { spin(state, event, 0) }\nmachine Wide { initial W.V0 {}; step step }\n")
	return b.String()
}

func TestMachinePlanSizeIsCharged(t *testing.T) {
	if r := Compile(wideTailsSource(10)); r.FindMachine("Wide") == nil {
		t.Fatalf("10 wide tails refused: %v", machineCodes(r))
	}
	r := Compile(wideTailsSource(20))
	if got := machineCodes(r); !slices.Equal(got, []string{"EF142"}) || r.FindMachine("Wide") != nil {
		t.Fatalf("20 wide tails codes = %v", got)
	}
}

// A refused check response removes every table that references projected
// types together, so no published value keeps a dangling type reference.
func TestMachineCheckResponseRefusalKeepsReferenceClosure(t *testing.T) {
	r := Compile(machinePrelude + lightStepSource + "machine Lights { initial Light.Red {}; step lightStep }\n")
	if r.FindMachine("Lights") == nil {
		t.Fatal(r.Diagnostics)
	}
	usage, ok := r.CheckResponse()["typeProjectionUsage"].(ProjectionUsage)
	if !ok || usage.ResponseBytes == 0 {
		t.Fatal("complete response has no measured size")
	}
	// The boundary: the largest budget that refuses the complete envelope
	// (its own size field settles within a few bytes). The metadata tables
	// must go, while the reduced envelope still fits.
	var response map[string]any
	for short := 1; short <= 64; short++ {
		limits := r.projectionLimits()
		limits.ResponseBytes = usage.ResponseBytes - short
		r.TypeProjectionLimits = limits
		if response = r.CheckResponse(); response["typeProjectionComplete"] == false {
			break
		}
	}
	if response["typeProjectionComplete"] != false || response["diagnosticsUnavailable"] != nil {
		t.Fatalf("expected the reduced refusal envelope: %v", response["typeProjectionError"])
	}
	if _, ok := response["machines"]; ok {
		t.Fatal("refused response kept machine plans")
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	var dangling []string
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, child := range v {
				if text, ok := child.(string); ok && key == "ref" && strings.HasPrefix(text, "t:") {
					dangling = append(dangling, text)
				}
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(decoded)
	if len(dangling) > 0 {
		t.Fatalf("refused response references %d types without a types table: %v", len(dangling), dangling[:1])
	}
}

// Recursion is analysed per context. A body first walked with the state
// unnarrowed admits a literal Stay there; reaching it again through a
// recursive call under a narrowed state is a new obligation, so it is walked
// again, never assumed covered.
func recursiveStaySource(stay string) string {
	return `import Machines "effra/machines"
enum S { Idle  Active }
enum E { Start  Stop }
fn step(state: S, event: E) -> Machines.Step<S, void> {
    match event {
        E.Stop => Machines.Step<S, void>.Stay(` + stay + `)
        E.Start => match state {
            S.Idle => step(state, E.Stop {})
            S.Active => Machines.Step<S, void>.Ignore {}
        }
    }
}
machine M { initial S.Idle {}; step step }
`
}

func TestMachineStayRecursionIsContextSensitive(t *testing.T) {
	source := recursiveStaySource("S.Active {}")
	r := Compile(source)
	if got := machineCodes(r); !slices.Equal(got, []string{"EF139"}) || r.FindMachine("M") != nil {
		t.Fatalf("recursive wrong-tag Stay codes = %v: %+v", got, r.Diagnostics)
	}
	d := r.Diagnostics[0]
	if anchor := source[d.Span.Offset : d.Span.Offset+d.Span.Length]; anchor != "Stay" || !strings.Contains(d.Message, "narrows the state to S.Idle but Stay constructs S.Active") {
		t.Fatalf("finding anchored at %q: %s", anchor, d.Message)
	}
	// The recursion only ever reaches the Stop arm with the state Idle, so a
	// Stay keeping Idle is accepted.
	if r := Compile(recursiveStaySource("S.Idle {}")); r.FindMachine("M") == nil {
		t.Fatalf("recursive same-tag Stay refused: %+v", r.Diagnostics)
	}
	// Recursion in the context it was entered with is covered and terminates.
	same := Compile(`import Machines "effra/machines"
enum S { Idle  Active }
enum E { Ping }
fn step(state: S, event: E) -> Machines.Step<S, void> {
    match state {
        S.Idle => step(state, event)
        S.Active => Machines.Step<S, void>.Stay(state)
    }
}
machine M { initial S.Idle {}; step step }
`)
	if same.FindMachine("M") == nil {
		t.Fatalf("same-context recursion refused: %+v", same.Diagnostics)
	}
}

func recursiveCycleSource(length int) string {
	var b strings.Builder
	b.WriteString(`import Machines "effra/machines"
enum S { Idle  Active }
enum E { Ping }
fn step(state: S, event: E) -> Machines.Step<S, void> {
    match state {
        S.Idle => h0(state)
        S.Active => Machines.Step<S, void>.Ignore {}
    }
}
`)
	for i := 0; i < length; i++ {
		fmt.Fprintf(&b, "fn h%d(state: S) -> Machines.Step<S, void> { h%d(state) }\n", i, (i+1)%length)
	}
	b.WriteString("machine M { initial S.Idle {}; step step }\n")
	return b.String()
}

// A recursive cycle that closes within the helper depth is covered when it
// returns to its first context; one longer than the depth under a narrowed
// state cannot be checked and is refused, never admitted.
func TestMachineRecursiveCyclesTerminateOrRefuse(t *testing.T) {
	if r := Compile(recursiveCycleSource(3)); r.FindMachine("M") == nil {
		t.Fatalf("short recursive cycle refused: %+v", r.Diagnostics)
	}
	r := Compile(recursiveCycleSource(maxMachineHelperDepth + 2))
	if got := machineCodes(r); !slices.Equal(got, []string{"EF142"}) || r.FindMachine("M") != nil {
		t.Fatalf("long narrowed cycle codes = %v: %+v", got, r.Diagnostics)
	}
}

// Trigger lists are plan output too. A singleton state with 2000 events makes
// each unnarrowed Ignore tail list every event, about 2000 units. One helper
// of 20 tails fits the budget; two helpers (40 tails) do not.
func wideEventTailsSource(helpers int) string {
	var b strings.Builder
	b.WriteString("import Machines \"effra/machines\"\nenum S { Only }\nenum E {")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, " E%d", i)
	}
	b.WriteString(" }\nfn step(state: S, event: E) -> Machines.Step<S, void> {\n")
	for h := 0; h < helpers-1; h++ {
		fmt.Fprintf(&b, "    if true { tails%d(state, event, 0) } else {\n", h)
	}
	fmt.Fprintf(&b, "    tails%d(state, event, 0)\n%s}\n", helpers-1, strings.Repeat("    }\n", helpers-1))
	for h := 0; h < helpers; h++ {
		fmt.Fprintf(&b, "fn tails%d(state: S, event: E, n: i64) -> Machines.Step<S, void> {\n", h)
		for i := 0; i < 19; i++ {
			fmt.Fprintf(&b, "    if n == %d { Machines.Step<S, void>.Ignore {} } else {\n", i)
		}
		b.WriteString("    Machines.Step<S, void>.Ignore {}\n")
		b.WriteString(strings.Repeat("    }\n", 19))
		b.WriteString("}\n")
	}
	b.WriteString("machine Wide { initial S.Only {}; step step }\n")
	return b.String()
}

func TestMachineTriggerListsAreCharged(t *testing.T) {
	if r := Compile(wideEventTailsSource(1)); r.FindMachine("Wide") == nil {
		t.Fatalf("20 wide-event tails refused: %v", machineCodes(r))
	}
	r := Compile(wideEventTailsSource(2))
	if got := machineCodes(r); !slices.Equal(got, []string{"EF142"}) || r.FindMachine("Wide") != nil {
		t.Fatalf("40 wide-event tails codes = %v: %+v", got, r.Diagnostics)
	}
}
