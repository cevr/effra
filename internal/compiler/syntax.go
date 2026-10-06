package compiler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Span offsets and columns are UTF-8 byte based, scoped to a semantic revision.
type Span struct {
	Offset int `json:"offset"`
	Length int `json:"length"`
	Line   int `json:"line"`
	Column int `json:"column"`
}
type Diagnostic struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Span    Span              `json:"span"`
	Related []RelatedLocation `json:"related,omitempty"`
}
type RelatedLocation struct {
	Message string `json:"message"`
	Span    Span   `json:"span"`
}

// Comment preserves the source text and byte location of a line comment for
// comment-aware semantic tooling. Text excludes the leading // marker.
type Comment struct {
	Text string
	Span Span
}
type token struct {
	text string
	kind string
	span Span
}
type syntaxFault struct{ diagnostic Diagnostic }
type parser struct {
	tokens      []token
	at          int
	depth       int
	noConstruct int
	types       map[string]*sourceType
}
type Param struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	TypeRef    TypeRef `json:"typeRef"`
	Span       Span    `json:"span"`
	typeID     TypeID
	sourceType *sourceType
}

// Field is a nominal declaration field. Type is kept as source text for
// compatibility with the original prototype; the checker resolves it to a
// canonical TypeRef before admitting the declaration.
type Field struct {
	Name       string `json:"name"`
	sourceType *sourceType
	Type       string  `json:"type"`
	TypeRef    TypeRef `json:"typeRef"`
	Span       Span    `json:"span"`
	typeID     TypeID
}
type Variant struct {
	Name          string  `json:"name"`
	Fields        []Field `json:"fields,omitempty"`
	Parenthesized bool    `json:"-"`
	Span          Span    `json:"span"`
}
type Record struct {
	Name         string              `json:"name"`
	Fields       []Field             `json:"fields,omitempty"`
	Span         Span                `json:"span"`
	Module       string              `json:"-"`
	SourceID     string              `json:"-"`
	Identity     string              `json:"-"`
	EmissionName string              `json:"-"`
	Parameters   []TemplateParameter `json:"-"`
}
type Enum struct {
	Name     string    `json:"name"`
	Variants []Variant `json:"variants"`
	Span     Span      `json:"span"`
}
type ErrorDecl struct {
	Name   string  `json:"name"`
	Fields []Field `json:"fields,omitempty"`
	Span   Span    `json:"span"`
}

// Declaration is the stable inspection projection for nominal application
// data. It deliberately contains no target-specific lowering details.
type Declaration struct {
	Source             string                  `json:"source,omitempty"`
	TemplateParameters []TemplateParameterView `json:"templateParameters,omitempty"`
	Kind               string                  `json:"kind"`
	Name               string                  `json:"name"`
	Identity           string                  `json:"identity,omitempty"`
	Fields             []Field                 `json:"fields,omitempty"`
	Variants           []Variant               `json:"variants,omitempty"`
	Span               Span                    `json:"span"`
}
type Function struct {
	Name         string
	Module       string `json:"-"`
	SourceID     string `json:"-"`
	EmissionName string `json:"-"`
	Params       []Param
	Return       string
	Effect       bool
	Errors       []string
	Services     []string
	Body         *Block
	Span         Span
	DeclSpan     Span `json:"-"`
	Ownership    []OwnershipFact
	Captures     []OwnershipFact
	// Identity is assigned by the checker from the canonical callable
	// contract. The source name remains a projection used by the emitters.
	Identity               string    `json:"-"`
	Owner                  string    `json:"-"`
	Contract               ValueType `json:"-"`
	Actual                 ValueType `json:"-"`
	returnType             *sourceType
	returnID               TypeID
	RowParameters          []RowParameter
	TypeParameters         []TemplateParameter
	returnFields           map[string]checkedExpression
	failureID              RowID
	serviceID              RowID
	signatureChecked       bool
	returnCallableEvidence callableEvidence
	CallbackPolicies       []CallbackPolicy
}
type Service struct {
	Name    string
	Methods []*Function
	Span    Span
}
type Provider struct {
	Name    string
	Service string
	Params  []Param
	// Services are the dependency values captured by the constructor. They
	// are requirements of construction, not requirements of the service
	// methods exposed by the resulting provider value.
	Services []string
	Methods  []*Function
	Span     Span
	Contract ValueType `json:"-"`
}

