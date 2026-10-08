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
	Code     string `json:"code"`
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Span     Span   `json:"span"`
}
type LintResult struct {
	ProducerMetadata
	SchemaVersion   int              `json:"schemaVersion"`
	Revision        string           `json:"revision"`
	Target          string           `json:"target"`
	Checked         bool             `json:"checked"`
	LintPassed      bool             `json:"lintPassed"`
	Strict          bool             `json:"strict"`
	Diagnostics     []Diagnostic     `json:"diagnostics"`
	LintDiagnostics []LintDiagnostic `json:"lintDiagnostics"`
	Errors          int              `json:"errors"`
	Warnings        int              `json:"warnings"`
	Suggestions     int              `json:"suggestions"`
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

func (r *Result) Lint(strict bool) LintResult {
	out := LintResult{SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target, Checked: r.Checked, LintPassed: r.Checked, Strict: strict, Diagnostics: r.Diagnostics, LintDiagnostics: []LintDiagnostic{}}
	out.ProducerMetadata = r.producerMetadata
	if r.Program == nil {
		return out
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
	out.Errors = len(suppressionDiagnostics)
	suppressionIndex := map[suppressionKey][]*lintSuppression{}
	for _, suppression := range suppressions {
		key := suppressionKey{revision: suppression.revision, rule: suppression.rule, line: suppression.targetLine}
		suppressionIndex[key] = append(suppressionIndex[key], suppression)
	}
	appendUnused := func() {
		for _, suppression := range suppressions {
			if !suppression.used {
				out.LintDiagnostics = append(out.LintDiagnostics, suppressionDiagnostic("unused lint suppression for "+suppression.rule, suppression.span))
				out.Errors++
			}
		}
	}
	if !r.Checked {
		appendUnused()
		slices.SortStableFunc(out.LintDiagnostics, func(a, b LintDiagnostic) int { return a.Span.Offset - b.Span.Offset })
		return out
	}
	add := func(index int, message string, span Span) {
		rule := rules[index]
		if suppressionFor(suppressionIndex, r.Revision, rule.Name, span.Line) != nil {
			return
		}
		out.LintDiagnostics = append(out.LintDiagnostics, LintDiagnostic{rule.Code, rule.Name, rule.Severity, message, span})
		if rule.Severity == "warning" {
			out.Warnings++
		} else {
			out.Suggestions++
		}
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
		findings, err := r.applyBuiltinRule(rule, snapshot)
		if err != nil {
			out.LintDiagnostics = append(out.LintDiagnostics, LintDiagnostic{Code: lintRunnerCode, Rule: "lint-runner", Severity: "error", Message: "built-in lint rule " + rule.Name + " did not complete: " + err.Error()})
			out.Errors++
			continue
		}
		for _, finding := range findings {
			add(index, finding.Message, Span{finding.Span.Offset, finding.Span.Length, finding.Span.Line, finding.Span.Column})
		}
	}
	appendUnused()
	slices.SortStableFunc(out.LintDiagnostics, func(a, b LintDiagnostic) int { return a.Span.Offset - b.Span.Offset })
	if out.Errors > 0 || strict && out.Warnings > 0 {
		out.LintPassed = false
	}
	return out
}
