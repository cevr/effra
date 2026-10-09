package compiler

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"unicode/utf8"

	rt "effra.local/prototype/runtime/effra"
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
	Help    string            `json:"help,omitempty"` // what to write instead, when known
	Span    Span              `json:"span"`
	Related []RelatedLocation `json:"related,omitempty"`
}
type RelatedLocation struct {
	Message string `json:"message"`
	Span    Span   `json:"span"`
}

// Comment preserves source comment text and byte location for comment-aware
// semantic tooling. Text excludes the line-comment marker or block delimiters.
type Comment struct {
	Text  string
	Span  Span
	Block bool
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
	// subjectList is true when the innermost protected control header is a
	// match subject list, whose commas delimit constructor subjects.
	subjectList bool
	// payloadBraces memoizes constructorBrace by brace token index.
	payloadBraces map[int]bool
	types         map[string]*sourceType
	// lastType and lastRow are the most recent type annotation occurrence
	// and row labels, for the declaration that owns them.
	lastType *typeSyntax
	lastRow  []rowLabel
	// lastPipe is the |> that ends the unparenthesised chain the most recent
	// expr call returned, zero when that chain held no pipe.
	lastPipe Span
	// genericAngles records the angle tokens admitted by the shared type and
	// generic-application grammar. The formatter consumes these facts instead
	// of guessing from every literal < or > token.
	genericAngles map[int]bool
	// multiplications records the `*` tokens parsed as the binary operator,
	// so the formatter can space them apart from a native pointer type `*T`.
	multiplications map[int]bool
	// absent is absent-construct recognition state; a condition parser has
	// none and recognizes nothing.
	absent *absentScan
}
type Param struct {
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	TypeRef        TypeRef        `json:"typeRef"`
	Span           Span           `json:"span"`
	RequiredChoice bool           `json:"requiredChoice"`
	DefaultValue   *ConstantValue `json:"defaultValue"`
	Extent         Span           `json:"-"`
	TypeSpan       Span           `json:"-"` // the type's tokens inside any grouping, for type-position diagnostics
	requiredChoice bool
	requiredSpan   Span
	defaultExpr    *Expr
	defaultSpan    Span
	typeID         TypeID
	sourceType     *sourceType
	binding        *localBinding
	// Annotation is this declaration's own type annotation occurrence.
	Annotation *typeSyntax `json:"-"`
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
	TypeSpan   Span    `json:"-"`
	typeID     TypeID
	Annotation *typeSyntax `json:"-"`
}
type Variant struct {
	Name          string  `json:"name"`
	Fields        []Field `json:"fields,omitempty"`
	Parenthesized bool    `json:"-"`
	Span          Span    `json:"span"`
}

// DataDeclaration owns the common nominal identity and first-order layout for
// records and closed enums. The checker keeps their distinct declaration kinds.
type DataDeclaration struct {
	owner        *checker
	Kind         string              `json:"-"`
	Name         string              `json:"name"`
	Fields       []Field             `json:"fields,omitempty"`
	Variants     []Variant           `json:"variants,omitempty"`
	Span         Span                `json:"span"`
	Module       string              `json:"-"`
	SourceID     string              `json:"-"`
	Identity     string              `json:"-"`
	EmissionName string              `json:"-"`
	Parameters   []TemplateParameter `json:"-"`
}
type Record = DataDeclaration
type Enum = DataDeclaration
type ErrorDecl struct {
	Name   string  `json:"name"`
	Fields []Field `json:"fields,omitempty"`
	Span   Span    `json:"span"`
}

// Declaration is the stable inspection projection for nominal application
// data. It deliberately contains no target-specific lowering details.
type Declaration struct {
	DataKind           string                  `json:"dataKind,omitempty"`
	Source             string                  `json:"source,omitempty"`
	TemplateParameters []TemplateParameterView `json:"templateParameters,omitempty"`
	Type               *TypeRef                `json:"type,omitempty"`
	Constant           *ConstantValue          `json:"constant,omitempty"`
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
	Extent       Span `json:"-"`
	ReturnSpan   Span `json:"-"`
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
	// codec is set on the direction functions a derive declaration
	// synthesizes; their body is one compiler-owned codec operation.
	codec *CodecDeclaration

	// ReturnAnnotation, ErrorLabels and ServiceLabels are the declared
	// result annotation and row label tokens.
	ReturnAnnotation           *typeSyntax `json:"-"`
	ErrorLabels, ServiceLabels []rowLabel  `json:"-"`
}
type Service struct {
	Name    string
	Methods []*Function
	Span    Span
	// byReference marks a builtin contract admitted only when the program
	// refers to it, whose providers are retained only through checked use.
	byReference bool
	// native lists builtin runtime modules referenced by the service's
	// operation signatures. Source services leave it empty.
	native []rt.RuntimeModule
}
type Provider struct {
	Name        string
	Service     string
	ServiceSpan Span `json:"-"`
	Params      []Param
	// Services are the dependency values captured by the constructor. They
	// are requirements of construction, not requirements of the service
	// methods exposed by the resulting provider value.
	Services []string
	Methods  []*Function
	Span     Span
	Contract ValueType `json:"-"`
	// native lists builtin runtime modules referenced by the provider's
	// implementation. Source providers leave it empty.
	native []rt.RuntimeModule
	// ServiceLabels are the constructor's `uses` row label tokens.
	ServiceLabels []rowLabel `json:"-"`
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
	interfaceProducer       bool
	references              map[string]bool
	semantic                *checker
	BundledTemplates        []*Record
	BundledTypeBindings     map[string]map[string]*Record
	BundledConstantBindings map[string]map[string]*Constant
	typeExpressions         map[string]*sourceType
	Imports                 []GoImport
	BundledImports          []BundledImport
	BundledFunctions        []*Function
	BundledBindings         map[string]map[string]*Function
	Comments                []Comment
	Items                   []*SyntaxItem `json:"-"`
	Bindings                map[string]Binding
	host                    *hostImports
	Modules                 []*goModule
	UsedImports             map[string]bool
	GoOnly                  bool
	Errors                  map[string]Span
	genericAngles           map[int]bool
	multiplications         map[int]bool
	ErrorDecls              []*ErrorDecl
	Records                 []*Record
	Enums                   []*Enum
	Services                []*Service
	Providers               []*Provider
	Layers                  []*Layer
	Functions               []*Function
	Constants               []*Constant
	Codecs                  []*CodecDeclaration
	DerivedFunctions        []*Function
	DerivedBindings         map[string]map[string]*Function
	BundledDerivations      map[string]map[string]*codecDerivation
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
	Constant      *Constant
	Codec         *CodecDeclaration
	Span          Span
	Extent        Span `json:"-"`
}

