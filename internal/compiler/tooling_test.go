package compiler

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLintUsesCheckedLexicalBindings(t *testing.T) {
	cases := []struct {
		name, source string
		warnings     int
	}{
		{"unused", `effect fn task() -> string { "ok" } effect fn main() -> void { let forgotten = task(); void }`, 1},
		{"branch use", `effect fn task() -> string { "ok" } effect fn main() -> string { let recipe = task(); if true {run recipe} else {""} }`, 0},
		{"scope use", `effect fn task() -> string { "ok" } effect fn main() -> string { let recipe = task(); scope {run recipe} }`, 0},
		{"independent bindings", `effect fn task() -> string { "ok" } effect fn a() -> string { let recipe = task(); run recipe } effect fn main() -> void {let recipe = task(); void}`, 1},
		{"acknowledged", `effect fn task() -> string { "ok" } effect fn main() -> void { let _ = task(); void }`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Compile(c.source)
			if !r.Checked {
				t.Fatal(r.Diagnostics)
			}
			normal, strict := r.Lint(false), r.Lint(true)
			if normal.Warnings != c.warnings || !normal.LintPassed || strict.LintPassed != (c.warnings == 0) {
				t.Fatalf("%+v %+v", normal, strict)
			}
		})
	}
	invalid := Compile(`effect fn task() -> void { void } effect fn main() -> void {task()}`)
	lint := invalid.Lint(false)
	if lint.Checked || lint.LintPassed || len(lint.LintDiagnostics) != 0 || len(lint.Diagnostics) == 0 {
		t.Fatal(lint)
	}
}
func TestLintProvisionAndTypeAt(t *testing.T) {
	source := `effect fn task() -> string {"ok"} effect fn main() -> string {run task().provide<Console>(Stdout)}`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	lint := r.Lint(true)
	if len(lint.LintDiagnostics) != 1 || lint.LintDiagnostics[0].Rule != "redundant-provision" || !lint.LintPassed {
		t.Fatal(lint)
	}
	query, err := r.TypeAt(strings.Index(source, "provide"))
	if err != nil || !query.Type.Effect || query.Type.Success != "string" || len(query.ExecutedRequirements) != 0 {
		t.Fatalf("%+v %v", query, err)
	}
	run, err := r.TypeAt(strings.Index(source, "run task"))
	if err != nil || run.Type.Effect {
		t.Fatalf("%+v %v", run, err)
	}
	for _, offset := range []int{-1, len(source), 0} {
		if _, err := r.TypeAt(offset); err == nil {
			t.Fatalf("accepted %d", offset)
		}
	}
}

func TestLintReasonedNextLineSuppressions(t *testing.T) {
	source := `effect fn task() -> string { "ok" }
effect fn main() -> void {
// effra-lint-disable-next-line unused-recipe -- intentionally deferred hook
let forgotten = task()
let alsoForgotten = task();
void}`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	lint := r.Lint(false)
	if !lint.LintPassed || len(lint.LintDiagnostics) != 1 {
		t.Fatalf("suppressed warning was not removed: %+v", lint)
	}
	remaining := lint.LintDiagnostics[0]
	if remaining.Rule != "unused-recipe" || remaining.Span.Line != 5 || remaining.Span.Offset != strings.Index(source, "let alsoForgotten") {
		t.Fatalf("wrong remaining advice: %+v", remaining)
	}
	if lint.Revision != r.Revision {
		t.Fatalf("lint revision drifted: %+v", lint)
	}
	strict := r.Lint(true)
	if strict.LintPassed || len(strict.LintDiagnostics) != 1 {
		t.Fatalf("remaining warning did not retain strict severity: %+v", strict)
	}

	stringDirective := `effect fn task() -> string { "ok" }
effect fn main() -> void {
let text = "// effra-lint-disable-next-line unused-recipe -- this is data"
let forgotten = task();
void}`
	stringResult := Compile(stringDirective)
	if !stringResult.Checked {
		t.Fatal(stringResult.Diagnostics)
	}
	stringLint := stringResult.Lint(false)
	if len(stringLint.LintDiagnostics) != 1 || stringLint.LintDiagnostics[0].Rule != "unused-recipe" {
		t.Fatalf("directive text in a string changed lint behavior: %+v", stringLint)
	}

	sameLine := `effect fn main() -> void {
// effra-lint-disable-next-line redundant-provision -- both nested boundaries are deliberate
run Console.log("x").provide<Console>(Stdout).provide<Console>(Stdout)
}`
	sameLineResult := Compile(sameLine)
	if !sameLineResult.Checked {
		t.Fatal(sameLineResult.Diagnostics)
	}
	if lint := sameLineResult.Lint(false); !lint.LintPassed || len(lint.LintDiagnostics) != 0 {
		t.Fatalf("one suppression did not cover all matching advice on its line: %+v", lint)
	}
}

