package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"ergo.local/prototype/internal/edition"
)

// The edition process tests drive the real CLI, MCP and LSP processes.

const editionDeadline = 120 * time.Second

// editionCodes matches every diagnostic code a surface can print.
var editionCodes = regexp.MustCompile(`\b(EFL?[0-9]{3}|S[0-9]{3})\b`)

const sketchCleanSource = `package p

func main() needs Console {
	Console.Println("ok")
}
`

// legacyCleanSource is a valid program of the earlier language.
const legacyCleanSource = `effect fn main() -> void {
    run Console.log("ok").provide<Console>(Stdout)
}
`

// editionWorkspace is a fresh directory, symlinks resolved, holding a go.mod
// that bounds every resolution inside it.
func editionWorkspace(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	editionWrite(t, filepath.Join(root, "go.mod"), "module workspace\n\ngo 1.27\n")
	return root
}

func editionWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func editionURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

// mcpSession runs one MCP server over root with the tool calls and returns
// each call's raw response, in order.
func mcpSession(t *testing.T, binary, root string, calls []map[string]any) []map[string]json.RawMessage {
	t.Helper()
	var input bytes.Buffer
	for _, message := range cliSmokeHandshake() {
		input.Write(mustMarshal(t, message))
		input.WriteByte('\n')
	}
	input.Write(mustMarshal(t, map[string]any{"jsonrpc": "2.0", "id": 0, "method": "tools/list"}))
	input.WriteByte('\n')
	for index, call := range calls {
		input.Write(mustMarshal(t, cliSmokeCall(index+1, call["name"].(string), call["arguments"].(map[string]any))))
		input.WriteByte('\n')
	}
	stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, input.String(), "mcp", root)
	if code != 0 {
		t.Fatalf("ergo mcp: exit %d\n%s", code, stderr)
	}
	byID := map[string]map[string]json.RawMessage{}
	for _, line := range strings.Split(strings.TrimSuffix(string(stdout), "\n"), "\n") {
		var response map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("MCP line %q: %v", line, err)
		}
		byID[string(response["id"])] = response
	}
	responses := make([]map[string]json.RawMessage, 0, len(calls)+1)
	for id := 0; id <= len(calls); id++ {
		response, ok := byID[strconv.Itoa(id)]
		if !ok {
			t.Fatalf("no MCP response %d in %s", id, stdout)
		}
		responses = append(responses, response)
	}
	return responses
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// mcpToolText returns a tool call's text content and whether it is a tool
// error.
func mcpToolText(t *testing.T, response map[string]json.RawMessage) (string, bool) {
	t.Helper()
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(response["result"], &result); err != nil || len(result.Content) == 0 {
		t.Fatalf("not a tool result: %s %s", response["result"], response["error"])
	}
	return result.Content[0].Text, result.IsError
}

// mcpFileTools lists the tools tools/list advertises with a file argument,
// with their required arguments.
func mcpFileTools(t *testing.T, listResponse map[string]json.RawMessage) map[string][]string {
	t.Helper()
	var listed struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listResponse["result"], &listed); err != nil {
		t.Fatal(err)
	}
	tools := map[string][]string{}
	for _, tool := range listed.Tools {
		if _, ok := tool.InputSchema.Properties["file"]; ok {
			tools[tool.Name] = tool.InputSchema.Required
		}
	}
	if len(tools) < 10 {
		t.Fatalf("tools/list advertises only %d file tools", len(tools))
	}
	return tools
}

// mcpFileArguments are the arguments a file tool is called with. A tool
// that requires an argument not listed here fails the test, so a tool added
// later must be given one.
func mcpFileArguments(t *testing.T, tool string, required []string, file string) map[string]any {
	t.Helper()
	arguments := map[string]any{"file": file}
	switch tool {
	case "code.inspect", "code.explain", "code.type":
		arguments["symbol"] = "main"
	case "code.typeAt":
		arguments["offset"] = 0
	}
	for _, key := range required {
		if _, ok := arguments[key]; !ok {
			t.Fatalf("tool %s requires %s; give it an argument in mcpFileArguments", tool, key)
		}
	}
	return arguments
}

