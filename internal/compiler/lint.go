package compiler

import "slices"

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
	Warnings        int              `json:"warnings"`
	Suggestions     int              `json:"suggestions"`
}

func LintRules() []LintRule {
	return []LintRule{
		{"EFL001", "unused-recipe", "warning", "A local lazy effect is never referenced. Construction does not execute it. Bind to _ to acknowledge deliberate omission."},
		{"EFL002", "redundant-provision", "suggestion", "The receiver does not require the provided service. A stable provision boundary may be intentional."},
		{"EFL003", "unused-go-import", "suggestion", "No admitted foreign call uses this package."},
	}
}
func (r *Result) Lint(strict bool) LintResult {
	out := LintResult{SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target, Checked: r.Checked, LintPassed: r.Checked, Strict: strict, Diagnostics: r.Diagnostics, LintDiagnostics: []LintDiagnostic{}}
	if !r.Checked {
		return out
	}
	add := func(index int, message string, span Span) {
		rule := LintRules()[index]
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
		for _, field := range e.Fields {
			expr(field.Value, env)
		}
		for _, arm := range e.Arms {
			branch := map[string]*binding{}
			for name, local := range env {
				branch[name] = local
			}
			for _, name := range arm.Pattern.Bindings {
				if name != "_" {
					branch[name] = &binding{used: false}
				}
			}
			block(arm.Body, branch)
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
			expr(s.Payload, env)
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
	slices.SortStableFunc(out.LintDiagnostics, func(a, b LintDiagnostic) int { return a.Span.Offset - b.Span.Offset })
	if strict && out.Warnings > 0 {
		out.LintPassed = false
	}
	return out
}
