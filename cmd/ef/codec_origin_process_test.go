package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

const codecOriginProcessInvoiceSource = `import Json "effra/json"

error Rejected
service Policy {
    effect fn suffix() -> string raises { Rejected }
}
impl Allow for Policy {
    effect fn suffix() -> string raises { Rejected } { "!" }
}
record Invoice { id: i64, memo: string }
record ArchivedInvoice { id: i64, memo: string }
derive Object = Json.codec<Invoice>(maxDepth: 1, maxBodyBytes: 128)
derive mirror = Json.codec<Invoice>(maxBodyBytes: 128, maxDepth: 1)
derive tiny = Json.codec<Invoice>(maxBodyBytes: 16, maxDepth: 1)
derive archive = Json.codec<ArchivedInvoice>(maxBodyBytes: 128, maxDepth: 1)

effect fn decorate(value: Invoice) -> Invoice raises { Rejected } uses { Policy } {
    let suffix = run Policy.suffix()
    Invoice { id: value.id, memo: value.memo + suffix }
}
effect fn process(body: string) -> string raises { JsonDecodeFailure, Rejected, JsonEncodeFailure } uses { Policy } {
    let input = run body |> Object.decode()
    let decorated = run input |> decorate()
    run decorated |> mirror.encode()
}
effect fn named(body: string) -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    let value = run Object.decode(input: body)
    run mirror.encode(value: value)
}
effect fn main() -> string raises { JsonDecodeFailure, Rejected, JsonEncodeFailure } {
    run process(body: "{\"id\":\"0007\",\"memo\":\"paid\",\"extra\":true}").provide<Policy>(Allow)
}
`

const codecOriginProcessSettingsSource = `import Json "effra/json"
record Window { title: string, visible: bool }
enum Mode { Idle, Ready { window: Window, marker: void } }
record State { mode: Mode, revision: i64 }
record Unrelated { id: string }
derive Effect = Json.codec<State>(maxDepth: 3, maxBodyBytes: 160)
derive spare = Json.codec<Unrelated>(maxDepth: 1, maxBodyBytes: 64)
fn closed() -> Unrelated { Unrelated { id: "unused" } }
effect fn main() -> string raises { JsonEncodeFailure } {
    let state = State { mode: Mode.Ready { window: Window { title: "restored", visible: true }, marker: void }, revision: 9 }
    run state |> Effect.encode()
}
`

func codecOriginSpanJSON(t *testing.T, source, witness string) (map[string]any, map[string]any) {
	t.Helper()
	start := strings.Index(source, "derive "+witness+" =")
	if start < 0 {
		t.Fatalf("derive %s not found", witness)
	}
	nameOffset := start + len("derive ")
	lineEnd := strings.IndexByte(source[start:], '\n')
	if lineEnd < 0 {
		lineEnd = len(source) - start
	}
	position := func(offset int) (int, int) {
		line, column := 1, 1
		for _, ch := range source[:offset] {
			if ch == '\n' {
				line++
				column = 1
			} else {
				column++
			}
		}
		return line, column
	}
	nameLine, nameColumn := position(nameOffset)
	extentLine, extentColumn := position(start)
	return map[string]any{"offset": float64(nameOffset), "length": float64(len(witness)), "line": float64(nameLine), "column": float64(nameColumn)}, map[string]any{"offset": float64(start), "length": float64(lineEnd), "line": float64(extentLine), "column": float64(extentColumn)}
}

