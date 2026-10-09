package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Ported from scripts/format_smoke.py: the compiled `ef fmt` CLI and the MCP
// code.format tool share formatter output, invocation and limit contracts,
// and the stdio MCP server keeps its frame, response and identifier bounds.
// Mixed-syntax atomicity, symlink refusal, invalid stdin UTF-8 and the
// expanded stdin output limit are asserted by TestFormatCLIProcessModesAndAtomicWrites.

const (
	formatSmokeSource     = "import go missing \"example.invalid/no-such-package\"\neffect fn main(required config: string, suffix: string = \"ok\") -> string { suffix }"
	formatSmokeInputLimit = 2 * 1024 * 1024
	formatSmokeOutputCap  = 4 * 1024 * 1024
	formatSmokeFrameLimit = 16 * 1024 * 1024
)

// formatSmokeRun runs the CLI in a directory with raw stdin bytes, bounded by
// a deadline instead of the Python subprocess timeout.
func formatSmokeRun(t *testing.T, binary, directory string, input []byte, args ...string) ([]byte, []byte, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = directory
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("ef %s exceeded its deadline", strings.Join(args, " "))
	}
	if err == nil {
		return stdout.Bytes(), stderr.Bytes(), 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return stdout.Bytes(), stderr.Bytes(), exit.ExitCode()
	}
	t.Fatal(err)
	return nil, nil, -1
}

// formatSmokeLine encodes one compact JSON-RPC line without HTML escaping,
// matching Python's json.dumps for the ASCII payloads used here.
func formatSmokeLine(t *testing.T, value any) string {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(buffer.String(), "\n")
}

func formatSmokeInitialize(t *testing.T, client string) string {
	return formatSmokeLine(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": client, "version": "1"},
	}})
}

const (
	formatSmokeReady = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	formatSmokePing  = `{"jsonrpc":"2.0","id":3,"method":"ping"}`
)

