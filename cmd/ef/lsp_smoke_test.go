package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Ported from scripts/lsp_smoke.py: actual Content-Length framed `ef lsp`
// processes, shared diagnostic parity with `ef diagnostics` and MCP
// project.diagnostics, hover/definition parity with `ef type --offset` and MCP
// code.type, and document formatting parity with `ef fmt --stdin` and MCP
// code.format. Each former `--review-case` is a named subtest:
// uri, operational, recovery, oversized, navigation and formatting, so
// `go test -run 'TestLSPSmokeFramedProcesses/^uri$' ./cmd/ef` isolates one.
func TestLSPSmokeFramedProcesses(t *testing.T) {
	binary := buildTestCLI(t)
	// The servers start outside every document's directory, as the smoke
	// started them from the repository root.
	workspace := smokeWorkspace(t, "go.mod", "examples/callables-factory.ef")
	cli := lspSmokeCLI{binary: binary, cwd: workspace}
	cases := []struct {
		name string
		run  func(*testing.T, lspSmokeCLI)
	}{
		{"parity", lspSmokeParity},
		{"documents", lspSmokeDocuments},
		{"navigation", lspSmokeNavigation},
		{"formatting", lspSmokeFormattingCase},
		{"imports", lspSmokeImports},
		{"protocol", lspSmokeBoundsAndProtocol},
		{"uri", lspSmokeURIAlias},
		{"operational", lspSmokeOperationalBudget},
		{"recovery", lspSmokeRefusalRecovery},
		{"oversized", lspSmokeOversizedURI},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.run(t, cli)
		})
	}
}

// lspSmokeCLI runs the test CLI from a fixed working directory.
type lspSmokeCLI struct {
	binary string
	cwd    string
}

// lspSmokeSession is one framed `ef lsp` process.
type lspSmokeSession struct {
	calls       []map[string]any
	target      string // default "go"
	fragmented  bool
	expected    int
	rawPrefix   []byte
	environment []string // added to the inherited environment
}

var (
	lspSmokeInit  = lspSmokeCall("initialize", map[string]any{"capabilities": map[string]any{}}, 1)
	lspSmokeReady = lspSmokeCall("initialized", map[string]any{}, nil)
	lspSmokeStop  = lspSmokeCall("shutdown", nil, "stop")
	lspSmokeExit  = lspSmokeCall("exit", nil, nil)
)

func lspSmokeCall(method string, params any, id any) map[string]any {
	value := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		value["params"] = params
	}
	if id != nil {
		value["id"] = id
	}
	return value
}

// lspSmokeFrame frames a message as the smoke did: its body is
// json.dumps(value, ensure_ascii=False), so a fragmented write splits literal
// UTF-8 sequences.
func lspSmokeFrame(t *testing.T, value any) []byte {
	t.Helper()
	body := smokeDumpsUTF8(t, value)
	return append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))), body...)
}

func lspSmokeDecode(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var messages []map[string]any
	for len(data) > 0 {
		header, rest, found := bytes.Cut(data, []byte("\r\n\r\n"))
		if !found || !bytes.HasPrefix(header, []byte("Content-Length: ")) {
			t.Fatalf("invalid LSP header %q", header)
		}
		count, err := strconv.Atoi(string(bytes.TrimPrefix(header, []byte("Content-Length: "))))
		if err != nil || count < 0 || len(rest) < count {
			t.Fatalf("invalid LSP length %q for %d remaining bytes", header, len(rest))
		}
		messages = append(messages, smokeJSON(t, rest[:count]))
		data = rest[count:]
	}
	return messages
}

// lspSmokeRun runs one CLI process under the smoke's deadline for it,
// without touching t, so goroutines may use it.
func lspSmokeRun(timeout time.Duration, binary, dir, input string, args ...string) ([]byte, []byte, int, error) {
	command := exec.Command(binary, args...)
	command.Dir = dir
	command.Stdin = strings.NewReader(input)
	return runSmokeDeadline(command, timeout)
}

// The smoke's subprocess deadlines: one framed LSP session, a CLI query
// (diagnostics, type or fmt), a diagnostics MCP session, a type or format MCP
// session, and the refused-target start.
const (
	lspSmokeSessionDeadline  = 40 * time.Second
	lspSmokeCLIDeadline      = 30 * time.Second
	lspSmokeDiagnosticsMCP   = 30 * time.Second
	lspSmokeToolMCP          = 60 * time.Second
	lspSmokeBadTargetTimeout = 5 * time.Second
)