func codecOriginSelection(t *testing.T, response map[string]any, source, witness, direction string) map[string]any {
	t.Helper()
	selection, ok := response["selection"].(map[string]any)
	if !ok {
		t.Fatalf("selection missing: %v", response)
	}
	target, ok := selection["target"].(map[string]any)
	if !ok || target["kind"] != "function" || target["name"] != witness+"."+direction || target["locationAvailable"] != true {
		t.Fatalf("direction target: %v", selection)
	}
	nameSpan, extent := codecOriginSpanJSON(t, source, witness)
	if !sameJSONValue(target["span"], nameSpan) || !sameJSONValue(target["extent"], extent) {
		t.Fatalf("direction origin=%v want span=%v extent=%v", target, nameSpan, extent)
	}
	callable := target["callable"].(map[string]any)
	parameters := callable["parameters"].([]any)
	if len(parameters) != 1 || !sameJSONValue(parameters[0].(map[string]any)["span"], codecZeroSpanJSON()) {
		t.Fatalf("synthetic parameter location was fabricated: %v", callable)
	}
	return selection
}

func codecZeroSpanJSON() map[string]any {
	return map[string]any{"offset": float64(0), "length": float64(0), "line": float64(0), "column": float64(0)}
}

func TestCodecOriginAcrossCLIAndMCPProcesses(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	files := map[string]string{"invoice.ef": codecOriginProcessInvoiceSource, "settings.ef": codecOriginProcessSettingsSource}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	type probe struct {
		file, source, witness, direction, needle string
	}
	probes := []probe{
		{file: "invoice.ef", source: codecOriginProcessInvoiceSource, witness: "Object", direction: "decode", needle: "Object.decode(input:"},
		{file: "settings.ef", source: codecOriginProcessSettingsSource, witness: "Effect", direction: "encode", needle: "Effect.encode()"},
	}
	cli := map[string]map[string]any{}
	for _, target := range []string{"go", "js"} {
		for _, p := range probes {
			offset := strings.Index(p.source, p.needle) + len(p.witness) + 1
			stdout, stderr, code := runTestCLI(t, binary, "type", filepath.Join(root, p.file), "--offset", strconv.Itoa(offset), "--target", target, "--json")
			if code != 0 || len(stderr) != 0 {
				t.Fatalf("%s %s type query failed: code=%d stderr=%q", target, p.file, code, stderr)
			}
			response := readProcessJSON(t, stdout)
			key := target + ":" + p.file
			cli[key] = response
			codecOriginSelection(t, response, p.source, p.witness, p.direction)
		}
	}

	requests := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "codec-origin", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "code.type", "arguments": map[string]any{"file": "invoice.ef", "offset": strings.Index(codecOriginProcessInvoiceSource, "Object.decode(input:") + len("Object."), "target": "go"}}},
		{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "code.type", "arguments": map[string]any{"file": "settings.ef", "offset": strings.Index(codecOriginProcessSettingsSource, "Effect.encode()") + len("Effect."), "target": "go"}}},
		{"jsonrpc": "2.0", "id": 4, "method": "ping"},
	}
	var input bytes.Buffer
	for _, request := range requests {
		if err := json.NewEncoder(&input).Encode(request); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := runTestCLIDir(t, binary, root, input.String(), "mcp", root)
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("MCP codec origin process failed: code=%d stderr=%q", code, stderr)
	}
	lines := bytes.Split(bytes.TrimSpace(stdout), []byte{'\n'})
	if len(lines) != 4 {
		t.Fatalf("MCP response count changed: %d\n%s", len(lines), stdout)
	}
	for i, p := range probes {
		mcpEnvelope := readProcessJSON(t, lines[i+1])
		mcp := mcpEnvelope["result"].(map[string]any)["structuredContent"].(map[string]any)
		codecOriginSelection(t, mcp, p.source, p.witness, p.direction)
		cliResponse := cli["go:"+p.file]
		for _, key := range []string{"selection", "types", "rows", "declarations", "revision"} {
			if !sameJSONValue(cliResponse[key], mcp[key]) {
				t.Fatalf("CLI/MCP %s origin mismatch in %s: CLI=%v MCP=%v", key, p.file, cliResponse[key], mcp[key])
			}
		}
	}
	if ping := readProcessJSON(t, lines[3]); ping["error"] != nil || ping["result"] == nil {
		t.Fatalf("MCP process did not remain framed: %v", ping)
	}
}

