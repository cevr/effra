package lintpacks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/lintpacks"
	"effra.local/prototype/internal/lsp"
	"effra.local/prototype/internal/mcp"
	"effra.local/prototype/lint"
)

// surfaceReports is one source analysed by every surface under one
// session: the CLI's lint result and diagnostic report (the same session
// and merge the ef command uses), MCP project.lint and project.diagnostics,
// and the LSP effra/lintStatus notification of a client that opted in.
type surfaceReports struct {
	lint      compiler.LintResult
	report    compiler.DiagnosticReport
	mcpLint   compiler.LintResult
	mcpReport compiler.DiagnosticReport
	lsp       []compiler.DiagnosticSuppression
	published []compiler.LSPDiagnostic
}

func analyseOnEverySurface(t *testing.T, session *lintpacks.Session, target, text string) surfaceReports {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "main.ef")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	uri, _ := compiler.FileURI(path)
	snapshot := compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: text}
	result := compiler.CompileAt(text, target, root)
	var out surfaceReports
	out.lint = result.LintWith(false, session.Run(context.Background(), result, snapshot))
	out.report = result.DiagnosticReportWith(snapshot, false, session.Run(context.Background(), result, snapshot))

	messages := []string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"` + mcp.ProtocolVersion + `","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"project.lint","arguments":{"file":"main.ef","target":"` + target + `"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.diagnostics","arguments":{"file":"main.ef","target":"` + target + `"}}}`,
	}
	var output bytes.Buffer
	if err := mcp.Serve(root, session, strings.NewReader(strings.Join(messages, "\n")+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var reply struct {
			ID     int `json:"id"`
			Result struct {
				IsError           bool            `json:"isError"`
				StructuredContent json.RawMessage `json:"structuredContent"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &reply); err != nil || reply.Result.IsError {
			t.Fatalf("MCP reply %s", line)
		}
		switch reply.ID {
		case 1:
			var content struct {
				Lint compiler.LintResult `json:"lint"`
			}
			json.Unmarshal(reply.Result.StructuredContent, &content)
			out.mcpLint = content.Lint
		case 2:
			json.Unmarshal(reply.Result.StructuredContent, &out.mcpReport)
		}
	}

	var in, lspOut bytes.Buffer
	for _, message := range []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"capabilities": map[string]any{"experimental": map[string]any{"effraLintStatus": true}}}},
		map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "effra", "version": 1, "text": text}}},
	} {
		body, _ := json.Marshal(message)
		fmt.Fprintf(&in, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	lsp.Serve(target, session, &in, &lspOut)
	statuses := 0
	for _, body := range frames(t, &lspOut) {
		var message struct {
			Method string `json:"method"`
			Params struct {
				URI          string                           `json:"uri"`
				Version      int                              `json:"version"`
				Revision     string                           `json:"revision"`
				Suppressions []compiler.DiagnosticSuppression `json:"suppressions"`
				Diagnostics  []compiler.LSPDiagnostic         `json:"diagnostics"`
			} `json:"params"`
		}
		json.Unmarshal(body, &message)
		switch message.Method {
		case "textDocument/publishDiagnostics":
			out.published = message.Params.Diagnostics
		case "effra/lintStatus":
			statuses++
			if message.Params.URI != uri || message.Params.Version != 1 || message.Params.Revision != result.Revision {
				t.Fatalf("lint status identity: %s", body)
			}
			out.lsp = message.Params.Suppressions
		}
	}
	if statuses != 1 {
		t.Fatalf("%d effra/lintStatus notifications", statuses)
	}
	return out
}

// Every suppression state and every not-evaluated reason, through the
// public CLI, MCP and LSP surfaces of one shared session: each reports the
// same statuses with the same directive spans and UTF-16 ranges. Only an
// unused suppression is an error; a suppression never removes a compiler
// diagnostic or a lint-runner error.
func TestSuppressionStatesAgreeAcrossSurfaces(t *testing.T) {
	dir := packs(t)
	serve := filepath.Join(dir, "fixture-serve", "manifest.json")
	// A rule requiring lexical binding facts, which a source past the
	// lexical fact budget cannot supply. The runner skips it before
	// starting the pack.
	var bindings lint.Manifest
	data, _ := os.ReadFile(serve)
	json.Unmarshal(data, &bindings)
	bindings.Rules[0].Requires = append(bindings.Rules[0].Requires, lint.FamilyBindings)
	bindingsManifest := filepath.Join(dir, "fixture-serve", "bindings.json")
	data, _ = json.Marshal(bindings)
	if err := os.WriteFile(bindingsManifest, data, 0o644); err != nil {
		t.Fatal(err)
	}

	const directive = "// effra-lint-disable-next-line fixture/rename-main -- the entry point keeps its name\n"
	const main = "effect fn main() -> void { void }\n"
	// 50,000 statements exhaust the 100,000 lexical syntax facts while
	// staying inside the LSP's 256 KiB document limit.
	exhausted := directive + "effect fn main() -> void {\n" + strings.Repeat("void\n", 50000) + "}\n"
	cases := []struct {
		name, target, text string
		session            *lintpacks.Session
		status, reason     string
		packFinding        bool
	}{
		{"applied", "go", directive + main, load(t, lintpacks.Selection{Manifests: []string{serve}}), compiler.SuppressionApplied, "", false},
		{"unused", "go", directive + "effect fn helper() -> void { void }\n" + main, load(t, lintpacks.Selection{Manifests: []string{serve}}), compiler.SuppressionUnused, "", true},
		{"unchecked source", "go", directive + "effect fn main() -> void { run Console.log(\"x\") }\n", load(t, lintpacks.Selection{Manifests: []string{serve}}), compiler.SuppressionNotEvaluated, compiler.NotEvaluatedUncheckedSource, false},
		{"pack not selected", "go", directive + main, nil, compiler.SuppressionNotEvaluated, compiler.NotEvaluatedPackNotSelected, false},
		{"rule off", "go", directive + main, load(t, lintpacks.Selection{Config: writeConfig(t, `{"version":1,"packs":[{"manifest":`+strconv.Quote(serve)+`}],"rules":{"fixture/rename-main":"off"}}`)}), compiler.SuppressionNotEvaluated, compiler.NotEvaluatedRuleOff, false},
		{"pack failed", "go", directive + main, load(t, lintpacks.Selection{Manifests: []string{filepath.Join(dir, "fixture-crash", "manifest.json")}}), compiler.SuppressionNotEvaluated, compiler.NotEvaluatedPackFailed, false},
		{"facts unavailable", "go", exhausted, load(t, lintpacks.Selection{Manifests: []string{bindingsManifest}}), compiler.SuppressionNotEvaluated, compiler.NotEvaluatedFactsUnavailable, false},
		{"target unsupported", "js", directive + main, load(t, lintpacks.Selection{Manifests: []string{filepath.Join(dir, "fixture-go", "manifest.json")}}), compiler.SuppressionNotEvaluated, compiler.NotEvaluatedTargetUnsupported, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := analyseOnEverySurface(t, c.session, c.target, c.text)
			want := compiler.SuppressionStatus{Rule: "fixture/rename-main", Status: c.status, Reason: c.reason, Span: compiler.Span{Offset: 0, Length: len(directive) - 1, Line: 1, Column: 1}, Line: 2}
			if !reflect.DeepEqual(got.lint.Suppressions, []compiler.SuppressionStatus{want}) {
				t.Fatalf("CLI lint suppressions %+v, want %+v", got.lint.Suppressions, want)
			}
			wantRange := &compiler.DiagnosticRange{End: compiler.DiagnosticPosition{Character: len(directive) - 1}}
			if len(got.report.Suppressions) != 1 || got.report.Suppressions[0].SuppressionStatus != want || !reflect.DeepEqual(got.report.Suppressions[0].Range, wantRange) {
				t.Fatalf("CLI report suppressions %+v", got.report.Suppressions)
			}
			if !reflect.DeepEqual(got.mcpLint.Suppressions, got.lint.Suppressions) || !reflect.DeepEqual(got.mcpReport.Suppressions, got.report.Suppressions) || !reflect.DeepEqual(got.lsp, got.report.Suppressions) {
				t.Fatalf("surfaces disagree:\nMCP lint %+v\nMCP report %+v\nLSP %+v\nwant %+v", got.mcpLint.Suppressions, got.mcpReport.Suppressions, got.lsp, got.report.Suppressions)
			}
			// Only unused is an EFL004 error; the finding it failed to cover
			// remains. An applied suppression removes the pack finding.
			unused, packFinding := false, false
			for _, finding := range got.report.Diagnostics {
				unused = unused || finding.Code == "EFL004"
				packFinding = packFinding || finding.Code == "fixture/rename-main"
			}
			if unused != (c.status == compiler.SuppressionUnused) || packFinding != c.packFinding {
				t.Fatalf("diagnostics: %+v", got.report.Diagnostics)
			}
			if got.published == nil || len(got.published) != len(got.report.Diagnostics) {
				t.Fatalf("LSP published %+v for %+v", got.published, got.report.Diagnostics)
			}
		})
	}
}