// ConstantValue is a checked scalar source value. Nominal constructors and
// computed expressions remain outside the constant-default admission.
type ConstantValue struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Constant struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Span     Span           `json:"span"`
	Extent   Span           `json:"-"`
	TypeSpan Span           `json:"-"`
	Expr     *Expr          `json:"-"`
	Module   string         `json:"-"`
	SourceID string         `json:"-"`
	Identity string         `json:"-"`
	Value    *ConstantValue `json:"-"`
	invalid  bool
	typeID   TypeID
}
type Block struct {
	Statements []*Statement
	Explicit   bool `json:"-"`
	Extent     Span `json:"-"`
}
type Statement struct {
	Kind     string
	Name     string
	Value    *Expr
	Payload  *Expr
	Span     Span
	NameSpan Span `json:"-"`
	Extent   Span `json:"-"`
	binding  *localBinding
}

// FieldValue.Label is the explicit `name:` token. Shorthand fields and
// checker-normalized positional arguments have no label token of their own.
type FieldValue struct {
	Name  string
	Value *Expr
	Span  Span
	Label Span `json:"-"`
}
type MatchPattern struct {
	TypeName    string
	VariantName string
	Bindings    map[string]string
	Span        Span
	Extent      Span          `json:"-"`
	Names       []PatternName `json:"-"`
	// Segments are the dotted name tokens in source order; Span covers them.
	Segments []Span `json:"-"`
	// Payload covers the binding braces when present, so syntax consumers can
	// recognize pattern punctuation without re-deriving it from tokens.
	Payload      Span  `json:"-"`
	ResolvedEnum *Enum `json:"-"`
}

// PatternName retains source order and the alias token independently of the
// checker-facing field map. Span remains the existing diagnostic anchor.
type PatternName struct {
	Field     string
	Name      string
	FieldSpan Span
	NameSpan  Span
}

// MatchArm holds one pattern cell per match subject, in subject order. Each
// cell lists its named-variant alternatives (`A | B`) in source order.
type MatchArm struct {
	Patterns [][]*MatchPattern
	Body     *Block
	Span     Span
	Extent   Span `json:"-"`
	// binders holds one binder per bound name of the arm: the alternatives of
	// a cell bind the same names, and the arm's body reads each name once.
	binders map[string]*localBinding
}

// EachPattern visits every alternative of every cell in source order.
func (a *MatchArm) EachPattern(visit func(subject int, pattern *MatchPattern)) {
	for subject, cell := range a.Patterns {
		for _, pattern := range cell {
			visit(subject, pattern)
		}
	}
}

type Expr struct {
	constructorType  *sourceType
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
	Extent           Span `json:"-"`
	// NameSpan is the declaration-name token written inside provide<S>,
	// catch<E> and provide(Layer); Span remains their method anchor.
	NameSpan  Span `json:"-"`
	Type      ValueType
	checked   checkedExpression
	layerPlan *LayerPlan
	matchPlan *matchPlan
	// codec is the derive declaration a synthesized codec operation runs.
	codec *CodecDeclaration
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
	// binding is the local binder a name or static layer provision resolves
	// to; nil when it resolves globally.
	binding *localBinding
	// ArgumentParameters is a checked call's label binding:
	// ArgumentParameters[i] is the parameter that bound argument i (see
	// boundArguments) binds. It is nil when every argument binds by position.
	// Args stay in source order, which is also evaluation order.
	ArgumentParameters []int `json:"-"`
	// BoundArguments is set only on a checked call that omits defaulted
	// parameters: Args in source order, with the checked literal of each
	// omitted parameter's constant default placed before the first authored
	// argument that binds a later parameter. It is never authored syntax:
	// tooling that walks source reads Args alone.
	BoundArguments []*Expr `json:"-"`
	// defaultArgument marks a constant literal the checker synthesized for an
	// omitted parameter.
	defaultArgument bool
	// PipeSpan is the |> token of a call written `x |> f(...)`; Args[0] is x.
	// Diagnostics read it for wording and anchors only: it never decides what
	// is accepted, how a call is checked or what is emitted.
	PipeSpan Span `json:"-"`
	// allowMinLiteral is set only for the magnitude immediately under a unary
	// minus. The lexer admits that one extra magnitude so the signed minimum
	// value can be represented without pretending that positive 2^63 fits.
	allowMinLiteral bool
}

func i64LiteralMagnitude(text string) (uint64, bool) {
	value, err := strconv.ParseUint(text, 10, 64)
	return value, err == nil
}

func isMinI64Literal(text string) bool {
	value, ok := i64LiteralMagnitude(text)
	return ok && value == uint64(1<<63)
}

