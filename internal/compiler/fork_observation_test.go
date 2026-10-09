package compiler

import (
	"strings"
	"testing"
)

// A forked child's typed failure is charged where the runtime raises it
// (design §5.3): to an observing join or interrupt, or to the owner that
// closes over an unobserved child. Recovering the forking call does not
// discharge a child the call left unobserved.

// TestUnobservedChildFailureStaysRefused pins the programs whose refusal no
// longer comes from a fork-site charge: each is refused because recovering
// the forking call cannot handle the pending child failure, so the consumer
// leaves WithFile undeclared.
func TestUnobservedChildFailureStaysRefused(t *testing.T) {
	fixture := ownershipFixture(t)
	sources := ownershipProbeSources(t, fixture)
	for _, program := range ownershipMatrixPrograms(fixture) {
		if program.Name == "ch_none_stage_read" {
			sources["ch_none_stage_read"] = program.Source
		}
	}
	for _, name := range []string{"ch_none_stage_read", "rv-fork-unjoined-failure", "rv-fork-unjoined-failure-sleep"} {
		t.Run(name, func(t *testing.T) {
			source, ok := sources[name]
			if !ok {
				t.Fatal("missing program")
			}
			r := CompileFor(source, "go")
			if r.Checked || len(r.Diagnostics) != 1 {
				t.Fatalf("want one EF107, got %+v", r.Diagnostics)
			}
			d := r.Diagnostics[0]
			consume := strings.Count(source[:strings.Index(source, "effect fn consume(")], "\n") + 1
			if d.Code != "EF107" || d.Message != "undeclared failures: WithFile" || d.Span.Line != consume {
				t.Fatalf("want EF107 undeclared WithFile at consume (line %d), got %+v", consume, d)
			}
		})
	}
}

