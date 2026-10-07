package compiler

import (
	"fmt"
	"slices"
)

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

// maxLexicalFacts bounds original syntax facts and, separately, the name
// facts (declaration and reference tokens) that point into them.
const maxLexicalFacts = 100000

// lexicalTarget is the declaration denoted by one source name token. It keeps
// the checker's own declaration pointers; nothing is re-resolved by name.
// Variant and field narrow an owning data or error declaration.
type lexicalTarget struct {
	kind     string
	function *Function
	service  *Service
	provider *Provider
	data     *DataDeclaration
	failure  *ErrorDecl
	layer    *Layer
	module   *SyntaxItem
	variant  string
	field    string
}

// lexicalName pairs one original token with the declaration it denotes.
type lexicalName struct {
	Span   Span
	Target lexicalTarget
}

type lexicalFacts struct {
	syntax      []sourceSyntaxFact
	expressions map[*Expr]int
	functions   map[*Function]int
	parameters  map[int]int
	statements  map[*Statement]int
	patterns    map[*MatchPattern]int
	bindings    map[string]lexicalBinding
	uses        map[*Expr]string
	// items maps each original top-level declaration to its syntax item.
	// Declarations absent here (builtins, bundled modules) have no location
	// in this snapshot's text.
	items map[any]*SyntaxItem
	// aliases are the original import declarations, keyed by their alias.
	aliases map[string]*SyntaxItem
	// declarations are original declaration name tokens; references are the
	// tokens whose resolution the checker performed, keyed by token offset.
	declarations []lexicalName
	references   map[int]lexicalName
	complete     bool
}

// captureOriginalSyntax runs before imports/checking can rewrite source
// children. A refusal affects only tooling; it never changes source admission.
func captureOriginalSyntax(program *Program) *lexicalFacts {
	facts := &lexicalFacts{expressions: map[*Expr]int{}, functions: map[*Function]int{}, parameters: map[int]int{}, statements: map[*Statement]int{}, patterns: map[*MatchPattern]int{}, bindings: map[string]lexicalBinding{}, uses: map[*Expr]string{}, items: map[any]*SyntaxItem{}, aliases: map[string]*SyntaxItem{}, references: map[int]lexicalName{}, complete: true}
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
		facts.declare(item)
	}
	return facts
}

