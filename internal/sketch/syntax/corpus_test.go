package syntax

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// corpusFiles returns the in-repo sketch-eval corpus (spec D7) split into
// the v0 sources (sketch-eval-8, sketch-eval-9) and the superseded rest.
func corpusFiles(t testing.TB) (v0, superseded map[string]string) {
	t.Helper()
	v0, superseded = map[string]string{}, map[string]string{}
	root := filepath.Join("testdata", "corpus")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		switch strings.SplitN(rel, "/", 2)[0] {
		case "sketch-eval-8", "sketch-eval-9":
			v0[rel] = string(data)
		default:
			superseded[rel] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(v0) != 25 || len(superseded) != 53 {
		t.Fatalf("corpus has %d v0 and %d superseded files, want 25 and 53", len(v0), len(superseded))
	}
	return v0, superseded
}

// repoInputs are the migrated examples and the 2026 example program: 2026
// sources outside the corpus that must parse.
func repoInputs(t testing.TB) map[string]string {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join("..", "..", "..", "docs", "research", "ef-migrate", "*.ef"))
	paths = append(paths, filepath.Join("..", "..", "..", "examples", "editions", "sketch", "main.ef"))
	inputs := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		inputs[filepath.ToSlash(path)] = string(data)
	}
	if len(inputs) != 6 {
		t.Fatalf("found %d repository inputs, want 6", len(inputs))
	}
	return inputs
}

func TestCorpusParses(t *testing.T) {
	v0, _ := corpusFiles(t)
	for name, src := range v0 {
		if _, err := Parse(src); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, src := range repoInputs(t) {
		if _, err := Parse(src); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

var enumType = regexp.MustCompile(`(?m)^type\s+\w+\s+enum\b`)

// TestSupersededCorpus: a pre-v0 file either parses or fails with a located
// S001, and a file with a top-level enum type fails at or before it.
func TestSupersededCorpus(t *testing.T) {
	_, superseded := corpusFiles(t)
	for name, src := range superseded {
		_, err := Parse(src)
		loc := enumType.FindStringIndex(src)
		if err == nil {
			if loc != nil {
				t.Errorf("%s: an enum type parsed", name)
			}
			continue
		}
		var syntaxErr *Error
		if !errors.As(err, &syntaxErr) || syntaxErr.Code != CodeSyntax || syntaxErr.Line < 1 {
			t.Errorf("%s: %v is not a located S001", name, err)
			continue
		}
		if loc != nil && syntaxErr.Offset > loc[1] {
			t.Errorf("%s: first error %v after the enum type at byte %d", name, err, loc[0])
		}
	}
}