func formatSmokeCall(t *testing.T, id any, arguments map[string]any) string {
	return formatSmokeLine(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": "code.format", "arguments": arguments}})
}

// formatSmokeMCP sends newline-terminated lines to `ef mcp <workspace>` and
// decodes every reply line.
func formatSmokeMCP(t *testing.T, binary, workspace string, lines ...string) ([]map[string]any, []byte, []byte, int) {
	t.Helper()
	stdout, stderr, code := formatSmokeRun(t, binary, workspace, []byte(strings.Join(lines, "\n")+"\n"), "mcp", workspace)
	var replies []map[string]any
	for _, raw := range bytes.Split(bytes.TrimRight(stdout, "\n"), []byte("\n")) {
		if len(raw) != 0 {
			replies = append(replies, smokeJSON(t, raw))
		}
	}
	return replies, stdout, stderr, code
}

func formatSmokeIDs(replies []map[string]any) []any {
	ids := make([]any, 0, len(replies))
	for _, reply := range replies {
		ids = append(ids, reply["id"])
	}
	return ids
}

func formatSmokeEmptyResult(reply map[string]any) bool {
	result, ok := reply["result"].(map[string]any)
	return ok && len(result) == 0
}

func formatSmokeToolText(reply map[string]any) (bool, string) {
	result, _ := reply["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return result["isError"] == true, ""
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return result["isError"] == true, text
}

func formatSmokeError(reply map[string]any) (any, any) {
	failure, _ := reply["error"].(map[string]any)
	return failure["code"], failure["message"]
}

func formatSmokeFirstFailure(t *testing.T, stdout []byte) map[string]any {
	t.Helper()
	failures, _ := smokeJSON(t, stdout)["failures"].([]any)
	if len(failures) == 0 {
		t.Fatalf("report has no failures: %s", stdout)
	}
	failure, _ := failures[0].(map[string]any)
	return failure
}

func formatSmokeWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func formatSmokeRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func formatSmokeExpansion(lines int) []byte {
	var source bytes.Buffer
	source.WriteString("effect fn main() -> void { ")
	source.WriteString(strings.Repeat("scope { ", 64))
	source.WriteString(strings.Repeat("void;\n", lines))
	source.WriteString(strings.Repeat(" }", 64))
	source.WriteString(" }")
	return source.Bytes()
}

// formatSmokeExactIDLine fills a request's string id with a repeated scalar so
// the whole line is exactly the frame limit in bytes.
func formatSmokeExactIDLine(t *testing.T, prefix, suffix, scalar string) (string, string) {
	t.Helper()
	available := formatSmokeFrameLimit - len(prefix+suffix)
	repetitions := available / len(scalar)
	identifier := strings.Repeat(scalar, repetitions) + strings.Repeat("a", available-repetitions*len(scalar))
	line := prefix + identifier + suffix
	if len(line) != formatSmokeFrameLimit {
		t.Fatalf("line is %d bytes, want %d", len(line), formatSmokeFrameLimit)
	}
	return line, identifier
}

func TestFormatSmokeCLIAndMCPProcessAdapters(t *testing.T) {
	binary := buildTestCLI(t)

	stdinOut, stdinErr, code := formatSmokeRun(t, binary, t.TempDir(), []byte(formatSmokeSource), "fmt", "--stdin")
	if code != 0 || len(stdinErr) != 0 || !strings.Contains(string(stdinOut), "import go missing") || !bytes.HasSuffix(stdinOut, []byte("\n")) {
		t.Fatalf("stdin: exit %d stdout=%q stderr=%q", code, stdinOut, stdinErr)
	}

	// The check, write and no-op runs share one file; the MCP session then
	// formats that written file from disk.
	chain := t.TempDir()
	path := filepath.Join(chain, "main.ef")
	formatSmokeWrite(t, path, []byte(formatSmokeSource))
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := formatSmokeRun(t, binary, chain, nil, "fmt", "--check", "--json", path)
	if code != 1 || len(stderr) != 0 {
		t.Fatalf("check: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	checkReport := smokeJSON(t, stdout)
	checkFiles, _ := checkReport["files"].([]any)
	if checkReport["success"] != true || len(checkFiles) == 0 {
		t.Fatalf("check report: %v", checkReport)
	}
	if file, _ := checkFiles[0].(map[string]any); file["changed"] != true || file["written"] != false {
		t.Fatalf("check file report: %v", file)
	}
	if got := formatSmokeRead(t, path); string(got) != formatSmokeSource {
		t.Fatalf("check changed the file: %q", got)
	}

	stdout, stderr, code = formatSmokeRun(t, binary, chain, nil, "fmt", "--json", path)
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("write: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	writeReport := smokeJSON(t, stdout)
	writeFiles, _ := writeReport["files"].([]any)
	if writeReport["formatterVersion"] != "effra/formatter-12" || writeReport["schemaVersion"] != float64(1) || len(writeFiles) == 0 {
		t.Fatalf("write report: %v", writeReport)
	}
	if file, _ := writeFiles[0].(map[string]any); file["written"] != true {
		t.Fatalf("write file report: %v", file)
	}
	written, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if written.Mode().Perm() != original.Mode().Perm() {
		t.Fatalf("write changed mode %#o to %#o", original.Mode().Perm(), written.Mode().Perm())
	}
	formatted := formatSmokeRead(t, path)

	stdout, stderr, code = formatSmokeRun(t, binary, chain, nil, "fmt", path)
	if code != 0 || len(stdout) != 0 || !strings.Contains(string(stderr), "already formatted") {
		t.Fatalf("no-op: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	noop, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(formatSmokeRead(t, path), formatted) || !noop.ModTime().Equal(written.ModTime()) {
		t.Fatal("no-op rewrote the formatted file")
	}

	t.Run("angle-roles", func(t *testing.T) {
		t.Parallel()
		source := "record Box<T: type> { value: T }\n" +
			"fn less(left: i64, right: i64) -> bool { left < right }\n" +
			"fn greater(value: i64) -> bool { value > (0) }\n" +
			"effect fn main() -> void { void }"
		want := "record Box<T: type> {\n    value: T\n}\n" +
			"fn less(left: i64, right: i64) -> bool {\n    left < right\n}\n" +
			"fn greater(value: i64) -> bool {\n    value > (0)\n}\n" +
			"effect fn main() -> void {\n    void\n}\n"
		dir := t.TempDir()
		stdout, stderr, code := formatSmokeRun(t, binary, dir, []byte(source), "fmt", "--stdin")
		if code != 0 || len(stderr) != 0 || string(stdout) != want {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		again, _, code := formatSmokeRun(t, binary, dir, stdout, "fmt", "--stdin")
		if code != 0 || string(again) != want {
			t.Fatalf("not idempotent: exit %d stdout=%q", code, again)
		}
	})

	t.Run("hardlink-alias", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		valid := filepath.Join(dir, "valid.ef")
		alias := filepath.Join(dir, "alias.ef")
		validBytes := []byte(`effect fn main() -> string { "valid" }`)
		formatSmokeWrite(t, valid, validBytes)
		if err := os.Link(valid, alias); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := formatSmokeRun(t, binary, dir, nil, "fmt", "--json", valid, alias)
		if code != 2 || len(stderr) != 0 || formatSmokeFirstFailure(t, stdout)["code"] != "EFMT_ALIAS" {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		if !bytes.Equal(formatSmokeRead(t, valid), validBytes) || !bytes.Equal(formatSmokeRead(t, alias), validBytes) {
			t.Fatal("alias refusal wrote a file")
		}
	})

	t.Run("option-like-path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		formatSmokeWrite(t, filepath.Join(dir, "--option.ef"), []byte(formatSmokeSource))
		if stdout, stderr, code := formatSmokeRun(t, binary, dir, nil, "fmt", "--", "--option.ef"); code != 0 {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	t.Run("duplicate-requested-paths", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		formatSmokeWrite(t, filepath.Join(dir, "main.ef"), formatted)
		stdout, stderr, code := formatSmokeRun(t, binary, dir, nil, "fmt", "--json", "main.ef", "./main.ef")
		if code != 0 || len(stderr) != 0 {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		files, _ := smokeJSON(t, stdout)["files"].([]any)
		if len(files) != 1 {
			t.Fatalf("files: %v", files)
		}
		if file, _ := files[0].(map[string]any); !reflect.DeepEqual(file["requestedPaths"], []any{"main.ef", "./main.ef"}) {
			t.Fatalf("requested paths: %v", file)
		}
	})

	t.Run("input-limits", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		var inputs []string
		for index := 0; index < 4; index++ {
			name := "input-" + strconv.Itoa(index) + ".ef"
			formatSmokeWrite(t, filepath.Join(dir, name), bytes.Repeat([]byte(" "), formatSmokeInputLimit))
			inputs = append(inputs, name)
		}
		stdout, stderr, code := formatSmokeRun(t, binary, dir, nil, append([]string{"fmt", "--check", "--json"}, inputs...)...)
		if code != 1 || smokeJSON(t, stdout)["success"] != true {
			t.Fatalf("exact aggregate input: exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		extra := filepath.Join(dir, "aggregate-extra.ef")
		formatSmokeWrite(t, extra, []byte(" "))
		stdout, stderr, code = formatSmokeRun(t, binary, dir, nil, append(append([]string{"fmt", "--json"}, inputs...), "aggregate-extra.ef")...)
		failure := formatSmokeFirstFailure(t, stdout)
		if message, _ := failure["message"].(string); code != 2 || failure["code"] != "EFMT_INPUT_LIMIT" || !strings.Contains(message, "aggregate input limit") {
			t.Fatalf("aggregate input over: exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		formatSmokeWrite(t, extra, bytes.Repeat([]byte(" "), formatSmokeInputLimit+1))
		stdout, stderr, code = formatSmokeRun(t, binary, dir, nil, "fmt", "--json", "aggregate-extra.ef")
		if code != 2 || formatSmokeFirstFailure(t, stdout)["code"] != "EFMT_INPUT_LIMIT" {
			t.Fatalf("per-file input over: exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	t.Run("output-limits", func(t *testing.T) {
		t.Parallel()
		expansion := formatSmokeExpansion(15000)
		expanded, stderr, code := formatSmokeRun(t, binary, t.TempDir(), expansion, "fmt", "--stdin")
		if code != 0 {
			t.Fatalf("expansion: exit %d stderr=%q", code, stderr)
		}
		padding := formatSmokeOutputCap - len(expanded) - 3
		if padding < 0 {
			t.Fatalf("expansion output %d bytes leaves no padding", len(expanded))
		}
		exactSource := append([]byte("//"+strings.Repeat("x", padding)+"\n"), expansion...)
		t.Run("stdin", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			stdout, stderr, code := formatSmokeRun(t, binary, dir, exactSource, "fmt", "--stdin")
			if code != 0 || len(stdout) != formatSmokeOutputCap {
				t.Fatalf("exact output: exit %d stdout=%d bytes stderr=%q", code, len(stdout), stderr)
			}
			oneOver := append([]byte("//x"), exactSource[2:]...)
			stdout, stderr, code = formatSmokeRun(t, binary, dir, oneOver, "fmt", "--stdin")
			if code != 2 || len(stdout) != 0 || !bytes.Contains(stderr, []byte("EFMT_OUTPUT_LIMIT")) {
				t.Fatalf("one over output: exit %d stdout=%d bytes stderr=%q", code, len(stdout), stderr)
			}
		})
		t.Run("aggregate", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var outputs []string
			for index := 0; index < 4; index++ {
				name := "output-" + strconv.Itoa(index) + ".ef"
				formatSmokeWrite(t, filepath.Join(dir, name), exactSource)
				outputs = append(outputs, name)
			}
			stdout, stderr, code := formatSmokeRun(t, binary, dir, nil, append([]string{"fmt", "--check", "--json"}, outputs...)...)
			if code != 1 || smokeJSON(t, stdout)["success"] != true {
				t.Fatalf("exact aggregate output: exit %d stdout=%q stderr=%q", code, stdout, stderr)
			}
			formatSmokeWrite(t, filepath.Join(dir, "aggregate-empty.ef"), nil)
			stdout, stderr, code = formatSmokeRun(t, binary, dir, nil, append(append([]string{"fmt", "--check", "--json"}, outputs...), "aggregate-empty.ef")...)
			if code != 1 || smokeJSON(t, stdout)["success"] != true {
				t.Fatalf("exact aggregate output with empty file: exit %d stdout=%q stderr=%q", code, stdout, stderr)
			}
			formatSmokeWrite(t, filepath.Join(dir, "aggregate-extra.ef"), []byte("//extra"))
			stdout, stderr, code = formatSmokeRun(t, binary, dir, nil, append(append([]string{"fmt", "--json"}, outputs...), "aggregate-extra.ef")...)
			failure := formatSmokeFirstFailure(t, stdout)
			if message, _ := failure["message"].(string); code != 2 || failure["code"] != "EFMT_OUTPUT_LIMIT" || !strings.Contains(message, "aggregate output limit") {
				t.Fatalf("aggregate output over: exit %d stdout=%q stderr=%q", code, stdout, stderr)
			}
			for _, name := range outputs {
				if !bytes.Equal(formatSmokeRead(t, filepath.Join(dir, name)), exactSource) {
					t.Fatalf("aggregate output refusal wrote %s", name)
				}
			}
		})
	})

	t.Run("stdin-json-invocation", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code := formatSmokeRun(t, binary, t.TempDir(), []byte(formatSmokeSource), "fmt", "--stdin", "--json")
		if code != 2 || len(stderr) != 0 || formatSmokeFirstFailure(t, stdout)["code"] != "EFMT_INVOCATION" {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	t.Run("mcp-session", func(t *testing.T) {
		t.Parallel()
		digest := sha256.Sum256([]byte(formatSmokeSource))
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain,
			formatSmokeInitialize(t, "format-smoke"),
			formatSmokeReady,
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
			formatSmokeCall(t, 3, map[string]any{"source": formatSmokeSource, "uri": "buffer://main.ef", "expectedDigest": hex.EncodeToString(digest[:])}),
			formatSmokeCall(t, 4, map[string]any{"file": "main.ef"}),
			formatSmokeCall(t, 5, map[string]any{"source": formatSmokeSource, "expectedDigest": "stale"}),
			`{"jsonrpc":"2.0","id":6,"method":"ping"}`,
		)
		if code != 0 || !reflect.DeepEqual(formatSmokeIDs(replies), []any{1.0, 2.0, 3.0, 4.0, 5.0, 6.0}) {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		listed := false
		tools, _ := replies[1]["result"].(map[string]any)["tools"].([]any)
		for _, tool := range tools {
			if entry, _ := tool.(map[string]any); entry["name"] == "code.format" {
				listed = true
			}
		}
		if !listed {
			t.Fatalf("code.format not listed: %v", replies[1])
		}
		buffer, _ := replies[2]["result"].(map[string]any)["structuredContent"].(map[string]any)
		if buffer["formatterVersion"] != writeReport["formatterVersion"] || buffer["schemaVersion"] != writeReport["schemaVersion"] ||
			buffer["origin"] != "buffer" || buffer["uri"] != "buffer://main.ef" || buffer["text"] != string(stdinOut) {
			t.Fatalf("buffer result: %v", replies[2])
		}
		disk, _ := replies[3]["result"].(map[string]any)["structuredContent"].(map[string]any)
		if disk["origin"] != "disk" || disk["text"] != string(stdinOut) {
			t.Fatalf("disk result: %v", replies[3])
		}
		if isError, text := formatSmokeToolText(replies[4]); !isError || !strings.Contains(text, "stale format source") {
			t.Fatalf("stale digest: %v", replies[4])
		}
		if !formatSmokeEmptyResult(replies[5]) {
			t.Fatalf("ping: %v", replies[5])
		}
	})

	t.Run("mcp-output-limit", func(t *testing.T) {
		t.Parallel()
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain,
			formatSmokeInitialize(t, "format-expansion"),
			formatSmokeReady,
			formatSmokeCall(t, 2, map[string]any{"source": string(formatSmokeExpansion(20000))}),
			formatSmokePing,
		)
		if code != 0 || !reflect.DeepEqual(formatSmokeIDs(replies), []any{1.0, 2.0, 3.0}) {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		result, _ := replies[1]["result"].(map[string]any)
		_, structured := result["structuredContent"]
		if isError, text := formatSmokeToolText(replies[1]); !isError || structured || !strings.Contains(text, "MCP output limit") {
			t.Fatalf("expanded output: %v", replies[1])
		}
	})

	t.Run("mcp-target-argument", func(t *testing.T) {
		t.Parallel()
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain,
			formatSmokeInitialize(t, "format-target"),
			formatSmokeReady,
			formatSmokeCall(t, 2, map[string]any{"source": "", "target": "go"}),
			formatSmokePing,
		)
		if code != 0 || len(replies) != 3 || !formatSmokeEmptyResult(replies[2]) || replies[1]["error"] == nil {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	initialize := formatSmokeInitialize(t, "format-smoke")
	compactFormat := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"code.format","arguments":{"source":""}}}`
	for _, size := range []int{formatSmokeFrameLimit - 1, formatSmokeFrameLimit, formatSmokeFrameLimit + 1, formatSmokeFrameLimit + 1024*1024} {
		t.Run("mcp-frame-"+strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()
			raw := compactFormat + strings.Repeat(" ", size-len(compactFormat))
			replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain, initialize, formatSmokeReady, raw, formatSmokePing)
			want := []any{1.0, 2.0, 3.0}
			if size > formatSmokeFrameLimit {
				want = []any{1.0, nil, 3.0}
			}
			if code != 0 || len(stderr) != 0 || !reflect.DeepEqual(formatSmokeIDs(replies), want) {
				t.Fatalf("exit %d ids=%v stdout=%.400q stderr=%q", code, formatSmokeIDs(replies), stdout, stderr)
			}
		})
	}

	t.Run("mcp-malformed-json", func(t *testing.T) {
		t.Parallel()
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain, initialize, formatSmokeReady, "{bad json}", formatSmokePing)
		if code != 0 || !reflect.DeepEqual(formatSmokeIDs(replies), []any{1.0, nil, 3.0}) {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	t.Run("mcp-malformed-json-at-eof", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code := formatSmokeRun(t, binary, chain, []byte(initialize+"\n"+formatSmokeReady+"\n{bad json}"), "mcp", chain)
		lines := bytes.Split(bytes.TrimRight(stdout, "\n"), []byte("\n"))
		if code != 0 || len(lines) == 0 {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		if errorCode, _ := formatSmokeError(smokeJSON(t, lines[len(lines)-1])); errorCode != float64(-32700) {
			t.Fatalf("final reply is not a parse error: %q", lines[len(lines)-1])
		}
	})

	t.Run("mcp-response-envelope-limit", func(t *testing.T) {
		t.Parallel()
		// Quotes remain escaped by JSON even when the bounded response
		// encoder leaves HTML characters alone. Fill the admitted request
		// close enough to the frame cap that the response envelope itself
		// crosses the cap.
		empty := formatSmokeCall(t, 2, map[string]any{"source": "", "uri": ""})
		length := min(formatSmokeFrameLimit/2, (formatSmokeFrameLimit-len(empty))/2)
		line := formatSmokeCall(t, 2, map[string]any{"source": "", "uri": strings.Repeat(`"`, length)})
		if len(line) > formatSmokeFrameLimit {
			t.Fatalf("request line is %d bytes", len(line))
		}
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain, initialize, formatSmokeReady, line, formatSmokePing)
		if code != 0 || len(replies) < 2 || !formatSmokeEmptyResult(replies[len(replies)-1]) {
			t.Fatalf("exit %d stdout=%.400q stderr=%q", code, stdout, stderr)
		}
		if isError, text := formatSmokeToolText(replies[1]); !isError || !strings.Contains(text, "encoded bytes") {
			t.Fatalf("oversized response: %.400v", replies[1])
		}
	})

	t.Run("mcp-html-id-unescaped", func(t *testing.T) {
		t.Parallel()
		id := strings.Repeat("<", 3*1024*1024)
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain, initialize, formatSmokeReady, formatSmokeCall(t, id, map[string]any{"source": ""}), formatSmokePing)
		if code != 0 || len(replies) < 2 || !formatSmokeEmptyResult(replies[len(replies)-1]) {
			t.Fatalf("exit %d stdout=%.400q stderr=%q", code, stdout, stderr)
		}
		if replies[1]["id"] != id || bytes.Contains(stdout, []byte("\\u003c")) {
			t.Fatalf("raw id was not echoed unescaped: %.400q", stdout)
		}
	})

	t.Run("mcp-near-limit-id", func(t *testing.T) {
		t.Parallel()
		id := strings.Repeat("a", formatSmokeFrameLimit-len(formatSmokeCall(t, "", map[string]any{"source": ""})))
		line := formatSmokeCall(t, id, map[string]any{"source": ""})
		if len(line) != formatSmokeFrameLimit {
			t.Fatalf("request line is %d bytes", len(line))
		}
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain, initialize, formatSmokeReady, line, formatSmokePing)
		if code != 0 || !reflect.DeepEqual(formatSmokeIDs(replies), []any{1.0, id, 3.0}) {
			t.Fatalf("exit %d stdout=%.400q stderr=%q", code, stdout, stderr)
		}
		if errorCode, message := formatSmokeError(replies[1]); errorCode != float64(-32000) || message != "too big" {
			t.Fatalf("near-limit id: %.400v", replies[1]["error"])
		}
	})

	validPrefix := `{"jsonrpc":"2.0","id":"`
	validSuffix := `","method":"tools/call","params":{"name":"code.format","arguments":{"source":""}}}`
	for _, separator := range []string{"\u2028", "\u2029"} {
		t.Run(fmt.Sprintf("mcp-separator-id-U+%04X", []rune(separator)[0]), func(t *testing.T) {
			t.Parallel()
			line, id := formatSmokeExactIDLine(t, validPrefix, validSuffix, separator)
			replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain, initialize, formatSmokeReady, line, formatSmokePing)
			if code != 0 || !reflect.DeepEqual(formatSmokeIDs(replies), []any{1.0, id, 3.0}) {
				t.Fatalf("exit %d stdout=%.400q stderr=%q", code, stdout, stderr)
			}
			if errorCode, message := formatSmokeError(replies[1]); errorCode != float64(-32000) || message != "too big" {
				t.Fatalf("separator id: %.400v", replies[1]["error"])
			}
			for _, reply := range bytes.Split(stdout, []byte("\n")) {
				if len(reply) > formatSmokeFrameLimit {
					t.Fatalf("reply line is %d bytes", len(reply))
				}
			}
		})
	}

	missingPrefix := `{"jsonrpc":"2.0","id":"`
	missingSuffix := `","method":"tools/call","params":{"name":"code.format"}}`
	missingLine, missingID := formatSmokeExactIDLine(t, missingPrefix, missingSuffix, "a")
	for _, c := range []struct {
		name, line, id string
	}{
		{"exact", missingLine, missingID},
		{"one-under", missingPrefix + missingID[:len(missingID)-1] + missingSuffix, missingID[:len(missingID)-1]},
	} {
		t.Run("mcp-missing-arguments-"+c.name, func(t *testing.T) {
			t.Parallel()
			replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain, initialize, formatSmokeReady, c.line, formatSmokePing)
			if code != 0 || !reflect.DeepEqual(formatSmokeIDs(replies), []any{1.0, c.id, 3.0}) {
				t.Fatalf("exit %d stdout=%.400q stderr=%q", code, stdout, stderr)
			}
			if errorCode, message := formatSmokeError(replies[1]); errorCode != float64(-32602) || message != "too big" {
				t.Fatalf("missing arguments: %.400v", replies[1]["error"])
			}
		})
	}

	t.Run("mcp-unknown-argument-key", func(t *testing.T) {
		t.Parallel()
		prefix := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"code.format","arguments":{"source":"","`
		suffix := `":""}}}`
		keyBytes := formatSmokeFrameLimit - len(prefix+suffix)
		line := prefix + strings.Repeat("\u2028", keyBytes/3) + strings.Repeat("a", keyBytes%3) + suffix
		if len(line) != formatSmokeFrameLimit {
			t.Fatalf("request line is %d bytes", len(line))
		}
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain, initialize, formatSmokeReady, line, formatSmokePing)
		if code != 0 || !reflect.DeepEqual(formatSmokeIDs(replies), []any{1.0, 2.0, 3.0}) {
			t.Fatalf("exit %d stdout=%.400q stderr=%q", code, stdout, stderr)
		}
		if errorCode, message := formatSmokeError(replies[1]); errorCode != float64(-32602) || message != "too big" {
			t.Fatalf("unknown key: %.400v", replies[1]["error"])
		}
	})

	t.Run("mcp-id-variants", func(t *testing.T) {
		t.Parallel()
		call := `,"method":"tools/call","params":{"name":"code.format","arguments":{"source":""}}}`
		quoted := "quote\"\u0001"
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, chain,
			formatSmokeInitialize(t, "format-id-variants"),
			formatSmokeReady,
			`{"jsonrpc":"2.0","id":-0.0`+call,
			`{"jsonrpc":"2.0","id":null`+call,
			formatSmokeCall(t, quoted, map[string]any{"source": ""}),
			formatSmokePing,
		)
		if code != 0 || !reflect.DeepEqual(formatSmokeIDs(replies), []any{1.0, 0.0, nil, quoted, 3.0}) {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		for _, reply := range replies[1:4] {
			result, _ := reply["result"].(map[string]any)
			if structured, _ := result["structuredContent"].(map[string]any); structured["origin"] != "buffer" {
				t.Fatalf("id variant reply: %v", reply)
			}
		}
	})

	t.Run("mcp-invalid-utf8-file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		formatSmokeWrite(t, filepath.Join(dir, "invalid-utf8.ef"), []byte("// invalid \xff\neffect fn main() -> void { void }\n"))
		replies, stdout, stderr, code := formatSmokeMCP(t, binary, dir,
			formatSmokeInitialize(t, "format-utf8"),
			formatSmokeReady,
			formatSmokeCall(t, 2, map[string]any{"file": "invalid-utf8.ef"}),
			formatSmokePing,
		)
		if code != 0 || len(replies) != 3 || !formatSmokeEmptyResult(replies[2]) {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		if isError, text := formatSmokeToolText(replies[1]); !isError || !strings.Contains(text, "not valid UTF-8") {
			t.Fatalf("invalid UTF-8 file: %v", replies[1])
		}
	})
}
