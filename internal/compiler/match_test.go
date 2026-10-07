package compiler

import (
	"fmt"
	"strings"
	"testing"
)

// A state x event decision table: the first real caller for product matches.
const sessionStepSource = `enum Session { Idle; Active { key: string }; Closed }
enum Event { Open { key: string }; Refresh { key: string }; Close }
enum Step { Go { next: Session }; Stay; Reject { reason: string } }
fn step(state: Session, event: Event) -> Step {
 match state, event {
  Session.Idle, Event.Open { key } | Event.Refresh { key } => Step.Go { next: Session.Active { key: key } }
  Session.Active { key: current }, Event.Open { key } => if current == key { Step.Stay {} } else { Step.Reject { reason: "busy " + current } }
  Session.Active, Event.Refresh { key } => Step.Go { next: Session.Active { key: key } }
  Session.Idle | Session.Active, Event.Close => Step.Go { next: Session.Closed {} }
  Session.Closed, Event.Open | Event.Refresh | Event.Close => Step.Reject { reason: "closed" }
 }
}
fn state(value: Session) -> string { match value { Session.Idle => "idle"; Session.Active { key } => "active " + key; Session.Closed => "closed" } }
fn describe(value: Step) -> string { match value { Step.Go { next } => state(next); Step.Stay => "stay"; Step.Reject { reason } => "reject " + reason } }
effect fn main() -> string {
 describe(step(Session.Idle {}, Event.Open { key: "a" })) + ";" + describe(step(Session.Active { key: "a" }, Event.Open { key: "b" })) + ";" + describe(step(Session.Active { key: "a" }, Event.Open { key: "a" })) + ";" + describe(step(Session.Active { key: "a" }, Event.Refresh { key: "c" })) + ";" + describe(step(Session.Active { key: "a" }, Event.Close {})) + ";" + describe(step(Session.Closed {}, Event.Refresh { key: "x" }))
}`

const sessionStepOutput = "active a;reject busy a;stay;active c;closed;reject closed"

// Option/Result pairing plus a generic enum whose alternatives share a binder.
const absencePairingSource = `import Data "effra/data"
record User { name: string }
record Missing { message: string }
enum Reply<T: type> { Fresh { value: T }; Stale { value: T }; Gone }
fn pick(cached: Data.Option<User>, fetched: Data.Result<User, Missing>) -> string {
 match cached, fetched {
  Data.Option.Some { value: user }, Data.Result.Ok | Data.Result.Err => "cached " + user.name
  Data.Option.None, Data.Result.Ok { value: user } => "fetched " + user.name
  Data.Option.None, Data.Result.Err { error } => "missing " + error.message
 }
}
fn latest(reply: Reply<User>, fallback: Data.Option<User>) -> string {
 match reply, fallback {
  Reply.Fresh { value: user } | Reply.Stale { value: user }, Data.Option.None | Data.Option.Some => user.name
  Reply.Gone, Data.Option.Some { value: user } => "fallback " + user.name
  Reply.Gone, Data.Option.None => "none"
 }
}
fn ada() -> User { User { name: "Ada" } }
fn none() -> Data.Option<User> { Data.Option<User>.None {} }
effect fn main() -> string {
 pick(Data.Option.Some { value: ada() }, Data.Result<User, Missing>.Err { error: Missing { message: "x" } }) + ";" + pick(none(), Data.Result<User, Missing>.Ok { value: ada() }) + ";" + pick(none(), Data.Result<User, Missing>.Err { error: Missing { message: "gone" } }) + ";" + latest(Reply<User>.Stale { value: ada() }, none()) + ";" + latest(Reply<User>.Gone {}, Data.Option.Some { value: User { name: "Bo" } }) + ";" + latest(Reply<User>.Gone {}, none())
}`

const absencePairingOutput = "cached Ada;fetched Ada;missing gone;Ada;fallback Bo;none"

// Effectful subjects print as they are evaluated; the table fails in one arm.
const orderedSubjectsSource = `enum Light { Red; Green }
enum Signal { Stop; Go }
error Blocked { light: string }
effect fn light(label: string, red: bool) -> Light {
 run Console.log(label).provide<Console>(Stdout)
 if red { Light.Red {} } else { Light.Green {} }
}
effect fn signal(label: string) -> Signal {
 run Console.log(label).provide<Console>(Stdout)
 Signal.Go {}
}
effect fn decide(red: bool) -> string raises {Blocked} {
 match run light("first", red), run signal("second") {
  Light.Red, Signal.Stop | Signal.Go => fail Blocked { light: "red" }
  Light.Green, Signal.Stop => "wait"
  Light.Green, Signal.Go => "go"
 }
}
effect fn main() -> string raises {Blocked} { run decide(false) }`

func TestMatchProductsRunOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, source, output string }{
		{"state event table", sessionStepSource, sessionStepOutput},
		{"option result pairing", absencePairingSource, absencePairingOutput},
		{"ordered subjects", orderedSubjectsSource, "first\nsecond\ngo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runGenericDataNative(t, tc.source, tc.output+"\n")
			assertion := fmt.Sprintf(`console.log(await Effect.runPromise(__ef_function_main()));`)
			if output := runJS(t, tc.source, assertion); output != tc.output+"\n" {
				t.Fatalf("JS product match: %q", output)
			}
		})
	}
	r := Compile(orderedSubjectsSource)
	decide := r.Find("decide")
	if decide == nil || len(decide.Actual.Errors) != 1 || decide.Actual.Errors[0] != "Blocked" {
		t.Fatalf("failure raised by a product arm lost from the row: %+v", decide)
	}
}

func TestMatchProductFailingArmOnBothBackends(t *testing.T) {
	source := strings.Replace(orderedSubjectsSource, "run decide(false)", `run decide(true).catch<Blocked>("blocked")`, 1)
	runGenericDataNative(t, source, "first\nsecond\nblocked\n")
	if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "first\nsecond\nblocked\n" {
		t.Fatalf("JS failing product arm: %q", output)
	}
}

func TestMatchProductDiagnostics(t *testing.T) {
	const lights = "enum Light { Red; Amber; Green }\nenum Signal { Stop; Go { speed: i64 }; Wait { speed: i64 } }\nenum Mixed { Fast { speed: i64 }; Named { speed: string } }\n"
	for _, tc := range []struct {
		name, source, code, message string
	}{
		{"missing pair", strings.Replace(sessionStepSource, "Event.Open | Event.Refresh | Event.Close", "Event.Open | Event.Refresh", 1), "EF117", "missing match arm for Session.Closed, Event.Close"},
		{"unreachable arm", strings.Replace(sessionStepSource, "  Session.Closed,", "  Session.Idle, Event.Close => Step.Stay {}\n  Session.Closed,", 1), "EF117", "unreachable match arm"},
		{"unreachable alternative", lights + `fn f(l: Light, s: Signal) -> string { match l, s { Light.Red, Signal.Stop => "a"; Light.Red | Light.Amber | Light.Green, Signal.Stop => "b"; Light.Red | Light.Amber | Light.Green, Signal.Go | Signal.Wait => "c" } }`, "EF117", "unreachable alternative Light.Red"},
		{"duplicate alternative", lights + `fn f(l: Light) -> string { match l { Light.Red | Light.Red => "a"; Light.Amber | Light.Green => "b" } }`, "EF117", "duplicate alternative Light.Red"},
		{"inconsistent names", lights + `fn f(s: Signal) -> string { match s { Signal.Stop => "a"; Signal.Go { speed } | Signal.Wait => "b" } }`, "EF121", "alternatives must bind the same names"},
		{"inconsistent types", lights + `fn f(m: Mixed) -> string { match m { Mixed.Fast { speed } | Mixed.Named { speed } => "b" } }`, "EF121", "has a different type"},
		{"cross subject binding", lights + `fn f(a: Signal, b: Signal) -> string { match a, b { Signal.Go { speed } | Signal.Wait { speed }, Signal.Go { speed } | Signal.Wait { speed } => "x"; Signal.Stop, Signal.Stop | Signal.Go | Signal.Wait => "y"; Signal.Go | Signal.Wait, Signal.Stop => "z" } }`, "EF121", "duplicate pattern binding speed"},
		{"arm arity", lights + `fn f(l: Light, s: Signal) -> string { match l, s { Light.Red => "a" } }`, "EF118", "1 subject pattern(s); the match has 2 subject(s)"},
		{"catch-all subject", lights + `fn f(l: Light, s: Signal) -> string { match l, s { _, Signal.Stop => "a" } }`, "EF118", "catch-all"},
		{"wrong subject enum", lights + `fn f(l: Light, s: Signal) -> string { match l, s { Signal.Stop, Light.Red => "a" } }`, "EF116", "expected Light"},
		{"effect subject", lights + `effect fn red() -> Light { Light.Red {} }
effect fn f(s: Signal) -> string { match red(), s { Light.Red | Light.Amber | Light.Green, Signal.Stop | Signal.Go | Signal.Wait => "a" } }`, "EF106", "match subject must be a value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(tc.source)
			if r.Checked {
				t.Fatalf("admitted: %s", tc.source)
			}
			for _, diagnostic := range r.Diagnostics {
				if diagnostic.Code == tc.code && strings.Contains(diagnostic.Message, tc.message) {
					return
				}
			}
			t.Fatalf("want %s %q, got %+v", tc.code, tc.message, r.Diagnostics)
		})
	}
}

