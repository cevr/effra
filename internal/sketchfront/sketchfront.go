// Package sketchfront is Ergo's one front end: a thin adapter
// that parses with internal/sketch/syntax, checks with internal/sketch/check,
// and formats with internal/sketch/format. It maps every diagnostic to a
// report diagnostic with a UTF-8 byte span, so every surface projects it
// with the shared report code. The CLI checks and formats sources only
// through Check and Format, so it reports one identity.
//
// Build emits a checked program into the content-addressed emission cache
// (internal/sketch/build, #224) and links it with the go command. Until
// checked-tree lowering (internal/sketch/lower) replaces it, emission goes
// through the prototype's bridge, checker and direct-Go emitter
// (internal/sketch/proto): a program the checker accepts and the prototype
// cannot lower is refused with S109 naming #50, and nothing is published
// (spec D12).
//
// No runtime or generated-code package may import it.
package sketchfront

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"unicode"
	"unicode/utf8"

	"ergo.local/prototype/internal/producer"
	"ergo.local/prototype/internal/report"
	"ergo.local/prototype/internal/sketch/build"
	"ergo.local/prototype/internal/sketch/check"
	"ergo.local/prototype/internal/sketch/format"
	"ergo.local/prototype/internal/sketch/lower"
	"ergo.local/prototype/internal/sketch/proto"
	"ergo.local/prototype/internal/sketch/std"
	"ergo.local/prototype/internal/sketch/syntax"
)

// Analysis is the 2026 front-end's outcome for one source text. It keeps
// the parsed file and the checker's package, so building reads the same
// checked program and never parses or checks again.
type Analysis struct {
	// Diagnostics are the checker's diagnostics in source order, with UTF-8
	// byte spans into the analysed text.
	Diagnostics []report.Diagnostic
	source      string
	file        *syntax.File
	pkg         *check.Package
}

// Check parses source and checks it as one package with the 2026 checker.
// A syntax error is the only diagnostic of a source that does not parse.
func Check(source string) Analysis {
	file, err := syntax.Parse(source)
	if err != nil {
		return Analysis{Diagnostics: []report.Diagnostic{syntaxDiagnostic(err)}, source: source}
	}
	analysis := Analysis{source: source, file: file}
	pkg, diagnostics := check.Check("main", []*syntax.File{file}, nil)
	analysis.pkg = pkg
	for _, d := range diagnostics {
		analysis.Diagnostics = append(analysis.Diagnostics, report.Diagnostic{
			Code:    d.Code,
			Message: d.Message,
			Span:    report.Span{Offset: d.Offset, Length: d.Length, Line: d.Line, Column: d.Column},
		})
	}
	return analysis
}

// EmitterTicket owns lowering from the checker's output; until it lands, a
// program the prototype emitter cannot lower is refused at build.
const EmitterTicket = "#50"

// lower converts a checked program through the bridge and checks it with
// the prototype checker, the emitter's input. Every refusal is re-coded
// S109 at its position, naming #50 (spec D12).
func (a Analysis) lower() (*proto.Checked, []report.Diagnostic) {
	converted, refusals := proto.FromSyntax(a.file)
	var checked *proto.Checked
	diagnostics := refusals
	if converted != nil {
		checked, diagnostics = proto.Check(converted)
	}
	if len(diagnostics) == 0 {
		return checked, nil
	}
	lines := lineStarts(a.source)
	var out []report.Diagnostic
	for _, d := range diagnostics {
		out = append(out, report.Diagnostic{
			Code:    check.CodeOutside,
			Message: fmt.Sprintf("the prototype emitter cannot lower this program yet (%s): %s %s", EmitterTicket, d.Code, d.Msg),
			Span:    span(a.source, lines, d.Pos.Line, d.Pos.Col),
		})
	}
	return nil, out
}

// BuildError is a build refusal: the diagnostics that stop the build, with
// their positions. Nothing is written.
type BuildError struct{ Diagnostics []report.Diagnostic }

