package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestFiniteCallableCLIAndMCPParityAfterWholePublicationRefusal(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	var source strings.Builder
	source.WriteString(`error A error B
effect fn first(x:string)->string raises {A}{x}
effect fn second(x:string)->string raises {B}{x}
effect fn chain<E: raises>(a:effect fn(string)->string raises {E},b:effect fn(string)->string raises {E},x:string)->string raises {E}{let next=run a(x);run b(next)}
effect fn selected()->string raises {A,B}{run chain(first,second,"42")}
effect fn main()->(){()}
`)
	// Individually small declarations exhaust aggregate publication, rather
	// than source admission or the selected callable's finite closure.
	for i := 0; i < 2200; i++ {
		fmt.Fprintf(&source, "fn unrelated%d(p:string)->string{p}\n", i)
	}
	file := filepath.Join(root, "callbacks.ef")
	if err := os.WriteFile(file, []byte(source.String()), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runTestCLI(t, binary, "check", file)
	if code != 0 {
		t.Fatalf("admitted callback source refused: %s", stderr)
	}
	checked := readProcessJSON(t, stdout)
	if checked["checked"] != true || checked["typeProjectionComplete"] != false {
		t.Fatalf("aggregate source did not cause publication-only refusal: %v", checked)
	}
	for _, key := range []string{"symbols", "types", "rows", "declarations", "bindings"} {
		if checked[key] != nil {
			t.Fatalf("refused check retained %s authority", key)
		}
	}
	stdout, stderr, code = runTestCLI(t, binary, "inspect", file, "chain")
	if code != 0 {
		t.Fatalf("selected generic callable: %s", stderr)
	}
	inspect := readProcessJSON(t, stdout)
	assertResponseReferences(t, inspect)
	offset := strings.Index(source.String(), "chain(first")
	stdout, stderr, code = runTestCLI(t, binary, "query", file, strconv.Itoa(offset))
	if code != 0 {
		t.Fatalf("instantiated callback query: %s", stderr)
	}
	query := readProcessJSON(t, stdout)
	assertResponseReferences(t, query)
	value := query["expression"].(map[string]any)["type"].(map[string]any)
	if !reflect.DeepEqual(value["failures"], []any{"A", "B"}) {
		t.Fatalf("application union lost: %v", value)
	}
	application := value["application"].(map[string]any)
	if len(application["rowArguments"].([]any)) != 1 {
		t.Fatalf("instantiated row argument missing: %v", application)
	}
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "callable-test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "callbacks.ef"}}},
		{"jsonrpc": "2.0", "id": 3, "method": "ping"},
		{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "code.inspect", "arguments": map[string]any{"file": "callbacks.ef", "symbol": "chain"}}},
		{"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": map[string]any{"name": "code.typeAt", "arguments": map[string]any{"file": "callbacks.ef", "offset": offset}}},
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
	if len(lines) != 5 {
		t.Fatalf("response count: %d", len(lines))
	}
	refused := readProcessJSON(t, lines[1])["result"].(map[string]any)["structuredContent"].(map[string]any)
	if refused["checked"] != true || refused["typeProjectionComplete"] != false {
		t.Fatalf("MCP conflated validity and publication: %v", refused)
	}
	if ping := readProcessJSON(t, lines[2]); ping["error"] != nil || ping["result"] == nil {
		t.Fatalf("queued ping failed: %v", ping)
	}
	for i, pair := range []struct {
		cli  map[string]any
		keys []string
	}{
		{inspect, []string{"symbol", "types", "rows", "revision"}},
		{query, []string{"expression", "types", "rows", "revision"}},
	} {
		mcp := readProcessJSON(t, lines[3+i])["result"].(map[string]any)["structuredContent"].(map[string]any)
		assertResponseReferences(t, mcp)
		for _, key := range pair.keys {
			if !reflect.DeepEqual(pair.cli[key], mcp[key]) {
				t.Fatalf("callable CLI/MCP %s mismatch", key)
			}
		}
	}
}
