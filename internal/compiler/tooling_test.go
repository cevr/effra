package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestLintUsesCheckedLexicalBindings(t *testing.T) {
	cases := []struct {
		name, source string
		warnings     int
	}{
		{"unused", `effect fn task() -> string { "ok" } effect fn main() -> () { let forgotten = task(); () }`, 1},
		{"branch use", `effect fn task() -> string { "ok" } effect fn main() -> string { let recipe = task(); if true {run recipe} else {""} }`, 0},
		{"scope use", `effect fn task() -> string { "ok" } effect fn main() -> string { let recipe = task(); scope {run recipe} }`, 0},
		{"independent bindings", `effect fn task() -> string { "ok" } effect fn a() -> string { let recipe = task(); run recipe } effect fn main() -> () {let recipe = task(); ()}`, 1},
		{"acknowledged", `effect fn task() -> string { "ok" } effect fn main() -> () { let _ = task(); () }`, 0},
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
	invalid := Compile(`effect fn task() -> () { () } effect fn main() -> () {task()}`)
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
effect fn main() -> () {
// effra-lint-disable-next-line unused-recipe -- intentionally deferred hook
let forgotten = task()
let alsoForgotten = task();
()}`
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
effect fn main() -> () {
let text = "// effra-lint-disable-next-line unused-recipe -- this is data"
let forgotten = task();
()}`
	stringResult := Compile(stringDirective)
	if !stringResult.Checked {
		t.Fatal(stringResult.Diagnostics)
	}
	stringLint := stringResult.Lint(false)
	if len(stringLint.LintDiagnostics) != 1 || stringLint.LintDiagnostics[0].Rule != "unused-recipe" {
		t.Fatalf("directive text in a string changed lint behavior: %+v", stringLint)
	}

	sameLine := `effect fn main() -> () {
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
			`effect fn main() -> () {
// effra-lint-disable-next-line future-rule -- waiting for a future rule
()}`,
			"unknown lint rule future-rule",
			2,
		},
		{
			"missing reason",
			`effect fn task() -> string { "ok" }
effect fn main() -> () {
// effra-lint-disable-next-line unused-recipe
let forgotten = task();
()}`,
			"must include a non-empty reason",
			3,
		},
		{
			"multiple rules",
			`effect fn main() -> () {
// effra-lint-disable-next-line unused-recipe redundant-provision -- one target is required
()}`,
			"must name exactly one lint rule",
			2,
		},
		{
			"unused",
			`effect fn main() -> () {
// effra-lint-disable-next-line unused-recipe -- no advice is on the next line
()}`,
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
	source := `effect fn main() -> () {
// effra-lint-disable-next-line unused-recipe -- this must never hide a compiler error
run Console.log("x")
}`
	r := Compile(source)
	if r.Checked || !hasCode(r, "EF108") {
		t.Fatalf("compiler diagnostic was suppressed: %+v", r.Diagnostics)
	}
	lint := r.Lint(false)
	if lint.Checked || lint.LintPassed || len(lint.Diagnostics) == 0 || lint.Errors != 1 || len(lint.LintDiagnostics) != 1 {
		t.Fatalf("unchecked source received suppressible lint semantics: %+v", lint)
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

func BenchmarkLintAndGraph10KLines(b *testing.B) {
	var source strings.Builder
	for i := 0; i < 2000; i++ {
		source.WriteString("effect fn f" + fmtInt(i) + "() -> string\nthrows {}\nuses {}\n{\n\"value\" }\n")
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
