package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
)

const publicVoidSource = `import Data "effra/data"
record Holder<T: type> { callback: fn() -> T }
fn pure() -> void { void }
fn selected() -> Holder<void> { Holder<void> { callback: pure } }
fn present() -> Data.Option<void> { Data.Option.Some { value: void } }
fn absent() -> Data.Option<void> { Data.Option<void>.None {} }
effect fn test_void() -> void { void }
effect fn main() -> void { pure(); void }
`

func assertVoidProjection(t *testing.T, response map[string]any) {
	t.Helper()
	if response["schemaVersion"] != float64(compiler.SemanticSchemaVersion) || response["checked"] != true || response["typeProjectionComplete"] != true {
		t.Fatalf("public response lost the admitted void contract: %v", response)
	}
	types, ok := response["types"].([]any)
	if !ok {
		t.Fatalf("public response omitted its type projection: %v", response)
	}
	for _, raw := range types {
		node, ok := raw.(map[string]any)
		if ok && node["kind"] == "primitive" && node["name"] == "void" {
			return
		}
	}
	t.Fatalf("public type projection has no canonical void primitive: %v", response)
}

func assertVoidDiagnostic(t *testing.T, response map[string]any, offset int, message string) {
	t.Helper()
	if response["checked"] != false {
		t.Fatalf("legacy no-value syntax was presented as checked: %v", response)
	}
	diagnostics, ok := response["diagnostics"].([]any)
	if !ok || len(diagnostics) == 0 {
		t.Fatalf("legacy no-value syntax omitted diagnostics: %v", response)
	}
	for _, raw := range diagnostics {
		finding, ok := raw.(map[string]any)
		if !ok || finding["code"] != "EF002" {
			continue
		}
		if !strings.Contains(finding["message"].(string), message) {
			t.Fatalf("wrong legacy diagnostic message: %v", finding)
		}
		span, ok := finding["span"].(map[string]any)
		if !ok || span["offset"] != float64(offset) || span["length"] != float64(2) {
			t.Fatalf("legacy diagnostic lost its exact UTF-8 span: %v", finding)
		}
		return
	}
	t.Fatalf("legacy syntax did not produce focused EF002: %v", diagnostics)
}