func TestLintSuppressionValidation(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		message string
		line    int
	}{
		{
			"unknown rule",
			`effect fn main() -> void {
// effra-lint-disable-next-line future-rule -- waiting for a future rule
void}`,
			"unknown lint rule future-rule",
			2,
		},
		{
			"missing reason",
			`effect fn task() -> string { "ok" }
effect fn main() -> void {
// effra-lint-disable-next-line unused-recipe
let forgotten = task();
void}`,
			"must include a non-empty reason",
			3,
		},
		{
			"multiple rules",
			`effect fn main() -> void {
// effra-lint-disable-next-line unused-recipe redundant-provision -- one target is required
void}`,
			"must name exactly one lint rule",
			2,
		},
		{
			"unused",
			`effect fn main() -> void {
// effra-lint-disable-next-line unused-recipe -- no advice is on the next line
void}`,
			"unused lint suppression",
			2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(tc.source)
			if !r.Checked {
				t.Fatal(r.Diagnostics)
			}
			lint := r.Lint(false)
			if lint.LintPassed {
				t.Fatalf("invalid suppression passed: %+v", lint)
			}
			var diagnostic LintDiagnostic
			found := false
			for _, candidate := range lint.LintDiagnostics {
				if candidate.Code == "EFL004" {
					diagnostic = candidate
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing suppression diagnostic: %+v", lint)
			}
			if diagnostic.Code != "EFL004" || diagnostic.Severity != "error" || diagnostic.Span.Line != tc.line || !strings.Contains(diagnostic.Message, tc.message) {
				t.Fatalf("wrong suppression diagnostic: %+v", diagnostic)
			}
			if diagnostic.Span.Offset < 0 || diagnostic.Span.Length == 0 || diagnostic.Span.Offset >= len(tc.source) {
				t.Fatalf("invalid byte span: %+v", diagnostic.Span)
			}
		})
	}
}

func TestLintSuppressionCannotHideCompilerDiagnostics(t *testing.T) {
	source := `effect fn main() -> void {
// effra-lint-disable-next-line unused-recipe -- this must never hide a compiler error
run Console.log("x")
}`
	r := Compile(source)
	if r.Checked || !hasCode(r, "EF108") {
		t.Fatalf("compiler diagnostic was suppressed: %+v", r.Diagnostics)
	}
	// No rule ran over unchecked source, so the suppression is neither
	// applied nor unused: it was not evaluated.
	lint := r.Lint(false)
	if lint.Checked || lint.LintPassed || len(lint.Diagnostics) == 0 || lint.Errors != 0 || len(lint.LintDiagnostics) != 0 || len(lint.Suppressions) != 1 {
		t.Fatalf("unchecked source received suppressible lint semantics: %+v", lint)
	}
	want := []SuppressionStatus{{Rule: "unused-recipe", Status: SuppressionNotEvaluated, Reason: NotEvaluatedUncheckedSource, Span: lint.Suppressions[0].Span, Line: 3}}
	if !reflect.DeepEqual(lint.Suppressions, want) || lint.Suppressions[0].Span.Line != 2 {
		t.Fatalf("suppression status on unchecked source: %+v", lint.Suppressions)
	}
}

func TestDependencyGraphTracksProvidersAndConsumers(t *testing.T) {
	source, err := os.ReadFile("../../examples/workflow.ef")
	if err != nil {
		t.Fatal(err)
	}
	r := Compile(string(source))
	graph, err := r.Graph()
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]bool{}
	for _, n := range graph.Nodes {
		if nodes[n.ID] {
			t.Fatal("duplicate node", n.ID)
		}
		nodes[n.ID] = true
	}
	needed := map[string]bool{"requires": false, "implements": false, "provides": false, "calls": false}
	for _, e := range graph.Edges {
		if !nodes[e.From] || !nodes[e.To] {
			t.Fatal("dangling edge", e)
		}
		if e.Service == "Directory" {
			needed[e.Kind] = true
		}
	}
	for kind, found := range needed {
		if !found {
			t.Fatal("missing edge", kind)
		}
	}
	if graph.Revision != r.Revision || len(graph.Limitations) == 0 {
		t.Fatal(graph)
	}
	if _, err := Compile(`fn nope() -> missing {}`).Graph(); err == nil {
		t.Fatal("unchecked graph")
	}
}

