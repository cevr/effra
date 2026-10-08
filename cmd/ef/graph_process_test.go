package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
)

type legacyGraphUsage struct {
	Nodes              int `json:"nodes"`
	Edges              int `json:"edges"`
	RowLabels          int `json:"rowLabels"`
	NameBytes          int `json:"nameBytes"`
	CompatibilityBytes int `json:"compatibilityBytes"`
	ResponseBytes      int `json:"responseBytes"`
}

// splitLegacyGraphUsage removes the single typeProjectionUsage object from an
// encoded legacy graph and returns it with the remaining bytes.
func splitLegacyGraphUsage(t *testing.T, encoded []byte) (legacyGraphUsage, []byte) {
	t.Helper()
	key := []byte(`"typeProjectionUsage":`)
	start := bytes.Index(encoded, key)
	if start < 0 || bytes.Count(encoded, key) != 1 {
		t.Fatal("legacy graph must carry one typeProjectionUsage object")
	}
	end := start + len(key) + bytes.IndexByte(encoded[start+len(key):], '}') + 1
	var usage legacyGraphUsage
	if err := json.Unmarshal(encoded[start+len(key):end], &usage); err != nil {
		t.Fatal(err)
	}
	rest := append(append([]byte{}, encoded[:start]...), encoded[end:]...)
	return usage, rest
}

func jsonStringBytes(value any) int {
	switch value := value.(type) {
	case string:
		return len(value)
	case []any:
		total := 0
		for _, item := range value {
			total += jsonStringBytes(item)
		}
		return total
	case map[string]any:
		total := 0
		for _, item := range value {
			total += jsonStringBytes(item)
		}
		return total
	}
	return 0
}

