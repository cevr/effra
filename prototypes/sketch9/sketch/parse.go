package sketch

import (
	"fmt"
	"strings"
)

// Diagnostic is one checker or parser message.
type Diagnostic struct {
	Pos  Pos
	Code string
	Msg  string
}

func (d Diagnostic) Format(file string) string {
	return fmt.Sprintf("%s:%d:%d: %s %s", file, d.Pos.Line, d.Pos.Col, d.Code, d.Msg)
}

type parseError struct{ d Diagnostic }

type parser struct {
	toks []token
	i    int
	// noLit stops `ident {` from parsing as a composite literal in if and
	// switch headers, as in Go.
	noLit bool
}

// Parse parses one file. Syntax errors carry code S001.
func Parse(name, src string) (f *File, err error) {
	toks, lerr := lex(src)
	if lerr != nil {
		where, msg, _ := strings.Cut(lerr.Error(), ": ")
		return nil, fmt.Errorf("%s:%s: S001 %s", name, where, msg)
	}
	p := &parser{toks: toks}
	defer func() {
		if r := recover(); r != nil {
			pe, ok := r.(parseError)
			if !ok {
				panic(r)
			}
			err = fmt.Errorf("%s", pe.d.Format(name))
		}
	}()
	f = p.file()
	f.Name = name
	return f, nil
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}
	return t
}

func (p *parser) fail(pos Pos, format string, args ...any) {
	panic(parseError{Diagnostic{pos, "S001", fmt.Sprintf(format, args...)}})
}

func (p *parser) is(text string) bool {
	t := p.peek()
	return (t.kind == tOp || t.kind == tIdent) && t.text == text
}

func (p *parser) expect(text string) token {
	t := p.next()
	if (t.kind != tOp && t.kind != tIdent) || t.text != text {
		p.fail(t.pos, "expected %q, found %q", text, t.text)
	}
	return t
}

func (p *parser) ident() token {
	t := p.next()
	if t.kind != tIdent || keywords[t.text] {
		p.fail(t.pos, "expected a name, found %q", t.text)
	}
	return t
}

func (p *parser) skipSemis() {
	for p.peek().kind == tSemi {
		p.next()
	}
}

func (p *parser) endStmt() {
	t := p.peek()
	if t.kind == tSemi {
		p.next()
		return
	}
	if t.kind == tEOF || p.is("}") {
		return
	}
	p.fail(t.pos, "expected end of statement, found %q", t.text)
}

func (p *parser) file() *File {
	f := &File{}
	p.skipSemis()
	p.expect("package")
	f.Package = p.ident().text
	p.endStmt()
	p.skipSemis()
	for p.is("import") {
		p.next()
		t := p.next()
		if t.kind != tString {
			p.fail(t.pos, "import takes one quoted path")
		}
		f.Imports = append(f.Imports, strings.Trim(t.text, "\"`"))
		p.endStmt()
		p.skipSemis()
	}
	for p.peek().kind != tEOF {
		switch {
		case p.is("type"):
			f.Types = append(f.Types, p.typeDecl())
		case p.is("func"):
			f.Funcs = append(f.Funcs, p.funcDecl())
		case p.is("wire"):
			pos := p.next().pos
			name := p.ident().text
			f.Wires = append(f.Wires, &WireDecl{Pos: pos, Name: name, Entries: p.wireBody()})
		default:
			t := p.peek()
			p.fail(t.pos, "unsupported declaration starting with %q", t.text)
		}
		p.endStmt()
		p.skipSemis()
	}
	return f
}

func (p *parser) typeDecl() *TypeDecl {
	pos := p.expect("type").pos
	d := &TypeDecl{Pos: pos, Name: p.ident().text}
	switch {
	case p.is("struct"):
		p.next()
		p.expect("{")
		p.skipSemis()
		for !p.is("}") {
			ft := p.ident()
			d.Fields = append(d.Fields, Param{Pos: ft.pos, Name: ft.text, Type: p.typeName()})
			p.endStmt()
			p.skipSemis()
		}
		p.expect("}")
	case p.is("interface"):
		p.next()
		p.expect("{")
		p.skipSemis()
		// A union is `interface{ A | B }`; a capability lists methods.
		if p.peek().kind == tIdent && p.toks[p.i+1].text != "(" {
			d.Kind = kindUnion
			d.Variants = append(d.Variants, p.ident().text)
			for p.is("|") {
				p.next()
				d.Variants = append(d.Variants, p.ident().text)
			}
			p.skipSemis()
		} else {
			d.Kind = kindCapability
			for !p.is("}") {
				nt := p.ident()
				s := &Sig{Pos: nt.pos, Name: nt.text}
				p.signature(s)
				d.Methods = append(d.Methods, s)
				p.endStmt()
				p.skipSemis()
			}
		}
		p.expect("}")
	default:
		p.fail(p.peek().pos, "only struct and interface types are supported")
	}
	return d
}

