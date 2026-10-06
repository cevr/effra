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
