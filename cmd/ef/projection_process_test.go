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

// This walker reads the actual JSON envelope rather than consulting any
// compiler projection helper or deriving expected IDs from its own tables.
func assertResponseReferences(t *testing.T, response map[string]any) {
	t.Helper()
	if response["checked"] == false || response["typeProjectionComplete"] != true {
		t.Fatalf("response is not authoritative: %v", response)
	}
	types := map[string]bool{}
	rows := map[string]bool{}
	for _, table := range []struct {
		key string
		ids map[string]bool
	}{{"types", types}, {"rows", rows}} {
		values, _ := response[table.key].([]any)
		for _, value := range values {
			id := value.(map[string]any)["id"].(string)
			if id == "" || table.ids[id] {
				t.Fatalf("duplicate/empty %s definition %q", table.key, id)
			}
			table.ids[id] = true
		}
	}
	var walk func(any)
	walk = func(value any) {
		switch value := value.(type) {
		case []any:
			for _, child := range value {
				walk(child)
			}
		case map[string]any:
			for key, child := range value {
				if id, ok := child.(string); ok && id != "" {
					switch key {
					case "ref", "signature", "result":
						if !types[id] {
							t.Fatalf("undefined type reference %s=%q", key, id)
						}
					case "failureRow", "serviceRow":
						if !rows[id] {
							t.Fatalf("undefined row reference %s=%q", key, id)
						}
					case "scope":
						if id != response["revision"] {
							t.Fatalf("wrong revision on %q", id)
						}
					}
				}
				if key == "args" {
					if ids, ok := child.([]any); ok {
						for _, id := range ids {
							if !types[id.(string)] {
								t.Fatalf("undefined child %q", id)
							}
						}
					}
				}
				walk(child)
			}
		}
	}
	walk(response)
}

func readProcessJSON(t *testing.T, output []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(output, &value); err != nil {
		t.Fatalf("invalid process JSON: %v\n%s", err, output)
	}
	return value
}

