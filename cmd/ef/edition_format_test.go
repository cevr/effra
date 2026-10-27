package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/sketchfront"
)

// sketchUnformatted formats to sketchCleanSource.
const sketchUnformatted = "package p\n\n\n\nfunc main()  uses Console {\n        Console.Println(\"ok\")\n}\n"

// TestFmtFormatsEachFileWithItsEditionsFormatter: one ef fmt request with a
// 2026 file and a legacy file formats each with its own edition's
// formatter, names that formatter per file, and writes both in place; the
// three surfaces agree on the 2026 text; a 2026 syntax error is EFMT_SYNTAX
// naming S001; --stdin stays legacy.
func TestFmtFormatsEachFileWithItsEditionsFormatter(t *testing.T) {
	binary := buildTestCLI(t)
	root := editionWorkspace(t)
	sketch := filepath.Join(root, "new", "main.ef")
	legacy := filepath.Join(root, "old", "main.ef")
	malformed := filepath.Join(root, "new", "bad.ef")
	editionWrite(t, filepath.Join(root, "new", "effra.json"), `{"edition":"2026"}`)
	editionWrite(t, sketch, sketchUnformatted)
	legacyUnformatted := strings.ReplaceAll(legacyCleanSource, "    ", "  ")
	editionWrite(t, legacy, legacyUnformatted)
	malformedSource := "package p\n\nfunc main() {\n\tx := )\n}\n"
	editionWrite(t, malformed, malformedSource)

	// Check mode: both files change, each under its own formatter.
	stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", "fmt", "--check", "--json", "new/main.ef", "old/main.ef")
	if code != 1 {
		t.Fatalf("ef fmt --check: exit %d, want 1\n%s\n%s", code, stdout, stderr)
	}
	var report formatReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("report %s: %v", stdout, err)
	}
	if report.FormatterVersion != compiler.FormatterIdentity || len(report.Files) != 2 {
		t.Fatalf("report %+v", report)
	}
	for index, want := range []string{sketchfront.FormatterIdentity, compiler.FormatterIdentity} {
		if file := report.Files[index]; file.FormatterVersion != want || !file.Changed || file.Written {
			t.Fatalf("file %d: %+v, want formatter %s, changed, not written", index, file, want)
		}
	}
	if report.Files[0].OutputDigest != compiler.FormatDigest(sketchCleanSource) {
		t.Fatalf("2026 output digest %s, want the digest of the canonical text", report.Files[0].OutputDigest)
	}

	// MCP and the LSP give the same 2026 text before anything is written.
	responses := mcpSession(t, binary, root, []map[string]any{{"name": "code.format", "arguments": map[string]any{"file": "new/main.ef"}}})
	var tool struct {
		Structured struct {
			Text             string `json:"text"`
			FormatterVersion string `json:"formatterVersion"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(responses[1]["result"], &tool); err != nil || tool.Structured.Text != sketchCleanSource || tool.Structured.FormatterVersion != sketchfront.FormatterIdentity {
		t.Fatalf("MCP code.format answered %s", responses[1]["result"])
	}
	uri := editionURI(sketch)
	messages := lspSession(t, binary, root, []map[string]any{
		lspOpen(uri, sketchUnformatted),
		lspSmokeCall("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 4, "insertSpaces": true}}, "format"),
	})
	var edits []struct {
		NewText string `json:"newText"`
	}
	if reply := lspReply(t, messages, "format"); json.Unmarshal(reply["result"], &edits) != nil || len(edits) != 1 || edits[0].NewText != sketchCleanSource {
		t.Fatalf("LSP formatting answered %s %s", reply["result"], reply["error"])
	}

	// Write mode writes both files with their own formatter's text.
	if stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", "fmt", "new/main.ef", "old/main.ef"); code != 0 {
		t.Fatalf("ef fmt: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if after, err := os.ReadFile(sketch); err != nil || string(after) != sketchCleanSource {
		t.Fatalf("the 2026 file is %q (%v)", after, err)
	}
	legacyFormatted, err := compiler.FormatSource(legacyUnformatted)
	if err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(legacy); err != nil || string(after) != legacyFormatted.Text {
		t.Fatalf("the legacy file is %q (%v)", after, err)
	}

	// A 2026 syntax error: EFMT_SYNTAX naming S001 and its position, nothing
	// written.
	_, stderr, code = runSmokeCLI(t, editionDeadline, binary, root, "", "fmt", "new/bad.ef")
	if code != 2 || !strings.Contains(string(stderr), "EFMT_SYNTAX") || !strings.Contains(string(stderr), "source cannot be formatted: S001 at 4:7: ") {
		t.Fatalf("ef fmt of a malformed 2026 file: exit %d, stderr %q", code, stderr)
	}
	stdout, _, _ = runSmokeCLI(t, editionDeadline, binary, root, "", "fmt", "--json", "new/bad.ef")
	report = formatReport{}
	if err := json.Unmarshal(stdout, &report); err != nil || len(report.Failures) != 1 || report.Failures[0].Code != "EFMT_SYNTAX" ||
		len(report.Failures[0].Diagnostics) != 1 || report.Failures[0].Diagnostics[0].Code != "S001" {
		t.Fatalf("ef fmt --json of a malformed 2026 file: %s", stdout)
	}
	if after, err := os.ReadFile(malformed); err != nil || string(after) != malformedSource {
		t.Fatal("the malformed file changed")
	}

	// --stdin has no directory and stays legacy: it refuses 2026 text.
	if stdout, _, code := runSmokeCLI(t, editionDeadline, binary, root, sketchUnformatted, "fmt", "--stdin"); code != 2 || len(stdout) != 0 {
		t.Fatalf("ef fmt --stdin of 2026 text: exit %d, stdout %q", code, stdout)
	}
}

// TestFmtFormatsAMixedVarGroupUnderTheDefaultBound (review r2 M1): a var
// group whose long type is followed by an inferred declaration and 2,000
// short typed ones formats under the default 4 MiB bound. Before the fix
// the empty type cell carried the long type's width to every short line,
// and the 39,225-byte file needed 8,232,442 bytes of output.
func TestFmtFormatsAMixedVarGroupUnderTheDefaultBound(t *testing.T) {
	binary := buildTestCLI(t)
	root := editionWorkspace(t)
	long := strings.Repeat("L", 4096)
	var b strings.Builder
	b.WriteString("package p\ntype " + long + " struct{}\nvar (\na " + long + " = " + long + "{}\nb = 2\n")
	for i := range 2000 {
		fmt.Fprintf(&b, "c%d int = 3\n", i)
	}
	b.WriteString(")\n")
	source := b.String()
	editionWrite(t, filepath.Join(root, "new", "effra.json"), `{"edition":"2026"}`)
	editionWrite(t, filepath.Join(root, "new", "alignment-empty.ef"), source)
	stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", "fmt", "--json", "new/alignment-empty.ef")
	if code != 0 {
		t.Fatalf("ef fmt: exit %d\n%s\n%s", code, firstLines(stdout), stderr)
	}
	after, err := os.ReadFile(filepath.Join(root, "new", "alignment-empty.ef"))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(source)+len(source)/5 {
		t.Fatalf("a %d-byte source formatted to %d bytes", len(source), len(after))
	}
}

// TestFmtFormatsATaperedStructUnderTheDefaultBound (review r3 M1): a struct
// whose first field name is 4,096 wide, whose next names taper to 40 (each
// half the geometric mean of the names before it) and which ends with
// 16,384 short names formats under the default 4 MiB bound. Before the
// section bound, the 225,682-byte file needed 69,090,014 bytes of output.
func TestFmtFormatsATaperedStructUnderTheDefaultBound(t *testing.T) {
	binary := buildTestCLI(t)
	root := editionWorkspace(t)
	var b strings.Builder
	b.WriteString("package p\ntype T struct {\n")
	lnsum, count := 0.0, 0
	for w := 4096; ; {
		name := fmt.Sprintf("p%d", count)
		fmt.Fprintf(&b, "%s%s int\n", name, strings.Repeat("a", w-len(name)))
		lnsum += math.Log(float64(w))
		count++
		if w == 40 {
			break
		}
		w = max(40, int(math.Ceil(math.Exp(lnsum/float64(count))/2)))
	}
	for i := range 4 * 4096 {
		fmt.Fprintf(&b, "f%d int\n", i)
	}
	b.WriteString("}\n")
	source := b.String()
	if len(source) != 225682 {
		t.Fatalf("the generated taper is %d bytes, want the reviewer's 225682", len(source))
	}
	editionWrite(t, filepath.Join(root, "new", "effra.json"), `{"edition":"2026"}`)
	editionWrite(t, filepath.Join(root, "new", "alignment-taper.ef"), source)
	stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", "fmt", "--json", "new/alignment-taper.ef")
	if code != 0 {
		t.Fatalf("ef fmt: exit %d\n%s\n%s", code, firstLines(stdout), stderr)
	}
	after, err := os.ReadFile(filepath.Join(root, "new", "alignment-taper.ef"))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > 2*len(source) {
		t.Fatalf("a %d-byte source formatted to %d bytes", len(source), len(after))
	}
}

func firstLines(b []byte) string {
	if len(b) > 400 {
		return string(b[:400]) + "…"
	}
	return string(b)
}
