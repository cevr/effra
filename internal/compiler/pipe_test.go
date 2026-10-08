package compiler

import (
	"strings"
	"testing"
)

const pipeDeclarations = `fn shout(text: string) -> string { text + "!" }
fn zero() -> string { "z" }
fn wrap(text: string, open: string, close: string) -> string { open + text + close }
fn pair(left: string, right: string) -> string { left + right }
record Card { title: string }
effect fn load() -> string { "l" }
`

// pipeProbe compiles one function body after the shared declarations.
func pipeProbe(body string) (string, *Result) {
	source := pipeDeclarations + "fn probe() -> string { " + body + " }\n"
	return source, CompileFor(source, "js")
}

// callText renders a call tree the way the hand-written nested program reads,
// so a desugaring is compared as a shape rather than as bytes.
func callText(e *Expr) string {
	switch e.Kind {
	case "call":
		args := make([]string, len(e.Args))
		for i, arg := range e.Args {
			args[i] = callText(arg)
		}
		callee := callText(e.Left)
		for _, field := range e.Fields {
			for i, arg := range e.Args {
				if arg == field.Value {
					args[i] = field.Name + ": " + args[i]
				}
			}
		}
		return callee + "(" + strings.Join(args, ", ") + ")"
	case "member":
		return callText(e.Left) + "." + e.Name
	case "name":
		return e.Name
	case "string":
		return `"` + e.Text + `"`
	case "binary":
		return "(" + callText(e.Left) + " " + e.Name + " " + callText(e.Right) + ")"
	case "run":
		return "run " + callText(e.Left)
	}
	return e.Kind
}

func probeBodyText(t *testing.T, r *Result) string {
	t.Helper()
	for _, function := range r.Program.Functions {
		if function.Name == "probe" {
			return callText(function.Body.Statements[len(function.Body.Statements)-1].Value)
		}
	}
	t.Fatal("no probe function")
	return ""
}

// A pipe is a parse-time rewrite: the piped value becomes positional argument
// one of the ordinary call, pipes associate left and the postfix chain
// continues after the call.
func TestPipeDesugarsToOrdinaryCalls(t *testing.T) {
	for _, test := range []struct{ name, body, want string }{
		{"single", `"x" |> shout()`, `shout("x")`},
		{"extra arguments", `"x" |> wrap("<", ">")`, `wrap("x", "<", ">")`},
		{"labels bind as usual", `"x" |> wrap(close: ">", open: "<")`, `wrap("x", close: ">", open: "<")`},
		{"left associative", `"x" |> shout() |> wrap("<", ">") |> shout()`, `shout(wrap(shout("x"), "<", ">"))`},
		{"path callee", `"x" |> Text.trim()`, `Text.trim("x")`},
		{"chain continues after call", `"x" |> shout().length`, `shout("x").length`},
		{"continues into another pipe", `"x" |> shout().length |> shout()`, `shout(shout("x").length)`},
		{"line leading", "\"x\"\n        |> shout()\n        |> shout()", `shout(shout("x"))`},
		{"nested pipe argument", `"x" |> pair("a" |> shout())`, `pair("x", shout("a"))`},
		{"run takes the whole chain", `run load() |> shout()`, `run shout(load())`},
		{"parenthesised run pipes the result", `(run load()) |> shout()`, `shout(run load())`},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, diagnostics := parse(pipeDeclarations + "fn probe() -> string { " + test.body + " }\n")
			if len(diagnostics) > 0 {
				t.Fatalf("diagnostics = %+v", diagnostics)
			}
			got := probeBodyText(t, &Result{Program: program})
			if got != test.want {
				t.Fatalf("desugared = %s, want %s", got, test.want)
			}
		})
	}
}

func TestPipeRecordsTheOperatorSpanForTooling(t *testing.T) {
	source := pipeDeclarations + "fn probe() -> string { \"x\" |> shout() }\n"
	program, diagnostics := parse(source)
	if len(diagnostics) > 0 {
		t.Fatal(diagnostics)
	}
	var call *Expr
	for _, function := range program.Functions {
		if function.Name == "probe" {
			call = function.Body.Statements[0].Value
		}
	}
	if call == nil || call.Kind != "call" || call.PipeSpan.Length != 2 || source[call.PipeSpan.Offset:call.PipeSpan.Offset+2] != "|>" {
		t.Fatalf("call = %+v", call)
	}
}

// Both operators, either side: an unparenthesised pipe chain beside a binary
// operator is a syntax error, so a + b |> f() can never read as a + f(b).
func TestPipeBesideBinaryOperatorsIsRejected(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"plus right", `"a" + "b" |> shout()`},
		{"plus left", `"a" |> shout() + "b"`},
		{"equals right", `"a" == "b" |> shout()`},
		{"equals left", `"a" |> shout() == "b"`},
		{"plus both", `"a" |> shout() + "b" |> shout()`},
		{"member continuation", `"a" |> shout().length + "b"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, r := pipeProbe(test.body)
			if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF002" || !strings.Contains(r.Diagnostics[0].Message, "parenthesise") {
				t.Fatalf("diagnostics = %+v", r.Diagnostics)
			}
			span := r.Diagnostics[0].Span
			if got := source[span.Offset : span.Offset+span.Length]; got != "|>" {
				t.Fatalf("anchored at %q, want the |> token", got)
			}
		})
	}
}

func TestParenthesisedPipesBesideBinaryOperatorsAreAccepted(t *testing.T) {
	for _, body := range []string{
		`("a" |> shout()) + "b"`,
		`"a" + ("b" |> shout())`,
		`("a" |> shout()) + ("b" |> shout())`,
		`("a" + "b") |> shout()`,
		`"a" + ("b" |> shout()) + "c"`,
		`pair("a" |> shout(), "b") + "c"`,
	} {
		if _, r := pipeProbe(body); !r.Checked {
			t.Errorf("%s rejected: %+v", body, r.Diagnostics)
		}
	}
}
