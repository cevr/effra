package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
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

func runTestCLIInput(t *testing.T, binary, input string, args ...string) ([]byte, []byte, int) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Stdin = strings.NewReader(input)
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

func TestFormatCLIProcessModesAndAtomicWrites(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	path := filepath.Join(root, "main.ef")
	source := `import go missing "example.invalid/no-such-package"
effect fn main() -> string { "ok" }`
	want, err := compiler.FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0711); err != nil {
		t.Fatal(err)
	}

	stdinOutput, stdinError, code := runTestCLIInput(t, binary, source, "fmt", "--stdin")
	if code != 0 || string(stdinOutput) != want.Text || len(stdinError) != 0 {
		t.Fatalf("stdin formatting mismatch: code=%d stdout=%q stderr=%q want=%q", code, stdinOutput, stdinError, want.Text)
	}
	jsonOutput, jsonError, code := runTestCLI(t, binary, "fmt", "--check", "--json", path)
	if code != 1 || len(jsonError) != 0 {
		t.Fatalf("format check did not report difference: code=%d stderr=%q", code, jsonError)
	}
	var checkReport struct {
		Success bool   `json:"success"`
		Mode    string `json:"mode"`
		Files   []struct {
			Changed bool `json:"changed"`
			Written bool `json:"written"`
		} `json:"files"`
	}
	if err := json.Unmarshal(jsonOutput, &checkReport); err != nil || !checkReport.Success || checkReport.Mode != "check" || len(checkReport.Files) != 1 || !checkReport.Files[0].Changed || checkReport.Files[0].Written {
		t.Fatalf("wrong format check report: err=%v report=%+v", err, checkReport)
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeOutput, writeError, code := runTestCLI(t, binary, "fmt", "--json", path)
	if code != 0 || len(writeError) != 0 {
		t.Fatalf("format write failed: code=%d stderr=%q stdout=%q", code, writeError, writeOutput)
	}
	var writeReport struct {
		FormatterVersion string `json:"formatterVersion"`
		Files            []struct {
			Changed bool `json:"changed"`
			Written bool `json:"written"`
		} `json:"files"`
	}
	if err := json.Unmarshal(writeOutput, &writeReport); err != nil || writeReport.FormatterVersion == "" || len(writeReport.Files) != 1 || !writeReport.Files[0].Changed || !writeReport.Files[0].Written {
		t.Fatalf("wrong format write report: err=%v report=%+v", err, writeReport)
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != want.Text {
		t.Fatalf("wrong formatted file: got=%q want=%q", actual, want.Text)
	}
	afterWrite, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if afterWrite.Mode().Perm() != 0711 || before.Mode().Perm() != afterWrite.Mode().Perm() {
		t.Fatalf("file mode changed: before=%#o after=%#o", before.Mode().Perm(), afterWrite.Mode().Perm())
	}
	unchangedOutput, unchangedError, code := runTestCLI(t, binary, "fmt", path)
	if code != 0 || len(unchangedOutput) != 0 || !strings.Contains(string(unchangedError), "already formatted") {
		t.Fatalf("unchanged formatting was not a no-op: code=%d stdout=%q stderr=%q", code, unchangedOutput, unchangedError)
	}
	afterNoop, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !afterNoop.ModTime().Equal(afterWrite.ModTime()) {
		t.Fatal("unchanged formatting rewrote the file")
	}

	valid := filepath.Join(root, "valid.ef")
	invalid := filepath.Join(root, "invalid.ef")
	validSource := `effect fn main() -> string { "valid" }`
	if err := os.WriteFile(valid, []byte(validSource), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalid, []byte(`effect fn main() -> string { @ }`), 0600); err != nil {
		t.Fatal(err)
	}
	mixedOutput, mixedError, code := runTestCLI(t, binary, "fmt", "--json", valid, invalid)
	if code != 2 || len(mixedError) != 0 {
		t.Fatalf("mixed syntax failure had wrong process result: code=%d stderr=%q stdout=%q", code, mixedError, mixedOutput)
	}
	var mixedReport struct {
		Success  bool `json:"success"`
		Partial  bool `json:"partial"`
		Failures []struct {
			Code string `json:"code"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(mixedOutput, &mixedReport); err != nil || mixedReport.Success || mixedReport.Partial || len(mixedReport.Failures) == 0 || mixedReport.Failures[0].Code != "EFMT_SYNTAX" {
		t.Fatalf("mixed syntax failure was not structured: err=%v report=%+v", err, mixedReport)
	}
	unchangedValid, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchangedValid) != validSource {
		t.Fatal("syntax failure partially wrote an earlier file")
	}

	_, stdinJSONError, code := runTestCLIInput(t, binary, source, "fmt", "--stdin", "--json")
	if code != 2 || len(stdinJSONError) != 0 {
		t.Fatalf("incompatible stdin invocation was not a structured JSON failure: code=%d stderr=%q", code, stdinJSONError)
	}
	symlink := filepath.Join(root, "link.ef")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	_, symlinkError, code := runTestCLI(t, binary, "fmt", symlink)
	if code != 2 || !strings.Contains(string(symlinkError), "EFMT_SYMLINK") {
		t.Fatalf("symlink write was not rejected: code=%d stderr=%q", code, symlinkError)
	}
}

func TestFormatApplySeamReportsStaleAndEarlierWrites(t *testing.T) {
	root := t.TempDir()
	source := `effect fn main() -> string { "ok" }`
	firstPath := filepath.Join(root, "first.ef")
	secondPath := filepath.Join(root, "second.ef")
	if err := os.WriteFile(firstPath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := compiler.FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	firstInfo, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	report := formatReport{Files: []formatFileReport{{Path: firstPath}, {Path: secondPath}}}
	plans := []formatPlan{
		{path: firstPath, info: firstInfo, source: []byte(source), result: result, reportAt: 0},
		{path: secondPath, info: secondInfo, source: []byte(source), result: result, reportAt: 1},
	}
	calls := 0
	err = applyFormatPlans(plans, &report, func(plan formatPlan) error {
		calls++
		if calls == 2 {
			return &formatAdapterError{code: "EFMT_STALE", path: plan.path, message: "test stale source"}
		}
		return nil
	})
	if err == nil || !report.Partial || report.Success || !report.Files[0].Written || report.Files[1].Written || len(report.Failures) != 1 || report.Failures[0].Code != "EFMT_STALE" {
		t.Fatalf("partial apply report lost causal state: err=%v report=%+v", err, report)
	}
}
