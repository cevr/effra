package compiler

import (
	"slices"
	"strings"
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

func LintRules() []LintRule {
	return []LintRule{
		{"EFL001", "unused-recipe", "warning", "A local lazy effect is never referenced. Construction does not execute it. Bind to _ to acknowledge deliberate omission."},
		{"EFL002", "redundant-provision", "suggestion", "The receiver does not require the provided service. A stable provision boundary may be intentional."},
		{"EFL003", "unused-go-import", "suggestion", "No admitted foreign call uses this package."},
		{"EFL004", suppressionRule, "error", "A next-line lint suppression must name a known rule, explain the exception, and suppress advice on the following source line."},
	}
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
		if !strings.HasPrefix(trimmed, suppressionPrefix) {
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
func (r *Result) Lint(strict bool) LintResult {
	out := LintResult{SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target, Checked: r.Checked, LintPassed: r.Checked, Strict: strict, Diagnostics: r.Diagnostics, LintDiagnostics: []LintDiagnostic{}}
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
	type binding struct {
		statement *Statement
		used      bool
	}
	var locals []*binding
	var block func(*Block, map[string]*binding)
	var expr func(*Expr, map[string]*binding)
	expr = func(e *Expr, env map[string]*binding) {
		if e == nil {
			return
		}
		if e.Kind == "name" && env[e.Name] != nil {
			env[e.Name].used = true
		}
		if e.Kind == "provide" && !slices.Contains(e.Left.Type.Services, e.Name) {
			add(1, "receiver does not require "+e.Name, e.Span)
		}
		expr(e.Left, env)
		expr(e.Right, env)
		for _, a := range e.Args {
			expr(a, env)
		}
		block(e.Then, env)
		block(e.Else, env)
	}
	block = func(b *Block, outer map[string]*binding) {
		if b == nil {
			return
		}
		env := map[string]*binding{}
		for name, local := range outer {
			env[name] = local
		}
		for _, s := range b.Statements {
			expr(s.Value, env)
			if s.Kind == "let" {
				local := &binding{statement: s}
				env[s.Name] = local
				if s.Name != "_" && s.Value.Type.Effect {
					locals = append(locals, local)
				}
			}
		}
	}
	for _, f := range r.Program.Functions {
		block(f.Body, nil)
	}
	for _, p := range r.Program.Providers {
		for _, f := range p.Methods {
			block(f.Body, nil)
		}
	}
	for _, local := range locals {
		if !local.used {
			add(0, "lazy recipe "+local.statement.Name+" is never referenced", local.statement.Span)
		}
	}
	for _, imp := range r.Program.Imports {
		if !r.Program.UsedImports[imp.Alias] {
			add(2, "unused Go import "+imp.Alias, imp.Span)
		}
	}
	appendUnused()
	slices.SortStableFunc(out.LintDiagnostics, func(a, b LintDiagnostic) int { return a.Span.Offset - b.Span.Offset })
	if out.Errors > 0 || strict && out.Warnings > 0 {
		out.LintPassed = false
	}
	return out
}
