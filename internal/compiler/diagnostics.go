package compiler

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const DiagnosticReportSchemaVersion = 1

// SourceSnapshot identifies the exact text that produced a semantic result.
// The source text is kept outside JSON so adapters can choose their transport
// without creating a second analysis model.
type SourceSnapshot struct {
	URI    string
	Origin string
	Text   string
}

type SourceIdentity struct {
	URI    string `json:"uri"`
	Origin string `json:"origin"`
}

type DiagnosticPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type DiagnosticRange struct {
	Start DiagnosticPosition `json:"start"`
	End   DiagnosticPosition `json:"end"`
}

// LSPDiagnostic is the plain-text LSP projection. Character offsets use
// UTF-16 code units, the mandatory default encoding in the LSP contract.
type LSPDiagnostic struct {
	Range    DiagnosticRange `json:"range"`
	Severity int             `json:"severity"`
	Code     string          `json:"code"`
	Source   string          `json:"source"`
	Message  string          `json:"message"`
}

type DiagnosticFinding struct {
	Code              string         `json:"code"`
	Origin            string         `json:"origin"`
	Rule              string         `json:"rule,omitempty"`
	Severity          string         `json:"severity"`
	Message           string         `json:"message"`
	Span              Span           `json:"span"`
	LocationAvailable bool           `json:"locationAvailable"`
	LSP               *LSPDiagnostic `json:"lsp,omitempty"`
}

type DiagnosticCounts struct {
	Errors      int `json:"errors"`
	Warnings    int `json:"warnings"`
	Information int `json:"information"`
	Hints       int `json:"hints"`
}

// DiagnosticReport is the shared compiler-owned projection used by CLI and
// MCP. TotalCounts always describes the complete finding set, even when an
// adapter applies a bounded result limit.
type DiagnosticReport struct {
	SchemaVersion         int                 `json:"schemaVersion"`
	Source                SourceIdentity      `json:"source"`
	Revision              string              `json:"revision"`
	Target                string              `json:"target"`
	Checked               bool                `json:"checked"`
	Strict                bool                `json:"strict"`
	PolicyPassed          bool                `json:"policyPassed"`
	LintAvailable         bool                `json:"lintAvailable"`
	LintUnavailableReason string              `json:"lintUnavailableReason,omitempty"`
	TotalCounts           DiagnosticCounts    `json:"totalCounts"`
	Diagnostics           []DiagnosticFinding `json:"diagnostics"`
	ReturnedCount         int                 `json:"returnedCount"`
	Truncated             bool                `json:"truncated"`
}

type sourcePositionIndex struct {
	positions []DiagnosticPosition
	valid     []bool
}

func newSourcePositionIndex(source string) sourcePositionIndex {
	index := sourcePositionIndex{
		positions: make([]DiagnosticPosition, len(source)+1),
		valid:     make([]bool, len(source)+1),
	}
	line, character := 0, 0
	index.positions[0] = DiagnosticPosition{Line: line, Character: character}
	index.valid[0] = true
	for offset := 0; offset < len(source); {
		if source[offset] == '\r' && offset+1 < len(source) && source[offset+1] == '\n' {
			index.valid[offset+1] = false
			offset += 2
			line++
			character = 0
			index.positions[offset] = DiagnosticPosition{Line: line, Character: character}
			index.valid[offset] = true
			continue
		}
		if source[offset] == '\n' {
			offset++
			line++
			character = 0
			index.positions[offset] = DiagnosticPosition{Line: line, Character: character}
			index.valid[offset] = true
			continue
		}
		runeValue, width := utf8.DecodeRuneInString(source[offset:])
		if width == 0 {
			break
		}
		for interior := 1; interior < width; interior++ {
			index.valid[offset+interior] = false
		}
		offset += width
		if runeValue > 0xffff {
			character += 2
		} else {
			character++
		}
		index.positions[offset] = DiagnosticPosition{Line: line, Character: character}
		index.valid[offset] = true
	}
	return index
}

// UTF16Range maps an existing UTF-8 byte span to a valid, end-exclusive LSP
// range. It returns false when the span is outside the source, lands inside a
// UTF-8 sequence, or ends between the two bytes of a CRLF sequence.
func UTF16Range(source string, span Span) (DiagnosticRange, bool) {
	if span.Offset < 0 || span.Length < 0 || span.Offset > len(source) || span.Length > len(source)-span.Offset {
		return DiagnosticRange{}, false
	}
	index := newSourcePositionIndex(source)
	end := span.Offset + span.Length
	if !index.valid[span.Offset] || !index.valid[end] {
		return DiagnosticRange{}, false
	}
	return DiagnosticRange{Start: index.positions[span.Offset], End: index.positions[end]}, true
}

