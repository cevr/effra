package compiler

import (
	"strings"
	"testing"
)

// Decision review round 2: accepted programs run natively with the expected
// output; refusals carry the expected diagnostic.

func requireDecisionRefusal(t *testing.T, source, code, message string) {
	t.Helper()
	r := CompileFor(source, "go")
	d, ok := findDecisionDiagnostic(r, code)
	if !ok || !strings.Contains(d.Message, message) {
		t.Fatalf("go: want %s %q, got %+v", code, message, r.Diagnostics)
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
	runGenericDataNative(t, source, "enter active\n")

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
	if r := CompileFor(service, "go"); len(r.Diagnostics) != 0 {
		t.Fatalf("go: service named decision: %+v", r.Diagnostics)
	}
	for _, refused := range []string{"decision.enter(state)", "decision.enter { next: state }", "decision(state)"} {
		requireDecisionRefusal(t, decisionPrelude+"fn step(state: S, event: E) -> decision<S, void> { "+refused+" }\n", "EF143", "a decision is written with its forms")
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
			requireDecisionRefusal(t, source, "EF143", "this statement discards its decision; a decision is a value, not an early exit")
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
	runGenericDataNative(t, source, "reject\n")
}

// M3: the decision words are explicit field labels, so a derived codec carries
// the wire key "done". Shorthand, which reads or binds a local, is refused.
func TestDecisionWordsAreExplicitFieldLabels(t *testing.T) {
	source := `import Json "effra/json"

record Todo { title: string  done: bool  enter: i64 }
enum Step { Finished { done: bool } }
enum Mark { Finished(done: bool, enter: i64) }

derive todoJson = Json.codec<Todo>(maxBodyBytes: 256, maxDepth: 4)

fn flip(t: Todo) -> Todo {
    Todo { title: t.title, done: if t.done { false } else { true }, enter: t.enter - 1 }
}
fn marked(m: Mark) -> string {
    match m {
        Mark.Finished { done: d, enter: e } => if d { "marked " } else { "open " }
    }
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
    let named = Todo(title: "b", done: false, enter: 0)
    let mark = Mark.Finished(done: named.done, enter: named.enter)
    wire + "|" + (if finished(step) { "done" } else { "open" }) + "|" + marked(mark) + named.title
}`
	runGenericDataNative(t, source, `{"title":"a","done":false,"enter":"2"}|done|open b`+"\n")

	for _, c := range []struct{ name, source, message string }{
		{"constructor shorthand", "record Todo { done: bool }\nfn f(x: bool) -> Todo { Todo { done } }\n", "done is a decision word, so it cannot be shorthand for a local; write `done: value`"},
		{"pattern shorthand", "enum Step { Finished { done: bool } }\nfn f(s: Step) -> bool {\n    match s {\n        Step.Finished { done } => true\n    }\n}\n", "done is a decision word, so it cannot be shorthand for a local; write `done: binder`"},
	} {
		t.Run(c.name, func(t *testing.T) {
			requireDecisionRefusal(t, c.source, "EF002", c.message)
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

// m1/N1: `done` takes an operand exactly when the next token on its line can
// start an expression. An arm separator (`,`, a newline, `}`) ends a bare
// `done`; comma-separated single-line arms keep their operands, including
// in a product match; a nested match and a multi-line operand belong to
// `done`. Formatting the program is a fixed point that runs the same.
func TestDecisionDoneOperandFollowsTheGrammarRule(t *testing.T) {
	source := decisionLabelSource(`fn bare(state: S, event: E) -> decision<S, void> {
    match event { E.Ping => done, E.Pong => stay state }
}
fn unit(state: S, event: E) -> decision<S, void> {
    match event { E.Ping => done void, E.Pong => stay state }
}
fn valued(state: S, event: E) -> decision<S, i64> {
    let n = 1
    match event { E.Ping => done n, E.Pong => stay state }
}
fn literal(state: S, event: E) -> decision<S, i64> {
    match event { E.Ping => done 1, E.Pong => stay state }
}
fn product(state: S, event: E) -> decision<S, i64> {
    match state, event { S.Idle, E.Ping => done 1, S.Idle, E.Pong => stay state, S.Active, E.Ping | E.Pong => done 2 }
}
fn nested(state: S, event: E) -> decision<S, i64> {
    match event { E.Ping => done match state { S.Idle => 1, S.Active => 2 }, E.Pong => stay state }
}
fn multiline(state: S, event: E) -> decision<S, i64> {
    match event {
        E.Ping => done (1 +
            2)
        E.Pong => stay state
    }
}
fn outputIs(d: decision<S, i64>, want: i64) -> string {
    match d {
        done n => if n == want { "y" } else { "n" }
        enter _ | stay _ | ignore | reject => "-"
    }
}
`) + `effect fn main() -> string {
    let idle = S.Idle {}
    label(bare(idle, E.Ping {})) + ";" + label(bare(idle, E.Pong {})) + ";" + label(unit(idle, E.Ping {})) + ";" + outputIs(valued(idle, E.Ping {}), 1) + outputIs(literal(idle, E.Ping {}), 1) + outputIs(product(idle, E.Ping {}), 1) + outputIs(product(S.Active { n: 1 }, E.Pong {}), 2) + outputIs(nested(idle, E.Ping {}), 1) + outputIs(multiline(idle, E.Ping {}), 3)
}`
	const want = "done;stay idle;done;yyyyyy\n"
	runGenericDataNative(t, source, want)

	formatted, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	again, err := FormatSource(formatted.Text)
	if err != nil || again.Text != formatted.Text {
		t.Fatalf("formatting is not a fixed point: %v\n%s", err, again.Text)
	}
	for _, kept := range []string{"E.Ping => done,\n", "E.Ping => done n,\n", "E.Ping => done 1,\n", "E.Ping => done void,\n", "S.Idle, E.Ping => done 1,\n"} {
		if !strings.Contains(formatted.Text, kept) {
			t.Fatalf("formatted program lacks %q:\n%s", kept, formatted.Text)
		}
	}
	runGenericDataNative(t, formatted.Text, want)

	// Without a separator, the next arm's pattern is the operand and the arrow
	// after it is refused with the repair.
	refused := decisionPrelude + `fn step(state: S, event: E) -> decision<S, void> {
    match event { E.Ping => done E.Pong => stay state }
}
`
	requireDecisionRefusal(t, refused, "EF002", "end a bare `done` with `,` or start the next arm on a new line")
}

// Round 3 O1: the missing-separator repair also covers a next arm that is an
// or-pattern or a product arm. The repair is diagnostic recovery: it fires
// only where the match is already ill-formed, and operand detection is
// unchanged.
func TestDecisionMissingSeparatorRepairCoversOrAndProductArms(t *testing.T) {
	for _, tc := range []struct{ name, body, result string }{
		{"arrow", `match event { E.Ping => done E.Pong => stay state }`, "void"},
		{"or-pattern", `match event { E.Ping => done E.Pong | E.Ping => stay state }`, "void"},
		{"product", `match state, event { S.Idle, E.Ping => done S.Idle, E.Pong => stay state, _, _ => ignore }`, "S"},
		{"product or-pattern", `match state, event { S.Idle, E.Ping => done S.Idle, E.Pong | E.Ping => stay state, _, _ => ignore }`, "S"},
		{"three subjects", `match state, event, event { S.Idle, E.Ping, E.Ping => done S.Idle, E.Pong, E.Pong => stay state, _, _, _ => ignore }`, "S"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := decisionPrelude + `fn step(state: S, event: E) -> decision<S, ` + tc.result + `> {
    ` + tc.body + `
}
`
			requireDecisionRefusal(t, source, "EF002", "so the next arm lost its first pattern; end a bare `done` with `,` or start the next arm on a new line")
		})
	}

	// A short product arm that does not follow `done X,` on its line keeps the
	// ordinary arity diagnostic, and so does one after `done X,` and a newline.
	for _, body := range []string{
		`match state, event { S.Idle, E.Ping => ignore, E.Pong => stay state, _, _ => ignore }`,
		"match state, event {\n        S.Idle, E.Ping => done S.Idle,\n        E.Pong => stay state\n        _, _ => ignore\n    }",
	} {
		source := decisionPrelude + `fn step(state: S, event: E) -> decision<S, S> {
    ` + body + `
}
`
		r := CompileFor(source, "go")
		if _, ok := findDecisionDiagnostic(r, "EF118"); !ok {
			t.Fatalf("go: want EF118 for %q, got %+v", body, r.Diagnostics)
		}
		for _, d := range r.Diagnostics {
			if strings.Contains(d.Message, "lost its first pattern") {
				t.Fatalf("go: repair fired outside a same-line `done X,`: %+v", d)
			}
		}
	}
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
	if r := CompileFor(canonical, "go"); len(r.Diagnostics) != 0 {
		t.Fatalf("go: %+v", r.Diagnostics)
	}
}

// o6: the prefix precedence mistake names its repair.
func TestDecisionPrecedenceMistakeNamesTheRepair(t *testing.T) {
	source := decisionPrelude + `fn step(state: S, event: E) -> decision<S, i64> {
    let a = 1
    done a + 2
}
`
	requireDecisionRefusal(t, source, "EF106", "`done` binds tighter than +, so write `done (a + b)`")
}

// o1: plans publish each transition under its source word.
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
	plan := CompileFor(source, "go").FindMachine("M")
	if plan == nil {
		t.Fatal("go: no plan")
	}
	got := []string{}
	for _, transition := range plan.Transitions {
		got = append(got, transition.Decision)
	}
	if strings.Join(got, ",") != "enter,ignore,stay,done" {
		t.Fatalf("go: plan decisions = %v", got)
	}
}