func (c lspSmokeCLI) exchange(t *testing.T, s lspSmokeSession) []map[string]any {
	t.Helper()
	target := s.target
	if target == "" {
		target = "go"
	}
	payload := append([]byte(nil), s.rawPrefix...)
	split := 0
	for index, call := range s.calls {
		frame := lspSmokeFrame(t, call)
		if index < 3 {
			split += len(frame)
		}
		payload = append(payload, frame...)
	}
	// The deadline kills the server, and Wait below reaps it before the test
	// fails; WaitDelay bounds the wait for pipes a grandchild might hold.
	ctx, cancel := context.WithTimeout(context.Background(), lspSmokeSessionDeadline)
	defer cancel()
	command := exec.CommandContext(ctx, c.binary, "lsp", "--target", target)
	command.WaitDelay = smokeWaitDelay
	command.Dir = c.cwd
	if s.environment != nil {
		command.Env = append(os.Environ(), s.environment...)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	// A server that terminates early closes its input; its exit status and
	// stderr below judge the session, so write failures are not themselves
	// failures.
	if s.fragmented {
		// Split both header and Unicode body bytes, including the entire open
		// notification. The suffix coalesces several complete messages in one
		// write without timing assumptions.
		for _, b := range payload[:split] {
			if _, err := stdin.Write([]byte{b}); err != nil {
				break
			}
		}
		payload = payload[split:]
	}
	_, _ = stdin.Write(payload)
	_ = stdin.Close()
	err = command.Wait()
	if ctx.Err() != nil {
		t.Fatalf("lsp --target %s timed out; stderr=%q", target, stderr.Bytes())
	}
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if code != s.expected {
		t.Fatalf("lsp exit %d, want %d; stderr=%q", code, s.expected, stderr.Bytes())
	}
	if s.expected == 0 && stderr.Len() != 0 {
		t.Fatalf("lsp stderr after clean exit: %q", stderr.Bytes())
	}
	if s.expected != 0 && stderr.Len() == 0 {
		t.Fatal("abnormal termination must be explicit")
	}
	return lspSmokeDecode(t, stdout.Bytes())
}

// lspSmokeURI is Python's pathlib as_uri: every byte except unreserved
// characters and "/" is percent-encoded with upper-case hex.
func lspSmokeURI(path string) string {
	var uri strings.Builder
	uri.WriteString("file://")
	for _, c := range []byte(path) {
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || strings.IndexByte("_.-~/", c) >= 0 {
			uri.WriteByte(c)
		} else {
			fmt.Fprintf(&uri, "%%%02X", c)
		}
	}
	return uri.String()
}

// lspSmokePosition is the independent protocol oracle: decode the known byte
// prefix, normalize editor line endings, then count UTF-16 code units on its
// final line.
func lspSmokePosition(t *testing.T, source string, offset int) map[string]any {
	t.Helper()
	offset = min(offset, len(source))
	prefix := source[:offset]
	if !utf8.ValidString(prefix) {
		t.Fatalf("byte offset %d splits a UTF-8 sequence", offset)
	}
	prefix = strings.ReplaceAll(strings.ReplaceAll(prefix, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(prefix, "\n")
	return map[string]any{"line": len(lines) - 1, "character": len(utf16.Encode([]rune(lines[len(lines)-1])))}
}

func lspSmokeRange(t *testing.T, source string, offset, length int) map[string]any {
	t.Helper()
	return map[string]any{"start": lspSmokePosition(t, source, offset), "end": lspSmokePosition(t, source, offset+length)}
}

func lspSmokeSpanRange(t *testing.T, source string, span map[string]any) map[string]any {
	t.Helper()
	return lspSmokeRange(t, source, lspSmokeInt(t, span["offset"]), lspSmokeInt(t, span["length"]))
}

func lspSmokeInt(t *testing.T, value any) int {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("not a JSON number: %v", value)
	}
	return int(number)
}

func lspSmokeMap(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func lspSmokeOpened(uri, text string, version int) map[string]any {
	return lspSmokeCall("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "effra", "version": version, "text": text}}, nil)
}

func lspSmokeChanged(uri, text string, version int) map[string]any {
	return lspSmokeCall("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": version},
		"contentChanges": []any{map[string]any{"text": text}}}, nil)
}

func lspSmokeClosed(uri string) map[string]any {
	return lspSmokeCall("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}}, nil)
}

func lspSmokeUnknown(id string) map[string]any {
	return lspSmokeCall("unknown", nil, id)
}

func lspSmokePublications(messages []map[string]any) []map[string]any {
	var published []map[string]any
	for _, message := range messages {
		if message["method"] == "textDocument/publishDiagnostics" {
			published = append(published, lspSmokeMap(message["params"]))
		}
	}
	return published
}

func lspSmokeLogs(messages []map[string]any) []string {
	var logs []string
	for _, message := range messages {
		if message["method"] == "window/logMessage" {
			text, _ := lspSmokeMap(message["params"])["message"].(string)
			logs = append(logs, text)
		}
	}
	return logs
}

// lspSmokeAnyMessage reports whether any message's params.message contains
// text.
func lspSmokeAnyMessage(messages []map[string]any, text string) bool {
	for _, message := range messages {
		if value, _ := lspSmokeMap(message["params"])["message"].(string); strings.Contains(value, text) {
			return true
		}
	}
	return false
}

func lspSmokeReplies(messages []map[string]any) map[string]map[string]any {
	replies := map[string]map[string]any{}
	for _, message := range messages {
		if id, ok := message["id"]; ok {
			replies[fmt.Sprint(id)] = message
		}
	}
	return replies
}

func lspSmokeReply(t *testing.T, messages []map[string]any, id string) map[string]any {
	t.Helper()
	reply, ok := lspSmokeReplies(messages)[id]
	if !ok {
		t.Fatalf("no reply %q in %v", id, messages)
	}
	return reply
}

func lspSmokeExpectError(t *testing.T, reply map[string]any, code int) {
	t.Helper()
	if got := lspSmokeMap(reply["error"])["code"]; got != float64(code) {
		t.Fatalf("error code %v, want %d: %v", got, code, reply)
	}
}

// lspSmokeResult returns a reply's result, which must be present.
func lspSmokeResult(t *testing.T, reply map[string]any) any {
	t.Helper()
	result, ok := reply["result"]
	if !ok {
		t.Fatalf("reply lacks result: %v", reply)
	}
	return result
}

func lspSmokeSame(t *testing.T, got, want any, context string) {
	t.Helper()
	if !sameJSONValue(got, want) {
		t.Fatalf("%s:\n got %s\nwant %s", context, smokeDumpsUTF8(t, got), smokeDumpsUTF8(t, want))
	}
}

func lspSmokeWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lspSmokeAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or is unreadable: %v", path, err)
	}
}

func (c lspSmokeCLI) diagnostics(t *testing.T, path string) map[string]any {
	t.Helper()
	stdout, stderr, code, err := lspSmokeRun(lspSmokeCLIDeadline, c.binary, c.cwd, "", "diagnostics", path, "--json")
	if err != nil || (code != 0 && code != 1) {
		t.Fatalf("diagnostics exit %d: %v %q", code, err, stderr)
	}
	report := smokeJSON(t, stdout)
	if (code == 0) != (report["policyPassed"] == true) {
		t.Fatalf("diagnostics exit %d disagrees with policyPassed: %v", code, report)
	}
	return report
}

// mcp sends each request as json.dumps(request), the smoke's ASCII-escaped
// MCP encoding, to one stdio session.
func (c lspSmokeCLI) mcp(t *testing.T, timeout time.Duration, directory string, requests []any) []map[string]any {
	t.Helper()
	var input bytes.Buffer
	for _, request := range requests {
		input.Write(smokeDumps(t, request))
		input.WriteByte('\n')
	}
	stdout, stderr, code, err := lspSmokeRun(timeout, c.binary, c.cwd, input.String(), "mcp", directory)
	if err != nil || code != 0 {
		t.Fatalf("mcp exit %d: %v %q", code, err, stderr)
	}
	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimRight(string(stdout), "\n"), "\n") {
		replies = append(replies, smokeJSON(t, []byte(line)))
	}
	return replies
}

func lspSmokeMCPInitialize(client string, id any) []any {
	return []any{
		map[string]any{"jsonrpc": "2.0", "id": id, "method": "initialize", "params": map[string]any{
			"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": client, "version": "1"}}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
	}
}

// mcpDiagnostics is project.diagnostics for one file, followed by a queued
// ping that must still be answered.
func (c lspSmokeCLI) mcpDiagnostics(t *testing.T, directory, file string) map[string]any {
	t.Helper()
	requests := append(lspSmokeMCPInitialize("diagnostics-smoke", 1),
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call",
			"params": map[string]any{"name": "project.diagnostics", "arguments": map[string]any{"file": file}}},
		map[string]any{"jsonrpc": "2.0", "id": "after", "method": "ping"})
	replies := c.mcp(t, lspSmokeDiagnosticsMCP, directory, requests)
	if len(replies) != 3 {
		t.Fatalf("mcp replies: %v", replies)
	}
	lspSmokeSame(t, replies[2], map[string]any{"jsonrpc": "2.0", "id": "after", "result": map[string]any{}}, "queued ping")
	return lspSmokeMap(lspSmokeMap(replies[1]["result"])["structuredContent"])
}

