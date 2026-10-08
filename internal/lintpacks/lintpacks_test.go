package lintpacks_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"effra.local/prototype/examples/lintpack"
	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/lintpacks"
	"effra.local/prototype/internal/lsp"
	"effra.local/prototype/internal/mcp"
	"effra.local/prototype/internal/producer"
	"effra.local/prototype/lint"
)

var built struct {
	once sync.Once
	dir  string
	err  error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if built.dir != "" {
		os.RemoveAll(built.dir)
	}
	os.Exit(code)
}

// packs builds the example policy pack and the runner fixture pack once and
// writes their manifests: policy/manifest.json, and fixture-MODE/manifest.json
// for each fixture behaviour the tests select.
func packs(t *testing.T) string {
	t.Helper()
	built.once.Do(func() {
		built.dir, built.err = os.MkdirTemp("", "effra-lintpacks-")
		if built.err != nil {
			return
		}
		for name, pkg := range map[string]string{"policy-lint": "../../examples/lintpack/cmd/policy-lint", "fixturepack": "../../lint/testdata/fixturepack"} {
			if runtime.GOOS == "windows" {
				name += ".exe" // the manifests' extensionless paths resolve to it
			}
			if output, err := exec.Command("go", "build", "-o", filepath.Join(built.dir, "bin", name), pkg).CombinedOutput(); err != nil {
				built.err = errors.New(string(output))
				return
			}
		}
		policy, err := lintpack.Pack.Manifest(lint.Executable{Path: "../bin/policy-lint"})
		if err != nil {
			built.err = err
			return
		}
		manifests := map[string]lint.Manifest{"policy": policy}
		for _, mode := range []string{"serve", "crash", "misplaced", "env"} {
			manifests["fixture-"+mode] = lint.Manifest{
				ManifestVersion: lint.ManifestVersion, Namespace: "fixture", Version: "1.0.0",
				FactSchema: lint.ManifestSchema{Name: lint.FactSchemaName, Versions: []int{lint.FactSchemaVersion}},
				Executable: lint.Executable{Path: "../bin/fixturepack", Args: []string{mode}},
				Rules: []lint.ManifestRule{{
					Name: "rename-main", Version: "1", Description: "fixture rule", DefaultSeverity: lint.SeverityWarning,
					Requires: []lint.Family{lint.FamilyCallables},
					Options:  []lint.OptionSpec{{Name: "to", Type: lint.OptionString, Default: json.RawMessage(`"entry"`)}},
				}},
			}
		}
		// A serving fixture whose rule supports only the go target.
		goOnly := manifests["fixture-serve"]
		goOnly.Rules = []lint.ManifestRule{goOnly.Rules[0]}
		goOnly.Rules[0].Targets = []string{"go"}
		manifests["fixture-go"] = goOnly
		for name, manifest := range manifests {
			data, _ := json.Marshal(manifest)
			if built.err = os.MkdirAll(filepath.Join(built.dir, name), 0o755); built.err != nil {
				return
			}
			if built.err = os.WriteFile(filepath.Join(built.dir, name, "manifest.json"), data, 0o644); built.err != nil {
				return
			}
		}
	})
	if built.err != nil {
		t.Fatal(built.err)
	}
	return built.dir
}

func writeConfig(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lint.json")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func load(t *testing.T, selection lintpacks.Selection) *lintpacks.Session {
	t.Helper()
	session, err := lintpacks.Load(selection)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func compile(t *testing.T) (*compiler.Result, compiler.SourceSnapshot) {
	t.Helper()
	path, err := filepath.Abs("../../examples/lintpack/testdata/provider_boundary.ef")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	uri, _ := compiler.FileURI(path)
	result := compiler.CompileAt(string(source), "go", filepath.Dir(path))
	if !result.Checked {
		t.Fatal(result.Diagnostics)
	}
	return result, compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: string(source)}
}

const policyRules = `"rules":{"policy/forbidden-failure":"off","policy/provider-boundary":{"options":{"provider":"LiveMail","allow":["main"]}}}`

