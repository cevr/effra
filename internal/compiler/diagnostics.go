package compiler

import (
	"fmt"
	"net/url"
	"path/filepath"
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
	Text   string `json:"-"`
}

// FileURI identifies the requested document, not its current symlink target.
// URI construction never reads the filesystem: a replacement during checking
// must not attach the captured source bytes to a different document. Relative
// and absolute requests for the same logical path share an escaped URI.
func FileURI(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String(), nil
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
	Range              DiagnosticRange         `json:"range"`
	Severity           int                     `json:"severity"`
	Code               string                  `json:"code"`
	Source             string                  `json:"source"`
	Message            string                  `json:"message"`
	RelatedInformation []LSPRelatedInformation `json:"relatedInformation,omitempty"`
}
type LSPRelatedInformation struct {
	Location LSPDiagnosticLocation `json:"location"`
	Message  string                `json:"message"`
}
type LSPDiagnosticLocation struct {
	URI   string          `json:"uri"`
	Range DiagnosticRange `json:"range"`
}

type DiagnosticFinding struct {
	Code              string            `json:"code"`
	Origin            string            `json:"origin"`
	Rule              string            `json:"rule,omitempty"`
	Severity          string            `json:"severity"`
	Message           string            `json:"message"`
	Help              string            `json:"help,omitempty"`
	Span              Span              `json:"span"`
	Related           []RelatedLocation `json:"related,omitempty"`
	LocationAvailable bool              `json:"locationAvailable"`
	LSP               *LSPDiagnostic    `json:"lsp,omitempty"`
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
	ProducerMetadata
	SchemaVersion         int                 `json:"schemaVersion"`
	Source                SourceIdentity      `json:"source"`
	Revision              string              `json:"revision"`
	Target                string              `json:"target"`
	Checked               bool                `json:"checked"`
	Strict                bool                `json:"strict"`
	PolicyPassed          bool                `json:"policyPassed"`
	LintAvailable         bool                `json:"lintAvailable"`
	LintComplete          bool                `json:"lintComplete"`
	LintUnavailableReason string              `json:"lintUnavailableReason,omitempty"`
	TotalCounts           DiagnosticCounts    `json:"totalCounts"`
	Diagnostics           []DiagnosticFinding `json:"diagnostics"`
	// Suppressions is the status of every well-formed suppression
	// directive, also for unchecked source, where none was evaluated.
	Suppressions  []DiagnosticSuppression `json:"suppressions"`
	ReturnedCount int                     `json:"returnedCount"`
	Truncated     bool                    `json:"truncated"`
}

// DiagnosticSuppression is a suppression status with the zero-based UTF-16
// range of its directive, which every surface reports identically. Range
// is absent only when the report has no text to locate the directive in.
type DiagnosticSuppression struct {
	SuppressionStatus
	Range *DiagnosticRange `json:"range,omitempty"`
}

func legalDiagnosticPosition(source string, index sourcePositionIndex, offset int) (DiagnosticPosition, bool) {
	if index.valid[offset] {
		return index.positions[offset], true
	}
	// A lexer span may end after CR but before LF. LSP has no position at
	// that byte boundary, so project that endpoint to the line end before CR
	// while preserving the original UTF-8 byte span in the finding.
	if offset > 0 && offset < len(source) && source[offset-1] == '\r' && source[offset] == '\n' && index.valid[offset-1] {
		return index.positions[offset-1], true
	}
	return DiagnosticPosition{}, false
}

type sourcePositionIndex struct {
	positions []DiagnosticPosition
	valid     []bool
	lines     []int // byte offset where each LSP line starts
}

