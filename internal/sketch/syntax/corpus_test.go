package syntax

import (
	"errors"
	"regexp"
	"testing"

	"ergo.local/prototype/internal/sketch/sketchtest"
)

// TestPositiveInputsParse: the v0 corpus, the migrated examples, the 2026
// example, every docs/spec go fence and the grammar fixtures parse.
func TestPositiveInputsParse(t *testing.T) {
	inputs, err := sketchtest.Positive()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		if _, err := Parse(in.Src); err != nil {
			t.Errorf("%s: %v", in.Name, err)
		}
	}
}

var enumType = regexp.MustCompile(`(?m)^type\s+\w+\s+enum\b`)

// TestSupersededCorpus: a pre-v0 file either parses or fails with a located
// S001, and a file with a top-level enum type fails at or before it.
func TestSupersededCorpus(t *testing.T) {
	_, superseded, err := sketchtest.Corpus()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range superseded {
		_, err := Parse(in.Src)
		loc := enumType.FindStringIndex(in.Src)
		if err == nil {
			if loc != nil {
				t.Errorf("%s: an enum type parsed", in.Name)
			}
			continue
		}
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeSyntax || e.Line < 1 {
			t.Errorf("%s: %v is not a located S001", in.Name, err)
			continue
		}
		if loc != nil && e.Offset > loc[1] {
			t.Errorf("%s: first error %v after the enum type at byte %d", in.Name, err, loc[0])
		}
	}
}
