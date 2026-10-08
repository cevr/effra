package compiler

import (
	"reflect"
	"regexp"
	"slices"
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

// assertPipeDiagnostics checks each diagnostic's message and the anchor token
// found inside its unique context substring.
func assertPipeDiagnostics(t *testing.T, source string, diagnostics []Diagnostic, want ...expectedDiagnostic) {
	t.Helper()
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %+v, want %d", diagnostics, len(want))
	}
	for i, expected := range want {
		got := diagnostics[i]
		if strings.Count(source, expected.context) != 1 || !strings.Contains(expected.context, expected.token) {
			t.Fatalf("ambiguous context %q", expected.context)
		}
		offset := strings.Index(source, expected.context) + strings.Index(expected.context, expected.token)
		if got.Message != expected.message || got.Span.Offset != offset || got.Span.Length != len(expected.token) {
			t.Fatalf("diagnostic %d = %s %q at %d+%d (%q), want %q at %d+%d (%q)", i, got.Code, got.Message, got.Span.Offset, got.Span.Length, source[got.Span.Offset:got.Span.Offset+got.Span.Length], expected.message, offset, len(expected.token), expected.token)
		}
	}
}

func TestPipeRejectsAnythingButACallOnTheRight(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       expectedDiagnostic
	}{
		{"bare name", `"x" |> shout`, expectedDiagnostic{"the right side of |> must be a call; write shout()", `|> shout }`, "shout"}},
		{"bare path", `"x" |> Text.trim`, expectedDiagnostic{"the right side of |> must be a call; write Text.trim()", `Text.trim }`, "trim"}},
		{"bare before operator", `("x" |> shout) + "y"`, expectedDiagnostic{"the right side of |> must be a call; write shout()", `|> shout)`, "shout"}},
		{"bare in argument", `pair("x" |> shout, "y")`, expectedDiagnostic{"the right side of |> must be a call; write shout()", `|> shout,`, "shout"}},
		{"parenthesised callee", `"x" |> (shout)()`, expectedDiagnostic{"|> takes a function or operation name followed by arguments", `(shout)`, "("}},
		{"run operand", `"x" |> run shout()`, expectedDiagnostic{"|> takes a function or operation name followed by arguments", `run shout`, "run"}},
		{"literal", `"x" |> 1()`, expectedDiagnostic{"|> takes a function or operation name followed by arguments", `1()`, "1"}},
		{"nothing", `"x" |>`, expectedDiagnostic{"|> takes a function or operation name followed by arguments", `|> }`, "}"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, r := pipeProbe(test.body)
			if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF002" {
				t.Fatalf("diagnostics = %+v", r.Diagnostics)
			}
			assertPipeDiagnostics(t, source, r.Diagnostics, test.want)
		})
	}
}

// The checker may read PipeSpan to word and anchor a diagnostic.
func TestPipeDiagnosticsNameThePipe(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       []expectedDiagnostic
	}{
		{"no parameters", `"x" |> zero()`, []expectedDiagnostic{{"zero takes no parameters, so it cannot receive the piped value", `|> zero`, "|>"}}},
		{"label names the first parameter", `"x" |> wrap(text: "t", open: "<", close: ">")`, []expectedDiagnostic{{"the piped value already binds parameter text; remove label text", `text: "t"`, "text"}}},
		{"label names a later bound parameter", `"x" |> wrap("<", open: "o", close: ">")`, []expectedDiagnostic{{"argument label open names a parameter already bound by positional argument 2", `open: "o"`, "open"}}},
		{"wrong type", `1 |> shout()`, []expectedDiagnostic{{"argument must be string", `1 |>`, "1"}}},
		{"wrong type inside a chain", `"x" |> shout() |> wrap(1, ">") |> pair("y")`, []expectedDiagnostic{{"argument must be string", `1, ">"`, "1"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := pipeDeclarations + "effect fn probe() -> string { " + test.body + " }\n"
			r := CompileFor(source, "js")
			assertPipeDiagnostics(t, source, r.Diagnostics, test.want...)
		})
	}
}

func TestPipeOfAWrongTypedStepAnchorsAtThePipe(t *testing.T) {
	source := pipeDeclarations + "fn probe() -> string { \"x\" |> shout() |> wrap(\"<\", \">\") |> takesCard() }\nfn takesCard(card: Card) -> string { card.title }\n"
	r := CompileFor(source, "js")
	assertPipeDiagnostics(t, source, r.Diagnostics, expectedDiagnostic{"argument must be Card; piped from wrap(...): string", `|> takesCard`, "|>"})
}