// lspSession runs one LSP server with the messages framed after the
// handshake, then shuts it down, and returns every message as raw fields.
func lspSession(t *testing.T, binary, dir string, messages []map[string]any) []map[string]json.RawMessage {
	t.Helper()
	var input bytes.Buffer
	all := append([]map[string]any{lspSmokeCall("initialize", map[string]any{}, "init"), lspSmokeCall("initialized", map[string]any{}, nil)}, messages...)
	all = append(all, lspSmokeCall("shutdown", nil, "shutdown"), lspSmokeCall("exit", nil, nil))
	for _, message := range all {
		body := mustMarshal(t, message)
		fmt.Fprintf(&input, "Content-Length: %d\r\n\r\n", len(body))
		input.Write(body)
	}
	stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, dir, input.String(), "lsp")
	if code != 0 {
		t.Fatalf("ergo lsp: exit %d\n%s", code, stderr)
	}
	var decoded []map[string]json.RawMessage
	for data := stdout; len(data) > 0; {
		header, rest, found := bytes.Cut(data, []byte("\r\n\r\n"))
		count, err := strconv.Atoi(string(bytes.TrimPrefix(header, []byte("Content-Length: "))))
		if !found || err != nil || len(rest) < count {
			t.Fatalf("invalid LSP frame %q", header)
		}
		var message map[string]json.RawMessage
		if err := json.Unmarshal(rest[:count], &message); err != nil {
			t.Fatal(err)
		}
		decoded = append(decoded, message)
		data = rest[count:]
	}
	return decoded
}

func lspReply(t *testing.T, messages []map[string]json.RawMessage, id string) map[string]json.RawMessage {
	t.Helper()
	want := string(mustMarshal(t, id))
	for _, message := range messages {
		if string(message["id"]) == want {
			return message
		}
	}
	t.Fatalf("no LSP reply %s", id)
	return nil
}

func lspOpen(uri, text string) map[string]any {
	return lspSmokeCall("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "ergo", "version": 1, "text": text}}, nil)
}

// lspPublished returns the diagnostics of the publication for uri.
func lspPublished(t *testing.T, messages []map[string]json.RawMessage, uri string) []map[string]any {
	t.Helper()
	for _, message := range messages {
		if string(message["method"]) != `"textDocument/publishDiagnostics"` {
			continue
		}
		var params struct {
			URI         string           `json:"uri"`
			Diagnostics []map[string]any `json:"diagnostics"`
		}
		if err := json.Unmarshal(message["params"], &params); err != nil {
			t.Fatal(err)
		}
		if params.URI == uri {
			return params.Diagnostics
		}
	}
	t.Fatalf("no publication for %s", uri)
	return nil
}

// onlyCodes fails unless every diagnostic code in text is one of want and
// at least one occurs.
func onlyCodes(t *testing.T, context, text string, want ...string) {
	t.Helper()
	found := editionCodes.FindAllString(text, -1)
	if len(found) == 0 {
		t.Fatalf("%s: no diagnostic code in %q", context, text)
	}
	for _, code := range found {
		if !slices.Contains(want, code) {
			t.Fatalf("%s: code %s beside %v in %q", context, code, want, text)
		}
	}
}

func TestEditionsBuildSideBySide(t *testing.T) {
	binary := buildTestCLI(t)
	workspace := smokeWorkspace(t, "examples/editions", "go.mod", "ergo.bindings.json")
	mainFile := map[string]string{"legacy": "main.ef", "sketch": "main.ergo"}
	for _, module := range []string{"legacy", "sketch"} {
		dir := filepath.Join("examples", "editions", module)
		expected, err := os.ReadFile(filepath.Join(workspace, dir, "expected.out"))
		if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(workspace, "out", module)
		stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, workspace, "", "build", filepath.Join(dir, mainFile[module]), "-o", output)
		if code != 0 {
			t.Fatalf("ergo build %s: exit %d\n%s%s", module, code, stdout, stderr)
		}
		got, err := exec.Command(output).Output()
		if err != nil {
			t.Fatalf("%s executable: %v", module, err)
		}
		if !bytes.Equal(got, expected) {
			t.Fatalf("%s printed %q, want %q", module, got, expected)
		}
		stdout, stderr, code = runSmokeCLI(t, editionDeadline, binary, workspace, "", "run", filepath.Join(dir, mainFile[module]))
		if code != 0 || !bytes.Equal(stdout, expected) {
			t.Fatalf("ergo run %s: exit %d, stdout %q, want %q\n%s", module, code, stdout, expected, stderr)
		}
	}
	// The editions are what the manifests say.
	for module, want := range map[string]string{"legacy": `"edition":"legacy","source":"manifest"`, "sketch": `"edition":"2026","source":"manifest"`} {
		stdout, _, code := runSmokeCLI(t, editionDeadline, binary, workspace, "", "edition", filepath.Join("examples", "editions", module, mainFile[module]))
		if code != 0 || !strings.Contains(string(stdout), want) {
			t.Fatalf("ergo edition %s = %s (exit %d), want %s", module, stdout, code, want)
		}
	}
}