func TestMatchProductWitnessReportIsBounded(t *testing.T) {
	r := Compile(`enum Three { A; B; C }
fn f(x: Three, y: Three, z: Three) -> string { match x, y, z { Three.A, Three.A, Three.A => "a" } }`)
	missing := []string{}
	omitted := false
	for _, diagnostic := range r.Diagnostics {
		switch {
		case strings.HasPrefix(diagnostic.Message, "missing match arm for "):
			missing = append(missing, strings.TrimPrefix(diagnostic.Message, "missing match arm for "))
		case diagnostic.Message == "further uncovered match combinations omitted":
			omitted = true
		}
	}
	// Witnesses follow declaration order; the 26 uncovered triples are not
	// all listed, and the report says so instead of implying completeness.
	if len(missing) != maxReportedMissingMatchArms || missing[0] != "Three.A, Three.A, Three.B" || missing[2] != "Three.A, Three.B, Three.A" || !omitted {
		t.Fatalf("bounded witnesses: %q omitted=%v", missing, omitted)
	}
}

// cyclicProductSource builds n Bit subjects whose arms each fix one adjacent
// pair. Distinguishable combinations grow exponentially with n while the
// source grows linearly.
func cyclicProductSource(n int) string {
	var source strings.Builder
	source.WriteString("enum Bit { Zero; One }\nfn f(")
	for index := range n {
		if index > 0 {
			source.WriteString(", ")
		}
		fmt.Fprintf(&source, "b%d: Bit", index)
	}
	source.WriteString(") -> string {\n match ")
	for index := range n {
		if index > 0 {
			source.WriteString(", ")
		}
		fmt.Fprintf(&source, "b%d", index)
	}
	source.WriteString(" {\n")
	for arm := range n {
		cells := make([]string, n)
		for column := range n {
			switch column {
			case arm:
				cells[column] = "Bit.Zero"
			case (arm + 1) % n:
				cells[column] = "Bit.One"
			default:
				cells[column] = "Bit.Zero | Bit.One"
			}
		}
		fmt.Fprintf(&source, "  %s => \"%d\"\n", strings.Join(cells, ", "), arm)
	}
	source.WriteString(" }\n}\n")
	return source.String()
}

func TestMatchProductCoverageIsBounded(t *testing.T) {
	within := Compile(cyclicProductSource(10))
	if hasCode(within, matchCoverageExhaustedCode) || !hasCode(within, "EF117") {
		t.Fatalf("10-subject product should finish analysis and report real witnesses: %+v", within.Diagnostics)
	}
	exhausted := Compile(cyclicProductSource(14))
	if exhausted.Checked || !hasCode(exhausted, matchCoverageExhaustedCode) {
		t.Fatalf("14-subject product must refuse with an exhaustion diagnostic: %+v", exhausted.Diagnostics)
	}
	for _, diagnostic := range exhausted.Diagnostics {
		if strings.HasPrefix(diagnostic.Message, "missing match arm") || strings.HasPrefix(diagnostic.Message, "unreachable") {
			t.Fatalf("exhausted analysis published a partial coverage claim: %+v", diagnostic)
		}
	}
	// The budget is a work count, so refusal is deterministic and stops near
	// the bound instead of finishing the exponential walk.
	subjects := make([]matchPlanSubject, 14)
	for index := range subjects {
		subjects[index].variants = []string{"Zero", "One"}
	}
	coverage := newMatchCoverage(subjects)
	for arm := range 14 {
		row := make([]variantSet, 14)
		for column := range row {
			row[column] = newVariantSet(2)
			if column != (arm+1)%14 {
				row[column].add(0)
			}
			if column != arm {
				row[column].add(1)
			}
		}
		coverage.rows = append(coverage.rows, row)
	}
	coverage.missing()
	if !coverage.exhausted || coverage.work > maxMatchCoverageWork+64 {
		t.Fatalf("coverage work was not bounded: work=%d exhausted=%v", coverage.work, coverage.exhausted)
	}
}

func TestMatchProductFormattingRoundTrip(t *testing.T) {
	for _, source := range []string{sessionStepSource, absencePairingSource, orderedSubjectsSource} {
		first, err := FormatSource(source)
		if err != nil {
			t.Fatal(err)
		}
		second, err := FormatSource(first.Text)
		if err != nil || second.Changed || second.Text != first.Text {
			t.Fatalf("formatting is not idempotent: %v\n%s\n---\n%s", err, first.Text, second.Text)
		}
		if r := Compile(first.Text); !r.Checked {
			t.Fatalf("formatted product match no longer checks: %+v\n%s", r.Diagnostics, first.Text)
		}
	}
	formatted, _ := FormatSource(sessionStepSource)
	for _, line := range []string{
		"    match state, event {\n",
		"        Session.Idle, Event.Open { key } | Event.Refresh { key } => Step.Go {",
		"        Session.Closed, Event.Open | Event.Refresh | Event.Close => Step.Reject {",
	} {
		if !strings.Contains(formatted.Text, line) {
			t.Fatalf("formatter split a product arm; want %q in\n%s", line, formatted.Text)
		}
	}
}

