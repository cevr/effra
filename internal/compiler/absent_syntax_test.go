package compiler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestAbsentSyntaxCatalogWording(t *testing.T) {
	for _, entry := range absentSyntaxCatalog {
		switch entry.Status {
		case constructAbsent:
			if !strings.HasPrefix(entry.Message, "Effra has no ") {
				t.Errorf("absent %s/%s does not say Effra has no it: %q", entry.Position, entry.Spelling, entry.Message)
			}
		case constructPlanned:
			if !strings.HasSuffix(entry.Message, "not yet supported") {
				t.Errorf("planned %s/%s does not say not yet supported: %q", entry.Position, entry.Spelling, entry.Message)
			}
		default:
			t.Errorf("%s/%s has unknown status %q", entry.Position, entry.Spelling, entry.Status)
		}
		if entry.Help == "" || entry.Construct == "" {
			t.Errorf("%s/%s lacks a construct family or help", entry.Position, entry.Spelling)
		}
	}
}

// nestedLoopHeaders nests loops through their conditions:
// `while 0 + (if a { <inner> } else { 0 }) { }`.
func nestedLoopHeaders(depth int) string {
	inner := "0"
	for range depth {
		inner = "while 0 + (if a { " + inner + " } else { 0 }) { }"
	}
	return inner
}

// Recognition examines each token a bounded number of times: once in the
// header scan, and once for each enclosing recognized construct whose
// condition or body holds it. Loops nested through their conditions and long
// runs of a bound loop name stay within that bound instead of doubling per
// level or rescanning the rest of the block for every statement.
func TestAbsentSyntaxRecognitionWorkIsBounded(t *testing.T) {
	for _, test := range []struct {
		name, body string
		depth      int
	}{
		{"nested-headers", "{ " + nestedLoopHeaders(12) + " }", 12},
		{"bound-loop-names", "{ " + strings.Repeat("while ", 2000) + "}", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			tokens, _, diagnostics := lex(test.body)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			p := &parser{tokens: tokens, types: map[string]*sourceType{}, absent: &absentScan{}}
			func() {
				// The nested loop bodies are refused syntax; only the work matters.
				defer func() {
					if value := recover(); value != nil {
						if _, fault := value.(syntaxFault); !fault {
							panic(value)
						}
					}
				}()
				p.block()
			}()
			if bound := len(tokens) * (test.depth + 2); p.absent.work > bound {
				t.Fatalf("recognition examined %d tokens, bound %d for %d tokens", p.absent.work, bound, len(tokens))
			}
		})
	}
}

// Forty loops nested through their conditions compile at once: a condition's
// blocks recognize no constructs of their own.
func TestAbsentSyntaxDeepLoopHeadersComplete(t *testing.T) {
	deep := "fn f(a: bool) -> string { " + nestedLoopHeaders(40) + " }"
	if r := Compile(deep); r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != absentSyntaxCode {
		t.Fatalf("nested loop headers: got %+v", r.Diagnostics)
	}
}

// A condition parses at the depth of the statement it belongs to, so a
// condition past the nesting limit is no construct and keeps the f4c16b0
// nesting diagnostic.
func TestAbsentSyntaxRecognitionKeepsTheNestingLimit(t *testing.T) {
	limit := "fn f(a: bool) -> string { " + strings.Repeat("0 + (", 100) + "if a { while 0 + " + strings.Repeat("(", 200) + "0" + strings.Repeat(")", 200) + " { } } else { 0 }" + strings.Repeat(")", 100) + " }"
	r := Compile(limit)
	if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF002" || r.Diagnostics[0].Message != "syntax nesting exceeds prototype limit of 256" {
		t.Fatalf("a condition past the nesting limit: got %+v", r.Diagnostics)
	}
}

