package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Each replacement is checked source spelling what an EF003 help names, so
// the help stays true for the current language.
var absentReplacements = map[string]string{
	"option": `import Data "effra/data"
fn find(key: string) -> Data.Option<string> { if key == "a" { Data.Option.Some { value: key } } else { Data.Option<string>.None {} } }
effect fn main() -> void { void }
`,
	"fail": `error Missing
effect fn find(key: string) -> string raises { Missing } { fail Missing }
effect fn main() -> string { run find("a").catch<Missing>("fallback") }
`,
	"recover": `error Missing
effect fn find(key: string) -> string raises { Missing } { fail Missing }
effect fn main() -> string { scope { run find("a").catch<Missing>("fallback") } }
`,
	"conditional": `import Data "effra/data"
fn pick(b: bool) -> string { if b { "a" } else { "b" } }
fn label(result: Data.Result<string, string>) -> string { match result { Data.Result.Ok { value } => value, Data.Result.Err { error } => error } }
effect fn main() -> void { void }
`,
	"codec": `import Convert "effra/conversions"
record User { name: string }
effect fn decode(input: string) -> User { User { name: input } }
effect fn encode(user: User) -> string { user.name }
effect fn main() -> string { let converter = Convert.witness(decode, encode); let user = run converter.decode("Ada"); user.name }
`,
	"enum": `enum Input { Text { value: string }, Flag { value: bool } }
fn describe(input: Input) -> string { match input { Input.Text { value } => value, Input.Flag { value } => if value { "yes" } else { "no" } } }
effect fn main() -> void { void }
`,
	"run": `effect fn step() -> string { "x" }
effect fn main() -> string { let value = run step(); value }
`,
	"service": `service Counter { effect fn current() -> string }
impl Fixed for Counter { effect fn current() -> string { "0" } }
layer App { Counter = Fixed }
fn initial() -> string { "0" }
effect fn main() -> string { run Counter.current().provide(App) }
`,
	"import": `import Data "effra/data"
fn none() -> Data.Option<string> { Data.Option<string>.None {} }
effect fn main() -> void { void }
`,
	"booleans": `fn not(b: bool) -> bool { if b { false } else { true } }
fn both(a: bool, b: bool) -> bool { if a { b } else { false } }
fn differ(a: string, b: string) -> bool { if a == b { false } else { true } }
effect fn main() -> void { void }
`,
	"else": `effect fn main() -> void uses { Console } { if true { run Console.log("x") } else { void } }
`,
	"named": `effect fn shout(x: string) -> string { x + "!" }
effect fn apply(f: effect fn(string) -> string, x: string) -> string { run f(x) }
effect fn main() -> string { run apply(shout, "a") }
`,
	"recursion": `fn countdown(n: i64) -> string { if n == 0 { "done" } else { countdown(0) } }
effect fn main() -> void { void }
`,
	"let": `fn f() -> string { let x = "a"; let next = x + "b"; next }
effect fn main() -> void { void }
`,
}