func normalizedI64Literal(text string) string {
	value, ok := i64LiteralMagnitude(text)
	if !ok {
		return text
	}
	return strconv.FormatUint(value, 10)
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
			comments = append(comments, Comment{Text: source[commentStart+2 : i], Span: Span{commentStart, i - commentStart, l, c}})
			continue
		}
		if ch == '/' && i+1 < len(source) && source[i+1] == '*' {
			commentStart := i
			i += 2
			column += 2
			closed := false
			for i < len(source) {
				if source[i] == '*' && i+1 < len(source) && source[i+1] == '/' {
					i += 2
					column += 2
					closed = true
					break
				}
				if source[i] == '\n' {
					i++
					line++
					column = 1
					continue
				}
				if source[i] == '\r' {
					if i+1 == len(source) || source[i+1] != '\n' {
						return nil, comments, []Diagnostic{{Code: "EF001", Message: "standalone carriage return is unsupported; use LF or CRLF line endings", Span: Span{i, 1, line, column}}}
					}
					i += 2
					line++
					column = 1
					continue
				}
				i++
				column++
			}
			if !closed {
				return nil, comments, []Diagnostic{{Code: "EF001", Message: "unterminated block comment", Span: Span{commentStart, i - commentStart, l, c}}}
			}
			comments = append(comments, Comment{Text: source[commentStart+2 : i-2], Span: Span{commentStart, i - commentStart, l, c}, Block: true})
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
			value, err := strconv.ParseUint(source[start:i], 10, 64)
			if err != nil || value > uint64(1<<63) {
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
		} else if i+1 < len(source) && (source[i:i+2] == "->" || source[i:i+2] == "|>" || source[i:i+2] == "==" || source[i:i+2] == "=>" || source[i:i+2] == "<=" || source[i:i+2] == ">=") {
			i += 2
		} else if strings.ContainsRune("{}():,;.+-<>=|*[]/%", rune(ch)) {
			i++
		} else {
			if operator, ok := absentOperatorAt(source[i:]); ok {
				return nil, comments, []Diagnostic{operator.diagnostic(Span{start, len(operator.Spelling), l, c})}
			}
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
	program, _, _, diagnostics = parseSyntax(source)
	return program, diagnostics
}

// parseSyntax parses source. The comments are those the lexer collected,
// returned whether or not a program was built: a syntax fault does not
// erase what the source says before or around it. After a lexical fault
// they are the comments lexed before it.
func parseSyntax(source string) (program *Program, tokens []token, comments []Comment, diagnostics []Diagnostic) {
	tokens, comments, diagnostics = lex(source)
	if len(diagnostics) > 0 {
		return nil, tokens, comments, diagnostics
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
	p := parser{tokens: tokens, types: map[string]*sourceType{}, absent: &absentScan{}, genericAngles: map[int]bool{}, multiplications: map[int]bool{}}
	program = &Program{Comments: comments, Errors: map[string]Span{}, Bindings: map[string]Binding{}, UsedImports: map[string]bool{}, genericAngles: p.genericAngles, multiplications: p.multiplications}
	program.typeExpressions = p.types
	for p.peek().kind != "eof" {
		switch p.peek().text {
		case "import":
			start := p.peek().span
			p.take()
			native := p.accept("go")
			if !native && p.peek().text == "(" {
				p.failAbsent(absentInValue, "import", start)
			}
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
		case "const":
			start := p.take().span
			name := p.name()
			p.expect(":")
			typ, typeSpan := p.typeAnnotation()
			p.expect("=")
			value := p.scalarConstantExpr()
			decl := &Constant{Name: name.text, Type: typ, TypeSpan: typeSpan, Expr: value, Span: name.span, Extent: p.extent(start)}
			program.Constants = append(program.Constants, decl)
			program.Items = append(program.Items, &SyntaxItem{Kind: "constant", Constant: decl, Span: start, Extent: decl.Extent})
			p.accept(";")
		case "record", "struct":
			start := p.peek().span
			p.take()
			name := p.name()
			record := &Record{Kind: "record", Name: name.text, Span: name.span}
			if p.acceptGenericOpen() {
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
			e := &Enum{Kind: "enum", Name: name.text, Span: name.span}
			if p.acceptGenericOpen() {
				e.Parameters = p.templateParameters()
			}
			p.expect("{")
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
						typ, typeSpan := p.typeAnnotation()
						variant.Fields = append(variant.Fields, Field{Name: field.text, Type: typ, sourceType: p.types[typ], Span: field.span, TypeSpan: typeSpan, Annotation: p.lastType})
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
					typ, typeSpan := p.typeAnnotation()
					params = append(params, Param{Name: param.text, Type: typ, sourceType: p.types[typ], Span: param.span, Extent: p.extent(param.span), TypeSpan: typeSpan, Annotation: p.lastType})
					if !p.accept(",") {
						p.expect(")")
						break
					}
				}
			}
			p.expect("for")
			service := p.name()
			var services []string
			var serviceLabels []rowLabel
			if p.accept("uses") {
				services, serviceLabels = p.row(), p.lastRow
			}
			p.expect("{")
			v := &Provider{Name: name.text, Service: service.text, ServiceSpan: service.span, Params: params, Services: services, ServiceLabels: serviceLabels, Span: name.span}
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
		case "derive":
			start := p.take().span
			codec := p.codecDeclaration()
			program.Codecs = append(program.Codecs, codec)
			program.Items = append(program.Items, &SyntaxItem{Kind: "derive", Codec: codec, Span: start})
			p.accept(";")
		default:
			if head := p.peek(); head.kind == "name" {
				if _, absent := absentSyntaxAt(absentInDeclaration, head.text); absent {
					p.failAbsent(absentInDeclaration, head.text, head.span)
				}
			}
			p.fail(p.peek(), "expected error, record, enum, service, impl, layer, derive, or function declaration")
		}
		item := program.Items[len(program.Items)-1]
		item.Extent = p.extent(item.Span)
	}
	return program, tokens, comments, nil
}
func (p *parser) peek() token { return p.tokens[p.at] }

// extent includes all consumed tokens while retaining the original anchor's
// byte-based line and column. Trivia after the final token is not a node.
func (p *parser) extent(start Span) Span {
	if p.at > 0 {
		last := p.tokens[p.at-1].span
		start.Length = last.Offset + last.Length - start.Offset
	}
	return start
}
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

func (p *parser) markMultiplication(index int) {
	if p.multiplications == nil {
		p.multiplications = map[int]bool{}
	}
	p.multiplications[index] = true
}

func (p *parser) markGenericAngle(index int) {
	if p.genericAngles == nil {
		p.genericAngles = map[int]bool{}
	}
	p.genericAngles[index] = true
}

func (p *parser) acceptGenericOpen() bool {
	if !p.accept("<") {
		return false
	}
	p.markGenericAngle(p.at - 1)
	return true
}

func (p *parser) acceptGenericClose() bool {
	if !p.accept(">") {
		return false
	}
	p.markGenericAngle(p.at - 1)
	return true
}

func (p *parser) expectGenericOpen() token {
	v := p.expect("<")
	p.markGenericAngle(p.at - 1)
	return v
}

func (p *parser) expectGenericClose() token {
	v := p.expect(">")
	p.markGenericAngle(p.at - 1)
	return v
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
	if v.text == "void" {
		p.fail(v, "void is reserved for the no-value type and expression")
	}
	return v
}

// memberName admits a qualified host member without making the source
// keyword available as a local/declaration name. Host interop keeps its
// existing qualified-name rules; `sdk.void` is therefore parsed as a member
// and is rejected or admitted by the normal import checker.
func (p *parser) memberName() token {
	v := p.take()
	if v.kind != "name" {
		p.fail(v, "expected identifier")
	}
	return v
}

func (p *parser) failSpan(span Span, message string) {
	panic(syntaxFault{Diagnostic{Code: "EF002", Message: message, Span: span}})
}
func (p *parser) fail(v token, message string) {
	panic(syntaxFault{Diagnostic{Code: "EF002", Message: message, Span: v.span}})
}
func (p *parser) typ() string {
	typ, _ := p.typeAnnotation()
	return typ
}

// typeAnnotation parses a type and reports the span of its tokens inside any
// grouping parentheses, so a diagnostic about the type marks the type itself.
// Each occurrence also leaves its syntax tree in p.lastType.
func (p *parser) typeAnnotation() (string, Span) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 64 {
		p.fail(p.peek(), "type nesting exceeds limit of 64")
	}
	if p.accept("(") {
		open := p.tokens[p.at-1]
		if p.accept(")") {
			close := p.tokens[p.at-1]
			p.failSpan(Span{Offset: open.span.Offset, Length: close.span.Offset + close.span.Length - open.span.Offset, Line: open.span.Line, Column: open.span.Column}, "use void instead of () for the no-value type")
		}
		inner, span := p.typeAnnotation()
		p.expect(")")
		return inner, span
	}
	start := p.peek().span
	occurrence := &typeSyntax{}
	done := func(name string) (string, Span) {
		occurrence.Extent = p.extent(start)
		p.lastType = occurrence
		return name, occurrence.Extent
	}
	if p.peek().text == "fn" || p.peek().text == "effect" {
		typ := &sourceType{Effect: p.accept("effect")}
		occurrence.Form = "callable"
		p.expect("fn")
		p.expect("(")
		for !p.accept(")") {
			parameter := p.typ()
			typ.Parameters = append(typ.Parameters, parameter)
			typ.ParameterTypes = append(typ.ParameterTypes, p.types[parameter])
			occurrence.Args = append(occurrence.Args, p.lastType)
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
		occurrence.Result = p.lastType
		if p.accept("raises") {
			typ.Failures, occurrence.Failures = p.row(), p.lastRow
		}
		if p.accept("uses") {
			typ.Services, occurrence.Services = p.row(), p.lastRow
		}
		name := typ.display()
		p.types[name] = typ
		return done(name)
	}
	if p.peek().text == "void" {
		p.take()
		return done(voidTypeName)
	}
	if form := p.hostTypeForm(occurrence); form != nil {
		name := form.display()
		p.types[name] = form
		return done(name)
	}
	name := p.name()
	if name.text == "Effect" && p.peek().text == "<" {
		p.fail(name, "typed recipes are unsupported; use an explicit effect fn callback contract")
	}
	text := name.text
	occurrence.Name = rowLabel{Name: name.text, Span: name.span}
	if p.accept(".") {
		member := p.memberName()
		text += "." + member.text
		occurrence.Qualifier, occurrence.Name = occurrence.Name, rowLabel{Name: member.text, Span: member.span}
	}
	if p.acceptGenericOpen() {
		t := &sourceType{Application: text, Span: name.span}
		for {
			argument := p.typ()
			t.ApplicationArguments = append(t.ApplicationArguments, argument)
			t.ApplicationArgumentTypes = append(t.ApplicationArgumentTypes, p.types[argument])
			occurrence.Args = append(occurrence.Args, p.lastType)
			if len(t.ApplicationArguments) > 8 {
				p.fail(name, "at most eight template arguments are supported")
			}
			if p.acceptGenericClose() {
				break
			}
			p.expect(",")
		}
		text = t.display()
		p.types[text] = t
	}
	return done(text)
}

// hostTypeForm parses Go's own spelling of a native pointer, slice or map
// type (`*sdk.Client`, `[]string`, `map[string]int`). The spelling is the one
// inspection displays, so a reported host type round-trips into source; the
// checker admits it only through the imported native declarations.
func (p *parser) hostTypeForm(occurrence *typeSyntax) *sourceType {
	start := p.peek()
	form := &sourceType{Span: start.span}
	switch {
	case p.accept("*"):
		form.HostForm = "pointer"
	case p.accept("["):
		p.expect("]")
		form.HostForm = "slice"
	case start.text == "map" && p.at+1 < len(p.tokens) && p.tokens[p.at+1].text == "[":
		p.take()
		p.take()
		form.HostForm = "map"
		key := p.typ()
		form.HostArguments = append(form.HostArguments, key)
		form.HostArgumentTypes = append(form.HostArgumentTypes, p.types[key])
		occurrence.Args = append(occurrence.Args, p.lastType)
		p.expect("]")
	default:
		return nil
	}
	occurrence.Form = "host"
	element := p.typ()
	form.HostArguments = append(form.HostArguments, element)
	form.HostArgumentTypes = append(form.HostArgumentTypes, p.types[element])
	occurrence.Args = append(occurrence.Args, p.lastType)
	return form
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
			parameter.Constraint, parameter.Annotation = p.types[constraint], p.lastType
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
		if p.acceptGenericClose() {
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
		typ, typeSpan := p.typeAnnotation()
		fields = append(fields, Field{Name: name.text, Type: typ, sourceType: p.types[typ], Span: name.span, TypeSpan: typeSpan, Annotation: p.lastType})
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
		field := FieldValue{Name: name.text, Span: name.span}
		if p.accept(":") {
			// Payload braces delimit their values, so constructor braces
			// inside an if/match header are unambiguous again.
			protected := p.noConstruct
			p.noConstruct = 0
			field.Value, field.Label = p.expr(0), name.span
			p.noConstruct = protected
		} else {
			// Record construction permits shorthand `{name}` for `{name: name}`.
			field.Value = &Expr{Kind: "name", Name: name.text, Span: name.span, Extent: name.span}
		}
		fields = append(fields, field)
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
	p.lastRow = nil
	for !p.accept("}") {
		label := p.name()
		names = append(names, label.text)
		p.lastRow = append(p.lastRow, rowLabel{Name: label.text, Span: label.span})
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
	if p.acceptGenericOpen() {
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
			if p.acceptGenericClose() {
				break
			}
			p.expect(",")
		}
	}
	p.expect("(")
	for !p.accept(")") {
		required := token{}
		if p.peek().text == "required" && p.at+2 < len(p.tokens) && p.tokens[p.at+1].kind == "name" && p.tokens[p.at+2].text == ":" {
			required = p.take()
		}
		param := p.name()
		p.expect(":")
		typ, typeSpan := p.typeAnnotation()
		parameter := Param{
			Name: param.text, Type: typ, sourceType: p.types[typ],
			Span: param.span, TypeSpan: typeSpan, Annotation: p.lastType,
			requiredChoice: required.text == "required", requiredSpan: required.span,
		}
		if p.accept("=") {
			parameter.defaultExpr = p.scalarConstantExpr()
			parameter.defaultSpan = parameter.defaultExpr.Span
		}
		parameter.Extent = p.extent(param.span)
		f.Params = append(f.Params, parameter)
		if !p.accept(",") {
			p.expect(")")
			break
		}
	}
	p.expect("->")
	f.Return, f.ReturnSpan = p.typeAnnotation()
	f.ReturnAnnotation = p.lastType
	f.returnType = p.types[f.Return]
	if p.accept("raises") {
		f.Errors, f.ErrorLabels = p.row(), p.lastRow
	} else if p.peek().text == "throws" {
		old := p.take()
		p.fail(old, "the Effra failure-row keyword `throws` was replaced by `raises`; use `raises`")
	}
	if p.accept("uses") {
		f.Services, f.ServiceLabels = p.row(), p.lastRow
	}
	if body {
		f.Body = p.block()
	} else {
		p.accept(";")
	}
	f.Extent = p.extent(declSpan)
	return f
}

// scalarConstantExpr admits a direct unary-minus integer token as one signed
// scalar literal. It does not enable general unary expression syntax.
func (p *parser) scalarConstantExpr() *Expr {
	if p.peek().text == "-" {
		minus := p.take()
		if p.at+1 >= len(p.tokens) || p.tokens[p.at].kind != "integer" {
			p.failSpan(minus.span, "constant value must be a scalar literal or direct constant alias")
		}
		magnitude := p.take()
		span := minus.span
		span.Length = magnitude.span.Offset + magnitude.span.Length - minus.span.Offset
		return &Expr{Kind: "integer", Text: "-" + magnitude.text, Span: span, Extent: span}
	}
	if p.peek().text == "+" {
		p.failSpan(p.peek().span, "constant value must be a scalar literal or direct constant alias")
	}
	return p.expr(0)
}

func (p *parser) block() *Block {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 256 {
		p.fail(p.peek(), "syntax nesting exceeds prototype limit of 256")
	}
	open := p.expect("{")
	b := &Block{Explicit: true}
	// Block braces delimit their statements, so constructor braces inside a
	// block nested in an if/match header are unambiguous again.
	protected, subjectList := p.noConstruct, p.subjectList
	p.noConstruct, p.subjectList = 0, false
	defer func() { p.noConstruct, p.subjectList = protected, subjectList }()
	// construct is the latest statement in this block recognized as an
	// absent braced construct such as `while c { ... }`. Its tokens are no
	// Effra syntax, so a syntax fault inside them reports that construct at
	// its keyword rather than at whichever token the misparse reached.
	var construct absentConstruct
	defer p.recoverAbsentConstruct(&construct)
	for !p.accept("}") {
		start := p.peek()
		if _, absent := absentSyntaxAt(absentInBlock, start.text); absent && start.kind == "name" {
			if recognized, ok := p.absentConstructAt(); ok {
				construct = recognized
			}
		}
		s := &Statement{Span: start.span}
		if p.accept("let") {
			s.Kind = "let"
			name := p.name()
			s.Name, s.NameSpan = name.text, name.span
			p.expect("=")
			s.Value = p.expr(0)
		} else if p.accept("fail") {
			s.Kind = "fail"
			name := p.name()
			s.Name, s.NameSpan = name.text, name.span
			if p.peek().text == "(" {
				p.take()
				if !p.accept(")") {
					s.Payload = p.expr(0)
					p.expect(")")
				}
			} else if p.peek().text == "{" {
				payloadStart := p.peek().span
				s.Payload = &Expr{Kind: "payload", Fields: p.fieldValues(), Span: start.span, Extent: p.extent(payloadStart)}
			}
		} else {
			if start.text == "=" && len(b.Statements) > 0 {
				p.failAbsent(absentInStatement, "=", start.span)
			}
			s.Kind = "expr"
			s.Value = p.expr(0)
		}
		s.Extent = p.extent(start.span)
		b.Statements = append(b.Statements, s)
		p.accept(";")
	}
	b.Extent = p.extent(open.span)
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
	// chainPipe is the |> token of the last pipe in this unparenthesised
	// chain, zero when there is none. A pipe chain beside a binary operator is
	// a syntax error, so `a + b |> f()` can never be read as `a + f(b)`.
	var chainPipe Span
	switch {
	case (start.text == "fn" && p.anonymousCallableHead()) || (start.text == "effect" && p.peek().text == "fn"):
		p.failAbsent(absentInStatement, start.text, start.span)
	case start.text == "scope":
		e.Kind = "scope"
		e.Then = p.block()
	case start.text == "fork":
		e.Kind = "fork"
		e.Left = p.expr(prefixPrecedence)
		chainPipe = p.lastPipe
	case start.kind == "integer":
		e.Kind = "integer"
		e.Text = start.text
	case start.text == "-":
		e.Kind = "unary"
		e.Name = start.text
		e.Left = p.expr(prefixPrecedence)
		e.Left.allowMinLiteral = e.Left.Kind == "integer" && isMinI64Literal(e.Left.Text)
		chainPipe = p.lastPipe
	case start.text == "run":
		e.Kind = "run"
		e.Left = p.expr(prefixPrecedence)
		chainPipe = p.lastPipe
	case start.text == "if":
		e.Kind = "if"
		p.noConstruct++
		subjectList := p.subjectList
		p.subjectList = false
		e.Left = p.expr(0)
		p.subjectList = subjectList
		p.noConstruct--
		e.Then = p.block()
		if !p.accept("else") {
			p.failAbsent(absentInStatement, "if", start.span)
		}
		e.Else = p.block()
	case start.text == "match":
		e.Kind = "match"
		// Subjects are the ordered Args of the match, so every shared child
		// traversal observes them once and in evaluation order.
		p.noConstruct++
		subjectList := p.subjectList
		p.subjectList = true
		e.Args = append(e.Args, p.expr(0))
		for p.accept(",") {
			e.Args = append(e.Args, p.expr(0))
		}
		p.subjectList = subjectList
		p.noConstruct--
		p.expect("{")
		// The arm list is delimited by the match braces, so arm bodies of a
		// match nested in another header parse constructors unprotected.
		protected := p.noConstruct
		p.noConstruct, p.subjectList = 0, false
		for !p.accept("}") {
			cells := [][]*MatchPattern{p.patternCell()}
			for p.accept(",") {
				cells = append(cells, p.patternCell())
			}
			pattern := cells[0][0]
			p.expect("=>")
			var body *Block
			if p.peek().text == "fail" {
				start := p.take()
				name := p.name()
				statement := &Statement{Kind: "fail", Name: name.text, NameSpan: name.span, Span: start.span}
				if p.peek().text == "(" {
					p.take()
					if !p.accept(")") {
						statement.Payload = p.expr(0)
						p.expect(")")
					}
				} else if p.peek().text == "{" {
					payloadStart := p.peek().span
					statement.Payload = &Expr{Kind: "payload", Fields: p.fieldValues(), Span: start.span, Extent: p.extent(payloadStart)}
				}
				statement.Extent = p.extent(start.span)
				body = &Block{Statements: []*Statement{statement}, Extent: statement.Extent}
			} else if p.peek().text == "{" {
				body = p.block()
			} else {
				value := p.expr(0)
				body = &Block{Statements: []*Statement{{Kind: "expr", Value: value, Span: value.Span, Extent: value.Extent}}, Extent: value.Extent}
			}
			e.Arms = append(e.Arms, &MatchArm{Patterns: cells, Body: body, Span: pattern.Span, Extent: p.extent(pattern.Span)})
			p.accept(",")
			p.accept(";")
		}
		p.noConstruct, p.subjectList = protected, subjectList
	case start.kind == "string":
		e.Kind = "string"
		_ = json.Unmarshal([]byte(start.text), &e.Text)
	case start.text == "true" || start.text == "false":
		e.Kind = "bool"
		e.Text = start.text
	case start.text == "void":
		e.Kind = "void"
	case start.text == "(":
		if p.accept(")") {
			close := p.tokens[p.at-1]
			p.failSpan(Span{Offset: start.span.Offset, Length: close.span.Offset + close.span.Length - start.span.Offset, Line: start.span.Line, Column: start.span.Column}, "use void instead of () for a no-value expression")
		} else {
			// Parentheses delimit their contents, so constructor braces inside
			// an if/match header are unambiguous again.
			protected := p.noConstruct
			p.noConstruct = 0
			e = p.expr(0)
			p.noConstruct = protected
			p.expect(")")
		}
	case start.kind == "name":
		e.Kind = "name"
		e.Name = start.text
	default:
		p.fail(start, "expected expression")
	}
	// piped is the value on the left of a |> whose call has not been read yet.
	// Until the call's argument list ends, the right side is a static name path
	// with optional type arguments; the intrinsic method names are ordinary
	// members there, so `x |> M.timeout(...)` is a plain call.
	var piped *Expr
	var pipeSpan Span
	// from is where the current e begins: the primary's first token, or the
	// callee's own first token once a |> has been read, so the callee's extent
	// does not swallow its subject. The call that follows begins at start again.
	from := start.span
	for {
		e.Extent = p.extent(from)
		if piped != nil {
			if p.accept(".") {
				m := p.memberName()
				e = &Expr{Kind: "member", Name: m.text, Left: e, Span: m.span}
				continue
			}
			if t := p.peek().text; t != "(" && t != "<" {
				p.failSpan(e.Span, "the right side of |> must be a call; write "+expressionName(e)+"()")
			}
		}
		if p.peek().text == "|>" {
			pipeSpan = p.take().span
			chainPipe = pipeSpan
			head := p.take()
			if head.kind != "name" || pipeHeadReserved[head.text] {
				p.fail(head, "|> takes a function or operation name followed by arguments")
			}
			piped, e, from = e, &Expr{Kind: "name", Name: head.text, Span: head.span}, head.span
			continue
		}
		if (p.noConstruct == 0 || p.constructorBrace()) && p.peek().text == "{" && (e.Kind == "name" || e.Kind == "member") {
			e = &Expr{Kind: "construct", Left: e, Fields: p.fieldValues(), Span: e.Span}
			continue
		}
		if (e.Kind == "name" || e.Kind == "member") && p.genericApplicationAhead() {
			if e.constructorType != nil {
				p.fail(p.peek(), "constructor application arguments may be supplied only once")
			}
			name := expressionName(e)
			p.expectGenericOpen()
			t := &sourceType{Application: name, Span: e.Span}
			for {
				argument := p.typ()
				t.ApplicationArguments = append(t.ApplicationArguments, argument)
				t.ApplicationArgumentTypes = append(t.ApplicationArgumentTypes, p.types[argument])
				if len(t.ApplicationArguments) > 8 {
					p.fail(p.peek(), "at most eight template arguments are supported")
				}
				if p.acceptGenericClose() {
					break
				}
				p.expect(",")
			}
			e.constructorType = t
			p.types[t.display()] = t
			continue
		}
		if p.peek().text == "(" {
			if e.Kind != "name" && e.Kind != "member" && e.Kind != "void" {
				p.fail(p.peek(), "only named functions and service methods are callable")
			}
			p.take()
			call := &Expr{Kind: "call", Left: e, Span: e.Span}
			protected := p.noConstruct
			p.noConstruct = 0
			for !p.accept(")") {
				if p.peek().kind == "name" && p.at+1 < len(p.tokens) && p.tokens[p.at+1].text == ":" {
					field := p.name()
					p.expect(":")
					value := p.expr(0)
					call.Fields = append(call.Fields, FieldValue{Name: field.text, Value: value, Span: field.span, Label: field.span})
					call.Args = append(call.Args, value)
				} else {
					call.Args = append(call.Args, p.expr(0))
				}
				if !p.accept(",") {
					p.expect(")")
					break
				}
			}
			p.noConstruct = protected
			if piped != nil {
				call.Args, call.PipeSpan, piped = append([]*Expr{piped}, call.Args...), pipeSpan, nil
				from = start.span
			}
			e = call
			continue
		}
		if p.accept(".") {
			method := p.memberName()
			if method.text == "orFail" {
				p.expect("(")
				p.expect(")")
				e = &Expr{Kind: "orFail", Left: e, Span: method.span}
			} else if method.text == "timeout" {
				p.expect("(")
				arg := p.expr(0)
				p.expect(")")
				e = &Expr{Kind: "timeout", Left: e, Right: arg, Span: method.span}
			} else if method.text == "as" && p.peek().text == "<" {
				// A native type assertion: value.as<T>() keeps Go's match
				// status beside the adapted value, like v, ok := value.(T).
				p.expectGenericOpen()
				t := p.typ()
				p.expectGenericClose()
				p.expect("(")
				p.expect(")")
				e = &Expr{Kind: "hostAssert", Name: t, Left: e, Span: method.span}
			} else if method.text == "provide" || method.text == "catch" {
				if method.text == "provide" && p.accept("(") {
					layer := p.name()
					p.expect(")")
					e = &Expr{Kind: "provideLayer", Name: layer.text, NameSpan: layer.span, Left: e, Span: method.span}
					continue
				}
				p.expectGenericOpen()
				t := p.name()
				p.expectGenericClose()
				p.expect("(")
				arg := p.expr(0)
				p.expect(")")
				e = &Expr{Kind: method.text, Name: t.text, NameSpan: t.span, Left: e, Right: arg, Span: method.span}
			} else {
				e = &Expr{Kind: "member", Name: method.text, Left: e, Span: method.span}
			}
			continue
		}
		precedence := binaryPrecedence[p.peek().text]
		if precedence == 0 || precedence < min {
			break
		}
		if chainPipe.Length > 0 {
			p.failSpan(chainPipe, pipeBesideOperator(p.peek().text))
		}
		if p.peek().text == "*" {
			p.markMultiplication(p.at)
		}
		op := p.take()
		right := p.expr(precedence + 1)
		if p.lastPipe.Length > 0 {
			p.failSpan(p.lastPipe, pipeBesideOperator(op.text))
		}
		e = &Expr{Kind: "binary", Name: op.text, Left: e, Right: right, Span: op.span}
	}
	e.Extent = p.extent(start.span)
	p.lastPipe = chainPipe
	return e
}

// genericApplicationAhead distinguishes a type-argument postfix from the
// newly admitted less-than comparison. It speculates with the same bounded
// type grammar used by the real parser, so a later declaration cannot supply
// a closing `>` and no speculative type or formatter facts escape.
func (p *parser) genericApplicationAhead() bool {
	if p.peek().text != "<" {
		return false
	}
	candidate := *p
	// The probe only needs the type grammar's syntax state. Fresh maps keep
	// unrelated declarations from becoming per-comparison work and prevent
	// speculative aliases or formatter facts from sharing parser state.
	candidate.types = map[string]*sourceType{}
	candidate.genericAngles = map[int]bool{}
	candidate.multiplications = map[int]bool{}
	return func() (ok bool) {
		defer func() {
			if value := recover(); value != nil {
				if _, fault := value.(syntaxFault); !fault {
					panic(value)
				}
				ok = false
			}
		}()
		candidate.expectGenericOpen()
		argumentCount := 0
		for {
			candidate.typ()
			argumentCount++
			if argumentCount > 8 {
				return false
			}
			if candidate.acceptGenericClose() {
				next := candidate.peek().text
				return next == "(" || next == "{" || next == "."
			}
			if !candidate.accept(",") {
				return false
			}
		}
	}()
}

// binaryPrecedence is every binary operator and its binding strength. An
// unparenthesised pipe chain beside any of them is refused (pipeBesideOperator),
// and TestEveryBinaryOperatorIsClassifiedAgainstThePipe fails for an operator
// added here without a decision about it.
var binaryPrecedence = map[string]int{
	"==": 1, "<": 1, "<=": 1, ">": 1, ">=": 1,
	"+": 2, "-": 2,
	"*": 3, "/": 3, "%": 3,
}

// prefixPrecedence binds the operand of a prefix form (unary -, run, fork)
// tighter than every binary operator, so `-a / b` is `(-a) / b` as in Go and
// JavaScript. Truncating division distinguishes the two at the signed minimum.
const prefixPrecedence = 4

// pipeHeadReserved are the words that start another expression form, so they
// cannot name the function on the right of a |>.
var pipeHeadReserved = map[string]bool{"run": true, "fork": true, "if": true, "match": true, "scope": true, "true": true, "false": true, "void": true}

func pipeBesideOperator(op string) string {
	return "a |> chain beside " + op + " is ambiguous; parenthesise: (a |> f()) " + op + " b or a " + op + " (b |> f())"
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
	if p.at+2 < len(p.tokens) && p.tokens[p.at+2].text == ":" {
		return true
	}
	// An empty or shorthand-first payload can look like a control body. It is
	// a payload exactly when the payload grammar itself accepts the braces and
	// the token after them can only follow a constructor. The decision depends
	// only on the brace position and the protected header context, so each
	// brace is decided once even when an enclosing payload is decided first.
	if decided, ok := p.payloadBraces[p.at]; ok {
		return decided
	}
	start := p.at
	end, ok := p.speculatePayload()
	decided := ok && p.constructorFollows(end)
	if p.payloadBraces == nil {
		p.payloadBraces = map[int]bool{}
	}
	p.payloadBraces[start] = decided
	return decided
}

// speculatePayload parses a payload with fieldValues, then restores the
// parser, and reports the token index after its closing brace.
func (p *parser) speculatePayload() (end int, ok bool) {
	at, depth, noConstruct, subjectList, types, genericAngles, multiplications := p.at, p.depth, p.noConstruct, p.subjectList, p.types, p.genericAngles, p.multiplications
	p.types = maps.Clone(types)
	p.genericAngles = maps.Clone(genericAngles)
	p.multiplications = maps.Clone(multiplications)
	defer func() {
		if value := recover(); value != nil {
			if _, fault := value.(syntaxFault); !fault {
				panic(value)
			}
			end, ok = 0, false
		}
		p.at, p.depth, p.noConstruct, p.subjectList, p.types, p.genericAngles, p.multiplications = at, depth, noConstruct, subjectList, types, genericAngles, multiplications
	}()
	p.fieldValues()
	return p.at, true
}

// constructorFollows reports whether the token after a candidate payload's
// closing brace can only follow a constructor in a control header: the
// control body's own brace, or a comma delimiting match subjects. A control
// body is never followed by a subject comma; its arms or else come first.
func (p *parser) constructorFollows(index int) bool {
	if index >= len(p.tokens) {
		return false
	}
	switch p.tokens[index].text {
	case "{":
		return true
	case ",":
		return p.subjectList
	}
	return false
}

// patternCell parses one subject's alternatives: `A.X { x } | A.Y { x }`.
func (p *parser) patternCell() []*MatchPattern {
	cell := []*MatchPattern{p.pattern()}
	for p.accept("|") {
		cell = append(cell, p.pattern())
	}
	return cell
}

func (p *parser) pattern() *MatchPattern {
	first := p.name()
	pattern := &MatchPattern{TypeName: first.text, Bindings: map[string]string{}, Span: first.span, Segments: []Span{first.span}}
	if p.accept(".") {
		variant := p.name()
		pattern.VariantName = variant.text
		pattern.Span.Length = variant.span.Offset + variant.span.Length - pattern.Span.Offset
		pattern.Segments = append(pattern.Segments, variant.span)
		if p.accept(".") {
			pattern.TypeName += "." + variant.text
			variant = p.name()
			pattern.VariantName = variant.text
			pattern.Span.Length = variant.span.Offset + variant.span.Length - pattern.Span.Offset
			pattern.Segments = append(pattern.Segments, variant.span)
		}
	}
	if p.peek().text == "{" {
		payloadStart := p.take().span
		seen := map[string]bool{}
		for !p.accept("}") {
			field := p.name()
			if seen[field.text] {
				p.fail(field, "duplicate pattern field "+field.text)
			}
			seen[field.text] = true
			binding := field
			if p.accept(":") {
				binding = p.name()
			}
			pattern.Bindings[field.text] = binding.text
			pattern.Names = append(pattern.Names, PatternName{Field: field.text, Name: binding.text, FieldSpan: field.span, NameSpan: binding.span})
			if !p.accept(",") && !p.accept(";") {
				p.expect("}")
				break
			}
		}
		pattern.Payload = p.extent(payloadStart)
	}
	pattern.Extent = p.extent(first.span)
	return pattern
}

func expressionName(e *Expr) string {
	if e == nil {
		return ""
	}
	if e.Kind == "name" {
		return e.Name
	}
	if e.Kind == "member" {
		if parent := expressionName(e.Left); parent != "" {
			return parent + "." + e.Name
		}
	}
	return ""
}