// Layer declarations select construction recipes without executing them.
type Layer struct {
	Name                                               string
	Provides, Errors, Services                         []string
	DeclaredProvides, DeclaredErrors, DeclaredServices bool
	Entries                                            []*LayerEntry
	Span                                               Span
}
type LayerEntry struct {
	Kind         string
	Name         string
	Value        *Expr
	Span         Span
	Continuation bool
}
type Program struct {
	interfaceProducer   bool
	semantic            *checker
	BundledTemplates    []*Record
	BundledTypeBindings map[string]map[string]*Record
	typeExpressions     map[string]*sourceType
	Imports             []GoImport
	BundledImports      []BundledImport
	BundledFunctions    []*Function
	BundledBindings     map[string]map[string]*Function
	Comments            []Comment
	Items               []*SyntaxItem `json:"-"`
	Bindings            map[string]Binding
	Modules             []*goModule
	UsedImports         map[string]bool
	GoOnly              bool
	Errors              map[string]Span
	ErrorDecls          []*ErrorDecl
	Records             []*Record
	Enums               []*Enum
	Services            []*Service
	Providers           []*Provider
	Layers              []*Layer
	Functions           []*Function
}

// SyntaxItem preserves the lexical declaration order that semantic
// projections intentionally group by kind. Formatter and future source
// adapters use these nodes as the ordered syntax seam; the grouped Program
// slices remain the checker-facing representation.
type SyntaxItem struct {
	Kind          string
	Import        *GoImport
	BundledImport *BundledImport
	Error         *ErrorDecl
	Record        *Record
	Enum          *Enum
	Service       *Service
	Provider      *Provider
	Layer         *Layer
	Function      *Function
	Span          Span
}
type Block struct {
	Statements []*Statement
	Explicit   bool `json:"-"`
}
type Statement struct {
	Kind    string
	Name    string
	Value   *Expr
	Payload *Expr
	Span    Span
}
type FieldValue struct {
	Name  string
	Value *Expr
	Span  Span
}
type MatchPattern struct {
	TypeName    string
	VariantName string
	Bindings    map[string]string
	Span        Span
}
type MatchArm struct {
	Pattern *MatchPattern
	Body    *Block
	Span    Span
}
type Expr struct {
	ResolvedTemplate *Record
	Kind             string
	Name             string
	Text             string
	Args             []*Expr
	Left             *Expr
	Right            *Expr
	Then             *Block
	Else             *Block
	Fields           []FieldValue
	Arms             []*MatchArm
	Span             Span
	Type             ValueType
	checked          checkedExpression
	// Evaluation is the work incurred while evaluating this expression now.
	// Deferred effect rows remain on Type. Keeping the two facts beside the
	// checked node lets callers reuse the result without walking the subtree.
	Evaluation EvaluationRows `json:"-"`
	// Executed is the row contribution of this expression when it is consumed
	// by its enclosing computation. It is a cached projection of Type for
	// run/branch/scope nodes and of Evaluation for ordinary values.
	Executed         EvaluationRows `json:"-"`
	Identity         string         `json:"-"`
	ResolvedFunction *Function      `json:"-"`
}

