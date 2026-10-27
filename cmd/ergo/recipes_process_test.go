package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestRecipeDataCLIAndMCPParity(t *testing.T) {
	binary := buildTestCLI(t)
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "recipes-queue.ef"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	file := filepath.Join(root, "queue.ef")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runTestCLI(t, binary, "check", file)
	if code != 0 {
		t.Fatalf("recipe queue check: %s", stderr)
	}
	checked := readProcessJSON(t, stdout)
	if checked["checked"] != true || checked["typeProjectionComplete"] != true {
		t.Fatalf("recipe queue publication: %v", checked)
	}
	assertResponseReferences(t, checked)
	stdout, stderr, code = runTestCLI(t, binary, "inspect", file, "delivery")
	if code != 0 {
		t.Fatalf("recipe factory inspection: %s", stderr)
	}
	inspect := readProcessJSON(t, stdout)
	assertResponseReferences(t, inspect)
	result := inspect["symbol"].(map[string]any)["contract"].(map[string]any)["type"].(map[string]any)
	if result["kind"] != "recipe" || result["failureRow"] == nil || result["serviceRow"] == nil {
		t.Fatalf("recipe result lost explicit rows: %v", result)
	}
	offset := strings.Index(string(source), "task.catch")
	stdout, stderr, code = runTestCLI(t, binary, "query", file, strconv.Itoa(offset))
	if code != 0 {
		t.Fatalf("stored recipe query: %s", stderr)
	}
	query := readProcessJSON(t, stdout)
	assertResponseReferences(t, query)
	value := query["expression"].(map[string]any)["type"].(map[string]any)
	if value["contract"].(map[string]any)["ref"] != result["ref"] || !reflect.DeepEqual(value["failures"], []any{"Rejected"}) || !reflect.DeepEqual(value["requirements"], []any{"Console"}) {
		t.Fatalf("enum payload recipe lost identity or rows: %v", value)
	}
	assertMCPMatchesCLI(t, binary, root, "queue.ef", "delivery", offset, inspect, query)
}

// assertMCPMatchesCLI asks the MCP server for the same symbol inspection and
// typeAt query as the CLI and requires identical canonical projections.
func assertMCPMatchesCLI(t *testing.T, binary, root, name, symbol string, offset int, inspect, query map[string]any) {
	t.Helper()
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "parity-test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "code.inspect", "arguments": map[string]any{"file": name, "symbol": symbol}}},
		{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "code.typeAt", "arguments": map[string]any{"file": name, "offset": offset}}},
	}
	var input bytes.Buffer
	for _, message := range messages {
		if err := json.NewEncoder(&input).Encode(message); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(binary, "mcp", root)
	command.Stdin = &input
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != 3 {
		t.Fatalf("response count: %d", len(lines))
	}
	for i, pair := range []struct {
		cli  map[string]any
		keys []string
	}{
		{inspect, []string{"symbol", "types", "rows", "revision"}},
		{query, []string{"expression", "types", "rows", "revision"}},
	} {
		mcp := readProcessJSON(t, lines[1+i])["result"].(map[string]any)["structuredContent"].(map[string]any)
		assertResponseReferences(t, mcp)
		for _, key := range pair.keys {
			if !reflect.DeepEqual(pair.cli[key], mcp[key]) {
				t.Fatalf("%s CLI/MCP %s mismatch", symbol, key)
			}
		}
	}
}
