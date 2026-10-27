package compiler

import (
	"encoding/json"
	"strings"
	"testing"
)

// A foreign result reuses the report and its projections without checked
// facts of this compiler.
func TestForeignResultReportsOnlyItsDiagnostics(t *testing.T) {
	source := "package p\n\nfunc main() { é $ }\n"
	offset := strings.Index(source, "$")
	located := Diagnostic{Code: "S001", Message: "unexpected character", Span: Span{Offset: offset, Length: 1, Line: 3, Column: offset - strings.LastIndexByte(source[:offset], '\n')}}
	refusal := Diagnostic{Code: "EF144", Message: "edition manifest refused"}
	r := ForeignResult(source, Target, []Diagnostic{located, refusal})
	if r.Checked || r.Program != nil || len(r.Diagnostics) != 2 || r.Revision == "" {
		t.Fatalf("foreign result %+v", r)
	}
	report := r.ForeignReport(SourceSnapshot{URI: "file:///w/main.ef", Text: source}, false, "lint is unavailable")
	if report.Checked || report.PolicyPassed || report.LintAvailable || report.LintComplete || report.LintUnavailableReason != "lint is unavailable" || len(report.Diagnostics) != 2 {
		t.Fatalf("report %+v", report)
	}
	first, second := report.Diagnostics[0], report.Diagnostics[1]
	if first.Code != "S001" || !first.LocationAvailable || first.LSP.Range.Start != (DiagnosticPosition{Line: 2, Character: 16}) {
		t.Fatalf("located finding %+v %+v", first, first.LSP)
	}
	if second.Code != "EF144" || second.LocationAvailable || second.LSP == nil || second.LSP.Range != (DiagnosticRange{}) || second.LSP.Code != "EF144" {
		t.Fatalf("a location-less finding must project to the document start: %+v", second)
	}
	response := r.ForeignCheckResponse("")
	encoded, _ := json.Marshal(response)
	for _, absent := range []string{"symbols", "types", "applications", "declarations"} {
		if _, ok := response[absent]; ok {
			t.Fatalf("foreign check response carries %s: %s", absent, encoded)
		}
	}
	if response["edition"] != nil || response["checked"] != false || !strings.Contains(string(encoded), `"edition":null`) {
		t.Fatalf("refused check response %s", encoded)
	}

	clean := ForeignResult("package p\n", Target, nil)
	if !clean.Checked {
		t.Fatal("a foreign result without diagnostics is not checked")
	}
	cleanReport := clean.ForeignReport(SourceSnapshot{Text: "package p\n"}, true, "lint is unavailable")
	if !cleanReport.PolicyPassed || len(cleanReport.Diagnostics) != 0 || cleanReport.LintAvailable {
		t.Fatalf("clean report %+v", cleanReport)
	}
	if response := clean.ForeignCheckResponse("2026"); response["edition"] != "2026" || response["checked"] != true {
		t.Fatalf("clean check response %v", response)
	}
	if bad := ForeignResult("package p\n", "js", nil); bad.Checked || len(bad.Diagnostics) != 1 || bad.Diagnostics[0].Code != "EF110" {
		t.Fatalf("an unsupported target is not EF110: %+v", bad.Diagnostics)
	}
}