func (e *BuildError) Error() string {
	if len(e.Diagnostics) == 0 {
		return "the 2026 front-end refused the build"
	}
	d := e.Diagnostics[0]
	if d.Span.Line > 0 {
		return fmt.Sprintf("%s at %d:%d: %s", d.Code, d.Span.Line, d.Span.Column, d.Message)
	}
	return fmt.Sprintf("%s: %s", d.Code, d.Message)
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
// the first diagnostic's code and position.
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

// LanguageRevision is the fixed language revision every emission names: the
// #196 v1 contract of 2026-10-28. There is no user edition selector.
const LanguageRevision = "ergo-v1-2026-10-28"

// emitterRevision names the emitter that builds use. Stage 2 of lower-v1
// replaces the prototype emitter with internal/sketch/lower.
const emitterRevision = "proto-emit/1"

// BuildRequest is one build of a checked analysis.
type BuildRequest struct {
	// Source is the admitted absolute path of the analysed source. //line
	// directives and the emission key name it.
	Source string
	// Output is the executable to write.
	Output string
	// Cache holds the generated modules; Toolchain links them.
	Cache     build.Cache
	Toolchain build.Toolchain
	// Stderr receives the go command's output.
	Stderr io.Writer
}

// Build emits the checked analysis into the emission cache, or reuses its
// published module, and links the module to the request's output with the
// go command. It returns the module whenever one is published, also when
// the link fails, so the caller can name it. An analysis with diagnostics,
// one whose source path cannot be anchored, or one the prototype cannot
// lower is refused before anything is published; a refusal by the
// prototype is a *BuildError.
func Build(a Analysis, req BuildRequest) (build.Module, error) {
	if len(a.Diagnostics) > 0 || a.file == nil || a.pkg == nil {
		return build.Module{}, &BuildError{Diagnostics: a.Diagnostics}
	}
	unit := lower.Unit{Package: a.pkg, Files: []*syntax.File{a.file}, Sources: []lower.Source{{Path: req.Source, Text: a.source}}}
	if err := unit.Validate(); err != nil {
		return build.Module{}, err
	}
	input := build.Input{
		Dependencies: stdDependencies(),
		Compiler:     producer.Current().Qualifier,
		Emitter:      emitterRevision,
		Runtime:      []build.File{{Path: "rt/rt.go", Data: []byte(proto.RuntimeSource())}},
		Language:     LanguageRevision,
		Mode:         build.ModeBuild,
	}
	for _, s := range unit.Sources {
		input.Sources = append(input.Sources, build.Source{Path: s.Path, Text: []byte(s.Text)})
	}
	module, err := req.Cache.Module(input, func() ([]build.File, error) {
		checked, refusals := a.lower()
		if len(refusals) > 0 {
			return nil, &BuildError{Diagnostics: refusals}
		}
		checked.File.Name = unit.Sources[0].Path
		code, err := proto.Emit(checked)
		if err != nil {
			return nil, fmt.Errorf("2026 emission failed: %w", err)
		}
		return []build.File{{Path: "go.mod", Data: []byte(proto.GoMod)}, {Path: "main.go", Data: []byte(code)}}, nil
	})
	if err != nil {
		return build.Module{}, err
	}
	absolute, err := filepath.Abs(req.Output)
	if err != nil {
		return module, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return module, err
	}
	child := req.Toolchain.Command(module, absolute)
	child.Stdout, child.Stderr = req.Stderr, req.Stderr
	if err := child.Run(); err != nil {
		return module, fmt.Errorf("Go build failed: %w", err)
	}
	return module, nil
}

// stdDependencies summarises the bundled standard library: the prelude and
// every importable package. Until package summaries exist (196b, #7), each
// summary is the conservative digest of the package's whole source, for
// its interface and its implementation alike.
func stdDependencies() []build.Dependency {
	deps := []build.Dependency{sourceDependency(std.PreludePath, std.Prelude())}
	for _, path := range std.Packages() {
		files, _ := std.Package(path)
		deps = append(deps, sourceDependency(path, files))
	}
	return deps
}

// sourceDependency summarises a source package by the digest of its file
// names and bytes, each length-prefixed so no two packages encode alike.
func sourceDependency(name string, files map[string]string) build.Dependency {
	names := make([]string, 0, len(files))
	for file := range files {
		names = append(names, file)
	}
	slices.Sort(names)
	hash := sha256.New()
	for _, file := range names {
		fmt.Fprintf(hash, "%d:%s%d:", len(file), file, len(files[file]))
		hash.Write([]byte(files[file]))
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	return build.Dependency{Name: name, Interface: digest, Implementation: digest}
}
