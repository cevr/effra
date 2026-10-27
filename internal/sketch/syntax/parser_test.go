package syntax

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestGrammarFixturesParse(t *testing.T) {
	for _, f := range grammarFixtures {
		if _, err := Parse(f.src); err != nil {
			t.Errorf("%s: %v", f.name, err)
		}
	}
}

// TestMalformed: every malformed input is a located S001 on its line, never
// a passthrough.
func TestMalformed(t *testing.T) {
	for _, m := range malformedTable {
		t.Run(m.name, func(t *testing.T) {
			src := m.src
			if m.generate != nil {
				src = m.generate()
			}
			file, err := Parse(src)
			if file != nil || err == nil {
				t.Fatal("parsed")
			}
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("%T is not *Error", err)
			}
			if e.Code != CodeSyntax || !slices.Contains(m.lines, e.Line) {
				t.Fatalf("%v: want S001 on line %v", e, m.lines)
			}
			// The span is inside the source and its line and column agree.
			if e.Offset < 0 || e.Offset+e.Length > len(src) {
				t.Fatalf("span %d+%d outside the %d-byte source", e.Offset, e.Length, len(src))
			}
			line := strings.Count(src[:e.Offset], "\n") + 1
			column := e.Offset - (strings.LastIndexByte(src[:e.Offset], '\n') + 1) + 1
			if line != e.Line || column != e.Column {
				t.Fatalf("line %d column %d, want %d and %d from the offset", e.Line, e.Column, line, column)
			}
		})
	}
}