func TestForkedChildFailureIsChargedWhereObserved(t *testing.T) {
	const prelude = `error A
error B
effect fn task() -> string raises {A} { fail A }
effect fn other() -> string raises {B} { "other" }
`
	for _, tc := range []struct {
		name  string
		stage string
		// refused is the label the recovering caller leaves undeclared.
		refused string
	}{
		{name: "joined before the end", stage: `{ let f = fork task() run f.join() }`},
		{name: "interrupted before the end", stage: `{ let f = fork task() run f.interrupt() "done" }`},
		{name: "observed in both arms", stage: `{ let f = fork task() if true { run f.join() } else { run f.interrupt() "interrupted" } }`},
		{name: "scope raises on close", stage: `{ scope { let f = fork task() "done" } }`},
		{name: "unobserved at the end", stage: `{ let f = fork task() "done" }`, refused: "A"},
		{name: "exit before the join", stage: `{ let f = fork task() run other() run f.join() }`, refused: "A"},
		{name: "observed in one arm", stage: `{ let f = fork task() if true { run f.join() } else { "skipped" } }`, refused: "A"},
		{name: "cancel does not observe", stage: `{ let f = fork task() run f.cancel() "done" }`, refused: "A"},
		{name: "join in another child", stage: `{ let f = fork task() let g = fork f.join() run g.join() }`, refused: "A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := prelude + "effect fn stage() -> string raises {A, B} " + tc.stage + `
effect fn main() -> string raises {B} { run stage().catch<A>("handled") }`
			r := CompileFor(source, "go")
			if tc.refused == "" {
				if !r.Checked {
					t.Fatalf("observed child refused: %+v", r.Diagnostics)
				}
				return
			}
			if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF107" || r.Diagnostics[0].Message != "undeclared failures: "+tc.refused {
				t.Fatalf("want EF107 undeclared %s at main, got %+v", tc.refused, r.Diagnostics)
			}
		})
	}
}

// TestForkSiteChargesNoFailure: forking charges the child's requirements,
// not its failures; the joining run charges them.
// A join nested in an operand observes its child exactly as a join at
// statement level does (M1): in a call argument, an operand, a nested scope,
// an if arm and a match arm. It counts only on the exits after it completed,
// and only if every arm of a branch observes.
func TestNestedJoinsObserveTheirChild(t *testing.T) {
	const prelude = `error A
error B
enum Pick { X, Y }
effect fn task() -> string raises {A} { fail A }
effect fn other() -> string raises {B} { "other" }
fn id(s: string) -> string { s }
fn both(a: string, b: string) -> string { a + b }
fn drop(v: void) -> string { "dropped" }
effect fn lift(s: string) -> string { s }
`
	for _, tc := range []struct {
		name  string
		stage string
		// refused is the label the recovering caller leaves undeclared.
		refused string
	}{
		{name: "call argument", stage: `{ let f = fork task() id(run f.join()) }`},
		{name: "nested call arguments", stage: `{ let f = fork task() id(id(run f.join())) }`},
		{name: "effect call argument", stage: `{ let f = fork task() run lift(run f.join()) }`},
		{name: "let of a call", stage: `{ let f = fork task() let s = id(run f.join()) s }`},
		{name: "operand", stage: `{ let f = fork task() "x" + run f.join() }`},
		{name: "nested scope in an argument", stage: `{ let f = fork task() id(scope { run f.join() }) }`},
		{name: "interrupt in an argument", stage: `{ let f = fork task() drop(run f.interrupt()) }`},
		{name: "every match arm", stage: `{ let f = fork task() match Pick.X {} { Pick.X => run f.join() Pick.Y => run f.join() } }`},
		{name: "every match arm in an argument", stage: `{ let f = fork task() id(match Pick.X {} { Pick.X => run f.join() Pick.Y => run f.join() }) }`},
		{name: "every if arm in an argument", stage: `{ let f = fork task() id(if true { run f.join() } else { run f.join() }) }`},
		{name: "join before the failing operand", stage: `{ let f = fork task() both(run f.join(), run other()) }`},
		{name: "one match arm", stage: `{ let f = fork task() match Pick.X {} { Pick.X => run f.join() Pick.Y => "skipped" } }`, refused: "A"},
		{name: "one match arm in an argument", stage: `{ let f = fork task() id(match Pick.X {} { Pick.X => "skipped" Pick.Y => run f.join() }) }`, refused: "A"},
		{name: "one if arm in an argument", stage: `{ let f = fork task() id(if true { run f.join() } else { "skipped" }) }`, refused: "A"},
		{name: "failing operand before the join", stage: `{ let f = fork task() both(run other(), run f.join()) }`, refused: "A"},
		{name: "failing operand before the join, nested", stage: `{ let f = fork task() id(both(run other(), run f.join())) }`, refused: "A"},
		{name: "join in a forked recipe's operand", stage: `{ let f = fork task() let g = fork lift(run f.join()) run g.join() }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := prelude + "effect fn stage() -> string raises {A, B} " + tc.stage + `
effect fn main() -> string raises {B} { run stage().catch<A>("handled") }`
			r := CompileFor(source, "go")
			if tc.refused == "" {
				if !r.Checked {
					t.Fatalf("observed child refused: %+v", r.Diagnostics)
				}
				return
			}
			if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF107" || r.Diagnostics[0].Message != "undeclared failures: "+tc.refused {
				t.Fatalf("want EF107 undeclared %s at main, got %+v", tc.refused, r.Diagnostics)
			}
		})
	}
}