// mcpTools calls one MCP tool per argument set and returns results by index.
func (c lspSmokeCLI) mcpTools(t *testing.T, directory, tool string, arguments []map[string]any) []map[string]any {
	t.Helper()
	requests := lspSmokeMCPInitialize("lsp-smoke", "init")
	for index, argument := range arguments {
		requests = append(requests, map[string]any{"jsonrpc": "2.0", "id": index, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": argument}})
	}
	byID := lspSmokeReplies(c.mcp(t, lspSmokeToolMCP, directory, requests))
	results := make([]map[string]any, len(arguments))
	for index := range arguments {
		reply, ok := byID[strconv.Itoa(index)]
		if !ok {
			t.Fatalf("no %s reply %d", tool, index)
		}
		results[index] = lspSmokeMap(reply["result"])
	}
	return results
}

func lspSmokeParity(t *testing.T, cli lspSmokeCLI) {
	warning := "effect fn task() -> string { \"ok\" }\r\n" +
		"effect fn main() -> string { let s = \"𐐀e\u0301\"; " +
		"let forgotten = task(); run task().provide<Console>(Stdout) }\r\n"
	fixtures := []struct{ name, text string }{
		{"unicode.ef", "effect fn main() -> void { \"𐐀e\u0301\" @ }"},
		{"crlf.ef", "// comment\r\neffect fn main() -> void { void }\r\n@"},
		{"eof.ef", "effect fn main() -> void {\r\n"},
		{"warning.ef", warning},
		{"lint-error.ef", "// effra-lint-disable-next-line bogus -- reason\r\neffect fn main() -> void { void }"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			path := filepath.Join(directory, fixture.name)
			lspSmokeWrite(t, path, fixture.text)
			report := cli.diagnostics(t, path)
			remote := cli.mcpDiagnostics(t, directory, fixture.name)
			assertReportParity(t, report, remote, parityOptions{reportSchema: 1, snapshotSchema: 8})
			messages := cli.exchange(t, lspSmokeSession{fragmented: true, calls: []map[string]any{
				lspSmokeInit, lspSmokeReady, lspSmokeOpened(lspSmokeURI(path), fixture.text, 4), lspSmokeStop, lspSmokeExit}})
			lspSmokeSame(t, lspSmokeMap(lspSmokeResult(t, messages[0]))["capabilities"], map[string]any{
				"positionEncoding": "utf-16", "textDocumentSync": map[string]any{"openClose": true, "change": 1},
				"hoverProvider": true, "definitionProvider": true, "documentFormattingProvider": true}, "capabilities")
			findings, _ := report["diagnostics"].([]any)
			projected := []any{}
			for _, finding := range findings {
				projected = append(projected, lspSmokeMap(finding)["lsp"])
			}
			lspSmokeSame(t, lspSmokePublications(messages), []any{map[string]any{
				"uri": lspSmokeURI(path), "version": 4, "diagnostics": projected}}, "publication matches the CLI report")
			for _, finding := range findings {
				finding := lspSmokeMap(finding)
				lspSmokeSame(t, lspSmokeMap(finding["lsp"])["range"],
					lspSmokeSpanRange(t, fixture.text, lspSmokeMap(finding["span"])), "editor range of the byte span")
			}
		})
	}
}

func lspSmokeDocuments(t *testing.T, cli lspSmokeCLI) {
	directory := t.TempDir()
	bad := "fn bad() -> string { true }"
	good := "fn good() -> string { \"good\" }"
	newPath := filepath.Join(directory, "never-created.ef")
	newURI := lspSmokeURI(newPath)
	t.Run("versions", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(directory, "unsaved space.ef")
		uri := lspSmokeURI(path)
		lspSmokeWrite(t, path, good)
		messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{
			lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, bad, 7), lspSmokeChanged(uri, good, 8),
			lspSmokeChanged(uri, bad, 7), lspSmokeOpened(uri, bad, 1),
			lspSmokeCall("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 9},
				"contentChanges": []any{map[string]any{"range": nil, "text": bad}}}, nil),
			lspSmokeClosed(uri), lspSmokeOpened(uri, bad, -2), lspSmokeCall("$/cancelRequest", map[string]any{"id": "already-complete"}, nil),
			lspSmokeCall("textDocument/rename", nil, "rename"), lspSmokeStop, lspSmokeExit}})
		published := lspSmokePublications(messages)
		versions := []any{}
		for _, publication := range published {
			versions = append(versions, publication["version"])
		}
		lspSmokeSame(t, versions, []any{7, 8, nil, -2}, "publication versions")
		if diagnostics, _ := published[0]["diagnostics"].([]any); len(diagnostics) == 0 {
			t.Fatalf("invalid buffer published no diagnostics: %v", published[0])
		}
		lspSmokeSame(t, published[1]["diagnostics"], []any{}, "valid change clears diagnostics")
		lspSmokeSame(t, published[2]["diagnostics"], []any{}, "close clears diagnostics")
		if logs := lspSmokeLogs(messages); len(logs) != 3 {
			t.Fatalf("logs = %q, want 3", logs)
		}
		lspSmokeExpectError(t, lspSmokeReply(t, messages, "rename"), -32601)
		lspSmokeSame(t, cli.diagnostics(t, path)["diagnostics"], []any{}, "disk changed while analyzing buffer")
	})
	for _, target := range []string{"go", "js"} {
		t.Run("never-created/"+target, func(t *testing.T) {
			t.Parallel()
			lspSmokeAbsent(t, newPath)
			published := lspSmokePublications(cli.exchange(t, lspSmokeSession{target: target, calls: []map[string]any{
				lspSmokeInit, lspSmokeReady, lspSmokeOpened(newURI, good, 1), lspSmokeStop, lspSmokeExit}}))
			if len(published) == 0 {
				t.Fatal("no publication")
			}
			lspSmokeSame(t, published[0]["diagnostics"], []any{}, "unsaved new document")
			lspSmokeAbsent(t, newPath)
		})
	}
	// The compiler used to panic on a valid effect factory carrying a pure
	// callback. A real document must publish, accept the next edit, and
	// recover.
	data, err := os.ReadFile(filepath.Join(cli.cwd, "examples", "callables-factory.ef"))
	if err != nil {
		t.Fatal(err)
	}
	factories := string(data)
	invalidTimeout := strings.ReplaceAll(factories, "let first = run pureFailure().catch<Missing>(keep)", "let first = keep.timeout(10)")
	if invalidTimeout == factories {
		t.Fatal("factory example no longer holds the replaced line")
	}
	for _, target := range []string{"go", "js"} {
		t.Run("factory-recovery/"+target, func(t *testing.T) {
			t.Parallel()
			reports := lspSmokePublications(cli.exchange(t, lspSmokeSession{target: target, calls: []map[string]any{
				lspSmokeInit, lspSmokeReady, lspSmokeOpened(newURI, factories, 1), lspSmokeChanged(newURI, invalidTimeout, 2),
				lspSmokeChanged(newURI, factories, 3), lspSmokeStop, lspSmokeExit}}))
			versions := []any{}
			for _, report := range reports {
				versions = append(versions, report["version"])
			}
			lspSmokeSame(t, versions, []any{1, 2, 3}, "factory versions")
			lspSmokeSame(t, reports[0]["diagnostics"], []any{}, "valid factory")
			lspSmokeSame(t, reports[2]["diagnostics"], []any{}, "recovered factory")
			found := false
			diagnostics, _ := reports[1]["diagnostics"].([]any)
			for _, diagnostic := range diagnostics {
				found = found || lspSmokeMap(diagnostic)["code"] == "EF106"
			}
			if !found {
				t.Fatalf("invalid timeout lacks EF106: %v", reports[1])
			}
		})
	}
}

