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

func TestCallableFactoriesKeepCLIAndMCPAliveWithCompleteNestedContracts(t *testing.T) {
	binary := buildTestCLI(t)
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "callables-factory.ef"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	file := filepath.Join(root, "factories.ef")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	invalid := strings.Replace(string(source), "let first = run pureFailure().catch<Missing>(keep)", "let first = keep.timeout(10)", 1)
	if invalid == string(source) {
		t.Fatal("invalid timeout mutation did not match fixture")
	}
	invalidFile := filepath.Join(root, "invalid-timeout.ef")
	if err := os.WriteFile(invalidFile, []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runTestCLI(t, binary, "check", invalidFile)
	if code != 1 {
		t.Fatalf("invalid timeout must diagnose, exit=%d %s %s", code, stdout, stderr)
	}
	assertInvalidTimeoutReport(t, readProcessJSON(t, stdout))
	stdout, stderr, code = runTestCLI(t, binary, "check", file)
	if code != 0 || readProcessJSON(t, stdout)["checked"] != true {
		t.Fatalf("valid factory check: exit=%d %s %s", code, stdout, stderr)
	}
	stdout, stderr, code = runTestCLI(t, binary, "inspect", file, "factory")
	if code != 0 {
		t.Fatalf("factory inspection: %s", stderr)
	}
	inspect := readProcessJSON(t, stdout)
	assertResponseReferences(t, inspect)
	body := inspect["symbol"].(map[string]any)["bodyContract"].(map[string]any)
	assertFactoryProcessContract(t, inspect, body)
	offset := strings.Index(string(source), "factory().catch")
	stdout, stderr, code = runTestCLI(t, binary, "query", file, strconv.Itoa(offset))
	if code != 0 {
		t.Fatalf("factory query: %s", stderr)
	}
	query := readProcessJSON(t, stdout)
	assertResponseReferences(t, query)
	assertFactoryProcessContract(t, query, query["expression"].(map[string]any)["type"].(map[string]any))
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "factory-test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": "invalid", "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "invalid-timeout.ef"}}},
		{"jsonrpc": "2.0", "id": "after-invalid", "method": "ping"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "project.check", "arguments": map[string]any{"file": "factories.ef"}}},
		{"jsonrpc": "2.0", "id": 3, "method": "ping"},
		{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "code.inspect", "arguments": map[string]any{"file": "factories.ef", "symbol": "factory"}}},
		{"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": map[string]any{"name": "code.typeAt", "arguments": map[string]any{"file": "factories.ef", "offset": offset}}},
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
	if len(lines) != 7 {
		t.Fatalf("MCP response count: %d", len(lines))
	}
	assertInvalidTimeoutReport(t, readProcessJSON(t, lines[1])["result"].(map[string]any)["structuredContent"].(map[string]any))
	if ping := readProcessJSON(t, lines[2]); ping["result"] == nil || ping["error"] != nil {
		t.Fatalf("ping after invalid timeout: %v", ping)
	}
	checked := readProcessJSON(t, lines[3])["result"].(map[string]any)["structuredContent"].(map[string]any)
	if checked["checked"] != true {
		t.Fatalf("valid factory MCP check: %v", checked)
	}
	if ping := readProcessJSON(t, lines[4]); ping["result"] == nil || ping["error"] != nil {
		t.Fatalf("queued ping: %v", ping)
	}
	for i, pair := range []struct {
		cli map[string]any
		key string
	}{{inspect, "symbol"}, {query, "expression"}} {
		remote := readProcessJSON(t, lines[5+i])["result"].(map[string]any)["structuredContent"].(map[string]any)
		assertResponseReferences(t, remote)
		for _, key := range []string{pair.key, "types", "rows", "revision"} {
			if !reflect.DeepEqual(pair.cli[key], remote[key]) {
				t.Fatalf("factory CLI/MCP %s differs", key)
			}
		}
	}
}

func assertInvalidTimeoutReport(t *testing.T, report map[string]any) {
	t.Helper()
	if report["checked"] != false {
		t.Fatalf("invalid timeout admitted: %v", report)
	}
	for _, raw := range report["diagnostics"].([]any) {
		if raw.(map[string]any)["code"] == "EF106" {
			return
		}
	}
	t.Fatalf("invalid timeout lost EF106: %v", report)
}

func assertFactoryProcessContract(t *testing.T, response, value map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(value["failures"], []any{"Missing"}) || !reflect.DeepEqual(value["requirements"], []any{"Logger"}) || value["callable"] != nil {
		t.Fatalf("outer factory contract corrupted: %v", value)
	}
	nodes := map[string]map[string]any{}
	for _, raw := range response["types"].([]any) {
		node := raw.(map[string]any)
		nodes[node["id"].(string)] = node
	}
	outer := nodes[value["contract"].(map[string]any)["ref"].(string)]
	if outer["kind"] != "recipe" {
		t.Fatalf("factory invocation not recipe: %v", outer)
	}
	inner := nodes[outer["result"].(string)]
	rows := map[string]any{}
	for _, raw := range response["rows"].([]any) {
		row := raw.(map[string]any)
		rows[row["id"].(string)] = row["labels"]
	}
	if inner["kind"] != "callable" || inner["callableKind"] != "effect" || !reflect.DeepEqual(rows[inner["failureRow"].(string)], []any{"Broken"}) || !reflect.DeepEqual(rows[inner["serviceRow"].(string)], []any{"Directory"}) {
		t.Fatalf("returned callback contract erased: %v", inner)
	}
	if len(inner["args"].([]any)) != 1 || nodes[inner["result"].(string)]["name"] != "string" {
		t.Fatalf("returned callback shape corrupted: %v", inner)
	}
}