func (p *parser) typeName() string {
	t := p.next()
	if t.kind != tIdent {
		p.fail(t.pos, "expected a type, found %q", t.text)
	}
	if p.is(".") {
		p.next()
		return t.text + "." + p.ident().text
	}
	return t.text
}

func (p *parser) qname() QName {
	t := p.ident()
	if p.is(".") {
		p.next()
		return QName{Pos: t.pos, Pkg: t.text, Name: p.ident().text}
	}
	return QName{Pos: t.pos, Name: t.text}
}

// signature parses params, results and an optional uses clause into s.
func (p *parser) signature(s *Sig) {
	p.expect("(")
	for !p.is(")") {
		nt := p.ident()
		s.Params = append(s.Params, Param{Pos: nt.pos, Name: nt.text, Type: p.typeName()})
		if !p.is(")") {
			p.expect(",")
		}
	}
	p.expect(")")
	switch {
	case p.is("("):
		p.next()
		s.Result = p.typeName()
		p.expect(",")
		p.errorRow(s)
		p.expect(")")
	case p.is("error"):
		p.errorRow(s)
	case p.peek().kind == tIdent && !keywords[p.peek().text]:
		s.Result = p.typeName()
	}
	if p.is("uses") {
		p.next()
		s.Uses = append(s.Uses, p.qname())
		for p.is(",") {
			p.next()
			s.Uses = append(s.Uses, p.qname())
		}
	}
}

func (p *parser) errorRow(s *Sig) {
	p.expect("error")
	p.expect("[")
	s.Fallible = true
	s.Errors = append(s.Errors, p.qname())
	for p.is("|") {
		p.next()
		s.Errors = append(s.Errors, p.qname())
	}
	p.expect("]")
}

func (p *parser) funcDecl() *FuncDecl {
	pos := p.expect("func").pos
	f := &FuncDecl{}
	if p.is("(") {
		p.next()
		rt := p.ident()
		recv := &Param{Pos: rt.pos, Type: rt.text}
		if !p.is(")") {
			recv.Name, recv.Type = rt.text, p.typeName()
		}
		p.expect(")")
		f.Recv = recv
	}
	nt := p.ident()
	f.Pos, f.Name = pos, nt.text
	p.signature(&f.Sig)
	f.Body = p.block()
	return f
}

func (p *parser) wireBody() []WireEntry {
	p.expect("{")
	p.skipSemis()
	var entries []WireEntry
	for !p.is("}") {
		ct := p.ident()
		p.expect("=")
		entries = append(entries, WireEntry{Pos: ct.pos, Cap: ct.text, Value: p.expr()})
		p.endStmt()
		p.skipSemis()
	}
	p.expect("}")
	return entries
}

func (p *parser) block() *Block {
	b := &Block{Pos: p.expect("{").pos}
	saved := p.noLit
	p.noLit = false
	p.skipSemis()
	for !p.is("}") {
		b.Stmts = append(b.Stmts, p.stmt())
		p.endStmt()
		p.skipSemis()
	}
	p.expect("}")
	p.noLit = saved
	return b
}

func (p *parser) stmt() Stmt {
	t := p.peek()
	switch {
	case p.is("return"):
		p.next()
		if p.peek().kind == tSemi || p.is("}") {
			return &ReturnStmt{Pos: t.pos}
		}
		return &ReturnStmt{Pos: t.pos, Value: p.expr()}
	case p.is("if"):
		return p.ifStmt()
	case p.is("switch"):
		return p.switchStmt()
	case t.kind == tIdent && !keywords[t.text] && p.toks[p.i+1].text == ":=":
		p.next()
		p.next()
		return &DefineStmt{Pos: t.pos, Name: t.text, Value: p.expr()}
	}
	return &ExprStmt{Pos: t.pos, X: p.expr()}
}

func (p *parser) ifStmt() Stmt {
	pos := p.expect("if").pos
	p.noLit = true
	cond := p.expr()
	p.noLit = false
	s := &IfStmt{Pos: pos, Cond: cond, Then: p.block()}
	if p.is("else") {
		p.next()
		s.Else = p.block()
	}
	return s
}

func (p *parser) switchStmt() Stmt {
	pos := p.expect("switch").pos
	bind := p.ident().text
	p.expect(":=")
	p.noLit = true
	subject := p.postfix(p.primary(), true)
	p.noLit = false
	p.expect(".")
	p.expect("(")
	p.expect("type")
	p.expect(")")
	s := &TypeSwitchStmt{Pos: pos, Bind: bind, Subject: subject}
	p.expect("{")
	p.skipSemis()
	for p.is("case") {
		cpos := p.next().pos
		c := TypeCase{Pos: cpos, Types: []QName{p.qname()}}
		for p.is(",") {
			p.next()
			c.Types = append(c.Types, p.qname())
		}
		p.expect(":")
		c.Body = &Block{Pos: cpos}
		p.skipSemis()
		for !p.is("case") && !p.is("}") {
			c.Body.Stmts = append(c.Body.Stmts, p.stmt())
			p.endStmt()
			p.skipSemis()
		}
		s.Cases = append(s.Cases, c)
	}
	p.expect("}")
	return s
}