// The EF003 table in docs/tooling.md publishes the catalog: each row's
// construct, backticked spellings and status must match it, so retiring or
// reclassifying an entry fails until the table follows.
func TestAbsentSyntaxDocsTableMatchesCatalog(t *testing.T) {
	data, err := os.ReadFile("../../docs/tooling.md")
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(data), "| Construct | Spellings | Status |")
	if !found {
		t.Fatal("docs/tooling.md has no EF003 table")
	}
	table, _, _ = strings.Cut(table, "\n\n")
	type classification struct{ construct, spelling, status string }
	documented := map[classification]bool{}
	spelling := regexp.MustCompile("`([^`]+)`")
	construct := strings.NewReplacer("`", "", " ", "-", "/", "-")
	for _, row := range strings.Split(table, "\n")[2:] {
		cells := strings.Split(strings.Trim(row, "| "), " | ")
		if len(cells) != 5 {
			t.Fatalf("malformed EF003 row %q", row)
		}
		spellings := spelling.FindAllStringSubmatch(cells[1], -1)
		if len(spellings) == 0 {
			t.Errorf("EF003 row %q names no spelling", row)
		}
		for _, match := range spellings {
			documented[classification{construct.Replace(cells[0]), match[1], cells[2]}] = true
		}
	}
	cataloged := map[classification]bool{}
	for _, entry := range absentSyntaxCatalog {
		cataloged[classification{entry.Construct, entry.Spelling, entry.Status}] = true
	}
	for entry := range cataloged {
		if !documented[entry] {
			t.Errorf("docs/tooling.md EF003 table lacks %+v", entry)
		}
	}
	for entry := range documented {
		if !cataloged[entry] {
			t.Errorf("docs/tooling.md EF003 table documents %+v, which the catalog does not", entry)
		}
	}
}

// Help travels with the finding and inside the LSP message, the one shared
// projection CLI, MCP and the language server publish.
func TestAbsentSyntaxHelpReachesEveryReportSurface(t *testing.T) {
	source := "effect fn main() -> void { let x = null; void }"
	entry, ok := absentSyntaxAt(absentInValue, "null")
	if !ok {
		t.Fatal("null is not classified")
	}
	span := Span{Offset: strings.Index(source, "null"), Length: 4, Line: 1, Column: strings.Index(source, "null") + 1}
	r := &Result{Diagnostics: []Diagnostic{entry.diagnostic(span)}}
	report := r.DiagnosticReport(SourceSnapshot{URI: "main.ef", Origin: "disk", Text: source}, false)
	if len(report.Diagnostics) != 1 {
		t.Fatalf("report changed cardinality: %+v", report.Diagnostics)
	}
	finding := report.Diagnostics[0]
	if finding.Code != "EF003" || finding.Message != entry.Message || finding.Help != entry.Help || finding.Span != span {
		t.Fatalf("finding lost the construct: %+v", finding)
	}
	if finding.LSP == nil || finding.LSP.Message != entry.Message+"\nhelp: "+entry.Help || finding.LSP.Code != "EF003" {
		t.Fatalf("LSP projection lost the help: %+v", finding.LSP)
	}
}

// Look-alike identifiers, strings, comments and even exact keywords bound as
// ordinary names keep their f4c16b0 behavior: admitted and unchanged.
func TestAbsentSyntaxLeavesLookAlikesAndBoundKeywordsAdmitted(t *testing.T) {
	for name, source := range map[string]string{
		"look-alikes": `// throw null; x as string; a ? b : c; !ok && done; try { } catch { }
/* let module = 1; async fn f() {} for x in y { } x != y */
record Settings { nullable: bool, asValue: string, tryCount: i64, throwing: bool, anyOf: string, forEach: string }
fn describe(settings: Settings, unknownish: string) -> string {
    let awaiting = "null ? undefined : nil && !throw != import(x)";
    let isNull = settings.nullable;
    if isNull { settings.asValue + awaiting + unknownish } else { "try { } catch { } as any" }
}
effect fn main() -> void { void }
`,
		"bound-keywords": `record try { x: string }
record any { x: string }
fn as(x: string) -> string { x }
fn throw(x: string) -> string { x }
fn pick(t: any) -> string { t.x }
fn names() -> string { let null = "a"; let nil = null; let await = as(nil); let made = try { x: await }; throw(made.x) }
effect fn main() -> void { void }
`,
	} {
		t.Run(name, func(t *testing.T) {
			if r := Compile(source); !r.Checked || len(r.Diagnostics) != 0 {
				t.Fatalf("admitted source changed: %+v", r.Diagnostics)
			}
		})
	}
}

// Every probe that parsed at f4c16b0 still formats, to source that reports
// the same construct: diagnosis happens where the spelling already failed,
// so the syntax-only formatter is untouched.
func TestAbsentSyntaxKeepsParseableProbesFormattable(t *testing.T) {
	for _, source := range []string{
		"effect fn main() -> void { let x = null; void }",
		`effect fn main() -> void { throw "x" }`,
		"fn cast(x: string) -> string { x as string }",
		`effect fn f() -> void { let m = import("x"); void }`,
		`fn f() -> string { for x in y { x }; "a" }`,
		`fn f(x: unknown) -> string { "a" }`,
		`fn f() -> any { "a" }`,
	} {
		formatted, err := FormatSource(source)
		if err != nil {
			t.Fatalf("parseable probe no longer formats: %q: %v", source, err)
		}
		again, err := FormatSource(formatted.Text)
		if err != nil || again.Changed {
			t.Fatalf("formatting is not stable for %q: %v", source, err)
		}
		before, after := Compile(source), Compile(formatted.Text)
		if !hasCode(before, absentSyntaxCode) || !hasCode(after, absentSyntaxCode) {
			t.Fatalf("formatting changed the absent construct report: %+v / %+v", before.Diagnostics, after.Diagnostics)
		}
	}
}