func TestMatchProductBindingOwnership(t *testing.T) {
	const prefix = `enum Packet { A { file: File } B { file: File } }
effect fn pick(borrowed: File) -> File raises {IoError} uses {Files} {
 scope {
  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  let safe = Packet.A { file: borrowed }
  let owned = Packet.B { file: inner }
  match safe, owned {
`
	const suffix = `
 }
}
effect fn main() -> void { void }
`
	// Each binder projects its own subject's payload.
	requireOwnershipAccepted(t, prefix+`   Packet.A { file: kept } | Packet.B { file: kept }, Packet.A { file } | Packet.B { file } => kept
  }`+suffix)
	requireOwnershipRejected(t, prefix+`   Packet.A { file: kept } | Packet.B { file: kept }, Packet.A { file } | Packet.B { file } => file
  }`+suffix)
	// The inner handle is only in the second alternative; a binder joined from
	// the first alternative alone would let it escape the scope.
	requireOwnershipRejected(t, prefix+`   Packet.A | Packet.B, Packet.A { file } | Packet.B { file } => file
  }`+suffix)
}

func TestMatchProductLexicalBindingsResolveToFirstAlternative(t *testing.T) {
	r := Compile(sessionStepSource)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	use := strings.Index(sessionStepSource, "Session.Active { key: key } }\n  Session.Active { key: current }") + len("Session.Active { key: ")
	declaration := strings.Index(sessionStepSource, "Event.Open { key }") + len("Event.Open { ")
	for e, id := range r.lexical.uses {
		if e.Kind == "name" && e.Span.Offset == use {
			if binding := r.lexical.bindings[id]; binding.NameSpan.Offset != declaration {
				t.Fatalf("or-pattern use resolved to %+v, want offset %d", binding, declaration)
			}
			return
		}
	}
	t.Fatal("or-pattern binder use was not recorded")
}

// alternativeCallableSource binds f from two alternatives whose callable payload
// types are written as first and second.
func alternativeCallableSource(first, second string) string {
	return `error Bad
service Secret { effect fn read() -> string }
enum Choice {
 A { f: effect fn() -> string` + first + ` }
 B { f: effect fn() -> string` + second + ` }
}
effect fn bad() -> string raises {Bad} { fail Bad }
effect fn good() -> string raises {Bad} { "good" }
effect fn choose(c: Choice) -> string raises {Bad} {
 match c { Choice.A { f } | Choice.B { f } => run f() }
}
effect fn main() -> string {
 let first = run choose(Choice.A { f: good }).catch<Bad>("caught")
 let second = run choose(Choice.B { f: bad }).catch<Bad>("caught")
 first + ";" + second
}`
}

func TestMatchAlternativeBindersRequireIdenticalCallableRows(t *testing.T) {
	// A binder carries one payload contract. Admitting rows from only the first
	// alternative would drop an undeclared failure or a missing service from
	// the enclosing function, depending on the order alternatives are written.
	for _, tc := range []struct{ name, first, second, message string }{
		{"failure row second", "", " raises {Bad}", "callable raises {Bad} versus {}"},
		{"failure row first", " raises {Bad}", "", "callable raises {} versus {Bad}"},
		{"service row second", "", " uses {Secret}", "callable uses {Secret} versus {}"},
		{"service row first", " uses {Secret}", "", "callable uses {} versus {Secret}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(alternativeCallableSource(tc.first, tc.second))
			if r.Checked {
				t.Fatal("alternatives with different callable rows were admitted")
			}
			for _, diagnostic := range r.Diagnostics {
				if diagnostic.Code == "EF121" && strings.Contains(diagnostic.Message, "alternative binding f has a different type") && strings.Contains(diagnostic.Message, tc.message) {
					return
				}
			}
			t.Fatalf("want EF121 naming %q, got %+v", tc.message, r.Diagnostics)
		})
	}
	source := alternativeCallableSource(" raises {Bad}", " raises {Bad}")
	runGenericDataNative(t, source, "good;caught\n")
	if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "good;caught\n" {
		t.Fatalf("JS identical-row alternatives: %q", output)
	}
	if choose := Compile(source).Find("choose"); choose == nil || len(choose.Actual.Errors) != 1 || choose.Actual.Errors[0] != "Bad" {
		t.Fatalf("joined callable binder lost its failure row: %+v", choose)
	}
}
