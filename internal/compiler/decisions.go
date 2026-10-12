package compiler

// Decision syntax: the predeclared generic type `decision<S, O>` and the five
// forms `enter e`, `stay e`, `ignore`, `reject` and `done e` (bare `done` when
// O is void). The forms are typed expressions and, with the same words,
// patterns. A machine's step and complete return a decision; the machine plan
// reads decision forms structurally, never through the identity of a library
// declaration (docs/specs/state-machines.md#decisions).
//
// Source `enter` is the plan transition kind "go": the source word names the
// lifecycle (close the current entry, start a new one) and avoids Go's
// goroutine statement; the plan protocol keeps its established word.

// decisionCode refuses a decision form outside a decision context and a
// decision written through the template rather than its forms.
const decisionCode = "EF143"

const (
	decisionTypeName     = "decision"
	decisionModule       = "effra/language"
	decisionIdentity     = "template:" + decisionModule + ":predeclared:" + decisionTypeName
	decisionSourceID     = "language:decision"
	decisionEmissionName = "predeclared_decision"
)

// decisionWords maps each reserved decision word to the payload field its
// form initializes; ignore and reject carry none.
var decisionWords = map[string]string{"enter": "next", "stay": "next", "ignore": "", "reject": "", "done": "output"}

func isDecisionWord(text string) bool {
	_, ok := decisionWords[text]
	return ok
}

// decisionForms is the spelling every refusal names.
const decisionForms = "`enter e`, `stay e`, `ignore`, `reject` and `done e`"

func reservedDecisionWordMessage(word string) string {
	form := word
	if decisionWords[word] != "" {
		form += " value"
	}
	return word + " is reserved for the decision form `" + form + "`; choose another name"
}

// decision parses a decision form after its word. enter and stay always take
// an operand. done takes one only when an expression starts on the same line,
// so a bare `done` can end a match arm or a block.
func (p *parser) decision(word token) *Expr {
	e := &Expr{Kind: "decision", Name: word.text, Span: word.span}
	field := decisionWords[word.text]
	if field == "" {
		return e
	}
	if word.text == "done" && !p.decisionOperandAhead(word) {
		return e
	}
	operand := p.expr(prefixPrecedence)
	e.Fields = []FieldValue{{Name: field, Value: operand, Span: operand.Span}}
	return e
}

func (p *parser) decisionOperandAhead(word token) bool {
	next := p.peek()
	if next.span.Line != word.span.Line {
		return false
	}
	switch next.kind {
	case "integer", "string":
		return true
	case "name":
		return next.text != "else"
	}
	return next.text == "(" || next.text == "-"
}

// decisionPattern parses `enter next`, `stay next`, `ignore`, `reject`,
// `done output` and bare `done`. A binder names the payload; `_` discards it.
func (p *parser) decisionPattern(word token) *MatchPattern {
	pattern := &MatchPattern{TypeName: decisionTypeName, VariantName: word.text, Bindings: map[string]string{}, Span: word.span, decision: true}
	field := decisionWords[word.text]
	if next := p.peek(); next.kind == "name" && next.span.Line == word.span.Line && !isDecisionWord(next.text) {
		binder := p.name()
		if field == "" {
			p.fail(binder, word.text+" carries no payload to bind")
		}
		pattern.Bindings[field] = binder.text
		pattern.Names = append(pattern.Names, PatternName{Field: field, Name: binder.text, FieldSpan: binder.span, NameSpan: binder.span})
	}
	pattern.Extent = p.extent(word.span)
	return pattern
}

// regionUpdateAhead reports `field <- decision` inside constructor braces.
// Region bindings are not admitted yet, so the form is refused rather than
// read as the comparison `field < -decision`.
func (p *parser) regionUpdateAhead() bool {
	if p.at+1 >= len(p.tokens) {
		return false
	}
	less, minus := p.tokens[p.at], p.tokens[p.at+1]
	return less.text == "<" && minus.text == "-" && less.span.Offset+less.span.Length == minus.span.Offset
}

// newDecisionTemplate builds the predeclared closed enum. Its variants are
// the decision words, which no source declaration can spell.
func newDecisionTemplate() *Record {
	field := func(name, typ string) []Field { return []Field{{Name: name, Type: typ}} }
	return &Record{
		Kind:         "enum",
		Name:         decisionTypeName,
		Module:       decisionModule,
		SourceID:     decisionSourceID,
		Identity:     decisionIdentity,
		EmissionName: decisionEmissionName,
		Parameters:   []TemplateParameter{{Name: "S", Kind: "type"}, {Name: "O", Kind: "type"}},
		Variants: []Variant{
			{Name: "enter", Fields: field("next", "S")},
			{Name: "stay", Fields: field("next", "S")},
			{Name: "ignore"},
			{Name: "reject"},
			{Name: "done", Fields: field("output", "O")},
		},
	}
}

// addPredeclaredDecision makes `decision<S, O>` available when the program
// spells it. A source declaration named decision shadows the predeclared
// type, as a local declaration shadows Go's predeclared identifiers.
func addPredeclaredDecision(program *Program) {
	if program.decisionTemplate != nil {
		return
	}
	for _, data := range append(append([]*DataDeclaration{}, program.Records...), program.Enums...) {
		if data.Name == decisionTypeName {
			return
		}
	}
	for _, typ := range program.typeExpressions {
		if typ != nil && typ.Application == decisionTypeName {
			program.decisionTemplate = newDecisionTemplate()
			return
		}
	}
}