func lex(source string) ([]token, []Comment, []Diagnostic) {
	if diagnostic, ok := firstInvalidUTF8Diagnostic(source); ok {
		return nil, nil, []Diagnostic{diagnostic}
	}
	var out []token
	var comments []Comment
	line, column := 1, 1
	for i := 0; i < len(source); {
		start, l, c := i, line, column
		ch := source[i]
		if ch == '\n' {
			i++
			line++
			column = 1
			continue
		}
		if ch == '\r' && (i+1 == len(source) || source[i+1] != '\n') {
			return nil, comments, []Diagnostic{{Code: "EF001", Message: "standalone carriage return is unsupported; use LF or CRLF line endings", Span: Span{i, 1, line, column}}}
		}
		if ch == ' ' || ch == '\r' || ch == '\t' {
			i++
			column++
			continue
		}
		if ch == '/' && i+1 < len(source) && source[i+1] == '/' {
			commentStart := i
			i += 2
			column += 2
			for i < len(source) && source[i] != '\n' && source[i] != '\r' {
				i++
				column++
			}
			comments = append(comments, Comment{source[commentStart+2 : i], Span{commentStart, i - commentStart, l, c}})
			continue
		}
		kind := "symbol"
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' {
			kind = "name"
			i++
			for i < len(source) && ((source[i] >= 'a' && source[i] <= 'z') || (source[i] >= 'A' && source[i] <= 'Z') || (source[i] >= '0' && source[i] <= '9') || source[i] == '_') {
				i++
			}
		} else if ch >= '0' && ch <= '9' {
			kind = "integer"
			i++
			for i < len(source) && source[i] >= '0' && source[i] <= '9' {
				i++
			}
			if _, err := strconv.ParseInt(source[start:i], 10, 64); err != nil {
				return nil, comments, []Diagnostic{{Code: "EF001", Message: "integer exceeds i64 range", Span: Span{start, i - start, l, c}}}
			}
		} else if ch == '"' {
			kind = "string"
			i++
			for i < len(source) && source[i] != '"' && source[i] != '\n' {
				if source[i] == '\\' {
					i++
					if i >= len(source) {
						break
					}
				}
				i++
			}
			if i >= len(source) || source[i] != '"' {
				return nil, comments, []Diagnostic{{Code: "EF001", Message: "unterminated string", Span: Span{start, i - start, l, c}}}
			}
			i++
			var decoded string
			if err := json.Unmarshal([]byte(source[start:i]), &decoded); err != nil {
				return nil, comments, []Diagnostic{{Code: "EF001", Message: "strings use JSON escapes", Span: Span{start, i - start, l, c}}}
			}
		} else if i+1 < len(source) && (source[i:i+2] == "->" || source[i:i+2] == "==" || source[i:i+2] == "=>") {
			i += 2
		} else if strings.ContainsRune("{}():,;.+<>=", rune(ch)) {
			i++
		} else {
			return nil, comments, []Diagnostic{{Code: "EF001", Message: fmt.Sprintf("unsupported character %q", ch), Span: Span{start, 1, l, c}}}
		}
		column += i - start
		out = append(out, token{source[start:i], kind, Span{start, i - start, l, c}})
	}
	out = append(out, token{"<eof>", "eof", Span{len(source), 0, line, column}})
	return out, comments, nil
}

func firstInvalidUTF8Diagnostic(source string) (Diagnostic, bool) {
	line, column := 1, 1
	for offset := 0; offset < len(source); {
		if source[offset] == '\n' {
			offset++
			line++
			column = 1
			continue
		}
		if source[offset] == '\r' {
			offset++
			column++
			continue
		}
		_, size := utf8.DecodeRuneInString(source[offset:])
		if size == 1 && source[offset] >= utf8.RuneSelf {
			return Diagnostic{
				Code:    "EF001",
				Message: "source is not valid UTF-8",
				Span:    Span{Offset: offset, Length: 1, Line: line, Column: column},
			}, true
		}
		offset += size
		column += size
	}
	return Diagnostic{}, false
}

func parse(source string) (program *Program, diagnostics []Diagnostic) {
	program, _, diagnostics = parseSyntax(source)
	return program, diagnostics
}

