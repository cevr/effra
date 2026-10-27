package compiler

import (
	"crypto/sha256"
	"encoding/hex"
)

// ForeignResult is the diagnostics-only result for source that this
// compiler did not analyse: source another edition's front-end checked, or
// source an edition refusal stopped before any front-end ran. It carries the
// source revision, the target and the given diagnostics, and no program, so
// it reuses the diagnostic report and its UTF-8 span, UTF-16 range and LSP
// projections without inventing checked facts. Checked means only that the
// diagnostics are empty. A surface must not ask a foreign result for
// navigation, inspection, graphs, tests or emission: it routes those
// operations by edition first.
func ForeignResult(source, target string, diagnostics []Diagnostic) *Result {
	hash := sha256.Sum256([]byte(source))
	r := &Result{SchemaVersion: SemanticSchemaVersion, Revision: hex.EncodeToString(hash[:]), Target: target, Diagnostics: append([]Diagnostic{}, diagnostics...), Symbols: []Symbol{}, TypeProjectionBudget: maxTypeProjectionNodes, TypeProjectionLimits: defaultProjectionLimits, facts: map[*Expr]ExpressionFacts{}, sourceBytes: len(source)}
	if diagnostic, unsupported := UnsupportedTarget(target); unsupported {
		r.Diagnostics = []Diagnostic{diagnostic}
	}
	r.Checked = len(r.Diagnostics) == 0
	return r
}

// ForeignReport is the diagnostic report of a foreign result. Lint is
// unavailable for it, for the reason given. A diagnostic without a source
// location, such as an edition refusal, is projected to the document start
// for LSP, as a lint-runner error is, so an editor shows it instead of
// dropping it.
func (r *Result) ForeignReport(snapshot SourceSnapshot, strict bool, lintUnavailable string) DiagnosticReport {
	report := r.DiagnosticReportWith(snapshot, strict, LintPacks{})
	report.LintAvailable = false
	report.LintComplete = false
	report.LintUnavailableReason = lintUnavailable
	for index, finding := range report.Diagnostics {
		if finding.LSP == nil {
			severity, lspSeverity := diagnosticSeverity(finding.Severity)
			report.Diagnostics[index].Severity = severity
			report.Diagnostics[index].LSP = &LSPDiagnostic{Severity: lspSeverity, Code: finding.Code, Source: "ergo", Message: lspMessage(finding.Message, finding.Help)}
		}
	}
	return report
}

// ForeignCheckResponse is the check envelope of a foreign result: the
// admission fields of CheckResponse and the edition that answered. It has no
// symbol, type or application tables, because this compiler checked nothing.
// An empty edition, after an edition refusal, is null.
func (r *Result) ForeignCheckResponse(edition string) map[string]any {
	var answered any
	if edition != "" {
		answered = edition
	}
	diagnostics := r.Diagnostics
	truncated := len(diagnostics) > 100
	if truncated {
		diagnostics = diagnostics[:100]
	}
	response := map[string]any{
		"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked,
		"edition": answered, "diagnostics": diagnostics, "diagnosticsTruncated": truncated,
	}
	r.AddProducer(response)
	return response
}
