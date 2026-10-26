package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestEncodingMatchesPythonJSON compares both encoders with vectors that
// Python wrote: testdata/pyjson-vectors.json holds an input document and
// json.dumps(json.loads(input), ensure_ascii=False, sort_keys=True,
// separators=(",", ":")) and json.dumps(..., indent=2, sort_keys=True). The
// input covers escapes, control characters, DEL, non-ASCII, astral planes,
// U+2028, integer and float forms and duplicate keys.
func TestEncodingMatchesPythonJSON(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "pyjson-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct{ Input, Compact, Indented string }
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	value, err := decodeJSON(vectors.Input)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(canonicalJSON(value)); got != vectors.Compact {
		t.Errorf("canonical encoding\n got %q\nwant %q", got, vectors.Compact)
	}
	if got := indentedJSON(value); got != vectors.Indented {
		t.Errorf("indented encoding\n got %q\nwant %q", got, vectors.Indented)
	}
}

// TestPythonTextHelpers pins the helpers against values Python printed.
func TestPythonTextHelpers(t *testing.T) {
	t.Parallel()
	for _, check := range []struct {
		value any
		want  string
	}{
		{"tab\there\x7f é\U0001F600'", `"tab\there\x7f\xa0é😀'"`},
		{[]any{"x", nil, true, json.Number("1.5")}, `['x', None, True, 1.5]`},
		{`a"b'c`, `'a"b\'c'`},
		{"it's", `"it's"`},
	} {
		if got := pyRepr(check.value); got != check.want {
			t.Errorf("repr = %s, want %s", got, check.want)
		}
	}
	if got := pyStrip(" \x1f　 x "); got != "x" {
		t.Errorf("strip = %q", got)
	}
	if _, err := safeRelativePath("a/./b"); err != nil {
		t.Errorf("pathlib collapses '.' segments, so a/./b is safe: %v", err)
	}
	for _, unsafe := range []any{"../x", "a/../b", "/abs", "", "a\\b", "a\nb", json.Number("3"), nil} {
		if _, err := safeRelativePath(unsafe); err == nil {
			t.Errorf("unsafe path %s accepted", pyRepr(unsafe))
		}
	}
}
