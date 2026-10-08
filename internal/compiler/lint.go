package compiler

import (
	"fmt"
	"slices"
	"strings"

	lintsdk "effra.local/prototype/lint"
)

// Lint is deliberately separate from admission: optional advice cannot weaken
// compiler errors or manufacture types for an unchecked tree.
type LintRule struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
}
type LintDiagnostic struct {
	Code        string               `json:"code"`
	Rule        string               `json:"rule"`
	Severity    string               `json:"severity"`
	Message     string               `json:"message"`
	Span        Span                 `json:"span"`
	Related     []RelatedLocation    `json:"related,omitempty"`
	Suggestions []lintsdk.Suggestion `json:"suggestions,omitempty"`
}

// LintResult is built-in lint advice merged with the reports of the
// selected rule packs. Complete is false when lint could not evaluate every
// enabled rule: unchecked source, a built-in rule that did not complete, or
// a pack rule that failed or was skipped (unless inapplicable). Packs
// lists each selected pack's execution in namespace order; its findings are
// in LintDiagnostics.
type LintResult struct {
	ProducerMetadata
	SchemaVersion   int              `json:"schemaVersion"`
	Revision        string           `json:"revision"`
	Target          string           `json:"target"`
	Checked         bool             `json:"checked"`
	LintPassed      bool             `json:"lintPassed"`
	Complete        bool             `json:"complete"`
	Strict          bool             `json:"strict"`
	Diagnostics     []Diagnostic     `json:"diagnostics"`
	LintDiagnostics []LintDiagnostic `json:"lintDiagnostics"`
	Packs           []LintPackStatus `json:"packs"`
	Errors          int              `json:"errors"`
	Warnings        int              `json:"warnings"`
	Information     int              `json:"information"`
	Suggestions     int              `json:"suggestions"`
}

// LintPacks is the lint configuration in effect with the reports of its
// selected rule packs over one result, in namespace order. The zero value
// is the default configuration with no packs.
type LintPacks struct {
	Configuration *lintsdk.Configuration
	Reports       []LintPackReport
}

// LintPackReport is one selected pack's report with the manifest identity
// that names it in a lint-runner error.
type LintPackReport struct {
	Pack     string
	Identity string
	Report   lintsdk.Report
}

// LintPackStatus is one selected pack's execution in a lint result.
type LintPackStatus struct {
	Pack     string                    `json:"pack"`
	Identity string                    `json:"identity"`
	Analysis lintsdk.AnalysisIdentity  `json:"analysis"`
	Complete bool                      `json:"complete"`
	Failure  *lintsdk.ExecutionFailure `json:"failure,omitempty"`
	Rules    []lintsdk.RuleStatus      `json:"rules"`
}

// builtinSeverity is the displayed severity of a built-in rule under the
// configuration, or false when the rule is off. Configured hint keeps the
// built-in vocabulary's "suggestion".
func (p LintPacks) builtinSeverity(rule LintRule) (string, bool) {
	if p.Configuration == nil {
		return rule.Severity, true
	}
	setting, ok := p.Configuration.Setting(rule.Name)
	if !ok || !setting.Configured {
		return rule.Severity, true
	}
	if setting.Severity == lintsdk.SeverityOff {
		return "", false
	}
	return lintSeverity(setting.Severity), true
}

func lintSeverity(severity lintsdk.Severity) string {
	if severity == lintsdk.SeverityHint {
		return "suggestion"
	}
	return string(severity)
}

// DefaultLintConfiguration is the configuration of a session that selected
// no lint configuration: every built-in rule at its default, no packs.
func DefaultLintConfiguration() *lintsdk.Configuration {
	registry, err := LintRegistry()
	if err != nil {
		panic(err)
	}
	configuration, problems := registry.Configure(lintsdk.Config{Version: lintsdk.ConfigVersion})
	if len(problems) > 0 {
		panic(fmt.Sprint(problems))
	}
	return configuration
}

type lintSuppression struct {
	revision   string
	rule       string
	targetLine int
	span       Span
	used       bool
}
type suppressionKey struct {
	revision string
	rule     string
	line     int
}

const (
	suppressionPrefix = "effra-lint-disable-next-line"
	suppressionCode   = "EFL004"
	suppressionRule   = "invalid-suppression"
)

func isSuppressionComment(comment Comment) bool {
	return !comment.Block && strings.HasPrefix(strings.TrimLeft(comment.Text, " \t"), suppressionPrefix)
}

