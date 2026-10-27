package syntax

import (
	"errors"
	"strings"
	"testing"
)

// FuzzParse: Parse never panics, and every error is a located S001 whose
// line and column agree with its byte offset. The corpus, the spec fences
// and the fixture tables are the seeds.
func FuzzParse(f *testing.F) {
	v0, superseded := corpusFiles(f)
	for _, src := range v0 {
		f.Add(src)
	}
	for _, src := range superseded {
		f.Add(src)
	}
	for _, src := range specFences(f) {
		f.Add(src)
	}
	for _, g := range grammarFixtures {
		f.Add(g.src)
	}
	for _, m := range malformedTable {
		if m.generate == nil {
			f.Add(m.src)
		}
	}
	f.Fuzz(func(t *testing.T, src string) {
		file, err := Parse(src)
		if err == nil {
			if file == nil {
				t.Fatal("no file and no error")
			}
			return
		}
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeSyntax || file != nil {
			t.Fatalf("error %v (%T) is not a lone S001", err, err)
		}
		if e.Offset < 0 || e.Length < 0 || e.Offset+e.Length > len(src) {
			t.Fatalf("span %d+%d outside the %d-byte source", e.Offset, e.Length, len(src))
		}
		line := strings.Count(src[:e.Offset], "\n") + 1
		column := e.Offset - (strings.LastIndexByte(src[:e.Offset], '\n') + 1) + 1
		if line != e.Line || column != e.Column {
			t.Fatalf("%d:%d, want %d:%d", e.Line, e.Column, line, column)
		}
	})
}
