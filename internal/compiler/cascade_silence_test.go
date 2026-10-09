package compiler

import (
	"strings"
	"testing"
)

const cascadeSilenceHeader = `record Label { text: string }
record Holder { task: Effect<string> }
enum Pick { A, B }
error Oops { text: string }
layer Builtin { Clock = LiveClock }
effect fn tick() -> string { "ok" }
effect fn take(x: string) -> string { x }
fn handle(o: Oops) -> string { "h" }
effect fn say() -> string uses { Console } {
    run Console.log("x")
    "y"
}
`

// cascadeSilenceConsumers consume the invalid local `s`, whose initializer is
// a scope with an unexecuted recipe tail (one EF105 on line 14).
var cascadeSilenceConsumers = []struct {
	name string
	body []string
}{
	{"run", []string{"run s"}},
	{"fork-join", []string{"let f = fork s", "run f.join()"}},
	{"timeout", []string{"run s.timeout(5)"}},
	{"provide-service", []string{"run s.provide<Console>(Stdout)"}},
	{"provide-layer", []string{"run s.provide(Builtin)"}},
	{"provider-operand", []string{"run say().provide<Console>(s)"}},
	{"catch", []string{`run s.catch<Oops>("c")`}},
	{"recover", []string{"run s.recover<Oops>(handle)"}},
	{"field", []string{"s.text"}},
	{"match-scrutinee", []string{"match s {", `    Pick.A => "a"`, `    Pick.B => "b"`, "}"}},
	{"if-condition", []string{`if s { "a" } else { "b" }`}},
	{"if-branch", []string{`if true { s } else { "b" }`}},
	{"match-arm", []string{"let p = Pick.A {}", "match p {", "    Pick.A => s", `    Pick.B => "b"`, "}"}},
	{"operator-plus", []string{`s + "x"`}},
	{"operator-eq", []string{`let b = s == "x"`, `"done"`}},
	{"call-argument", []string{"run take(s)"}},
	{"record-field", []string{"let l = Label { text: s }", "l.text"}},
	{"recipe-field", []string{"let h = Holder { task: s }", "run h.task"}},
	{"variant-payload", []string{"fail Oops { text: s }"}},
	{"return", []string{"s"}},
	{"statement", []string{"s", `"done"`}},
	{"callable-call", []string{"s()"}},
	{"fiber-join-direct", []string{"run s.join()"}},
	{"orFail", []string{"run s.orFail()"}},
	{"fiber-interrupt-direct", []string{"run s.interrupt()", `"done"`}},
	{"fiber-cancel-direct", []string{"run s.cancel()", `"done"`}},
	{"timeout-duration", []string{"run tick().timeout(s)"}},
}

// TestConsumersAreSilentOnInvalidOperands is the cascade contract (lane E2 R5
// design §7.2): a consumer of an already-reported invalid operand adds no
// diagnostic, so each program reports exactly its originating EF105.
func TestConsumersAreSilentOnInvalidOperands(t *testing.T) {
	for _, consumer := range cascadeSilenceConsumers {
		lines := append([]string{"let s = scope { tick() }"}, consumer.body...)
		source := cascadeSilenceHeader + "effect fn main() -> string raises { Oops, Timeout } uses { Scheduler } {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
		for _, target := range []string{"go", "js"} {
			diagnostics := CompileFor(source, target).Diagnostics
			if len(diagnostics) != 1 || diagnostics[0].Code != "EF105" || diagnostics[0].Span.Line != 14 {
				t.Errorf("%s/%s: want only the originating EF105 on line 14, got %v", consumer.name, target, diagnostics)
			}
		}
	}
}
