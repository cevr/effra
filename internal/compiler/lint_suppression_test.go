package compiler

import (
	"strings"
	"testing"

	lintsdk "effra.local/prototype/lint"
)

func suppressionManifest(namespace string, rules ...string) lintsdk.Manifest {
	manifest := lintsdk.Manifest{
		ManifestVersion: lintsdk.ManifestVersion, Namespace: namespace, Version: "1.0.0",
		FactSchema: lintsdk.ManifestSchema{Name: lintsdk.FactSchemaName, Versions: []int{lintsdk.FactSchemaVersion}},
		Executable: lintsdk.Executable{Path: namespace + "-lint"},
	}
	for _, rule := range rules {
		manifest.Rules = append(manifest.Rules, lintsdk.ManifestRule{Name: rule, Version: "1", Description: "rule", DefaultSeverity: lintsdk.SeverityWarning, Requires: []lintsdk.Family{lintsdk.FamilyCallables}})
	}
	return manifest
}

// Every well-formed directive receives a status from the rule it names:
// applied when its rule removed a finding on the covered line, unused when
// the rule completed without one, and not-evaluated with the reason the
// rule did not run. Only unused is an error. Malformed and unknown names
// are EFL004 findings without a status.
func TestLintSuppressionStatesFollowRuleExecution(t *testing.T) {
	source := strings.Join([]string{
		`effect fn task() -> string { "ok" }`,
		`effect fn main() -> void {`,
		`// effra-lint-disable-next-line unused-recipe -- applied`,
		`let forgotten = task()`,
		`// effra-lint-disable-next-line redundant-provision -- nothing to suppress`,
		`void`,
		`// effra-lint-disable-next-line unused-go-import -- configured off`,
		`void`,
		`// effra-lint-disable-next-line acme/applied -- applied`,
		`void`,
		`// effra-lint-disable-next-line acme/unused -- the finding is elsewhere`,
		`void`,
		`void`,
		`// effra-lint-disable-next-line acme/off -- off`,
		`void`,
		`// effra-lint-disable-next-line acme/failed -- failed`,
		`void`,
		`// effra-lint-disable-next-line acme/facts -- facts`,
		`void`,
		`// effra-lint-disable-next-line acme/target -- target`,
		`void`,
		`// effra-lint-disable-next-line crash/rule -- crashed pack`,
		`void`,
		`// effra-lint-disable-next-line other/rule -- not selected`,
		`void`,
		`// effra-lint-disable-next-line acme/missing -- unknown rule of a selected pack`,
		`// effra-lint-disable-next-line effra/rule -- reserved namespace`,
		`// effra-lint-disable-next-line invalid-suppression -- fixed rule`,
		`// effra-lint-disable-next-line lint-runner -- runner`,
		`// effra-lint-disable-next-line EF108 -- compiler code`,
		`// effra-lint-disable-next-line Acme/rule -- not an identifier`,
		`// effra-lint-disable-next-line acme/a/b -- two separators`,
		`// effra-lint-disable-next-line acme/ -- empty rule`,
		`void`,
		`}`,
	}, "\n")
	result := CompileAt(source, "go", ".")
	if !result.Checked {
		t.Fatal(result.Diagnostics)
	}
	registry, err := LintRegistry(suppressionManifest("acme", "applied", "unused", "off", "failed", "facts", "target"), suppressionManifest("crash", "rule"))
	if err != nil {
		t.Fatal(err)
	}
	configuration, problems := registry.Configure(lintsdk.Config{Version: lintsdk.ConfigVersion, Rules: map[string]lintsdk.RuleConfig{"unused-go-import": {Severity: lintsdk.SeverityOff}, "acme/off": {Severity: lintsdk.SeverityOff}}})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	finding := func(rule string, line int) lintsdk.ReportedFinding {
		return lintsdk.ReportedFinding{Rule: rule, Severity: lintsdk.SeverityWarning, Message: rule + " finding", Span: lintsdk.Span{Offset: 0, Length: 1, Line: line, Column: 1}}
	}
	packs := LintPacks{Configuration: configuration, Reports: []LintPackReport{
		{Pack: "acme", Identity: "sha256:acme", Report: lintsdk.Report{Rules: []lintsdk.RuleStatus{
			{Rule: "acme/applied", Status: lintsdk.StatusCompleted, Findings: 1},
			{Rule: "acme/facts", Status: lintsdk.StatusSkipped, Reason: "facts-unavailable: bindings (fact-budget-exhausted)"},
			{Rule: "acme/failed", Status: lintsdk.StatusFailed, Reason: "rule returned an error"},
			{Rule: "acme/off", Status: lintsdk.StatusOff},
			{Rule: "acme/target", Status: lintsdk.StatusSkipped, Reason: "target-unsupported: go", Inapplicable: true},
			{Rule: "acme/unused", Status: lintsdk.StatusCompleted, Findings: 1},
		}, Findings: []lintsdk.ReportedFinding{finding("acme/applied", 10), finding("acme/unused", 13)}}},
		{Pack: "crash", Identity: "sha256:crash", Report: lintsdk.Report{Failure: &lintsdk.ExecutionFailure{Pack: "crash", Code: lintsdk.FailureCrashed, Message: "pack exited unsuccessfully"}, Rules: []lintsdk.RuleStatus{{Rule: "crash/rule", Status: lintsdk.StatusFailed, Reason: "pack-failed: crashed"}}}},
	}}
	lint := result.LintWith(false, packs)

	want := []struct{ rule, status, reason string }{
		{"unused-recipe", SuppressionApplied, ""},
		{"redundant-provision", SuppressionUnused, ""},
		{"unused-go-import", SuppressionNotEvaluated, NotEvaluatedRuleOff},
		{"acme/applied", SuppressionApplied, ""},
		{"acme/unused", SuppressionUnused, ""},
		{"acme/off", SuppressionNotEvaluated, NotEvaluatedRuleOff},
		{"acme/failed", SuppressionNotEvaluated, NotEvaluatedPackFailed},
		{"acme/facts", SuppressionNotEvaluated, NotEvaluatedFactsUnavailable},
		{"acme/target", SuppressionNotEvaluated, NotEvaluatedTargetUnsupported},
		{"crash/rule", SuppressionNotEvaluated, NotEvaluatedPackFailed},
		{"other/rule", SuppressionNotEvaluated, NotEvaluatedPackNotSelected},
	}
	if len(lint.Suppressions) != len(want) {
		t.Fatalf("suppressions: %+v", lint.Suppressions)
	}
	for i, status := range lint.Suppressions {
		if status.Rule != want[i].rule || status.Status != want[i].status || status.Reason != want[i].reason || status.Line != status.Span.Line+1 {
			t.Fatalf("suppression %d: %+v, want %+v", i, status, want[i])
		}
	}

	var invalid, unused []string
	for _, diagnostic := range lint.LintDiagnostics {
		switch {
		case diagnostic.Code == suppressionCode && strings.HasPrefix(diagnostic.Message, "unused"):
			unused = append(unused, diagnostic.Message)
		case diagnostic.Code == suppressionCode:
			invalid = append(invalid, diagnostic.Message)
		case diagnostic.Rule == "acme/applied" || diagnostic.Rule == "unused-recipe":
			t.Fatalf("suppressed finding published: %+v", diagnostic)
		}
	}
	if strings.Join(unused, "|") != "unused lint suppression for redundant-provision|unused lint suppression for acme/unused" {
		t.Fatalf("unused: %q", unused)
	}
	wantInvalid := []string{
		"unknown lint rule acme/missing",
		"unknown lint rule effra/rule",
		"unknown lint rule invalid-suppression",
		"unknown lint rule lint-runner",
		"unknown lint rule EF108",
		"malformed lint suppression: rule Acme/rule must be a built-in rule name or namespace/rule",
		"malformed lint suppression: rule acme/a/b must be a built-in rule name or namespace/rule",
		"malformed lint suppression: rule acme/ must be a built-in rule name or namespace/rule",
	}
	if strings.Join(invalid, "|") != strings.Join(wantInvalid, "|") {
		t.Fatalf("invalid:\n%q\nwant\n%q", invalid, wantInvalid)
	}
	// The unsuppressed pack finding remains; the failed rule and the crashed
	// pack are lint-runner errors that no directive can remove. Not-evaluated
	// adds no error of its own.
	runner := 0
	for _, diagnostic := range lint.LintDiagnostics {
		if diagnostic.Code == lintRunnerCode {
			runner++
		}
	}
	if runner != 3 || lint.Errors != len(wantInvalid)+2+runner || lint.Warnings != 1 || lint.LintPassed || lint.Complete {
		t.Fatalf("policy: runner=%d %+v", runner, lint)
	}
}