func TestPipeIntoRunHintsAtParentheses(t *testing.T) {
	source := pipeDeclarations + "effect fn probe() -> string { run load() |> shout() }\n"
	r := CompileFor(source, "js")
	if len(r.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v", r.Diagnostics)
	}
	const hint = "argument must be string; |> binds inside run, so to pipe the result write (run load(...)) |> shout()"
	if got := r.Diagnostics[0]; got.Code != "EF106" || got.Message != hint {
		t.Fatalf("first diagnostic = %+v, want EF106 %q", got, hint)
	}
	if got := r.Diagnostics[1]; got.Code != "EF105" || got.Message != "run requires an Effect value" {
		t.Fatalf("second diagnostic = %+v", got)
	}
	// The parenthesised spelling is the repair the hint names.
	repaired := pipeDeclarations + "effect fn probe() -> string { (run load()) |> shout() }\n"
	if r := CompileFor(repaired, "js"); !r.Checked {
		t.Fatalf("repaired source rejected: %+v", r.Diagnostics)
	}
}

// clearPipeSpans zeroes PipeSpan on every call reachable through exported
// program fields.
func clearPipeSpans(program *Program) {
	seen := map[uintptr]bool{}
	var visit func(reflect.Value)
	visit = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			if expr, ok := v.Interface().(*Expr); ok {
				expr.PipeSpan = Span{}
			}
			visit(v.Elem())
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					visit(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				visit(v.Index(i))
			}
		case reflect.Map:
			for _, key := range v.MapKeys() {
				visit(v.MapIndex(key))
			}
		}
	}
	visit(reflect.ValueOf(program))
}

// pipeCalleeNames finds the function each |> in a body calls.
var pipeCalleeNames = regexp.MustCompile(`\|> (\w+)\(`)

// PipeSpan is for tooling: erasing it from every call leaves the accepted and
// rejected programs, the diagnostic codes and the emitted bytes unchanged.
func TestPipeSpanNeverDecidesCompilation(t *testing.T) {
	bodies := []string{
		`"x" |> shout()`,
		`"x" |> wrap(close: ">", open: "<")`,
		`"x" |> shout() |> wrap("<", ">") |> shout()`,
		`"x" |> zero()`,
		`"x" |> wrap(text: "t", open: "<", close: ">")`,
		`1 |> shout()`,
		`"x" |> shout() |> wrap("<", ">") |> takesCard()`,
		`run load() |> shout()`,
	}
	for _, body := range bodies {
		// main runs probe, so entry emission keeps the piped calls on both targets.
		source := pipeDeclarations + "effect fn takesCard(card: Card) -> string { card.title }\neffect fn probe() -> string { " + body + " }\neffect fn main() -> void {\n    let _ = run probe()\n}\n"
		for _, target := range []string{"go", "js"} {
			with := CompileAt(source, target, ".")
			without := compileTransformed(source, target, ".", clearPipeSpans)
			if with.Checked != without.Checked || len(with.Diagnostics) != len(without.Diagnostics) {
				t.Fatalf("%s [%s]: checked %v/%v, diagnostics %+v / %+v", body, target, with.Checked, without.Checked, with.Diagnostics, without.Diagnostics)
			}
			for i := range with.Diagnostics {
				if with.Diagnostics[i].Code != without.Diagnostics[i].Code {
					t.Fatalf("%s [%s]: code %s became %s", body, target, with.Diagnostics[i].Code, without.Diagnostics[i].Code)
				}
			}
			if !with.Checked {
				continue
			}
			a, b := emittedProgram(t, with), emittedProgram(t, without)
			if a != b {
				t.Fatalf("%s [%s]: emitted bytes differ", body, target)
			}
			// The comparison is empty unless the piped code was emitted.
			for _, callee := range pipeCalleeNames.FindAllStringSubmatch(body, -1) {
				if !strings.Contains(a, callee[1]) {
					t.Fatalf("%s [%s]: the emitted program never mentions %s, so the comparison saw no pipe code", body, target, callee[1])
				}
			}
		}
	}
}