// Pack findings join built-in advice under the pack's rule identity, with
// related locations; built-in rules follow their configured severity.
func TestSessionMergesPackFindingsWithBuiltinAdvice(t *testing.T) {
	dir := packs(t)
	config := writeConfig(t, `{"version":1,"packs":[{"manifest":"`+filepath.Join(dir, "policy", "manifest.json")+`"}],`+policyRules+`}`)
	session := load(t, lintpacks.Selection{Config: config})
	result, snapshot := compile(t)
	merged := result.LintWith(false, session.Run(context.Background(), result, snapshot))
	if !merged.Complete || merged.LintPassed || merged.Errors != 2 || len(merged.Packs) != 1 {
		t.Fatalf("%+v", merged)
	}
	status := merged.Packs[0]
	if status.Pack != "policy" || status.Failure != nil || status.Analysis.Execution == nil || !status.Analysis.Execution.Complete || status.Identity != status.Analysis.Packs[0][len("policy@"):] {
		t.Fatalf("%+v", status)
	}
	var layered compiler.LintDiagnostic
	for _, diagnostic := range merged.LintDiagnostics {
		if diagnostic.Rule != "policy/provider-boundary" || diagnostic.Code != diagnostic.Rule || diagnostic.Severity != "error" {
			t.Fatalf("pack finding identity: %+v", diagnostic)
		}
		if strings.HasPrefix(diagnostic.Message, "layered") {
			layered = diagnostic
		}
	}
	if len(layered.Related) != 1 || layered.Related[0].Span.Line != 18 {
		t.Fatalf("related location: %+v", layered)
	}

	report := result.DiagnosticReportWith(snapshot, false, session.Run(context.Background(), result, snapshot))
	if !report.LintComplete || report.PolicyPassed || report.TotalCounts.Errors != 2 {
		t.Fatalf("%+v", report)
	}
	for _, finding := range report.Diagnostics {
		if finding.LSP == nil || !finding.LocationAvailable {
			t.Fatalf("pack finding without a location: %+v", finding)
		}
		if strings.HasPrefix(finding.Message, "layered") && (len(finding.LSP.RelatedInformation) != 1 || finding.LSP.RelatedInformation[0].Location.Range.Start.Line != 17) {
			t.Fatalf("related information: %+v", finding.LSP)
		}
	}

	// The same source under a configuration that turns the pack rule off
	// and makes built-in advice an error.
	quiet := writeConfig(t, `{"version":1,"packs":[{"manifest":"`+filepath.Join(dir, "policy", "manifest.json")+`"}],"rules":{"policy/provider-boundary":"off","policy/forbidden-failure":"off"}}`)
	off := result.LintWith(false, load(t, lintpacks.Selection{Config: quiet}).Run(context.Background(), result, snapshot))
	if !off.LintPassed || !off.Complete || len(off.LintDiagnostics) != 0 || off.Packs[0].Analysis.Execution != nil {
		t.Fatalf("an all-off pack: %+v", off)
	}
}

// A pack that fails becomes one lint-runner error naming the pack, so
// incomplete analysis fails policy; other packs' findings remain, and the
// order of packs is their namespace order, not selection or completion.
func TestSessionReportsAPackFailureAsALintRunnerError(t *testing.T) {
	dir := packs(t)
	config := writeConfig(t, `{"version":1,`+policyRules+`}`)
	session := load(t, lintpacks.Selection{Config: config, Manifests: []string{filepath.Join(dir, "policy", "manifest.json"), filepath.Join(dir, "fixture-crash", "manifest.json")}})
	result, snapshot := compile(t)
	merged := result.LintWith(false, session.Run(context.Background(), result, snapshot))
	if merged.Complete || merged.LintPassed || len(merged.Packs) != 2 || merged.Packs[0].Pack != "fixture" || merged.Packs[1].Pack != "policy" {
		t.Fatalf("%+v", merged)
	}
	runner := merged.LintDiagnostics[0]
	if runner.Code != "EFL000" || runner.Rule != "lint-runner" || runner.Severity != "error" || runner.Span != (compiler.Span{}) || !strings.Contains(runner.Message, "rule pack fixture ("+merged.Packs[0].Identity+") failed: crashed: pack exited unsuccessfully") {
		t.Fatalf("runner error: %+v", runner)
	}
	if merged.Errors != 3 || merged.LintDiagnostics[1].Rule != "policy/provider-boundary" {
		t.Fatalf("policy findings lost beside the failure: %+v", merged.LintDiagnostics)
	}
	report := result.DiagnosticReportWith(snapshot, false, session.Run(context.Background(), result, snapshot))
	var found bool
	for _, finding := range report.Diagnostics {
		if finding.Code == "EFL000" {
			found = true
			if finding.LocationAvailable || finding.LSP == nil || finding.LSP.Range != (compiler.DiagnosticRange{}) || finding.LSP.Severity != 1 {
				t.Fatalf("runner error projection: %+v", finding)
			}
		}
	}
	if !found || report.LintComplete || report.PolicyPassed {
		t.Fatalf("%+v", report)
	}
}

