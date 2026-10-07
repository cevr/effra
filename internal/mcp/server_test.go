package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/producer"
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
	if len(listed) != 12 {
		t.Fatal(listed)
	}
	foundType := false
	for _, item := range listed {
		if item.(map[string]any)["name"] == "code.type" {
			foundType = true
		}
	}
	if !foundType {
		t.Fatal("selected type tool was not advertised")
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

func TestServeRejectsInvalidTextBeforeJSONNormalization(t *testing.T) {
	invalidUTF8 := append([]byte{'"', '/', '/', ' '}, 0xff)
	invalidUTF8 = append(invalidUTF8, '"')
	replacement, err := json.Marshal("// \uFFFD")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name       string
		sourceJSON []byte
		malformed  bool
	}{
		{name: "invalid UTF-8", sourceJSON: invalidUTF8, malformed: true},
		{name: "unpaired high surrogate", sourceJSON: []byte("\"// \\ud800\""), malformed: true},
		{name: "unpaired low surrogate", sourceJSON: []byte("\"// \\udc00\""), malformed: true},
		{name: "literal replacement character", sourceJSON: replacement},
		{name: "astral surrogate pair", sourceJSON: []byte("\"// \\ud83d\\ude00\"")},
		{name: "literal backslash escape", sourceJSON: mustMarshalMCPTestString("// \\ud800")},
		{name: "empty", sourceJSON: []byte(`""`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := append([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`+"\n"), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")...)
			input = append(input, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"code.format","arguments":{"source":`)...)
			input = append(input, test.sourceJSON...)
			input = append(input, []byte("}}}\n")...)
			input = append(input, []byte(`{"jsonrpc":"2.0","id":3,"method":"ping"}`+"\n")...)

			var output bytes.Buffer
			if err := Serve(t.TempDir(), bytes.NewReader(input), &output); err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(&output)
			var responses []map[string]any
			for {
				var response map[string]any
				if err := decoder.Decode(&response); err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					t.Fatal(err)
				}
				responses = append(responses, response)
			}
			if len(responses) != 3 {
				t.Fatalf("malformed or valid format request desynchronized ping: %d responses: %s", len(responses), output.String())
			}
			if test.malformed {
				errorValue, ok := responses[1]["error"].(map[string]any)
				if !ok || errorValue["code"] != float64(-32700) {
					t.Fatalf("invalid text was not refused as a parse error: %#v", responses[1])
				}
			} else {
				result, ok := responses[1]["result"].(map[string]any)
				if !ok || result["isError"] == true {
					t.Fatalf("valid text was refused: %#v", responses[1])
				}
			}
			if !reflect.DeepEqual(responses[2]["result"], map[string]any{}) {
				t.Fatalf("ping after text admission control was not served: %#v", responses[2])
			}
		})
	}
}

func mustMarshalMCPTestString(value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func TestLayerInspectionIncludesFileBeforeProjectionReceipt(t *testing.T) {
	source := `service Store { effect fn label() -> string }
impl Live for Store { effect fn label() -> string { "live" } }
layer App { Store = Live }
effect fn main() -> string { run Store.label().provide(App) }`
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		result, err := call(root, "code.inspect", arguments{File: "main.ef", Symbol: "App", Target: target})
		if err != nil {
			t.Fatalf("%s layer inspection failed: %v", target, err)
		}
		response, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("%s returned unexpected layer response: %#v", target, result)
		}
		if response["file"] != "main.ef" {
			t.Fatalf("%s layer response lost file metadata: %#v", target, response["file"])
		}
		usage, ok := response["typeProjectionUsage"].(compiler.ProjectionUsage)
		if !ok {
			t.Fatalf("%s layer response lost projection usage: %#v", target, response["typeProjectionUsage"])
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if usage.ResponseBytes != len(encoded) {
			t.Errorf("%s layer receipt omitted final file metadata: receipt=%d actual=%d", target, usage.ResponseBytes, len(encoded))
		}
	}
}

