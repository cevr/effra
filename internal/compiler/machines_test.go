package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func machineCodes(r *Result) []string {
	codes := []string{}
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

// The static Stay same-tag rule, ported from the 2026-10-07 machine admission
// probes onto the declaration. Each program is real .ef source; the
// expectation is the exact diagnostic code list.
func TestMachineStaySameTagRule(t *testing.T) {
	expected := map[string][]string{
		"stay-a01-reject-idle-stay-active.ef":    {"EF139"},
		"stay-a02-accept-stay-state.ef":          {},
		"stay-a03-accept-same-variant.ef":        {},
		"stay-a04-reject-helper-literal.ef":      {"EF139"},
		"stay-a05-accept-helper-normalize.ef":    {},
		"stay-a06-accept-dynamic-payload.ef":     {},
		"stay-a07-reject-step-helper.ef":         {"EF139"},
		"stay-a08-reject-aliased-import.ef":      {"EF139"},
		"stay-a09-reject-user-step-lookalike.ef": {"EF140"},
		"stay-a10-alias-state.ef":                {"EF139"},
		"stay-a11-shadowed-state-name.ef":        {},
		"stay-a12-or-patterns.ef":                {"EF139", "EF139"},
		"stay-a13-helper-state-second.ef":        {"EF139"},
		"stay-a14-reject-visible-if-path.ef":     {"EF139"},
		"stay-a15-reject-effectful-step.ef":      {"EF139"},
		// The spike's "only S-typed parameter" heuristic rejected this helper;
		// the declaration names the current state, so it is accepted.
		"stay-a16-accept-helper-other-state.ef": {},
		"stay-a17-reject-let-bound-stay.ef":     {"EF139"},
		"stay-a18-or-pattern-idle-active.ef":    {"EF139"},
		// Negative control: the same body bound by no declaration makes no
		// static claim; the declaration owns the rule.
		"stay-a19-unbound-function-unchecked.ef": {},
	}
	files, err := filepath.Glob(filepath.Join("testdata", "machines", "stay-*.ef"))
	if err != nil || len(files) != len(expected) {
		t.Fatalf("probe files = %d (%v), expected %d", len(files), err, len(expected))
	}
	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want, ok := expected[name]
			if !ok {
				t.Fatalf("no expectation for %s", name)
			}
			r := Compile(string(source))
			if got := machineCodes(r); !slices.Equal(got, want) {
				t.Fatalf("codes = %v, want %v: %+v", got, want, r.Diagnostics)
			}
			// Every Stay finding is anchored at the Stay variant token, or at
			// the caller's call of the first helper that contains the Stay.
			for _, d := range r.Diagnostics {
				if d.Code != "EF139" {
					continue
				}
				anchor := string(source)[d.Span.Offset : d.Span.Offset+d.Span.Length]
				if _, chain, ok := strings.Cut(d.Message, "(through helper "); ok {
					helper, _, _ := strings.Cut(strings.TrimSuffix(chain, ")"), " -> ")
					// A Stay in the step whose value comes from a helper stays
					// at the Stay; a Stay inside a helper anchors at its call.
					if anchor != helper && anchor != "Stay" {
						t.Fatalf("helper finding anchored at %q, want %q or Stay", anchor, helper)
					}
				} else if anchor != "Stay" {
					t.Fatalf("Stay finding anchored at %q", anchor)
				}
			}
			if len(want) == 0 && strings.Contains(string(source), "machine Session") && r.FindMachine("Session") == nil {
				t.Fatal("accepted declaration published no plan")
			}
		})
	}
}

func TestMachineStayDiagnosticNamesTheNarrowingAndHelper(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "machines", "stay-a07-reject-step-helper.ef"))
	if err != nil {
		t.Fatal(err)
	}
	r := Compile(string(source))
	if len(r.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", r.Diagnostics)
	}
	d := r.Diagnostics[0]
	if !strings.Contains(d.Message, "narrows the state to SessionState.Idle but Stay constructs SessionState.Active") || !strings.Contains(d.Message, "through helper stayActive") {
		t.Fatalf("message = %q", d.Message)
	}
	// The finding is reported at the call inside the bound step, not inside
	// the helper, whose own body is valid for other callers.
	if got := string(source)[d.Span.Offset : d.Span.Offset+d.Span.Length]; got != "stayActive" {
		t.Fatalf("anchor = %q", got)
	}
}

