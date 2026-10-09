package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// i64TextVectors are decimal conversion cases with their portable outcome:
// the canonical formatted value, or the failure message. The grammar is
// Go's base-10 strconv grammar, [+-]?[0-9]+, with syntax classified before
// range.
var i64TextVectors = []struct{ text, formatted, failure string }{
	{"0", "0", ""},
	{"-0", "0", ""},
	{"+0", "0", ""},
	{"007", "7", ""},
	{"-42", "-42", ""},
	{"+42", "42", ""},
	{"9223372036854775807", "9223372036854775807", ""},
	{"-9223372036854775808", "-9223372036854775808", ""},
	{"000000000000000000000009223372036854775807", "9223372036854775807", ""},
	{"9223372036854775808", "", "i64 out of range"},
	{"-9223372036854775809", "", "i64 out of range"},
	{"99999999999999999999999999", "", "i64 out of range"},
	// Go's ParseInt reports ErrRange here because its digit loop overflows
	// before it reaches the x; Effra classifies syntax first on both targets.
	{"99999999999999999999x", "", "invalid i64 syntax"},
	{"", "", "invalid i64 syntax"},
	{"+", "", "invalid i64 syntax"},
	{"-", "", "invalid i64 syntax"},
	{"+-1", "", "invalid i64 syntax"},
	{" 1", "", "invalid i64 syntax"},
	{"1 ", "", "invalid i64 syntax"},
	{"0x10", "", "invalid i64 syntax"},
	{"1_000", "", "invalid i64 syntax"},
	{"1e3", "", "invalid i64 syntax"},
	{"١", "", "invalid i64 syntax"},
}

func TestI64TextConversionsMatchAcrossGoAndJS(t *testing.T) {
	if _, err := strconv.ParseInt("99999999999999999999x", 10, 64); err == nil || !strings.Contains(err.Error(), "value out of range") {
		t.Fatalf("Go scan-order control changed: %v", err)
	}
	binary := buildTestCLI(t)
	root := t.TempDir()
	modules, err := filepath.Abs("../../node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	source.WriteString("import I64 \"effra/i64\"\n")
	for index, vector := range i64TextVectors {
		text, _ := json.Marshal(vector.text)
		expected := vector.formatted
		if expected == "" {
			expected = "unreachable"
		}
		fmt.Fprintf(&source, "effect fn test_parse_%d() -> void raises { AssertionFailed, I64ParseFailure } uses { Assert } {\n    let value = run I64.parse(%s)\n    run Assert.equalText(I64.format(value), %q)\n}\n", index, text, expected)
	}
	for index, value := range []string{"-9223372036854775808", "9223372036854775807", "0", "-1", "42"} {
		fmt.Fprintf(&source, "effect fn test_format_%d() -> void raises { AssertionFailed } uses { Assert } {\n    run Assert.equalText(I64.format(%s), %q)\n}\n", index, value, value)
	}
	path := filepath.Join(root, "i64text.ef")
	if err := os.WriteFile(path, []byte(source.String()), 0600); err != nil {
		t.Fatal(err)
	}
	type reason struct {
		Kind    string `json:"kind"`
		Tag     string `json:"tag"`
		Message string `json:"message"`
	}
	type outcome struct {
		Name    string   `json:"name"`
		Passed  bool     `json:"passed"`
		Reasons []reason `json:"reasons"`
	}
	reports := map[string]map[string]outcome{}
	for _, target := range []string{"go", "js"} {
		stdout, stderr, _ := runTestCLIDir(t, binary, root, "", "test", path, "--target", target)
		var report struct {
			Tests []outcome `json:"tests"`
		}
		if err := json.Unmarshal(stdout, &report); err != nil || len(report.Tests) != len(i64TextVectors)+5 {
			t.Fatalf("%s report: %v (%d tests)\nstdout=%s\nstderr=%s", target, err, len(report.Tests), stdout, stderr)
		}
		reports[target] = map[string]outcome{}
		for _, test := range report.Tests {
			reports[target][test.Name] = test
		}
	}
	if !reflect.DeepEqual(reports["go"], reports["js"]) {
		t.Fatalf("Go and JS conversion outcomes differ:\n%+v\n%+v", reports["go"], reports["js"])
	}
	for index, vector := range i64TextVectors {
		got := reports["go"][fmt.Sprintf("test_parse_%d", index)]
		if vector.failure == "" {
			if !got.Passed {
				t.Errorf("parse %q: want %s, got %+v", vector.text, vector.formatted, got)
			}
			continue
		}
		if want := []reason{{Kind: "failure", Tag: "I64ParseFailure", Message: vector.failure}}; got.Passed || !reflect.DeepEqual(got.Reasons, want) {
			t.Errorf("parse %q: want %+v, got %+v", vector.text, want, got)
		}
	}
	for index := range 5 {
		if got := reports["go"][fmt.Sprintf("test_format_%d", index)]; !got.Passed {
			t.Errorf("format case %d failed: %+v", index, got)
		}
	}
}