// absentProbes are the spike probes (p01-p10, p25-p27, p29, p30, p32, p33)
// plus the other spellings of each family. before is the f4c16b0 diagnostic
// count, which recovery must not exceed.
var absentProbes = []struct {
	name, source, marker, token, message, help, replacement string
	before                                                  int
}{
	{"p01-null", "effect fn main() -> void { let x = null; void }", "= null", "null", "Effra has no `null` value", "Data.Option<T>", "option", 1},
	{"p02-nil", "effect fn main() -> void { let x = nil; void }", "= nil", "nil", "Effra has no `nil` value", "Data.Option<T>", "option", 1},
	{"undefined", "fn f() -> string { undefined }", "undefined", "undefined", "Effra has no `undefined` value", "Data.Option<T>", "option", 2},
	{"p03-throw", `effect fn main() -> void { throw "x" }`, "throw", "throw", "Effra has no `throw`", "`fail E`", "fail", 2},
	{"throw-call", `fn f() -> string { throw("x") }`, "throw", "throw", "Effra has no `throw`", "`fail E`", "fail", 2},
	{"p04-try", "effect fn main() -> void { try { void } catch { void } }", "try", "try", "Effra has no `try`/`catch` blocks", "`.catch<E>(fallback)`", "recover", 1},
	{"catch-parameter", "effect fn main() -> void { try { } catch (e) { void } }", "catch", "catch", "Effra has no `try`/`catch` blocks", "`scope { ... }`", "recover", 1},
	{"p05-ternary", `fn pick(b: bool) -> string { b ? "a" : "b" }`, "?", "?", "the `?` operator is not yet supported", "`if c { a } else { b }`", "conditional", 1},
	{"result-question", "import Data \"effra/data\"\nfn g() -> Data.Result<string, string> { Data.Result<string, string>.Ok { value: \"a\" } }\nfn f() -> string { let x = g()?; \"a\" }", ")?", "?", "the `?` operator is not yet supported", "inspect a Data.Result with `match`", "conditional", 1},
	{"p06-as", "fn cast(x: string) -> string { x as string }", "x as", "as", "Effra has no `as` type assertions", "Convert.Codec", "codec", 3},
	{"p07-async", "async fn main() -> void { await void }", "async", "async", "Effra has no `async` functions", "`run`", "run", 1},
	{"await", "effect fn main() -> void { await void }", "await", "await", "Effra has no `await`", "`effect fn`", "run", 1},
	{"p08-modlet", "let counter = 0\neffect fn main() -> void { void }", "let", "let", "module-level `let` bindings are not yet supported", "zero-argument function", "service", 1},
	{"p09-closure", `fn mk() -> string { let f = fn(x: string) -> string { x }; "a" }`, "= fn(", "fn", "anonymous functions and closure captures are not yet supported", "named module function", "named", 1},
	{"effect-closure", "effect fn main() -> void { let f = effect fn() -> void { void }; void }", "= effect fn(", "effect", "anonymous functions and closure captures are not yet supported", "named module function", "named", 1},
	{"p10-if-no-else", `effect fn main() -> void uses { Console } { if true { run Console.log("x") } }`, "if true", "if", "Effra has no `if` without `else`", "`else { void }`", "else", 1},
	{"p25-dyn-import", `effect fn f() -> void { let m = import("x"); void }`, "import", "import", "Effra has no dynamic `import(...)`", "top of the module", "import", 1},
	{"import-declaration", "import(\"x\")\neffect fn main() -> void { void }", "import", "import", "Effra has no dynamic `import(...)`", "top of the module", "import", 1},
	{"p26-assign", `fn f() -> string { let x = "a"; x = "b"; x }`, `x = "b"`, "=", "assignment is not yet supported", "new name with `let`", "let", 1},
	{"p27-loop", `fn f() -> string { for x in y { x }; "a" }`, "for", "for", "`for` loops are not yet supported", "recursive named function", "recursion", 4},
	{"while", `fn f() -> string { while true { "a" }; "a" }`, "while", "while", "`while` loops are not yet supported", "recursive named function", "recursion", 1},
	{"p29-unknown", `fn f(x: unknown) -> string { "a" }`, "unknown", "unknown", "Effra has no `unknown` type", "closed enum", "enum", 1},
	{"p30-any", `fn f(x: any) -> string { "a" }`, "any", "any", "Effra has no `any` type", "closed enum", "enum", 1},
	{"return-any", `fn f() -> any { "a" }`, "any", "any", "Effra has no `any` type", "closed enum", "enum", 2},
	{"field-unknown", "record Box { value: unknown }\neffect fn main() -> void { void }", "unknown", "unknown", "Effra has no `unknown` type", "closed enum", "enum", 1},
	{"configuration-any", "service S { effect fn get() -> string }\nimpl Fixed(config: any) for S { effect fn get() -> string { \"a\" } }\neffect fn main() -> void { void }", "any", "any", "Effra has no `any` type", "closed enum", "enum", 1},
	{"grouped-param", `fn f(x: (unknown)) -> string { "a" }`, "(unknown)", "unknown", "Effra has no `unknown` type", "closed enum", "enum", 1},
	{"grouped-return", `fn f() -> ( any ) { "a" }`, "( any )", "any", "Effra has no `any` type", "closed enum", "enum", 2},
	{"grouped-field", "record Box { value: (unknown) }\neffect fn main() -> void { void }", "(unknown)", "unknown", "Effra has no `unknown` type", "closed enum", "enum", 1},
	{"grouped-variant-field", "enum E { A(x: (any)) }\neffect fn main() -> void { void }", "(any)", "any", "Effra has no `any` type", "closed enum", "enum", 1},
	{"grouped-configuration", "service S { effect fn get() -> string }\nimpl Fixed(config: ((any))) for S { effect fn get() -> string { \"a\" } }\neffect fn main() -> void { void }", "((any))", "any", "Effra has no `any` type", "closed enum", "enum", 1},
	{"p32-bang", "fn f(b: bool) -> bool { !b }", "!", "!", "Effra has no `!` operator", "`if b { false } else { true }`", "booleans", 1},
	{"inequality", "fn f(a: string, b: string) -> bool { a != b }", "!=", "!=", "Effra has no `!=` operator", "`==`", "booleans", 1},
	{"p33-and", "fn f(a: bool, b: bool) -> bool { a && b }", "&&", "&&", "Effra has no `&&` operator", "`if a { b } else { false }`", "booleans", 1},
}

