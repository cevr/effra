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
    effect fn get(id: string) -> string raises { Bad } uses { Console }
}
impl Memory for Users {
    effect fn get(id: string) -> string {
        "Ada"
    }
}
effect fn main(id: string) -> string raises { Bad } uses { Users } {
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
	source := `enum State { Ready(value: string) Idle Empty {} } error Bad { message: string } service Clock { effect fn sleep(ms: i64) -> void } impl Live for Clock { effect fn sleep(ms: i64) -> void { void } } effect fn work(state: State) -> string raises {Bad} uses {Clock} { let child = fork work(state).provide<Clock>(Live).timeout(500).catch<Bad>("fallback"); scope { run child } match state { State.Ready { value } => if value == "ok" { value } else { "other" } State.Idle => "idle" State.Empty => fail Bad { message: "empty" } } }`
	want := `enum State {
    Ready(value: string)
    Idle
    Empty {}
}
error Bad {
    message: string
}
service Clock {
    effect fn sleep(ms: i64) -> void
}
impl Live for Clock {
    effect fn sleep(ms: i64) -> void {
        void
    }
}
effect fn work(state: State) -> string raises { Bad } uses { Clock } {
    let child = fork work(state).provide<Clock>(Live).timeout(500).catch<Bad>("fallback");
    scope {
        run child
    }
    match state {
        State.Ready { value } => if value == "ok" {
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
	for _, name := range []string{"causal", "concurrency", "data", "http", "imports", "latest-task", "lifecycle", "main", "missing-service", "testing", "workflow"} {
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

	wrappedChain := `effect fn main() -> void {
// effra-lint-disable-next-line redundant-provision -- keep the chain target line
run hello().provide<Console>(Stdout)
    .provide<Console>(Stdout)
}`
	wrappedResult, err := FormatSource(wrappedChain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wrappedResult.Text, "run hello().provide<Console>(Stdout)\n        .provide<Console>(Stdout)") {
		t.Fatalf("source chain line break was not preserved:\n%s", wrappedResult.Text)
	}
	assertDirectiveTokenLines(t, wrappedChain, wrappedResult.Text)
	if again, err := FormatSource(wrappedResult.Text); err != nil || again.Text != wrappedResult.Text {
		t.Fatalf("wrapped directive target was not idempotent: %v\n%s", err, again.Text)
	}

	blankTarget := `effect fn main() -> void {
// effra-lint-disable-next-line future-rule -- blank target remains blank

void
}`
	blankResult, err := FormatSource(blankTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(blankResult.Text, "future-rule -- blank target remains blank\n\n    void") {
		t.Fatalf("blank directive target was moved:\n%s", blankResult.Text)
	}
	assertDirectiveTokenLines(t, blankTarget, blankResult.Text)

	pinnedConstruct := `effect fn main() -> string {
// effra-lint-disable-next-line future-rule -- preserve this whole syntax line
let value = Data { first: "a", second: "b" }; value
}
record Data { first: string, second: string }`
	pinnedConstructResult, err := FormatSource(pinnedConstruct)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pinnedConstructResult.Text, `let value = Data { first: "a", second: "b" }; value`) {
		t.Fatalf("pinned syntax line was split:\n%s", pinnedConstructResult.Text)
	}
	assertDirectiveTokenLines(t, pinnedConstruct, pinnedConstructResult.Text)

	for _, directive := range []string{
		`// effra-lint-disable-next-line future-rule -- malformed unknown rule`,
		`// effra-lint-disable-next-line unused-recipe -- unused directive`,
	} {
		caseSource := "effect fn main() -> void {\n" + directive + "\nvoid\n}"
		formatted, err := FormatSource(caseSource)
		if err != nil {
			t.Fatal(err)
		}
		assertLintMeaning(t, caseSource, formatted.Text)
	}
	for _, directive := range []string{
		`//effra-lint-disable-next-line`,
		`// effra-lint-disable-next-line unused-recipe`,
		`// effra-lint-disable-next-line unused-recipe --`,
	} {
		caseSource := "effect fn main() -> void {\n" + directive + "\nvoid\n}"
		formatted, err := FormatSource(caseSource)
		if err != nil {
			t.Fatal(err)
		}
		assertLintMeaning(t, caseSource, formatted.Text)
	}

	// A block comment spelling a directive is ordinary text in lint and
	// formatter alike, so moving its target onto the next line by formatting
	// cannot activate it, and it never suppresses a recipe on its own line.
	for _, caseSource := range []string{
		"effect fn task() -> void { void }\neffect fn main() -> void { /* effra-lint-disable-next-line unused-recipe -- intentional */ let pending = task(); void }",
		"effect fn task() -> void { void }\neffect fn main() -> void {\n/* effra-lint-disable-next-line unused-recipe -- intentional */\nlet pending = task(); void\n}",
	} {
		formatted, err := FormatSource(caseSource)
		if err != nil {
			t.Fatal(err)
		}
		assertLintMeaning(t, caseSource, formatted.Text)
		for _, source := range []string{caseSource, formatted.Text} {
			lint := Compile(source).Lint(true)
			if len(lint.LintDiagnostics) != 1 || lint.LintDiagnostics[0].Code != "EFL001" {
				t.Fatalf("block comment acted as a lint directive: %+v\n%s", lint.LintDiagnostics, source)
			}
		}
	}

	// An ordinary block comment inside a legitimate directive's target line
	// stays on that line together with the line's remaining tokens.
	for _, target := range []struct{ source, line string }{
		{"effect fn task() -> void { void }\neffect fn main() -> void {\n// effra-lint-disable-next-line unused-recipe -- both are intentional\nlet first = task(); /* ordinary text */ let second = task(); void\n}",
			"\n    let first = task(); /* ordinary text */ let second = task(); void\n"},
		{"effect fn task() -> void { void }\neffect fn main() -> void {\n// effra-lint-disable-next-line unused-recipe -- intentional\n/* ordinary text */ let pending = task(); void\n}",
			"\n    /* ordinary text */ let pending = task(); void\n"},
	} {
		formatted, err := FormatSource(target.source)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(formatted.Text, target.line) {
			t.Fatalf("block comment split a directive target line:\n%s", formatted.Text)
		}
		assertLintMeaning(t, target.source, formatted.Text)
		assertDirectiveTokenLines(t, target.source, formatted.Text)
		if lint := Compile(formatted.Text).Lint(true); len(lint.LintDiagnostics) != 0 {
			t.Fatalf("formatted directive target lost its suppression: %+v\n%s", lint.LintDiagnostics, formatted.Text)
		}
		if again, err := FormatSource(formatted.Text); err != nil || again.Text != formatted.Text {
			t.Fatalf("directive target with a block comment was not idempotent: %v\n%s", err, again.Text)
		}
	}
}

