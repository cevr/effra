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

// CLI and MCP processes publish the same canonical host facts: native type
// identity, Option adaptation of a nullable result and the complete binding
// signature, at one revision.
func TestHostTypeInspectionCLIAndMCPParity(t *testing.T) {
	binary := buildTestCLI(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	relative := "examples/host-types.ef"
	file := filepath.Join(root, relative)
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	offset := strings.Index(string(source), "match found") + len("match ")
	inspectOut, inspectErr, code := runTestCLI(t, binary, "inspect", file, "counter")
	if code != 0 {
		t.Fatalf("CLI inspect: %s", inspectErr)
	}
	inspect := readProcessJSON(t, inspectOut)
	assertResponseReferences(t, inspect)
	typeOut, typeErr, code := runTestCLI(t, binary, "type", file, "--offset", strconv.Itoa(offset), "--json")
	if code != 0 {
		t.Fatalf("CLI type: %s", typeErr)
	}
	selected := readProcessJSON(t, typeOut)

	found := false
	for _, binding := range inspect["bindings"].([]any) {
		b := binding.(map[string]any)
		if b["symbol"] != "host.Find" {
			continue
		}
		result := b["hostResults"].([]any)[0].(map[string]any)
		found = result["adaptation"] == "option" && result["native"] == "*effra.local/prototype/examples/hosttypes.Counter"
	}
	if !found {
		t.Fatalf("binding lacks nullable host result: %v", inspect["bindings"])
	}
	expression := selected["selection"].(map[string]any)["expression"].(map[string]any)["type"].(map[string]any)["type"].(map[string]any)
	definitions := map[string]map[string]any{}
	for _, node := range selected["types"].([]any) {
		definitions[node.(map[string]any)["id"].(string)] = node.(map[string]any)
	}
	option := definitions[expression["ref"].(string)]
	pointer := definitions[option["args"].([]any)[0].(string)]
	if option["declaration"] != "template:effra/data:module:Option" || pointer["kind"] != "host" || pointer["declaration"] != "go:*effra.local/prototype/examples/hosttypes.Counter" {
		t.Fatalf("selected host value is not Option of the native pointer: %v %v", option, pointer)
	}

	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "host-types-test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "code.inspect", "arguments": map[string]any{"file": relative, "symbol": "counter"}}},
		{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "code.type", "arguments": map[string]any{"file": relative, "offset": offset}}},
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
		t.Fatalf("unexpected MCP response count %d", len(lines))
	}
	for i, pair := range []struct {
		cli  map[string]any
		keys []string
	}{{inspect, []string{"symbol", "types", "rows", "declarations", "bindings", "revision"}}, {selected, []string{"selection", "types", "rows", "revision"}}} {
		mcp := readProcessJSON(t, lines[1+i])["result"].(map[string]any)["structuredContent"].(map[string]any)
		for _, key := range pair.keys {
			if !reflect.DeepEqual(pair.cli[key], mcp[key]) {
				t.Fatalf("CLI/MCP %s mismatch:\n%v\n%v", key, pair.cli[key], mcp[key])
			}
		}
	}
}
