package sketchfront

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"ergo.local/prototype/internal/report"
)

// utf16Column counts the UTF-16 code units of text, independently of the
// report's position index.
func utf16Column(text string) int { return len(utf16.Encode([]rune(text))) }

func TestSketchCheckReportsPrototypeDiagnostics(t *testing.T) {
	cases := []struct {
		name, source, code, at, token string
	}{
		{
			// The lexer refuses `$` after non-ASCII text on its line.
			name:   "lexer S001",
			source: "package p\n\n// héllo 🌍\nfunc main() needs Console {\n\tConsole.Println(\"日本🌍\") $\n}\n",
			code:   "S001", at: "$", token: "$",
		},
		{
			// The parser refuses the second, stray `)` after non-ASCII text.
			name:   "parser S001",
			source: "package p\n\nfunc main() needs Console {\n\tConsole.Println(\"ü€\"))\n}\n",
			code:   "S001", at: ")\n}", token: ")",
		},
		{
			// The checker reports an unknown name after an astral character.
			name:   "checker S104",
			source: "package p\n\nfunc main() needs Console {\n\tConsole.Println(\"🌍🌍\" + mïssing)\n}\n",
			code:   "S104", at: "mïssing", token: "mïssing",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			analysis := Check(c.source)
			if len(analysis.Diagnostics) == 0 {
				t.Fatal("no diagnostics")
			}
			got := analysis.Diagnostics[0]
			want := strings.Index(c.source, c.at)
			if got.Code != c.code || got.Span.Offset != want || c.source[got.Span.Offset:got.Span.Offset+got.Span.Length] != c.token {
				t.Fatalf("diagnostic %+v, want %s at byte %d covering %q", got, c.code, want, c.token)
			}
			lineStart := strings.LastIndexByte(c.source[:want], '\n') + 1
			line := strings.Count(c.source[:want], "\n")
			if got.Span.Line != line+1 || got.Span.Column != want-lineStart+1 {
				t.Fatalf("span line %d column %d, want %d and byte column %d", got.Span.Line, got.Span.Column, line+1, want-lineStart+1)
			}
			// The UTF-16 column differs from the byte column on every case,
			// so a mapping that confused the units would fail.
			character := utf16Column(c.source[lineStart:want])
			if character == want-lineStart {
				t.Fatalf("case has no non-ASCII text before the error")
			}
			report := analysis.Result(c.source, report.Target).Report(report.SourceSnapshot{URI: "file:///x/main.ef", Text: c.source}, false)
			if report.Checked || len(report.Diagnostics) != len(analysis.Diagnostics) {
				t.Fatalf("report checked=%v with %d findings, want unchecked with %d", report.Checked, len(report.Diagnostics), len(analysis.Diagnostics))
			}
			finding := report.Diagnostics[0]
			if finding.Code != c.code || finding.LSP == nil || !finding.LocationAvailable {
				t.Fatalf("finding %+v has no located LSP projection", finding)
			}
			start, end := finding.LSP.Range.Start, finding.LSP.Range.End
			if start.Line != line || start.Character != character || end.Line != line || end.Character != character+utf16Column(c.token) {
				t.Fatalf("LSP range %+v, want line %d characters %d..%d", finding.LSP.Range, line, character, character+utf16Column(c.token))
			}
		})
	}
	t.Run("unrecognised error shape", func(t *testing.T) {
		got := syntaxDiagnostic(errors.New("something else: S001 broken"))
		if got.Code != "S001" || got.Message != "something else: S001 broken" || got.Span != (report.Span{}) {
			t.Fatalf("fallback %+v, want the whole text without a location", got)
		}
		projected := report.NewResult("x", report.Target, []report.Diagnostic{got}).Report(report.SourceSnapshot{Text: "x"}, false)
		if lsp := projected.Diagnostics[0].LSP; lsp == nil || lsp.Range != (report.DiagnosticRange{}) || lsp.Code != "S001" {
			t.Fatalf("a location-less finding projects %+v, want the document start", lsp)
		}
	})
	t.Run("clean source", func(t *testing.T) {
		source, err := os.ReadFile(filepath.Join("..", "..", "examples", "editions", "sketch", "main.ef"))
		if err != nil {
			t.Fatal(err)
		}
		analysis := Check(string(source))
		if len(analysis.Diagnostics) != 0 {
			t.Fatalf("the 2026 example has diagnostics: %+v", analysis.Diagnostics)
		}
		if result := analysis.Result(string(source), report.Target); !result.Checked {
			t.Fatal("a clean analysis is not checked")
		}
	})
}

// TestFormat: the 2026 formatter keeps the legacy result shape, reports a
// syntax error as a located S001 failure, and honours the output limit.
func TestFormat(t *testing.T) {
	source := "package p\n\nfunc main()  needs Console {\nConsole.Println(\"a\")\n}\n"
	want := "package p\n\nfunc main() needs Console {\n\tConsole.Println(\"a\")\n}\n"
	result, err := Format(source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != want || !result.Changed || result.SchemaVersion != report.FormatterSchemaVersion ||
		result.InputDigest != report.FormatDigest(source) || result.OutputDigest != report.FormatDigest(want) {
		t.Fatalf("result %+v", result)
	}
	again, err := Format(want, 0)
	if err != nil || again.Changed || again.Text != want {
		t.Fatalf("formatted text changed again: %+v, %v", again, err)
	}
	if FormatterIdentity != "ergo-2026/formatter-1" {
		t.Fatalf("identity %q", FormatterIdentity)
	}

	_, err = Format("package p\n\nfunc main() {\n\tx := )\n}\n", 0)
	var failure report.FormatFailure
	if !errors.As(err, &failure) || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != "S001" {
		t.Fatalf("syntax error gave %v", err)
	}
	if d := failure.Diagnostics[0]; d.Span.Line != 4 || d.Span.Column != 7 {
		t.Fatalf("span %+v, want 4:7", d.Span)
	}
	if msg := FailureMessage(failure); !strings.HasPrefix(msg, "source cannot be formatted: S001 at 4:7: ") {
		t.Fatalf("message %q", msg)
	}

	_, err = Format(source, 10)
	var limit report.FormatLimitError
	if !errors.As(err, &limit) || limit.Limit != 10 {
		t.Fatalf("limit gave %v", err)
	}
	if _, err := Format(source, -1); err == nil {
		t.Fatal("a negative limit was accepted")
	}
}

func TestSketchBuildRunsProgram(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "editions", "sketch")
	source, err := os.ReadFile(filepath.Join(dir, "main.ef"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join(dir, "expected.out"))
	if err != nil {
		t.Fatal(err)
	}
	analysis := Check(string(source))
	if len(analysis.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %+v", analysis.Diagnostics)
	}
	output := filepath.Join(t.TempDir(), "program")
	var stderr bytes.Buffer
	if err := Build(analysis, filepath.Join(dir, "main.ef"), output, &stderr); err != nil {
		t.Fatalf("build: %v\n%s", err, stderr.String())
	}
	got, err := exec.Command(output).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !bytes.Equal(got, expected) {
		t.Fatalf("stdout %q, want %q", got, expected)
	}
	// An analysis with diagnostics never builds.
	if err := Build(Check("package p\n$"), "bad.ef", filepath.Join(t.TempDir(), "bad"), &stderr); err == nil {
		t.Fatal("an analysis with diagnostics built")
	}
}
