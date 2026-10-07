package compiler

import (
	"maps"
	"slices"
	"strings"
)

// absentSyntaxCode is the one stable code for source written in a construct
// Effra does not have. Each catalog entry contributes the construct-specific
// message and help, so tools key on one code while authors read what Effra
// uses instead.
const absentSyntaxCode = "EF003"

// A construct is absent when Effra deliberately leaves it out: a library,
// another construct or a diagnostic already covers the need. A planned
// construct is specified by the design but not implemented yet; its message
// says "not yet supported".
const (
	constructAbsent  = "absent"
	constructPlanned = "planned"
)

// Positions name the seam where a spelling already fails today. Diagnosing an
// absent construct there changes only the diagnostic: the set of admitted and
// formattable programs, and every look-alike identifier, stay as they were.
const (
	// absentInValue is an unresolved name in expression or callee position.
	absentInValue = "value"
	// absentInType is an unresolved name in type position.
	absentInType = "type"
	// absentInOperator is a character sequence the lexer does not admit.
	absentInOperator = "operator"
	// absentInDeclaration is a token where a module declaration must start.
	absentInDeclaration = "declaration"
	// absentInBlock is the keyword of a recognized braced construct whose
	// header or body the parser refuses, or a payload naming no data
	// declaration.
	absentInBlock = "block"
	// absentInStatement is a token whose statement form the parser refuses:
	// `=` after a statement, `if` without `else`, an anonymous `fn`.
	absentInStatement = "statement"
)

type absentSyntax struct {
	Construct string
	Position  string
	Spelling  string
	Status    string
	Message   string
	Help      string
}

func (a absentSyntax) diagnostic(span Span) Diagnostic {
	return Diagnostic{Code: absentSyntaxCode, Message: a.Message, Help: a.Help, Span: span}
}

const (
	absentOptionHelp   = "absence is explicit: use Data.Option<T> from `import Data \"effra/data\"`, built as `Data.Option.Some { value: x }` or `Data.Option<T>.None {}`"
	absentFailHelp     = "raise a declared failure with `fail E` in an `effect fn` that lists E in `raises { E }`; callers recover with `.catch<E>(fallback)`"
	absentRecoverHelp  = "failures are typed: recover with `.catch<E>(fallback)` on the effect that raises E; resources acquired inside `scope { ... }` are released when it closes"
	absentAssertHelp   = "values keep their checked types: convert with an explicit function, or decode external data with a checked codec such as Convert.Codec from `import Convert \"effra/conversions\"`"
	absentTopTypeHelp  = "declare a concrete type, or a closed enum of the admitted alternatives and `match` on it; decode external data into a concrete type with a checked codec"
	absentAsyncHelp    = "declare an `effect fn` and sequence other effects inside it with `run`"
	absentImportHelp   = "declare imports at the top of the module: `import Data \"effra/data\"`, or `import go name \"path\"` for a Go package"
	absentLoopHelp     = "repeat work with a recursive named function"
	absentNegationHelp = "negate with `if b { false } else { true }`"
)