func TestFormatKeepsSourceAfterSameLineBlockComments(t *testing.T) {
	source := "effect fn main() -> void {\nlet a = 1; /* settled */\nlet b = echo(/* first */ 1, /* second */ 2); void\n}"
	formatted, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(formatted.Text, "\n    let a = 1; /* settled */\n    let b = echo( /* first */ 1, /* second */ 2);\n") {
		t.Fatalf("block comment ended its source line:\n%s", formatted.Text)
	}
	if again, err := FormatSource(formatted.Text); err != nil || again.Text != formatted.Text {
		t.Fatalf("same-line block comments were not idempotent: %v\n%s", err, again.Text)
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
	for _, source := range []string{"// comment\rfn main() -> void { void }", "fn main() -> void {\rvoid\n}"} {
		result, err := FormatSource(source)
		failure, ok := err.(FormatFailure)
		if !ok || len(failure.Diagnostics) == 0 || failure.Diagnostics[0].Code != "EF001" || result.Text != "" {
			t.Fatalf("standalone CR did not produce EF001 without output: result=%+v err=%v", result, err)
		}
	}
	if result, err := FormatSource(`fn main() -> string { "a\rb" }`); err != nil || !strings.Contains(result.Text, `"a\rb"`) {
		t.Fatalf("escaped carriage return was rejected or rewritten: result=%+v err=%v", result, err)
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

func TestFormatRejectsInvalidUTF8WithExactLexicalSpan(t *testing.T) {
	for _, prefix := range []string{"// bad ", `fn main() -> string { "bad `} {
		source := append([]byte(prefix), 0xff)
		source = append(source, []byte(" byte\nfn main() -> void { void }\n")...)
		result, err := FormatSource(string(source))
		failure, ok := err.(FormatFailure)
		if !ok || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != "EF001" || result.Text != "" || result.OutputDigest != "" {
			t.Fatalf("invalid UTF-8 was not rejected without output: result=%+v err=%v", result, err)
		}
		diagnostic := failure.Diagnostics[0]
		if diagnostic.Span.Offset != len(prefix) || diagnostic.Span.Length != 1 || diagnostic.Span.Line != 1 || diagnostic.Span.Column != len(prefix)+1 {
			t.Fatalf("invalid UTF-8 span drifted: %+v", diagnostic.Span)
		}
	}
	validReplacement := "// valid replacement �\nfn main() -> string { \"�\" }"
	if result, err := FormatSource(validReplacement); err != nil || !strings.Contains(result.Text, "�") {
		t.Fatalf("valid U+FFFD source was rejected or rewritten: result=%+v err=%v", result, err)
	}
}

func TestFormatBoundedStopsBeforeHighIndentOutput(t *testing.T) {
	var source strings.Builder
	source.WriteString("effect fn main() -> void { ")
	for index := 0; index < 64; index++ {
		source.WriteString("scope { ")
	}
	source.WriteString("void")
	for index := 0; index < 64; index++ {
		source.WriteString(" }")
	}
	source.WriteString(" }")
	result, err := FormatSourceBounded(source.String(), 256)
	if _, ok := err.(FormatLimitError); !ok || result.Text != "" || result.OutputDigest != "" {
		t.Fatalf("bounded formatter returned partial output: result=%+v err=%v", result, err)
	}
}

func TestFormatBoundedHonorsExactOutputBoundary(t *testing.T) {
	source := `effect fn main() -> void { void }`
	want, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}

	exact, err := FormatSourceBounded(source, len(want.Text))
	if err != nil || exact.Text != want.Text || exact.OutputDigest != want.OutputDigest {
		t.Fatalf("exact output bound rejected the complete result: result=%+v err=%v want=%+v", exact, err, want)
	}

	below, err := FormatSourceBounded(source, len(want.Text)-1)
	if _, ok := err.(FormatLimitError); !ok || below.Text != "" || below.OutputDigest != "" {
		t.Fatalf("one-byte-short output bound returned replacement text: result=%+v err=%v", below, err)
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

func TestFormatterUsesSyntaxRolesForAnglesAndComparisons(t *testing.T) {
	const source = `record Box<T: type> { value: T }
fn less(left: i64, right: i64) -> bool { left < right }
fn greater(value: i64) -> bool { value > (0) }
effect fn main() -> void { void }`
	const want = `record Box<T: type> {
    value: T
}
fn less(left: i64, right: i64) -> bool {
    left < right
}
fn greater(value: i64) -> bool {
    value > (0)
}
effect fn main() -> void {
    void
}
`
	assertFormat(t, source, want)
}

func TestFormatPreservesBlankSeparation(t *testing.T) {
	source := `import go a "strings";

record R { value: string }

effect fn main() -> string {
let first = "a";

let second = first

second
}

fn after() -> void { void }
`
	want := `import go a "strings";

record R {
    value: string
}

effect fn main() -> string {
    let first = "a";

    let second = first

    second
}

fn after() -> void {
    void
}
`
	assertFormat(t, source, want)
}

func TestFormatPreservesServiceMethodBoundaries(t *testing.T) {
	source := `service S { effect fn one() -> void effect fn two() -> void }
impl P for S { effect fn one() -> void {} effect fn two() -> void { void } }`
	want := `service S {
    effect fn one() -> void
    effect fn two() -> void
}
impl P for S {
    effect fn one() -> void {}
    effect fn two() -> void {
        void
    }
}
`
	assertFormat(t, source, want)

	pinned := `service S {
// effra-lint-disable-next-line future-rule -- keep both declarations together
effect fn one() -> void effect fn two() -> void
}`
	result, err := FormatSource(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "effect fn one() -> void effect fn two() -> void") {
		t.Fatalf("pinned method line was split:\n%s", result.Text)
	}
	assertDirectiveTokenLines(t, pinned, result.Text)
}

func TestFormatContinuationIndentation(t *testing.T) {
	source := `error Bad {}
fn take(first: string, second: string) -> string { first }
fn split(
first: string,
second: string
) -> string { first }
effect fn main() -> string
raises {Bad}
uses {Console} {
take(
"one",
"two"
)
1 +
2 +
3
}`
	want := `error Bad {}
fn take(first: string, second: string) -> string {
    first
}
fn split(
    first: string,
    second: string
) -> string {
    first
}
effect fn main() -> string
    raises { Bad }
    uses { Console } {
    take(
        "one",
        "two"
    )
    1 +
        2 +
        3
}
`
	assertFormat(t, source, want)

	chain := `effect fn main() -> void {
run task().provide<Console>(Stdout)
.provide<Console>(Stdout)
}`
	chainWant := `effect fn main() -> void {
    run task().provide<Console>(Stdout)
        .provide<Console>(Stdout)
}
`
	assertFormat(t, chain, chainWant)

	pattern := `enum State { Ready { value: string } }
effect fn main(state: State) -> string {
match state {
State.Ready {
value,
other
} => value
}
}`
	patternWant := `enum State {
    Ready {
        value: string
    }
}
effect fn main(state: State) -> string {
    match state {
        State.Ready {
            value,
            other
        } => value
    }
}
`
	assertFormat(t, pattern, patternWant)

	pinned := `effect fn main() -> void {
// effra-lint-disable-next-line future-rule -- keep the opener line together
let value = scope { run task()
run task()
};
}`
	pinnedWant := `effect fn main() -> void {
    // effra-lint-disable-next-line future-rule -- keep the opener line together
    let value = scope { run task()
        run task()
    };
}
`
	assertFormat(t, pinned, pinnedWant)
	assertDirectiveTokenLines(t, pinned, mustFormat(t, pinned))
}

func TestFormatContinuationClosersAndOwnLineBlockOpeners(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "multiline raises closer",
			source: "error E\neffect fn f() -> void\nraises {\nE\n} { void }",
			want:   "error E\neffect fn f() -> void\n    raises {\n        E\n    } {\n    void\n}\n",
		},
		{
			name:   "fluent call closer",
			source: "effect fn f() -> string {\nrun task()\n.catch<E>(\n\"fallback\"\n)\n}",
			want:   "effect fn f() -> string {\n    run task()\n        .catch<E>(\n            \"fallback\"\n        )\n}\n",
		},
		{
			name:   "if and else own line braces",
			source: "fn f() -> i64 {\nif 1 == 1\n{\n1\n}\nelse\n{\n2\n}\n}",
			want:   "fn f() -> i64 {\n    if 1 == 1\n    {\n        1\n    }\n    else\n    {\n        2\n    }\n}\n",
		},
		{
			name:   "match own line brace",
			source: "fn f(x: i64) -> i64 {\nmatch x\n{\n_ => 1\n}\n}",
			want:   "fn f(x: i64) -> i64 {\n    match x\n    {\n        _ => 1\n    }\n}\n",
		},
		{
			name:   "signature row followed by block",
			source: "error E\neffect fn main() -> void\nraises {E}\nuses {Console}\n{\nrun task()\n}",
			want:   "error E\neffect fn main() -> void\n    raises { E }\n    uses { Console }\n{\n    run task()\n}\n",
		},
		{
			name:   "comment inside continuation delimiter",
			source: "error E\neffect fn f() -> void\nraises {\n// note\nE\n} { void }",
			want:   "error E\neffect fn f() -> void\n    raises {\n        // note\n        E\n    } {\n    void\n}\n",
		},
		{
			name:   "nested continuation",
			source: "fn f() -> i64 {\ntake(\ntake(\n1,\n2\n),\n3\n)\n}",
			want:   "fn f() -> i64 {\n    take(\n        take(\n            1,\n            2\n        ),\n        3\n    )\n}\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertFormat(t, test.source, test.want)
		})
	}

	pinned := `fn f() -> i64 {
match 1
// effra-lint-disable-next-line future -- keep opener
{
_ => 1
// effra-lint-disable-next-line future -- keep closer
}
}`
	assertFormat(t, pinned, `fn f() -> i64 {
    match 1
    // effra-lint-disable-next-line future -- keep opener
    {
        _ => 1
        // effra-lint-disable-next-line future -- keep closer
    }
}
`)
	assertDirectiveTokenLines(t, pinned, mustFormat(t, pinned))
}

func TestFormatSpacingBeforeGroupedExpressions(t *testing.T) {
	source := `fn take(first: i64, second: i64) -> i64 { first }
effect fn main() -> void {
// effra-lint-disable-next-line future-rule -- preserve the grouped spacing case
take(first:(1), second:(2));(2)
	match Choice.A { Choice.A => void }
}`
	want := `fn take(first: i64, second: i64) -> i64 {
    first
}
effect fn main() -> void {
    // effra-lint-disable-next-line future-rule -- preserve the grouped spacing case
    take(first: (1), second: (2)); (2)
    match Choice.A {
        Choice.A => void
    }
}
`
	assertFormat(t, source, want)
}

func TestFormatPreservesFullSyntaxTree(t *testing.T) {
	source := `record Data { first: string, second: string }
fn take(name: string, count: i64) -> string { name }
effect fn main() -> string { let value = Data { first: "x", second: "y" }; take(name: "x", count: 1) }`
	result, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	want := parsedSyntaxTree(t, source)
	got := parsedSyntaxTree(t, result.Text)
	if !equalSyntaxTrees(got, want) {
		t.Fatal("full syntax tree changed")
	}
}

func TestFormatFullSyntaxComparisonPreservesHiddenFieldsAndAliases(t *testing.T) {
	source := `enum State { Idle() }
effect fn main() -> void { void }`
	for _, field := range []string{"Parenthesized", "Explicit"} {
		t.Run(field, func(t *testing.T) {
			original := parsedSyntaxTree(t, source)
			changed := parsedSyntaxTree(t, source)
			if !equalSyntaxTrees(original, changed) {
				t.Fatal("identical parsed trees compare unequal")
			}
			if field == "Parenthesized" {
				variant := &changed.Enums[0].Variants[0]
				variant.Parenthesized = !variant.Parenthesized
			} else {
				body := changed.Functions[0].Body
				body.Explicit = !body.Explicit
			}
			if equalSyntaxTrees(original, changed) {
				t.Fatalf("comparison lost %s", field)
			}
		})
	}

	// Named arguments are shared through Args and Fields. Compare the complete
	// parsed graph without expanding that sharing into an exponential JSON tree.
	deep := nestedNamedCallSource(40)
	formatted := mustFormat(t, deep)
	if !equalSyntaxTrees(parsedSyntaxTree(t, deep), parsedSyntaxTree(t, formatted)) {
		t.Fatal("formatting changed the aliased syntax graph")
	}
}

func TestFormatNamedCallChildrenAreVisitedOnce(t *testing.T) {
	source := nestedNamedCallSource(40)
	result, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	again, err := FormatSource(result.Text)
	if err != nil || again.Text != result.Text {
		t.Fatalf("nested named calls were not idempotent: %v", err)
	}
}

func nestedNamedCallSource(depth int) string {
	return `fn leaf(value: string) -> string { value } effect fn main() -> string { ` + strings.Repeat("leaf(value: ", depth) + `"ok"` + strings.Repeat(")", depth) + ` }`
}

func TestForEachExprChildDeduplicatesCheckedCallFields(t *testing.T) {
	first := &Expr{Kind: "string", Text: "first"}
	second := &Expr{Kind: "string", Text: "second"}
	call := &Expr{Kind: "call", Args: []*Expr{first, second}, Fields: []FieldValue{{Name: "first", Value: first}, {Name: "second", Value: second}}}
	var visited []*Expr
	forEachExprChild(call, func(child *Expr) { visited = append(visited, child) })
	if !reflect.DeepEqual(visited, []*Expr{first, second}) {
		t.Fatalf("call children were not canonicalized: got=%p want=%p", visited, []*Expr{first, second})
	}
	construct := &Expr{Kind: "construct", Fields: []FieldValue{{Name: "first", Value: first}, {Name: "second", Value: second}}}
	visited = nil
	forEachExprChild(construct, func(child *Expr) { visited = append(visited, child) })
	if !reflect.DeepEqual(visited, []*Expr{first, second}) {
		t.Fatalf("constructor children were not visited: got=%p want=%p", visited, []*Expr{first, second})
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

func mustFormat(t *testing.T, source string) string {
	t.Helper()
	result, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	return result.Text
}

type syntaxPointer struct {
	typeOf reflect.Type
	value  uintptr
}

func parsedSyntaxTree(t *testing.T, source string) *Program {
	t.Helper()
	program, _, diagnostics := parseSyntax(source)
	if len(diagnostics) != 0 {
		t.Fatalf("source did not parse: %+v", diagnostics)
	}
	return program
}

func equalSyntaxTrees(left, right *Program) bool {
	for _, program := range []*Program{left, right} {
		program.Comments = nil
		normalizeSyntaxValue(reflect.ValueOf(program), map[syntaxPointer]bool{})
	}
	// DeepEqual memoizes compared pointer pairs, retaining all structural fields
	// and shared aliases. Comments have their own ordered preservation oracle.
	return reflect.DeepEqual(left, right)
}

func normalizeSyntaxValue(value reflect.Value, seen map[syntaxPointer]bool) {
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return
		}
		pointer := syntaxPointer{typeOf: value.Type(), value: value.Pointer()}
		if seen[pointer] {
			return
		}
		seen[pointer] = true
		normalizeSyntaxValue(value.Elem(), seen)
	case reflect.Struct:
		if value.Type() == reflect.TypeOf(Span{}) {
			if value.CanSet() {
				value.Set(reflect.Zero(value.Type()))
			}
			return
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if field.CanSet() {
				normalizeSyntaxValue(field, seen)
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			normalizeSyntaxValue(value.Index(index), seen)
		}
	case reflect.Map:
		spanType := reflect.TypeOf(Span{})
		if value.Type().Elem() == spanType {
			for _, key := range value.MapKeys() {
				value.SetMapIndex(key, reflect.Zero(spanType))
			}
			return
		}
		for _, key := range value.MapKeys() {
			element := value.MapIndex(key)
			if element.Kind() == reflect.Pointer {
				normalizeSyntaxValue(element, seen)
			}
		}
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

func assertDirectiveTokenLines(t *testing.T, before, after string) {
	t.Helper()
	beforeTokens, beforeComments, beforeDiagnostics := lex(before)
	afterTokens, afterComments, afterDiagnostics := lex(after)
	if len(beforeDiagnostics) != 0 || len(afterDiagnostics) != 0 {
		t.Fatalf("directive line oracle could not lex source: before=%+v after=%+v", beforeDiagnostics, afterDiagnostics)
	}
	afterCommentLines := map[string][]int{}
	for _, comment := range afterComments {
		afterCommentLines[comment.Text] = append(afterCommentLines[comment.Text], comment.Span.Line)
	}
	seen := map[string]int{}
	for _, comment := range beforeComments {
		body := strings.TrimLeft(comment.Text, " \t")
		if !strings.HasPrefix(body, "effra-lint-disable-next-line") {
			continue
		}
		lines := afterCommentLines[comment.Text]
		occurrence := seen[comment.Text]
		if occurrence >= len(lines) {
			t.Fatalf("directive comment was lost: %q", comment.Text)
		}
		seen[comment.Text] = occurrence + 1
		beforeLine := tokenLineText(beforeTokens, comment.Span.Line+1)
		afterLine := tokenLineText(afterTokens, lines[occurrence]+1)
		if beforeLine != afterLine {
			t.Fatalf("directive target token line changed: before=%q after=%q comment=%q", beforeLine, afterLine, comment.Text)
		}
	}
}

func tokenLineText(tokens []token, line int) string {
	texts := []string{}
	for _, current := range tokens {
		if current.kind != "eof" && current.span.Line == line {
			texts = append(texts, current.text)
		}
	}
	return strings.Join(texts, "\x00")
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

func BenchmarkFormatCommentHeavy(b *testing.B) {
	source := strings.Repeat("// comment attached to a declaration\nrecord R { value: string }\n", 1000)
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

func BenchmarkFormatCommentHeavyEmptyBraces(b *testing.B) {
	source := strings.Repeat("record R{// comment inside an empty declaration\n}\n", 1000)
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

func BenchmarkFormatDeepNamedCall(b *testing.B) {
	source := nestedNamedCallSource(100)
	b.Logf("source_bytes=%d depth=%d", len(source), 100)
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := FormatSource(source); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFormatLongBinary(b *testing.B) {
	source := `effect fn main() -> i64 { ` + strings.Repeat("1 + ", 10000) + "1 }"
	b.Logf("source_bytes=%d terms=%d", len(source), 10001)
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := FormatSource(source); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFormatLongFluentChain(b *testing.B) {
	const calls = 10000
	source := "effect fn main() -> void {\nrun task()\n" + strings.Repeat(".provide<Console>(Stdout)\n", calls) + "}\n"
	b.Logf("source_bytes=%d fluent_calls=%d", len(source), calls)
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := FormatSource(source); err != nil {
			b.Fatal(err)
		}
	}
}

// A line-leading |> continues the expression: the lexer drops newlines and no
// statement starts with the token. The formatter keeps the author's breaks,
// indents each continuation once and spaces the operator.
func TestFormatPipeContinuations(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"line leading", "fn a() -> string {\n    let v = \"x\"\n  |> shout()\n            |> shout()\n  .length\n    v\n}\n", "fn a() -> string {\n    let v = \"x\"\n        |> shout()\n        |> shout()\n        .length\n    v\n}\n"},
		{"same line spacing", "fn a() -> string { \"x\"|>shout()|>wrap(\"<\",\">\") }", "fn a() -> string {\n    \"x\" |> shout() |> wrap(\"<\", \">\")\n}\n"},
		{"under run", "effect fn a() -> string { run load()\n|> shout(\"a\")\n |> Effect.timeout(ms: 5) }", "effect fn a() -> string {\n    run load()\n        |> shout(\"a\")\n        |> Effect.timeout(ms: 5)\n}\n"},
		{"inside an argument list", "fn a() -> string { pair(\"x\"\n|> shout(), \"y\") }", "fn a() -> string {\n    pair(\"x\"\n        |> shout(), \"y\")\n}\n"},
		{"type arguments", "fn a() -> string { \"x\" |>shout<T>(1) }", "fn a() -> string {\n    \"x\" |> shout<T>(1)\n}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertFormat(t, test.source, test.want)
			// Type application nodes carry source spans that the tree
			// comparison does not normalise, for any call.
			if test.name != "type arguments" && !equalSyntaxTrees(parsedSyntaxTree(t, test.source), parsedSyntaxTree(t, test.want)) {
				t.Fatal("formatting changed the parsed pipe")
			}
		})
	}
}
