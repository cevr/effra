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
	inspectOut, inspectErr, code := runTestCLI(t, binary, "inspect", file, "program")
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

	components := map[string]map[string]any{}
	protocols := map[string]any{}
	for _, binding := range inspect["bindings"].([]any) {
		b := binding.(map[string]any)
		protocols[b["symbol"].(string)] = b["protocol"]
		for _, side := range []string{"hostParameters", "hostResults"} {
			if listed, ok := b[side].([]any); ok && len(listed) > 0 {
				components[b["symbol"].(string)+" "+side] = listed[0].(map[string]any)
			}
		}
	}
	for key, want := range map[string][3]string{
		"host.Find hostResults":                    {"*effra.local/prototype/examples/hosttypes.Counter", "Option<*host.Counter>", "option"},
		"host.Bytes hostResults":                   {"[]uint8", "Option<bytes>", "option"},
		"host.RawText hostResults":                 {"[]uint8", "Option<bytes>", "option"},
		"host.BytesClass hostParameters":           {"[]uint8", "bytes", "present"},
		"(*host.Counter).Increment hostParameters": {"*effra.local/prototype/examples/hosttypes.Counter", "*host.Counter", "receiver"},
		"io.Copy hostParameters":                   {"io.Writer", "io.Writer", "present"},
		"(*host.Plain).Read hostResults":           {"int", "bytes", "filled"},
		"(*host.ShortWriter).Write hostResults":    {"int", "int", "written"},
	} {
		got := components[key]
		if got["native"] != want[0] || got["type"] != want[1] || got["adaptation"] != want[2] {
			t.Fatalf("%s adaptation %v, want %v", key, got, want)
		}
	}
	if protocols["(*host.Plain).Read"] != "io.Reader" || protocols["(*host.ShortWriter).Write"] != "io.Writer" || protocols["io.Copy"] != nil {
		t.Fatalf("inspected I/O protocols: %v", protocols)
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
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "code.inspect", "arguments": map[string]any{"file": relative, "symbol": "program"}}},
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

// An unknown effra.bindings.json key is refused when metadata loads, and CLI
// check and MCP project.check report the same diagnostic.
func TestBindingContractKeyDiagnosticsCLIAndMCPParity(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	for name, contents := range map[string]string{
		"go.mod":              "module example.test/keys\n\ngo 1.27\n",
		"keys.go":             "package keys\n\nfunc Fetch() string { return \"fetch\" }\n",
		"effra.bindings.json": `{"example.test/keys.Fecth":{"cancellation":"unknown"}}`,
		"main.ef":             "import go keys \"example.test/keys\"\neffect fn main() -> string {\n    run keys.Fetch().provide<Foreign>(Host)\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := runTestCLI(t, binary, "check", filepath.Join(root, "main.ef"), "--target", "go")
	if code == 0 {
		t.Fatalf("CLI check accepted an unknown contract key: %s", stdout)
	}
	cli := readProcessJSON(t, stdout)
	want := `effra.bindings.json key "example.test/keys.Fecth" matches no Go function or method; near: "example.test/keys.Fetch"`
	diagnostics, _ := cli["diagnostics"].([]any)
	if len(diagnostics) != 1 || diagnostics[0].(map[string]any)["code"] != "EF111" || diagnostics[0].(map[string]any)["message"] != want {
		t.Fatalf("CLI contract key diagnostic: %v (stderr %s)", cli["diagnostics"], stderr)
	}
	var input bytes.Buffer
	for _, message := range []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "contract-keys-test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "main.ef", "target": "go"}}},
	} {
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
	if len(lines) != 2 {
		t.Fatalf("unexpected MCP response count %d", len(lines))
	}
	mcp := readProcessJSON(t, lines[1])["result"].(map[string]any)["structuredContent"].(map[string]any)
	for _, key := range []string{"checked", "diagnostics", "revision"} {
		if !reflect.DeepEqual(cli[key], mcp[key]) {
			t.Fatalf("CLI/MCP %s mismatch:\n%v\n%v", key, cli[key], mcp[key])
		}
	}
}
