package compiler

import "fmt"

// These facts describe original parser nodes, independently of checker
// lowering. Diagnostic anchors and full extents are deliberately separate.
type sourceSyntaxFact struct {
	Kind, Name               string
	Anchor, Extent, NameSpan Span
	Children                 []int
}

type lexicalBinding struct {
	ID, Kind, Name   string
	NameSpan, Extent Span
	Owner            int
	Checked          checkedExpression
}

const maxLexicalFacts = 100000

type lexicalFacts struct {
	syntax      []sourceSyntaxFact
	expressions map[*Expr]int
	functions   map[*Function]int
	parameters  map[int]int
	statements  map[*Statement]int
	patterns    map[*MatchPattern]int
	bindings    map[string]lexicalBinding
	uses        map[*Expr]string
	complete    bool
}

// captureOriginalSyntax runs before imports/checking can rewrite source
// children. A refusal affects only tooling; it never changes source admission.
func captureOriginalSyntax(program *Program) *lexicalFacts {
	facts := &lexicalFacts{expressions: map[*Expr]int{}, functions: map[*Function]int{}, parameters: map[int]int{}, statements: map[*Statement]int{}, patterns: map[*MatchPattern]int{}, bindings: map[string]lexicalBinding{}, uses: map[*Expr]string{}, complete: true}
	add := func(kind, name string, anchor, extent, nameSpan Span) int {
		if len(facts.syntax) >= maxLexicalFacts {
			facts.complete = false
			return -1
		}
		facts.syntax = append(facts.syntax, sourceSyntaxFact{Kind: kind, Name: name, Anchor: anchor, Extent: extent, NameSpan: nameSpan})
		return len(facts.syntax) - 1
	}
	child := func(parent, node int) {
		if parent >= 0 && node >= 0 {
			facts.syntax[parent].Children = append(facts.syntax[parent].Children, node)
		}
	}
	var expression func(*Expr) int
	var block func(*Block) int
	expression = func(e *Expr) int {
		if e == nil || !facts.complete {
			return -1
		}
		if id, ok := facts.expressions[e]; ok {
			return id
		}
		nameSpan := Span{}
		if e.Kind == "name" || e.Kind == "member" {
			nameSpan = e.Span
		}
		id := add(e.Kind, e.Name, e.Span, e.Extent, nameSpan)
		facts.expressions[e] = id
		forEachExprChild(e, func(e *Expr) { child(id, expression(e)) })
		child(id, block(e.Then))
		child(id, block(e.Else))
		for _, arm := range e.Arms {
			if !facts.complete {
				break
			}
			armID := add("matchArm", "", arm.Span, arm.Extent, Span{})
			child(id, armID)
			if p := arm.Pattern; p != nil {
				patternID := add("pattern", p.TypeName+"."+p.VariantName, p.Span, p.Extent, Span{})
				facts.patterns[p] = patternID
				child(armID, patternID)
				for _, name := range p.Names {
					if !facts.complete {
						break
					}
					extent := name.FieldSpan
					extent.Length = name.NameSpan.Offset + name.NameSpan.Length - extent.Offset
					child(patternID, add("patternBinding", name.Name, name.FieldSpan, extent, name.NameSpan))
				}
			}
			child(armID, block(arm.Body))
		}
		return id
	}
	block = func(b *Block) int {
		if b == nil || !facts.complete {
			return -1
		}
		id := add("block", "", b.Extent, b.Extent, Span{})
		for _, s := range b.Statements {
			if !facts.complete {
				break
			}
			sid := add(s.Kind, s.Name, s.Span, s.Extent, s.NameSpan)
			facts.statements[s] = sid
			child(id, sid)
			child(sid, expression(s.Value))
			child(sid, expression(s.Payload))
		}
		return id
	}
	parameter := func(parent int, p Param) {
		id := add("parameter", p.Name, p.Span, p.Extent, p.Span)
		facts.parameters[p.Span.Offset] = id
		child(parent, id)
	}
	function := func(f *Function) int {
		id := add("function", f.Name, f.Span, f.Extent, f.Span)
		facts.functions[f] = id
		for _, p := range f.Params {
			parameter(id, p)
		}
		child(id, block(f.Body))
		return id
	}
	for _, item := range program.Items {
		id := add("declaration:"+item.Kind, "", item.Span, item.Extent, Span{})
		if item.Function != nil {
			child(id, function(item.Function))
		}
		if p := item.Provider; p != nil {
			for _, param := range p.Params {
				parameter(id, param)
			}
			for _, f := range p.Methods {
				child(id, function(f))
			}
		}
		if s := item.Service; s != nil {
			for _, f := range s.Methods {
				child(id, function(f))
			}
		}
		if layer := item.Layer; layer != nil {
			for _, entry := range layer.Entries {
				child(id, expression(entry.Value))
			}
		}
		if !facts.complete {
			break
		}
	}
	return facts
}

// bindLocal records the very entry inserted into the checker's environment.
// Its ID is observational metadata and never participates in type/admission
// relations. Original declaration offsets make repeated checking idempotent.
func (c *checker) bindLocal(kind, name string, span, extent Span, owner int, value checkedExpression) checkedExpression {
	value.lexicalBinding = ""
	if c.lexicalOwner == nil || c.result.lexical == nil || !c.result.lexical.complete || span.Length == 0 {
		return value
	}
	id := fmt.Sprintf("binding:%s:%d", kind, span.Offset)
	value.lexicalBinding = id
	c.result.lexical.bindings[id] = lexicalBinding{ID: id, Kind: kind, Name: name, NameSpan: span, Extent: extent, Owner: owner, Checked: value.clone()}
	return value
}

func (c *checker) observeLocalUse(e *Expr, value checkedExpression) {
	if c.lexicalOwner == nil || c.result.lexical == nil || value.lexicalBinding == "" {
		return
	}
	if _, original := c.result.lexical.expressions[e]; original {
		c.result.lexical.uses[e] = value.lexicalBinding
	}
}

func (p *MatchPattern) bindingSpan(field string) Span {
	for _, name := range p.Names {
		if name.Field == field {
			return name.NameSpan
		}
	}
	return Span{}
}
