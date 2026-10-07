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