// emittedProgram returns the target output of a checked result.
func emittedProgram(t *testing.T, r *Result) string {
	t.Helper()
	if r.Target == "js" {
		text, _, err := r.Emit(true)
		if err != nil {
			t.Fatal(err)
		}
		return text
	}
	app, err := r.GoApplication(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	return string(app.Main)
}

// A pipe call's extent is the whole call, subject through closing parenthesis,
// so a query at the operator or the whitespace around it selects the call and
// not the callee that follows it. The callee is a callable value here, which
// carries checked facts of its own.
func TestPipeOffsetsSelectTheWholeCall(t *testing.T) {
	const call = `x |> f()`
	source := `fn apply(f: fn(string) -> string, x: string) -> string { ` + call + ` }
`
	r := CompileFor(source, "js")
	if !r.Checked {
		t.Fatalf("source rejected: %+v", r.Diagnostics)
	}
	start := strings.Index(source, call)
	for _, test := range []struct {
		name   string
		offset int
		want   string
	}{
		{"subject", start, "x"},
		{"space before |>", start + 1, call},
		{"|", start + 2, call},
		{">", start + 3, call},
		{"space after |>", start + 4, call},
		{"callee", start + 5, "f"},
		{"parenthesis", start + 6, call},
	} {
		offset := test.offset
		query, err := r.QueryType(TypeSelection{Offset: &offset})
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		extent := query.Selection.Extent
		if got := source[extent.Offset : extent.Offset+extent.Length]; got != test.want {
			t.Errorf("%s: selected %q, want %q", test.name, got, test.want)
		}
	}
}

// The parser's own children list stays disjoint: the callee's extent starts at
// the callee, not at the subject that precedes it.
func TestPipeCalleeExtentStartsAtTheCallee(t *testing.T) {
	source := pipeDeclarations + `fn probe() -> string { "x" |> shout() |> wrap("<", ">") }
`
	program, diagnostics := parse(source)
	if len(diagnostics) > 0 {
		t.Fatal(diagnostics)
	}
	for _, e := range expressionsOf(program) {
		if e.Kind != "call" || e.PipeSpan.Length == 0 {
			continue
		}
		callee := source[e.Left.Extent.Offset : e.Left.Extent.Offset+e.Left.Extent.Length]
		if callee != "shout" && callee != "wrap" {
			t.Errorf("callee extent %q, want the callee name alone", callee)
		}
		if e.Extent.Offset > e.Args[0].Extent.Offset {
			t.Errorf("call extent %+v starts after its subject %+v", e.Extent, e.Args[0].Extent)
		}
	}
}

const pipeIntrinsicNames = `service S {
    effect fn timeout(x: string) -> string
    effect fn orFail(x: string) -> string
    effect fn get(x: string) -> string
}
`

// The intrinsic method names .timeout, .orFail, .provide and .catch are
// reserved after any receiver in the nested spelling, so S.timeout("x") is the
// timeout intrinsic. After |> they are ordinary members: x |> S.timeout() is an
// ordinary call. This pins both readings; dot-method lane DOT6 removes the
// intrinsics and the exception with them.
func TestPipeIntrinsicNamesAreOrdinaryMembersAfterThePipe(t *testing.T) {
	check := func(body string) *Result {
		return CompileFor(pipeIntrinsicNames+"effect fn probe() -> string uses { S } { "+body+" }\n", "js")
	}
	for _, op := range []string{"timeout", "orFail", "get"} {
		if r := check(`run "x" |> S.` + op + `()`); !r.Checked {
			t.Errorf("x |> S.%s() rejected: %+v", op, r.Diagnostics)
		}
	}
	if r := check(`run S.get("x")`); !r.Checked {
		t.Errorf("S.get(\"x\") rejected: %+v", r.Diagnostics)
	}
	timeout := check(`run S.timeout("x")`)
	if !slices.ContainsFunc(timeout.Diagnostics, func(d Diagnostic) bool {
		return d.Code == "EF106" && strings.Contains(d.Message, "timeout requires an Effect")
	}) {
		t.Errorf("nested S.timeout(\"x\") = %+v, want the timeout intrinsic's EF106", timeout.Diagnostics)
	}
	orFail := check(`run S.orFail("x")`)
	if len(orFail.Diagnostics) != 1 || orFail.Diagnostics[0].Code != "EF002" {
		t.Errorf("nested S.orFail(\"x\") = %+v, want the orFail intrinsic's EF002", orFail.Diagnostics)
	}
}

// Every binary operator the parser admits is classified here as refused
// beside an unparenthesised pipe chain. Adding an operator without extending
// this table, and deciding its precedence against |>, fails the test.
var pipeBinaryOperators = map[string]string{"==": "refused", "+": "refused"}

func TestEveryBinaryOperatorIsClassifiedAgainstThePipe(t *testing.T) {
	for op := range binaryPrecedence {
		if pipeBinaryOperators[op] != "refused" {
			t.Errorf("binary operator %q is not classified against |>: add it to pipeBinaryOperators with the rule that applies", op)
		}
	}
	for op := range pipeBinaryOperators {
		if _, ok := binaryPrecedence[op]; !ok {
			t.Errorf("pipeBinaryOperators names %q, which the parser does not admit", op)
		}
		for _, body := range []string{`"a" |> shout() ` + op + ` "b"`, `"a" ` + op + ` "b" |> shout()`} {
			_, r := pipeProbe(body)
			if len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "EF002" || !strings.Contains(r.Diagnostics[0].Message, "parenthesise") {
				t.Errorf("%s: diagnostics = %+v, want the parenthesise refusal", body, r.Diagnostics)
			}
		}
	}
}