func TestDiagnosticsReportMatchesCompilerAndBoundsResults(t *testing.T) {
	root := t.TempDir()
	source := `effect fn task() -> string { "ok" }
effect fn main() -> string {
let forgotten = task()
run task().provide<Console>(Stdout)
}`
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := call(root, "project.diagnostics", arguments{File: "main.ef"})
	if err != nil {
		t.Fatal(err)
	}
	report, ok := result.(compiler.DiagnosticReport)
	if !ok {
		t.Fatalf("unexpected diagnostics result: %#v", result)
	}
	uri, err := compiler.FileURI(filepath.Join(root, "main.ef"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Checked || !report.PolicyPassed || !report.LintAvailable || report.Source.Origin != "disk" || report.Source.URI != uri {
		t.Fatalf("wrong diagnostics identity/policy: %+v", report)
	}
	if report.TotalCounts.Warnings != 1 || report.TotalCounts.Hints != 1 || len(report.Diagnostics) != 2 {
		t.Fatalf("wrong diagnostics counts: %+v", report)
	}
	expected := compiler.Compile(source).DiagnosticReport(compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: source}, false)
	if report.Revision != expected.Revision || report.Diagnostics[0].LSP == nil || report.Diagnostics[0].LSP.Severity != 2 {
		t.Fatalf("MCP report drifted from compiler snapshot: %+v", report)
	}
	strict, err := call(root, "project.diagnostics", arguments{File: "main.ef", Strict: true})
	if err != nil || strict.(compiler.DiagnosticReport).PolicyPassed {
		t.Fatalf("strict policy was not preserved: result=%+v err=%v", strict, err)
	}
	if _, err := call(root, "project.diagnostics", arguments{File: "main.ef", ExpectedRevision: "stale"}); err == nil || !strings.Contains(err.Error(), "stale semantic revision") {
		t.Fatalf("stale diagnostics revision was accepted: %v", err)
	}

	invalidSource := `effect fn main() -> () { run Console.log("x") }`
	if err := os.WriteFile(filepath.Join(root, "invalid.ef"), []byte(invalidSource), 0644); err != nil {
		t.Fatal(err)
	}
	invalid, err := call(root, "project.diagnostics", arguments{File: "invalid.ef"})
	if err != nil || invalid.(compiler.DiagnosticReport).LintAvailable || invalid.(compiler.DiagnosticReport).Checked {
		t.Fatalf("invalid source was presented as lintable: result=%+v err=%v", invalid, err)
	}

	var exact strings.Builder
	for i := 0; i < 101; i++ {
		exact.WriteString("effect fn duplicate() -> () { () }\n")
	}
	if err := os.WriteFile(filepath.Join(root, "exact.ef"), []byte(exact.String()), 0644); err != nil {
		t.Fatal(err)
	}
	exactResult, err := call(root, "project.diagnostics", arguments{File: "exact.ef"})
	if err != nil {
		t.Fatal(err)
	}
	exactReport := exactResult.(compiler.DiagnosticReport)
	if len(exactReport.Diagnostics) != 100 || exactReport.ReturnedCount != 100 || exactReport.Truncated {
		t.Fatalf("exact diagnostic limit changed result: %+v", exactReport)
	}

	var many strings.Builder
	for i := 0; i < 102; i++ {
		many.WriteString("effect fn duplicate() -> () { () }\n")
	}
	if err := os.WriteFile(filepath.Join(root, "many.ef"), []byte(many.String()), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := call(root, "project.diagnostics", arguments{File: "many.ef"}); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("over-limit diagnostics did not remain an explicit tool failure: %v", err)
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
		{"body rows", "body", true, 100, 100, 0, 0, 2, []int{50, 50}},
		{"requirement rows", "requirements", true, 0, 0, 100, 100, 2, []int{50, 50}},
		{"nested contribution names", "names", true, 100, 100, 0, 0, 1, []int{100}},
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
		if err == nil || result != nil || !strings.Contains(err.Error(), "requires checked source") {
			t.Fatalf("unchecked %s exposed facts: result=%+v err=%v", name, result, err)
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
		fmt.Fprintf(&builder, "effect fn target() -> () raises {%s} { () }\n", joined)
	case "body":
		first, second := splitInspectionRows(names)
		fmt.Fprintf(&builder, "effect fn first() -> () raises {%s} { () }\n", strings.Join(first, ","))
		fmt.Fprintf(&builder, "effect fn second() -> () raises {%s} { () }\n", strings.Join(second, ","))
		fmt.Fprintf(&builder, "effect fn target() -> () raises {%s} { run first(); run second() }\n", joined)
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
		fmt.Fprintf(&builder, "effect fn target() -> () uses {%s} { run part0(); run part1() }\n", strings.Join(services, ","))
	case "contributions":
		builder.Reset()
		builder.WriteString("error E0\neffect fn one() -> () raises {E0} { () }\neffect fn target() -> () raises {E0} {\n")
		for i := 0; i < count; i++ {
			builder.WriteString("run one();\n")
		}
		builder.WriteString("()}\n")
	case "names":
		fmt.Fprintf(&builder, "effect fn many() -> () raises {%s} { () }\n", joined)
		fmt.Fprintf(&builder, "effect fn target() -> () raises {%s} { run many() }\n", joined)
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

func TestProjectTestsReportsLiveLayerSelection(t *testing.T) {
	root := t.TempDir()
	source := `layer LiveClockLayer { Clock = LiveClock }
effect fn test_live_layer() -> () { () }`
	if err := os.WriteFile(filepath.Join(root, "main.ef"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		result, err := call(root, "project.tests", arguments{File: "main.ef", Target: target})
		if err != nil {
			t.Fatalf("%s target: %v", target, err)
		}
		payload := result.(map[string]any)
		if payload["liveRequired"] != true {
			t.Fatalf("%s target did not report live layer selection: %#v", target, payload)
		}
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

func TestFormatBufferAndDiskParity(t *testing.T) {
	source := `import go missing "example.invalid/no-such-package"
effect fn main() -> string { "ok" }`
	want, err := compiler.FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	args, err := decodeArguments("code.format", json.RawMessage(fmt.Sprintf(`{"source":%q,"uri":"buffer://main.ef","expectedDigest":%q}`, source, compiler.FormatDigest(source))))
	if err != nil {
		t.Fatal(err)
	}
	result, err := formatCode(t.TempDir(), args)
	if err != nil {
		t.Fatal(err)
	}
	payload := result.(map[string]any)
	if payload["origin"] != "buffer" || payload["uri"] != "buffer://main.ef" || payload["text"] != want.Text || payload["inputDigest"] != want.InputDigest || payload["outputDigest"] != want.OutputDigest {
		t.Fatalf("buffer format drifted from core: payload=%+v want=%+v", payload, want)
	}

	root := t.TempDir()
	path := filepath.Join(root, "main.ef")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	diskArgs, err := decodeArguments("code.format", json.RawMessage(`{"file":"main.ef"}`))
	if err != nil {
		t.Fatal(err)
	}
	diskResult, err := formatCode(root, diskArgs)
	if err != nil {
		t.Fatal(err)
	}
	diskPayload := diskResult.(map[string]any)
	uri, err := compiler.FileURI(path)
	if err != nil {
		t.Fatal(err)
	}
	if diskPayload["origin"] != "disk" || diskPayload["uri"] != uri || diskPayload["text"] != want.Text {
		t.Fatalf("disk format mismatch: payload=%+v want=%+v uri=%s", diskPayload, want, uri)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.ModTime() != after.ModTime() || before.Size() != after.Size() {
		t.Fatal("MCP formatting mutated the disk snapshot")
	}
}

func TestFormatRejectsStaleAndInvalidOrigins(t *testing.T) {
	for _, raw := range []string{
		`{"file":"main.ef","source":""}`,
		`{"uri":"buffer://main.ef"}`,
		`{"file":"main.ef","uri":"buffer://main.ef"}`,
		`{"source":"","expectedDigest":""}`,
		`{"source":"","target":"go"}`,
		`{"source":"","target":"js"}`,
	} {
		if _, err := decodeArguments("code.format", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid code.format arguments %s", raw)
		}
	}
	args, err := decodeArguments("code.format", json.RawMessage(fmt.Sprintf(`{"source":%q,"expectedDigest":"stale"}`, `effect fn main() -> string { "ok" }`)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := formatCode(t.TempDir(), args); err == nil || !strings.Contains(err.Error(), "stale format source") {
		t.Fatalf("stale digest was accepted: %v", err)
	}
	exact := strings.Repeat(" ", maxFormatSourceBytes)
	args, err = decodeArguments("code.format", json.RawMessage(fmt.Sprintf(`{"source":%q}`, exact)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := formatCode(t.TempDir(), args); err != nil {
		t.Fatalf("source at the exact limit was rejected: %v", err)
	}
	tooLarge := strings.Repeat(" ", maxFormatSourceBytes+1)
	args, err = decodeArguments("code.format", json.RawMessage(fmt.Sprintf(`{"source":%q}`, tooLarge)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := formatCode(t.TempDir(), args); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("source limit was not explicit: %v", err)
	}
}

func TestFormatRejectsInvalidUTF8ForBufferAndDisk(t *testing.T) {
	invalid := append([]byte("// invalid "), 0xff)
	invalid = append(invalid, []byte("\neffect fn main() -> () { () }\n")...)
	bufferArgs := arguments{Source: string(invalid), SourcePresent: true}
	if _, err := formatCode(t.TempDir(), bufferArgs); err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Fatalf("invalid buffer UTF-8 was accepted: %v", err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "invalid.ef")
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	diskArgs := arguments{File: "invalid.ef", FilePresent: true}
	if _, err := formatCode(root, diskArgs); err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Fatalf("invalid disk UTF-8 was accepted: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, invalid) {
		t.Fatalf("invalid disk source was changed: err=%v bytes=%v", err, got)
	}
}

func TestRejectedFormatRequestStillReleasesQueuedPing(t *testing.T) {
	root := t.TempDir()
	messages := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"code.format","arguments":{"source":"","target":"go"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"code.format","arguments":{"source":"effect fn main() -> string { @ }"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"ping"}`,
	}
	var output bytes.Buffer
	if err := Serve(root, strings.NewReader(strings.Join(messages, "\n")), &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	responses := []map[string]any{}
	for decoder.More() {
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 4 || responses[1]["error"] == nil || responses[2]["result"].(map[string]any)["isError"] != true || !reflect.DeepEqual(responses[3]["result"], map[string]any{}) {
		t.Fatalf("queued ping was not completed after rejected format: %+v", responses)
	}
}

func TestMCPFrameReaderDrainsOversizeAndAcceptsExactLimit(t *testing.T) {
	exact := strings.Repeat("x", maxMCPFrameBytes)
	reader := bufio.NewReader(strings.NewReader(exact + "\nnext\n"))
	frame, status, err := readMCPFrame(reader, maxMCPFrameBytes)
	if err != nil || status != mcpFrameComplete || len(frame) != maxMCPFrameBytes {
		t.Fatalf("exact frame was not admitted: status=%v length=%d err=%v", status, len(frame), err)
	}
	frame, status, err = readMCPFrame(reader, maxMCPFrameBytes)
	if err != nil || status != mcpFrameComplete || string(frame) != "next" {
		t.Fatalf("queued frame was not preserved: status=%v frame=%q err=%v", status, frame, err)
	}

	over := strings.Repeat("x", maxMCPFrameBytes+1)
	reader = bufio.NewReader(strings.NewReader(over + "\nnext\n"))
	frame, status, err = readMCPFrame(reader, maxMCPFrameBytes)
	if err != nil || status != mcpFrameTooLarge || frame != nil {
		t.Fatalf("oversized frame was accumulated or misclassified: status=%v frame=%v err=%v", status, frame, err)
	}
	frame, status, err = readMCPFrame(reader, maxMCPFrameBytes)
	if err != nil || status != mcpFrameComplete || string(frame) != "next" {
		t.Fatalf("oversized frame did not drain to the next request: status=%v frame=%q err=%v", status, frame, err)
	}
}

func TestMCPResponseFrameCapIsAllOrError(t *testing.T) {
	result := map[string]any{"origin": "buffer", "text": "", "uri": strings.Repeat("<", 1024)}
	res := response{JSONRPC: "2.0", ID: json.RawMessage("1"), Result: toolResult{Content: []map[string]string{{"type": "text", "text": "summary"}}, StructuredContent: result}}
	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var exact bytes.Buffer
	if err := writeMCPResponse(&exact, res, len(encoded)); err != nil {
		t.Fatalf("exact response bound rejected complete frame: %v", err)
	}
	if !bytes.HasSuffix(exact.Bytes(), []byte("\n")) || len(exact.Bytes()) != len(encoded)+1 {
		t.Fatalf("exact response frame was not written whole: %d bytes", exact.Len())
	}
	var over bytes.Buffer
	if err := writeMCPResponse(&over, res, len(encoded)-1); !errors.Is(err, errMCPResponseTooLarge) || over.Len() != 0 {
		t.Fatalf("oversized response was partially written: err=%v bytes=%d", err, over.Len())
	}
}

func TestBoundedFormatResponsePreservesIDsAndUsesCompactFallback(t *testing.T) {
	id := json.RawMessage(`"` + strings.Repeat("<", 1024) + `"`)
	value := response{
		JSONRPC: "2.0",
		ID:      id,
		Result: toolResult{
			Content: []map[string]string{{"type": "text", "text": "summary"}},
		},
	}
	encoded, err := marshalBoundedFormatResponse(value)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`\\u003c`)) || !bytes.Contains(encoded, []byte(strings.Repeat("<", 1024))) {
		t.Fatalf("bounded response changed a valid ID: %q", encoded[:min(len(encoded), 128)])
	}
	if !boundedFormatResponseFits(compactFormatError(id)) {
		t.Fatal("small bounded-format ID did not fit the compact fallback")
	}

	base, err := marshalBoundedFormatResponse(compactFormatError(json.RawMessage("null")))
	if err != nil {
		t.Fatal(err)
	}
	baseWithoutID := len(base) - len("null")
	exactIDBytes := maxMCPFrameBytes - baseWithoutID
	if exactIDBytes < 2 {
		t.Fatalf("compact fallback overhead unexpectedly exceeds frame cap: %d", baseWithoutID)
	}
	exactID := json.RawMessage(`"` + strings.Repeat("a", exactIDBytes-2) + `"`)
	if exact, err := marshalBoundedFormatResponse(compactFormatError(exactID)); err != nil || len(exact) != maxMCPFrameBytes {
		t.Fatalf("compact fallback exact boundary drifted: length=%d err=%v", len(exact), err)
	}
	var exactOutput bytes.Buffer
	if err := writeBoundedFormatResponse(&exactOutput, compactFormatError(exactID)); err != nil {
		t.Fatalf("compact fallback exact boundary rejected: %v", err)
	}
	overID := json.RawMessage(`"` + strings.Repeat("a", exactIDBytes-1) + `"`)
	var overOutput bytes.Buffer
	if err := writeBoundedFormatResponse(&overOutput, compactFormatError(overID)); !errors.Is(err, errMCPResponseTooLarge) || overOutput.Len() != 0 {
		t.Fatalf("compact fallback +1 boundary was not rejected atomically: err=%v bytes=%d", err, overOutput.Len())
	}

	t.Run("exact-limit unknown tool keeps its ID and queued ping", func(t *testing.T) {
		const head = `{"jsonrpc":"2.0","id":`
		const tail = `,"method":"tools/call","params":{"name":"x"}}`
		rawID := `"` + strings.Repeat("a", maxMCPFrameBytes-len(head)-len(tail)-2) + `"`
		frame := head + rawID + tail
		if len(frame) != maxMCPFrameBytes {
			t.Fatalf("request fixture is not exact-limit: %d", len(frame))
		}
		input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{}}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + frame + "\n" +
			`{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n"
		var output bytes.Buffer
		if err := Serve(t.TempDir(), strings.NewReader(input), &output); err != nil {
			t.Fatalf("admitted unknown-tool request terminated server: %v", err)
		}
		lines := bytes.Split(bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), []byte{'\n'})
		if len(lines) != 3 || len(lines[1]) > maxMCPFrameBytes {
			t.Fatalf("lost queued response or exceeded frame bound: responses=%d", len(lines))
		}
		var refused struct {
			ID    json.RawMessage `json:"id"`
			Error *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(lines[1], &refused); err != nil || string(refused.ID) != rawID || refused.Error == nil || refused.Error.Code != -32602 {
			t.Fatalf("compact refusal changed request ID or argument-error code: %v", err)
		}
		var ping struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(lines[2], &ping); err != nil || ping.ID != 3 || string(ping.Result) != "{}" || ping.Error != nil {
			t.Fatalf("queued ping was not answered: %v", err)
		}
	})
}

func TestBoundedFormatResponsePreservesValidatedScalarIDRepresentations(t *testing.T) {
	for _, raw := range []string{
		`"<\"\u0001"`,
		"\"\u2028\u2029\"",
		`-0.0`,
		`null`,
	} {
		encoded, err := marshalBoundedFormatResponse(response{
			JSONRPC: "2.0",
			ID:      json.RawMessage(raw),
			Error:   &rpcError{-32602, "too large"},
		})
		if err != nil {
			t.Fatalf("ID %q was rejected: %v", raw, err)
		}
		if !bytes.Contains(encoded, []byte(`,"id":`+raw+`,`)) && !bytes.Contains(encoded, []byte(`,"id":`+raw+`}`)) {
			t.Fatalf("bounded response changed scalar ID %q: %q", raw, encoded)
		}
	}
	if validMCPRequestID(json.RawMessage(`"bad`)) {
		t.Fatal("malformed scalar ID was admitted")
	}
	if validMCPRequestID(json.RawMessage("\"\xff\"")) {
		t.Fatal("invalid UTF-8 scalar ID was admitted")
	}
	if !validMCPRequestID(json.RawMessage(`999999999999999999999999999999999999999999999999999999999999999999999999`)) {
		t.Fatal("large JSON number ID was rejected by float conversion")
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
	checked := compiler.Compile(source)
	if err := checked.Qualify(producer.Current()); err != nil {
		t.Fatal(err)
	}
	expected := checked.Lint(true)
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