func TestCanonicalSuccessWireAdmission(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	var source strings.Builder
	for i := 0; i < 230; i++ {
		fmt.Fprintf(&source, "enum R%d { A { a: string, b: string, c: string, d: string } B { a: string, b: string, c: string, d: string } }\n", i)
	}
	source.WriteString(`effect fn main() -> string { "ok" }`)
	file := filepath.Join(root, "nested.ef")
	if err := os.WriteFile(file, []byte(source.String()), 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("CLI nested declarations", func(t *testing.T) {
		stdout, stderr, code := runTestCLI(t, binary, "check", file)
		if code != 0 {
			t.Fatalf("semantic check failed: %s", stderr)
		}
		response := readProcessJSON(t, stdout)
		if response["checked"] != true || response["typeProjectionComplete"] != true {
			t.Fatal("nested valid source lost its admitted compact projection")
		}
		limit := int(response["typeProjectionLimits"].(map[string]any)["responseBytes"].(float64))
		accounted := int(response["typeProjectionUsage"].(map[string]any)["responseBytes"].(float64))
		if len(stdout)-1 > limit || accounted != len(stdout)-1 || stdout[len(stdout)-1] != '\n' {
			t.Fatalf("CLI wire accounting: actual=%d accounted=%d limit=%d", len(stdout)-1, accounted, limit)
		}
	})
	if err := os.WriteFile(filepath.Join(root, "tiny.ef"), []byte(`effect fn main() -> string { "ok" }`), 0600); err != nil {
		t.Fatal(err)
	}
	const limit = 1024 * 1024
	ids := []struct {
		name   string
		raw    string
		refuse bool
	}{
		{"HTML", `"` + strings.Repeat("<", 200000) + `"`, false},
		{"raw U2028", `"` + strings.Repeat("\u2028", 200000) + `"`, false},
		{"raw U2029", `"` + strings.Repeat("\u2029", 200000) + `"`, false},
		{"quotes", `"` + strings.Repeat(`\"`, 200000) + `"`, false},
		{"controls", `"` + strings.Repeat(`\n`, 200000) + `"`, false},
		{"number", `1e+09`, false},
		{"escaped string", `"\u0061"`, false},
		{"null", `null`, false},
		{"near bound", `"` + strings.Repeat("a", limit-8192) + `"`, false},
		{"ID exceeds success bound", `"` + strings.Repeat("a", limit+256) + `"`, true},
	}
	for _, test := range ids {
		t.Run(test.name, func(t *testing.T) {
			input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"wire-test","version":"1"}}}` + "\n" +
				`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
				`{"jsonrpc":"2.0","id":` + test.raw + `,"method":"tools/call","params":{"name":"project.check","arguments":{"file":"tiny.ef"}}}` + "\n" +
				`{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n"
			command := exec.Command(binary, "mcp", root)
			command.Stdin = strings.NewReader(input)
			output, err := command.Output()
			if err != nil {
				t.Fatal(err)
			}
			lines := bytes.Split(bytes.TrimSuffix(output, []byte{'\n'}), []byte{'\n'})
			if len(lines) != 3 {
				t.Fatalf("response count %d", len(lines))
			}
			if !test.refuse && len(lines[1]) > limit {
				t.Fatalf("successful canonical frame exceeds limit: %d", len(lines[1]))
			}
			var envelope struct {
				ID json.RawMessage `json:"id"`
			}
			if err := json.Unmarshal(lines[1], &envelope); err != nil || string(envelope.ID) != test.raw {
				t.Fatal("known scalar ID representation changed", err)
			}
			result := readProcessJSON(t, lines[1])["result"].(map[string]any)
			if result["isError"] != test.refuse {
				t.Fatalf("unexpected success admission: bytes=%d isError=%v", len(lines[1]), result["isError"])
			}
			if !test.refuse {
				semantic := result["structuredContent"].(map[string]any)
				assertResponseReferences(t, semantic)
				encoded, err := json.Marshal(semantic)
				accounted := int(semantic["typeProjectionUsage"].(map[string]any)["responseBytes"].(float64))
				if err != nil || len(encoded) != accounted {
					t.Fatalf("MCP semantic accounting: actual=%d accounted=%d error=%v", len(encoded), accounted, err)
				}
			} else if result["structuredContent"] != nil || len(lines[1]) > 16*limit {
				t.Fatal("refusal retained partial authority or exceeded protocol budget")
			}
			if ping := readProcessJSON(t, lines[2]); ping["id"] != float64(3) || ping["error"] != nil || ping["result"] == nil {
				t.Fatal("queued ping lost", ping)
			}
		})
	}
}

func TestCanonicalProjectionCLIAndMCPProcessesRemainCompleteAndResponsive(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	var source strings.Builder
	source.WriteString("effect fn wide(")
	for i := 0; i < 2048; i++ {
		if i > 0 {
			source.WriteByte(',')
		}
		fmt.Fprintf(&source, "p%d: string", i)
	}
	source.WriteString(`) -> string { "wide" }
record Inner { text: string }
record Outer { inner: Inner }
service Users { effect fn get() -> string }
impl Configured(prefix: string) for Users { effect fn get() -> string { prefix } }
effect fn selected() -> Outer { Outer { inner: Inner { text: "ok" } } }
effect fn main() -> string { let provider = run Configured("ok") let value = run selected() value.inner.text }
`)
	file := filepath.Join(root, "main.ef")
	if err := os.WriteFile(file, []byte(source.String()), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runTestCLI(t, binary, "inspect", file, "selected")
	if code != 0 {
		t.Fatalf("small CLI inspect refused: %s", stderr)
	}
	cli := readProcessJSON(t, stdout)
	assertResponseReferences(t, cli)
	declarations := cli["declarations"].([]any)
	if len(declarations) != 2 {
		t.Fatalf("nominal closure should contain Outer and Inner: %v", declarations)
	}
	for _, node := range cli["types"].([]any) {
		if args, _ := node.(map[string]any)["args"].([]any); len(args) > 0 {
			t.Fatalf("selected root leaked wide signature: %v", node)
		}
	}
	_, stderr, code = runTestCLI(t, binary, "inspect", file, "wide")
	if code == 0 || len(stderr) == 0 {
		t.Fatal("wide CLI projection did not refuse explicitly")
	}
	offset := strings.Index(source.String(), "value.inner.text")
	queryOut, queryErr, code := runTestCLI(t, binary, "query", file, strconv.Itoa(offset))
	if code != 0 {
		t.Fatalf("query: %s", queryErr)
	}
	query := readProcessJSON(t, queryOut)
	assertResponseReferences(t, query)
	// The wide function makes whole-file graph inspection refuse, independently
	// of these small selected responses.
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "projection-test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "code.inspect", "arguments": map[string]any{"file": "main.ef", "symbol": "wide"}}},
		{"jsonrpc": "2.0", "id": 3, "method": "ping"},
		{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "code.inspect", "arguments": map[string]any{"file": "main.ef", "symbol": "selected"}}},
		{"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": map[string]any{"name": "code.typeAt", "arguments": map[string]any{"file": "main.ef", "offset": offset}}},
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
		t.Fatalf("unexpected MCP response count %d", len(lines))
	}
	refusal := readProcessJSON(t, lines[1])["result"].(map[string]any)
	if refusal["isError"] != true || refusal["structuredContent"] != nil {
		t.Fatalf("refusal retained authority: %v", refusal)
	}
	if ping := readProcessJSON(t, lines[2]); ping["result"] == nil || ping["error"] != nil {
		t.Fatalf("queued ping failed: %v", ping)
	}
	for i, pair := range []struct {
		cli  map[string]any
		keys []string
	}{{cli, []string{"symbol", "types", "rows", "declarations", "bindings", "revision"}}, {query, []string{"expression", "types", "rows", "revision"}}} {
		mcp := readProcessJSON(t, lines[3+i])["result"].(map[string]any)["structuredContent"].(map[string]any)
		assertResponseReferences(t, mcp)
		for _, key := range pair.keys {
			if !reflect.DeepEqual(pair.cli[key], mcp[key]) {
				t.Fatalf("CLI/MCP %s mismatch", key)
			}
		}
	}
}

func TestCheckedProviderGraphUsesCanonicalContracts(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	file := filepath.Join(root, "graph.ef")
	source := `service Users { effect fn get() -> string }
impl Configured(prefix: string) for Users { effect fn get() -> string { prefix } }
effect fn main() -> string { let provider = run Configured("ok") run Users.get().provide<Users>(provider) }`
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runTestCLI(t, binary, "graph", file)
	if code != 0 {
		t.Fatalf("provider graph: %s", stderr)
	}
	graph := readProcessJSON(t, stdout)
	assertResponseReferences(t, graph)
	byID := map[string]map[string]any{}
	for _, node := range graph["types"].([]any) {
		item := node.(map[string]any)
		byID[item["id"].(string)] = item
	}
	found := map[string]bool{}
	for _, node := range graph["nodes"].([]any) {
		item := node.(map[string]any)
		id := item["id"].(string)
		if id != "provider:Configured" && id != "provider-method:Configured.get" {
			continue
		}
		contract := item["contract"].(map[string]any)
		ref := contract["contract"].(map[string]any)["ref"].(string)
		kind := "providerRecipe"
		if id == "provider-method:Configured.get" {
			kind = "callable"
		}
		if byID[ref]["kind"] != kind {
			t.Fatalf("%s did not read checked %s contract: %v", id, kind, contract)
		}
		found[id] = true
	}
	if len(found) != 2 {
		t.Fatalf("provider/method graph controls missing: %v", found)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"graph-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.graph","arguments":{"file":"graph.ef"}}}`,
	}, "\n") + "\n"
	command := exec.Command(binary, "mcp", root)
	command.Stdin = strings.NewReader(input)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("unexpected graph MCP response count %d", len(lines))
	}
	mcp := readProcessJSON(t, lines[1])["result"].(map[string]any)["structuredContent"].(map[string]any)
	assertResponseReferences(t, mcp)
	if !reflect.DeepEqual(graph, mcp) {
		t.Fatal("actual CLI/MCP graph envelopes differ")
	}
}