func parseSyntax(source string) (program *Program, tokens []token, diagnostics []Diagnostic) {
	tokens, comments, diagnostics := lex(source)
	if len(diagnostics) > 0 {
		return nil, tokens, diagnostics
	}
	defer func() {
		if value := recover(); value != nil {
			if fault, ok := value.(syntaxFault); ok {
				program = nil
				diagnostics = []Diagnostic{fault.diagnostic}
			} else {
				panic(value)
			}
		}
	}()
	p := parser{tokens: tokens, types: map[string]*sourceType{}}
	program = &Program{Comments: comments, Errors: map[string]Span{}, Bindings: map[string]Binding{}, UsedImports: map[string]bool{}}
	program.typeExpressions = p.types
	for p.peek().kind != "eof" {
		switch p.peek().text {
		case "import":
			start := p.peek().span
			p.take()
			native := p.accept("go")
			alias := p.name()
			path := p.take()
			if path.kind != "string" {
				p.fail(path, "expected package path string")
			}
			var decoded string
			_ = json.Unmarshal([]byte(path.text), &decoded)
			if native {
				importDecl := &GoImport{alias.text, decoded, alias.span}
				program.Imports = append(program.Imports, *importDecl)
				program.Items = append(program.Items, &SyntaxItem{Kind: "import", Import: importDecl, Span: start})
			} else {
				importDecl := &BundledImport{Alias: alias.text, Path: decoded, Span: alias.span}
				program.BundledImports = append(program.BundledImports, *importDecl)
				program.Items = append(program.Items, &SyntaxItem{Kind: "import", BundledImport: importDecl, Span: start})
			}
			p.accept(";")
		case "error":
			start := p.peek().span
			p.take()
			name := p.name()
			if _, exists := program.Errors[name.text]; exists {
				p.fail(name, "duplicate error "+name.text)
			}
			program.Errors[name.text] = name.span
			decl := &ErrorDecl{Name: name.text, Span: name.span}
			if p.peek().text == "{" {
				decl.Fields = p.fields()
			}
			program.ErrorDecls = append(program.ErrorDecls, decl)
			program.Items = append(program.Items, &SyntaxItem{Kind: "error", Error: decl, Span: start})
			p.accept(";")
		case "record", "struct":
			start := p.peek().span
			p.take()
			name := p.name()
			record := &Record{Name: name.text, Span: name.span}
			if p.accept("<") {
				record.Parameters = p.templateParameters()
			}
			record.Fields = p.fields()
			program.Records = append(program.Records, record)
			program.Items = append(program.Items, &SyntaxItem{Kind: "record", Record: record, Span: start})
			p.accept(";")
		case "enum":
			start := p.peek().span
			p.take()
			name := p.name()
			p.expect("{")
			e := &Enum{Name: name.text, Span: name.span}
			for !p.accept("}") {
				variantName := p.name()
				variant := Variant{Name: variantName.text, Span: variantName.span}
				// A payload may be written with named fields in braces. Parenthesized
				// fields are accepted as a compact spelling for the same declaration.
				if p.peek().text == "{" {
					variant.Fields = p.fields()
				} else if p.accept("(") {
					variant.Parenthesized = true
					for !p.accept(")") {
						field := p.name()
						p.expect(":")
						typ := p.typ()
						variant.Fields = append(variant.Fields, Field{Name: field.text, Type: typ, sourceType: p.types[typ], Span: field.span})
						if !p.accept(",") {
							p.expect(")")
							break
						}
					}
				}
				e.Variants = append(e.Variants, variant)
				p.accept(",")
				p.accept(";")
			}
			program.Enums = append(program.Enums, e)
			program.Items = append(program.Items, &SyntaxItem{Kind: "enum", Enum: e, Span: start})
			p.accept(";")
		case "service":
			start := p.peek().span
			p.take()
			name := p.name()
			p.expect("{")
			s := &Service{Name: name.text, Span: name.span}
			for !p.accept("}") {
				f := p.function(false)
				if !f.Effect {
					p.fail(name, "service methods must be effect functions")
				}
				s.Methods = append(s.Methods, f)
			}
			program.Services = append(program.Services, s)
			program.Items = append(program.Items, &SyntaxItem{Kind: "service", Service: s, Span: start})
		case "impl":
			start := p.peek().span
			p.take()
			name := p.name()
			var params []Param
			if p.accept("(") {
				for !p.accept(")") {
					param := p.name()
					p.expect(":")
					typ := p.typ()
					params = append(params, Param{Name: param.text, Type: typ, sourceType: p.types[typ], Span: param.span})
					if !p.accept(",") {
						p.expect(")")
						break
					}
				}
			}
			p.expect("for")
			service := p.name()
			var services []string
			if p.accept("uses") {
				services = p.row()
			}
			p.expect("{")
			v := &Provider{Name: name.text, Service: service.text, Params: params, Services: services, Span: name.span}
			for !p.accept("}") {
				v.Methods = append(v.Methods, p.function(true))
			}
			program.Providers = append(program.Providers, v)
			program.Items = append(program.Items, &SyntaxItem{Kind: "impl", Provider: v, Span: start})
		case "effect", "fn":
			start := p.peek().span
			function := p.function(true)
			program.Functions = append(program.Functions, function)
			program.Items = append(program.Items, &SyntaxItem{Kind: "function", Function: function, Span: start})
		case "layer":
			start := p.take().span
			name := p.name()
			layer := &Layer{Name: name.text, Span: name.span}
			for p.peek().text != "{" {
				switch p.take().text {
				case "provides":
					if layer.DeclaredProvides {
						p.fail(p.peek(), "duplicate provides annotation")
					}
					layer.DeclaredProvides = true
					layer.Provides = p.row()
				case "raises":
					if layer.DeclaredErrors {
						p.fail(p.peek(), "duplicate raises annotation")
					}
					layer.DeclaredErrors = true
					layer.Errors = p.row()
				case "uses":
					if layer.DeclaredServices {
						p.fail(p.peek(), "duplicate uses annotation")
					}
					layer.DeclaredServices = true
					layer.Services = p.row()
				default:
					p.fail(p.peek(), "static layers admit only provides, raises and uses annotations; parameters are unsupported")
				}
			}
			p.expect("{")
			for !p.accept("}") {
				entry := &LayerEntry{Kind: "binding", Span: p.peek().span}
				if p.accept("merge") {
					continuation := false
					for {
						merged := p.name()
						span := merged.span
						if !continuation {
							span = entry.Span
						}
						layer.Entries = append(layer.Entries, &LayerEntry{Kind: "merge", Name: merged.text, Span: span, Continuation: continuation})
						continuation = true
						if !p.accept(",") {
							break
						}
					}
				} else {
					if p.accept("start") {
						p.fail(p.peek(), "startup recipes are unsupported in static layers")
					}
					if p.accept("replace") {
						entry.Kind = "replace"
					}
					entry.Name = p.name().text
					p.expect("=")
					entry.Value = p.expr(0)
					layer.Entries = append(layer.Entries, entry)
				}
				p.accept(";")
			}
			program.Layers = append(program.Layers, layer)
			program.Items = append(program.Items, &SyntaxItem{Kind: "layer", Layer: layer, Span: start})
		default:
			p.fail(p.peek(), "expected error, record, enum, service, impl, layer, or function declaration")
		}
	}
	return program, tokens, nil
}
func (p *parser) peek() token { return p.tokens[p.at] }
func (p *parser) take() token {
	v := p.peek()
	if v.kind != "eof" {
		p.at++
	}
	return v
}
func (p *parser) accept(text string) bool {
	if p.peek().text == text {
		p.take()
		return true
	}
	return false
}
func (p *parser) expect(text string) token {
	v := p.take()
	if v.text != text {
		p.fail(v, "expected "+text+", found "+v.text)
	}
	return v
}
func (p *parser) name() token {
	v := p.take()
	if v.kind != "name" {
		p.fail(v, "expected identifier")
	}
	return v
}
func (p *parser) fail(v token, message string) {
	panic(syntaxFault{Diagnostic{Code: "EF002", Message: message, Span: v.span}})
}
func (p *parser) typ() string {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 64 {
		p.fail(p.peek(), "type nesting exceeds limit of 64")
	}
	if p.accept("(") {
		if p.accept(")") {
			return "()"
		}
		inner := p.typ()
		p.expect(")")
		return inner
	}
	if p.peek().text == "fn" || p.peek().text == "effect" {
		typ := &sourceType{Effect: p.accept("effect")}
		p.expect("fn")
		p.expect("(")
		for !p.accept(")") {
			parameter := p.typ()
			typ.Parameters = append(typ.Parameters, parameter)
			typ.ParameterTypes = append(typ.ParameterTypes, p.types[parameter])
			if len(typ.Parameters) > 256 {
				p.fail(p.peek(), "callable type exceeds 256 parameters")
			}
			if !p.accept(",") {
				p.expect(")")
				break
			}
		}
		p.expect("->")
		typ.Result = p.typ()
		typ.ResultType = p.types[typ.Result]
		if p.accept("raises") {
			typ.Failures = p.row()
		}
		if p.accept("uses") {
			typ.Services = p.row()
		}
		name := typ.display()
		p.types[name] = typ
		return name
	}
	name := p.name()
	if name.text == "Effect" && p.peek().text == "<" {
		p.fail(name, "typed recipes are unsupported; use an explicit effect fn callback contract")
	}
	text := name.text
	if p.accept(".") {
		text += "." + p.name().text
	}
	if p.accept("<") {
		t := &sourceType{Application: text, Span: name.span}
		for {
			argument := p.typ()
			t.ApplicationArguments = append(t.ApplicationArguments, argument)
			t.ApplicationArgumentTypes = append(t.ApplicationArgumentTypes, p.types[argument])
			if len(t.ApplicationArguments) > 8 {
				p.fail(name, "at most eight template arguments are supported")
			}
			if p.accept(">") {
				break
			}
			p.expect(",")
		}
		text = t.display()
		p.types[text] = t
	}
	return text
}

