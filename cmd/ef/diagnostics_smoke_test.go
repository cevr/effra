package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// Ported from scripts/diagnostics_smoke.py: public diagnostic snapshots,
// editor ranges and admission boundaries, compared across the CLI and the
// stdio MCP server. The Linux-only symlink replacement race lives in
// diagnostics_smoke_linux_test.go.

// diagnosticsSmokeRequests is one MCP session calling project.diagnostics
// once per argument, followed by a ping, each line json.dumps(message).
func diagnosticsSmokeRequests(t *testing.T, arguments []map[string]any) string {
	t.Helper()
	messages := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
			"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "diagnostics-smoke", "version": "1"}}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
	}
	for index, value := range arguments {
		messages = append(messages, map[string]any{"jsonrpc": "2.0", "id": index + 2, "method": "tools/call",
			"params": map[string]any{"name": "project.diagnostics", "arguments": value}})
	}
	messages = append(messages, map[string]any{"jsonrpc": "2.0", "id": "after", "method": "ping"})
	var input strings.Builder
	for _, message := range messages {
		input.Write(smokeDumps(t, message))
		input.WriteByte('\n')
	}
	return input.String()
}

// diagnosticsSmokeReplies decodes newline-delimited JSON-RPC replies.
func diagnosticsSmokeReplies(t *testing.T, stdout []byte) []map[string]any {
	t.Helper()
	var replies []map[string]any
	for _, line := range bytes.Split(bytes.TrimRight(stdout, "\n"), []byte("\n")) {
		replies = append(replies, smokeJSON(t, line))
	}
	return replies
}

// diagnosticsSmokeMCP runs one session rooted at directory and returns the
// tool replies, after checking the trailing ping proves the session drained.
func diagnosticsSmokeMCP(t *testing.T, binary, directory string, arguments ...map[string]any) []map[string]any {
	t.Helper()
	stdout, stderr, code := runTestCLIDir(t, binary, directory, diagnosticsSmokeRequests(t, arguments), "mcp", directory)
	if code != 0 {
		t.Fatalf("mcp exit %d: %s", code, stderr)
	}
	replies := diagnosticsSmokeReplies(t, stdout)
	if len(replies) != len(arguments)+2 {
		t.Fatalf("replies = %v", replies)
	}
	want := map[string]any{"jsonrpc": "2.0", "id": "after", "result": map[string]any{}}
	if last := replies[len(replies)-1]; !reflect.DeepEqual(last, want) {
		t.Fatalf("trailing ping = %v", last)
	}
	return replies[1 : len(replies)-1]
}

// diagnosticsSmokeStructured returns one tool reply's structured report.
func diagnosticsSmokeStructured(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	result, _ := reply["result"].(map[string]any)
	report, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("reply has no structured report: %v", reply)
	}
	return report
}

// diagnosticsSmokeRemote is one project.diagnostics report over MCP.
func diagnosticsSmokeRemote(t *testing.T, binary, directory string, arguments map[string]any) map[string]any {
	t.Helper()
	return diagnosticsSmokeStructured(t, diagnosticsSmokeMCP(t, binary, directory, arguments)[0])
}

// diagnosticsSmokeCLI runs `ef diagnostics --json`, whose exit status must
// state the report's policy verdict.
func diagnosticsSmokeCLI(t *testing.T, binary, path string, strict bool) map[string]any {
	t.Helper()
	arguments := []string{"diagnostics", path, "--json"}
	if strict {
		arguments = append(arguments, "--strict")
	}
	stdout, stderr, code := runTestCLIDir(t, binary, filepath.Dir(path), "", arguments...)
	if code != 0 && code != 1 {
		t.Fatalf("diagnostics exit %d: %s", code, stderr)
	}
	report := smokeJSON(t, stdout)
	if want := map[bool]int{true: 0, false: 1}[report["policyPassed"] == true]; code != want {
		t.Fatalf("exit %d does not state the policy verdict: %v", code, report)
	}
	return report
}