func TestDependencyGraphUsesCheckedCallableIdentity(t *testing.T) {
	source := `
service Logger { effect fn log(message: string) -> string }
record Callbacks { log: fn(string) -> string }
fn target() -> string { "global target" }
fn alias() -> string { "global alias" }
fn invoke(target: fn() -> string) -> string { target() }
fn invokeRecord(Logger: Callbacks) -> string { Logger.log("callback") }
fn direct() -> string { target() }
effect fn serviceCall() -> string uses {Logger} { run Logger.log("service") }
effect fn builtinCall() -> void uses {Console} { run Console.log("builtin") }
`
	for _, target := range []string{"go", "js"} {
		r := CompileFor(source, target)
		if !r.Checked {
			t.Fatalf("%s target: %+v", target, r.Diagnostics)
		}
		graph, err := r.Graph()
		if err != nil {
			t.Fatalf("%s target: %v", target, err)
		}
		checkedDirect := false
		checkedService := false
		checkedBuiltin := false
		for _, edge := range graph.Edges {
			if edge.Kind != "calls" {
				continue
			}
			if edge.From == "function:invoke" && (edge.To == "function:target" || edge.To == "function:alias") {
				t.Fatalf("%s target: shadowed callback fabricated callable edge: %+v", target, edge)
			}
			if edge.From == "function:invokeRecord" && edge.To == "service:Logger" {
				t.Fatalf("%s target: record callback fabricated service edge: %+v", target, edge)
			}
			if edge.From == "function:direct" && edge.To == "function:target" {
				checkedDirect = true
			}
			if edge.To == "service:Logger" {
				checkedService = true
			}
			if edge.To == "service:Console" {
				checkedBuiltin = true
			}
		}
		if !checkedDirect {
			t.Fatalf("%s target: checked direct function call edge missing", target)
		}
		if !checkedService {
			t.Fatalf("%s target: checked service call edge missing", target)
		}
		if !checkedBuiltin {
			t.Fatalf("%s target: checked builtin service call edge missing", target)
		}
	}
}

func BenchmarkLintAndGraph10KLines(b *testing.B) {
	var source strings.Builder
	for i := 0; i < 2000; i++ {
		source.WriteString("effect fn f" + fmtInt(i) + "() -> string\nraises {}\nuses {}\n{\n\"value\" }\n")
	}
	r := Compile(source.String())
	if !r.Checked {
		b.Fatal(r.Diagnostics)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Lint(false)
		if _, err := r.Graph(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestUnusedGoImportAdviceKeepsInitialization(t *testing.T) {
	source := "import go strings \"strings\"\neffect fn main() -> void {\n    void\n}\n"
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	for _, strict := range []bool{false, true} {
		lint := r.Lint(strict)
		if !lint.LintPassed || len(lint.LintDiagnostics) != 1 {
			t.Fatalf("strict=%v: %+v", strict, lint)
		}
		advice := lint.LintDiagnostics[0]
		if advice.Code != "EFL003" || advice.Severity != "suggestion" || !strings.Contains(advice.Message, "still initializes its Go package") || !strings.Contains(advice.Message, "only if that initialization is unneeded") {
			t.Fatalf("EFL003 must not present deletion as behavior-preserving: %+v", advice)
		}
	}
	for _, rule := range LintRules() {
		if rule.Code == "EFL003" && !strings.Contains(rule.Description, "still initializes") {
			t.Fatalf("EFL003 rule description: %+v", rule)
		}
	}
	suppressed := Compile("// effra-lint-disable-next-line unused-go-import -- the package registers a driver\n" + source)
	if lint := suppressed.Lint(true); !lint.LintPassed || len(lint.LintDiagnostics) != 0 {
		t.Fatalf("suppressed EFL003: %+v", lint)
	}
}