const machinePrelude = `import Machines "effra/machines"
enum Light { Red  Green { cars: i64 } }
enum Press { Walk  Wait }
`

const lightStepSource = `fn lightStep(state: Light, event: Press) -> Machines.Step<Light, void> {
    match state, event {
        Light.Red, Press.Walk => Machines.Step<Light, void>.Go(Light.Green { cars: 0 })
        Light.Green, Press.Walk => Machines.Step<Light, void>.Go(Light.Red {})
        Light.Red | Light.Green, Press.Wait => Machines.Step<Light, void>.Ignore {}
    }
}
`

// Each declaration contract violation and not-yet-admitted capability is an
// isolated diagnostic; none of these programs publishes a plan.
func TestMachineDeclarationDiagnostics(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   []string
		text   string
	}{
		{"valid", machinePrelude + lightStepSource + "machine Lights { initial Light.Red {}; step lightStep }", []string{}, ""},
		{"missing step", machinePrelude + "machine Lights { initial Light.Red {} }", []string{"EF140"}, "requires a step binding"},
		{"missing initial", machinePrelude + lightStepSource + "machine Lights { step lightStep }", []string{"EF140"}, "requires an initial state"},
		{"unknown step", machinePrelude + "machine Lights { initial Light.Red {}; step nowhere }", []string{"EF140"}, "unknown module function nowhere"},
		{"step arity", machinePrelude + "fn one(state: Light) -> Machines.Step<Light, void> { Machines.Step<Light, void>.Ignore {} }\nmachine Lights { initial Light.Red {}; step one }", []string{"EF140"}, "must take (state, event) parameters"},
		{"step result", machinePrelude + "fn plain(state: Light, event: Press) -> Light { state }\nmachine Lights { initial Light.Red {}; step plain }", []string{"EF140"}, "must return the bundled effra/machines Step"},
		{"state first", machinePrelude + "fn flipped(event: Press, state: Light) -> Machines.Step<Light, void> { Machines.Step<Light, void>.Ignore {} }\nmachine Lights { initial Light.Red {}; step flipped }", []string{"EF140"}, "takes the current state first"},
		{"event is state", machinePrelude + "fn same(state: Light, other: Light) -> Machines.Step<Light, void> { Machines.Step<Light, void>.Ignore {} }\nmachine Lights { initial Light.Red {}; step same }", []string{"EF140"}, "must be distinct types"},
		{"event not enum", machinePrelude + "fn text(state: Light, event: string) -> Machines.Step<Light, void> { Machines.Step<Light, void>.Ignore {} }\nmachine Lights { initial Light.Red {}; step text }", []string{"EF140"}, "must be a closed, non-generic enum"},
		{"initial wrong type", machinePrelude + lightStepSource + "machine Lights { initial Press.Walk {}; step lightStep }", []string{"EF140"}, "must be a Light value"},
		{"initial not literal", machinePrelude + lightStepSource + "fn start() -> Light { Light.Red {} }\nmachine Lights { initial start(); step lightStep }", []string{"EF140"}, "literal Light constructor"},
		{"initial dynamic payload", machinePrelude + lightStepSource + "fn count() -> i64 { 3 }\nmachine Lights { initial Light.Green { cars: count() }; step lightStep }", []string{"EF140"}, "static payload"},
		{"enter not admitted", machinePrelude + lightStepSource + "fn enterLight(state: Light) -> Light { state }\nmachine Lights { initial Light.Red {}; step lightStep; enter enterLight }", []string{"EF141"}, "entry work is not yet admitted"},
		{"complete without enter", machinePrelude + lightStepSource + "machine Lights { initial Light.Red {}; step lightStep; complete lightStep }", []string{"EF140", "EF140"}, "requires an enter binding"},
		{"outcome forging", machinePrelude + lightStepSource + "fn enterLight(state: Light) -> Light { state }\nmachine Lights { initial Light.Red {}; step lightStep; enter enterLight; complete lightStep }", []string{"EF141", "EF140"}, "cannot forge work completion"},
		{"machine as value", machinePrelude + lightStepSource + "machine Lights { initial Light.Red {}; step lightStep }\nfn spawn() -> void { let handle = Lights\nvoid }", []string{"EF141"}, "is a declaration, not a value"},
		{"callable payload", `import Machines "effra/machines"
enum Job { Idle  Queued { work: fn() -> i64 } }
enum Cmd { Run }
fn jobStep(state: Job, event: Cmd) -> Machines.Step<Job, void> { Machines.Step<Job, void>.Ignore {} }
machine Jobs { initial Job.Idle {}; step jobStep }`, []string{"EF141"}, "carries a function or effect recipe"},
		{"duplicate name", machinePrelude + lightStepSource + "machine lightStep { initial Light.Red {}; step lightStep }", []string{"EF101"}, "duplicate declaration lightStep"},
		{"statechart word", machinePrelude + lightStepSource + "machine Lights { initial Light.Red {}; states { } }", []string{"EF002"}, "machine states is unsupported"},
		{"gen_statem word", machinePrelude + lightStepSource + "machine Lights { initial Light.Red {}; step lightStep; postpone lightStep }", []string{"EF002"}, "machine postpone is unsupported"},
		{"duplicate binding", machinePrelude + lightStepSource + "machine Lights { initial Light.Red {}; step lightStep; step lightStep }", []string{"EF002"}, "duplicate machine step binding"},
		{"inline behavior", machinePrelude + lightStepSource + "machine Lights { initial Light.Red {}; step lightStep(1) }", []string{"EF002"}, "names a module function"},
		{"type parameters", machinePrelude + lightStepSource + "machine Lights<T> { initial Light.Red {}; step lightStep }", []string{"EF002"}, "take no type parameters"},
		// Ordinary checking owns the step body: a missing pair, a pure step
		// executing an effect and an undeclared failure stay ordinary diagnostics.
		{"missing pair", machinePrelude + `fn lightStep(state: Light, event: Press) -> Machines.Step<Light, void> {
    match state, event {
        Light.Red, Press.Walk => Machines.Step<Light, void>.Go(Light.Green { cars: 0 })
        Light.Red | Light.Green, Press.Wait => Machines.Step<Light, void>.Ignore {}
    }
}
machine Lights { initial Light.Red {}; step lightStep }`, []string{"EF117"}, "missing match arm for Light.Green, Press.Walk"},
		{"pure step executes", machinePrelude + `service Sensor { effect fn busy() -> bool }
fn lightStep(state: Light, event: Press) -> Machines.Step<Light, void> {
    if run Sensor.busy() { Machines.Step<Light, void>.Reject {} } else { Machines.Step<Light, void>.Ignore {} }
}
machine Lights { initial Light.Red {}; step lightStep }`, nil, ""},
		{"undeclared failure", machinePrelude + `error Jammed
effect fn lightStep(state: Light, event: Press) -> Machines.Step<Light, void> {
    fail Jammed
}
machine Lights { initial Light.Red {}; step lightStep }`, nil, ""},
		{"missing service", machinePrelude + `service Sensor { effect fn busy() -> bool }
effect fn lightStep(state: Light, event: Press) -> Machines.Step<Light, void> raises { } uses { } {
    if run Sensor.busy() { Machines.Step<Light, void>.Reject {} } else { Machines.Step<Light, void>.Ignore {} }
}
machine Lights { initial Light.Red {}; step lightStep }`, []string{"EF108"}, "missing service requirements: Sensor"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(test.source)
			got := machineCodes(r)
			if test.want == nil {
				// Ordinary diagnostics only: none of the machine codes, and no plan.
				if len(got) == 0 || slices.ContainsFunc(got, func(code string) bool { return code >= "EF139" && code <= "EF142" }) {
					t.Fatalf("codes = %v: %+v", got, r.Diagnostics)
				}
			} else if !slices.Equal(got, test.want) {
				t.Fatalf("codes = %v, want %v: %+v", got, test.want, r.Diagnostics)
			}
			if test.text != "" && !slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool { return strings.Contains(d.Message, test.text) }) {
				t.Fatalf("no diagnostic mentions %q: %+v", test.text, r.Diagnostics)
			}
			if len(got) > 0 && r.FindMachine("Lights") != nil {
				t.Fatal("a rejected declaration published a plan")
			}
		})
	}
}