func TestVoidContractsAcrossCLIAndMCPProcesses(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	file := filepath.Join(root, "main.ef")
	if err := os.WriteFile(file, []byte(publicVoidSource), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		stdout, stderr, code := runTestCLI(t, binary, "check", file, "--target", target)
		if code != 0 || len(stderr) != 0 {
			t.Fatalf("%s CLI check rejected canonical void source: code=%d stderr=%q", target, code, stderr)
		}
		response := readProcessJSON(t, stdout)
		assertVoidProjection(t, response)
		if response["producerIdentity"] != compiler.SemanticProducerIdentity {
			t.Fatalf("%s CLI check lost the versioned semantic producer: %v", target, response["producerIdentity"])
		}
	}

	stdout, stderr, code := runTestCLI(t, binary, "inspect", file, "pure")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("CLI inspection of pure void function failed: code=%d stderr=%q", code, stderr)
	}
	cliInspection := readProcessJSON(t, stdout)
	assertVoidProjection(t, cliInspection)
	assertResponseReferences(t, cliInspection)

	legacyType := filepath.Join(root, "legacy-type.ef")
	legacyTypeSource := "fn bad() -> () { void }\n"
	if err := os.WriteFile(legacyType, []byte(legacyTypeSource), 0600); err != nil {
		t.Fatal(err)
	}
	legacyLiteral := filepath.Join(root, "legacy.ef")
	legacyLiteralSource := "fn bad() -> void { () }\n"
	if err := os.WriteFile(legacyLiteral, []byte(legacyLiteralSource), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runTestCLI(t, binary, "diagnostics", legacyType, "--json")
	if code != 1 || !strings.Contains(string(stderr), "diagnostics failed policy") {
		t.Fatalf("CLI did not return a source report for the old type spelling: code=%d stderr=%q", code, stderr)
	}
	var legacyReport map[string]any
	if err := json.Unmarshal(stdout, &legacyReport); err != nil {
		t.Fatalf("CLI diagnostic report was not JSON: %v\n%s", err, stdout)
	}
	if legacyReport["schemaVersion"] != float64(compiler.DiagnosticReportSchemaVersion) {
		t.Fatalf("diagnostic transport schema changed unexpectedly: %v", legacyReport["schemaVersion"])
	}
	assertVoidDiagnostic(t, legacyReport, strings.Index(legacyTypeSource, "-> ()")+3, "no-value type")

	formatSource := "fn pure()->void{void}\n"
	formatPath := filepath.Join(root, "format.ef")
	if err := os.WriteFile(formatPath, []byte(formatSource), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runTestCLI(t, binary, "fmt", "--json", formatPath)
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("CLI formatter rejected canonical void source: code=%d stderr=%q", code, stderr)
	}
	formatReport := readProcessJSON(t, stdout)
	if formatReport["schemaVersion"] != float64(compiler.FormatterSchemaVersion) || formatReport["formatterVersion"] != compiler.FormatterIdentity {
		t.Fatalf("CLI formatter did not expose formatter-5: %v", formatReport)
	}

	formatArguments := map[string]any{"source": formatSource}
	requests := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "void-process-test", "version": "1"}}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "main.ef", "target": "go"}}},
		map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "code.inspect", "arguments": map[string]any{"file": "main.ef", "symbol": "pure", "target": "go"}}},
		map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "code.format", "arguments": formatArguments}},
		map[string]any{"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "legacy.ef"}}},
		map[string]any{"jsonrpc": "2.0", "id": 6, "method": "ping"},
	}
	var input bytes.Buffer
	for _, request := range requests {
		if err := json.NewEncoder(&input).Encode(request); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(binary, "mcp", root)
	command.Stdin = &input
	mcpOutput, err := command.Output()
	if err != nil {
		t.Fatalf("MCP void process failed: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(mcpOutput), []byte{'\n'})
	if len(lines) != 6 {
		t.Fatalf("MCP void process response count changed: %d\n%s", len(lines), mcpOutput)
	}
	mcpCheck := readProcessJSON(t, lines[1])["result"].(map[string]any)["structuredContent"].(map[string]any)
	assertVoidProjection(t, mcpCheck)
	if mcpCheck["producerIdentity"] != compiler.SemanticProducerIdentity {
		t.Fatalf("MCP check lost the versioned semantic producer: %v", mcpCheck["producerIdentity"])
	}
	mcpInspection := readProcessJSON(t, lines[2])["result"].(map[string]any)["structuredContent"].(map[string]any)
	assertVoidProjection(t, mcpInspection)
	assertResponseReferences(t, mcpInspection)
	for _, key := range []string{"symbol", "types", "rows", "revision"} {
		if !sameJSONValue(cliInspection[key], mcpInspection[key]) {
			t.Fatalf("CLI/MCP void contract drifted in %s: CLI=%v MCP=%v", key, cliInspection[key], mcpInspection[key])
		}
	}
	mcpFormat := readProcessJSON(t, lines[3])["result"].(map[string]any)["structuredContent"].(map[string]any)
	if mcpFormat["schemaVersion"] != float64(compiler.FormatterSchemaVersion) || mcpFormat["formatterVersion"] != compiler.FormatterIdentity || !strings.Contains(mcpFormat["text"].(string), "void") {
		t.Fatalf("MCP formatter did not expose canonical void/versioned output: %v", mcpFormat)
	}
	mcpLegacy := readProcessJSON(t, lines[4])["result"].(map[string]any)["structuredContent"].(map[string]any)
	assertVoidDiagnostic(t, mcpLegacy, strings.Index(legacyLiteralSource, "{ () }")+2, "no-value expression")
	if ping := readProcessJSON(t, lines[5]); ping["error"] != nil || ping["result"] == nil {
		t.Fatalf("MCP process did not remain framed after old spelling: %v", ping)
	}
}

func sameJSONValue(left, right any) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func TestVoidContractsAcrossFramedLSPAndBackendProcesses(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	file := filepath.Join(root, "main.ef")
	if err := os.WriteFile(file, []byte(publicVoidSource), 0600); err != nil {
		t.Fatal(err)
	}
	oldSource := `fn bad() -> void { if true { "😀" } else { () } }
`
	validSource := "fn good() -> void { void }\n"
	requests := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "initialized"},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": "file:///tmp/void-process.ef", "languageId": "effra", "version": 7, "text": oldSource}}},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{"textDocument": map[string]any{"uri": "file:///tmp/void-process.ef", "version": 8}, "contentChanges": []any{map[string]any{"text": validSource}}}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	}
	var input bytes.Buffer
	for _, request := range requests {
		frame := voidLSPFrame(t, request)
		_, _ = input.Write(frame)
	}
	stdout, stderr, code := runTestCLIInput(t, binary, input.String(), "lsp", "--target", "go")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("framed LSP void process failed: code=%d stderr=%q", code, stderr)
	}
	messages := readVoidLSPFrames(t, stdout)
	if len(messages) != 4 {
		t.Fatalf("framed LSP response count changed: %d\n%s", len(messages), stdout)
	}
	oldPublication := messages[1]["params"].(map[string]any)
	if oldPublication["version"] != float64(7) {
		t.Fatalf("old spelling publication lost document version: %v", oldPublication)
	}
	diagnostics := oldPublication["diagnostics"].([]any)
	if len(diagnostics) != 1 {
		t.Fatalf("old spelling publication changed diagnostic cardinality: %v", diagnostics)
	}
	finding := diagnostics[0].(map[string]any)
	if finding["code"] != "EF002" || !strings.Contains(finding["message"].(string), "no-value expression") {
		t.Fatalf("framed LSP did not expose the focused old-spelling diagnostic: %v", finding)
	}
	if got := finding["range"].(map[string]any); !sameJSONValue(got, map[string]any{
		// The astral emoji occupies two UTF-16 code units, so `()` spans 43..45.
		"start": map[string]any{"line": float64(0), "character": float64(43)},
		"end":   map[string]any{"line": float64(0), "character": float64(45)},
	}) {
		t.Fatalf("framed LSP range lost UTF-16 coordinates after astral text: %v want 0:43..0:45", got)
	}
	validPublication := messages[2]["params"].(map[string]any)
	if validPublication["version"] != float64(8) || len(validPublication["diagnostics"].([]any)) != 0 {
		t.Fatalf("valid void change did not clear old diagnostics at version 8: %v", validPublication)
	}
}

