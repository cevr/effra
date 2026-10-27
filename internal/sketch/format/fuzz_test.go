package format

import (
	"testing"

	"effra.local/prototype/internal/sketch/sketchtest"
	"effra.local/prototype/internal/sketch/syntax"
)

// FuzzFormat: for every input that parses, the output parses, formatting is
// idempotent, and comments are kept in order. The corpus, the spec fences
// and the fixture tables are the seeds.
func FuzzFormat(f *testing.F) {
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
	f.Fuzz(func(t *testing.T, src string) {
		if _, err := syntax.Parse(src); err != nil {
			return
		}
		out, err := Source(src)
		if err != nil {
			t.Fatalf("parsed but did not format: %v", err)
		}
		again, err := Source(out)
		if err != nil {
			t.Fatalf("the output does not parse: %v\n%s", err, out)
		}
		if again != out {
			t.Fatalf("not idempotent at line %d\n--- first\n%s\n--- second\n%s", firstDiffLine(out, again), out, again)
		}
		c1, ok1 := sketchtest.Comments(src)
		c2, ok2 := sketchtest.Comments(out)
		if ok1 && ok2 {
			if i, a, b := sketchtest.FirstDiff(c1, c2); i >= 0 {
				t.Fatalf("comment %d changed: %q became %q", i+1, a, b)
			}
		}
	})
}