// A rule that supports only go, under go and js analyses on one MCP
// server, and in the LSP under js. Enabled by its pack's default, it does
// not apply to js: the js analysis is complete and passes, with the skip
// reported. Enabled by the project's configuration, it is a guarantee js
// cannot give: js lint fails with a lint-runner error. Either way the go
// analysis runs it.
func TestSessionAppliesATargetRestrictedRuleByWhoEnabledIt(t *testing.T) {
	dir := packs(t)
	manifest := filepath.Join(dir, "fixture-go", "manifest.json")
	root, _ := filepath.Abs("../../examples/lintpack/testdata")
	_, snapshot := compile(t)
	lintTargets := func(session *lintpacks.Session) map[string]compiler.LintResult {
		t.Helper()
		messages := []string{
			`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"` + mcp.ProtocolVersion + `","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"project.lint","arguments":{"file":"provider_boundary.ef","target":"go"}}}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.lint","arguments":{"file":"provider_boundary.ef","target":"js"}}}`,
		}
		var output bytes.Buffer
		if err := mcp.Serve(root, session, strings.NewReader(strings.Join(messages, "\n")+"\n"), &output); err != nil {
			t.Fatal(err)
		}
		results := map[string]compiler.LintResult{}
		for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
			var reply struct {
				ID     int `json:"id"`
				Result struct {
					StructuredContent struct {
						Lint compiler.LintResult `json:"lint"`
					} `json:"structuredContent"`
				} `json:"result"`
			}
			if err := json.Unmarshal([]byte(line), &reply); err != nil {
				t.Fatal(output.String())
			}
			if reply.ID > 0 {
				results[reply.Result.StructuredContent.Lint.Target] = reply.Result.StructuredContent.Lint
			}
		}
		return results
	}
	runnerError := func(diagnostics []compiler.LintDiagnostic) bool {
		for _, diagnostic := range diagnostics {
			if diagnostic.Code == "EFL000" && strings.HasSuffix(diagnostic.Message, "was skipped: target-unsupported: js") {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		name     string
		session  *lintpacks.Session
		jsPasses bool
	}{
		{"pack default", load(t, lintpacks.Selection{Manifests: []string{manifest}}), true},
		{"project configuration", load(t, lintpacks.Selection{Config: writeConfig(t, `{"version":1,"packs":[{"manifest":`+strconv.Quote(manifest)+`}],"rules":{"fixture/rename-main":"warning"}}`)}), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			results := lintTargets(c.session)
			goLint, jsLint := results["go"], results["js"]
			if !goLint.Complete || !goLint.LintPassed || goLint.Packs[0].Rules[0].Status != lint.StatusCompleted || goLint.Packs[0].Rules[0].Findings != 1 {
				t.Fatalf("go: %+v", goLint)
			}
			status := jsLint.Packs[0].Rules[0]
			if status.Status != lint.StatusSkipped || status.Reason != "target-unsupported: js" || jsLint.Packs[0].Analysis.Execution != nil {
				t.Fatalf("js status: %+v", jsLint.Packs[0])
			}
			if jsLint.Complete != c.jsPasses || jsLint.LintPassed != c.jsPasses || runnerError(jsLint.LintDiagnostics) == c.jsPasses {
				t.Fatalf("js: %+v", jsLint)
			}
			if status.Inapplicable != c.jsPasses {
				t.Fatalf("js inapplicable: %+v", status)
			}
			published := publishFor(t, "js", c.session, snapshot)
			if published := slices.ContainsFunc(published, func(d compiler.LSPDiagnostic) bool {
				return d.Code == "EFL000" && d.Range == (compiler.DiagnosticRange{})
			}); published == c.jsPasses {
				t.Fatalf("LSP published a lint-runner error: %v", published)
			}
		})
	}
}