func diagnosticsSmokeWrite(t *testing.T, directory, name, source string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// diagnosticsSmokeURI is the file URI of an absolute path.
func diagnosticsSmokeURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

// diagnosticsSmokeEditorPosition is an independent protocol oracle: decode
// the known byte prefix, normalize editor line endings, then count UTF-16
// code units on its final line.
func diagnosticsSmokeEditorPosition(t *testing.T, source string, byteOffset int) map[string]any {
	t.Helper()
	prefix := source[:byteOffset]
	if !utf8.ValidString(prefix) {
		t.Fatalf("offset %d splits a character of %q", byteOffset, source)
	}
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(prefix, "\r\n", "\n"), "\r", "\n"), "\n")
	last := lines[len(lines)-1]
	return map[string]any{"line": float64(len(lines) - 1), "character": float64(len(utf16.Encode([]rune(last))))}
}

type diagnosticsSmokeFinding struct {
	code     string
	anchor   string // "" anchors at end of input
	length   int    // -1 extends to the first CRLF
	severity string
	numeric  int
}

// diagnosticsSmokeEpochControls proves the parity oracle checks the outer
// report and embedded snapshot epochs independently, and the producer
// qualifier. Each malformed report is compared with itself so parity
// inequality cannot make a missing epoch validator look like a refusal.
func diagnosticsSmokeEpochControls(t *testing.T, report, remote map[string]any) {
	t.Helper()
	options := parityOptions{reportSchema: 1, snapshotSchema: 8}
	assertReportParity(t, report, remote, options)
	rejected := func(label string, mutate func(map[string]any)) {
		t.Helper()
		malformed := deepCopyJSON(t, report)
		mutate(malformed)
		if checkReportParity(malformed, malformed, options) == nil {
			t.Fatalf("diagnostic report accepted malformed %s", label)
		}
	}
	snapshot := func(value map[string]any) map[string]any { return value["snapshot"].(map[string]any) }
	rejected("outer envelope epoch", func(value map[string]any) { value["schemaVersion"] = float64(8) })
	rejected("boolean outer envelope epoch", func(value map[string]any) { value["schemaVersion"] = true })
	rejected("semantic snapshot epoch", func(value map[string]any) { snapshot(value)["schemaVersion"] = float64(999) })
	rejected("boolean semantic snapshot epoch", func(value map[string]any) { snapshot(value)["schemaVersion"] = true })
	rejected("producer qualifier", func(value map[string]any) {
		producer := value["producer"].(map[string]any)
		switch producer["reuseScope"] {
		case "artifact":
			digest, _ := producer["digest"].(string)
			last := "0"
			if strings.HasSuffix(digest, "0") {
				last = "1"
			}
			producer["qualifier"] = digest[:len(digest)-1] + last
		case "process":
			producer["qualifier"] = "process:"
		default:
			producer["qualifier"] = "unexpected"
		}
	})
}

