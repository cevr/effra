package syntax

import (
	"errors"
	"strings"
	"testing"

	"effra.local/prototype/internal/sketch/sketchtest"
)

// FuzzParse: Parse never panics, and every error is a located S001 whose
// line and column agree with its byte offset. The corpus, the spec fences
// and the fixture tables are the seeds.
func FuzzParse(f *testing.F) {
	positive, err := sketchtest.Positive()
	if err != nil {
		f.Fatal(err)
	}
	_, superseded, err := sketchtest.Corpus()
	if err != nil {
		f.Fatal(err)
	}
	for _, in := range append(positive, superseded...) {
		f.Add(in.Src)
	}
	for _, m := range sketchtest.MalformedTable {
		if m.Generate == nil {
			f.Add(m.Src)
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