func TestLargeCheckedProgramBuildsBothTargetsAfterProjectionRefusal(t *testing.T) {
	binary := buildTestCLI(t)
	var source strings.Builder
	for i := 0; i < 2100; i++ {
		fmt.Fprintf(&source, "record R%d { value: string }\neffect fn f%d() -> R%d { R%d { value: \"ok\" } }\n", i, i, i, i)
	}
	source.WriteString("effect fn main() -> string { let value = run f0() value.value }")
	t.Run("canonical-node-limit", func(t *testing.T) { assertCheckedBuildsAfterRefusal(t, binary, source.String(), "f0") })
	source.Reset()
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&source, "effect fn f%d(", i)
		for p := 0; p < 96; p++ {
			if p > 0 {
				source.WriteByte(',')
			}
			fmt.Fprintf(&source, "p%d: string", p)
		}
		source.WriteString(") -> string { \"ok\" }\n")
	}
	source.WriteString("effect fn main() -> string { \"ok\" }")
	t.Run("aggregate-compatibility-limit", func(t *testing.T) { assertCheckedBuildsAfterRefusal(t, binary, source.String(), "f39") })
}

func assertCheckedBuildsAfterRefusal(t *testing.T, binary, source, selected string) {
	t.Helper()
	root := t.TempDir()
	file := filepath.Join(root, "large.ef")
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runTestCLI(t, binary, "inspect", file, selected)
	if code != 0 {
		t.Fatalf("selected valid function after whole-source refusal: %s", stderr)
	}
	assertResponseReferences(t, readProcessJSON(t, stdout))
	selectedCLI := readProcessJSON(t, stdout)
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "aggregate-test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "large.ef"}}},
		{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "code.inspect", "arguments": map[string]any{"file": "large.ef", "symbol": selected}}},
		{"jsonrpc": "2.0", "id": 4, "method": "ping"},
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
		t.Fatalf("aggregate MCP response count: %d", len(lines))
	}
	checked := readProcessJSON(t, lines[1])["result"].(map[string]any)["structuredContent"].(map[string]any)
	if checked["checked"] != true || checked["typeProjectionComplete"] != false {
		t.Fatalf("MCP lost admitted-source refusal: %v", checked)
	}
	for _, key := range []string{"symbols", "declarations", "bindings", "types", "rows"} {
		if checked[key] != nil {
			t.Fatalf("MCP refusal retained %s", key)
		}
	}
	selectedMCP := readProcessJSON(t, lines[2])["result"].(map[string]any)["structuredContent"].(map[string]any)
	assertResponseReferences(t, selectedMCP)
	for _, key := range []string{"symbol", "types", "rows", "declarations", "bindings", "revision"} {
		if !reflect.DeepEqual(selectedCLI[key], selectedMCP[key]) {
			t.Fatalf("aggregate selected CLI/MCP %s mismatch", key)
		}
	}
	if ping := readProcessJSON(t, lines[3]); ping["result"] == nil || ping["error"] != nil {
		t.Fatalf("aggregate queued ping failed: %v", ping)
	}
	for _, target := range []string{"go", "js"} {
		stdout, stderr, code := runTestCLI(t, binary, "check", file, "--target", target)
		if code != 0 {
			t.Fatalf("%s check rejected valid large source: %s", target, stderr)
		}
		response := readProcessJSON(t, stdout)
		if response["checked"] != true || response["typeProjectionComplete"] != false || response["typeProjectionError"] == nil {
			t.Fatalf("%s check conflated validity and projection: %v", target, response)
		}
		for _, key := range []string{"symbols", "declarations", "types", "rows", "bindings"} {
			if response[key] != nil {
				t.Fatalf("refused check retained %s authority", key)
			}
		}
		output := filepath.Join(root, "large-"+target)
		if target == "js" {
			output += ".mjs"
		}
		args := []string{"build", file, "--target", target, "-o", output}
		if target == "js" {
			args = append(args, "--entry")
		}
		command := exec.Command(binary, args...)
		command.Dir = root
		if result, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s large build failed: %v\n%s", target, err, result)
		}
		if stat, err := os.Stat(output); err != nil || stat.Size() == 0 {
			t.Fatalf("%s build has no artifact: %v", target, err)
		}
		if target == "go" {
			if result, err := exec.Command(output).CombinedOutput(); err != nil || string(result) != "ok\n" {
				t.Fatalf("large native execution: %v %s", err, result)
			}
		}
		if target == "js" {
			modules, err := filepath.Abs("../../node_modules")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
				t.Fatal(err)
			}
			if result, err := exec.Command("bun", output).CombinedOutput(); err != nil || string(result) != "ok\n" {
				t.Fatalf("large JS execution: %v %s", err, result)
			}
		}
	}
}
