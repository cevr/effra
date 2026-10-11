package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// endpointOriginSpans returns the derive name span and the full declaration
// line extent of an endpoint or route table, as JSON spans.
func endpointOriginSpans(t *testing.T, source, name string) (map[string]any, map[string]any) {
	t.Helper()
	return codecOriginSpanJSON(t, source, name)
}

// endpointOriginTarget checks that a selection's target is the synthesized
// handler function of name, located at its derive declaration, with the
// synthetic request parameter unlocated.
func endpointOriginTarget(t *testing.T, response map[string]any, source, name, kind string) {
	t.Helper()
	selection, ok := response["selection"].(map[string]any)
	if !ok || selection["kind"] != kind {
		t.Fatalf("%s selection: %v", name, response["selection"])
	}
	target, ok := selection["target"].(map[string]any)
	if !ok || target["kind"] != "function" || target["name"] != name || target["identity"] != "function:module:file:module:"+name || target["locationAvailable"] != true {
		t.Fatalf("%s target: %v", name, selection["target"])
	}
	span, extent := endpointOriginSpans(t, source, name)
	if !sameJSONValue(target["span"], span) || !sameJSONValue(target["extent"], extent) {
		t.Fatalf("%s origin=%v want span=%v extent=%v", name, target, span, extent)
	}
	parameters := target["callable"].(map[string]any)["parameters"].([]any)
	if len(parameters) != 1 || !sameJSONValue(parameters[0].(map[string]any)["span"], codecZeroSpanJSON()) {
		t.Fatalf("%s synthetic parameter location was fabricated: %v", name, parameters)
	}
	if kind == "declaration" && (!sameJSONValue(selection["span"], span) || !sameJSONValue(selection["extent"], extent)) {
		t.Fatalf("%s declaration selection=%v", name, selection)
	}
}

// An endpoint's or route table's derive name and every call of its
// synthesized handler resolve to that derive declaration, identically
// through the CLI on both targets and MCP, and as LSP definitions over an
// unsaved buffer; an ordinary function call is the control.
func TestEndpointOriginAcrossCLIMCPAndLSP(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "http-endpoints.ef"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if err := os.WriteFile(filepath.Join(root, "endpoints.ef"), source, 0600); err != nil {
		t.Fatal(err)
	}
	type probe struct {
		name, kind string
		offset     int
	}
	probes := []probe{
		{"placeOrder", "declaration", strings.Index(text, "derive placeOrder") + len("derive ")},
		{"api", "declaration", strings.Index(text, "derive api") + len("derive ")},
		{"api", "expression", strings.Index(text, "run api(request)") + len("run ")},
	}
	cli := []map[string]any{}
	for _, target := range []string{"go", "js"} {
		for _, p := range probes {
			stdout, stderr, code := runTestCLI(t, binary, "type", filepath.Join(root, "endpoints.ef"), "--offset", strconv.Itoa(p.offset), "--target", target, "--json")
			if code != 0 || len(stderr) != 0 {
				t.Fatalf("%s %s query: code=%d stderr=%q", target, p.name, code, stderr)
			}
			response := readProcessJSON(t, stdout)
			endpointOriginTarget(t, response, text, p.name, p.kind)
			if target == "go" {
				cli = append(cli, response)
			}
		}
	}
	requests := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "endpoint-origin", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
	}
	for i, p := range probes {
		requests = append(requests, map[string]any{"jsonrpc": "2.0", "id": i + 2, "method": "tools/call", "params": map[string]any{"name": "code.type", "arguments": map[string]any{"file": "endpoints.ef", "offset": p.offset, "target": "go"}}})
	}
	var input bytes.Buffer
	for _, request := range requests {
		if err := json.NewEncoder(&input).Encode(request); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := runTestCLIDir(t, binary, root, input.String(), "mcp", root)
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("MCP endpoint origin: code=%d stderr=%q", code, stderr)
	}
	lines := bytes.Split(bytes.TrimSpace(stdout), []byte{'\n'})
	if len(lines) != len(probes)+1 {
		t.Fatalf("MCP response count %d\n%s", len(lines), stdout)
	}
	for i, p := range probes {
		mcp := readProcessJSON(t, lines[i+1])["result"].(map[string]any)["structuredContent"].(map[string]any)
		endpointOriginTarget(t, mcp, text, p.name, p.kind)
		for _, key := range []string{"selection", "types", "rows", "revision"} {
			if !sameJSONValue(cli[i][key], mcp[key]) {
				t.Fatalf("CLI/MCP %s differ for %s: CLI=%v MCP=%v", key, p.name, cli[i][key], mcp[key])
			}
		}
	}

	// LSP: an unsaved CRLF buffer with an astral prefix.
	buffer := "// 😀 unsaved\r\n" + strings.ReplaceAll(text, "\n", "\r\n")
	uri := "file:///tmp/effra-endpoint-origin-unsaved.ef"
	call := strings.Index(buffer, "run api(request)") + len("run ")
	derive := strings.Index(buffer, "derive placeOrder") + len("derive ")
	ordinary := strings.Index(buffer, "status(run api(")
	lspRequests := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "initialized"},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "effra", "version": 3, "text": buffer}}},
		map[string]any{"jsonrpc": "2.0", "id": "call", "method": "textDocument/definition", "params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": codecOriginLSPPosition(buffer, call)}},
		map[string]any{"jsonrpc": "2.0", "id": "derive", "method": "textDocument/definition", "params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": codecOriginLSPPosition(buffer, derive)}},
		map[string]any{"jsonrpc": "2.0", "id": "ordinary", "method": "textDocument/definition", "params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": codecOriginLSPPosition(buffer, ordinary)}},
		map[string]any{"jsonrpc": "2.0", "id": 9, "method": "shutdown"},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	}
	input.Reset()
	for _, request := range lspRequests {
		input.Write(voidLSPFrame(t, request))
	}
	stdout, stderr, code = runTestCLIInput(t, binary, input.String(), "lsp", "--target", "go")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("LSP endpoint origin: code=%d stderr=%q", code, stderr)
	}
	byID := map[string]map[string]any{}
	for _, message := range readVoidLSPFrames(t, stdout) {
		if id, ok := message["id"].(string); ok {
			byID[id] = message
		}
	}
	want := map[string]map[string]any{
		"call":     codecOriginLSPRange(buffer, strings.Index(buffer, "derive api")+len("derive "), len("api")),
		"derive":   codecOriginLSPRange(buffer, derive, len("placeOrder")),
		"ordinary": codecOriginLSPRange(buffer, strings.Index(buffer, "fn status(")+len("fn "), len("status")),
	}
	for id, expected := range want {
		result, ok := byID[id]["result"].(map[string]any)
		if !ok || result["uri"] != uri || !sameJSONValue(result["range"], expected) {
			t.Fatalf("%s definition=%v want %v", id, byID[id], expected)
		}
	}
}
