package lint

import (
	"strings"
	"testing"
)

// With the source text supplied, the runner accepts a finding only at a
// real character position whose line and byte column are the ones its
// offset has, so every projection of the finding names the same place.
func TestFindingPositionsMatchTheSourceText(t *testing.T) {
	// é is 2 bytes and 𝄞 is 4 (2 UTF-16 units): "c" is byte column 7,
	// rune column 5 and UTF-16 column 6 of line 2.
	const text = "é\nab𝄞c"
	at := func(offset, length, line, column int) Span {
		return Span{Offset: offset, Length: length, Line: line, Column: column}
	}
	accepted := []Span{at(0, 2, 1, 1), at(2, 0, 1, 3), at(3, 1, 2, 1), at(5, 4, 2, 3), at(9, 1, 2, 7), at(10, 0, 2, 8)}
	refused := []struct {
		span Span
		want string
	}{
		{at(1, 1, 1, 2), "UTF-8 character boundaries"},
		{at(5, 2, 2, 3), "UTF-8 character boundaries"},
		{at(3, 1, 1, 4), "is at line 2, column 1"},
		{at(9, 1, 2, 5), "is at line 2, column 7"},
		{at(9, 1, 2, 6), "is at line 2, column 7"},
		{at(11, 0, 2, 9), "outside the 10-byte source"},
	}
	evaluate := func(source Source, finding Finding) RuleStatus {
		t.Helper()
		pack := testPack(func(pass *Pass) error { pass.Report(finding); return nil })
		registry, err := NewRegistry(testBuiltins, testManifest(t, pack))
		if err != nil {
			t.Fatal(err)
		}
		snapshot := testSnapshot()
		snapshot.Source = source
		report, err := Evaluate(pack, configure(t, registry, `{"version":1}`), snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return report.Rules[0]
	}
	source := Source{Bytes: len(text), Text: text}
	for _, span := range accepted {
		if status := evaluate(source, Finding{Message: "m", Span: span}); status.Status != StatusCompleted {
			t.Fatalf("%+v refused: %s", span, status.Reason)
		}
	}
	for _, c := range refused {
		findings := map[string]Finding{
			"finding":    {Message: "m", Span: c.span},
			"related":    {Message: "m", Span: accepted[0], Related: []Related{{Message: "r", Span: c.span}}},
			"suggestion": {Message: "m", Span: accepted[0], Suggestions: []Suggestion{{Message: "s", Edits: []Edit{{Span: c.span, NewText: "x"}}}}},
		}
		for name, finding := range findings {
			if status := evaluate(source, finding); status.Status != StatusFailed || !strings.Contains(status.Reason, c.want) {
				t.Fatalf("%s %+v: %+v, want %q", name, c.span, status, c.want)
			}
		}
	}
	// Without the text only the byte bound holds; with text of another
	// length every span is refused.
	if status := evaluate(Source{Bytes: len(text)}, Finding{Message: "m", Span: at(1, 1, 1, 2)}); status.Status != StatusCompleted {
		t.Fatalf("bounds only: %+v", status)
	}
	if status := evaluate(Source{Bytes: len(text), Text: text + " "}, Finding{Message: "m", Span: accepted[0]}); status.Status != StatusFailed || !strings.Contains(status.Reason, "source text has 11 bytes") {
		t.Fatalf("mismatched text: %+v", status)
	}
}