func LintRules() []LintRule {
	return []LintRule{
		{"EFL001", "unused-recipe", "warning", "A local lazy effect is never referenced. Construction does not execute it. Bind to _ to acknowledge deliberate omission."},
		{"EFL002", "redundant-provision", "suggestion", "The receiver does not require the provided service. A stable provision boundary may be intentional."},
		{"EFL003", "unused-go-import", "suggestion", "No function of a Go import is referenced. The import still initializes its Go package; remove it only if that initialization is unneeded."},
		{"EFL004", suppressionRule, "error", "A next-line lint suppression must name a known rule, explain the exception, and suppress advice on the following source line."},
	}
}

// Built-in rules expressed over the public fact model. Their checks read
// resolved provision and import facts exactly as a rule pack would; codes,
// messages, spans and severities are unchanged. unused-recipe remains on the
// scoped walk below: its binding facts are unavailable when lexical facts
// exhaust their budget, and silently dropping that advice would change
// built-in behavior.
var (
	redundantProvisionRule = &lintsdk.Rule{
		Name:     "redundant-provision",
		Requires: []lintsdk.Family{lintsdk.FamilyDeclarations, lintsdk.FamilyProvisions},
		Check: func(pass *lintsdk.Pass) error {
			for _, provision := range pass.Snapshot.Provisions {
				if provision.Kind == lintsdk.ProvisionDirect && !slices.Contains(provision.Receiver, provision.Service) {
					pass.Reportf(provision.Span, "receiver does not require %s", pass.Snapshot.Service(provision.Service).Name)
				}
			}
			return nil
		},
	}
	unusedGoImportRule = &lintsdk.Rule{
		Name:     "unused-go-import",
		Requires: []lintsdk.Family{lintsdk.FamilyImports},
		Check: func(pass *lintsdk.Pass) error {
			for _, imported := range pass.Snapshot.Imports {
				if !imported.Used {
					pass.Reportf(imported.Span, "no function of Go import %s is referenced; the import still initializes its Go package, so remove it only if that initialization is unneeded", imported.Alias)
				}
			}
			return nil
		},
	}
)

// lintRunnerCode is the reserved lint-runner finding: a built-in rule that
// could not complete makes lint fail rather than look clean.
const lintRunnerCode = "EFL000"

// LintRegistry is the custom-lint rule registry: every built-in rule plus
// the explicitly selected pack manifests. Building or inspecting it never
// executes pack code.
func LintRegistry(packs ...lintsdk.Manifest) (*lintsdk.Registry, error) {
	var builtins []lintsdk.BuiltinRule
	for _, rule := range LintRules() {
		severity := lintsdk.Severity(rule.Severity)
		if rule.Severity == "suggestion" {
			severity = lintsdk.SeverityHint
		}
		builtin := lintsdk.BuiltinRule{Name: rule.Name, Code: rule.Code, Description: rule.Description, DefaultSeverity: severity, Fixed: rule.Name == suppressionRule}
		for _, checked := range []*lintsdk.Rule{redundantProvisionRule, unusedGoImportRule} {
			if checked.Name == rule.Name {
				builtin.Requires = checked.Requires
			}
		}
		builtins = append(builtins, builtin)
	}
	return lintsdk.NewRegistry(builtins, packs...)
}

func suppressionDiagnostic(message string, span Span) LintDiagnostic {
	return LintDiagnostic{Code: suppressionCode, Rule: suppressionRule, Severity: "error", Message: message, Span: span}
}

func parseSuppressions(comments []Comment, revision string, rules map[string]LintRule) ([]*lintSuppression, []LintDiagnostic) {
	var suppressions []*lintSuppression
	var diagnostics []LintDiagnostic
	for _, comment := range comments {
		body := comment.Text
		trimmed := strings.TrimLeft(body, " \t")
		if !isSuppressionComment(comment) {
			continue
		}
		span := comment.Span
		rest := trimmed[len(suppressionPrefix):]
		malformed := func(message string) {
			diagnostics = append(diagnostics, suppressionDiagnostic(message, span))
		}
		if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			malformed("malformed lint suppression: it must name exactly one lint rule and include a non-empty reason after ` -- `")
			continue
		}
		rest = strings.TrimSpace(rest)
		separator := strings.Index(rest, " -- ")
		if separator < 0 {
			if strings.TrimSpace(rest) != "" {
				malformed("malformed lint suppression: it must include a non-empty reason after ` -- `")
			} else {
				malformed("malformed lint suppression: it must name exactly one lint rule and include a non-empty reason after ` -- `")
			}
			continue
		}
		target := strings.TrimSpace(rest[:separator])
		reason := strings.TrimSpace(rest[separator+len(" -- "):])
		if target == "" || strings.ContainsAny(target, " \t,") {
			malformed("malformed lint suppression: it must name exactly one lint rule")
			continue
		}
		if reason == "" {
			malformed("malformed lint suppression: it must include a non-empty reason after ` -- `")
			continue
		}
		if _, ok := rules[target]; !ok {
			diagnostics = append(diagnostics, suppressionDiagnostic("unknown lint rule "+target, span))
			continue
		}
		suppressions = append(suppressions, &lintSuppression{revision: revision, rule: target, targetLine: span.Line + 1, span: span})
	}
	return suppressions, diagnostics
}