func codecOriginLSPPosition(source string, offset int) map[string]any {
	normalized := strings.ReplaceAll(strings.ReplaceAll(source[:offset], "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	return map[string]any{"line": len(lines) - 1, "character": len(utf16.Encode([]rune(lines[len(lines)-1])))}
}

func TestCodecOriginFramedLSPUsesUnsavedUTF16Buffer(t *testing.T) {
	binary := buildTestCLI(t)
	// The open document is intentionally unsaved and has CRLF plus an astral
	// prefix. Definition ranges must still point at the real derive/function
	// names in this accepted snapshot.
	source := "// 😀 unsaved\r\n" + strings.ReplaceAll(codecOriginProcessInvoiceSource, "\n", "\r\n")
	uri := "file:///tmp/effra-codec-origin-unsaved.ef"
	decode := strings.Index(source, "Object.decode(input:") + len("Object.")
	derive := strings.Index(source, "derive Object") + len("derive ")
	ordinary := strings.LastIndex(source, "process(body:")
	requests := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "initialized"},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "effra", "version": 7, "text": source}}},
		map[string]any{"jsonrpc": "2.0", "id": "decode", "method": "textDocument/definition", "params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": codecOriginLSPPosition(source, decode)}},
		map[string]any{"jsonrpc": "2.0", "id": "derive", "method": "textDocument/definition", "params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": codecOriginLSPPosition(source, derive)}},
		map[string]any{"jsonrpc": "2.0", "id": "ordinary", "method": "textDocument/definition", "params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": codecOriginLSPPosition(source, ordinary)}},
		map[string]any{"jsonrpc": "2.0", "id": 9, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	}
	var input bytes.Buffer
	for _, request := range requests {
		input.Write(voidLSPFrame(t, request))
	}
	stdout, stderr, code := runTestCLIInput(t, binary, input.String(), "lsp", "--target", "go")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("framed LSP codec origin process failed: code=%d stderr=%q", code, stderr)
	}
	byID := map[string]map[string]any{}
	for _, message := range readVoidLSPFrames(t, stdout) {
		if id, ok := message["id"].(string); ok {
			byID[id] = message
		}
	}
	for _, id := range []string{"decode", "derive", "ordinary"} {
		if byID[id]["error"] != nil {
			t.Fatalf("%s definition failed: %v", id, byID[id])
		}
	}
	decodeDefinition := byID["decode"]["result"].(map[string]any)
	deriveDefinition := byID["derive"]["result"].(map[string]any)
	ordinaryDefinition := byID["ordinary"]["result"].(map[string]any)
	if decodeDefinition["uri"] != uri || deriveDefinition["uri"] != uri || ordinaryDefinition["uri"] != uri {
		t.Fatalf("definition URI changed for unsaved buffer: %v %v %v", decodeDefinition, deriveDefinition, ordinaryDefinition)
	}
	if !sameJSONValue(decodeDefinition["range"], codecOriginLSPRange(source, strings.Index(source, "derive Object")+len("derive "), len("Object"))) {
		t.Fatalf("decode origin range=%v", decodeDefinition["range"])
	}
	if !sameJSONValue(deriveDefinition["range"], codecOriginLSPRange(source, derive, len("Object"))) {
		t.Fatalf("derive range=%v", deriveDefinition["range"])
	}
	ordinaryDeclaration := strings.LastIndex(source[:strings.LastIndex(source, "process(body:")], "effect fn process") + len("effect fn ")
	if !sameJSONValue(ordinaryDefinition["range"], codecOriginLSPRange(source, ordinaryDeclaration, len("process"))) {
		t.Fatalf("ordinary range=%v", ordinaryDefinition["range"])
	}
}

func codecOriginLSPRange(source string, offset, length int) map[string]any {
	return map[string]any{"start": codecOriginLSPPosition(source, offset), "end": codecOriginLSPPosition(source, offset+length)}
}
