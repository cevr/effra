package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
)

func TestProtocolLifecycleAndSemanticParity(t *testing.T) {
	root := t.TempDir()
	source := `effect fn hello() -> string { "hello" }`
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	messages := []string{
		`{"jsonrpc":"2.0","id":0,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"code.inspect","arguments":{"file":"main.ef","symbol":"hello"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"project.check","arguments":{"file":"main.ef","expectedRevision":"stale"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"project.describe","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"project.check","arguments":{"file":"main.ef"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"missing","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"project.check","arguments":{"file":"main.ef","extra":true}}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":123}}`,
		`{bad json}`,
	}
	var output bytes.Buffer
	if err := Serve(root, strings.NewReader(strings.Join(messages, "\n")), &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	responses := []map[string]any{}
	for decoder.More() {
		var r map[string]any
		if err := decoder.Decode(&r); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, r)
	}
	if len(responses) != 10 {
		t.Fatalf("notifications produced responses: %d", len(responses))
	}
	if responses[0]["error"] == nil {
		t.Fatal("tools allowed before initialize")
	}
	result := responses[1]["result"].(map[string]any)
	if result["protocolVersion"] != ProtocolVersion {
		t.Fatal(result)
	}
	listed := responses[2]["result"].(map[string]any)["tools"].([]any)
	if len(listed) != 9 {
		t.Fatal(listed)
	}
	inspected := responses[3]["result"].(map[string]any)["structuredContent"].(map[string]any)
	expected := compiler.Compile(source)
	if inspected["revision"] != expected.Revision || inspected["checked"] != true {
		t.Fatal(inspected)
	}
	raw, _ := json.Marshal(inspected["symbol"])
	var got compiler.Symbol
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, *expected.Find("hello")) {
		t.Fatalf("CLI/MCP semantic mismatch: %+v", got)
	}
	if responses[4]["result"].(map[string]any)["isError"] != true {
		t.Fatal("stale revision accepted")
	}
	if responses[7]["error"] == nil || responses[8]["error"] == nil || responses[9]["error"] == nil {
		t.Fatal("invalid protocol input accepted")
	}
}
func TestWorkspaceBounds(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(outside, "secret.ef")
	if err := os.WriteFile(file, []byte(`fn a() -> string { "outside" }`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, filepath.Join(root, "escape.ef")); err != nil {
		t.Fatal(err)
	}
	if _, err := readSource(root, "escape.ef"); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if _, err := readSource(root, file); err == nil {
		t.Fatal("absolute path accepted")
	}
	rel, _ := filepath.Rel(root, file)
	if _, err := readSource(root, rel); err == nil {
		t.Fatal("relative traversal accepted")
	}
}
func TestInvalidSourceIsACompilerResult(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(`effect fn main() -> () { run Console.log("x") }`), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := call(root, "project.check", arguments{File: "main.ef"})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["checked"] != false {
		t.Fatal("invalid source passed check")
	}
}

func TestArgumentSchemas(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"project.describe", "null"},
		{"project.check", `{"file":"x.ef","target":"llvm"}`}, {"project.describe", "{\"file\":\"\"}"},
		{"project.check", "{\"file\":null}"}, {"project.check", "{\"file\":\"x.ef\",\"symbol\":\"\"}"},
		{"code.inspect", "{\"file\":\"x.ef\"}"},
	} {
		if _, err := decodeArguments(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Fatalf("accepted %s %s", tc.name, tc.raw)
		}
	}
}

func TestTargetInspection(t *testing.T) {
	root := t.TempDir()
	source := `effect fn main() -> string { "hello" }`
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		result, err := call(root, "code.inspect", arguments{File: "main.ef", Symbol: "main", Target: target})
		if err != nil {
			t.Fatal(err)
		}
		if result.(map[string]any)["target"] != target {
			t.Fatal("wrong inspected target")
		}
	}
}

func TestLintSuppressionSemanticParity(t *testing.T) {
	root := t.TempDir()
	source := `effect fn task() -> string { "ok" }
effect fn main() -> () {
// effra-lint-disable-next-line unused-recipe -- intentional deferred hook
let forgotten = task();
()}`
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	expected := compiler.Compile(source).Lint(true)
	result, err := call(root, "project.lint", arguments{File: "main.ef", Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	payload := result.(map[string]any)
	raw, err := json.Marshal(payload["lint"])
	if err != nil {
		t.Fatal(err)
	}
	var got compiler.LintResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) || !got.LintPassed || len(got.LintDiagnostics) != 0 {
		t.Fatalf("CLI/MCP lint mismatch: got=%+v expected=%+v", got, expected)
	}

	source = `effect fn main() -> () {
// effra-lint-disable-next-line future-rule -- deliberate invalid example
()}`
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	invalid, err := call(root, "project.lint", arguments{File: "main.ef"})
	if err != nil {
		t.Fatal(err)
	}
	invalidPayload := invalid.(map[string]any)["lint"].(compiler.LintResult)
	if invalidPayload.LintPassed || invalidPayload.Errors != 1 || invalidPayload.LintDiagnostics[0].Span.Line != 2 {
		t.Fatalf("MCP accepted invalid suppression: %+v", invalidPayload)
	}
}
