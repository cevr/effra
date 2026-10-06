package compiler

import "fmt"

type ExpressionInfo struct {
	Kind                 string         `json:"kind"`
	Span                 Span           `json:"span"`
	Type                 ValueType      `json:"type"`
	ExecutedFailures     []string       `json:"executedFailures"`
	ExecutedRequirements []string       `json:"executedRequirements"`
	Evaluation           EvaluationRows `json:"evaluation"`
}

// TypeAt addresses an expression's diagnostic anchor, not a full source range.
// Unsupported/incomplete source never yields apparently authoritative types.
func (r *Result) TypeAt(offset int) (*ExpressionInfo, error) {
	if !r.Checked {
		return nil, fmt.Errorf("type queries require checked source")
	}
	if offset < 0 {
		return nil, fmt.Errorf("offset must be non-negative")
	}
	var found *Expr
	var block func(*Block)
	var expr func(*Expr)
	expr = func(e *Expr) {
		if e == nil {
			return
		}
		facts, hasFacts := r.facts[e]
		if hasFacts && facts.Type.Success != "" && offset >= e.Span.Offset && offset < e.Span.Offset+e.Span.Length && (found == nil || e.Span.Length < found.Span.Length) {
			found = e
		}
		forEachExprChild(e, expr)
		for _, arm := range e.Arms {
			block(arm.Body)
		}
		block(e.Then)
		block(e.Else)
	}
	block = func(b *Block) {
		if b != nil {
			for _, s := range b.Statements {
				expr(s.Value)
				expr(s.Payload)
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
	if found == nil {
		return nil, fmt.Errorf("no checked expression at byte offset %d; use an expression's diagnostic anchor", offset)
	}
	facts := r.facts[found]
	return &ExpressionInfo{Kind: found.Kind, Span: found.Span, Type: facts.Type, ExecutedFailures: append([]string{}, facts.Executed.Failures...), ExecutedRequirements: append([]string{}, facts.Executed.Requirements...), Evaluation: facts.Evaluation}, nil
}
