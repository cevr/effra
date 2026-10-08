package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"effra.local/prototype/examples/lintpack"
	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/lint"
)

// The CLI selects rule packs with --lint-config and --rules for lint,
// diagnostics, lint rules, mcp and lsp. A configuration that does not load
// is an invalid invocation (exit 2) and starts no pack.
func TestLintPackSelectionCLIProcess(t *testing.T) {
	binary := buildTestCLI(t)
	dir := t.TempDir()
	if output, err := exec.Command("go", "build", "-o", filepath.Join(dir, "packs", executableName("policy-lint")), "../../examples/lintpack/cmd/policy-lint").CombinedOutput(); err != nil {
		t.Fatal(string(output))
	}
	manifest, err := lintpack.Pack.Manifest(lint.Executable{Path: "policy-lint"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "packs", "policy.json"), data, 0o644)
	// The configuration lives in its own directory and names the manifest
	// relative to it; the CLI runs from elsewhere.
	os.MkdirAll(filepath.Join(dir, "config"), 0o755)
	config := filepath.Join(dir, "config", "lint.json")
	os.WriteFile(config, []byte(`{"version":1,"packs":[{"manifest":"../packs/policy.json"}],"rules":{"policy/forbidden-failure":"off","policy/provider-boundary":{"options":{"provider":"LiveMail","allow":["main"]}}}}`), 0o644)
	source, _ := filepath.Abs("../../examples/lintpack/testdata/provider_boundary.ef")

	stdout, stderr, code := runTestCLI(t, binary, "lint", source, "--lint-config", config)
	var result compiler.LintResult
	if err := json.Unmarshal(stdout, &result); err != nil || code != 1 {
		t.Fatalf("lint: exit %d %s %s", code, stdout, stderr)
	}
	if result.LintPassed || !result.Complete || result.Errors != 2 || len(result.Packs) != 1 || result.LintDiagnostics[0].Rule != "policy/provider-boundary" {
		t.Fatalf("lint result %+v", result)
	}

	stdout, _, code = runTestCLI(t, binary, "diagnostics", source, "--json", "--lint-config", config)
	var report compiler.DiagnosticReport
	if err := json.Unmarshal(stdout, &report); err != nil || code != 1 || report.TotalCounts.Errors != 2 || !report.LintComplete {
		t.Fatalf("diagnostics: exit %d %s", code, stdout)
	}
	stdout, _, code = runTestCLI(t, binary, "diagnostics", source, "--lint-config", config)
	if code != 1 || !strings.Contains(string(stdout), "error policy/provider-boundary: aliased provides LiveMail") {
		t.Fatalf("diagnostics text: exit %d %s", code, stdout)
	}

	stdout, _, code = runTestCLI(t, binary, "lint", "rules", "--lint-config", config)
	var rules []lint.RuleInfo
	if err := json.Unmarshal(stdout, &rules); err != nil || code != 0 || len(rules) != 6 {
		t.Fatalf("lint rules: exit %d %s", code, stdout)
	}
	if rules[0].Rule != "invalid-suppression" || rules[1].Rule != "policy/forbidden-failure" || rules[1].Severity != lint.SeverityOff || rules[1].PackIdentity == "" {
		t.Fatalf("lint rules %+v", rules)
	}
	stdout, _, code = runTestCLI(t, binary, "lint", "rules")
	if json.Unmarshal(stdout, &rules); code != 0 || len(rules) != 4 || rules[0].Rule != "invalid-suppression" || !rules[0].Builtin {
		t.Fatalf("default lint rules: exit %d %s", code, stdout)
	}

	// The same manifest selected twice is refused before any pack starts:
	// the program is gone, so starting it could only fail differently.
	os.Remove(filepath.Join(dir, "packs", executableName("policy-lint")))
	invalid := []struct {
		args []string
		want string
	}{
		{[]string{"lint", source, "--lint-config", config, "--rules", filepath.Join(dir, "packs", "policy.json")}, "duplicate"},
		{[]string{"lint", source, "--lint-config", filepath.Join(dir, "absent.json")}, "absent.json"},
		{[]string{"diagnostics", source, "--rules"}, "--rules requires a value"},
		// Selected alone, the policy pack lacks its required options.
		{[]string{"lint", "rules", "--rules", filepath.Join(dir, "packs", "policy.json")}, `missing required option "provider"`},
		{[]string{"lint", source, "--lint-config", config, "--lint-config", config}, "once"},
		// An empty path is refused, never read as no selection, and it
		// counts as the one --lint-config.
		{[]string{"lint", source, "--lint-config", ""}, "not an empty value"},
		{[]string{"lint", source, "--lint-config", "", "--lint-config", config}, "not an empty value"},
		{[]string{"lint", source, "--lint-config", config, "--lint-config", ""}, "once"},
		{[]string{"diagnostics", source, "--rules", ""}, "not an empty value"},
		{[]string{"lint", filepath.Join(dir, "source.txt")}, ".ef extension"},
		{[]string{"lint", "rules", "--lint-config", filepath.Join(dir, "absent.json")}, "absent.json"},
		{[]string{"mcp", dir, "--lint-config", filepath.Join(dir, "absent.json")}, "absent.json"},
		{[]string{"lsp", "--lint-config", filepath.Join(dir, "absent.json")}, "absent.json"},
	}
	for _, c := range invalid {
		if _, stderr, code := runTestCLI(t, binary, c.args...); code != 2 || !strings.Contains(string(stderr), c.want) {
			t.Fatalf("%v: exit %d %s", c.args, code, stderr)
		}
	}
	if _, stderr, code := runTestCLI(t, binary, "check", source, "--lint-config", config); code == 0 || !strings.Contains(string(stderr), "only supported by lint and diagnostics") {
		t.Fatalf("check with a lint configuration: exit %d %s", code, stderr)
	}
	// The removed program is a pack failure at run time: lint reports a
	// lint-runner error and fails, with the built-in result intact.
	stdout, _, code = runTestCLI(t, binary, "lint", source, "--lint-config", config)
	if json.Unmarshal(stdout, &result); code != 1 || result.Complete || result.LintDiagnostics[0].Code != "EFL000" || !strings.Contains(result.LintDiagnostics[0].Message, "rule pack policy") {
		t.Fatalf("missing program: exit %d %s", code, stdout)
	}
}

