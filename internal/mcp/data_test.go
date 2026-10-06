package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClosedDataDeclarationInspectionOverStdio(t *testing.T) {
	root := t.TempDir()
	source := `record User { id: string }
enum State { Ready Waiting { reason: string } }
error Invalid { message: string }
fn describe(state: State) -> string { match state { State.Ready => "ready" State.Waiting { reason } => reason } }`
	if err := os.WriteFile(filepath.Join(root, "data.ef"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	messages := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"code.inspect","arguments":{"file":"data.ef","symbol":"State"}}}`,
	}, "\n")
	var output bytes.Buffer
	if err := Serve(root, strings.NewReader(messages), &output); err != nil {
		t.Fatal(err)
	}
	var first, second map[string]any
	decoder := json.NewDecoder(&output)
	if err := decoder.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&second); err != nil {
		t.Fatal(err)
	}
	content := second["result"].(map[string]any)["structuredContent"].(map[string]any)
	declaration := content["declaration"].(map[string]any)
	if declaration["kind"] != "enum" || declaration["name"] != "State" {
		t.Fatalf("wrong declaration: %+v", declaration)
	}
	variants := declaration["variants"].([]any)
	if len(variants) != 2 || variants[1].(map[string]any)["name"] != "Waiting" {
		t.Fatalf("wrong variants: %+v", variants)
	}
}