// Recovery covers only the recognized construct: its keyword, header and
// braced body. An EF003 raised inside that body is kept, and every fault
// outside it, including faults after a bound name such as `record try` or
// `fn while`, keeps its own f4c16b0 diagnostic and span.
func TestAbsentSyntaxRecoveryIsBoundedToTheConstruct(t *testing.T) {
	for _, test := range []struct{ name, source, code, message, marker, token string }{
		{"no-head", "fn f() -> string { let x = = \"a\"; x }", "EF002", "expected expression", "= \"a\"", "="},
		{"leading-assignment", "fn f() -> string { = \"a\" }", "EF002", "expected expression", "= \"a\"", "="},
		{"bare-block", "fn f() -> string { { \"a\" } }", "EF002", "expected expression", "{ \"a\"", "{"},
		{"lexical", "record R { x: string } fn f() -> string { R { x: & } }", "EF001", "unsupported character '&'", "&", "&"},
		{"body-fault", "fn f(b: bool) -> string { while b { \"a\" }; \"a\" }", "EF003", "`while` loops are not yet supported", "while", "while"},
		{"body-keeps-ef003", "fn f(b: bool) -> string {\n    while b { x: if b { \"a\" } }\n}", "EF003", "Effra has no `if` without `else`", "if b", "if"},
		{"c-family-header", "fn f(n: string) -> string { while (i < n) { i++ }; \"a\" }", "EF003", "`while` loops are not yet supported", "while", "while"},
		{"go-range", "fn f(xs: string) -> string { for _, x := range xs { x }; \"a\" }", "EF003", "`for` loops are not yet supported", "for", "for"},
		{"go-for-clause", "fn f(n: string) -> string { for i := 0; i < n; i++ { x }; \"a\" }", "EF003", "`for` loops are not yet supported", "for", "for"},
		{"catch-clause", "effect fn main() -> void { try { } catch (e) { void } }", "EF003", "Effra has no `try`/`catch` blocks", "catch", "catch"},
		{"after-body", "fn f(b: bool) -> string { while b { x }; let c = = \"d\"; \"a\" }", "EF002", "expected expression", "= \"d\"", "="},
		{"bound-try", "record try { x: string }\nfn f() -> string {\n    try { x: \"a\" }\n    let b = = \"c\"\n    \"b\"\n}", "EF002", "expected expression", "= \"c\"", "="},
		{"bound-while", "fn while(x: string) -> string { x }\nfn f() -> string {\n    while(\"a\")\n    let b = = \"c\"\n    \"b\"\n}", "EF002", "expected expression", "= \"c\"", "="},
		{"far-fault", "fn helper() -> string { \"a\" }\nfn f() -> string {\n    for x in y { x }\n    let a = helper()\n    let c = helper(,)\n    \"b\"\n}", "EF002", "expected expression", ",)", ","},
		{"headless", "record R { x: string }\nfn f() -> string {\n    while\n    let r = R { x: \"a\" }\n    let b = = \"c\"\n    \"b\"\n}", "EF002", "expected expression", "= \"c\"", "="},
		{"headless-later-brace", "record R { x: string }\nfn f() -> string {\n    while\n    let r = R { x: = }\n    \"b\"\n}", "EF002", "expected expression", "= }", "="},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(test.source)
			span := Span{Offset: strings.Index(test.source, test.marker) + strings.Index(test.marker, test.token), Length: len(test.token)}
			if r.Checked || len(r.Diagnostics) != 1 {
				t.Fatalf("got %+v, want one %s %q", r.Diagnostics, test.code, test.message)
			}
			got := r.Diagnostics[0]
			if got.Code != test.code || got.Message != test.message || got.Span.Offset != span.Offset || got.Span.Length != span.Length {
				t.Fatalf("got %s %q at %d+%d, want %s %q at %d+%d", got.Code, got.Message, got.Span.Offset, got.Span.Length, test.code, test.message, span.Offset, span.Length)
			}
		})
	}
}