var lspSmokeNavigationText = strings.Join([]string{
	`import Fns "effra/functions"`,
	`import Data "effra/data"`,
	`// 𐐀 helper is a comment, not a reference`,
	`error Missing { id: string }`,
	`record Box { value: string }`,
	`enum Shape { Circle { radius: i64 }, Square }`,
	`service Users { effect fn get(id: string) -> string raises {Missing} }`,
	`impl Fixed for Users { effect fn get(id: string) -> string raises {Missing} { "𐐀" + id } }`,
	`fn helper() -> string { "helper" }`,
	`fn shadow(helper: string) -> string { let mark = "𐐀é"; mark + helper }`,
	`fn boxed(value: string) -> Box { Box { value: value } }`,
	`fn area(shape: Shape) -> i64 { match shape { Shape.Circle { radius: r } => r, Shape.Square => 0 } }`,
	`fn pick(o: Data.Option<string>) -> string { match o { Data.Option.Some { value: v } => v, Data.Option.None => "none" } }`,
	`fn same(input: string) -> string { Fns.identity(input) }`,
	`fn greet() -> string { "𐐀" + helper() }`,
	`effect fn load(id: string) -> string raises {Missing} uses {Users} {`,
	`    if id == "" { fail Missing { id: id } } else { run Users.get(id) }`,
	`}`,
	`effect fn fixed(id: string) -> string raises {Missing} { run load(id).provide<Users>(Fixed) }`,
	`enum Reading { Cold { level: i64 }, Hot { level: i64 } }`,
	`fn warmth(reading: Reading) -> i64 { match reading { Reading.Cold { level: degrees } | Reading.Hot { level: degrees } => degrees } }`,
	``,
}, "\r\n")

// lspSmokeToken names a token by its unique context: the byte offset is the
// UTF-8 offset of name inside context; the LSP receives only the UTF-16
// editor position.
type lspSmokeToken struct{ context, name string }

var lspSmokeNamed = []lspSmokeToken{
	{"import Fns", "Fns"}, {"Fns.identity", "Fns"}, {"Fns.identity", "identity"},
	{"error Missing", "Missing"}, {"fail Missing", "Missing"}, {"Missing { id: id", "id"},
	{"record Box", "Box"}, {"Box { value: value }", "Box"}, {"Box { value: value }", "value"},
	{"enum Shape", "Shape"}, {"Shape.Circle {", "Shape"}, {"Shape.Circle {", "Circle"},
	{"radius: r", "radius"}, {"=> r,", "r"}, {"Shape.Square =>", "Square"},
	{"Data.Option.Some { value: v }", "Data"}, {"Data.Option.Some { value: v }", "Option"},
	{"Data.Option.Some { value: v }", "Some"}, {"=> v,", "v"},
	{"service Users", "Users"}, {"service Users { effect fn get(id", "id"},
	{"impl Fixed for Users { effect fn get(id", "id"}, {"impl Fixed for Users", "Users"}, {"run Users.get", "Users"},
	{"run Users.get", "get"}, {"provide<Users>(Fixed)", "Users"}, {"provide<Users>(Fixed)", "Fixed"},
	{"Fixed for Users { effect fn get", "get"}, {"run load(id)", "load"},
	{"fn helper()", "helper"}, {"fn shadow(helper", "helper"}, {"mark + helper", "helper"},
	{"let mark", "mark"}, {"; mark +", "mark"}, {"+ helper()", "helper"}, {"fn boxed(value", "value"},
	{"{ value: value }", "value }"}, {"Cold { level: degrees }", "degrees"},
	{"Hot { level: degrees }", "degrees"}, {"=> degrees", "degrees"},
	// Type annotations and row labels resolve through checked types and rows.
	{"-> Box {", "Box"}, {"shape: Shape)", "Shape"}, {"raises {Missing} uses", "Missing"},
	{"uses {Users} {", "Users"}, {"pick(o: Data.Option<string>)", "Data"},
	{"pick(o: Data.Option<string>)", "Option"},
}

var lspSmokeUnnamed = []lspSmokeToken{
	{"// 𐐀 helper", "helper"}, {"fn helper()", "fn"}, {"{ let mark", " "},
	{`"𐐀é"`, "é"}, {"run load(id)", "run"}, {"raises {Missing} uses", "raises"},
	{"pick(o: Data.Option<string>)", "string"},
}

func lspSmokeOffset(t *testing.T, text string, token lspSmokeToken) int {
	t.Helper()
	if strings.Count(text, token.context) != 1 || !strings.Contains(token.context, token.name) {
		t.Fatalf("token %v is not unique", token)
	}
	return strings.Index(text, token.context) + strings.Index(token.context, token.name)
}

func lspSmokeNamedIndex(t *testing.T, context, name string) int {
	t.Helper()
	for index, token := range lspSmokeNamed {
		if token == (lspSmokeToken{context, name}) {
			return index
		}
	}
	t.Fatalf("no named token %q/%q", context, name)
	return -1
}