func (p *parser) templateParameters() []TemplateParameter {
	parameters := []TemplateParameter{}
	for {
		name := p.name()
		p.expect(":")
		kind := p.take()
		parameter := TemplateParameter{Name: name.text, Kind: kind.text, Span: name.span}
		switch kind.text {
		case "type":
		case "callable":
			constraint := p.typ()
			parameter.Constraint = p.types[constraint]
			if parameter.Constraint == nil || parameter.Constraint.Application != "" {
				p.fail(kind, "callable constraints require a direct callable shape")
			}
		default:
			p.fail(kind, "template parameters require type or callable kind")
		}
		parameters = append(parameters, parameter)
		if len(parameters) > 8 {
			p.fail(name, "at most eight template parameters are supported")
		}
		if p.accept(">") {
			break
		}
		p.expect(",")
	}
	return parameters
}
func (p *parser) fields() []Field {
	p.expect("{")
	var fields []Field
	for !p.accept("}") {
		name := p.name()
		p.expect(":")
		typ := p.typ()
		fields = append(fields, Field{Name: name.text, Type: typ, sourceType: p.types[typ], Span: name.span})
		if !p.accept(",") && !p.accept(";") {
			if p.peek().text != "}" {
				continue
			}
			p.expect("}")
			break
		}
	}
	return fields
}
func (p *parser) fieldValues() []FieldValue {
	p.expect("{")
	var fields []FieldValue
	for !p.accept("}") {
		name := p.name()
		var value *Expr
		if p.accept(":") {
			value = p.expr(0)
		} else {
			// Record construction permits shorthand `{name}` for `{name: name}`.
			value = &Expr{Kind: "name", Name: name.text, Span: name.span}
		}
		fields = append(fields, FieldValue{Name: name.text, Value: value, Span: name.span})
		if !p.accept(",") && !p.accept(";") {
			if p.peek().text != "}" {
				continue
			}
			p.expect("}")
			break
		}
	}
	return fields
}
func (p *parser) row() []string {
	p.expect("{")
	var names []string
	for !p.accept("}") {
		names = append(names, p.name().text)
		if !p.accept(",") {
			p.expect("}")
			break
		}
	}
	return names
}
func (p *parser) function(body bool) *Function {
	declSpan := p.peek().span
	effect := p.accept("effect")
	p.expect("fn")
	name := p.name()
	f := &Function{Name: name.text, Effect: effect, Span: name.span, DeclSpan: declSpan}
	if p.accept("<") {
		for {
			parameter := p.name()
			p.expect(":")
			kind := p.take()
			if kind.text == "type" {
				f.TypeParameters = append(f.TypeParameters, TemplateParameter{Name: parameter.text, Kind: "type", Span: parameter.span})
				if len(f.TypeParameters) > 8 {
					p.fail(parameter, "at most eight type parameters are supported")
				}
			} else if kind.text != "raises" && kind.text != "uses" {
				p.fail(kind, "function parameters require type, raises or uses kind")
			} else {
				f.RowParameters = append(f.RowParameters, RowParameter{Name: parameter.text, Kind: kind.text, Span: parameter.span})
			}
			if len(f.RowParameters) > 8 {
				p.fail(parameter, "at most eight explicit row parameters are supported")
			}
			if p.accept(">") {
				break
			}
			p.expect(",")
		}
	}
	p.expect("(")
	for !p.accept(")") {
		param := p.name()
		p.expect(":")
		typ := p.typ()
		f.Params = append(f.Params, Param{Name: param.text, Type: typ, sourceType: p.types[typ], Span: param.span})
		if !p.accept(",") {
			p.expect(")")
			break
		}
	}
	p.expect("->")
	f.Return = p.typ()
	f.returnType = p.types[f.Return]
	if p.accept("raises") {
		f.Errors = p.row()
	} else if p.peek().text == "throws" {
		old := p.take()
		p.fail(old, "the Effra failure-row keyword `throws` was replaced by `raises`; use `raises`")
	}
	if p.accept("uses") {
		f.Services = p.row()
	}
	if body {
		f.Body = p.block()
	} else {
		p.accept(";")
	}
	return f
}
func (p *parser) block() *Block {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 256 {
		p.fail(p.peek(), "syntax nesting exceeds prototype limit of 256")
	}
	p.expect("{")
	b := &Block{Explicit: true}
	for !p.accept("}") {
		start := p.peek()
		s := &Statement{Span: start.span}
		if p.accept("let") {
			s.Kind = "let"
			s.Name = p.name().text
			p.expect("=")
			s.Value = p.expr(0)
		} else if p.accept("fail") {
			s.Kind = "fail"
			s.Name = p.name().text
			if p.peek().text == "(" {
				p.take()
				if !p.accept(")") {
					s.Payload = p.expr(0)
					p.expect(")")
				}
			} else if p.peek().text == "{" {
				s.Payload = &Expr{Kind: "payload", Fields: p.fieldValues(), Span: start.span}
			}
		} else {
			s.Kind = "expr"
			s.Value = p.expr(0)
		}
		b.Statements = append(b.Statements, s)
		p.accept(";")
	}
	return b
}
func (p *parser) expr(min int) *Expr {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 256 {
		p.fail(p.peek(), "syntax nesting exceeds prototype limit of 256")
	}
	start := p.take()
	e := &Expr{Span: start.span}
	switch {
	case (start.text == "fn" && p.anonymousCallableHead()) || (start.text == "effect" && p.peek().text == "fn"):
		p.fail(start, "anonymous functions and closure captures are unsupported; declare a named module function")
	case start.text == "scope":
		e.Kind = "scope"
		e.Then = p.block()
	case start.text == "fork":
		e.Kind = "fork"
		e.Left = p.expr(3)
	case start.kind == "integer":
		e.Kind = "integer"
		e.Text = start.text
	case start.text == "run":
		e.Kind = "run"
		e.Left = p.expr(3)
	case start.text == "if":
		e.Kind = "if"
		p.noConstruct++
		e.Left = p.expr(0)
		p.noConstruct--
		e.Then = p.block()
		p.expect("else")
		e.Else = p.block()
	case start.text == "match":
		e.Kind = "match"
		p.noConstruct++
		e.Left = p.expr(0)
		p.noConstruct--
		p.expect("{")
		for !p.accept("}") {
			pattern := p.pattern()
			p.expect("=>")
			var body *Block
			if p.peek().text == "fail" {
				start := p.take()
				statement := &Statement{Kind: "fail", Name: p.name().text, Span: start.span}
				if p.peek().text == "(" {
					p.take()
					if !p.accept(")") {
						statement.Payload = p.expr(0)
						p.expect(")")
					}
				} else if p.peek().text == "{" {
					statement.Payload = &Expr{Kind: "payload", Fields: p.fieldValues(), Span: start.span}
				}
				body = &Block{Statements: []*Statement{statement}}
			} else if p.peek().text == "{" {
				body = p.block()
			} else {
				value := p.expr(0)
				body = &Block{Statements: []*Statement{{Kind: "expr", Value: value, Span: value.Span}}}
			}
			e.Arms = append(e.Arms, &MatchArm{Pattern: pattern, Body: body, Span: pattern.Span})
			p.accept(",")
			p.accept(";")
		}
	case start.kind == "string":
		e.Kind = "string"
		_ = json.Unmarshal([]byte(start.text), &e.Text)
	case start.text == "true" || start.text == "false":
		e.Kind = "bool"
		e.Text = start.text
	case start.text == "(":
		if p.accept(")") {
			e.Kind = "unit"
		} else {
			e = p.expr(0)
			p.expect(")")
		}
	case start.kind == "name":
		e.Kind = "name"
		e.Name = start.text
	default:
		p.fail(start, "expected expression")
	}
	for {
		if (p.noConstruct == 0 || p.constructorBrace()) && p.peek().text == "{" && (e.Kind == "name" || e.Kind == "member") {
			e = &Expr{Kind: "construct", Left: e, Fields: p.fieldValues(), Span: e.Span}
			continue
		}
		if p.peek().text == "(" {
			if e.Kind != "name" && e.Kind != "member" {
				p.fail(p.peek(), "only named functions and service methods are callable")
			}
			p.take()
			call := &Expr{Kind: "call", Left: e, Span: e.Span}
			for !p.accept(")") {
				if p.peek().kind == "name" && p.at+1 < len(p.tokens) && p.tokens[p.at+1].text == ":" {
					field := p.name()
					p.expect(":")
					value := p.expr(0)
					call.Fields = append(call.Fields, FieldValue{Name: field.text, Value: value, Span: field.span})
					call.Args = append(call.Args, value)
				} else {
					call.Args = append(call.Args, p.expr(0))
				}
				if !p.accept(",") {
					p.expect(")")
					break
				}
			}
			e = call
			continue
		}
		if p.accept(".") {
			method := p.name()
			if method.text == "orFail" {
				p.expect("(")
				p.expect(")")
				e = &Expr{Kind: "orFail", Left: e, Span: method.span}
			} else if method.text == "timeout" {
				p.expect("(")
				arg := p.expr(0)
				p.expect(")")
				e = &Expr{Kind: "timeout", Left: e, Right: arg, Span: method.span}
			} else if method.text == "provide" || method.text == "catch" {
				if method.text == "provide" && p.accept("(") {
					layer := p.name()
					p.expect(")")
					e = &Expr{Kind: "provideLayer", Name: layer.text, Left: e, Span: method.span}
					continue
				}
				p.expect("<")
				t := p.name()
				p.expect(">")
				p.expect("(")
				arg := p.expr(0)
				p.expect(")")
				e = &Expr{Kind: method.text, Name: t.text, Left: e, Right: arg, Span: method.span}
			} else {
				e = &Expr{Kind: "member", Name: method.text, Left: e, Span: method.span}
			}
			continue
		}
		precedence := 0
		switch p.peek().text {
		case "==":
			precedence = 1
		case "+":
			precedence = 2
		}
		if precedence == 0 || precedence < min {
			break
		}
		op := p.take()
		e = &Expr{Kind: "binary", Name: op.text, Left: e, Right: p.expr(precedence + 1), Span: op.span}
	}
	return e
}