func TestDiagnosticsSmokePositions(t *testing.T) {
	binary := buildTestCLI(t)
	warning := "effect fn task() -> string { \"ok\" }\r\n" +
		"effect fn main() -> string { let s = \"𐐀e\u0301\"; " +
		"let forgotten = task(); run task().provide<Console>(Stdout) }\r\n"
	for index, fixture := range []struct {
		name     string
		source   string
		expected []diagnosticsSmokeFinding
	}{
		{"unicode.ef", "effect fn main() -> void { \"𐐀e\u0301\" @ }", []diagnosticsSmokeFinding{{"EF001", "@", 1, "error", 1}}},
		{"crlf.ef", "// comment\r\neffect fn main() -> void { void }\r\n@", []diagnosticsSmokeFinding{{"EF001", "@", 1, "error", 1}}},
		{"eof.ef", "effect fn main() -> void {\r\n", []diagnosticsSmokeFinding{{"EF002", "", 0, "error", 1}}},
		{"warning.ef", warning, []diagnosticsSmokeFinding{{"EFL001", "let forgotten", 3, "warning", 2}, {"EFL002", "provide", 7, "hint", 4}}},
		{"suppression.ef", "// effra-lint-disable-next-line bogus -- reason\r\neffect fn main() -> void { void }", []diagnosticsSmokeFinding{{"EFL004", "//", -1, "error", 1}}},
		{"cr-code.ef", "effect fn main() -> void { void }\r@", []diagnosticsSmokeFinding{{"EF001", "\r", 1, "error", 1}}},
		{"cr-comment.ef", "// comment\r@ effect fn main() -> void { void }\n", []diagnosticsSmokeFinding{{"EF001", "\r", 1, "error", 1}}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			source := fixture.source
			path := diagnosticsSmokeWrite(t, directory, fixture.name, source)
			report := diagnosticsSmokeCLI(t, binary, path, false)
			remote := diagnosticsSmokeRemote(t, binary, directory, map[string]any{"file": fixture.name})
			assertReportParity(t, report, remote, parityOptions{reportSchema: 1, snapshotSchema: 8})
			if index == 0 {
				// The epoch red controls run once, on the first fixture.
				diagnosticsSmokeEpochControls(t, report, remote)
			}
			digest := sha256.Sum256([]byte(source))
			if report["revision"] != hex.EncodeToString(digest[:]) {
				t.Fatalf("revision = %v", report["revision"])
			}
			if want := map[string]any{"uri": diagnosticsSmokeURI(path), "origin": "disk"}; !reflect.DeepEqual(report["source"], want) {
				t.Fatalf("source = %v, want %v", report["source"], want)
			}
			findings, _ := report["diagnostics"].([]any)
			if len(findings) != len(fixture.expected) {
				t.Fatalf("findings = %v", findings)
			}
			for index, expected := range fixture.expected {
				finding := findings[index].(map[string]any)
				offset := len(source)
				if expected.anchor != "" {
					offset = strings.Index(source, expected.anchor)
				}
				length := expected.length
				if length < 0 {
					length = strings.Index(source, "\r\n") - offset
				}
				origin := "compiler"
				if strings.HasPrefix(expected.code, "EFL") {
					origin = "lint"
				}
				if finding["code"] != expected.code || finding["severity"] != expected.severity || finding["origin"] != origin {
					t.Fatalf("finding %v, want %+v from %s", finding, expected, origin)
				}
				span, _ := finding["span"].(map[string]any)
				if span["offset"] != float64(offset) || span["length"] != float64(length) {
					t.Fatalf("span %v, want offset %d length %d", span, offset, length)
				}
				if finding["locationAvailable"] != true {
					t.Fatalf("location unavailable: %v", finding)
				}
				lsp, _ := finding["lsp"].(map[string]any)
				if lsp["severity"] != float64(expected.numeric) {
					t.Fatalf("LSP severity: %v", finding)
				}
				wantRange := map[string]any{
					"start": diagnosticsSmokeEditorPosition(t, source, offset),
					"end":   diagnosticsSmokeEditorPosition(t, source, offset+length),
				}
				if !reflect.DeepEqual(lsp["range"], wantRange) {
					t.Fatalf("LSP range = %v, want %v", lsp["range"], wantRange)
				}
			}
			if strings.HasPrefix(fixture.name, "cr-") {
				if message, _ := findings[0].(map[string]any)["message"].(string); !strings.Contains(message, "use LF or CRLF") {
					t.Fatalf("lone CR message: %v", findings)
				}
			}
			if fixture.name == "warning.ef" {
				strict := diagnosticsSmokeCLI(t, binary, path, true)
				if report["policyPassed"] != true || strict["policyPassed"] != false {
					t.Fatalf("strict policy: %v / %v", report, strict)
				}
				if !reflect.DeepEqual(report["diagnostics"], strict["diagnostics"]) {
					t.Fatalf("strict changed findings: %v", strict)
				}
				strictRemote := diagnosticsSmokeRemote(t, binary, directory, map[string]any{"file": fixture.name, "strict": true})
				assertReportParity(t, strictRemote, strict, parityOptions{reportSchema: 1, snapshotSchema: 8})
			}
		})
	}

	for _, fixture := range []struct {
		name    string
		source  string
		checked bool
	}{
		{"escaped-cr.ef", "effect fn main() -> string { \"\\r\" }", true},
		{"hidden-error.ef", "// effra-lint-disable-next-line bogus -- reason\n" +
			"effect fn main() -> void { run Console.log(\"x\") }", false},
		{"suppressed.ef", "effect fn task() -> string { \"ok\" }\n" +
			"effect fn main() -> string {\n" +
			"// effra-lint-disable-next-line unused-recipe -- intentional\n" +
			"let forgotten = task()\n\"ok\"\n}", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			report := diagnosticsSmokeCLI(t, binary, diagnosticsSmokeWrite(t, directory, fixture.name, fixture.source), true)
			remote := diagnosticsSmokeRemote(t, binary, directory, map[string]any{"file": fixture.name, "strict": true})
			assertReportParity(t, report, remote, parityOptions{reportSchema: 1, snapshotSchema: 8})
			if report["checked"] != fixture.checked {
				t.Fatalf("checked = %v: %v", report["checked"], report)
			}
			findings, isList := report["diagnostics"].([]any)
			if fixture.checked {
				if report["policyPassed"] != true || !isList || len(findings) != 0 {
					t.Fatalf("clean strict report: %v", report)
				}
				return
			}
			if reason, _ := report["lintUnavailableReason"].(string); report["lintAvailable"] != false || reason == "" {
				t.Fatalf("lint availability: %v", report)
			}
			sawUses := false
			for _, item := range findings {
				finding := item.(map[string]any)
				sawUses = sawUses || finding["code"] == "EF108"
				if finding["origin"] != "compiler" {
					t.Fatalf("non-compiler finding while lint is unavailable: %v", report)
				}
			}
			if !sawUses {
				t.Fatalf("hidden EF108 not reported: %v", report)
			}
		})
	}
}