// compilerTemplates are the templates the checked file did not declare:
// bundled imports and the predeclared decision type.
func (p *Program) compilerTemplates() []*Record {
	if p.decisionTemplate == nil {
		return p.BundledTemplates
	}
	return append(append([]*Record{}, p.BundledTemplates...), p.decisionTemplate)
}

// isDecisionTemplate reports the predeclared decision template.
func (c *checker) isDecisionTemplate(r *Record) bool {
	return r != nil && r == c.program.decisionTemplate
}

// decisionApplication returns the decision type expected at a form: the
// declared result of the enclosing function, when it is decision<S, O>.
func (c *checker) decisionApplication() (TypeID, *semanticTypeNode, bool) {
	f, template := c.reportingFunction, c.program.decisionTemplate
	if f == nil || template == nil {
		return invalidTypeID, nil, false
	}
	n := c.node(f.returnID)
	if n == nil || n.Kind != "application" || n.Declaration != template.Identity || len(n.Args) != 2 {
		return invalidTypeID, nil, false
	}
	return f.returnID, n, true
}

// decisionForm checks one decision form against the enclosing function's
// declared decision type. The payload is a pure value, as a constructor
// payload is, and the result is that decision type's variant.
func (c *checker) decisionForm(e *Expr, env localEnv) checkedExpression {
	id, node, ok := c.decisionApplication()
	if !ok {
		for _, field := range e.Fields {
			c.expr(field.Value, env, false)
		}
		c.diagnostic(decisionCode, "`"+e.Name+"` is a decision form; it needs an enclosing function that returns decision<S, O>", e.Span)
		return c.checkedData("invalid")
	}
	template := c.program.decisionTemplate
	variant := template.Variants[decisionVariantIndex(template, e.Name)]
	declared, ok := c.instantiateDataFields(template, variant.Fields, node.Args)
	if !ok {
		c.diagnostic("EF127", "decision payload substitution unavailable", e.Span)
		return c.checkedData("invalid")
	}
	result := c.checkedData("invalid")
	fields := map[string]checkedExpression{}
	if len(declared) == 1 {
		formal := declared[0]
		var value checkedExpression
		if len(e.Fields) == 0 {
			// Bare done: the output is the void value.
			value = c.checkedData(voidTypeName)
			if !c.assignable(value.valueID(), formal.typeID, 0) {
				c.diagnostic("EF106", "bare `done` needs a void output; write `done value` with a "+c.displayTypeID(formal.typeID), e.Span)
				return result
			}
		} else {
			field := e.Fields[0]
			value = c.expr(field.Value, env, false)
			if c.poisoned(value) {
				return result
			}
			if value.isEffect() {
				c.diagnostic("EF106", "`"+e.Name+"` takes a value; execute an Effect with run first", field.Value.Span)
				return result
			}
			if !c.assignable(value.valueID(), formal.typeID, 0) {
				c.diagnostic("EF106", "`"+e.Name+"` needs a "+c.displayTypeID(formal.typeID)+"; found "+c.displayChecked(value), field.Value.Span)
				return result
			}
		}
		value.value = c.values.occurrence(formal.typeID, value.ownershipFacts(), value.captureFacts())
		fields[formal.Name] = value
	}
	owners, captures := []OwnershipFact{}, []OwnershipFact{}
	evaluation := c.evaluation(emptyRowID, emptyRowID)
	for _, field := range declared {
		value := fields[field.Name]
		owners = append(owners, prependFacts(field.Name, heldFacts(value, 0))...)
		if n := value.node(); n == nil || n.Kind != "recipe" {
			captures = append(captures, prependFacts(field.Name, value.captureFacts())...)
		}
		evaluation = c.unionEvaluationFacts(evaluation, value.executed)
	}
	result = c.checkedDataID(id, normalizeFacts(owners), normalizeFacts(captures))
	result.setOwnership(prependFacts(e.Name, result.ownershipFacts()))
	result.setCaptures(prependFacts(e.Name, result.captureFacts()))
	payload := c.checkedData(voidTypeName)
	payload.fields = fields
	result.fields = map[string]checkedExpression{e.Name: payload}
	result.evaluation = evaluation
	e.ResolvedTemplate = template
	return result
}

func decisionVariantIndex(template *Record, word string) int {
	for i, variant := range template.Variants {
		if variant.Name == word {
			return i
		}
	}
	return -1
}

// variantLabel spells a variant the way source writes it: a decision variant
// is its bare word, any other is Owner.Variant.
func variantLabel(owner *Enum, variant string) string {
	if owner != nil && owner.Identity == decisionIdentity {
		return variant
	}
	return owner.Name + "." + variant
}

// decisionFields returns the payload fields a decision form initializes,
// supplying bare done's void output for the emitters.
func decisionFields(e *Expr) []FieldValue {
	if e.Name == "done" && len(e.Fields) == 0 {
		return []FieldValue{{Name: "output", Value: &Expr{Kind: "void", Span: e.Span}, Span: e.Span}}
	}
	return e.Fields
}

// constructVariant is the variant a generic enum construction selects.
func constructVariant(e *Expr) string {
	if e.Kind == "decision" {
		return e.Name
	}
	if e.Left == nil {
		return ""
	}
	return e.Left.Name
}

// removedMachinesModule held the bundled Step template before decision syntax.
const removedMachinesModule = "effra/machines"

// stepTemplateMigration is the help for the removed bundled Step template.
const stepTemplateMigration = "the bundled effra/machines module and its Step template are removed; declare the result as decision<S, O> and write " + decisionForms + " (Go -> enter, Stay -> stay, Ignore -> ignore, Reject -> reject, Done -> done)"
