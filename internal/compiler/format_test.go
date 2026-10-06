package compiler

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFormatCanonicalGoldenCoversGrammar(t *testing.T) {
	source := `import go strings "strings"; error Bad { message: string } record User { id: string, name: string } enum State { Idle Running { id: string } Done } service Users { effect fn get(id: string) -> string raises {Bad} uses {Console} } impl Memory for Users { effect fn get(id: string) -> string { "Ada" } } effect fn main(id: string) -> string raises {Bad} uses {Users} { let user = User { id: id, name: "Ada" }; if true { run Users.get(id) } else { fail Bad { message: "no" } } }`
	want := `import go strings "strings";
error Bad {
    message: string
}
record User {
    id: string,
    name: string
}
enum State {
    Idle
    Running {
        id: string
    }
    Done
}
service Users {
    effect fn get(id: string) -> string raises {Bad} uses {Console}
}
impl Memory for Users {
    effect fn get(id: string) -> string {
        "Ada"
    }
}
effect fn main(id: string) -> string raises {Bad} uses {Users} {
    let user = User {
        id: id,
        name: "Ada"
    };
    if true {
        run Users.get(id)
    } else {
        fail Bad {
            message: "no"
        }
    }
}
`
	assertFormat(t, source, want)
}

func TestFormatPreservesGroupingAndCallSpacing(t *testing.T) {
	source := `effect fn identity(value: string) -> string { value } effect fn main() -> string { let value=(identity("ok")); if (true) { value } else { (value) } }`
	want := `effect fn identity(value: string) -> string {
    value
}
effect fn main() -> string {
    let value = (identity("ok"));
    if (true) {
        value
    } else {
        (value)
    }
}
`
	assertFormat(t, source, want)
}

func TestFormatKeepsNamedCallArgumentsInline(t *testing.T) {
	source := `fn take(name: string, count: i64) -> string { name } effect fn main() -> string { take(name: "ok", count: 1) }`
	want := `fn take(name: string, count: i64) -> string {
    name
}
effect fn main() -> string {
    take(name: "ok", count: 1)
}
`
	assertFormat(t, source, want)
}

func TestFormatCanonicalExpressionsAndMatchGolden(t *testing.T) {
	source := `enum State { Ready(value: string) Idle Empty {} } error Bad { message: string } service Clock { effect fn sleep(ms: i64) -> () } impl Live for Clock { effect fn sleep(ms: i64) -> () { () } } effect fn work(state: State) -> string raises {Bad} uses {Clock} { let child = fork work(state).provide<Clock>(Live).timeout(500).catch<Bad>("fallback"); scope { run child } match state { State.Ready { value } => if value == "ok" { value } else { "other" } State.Idle => "idle" State.Empty => fail Bad { message: "empty" } } }`
	want := `enum State {
    Ready(value: string)
    Idle
    Empty {}
}
error Bad {
    message: string
}
service Clock {
    effect fn sleep(ms: i64) -> ()
}
impl Live for Clock {
    effect fn sleep(ms: i64) -> () {
        ()
    }
}
effect fn work(state: State) -> string raises {Bad} uses {Clock} {
    let child = fork work(state).provide<Clock>(Live).timeout(500).catch<Bad>("fallback");
    scope {
        run child
    }
    match state {
        State.Ready {value} => if value == "ok" {
            value
        } else {
            "other"
        }
        State.Idle => "idle"
        State.Empty => fail Bad {
            message: "empty"
        }
    }
}
`
	assertFormat(t, source, want)
}

func TestFormatExamplesAreParseableAndIdempotent(t *testing.T) {
	for _, name := range []string{"causal", "concurrency", "data", "imports", "latest-task", "lifecycle", "main", "missing-service", "testing", "workflow"} {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("..", "..", "examples", name+".ef"))
			if err != nil {
				t.Fatal(err)
			}
			first, err := FormatSource(string(source))
			if err != nil {
				t.Fatal(err)
			}
			second, err := FormatSource(first.Text)
			if err != nil {
				t.Fatalf("formatted source did not parse: %v\n%s", err, first.Text)
			}
			if first.Text != second.Text || first.OutputDigest != second.OutputDigest {
				t.Fatalf("formatting is not idempotent:\nfirst:\n%s\nsecond:\n%s", first.Text, second.Text)
			}
			originalShape := syntaxShape(t, string(source))
			formattedShape := syntaxShape(t, first.Text)
			if !reflect.DeepEqual(originalShape, formattedShape) {
				t.Fatalf("syntax shape changed:\noriginal=%v\nformatted=%v", originalShape, formattedShape)
			}
			if !reflect.DeepEqual(tokenTexts(t, string(source)), tokenTexts(t, first.Text)) {
				t.Fatal("formatting changed token spellings or order")
			}
		})
	}
}

