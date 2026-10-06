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

func buildTestCLI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ef")
	command := exec.Command("go", "build", "-o", path, ".")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return path
}

func runTestCLI(t *testing.T, binary string, args ...string) ([]byte, []byte, int) {
	t.Helper()
	command := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
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

func TestDiagnosticsCLIProcessFormatsTextJSONAndUsage(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	path := filepath.Join(root, "space é.ef")
	source := `effect fn task() -> string { "ok" }
effect fn main() -> string {
let forgotten = task()
run task().provide<Console>(Stdout)
}`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}

	textOutput, textError, code := runTestCLI(t, binary, "diagnostics", path)
	if code != 0 || len(textError) != 0 || !strings.Contains(string(textOutput), "warning EFL001") || !strings.Contains(string(textOutput), "hint EFL002") {
		t.Fatalf("unexpected diagnostics text: code=%d stdout=%q stderr=%q", code, textOutput, textError)
	}

	jsonOutput, jsonError, code := runTestCLI(t, binary, "diagnostics", path, "--strict", "--json")
	if code != 1 || len(jsonError) == 0 {
		t.Fatalf("strict diagnostics did not fail policy: code=%d stderr=%q", code, jsonError)
	}
	var report struct {
		Checked      bool `json:"checked"`
		PolicyPassed bool `json:"policyPassed"`
		Strict       bool `json:"strict"`
		Source       struct {
			URI string `json:"uri"`
		} `json:"source"`
		Diagnostics []struct {
			Severity string `json:"severity"`
			LSP      struct {
				Severity int `json:"severity"`
			} `json:"lsp"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(jsonOutput, &report); err != nil {
		t.Fatalf("diagnostics JSON was not parseable: %v\n%s", err, jsonOutput)
	}
	if !report.Checked || report.PolicyPassed || !report.Strict || !strings.HasPrefix(report.Source.URI, "file://") || strings.Contains(report.Source.URI, " ") || !strings.Contains(report.Source.URI, "%20") || !strings.Contains(report.Source.URI, "%C3%A9") || len(report.Diagnostics) != 2 || report.Diagnostics[0].Severity != "warning" || report.Diagnostics[0].LSP.Severity != 2 || report.Diagnostics[1].Severity != "hint" || report.Diagnostics[1].LSP.Severity != 4 {
		t.Fatalf("wrong diagnostics JSON: %+v", report)
	}

	_, usageErrorOutput, code := runTestCLI(t, binary, "diagnostics", path, "--unknown")
	if code != 2 || !strings.Contains(string(usageErrorOutput), "unknown option") {
		t.Fatalf("invalid diagnostics invocation did not use exit 2: code=%d stderr=%q", code, usageErrorOutput)
	}

	_, missingError, code := runTestCLI(t, binary, "diagnostics", filepath.Join(root, "missing.ef"), "--json")
	if code != 1 || len(missingError) == 0 {
		t.Fatalf("missing source was not reported as an operational failure: code=%d stderr=%q", code, missingError)
	}
	_, unsupportedError, code := runTestCLI(t, binary, "diagnostics", path, "--target", "llvm")
	if code != 2 || !strings.Contains(string(unsupportedError), "unsupported target") {
		t.Fatalf("unsupported target was not rejected as invalid invocation: code=%d stderr=%q", code, unsupportedError)
	}

	suppressedPath := filepath.Join(root, "suppressed.ef")
	suppressedSource := `effect fn task() -> string { "ok" }
effect fn main() -> string {
// effra-lint-disable-next-line unused-recipe -- deliberate deferred hook
let forgotten = task()
"ok"
}`
	if err := os.WriteFile(suppressedPath, []byte(suppressedSource), 0600); err != nil {
		t.Fatal(err)
	}
	suppressedOutput, suppressedError, code := runTestCLI(t, binary, "diagnostics", suppressedPath, "--strict", "--json")
	if code != 0 || len(suppressedError) != 0 {
		t.Fatalf("reasoned suppression did not pass diagnostics policy: code=%d stderr=%q", code, suppressedError)
	}
	var suppressed struct {
		PolicyPassed bool              `json:"policyPassed"`
		Diagnostics  []json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal(suppressedOutput, &suppressed); err != nil || !suppressed.PolicyPassed || len(suppressed.Diagnostics) != 0 {
		t.Fatalf("suppressed diagnostics report was not empty and passing: err=%v report=%+v", err, suppressed)
	}
}