func TestForkSiteChargesNoFailure(t *testing.T) {
	r := CompileFor(`error A
effect fn task() -> string raises {A} { fail A }
effect fn main() -> string raises {A} { let f = fork task() run f.join() }`, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
}

// TestCallableContractsCarryNoPendingFailure: a callable value or a service
// method is invoked by callers that cannot see its body, so it may not leave
// a forked child unobserved.
func TestCallableContractsCarryNoPendingFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "function value", source: `error A
effect fn task() -> string raises {A} { fail A }
effect fn stage() -> string raises {A} { let f = fork task() "done" }
effect fn apply(cb: effect fn() -> string raises {A}) -> string raises {A} { run cb() }
effect fn main() -> string raises {A} { run apply(stage) }`},
		{name: "service method", source: `error A
effect fn task() -> string raises {A} { fail A }
service Work {
    effect fn start() -> string raises { A }
}
impl Background for Work {
    effect fn start() -> string raises { A } { let f = fork task() "done" }
}
effect fn main() -> string raises {A} { run Work.start().provide<Work>(Background) }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := CompileFor(tc.source, "go")
			if r.Checked || !hasCode(r, "EF107") {
				t.Fatalf("pending contract admitted: %+v", r.Diagnostics)
			}
			found := false
			for _, d := range r.Diagnostics {
				found = found || d.Code == "EF107" && strings.Contains(d.Message, "may leave forked child failures unobserved (A)")
			}
			if !found {
				t.Fatalf("refused for another reason: %+v", r.Diagnostics)
			}
		})
	}
}

// TestCarriedPendingEndsAtTypeBoundaries: a recipe that may leave a forked
// child unobserved keeps its evidence while it is bound, stored in local
// data or returned by a named call, so recovering it there keeps the
// pending label. Where only a type reaches the consumer (an argument, a
// failure payload, a joined result) passing it is refused.
func TestCarriedPendingEndsAtTypeBoundaries(t *testing.T) {
	const prelude = `error A
record Holder { task: Effect<string, {A}> }
error Carry { task: Effect<string, {A}> }
effect fn task() -> string raises {A} { fail A }
effect fn stage() -> string raises {A} { let f = fork task() "done" }
`
	for _, tc := range []struct {
		name    string
		program string
		message string
	}{
		{name: "local data", program: `effect fn main() -> string { let h = Holder { task: stage() } run h.task.catch<A>("h") }`, message: "undeclared failures: A"},
		{name: "named result", program: `fn make() -> Holder { Holder { task: stage() } }
effect fn main() -> string { run make().task.catch<A>("h") }`, message: "undeclared failures: A"},
		{name: "argument", program: `effect fn handle(r: Effect<string, {A}>) -> string { run r.catch<A>("h") }
effect fn main() -> string { run handle(stage()) }`, message: "recipe may leave forked child failures unobserved (A)"},
		{name: "argument field", program: `effect fn handle(h: Holder) -> string { run h.task.catch<A>("h") }
effect fn main() -> string { run handle(Holder { task: stage() }) }`, message: "recipe may leave forked child failures unobserved (A)"},
		{name: "failure payload", program: `effect fn throw() -> string raises {Carry} { fail Carry { task: stage() } }
effect fn handler(c: Carry) -> string { run c.task.catch<A>("h") }
effect fn main() -> string { run throw().recover<Carry>(handler) }`, message: "recipe may leave forked child failures unobserved (A)"},
		{name: "joined result", program: `effect fn make() -> Holder { Holder { task: stage() } }
effect fn main() -> string { let f = fork make() let h = run f.join() run h.task.catch<A>("h") }`, message: "recipe may leave forked child failures unobserved (A)"},
		{name: "callable result", program: `fn make() -> Holder { Holder { task: stage() } }
effect fn apply(cb: fn() -> Holder) -> string { run cb().task.catch<A>("h") }
effect fn main() -> string { run apply(make) }`, message: "make may leave forked child failures unobserved (A)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := CompileFor(prelude+tc.program, "go")
			found := false
			for _, d := range r.Diagnostics {
				found = found || d.Code == "EF107" && strings.HasPrefix(d.Message, tc.message)
			}
			if r.Checked || !found {
				t.Fatalf("want EF107 %q, got %+v", tc.message, r.Diagnostics)
			}
		})
	}
}
