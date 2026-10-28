// Package sketchfront is Ergo's one front end: a thin adapter
// that parses with internal/sketch/syntax, checks and emits through the
// prototype checker and emitter behind the bridge (internal/sketch/proto),
// and formats with internal/sketch/format. It maps every diagnostic to a
// report diagnostic with a UTF-8 byte span, so every surface projects it
// with the shared report code, and it builds a checked program to a native
// executable through the prototype's direct-Go emitter. CLI, MCP and LSP
// format 2026 sources only through Format, so they report one identity.
//
// No runtime or generated-code package may import it.
package sketchfront

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode"
	"unicode/utf8"

	"ergo.local/prototype/internal/receipt"
	"ergo.local/prototype/internal/report"
	"ergo.local/prototype/internal/sketch/format"
	"ergo.local/prototype/internal/sketch/proto"
	"ergo.local/prototype/internal/sketch/syntax"
)

// Analysis is the 2026 front-end's outcome for one source text.
type Analysis struct {
	// Diagnostics are the front-end's diagnostics in source order, with
	// UTF-8 byte spans into the analysed text.
	Diagnostics []report.Diagnostic
	checked     *proto.Checked
}

// Check parses source, converts it through the bridge and checks it with
// the prototype checker. A syntax error is the only diagnostic; a bridge
// refusal (S109) stops before the checker, which never sees a partial
// program.
func Check(source string) Analysis {
	file, err := syntax.Parse(source)
	if err != nil {
		return Analysis{Diagnostics: []report.Diagnostic{syntaxDiagnostic(err)}}
	}
	lines := lineStarts(source)
	converted, refusals := proto.FromSyntax(file)
	var analysis Analysis
	diagnostics := refusals
	if converted != nil {
		analysis.checked, diagnostics = proto.Check(converted)
	}
	for _, diagnostic := range diagnostics {
		analysis.Diagnostics = append(analysis.Diagnostics, report.Diagnostic{Code: diagnostic.Code, Message: diagnostic.Msg, Span: span(source, lines, diagnostic.Pos.Line, diagnostic.Pos.Col)})
	}
	if len(analysis.Diagnostics) > 0 {
		analysis.checked = nil
	}
	return analysis
}

// syntaxDiagnostic is the report diagnostic of a parse error, located by
// the parser's own byte span.
func syntaxDiagnostic(err error) report.Diagnostic {
	var e *syntax.Error
	if !errors.As(err, &e) {
		return report.Diagnostic{Code: syntax.CodeSyntax, Message: err.Error()}
	}
	return report.Diagnostic{Code: e.Code, Message: e.Message, Span: report.Span{Offset: e.Offset, Length: e.Length, Line: e.Line, Column: e.Column}}
}

// Result is the diagnostics-only report result of the analysis.
func (a Analysis) Result(source, target string) *report.Result {
	return report.NewResult(source, target, a.Diagnostics)
}

// FormatterIdentity names the 2026 formatter on every surface.
const FormatterIdentity = format.Identity

// Format formats a 2026 source with the syntax-only formatter, with the
// legacy formatter's result shape and errors: a report.FormatFailure
// holding the syntax diagnostic, or a report.FormatLimitError when the
// output would exceed a positive maxOutputBytes.
func Format(source string, maxOutputBytes int) (report.FormatResult, error) {
	if maxOutputBytes < 0 {
		return report.FormatResult{}, fmt.Errorf("formatter output limit must not be negative")
	}
	inputDigest := report.FormatDigest(source)
	partial := report.FormatResult{SchemaVersion: report.FormatterSchemaVersion, InputDigest: inputDigest}
	// The bound is checked while rendering, before the text is built.
	text, err := format.SourceBounded(source, maxOutputBytes)
	var limit *format.LimitError
	switch {
	case errors.As(err, &limit):
		return partial, report.FormatLimitError{Limit: maxOutputBytes}
	case err != nil:
		return partial, report.FormatFailure{Diagnostics: []report.Diagnostic{syntaxDiagnostic(err)}}
	}
	return report.FormatResult{
		SchemaVersion: report.FormatterSchemaVersion,
		InputDigest:   inputDigest,
		OutputDigest:  report.FormatDigest(text),
		Changed:       text != source,
		Text:          text,
	}, nil
}

// FailureMessage is the refusal text of a 2026 formatting failure: it names
// the first diagnostic's code and position, as the LSP does for legacy.
func FailureMessage(failure report.FormatFailure) string {
	if len(failure.Diagnostics) == 0 {
		return failure.Error()
	}
	d := failure.Diagnostics[0]
	if d.Span.Line > 0 {
		return fmt.Sprintf("source cannot be formatted: %s at %d:%d: %s", d.Code, d.Span.Line, d.Span.Column, d.Message)
	}
	return fmt.Sprintf("source cannot be formatted: %s: %s", d.Code, d.Message)
}

// lineStarts are the byte offsets where the prototype's lines start: it
// counts LF only.
func lineStarts(source string) []int {
	starts := []int{0}
	for offset := 0; offset < len(source); offset++ {
		if source[offset] == '\n' {
			starts = append(starts, offset+1)
		}
	}
	return starts
}

// span maps a prototype position to a byte span. The prototype's column is
// one-based and counts bytes (its lexer advances the column once per byte),
// so the offset is the line start plus the column minus one. The span covers
// the identifier or number at that offset, else one character, and is empty
// at the end of the text. A position outside the text has no location.
func span(source string, lines []int, line, col int) report.Span {
	if line < 1 || line > len(lines) || col < 1 {
		return report.Span{}
	}
	offset := lines[line-1] + col - 1
	end := len(source)
	if line < len(lines) {
		end = lines[line] - 1
	}
	if offset > end || offset > len(source) {
		return report.Span{}
	}
	length := 0
	for offset+length < len(source) {
		r, width := utf8.DecodeRuneInString(source[offset+length:])
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			length += width
			continue
		}
		if length == 0 && r != '\n' {
			length = width
		}
		break
	}
	return report.Span{Offset: offset, Length: length, Line: line, Column: col}
}

// Build emits the checked analysis as a Go module in a fresh temporary
// directory, builds it to output with the go command and removes the
// module. lineName is the source path //line directives name. The go
// command's output goes to stderr.
func Build(a Analysis, lineName, output string, stderr io.Writer) error {
	if a.checked == nil {
		return fmt.Errorf("the 2026 front-end has diagnostics; nothing to build")
	}
	a.checked.File.Name = lineName
	code, err := proto.Emit(a.checked)
	if err != nil {
		return fmt.Errorf("2026 emission failed: %w", err)
	}
	module, err := os.MkdirTemp("", "ergo-2026-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(module)
	files := map[string]string{"go.mod": proto.GoMod, "main.go": code, filepath.Join("rt", "rt.go"): proto.RuntimeSource()}
	for name, body := range files {
		path := filepath.Join(module, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return err
	}
	child := receipt.GoBuildCommand(module, absolute, ".")
	child.Stdout, child.Stderr = stderr, stderr
	if err := child.Run(); err != nil {
		return fmt.Errorf("Go build failed: %w", err)
	}
	return nil
}
