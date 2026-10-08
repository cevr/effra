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
// and whether the analysis was complete. Built-in advice is outside it, so
// a fixture tests its packs, not the compiler's own rules.
type Expectation struct {
	Complete bool                      `json:"complete"`
	Rules    []lint.RuleStatus         `json:"rules"`
	Findings []compiler.LintDiagnostic `json:"findings"`
}

// ExpectationPath is the expectation file of a fixture: name.ef expects
// name.lint.json beside it.
func ExpectationPath(fixture string) string {
	return strings.TrimSuffix(fixture, ".ef") + ".lint.json"
}

// ErrUnchecked marks a fixture whose source does not check: pack rules
// over checked facts cannot run on it, so it cannot test them.
var ErrUnchecked = errors.New("fixture source does not check")

// Fixture lints one fixture file with the selected packs through the same
// path as ef lint: compile, run every pack as a process over the protocol,
// merge. The fixture is only read.
func (s *Session) Fixture(ctx context.Context, path, target string) (Expectation, error) {
	if !s.SelectsPacks() {
		return Expectation{}, fmt.Errorf("no rule pack is selected")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Expectation{}, err
	}
	source, err := sourcefile.ReadRegularFile(absolute, 0)
	if err != nil {
		return Expectation{}, err
	}
	uri, err := compiler.FileURI(absolute)
	if err != nil {
		return Expectation{}, err
	}
	snapshot := compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: string(source)}
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
	normal := Expectation{Complete: e.Complete, Rules: slices.Clone(e.Rules), Findings: slices.Clone(e.Findings)}
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