func diagnosticSeverity(name string) (string, int) {
	switch name {
	case "error":
		return "error", 1
	case "warning":
		return "warning", 2
	case "information", "info":
		return "information", 3
	case "suggestion", "hint":
		return "hint", 4
	default:
		return "error", 1
	}
}

func (r *Result) DiagnosticReport(snapshot SourceSnapshot, strict bool) DiagnosticReport {
	if snapshot.Origin == "" {
		snapshot.Origin = "disk"
	}
	if snapshot.URI == "" {
		snapshot.URI = "<buffer>"
	}
	report := DiagnosticReport{
		SchemaVersion: DiagnosticReportSchemaVersion,
		Source:        SourceIdentity{URI: snapshot.URI, Origin: snapshot.Origin},
		Revision:      r.Revision,
		Target:        r.Target,
		Checked:       r.Checked,
		Strict:        strict,
		Diagnostics:   []DiagnosticFinding{},
	}
	if !r.Checked {
		report.LintUnavailableReason = "source is unchecked; semantic lint advice is unavailable"
	}
	report.LintAvailable = r.Checked
	positionIndex := newSourcePositionIndex(snapshot.Text)
	seen := map[string]bool{}
	appendFinding := func(code, origin, rule, severity, message string, span Span) {
		severity, lspSeverity := diagnosticSeverity(severity)
		key := strings.Join([]string{code, origin, rule, message, fmt.Sprintf("%d:%d", span.Offset, span.Length)}, "\x00")
		if seen[key] {
			return
		}
		seen[key] = true
		finding := DiagnosticFinding{Code: code, Origin: origin, Rule: rule, Severity: severity, Message: message, Span: span}
		// A zero Span is the compiler's explicit no-location value for
		// diagnostics such as an unsupported target. Real source spans carry
		// one-based lexer coordinates, including valid zero-length EOF spans.
		hasSourceSpan := span.Offset != 0 || span.Length != 0 || span.Line != 0 || span.Column != 0
		if hasSourceSpan && span.Offset >= 0 && span.Length >= 0 && span.Offset <= len(snapshot.Text) && span.Length <= len(snapshot.Text)-span.Offset {
			end := span.Offset + span.Length
			if positionIndex.valid[span.Offset] && positionIndex.valid[end] {
				finding.LocationAvailable = true
				finding.LSP = &LSPDiagnostic{Range: DiagnosticRange{Start: positionIndex.positions[span.Offset], End: positionIndex.positions[end]}, Severity: lspSeverity, Code: code, Source: "effra", Message: message}
			}
		}
		report.Diagnostics = append(report.Diagnostics, finding)
	}
	for _, diagnostic := range r.Diagnostics {
		appendFinding(diagnostic.Code, "compiler", "", "error", diagnostic.Message, diagnostic.Span)
	}
	var lint LintResult
	if r.Checked {
		lint = r.Lint(strict)
		for _, diagnostic := range lint.LintDiagnostics {
			appendFinding(diagnostic.Code, "lint", diagnostic.Rule, diagnostic.Severity, diagnostic.Message, diagnostic.Span)
		}
		report.PolicyPassed = lint.LintPassed
	} else {
		report.PolicyPassed = false
	}
	sort.SliceStable(report.Diagnostics, func(i, j int) bool {
		left, right := report.Diagnostics[i], report.Diagnostics[j]
		if left.LocationAvailable != right.LocationAvailable {
			return left.LocationAvailable
		}
		if left.Span.Offset != right.Span.Offset {
			return left.Span.Offset < right.Span.Offset
		}
		if left.Span.Length != right.Span.Length {
			return left.Span.Length < right.Span.Length
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Origin < right.Origin
	})
	for _, finding := range report.Diagnostics {
		switch finding.Severity {
		case "error":
			report.TotalCounts.Errors++
		case "warning":
			report.TotalCounts.Warnings++
		case "information":
			report.TotalCounts.Information++
		case "hint":
			report.TotalCounts.Hints++
		}
	}
	report.ReturnedCount = len(report.Diagnostics)
	return report
}

func (r DiagnosticReport) Bounded(limit int) (DiagnosticReport, error) {
	if limit < 0 {
		return DiagnosticReport{}, fmt.Errorf("diagnostic limit must not be negative")
	}
	if len(r.Diagnostics) > limit {
		r.Diagnostics = append([]DiagnosticFinding(nil), r.Diagnostics[:limit]...)
		r.Truncated = true
	}
	r.ReturnedCount = len(r.Diagnostics)
	return r, nil
}