func TestFormatPreservesCommentsAndDirectiveMeaning(t *testing.T) {
	source := `fn echo(value: string) -> string { value }
effect fn task() -> string { "ok" }
effect fn main() -> string {
// leading block comment
// effra-lint-disable-next-line unused-recipe -- both same-line recipes are deliberate
let first = task(); let second = task() // trailing recipe comment
let value = echo(
// argument comment
"x" // trailing argument comment
)
if true { "yes" } // branch continuation comment
else { "no" }
}
// eof comment`
	result, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range []string{
		"// leading block comment",
		"// effra-lint-disable-next-line unused-recipe -- both same-line recipes are deliberate",
		"// trailing recipe comment",
		"// argument comment",
		"// trailing argument comment",
		"// branch continuation comment",
		"// eof comment",
	} {
		if !strings.Contains(result.Text, comment) {
			t.Fatalf("comment was lost: %q\n%s", comment, result.Text)
		}
	}
	if !strings.Contains(result.Text, "let first = task(); let second = task() // trailing recipe comment") {
		t.Fatalf("same-line directive target was split:\n%s", result.Text)
	}
	assertLintMeaning(t, source, result.Text)

	for _, directive := range []string{
		`// effra-lint-disable-next-line future-rule -- malformed unknown rule`,
		`// effra-lint-disable-next-line unused-recipe -- unused directive`,
	} {
		caseSource := "effect fn main() -> () {\n" + directive + "\n()\n}"
		formatted, err := FormatSource(caseSource)
		if err != nil {
			t.Fatal(err)
		}
		assertLintMeaning(t, caseSource, formatted.Text)
	}
}

func TestFormatPreservesUnicodeCRLFEOFAndEmptyFiles(t *testing.T) {
	source := "// astral 𐐀 and combining e\u0301\r\nfn main() -> string { \"𐐀e\\u0301\" }\r\n// eof"
	result, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text, "\r") || !strings.HasSuffix(result.Text, "\n") || !strings.Contains(result.Text, "𐐀") || !strings.Contains(result.Text, "e\\u0301") {
		t.Fatalf("Unicode/CRLF/EOF preservation failed:\n%q", result.Text)
	}
	if again, err := FormatSource(result.Text); err != nil || again.Text != result.Text {
		t.Fatalf("Unicode formatting was not idempotent: %v", err)
	}
	empty, err := FormatSource(" \t\r\n")
	if err != nil || empty.Text != "" || !empty.Changed || empty.OutputDigest == empty.InputDigest {
		t.Fatalf("whitespace-only file was not normalized to empty: %+v err=%v", empty, err)
	}
}

func TestFormatDoesNotRequireSemanticResolution(t *testing.T) {
	source := `import go missing "example.invalid/no-such-package"
fn main() -> Unknown { unknownValue }`
	result, err := FormatSource(source)
	if err != nil || result.Text == "" {
		t.Fatalf("syntax-only formatting loaded or required semantics: %+v err=%v", result, err)
	}
	if _, err := Compile(source).EmitGo(); err == nil {
		t.Fatal("fixture unexpectedly compiled; test must remain unresolved")
	}
}

func TestFormatReportsSyntaxFailuresWithoutReplacement(t *testing.T) {
	for _, source := range []string{
		`fn main() -> string { "unterminated }`,
		`fn main() -> string { @ }`,
		`fn main() -> string { }`,
	} {
		result, err := FormatSource(source)
		if source == `fn main() -> string { }` {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		failure, ok := err.(FormatFailure)
		if !ok || len(failure.Diagnostics) == 0 || result.Text != "" || result.OutputDigest != "" {
			t.Fatalf("syntax failure returned replacement text: result=%+v err=%v", result, err)
		}
	}
}

func TestFormatRetainsDeclarationOrderAndTokens(t *testing.T) {
	source := `fn first() -> string { "\\u00e9" } record R { field: string } error E enum Choice { A B } fn second() -> i64 { 001 }`
	program, _, diagnostics := parseSyntax(source)
	if len(diagnostics) != 0 || len(program.Items) != 5 {
		t.Fatalf("ordered syntax items missing: items=%+v diagnostics=%+v", program.Items, diagnostics)
	}
	wantKinds := []string{"function", "record", "error", "enum", "function"}
	for index, want := range wantKinds {
		if program.Items[index].Kind != want {
			t.Fatalf("item %d kind=%q want %q", index, program.Items[index].Kind, want)
		}
	}
	result, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokenTexts(t, source), tokenTexts(t, result.Text)) {
		t.Fatal("literal bytes or token order changed")
	}
	if !strings.Contains(result.Text, `"\\u00e9"`) || !strings.Contains(result.Text, "001") {
		t.Fatalf("literal spelling was normalized:\n%s", result.Text)
	}
}