// editionCase is one source file whose edition every surface reports.
type editionCase struct {
	rel  string
	want string // a fragment of the projection, or "" for generated cases
}

func TestEditionAgreesAcrossCLIMCPLSP(t *testing.T) {
	binary := buildTestCLI(t)
	root := editionWorkspace(t)
	write := func(rel, content string) { editionWrite(t, filepath.Join(root, filepath.FromSlash(rel)), content) }
	cases := []editionCase{
		{"default/main.ef", `{"edition":"legacy","source":"default","manifest":""}`},
		{"legacy/main.ef", `"edition":"legacy","source":"manifest"`},
		{"e2026/main.ef", `"edition":"2026","source":"manifest"`},
		{"nested/inner/main.ef", `"edition":"legacy","source":"manifest","manifest":"` + filepath.Join(root, "nested", "inner", "ergo.json") + `"`},
		{"nested/sub/main.ef", `"edition":"2026","source":"manifest","manifest":"` + filepath.Join(root, "nested", "ergo.json") + `"`},
		{"above/mod/main.ef", `{"edition":"legacy","source":"default","manifest":""}`},
		{"unsupported/main.ef", `"edition":null,"source":"manifest"`},
		{"malformed/main.ef", `"edition":null,"source":"manifest"`},
	}
	write("legacy/ergo.json", `{"edition":"legacy"}`)
	write("e2026/ergo.json", `{"edition":"2026"}`)
	write("nested/ergo.json", `{"edition":"2026"}`)
	write("nested/inner/ergo.json", `{"edition":"legacy"}`)
	write("above/ergo.json", `{"edition":"2026"}`)
	write("above/mod/go.mod", "module above\n")
	write("unsupported/ergo.json", `{"edition":"2099"}`)
	write("malformed/ergo.json", `{"edition":"2026","extra":true}`)
	// Generated cases: random paths with random manifests and module roots.
	random := rand.New(rand.NewPCG(194, 2026))
	manifests := []string{`{"edition":"legacy"}`, `{"edition":"2026"}`, `{"edition":"Legacy"}`, `{"edition":"2026"`, `{"edition":"2026","name":"x"}`, `{}`}
	for index := range 24 {
		parts := []string{"gen", strconv.Itoa(index)}
		for range 1 + random.IntN(4) {
			parts = append(parts, fmt.Sprintf("d%d", random.IntN(3)))
		}
		for depth := 2; depth <= len(parts); depth++ {
			dir := strings.Join(parts[:depth], "/")
			if random.IntN(3) == 0 {
				if _, err := os.Lstat(filepath.Join(root, dir, "ergo.json")); err != nil {
					write(dir+"/ergo.json", manifests[random.IntN(len(manifests))])
				}
			}
			if random.IntN(6) == 0 {
				write(dir+"/go.mod", "module generated\n")
			}
		}
		cases = append(cases, editionCase{rel: strings.Join(parts, "/") + "/main.ef"})
	}
	var calls []map[string]any
	var requests []map[string]any
	for index, c := range cases {
		write(c.rel, "package p\n")
		calls = append(calls, map[string]any{"name": "project.edition", "arguments": map[string]any{"file": c.rel}})
		requests = append(requests, lspSmokeCall("ergo/edition", map[string]any{"textDocument": map[string]any{"uri": editionURI(filepath.Join(root, filepath.FromSlash(c.rel)))}}, fmt.Sprintf("edition-%d", index)))
	}
	mcp := mcpSession(t, binary, root, calls)
	lsp := lspSession(t, binary, root, requests)
	refusals := 0
	for index, c := range cases {
		path := filepath.Join(root, filepath.FromSlash(c.rel))
		stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", "edition", path)
		cli := strings.TrimSuffix(string(stdout), "\n")
		if (code != 0) != strings.Contains(cli, `"diagnostic":`) || code > 1 {
			t.Fatalf("%s: ergo edition exit %d for %s\n%s", c.rel, code, cli, stderr)
		}
		mcpText, isError := mcpToolText(t, mcp[index+1])
		lspResult := string(lspReply(t, lsp, fmt.Sprintf("edition-%d", index))["result"])
		if isError || cli != mcpText || cli != lspResult {
			t.Fatalf("%s: surfaces disagree\nCLI %s\nMCP %s (error %v)\nLSP %s", c.rel, cli, mcpText, isError, lspResult)
		}
		if want := string(edition.ResolveDocument(path).JSON()); cli != want {
			t.Fatalf("%s: surfaces report %s, the resolver %s", c.rel, cli, want)
		}
		if c.want != "" && !strings.Contains(cli, c.want) {
			t.Fatalf("%s: %s lacks %s", c.rel, cli, c.want)
		}
		if strings.Contains(cli, `"code":"EF144"`) {
			refusals++
		}
	}
	if refusals < 3 {
		t.Fatalf("only %d cases reached EF144", refusals)
	}
}