// A suppression never hides a compiler error, whatever it names, and a
// not-evaluated suppression alone does not fail policy.
func TestLintSuppressionNeverHidesCompilerErrors(t *testing.T) {
	for _, rule := range []string{"unused-recipe", "EF108", "acme/rule"} {
		source := "effect fn main() -> void {\n// effra-lint-disable-next-line " + rule + " -- hide it\nrun Console.log(\"x\")\n}"
		result := CompileAt(source, "go", ".")
		if result.Checked || !hasCode(result, "EF108") {
			t.Fatalf("%s: compiler diagnostic was suppressed: %+v", rule, result.Diagnostics)
		}
		report := result.DiagnosticReport(SourceSnapshot{URI: "main.ef", Text: source}, false)
		if report.PolicyPassed || len(report.Diagnostics) == 0 || report.Diagnostics[0].Code != "EF108" {
			t.Fatalf("%s: %+v", rule, report)
		}
	}
	// Control: checked source whose only directive names an unselected
	// pack passes with that directive not evaluated.
	source := "effect fn main() -> void {\n// effra-lint-disable-next-line acme/rule -- pack runs in CI\nvoid\n}"
	lint := CompileAt(source, "go", ".").Lint(true)
	if !lint.LintPassed || len(lint.LintDiagnostics) != 0 || len(lint.Suppressions) != 1 || lint.Suppressions[0].Reason != NotEvaluatedPackNotSelected {
		t.Fatalf("control: %+v", lint)
	}
}