// absentSyntaxCatalog classifies each construct against docs/design.md,
// docs/specs and NORTH_STAR.md. Planned entries cite the design that plans
// them in docs/tooling.md; a construct the specifications never plan is
// absent.
var absentSyntaxCatalog = []absentSyntax{
	{"null", absentInValue, "null", constructAbsent, "Effra has no `null` value", absentOptionHelp},
	{"null", absentInValue, "nil", constructAbsent, "Effra has no `nil` value", absentOptionHelp},
	{"null", absentInValue, "undefined", constructAbsent, "Effra has no `undefined` value", absentOptionHelp},
	{"throw", absentInValue, "throw", constructAbsent, "Effra has no `throw`", absentFailHelp},
	{"try-catch", absentInValue, "try", constructAbsent, "Effra has no `try`/`catch` blocks", absentRecoverHelp},
	{"try-catch", absentInValue, "catch", constructAbsent, "Effra has no `try`/`catch` blocks", absentRecoverHelp},
	{"try-catch", absentInBlock, "try", constructAbsent, "Effra has no `try`/`catch` blocks", absentRecoverHelp},
	{"try-catch", absentInBlock, "catch", constructAbsent, "Effra has no `try`/`catch` blocks", absentRecoverHelp},
	{"type-assertion", absentInValue, "as", constructAbsent, "Effra has no `as` type assertions", absentAssertHelp},
	{"top-type", absentInType, "unknown", constructAbsent, "Effra has no `unknown` type", absentTopTypeHelp},
	{"top-type", absentInType, "any", constructAbsent, "Effra has no `any` type", absentTopTypeHelp},
	{"async", absentInDeclaration, "async", constructAbsent, "Effra has no `async` functions", absentAsyncHelp},
	{"async", absentInValue, "async", constructAbsent, "Effra has no `async` functions", absentAsyncHelp},
	{"async", absentInValue, "await", constructAbsent, "Effra has no `await`", absentAsyncHelp},
	{"module-binding", absentInDeclaration, "let", constructPlanned, "module-level `let` bindings are not yet supported", "for a fixed value, declare a zero-argument function such as `fn initial() -> string { \"0\" }` and call it; state shared across calls belongs to a service whose implementation a layer provides"},
	{"dynamic-import", absentInValue, "import", constructAbsent, "Effra has no dynamic `import(...)`", absentImportHelp},
	{"question-operator", absentInOperator, "?", constructPlanned, "the `?` operator is not yet supported", "write a conditional as `if c { a } else { b }`; until Result propagation with `?` lands, inspect a Data.Result with `match`"},
	{"negation", absentInOperator, "!", constructAbsent, "Effra has no `!` operator", absentNegationHelp},
	{"inequality", absentInOperator, "!=", constructAbsent, "Effra has no `!=` operator", "compare with `==` and swap the branches: `if a == b { false } else { true }`"},
	{"logical-and", absentInOperator, "&&", constructAbsent, "Effra has no `&&` operator", "write `if a { b } else { false }`"},
	{"if-without-else", absentInStatement, "if", constructAbsent, "Effra has no `if` without `else`", "`if` is an expression with two branches: add `else { void }` when both complete with no value"},
	{"closure", absentInStatement, "fn", constructPlanned, "anonymous functions and closure captures are not yet supported", "declare a named module function and pass it by name"},
	{"closure", absentInStatement, "effect", constructPlanned, "anonymous functions and closure captures are not yet supported", "declare a named module function and pass it by name"},
	{"loop", absentInValue, "for", constructPlanned, "`for` loops are not yet supported", absentLoopHelp},
	{"loop", absentInValue, "while", constructPlanned, "`while` loops are not yet supported", absentLoopHelp},
	{"loop", absentInBlock, "for", constructPlanned, "`for` loops are not yet supported", absentLoopHelp},
	{"loop", absentInBlock, "while", constructPlanned, "`while` loops are not yet supported", absentLoopHelp},
	{"assignment", absentInStatement, "=", constructPlanned, "assignment is not yet supported", "bind the new value to a new name with `let`; state shared across calls belongs to a service"},
}

type absentSyntaxKey struct{ position, spelling string }

var absentSyntaxIndex = func() map[absentSyntaxKey]absentSyntax {
	index := make(map[absentSyntaxKey]absentSyntax, len(absentSyntaxCatalog))
	for _, entry := range absentSyntaxCatalog {
		key := absentSyntaxKey{entry.Position, entry.Spelling}
		if _, duplicate := index[key]; duplicate {
			panic("duplicate absent syntax entry " + entry.Position + " " + entry.Spelling)
		}
		index[key] = entry
	}
	return index
}()

// absentSyntaxAt reports the construct a spelling denotes at a failing seam.
func absentSyntaxAt(position, spelling string) (absentSyntax, bool) {
	entry, ok := absentSyntaxIndex[absentSyntaxKey{position, spelling}]
	return entry, ok
}

// absentOperatorAt reports the longest absent operator spelled at the start
// of text, where the lexer would otherwise refuse its first character.
func absentOperatorAt(text string) (absentSyntax, bool) {
	var found absentSyntax
	ok := false
	for _, entry := range absentSyntaxCatalog {
		if entry.Position == absentInOperator && strings.HasPrefix(text, entry.Spelling) && len(entry.Spelling) > len(found.Spelling) {
			found, ok = entry, true
		}
	}
	return found, ok
}

func (p *parser) failAbsent(position, spelling string, span Span) {
	entry, ok := absentSyntaxAt(position, spelling)
	if !ok {
		panic("unclassified absent syntax " + position + " " + spelling)
	}
	panic(syntaxFault{entry.diagnostic(span)})
}

// absentConstruct is a statement recognized as an absent braced construct:
// its keyword, and the offset just past its body's closing brace.
type absentConstruct struct {
	head token
	end  int
}

func (c absentConstruct) covers(span Span) bool {
	return c.head.kind != "" && span.Offset >= c.head.span.Offset && span.Offset < c.end
}