func lspSmokeLocated(t *testing.T, method, id, uri, text string, offset int) map[string]any {
	t.Helper()
	return lspSmokeCall(method, map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": lspSmokePosition(t, text, offset)}, id)
}

// lspSmokeTypes runs `ef type --offset` once per offset, concurrently.
// A failing query must explain itself on stderr only and yields nil.
func (c lspSmokeCLI) lspSmokeTypes(t *testing.T, path, target string, offsets []int) []map[string]any {
	t.Helper()
	results := make([]map[string]any, len(offsets))
	failures := make([]string, len(offsets))
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	var group sync.WaitGroup
	for index, offset := range offsets {
		group.Add(1)
		go func() {
			defer group.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			stdout, stderr, code, err := lspSmokeRun(lspSmokeCLIDeadline, c.binary, c.cwd, "", "type", path, "--target", target, "--offset", strconv.Itoa(offset))
			switch {
			case err != nil:
				failures[index] = err.Error()
			case code != 0:
				if len(stdout) != 0 || len(stderr) == 0 {
					failures[index] = fmt.Sprintf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
				}
			default:
				if err := json.Unmarshal(stdout, &results[index]); err != nil {
					failures[index] = fmt.Sprintf("%v: %s", err, stdout)
				}
			}
		}()
	}
	group.Wait()
	for index, failure := range failures {
		if failure != "" {
			t.Fatalf("ef type --offset %d: %s", offsets[index], failure)
		}
	}
	return results
}

func lspSmokeNavigation(t *testing.T, cli lspSmokeCLI) {
	directory := t.TempDir()
	path := filepath.Join(directory, "navigation.ef")
	uri := lspSmokeURI(path)
	text := lspSmokeNavigationText
	lspSmokeWrite(t, path, text)
	var named []int
	for _, token := range lspSmokeNamed {
		named = append(named, lspSmokeOffset(t, text, token))
	}
	// The last byte of a token selects the same name as its first.
	for _, token := range lspSmokeNamed {
		named = append(named, lspSmokeOffset(t, text, token)+len(strings.Fields(token.name)[0])-1)
	}
	offsets := append([]int(nil), named...)
	for _, token := range lspSmokeUnnamed {
		offsets = append(offsets, lspSmokeOffset(t, text, token))
	}
	astral := lspSmokeOffset(t, text, lspSmokeToken{`"𐐀é"`, "𐐀"})
	surrogate := lspSmokePosition(t, text, astral)
	surrogate["character"] = surrogate["character"].(int) + 1
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			requests := []map[string]any{lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, text, 5)}
			for index, offset := range offsets {
				requests = append(requests,
					lspSmokeLocated(t, "textDocument/hover", fmt.Sprintf("hover-%d", index), uri, text, offset),
					lspSmokeLocated(t, "textDocument/definition", fmt.Sprintf("definition-%d", index), uri, text, offset))
			}
			requests = append(requests,
				lspSmokeCall("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": surrogate}, "surrogate"),
				lspSmokeCall("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri},
					"position": map[string]any{"line": 99, "character": 0}}, "beyond"),
				lspSmokeCall("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}}, "no-position"),
				lspSmokeLocated(t, "textDocument/hover", "after-refusals", uri, text, named[0]),
				lspSmokeStop, lspSmokeExit)
			messages := cli.exchange(t, lspSmokeSession{target: target, calls: requests})
			replies := lspSmokeReplies(messages)
			reply := func(id string) map[string]any {
				t.Helper()
				value, ok := replies[id]
				if !ok {
					t.Fatalf("no reply %q", id)
				}
				return value
			}
			for _, id := range []string{"surrogate", "beyond", "no-position"} {
				lspSmokeExpectError(t, reply(id), -32602)
			}
			lspSmokeSame(t, lspSmokeResult(t, reply("after-refusals")), lspSmokeResult(t, reply("hover-0")), "hover after refusals")
			arguments := make([]map[string]any, len(offsets))
			for index, offset := range offsets {
				arguments[index] = map[string]any{"file": "navigation.ef", "target": target, "offset": offset}
			}
			remote := cli.mcpTools(t, directory, "code.type", arguments)
			locals := cli.lspSmokeTypes(t, path, target, offsets)
			for index, offset := range offsets {
				local := locals[index]
				var shared map[string]any
				if remote[index]["isError"] != true {
					shared = lspSmokeMap(remote[index]["structuredContent"])
				}
				if (local == nil) != (shared == nil) {
					t.Fatalf("offset %d: CLI %v, MCP %v", offset, local, shared)
				}
				hover, definition := reply(fmt.Sprintf("hover-%d", index)), reply(fmt.Sprintf("definition-%d", index))
				if _, failed := hover["error"]; failed {
					t.Fatalf("offset %d hover refused: %v", offset, hover)
				}
				if _, failed := definition["error"]; failed {
					t.Fatalf("offset %d definition refused: %v", offset, definition)
				}
				var selection map[string]any
				if local != nil {
					lspSmokeSame(t, shared["selection"], local["selection"], fmt.Sprintf("offset %d MCP/CLI selection", offset))
					selection = lspSmokeMap(local["selection"])
				}
				span := lspSmokeMap(selection["span"])
				_, hasTarget := selection["target"]
				names := selection != nil && hasTarget &&
					lspSmokeInt(t, span["offset"]) <= offset && offset < lspSmokeInt(t, span["offset"])+lspSmokeInt(t, span["length"])
				if names != (index < len(named)) {
					t.Fatalf("index %d offset %d names=%v: %v", index, offset, names, selection)
				}
				if !names {
					if lspSmokeResult(t, hover) != nil || lspSmokeResult(t, definition) != nil {
						t.Fatalf("offset %d names nothing yet answered: %v %v", offset, hover, definition)
					}
					continue
				}
				lspSmokeSame(t, lspSmokeResult(t, hover), map[string]any{
					"contents": map[string]any{"kind": "plaintext", "value": selection["presentation"]},
					"range":    lspSmokeSpanRange(t, text, span)}, fmt.Sprintf("offset %d hover", offset))
				declared := lspSmokeMap(selection["target"])
				if declared["locationAvailable"] == true {
					lspSmokeSame(t, lspSmokeResult(t, definition), map[string]any{
						"uri": uri, "range": lspSmokeSpanRange(t, text, lspSmokeMap(declared["span"]))}, fmt.Sprintf("offset %d definition", offset))
				} else if lspSmokeResult(t, definition) != nil || declared["source"] == "source:user" {
					t.Fatalf("offset %d: unlocated target %v answered %v", offset, declared, definition)
				}
			}
			// Bundled targets have no location in this buffer; local ones do.
			for _, key := range []lspSmokeToken{{"Fns.identity", "identity"}, {"Data.Option.Some { value: v }", "Some"}} {
				if result := lspSmokeResult(t, reply(fmt.Sprintf("definition-%d", lspSmokeNamedIndex(t, key.context, key.name)))); result != nil {
					t.Fatalf("bundled %v located: %v", key, result)
				}
			}
			// Every or-pattern binder token is the one joined binding,
			// declared by the first alternative's token.
			firstBinder := lspSmokeRange(t, text, lspSmokeOffset(t, text, lspSmokeToken{"Cold { level: degrees }", "degrees"}), 7)
			for _, key := range []lspSmokeToken{{"Cold { level: degrees }", "degrees"}, {"Hot { level: degrees }", "degrees"}, {"=> degrees", "degrees"}} {
				index := lspSmokeNamedIndex(t, key.context, key.name)
				lspSmokeSame(t, lspSmokeMap(lspSmokeResult(t, reply(fmt.Sprintf("definition-%d", index))))["range"], firstBinder, fmt.Sprintf("%v definition", key))
				hover := lspSmokeMap(lspSmokeResult(t, reply(fmt.Sprintf("hover-%d", index))))
				if value := lspSmokeMap(hover["contents"])["value"]; value != "pattern degrees: i64" {
					t.Fatalf("%v hover = %v", key, hover)
				}
			}
			shadowed := lspSmokeMap(lspSmokeResult(t, reply(fmt.Sprintf("definition-%d", lspSmokeNamedIndex(t, "mark + helper", "helper")))))["range"]
			lspSmokeSame(t, shadowed, lspSmokeRange(t, text, lspSmokeOffset(t, text, lspSmokeToken{"fn shadow(helper", "helper"}), 6), "shadowed parameter definition")
		})
	}
	// The buffer, not the disk file, is navigated: a rename in an unsaved
	// change moves the definition while the file still holds the old text.
	t.Run("unsaved-buffer", func(t *testing.T) {
		t.Parallel()
		renamed := strings.ReplaceAll(text, "helper", "assist")
		use := lspSmokeOffset(t, renamed, lspSmokeToken{"+ assist()", "assist"})
		messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{
			lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, text, 1), lspSmokeChanged(uri, renamed, 2), lspSmokeChanged(uri, text, 2),
			lspSmokeLocated(t, "textDocument/hover", "hover", uri, renamed, use),
			lspSmokeLocated(t, "textDocument/definition", "definition", uri, renamed, use), lspSmokeStop, lspSmokeExit}})
		hover := lspSmokeMap(lspSmokeResult(t, lspSmokeReply(t, messages, "hover")))
		if value := lspSmokeMap(hover["contents"])["value"]; value != "fn assist() -> string" {
			t.Fatalf("hover = %v", hover)
		}
		declared := lspSmokeOffset(t, renamed, lspSmokeToken{"fn assist()", "assist"})
		lspSmokeSame(t, lspSmokeMap(lspSmokeResult(t, lspSmokeReply(t, messages, "definition")))["range"],
			lspSmokeRange(t, renamed, declared, 6), "definition in the unsaved buffer")
		data, err := os.ReadFile(path)
		if err != nil || string(data) != text {
			t.Fatalf("disk file changed: %v", err)
		}
	})
}

