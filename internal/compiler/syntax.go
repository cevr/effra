package compiler

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Span offsets and columns are UTF-8 byte based, scoped to a semantic revision.
type Span struct {
	Offset int `json:"offset"`
	Length int `json:"length"`
	Line   int `json:"line"`
	Column int `json:"column"`
}
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Span    Span   `json:"span"`
}
type token struct {
	text string
	kind string
	span Span
}
type syntaxFault struct{ diagnostic Diagnostic }
type parser struct {
	tokens []token
	at     int
}
type Param struct {
	Name string
	Type string
	Span Span
}
type Function struct {
	Name     string
	Params   []Param
	Return   string
	Effect   bool
	Errors   []string
	Services []string
	Body     *Block
	Span     Span
}
type Service struct {
	Name    string
	Methods []*Function
	Span    Span
}
type Provider struct {
	Name    string
	Service string
	Methods []*Function
	Span    Span
}
type Program struct {
	Errors    map[string]Span
	Services  []*Service
	Providers []*Provider
	Functions []*Function
}
type Block struct{ Statements []*Statement }
type Statement struct {
	Kind  string
	Name  string
	Value *Expr
	Span  Span
}
type Expr struct {
	Kind  string
	Name  string
	Text  string
	Args  []*Expr
	Left  *Expr
	Right *Expr
	Then  *Block
	Else  *Block
	Span  Span
	Type  ValueType
}

func lex(source string) ([]token, []Diagnostic) {
	var out []token
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
		if ch == ' ' || ch == '\r' || ch == '\t' {
			i++
			column++
			continue
		}
		if ch == '/' && i+1 < len(source) && source[i+1] == '/' {
			for i < len(source) && source[i] != '\n' {
				i++
				column++
			}
			continue
		}
		kind := "symbol"
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' {
			kind = "name"
			i++
			for i < len(source) && ((source[i] >= 'a' && source[i] <= 'z') || (source[i] >= 'A' && source[i] <= 'Z') || (source[i] >= '0' && source[i] <= '9') || source[i] == '_') {
				i++
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
				return nil, []Diagnostic{{"EF001", "unterminated string", Span{start, i - start, l, c}}}
			}
			i++
			var decoded string
			if err := json.Unmarshal([]byte(source[start:i]), &decoded); err != nil {
				return nil, []Diagnostic{{"EF001", "strings use JSON escapes", Span{start, i - start, l, c}}}
			}
		} else if i+1 < len(source) && (source[i:i+2] == "->" || source[i:i+2] == "==") {
			i += 2
		} else if strings.ContainsRune("{}():,;.+<>=", rune(ch)) {
			i++
		} else {
			return nil, []Diagnostic{{"EF001", fmt.Sprintf("unsupported character %q", ch), Span{start, 1, l, c}}}
		}
		column += i - start
		out = append(out, token{source[start:i], kind, Span{start, i - start, l, c}})
	}
	out = append(out, token{"<eof>", "eof", Span{len(source), 0, line, column}})
	return out, nil
}
func parse(source string) (program *Program, diagnostics []Diagnostic) {
	tokens, diagnostics := lex(source)
	if len(diagnostics) > 0 {
		return nil, diagnostics
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
	p := parser{tokens: tokens}
	program = &Program{Errors: map[string]Span{}}
	for p.peek().kind != "eof" {
		switch p.peek().text {
		case "error":
			p.take()
			name := p.name()
			if _, exists := program.Errors[name.text]; exists {
				p.fail(name, "duplicate error "+name.text)
			}
			program.Errors[name.text] = name.span
			p.accept(";")
		case "service":
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
		case "impl":
			p.take()
			name := p.name()
			p.expect("for")
			service := p.name()
			p.expect("{")
			v := &Provider{Name: name.text, Service: service.text, Span: name.span}
			for !p.accept("}") {
				v.Methods = append(v.Methods, p.function(true))
			}
			program.Providers = append(program.Providers, v)
		case "effect", "fn":
			program.Functions = append(program.Functions, p.function(true))
		default:
			p.fail(p.peek(), "expected error, service, impl, or function declaration")
		}
	}
	return program, nil
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
	panic(syntaxFault{Diagnostic{"EF002", message, v.span}})
}
func (p *parser) typ() string {
	if p.accept("(") {
		p.expect(")")
		return "()"
	}
	return p.name().text
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
	effect := p.accept("effect")
	p.expect("fn")
	name := p.name()
	p.expect("(")
	f := &Function{Name: name.text, Effect: effect, Span: name.span}
	for !p.accept(")") {
		param := p.name()
		p.expect(":")
		f.Params = append(f.Params, Param{param.text, p.typ(), param.span})
		if !p.accept(",") {
			p.expect(")")
			break
		}
	}
	p.expect("->")
	f.Return = p.typ()
	if p.accept("throws") {
		f.Errors = p.row()
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
	p.expect("{")
	b := &Block{}
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
	start := p.take()
	e := &Expr{Span: start.span}
	switch {
	case start.text == "run":
		e.Kind = "run"
		e.Left = p.expr(3)
	case start.text == "if":
		e.Kind = "if"
		e.Left = p.expr(0)
		e.Then = p.block()
		p.expect("else")
		e.Else = p.block()
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
		if p.peek().text == "(" {
			if e.Kind != "name" && e.Kind != "member" {
				p.fail(p.peek(), "only named functions and service methods are callable")
			}
			p.take()
			call := &Expr{Kind: "call", Left: e, Span: e.Span}
			for !p.accept(")") {
				call.Args = append(call.Args, p.expr(0))
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
			if method.text == "provide" || method.text == "catch" {
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