// declare records the name tokens an original item introduces. The item
// pointers are the parser's own nodes, which the checker registers unchanged.
func (facts *lexicalFacts) declare(item *SyntaxItem) {
	name := func(span Span, target lexicalTarget) {
		if !facts.complete || len(facts.declarations)+len(facts.references) >= maxLexicalFacts {
			facts.complete = false
			return
		}
		facts.declarations = append(facts.declarations, lexicalName{Span: span, Target: target})
	}
	fields := func(declared []Field, target lexicalTarget) {
		for _, field := range declared {
			target.field = field.Name
			name(field.Span, target)
		}
	}
	functions := func(owner lexicalTarget, methods []*Function) {
		for _, f := range methods {
			owner.function = f
			name(f.Span, owner)
		}
	}
	switch {
	case item.BundledImport != nil:
		facts.aliases[item.BundledImport.Alias] = item
		name(item.BundledImport.Span, lexicalTarget{kind: "module", module: item})
	case item.Import != nil:
		facts.aliases[item.Import.Alias] = item
		name(item.Import.Span, lexicalTarget{kind: "hostModule", module: item})
	case item.Error != nil:
		facts.items[item.Error] = item
		name(item.Error.Span, lexicalTarget{kind: "error", failure: item.Error})
		fields(item.Error.Fields, lexicalTarget{kind: "field", failure: item.Error})
	case item.Record != nil || item.Enum != nil:
		data := item.Record
		if data == nil {
			data = item.Enum
		}
		facts.items[data] = item
		name(data.Span, lexicalTarget{kind: data.Kind, data: data})
		fields(data.Fields, lexicalTarget{kind: "field", data: data})
		for _, variant := range data.Variants {
			name(variant.Span, lexicalTarget{kind: "variant", data: data, variant: variant.Name})
			fields(variant.Fields, lexicalTarget{kind: "field", data: data, variant: variant.Name})
		}
	case item.Service != nil:
		facts.items[item.Service] = item
		name(item.Service.Span, lexicalTarget{kind: "service", service: item.Service})
		functions(lexicalTarget{kind: "operation", service: item.Service}, item.Service.Methods)
	case item.Provider != nil:
		facts.items[item.Provider] = item
		name(item.Provider.Span, lexicalTarget{kind: "provider", provider: item.Provider})
		functions(lexicalTarget{kind: "method", provider: item.Provider}, item.Provider.Methods)
	case item.Function != nil:
		facts.items[item.Function] = item
		functions(lexicalTarget{kind: "function"}, []*Function{item.Function})
	case item.Layer != nil:
		facts.items[item.Layer] = item
		name(item.Layer.Span, lexicalTarget{kind: "layer", layer: item.Layer})
	}
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

// bindSignatureParameters records the parameters of an original bodyless
// declaration, a service operation, from its checked signature. No body
// environment binds them, so they have declaration facts and no uses.
func (c *checker) bindSignatureParameters(f *Function) {
	facts := c.result.lexical
	if facts == nil {
		return
	}
	if _, original := facts.functions[f]; !original {
		return
	}
	previous := c.lexicalOwner
	c.lexicalOwner = f
	defer func() { c.lexicalOwner = previous }()
	for _, p := range f.Params {
		c.bindLocal("parameter", p.Name, p.Span, p.Extent, facts.parameters[p.Span.Offset], c.checkedDataID(p.typeID, nil, nil))
	}
}

func (c *checker) observeLocalUse(e *Expr, value checkedExpression) {
	if c.lexicalOwner == nil || c.result.lexical == nil || value.lexicalBinding == "" {
		return
	}
	if _, original := c.result.lexical.expressions[e]; original {
		c.result.lexical.uses[e] = value.lexicalBinding
	}
}

// observeReference records the declaration one checker resolution selected
// for an original token. Bundled bodies and checker-synthesized nodes are not
// original syntax, so they never publish references into this snapshot.
func (c *checker) observeReference(node any, span Span, target lexicalTarget) {
	facts := c.result.lexical
	if facts == nil || !facts.complete || span.Length == 0 {
		return
	}
	// Bodies may be checked more than once; only the recording pass, which
	// also publishes expression facts, publishes their references.
	original := false
	switch n := node.(type) {
	case *Expr:
		_, original = facts.expressions[n]
		original = original && c.recordFacts
	case *Statement:
		_, original = facts.statements[n]
		original = original && c.recordFacts
	case *MatchPattern:
		_, original = facts.patterns[n]
		original = original && c.recordFacts
	case *Provider:
		original = facts.items[n] != nil
	}
	if !original {
		return
	}
	if _, exists := facts.references[span.Offset]; !exists && len(facts.declarations)+len(facts.references) >= maxLexicalFacts {
		facts.complete = false
		return
	}
	facts.references[span.Offset] = lexicalName{Span: span, Target: target}
}

// observeFieldLabels records explicit `name:` labels against the declared
// fields of the owner the checker selected for this payload.
func (c *checker) observeFieldLabels(node any, values []FieldValue, declared []Field, owner lexicalTarget) {
	owner.kind = "field"
	for _, value := range values {
		if value.Label.Length == 0 || !slices.ContainsFunc(declared, func(f Field) bool { return f.Name == value.Name }) {
			continue
		}
		owner.field = value.Name
		c.observeReference(node, value.Label, owner)
	}
}

// observeModuleAlias records an import alias qualifier resolved by the checker.
func (c *checker) observeModuleAlias(node any, qualifier *Expr) {
	if qualifier == nil || qualifier.Kind != "name" || c.result.lexical == nil {
		return
	}
	if item := c.result.lexical.aliases[qualifier.Name]; item != nil {
		c.observeReference(node, qualifier.Span, lexicalTarget{kind: "module", module: item})
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