func voidLSPFrame(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))), body...)
}

func readVoidLSPFrames(t *testing.T, output []byte) []map[string]any {
	t.Helper()
	reader := bufio.NewReader(bytes.NewReader(output))
	var messages []map[string]any
	for {
		header, err := reader.ReadString('\n')
		if err == io.EOF && len(header) == 0 {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(header, "Content-Length: ") || !strings.HasSuffix(header, "\r\n") {
			t.Fatalf("invalid LSP response header %q", header)
		}
		length, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(header, "Content-Length: "), "\r\n"))
		if err != nil || length < 0 {
			t.Fatalf("invalid LSP response length %q", header)
		}
		blank, err := reader.ReadString('\n')
		if err != nil || blank != "\r\n" {
			t.Fatalf("invalid LSP response separator %q: %v", blank, err)
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatal(err)
		}
		var message map[string]any
		if err := json.Unmarshal(body, &message); err != nil {
			t.Fatalf("invalid LSP response JSON: %v\n%s", err, body)
		}
		messages = append(messages, message)
	}
	return messages
}

func TestVoidBackendsAndTestRunnerRemainSilentAndVersioned(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	file := filepath.Join(root, "main.ef")
	if err := os.WriteFile(file, []byte(publicVoidSource), 0600); err != nil {
		t.Fatal(err)
	}
	modules, err := filepath.Abs("../../node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		stdout, stderr, code := runTestCLIDir(t, binary, root, "", "run", file, "--target", target)
		if code != 0 || len(stdout) != 0 || len(stderr) != 0 {
			t.Fatalf("%s void entry was not silent: code=%d stdout=%q stderr=%q", target, code, stdout, stderr)
		}
		stdout, stderr, code = runTestCLIDir(t, binary, root, "", "test", file, "--target", target)
		if code != 0 || len(stderr) != 0 {
			t.Fatalf("%s void test runner failed: code=%d stdout=%q stderr=%q", target, code, stdout, stderr)
		}
		var report struct {
			Passed bool `json:"passed"`
			Tests  []struct {
				Name   string `json:"name"`
				Passed bool   `json:"passed"`
			} `json:"tests"`
			Target string `json:"target"`
		}
		if err := json.Unmarshal(stdout, &report); err != nil || !report.Passed || report.Target != target || len(report.Tests) != 1 || report.Tests[0].Name != "test_void" || !report.Tests[0].Passed {
			t.Fatalf("%s void test report was not complete: err=%v report=%+v stdout=%q", target, err, report, stdout)
		}
	}

	output := filepath.Join(root, "dist", "void.mjs")
	stdout, stderr, code := runTestCLIDir(t, binary, root, "", "build", file, "--target", "js", "-o", output, "--entry")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("JavaScript void library build failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	declaration, err := os.ReadFile(strings.TrimSuffix(output, ".mjs") + ".d.mts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(declaration), "void") || !strings.Contains(string(declaration), "Effect.Effect<void") {
		t.Fatalf("JavaScript declaration did not retain ordinary/effect void contracts:\n%s", declaration)
	}

	goFile := filepath.Join(root, "error-only.ef")
	goSource := `import go os "os"
effect fn main() -> void raises {GoError} { run os.Chdir(".").orFail().provide<Foreign>(Host) }
`
	if err := os.WriteFile(goFile, []byte(goSource), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runTestCLIDir(t, binary, root, "", "check", goFile, "--target", "go")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("Go error-only import was not admitted as a void contract: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runTestCLIDir(t, binary, root, "", "run", goFile, "--target", "go")
	if code != 0 || len(stdout) != 0 || len(stderr) != 0 {
		t.Fatalf("Go error-only void wrapper did not execute: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