func TestDiagnosticsSmokeBounds(t *testing.T) {
	binary := buildTestCLI(t)
	directory := t.TempDir()
	for _, count := range []int{100, 101} {
		diagnosticsSmokeWrite(t, directory, "findings-"+strconv.Itoa(count)+".ef", strings.Repeat("fn duplicate() -> void { void }\n", count+1))
	}
	const limit = 2 * 1024 * 1024
	for _, size := range []int{limit, limit + 1} {
		diagnosticsSmokeWrite(t, directory, "size-"+strconv.Itoa(size)+".ef", "//"+strings.Repeat("x", size-2))
	}
	if err := os.Mkdir(filepath.Join(directory, "directory.ef"), 0o755); err != nil {
		t.Fatal(err)
	}
	var arguments []map[string]any
	for _, name := range []string{"findings-100.ef", "findings-101.ef", "size-2097152.ef", "size-2097153.ef", "directory.ef"} {
		arguments = append(arguments, map[string]any{"file": name})
	}
	if diagnosticsSmokeMkfifo(filepath.Join(directory, "fifo.ef")) == nil {
		arguments = append(arguments, map[string]any{"file": "fifo.ef"})
	}
	replies := diagnosticsSmokeMCP(t, binary, directory, arguments...)
	report := diagnosticsSmokeStructured(t, replies[0])
	totals, _ := report["totalCounts"].(map[string]any)
	if report["returnedCount"] != float64(100) || totals["errors"] != float64(100) {
		t.Fatalf("exact finding limit: %v", report)
	}
	if report["policyPassed"] != false || report["truncated"] != false {
		t.Fatalf("exact-limit result was incomplete or passed policy: %v", report)
	}
	overflow, _ := replies[1]["result"].(map[string]any)
	content, _ := overflow["content"].([]any)
	if overflow["isError"] != true || len(content) == 0 || !strings.Contains(content[0].(map[string]any)["text"].(string), "limit") {
		t.Fatalf("101 findings were not rejected: %v", replies[1])
	}
	if exact := diagnosticsSmokeStructured(t, replies[2]); exact["returnedCount"] != float64(0) {
		t.Fatalf("exact source size was rejected or fabricated a finding: %v", replies[2])
	}
	for index, reply := range replies[3:] {
		if result, _ := reply["result"].(map[string]any); result["isError"] != true {
			t.Fatalf("source admission accepted %v: %v", arguments[3+index]["file"], reply)
		}
	}
}