// lspSmokeFormatting pairs each source with its failure code: one shared
// formatter answers all three surfaces.
var lspSmokeFormatting = []struct{ name, source, code string }{
	{"required-parameter", `fn mark(required required:string,suffix:string="!")->string{required}`, ""},
	{"astral-crlf", "// 𐐀 note\r\nfn mark() -> string {   \"𐐀é\" }  // 𐐀𐐀 end", ""},
	{"unresolved-import", "import go missing \"example.invalid/no-such-package\"\r\nfn bad() -> string {\r\n  true }\r\n\r\n\r\n", ""},
	{"combining-astral", "fn mark() -> string { \"e\u0301𐐀\" }\n", ""},
	{"empty", "", ""},
	{"whitespace", "  \r\n\t\r\n", ""},
	{"raw-cr", "fn a() -> void {\r void }", "EF001"},
	{"legacy-unit", "fn a() -> () { () }", "EF002"},
}

func lspSmokeFormattingCase(t *testing.T, cli lspSmokeCLI) {
	directory := t.TempDir()
	// Buffers are unsaved: the document path never exists on disk.
	path := filepath.Join(directory, "unsaved-format.ef")
	uri := lspSmokeURI(path)
	t.Cleanup(func() { lspSmokeAbsent(t, path) })
	arguments := make([]map[string]any, len(lspSmokeFormatting))
	for index, c := range lspSmokeFormatting {
		arguments[index] = map[string]any{"source": c.source}
	}
	remote := cli.mcpTools(t, directory, "code.format", arguments)
	stale := "fn stale() -> void {   void }"
	options := map[string]any{"tabSize": 2, "insertSpaces": false}
	formatRequest := func(id string) map[string]any {
		return lspSmokeCall("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": uri}, "options": options}, id)
	}
	for index, c := range lspSmokeFormatting {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code, err := lspSmokeRun(lspSmokeCLIDeadline, cli.binary, cli.cwd, c.source, "fmt", "--stdin")
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"go", "js"} {
				t.Run(target, func(t *testing.T) {
					t.Parallel()
					messages := cli.exchange(t, lspSmokeSession{target: target, calls: []map[string]any{
						lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, stale, 1), lspSmokeChanged(uri, c.source, 3),
						lspSmokeChanged(uri, stale, 2), formatRequest("format"), lspSmokeUnknown("after"), lspSmokeStop, lspSmokeExit}})
					reply := lspSmokeReply(t, messages, "format")
					lspSmokeExpectError(t, lspSmokeReply(t, messages, "after"), -32601)
					if c.code != "" {
						_, detail, found := strings.Cut(string(stderr), "source cannot be formatted: ")
						if !found {
							t.Fatalf("fmt stderr lacks the formatting refusal: %q", stderr)
						}
						detail = strings.TrimSpace(detail)
						if code != 2 || len(stdout) != 0 || !strings.Contains(string(stderr), "EFMT_SYNTAX") {
							t.Fatalf("fmt exit %d stdout=%q stderr=%q", code, stdout, stderr)
						}
						content, _ := remote[index]["content"].([]any)
						if remote[index]["isError"] != true || len(content) == 0 ||
							!strings.Contains(fmt.Sprint(lspSmokeMap(content[0])["text"]), detail) {
							t.Fatalf("MCP refusal lacks %q: %v", detail, remote[index])
						}
						lspSmokeExpectError(t, reply, -32803)
						message, _ := lspSmokeMap(reply["error"])["message"].(string)
						if !strings.Contains(message, fmt.Sprintf("cannot format: %s: %s", c.code, detail)) || !strings.Contains(message, "version 3") {
							t.Fatalf("LSP refusal message: %v", reply)
						}
						return
					}
					if code != 0 {
						t.Fatalf("fmt exit %d stderr=%q", code, stderr)
					}
					formatted := string(stdout)
					shared := lspSmokeMap(remote[index]["structuredContent"])
					if shared["text"] != formatted || shared["changed"] != (formatted != c.source) {
						t.Fatalf("MCP format %v, CLI %q", shared, formatted)
					}
					if shared["formatterVersion"] != "effra/formatter-12" {
						t.Fatalf("formatter version: %v", shared)
					}
					if formatted == c.source {
						lspSmokeSame(t, lspSmokeResult(t, reply), []any{}, "canonical source edits")
					} else {
						whole := map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": lspSmokePosition(t, c.source, len(c.source))}
						lspSmokeSame(t, lspSmokeResult(t, reply), []any{map[string]any{"range": whole, "newText": formatted}}, "whole-document edit")
					}
					// The formatted buffer is already canonical: no edits.
					again := cli.exchange(t, lspSmokeSession{target: target, calls: []map[string]any{
						lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, formatted, 1), formatRequest("again"), lspSmokeStop, lspSmokeExit}})
					lspSmokeSame(t, lspSmokeResult(t, lspSmokeReply(t, again, "again")), []any{}, "formatted buffer edits")
				})
			}
		})
	}
}

func lspSmokeImports(t *testing.T, cli lspSmokeCLI) {
	project := filepath.Join(t.TempDir(), "imports")
	sdk := filepath.Join(project, "sdk")
	if err := os.MkdirAll(sdk, 0o755); err != nil {
		t.Fatal(err)
	}
	lspSmokeWrite(t, filepath.Join(project, "go.mod"), "module example.local/lsp\n\ngo 1.27\n")
	lspSmokeWrite(t, filepath.Join(sdk, "sdk.go"), "package sdk\nfunc Name() string { return \"ok\" }\n")
	uri := lspSmokeURI(filepath.Join(project, "new.ef"))
	text := "import go sdk \"example.local/lsp/sdk\"\neffect fn main() -> string uses {Foreign} { run sdk.Name() }\n"
	t.Run("module-relative", func(t *testing.T) {
		t.Parallel()
		// No .ef file exists and the server starts elsewhere: module
		// resolution must still use the captured document's directory.
		result := lspSmokePublications(cli.exchange(t, lspSmokeSession{calls: []map[string]any{
			lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, text, 1), lspSmokeStop, lspSmokeExit}}))
		if len(result) == 0 {
			t.Fatal("no publication")
		}
		lspSmokeSame(t, result[0]["diagnostics"], []any{}, "module-relative import")
	})
	t.Run("missing-package", func(t *testing.T) {
		t.Parallel()
		missing := "import go sdk \"effra.invalid/lsp-missing\"\n"
		messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{
			lspSmokeInit, lspSmokeReady, lspSmokeChanged(uri, missing, 2), lspSmokeOpened(uri, missing, 1),
			lspSmokeUnknown("after"), lspSmokeStop, lspSmokeExit}})
		if published := lspSmokePublications(messages); len(published) != 0 {
			t.Fatalf("operational failure published: %v", messages)
		}
		if !lspSmokeAnyMessage(messages, "EF111") {
			t.Fatalf("no EF111 log: %v", messages)
		}
		lspSmokeExpectError(t, lspSmokeReply(t, messages, "after"), -32601)
	})
}

