package compiler

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Decision syntax (docs/specs/state-machines.md#decisions): the predeclared
// decision<S, O>, the forms enter, stay, ignore, reject and done, and the
// same words as patterns.

const decisionPrelude = `enum S { Idle  Active { n: i64 } }
enum E { Ping  Pong }
`

func decisionCodes(r *Result) []string {
	codes := []string{}
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

func findDecisionDiagnostic(r *Result, code string) (Diagnostic, bool) {
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return d, true
		}
	}
	return Diagnostic{}, false
}

// Each source is accepted with no diagnostic.
func TestDecisionFormsAndPatternsCheck(t *testing.T) {
	for _, c := range []struct{ name, source string }{
		{"every form", decisionPrelude + `fn step(state: S, event: E) -> decision<S, i64> {
    match state, event {
        S.Idle, E.Ping => enter S.Active { n: 1 }
        S.Idle, E.Pong => ignore
        S.Active { n }, E.Ping => if n > 2 { done n } else { stay S.Active { n: n + 1 } }
        S.Active, E.Pong => reject
    }
}
machine M { initial S.Idle {}; step step }
`},
		{"bare done with a void output", decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    match event {
        E.Ping => done
        E.Pong => stay state
    }
}
machine M { initial S.Idle {}; step step }
`},
		{"decision patterns with binders and alternatives", decisionPrelude + `fn describe(decision: decision<S, i64>) -> string {
    match decision {
        enter next | stay next => label(next)
        ignore | reject => "unchanged"
        done _ => "done"
    }
}
fn label(state: S) -> string {
    match state {
        S.Idle => "idle"
        S.Active => "active"
    }
}
`},
		{"a decision helper flows to its caller", decisionPrelude + `fn activate(n: i64) -> decision<S, void> {
    enter S.Active { n: n }
}
fn step(state: S, event: E) -> decision<S, void> {
    let chosen = activate(1)
    match event {
        E.Ping => chosen
        E.Pong => stay state
    }
}
machine M { initial S.Idle {}; step step }
`},
		{"go is an ordinary identifier", `fn go(steps: i64) -> i64 { steps }
fn twice() -> i64 { go(2) }
`},
		{"a user enum named Step is ordinary data", `enum Step { Go  Stay }
fn pick(step: Step) -> string {
    match step {
        Step.Go => "go"
        Step.Stay => "stay"
    }
}
`},
		{"a source declaration named decision shadows the predeclared type", `record decision { weight: i64 }
fn heavier(left: decision, right: decision) -> bool { left.weight > right.weight }
`},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := Compile(c.source)
			if len(r.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %+v", r.Diagnostics)
			}
		})
	}
}

// The design's negative controls.
func TestDecisionNegativeControls(t *testing.T) {
	for _, c := range []struct {
		name, source, code, message string
	}{
		{"a form where no decision is expected", `fn label() -> string { enter "x" }
`, "EF143", "needs an enclosing function that returns decision<S, O>"},
		{"a form in a constant-free context", decisionPrelude + `fn pick(state: S) -> S { stay state }
`, "EF143", "`stay` is a decision form"},
		{"a form as a statement", decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    ignore
    stay state
}
`, "EF143", "`ignore` as a statement discards its decision"},
		{"a target of another type", decisionPrelude + `enum Other { X }
fn step(state: S, event: E) -> decision<S, void> { enter Other.X {} }
`, "EF106", "`enter` needs a S; found Other"},
		{"bare done when the output is not void", decisionPrelude + `fn step(state: S, event: E) -> decision<S, i64> { done }
`, "EF106", "bare `done` needs a void output; write `done value` with a i64"},
		{"an output of the wrong type", decisionPrelude + `fn step(state: S, event: E) -> decision<S, i64> { done "x" }
`, "EF106", "`done` needs a i64; found string"},
		{"a stay that changes the narrowed tag", decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    match state {
        S.Idle => stay S.Active { n: 0 }
        S.Active => stay state
    }
}
machine M { initial S.Idle {}; step step }
`, "EF139", "`stay` keeps the current entry"},
		{"a non-enum state", `enum E { Ping }
fn step(state: i64, event: E) -> decision<i64, void> { stay state }
machine M { initial 0; step step }
`, "EF140", "must be a closed, non-generic enum"},
		{"a step returning another type", decisionPrelude + `fn step(state: S, event: E) -> S { state }
machine M { initial S.Idle {}; step step }
`, "EF140", "must return decision<State, Output>"},
		{"a decision match missing done", decisionPrelude + `fn describe(decision: decision<S, i64>) -> string {
    match decision {
        enter _ | stay _ => "moved"
        ignore | reject => "unchanged"
    }
}
`, "EF117", "missing match arm for done"},
		{"a decision pattern against another subject", decisionPrelude + `fn label(state: S) -> string {
    match state {
        enter next => "x"
    }
}
`, "EF116", "decision pattern `enter` needs a decision<S, O> subject"},
		{"constructing the decision type", decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> { decision.enter { next: state } }
`, "EF143", "a decision is written with its forms"},
		{"calling the decision type's variant", decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> { decision.enter(state) }
`, "EF143", "a decision is written with its forms"},
		{"a dotted decision pattern", decisionPrelude + `fn label(d: decision<S, void>) -> string {
    match d {
        decision.enter { next } => "x"
        stay _ => "x"
        ignore | reject | done => "x"
    }
}
`, "EF143", "a decision pattern is written with its word"},
		{"a reserved word as a function name", `fn done() -> i64 { 1 }
`, "EF002", "done is reserved for the decision form `done value`"},
		{"a reserved word as a local", `fn f() -> i64 {
    let ignore = 1
    ignore
}
`, "EF002", "ignore is reserved for the decision form `ignore`"},
		{"a reserved word as a parameter", `fn f(enter: i64) -> i64 { 1 }
`, "EF002", "enter is reserved for the decision form `enter value`"},
		{"a binder on a decision without payload", decisionPrelude + `fn f(d: decision<S, void>) -> i64 {
    match d {
        reject why => 1
    }
}
`, "EF002", "reject carries no payload to bind"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := Compile(c.source)
			d, ok := findDecisionDiagnostic(r, c.code)
			if !ok || !strings.Contains(d.Message, c.message) {
				t.Fatalf("want %s %q, got %+v", c.code, c.message, r.Diagnostics)
			}
		})
	}
}

// The stale bundled import is the ordinary unknown-module diagnostic, and its
// help names the decision forms that replace Step.
func TestDecisionStaleStepImportHasAMigrationHint(t *testing.T) {
	r := Compile(`import Machines "effra/machines"
` + decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> { ignore }
`)
	d, ok := findDecisionDiagnostic(r, "EF126")
	if !ok || !strings.Contains(d.Message, "unsupported bundled module effra/machines") {
		t.Fatalf("diagnostics = %+v", r.Diagnostics)
	}
	for _, want := range []string{"decision<S, O>", "`enter e`", "Go -> enter", "Done -> done"} {
		if !strings.Contains(d.Help, want) {
			t.Fatalf("help %q lacks %q", d.Help, want)
		}
	}
	if got := decisionCodes(r); !slices.Equal(got, []string{"EF126"}) {
		t.Fatalf("codes = %v", got)
	}
}

