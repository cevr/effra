package compiler

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

const scopeTailMessage = "scope result is an unexecuted recipe; use run here to execute it before the scope closes"

const scopeTailPrelude = `enum Choice {
    First
    Second
}
effect fn tick(label: string) -> string uses { Console } {
    run Console.log("tick " + label)
    label
}
effect fn tock(label: string) -> string uses { Console } {
    run Console.log("tock " + label)
    label
}
effect fn factory() -> Effect<string, {}, { Console }> {
    tick("nested")
}
`

const scopeTailMain = `effect fn main() -> void {
    let text = run body(true, Choice.Second {}).provide<Console>(Stdout)
    run Console.log("result " + text).provide<Console>(Stdout)
}
`

// scopeTailCases put a recipe in the final position of a scope. Each tail is
// the complete final expression, which the diagnostic and its edit cover.
var scopeTailCases = []struct {
	name, before, tail, after, output string
}{
	{"direct call", "scope { ", `tick("direct")`, " }", "tick direct\nresult direct\n"},
	{"alias", "scope {\n        let pending = tick(\"alias\")\n        ", "pending", "\n    }", "tick alias\nresult alias\n"},
	{"selected if", "scope {\n        ", `if flag { tick("if") } else { tock("if") }`, "\n    }", "tick if\nresult if\n"},
	{"selected match", "scope {\n        ", "match choice {\n            Choice.First => tick(\"match\"),\n            Choice.Second => tock(\"match\")\n        }", "\n    }", "tock match\nresult match\n"},
	{"nested result of one run", "scope { ", "run factory()", " }", "tick nested\nresult nested\n"},
	{"multiline comments and unicode", "scope {\n        // choose 🙂\n        ", "if flag {\n            tick(\"héllo 🙂\") /* keep */\n        } else {\n            tock(\"ünï\")\n        }", "\n    }", "tick héllo 🙂\nresult héllo 🙂\n"},
}

func scopeTailSource(before, tail, after string) string {
	return scopeTailPrelude + "effect fn body(flag: bool, choice: Choice) -> string uses { Console } {\n    " + before + tail + after + "\n}\n" + scopeTailMain
}

// applySuggestion applies one suggestion's edits in memory, last first.
func applySuggestion(t *testing.T, source string, suggestion Suggestion) string {
	t.Helper()
	edits := slices.Clone(suggestion.Edits)
	slices.SortFunc(edits, func(a, b SourceEdit) int { return b.Span.Offset - a.Span.Offset })
	for _, edit := range edits {
		end := edit.Span.Offset + edit.Span.Length
		if edit.Span.Offset < 0 || end > len(source) {
			t.Fatalf("edit outside source: %+v", edit)
		}
		source = source[:edit.Span.Offset] + edit.NewText + source[end:]
	}
	return source
}

// spanAt independently projects a byte offset to the compiler's 1-based
// line and byte column.
func spanAt(source string, offset, length int) Span {
	line := 1 + strings.Count(source[:offset], "\n")
	column := offset - strings.LastIndex(source[:offset], "\n")
	return Span{Offset: offset, Length: length, Line: line, Column: column}
}

func TestScopeFinalRecipeReportsOneScopeDiagnosticWithRunEdit(t *testing.T) {
	for _, test := range scopeTailCases {
		source := scopeTailSource(test.before, test.tail, test.after)
		start := strings.Index(source, test.before+test.tail) + len(test.before)
		for _, target := range []string{"go", "js"} {
			t.Run(test.name+"/"+target, func(t *testing.T) {
				r := CompileFor(source, target)
				if r.Checked || len(r.Diagnostics) != 1 {
					t.Fatalf("want one scope diagnostic: %+v", r.Diagnostics)
				}
				diagnostic := r.Diagnostics[0]
				if diagnostic.Code != "EF105" || diagnostic.Message != scopeTailMessage || diagnostic.Span != spanAt(source, start, len(test.tail)) {
					t.Fatalf("scope diagnostic = %+v, want span %+v", diagnostic, spanAt(source, start, len(test.tail)))
				}
				if len(diagnostic.Suggestions) != 1 || diagnostic.Suggestions[0].Message != "Execute this recipe inside the scope" {
					t.Fatalf("suggestion = %+v", diagnostic.Suggestions)
				}
				for _, edit := range diagnostic.Suggestions[0].Edits {
					if edit.Span != spanAt(source, edit.Span.Offset, edit.Span.Length) {
						t.Fatalf("edit position is not the source position: %+v", edit)
					}
				}
				fixed := applySuggestion(t, source, diagnostic.Suggestions[0])
				if want := scopeTailSource(test.before, "run ("+test.tail+")", test.after); fixed != want {
					t.Fatalf("edit did not wrap the complete tail once:\n%s", fixed)
				}
				if r := CompileFor(fixed, target); !r.Checked {
					t.Fatalf("suggested edit does not check: %+v", r.Diagnostics)
				}
			})
		}
		t.Run(test.name+"/execution", func(t *testing.T) {
			fixed := scopeTailSource(test.before, "run ("+test.tail+")", test.after)
			runGenericDataNative(t, fixed, test.output)
			if output := runJSForTarget(t, "js", fixed, `await Effect.runPromise(__ef_function_main());`); output != test.output {
				t.Fatalf("JS output = %q", output)
			}
		})
	}
}

