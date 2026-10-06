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
		if !facts.complete || len(facts.syntax) >= maxLexicalFacts {
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
		if id < 0 {
			return -1
		}
		facts.expressions[e] = id
		forEachExprChildUntil(e, func(childExpr *Expr) bool {
			child(id, expression(childExpr))
			return facts.complete
		})
		if !facts.complete {
			return id
		}
		child(id, block(e.Then))
		if !facts.complete {
			return id
		}
		child(id, block(e.Else))
		for _, arm := range e.Arms {
			if !facts.complete {
				break
			}
			armID := add("matchArm", "", arm.Span, arm.Extent, Span{})
			if armID < 0 {
				break
			}
			child(id, armID)
			if p := arm.Pattern; p != nil {
				if !facts.complete {
					break
				}
				patternID := add("pattern", p.TypeName+"."+p.VariantName, p.Span, p.Extent, Span{})
				if patternID < 0 {
					break
				}
				facts.patterns[p] = patternID
				child(armID, patternID)
				for _, name := range p.Names {
					if !facts.complete {
						break
					}
					extent := name.FieldSpan
					extent.Length = name.NameSpan.Offset + name.NameSpan.Length - extent.Offset
					nameID := add("patternBinding", name.Name, name.FieldSpan, extent, name.NameSpan)
					if nameID < 0 {
						break
					}
					child(patternID, nameID)
				}
			}
			if !facts.complete {
				break
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
		if id < 0 {
			return -1
		}
		for _, s := range b.Statements {
			if !facts.complete {
				break
			}
			sid := add(s.Kind, s.Name, s.Span, s.Extent, s.NameSpan)
			if sid < 0 {
				break
			}
			facts.statements[s] = sid
			child(id, sid)
			child(sid, expression(s.Value))
			if !facts.complete {
				break
			}
			child(sid, expression(s.Payload))
		}
		return id
	}
	parameter := func(parent int, p Param) bool {
		if !facts.complete {
			return false
		}
		id := add("parameter", p.Name, p.Span, p.Extent, p.Span)
		if id < 0 {
			return false
		}
		facts.parameters[p.Span.Offset] = id
		child(parent, id)
		return facts.complete
	}
	function := func(f *Function) int {
		if f == nil || !facts.complete {
			return -1
		}
		id := add("function", f.Name, f.Span, f.Extent, f.Span)
		if id < 0 {
			return -1
		}
		facts.functions[f] = id
		for _, p := range f.Params {
			if !parameter(id, p) {
				return id
			}
		}
		if facts.complete {
			child(id, block(f.Body))
		}
		return id
	}
	for _, item := range program.Items {
		if !facts.complete {
			break
		}
		id := add("declaration:"+item.Kind, "", item.Span, item.Extent, Span{})
		if id < 0 {
			break
		}
		if item.Function != nil {
			child(id, function(item.Function))
		}
		if !facts.complete {
			break
		}
		if p := item.Provider; p != nil {
			for _, param := range p.Params {
				if !parameter(id, param) {
					break
				}
			}
			if !facts.complete {
				break
			}
			for _, f := range p.Methods {
				if !facts.complete {
					break
				}
				child(id, function(f))
			}
		}
		if !facts.complete {
			break
		}
		if s := item.Service; s != nil {
			for _, f := range s.Methods {
				if !facts.complete {
					break
				}
				child(id, function(f))
			}
		}
		if !facts.complete {
			break
		}
		if layer := item.Layer; layer != nil {
			for _, entry := range layer.Entries {
				if !facts.complete {
					break
				}
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