// absentConstructAt recognizes, without consuming tokens, the braced construct
// a statement head spells in the languages Effra authors arrive from:
//
//	try { ... }
//	catch [( ... )] { ... }
//	while ( ... ) { ... }  or  while condition { ... }
//	for ( ... ) { ... }    or  for x in xs { ... }  or  for k, v := range m { ... }
//	for init; condition; post { ... }  or  for condition { ... }  or  for { ... }
//
// A condition is whatever an `if` header admits. A head without that shape,
// such as `while` bound as a function and called, or a bare `while` followed
// by an ordinary statement, is no construct, so faults after it keep their
// own diagnostic.
func (p *parser) absentConstructAt() (construct absentConstruct, ok bool) {
	// Recognition only moves through tokens; conditions parse in a separate
	// parser, so restoring the position restores the parser.
	at := p.at
	defer func() {
		if value := recover(); value != nil {
			if _, fault := value.(syntaxFault); !fault {
				panic(value)
			}
			construct, ok = absentConstruct{}, false
		}
		p.at = at
	}()
	head := p.take()
	switch head.text {
	case "catch":
		if p.peek().text == "(" {
			p.tokenGroup("(", ")")
		}
	case "while":
		p.loopHeader(false)
	case "for":
		if p.peek().text != "(" && p.peek().text != "{" {
			binding := p.at
			if p.peek().kind == "name" {
				p.take()
				for p.accept(",") {
					p.name()
				}
			}
			if !p.accept("in") && !(p.accept(":") && p.accept("=") && p.accept("range")) {
				p.at = binding
			}
		}
		if p.peek().text != "{" {
			p.loopHeader(true)
		}
	}
	p.tokenGroup("{", "}")
	last := p.tokens[p.at-1].span
	return absentConstruct{head, last.Offset + last.Length}, true
}

// loopHeader is the tokens before the first brace outside parentheses, which
// opens the body: a parenthesized C-family header, a Go for clause when
// clauses are admitted, or a condition that parses as one Effra expression.
// As in Rust, a loop condition holds no payload braces, so a body such as
// `{ x: y }` is never read as a constructor.
func (p *parser) loopHeader(clauses bool) {
	start, separators := p.at, 0
	for depth := 0; depth > 0 || p.peek().text != "{"; {
		switch v := p.take(); {
		case v.kind == "eof" || v.text == "}" && depth == 0:
			p.fail(v, "expected {")
		case v.text == "(":
			depth++
		case v.text == ")":
			depth--
		case v.text == ";" && depth == 0:
			separators++
		}
	}
	if clauses && separators == 2 {
		return
	}
	header := append(slices.Clone(p.tokens[start:p.at]), token{"<eof>", "eof", p.peek().span})
	if header[0].text == "(" {
		grouped := &parser{tokens: header}
		grouped.tokenGroup("(", ")")
		if grouped.peek().kind == "eof" {
			return
		}
	}
	condition := &parser{tokens: header, types: maps.Clone(p.types)}
	condition.expr(0)
	if end := condition.peek(); end.kind != "eof" {
		condition.fail(end, "expected {")
	}
}

// tokenGroup consumes a balanced group of foreign tokens.
func (p *parser) tokenGroup(open, close string) {
	p.expect(open)
	for depth := 1; depth > 0; {
		switch v := p.take(); {
		case v.kind == "eof":
			p.fail(v, "expected "+close)
		case v.text == open:
			depth++
		case v.text == close:
			depth--
		}
	}
}

// recoverAbsentConstruct reports a syntax fault inside the recognized
// construct as that construct at its keyword. An EF003 raised there, and every
// fault outside it, keeps its own diagnostic.
func (p *parser) recoverAbsentConstruct(construct *absentConstruct) {
	if construct.head.kind == "" {
		return
	}
	if value := recover(); value != nil {
		if fault, ok := value.(syntaxFault); ok && fault.diagnostic.Code != absentSyntaxCode && construct.covers(fault.diagnostic.Span) {
			p.failAbsent(absentInBlock, construct.head.text, construct.head.span)
		}
		panic(value)
	}
}

// absentName reports an unresolved name as the construct it spells, if any.
func (c *checker) absentName(position, name string, span Span) bool {
	entry, ok := absentSyntaxAt(position, name)
	if !ok {
		return false
	}
	if !c.suppressDiagnostics {
		c.result.Diagnostics = append(c.result.Diagnostics, entry.diagnostic(span))
	}
	return true
}
