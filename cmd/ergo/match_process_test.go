package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const productMatchSource = `enum Light { Red; Amber; Green }
enum Signal { Stop; Go { speed: i64 }; Wait { speed: i64 } }
fn plan(light: Light, signal: Signal) -> string {
    match light, signal {
        Light.Red | Light.Amber, Signal.Go { speed } | Signal.Wait { speed } => "slow"
        Light.Green, Signal.Go { speed: s } | Signal.Wait { speed: s } => "go"
        Light.Red, Signal.Stop => "halt"
        Light.Amber | Light.Green, Signal.Stop => "brake"
    }
}
effect fn main() -> string {
    plan(Light.Amber {}, Signal.Stop {})
}
`

func TestProductMatchFactsAgreeAcrossCLIAndMCPProcesses(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	missing := strings.Replace(productMatchSource, "Light.Amber | Light.Green, Signal.Stop", "Light.Amber, Signal.Stop", 1)
	for name, source := range map[string]string{"main.ef": productMatchSource, "missing.ef": missing} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cli := map[string]map[string]any{}
	for _, name := range []string{"main.ef", "missing.ef"} {
		stdout, stderr, _ := runTestCLI(t, binary, "check", filepath.Join(root, name), "--target", "go")
		if len(stdout) == 0 {
			t.Fatalf("CLI check of %s wrote no report: %q", name, stderr)
		}
		cli[name] = readProcessJSON(t, stdout)
	}
	if cli["main.ef"]["checked"] != true || cli["missing.ef"]["checked"] != false {
		t.Fatalf("CLI product match admission: %v / %v", cli["main.ef"]["checked"], cli["missing.ef"]["diagnostics"])
	}
	if !strings.Contains(string(mustJSON(t, cli["missing.ef"]["diagnostics"])), "missing match arm for Light.Green, Signal.Stop") {
		t.Fatalf("CLI missing-pair witness: %v", cli["missing.ef"]["diagnostics"])
	}
	formatted, stderr, code := runTestCLIInput(t, binary, productMatchSource, "fmt", "--stdin")
	if code != 0 || len(stderr) != 0 || !strings.Contains(string(formatted), "\n    match light, signal {\n        Light.Red | Light.Amber, Signal.Go { speed } | Signal.Wait { speed } => \"slow\"\n") {
		t.Fatalf("CLI formatter split a product match: code=%d stderr=%q\n%s", code, stderr, formatted)
	}

	requests := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "match-process-test", "version": "1"}}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "main.ef", "target": "go"}}},
		map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "missing.ef", "target": "go"}}},
		map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "code.format", "arguments": map[string]any{"source": productMatchSource}}},
	}
	var input bytes.Buffer
	for _, request := range requests {
		if err := json.NewEncoder(&input).Encode(request); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(binary, "mcp", root)
	command.Stdin = &input
	output, err := command.Output()
	if err != nil {
		t.Fatalf("MCP product match process failed: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != 4 {
		t.Fatalf("MCP response count: %d\n%s", len(lines), output)
	}
	for index, name := range []string{"main.ef", "missing.ef"} {
		mcp := readProcessJSON(t, lines[index+1])["result"].(map[string]any)["structuredContent"].(map[string]any)
		for _, key := range []string{"checked", "diagnostics", "symbols", "revision"} {
			if !sameJSONValue(cli[name][key], mcp[key]) {
				t.Fatalf("CLI/MCP %s drifted in %s: CLI=%v MCP=%v", name, key, cli[name][key], mcp[key])
			}
		}
	}
	mcpFormat := readProcessJSON(t, lines[3])["result"].(map[string]any)["structuredContent"].(map[string]any)
	if mcpFormat["text"] != string(formatted) {
		t.Fatalf("MCP formatter disagrees with CLI: %v", mcpFormat)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
