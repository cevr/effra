package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// TestCommittedManifestCanonicalIdentity recomputes the Python-produced
// integrity block and rendering of the committed manifest without git.
func TestCommittedManifestCanonicalIdentity(t *testing.T) {
	t.Parallel()
	path := filepath.Join(repoRoot(t), manifestRelative)
	text, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := readJSONObject(t, path)
	if got := renderManifest(manifest); got != text {
		t.Fatal("rendering the committed manifest does not reproduce its bytes")
	}
	integrity := integrityForManifest(manifest)
	if _, differs := firstDifference(integrity, object(manifest, "integrity"), ""); differs {
		t.Fatalf("recomputed integrity %v differs from committed %v", integrity, manifest["integrity"])
	}
	if integrity["rootSha256"] != releaseRootIdentitySHA256 || integrity["referenceIdentitySha256"] != releaseReferenceIdentitySHA256 || integrity["licenseIdentitySha256"] != releaseLicenseIdentitySHA256 {
		t.Fatalf("recomputed integrity %v is not the pinned release identity", integrity)
	}
	if err := validateReleaseIdentity(manifest); err != nil {
		t.Fatal(err)
	}
}

// TestPythonTextHelpers pins the helpers against values Python printed.
func TestPythonTextHelpers(t *testing.T) {
	t.Parallel()
	if got, want := pySplitlines("a\x1cb\u0085c d\r\ne\rf\vg\fh\n\n"), []string{"a", "b", "c", "d", "e", "f", "g", "h", ""}; !slices.Equal(got, want) {
		t.Errorf("splitlines = %q, want %q", got, want)
	}
	if got, want := shlexJoin("/tmp/a b", "it's", "", "ok-path/x.sh", "--force", `a\$b`), `'/tmp/a b' 'it'"'"'s' '' ok-path/x.sh --force 'a\$b'`; got != want {
		t.Errorf("shlex.join = %s, want %s", got, want)
	}
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
	if !anchorMatches("\t   it.effect(\"label\", () =>", "label") || anchorMatches("  it.only(\"label\",", "label") || anchorMatches("it(\"label\")", "label") {
		t.Error("anchor matching differs from the Python pattern")
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
