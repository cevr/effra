package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A rejected scope-tail recipe carries one suggested run edit. CLI check,
// CLI diagnostics and the MCP equivalents report the same proposal for the
// same source revision, and a stale revision is refused rather than
// answered with positions from other text.
func TestScopeFinalRecipeSuggestionCLIAndMCPParity(t *testing.T) {
	binary := buildTestCLI(t)
	source := `effect fn tick(label: string) -> string uses { Console } {
    run Console.log("tick " + label)
    label
}
effect fn body(flag: bool) -> string uses { Console } {
    let mark = "ünï 🙂"
    scope {
        // choose one
        if flag {
            tick(mark) /* kept */
        } else {
            tick("é")
        }
    }
}
effect fn main() -> void { void }
`
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "scope.ef"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "scope.ef")
	stdout, _, code := runTestCLI(t, binary, "check", file)
	if code == 0 {
		t.Fatal("scope tail recipe checked")
	}
	checked := readProcessJSON(t, stdout)
	diagnostics := checked["diagnostics"].([]any)
	if len(diagnostics) != 1 {
		t.Fatalf("check diagnostics: %v", diagnostics)
	}
	checkSuggestions := diagnostics[0].(map[string]any)["suggestions"].([]any)
	if len(checkSuggestions) != 1 || checkSuggestions[0].(map[string]any)["message"] != "Execute this recipe inside the scope" {
		t.Fatalf("check suggestion: %v", checkSuggestions)
	}
	text, _, _ := runTestCLI(t, binary, "diagnostics", file)
	if !strings.Contains(string(text), "EF105: scope result is an unexecuted recipe") || !strings.Contains(string(text), "\n  suggestion: Execute this recipe inside the scope\n") {
		t.Fatalf("text diagnostics: %s", text)
	}
	stdout, _, _ = runTestCLI(t, binary, "diagnostics", file, "--json")
	report := readProcessJSON(t, stdout)
	findings := report["diagnostics"].([]any)
	if len(findings) != 1 || report["revision"] != checked["revision"] {
		t.Fatalf("diagnostics report: %v", report)
	}
	reportSuggestions := findings[0].(map[string]any)["suggestions"].([]any)
	// The projection adds UTF-16 ranges to the same byte edits.
	for i, raw := range reportSuggestions[0].(map[string]any)["edits"].([]any) {
		edit := raw.(map[string]any)
		original := checkSuggestions[0].(map[string]any)["edits"].([]any)[i].(map[string]any)
		if !reflect.DeepEqual(edit["span"], original["span"]) || edit["newText"] != original["newText"] || edit["range"] == nil {
			t.Fatalf("edit %d: report %v, check %v", i, edit, original)
		}
	}
	edits := reportSuggestions[0].(map[string]any)["edits"].([]any)
	tail := strings.Index(source, "if flag")
	end := strings.LastIndex(source, "}\n    }\n}") + 1
	if start := edits[0].(map[string]any)["span"].(map[string]any)["offset"]; start != float64(tail) {
		t.Fatalf("edit starts at %v, want %d", start, tail)
	}
	if finish := edits[1].(map[string]any)["span"].(map[string]any)["offset"]; finish != float64(end) {
		t.Fatalf("edit ends at %v, want %d", finish, end)
	}

	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "scope-test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "scope.ef"}}},
		{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "project.diagnostics", "arguments": map[string]any{"file": "scope.ef", "expectedRevision": checked["revision"]}}},
		{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "project.diagnostics", "arguments": map[string]any{"file": "scope.ef", "expectedRevision": "stale"}}},
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
	if len(lines) != 4 {
		t.Fatalf("response count: %d", len(lines))
	}
	mcpCheck := readProcessJSON(t, lines[1])["result"].(map[string]any)["structuredContent"].(map[string]any)
	if !reflect.DeepEqual(mcpCheck["diagnostics"], checked["diagnostics"]) {
		t.Fatalf("MCP check diagnostics differ:\n%v\n%v", mcpCheck["diagnostics"], checked["diagnostics"])
	}
	mcpReport := readProcessJSON(t, lines[2])["result"].(map[string]any)["structuredContent"].(map[string]any)
	if !reflect.DeepEqual(mcpReport["diagnostics"], report["diagnostics"]) || mcpReport["revision"] != report["revision"] {
		t.Fatalf("MCP diagnostics differ:\n%v\n%v", mcpReport["diagnostics"], report["diagnostics"])
	}
	if stale := readProcessJSON(t, lines[3])["result"].(map[string]any); stale["isError"] != true || stale["structuredContent"] != nil {
		t.Fatalf("stale revision answered: %v", stale)
	}
}