// The rejected tail is the single root diagnostic: the scope does not also
// report a discarded effect or a mismatched body type, while errors that a
// fixed program would still have are reported on recheck.
func TestScopeFinalRecipeReplacesCascadeAndKeepsIndependentErrors(t *testing.T) {
	cascade := `effect fn recipe() -> string { "ok" }
effect fn main() -> string {
    scope { recipe() }
}
`
	if r := Compile(cascade); len(r.Diagnostics) != 1 || r.Diagnostics[0].Message != scopeTailMessage {
		t.Fatalf("cascade not replaced: %+v", r.Diagnostics)
	}
	for _, test := range []struct{ name, source, before, after string }{
		{"undeclared failure after the edit", `error Late
effect fn late() -> string raises { Late } { fail Late }
effect fn main() -> string {
    scope { late() }
}
`, "", "EF107"},
		{"undeclared service after the edit", `effect fn speak() -> string uses { Console } {
    run Console.log("hi")
    "hi"
}
effect fn main() -> string {
    scope { speak() }
}
`, "", "EF108"},
		{"scope-owned File result stays refused", `effect fn borrowed(file: File) -> File { file }
effect fn leak() -> File raises { IoError } {
    scope {
        let file = run Files.openRead("x").provide<Files>(LiveFiles)
        borrowed(file)
    }
}
effect fn main() -> void { void }
`, "EF123", "EF123"},
		{"consumed File with an independent result", `effect fn readIt(file: File) -> string raises { IoError } {
    run Files.readText(file).provide<Files>(LiveFiles)
}
effect fn read() -> string raises { IoError } {
    scope {
        let file = run Files.openRead("x").provide<Files>(LiveFiles)
        readIt(file)
    }
}
effect fn main() -> void { void }
`, "EF123", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(test.source)
			codes := []string{}
			var suggestion *Suggestion
			for _, diagnostic := range r.Diagnostics {
				codes = append(codes, diagnostic.Code)
				if diagnostic.Message == scopeTailMessage {
					suggestion = &diagnostic.Suggestions[0]
				}
			}
			want := []string{"EF105"}
			if test.before != "" {
				want = append(want, test.before)
			}
			slices.Sort(codes)
			if suggestion == nil || !slices.Equal(codes, want) {
				t.Fatalf("before the edit: %+v", r.Diagnostics)
			}
			fixed := Compile(applySuggestion(t, test.source, *suggestion))
			if test.after == "" {
				if !fixed.Checked {
					t.Fatalf("fixed program refused: %+v", fixed.Diagnostics)
				}
			} else if fixed.Checked || !hasCode(fixed, test.after) || hasDiagnosticMessage(fixed, scopeTailMessage) {
				t.Fatalf("after the edit want %s: %+v", test.after, fixed.Diagnostics)
			}
		})
	}
}

// The rejected scope result is the already-reported invalid type. Every
// consumer that depends only on it accepts it silently, so each form reports
// exactly the scope diagnostic on both targets, while a check independent of
// it (the if condition below) still reports.
func TestScopeFinalRecipeInvalidResultIsSilentForDependentConsumers(t *testing.T) {
	const prefix = `error Boom
effect fn tick() -> string raises { Boom } { "ok" }
record Label { text: string }
record Box { open: Effect<string, { Boom }> }
fn handle(failure: Boom) -> string { "handled" }
`
	for _, test := range []struct {
		name, body string
		want       []string
	}{
		{"let then run", `effect fn main() -> string raises { Boom } {
    let s = scope { tick() }
    run s
}`, []string{"EF105"}},
		{"outer run", `effect fn main() -> string raises { Boom } {
    run scope { tick() }
}`, []string{"EF105"}},
		{"data payload field", `effect fn main() -> Label {
    let text = scope { tick() }
    Label { text: text }
}`, []string{"EF105"}},
		{"recipe payload field", `effect fn main() -> Box {
    let task = scope { tick() }
    Box { open: task }
}`, []string{"EF105"}},
		{"if branch join", `effect fn main(flag: bool) -> string {
    if flag { scope { tick() } } else { "other" }
}`, []string{"EF105"}},
		{"operator", `effect fn main() -> string {
    let s = scope { tick() }
    s + "!"
}`, []string{"EF105"}},
		{"catch receiver", `effect fn main() -> string {
    let s = scope { tick() }
    run s.catch<Boom>("fallback")
}`, []string{"EF105"}},
		{"recover receiver", `effect fn main() -> string {
    let s = scope { tick() }
    run s.recover<Boom>(handle)
}`, []string{"EF105"}},
		{"independent condition still reports", `effect fn main() -> string {
    if 1 { scope { tick() } } else { "other" }
}`, []string{"EF105", "EF106"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []string{"go", "js"} {
				r := CompileFor(prefix+test.body+"\n", target)
				codes := []string{}
				for _, diagnostic := range r.Diagnostics {
					codes = append(codes, diagnostic.Code)
				}
				slices.Sort(codes)
				if !slices.Equal(codes, test.want) || !hasDiagnosticMessage(r, scopeTailMessage) {
					t.Fatalf("%s: want %v with the scope diagnostic: %+v", target, test.want, r.Diagnostics)
				}
			}
		})
	}
}