// expr: ['try'] binary ['catch' arms]
func (p *parser) expr() Expr {
	if p.is("try") {
		pos := p.next().pos
		return &TryExpr{Pos: pos, X: p.catchExpr()}
	}
	return p.catchExpr()
}

func (p *parser) catchExpr() Expr {
	x := p.binary(0)
	if !p.is("catch") {
		return x
	}
	pos := p.next().pos
	c := &CatchExpr{Pos: pos, X: x}
	p.expect("{")
	p.skipSemis()
	for p.is("case") {
		apos := p.next().pos
		arm := CatchArm{Pos: apos, Types: []QName{p.qname()}}
		for p.is(",") {
			p.next()
			arm.Types = append(arm.Types, p.qname())
		}
		p.expect(":")
		p.skipSemis()
		arm.Value = p.expr()
		p.skipSemis()
		c.Arms = append(c.Arms, arm)
	}
	p.expect("}")
	return c
}

var precedence = map[string]int{"||": 1, "&&": 2, "==": 3, "!=": 3, "<": 3, "<=": 3, ">": 3, ">=": 3, "+": 4, "-": 4, "*": 5, "/": 5}

func (p *parser) binary(min int) Expr {
	x := p.postfix(p.primary(), false)
	for {
		t := p.peek()
		prec, ok := precedence[t.text]
		if t.kind != tOp || !ok || prec <= min {
			return x
		}
		p.next()
		x = &Binary{Pos: t.pos, Op: t.text, L: x, R: p.binary(prec)}
	}
}

func (p *parser) primary() Expr {
	t := p.next()
	switch {
	case t.kind == tString || t.kind == tInt || t.kind == tDuration:
		return &BasicLit{Pos: t.pos, Kind: t.kind, Text: t.text}
	case t.kind == tIdent && t.text == "wire":
		return &WireLit{Pos: t.pos, Entries: p.wireBody()}
	case t.kind == tIdent && !keywords[t.text]:
		if p.is("{") && !p.noLit && isTypeName(t.text) {
			return p.compositeLit(t)
		}
		return &Ident{Pos: t.pos, Name: t.text}
	case t.kind == tOp && t.text == "(":
		x := p.expr()
		p.expect(")")
		return x
	}
	p.fail(t.pos, "unexpected %q", t.text)
	return nil
}

// isTypeName reports whether a name can start a composite literal: types are
// capitalised in this subset.
func isTypeName(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }

func (p *parser) compositeLit(t token) Expr {
	lit := &CompositeLit{Pos: t.pos, Type: t.text}
	p.expect("{")
	p.skipSemis()
	for !p.is("}") {
		if p.peek().kind == tIdent && p.toks[p.i+1].text == ":" {
			key := p.next().text
			p.next()
			lit.Elems = append(lit.Elems, KeyValue{Key: key, Value: p.expr()})
		} else {
			lit.Positional = true
			lit.Elems = append(lit.Elems, KeyValue{Value: p.expr()})
		}
		if !p.is("}") {
			p.expect(",")
		}
		p.skipSemis()
	}
	p.expect("}")
	return lit
}

// postfix parses selectors and calls. stopAtType leaves `.(type)` for the
// type switch.
func (p *parser) postfix(x Expr, stopAtType bool) Expr {
	for {
		switch {
		case p.is("."):
			if stopAtType && p.toks[p.i+1].text == "(" {
				return x
			}
			p.next()
			sel := p.ident()
			x = &Selector{Pos: sel.pos, X: x, Sel: sel.text}
		case p.is("("):
			pos := p.next().pos
			call := &Call{Pos: pos, Fun: x}
			for !p.is(")") {
				call.Args = append(call.Args, p.expr())
				if !p.is(")") {
					p.expect(",")
				}
			}
			p.expect(")")
			if p.is("{") && takesBlock(x) {
				call.Block = p.block()
			}
			x = call
		default:
			return x
		}
	}
}

// takesBlock reports whether a callee takes a trailing block in this subset.
func takesBlock(fun Expr) bool {
	switch f := fun.(type) {
	case *Ident:
		return f.Name == "with"
	case *Selector:
		if pkg, ok := f.X.(*Ident); ok {
			return pkg.Name == "timeout" && f.Sel == "After"
		}
	}
	return false
}
