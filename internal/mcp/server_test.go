package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestInspectionBoundsCoverNestedSymbolDetails(t *testing.T) {
	for _, test := range []struct {
		name string
		mode string
	}{
		{"declared rows", "declared"},
		{"body rows", "body"},
		{"requirement rows", "requirements"},
		{"contribution count", "contributions"},
		{"nested contribution names", "names"},
	} {
		t.Run(test.name+" at 100", func(t *testing.T) {
			assertInspectionBoundary(t, inspectionRowsSource(100, test.mode), false)
		})
		t.Run(test.name+" at 101", func(t *testing.T) {
			assertInspectionBoundary(t, inspectionRowsSource(101, test.mode), true)
		})
	}

	for _, test := range []struct {
		name                    string
		mode                    string
		checked                 bool
		contractFailures        int
		bodyFailures            int
		contractRequirements    int
		bodyRequirements        int
		contributions           int
		contributionNameLengths []int
	}{
		{"body rows", "body", false, 0, 100, 0, 0, 2, []int{50, 50}},
		{"requirement rows", "requirements", false, 0, 0, 0, 100, 2, []int{50, 50}},
		{"nested contribution names", "names", false, 0, 0, 0, 0, 1, []int{100}},
	} {
		t.Run(test.name+" preserve independent dimensions", func(t *testing.T) {
			assertInspectionDimensions(t, inspectionRowsSource(100, test.mode), inspectionDimensions{
				checked:                 test.checked,
				contractFailures:        test.contractFailures,
				bodyFailures:            test.bodyFailures,
				contractRequirements:    test.contractRequirements,
				bodyRequirements:        test.bodyRequirements,
				contributions:           test.contributions,
				contributionNameLengths: test.contributionNameLengths,
			})
		})
	}

	unchecked := `effect fn main() -> () { run Console.log("x") }`
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(unchecked), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"code.inspect", "code.explain"} {
		result, err := call(root, name, arguments{File: "main.ef", Symbol: "main"})
		if err != nil {
			t.Fatalf("unchecked %s rejected: %v", name, err)
		}
		if result.(map[string]any)["checked"] != false {
			t.Fatalf("unchecked %s was not preserved: %+v", name, result)
		}
	}
}

func assertInspectionBoundary(t *testing.T, source string, wantError bool) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"code.inspect", "code.explain"} {
		result, err := call(root, name, arguments{File: "main.ef", Symbol: "target"})
		if wantError {
			if err == nil || !strings.Contains(err.Error(), "symbol exceeds prototype inspection limits") {
				t.Fatalf("%s accepted over-limit symbol: result=%+v err=%v", name, result, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s rejected 100-item symbol: %v", name, err)
		}
		if result.(map[string]any)["symbol"] == nil {
			t.Fatalf("%s omitted bounded symbol: %+v", name, result)
		}
	}
}

type inspectionDimensions struct {
	checked                 bool
	contractFailures        int
	bodyFailures            int
	contractRequirements    int
	bodyRequirements        int
	contributions           int
	contributionNameLengths []int
}

func assertInspectionDimensions(t *testing.T, source string, expected inspectionDimensions) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"code.inspect", "code.explain"} {
		result, err := call(root, name, arguments{File: "main.ef", Symbol: "target"})
		if err != nil {
			t.Fatalf("%s rejected bounded dimensions: %v", name, err)
		}
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("%s returned unexpected result: %#v", name, result)
		}
		if checked, ok := payload["checked"].(bool); !ok || checked != expected.checked {
			t.Fatalf("%s checked=%v, want %v: %#v", name, payload["checked"], expected.checked, payload)
		}
		symbol, ok := payload["symbol"].(*compiler.Symbol)
		if !ok {
			t.Fatalf("%s returned unexpected symbol: %#v", name, payload["symbol"])
		}
		if got := len(symbol.Contract.Errors); got != expected.contractFailures {
			t.Fatalf("%s contract failures=%d, want %d", name, got, expected.contractFailures)
		}
		if got := len(symbol.Actual.Errors); got != expected.bodyFailures {
			t.Fatalf("%s body failures=%d, want %d", name, got, expected.bodyFailures)
		}
		if got := len(symbol.Contract.Services); got != expected.contractRequirements {
			t.Fatalf("%s contract requirements=%d, want %d", name, got, expected.contractRequirements)
		}
		if got := len(symbol.Actual.Services); got != expected.bodyRequirements {
			t.Fatalf("%s body requirements=%d, want %d", name, got, expected.bodyRequirements)
		}
		if len(symbol.Contributions) != expected.contributions {
			t.Fatalf("%s contributions=%d, want %d", name, len(symbol.Contributions), expected.contributions)
		}
		for i, want := range expected.contributionNameLengths {
			if i >= len(symbol.Contributions) {
				t.Fatalf("%s contribution %d missing; got %d contributions", name, i, len(symbol.Contributions))
			}
			if got := len(symbol.Contributions[i].Names); got != want {
				t.Fatalf("%s contribution %d names=%d, want %d", name, i, got, want)
			}
		}
	}
}

func inspectionRowsSource(count int, mode string) string {
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf("E%d", i)
	}
	var builder strings.Builder
	for _, name := range names {
		fmt.Fprintf(&builder, "error %s\n", name)
	}
	joined := strings.Join(names, ",")
	switch mode {
	case "declared":
		fmt.Fprintf(&builder, "effect fn target() -> () throws {%s} { () }\n", joined)
	case "body":
		first, second := splitInspectionRows(names)
		fmt.Fprintf(&builder, "effect fn first() -> () throws {%s} { () }\n", strings.Join(first, ","))
		fmt.Fprintf(&builder, "effect fn second() -> () throws {%s} { () }\n", strings.Join(second, ","))
		builder.WriteString("effect fn target() -> () { run first(); run second() }\n")
	case "requirements":
		services := make([]string, count)
		for i := range services {
			services[i] = fmt.Sprintf("S%d", i)
			fmt.Fprintf(&builder, "service %s { effect fn get() -> () }\n", services[i])
		}
		first, second := splitInspectionRows(services)
		for i, row := range [][]string{first, second} {
			fmt.Fprintf(&builder, "effect fn part%d() -> () uses {%s} {\n", i, strings.Join(row, ","))
			for _, service := range row {
				fmt.Fprintf(&builder, "run %s.get();\n", service)
			}
			builder.WriteString("()}\n")
		}
		builder.WriteString("effect fn target() -> () { run part0(); run part1() }\n")
	case "contributions":
		builder.Reset()
		builder.WriteString("error E0\neffect fn one() -> () throws {E0} { () }\neffect fn target() -> () throws {E0} {\n")
		for i := 0; i < count; i++ {
			builder.WriteString("run one();\n")
		}
		builder.WriteString("()}\n")
	case "names":
		builder.WriteString("record Box { value: () }\n")
		fmt.Fprintf(&builder, "effect fn many() -> () throws {%s} { () }\n", joined)
		builder.WriteString("effect fn target() -> Box { Box { value: run many() } }\n")
	}
	return builder.String()
}

func splitInspectionRows(names []string) ([]string, []string) {
	middle := len(names) / 2
	return names[:middle], names[middle:]
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