// Explicit execution in value position keeps its meaning: only the selected
// recipe runs, once, and a non-recipe result crosses the scope.
func TestScopeFinalRecipeValuePositionControlsRunOnGoAndJS(t *testing.T) {
	source := scopeTailPrelude + `effect fn runSelected(flag: bool) -> string uses { Console } {
    scope { run if flag { tick("selected") } else { tock("selected") } }
}
effect fn runArm(flag: bool) -> string uses { Console } {
    scope {
        if flag { run tick("arm") } else { run tock("arm") }
    }
}
effect fn pure() -> string { scope { "pure" } }
effect fn lastLet() -> void {
    scope {
        let unused = "value"
    }
}
fn lazy() -> Effect<string, {}, { Console }> { tick("lazy") }
effect fn main() -> void {
    let pending = lazy()
    let first = run runSelected(false).provide<Console>(Stdout)
    let second = run runArm(true).provide<Console>(Stdout)
    run lastLet()
    run Console.log(first + " " + second + " " + run pure()).provide<Console>(Stdout)
}
`
	for _, target := range []string{"go", "js"} {
		if r := CompileFor(source, target); !r.Checked {
			t.Fatalf("%s value-position controls refused: %+v", target, r.Diagnostics)
		}
	}
	expected := "tock selected\ntick arm\nselected arm pure\n"
	runGenericDataNative(t, source, expected)
	if output := runJSForTarget(t, "js", source, `await Effect.runPromise(__ef_function_main());`); output != expected {
		t.Fatalf("JS output = %q", output)
	}
}

// The shared diagnostic projection carries the same suggestion with UTF-16
// ranges for every edit.
func TestScopeFinalRecipeSuggestionProjectsUTF16Ranges(t *testing.T) {
	// Both edits sit on a line with non-ASCII text before them, where byte
	// columns and UTF-16 characters differ.
	source := scopeTailSource(`let mark = "ünï 🙂"
    scope { `, `tick(mark + "é")`, " }")
	r := Compile(source)
	report := r.DiagnosticReport(SourceSnapshot{URI: "file:///scope.ef", Text: source}, false)
	if len(report.Diagnostics) != 1 || len(report.Diagnostics[0].Suggestions) != 1 {
		t.Fatalf("report = %+v", report.Diagnostics)
	}
	suggestion := report.Diagnostics[0].Suggestions[0]
	compiled := r.Diagnostics[0].Suggestions[0]
	if suggestion.Message != compiled.Message || len(suggestion.Edits) != len(compiled.Edits) {
		t.Fatalf("projection differs from compiler suggestion: %+v / %+v", suggestion, compiled)
	}
	utf16Position := func(offset int) DiagnosticPosition {
		lineStart := strings.LastIndex(source[:offset], "\n") + 1
		return DiagnosticPosition{Line: strings.Count(source[:offset], "\n"), Character: len(utf16.Encode([]rune(source[lineStart:offset])))}
	}
	for i, edit := range suggestion.Edits {
		if edit.Span != compiled.Edits[i].Span || edit.NewText != compiled.Edits[i].NewText {
			t.Fatalf("edit %d projection: %+v", i, edit)
		}
		want := DiagnosticRange{Start: utf16Position(edit.Span.Offset), End: utf16Position(edit.Span.Offset + edit.Span.Length)}
		if edit.Range != want {
			t.Fatalf("edit %d range = %+v, want %+v", i, edit.Range, want)
		}
	}
	if end := suggestion.Edits[len(suggestion.Edits)-1]; end.Range.Start.Character == end.Span.Column-1 {
		t.Fatalf("closing edit after non-ASCII text must project UTF-16, not byte, columns: %+v", end)
	}
}