type transitionKey struct {
	decision, from, on, targets, precision string
	guarded                                bool
}

func transitionKeys(plan *MachinePlan) []transitionKey {
	keys := []transitionKey{}
	for _, t := range plan.Transitions {
		keys = append(keys, transitionKey{t.Decision, strings.Join(t.From, "|"), strings.Join(t.On, "|"), strings.Join(t.Targets, "|"), t.Precision, t.Guarded})
	}
	return keys
}

func compileExample(t *testing.T, name string) *Result {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	r := Compile(string(source))
	if !r.Checked {
		t.Fatalf("%s: %+v", name, r.Diagnostics)
	}
	return r
}

// Two unrelated programs: a pure search/reload controller and an effectful
// circuit breaker whose step consults a service.
func TestMachinePlansForUnrelatedPrograms(t *testing.T) {
	search := compileExample(t, "machine-search.ef").FindMachine("Search")
	if search == nil {
		t.Fatal("no Search plan")
	}
	if search.Initial.Variant != "Idle" || search.State.Name != "Query" || search.Event.Name != "QueryEvent" || search.Output.Type.Name != "i64" || !search.Terminal {
		t.Fatalf("search identity = %+v %+v %+v %+v", search.Initial, search.State, search.Event, search.Output.Type)
	}
	if len(search.Functions) != 1 || search.Functions[0].Kind != "pure" || len(search.TerminalFailures) != 0 || len(search.Requirements) != 0 {
		t.Fatalf("search functions = %+v", search.Functions)
	}
	wantSearch := []transitionKey{
		{"go", "Idle|Ready|Failed", "Submit", "Loading", "literal", false},
		{"stay", "Loading", "Submit", "Loading", "current", true},
		{"go", "Loading", "Submit", "Loading", "literal", true},
		{"go", "Loading", "Results", "Ready", "literal", false},
		{"go", "Loading", "Errored", "Failed", "literal", false},
		{"go", "Ready|Failed", "Reload", "Loading", "literal", false},
		{"ignore", "Idle|Loading", "Reload", "", "literal", false},
		{"ignore", "Idle|Ready|Failed", "Results|Errored", "", "literal", false},
		{"done", "Ready", "Close", "", "literal", false},
		{"reject", "Idle|Loading|Failed", "Close", "", "literal", false},
	}
	if got := transitionKeys(search); !slices.Equal(got, wantSearch) {
		t.Fatalf("search transitions =\n%v\nwant\n%v", got, wantSearch)
	}
	breaker := compileExample(t, "machine-breaker.ef").FindMachine("CircuitBreaker")
	if breaker == nil {
		t.Fatal("no CircuitBreaker plan")
	}
	step := breaker.Functions[0]
	if step.Kind != "effect" || !slices.Equal(step.Failures, []string{"ProbeUnavailable"}) || !slices.Equal(step.Services, []string{"Probe"}) {
		t.Fatalf("breaker step = %+v", step)
	}
	if len(breaker.TerminalFailures) != 1 || breaker.TerminalFailures[0].Name != "ProbeUnavailable" || len(breaker.Requirements) != 1 || breaker.Requirements[0].Name != "Probe" {
		t.Fatalf("breaker rows = %+v %+v", breaker.TerminalFailures, breaker.Requirements)
	}
	keys := transitionKeys(breaker)
	// A Go target built by a helper is still a literal tag; the decision under
	// the effectful probe condition is guarded.
	for _, want := range []transitionKey{
		{"go", "Closed", "Failure", "Open", "literal", true},
		{"go", "Open", "Retry", "HalfOpen", "literal", true},
		{"stay", "Open", "Retry", "Open", "current", true},
		{"done", "Closed|Open|HalfOpen", "Retire", "", "literal", false},
	} {
		if !slices.Contains(keys, want) {
			t.Fatalf("breaker transitions %v lack %v", keys, want)
		}
	}
	for _, state := range append(search.States, breaker.States...) {
		if !state.Enterable {
			t.Fatalf("state %s is unexpectedly not enterable", state.Variant)
		}
	}
}

