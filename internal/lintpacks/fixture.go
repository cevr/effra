package lintpacks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"effra.local/prototype/internal/compiler"
	sourcefile "effra.local/prototype/internal/source"
	"effra.local/prototype/lint"
)

// Expectation is what a source fixture expects of the selected packs: the
// status of every pack rule, the pack findings and any lint-runner errors,
// and whether the analysis was complete, plus the status of every
// suppression naming a namespace/rule. Built-in advice and built-in
// suppressions are outside it, so a fixture tests its packs, not the
// compiler's own rules.
type Expectation struct {
	Complete     bool                         `json:"complete"`
	Rules        []lint.RuleStatus            `json:"rules"`
	Findings     []compiler.LintDiagnostic    `json:"findings"`
	Suppressions []compiler.SuppressionStatus `json:"suppressions,omitempty"`
}

// ExpectationPath is the expectation file of a fixture: name.ef expects
// name.lint.json beside it.
func ExpectationPath(fixture string) string {
	return strings.TrimSuffix(fixture, ".ef") + ".lint.json"
}

// ErrUnchecked marks a fixture whose source does not check: pack rules
// over checked facts cannot run on it, so it cannot test them.
var ErrUnchecked = errors.New("fixture source does not check")

// SourceReader reads an admitted source: the caller opened the file once
// and bound it to its edition, and Read returns the bytes of that open file
// from its start, without opening a path again. *edition.Source is one.
type SourceReader interface {
	Read(maxBytes int) ([]byte, error)
}

// pathReader reads a source by its path, for the path entry points.
type pathReader string

func (p pathReader) Read(maxBytes int) ([]byte, error) {
	return sourcefile.ReadRegularFile(string(p), maxBytes)
}

// Fixture lints one fixture file with the selected packs through the same
// path as ef lint: compile, run every pack as a process over the protocol,
// merge. The fixture is only read.
func (s *Session) Fixture(ctx context.Context, path, target string) (Expectation, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Expectation{}, err
	}
	return s.FixtureSource(ctx, absolute, pathReader(absolute), target)
}

// FixtureSource is Fixture for a source the caller has admitted: the bytes
// come from source, and path, made absolute, names the fixture for its URI
// and its imports. No path is read for the source.
func (s *Session) FixtureSource(ctx context.Context, path string, source SourceReader, target string) (Expectation, error) {
	if !s.SelectsPacks() {
		return Expectation{}, fmt.Errorf("no rule pack is selected")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Expectation{}, err
	}
	text, err := source.Read(0)
	if err != nil {
		return Expectation{}, err
	}
	uri, err := compiler.FileURI(absolute)
	if err != nil {
		return Expectation{}, err
	}
	snapshot := compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: string(text)}
	result := compiler.CompileAt(snapshot.Text, target, filepath.Dir(absolute))
	if !result.Checked {
		first := result.Diagnostics[0]
		return Expectation{}, fmt.Errorf("%w: %s at %d:%d: %s", ErrUnchecked, first.Code, first.Span.Line, first.Span.Column, first.Message)
	}
	packs := s.Run(ctx, result, snapshot)
	merged := result.LintWith(false, packs)
	expectation := Expectation{Complete: merged.Complete, Rules: []lint.RuleStatus{}, Findings: []compiler.LintDiagnostic{}}
	for _, pack := range packs.Reports {
		expectation.Rules = append(expectation.Rules, pack.Report.Rules...)
	}
	for _, diagnostic := range merged.LintDiagnostics {
		if strings.Contains(diagnostic.Rule, "/") || diagnostic.Rule == "lint-runner" {
			expectation.Findings = append(expectation.Findings, diagnostic)
		}
	}
	for _, suppression := range merged.Suppressions {
		if strings.Contains(suppression.Rule, "/") {
			expectation.Suppressions = append(expectation.Suppressions, suppression)
		}
	}
	return expectation, nil
}

// Encode is the canonical expectation file content.
func (e Expectation) Encode() []byte {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

// ParseExpectation reads an expectation file's content strictly: one JSON
// object with known fields, then only whitespace.
func ParseExpectation(data []byte) (Expectation, error) {
	var expectation Expectation
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&expectation); err != nil {
		return Expectation{}, fmt.Errorf("invalid expectation: %w", err)
	}
	// More reports false before a stray closing delimiter, so only the end
	// of input can follow the value.
	if _, err := decoder.Token(); err != io.EOF {
		return Expectation{}, fmt.Errorf("invalid expectation: data follows the expectation")
	}
	return expectation, nil
}

// Equal compares expectations by content, not by formatting.
func (e Expectation) Equal(other Expectation) bool {
	return reflect.DeepEqual(e.normal(), other.normal())
}

func (e Expectation) normal() Expectation {
	normal := Expectation{Complete: e.Complete, Rules: slices.Clone(e.Rules), Findings: slices.Clone(e.Findings), Suppressions: slices.Clone(e.Suppressions)}
	if len(normal.Suppressions) == 0 {
		normal.Suppressions = nil
	}
	if normal.Rules == nil {
		normal.Rules = []lint.RuleStatus{}
	}
	if normal.Findings == nil {
		normal.Findings = []compiler.LintDiagnostic{}
	}
	for i := range normal.Findings {
		if len(normal.Findings[i].Related) == 0 {
			normal.Findings[i].Related = nil
		}
		if len(normal.Findings[i].Suggestions) == 0 {
			normal.Findings[i].Suggestions = nil
		}
	}
	return normal
}

// WriteExpectation replaces an expectation file atomically.
func WriteExpectation(path string, expectation Expectation) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(expectation.Encode()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}