func lspSmokeBoundsAndProtocol(t *testing.T, cli lspSmokeCLI) {
	uri := lspSmokeURI(filepath.Join(t.TempDir(), "bounded.ef"))
	run := func(name string, check func(*testing.T)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			check(t)
		})
	}
	run("document-size", func(t *testing.T) {
		maximum := "//" + strings.Repeat("x", 256*1024-2)
		messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, maximum, 1),
			lspSmokeChanged(uri, maximum+"x", 2), lspSmokeChanged(uri, "", 2), lspSmokeStop, lspSmokeExit}})
		versions := []any{}
		for _, publication := range lspSmokePublications(messages) {
			versions = append(versions, publication["version"])
		}
		lspSmokeSame(t, versions, []any{1, 2}, "versions around the text limit")
		if logs := lspSmokeLogs(messages); len(logs) != 1 {
			t.Fatalf("logs = %q, want 1", logs)
		}
	})
	run("finding-limit", func(t *testing.T) {
		messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{lspSmokeInit, lspSmokeReady,
			lspSmokeOpened(uri, strings.Repeat("fn duplicate() -> void { void }\n", 1002), 1),
			lspSmokeChanged(uri, "", 2), lspSmokeStop, lspSmokeExit}})
		versions := []any{}
		for _, publication := range lspSmokePublications(messages) {
			versions = append(versions, publication["version"])
		}
		lspSmokeSame(t, versions, []any{2}, "versions around the finding limit")
		if !lspSmokeAnyMessage(messages, "limit") {
			t.Fatalf("no limit log: %v", messages)
		}
	})
	run("invalid-uris", func(t *testing.T) {
		invalid := []string{"file://remote/tmp/bad.ef", "file:///tmp/bad.ef?query",
			"file:///tmp/a/../bad.ef", "untitled:bad.ef", "file:///tmp/bad%00.ef"}
		calls := []map[string]any{lspSmokeInit, lspSmokeReady}
		for _, invalidURI := range invalid {
			calls = append(calls, lspSmokeOpened(invalidURI, "", 1))
		}
		messages := cli.exchange(t, lspSmokeSession{calls: append(calls, lspSmokeOpened(uri, "", 1), lspSmokeStop, lspSmokeExit)})
		if published := lspSmokePublications(messages); len(published) != 1 {
			t.Fatalf("publications = %v", published)
		}
		if logs := lspSmokeLogs(messages); len(logs) != len(invalid) {
			t.Fatalf("logs = %q, want %d", logs, len(invalid))
		}
	})
	// Invalid body/ID and unsupported method preserve the next frame boundary.
	run("framing-recovery", func(t *testing.T) {
		messages := cli.exchange(t, lspSmokeSession{rawPrefix: []byte("Content-Length: 1\r\n\r\n{"), calls: []map[string]any{
			lspSmokeInit, lspSmokeReady, {"jsonrpc": "2.0", "id": []any{}, "method": "bad"},
			lspSmokeUnknown("after"), lspSmokeStop, lspSmokeExit}})
		lspSmokeExpectError(t, messages[0], -32700)
		invalidRequest := false
		for _, message := range messages {
			invalidRequest = invalidRequest || lspSmokeMap(message["error"])["code"] == float64(-32600)
		}
		if !invalidRequest {
			t.Fatalf("no Invalid Request: %v", messages)
		}
		lspSmokeExpectError(t, lspSmokeReply(t, messages, "after"), -32601)
	})
	run("post-shutdown-request", func(t *testing.T) {
		messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{lspSmokeInit, lspSmokeReady, lspSmokeStop, lspSmokeUnknown("late"), lspSmokeExit}})
		lspSmokeExpectError(t, lspSmokeReply(t, messages, "late"), -32600)
	})
	// EOF after shutdown releases the session cleanly.
	run("eof-after-shutdown", func(t *testing.T) {
		cli.exchange(t, lspSmokeSession{calls: []map[string]any{lspSmokeInit, lspSmokeReady, lspSmokeStop}})
	})
	run("eof-before-shutdown", func(t *testing.T) {
		cli.exchange(t, lspSmokeSession{expected: 1, calls: []map[string]any{lspSmokeInit, lspSmokeReady}})
	})
	run("exit-before-shutdown", func(t *testing.T) {
		cli.exchange(t, lspSmokeSession{expected: 1, calls: []map[string]any{lspSmokeExit}})
	})
	for name, data := range map[string]string{
		"oversized-body":   "Content-Length: 2097153\r\n\r\n",
		"partial-body":     "Content-Length: 2\r\n\r\n{",
		"duplicate-length": "Content-Length: 1\r\nContent-Length: 1\r\n\r\n{",
		"oversized-header": "X: " + strings.Repeat("x", 8192) + "\r\n\r\n",
	} {
		run("terminal-framing/"+name, func(t *testing.T) {
			cli.exchange(t, lspSmokeSession{expected: 1, rawPrefix: []byte(data)})
		})
	}
	run("bad-target", func(t *testing.T) {
		stdout, stderr, code, err := lspSmokeRun(lspSmokeBadTargetTimeout, cli.binary, cli.cwd, "", "lsp", "--target", "bad")
		if err != nil || code != 2 || len(stdout) != 0 || len(stderr) == 0 {
			t.Fatalf("lsp --target bad: exit %d %v stdout=%q stderr=%q", code, err, stdout, stderr)
		}
	})
}

func lspSmokeURIAlias(t *testing.T, cli lspSmokeCLI) {
	path := filepath.Join(t.TempDir(), "c++", "@scope", "counter:one,x=y;z.ef")
	original := lspSmokeURI(path)
	alias := "FILE:" + path
	good := "fn good() -> string { \"good\" }"
	duplicate := lspSmokeOpened(alias, "bad", 2)
	messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{
		lspSmokeInit, lspSmokeReady, lspSmokeOpened(original, "fn bad() -> string { true }", 1),
		duplicate, lspSmokeChanged(alias, good, 2), lspSmokeClosed(alias), duplicate, lspSmokeStop, lspSmokeExit}})
	published := lspSmokePublications(messages)
	uris, versions := []any{}, []any{}
	for _, publication := range published {
		uris = append(uris, publication["uri"])
		versions = append(versions, publication["version"])
	}
	lspSmokeSame(t, uris, []any{original, original, original, alias}, "alias publication URIs")
	lspSmokeSame(t, versions, []any{1, 2, nil, 2}, "alias publication versions")
	if diagnostics, _ := published[0]["diagnostics"].([]any); len(diagnostics) == 0 {
		t.Fatalf("invalid buffer published no diagnostics: %v", published[0])
	}
	lspSmokeSame(t, published[1]["diagnostics"], []any{}, "alias change clears diagnostics")
	if logs := lspSmokeLogs(messages); len(logs) != 1 {
		t.Fatalf("logs = %q, want 1", logs)
	}
	// Existing encoders need not agree on reserved/unreserved bytes or hex
	// case.
	for name, spelling := range map[string]string{
		"lower-hex":          strings.ReplaceAll(original, "%2B", "%2b"),
		"encoded-unreserved": strings.ReplaceAll(original, "counter", "%63ounter"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			published := lspSmokePublications(cli.exchange(t, lspSmokeSession{calls: []map[string]any{
				lspSmokeInit, lspSmokeReady, lspSmokeOpened(spelling, good, 1), lspSmokeStop, lspSmokeExit}}))
			if len(published) == 0 || published[0]["uri"] != spelling {
				t.Fatalf("publication for %q: %v", spelling, published)
			}
		})
	}
}

