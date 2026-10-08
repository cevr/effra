package lintpack

import (
	"os"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/lint"
)

type expected struct {
	rule, severity, message, text string
	related                       []string
}

// evaluate compiles a real fixture and runs the pack in-process through the
// same registry, configuration and fact derivation a runner uses.
func evaluate(t *testing.T, fixture, config string) (lint.Report, string) {
	t.Helper()
	source, err := os.ReadFile("testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	result := compiler.CompileAt(string(source), "go", "testdata")
	if !result.Checked {
		t.Fatal(result.Diagnostics)
	}
	manifest, err := Pack.Manifest(lint.Executable{Path: "bin/policy-lint"})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := compiler.LintRegistry(manifest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := lint.ParseConfig([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	configuration, problems := registry.Configure(parsed)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	report, err := lint.Evaluate(Pack, configuration, result.LintFacts())
	if err != nil {
		t.Fatal(err)
	}
	return report, string(source)
}

func assertFindings(t *testing.T, report lint.Report, source string, want []expected) {
	t.Helper()
	if !report.Complete {
		t.Fatalf("incomplete analysis: %+v", report.Rules)
	}
	if len(report.Findings) != len(want) {
		t.Fatalf("got %d findings, want %d: %+v", len(report.Findings), len(want), report.Findings)
	}
	// Spans are compiler anchors (a token), so expectations name the source
	// text starting at the anchor.
	at := func(span lint.Span) string {
		if span.Length == 0 {
			return ""
		}
		return source[span.Offset:]
	}
	for i, finding := range report.Findings {
		w := want[i]
		if finding.Rule != w.rule || string(finding.Severity) != w.severity || finding.Message != w.message || !strings.HasPrefix(at(finding.Span), w.text) {
			t.Fatalf("finding %d: got %+v, want %+v", i, finding, w)
		}
		if len(finding.Related) != len(w.related) {
			t.Fatalf("finding %d related: %+v, want %v", i, finding.Related, w.related)
		}
		for j, related := range finding.Related {
			if !strings.HasPrefix(at(related.Span), w.related[j]) {
				t.Fatalf("finding %d related %d: %+v, want %q", i, j, related, w.related[j])
			}
		}
	}
}

const boundaryConfig = `{"version":1,"rules":{
	"policy/forbidden-failure":"off",
	"policy/provider-boundary":{"options":{"provider":"LiveMail","allow":["main"]}}}}`

func TestProviderBoundaryFollowsResolvedProviderIdentity(t *testing.T) {
	report, source := evaluate(t, "provider_boundary.ef", boundaryConfig)
	// shadowed spells LiveMail three times but provides a local FakeMail
	// value; aliased never spells LiveMail at its provision; layered
	// provides it only through a layer selection. A spelling rule would
	// flag the first and miss the other two.
	assertFindings(t, report, source, []expected{
		{"policy/provider-boundary", "error", "aliased provides LiveMail; only [main] may provide it", "provide<Mail>(mailer)", nil},
		{"policy/provider-boundary", "error", "layered provides LiveMail; only [main] may provide it", "provide(Production)", []string{"Mail = LiveMail"}},
	})
	shadowed := strings.Index(source, "fn shadowed")
	if !strings.Contains(source[shadowed:strings.Index(source, "fn aliased")], ".provide<Mail>(LiveMail)") {
		t.Fatal("fixture lost its same-spelled shadowing control")
	}
	if report.Rules[0].Rule != "policy/forbidden-failure" || report.Rules[0].Status != lint.StatusOff || report.Rules[1].Status != lint.StatusCompleted || report.Rules[1].Findings != 2 {
		t.Fatalf("statuses: %+v", report.Rules)
	}

	// An allowed exception and a configured severity are policy, not
	// language: permitting layered leaves only the alias.
	report, source = evaluate(t, "provider_boundary.ef", `{"version":1,"rules":{"policy/forbidden-failure":"off",
		"policy/provider-boundary":{"severity":"warning","options":{"provider":"LiveMail","allow":["main","layered"]}}}}`)
	assertFindings(t, report, source, []expected{
		{"policy/provider-boundary", "warning", "aliased provides LiveMail; only [main layered] may provide it", "provide<Mail>(mailer)", nil},
	})

	// Restricting the fake instead reports exactly the shadowed provision.
	report, source = evaluate(t, "provider_boundary.ef", `{"version":1,"rules":{"policy/forbidden-failure":"off",
		"policy/provider-boundary":{"options":{"provider":"FakeMail","allow":["main"]}}}}`)
	assertFindings(t, report, source, []expected{
		{"policy/provider-boundary", "error", "shadowed provides FakeMail; only [main] may provide it", "provide<Mail>(LiveMail)", nil},
	})
}

func TestForbiddenFailureReadsCompleteCheckedRows(t *testing.T) {
	report, source := evaluate(t, "forbidden_failure.ef", `{"version":1,"rules":{"policy/provider-boundary":"off",
		"policy/forbidden-failure":{"options":{"failure":"Denied","functions":["forwards","recovers","declaresOnly","mentions","main"]}}}}`)
	// forwards receives Denied only through its helper; recovers removes it
	// with catch; declaresOnly declares but cannot raise it; mentions spells Denied as a comment, string, local and
	// record field without raising it.
	assertFindings(t, report, source, []expected{
		{"policy/forbidden-failure", "error", "failure Denied can escape forwards", "forwards", []string{"run helper(id)"}},
	})

	operators := []struct{ failure, function, contribution string }{
		{"Timeout", "waits", "run slow().timeout(1)"},
		{"GoError", "parses", `run strconv.ParseBool("maybe").orFail()`},
	}
	for _, operator := range operators {
		report, source := evaluate(t, "forbidden_failure.ef", `{"version":1,"rules":{"policy/provider-boundary":"off",
			"policy/forbidden-failure":{"options":{"failure":"`+operator.failure+`","functions":["`+operator.function+`","main"]}}}}`)
		assertFindings(t, report, source, []expected{
			{"policy/forbidden-failure", "error", "failure " + operator.failure + " can escape " + operator.function, operator.function, []string{operator.contribution}},
		})
	}
}

func TestPolicyConfigurationFailuresAreExplicit(t *testing.T) {
	// A configuration naming a declaration the program lacks fails the rule;
	// it is never a clean pass.
	report, _ := evaluate(t, "forbidden_failure.ef", `{"version":1,"rules":{"policy/provider-boundary":"off",
		"policy/forbidden-failure":{"options":{"failure":"Missing","functions":["main"]}}}}`)
	if report.Complete || report.Rules[0].Status != lint.StatusFailed || !strings.Contains(report.Rules[0].Reason, `configured failure "Missing" is not declared`) {
		t.Fatalf("missing declaration: %+v", report.Rules)
	}
	// Required options are validated before any rule runs.
	manifest, _ := Pack.Manifest(lint.Executable{Path: "bin/policy-lint"})
	registry, err := compiler.LintRegistry(manifest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := lint.ParseConfig([]byte(`{"version":1,"rules":{"policy/forbidden-failure":{"options":{"failure":"Denied"}}}}`))
	if _, problems := registry.Configure(parsed); len(problems) != 2 || problems[0].Code != lint.ProblemInvalidOptions {
		t.Fatalf("missing required options accepted: %+v", problems)
	}
	// Unchecked source has no checked facts: rules are skipped, not passed.
	unchecked := compiler.Compile(`effect fn main() -> void { run missing() }`)
	parsed, _ = lint.ParseConfig([]byte(boundaryConfig))
	configuration, problems := registry.Configure(parsed)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	skipped, err := lint.Evaluate(Pack, configuration, unchecked.LintFacts())
	if err != nil || skipped.Complete || skipped.Rules[1].Status != lint.StatusSkipped || !strings.Contains(skipped.Rules[1].Reason, lint.ReasonUncheckedSource) {
		t.Fatalf("unchecked: %+v %v", skipped, err)
	}
}
