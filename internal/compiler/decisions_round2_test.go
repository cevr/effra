package compiler

import (
	"strings"
	"testing"
)

// Decision review round 2: each fix is exercised on both targets. Accepted
// programs run natively and on JavaScript with the same output; refusals carry
// the same diagnostic for both targets.

var decisionTargets = []string{"go", "js"}

func runDecisionOnBothTargets(t *testing.T, source, want string) {
	t.Helper()
	runGenericDataNative(t, source, want)
	if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != want {
		t.Fatalf("JavaScript: want %q, got %q", want, output)
	}
}

func requireDecisionRefusalOnBothTargets(t *testing.T, source, code, message string) {
	t.Helper()
	for _, target := range decisionTargets {
		r := CompileFor(source, target)
		d, ok := findDecisionDiagnostic(r, code)
		if !ok || !strings.Contains(d.Message, message) {
			t.Fatalf("%s: want %s %q, got %+v", target, code, message, r.Diagnostics)
		}
	}
}

func decisionLabelSource(steps string) string {
	return decisionPrelude + steps + `
fn label(d: decision<S, void>) -> string {
    match d {
        enter next => "enter " + name(next)
        stay next => "stay " + name(next)
        ignore => "ignore"
        reject => "reject"
        done => "done"
    }
}
fn name(s: S) -> string {
    match s {
        S.Idle => "idle"
        S.Active { n } => if n == 1 { "active" } else { "active?" }
    }
}
`
}

// M1: the predeclared decision type lives in type position only. A function
// named decision is still called, next to a step that spells decision<S, O>;
// building a decision through the type is still EF143.
func TestDecisionNameResolvesAsAValueOutsideTypePosition(t *testing.T) {
	source := decisionLabelSource(`fn decision(n: i64) -> i64 {
    n + 1
}
fn step(state: S, event: E) -> decision<S, void> {
    if decision(1) == 2 { enter S.Active { n: 1 } } else { reject }
}
`) + `effect fn main() -> string {
    label(step(S.Idle {}, E.Ping {}))
}`
	runDecisionOnBothTargets(t, source, "enter active\n")

	service := decisionPrelude + `service decision {
    effect fn get() -> i64
}
effect fn read() -> i64 uses { decision } {
    run decision.get()
}
fn step(state: S, event: E) -> decision<S, void> {
    ignore
}
`
	for _, target := range decisionTargets {
		if r := CompileFor(service, target); len(r.Diagnostics) != 0 {
			t.Fatalf("%s: service named decision: %+v", target, r.Diagnostics)
		}
	}
	for _, refused := range []string{"decision.enter(state)", "decision.enter { next: state }", "decision(state)"} {
		requireDecisionRefusalOnBothTargets(t, decisionPrelude+"fn step(state: S, event: E) -> decision<S, void> { "+refused+" }\n", "EF143", "a decision is written with its forms")
	}
}

// M2: a discarded decision is refused by its checked type, whatever the
// statement's syntax. Binding or passing the value consumes it.
func TestDecisionDiscardFollowsTheCheckedType(t *testing.T) {
	for _, c := range []struct{ name, body string }{
		{"if", "if true { ignore } else { reject }\n    stay state"},
		{"match", "match event {\n        E.Ping => ignore\n        E.Pong => reject\n    }\n    stay state"},
		{"helper call", "guard(state)\n    stay state"},
	} {
		t.Run(c.name, func(t *testing.T) {
			source := decisionPrelude + `fn guard(s: S) -> decision<S, void> {
    ignore
}
fn step(state: S, event: E) -> decision<S, void> {
    ` + c.body + `
}
`
			requireDecisionRefusalOnBothTargets(t, source, "EF143", "this statement discards its decision; a decision is a value, not an early exit")
		})
	}
	source := decisionLabelSource(`fn guard(s: S) -> decision<S, void> {
    reject
}
fn step(state: S, event: E) -> decision<S, void> {
    let checked = guard(state)
    let shown = label(guard(state))
    if shown == "reject" { checked } else { stay state }
}
`) + `effect fn main() -> string {
    label(step(S.Idle {}, E.Ping {}))
}`
	runDecisionOnBothTargets(t, source, "reject\n")
}