// Loading validates the whole selection and inspects rules from manifests
// alone: a pack whose program does not exist loads and lists its rules.
func TestLoadValidatesWithoutStartingPacks(t *testing.T) {
	dir := t.TempDir()
	manifest, err := lint.LoadManifest(filepath.Join(packs(t), "fixture-serve", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Manifest.Executable.Path = "missing-program"
	data, _ := json.Marshal(manifest.Manifest)
	path := filepath.Join(dir, "manifest.json")
	os.WriteFile(path, data, 0o644)
	session := load(t, lintpacks.Selection{Manifests: []string{path}})
	var names []string
	for _, rule := range session.Rules() {
		names = append(names, rule.Rule)
	}
	if want := []string{"fixture/rename-main", "invalid-suppression", "redundant-provision", "unused-go-import", "unused-recipe"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("rules %v", names)
	}
	refusals := []struct {
		selection lintpacks.Selection
		want      string
	}{
		{lintpacks.Selection{Config: writeConfig(t, `{"version":1,"rules":{"fixture/nope":"error"}}`), Manifests: []string{path}}, "unknown-rule"},
		{lintpacks.Selection{Config: writeConfig(t, `{"version":1,"rules":{"unused-recipe":"loud"}}`)}, "invalid lint configuration"},
		{lintpacks.Selection{Manifests: []string{path, path}}, "fixture"},
		{lintpacks.Selection{Config: writeConfig(t, `{"version":1,"extends":["fixture/strict"]}`), Manifests: []string{path}}, "unknown-preset"},
		{lintpacks.Selection{Manifests: []string{filepath.Join(dir, "absent.json")}}, "absent.json"},
	}
	for _, refusal := range refusals {
		if session, err := lintpacks.Load(refusal.selection); err == nil || session != nil || !strings.Contains(err.Error(), refusal.want) {
			t.Fatalf("got %v, want %q", err, refusal.want)
		}
	}
	if session, err := lintpacks.Load(lintpacks.Selection{}); session != nil || err != nil {
		t.Fatal("an empty selection is not the default session")
	}
	var none *lintpacks.Session
	if len(none.Rules()) != 4 {
		t.Fatal("the default session lists more than the built-in rules")
	}
}

// MCP and LSP run the session they were started with: MCP lint equals the
// direct merge, and the LSP publishes a pack failure at the document start.
func TestSurfacesRunTheirStartupSession(t *testing.T) {
	dir := packs(t)
	config := writeConfig(t, `{"version":1,"packs":[{"manifest":"`+filepath.Join(dir, "policy", "manifest.json")+`"}],`+policyRules+`}`)
	session := load(t, lintpacks.Selection{Config: config})
	result, snapshot := compile(t)
	root, _ := filepath.Abs("../../examples/lintpack/testdata")
	if err := result.Qualify(producer.Current()); err != nil {
		t.Fatal(err)
	}
	want := result.LintWith(true, session.Run(context.Background(), result, snapshot))

	messages := []string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"` + mcp.ProtocolVersion + `","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"project.lint","arguments":{"file":"provider_boundary.ef","strict":true}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"lint.rules","arguments":{}}}`,
	}
	var output bytes.Buffer
	if err := mcp.Serve(root, session, strings.NewReader(strings.Join(messages, "\n")+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	type reply struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	var replies []reply
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var r reply
		json.Unmarshal([]byte(line), &r)
		replies = append(replies, r)
	}
	var lintReply struct {
		Lint compiler.LintResult `json:"lint"`
	}
	if err := json.Unmarshal(replies[1].Result.StructuredContent, &lintReply); err != nil {
		t.Fatal(output.String())
	}
	if !reflect.DeepEqual(lintReply.Lint.LintDiagnostics, want.LintDiagnostics) || !reflect.DeepEqual(lintReply.Lint.Packs, want.Packs) || lintReply.Lint.LintPassed {
		t.Fatalf("MCP lint:\n%+v\nwant\n%+v", lintReply.Lint, want)
	}
	var rules []lint.RuleInfo
	json.Unmarshal(replies[2].Result.StructuredContent, &rules)
	if !reflect.DeepEqual(rules, session.Rules()) {
		t.Fatalf("MCP lint.rules %+v", rules)
	}

	crash := load(t, lintpacks.Selection{Manifests: []string{filepath.Join(dir, "fixture-crash", "manifest.json")}})
	published := publish(t, crash, snapshot)
	if len(published) != 1 || published[0].Code != "EFL000" || published[0].Range != (compiler.DiagnosticRange{}) || !strings.Contains(published[0].Message, "rule pack fixture") {
		t.Fatalf("published %+v", published)
	}
}

func frames(t *testing.T, out *bytes.Buffer) [][]byte {
	t.Helper()
	var bodies [][]byte
	reader := bufio.NewReader(out)
	for {
		header, err := reader.ReadString('\n')
		if err == io.EOF {
			return bodies
		}
		if err != nil {
			t.Fatal(err)
		}
		length, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(header, "Content-Length:")))
		if err != nil {
			t.Fatalf("header %q", header)
		}
		reader.ReadString('\n')
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
	}
}