func absentFinding(t *testing.T, report map[string]any, name string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, raw := range report["diagnostics"].([]any) {
		finding := raw.(map[string]any)
		if finding["code"] == "EF003" {
			if found != nil {
				t.Fatalf("%s reported more than one absent construct: %v", name, report["diagnostics"])
			}
			found = finding
		}
	}
	if found == nil {
		t.Fatalf("%s produced no EF003: %v", name, report["diagnostics"])
	}
	return found
}

func TestAbsentSyntaxDiagnosticsAcrossCLIMCPAndLSP(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	cliFindings := map[string]map[string]any{}
	var mcpInput bytes.Buffer
	var lspInput bytes.Buffer
	encode := func(value any) {
		if err := json.NewEncoder(&mcpInput).Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	encode(map[string]any{"jsonrpc": "2.0", "id": 0, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "absent-syntax-test", "version": "1"}}})
	encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	lspInput.Write(voidLSPFrame(t, map[string]any{"jsonrpc": "2.0", "id": 0, "method": "initialize", "params": map[string]any{}}))
	lspInput.Write(voidLSPFrame(t, map[string]any{"jsonrpc": "2.0", "method": "initialized"}))
	for index, probe := range absentProbes {
		file := filepath.Join(root, probe.name+".ef")
		if err := os.WriteFile(file, []byte(probe.source), 0600); err != nil {
			t.Fatal(err)
		}
		stdout, _, code := runTestCLI(t, binary, "diagnostics", file, "--json")
		if code != 1 {
			t.Fatalf("%s was not refused: exit %d", probe.name, code)
		}
		report := readProcessJSON(t, stdout)
		if report["checked"] == true {
			t.Fatalf("%s was admitted as checked source", probe.name)
		}
		if count := len(report["diagnostics"].([]any)); count > probe.before {
			t.Fatalf("%s recovery cascaded: %d diagnostics, %d before", probe.name, count, probe.before)
		}
		finding := absentFinding(t, report, probe.name)
		offset := strings.Index(probe.source, probe.marker) + strings.Index(probe.marker, probe.token)
		span := finding["span"].(map[string]any)
		if finding["message"] != probe.message || span["offset"] != float64(offset) || span["length"] != float64(len(probe.token)) {
			t.Fatalf("%s: got %q at %v, want %q at %d+%d", probe.name, finding["message"], span, probe.message, offset, len(probe.token))
		}
		help, _ := finding["help"].(string)
		if !strings.Contains(help, probe.help) {
			t.Fatalf("%s help %q does not name %q", probe.name, help, probe.help)
		}
		lsp := finding["lsp"].(map[string]any)
		if lsp["code"] != "EF003" || lsp["message"] != probe.message+"\nhelp: "+help {
			t.Fatalf("%s LSP projection lost the message or help: %v", probe.name, lsp)
		}
		cliFindings[probe.name] = finding
		encode(map[string]any{"jsonrpc": "2.0", "id": index + 1, "method": "tools/call", "params": map[string]any{"name": "project.diagnostics", "arguments": map[string]any{"file": probe.name + ".ef"}}})
		lspInput.Write(voidLSPFrame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": "file:///absent/" + probe.name + ".ef", "languageId": "effra", "version": 1, "text": probe.source}}}))
		// Closing keeps the probes under the server's open-document limit; the
		// empty publication on close carries no EF003 to collect.
		lspInput.Write(voidLSPFrame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didClose", "params": map[string]any{"textDocument": map[string]any{"uri": "file:///absent/" + probe.name + ".ef"}}}))
	}

	// MCP reports the same finding, help and editor projection.
	command := exec.Command(binary, "mcp", root)
	command.Stdin = &mcpInput
	output, err := command.Output()
	if err != nil {
		t.Fatalf("MCP process failed: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != len(absentProbes)+1 {
		t.Fatalf("MCP response count %d, want %d", len(lines), len(absentProbes)+1)
	}
	for index, probe := range absentProbes {
		report := readProcessJSON(t, lines[index+1])["result"].(map[string]any)["structuredContent"].(map[string]any)
		if finding := absentFinding(t, report, probe.name); !sameJSONValue(finding, cliFindings[probe.name]) {
			t.Fatalf("%s CLI/MCP drift:\nCLI=%v\nMCP=%v", probe.name, cliFindings[probe.name], finding)
		}
	}

	// LSP publishes exactly the projection embedded in the shared report.
	lspInput.Write(voidLSPFrame(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "shutdown"}))
	lspInput.Write(voidLSPFrame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"}))
	stdout, stderr, code := runTestCLIInput(t, binary, lspInput.String(), "lsp")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("LSP process failed: code=%d stderr=%q", code, stderr)
	}
	published := map[string]any{}
	for _, message := range readVoidLSPFrames(t, stdout) {
		if message["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		params := message["params"].(map[string]any)
		for _, raw := range params["diagnostics"].([]any) {
			if diagnostic := raw.(map[string]any); diagnostic["code"] == "EF003" {
				published[params["uri"].(string)] = diagnostic
			}
		}
	}
	for _, probe := range absentProbes {
		if got := published["file:///absent/"+probe.name+".ef"]; !sameJSONValue(got, cliFindings[probe.name]["lsp"]) {
			t.Fatalf("%s CLI/LSP drift:\nCLI=%v\nLSP=%v", probe.name, cliFindings[probe.name]["lsp"], got)
		}
	}

	// Text output gives the help on its own line.
	stdout, _, _ = runTestCLI(t, binary, "diagnostics", filepath.Join(root, "p01-null.ef"))
	if !strings.Contains(string(stdout), "error EF003: Effra has no `null` value\n  help: absence is explicit") {
		t.Fatalf("text diagnostics omitted help: %s", stdout)
	}

	// Every help names checked source.
	for name, source := range absentReplacements {
		file := filepath.Join(root, "replacement-"+name+".ef")
		if err := os.WriteFile(file, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		stdout, _, code := runTestCLI(t, binary, "check", file)
		if report := readProcessJSON(t, stdout); code != 0 || report["checked"] != true {
			t.Fatalf("replacement %s does not check: %v", name, report["diagnostics"])
		}
	}
	for _, probe := range absentProbes {
		if _, ok := absentReplacements[probe.replacement]; !ok {
			t.Fatalf("%s names no replacement %q", probe.name, probe.replacement)
		}
	}
}