func suppressionFor(suppressions map[suppressionKey][]*lintSuppression, revision, rule string, line int) *lintSuppression {
	key := suppressionKey{revision: revision, rule: rule, line: line}
	var matched *lintSuppression
	for _, suppression := range suppressions[key] {
		if suppression.revision == revision {
			suppression.used = true
			matched = suppression
		}
	}
	return matched
}
func (r *Result) applyBuiltinRule(rule *lintsdk.Rule, snapshot *lintsdk.Snapshot) ([]lintsdk.Finding, error) {
	for _, family := range rule.Requires {
		if reason := snapshot.UnavailableReason(family); reason != "" {
			return nil, fmt.Errorf("%s facts unavailable: %s", family, reason)
		}
	}
	return lintsdk.Apply(rule, snapshot, lintsdk.Options{})
}

// Lint is built-in lint advice under the default configuration.
func (r *Result) Lint(strict bool) LintResult {
	return r.LintWith(strict, LintPacks{})
}

// LintWith merges built-in advice under the configuration with the reports
// of the selected rule packs. A pack failure, or a pack rule that failed or
// was skipped, is a reserved lint-runner error naming the pack: incomplete
// pack execution fails lint policy, since an enabled rule that did not run
// has not passed. An inapplicable skip is not: the rule, which only its
// pack's default or a preset enabled, does not apply to this target.
// Built-in findings are kept. Findings are ordered by source offset, with
// built-in rules first and packs in namespace order on equal offsets, never
// by pack completion order.
func (r *Result) LintWith(strict bool, packs LintPacks) LintResult {
	out := LintResult{SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target, Checked: r.Checked, LintPassed: r.Checked, Complete: r.Checked, Strict: strict, Diagnostics: r.Diagnostics, LintDiagnostics: []LintDiagnostic{}, Packs: []LintPackStatus{}}
	out.ProducerMetadata = r.producerMetadata
	count := func(severity string) {
		switch severity {
		case "error":
			out.Errors++
		case "warning":
			out.Warnings++
		case "information":
			out.Information++
		default:
			out.Suggestions++
		}
	}
	// Pack diagnostics join built-in advice only when the result is
	// finished: appended last and stably sorted by offset, they follow
	// built-in advice at an equal offset and keep their namespace order.
	var packDiagnostics []LintDiagnostic
	runnerError := func(to *[]LintDiagnostic, message string) {
		*to = append(*to, LintDiagnostic{Code: lintRunnerCode, Rule: "lint-runner", Severity: "error", Message: message})
		out.Errors++
		out.Complete = false
	}
	for _, pack := range packs.Reports {
		report := pack.Report
		out.Packs = append(out.Packs, LintPackStatus{Pack: pack.Pack, Identity: pack.Identity, Analysis: report.Analysis, Complete: report.Complete, Failure: report.Failure, Rules: report.Rules})
		if !report.Complete {
			out.Complete = false
		}
		if report.Failure != nil {
			runnerError(&packDiagnostics, fmt.Sprintf("rule pack %s (%s) failed: %s: %s", pack.Pack, pack.Identity, report.Failure.Code, report.Failure.Message))
			continue
		}
		for _, status := range report.Rules {
			switch status.Status {
			case lintsdk.StatusFailed:
				runnerError(&packDiagnostics, fmt.Sprintf("rule %s of pack %s (%s) failed: %s", status.Rule, pack.Pack, pack.Identity, status.Reason))
			case lintsdk.StatusSkipped:
				if status.Inapplicable {
					continue
				}
				runnerError(&packDiagnostics, fmt.Sprintf("rule %s of pack %s (%s) was skipped: %s", status.Rule, pack.Pack, pack.Identity, status.Reason))
			}
		}
		for _, finding := range report.Findings {
			diagnostic := LintDiagnostic{Code: finding.Rule, Rule: finding.Rule, Severity: lintSeverity(finding.Severity), Message: finding.Message, Span: lintSpan(finding.Span), Suggestions: finding.Suggestions}
			for _, related := range finding.Related {
				diagnostic.Related = append(diagnostic.Related, RelatedLocation{Message: related.Message, Span: lintSpan(related.Span)})
			}
			packDiagnostics = append(packDiagnostics, diagnostic)
			count(diagnostic.Severity)
		}
	}
	finish := func() LintResult {
		out.LintDiagnostics = append(out.LintDiagnostics, packDiagnostics...)
		slices.SortStableFunc(out.LintDiagnostics, func(a, b LintDiagnostic) int { return a.Span.Offset - b.Span.Offset })
		if out.Errors > 0 || strict && out.Warnings > 0 || !r.Checked {
			out.LintPassed = false
		}
		return out
	}
	if r.Program == nil {
		return finish()
	}
	rules := LintRules()
	rulesByName := make(map[string]LintRule, len(rules))
	for _, rule := range rules {
		if rule.Name != suppressionRule {
			rulesByName[rule.Name] = rule
		}
	}
	suppressions, suppressionDiagnostics := parseSuppressions(r.Program.Comments, r.Revision, rulesByName)
	out.LintDiagnostics = append(out.LintDiagnostics, suppressionDiagnostics...)
	out.Errors += len(suppressionDiagnostics)
	suppressionIndex := map[suppressionKey][]*lintSuppression{}
	for _, suppression := range suppressions {
		key := suppressionKey{revision: suppression.revision, rule: suppression.rule, line: suppression.targetLine}
		suppressionIndex[key] = append(suppressionIndex[key], suppression)
	}
	// A suppression of a rule that is off was not evaluated: only a rule
	// that ran can show that its suppression went unused.
	appendUnused := func() {
		for _, suppression := range suppressions {
			if _, enabled := packs.builtinSeverity(rulesByName[suppression.rule]); !suppression.used && enabled {
				out.LintDiagnostics = append(out.LintDiagnostics, suppressionDiagnostic("unused lint suppression for "+suppression.rule, suppression.span))
				out.Errors++
			}
		}
	}
	if !r.Checked {
		appendUnused()
		return finish()
	}
	add := func(index int, message string, span Span) {
		rule := rules[index]
		severity, enabled := packs.builtinSeverity(rule)
		if !enabled || suppressionFor(suppressionIndex, r.Revision, rule.Name, span.Line) != nil {
			return
		}
		out.LintDiagnostics = append(out.LintDiagnostics, LintDiagnostic{Code: rule.Code, Rule: rule.Name, Severity: severity, Message: message, Span: span})
		count(severity)
	}
	// Uses are the binders resolveBindings assigned to name expressions.
	used := map[*localBinding]bool{}
	var lazy []*Statement
	var block func(*Block)
	var expr func(*Expr)
	expr = func(e *Expr) {
		if e == nil {
			return
		}
		if e.Kind == "name" && e.binding != nil {
			used[e.binding] = true
		}
		forEachExprChild(e, expr)
		for _, arm := range e.Arms {
			block(arm.Body)
		}
		block(e.Then)
		block(e.Else)
	}
	block = func(b *Block) {
		if b == nil {
			return
		}
		for _, s := range b.Statements {
			expr(s.Value)
			expr(s.Payload)
			if s.Kind == "let" && s.Name != "_" && s.Value.Type.Effect {
				lazy = append(lazy, s)
			}
		}
	}
	for _, f := range r.Program.Functions {
		block(f.Body)
	}
	for _, p := range r.Program.Providers {
		for _, f := range p.Methods {
			block(f.Body)
		}
	}
	for _, s := range lazy {
		if !used[s.binding] {
			add(0, "lazy recipe "+s.Name+" is never referenced", s.Span)
		}
	}
	snapshot := r.LintFacts(lintsdk.FamilyDeclarations, lintsdk.FamilyProvisions, lintsdk.FamilyImports)
	for _, rule := range []*lintsdk.Rule{redundantProvisionRule, unusedGoImportRule} {
		index := slices.IndexFunc(rules, func(builtin LintRule) bool { return builtin.Name == rule.Name })
		if _, enabled := packs.builtinSeverity(rules[index]); !enabled {
			continue
		}
		findings, err := r.applyBuiltinRule(rule, snapshot)
		if err != nil {
			runnerError(&out.LintDiagnostics, "built-in lint rule "+rule.Name+" did not complete: "+err.Error())
			continue
		}
		for _, finding := range findings {
			add(index, finding.Message, lintSpan(finding.Span))
		}
	}
	appendUnused()
	return finish()
}

func lintSpan(span lintsdk.Span) Span {
	return Span{span.Offset, span.Length, span.Line, span.Column}
}
