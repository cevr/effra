// Package sketchfront is the front-end of the 2026 edition: a thin adapter
// over the frozen prototype compiler effra.local/sketch9. It is the only
// package that imports the prototype. It maps the prototype's diagnostics to
// compiler diagnostics with UTF-8 byte spans, so every surface projects them
// with the shared report code, and it builds a checked program to a native
// executable through the prototype's direct-Go emitter.
//
// #195 moves the parser into this module; this package then changes its
// import and no surface changes. No runtime or generated-code package may
// import it: generated modules require this module without the prototype's
// replacement.
package sketchfront

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/receipt"
	"effra.local/sketch9/sketch"
)

// sourceName is the fixed file name the adapter passes to the prototype's
// parser, which prefixes its syntax error with it.
const sourceName = "effra-2026.ef"

// Analysis is the 2026 front-end's outcome for one source text.
type Analysis struct {
	// Diagnostics are the prototype's diagnostics in source order, with
	// UTF-8 byte spans into the analysed text.
	Diagnostics []compiler.Diagnostic
	checked     *sketch.Checked
}

// Check parses and checks source with the prototype.
func Check(source string) Analysis {
	lines := lineStarts(source)
	file, err := sketch.Parse(sourceName, source)
	if err != nil {
		return Analysis{Diagnostics: []compiler.Diagnostic{parseDiagnostic(source, lines, err.Error())}}
	}
	checked, diagnostics := sketch.Check(file)
	analysis := Analysis{checked: checked}
	for _, diagnostic := range diagnostics {
		analysis.Diagnostics = append(analysis.Diagnostics, compiler.Diagnostic{Code: diagnostic.Code, Message: diagnostic.Msg, Span: span(source, lines, diagnostic.Pos.Line, diagnostic.Pos.Col)})
	}
	if len(analysis.Diagnostics) > 0 {
		analysis.checked = nil
	}
	return analysis
}

// Result is the diagnostics-only compiler result of the analysis.
func (a Analysis) Result(source, target string) *compiler.Result {
	return compiler.ForeignResult(source, target, a.Diagnostics)
}

// parseDiagnostic recovers S001 and its position from the prototype's
// formatted error "NAME:LINE:COL: CODE MESSAGE". An error of any other shape
// keeps its whole text and no location, so nothing is dropped.
func parseDiagnostic(source string, lines []int, text string) compiler.Diagnostic {
	fallback := compiler.Diagnostic{Code: "S001", Message: text}
	rest, ok := strings.CutPrefix(text, sourceName+":")
	if !ok {
		return fallback
	}
	lineText, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return fallback
	}
	colText, rest, ok := strings.Cut(rest, ": ")
	if !ok {
		return fallback
	}
	line, lineErr := strconv.Atoi(lineText)
	col, colErr := strconv.Atoi(colText)
	code, message, ok := strings.Cut(rest, " ")
	if lineErr != nil || colErr != nil || !ok || !strings.HasPrefix(code, "S") {
		return fallback
	}
	return compiler.Diagnostic{Code: code, Message: message, Span: span(source, lines, line, col)}
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
func span(source string, lines []int, line, col int) compiler.Span {
	if line < 1 || line > len(lines) || col < 1 {
		return compiler.Span{}
	}
	offset := lines[line-1] + col - 1
	end := len(source)
	if line < len(lines) {
		end = lines[line] - 1
	}
	if offset > end || offset > len(source) {
		return compiler.Span{}
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
	return compiler.Span{Offset: offset, Length: length, Line: line, Column: col}
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
	code, err := sketch.Emit(a.checked)
	if err != nil {
		return fmt.Errorf("2026 emission failed: %w", err)
	}
	module, err := os.MkdirTemp("", "effra-2026-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(module)
	files := map[string]string{"go.mod": sketch.GoMod, "main.go": code, filepath.Join("rt", "rt.go"): sketch.RuntimeSource()}
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