// The option-free `ef graph FILE` contract is the legacy dependency graph.
// Its bytes are frozen by the compiler goldens; the CLI adds only the
// executing producer's qualification ahead of the same compact encoding.
func TestLegacyGraphCLIBytesMatchFrozenGolden(t *testing.T) {
	binary := buildTestCLI(t)
	for _, fixture := range []struct {
		name, target string
		flags        []string
	}{
		{name: "workflow", target: "go"},
		{name: "layers", target: "js", flags: []string{"--target", "js"}},
		{name: "layers-workflow", target: "go", flags: []string{"--target", "go"}},
		{name: "callables-factory", target: "go"},
	} {
		t.Run(fixture.name+"-"+fixture.target, func(t *testing.T) {
			file := filepath.Join("..", "..", "examples", fixture.name+".ef")
			stdout, stderr, code := runTestCLI(t, binary, append([]string{"graph", file}, fixture.flags...)...)
			if code != 0 {
				t.Fatalf("legacy graph refused: %s", stderr)
			}
			golden, err := os.ReadFile(filepath.Join("..", "..", "internal", "compiler", "testdata", "graph", "legacy", fixture.name+"-"+fixture.target+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var qualification struct {
				Producer json.RawMessage `json:"producer"`
				Snapshot json.RawMessage `json:"snapshot"`
			}
			if err := json.Unmarshal(stdout, &qualification); err != nil || len(qualification.Producer) == 0 || len(qualification.Snapshot) == 0 {
				t.Fatalf("legacy graph lost producer qualification: %v", err)
			}
			prefix := `{"producer":` + string(qualification.Producer) + `,"snapshot":` + string(qualification.Snapshot) + `,`
			want := append([]byte(prefix), golden[1:]...)
			// Usage accounting is the only producer-dependent arithmetic: the
			// qualification adds its encoded bytes and its string bytes.
			actualUsage, actualRest := splitLegacyGraphUsage(t, stdout)
			goldenUsage, wantRest := splitLegacyGraphUsage(t, want)
			if !bytes.Equal(actualRest, wantRest) {
				t.Fatalf("legacy ef graph bytes changed for %s", fixture.name)
			}
			var metadata any
			if err := json.Unmarshal([]byte(`[`+string(qualification.Producer)+`,`+string(qualification.Snapshot)+`]`), &metadata); err != nil {
				t.Fatal(err)
			}
			added := len(prefix) - 1
			expected := goldenUsage
			expected.NameBytes += jsonStringBytes(metadata)
			expected.CompatibilityBytes += added
			expected.ResponseBytes += added
			if actualUsage != expected || actualUsage.ResponseBytes != len(stdout)-1 {
				t.Fatalf("legacy usage accounting changed: actual=%+v expected=%+v bytes=%d", actualUsage, expected, len(stdout)-1)
			}
		})
	}
}

type graphProcessCase struct {
	name  string
	file  string
	flags []string
	args  map[string]any
}

// graphProcessShims puts fake go and node executables first on PATH. Each one
// appends to a counter file, so any build, codegen or execution attempt by a
// graph request is observable.
func graphProcessShims(t *testing.T) (string, string) {
	t.Helper()
	bin := t.TempDir()
	counter := filepath.Join(t.TempDir(), "executions")
	for _, name := range []string{"go", "node", "bun"} {
		script := "#!/bin/sh\necho " + name + " >> '" + counter + "'\nexit 97\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return bin + string(os.PathListSeparator) + os.Getenv("PATH"), counter
}

// graphProcessCommand runs ef with the shimmed PATH and the workspace as its
// working directory, so the workspace listing observes every relative write.
func graphProcessCommand(binary, path, dir string, args ...string) *exec.Cmd {
	command := exec.Command(binary, args...)
	command.Env = append(os.Environ(), "PATH="+path)
	command.Dir = dir
	return command
}

func listDirectory(t *testing.T, root string) []string {
	t.Helper()
	names := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		names = append(names, fmt.Sprintf("%s %d %s", path, info.Size(), info.ModTime()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func runGraphMCP(t *testing.T, binary, path, root string, calls []map[string]any) []map[string]any {
	t.Helper()
	var input bytes.Buffer
	requests := []any{
		map[string]any{"jsonrpc": "2.0", "id": 0, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "graph-process-test", "version": "1"}}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
	}
	for index, call := range calls {
		requests = append(requests, map[string]any{"jsonrpc": "2.0", "id": index + 1, "method": "tools/call", "params": call})
	}
	requests = append(requests, map[string]any{"jsonrpc": "2.0", "id": len(calls) + 1, "method": "tools/list"})
	for _, request := range requests {
		if err := json.NewEncoder(&input).Encode(request); err != nil {
			t.Fatal(err)
		}
	}
	command := graphProcessCommand(binary, path, root, "mcp", root)
	command.Stdin = &input
	output, err := command.Output()
	if err != nil {
		t.Fatalf("MCP graph process failed: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != len(calls)+2 {
		t.Fatalf("MCP answered %d of %d requests:\n%s", len(lines)-2, len(calls), output)
	}
	responses := []map[string]any{}
	for index, line := range lines[1:] {
		response := readProcessJSON(t, line)
		if response["id"] != float64(index+1) {
			t.Fatalf("MCP response order: %v", response["id"])
		}
		responses = append(responses, response)
	}
	return responses
}

func copyExample(t *testing.T, root, name string) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), source, 0600); err != nil {
		t.Fatal(err)
	}
}

// One selected view per request, identical across CLI and MCP: json is the
// view, and every diagram's text is the CLI stdout. No request builds, emits
// or executes anything.
func TestGraphViewsAgreeAcrossCLIAndMCPWithoutExecution(t *testing.T) {
	binary := buildTestCLI(t)
	path, counter := graphProcessShims(t)
	root := t.TempDir()
	copyExample(t, root, "layers.ef")
	copyExample(t, root, "testing.ef")
	before := listDirectory(t, root)
	cases := []graphProcessCase{
		{"dependency-json", "layers.ef", []string{"--format", "json"}, map[string]any{"format": "json"}},
		{"dependency-focus", "layers.ef", []string{"--focus", "function:main", "--depth", "2", "--direction", "outgoing", "--edge-kind", "contains", "--edge-kind", "calls"}, map[string]any{"focus": "function:main", "depth": 2, "direction": "outgoing", "edgeKinds": []any{"calls", "contains"}}},
		{"dependency-focus-integral-depth", "layers.ef", []string{"--focus", "function:main", "--depth", "2.0"}, map[string]any{"focus": "function:main", "depth": json.Number("2.0")}},
		{"dependency-collapse-mermaid", "layers.ef", []string{"--format", "mermaid", "--collapse", "function:main"}, map[string]any{"format": "mermaid", "collapse": []any{"function:main"}}},
		{"layers-json", "layers.ef", []string{"--kind", "layers"}, map[string]any{"kind": "layers"}},
		{"layers-mermaid", "layers.ef", []string{"--kind", "layers", "--format", "mermaid"}, map[string]any{"kind": "layers", "format": "mermaid"}},
		{"layers-dot", "layers.ef", []string{"--kind", "layers", "--format", "dot"}, map[string]any{"kind": "layers", "format": "dot"}},
		{"application-json", "layers.ef", []string{"--kind", "application"}, map[string]any{"kind": "application"}},
		{"application-mermaid", "layers.ef", []string{"--kind", "application", "--mode", "build", "--format", "mermaid"}, map[string]any{"kind": "application", "mode": "build", "format": "mermaid"}},
		{"application-test-dot", "testing.ef", []string{"--kind", "application", "--mode", "test", "--format", "dot"}, map[string]any{"kind": "application", "mode": "test", "format": "dot"}},
	}
	calls := []map[string]any{}
	for _, test := range cases {
		arguments := map[string]any{"file": test.file}
		for key, value := range test.args {
			arguments[key] = value
		}
		calls = append(calls, map[string]any{"name": "project.graph", "arguments": arguments})
	}
	responses := runGraphMCP(t, binary, path, root, calls)
	for index, test := range cases {
		command := graphProcessCommand(binary, path, root, append([]string{"graph", filepath.Join(root, test.file)}, test.flags...)...)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Run(); err != nil {
			t.Fatalf("%s: CLI refused: %v %s", test.name, err, stderr.String())
		}
		result := responses[index]["result"].(map[string]any)
		if result["isError"] != false {
			t.Fatalf("%s: MCP refused: %v", test.name, result["content"])
		}
		structured := result["structuredContent"].(map[string]any)
		view := structured
		if rendering, ok := structured["rendering"].(map[string]any); ok {
			view = structured["view"].(map[string]any)
			if rendering["text"] != stdout.String() || rendering["rendererVersion"] != float64(1) {
				t.Fatalf("%s: MCP rendering differs from CLI stdout:\n%s\n---\n%v", test.name, stdout.String(), rendering["text"])
			}
		} else if !sameJSONValue(readProcessJSON(t, stdout.Bytes()), structured) {
			t.Fatalf("%s: CLI and MCP views differ", test.name)
		}
		effra := view["data"].(map[string]any)["effra"].(map[string]any)
		if effra["graphViewVersion"] != float64(1) || view["mode"] != "directed" {
			t.Fatalf("%s: not a GraphViewV1: %v", test.name, effra["graphViewVersion"])
		}
	}
	if _, err := os.Stat(counter); !os.IsNotExist(err) {
		executions, _ := os.ReadFile(counter)
		t.Fatalf("graph requests invoked a toolchain or runtime: %s", executions)
	}
	if after := listDirectory(t, root); !slices.Equal(before, after) {
		t.Fatalf("graph requests wrote into the workspace:\n%v\n%v", before, after)
	}
	// Causal control: the same shims do observe a request that runs code.
	if _, _, code := runTestCLIWithPath(t, binary, path, root, "run", filepath.Join(root, "layers.ef")); code == 0 {
		t.Fatal("ef run succeeded without the shimmed toolchain")
	}
	if executions, err := os.ReadFile(counter); err != nil || !strings.Contains(string(executions), "go") {
		t.Fatalf("toolchain shim did not observe ef run: %v %q", err, executions)
	}
	listed := responses[len(responses)-1]["result"].(map[string]any)["tools"].([]any)
	var properties map[string]any
	for _, tool := range listed {
		if tool.(map[string]any)["name"] == "project.graph" {
			properties = tool.(map[string]any)["inputSchema"].(map[string]any)["properties"].(map[string]any)
		}
	}
	help, _, _ := runTestCLI(t, binary, "--help")
	for _, option := range compiler.GraphOptions() {
		property, ok := properties[option.Field].(map[string]any)
		if !ok || !strings.Contains(string(help), option.Flag) {
			t.Fatalf("option %s is not advertised by both CLI and MCP", option.Field)
		}
		if len(option.Values) > 0 {
			if !sameJSONValue(property["enum"], option.Values) || !strings.Contains(string(help), strings.Join(option.Values, "|")) {
				t.Fatalf("option %s values differ: MCP %v", option.Field, property["enum"])
			}
		}
	}
}

// Every refusal carries the same CODE: message on CLI stderr and in the MCP
// tool error, with the invocation class exiting 2; a valid request queued
// behind the refusals still completes.
func TestGraphViewRefusalsAgreeAcrossCLIAndMCP(t *testing.T) {
	binary := buildTestCLI(t)
	path, counter := graphProcessShims(t)
	root := t.TempDir()
	copyExample(t, root, "layers.ef")
	if err := os.WriteFile(filepath.Join(root, "unchecked.ef"), []byte("fn broken() -> missing {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var wide strings.Builder
	for i := 0; i < 1100; i++ {
		fmt.Fprintf(&wide, "fn f%d(value: string) -> string { value }\n", i)
	}
	wide.WriteString("effect fn main() -> string { f0(\"x\") }\n")
	if err := os.WriteFile(filepath.Join(root, "wide.ef"), []byte(wide.String()), 0600); err != nil {
		t.Fatal(err)
	}
	refusals := []struct {
		code  string
		exit  int
		file  string
		flags []string
		args  map[string]any
	}{
		{compiler.GraphRefusalKind, 2, "layers.ef", []string{"--kind", "flow"}, map[string]any{"kind": "flow"}},
		{compiler.GraphRefusalKindUnavailable, 2, "layers.ef", []string{"--kind", "machine"}, map[string]any{"kind": "machine"}},
		{compiler.GraphRefusalFormatUnavailable, 2, "layers.ef", []string{"--format", "html"}, map[string]any{"format": "html"}},
		{compiler.GraphRefusalFormat, 2, "layers.ef", []string{"--format", "svg"}, map[string]any{"format": "svg"}},
		{compiler.GraphRefusalIncompatible, 2, "layers.ef", []string{"--depth", "1"}, map[string]any{"depth": 1}},
		{compiler.GraphRefusalIncompatible, 2, "layers.ef", []string{"--kind", "layers", "--mode", "build"}, map[string]any{"kind": "layers", "mode": "build"}},
		{compiler.GraphRefusalEdgeKind, 2, "layers.ef", []string{"--kind", "layers", "--edge-kind", "calls"}, map[string]any{"kind": "layers", "edgeKinds": []any{"calls"}}},
		{compiler.GraphRefusalMode, 2, "layers.ef", []string{"--kind", "application", "--mode", "run"}, map[string]any{"kind": "application", "mode": "run"}},
		{compiler.GraphRefusalDepthLimit, 2, "layers.ef", []string{"--focus", "function:main", "--depth", "65"}, map[string]any{"focus": "function:main", "depth": 65}},
		{compiler.GraphRefusalDepthLimit, 2, "layers.ef", []string{"--focus", "function:main", "--depth", "3000000000"}, map[string]any{"focus": "function:main", "depth": 3000000000}},
		{compiler.GraphRefusalDepthLimit, 2, "layers.ef", []string{"--focus", "function:main", "--depth", "99999999999999999999"}, map[string]any{"focus": "function:main", "depth": json.Number("99999999999999999999")}},
		{compiler.GraphRefusalInvocation, 2, "layers.ef", []string{"--focus", "function:main", "--depth", "two"}, map[string]any{"focus": "function:main", "depth": "two"}},
		{compiler.GraphRefusalInvocation, 2, "layers.ef", []string{"--focus", "function:main", "--depth", "2.5"}, map[string]any{"focus": "function:main", "depth": 2.5}},
		{compiler.GraphRefusalInvocation, 2, "layers.ef", []string{"--focus", "function:main", "--depth", "1e400"}, map[string]any{"focus": "function:main", "depth": json.Number("1e400")}},
		{compiler.GraphRefusalFocus, 1, "layers.ef", []string{"--focus", "function:absent"}, map[string]any{"focus": "function:absent"}},
		{compiler.GraphRefusalIncompatible, 2, "layers.ef", []string{"--kind", "layers", "--collapse", "x"}, map[string]any{"kind": "layers", "collapse": []any{"x"}}},
		{compiler.GraphRefusalCollapse, 1, "layers.ef", []string{"--collapse", "function:absent"}, map[string]any{"collapse": []any{"function:absent"}}},
		{compiler.GraphRefusalTarget, 1, "layers.ef", []string{"--kind", "application", "--target", "js"}, map[string]any{"kind": "application", "target": "js"}},
		{compiler.GraphRefusalPlan, 1, "layers.ef", []string{"--kind", "application", "--mode", "test"}, map[string]any{"kind": "application", "mode": "test"}},
		{compiler.GraphRefusalUnchecked, 1, "unchecked.ef", []string{"--kind", "layers"}, map[string]any{"kind": "layers"}},
		{compiler.GraphRefusalNodeLimit, 1, "wide.ef", []string{"--format", "json"}, map[string]any{"format": "json"}},
	}
	calls := []map[string]any{}
	for _, refusal := range refusals {
		arguments := map[string]any{"file": refusal.file}
		for key, value := range refusal.args {
			arguments[key] = value
		}
		calls = append(calls, map[string]any{"name": "project.graph", "arguments": arguments})
	}
	stale := []map[string]any{
		{"name": "project.graph", "arguments": map[string]any{"file": "layers.ef", "kind": "layers", "expectedRevision": "stale"}},
		{"name": "project.graph", "arguments": map[string]any{"file": "layers.ef", "kind": "layers", "expectedProducer": "stale-producer"}},
		{"name": "project.graph", "arguments": map[string]any{"file": "wide.ef", "focus": "function:main", "depth": 1}},
		{"name": "project.graph", "arguments": map[string]any{"file": "layers.ef", "kind": "layers", "format": "dot"}},
	}
	responses := runGraphMCP(t, binary, path, root, append(calls, stale...))
	for index, refusal := range refusals {
		stdout, stderr, code := runTestCLIWithPath(t, binary, path, root, append([]string{"graph", filepath.Join(root, refusal.file)}, refusal.flags...)...)
		message := strings.TrimSpace(string(stderr))
		if code != refusal.exit || len(stdout) != 0 || !strings.HasPrefix(message, refusal.code+": ") {
			t.Fatalf("CLI %v: exit=%d stdout=%q stderr=%q, want %s exit %d", refusal.flags, code, stdout, stderr, refusal.code, refusal.exit)
		}
		result, _ := responses[index]["result"].(map[string]any)
		if result == nil {
			t.Fatalf("MCP %v: no tool result: %v", refusal.args, responses[index])
		}
		text := result["content"].([]any)[0].(map[string]any)["text"]
		if result["isError"] != true || text != message || result["structuredContent"] != nil {
			t.Fatalf("MCP %v: %v, want %q", refusal.args, result, message)
		}
	}
	for offset, want := range []string{"stale semantic revision", "producer"} {
		result := responses[len(refusals)+offset]["result"].(map[string]any)
		text, _ := result["content"].([]any)[0].(map[string]any)["text"].(string)
		if result["isError"] != true || !strings.Contains(text, want) {
			t.Fatalf("stale MCP request was not refused: %v", result)
		}
	}
	for _, queued := range responses[len(refusals)+2 : len(refusals)+4] {
		result := queued["result"].(map[string]any)
		if result["isError"] != false || result["structuredContent"] == nil {
			t.Fatalf("valid request queued after refusals did not complete: %v", result["content"])
		}
	}
	focused, stderr, code := runTestCLIWithPath(t, binary, path, root, "graph", filepath.Join(root, "wide.ef"), "--focus", "function:main")
	if code != 0 || readProcessJSON(t, focused)["data"].(map[string]any)["effra"].(map[string]any)["completeness"].(map[string]any)["factNodes"].(float64) <= 1000 {
		t.Fatalf("focused CLI view over the cap failed: %s", stderr)
	}
	legacy, _, code := runTestCLIWithPath(t, binary, path, root, "graph", filepath.Join(root, "layers.ef"))
	if code != 0 || bytes.Contains(legacy, []byte("graphViewVersion")) {
		t.Fatal("option-free graph is no longer the legacy wire")
	}
	if _, err := os.Stat(counter); !os.IsNotExist(err) {
		t.Fatal("a refused graph request invoked a toolchain or runtime")
	}
}

// The public graph process must carry checked derived codec call facts through
// both transports. This also verifies that selected edges retain their node
// contracts and source spans after GraphView reference closure.
func TestGraphProcessPublishesDerivedCodecCallsAcrossCLIAndMCP(t *testing.T) {
	binary := buildTestCLI(t)
	path, counter := graphProcessShims(t)
	root := t.TempDir()
	source := `import Json "effra/json"
record Parcel { id: i64, label: string }
derive parcelJson = Json.codec<Parcel>(maxBodyBytes: 256, maxDepth: 1)
effect fn normalize(value: Parcel) -> Parcel { value }
effect fn direct(body: string) -> Parcel raises { JsonDecodeFailure } {
  run parcelJson.decode(body)
}
effect fn transfer(body: string) -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
  let decoded = run body |> parcelJson.decode()
  let normalized = run decoded |> normalize()
  run normalized |> parcelJson.encode()
}
effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
  let decoded = run direct("{\"id\":\"1\",\"label\":\"a\"}")
  run transfer("{\"id\":\"1\",\"label\":\"a\"}")
}`
	file := filepath.Join(root, "parcel.ef")
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cli, stderr, code := runTestCLIWithPath(t, binary, path, root, "graph", file, "--format", "json", "--edge-kind", "calls")
	if code != 0 {
		t.Fatalf("CLI graph refused: %d %s", code, stderr)
	}
	cliView := readProcessJSON(t, cli)
	responses := runGraphMCP(t, binary, path, root, []map[string]any{{
		"name":      "project.graph",
		"arguments": map[string]any{"file": "parcel.ef", "format": "json", "edgeKinds": []any{"calls"}},
	}})
	result := responses[0]["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("MCP graph refused: %v", result["content"])
	}
	mcpView := result["structuredContent"].(map[string]any)
	if !sameJSONValue(cliView, mcpView) {
		t.Fatalf("CLI and MCP derived-call views differ")
	}
	edges := cliView["edges"].([]any)
	want := map[string]int{
		"function:direct":            1,
		"function:transfer":          1,
		"function:normalize":         1,
		"function:parcelJson.decode": 2,
		"function:parcelJson.encode": 1,
	}
	counts := map[string]int{}
	for _, raw := range edges {
		edge := raw.(map[string]any)
		data := edge["data"].(map[string]any)["effra"].(map[string]any)
		if data["relation"] == "calls" {
			counts[edge["targetId"].(string)]++
		}
	}
	for target, count := range want {
		if counts[target] != count {
			t.Fatalf("public call topology for %s: got %d, want %d (%v)", target, counts[target], count, counts)
		}
	}
	nodes := cliView["nodes"].([]any)
	for _, target := range []string{"function:parcelJson.decode", "function:parcelJson.encode"} {
		var found map[string]any
		for _, raw := range nodes {
			node := raw.(map[string]any)
			if node["id"] == target {
				found = node
				break
			}
		}
		if found == nil {
			t.Fatalf("public view omitted derived codec node %s", target)
		}
		facts := found["data"].(map[string]any)["effra"].(map[string]any)
		if facts["source"] == "" || facts["span"] == nil || facts["contract"] == nil {
			t.Fatalf("public derived codec node lost source/span/contract closure: %v", found)
		}
	}
	if _, err := os.Stat(counter); !os.IsNotExist(err) {
		t.Fatalf("graph request invoked a toolchain or runtime: %v", err)
	}
}

func runTestCLIWithPath(t *testing.T, binary, path, dir string, args ...string) ([]byte, []byte, int) {
	t.Helper()
	command := graphProcessCommand(binary, path, dir, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err == nil {
		return stdout.Bytes(), stderr.Bytes(), 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return stdout.Bytes(), stderr.Bytes(), exit.ExitCode()
	}
	t.Fatal(err)
	return nil, nil, -1
}