// ef lint test runs real .ef fixtures through the production pack path and
// compares them with their expectations; --update writes expectations and
// never touches a fixture. Built-in advice is outside an expectation.
func TestLintFixtureCommandProcess(t *testing.T) {
	binary := buildTestCLI(t)
	dir := t.TempDir()
	if output, err := exec.Command("go", "build", "-o", filepath.Join(dir, executableName("policy-lint")), "../../examples/lintpack/cmd/policy-lint").CombinedOutput(); err != nil {
		t.Fatal(string(output))
	}
	manifest, _ := lintpack.Pack.Manifest(lint.Executable{Path: "policy-lint"})
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "policy.json"), data, 0o644)
	config := filepath.Join(dir, "lint.json")
	os.WriteFile(config, []byte(`{"version":1,"packs":[{"manifest":"policy.json"}],"rules":{"policy/forbidden-failure":"off","policy/provider-boundary":{"options":{"provider":"LiveMail","allow":["main"]}}}}`), 0o644)
	fixtures := filepath.Join(dir, "fixtures")
	os.MkdirAll(fixtures, 0o755)
	boundary, _ := os.ReadFile("../../examples/lintpack/testdata/provider_boundary.ef")
	os.WriteFile(filepath.Join(fixtures, "boundary.ef"), boundary, 0o644)
	// A negative control: no provision of LiveMail, but built-in advice.
	clean := "service Mail {\n    effect fn send(to: string) -> void\n}\n\nimpl LiveMail for Mail {\n    effect fn send(to: string) -> void {\n        void\n    }\n}\n\neffect fn task() -> string { \"ok\" }\neffect fn main() -> void { let forgotten = task(); void }\n"
	os.WriteFile(filepath.Join(fixtures, "clean.ef"), []byte(clean), 0o644)

	stdout, stderr, code := runTestCLI(t, binary, "lint", "test", fixtures, "--update", "--lint-config", config)
	if code != 0 || strings.Count(string(stdout), "updated ") != 2 {
		t.Fatalf("update: exit %d %s %s", code, stdout, stderr)
	}
	var expectation struct {
		Complete bool                      `json:"complete"`
		Rules    []lint.RuleStatus         `json:"rules"`
		Findings []compiler.LintDiagnostic `json:"findings"`
	}
	data, _ = os.ReadFile(filepath.Join(fixtures, "boundary.lint.json"))
	if json.Unmarshal(data, &expectation); !expectation.Complete || len(expectation.Findings) != 2 || expectation.Findings[0].Rule != "policy/provider-boundary" || len(expectation.Rules) != 2 || expectation.Rules[1].Findings != 2 {
		t.Fatalf("boundary expectation %s", data)
	}
	data, _ = os.ReadFile(filepath.Join(fixtures, "clean.lint.json"))
	if json.Unmarshal(data, &expectation); len(expectation.Findings) != 0 || expectation.Rules[1].Status != lint.StatusCompleted {
		t.Fatalf("clean expectation %s", data)
	}
	if lintOut, _, _ := runTestCLI(t, binary, "lint", filepath.Join(fixtures, "clean.ef")); !strings.Contains(string(lintOut), "unused-recipe") {
		t.Fatalf("control: the clean fixture has no built-in advice: %s", lintOut)
	}
	if after, _ := os.ReadFile(filepath.Join(fixtures, "clean.ef")); string(after) != clean {
		t.Fatal("ef lint test modified a fixture")
	}

	stdout, _, code = runTestCLI(t, binary, "lint", "test", fixtures, "--lint-config", config)
	if code != 0 || strings.Count(string(stdout), "ok   ") != 2 {
		t.Fatalf("check: exit %d %s", code, stdout)
	}
	// An expectation that differs by one severity fails, showing both.
	expected := filepath.Join(fixtures, "boundary.lint.json")
	data, _ = os.ReadFile(expected)
	os.WriteFile(expected, []byte(strings.Replace(string(data), `"severity": "error"`, `"severity": "warning"`, 1)), 0o644)
	stdout, _, code = runTestCLI(t, binary, "lint", "test", filepath.Join(fixtures, "boundary.ef"), filepath.Join(fixtures, "clean.ef"), "--lint-config", config)
	if code != 1 || !strings.Contains(string(stdout), "FAIL "+filepath.Join(fixtures, "boundary.ef")+"\n--- expected") || !strings.Contains(string(stdout), "ok   "+filepath.Join(fixtures, "clean.ef")) {
		t.Fatalf("mismatch: exit %d %s", code, stdout)
	}
	os.Remove(filepath.Join(fixtures, "clean.lint.json"))
	os.WriteFile(filepath.Join(fixtures, "broken.ef"), []byte("effect fn main() -> void { missing() }\n"), 0o644)
	stdout, _, code = runTestCLI(t, binary, "lint", "test", fixtures, "--update", "--lint-config", config)
	if code != 1 || !strings.Contains(string(stdout), "broken.ef: fixture source does not check") {
		t.Fatalf("unchecked fixture: exit %d %s", code, stdout)
	}
	os.Remove(filepath.Join(fixtures, "broken.ef"))
	os.Remove(filepath.Join(fixtures, "clean.lint.json"))
	stdout, _, code = runTestCLI(t, binary, "lint", "test", filepath.Join(fixtures, "clean.ef"), "--lint-config", config)
	if code != 1 || !strings.Contains(string(stdout), "no expectation") {
		t.Fatalf("missing expectation: exit %d %s", code, stdout)
	}

	empty := t.TempDir()
	noPacks := filepath.Join(empty, "no-packs.json")
	os.WriteFile(noPacks, []byte(`{"version":1}`), 0o644)
	for _, args := range [][]string{
		{"lint", "test", fixtures},
		{"lint", "test", "--lint-config", config},
		{"lint", "test", empty, "--lint-config", config},
		{"lint", "test", config, "--lint-config", config},
		{"lint", "test", fixtures, "--lint-config", config, "--target", "c"},
		// A configuration that selects no pack has nothing to test.
		{"lint", "test", fixtures, "--lint-config", noPacks},
	} {
		if _, stderr, code := runTestCLI(t, binary, args...); code != 2 {
			t.Fatalf("%v: exit %d %s", args, code, stderr)
		}
	}
}