// Enterable is a sound over-approximation: a state with no possible incoming
// edge is reported, a dynamic Go target makes every state possible, and the
// fact never claims reachability.
func TestMachinePlanEnterableAndConservativeTargets(t *testing.T) {
	base := `import Machines "effra/machines"
enum Door { Shut  Open  Broken }
enum Push { In  Out { to: Door } }
fn doorStep(state: Door, event: Push) -> Machines.Step<Door, void> {
    match state, event {
        Door.Shut, Push.In => Machines.Step<Door, void>.Go(Door.Open {})
        Door.Open, Push.In => Machines.Step<Door, void>.Go(Door.Shut {})
        Door.Broken, Push.In => Machines.Step<Door, void>.Ignore {}
        Door.Shut | Door.Open | Door.Broken, Push.Out { to } => OUT
    }
}
machine Doors { initial Door.Shut {}; step doorStep }
`
	enterable := func(source string) map[string]bool {
		r := Compile(source)
		plan := r.FindMachine("Doors")
		if plan == nil {
			t.Fatalf("no plan: %+v", r.Diagnostics)
		}
		out := map[string]bool{}
		for _, state := range plan.States {
			out[state.Variant] = state.Enterable
		}
		return out
	}
	literal := enterable(strings.Replace(base, "OUT", "Machines.Step<Door, void>.Reject {}", 1))
	if !literal["Shut"] || !literal["Open"] || literal["Broken"] {
		t.Fatalf("literal enterable = %v", literal)
	}
	r := Compile(strings.Replace(base, "OUT", "Machines.Step<Door, void>.Go(to)", 1))
	plan := r.FindMachine("Doors")
	if plan == nil {
		t.Fatalf("dynamic: %+v", r.Diagnostics)
	}
	last := plan.Transitions[len(plan.Transitions)-1]
	if last.Decision != "go" || last.Precision != "conservative" || strings.Join(last.Targets, "|") != "Shut|Open|Broken" {
		t.Fatalf("dynamic target = %+v", last)
	}
	if dynamic := enterable(strings.Replace(base, "OUT", "Machines.Step<Door, void>.Go(to)", 1)); !dynamic["Broken"] {
		t.Fatalf("dynamic enterable = %v", dynamic)
	}
	// A Step value the walk cannot see into is an "any" decision.
	opaque := strings.Replace(base, "OUT", "pick(to)", 1) + "fn pick(to: Door) -> Machines.Step<Door, void> { pickAgain(to) }\nfn pickAgain(to: Door) -> Machines.Step<Door, void> { pick(to) }\n"
	r = Compile(opaque)
	if r.FindMachine("Doors") == nil {
		t.Fatalf("opaque: %+v", r.Diagnostics)
	}
	found := false
	for _, t := range r.FindMachine("Doors").Transitions {
		found = found || t.Decision == "any" && t.Precision == "conservative"
	}
	if !found {
		t.Fatalf("recursive helper did not produce a conservative decision: %+v", r.FindMachine("Doors").Transitions)
	}
}

