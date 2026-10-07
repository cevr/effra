package compiler

import (
	"slices"
	"strings"
	"testing"
)

const callArgumentDeclarations = `record Card { title: string, body: string }
fn card(title: string, body: string) -> Card { Card { title, body } }
effect fn joined(first: string, second: string) -> string { first + second }
service Pairs { effect fn join(left: string, right: string) -> string }
impl Prefixed(prefix: string, suffix: string) for Pairs {
    effect fn join(left: string, right: string) -> string { prefix + left + right + suffix }
}
effect fn call<E: raises, R: uses>(callback: effect fn(string) -> string raises {E} uses {R}, input: string) -> string raises {E} uses {R} { run callback(input) }
effect fn shout(text: string) -> string { text + "!" }
`

// expectedDiagnostic names one diagnostic by its message and the token its
// span covers: token is found inside the unique context substring.
type expectedDiagnostic struct {
	message, context, token string
}

func TestCallArgumentsBindByLabel(t *testing.T) {
	for _, test := range []struct {
		name, body  string
		permutation []int
		diagnostics []expectedDiagnostic
		// cascades admits further diagnostics after the expected ones. A
		// rejected binding leaves row parameters uninferred, as a positional
		// arity error does.
		cascades bool
	}{
		{name: "positional", body: `fn probe() -> Card { card("t", "b") }`},
		{name: "labels in parameter order", body: `fn probe() -> Card { card(title: "t", body: "b") }`},
		{name: "reordered labels", body: `fn probe() -> Card { card(body: "b", title: "t") }`, permutation: []int{1, 0}},
		{name: "positional then label", body: `fn probe() -> Card { card("t", body: "b") }`},
		{name: "effect fn", body: `effect fn probe() -> string { run joined(second: "y", first: "x") }`, permutation: []int{1, 0}},
		{name: "service operation", body: `effect fn probe() -> string uses { Pairs } { run Pairs.join(right: "r", left: "l") }`, permutation: []int{1, 0}},
		{name: "provider constructor", body: `effect fn probe() -> string { let pairs = run Prefixed(suffix: ">", prefix: "<") run Pairs.join("l", "r").provide<Pairs>(pairs) }`, permutation: []int{1, 0}},
		{name: "row-polymorphic callback", body: `effect fn probe() -> string { run call(input: "x", callback: shout) }`, permutation: []int{1, 0}},
		{name: "unknown label", body: `fn probe() -> Card { card(title: "t", heading: "b") }`, diagnostics: []expectedDiagnostic{
			{"unknown argument label heading", `heading: "b"`, "heading"},
			{"missing argument for parameter body", `card(title: "t", heading`, "card"},
		}},
		{name: "duplicate label", body: `fn probe() -> Card { card(title: "t", title: "u") }`, diagnostics: []expectedDiagnostic{
			{"duplicate argument label title", `title: "u"`, "title"},
			{"missing argument for parameter body", `card(title: "t", title`, "card"},
		}},
		{name: "row-polymorphic unknown label", body: `effect fn probe() -> string { run call(input: "x", handler: shout) }`, diagnostics: []expectedDiagnostic{
			{"unknown argument label handler", `handler: shout`, "handler"},
			{"missing argument for parameter callback", `call(input`, "call"},
		}, cascades: true},
		{name: "missing argument", body: `fn probe() -> Card { card(body: "b") }`, diagnostics: []expectedDiagnostic{
			{"missing argument for parameter title", `card(body`, "card"},
		}},
		{name: "label collides with positional", body: `fn probe() -> Card { card("t", title: "u") }`, diagnostics: []expectedDiagnostic{
			{"argument label title names a parameter already bound by positional argument 1", `title: "u"`, "title"},
			{"missing argument for parameter body", `card("t", title`, "card"},
		}},
		{name: "positional after label", body: `fn probe() -> Card { card(title: "t", "b") }`, diagnostics: []expectedDiagnostic{
			{"positional argument cannot follow a labelled argument", `, "b")`, `"b"`},
			{"missing argument for parameter body", `card(title: "t", "b")`, "card"},
		}},
		{name: "extra positional with label", body: `fn probe() -> Card { card("t", "b", "c", body: "d") }`, diagnostics: []expectedDiagnostic{
			{"argument label body names a parameter already bound by positional argument 2", `body: "d"`, "body"},
			{"incorrect argument count", `card("t", "b", "c"`, "card"},
		}},
		{name: "unknown service operation label", body: `effect fn probe() -> string uses { Pairs } { run Pairs.join(left: "l", rigth: "r") }`, diagnostics: []expectedDiagnostic{
			{"unknown argument label rigth", `rigth: "r"`, "rigth"},
			{"missing argument for parameter right", `Pairs.join(left`, "join"},
		}},
		{name: "duplicate provider label", body: `effect fn probe() -> string { let pairs = run Prefixed(prefix: "<", prefix: ">") run Pairs.join("l", "r").provide<Pairs>(pairs) }`, diagnostics: []expectedDiagnostic{
			{"duplicate argument label prefix", `prefix: ">"`, "prefix"},
			{"missing argument for parameter suffix", `Prefixed(prefix: "<"`, "Prefixed"},
		}},
		{name: "callable value label", body: `fn probe(step: fn(string) -> string) -> string { step(value: "x") }`, diagnostics: []expectedDiagnostic{
			{"callable values take positional arguments; remove label value", `value: "x"`, "value"},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := callArgumentDeclarations + test.body + "\n"
			r := Compile(source)
			assertCallDiagnostics(t, source, r, test.diagnostics, test.cascades)
			if len(test.diagnostics) > 0 {
				return
			}
			call := labelledProbeCall(r)
			if call == nil && test.permutation != nil {
				t.Fatal("probe call was not found")
			}
			if call != nil && !slices.Equal(call.ArgumentParameters, test.permutation) {
				t.Fatalf("argument parameters = %v, want %v", call.ArgumentParameters, test.permutation)
			}
		})
	}
}