// The status notification is opt-in: a client that does not declare the
// experimental capability receives publishDiagnostics alone.
func TestLintStatusNotificationIsOptIn(t *testing.T) {
	_, snapshot := compile(t)
	var in, out bytes.Buffer
	for _, message := range []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"capabilities": map[string]any{"experimental": map[string]any{"effraLintStatus": false}}}},
		map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": snapshot.URI, "languageId": "effra", "version": 1, "text": snapshot.Text}}},
	} {
		body, _ := json.Marshal(message)
		fmt.Fprintf(&in, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	lsp.Serve("go", nil, &in, &out)
	var methods []string
	for _, body := range frames(t, &out) {
		var message struct {
			Method string `json:"method"`
		}
		json.Unmarshal(body, &message)
		if message.Method != "" {
			methods = append(methods, message.Method)
		}
	}
	if !reflect.DeepEqual(methods, []string{"textDocument/publishDiagnostics"}) {
		t.Fatalf("notifications %v", methods)
	}
}

// A receipt runs the production pack path and records each phase; with
// the pack's rule off, no process starts.
func TestReceiptRecordsPackPhases(t *testing.T) {
	dir := packs(t)
	serve := filepath.Join(dir, "fixture-serve", "manifest.json")
	path, _ := filepath.Abs("../../examples/lintpack/testdata/provider_boundary.ef")
	receipt, err := load(t, lintpacks.Selection{Manifests: []string{serve}}).Receipt(context.Background(), path, "go", 2)
	if err != nil || len(receipt.Runs) != 2 {
		t.Fatalf("%v %+v", err, receipt)
	}
	for _, run := range receipt.Runs {
		pack := run.PackRuns[0]
		if !run.Checked || !pack.Started || pack.FirstByteNanos <= 0 || pack.ExitNanos <= 0 || pack.ResponseBytes == 0 || pack.Rules[0].Status != lint.StatusCompleted {
			t.Fatalf("enabled run %+v", run)
		}
	}
	off := load(t, lintpacks.Selection{Config: writeConfig(t, `{"version":1,"packs":[{"manifest":`+strconv.Quote(serve)+`}],"rules":{"fixture/rename-main":"off"}}`)})
	receipt, err = off.Receipt(context.Background(), path, "go", 1)
	if err != nil || receipt.Runs[0].PackRuns[0].Started || receipt.Runs[0].PackRuns[0].SpawnNanos != 0 {
		t.Fatalf("disabled: %v %+v", err, receipt.Runs)
	}
	if _, err := off.Receipt(context.Background(), path, "go", 0); err == nil {
		t.Fatal("zero runs accepted")
	}
}
