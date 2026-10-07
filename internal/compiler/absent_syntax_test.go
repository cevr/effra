package compiler

import (
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

// A genuine diagnostic raised inside a braced absent construct is kept when it
// is itself an absent construct, and parse faults outside any absent head are
// unchanged.
func TestAbsentSyntaxRecoveryKeepsUnrelatedSyntaxFaults(t *testing.T) {
	for _, test := range []struct{ source, code, message string }{
		{"fn f() -> string { let x = = \"a\"; x }", "EF002", "expected expression"},
		{"fn f() -> string { = \"a\" }", "EF002", "expected expression"},
		{"fn f() -> string { { \"a\" } }", "EF002", "expected expression"},
		{"fn f(b: bool) -> string { while b { \"a\" }; if b { \"a\" } }", "EF003", "`while` loops are not yet supported"},
		{"record R { x: string } fn f() -> string { R { x: & } }", "EF001", "unsupported character '&'"},
	} {
		r := Compile(test.source)
		if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != test.code || r.Diagnostics[0].Message != test.message {
			t.Fatalf("%q: got %+v, want %s %q", test.source, r.Diagnostics, test.code, test.message)
		}
	}
}
