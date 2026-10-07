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

// Derived codec witnesses, their direction rows and their plans are read
// from the actual CLI and MCP envelopes, which must be identical.
func TestDerivedCodecFactsMatchAcrossCLIAndMCP(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	files := map[string]string{
		"codec.ef": `import Json "effra/json"
record User { id: i64, name: string }
enum Event { Joined { user: User }, Left }
derive eventJson = Json.codec<Event>(maxBodyBytes: 2048, maxDepth: 4)
effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    let event = run eventJson.decode("{\"_tag\":\"Left\"}")
    run eventJson.encode(event)
}
`,
		"refused.ef": `import Json "effra/json"
record Job { run: effect fn() -> void }
derive jobJson = Json.codec<Job>
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"codec-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.check","arguments":{"file":"codec.ef"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"project.check","arguments":{"file":"refused.ef"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"code.inspect","arguments":{"file":"codec.ef","symbol":"eventJson.decode"}}}`,
	}, "\n") + "\n"
	command := exec.Command(binary, "mcp", root)
	command.Stdin = strings.NewReader(input)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != 4 {
		t.Fatalf("unexpected codec MCP response count %d", len(lines))
	}
	mcp := func(line []byte) map[string]any {
		return readProcessJSON(t, line)["result"].(map[string]any)["structuredContent"].(map[string]any)
	}
	// Wall-clock phase timings are the only facts allowed to differ. The
	// projection usage accounts for the envelope's own bytes, timings
	// included, so each byte count is compared net of its timings' size.
	same := func(cli, mcp map[string]any) bool {
		for _, response := range []map[string]any{cli, mcp} {
			timings, err := json.Marshal(response["timings"])
			if err != nil {
				t.Fatal(err)
			}
			usage := response["typeProjectionUsage"].(map[string]any)
			for _, key := range []string{"compatibilityBytes", "responseBytes"} {
				usage[key] = usage[key].(float64) - float64(len(timings))
			}
			delete(response, "timings")
		}
		return reflect.DeepEqual(cli, mcp)
	}

	stdout, stderr, code := runTestCLI(t, binary, "check", filepath.Join(root, "codec.ef"))
	if code != 0 {
		t.Fatalf("codec check: %s", stderr)
	}
	check := readProcessJSON(t, stdout)
	assertResponseReferences(t, check)
	if !same(check, mcp(lines[1])) {
		t.Fatal("actual CLI/MCP codec check envelopes differ")
	}
	types := map[string]bool{}
	for _, node := range check["types"].([]any) {
		types[node.(map[string]any)["id"].(string)] = true
	}
	codecs := check["codecs"].([]any)
	plans := check["codecPlans"].([]any)
	if len(codecs) != 1 || len(plans) != 1 {
		t.Fatalf("codec inspection: %v %v", codecs, plans)
	}
	witness := codecs[0].(map[string]any)
	plan := plans[0].(map[string]any)
	if witness["name"] != "eventJson" || witness["derivation"] != "effra/json.codec" || witness["plan"] != plan["id"] || !types[witness["domainType"].(string)] {
		t.Fatalf("witness does not reference its checked type and plan: %v", witness)
	}
	if decode := witness["decode"].(map[string]any); decode["function"] != "function:module:file:module:eventJson.decode" || !reflect.DeepEqual(decode["failures"], []any{"JsonDecodeFailure"}) {
		t.Fatalf("decode direction: %v", decode)
	}
	bounds := plan["bounds"].(map[string]any)
	if bounds["maxBodyBytes"] != float64(2048) || bounds["maxDepth"] != float64(4) || plan["nesting"] != float64(2) || len(plan["nodes"].([]any)) != 4 {
		t.Fatalf("plan: %v", plan)
	}
	symbols := map[string]bool{}
	for _, symbol := range check["symbols"].([]any) {
		symbols[symbol.(map[string]any)["name"].(string)] = true
	}
	if !symbols["eventJson.decode"] || !symbols["eventJson.encode"] {
		t.Fatalf("direction functions are not projected symbols: %v", symbols)
	}
	stdout, stderr, code = runTestCLI(t, binary, "inspect", filepath.Join(root, "codec.ef"), "eventJson.decode")
	if code != 0 {
		t.Fatalf("codec inspect: %s", stderr)
	}
	// MCP names the inspected file in its envelope and accounts for it in
	// the usage; the inspected facts themselves must be identical.
	inspected, mcpInspected := readProcessJSON(t, stdout), mcp(lines[3])
	for _, key := range []string{"symbol", "types", "rows", "declarations", "bundledBindings", "revision"} {
		if !reflect.DeepEqual(inspected[key], mcpInspected[key]) {
			t.Fatalf("actual CLI/MCP direction inspections differ in %s", key)
		}
	}
	if contract := inspected["symbol"].(map[string]any)["contract"].(map[string]any); contract["success"] != "Event" || !reflect.DeepEqual(contract["failures"], []any{"JsonDecodeFailure"}) {
		t.Fatalf("inspected decode contract: %v", contract)
	}

	stdout, _, code = runTestCLI(t, binary, "check", filepath.Join(root, "refused.ef"))
	if code == 0 {
		t.Fatal("refused codec checked")
	}
	refused := readProcessJSON(t, stdout)
	if !same(refused, mcp(lines[2])) {
		t.Fatal("actual CLI/MCP codec refusal envelopes differ")
	}
	diagnostics := refused["diagnostics"].([]any)
	first := diagnostics[0].(map[string]any)
	if len(diagnostics) != 1 || first["code"] != "EF138" || !strings.Contains(first["message"].(string), "at Job.run is a function or effect recipe") {
		t.Fatalf("refusal: %v", diagnostics)
	}
}