// A pack finding has the positions the compiler would give the same bytes,
// whatever characters precede it: the byte span and byte column the CLI
// prints, and the UTF-16 range the LSP publishes. A pack that misplaces a
// finding fails its rule, which is published as a lint-runner error rather
// than at a position that names another place.
func TestPackFindingPositionsAgreeAcrossSurfaces(t *testing.T) {
	dir := packs(t)
	// é is 2 bytes and 1 UTF-16 unit; 𝄞 is 4 bytes and 2 units. main is at
	// byte 23, byte column 24 and UTF-16 character 20 (zero-based).
	text := "/* é𝄞 */ effect fn main() -> void { void }"
	root := t.TempDir()
	path := filepath.Join(root, "unicode.ef")
	os.WriteFile(path, []byte(text), 0o644)
	uri, _ := compiler.FileURI(path)
	snapshot := compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: text}
	result := compiler.CompileAt(text, "go", root)
	if !result.Checked {
		t.Fatal(result.Diagnostics)
	}
	serve := load(t, lintpacks.Selection{Manifests: []string{filepath.Join(dir, "fixture-serve", "manifest.json")}})
	merged := result.LintWith(false, serve.Run(context.Background(), result, snapshot))
	if len(merged.LintDiagnostics) != 1 || merged.LintDiagnostics[0].Span != (compiler.Span{Offset: 23, Length: 4, Line: 1, Column: 24}) || len(merged.LintDiagnostics[0].Suggestions) != 1 {
		t.Fatalf("%+v", merged)
	}
	report := result.DiagnosticReportWith(snapshot, false, serve.Run(context.Background(), result, snapshot))
	want := compiler.DiagnosticRange{Start: compiler.DiagnosticPosition{Character: 20}, End: compiler.DiagnosticPosition{Character: 24}}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].LSP == nil || report.Diagnostics[0].LSP.Range != want {
		t.Fatalf("%+v", report.Diagnostics)
	}
	if published := publish(t, serve, snapshot); len(published) != 1 || published[0].Range != want || published[0].Code != "fixture/rename-main" {
		t.Fatalf("LSP published %+v", published)
	}

	misplaced := load(t, lintpacks.Selection{Manifests: []string{filepath.Join(dir, "fixture-misplaced", "manifest.json")}})
	merged = result.LintWith(false, misplaced.Run(context.Background(), result, snapshot))
	if merged.Complete || merged.LintPassed || len(merged.LintDiagnostics) != 1 || merged.LintDiagnostics[0].Code != "EFL000" || !strings.Contains(merged.LintDiagnostics[0].Message, "is at line 1, column 25") {
		t.Fatalf("misplaced: %+v", merged.LintDiagnostics)
	}
	if published := publish(t, misplaced, snapshot); len(published) != 1 || published[0].Code != "EFL000" || published[0].Range != (compiler.DiagnosticRange{}) {
		t.Fatalf("LSP published %+v", published)
	}
}

// publish opens one document in an LSP session and returns the last
// diagnostics it published.
func publish(t *testing.T, session *lintpacks.Session, snapshot compiler.SourceSnapshot) []compiler.LSPDiagnostic {
	t.Helper()
	return publishFor(t, "go", session, snapshot)
}