// Parser controls: constructor braces after a form, the contextual `<-` of a
// region update (M2), and `<-` that is still a comparison with a negation.
func TestDecisionParserControls(t *testing.T) {
	t.Run("stay V { f: x } is a constructor operand", func(t *testing.T) {
		program, diagnostics := parse(decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    stay S.Active { n: 1 }
}
`)
		if len(diagnostics) != 0 {
			t.Fatalf("diagnostics = %+v", diagnostics)
		}
		value := program.Functions[0].Body.Statements[0].Value
		if value.Kind != "decision" || value.Name != "stay" || len(value.Fields) != 1 || value.Fields[0].Name != "next" || value.Fields[0].Value.Kind != "construct" {
			t.Fatalf("stay parsed as %+v", value)
		}
	})
	t.Run("stay V { f <- d } is a refused region update", func(t *testing.T) {
		_, diagnostics := parse(decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    stay S.Active { n <- ignore }
}
`)
		if len(diagnostics) != 1 || diagnostics[0].Code != "EF002" || !strings.Contains(diagnostics[0].Message, "region updates (`n <- decision`) are not admitted") {
			t.Fatalf("diagnostics = %+v", diagnostics)
		}
	})
	t.Run("x<-1 in a block is a comparison", func(t *testing.T) {
		program, diagnostics := parse(`fn negative(x: i64) -> bool {
    x<-1
}
`)
		if len(diagnostics) != 0 {
			t.Fatalf("diagnostics = %+v", diagnostics)
		}
		value := program.Functions[0].Body.Statements[0].Value
		if value.Kind != "binary" || value.Name != "<" || value.Right == nil || value.Right.Kind != "unary" {
			t.Fatalf("x<-1 parsed as %+v", value)
		}
		if r := Compile(`fn negative(x: i64) -> bool {
    x<-1
}
`); len(r.Diagnostics) != 0 {
			t.Fatalf("diagnostics = %+v", r.Diagnostics)
		}
	})
	t.Run("if x<-1 { } keeps its block", func(t *testing.T) {
		r := Compile(decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    let x = 0
    if x<-1 { stay state } else { enter S.Idle {} }
}
`)
		if len(r.Diagnostics) != 0 {
			t.Fatalf("diagnostics = %+v", r.Diagnostics)
		}
	})
	t.Run("a construct operand in an if header stays protected", func(t *testing.T) {
		// `stay state` inside the branch blocks; the header brace is a block.
		r := Compile(decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    if state == state { stay state } else { enter S.Active { n: 2 } }
}
`)
		if d, ok := findDecisionDiagnostic(r, "EF002"); ok {
			t.Fatalf("parse error %+v", d)
		}
	})
	t.Run("bare done ends an arm and a block", func(t *testing.T) {
		program, diagnostics := parse(decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    match event {
        E.Ping => done
        E.Pong => if true { done } else { done void }
    }
}
`)
		if len(diagnostics) != 0 {
			t.Fatalf("diagnostics = %+v", diagnostics)
		}
		arms := program.Functions[0].Body.Statements[0].Value.Arms
		first := arms[0].Body.Statements[0].Value
		if first.Kind != "decision" || first.Name != "done" || len(first.Fields) != 0 {
			t.Fatalf("bare done parsed as %+v", first)
		}
		branch := arms[1].Body.Statements[0].Value
		if bare := branch.Then.Statements[0].Value; bare.Kind != "decision" || len(bare.Fields) != 0 {
			t.Fatalf("bare done in a block parsed as %+v", bare)
		}
		if valued := branch.Else.Statements[0].Value; valued.Kind != "decision" || len(valued.Fields) != 1 || valued.Fields[0].Value.Kind != "void" {
			t.Fatalf("done void parsed as %+v", valued)
		}
	})
	t.Run("a form binds tighter than a binary operator", func(t *testing.T) {
		program, diagnostics := parse(`fn f() -> i64 { done 1 + 2 }
`)
		if len(diagnostics) != 0 {
			t.Fatalf("diagnostics = %+v", diagnostics)
		}
		value := program.Functions[0].Body.Statements[0].Value
		if value.Kind != "binary" || value.Left.Kind != "decision" {
			t.Fatalf("done 1 + 2 parsed as %+v", value)
		}
	})
}

// The plan reads decision forms structurally: every plan transition kind is
// produced by its source word, with source `enter` published as "go".
func TestDecisionWordsProducePlanTransitions(t *testing.T) {
	source := decisionPrelude + `fn step(state: S, event: E) -> decision<S, i64> {
    match state, event {
        S.Idle, E.Ping => enter S.Active { n: 1 }
        S.Idle, E.Pong => ignore
        S.Active { n }, E.Ping => if n > 2 { done n } else { stay S.Active { n: n + 1 } }
        S.Active, E.Pong => reject
    }
}
machine M { initial S.Idle {}; step step }
`
	r := Compile(source)
	plan := r.FindMachine("M")
	if plan == nil {
		t.Fatalf("no plan: %+v", r.Diagnostics)
	}
	words := map[string]string{"go": "enter", "stay": "stay", "ignore": "ignore", "reject": "reject", "done": "done"}
	seen := map[string]bool{}
	for _, transition := range plan.Transitions {
		word := source[transition.Span.Offset : transition.Span.Offset+transition.Span.Length]
		if words[transition.Decision] != word {
			t.Fatalf("transition %s anchored at %q", transition.Decision, word)
		}
		seen[transition.Decision] = true
	}
	if len(seen) != len(words) {
		t.Fatalf("decisions = %v", seen)
	}
}

// The formatter prints a form like run: one space before its operand, also
// before a parenthesised or negated operand.
func TestDecisionFormatting(t *testing.T) {
	source := decisionPrelude + `fn step(state: S, event: E) -> decision<S, i64> {
    match event {
        E.Ping => done -1
        E.Pong => stay (state)
    }
}
`
	formatted, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"=> done -1\n", "=> stay (state)\n"} {
		if !strings.Contains(formatted.Text, want) {
			t.Fatalf("formatted text lacks %q:\n%s", want, formatted.Text)
		}
	}
	again, err := FormatSource(formatted.Text)
	if err != nil || again.Text != formatted.Text {
		t.Fatalf("second pass differs: %v\n%s", err, again.Text)
	}
	if FormatterIdentity != "effra/formatter-16" {
		t.Fatalf("formatter identity %s", FormatterIdentity)
	}
}

// machineplan.Input documents what its opaque V must be so that no allowed
// library's reflection writes checker memory through it; the checker's
// projection type keeps to that contract.
func TestMachineProjectionIsInertToReflection(t *testing.T) {
	value := reflect.TypeFor[ValueType]()
	if value.Kind() != reflect.Struct {
		t.Fatalf("ValueType is a %s, not a struct held by value", value.Kind())
	}
	callbacks := []reflect.Type{reflect.TypeFor[fmt.Scanner](), reflect.TypeFor[fmt.Formatter](), reflect.TypeFor[fmt.Stringer](), reflect.TypeFor[fmt.GoStringer](), reflect.TypeFor[error]()}
	for _, typ := range []reflect.Type{value, reflect.PointerTo(value)} {
		for _, callback := range callbacks {
			if typ.Implements(callback) {
				t.Errorf("%s implements %s, which fmt calls back", typ, callback)
			}
		}
	}
}