// An existing identifier named fn remains callable. The unsupported literal
// form is distinguished by its result arrow after the parameter parentheses.
func (p *parser) anonymousCallableHead() bool {
	if p.peek().text != "(" {
		return false
	}
	depth := 0
	for at := p.at; at < len(p.tokens); at++ {
		switch p.tokens[at].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return at+1 < len(p.tokens) && p.tokens[at+1].text == "->"
			}
		}
	}
	return false
}

func (p *parser) constructorBrace() bool {
	if p.peek().text != "{" || p.at+1 >= len(p.tokens) {
		return false
	}
	// A match/if body starts with a pattern or statement. A named payload
	// constructor has a field colon immediately after its first identifier.
	if p.tokens[p.at+1].text == "}" {
		// Empty constructors need one token of context: a control-body brace
		// follows the constructor, while an empty if/match body is followed by
		// `else` or the enclosing delimiter.
		return p.at+2 < len(p.tokens) && p.tokens[p.at+2].text == "{"
	}
	if p.at+2 >= len(p.tokens) {
		return false
	}
	if p.tokens[p.at+2].text == ":" {
		return true
	}
	// A shorthand payload has the form `Constructor { value }`. During a
	// control expression, the following arm/body brace disambiguates it from
	// the control block itself; ordinary expressions remain unambiguous because
	// constructors are enabled outside that protected parser region.
	if p.tokens[p.at+1].kind == "name" && p.tokens[p.at+2].text == "}" {
		return p.noConstruct == 0 || (p.at+3 < len(p.tokens) && p.tokens[p.at+3].text == "{")
	}
	if p.tokens[p.at+1].kind != "name" || (p.tokens[p.at+2].text != "," && p.tokens[p.at+2].text != ";") {
		return false
	}
	index := p.at + 1
	for {
		if index >= len(p.tokens) {
			return false
		}
		if p.tokens[index].kind != "name" {
			return false
		}
		index++
		if index >= len(p.tokens) {
			return false
		}
		if p.tokens[index].text == "}" {
			return p.noConstruct == 0 || (index+1 < len(p.tokens) && p.tokens[index+1].text == "{")
		}
		if p.tokens[index].text != "," && p.tokens[index].text != ";" {
			return false
		}
		index++
	}
}

func (p *parser) pattern() *MatchPattern {
	first := p.name()
	pattern := &MatchPattern{TypeName: first.text, Bindings: map[string]string{}, Span: first.span}
	if p.accept(".") {
		variant := p.name()
		pattern.VariantName = variant.text
		pattern.Span.Length = variant.span.Offset + variant.span.Length - pattern.Span.Offset
	}
	if p.accept("{") {
		seen := map[string]bool{}
		for !p.accept("}") {
			field := p.name()
			if seen[field.text] {
				p.fail(field, "duplicate pattern field "+field.text)
			}
			seen[field.text] = true
			binding := field.text
			if p.accept(":") {
				binding = p.name().text
			}
			pattern.Bindings[field.text] = binding
			if !p.accept(",") && !p.accept(";") {
				p.expect("}")
				break
			}
		}
	}
	return pattern
}