func publishFor(t *testing.T, target string, session *lintpacks.Session, snapshot compiler.SourceSnapshot) []compiler.LSPDiagnostic {
	t.Helper()
	var in, out bytes.Buffer
	for _, message := range []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": snapshot.URI, "languageId": "effra", "version": 1, "text": snapshot.Text}}},
	} {
		body, _ := json.Marshal(message)
		fmt.Fprintf(&in, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	lsp.Serve(target, session, &in, &out)
	var published []compiler.LSPDiagnostic
	for _, body := range frames(t, &out) {
		var message struct {
			Method string `json:"method"`
			Params struct {
				Diagnostics []compiler.LSPDiagnostic `json:"diagnostics"`
			} `json:"params"`
		}
		json.Unmarshal(body, &message)
		if message.Method == "textDocument/publishDiagnostics" {
			published = message.Params.Diagnostics
		}
	}
	return published
}

// Every surface resolves a pack's selected variables from one capture of
// the host environment per analysis: the pack sees the selected value and
// nothing else, an unselected host change leaves the analysis identity
// alone, and a selected change is a different analysis.
func TestSessionPassesSelectedHostVariables(t *testing.T) {
	dir := packs(t)
	config := writeConfig(t, `{"version":1,"packs":[{"manifest":"`+filepath.Join(dir, "fixture-env", "manifest.json")+`","env":["EF_LINT_PROBE"]}]}`)
	session := load(t, lintpacks.Selection{Config: config})
	if rules := session.Rules(); rules[len(rules)-1].Rule != "unused-recipe" || !reflect.DeepEqual(rules[0].Environment, []string{"EF_LINT_PROBE"}) {
		t.Fatalf("inspection %+v", rules[0])
	}
	result, snapshot := compile(t)
	run := func() compiler.LintPackReport {
		t.Helper()
		packs := session.Run(context.Background(), result, snapshot)
		if len(packs.Reports) != 1 || packs.Reports[0].Report.Failure != nil || len(packs.Reports[0].Report.Findings) != 1 {
			t.Fatalf("%+v", packs.Reports)
		}
		return packs.Reports[0]
	}
	extra := len(lint.RequiredVariables(runtime.GOOS))
	t.Setenv("EF_LINT_PROBE", "alpha")
	t.Setenv("EF_LINT_UNSELECTED", "secret")
	alpha := run()
	if message := alpha.Report.Findings[0].Message; message != fmt.Sprintf("env %d present:alpha", 1+extra) {
		t.Fatalf("pack saw %q", message)
	}
	t.Setenv("EF_LINT_UNSELECTED", "changed")
	if again := run(); again.Report.Analysis.Digest != alpha.Report.Analysis.Digest {
		t.Fatal("an unselected host variable changed the analysis identity")
	}
	t.Setenv("EF_LINT_PROBE", "beta")
	if beta := run(); beta.Report.Analysis.Digest == alpha.Report.Analysis.Digest || beta.Report.Findings[0].Message != fmt.Sprintf("env %d present:beta", 1+extra) {
		t.Fatalf("a selected host change: %+v", beta.Report)
	}
	os.Unsetenv("EF_LINT_PROBE")
	if absent := run(); absent.Report.Findings[0].Message != fmt.Sprintf("env %d absent", extra) {
		t.Fatalf("an absent selected variable: %+v", absent.Report)
	}
	// Without a selection the pack receives nothing.
	bare := load(t, lintpacks.Selection{Manifests: []string{filepath.Join(dir, "fixture-env", "manifest.json")}})
	t.Setenv("EF_LINT_PROBE", "alpha")
	packs := bare.Run(context.Background(), result, snapshot)
	if message := packs.Reports[0].Report.Findings[0].Message; message != fmt.Sprintf("env %d absent", extra) {
		t.Fatalf("an unselecting pack saw %q", message)
	}
}

// An expectation is exactly one JSON object: trailing whitespace is
// formatting, while any data after the object, a stray closing delimiter
// included, makes it invalid rather than equal to its first value.
func TestParseExpectationRefusesTrailingData(t *testing.T) {
	valid := lintpacks.Expectation{Complete: true}.Encode()
	for _, data := range []string{string(valid), string(valid) + " \n\t\n"} {
		if expectation, err := lintpacks.ParseExpectation([]byte(data)); err != nil || !expectation.Complete {
			t.Fatalf("%q: %+v %v", data, expectation, err)
		}
	}
	for _, trailing := range []string{"}", "]", "{}", "x", `{"complete":false}`} {
		if _, err := lintpacks.ParseExpectation(append(valid, trailing...)); err == nil {
			t.Fatalf("trailing %q was accepted", trailing)
		}
	}
}