func newSourcePositionIndex(source string) sourcePositionIndex {
	index := sourcePositionIndex{
		positions: make([]DiagnosticPosition, len(source)+1),
		valid:     make([]bool, len(source)+1),
		lines:     []int{0},
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
			index.lines = append(index.lines, offset)
			continue
		}
		if source[offset] == '\n' || source[offset] == '\r' {
			offset++
			line++
			character = 0
			index.positions[offset] = DiagnosticPosition{Line: line, Character: character}
			index.valid[offset] = true
			index.lines = append(index.lines, offset)
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
// range. It returns false when the span is outside the source or lands inside
// a UTF-8 sequence. A CRLF-internal endpoint is normalized to the line end.
func UTF16Range(source string, span Span) (DiagnosticRange, bool) {
	return newSourcePositionIndex(source).rangeFor(source, span)
}

// SourcePositions converts in both directions between UTF-8 byte offsets and
// LSP UTF-16 positions of one source text, over the index diagnostics use.
type SourcePositions struct {
	source string
	index  sourcePositionIndex
}

func NewSourcePositions(source string) SourcePositions {
	return SourcePositions{source: source, index: newSourcePositionIndex(source)}
}

// Range is UTF16Range over this shared index.
func (p SourcePositions) Range(span Span) (DiagnosticRange, bool) {
	return p.index.rangeFor(p.source, span)
}

// Offset maps an LSP position to the byte offset of the code unit it names.
// As LSP specifies, a character beyond the line length denotes the line end,
// before its CR, LF or CRLF terminator. A line outside the source or a
// character between the two UTF-16 units of one astral character has none.
func (p SourcePositions) Offset(position DiagnosticPosition) (int, bool) {
	lines := p.index.lines
	if position.Line < 0 || position.Character < 0 || position.Line >= len(lines) {
		return 0, false
	}
	start, end := lines[position.Line], len(p.source)
	if position.Line+1 < len(lines) {
		end = lines[position.Line+1] - 1
		if end > start && p.source[end-1] == '\r' && p.source[end] == '\n' {
			end--
		}
	}
	for offset := start; offset <= end; offset++ {
		if !p.index.valid[offset] {
			continue
		}
		if character := p.index.positions[offset].Character; character == position.Character {
			return offset, true
		} else if character > position.Character {
			return 0, false
		}
	}
	return end, true
}

func (index sourcePositionIndex) rangeFor(source string, span Span) (DiagnosticRange, bool) {
	if span.Offset < 0 || span.Length < 0 || span.Offset > len(source) || span.Length > len(source)-span.Offset {
		return DiagnosticRange{}, false
	}
	end := span.Offset + span.Length
	start, startOK := legalDiagnosticPosition(source, index, span.Offset)
	finish, endOK := legalDiagnosticPosition(source, index, end)
	if !startOK || !endOK {
		return DiagnosticRange{}, false
	}
	return DiagnosticRange{Start: start, End: finish}, true
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

// lspMessage keeps help in the plain-text message, the one field every LSP
// client renders; the finding's own message and help stay separate.
func lspMessage(message, help string) string {
	if help == "" {
		return message
	}
	return message + "\nhelp: " + help
}

func (r *Result) DiagnosticReport(snapshot SourceSnapshot, strict bool) DiagnosticReport {
	return r.DiagnosticReportWith(snapshot, strict, LintPacks{})
}

// DiagnosticReportWith is the diagnostic report with lint advice under the
// configuration, merged with the reports of its selected rule packs. A
// lint-runner error has no source location; its LSP projection is the
// document start, so an editor shows that lint is incomplete instead of
// dropping the finding.
func (r *Result) DiagnosticReportWith(snapshot SourceSnapshot, strict bool, packs LintPacks) DiagnosticReport {
	if snapshot.Origin == "" {
		snapshot.Origin = "disk"
	}
	if snapshot.URI == "" {
		snapshot.URI = "<buffer>"
	}
	report := DiagnosticReport{
		ProducerMetadata: r.producerMetadata,
		SchemaVersion:    DiagnosticReportSchemaVersion,
		Source:           SourceIdentity{URI: snapshot.URI, Origin: snapshot.Origin},
		Revision:         r.Revision,
		Target:           r.Target,
		Checked:          r.Checked,
		Strict:           strict,
		Diagnostics:      []DiagnosticFinding{},
		Suppressions:     []DiagnosticSuppression{},
	}
	if !r.Checked {
		report.LintUnavailableReason = "source is unchecked; semantic lint advice is unavailable"
	}
	report.LintAvailable = r.Checked
	positionIndex := newSourcePositionIndex(snapshot.Text)
	seen := map[string]bool{}
	appendFinding := func(code, origin, rule, severity, message, help string, span Span, related []RelatedLocation) {
		severity, lspSeverity := diagnosticSeverity(severity)
		key := strings.Join([]string{code, origin, rule, message, fmt.Sprintf("%d:%d", span.Offset, span.Length)}, "\x00")
		if seen[key] {
			return
		}
		seen[key] = true
		finding := DiagnosticFinding{Code: code, Origin: origin, Rule: rule, Severity: severity, Message: message, Help: help, Span: span}
		// A zero Span is the compiler's explicit no-location value for
		// diagnostics such as an unsupported target. Real source spans carry
		// one-based lexer coordinates, including valid zero-length EOF spans.
		hasSourceSpan := span.Offset != 0 || span.Length != 0 || span.Line != 0 || span.Column != 0
		if location, ok := positionIndex.rangeFor(snapshot.Text, span); hasSourceSpan && ok {
			finding.LocationAvailable = true
			finding.LSP = &LSPDiagnostic{Range: location, Severity: lspSeverity, Code: code, Source: "effra", Message: lspMessage(message, help)}
		} else if code == lintRunnerCode && !hasSourceSpan {
			finding.LSP = &LSPDiagnostic{Severity: lspSeverity, Code: code, Source: "effra", Message: message}
		}
		if finding.Origin == "compiler" || len(related) > 0 {
			finding.Related = append([]RelatedLocation{}, related...)
		}
		if finding.LocationAvailable {
			for _, related := range related {
				if location, ok := positionIndex.rangeFor(snapshot.Text, related.Span); ok {
					finding.LSP.RelatedInformation = append(finding.LSP.RelatedInformation, LSPRelatedInformation{Location: LSPDiagnosticLocation{URI: snapshot.URI, Range: location}, Message: related.Message})
				}
			}
		}
		report.Diagnostics = append(report.Diagnostics, finding)
	}
	for _, diagnostic := range r.Diagnostics {
		appendFinding(diagnostic.Code, "compiler", "", "error", diagnostic.Message, diagnostic.Help, diagnostic.Span, diagnostic.Related)
	}
	// Lint always decides suppression statuses; its findings join the
	// report only for checked source.
	lint := r.LintWith(strict, packs)
	for _, status := range lint.Suppressions {
		suppression := DiagnosticSuppression{SuppressionStatus: status}
		if location, ok := positionIndex.rangeFor(snapshot.Text, status.Span); ok {
			suppression.Range = &location
		}
		report.Suppressions = append(report.Suppressions, suppression)
	}
	if r.Checked {
		for _, diagnostic := range lint.LintDiagnostics {
			appendFinding(diagnostic.Code, "lint", diagnostic.Rule, diagnostic.Severity, diagnostic.Message, "", diagnostic.Span, diagnostic.Related)
		}
		report.PolicyPassed = lint.LintPassed
		report.LintComplete = lint.Complete
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
		if left.Origin != right.Origin {
			return left.Origin < right.Origin
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		return left.Message < right.Message
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
		return DiagnosticReport{}, fmt.Errorf("diagnostic result exceeds limit of %d findings", limit)
	}
	r.ReturnedCount = len(r.Diagnostics)
	return r, nil
}