func lspSmokeOperationalBudget(t *testing.T, cli lspSmokeCLI) {
	project := filepath.Join(t.TempDir(), "large-errors")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	lspSmokeWrite(t, filepath.Join(project, "go.mod"), "module example.local/lsp-errors\n\ngo 1.26\n")
	uri := lspSmokeURI(filepath.Join(project, "unsaved.ef"))
	// A real go-list operational failure has detail exceeding the old output
	// budget, while this captured source stays below the document text limit.
	var text strings.Builder
	for i := range 4000 {
		fmt.Fprintf(&text, "import go absent%d \"example.invalid/absent%04d\"\n", i, i)
	}
	if text.Len() >= 256*1024 {
		t.Fatalf("source is %d bytes", text.Len())
	}
	messages := cli.exchange(t, lspSmokeSession{
		environment: []string{"GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off"},
		calls: []map[string]any{lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, text.String(), 19),
			lspSmokeUnknown("after-error"), lspSmokeClosed(uri), lspSmokeStop, lspSmokeExit}})
	logs := lspSmokeLogs(messages)
	if len(logs) != 1 || !strings.Contains(logs[0], "EF111") || !strings.Contains(logs[0], "version 19") || !strings.Contains(logs[0], uri) {
		t.Fatalf("logs = %.300q", logs)
	}
	if !strings.Contains(logs[0], "bytes omitted") || len(logs[0]) >= 16*1024 {
		t.Fatalf("log of %d bytes is not bounded: %.100q", len(logs[0]), logs[0])
	}
	lspSmokeExpectError(t, lspSmokeReply(t, messages, "after-error"), -32601)
	lspSmokeSame(t, lspSmokePublications(messages), []any{map[string]any{"uri": uri, "diagnostics": []any{}}}, "close publication")
}

func lspSmokeRefusalRecovery(t *testing.T, cli lspSmokeCLI) {
	uri := lspSmokeURI(filepath.Join(t.TempDir(), "recover.ef"))
	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		invalid := []string{"file:///tmp/a%2Fb.ef", "file:///tmp/a%2fb.ef",
			"file:///tmp/a%2Fb c.ef", "file:///tmp/ü%2Fb.ef",
			`file:///tmp/a%2Fb".ef`, "file:///tmp/a%2Fb{.ef",
			"file:///tmp/a/%2e%2e/b.ef", "file:///tmp//b.ef",
			"file:///tmp/%ff.ef", "file:///tmp/" + strings.Repeat("x", 4096) + ".ef"}
		calls := []map[string]any{lspSmokeInit, lspSmokeReady}
		for _, invalidURI := range invalid {
			calls = append(calls, lspSmokeOpened(invalidURI, "", 1))
		}
		messages := cli.exchange(t, lspSmokeSession{calls: append(calls, lspSmokeUnknown("after-refusal"),
			lspSmokeOpened(uri, "", 1), lspSmokeStop, lspSmokeExit)})
		if logs := lspSmokeLogs(messages); len(logs) != len(invalid) {
			t.Fatalf("logs = %d, want %d", len(logs), len(invalid))
		}
		lspSmokeExpectError(t, lspSmokeReply(t, messages, "after-refusal"), -32601)
		if published := lspSmokePublications(messages); len(published) != 1 {
			t.Fatalf("publications = %v", published)
		}
	})
	// Raw editor spellings and one-decode literal escapes remain admitted.
	for name, admitted := range map[string]string{
		"raw-space":      "file:///tmp/a b.ef",
		"raw-unicode":    "file:///tmp/ü.ef",
		"literal-escape": "file:///tmp/%252F.ef",
	} {
		t.Run("admitted/"+name, func(t *testing.T) {
			t.Parallel()
			messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{
				lspSmokeInit, lspSmokeReady, lspSmokeOpened(admitted, "", 1), lspSmokeStop, lspSmokeExit}})
			lspSmokeSame(t, lspSmokePublications(messages), []any{map[string]any{"uri": admitted, "version": 1, "diagnostics": []any{}}}, "admitted publication")
			if logs := lspSmokeLogs(messages); len(logs) != 0 {
				t.Fatalf("admitted URI logged: %q", logs)
			}
		})
	}
	// Reject the encoded separator before decoded-path alias lookup.
	t.Run("encoded-separator", func(t *testing.T) {
		t.Parallel()
		plain, encoded := "file:///tmp/a/b c.ef", "file:///tmp/a%2Fb c.ef"
		messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{
			lspSmokeInit, lspSmokeReady, lspSmokeOpened(plain, "", 1), lspSmokeOpened(encoded, "", 1), lspSmokeStop, lspSmokeExit}})
		lspSmokeSame(t, lspSmokePublications(messages), []any{map[string]any{"uri": plain, "version": 1, "diagnostics": []any{}}}, "plain publication")
		logs := lspSmokeLogs(messages)
		if len(logs) != 1 || !strings.Contains(logs[0], "must not encode path separators") || strings.Contains(logs[0], "duplicate") {
			t.Fatalf("logs = %q", logs)
		}
	})
	// Located findings remain all-or-refuse. Refusing this projection must
	// still permit close and a following request on the same framed session.
	t.Run("publication-refusal", func(t *testing.T) {
		t.Parallel()
		text := strings.Repeat(fmt.Sprintf("fn %s() -> void { void }\n", strings.Repeat("d", 200)), 1001)
		if len(text) >= 256*1024 {
			t.Fatalf("source is %d bytes", len(text))
		}
		messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{
			lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, text, 23), lspSmokeClosed(uri),
			lspSmokeUnknown("after-publication"), lspSmokeStop, lspSmokeExit}})
		logs := lspSmokeLogs(messages)
		if len(logs) != 1 || !strings.Contains(logs[0], "output") || !strings.Contains(logs[0], "version 23") {
			t.Fatalf("logs = %.300q", logs)
		}
		lspSmokeSame(t, lspSmokePublications(messages), []any{map[string]any{"uri": uri, "diagnostics": []any{}}}, "close publication")
		lspSmokeExpectError(t, lspSmokeReply(t, messages, "after-publication"), -32601)
	})
}

func lspSmokeOversizedURI(t *testing.T, cli lspSmokeCLI) {
	uri := "file:///nonexistent/loop-probe-x/" + strings.Repeat("x", 361837) + ".ef"
	messages := cli.exchange(t, lspSmokeSession{calls: []map[string]any{
		lspSmokeInit, lspSmokeReady, lspSmokeOpened(uri, "", 1), lspSmokeClosed(uri), lspSmokeUnknown("after-huge-uri"),
		lspSmokeOpened(lspSmokeURI(filepath.Join(t.TempDir(), "ordinary.ef")), "", 1), lspSmokeStop, lspSmokeExit}})
	if logs := lspSmokeLogs(messages); len(logs) != 2 {
		t.Fatalf("logs = %d, want 2", len(logs))
	}
	if published := lspSmokePublications(messages); len(published) != 1 {
		t.Fatalf("publications = %d, want 1", len(published))
	}
	lspSmokeExpectError(t, lspSmokeReply(t, messages, "after-huge-uri"), -32601)
}