func TestDiagnosticsSmokeIdentity(t *testing.T) {
	binary := buildTestCLI(t)
	t.Run("lexical-normalization", func(t *testing.T) {
		t.Parallel()
		// Lexical normalization must govern both the document identity and its bytes.
		directory := t.TempDir()
		dots := filepath.Join(directory, "dots")
		if err := os.MkdirAll(filepath.Join(dots, "real", "inner"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("real", "inner"), filepath.Join(dots, "link")); err != nil {
			t.Fatal(err)
		}
		lexical := diagnosticsSmokeWrite(t, dots, "b.ef", "effect fn main() -> void { void }")
		diagnosticsSmokeWrite(t, filepath.Join(dots, "real"), "b.ef", "effect fn main() -> void { run Console.log(\"x\") }")
		expected := diagnosticsSmokeCLI(t, binary, lexical, false)
		// Join would clean the path; the request must keep link/.. literally.
		requested := dots + string(filepath.Separator) + "link" + string(filepath.Separator) + ".." + string(filepath.Separator) + "b.ef"
		for _, report := range []map[string]any{
			diagnosticsSmokeCLI(t, binary, requested, false),
			diagnosticsSmokeRemote(t, binary, directory, map[string]any{"file": "dots/link/../b.ef"}),
		} {
			assertReportParity(t, report, expected, parityOptions{target: "go", reportSchema: 1, snapshotSchema: 8})
		}
	})
	t.Run("symlink-uri", func(t *testing.T) {
		t.Parallel()
		directory := t.TempDir()
		link, baseline := diagnosticsSmokeSymlinkFixture(t, binary, directory, "selected.ef")
		for _, report := range []map[string]any{
			diagnosticsSmokeCLI(t, binary, link, false),
			diagnosticsSmokeRemote(t, binary, directory, map[string]any{"file": filepath.Base(link)}),
		} {
			diagnosticsSmokeAssertSelected(t, report, link, baseline)
		}
	})
}

// diagnosticsSmokeSymlinkFixture writes one.ef (a Go import, so checking it
// spawns the Go toolchain), two.ef and the named link -> one.ef, and returns
// the link and one.ef's own report.
func diagnosticsSmokeSymlinkFixture(t *testing.T, binary, directory, name string) (string, map[string]any) {
	t.Helper()
	original := diagnosticsSmokeWrite(t, directory, "one.ef", "import go fmt \"fmt\"\neffect fn main() -> void { void }")
	diagnosticsSmokeWrite(t, directory, "two.ef", "effect fn main() -> void { run Console.log(\"x\") }")
	link := filepath.Join(directory, name)
	if err := os.Symlink(filepath.Base(original), link); err != nil {
		t.Fatal(err)
	}
	return link, diagnosticsSmokeCLI(t, binary, original, false)
}

// diagnosticsSmokeAssertSelected checks a report names the requested link
// while carrying the bytes and findings of the baseline document.
func diagnosticsSmokeAssertSelected(t *testing.T, report map[string]any, link string, baseline map[string]any) {
	t.Helper()
	if source, _ := report["source"].(map[string]any); source["uri"] != diagnosticsSmokeURI(link) {
		t.Fatalf("source = %v, want %s", report["source"], diagnosticsSmokeURI(link))
	}
	if report["revision"] != baseline["revision"] || !reflect.DeepEqual(report["diagnostics"], baseline["diagnostics"]) {
		t.Fatalf("report %v does not match baseline %v", report, baseline)
	}
}
