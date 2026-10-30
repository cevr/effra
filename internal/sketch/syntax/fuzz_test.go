package syntax

import (
	"errors"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/sketchtest"
)

// FuzzParse: the token stream keeps its invariant (checkStream), Parse
// never panics, and every error is a located S001 (or the parser's S108
// for a type that mixes fields and operations) whose line and column agree
// with its byte offset. The corpus, the spec fences, the fixture tables
// and the lookahead cases of #226 are the seeds; testdata/fuzz/FuzzParse
// holds the FuzzCheck crashers as regressions.
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
	for _, c := range lexErrorCases {
		f.Add(c.src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if broken := checkStream(src, scan(src).toks); broken != "" {
			t.Fatal(broken)
		}
		file, err := Parse(src)
		if err == nil {
			if file == nil {
				t.Fatal("no file and no error")
			}
			return
		}
		var e *Error
		if !errors.As(err, &e) || (e.Code != CodeSyntax && e.Code != CodeMixedMembers) || file != nil {
			t.Fatalf("error %v (%T) is not a lone S001 or S108", err, err)
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