// executableName is name as a program file on this platform: Windows
// starts only files with a PATHEXT extension, and a manifest's
// extensionless path resolves to one.
func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// canonicalNames compares variable names as the platform does: Windows
// names without case.
func canonicalNames(names []string) []string {
	if runtime.GOOS != "windows" {
		return names
	}
	upper := []string{}
	for _, name := range names {
		upper = append(upper, strings.ToUpper(name))
	}
	return upper
}

// ef lint resolves a pack's selected variables from its own process
// environment: the pack sees exactly them, and an invalid selection is an
// invalid invocation.
func TestLintPackEnvironmentCLIProcess(t *testing.T) {
	binary := buildTestCLI(t)
	dir := t.TempDir()
	if output, err := exec.Command("go", "build", "-o", filepath.Join(dir, executableName("fixturepack")), "../../lint/testdata/fixturepack").CombinedOutput(); err != nil {
		t.Fatal(string(output))
	}
	manifest := lint.Manifest{
		ManifestVersion: lint.ManifestVersion, Namespace: "fixture", Version: "1.0.0",
		FactSchema: lint.ManifestSchema{Name: lint.FactSchemaName, Versions: []int{lint.FactSchemaVersion}},
		Executable: lint.Executable{Path: "fixturepack", Args: []string{"env"}},
		Rules: []lint.ManifestRule{{
			Name: "rename-main", Version: "1", Description: "fixture rule", DefaultSeverity: lint.SeverityWarning,
			Requires: []lint.Family{lint.FamilyCallables},
			Options:  []lint.OptionSpec{{Name: "to", Type: lint.OptionString, Default: json.RawMessage(`"entry"`)}},
		}},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644)
	source := filepath.Join(dir, "main.ef")
	os.WriteFile(source, []byte("effect fn main() -> void { void }\n"), 0o644)
	lintWith := func(config string, env ...string) (compiler.LintResult, string, int) {
		t.Helper()
		path := filepath.Join(dir, "lint.json")
		os.WriteFile(path, []byte(config), 0o644)
		command := exec.Command(binary, "lint", source, "--lint-config", path)
		command.Env = append(os.Environ(), env...)
		var stdout, stderr strings.Builder
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		}
		var result compiler.LintResult
		json.Unmarshal([]byte(stdout.String()), &result)
		return result, stderr.String(), code
	}
	selected := `{"version":1,"packs":[{"manifest":"manifest.json","env":["EF_LINT_PROBE"]}]}`
	result, stderr, code := lintWith(selected, "EF_LINT_PROBE=alpha", "EF_LINT_UNSELECTED=secret")
	if code != 0 || len(result.LintDiagnostics) != 1 || !strings.HasSuffix(result.LintDiagnostics[0].Message, "present:alpha") || result.Packs[0].Analysis.Execution == nil || !reflect.DeepEqual(canonicalNames(result.Packs[0].Analysis.Execution.Variables), append([]string{"EF_LINT_PROBE"}, lint.RequiredVariables(runtime.GOOS)...)) {
		t.Fatalf("exit %d %s %+v", code, stderr, result)
	}
	if strings.Contains(fmt.Sprint(result.Packs), "alpha") {
		t.Fatal("Effra reported a selected value in its own metadata")
	}
	result, _, _ = lintWith(`{"version":1,"packs":[{"manifest":"manifest.json"}]}`, "EF_LINT_PROBE=alpha")
	if len(result.LintDiagnostics) != 1 || !strings.HasSuffix(result.LintDiagnostics[0].Message, "absent") {
		t.Fatalf("an unselecting pack: %+v", result.LintDiagnostics)
	}
	if _, stderr, code := lintWith(`{"version":1,"packs":[{"manifest":"manifest.json","env":["NOT-A-NAME"]}]}`); code != 2 || !strings.Contains(stderr, "invalid-environment") {
		t.Fatalf("invalid selection: exit %d %s", code, stderr)
	}
	// A js-only rule under the go target. Enabled by its pack's default it
	// does not apply: lint passes and is complete, reporting the skip.
	// Enabled by the project's configuration, the skip fails policy with a
	// lint-runner error (exit 1).
	manifest.Rules[0].Targets = []string{"js"}
	data, _ = json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "manifest-js.json"), data, 0o644)
	result, stderr, code = lintWith(`{"version":1,"packs":[{"manifest":"manifest-js.json"}]}`)
	if status := result.Packs[0].Rules[0]; code != 0 || !result.Complete || len(result.LintDiagnostics) != 0 || status.Status != lint.StatusSkipped || status.Reason != "target-unsupported: go" || !status.Inapplicable {
		t.Fatalf("default-enabled rule: exit %d %s %+v", code, stderr, result)
	}
	result, stderr, code = lintWith(`{"version":1,"packs":[{"manifest":"manifest-js.json"}],"rules":{"fixture/rename-main":"warning"}}`)
	if code != 1 || result.Complete || len(result.LintDiagnostics) != 1 || result.LintDiagnostics[0].Code != "EFL000" || !strings.Contains(result.LintDiagnostics[0].Message, "was skipped: target-unsupported: go") {
		t.Fatalf("configured rule: exit %d %s %+v", code, stderr, result)
	}
}