// M3: the decision words are explicit field labels, so a derived codec carries
// the wire key "done". Shorthand, which reads or binds a local, is refused.
func TestDecisionWordsAreExplicitFieldLabels(t *testing.T) {
	source := `import Json "effra/json"

record Todo { title: string  done: bool  enter: i64 }
enum Step { Finished { done: bool } }

derive todoJson = Json.codec<Todo>(maxBodyBytes: 256, maxDepth: 4)

fn flip(t: Todo) -> Todo {
    Todo { title: t.title, done: if t.done { false } else { true }, enter: t.enter - 1 }
}
fn finished(s: Step) -> bool {
    match s {
        Step.Finished { done: d } => d
    }
}
effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    let todo = run todoJson.decode("{\"title\":\"a\",\"done\":true,\"enter\":\"3\"}")
    let wire = run todoJson.encode(flip(todo))
    let step = Step.Finished { done: todo.done }
    wire + "|" + (if finished(step) { "done" } else { "open" })
}`
	runDecisionOnBothTargets(t, source, `{"title":"a","done":false,"enter":"2"}|done`+"\n")

	for _, c := range []struct{ name, source, message string }{
		{"constructor shorthand", "record Todo { done: bool }\nfn f(x: bool) -> Todo { Todo { done } }\n", "done is a decision word, so it cannot be shorthand for a local; write `done: value`"},
		{"pattern shorthand", "enum Step { Finished { done: bool } }\nfn f(s: Step) -> bool {\n    match s {\n        Step.Finished { done } => true\n    }\n}\n", "done is a decision word, so it cannot be shorthand for a local; write `done: binder`"},
	} {
		t.Run(c.name, func(t *testing.T) {
			requireDecisionRefusalOnBothTargets(t, c.source, "EF002", c.message)
		})
	}

	// A label after a dot is a member, never a prefix keyword: `t.done - 1`
	// keeps binary spacing and `t.enter(1)` stays a call.
	member := "fn f(t: Todo) -> i64 {\n    t.enter - t.done\n}\n"
	formatted, err := FormatSource(member)
	if err != nil || formatted.Text != member {
		t.Fatalf("member label formatting: %v\n%s", err, formatted.Text)
	}
}

// m1: a bare `done` before another arm on the same line takes no operand,
// while a same-line operand that holds its own arrows inside braces still
// belongs to `done`.
func TestDecisionBareDoneBeforeASameLineArm(t *testing.T) {
	source := decisionLabelSource(`fn step(state: S, event: E) -> decision<S, void> {
    match event { E.Ping => done E.Pong => stay state }
}
fn other(state: S, event: E) -> decision<S, i64> {
    match event { E.Ping => done match state { S.Idle => 1, S.Active => 2 } E.Pong => stay state }
}
fn count(d: decision<S, i64>) -> i64 {
    match d {
        done n => n
        enter _ | stay _ | ignore | reject => 0
    }
}
`) + `effect fn main() -> string {
    label(step(S.Idle {}, E.Ping {})) + ";" + label(step(S.Idle {}, E.Pong {})) + ";" + (if count(other(S.Idle {}, E.Ping {})) == 1 { "one" } else { "other" })
}`
	runDecisionOnBothTargets(t, source, "done;stay idle;one\n")
}

// o5: formatter-16 pins decision patterns, bare `done` and a multi-line
// constructor operand as a canonical fixed point.
func TestDecisionPatternsAndOperandsAreCanonical(t *testing.T) {
	const canonical = `enum S {
    Idle
    Active {
        n: i64,
        label: string
    }
}
enum E {
    Ping
    Pong
}
fn step(state: S, event: E) -> decision<S, void> {
    match event {
        E.Ping => done
        E.Pong => enter S.Active {
            n: 1,
            label: "a long label that pushes the constructor past the line width"
        }
    }
}
fn label(d: decision<S, void>) -> string {
    match d {
        enter next | stay next => "move"
        ignore | reject => "same"
        done => "done"
    }
}
fn count(d: decision<S, i64>) -> i64 {
    match d {
        done out => out
        enter _ | stay _ | ignore | reject => 0
    }
}
`
	formatted, err := FormatSource(canonical)
	if err != nil || formatted.Text != canonical {
		t.Fatalf("canonical decision source changed: %v\n%s", err, formatted.Text)
	}
	for _, target := range decisionTargets {
		if r := CompileFor(canonical, target); len(r.Diagnostics) != 0 {
			t.Fatalf("%s: %+v", target, r.Diagnostics)
		}
	}
}

// o6: the prefix precedence mistake names its repair.
func TestDecisionPrecedenceMistakeNamesTheRepair(t *testing.T) {
	source := decisionPrelude + `fn step(state: S, event: E) -> decision<S, i64> {
    let a = 1
    done a + 2
}
`
	requireDecisionRefusalOnBothTargets(t, source, "EF106", "`done` binds tighter than +, so write `done (a + b)`")
}

// o1: plans publish each transition under its source word on both targets.
func TestDecisionPlanKindsAreSourceWordsOnBothTargets(t *testing.T) {
	source := decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    match state, event {
        S.Idle, E.Ping => enter S.Active { n: 1 }
        S.Idle, E.Pong => ignore
        S.Active, E.Ping => stay state
        S.Active, E.Pong => done
    }
}
machine M { initial S.Idle {}; step step }
`
	for _, target := range decisionTargets {
		plan := CompileFor(source, target).FindMachine("M")
		if plan == nil {
			t.Fatalf("%s: no plan", target)
		}
		got := []string{}
		for _, transition := range plan.Transitions {
			got = append(got, transition.Decision)
		}
		if strings.Join(got, ",") != "enter,ignore,stay,done" {
			t.Fatalf("%s: plan decisions = %v", target, got)
		}
	}
}