func TestMachineInspectionAndGraphView(t *testing.T) {
	r := compileExample(t, "machine-search.ef")
	response, err := r.MachineInspection("Search", "machine-search.ef")
	if err != nil {
		t.Fatal(err)
	}
	if response["machine"].(*MachinePlan).ID != machineID("Search") || response["file"] != "machine-search.ef" {
		t.Fatalf("inspection = %v", response)
	}
	if _, err := r.MachineInspection("Nope", "x.ef"); err == nil {
		t.Fatal("unknown machine inspected")
	}
	if _, ok := r.CheckResponse()["machines"]; !ok {
		t.Fatal("check response omits machine plans")
	}
	request, err := ParseGraphRequest(map[string]any{"kind": "machine"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := r.GraphView(request)
	if err != nil {
		t.Fatal(err)
	}
	relations := map[string]int{}
	for _, edge := range view.Edges {
		relations[edge.Data.Effra.Relation]++
		if edge.Data.Effra.Relation != "contains" && edge.Data.Effra.Relation != "initial" && edge.Data.Effra.Transition == nil {
			t.Fatalf("transition edge without transition facts: %+v", edge)
		}
	}
	// Search: 4 states contained; 1 initial; ignore/reject are self-loops on
	// every source state; done goes to the output node.
	want := map[string]int{"contains": 4, "initial": 1, "go": 8, "stay": 1, "ignore": 5, "reject": 3, "done": 1}
	encodedWant, _ := json.Marshal(want)
	encodedGot, _ := json.Marshal(relations)
	if string(encodedWant) != string(encodedGot) {
		t.Fatalf("relations = %s, want %s", encodedGot, encodedWant)
	}
	if view.Data.Effra.Completeness.Semantics != machinePlanEvidence || len(view.Nodes) != 6 {
		t.Fatalf("view = %+v nodes=%d", view.Data.Effra.Completeness, len(view.Nodes))
	}
	focused, err := ParseGraphRequest(map[string]any{"kind": "machine", "focus": graphTupleID("machine-state", r.FindMachine("Search").ID, "Ready"), "edgeKinds": []any{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	if view, err = r.GraphView(focused); err != nil || len(view.Edges) != 4 {
		t.Fatalf("focused view = %v, %v", view, err)
	}
	if _, err := ParseGraphRequest(map[string]any{"kind": "machine", "edgeKinds": []any{"merges"}}); err == nil {
		t.Fatal("layer relation admitted for machine kind")
	}
	if _, err := ParseGraphRequest(map[string]any{"kind": "actor"}); err == nil || err.(*GraphRefusal).Code != GraphRefusalKindUnavailable {
		t.Fatalf("actor kind = %v", err)
	}
}

// Inspection is static: a step whose evaluation would never terminate is
// still planned, because planning never evaluates it.
func TestMachinePlanningNeverEvaluatesSteps(t *testing.T) {
	r := Compile(machinePrelude + `fn spin(state: Light, event: Press) -> Machines.Step<Light, void> { spin(state, event) }
machine Lights { initial Light.Red {}; step spin }`)
	plan := r.FindMachine("Lights")
	if plan == nil {
		t.Fatalf("%+v", r.Diagnostics)
	}
	if len(plan.Transitions) != 1 || plan.Transitions[0].Decision != "any" {
		t.Fatalf("transitions = %+v", plan.Transitions)
	}
}

func TestMachineFormattingIsCanonical(t *testing.T) {
	source := machinePrelude + lightStepSource + "machine Lights {\ninitial Light.Green { cars: 1 }\n      step lightStep\n}\n"
	first, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if FormatterIdentity != "effra/formatter-15" || !strings.Contains(first.Text, "machine Lights {\n    initial Light.Green {\n        cars: 1\n    }\n    step lightStep\n}") {
		t.Fatalf("formatted:\n%s", first.Text)
	}
	second, err := FormatSource(first.Text)
	if err != nil || second.Text != first.Text {
		t.Fatalf("not idempotent: %v\n%s", err, second.Text)
	}
	if r := Compile(first.Text); r.FindMachine("Lights") == nil {
		t.Fatalf("formatted source lost its plan: %+v", r.Diagnostics)
	}
}
