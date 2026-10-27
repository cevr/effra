package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"effra.local/prototype/internal/edition"
)

// serveCalls runs one session that initializes and then sends each tool
// call, and returns the decoded responses of the calls in order.
func serveCalls(t *testing.T, root string, calls ...string) []map[string]any {
	t.Helper()
	messages := []string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}
	for index, call := range calls {
		messages = append(messages, `{"jsonrpc":"2.0","id":`+jsonNumber(index+1)+`,"method":"tools/call","params":`+call+`}`)
	}
	var output bytes.Buffer
	if err := Serve(root, nil, strings.NewReader(strings.Join(messages, "\n")), &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var responses []map[string]any
	for decoder.More() {
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if len(responses) != len(calls)+1 {
		t.Fatalf("%d responses, want %d", len(responses), len(calls)+1)
	}
	return responses[1:]
}

func jsonNumber(n int) string {
	data, _ := json.Marshal(n)
	return string(data)
}

// toolText returns a tool result's text and whether it is a tool error.
func toolText(t *testing.T, response map[string]any) (string, bool) {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("no tool result: %v", response)
	}
	content := result["content"].([]any)[0].(map[string]any)
	isError, _ := result["isError"].(bool)
	return content["text"].(string), isError
}

func TestProjectEditionTool(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module workspace\n")
	write("main.ef", "effect fn main() -> void { void }\n")
	write("new/effra.json", `{"edition":"2026"}`)
	write("new/main.ef", "package p\n")
	write("bad/effra.json", `{"edition":"2099"}`)
	write("bad/main.ef", "package p\n")

	// The tool is listed with its schema.
	var listed *tool
	for _, candidate := range tools() {
		if candidate.Name == "project.edition" {
			listed = &candidate
		}
	}
	if listed == nil {
		t.Fatal("tools/list has no project.edition")
	}
	schema, _ := json.Marshal(listed.InputSchema)
	if want := `{"additionalProperties":false,"properties":{"file":{"description":"Workspace-relative .ef source file","type":"string"}},"required":["file"],"type":"object"}`; string(schema) != want {
		t.Fatalf("project.edition schema %s, want %s", schema, want)
	}

	call := func(arguments string) string { return `{"name":"project.edition","arguments":` + arguments + `}` }
	outside := filepath.Join(t.TempDir(), "outside.ef")
	if err := os.WriteFile(outside, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.ef")); err != nil {
		t.Fatal(err)
	}
	traversal, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	responses := serveCalls(t, root,
		call(`{"file":"main.ef"}`),
		call(`{"file":"new/main.ef"}`),
		call(`{"file":"bad/main.ef"}`),
		call(`{"file":`+jsonString(traversal)+`}`),
		call(`{"file":"escape.ef"}`),
		call(`{"file":`+jsonString(filepath.Join(root, "main.ef"))+`}`),
		call(`{"file":"main.txt"}`),
		call(`{"file":"missing.ef"}`),
		call(`{}`),
		call(`{"file":"main.ef","target":"go"}`),
	)
	wants := []string{
		string(edition.Resolution{Edition: edition.Legacy, Source: edition.SourceDefault}.JSON()),
		string(edition.Resolve(filepath.Join(root, "new")).JSON()),
		string(edition.Resolve(filepath.Join(root, "bad")).JSON()),
	}
	if !strings.Contains(wants[1], `"edition":"2026"`) || !strings.Contains(wants[2], `"code":"EF144"`) {
		t.Fatalf("fixture resolutions %v", wants)
	}
	for index, want := range wants {
		text, isError := toolText(t, responses[index])
		if isError || text != want {
			t.Fatalf("call %d: %q (error %v), want %s", index, text, isError, want)
		}
		// The decoded map loses key order; the text above carries the bytes.
		var wantValue any
		if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
			t.Fatal(err)
		}
		if structured := responses[index]["result"].(map[string]any)["structuredContent"]; !reflect.DeepEqual(structured, wantValue) {
			t.Fatalf("call %d structured content %v, want %s", index, structured, want)
		}
	}
	// Root containment and admission refuse exactly as the other tools do.
	refusals := []string{"file escapes workspace", "file escapes workspace", "workspace-relative .ef path", "workspace-relative .ef path", "no such file"}
	for offset, fragment := range refusals {
		text, isError := toolText(t, responses[len(wants)+offset])
		if !isError || !strings.Contains(text, fragment) {
			t.Fatalf("call %d: %q (error %v), want a refusal naming %q", len(wants)+offset, text, isError, fragment)
		}
	}
	for _, response := range responses[len(wants)+len(refusals):] {
		if response["error"] == nil {
			t.Fatalf("invalid arguments accepted: %v", response)
		}
	}
}

func jsonString(text string) string {
	data, _ := json.Marshal(text)
	return string(data)
}

// TestCodeFormatFollowsTheEdition: code.format formats a 2026 file with the
// 2026 formatter and names it, refuses a 2026 syntax error with its S code,
// and keeps the legacy formatter for a legacy file and for a source buffer.
func TestCodeFormatFollowsTheEdition(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unformatted := "package main\n\nfunc main()  uses Console {\nConsole.Println(\"a\")\n}\n"
	formatted := "package main\n\nfunc main() uses Console {\n\tConsole.Println(\"a\")\n}\n"
	for rel, text := range map[string]string{
		"go.mod":           "module workspace\n",
		"legacy.ef":        "fn ok() -> string { \"ok\" }\n",
		"new/effra.json":   `{"edition":"2026"}`,
		"new/main.ef":      unformatted,
		"new/malformed.ef": "package main\n\nfunc main() {\n\tx := )\n}\n",
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	call := func(arguments string) string { return `{"name":"code.format","arguments":` + arguments + `}` }
	responses := serveCalls(t, root,
		call(`{"file":"new/main.ef"}`),
		call(`{"file":"new/malformed.ef"}`),
		call(`{"file":"legacy.ef"}`),
		call(`{"source":`+jsonString(unformatted)+`}`),
	)
	structured := func(index int) map[string]any {
		t.Helper()
		result := responses[index]["result"].(map[string]any)
		if isError, _ := result["isError"].(bool); isError {
			t.Fatalf("call %d is an error: %v", index, result["content"])
		}
		return result["structuredContent"].(map[string]any)
	}
	if got := structured(0); got["text"] != formatted || got["formatterVersion"] != "effra-2026/formatter-1" || got["changed"] != true {
		t.Fatalf("2026 file: %v", got)
	}
	if text, isError := toolText(t, responses[1]); !isError || !strings.HasPrefix(text, "source cannot be formatted: S001 at 4:7: ") {
		t.Fatalf("2026 syntax error: %q (error %v)", text, isError)
	}
	if got := structured(2); got["formatterVersion"] == "effra-2026/formatter-1" {
		t.Fatalf("legacy file used the 2026 formatter: %v", got)
	}
	// A source buffer has no directory and stays legacy: the legacy
	// formatter refuses the 2026 text.
	if text, isError := toolText(t, responses[3]); !isError || strings.Contains(text, "S001") {
		t.Fatalf("source buffer: %q (error %v)", text, isError)
	}
}