// diagnosticKey is one finding as every surface can state it: the code and
// the zero-based UTF-16 range, plus the byte span where the surface has it.
type diagnosticKey struct {
	Code  string
	Range string
	Span  string
}

func reportKeys(t *testing.T, report []byte, withSpan bool) []diagnosticKey {
	t.Helper()
	var decoded struct {
		Diagnostics []struct {
			Code string          `json:"code"`
			Span json.RawMessage `json:"span"`
			LSP  struct {
				Range json.RawMessage `json:"range"`
			} `json:"lsp"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(report, &decoded); err != nil {
		t.Fatalf("report %s: %v", report, err)
	}
	keys := []diagnosticKey{}
	for _, finding := range decoded.Diagnostics {
		key := diagnosticKey{Code: finding.Code, Range: compactJSON(t, finding.LSP.Range)}
		if withSpan {
			key.Span = compactJSON(t, finding.Span)
		}
		keys = append(keys, key)
	}
	return keys
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return string(mustMarshal(t, value))
}

func TestDiagnosticsRouteByEditionAcrossSurfaces(t *testing.T) {
	binary := buildTestCLI(t)
	root := editionWorkspace(t)
	editionWrite(t, filepath.Join(root, "new", "ergo.json"), `{"edition":"2026"}`)
	files := []struct {
		rel, text, prefix string
		clean             bool
	}{
		{"new/legacy-syntax.ef", legacyCleanSource, "S", false},
		{"new/unknown-name.ef", "package p\n\nfunc main() needs Console {\n\tConsole.Println(\"🌍 ü\" + mïssing)\n}\n", "S", false},
		{"new/lexer.ef", "package p\n\n// ünïcode\nfunc main() needs Console { Console.Println(\"日本\") $ }\n", "S", false},
		{"new/clean.ef", sketchCleanSource, "", true},
		{"old/sketch-syntax.ef", sketchCleanSource, "EF", false},
		{"old/clean.ef", legacyCleanSource, "", true},
	}
	var calls []map[string]any
	var messages []map[string]any
	for _, file := range files {
		editionWrite(t, filepath.Join(root, filepath.FromSlash(file.rel)), file.text)
		calls = append(calls, map[string]any{"name": "project.diagnostics", "arguments": map[string]any{"file": file.rel}})
		messages = append(messages, lspOpen(editionURI(filepath.Join(root, filepath.FromSlash(file.rel))), file.text))
	}
	mcp := mcpSession(t, binary, root, calls)
	lsp := lspSession(t, binary, root, messages)
	for index, file := range files {
		path := filepath.Join(root, filepath.FromSlash(file.rel))
		// MCP and the LSP read the legacy spelling until retire-ef removes
		// them (#205); the CLI reads a 2026 source as .ergo (#221).
		cliPath := path
		if strings.HasPrefix(file.rel, "new/") {
			cliPath = strings.TrimSuffix(path, ".ef") + ".ergo"
			editionWrite(t, cliPath, file.text)
		}
		stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", "diagnostics", cliPath, "--json")
		if (code == 0) != file.clean || code > 1 {
			t.Fatalf("%s: ergo diagnostics exit %d\n%s%s", file.rel, code, stdout, stderr)
		}
		cli := reportKeys(t, stdout, true)
		var structured struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
		}
		if err := json.Unmarshal(mcp[index+1]["result"], &structured); err != nil || structured.StructuredContent == nil {
			t.Fatalf("%s: MCP result %s", file.rel, mcp[index+1]["result"])
		}
		fromMCP := reportKeys(t, structured.StructuredContent, true)
		published := lspPublished(t, lsp, editionURI(path))
		fromLSP := []diagnosticKey{}
		for _, diagnostic := range published {
			fromLSP = append(fromLSP, diagnosticKey{Code: diagnostic["code"].(string), Range: string(mustMarshal(t, diagnostic["range"]))})
		}
		if !slices.Equal(cli, fromMCP) {
			t.Fatalf("%s: CLI %v, MCP %v", file.rel, cli, fromMCP)
		}
		withoutSpans := make([]diagnosticKey, len(cli))
		for i, key := range cli {
			withoutSpans[i] = diagnosticKey{Code: key.Code, Range: key.Range}
		}
		if !slices.Equal(withoutSpans, fromLSP) {
			t.Fatalf("%s: CLI %v, LSP %v", file.rel, withoutSpans, fromLSP)
		}
		if file.clean != (len(cli) == 0) {
			t.Fatalf("%s: findings %v, want clean=%v", file.rel, cli, file.clean)
		}
		for _, key := range cli {
			if !strings.HasPrefix(key.Code, file.prefix) || file.prefix == "EF" && strings.HasPrefix(key.Code, "S") || file.prefix == "S" && !regexp.MustCompile(`^S[0-9]{3}$`).MatchString(key.Code) {
				t.Fatalf("%s: code %s, want %s codes only (%v)", file.rel, key.Code, file.prefix, cli)
			}
			if key.Range == "null" || key.Span == `{"column":0,"length":0,"line":0,"offset":0}` {
				t.Fatalf("%s: finding %v has no location", file.rel, key)
			}
		}
	}
}

// cliInvocation is the argument list a file command runs with on FILE. A
// command missing here fails the test, so a command added to fileCommands
// must be given one.
func cliInvocation(t *testing.T, command, file, output string) []string {
	t.Helper()
	switch command {
	case "edition", "check", "run", "lint", "graph", "test", "fmt":
		return []string{command, file}
	case "diagnostics":
		return []string{command, file, "--json"}
	case "build":
		return []string{command, file, "-o", output}
	case "query":
		return []string{command, file, "0"}
	case "type":
		return []string{command, file, "--symbol", "main"}
	case "inspect", "explain":
		return []string{command, file, "main"}
	case "lint test":
		return []string{"lint", "test", file}
	case "lint receipt":
		return []string{"lint", "receipt", file}
	}
	t.Fatalf("no invocation for file command %q; add one to cliInvocation", command)
	return nil
}

func TestUnsupportedEditionIsNeverPassedThrough(t *testing.T) {
	binary := buildTestCLI(t)
	workspace := smokeWorkspace(t, "internal/edition/testdata", "go.mod")
	for _, fixture := range []string{"unsupported", "malformed"} {
		rel := filepath.Join("internal", "edition", "testdata", fixture, "main.ef")
		path := filepath.Join(workspace, rel)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// Every file command of the CLI.
		for _, command := range fileCommands {
			output := filepath.Join(workspace, "out", fixture+"-"+strings.ReplaceAll(command.name, " ", "-"))
			args := cliInvocation(t, command.name, rel, output)
			stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, workspace, "", args...)
			context := fmt.Sprintf("%s: ergo %s", fixture, strings.Join(args, " "))
			if code == 0 {
				t.Fatalf("%s exited 0\n%s%s", context, stdout, stderr)
			}
			onlyCodes(t, context, string(stdout)+string(stderr), edition.CodeUnsupported)
			if _, err := os.Lstat(output); err == nil {
				t.Fatalf("%s wrote %s", context, output)
			}
		}
		if _, err := os.Lstat(filepath.Join(workspace, "dist")); err == nil {
			t.Fatalf("%s: a refused command wrote dist/", fixture)
		}
		// Every MCP tool with a file argument.
		list := mcpSession(t, binary, workspace, nil)[0]
		tools := mcpFileTools(t, list)
		names := slices.Sorted(maps.Keys(tools))
		var calls []map[string]any
		for _, name := range names {
			calls = append(calls, map[string]any{"name": name, "arguments": mcpFileArguments(t, name, tools[name], filepath.ToSlash(rel))})
		}
		responses := mcpSession(t, binary, workspace, calls)
		for index, name := range names {
			raw := string(responses[index+1]["result"]) + string(responses[index+1]["error"])
			onlyCodes(t, fixture+": MCP "+name, raw, edition.CodeUnsupported)
		}
		// LSP didOpen, formatting and navigation.
		uri := editionURI(path)
		messages := lspSession(t, binary, workspace, []map[string]any{
			lspOpen(uri, string(original)),
			lspSmokeCall("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 4, "insertSpaces": true}}, "format"),
			lspSmokeCall("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 3, "character": 4}}, "hover"),
		})
		published := lspPublished(t, messages, uri)
		if len(published) != 1 || published[0]["code"] != edition.CodeUnsupported {
			t.Fatalf("%s: didOpen published %v, want EF144 alone", fixture, published)
		}
		format := lspReply(t, messages, "format")
		if format["error"] == nil {
			t.Fatalf("%s: formatting answered %s", fixture, format["result"])
		}
		onlyCodes(t, fixture+": LSP formatting", string(format["error"]), edition.CodeUnsupported)
		if hover := lspReply(t, messages, "hover"); string(hover["result"]) != "null" {
			t.Fatalf("%s: hover answered %s", fixture, hover["result"])
		}
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, original) {
			t.Fatalf("%s: the source changed", fixture)
		}
	}
}

func TestUnavailableOperationsReportEF145(t *testing.T) {
	binary := buildTestCLI(t)
	workspace := smokeWorkspace(t, "examples/editions/sketch", "go.mod")
	rel := filepath.Join("examples", "editions", "sketch", "main.ergo")
	path := filepath.Join(workspace, rel)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	supported := []string{"edition", "check", "diagnostics", "build", "run", "fmt"}
	tickets := regexp.MustCompile(`#(196|204)\b`)
	for _, command := range fileCommands {
		if slices.Contains(supported, command.name) {
			continue
		}
		args := cliInvocation(t, command.name, rel, "")
		stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, workspace, "", args...)
		context := "ergo " + strings.Join(args, " ")
		if code == 0 || len(stdout) != 0 {
			t.Fatalf("%s: exit %d with stdout %q; want a refusal and no projection", context, code, stdout)
		}
		onlyCodes(t, context, string(stderr), edition.CodeUnavailable)
		if !tickets.Match(stderr) || !strings.Contains(string(stderr), `"2026"`) {
			t.Fatalf("%s: %q names no edition or ticket", context, stderr)
		}
	}
	// build --receipt is unavailable even though build is not.
	output, receiptPath := filepath.Join(workspace, "out", "program"), filepath.Join(workspace, "out", "receipt.json")
	stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, workspace, "", "build", rel, "-o", output, "--receipt", receiptPath)
	if code == 0 || len(stdout) != 0 {
		t.Fatalf("build --receipt: exit %d, stdout %q", code, stdout)
	}
	onlyCodes(t, "build --receipt", string(stderr), edition.CodeUnavailable)
	for _, written := range []string{output, receiptPath} {
		if _, err := os.Lstat(written); err == nil {
			t.Fatalf("build --receipt wrote %s", written)
		}
	}
	// MCP and the LSP read the legacy spelling until retire-ef removes them
	// (#205); they get a copy of the module's source under that name.
	rel = filepath.Join("examples", "editions", "sketch", "main.ef")
	editionWrite(t, filepath.Join(workspace, rel), string(original))
	// Every MCP file tool but the supported ones.
	list := mcpSession(t, binary, workspace, nil)[0]
	tools := mcpFileTools(t, list)
	var names []string
	var calls []map[string]any
	for name, required := range tools {
		if slices.Contains([]string{"project.edition", "project.check", "project.diagnostics", "code.format"}, name) {
			continue
		}
		names = append(names, name)
		calls = append(calls, map[string]any{"name": name, "arguments": mcpFileArguments(t, name, required, filepath.ToSlash(rel))})
	}
	responses := mcpSession(t, binary, workspace, calls)
	for index, name := range names {
		text, isError := mcpToolText(t, responses[index+1])
		if !isError || !strings.HasPrefix(text, edition.CodeUnavailable+": ") {
			t.Fatalf("MCP %s: %q (error %v), want EF145", name, text, isError)
		}
		onlyCodes(t, "MCP "+name, string(responses[index+1]["result"]), edition.CodeUnavailable)
	}
	// LSP: the formatted module needs no formatting edit; navigation has no
	// answer.
	uri := editionURI(filepath.Join(workspace, rel))
	messages := lspSession(t, binary, workspace, []map[string]any{
		lspOpen(uri, string(original)),
		lspSmokeCall("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 4, "insertSpaces": true}}, "format"),
		lspSmokeCall("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 5, "character": 6}}, "hover"),
		lspSmokeCall("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 5, "character": 6}}, "definition"),
	})
	if published := lspPublished(t, messages, uri); len(published) != 0 {
		t.Fatalf("the clean 2026 module published %v", published)
	}
	if format := lspReply(t, messages, "format"); format["error"] != nil || string(format["result"]) != "[]" {
		t.Fatalf("formatting answered %s %s, want no edits", format["result"], format["error"])
	}
	for _, id := range []string{"hover", "definition"} {
		if reply := lspReply(t, messages, id); string(reply["result"]) != "null" {
			t.Fatalf("%s answered %s", id, reply["result"])
		}
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, original) {
		t.Fatal("the source changed")
	}
}