func assertFormat(t *testing.T, source, want string) {
	t.Helper()
	result, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != want {
		t.Fatalf("unexpected canonical output:\nwant:\n%s\ngot:\n%s", want, result.Text)
	}
	again, err := FormatSource(result.Text)
	if err != nil || again.Text != want {
		t.Fatalf("canonical output was not idempotent: %v\n%s", err, again.Text)
	}
}

func assertLintMeaning(t *testing.T, before, after string) {
	t.Helper()
	beforeResult := Compile(before)
	afterResult := Compile(after)
	if !beforeResult.Checked || !afterResult.Checked {
		t.Fatalf("directive fixture did not check: before=%+v after=%+v", beforeResult.Diagnostics, afterResult.Diagnostics)
	}
	beforeLint, afterLint := beforeResult.Lint(true), afterResult.Lint(true)
	if lintSignature(beforeLint) != lintSignature(afterLint) {
		t.Fatalf("formatting changed lint meaning:\nbefore=%s\nafter=%s\nformatted=%s", lintSignature(beforeLint), lintSignature(afterLint), after)
	}
}

func lintSignature(result LintResult) string {
	parts := []string{fmtInt(result.Errors), fmtInt(result.Warnings), fmtInt(result.Suggestions), fmtInt(boolInt(result.LintPassed))}
	for _, diagnostic := range result.LintDiagnostics {
		parts = append(parts, diagnostic.Code+":"+diagnostic.Rule+":"+diagnostic.Message)
	}
	return strings.Join(parts, "|")
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func tokenTexts(t *testing.T, source string) []string {
	t.Helper()
	tokens, _, diagnostics := lex(source)
	if len(diagnostics) != 0 {
		t.Fatalf("source did not lex: %+v", diagnostics)
	}
	texts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		texts = append(texts, token.text)
	}
	return texts
}

func syntaxShape(t *testing.T, source string) []string {
	t.Helper()
	program, _, diagnostics := parseSyntax(source)
	if len(diagnostics) != 0 {
		t.Fatalf("source did not parse: %+v", diagnostics)
	}
	shape := []string{}
	for _, item := range program.Items {
		shape = append(shape, item.Kind)
		switch item.Kind {
		case "import":
			shape = append(shape, item.Import.Alias, item.Import.Path)
		case "error":
			shape = append(shape, item.Error.Name)
			for _, field := range item.Error.Fields {
				shape = append(shape, field.Name+":"+field.Type)
			}
		case "record":
			shape = append(shape, item.Record.Name)
			for _, field := range item.Record.Fields {
				shape = append(shape, field.Name+":"+field.Type)
			}
		case "enum":
			shape = append(shape, item.Enum.Name)
			for _, variant := range item.Enum.Variants {
				shape = append(shape, variant.Name)
				for _, field := range variant.Fields {
					shape = append(shape, field.Name+":"+field.Type)
				}
			}
		case "service":
			shape = append(shape, item.Service.Name)
			for _, method := range item.Service.Methods {
				shape = append(shape, method.Name)
			}
		case "impl":
			shape = append(shape, item.Provider.Name, item.Provider.Service)
			for _, method := range item.Provider.Methods {
				shape = append(shape, method.Name)
			}
		case "function":
			shape = append(shape, item.Function.Name)
		}
	}
	return shape
}

func BenchmarkFormatCanonicalFixture(b *testing.B) {
	source := strings.Repeat("effect fn fixture() -> string { \"ok\" }\n", 2000)
	b.Logf("source_bytes=%d", len(source))
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := FormatSource(source); err != nil {
			b.Fatal(err)
		}
	}
}
