package compiler

import (
	"slices"
	"strings"
	"testing"
)

func TestUTF16RangeUsesSharedSourceIndex(t *testing.T) {
	source := "a𐐀e\u0301\r\nz"
	start := strings.Index(source, "e")
	rangeValue, ok := UTF16Range(source, Span{Offset: start, Length: len("e\u0301")})
	if !ok {
		t.Fatal("astral and combining span was rejected")
	}
	if rangeValue.Start != (DiagnosticPosition{Line: 0, Character: 3}) || rangeValue.End != (DiagnosticPosition{Line: 0, Character: 5}) {
		t.Fatalf("unexpected UTF-16 range: %+v", rangeValue)
	}

	lineStart := strings.Index(source, "z")
	lineRange, ok := UTF16Range(source, Span{Offset: lineStart, Length: 1})
	if !ok || lineRange.Start != (DiagnosticPosition{Line: 1, Character: 0}) || lineRange.End != (DiagnosticPosition{Line: 1, Character: 1}) {
		t.Fatalf("unexpected CRLF range: %+v ok=%v", lineRange, ok)
	}
	if _, ok := UTF16Range(source, Span{Offset: strings.Index(source, "\r") + 1, Length: 0}); ok {
		t.Fatal("accepted a position between CRLF bytes")
	}
	if eof, ok := UTF16Range(source, Span{Offset: len(source), Length: 0}); !ok || eof.End != (DiagnosticPosition{Line: 1, Character: 1}) {
		t.Fatalf("unexpected EOF range: %+v ok=%v", eof, ok)
	}
}

func TestDiagnosticReportPreservesPolicyAndOrigins(t *testing.T) {
	source := `effect fn task() -> string { "ok" }
effect fn main() -> string {
let forgotten = task()
run task().provide<Console>(Stdout)
}`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	normal := r.DiagnosticReport(SourceSnapshot{URI: "main.ef", Origin: "disk", Text: source}, false)
	if !normal.Checked || !normal.PolicyPassed || !normal.LintAvailable || normal.Strict {
		t.Fatalf("unexpected normal report: %+v", normal)
	}
	if normal.TotalCounts.Warnings != 1 || normal.TotalCounts.Hints != 1 || normal.TotalCounts.Errors != 0 {
		t.Fatalf("unexpected normal counts: %+v", normal.TotalCounts)
	}
	if got := []string{normal.Diagnostics[0].Origin, normal.Diagnostics[1].Origin}; !slices.Equal(got, []string{"lint", "lint"}) {
		t.Fatalf("unexpected origins: %v", got)
	}
	if normal.Diagnostics[0].LSP == nil || normal.Diagnostics[0].LSP.Severity != 2 || normal.Diagnostics[1].LSP == nil || normal.Diagnostics[1].LSP.Severity != 4 {
		t.Fatalf("lint severity was not projected: %+v", normal.Diagnostics)
	}
	strict := r.DiagnosticReport(SourceSnapshot{URI: "main.ef", Origin: "disk", Text: source}, true)
	if strict.PolicyPassed || !strict.Strict || strict.TotalCounts.Warnings != normal.TotalCounts.Warnings || strict.Diagnostics[0].Severity != normal.Diagnostics[0].Severity {
		t.Fatalf("strict policy changed diagnostic severity: %+v", strict)
	}
}

func TestDiagnosticReportMarksUnavailableLintAndLocations(t *testing.T) {
	source := `effect fn main() -> () { run Console.log("x") }`
	r := Compile(source)
	if r.Checked {
		t.Fatal("invalid source was checked")
	}
	report := r.DiagnosticReport(SourceSnapshot{URI: "buffer://main.ef", Origin: "buffer", Text: source}, false)
	if report.Checked || report.PolicyPassed || report.LintAvailable || report.LintUnavailableReason == "" {
		t.Fatalf("invalid-source facts were lost: %+v", report)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Origin != "compiler" || report.Diagnostics[0].LSP == nil {
		t.Fatalf("compiler diagnostic was not preserved: %+v", report.Diagnostics)
	}

	unsupported := CompileFor(`effect fn main() -> () { () }`, "llvm")
	unsupportedReport := unsupported.DiagnosticReport(SourceSnapshot{URI: "main.ef", Origin: "disk", Text: `effect fn main() -> () { () }`}, false)
	if unsupportedReport.Target != "llvm" || len(unsupportedReport.Diagnostics) != 1 || unsupportedReport.Diagnostics[0].LocationAvailable || unsupportedReport.Diagnostics[0].LSP != nil {
		t.Fatalf("unsupported target fabricated a location: %+v", unsupportedReport)
	}
}

func TestDiagnosticReportBoundsRetainTotals(t *testing.T) {
	report := DiagnosticReport{Diagnostics: make([]DiagnosticFinding, 101), TotalCounts: DiagnosticCounts{Errors: 101}}
	bounded, err := report.Bounded(100)
	if err != nil || !bounded.Truncated || bounded.ReturnedCount != 100 || bounded.TotalCounts.Errors != 101 {
		t.Fatalf("bounded report lost pre-limit facts: %+v err=%v", bounded, err)
	}
	if _, err := report.Bounded(-1); err == nil {
		t.Fatal("negative diagnostic limit accepted")
	}
}