func TestGoHostCallsRejectArgumentLabels(t *testing.T) {
	source := `import go strings "strings"
effect fn main() -> string { run strings.ToUpper(s: "x").provide<Foreign>(Host) }
`
	assertCallDiagnostics(t, source, Compile(source), []expectedDiagnostic{
		{"Go functions take positional arguments; remove label s", `s: "x"`, "s"},
	}, false)
}

func assertCallDiagnostics(t *testing.T, source string, r *Result, want []expectedDiagnostic, cascades bool) {
	t.Helper()
	if len(want) == 0 {
		if !r.Checked {
			t.Fatalf("source was rejected: %+v", r.Diagnostics)
		}
		return
	}
	if r.Checked || len(r.Diagnostics) < len(want) || !cascades && len(r.Diagnostics) != len(want) {
		t.Fatalf("diagnostics = %+v, want %+v", r.Diagnostics, want)
	}
	for i, expected := range want {
		got := r.Diagnostics[i]
		start := strings.Index(source, expected.context)
		if start < 0 || strings.Count(source, expected.context) != 1 || !strings.Contains(expected.context, expected.token) {
			t.Fatalf("ambiguous context %q", expected.context)
		}
		offset := start + strings.Index(expected.context, expected.token)
		if got.Code != "EF106" || got.Message != expected.message || got.Span.Offset != offset || got.Span.Length != len(expected.token) {
			t.Fatalf("diagnostic %d = %s %q at %d+%d (%q), want EF106 %q at %d+%d (%q)", i, got.Code, got.Message, got.Span.Offset, got.Span.Length, source[got.Span.Offset:got.Span.Offset+got.Span.Length], expected.message, offset, len(expected.token), expected.token)
		}
	}
}

// labelledProbeCall returns the first labelled call in the probe function.
func labelledProbeCall(r *Result) *Expr {
	for _, item := range r.Program.Items {
		if item.Function == nil || item.Function.Name != "probe" {
			continue
		}
		var found *Expr
		var visit func(*Expr)
		visit = func(e *Expr) {
			if e == nil || found != nil {
				return
			}
			if e.Kind == "call" && len(e.Fields) > 0 {
				found = e
				return
			}
			forEachExprChild(e, visit)
		}
		for _, statement := range item.Function.Body.Statements {
			visit(statement.Value)
		}
		return found
	}
	return nil
}
