package compiler

import (
	"strings"
	"testing"

	lintsdk "effra.local/prototype/lint"
)

func lintConfiguration(t *testing.T, document string) *lintsdk.Configuration {
	t.Helper()
	registry, err := LintRegistry()
	if err != nil {
		t.Fatal(err)
	}
	config, err := lintsdk.ParseConfig([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	configuration, problems := registry.Configure(config)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	return configuration
}

// Built-in advice follows the configuration: off removes a rule and its
// unused-suppression check, and a configured severity replaces the default.
func TestLintWithAppliesConfiguredBuiltinSeverity(t *testing.T) {
	const forgotten = "effect fn task() -> string { \"ok\" }\neffect fn main() -> void { let forgotten = task(); void }"
	const suppressed = "effect fn task() -> string { \"ok\" }\neffect fn main() -> void {\n// effra-lint-disable-next-line unused-recipe -- kept\nlet forgotten = task()\nlet used = 1\nvoid\n}"
	cases := []struct {
		name, source, config string
		severity             string
		findings             int
		passed               bool
	}{
		{"default", forgotten, `{"version":1}`, "warning", 1, true},
		{"error", forgotten, `{"version":1,"rules":{"unused-recipe":"error"}}`, "error", 1, false},
		{"hint", forgotten, `{"version":1,"rules":{"unused-recipe":"hint"}}`, "suggestion", 1, true},
		{"off", forgotten, `{"version":1,"rules":{"unused-recipe":"off"}}`, "", 0, true},
		// The rule that would use the suppression did not run, so the
		// suppression is not reported unused.
		{"off suppressed", strings.Replace(suppressed, "let forgotten = task()", "let forgotten = 1", 1), `{"version":1,"rules":{"unused-recipe":"off"}}`, "", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := CompileAt(c.source, "go", ".")
			if !result.Checked {
				t.Fatal(result.Diagnostics)
			}
			lint := result.LintWith(false, LintPacks{Configuration: lintConfiguration(t, c.config)})
			if len(lint.LintDiagnostics) != c.findings || lint.LintPassed != c.passed || !lint.Complete {
				t.Fatalf("%+v", lint)
			}
			if c.findings > 0 && lint.LintDiagnostics[0].Severity != c.severity {
				t.Fatalf("severity %s, want %s", lint.LintDiagnostics[0].Severity, c.severity)
			}
		})
	}
	// Control: with the rule on, the same unused suppression is an error.
	result := CompileAt(strings.Replace(suppressed, "let forgotten = task()", "let forgotten = 1", 1), "go", ".")
	if lint := result.Lint(false); lint.LintPassed || lint.LintDiagnostics[0].Code != suppressionCode {
		t.Fatalf("control: %+v", lint.LintDiagnostics)
	}
}

// Pack statuses decide completeness and policy: a skipped or failed rule
// is a lint-runner error, so incomplete pack execution fails lint even
// when nothing else would. An inapplicable skip is not.
func TestLintWithMergesPackStatuses(t *testing.T) {
	result := CompileAt("effect fn main() -> void { void }", "go", ".")
	report := func(statuses ...lintsdk.RuleStatus) LintPacks {
		complete := true
		for _, status := range statuses {
			complete = complete && (status.Status != lintsdk.StatusSkipped || status.Inapplicable) && status.Status != lintsdk.StatusFailed
		}
		return LintPacks{Reports: []LintPackReport{{Pack: "acme", Identity: "sha256:m", Report: lintsdk.Report{Complete: complete, Rules: statuses}}}}
	}
	for _, reason := range []string{"target-unsupported: js", "facts-unavailable: bindings (fact-budget-exhausted)"} {
		skipped := result.LintWith(false, report(lintsdk.RuleStatus{Rule: "acme/a", Status: lintsdk.StatusSkipped, Reason: reason}))
		if skipped.Complete || skipped.LintPassed || skipped.Errors != 1 || len(skipped.LintDiagnostics) != 1 || skipped.LintDiagnostics[0].Code != lintRunnerCode || skipped.LintDiagnostics[0].Message != "rule acme/a of pack acme (sha256:m) was skipped: "+reason || skipped.Packs[0].Rules[0].Reason != reason {
			t.Fatalf("skipped: %+v", skipped)
		}
	}
	inapplicable := result.LintWith(false, report(lintsdk.RuleStatus{Rule: "acme/a", Status: lintsdk.StatusSkipped, Reason: "target-unsupported: js", Inapplicable: true}))
	if !inapplicable.Complete || !inapplicable.LintPassed || len(inapplicable.LintDiagnostics) != 0 || !inapplicable.Packs[0].Rules[0].Inapplicable {
		t.Fatalf("inapplicable: %+v", inapplicable)
	}
	// Control: a pack whose rules all completed or are off passes.
	off := result.LintWith(false, report(lintsdk.RuleStatus{Rule: "acme/a", Status: lintsdk.StatusOff}, lintsdk.RuleStatus{Rule: "acme/b", Status: lintsdk.StatusCompleted}))
	if !off.Complete || !off.LintPassed || len(off.LintDiagnostics) != 0 {
		t.Fatalf("complete: %+v", off)
	}
	failed := result.LintWith(false, report(lintsdk.RuleStatus{Rule: "acme/a", Status: lintsdk.StatusFailed, Reason: "boom"}))
	if failed.Complete || failed.LintPassed || failed.Errors != 1 || failed.LintDiagnostics[0].Code != lintRunnerCode || failed.LintDiagnostics[0].Message != "rule acme/a of pack acme (sha256:m) failed: boom" {
		t.Fatalf("failed: %+v", failed)
	}
	// Findings order by offset; on equal offsets packs keep their
	// namespace order.
	finding := func(rule string, offset int) lintsdk.ReportedFinding {
		return lintsdk.ReportedFinding{Rule: rule, Severity: lintsdk.SeverityInformation, Message: rule, Span: lintsdk.Span{Offset: offset, Length: 1, Line: 1, Column: offset + 1}}
	}
	packs := LintPacks{Reports: []LintPackReport{
		{Pack: "a", Report: lintsdk.Report{Complete: true, Findings: []lintsdk.ReportedFinding{finding("a/x", 5), finding("a/y", 1)}}},
		{Pack: "b", Report: lintsdk.Report{Complete: true, Findings: []lintsdk.ReportedFinding{finding("b/x", 1)}}},
	}}
	merged := result.LintWith(false, packs)
	var order []string
	for _, diagnostic := range merged.LintDiagnostics {
		order = append(order, diagnostic.Rule)
	}
	if strings.Join(order, ",") != "a/y,b/x,a/x" || merged.Information != 3 || !merged.LintPassed || !merged.Complete {
		t.Fatalf("order %v: %+v", order, merged)
	}
}

// On an equal offset, built-in advice comes first and then packs in
// namespace order.
func TestLintWithOrdersBuiltinAdviceBeforePackFindingsAtAnEqualOffset(t *testing.T) {
	result := CompileAt("effect fn task() -> string { \"ok\" }\neffect fn main() -> void { let forgotten = task(); void }", "go", ".")
	builtin := result.Lint(false)
	if len(builtin.LintDiagnostics) != 1 || builtin.LintDiagnostics[0].Rule != "unused-recipe" {
		t.Fatalf("control: %+v", builtin.LintDiagnostics)
	}
	span := builtin.LintDiagnostics[0].Span
	at := func(rule string) lintsdk.ReportedFinding {
		return lintsdk.ReportedFinding{Rule: rule, Severity: lintsdk.SeverityWarning, Message: rule, Span: lintsdk.Span{Offset: span.Offset, Length: span.Length, Line: span.Line, Column: span.Column}}
	}
	merged := result.LintWith(false, LintPacks{Reports: []LintPackReport{
		{Pack: "a", Report: lintsdk.Report{Complete: true, Findings: []lintsdk.ReportedFinding{at("a/x")}}},
		{Pack: "b", Report: lintsdk.Report{Complete: true, Findings: []lintsdk.ReportedFinding{at("b/x")}}},
	}})
	var order []string
	for _, diagnostic := range merged.LintDiagnostics {
		order = append(order, diagnostic.Rule)
	}
	if strings.Join(order, ",") != "unused-recipe,a/x,b/x" {
		t.Fatalf("order at offset %d: %v", span.Offset, order)
	}
}
