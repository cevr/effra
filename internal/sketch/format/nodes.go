package format

import (
	"strings"

	"effra.local/prototype/internal/sketch/syntax"
)

// ---------------------------------------------------------------- file and declarations

func (p *printer) printFile(f *syntax.File) {
	p.token(f.Package, "package")
	p.space()
	p.ident(f.Name)
	for _, d := range f.Decls {
		p.newline()
		p.decl(d)
	}
}

func (p *printer) ident(x *syntax.Ident) { p.token(x.NamePos, x.Name) }

func (p *printer) decl(d syntax.Decl) {
	switch d := d.(type) {
	case *syntax.ImportDecl:
		p.importDecl(d)
	case *syntax.VarDecl:
		p.varDecl(d)
	case *syntax.TypeDecl:
		p.token(d.Type, "type")
		p.space()
		p.ident(d.Name)
		if d.TypeParams != nil {
			p.fieldList(d.TypeParams, "[", "]", false)
		}
		p.space()
		p.expr(d.Spec)
	case *syntax.FuncDecl:
		p.token(d.Func, "func")
		p.space()
		if d.Recv != nil {
			p.fieldList(d.Recv, "(", ")", true)
			p.space()
		}
		p.ident(d.Name)
		if d.TypeParams != nil {
			p.fieldList(d.TypeParams, "[", "]", false)
		}
		p.signature(d.Type)
		if d.Body != nil {
			if p.blockOneLine(d.Body) {
				p.sep() // consecutive one-line functions align their bodies (gofmt)
			} else {
				p.space()
			}
			p.block(d.Body, true)
		}
	case *syntax.WireDecl:
		p.token(d.Wire, "wire")
		p.space()
		p.ident(d.Name)
		if d.Uses != nil {
			p.space()
			p.uses(d.Uses)
		}
		p.space()
		p.wireBody(d.Body)
	}
}

// group prints the parenthesised specs of an import or var declaration,
// one per line.
func (p *printer) group(lparen, rparen syntax.Pos, n int, spec func(i int)) {
	p.space()
	p.token(lparen, "(")
	if n == 0 && p.line(lparen) == p.line(rparen) {
		p.token(rparen, ")")
		return
	}
	p.indent++
	p.noBlank = true
	for i := 0; i < n; i++ {
		if i == 0 {
			p.newlineNoBlank()
		} else {
			p.newline()
		}
		spec(i)
	}
	p.closeBody(rparen, ")")
}

// closeBody ends a multi-line body: comments before the closer at the
// body's indentation, then the closer on its own line.
func (p *printer) closeBody(closePos syntax.Pos, close string) {
	p.newline()
	p.flushComments(closePos)
	p.indent--
	p.newlineNoBlank()
	p.token(closePos, close)
}

func (p *printer) importDecl(d *syntax.ImportDecl) {
	p.token(d.Import, "import")
	if !d.Lparen.IsValid() {
		p.space()
		p.importSpec(d.Specs[0])
		return
	}
	p.group(d.Lparen, d.Rparen, len(d.Specs), func(i int) { p.importSpec(d.Specs[i]) })
}

func (p *printer) importSpec(s *syntax.ImportSpec) {
	switch s.Kind {
	case syntax.ImportNamed:
		p.ident(s.Name)
		p.space()
	case syntax.ImportDot:
		p.token(s.NamePos, ".")
		p.space()
	case syntax.ImportGo:
		p.token(s.NamePos, "go")
		p.space()
	}
	p.basicLit(s.Path)
}

func (p *printer) varDecl(d *syntax.VarDecl) {
	p.token(d.Var, "var")
	if !d.Lparen.IsValid() {
		p.space()
		p.varSpec(d.Specs[0], false, false)
		return
	}
	typed := false
	for _, s := range d.Specs {
		typed = typed || s.Type != nil
	}
	p.group(d.Lparen, d.Rparen, len(d.Specs), func(i int) { p.varSpec(d.Specs[i], true, typed) })
}

// varSpec prints `names [Type] [= values]`; in a group the names, types and
// `=` align in columns.
func (p *printer) varSpec(s *syntax.VarSpec, aligned, typeColumn bool) {
	p.identList(s.Names, !aligned)
	if aligned && (typeColumn || s.Type != nil || len(s.Values) > 0) {
		p.sep()
	}
	if s.Type != nil {
		if !aligned {
			p.space()
		}
		p.typ(s.Type)
	}
	if len(s.Values) > 0 {
		if aligned && typeColumn {
			p.sep()
		} else if !aligned || s.Type != nil {
			p.space()
		}
		p.token(s.Assign, "=")
		p.assignRHS(s.Values)
	}
}

// identList prints grouped names; a line break after a comma follows the
// source. With indent, the names after the break take one continuation
// level (an unparenthesised var spec, the first parameter of a list that
// does not break, a switch binding); without it they take the level the
// enclosing list or block gives, as go/printer's identList(…, indent).
func (p *printer) identList(list []*syntax.Ident, indent bool) {
	broken := false
	for i, x := range list {
		if i > 0 {
			p.synth(",")
			if p.gap(x.Pos(), true) && !broken && indent {
				broken = true
				p.indent++
			}
		}
		p.ident(x)
	}
	if broken {
		p.indent--
	}
}

// assignRHS prints the right side of an assignment or var spec after its
// operator; a line break after the operator follows the source.
func (p *printer) assignRHS(values []syntax.Expr) {
	broken := p.gap(values[0].Pos(), true)
	if broken {
		p.indent++
	}
	p.exprList(values, 1)
	if broken {
		p.indent--
	}
}

// ---------------------------------------------------------------- lists

// list prints n comma-separated items between an opener and a closer. The
// line breaks after the opener, between items and before the closer follow
// the source; with any of them, the items indent one level and a closer on
// its own line follows a comma.
func (p *printer) list(openPos syntax.Pos, open string, items []syntax.Node, item func(i int), closePos syntax.Pos, close string) {
	if openPos.IsValid() {
		p.token(openPos, open)
	} else {
		p.synth(open)
	}
	n := len(items)
	if n == 0 {
		p.token(closePos, close)
		return
	}
	broken := p.line(items[0].Pos()) > p.lineOr(openPos, items[0].Pos()) || p.line(closePos) > p.line(items[n-1].End())
	for i := 1; i < n && !broken; i++ {
		broken = p.line(items[i].Pos()) > p.line(items[i-1].End())
	}
	if broken {
		p.indent++
	}
	savedBroken := p.listBroken
	p.listBroken = broken
	for i := range items {
		if i == 0 {
			if p.gap(items[0].Pos(), false) {
				p.noBlank = true
			}
		} else {
			p.synth(",")
			p.gap(items[i].Pos(), true)
		}
		item(i)
	}
	p.listBroken = savedBroken
	if broken {
		if p.breakBefore(closePos) {
			p.synth(",")
			p.closeBody(closePos, close)
			return
		}
		p.indent--
	}
	p.token(closePos, close)
}

func (p *printer) lineOr(pos, fallback syntax.Pos) int {
	if pos.IsValid() {
		return p.line(pos)
	}
	return p.line(fallback)
}

// exprList prints an unbracketed expression list; a line break after a
// comma follows the source and indents the rest one level.
func (p *printer) exprList(list []syntax.Expr, depth int) {
	broken := false
	for i, x := range list {
		if i > 0 {
			p.synth(",")
			if p.gap(x.Pos(), true) && !broken {
				broken = true
				p.indent++
			}
		}
		p.expr1(x, depth)
	}
	if broken {
		p.indent--
	}
}

func nodes[T syntax.Node](list []T) []syntax.Node {
	out := make([]syntax.Node, len(list))
	for i, x := range list {
		out[i] = x
	}
	return out
}

// ---------------------------------------------------------------- signatures and fields

// fieldList prints a parameter, result, receiver or type parameter list.
func (p *printer) fieldList(l *syntax.FieldList, open, close string, trailingComma bool) {
	p.list(l.Opening, open, nodes(l.List), func(i int) {
		f := l.List[i]
		p.identList(f.Names, !p.listBroken)
		if f.Type != nil {
			if len(f.Names) > 0 {
				p.space()
			}
			p.typ(f.Type)
		}
	}, l.Closing, close)
}

func (p *printer) signature(t *syntax.FuncType) {
	p.fieldList(t.Params, "(", ")", true)
	if r := t.Results; r != nil {
		p.space()
		if r.Opening.IsValid() {
			p.fieldList(r, "(", ")", true)
		} else {
			p.typ(r.List[0].Type)
		}
	}
	if t.Uses != nil {
		p.space()
		p.uses(t.Uses)
	}
}

func (p *printer) uses(u *syntax.UsesClause) {
	p.token(u.Uses, "uses")
	if u.Lparen.IsValid() {
		p.space()
		p.list(u.Lparen, "(", nodes(u.Names), func(i int) { p.typ(u.Names[i]) }, u.Rparen, ")")
		return
	}
	p.space()
	p.exprList(u.Names, 1)
}

// ---------------------------------------------------------------- types

func (p *printer) typ(x syntax.Expr) { p.expr1(x, 1) }

func (p *printer) structType(t *syntax.StructType) {
	fl := t.Fields
	field := func(f *syntax.Field, aligned bool) {
		p.identList(f.Names, false)
		if aligned {
			p.sep()
		} else {
			p.space()
		}
		p.typ(f.Type)
	}
	if p.oneLine(fl.Opening, fl.Closing, nodes(fl.List)...) {
		p.token(t.Struct, "struct")
		p.token(fl.Opening, "{")
		if len(fl.List) == 1 {
			p.space()
			field(fl.List[0], false)
			p.space()
		}
		p.token(fl.Closing, "}")
		return
	}
	p.token(t.Struct, "struct")
	p.space()
	p.openBody(fl.Opening)
	for i, f := range fl.List {
		p.bodyLine(i)
		field(f, true)
	}
	p.closeBody(fl.Closing, "}")
}

// oneLine reports whether a braced body stays on one line: the source has
// it on one line with at most one element (an explicit `;` between two
// elements becomes a line break), and nothing inside it prints on several
// lines.
func (p *printer) oneLine(lbrace, rbrace syntax.Pos, contents ...syntax.Node) bool {
	if p.line(lbrace) != p.line(rbrace) || len(contents) > 1 {
		return false
	}
	for _, n := range contents {
		if forcesBreak(n) {
			return false
		}
	}
	return true
}

// forcesBreak reports whether n prints on several lines even when its
// source is on one line: statement blocks, clauses, machines, and bodies
// with more than one element.
func forcesBreak(n syntax.Node) bool {
	if _, ok := n.(*syntax.BlockStmt); ok {
		return true // an element that is a block is a bare block statement
	}
	forced := false
	syntax.Inspect(n, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.IfStmt, *syntax.ForStmt, *syntax.RangeStmt, *syntax.SwitchStmt, *syntax.TypeSwitchStmt,
			*syntax.SelectStmt, *syntax.CatchExpr, *syntax.MachineType, *syntax.LabeledStmt:
			forced = true
		case *syntax.BlockStmt:
			list := nonEmpty(n.List)
			if len(list) > 1 {
				forced = true
			}
			for _, s := range list {
				if _, ok := s.(*syntax.BlockStmt); ok {
					forced = true
				}
			}
		case *syntax.StructType:
			forced = forced || len(n.Fields.List) > 1
		case *syntax.InterfaceType:
			forced = forced || len(n.Methods) > 1
		case *syntax.WireBody:
			forced = forced || len(n.Entries) > 1
		case *syntax.VarDecl:
			forced = forced || n.Lparen.IsValid() && len(n.Specs) > 0
		}
		return !forced
	})
	return forced
}

func (p *printer) openBody(lbrace syntax.Pos) {
	p.token(lbrace, "{")
	p.indent++
	p.noBlank = true
}

func (p *printer) bodyLine(i int) {
	if i == 0 {
		p.newlineNoBlank()
	} else {
		p.newline()
	}
}

func (p *printer) interfaceType(t *syntax.InterfaceType) {
	n := len(t.Methods)
	oneLine := p.oneLine(t.Lbrace, t.Rbrace, nodes(t.Methods)...)
	if t.Union != nil {
		n = 1
		oneLine = p.oneLine(t.Lbrace, t.Rbrace)
		for _, x := range t.Union {
			oneLine = oneLine && !forcesBreak(x)
		}
	}
	method := func(m *syntax.Field) {
		p.ident(m.Names[0])
		p.signature(m.Type.(*syntax.FuncType))
	}
	if oneLine {
		p.token(t.Interface, "interface")
		p.token(t.Lbrace, "{")
		switch {
		case t.Union != nil:
			p.space()
			p.union(t.Union)
			p.space()
		case n == 1:
			p.space()
			method(t.Methods[0])
			p.space()
		}
		p.token(t.Rbrace, "}")
		return
	}
	p.token(t.Interface, "interface")
	p.space()
	p.openBody(t.Lbrace)
	if t.Union != nil {
		p.bodyLine(0)
		p.union(t.Union)
	}
	for i, m := range t.Methods {
		p.bodyLine(i)
		method(m)
	}
	p.closeBody(t.Rbrace, "}")
}

// union prints `A | B`; a line break after `|` follows the source.
func (p *printer) union(list []syntax.Expr) {
	broken := false
	for i, x := range list {
		if i > 0 {
			p.space()
			p.synth("|")
			if p.gap(x.Pos(), true) && !broken {
				broken = true
				p.indent++
			}
		}
		p.typ(x)
	}
	if broken {
		p.indent--
	}
}

func (p *printer) machineType(t *syntax.MachineType) {
	p.token(t.Machine, "machine")
	p.list(t.Lbrack, "[", []syntax.Node{t.State, t.Signal}, func(i int) {
		p.typ([]syntax.Expr{t.State, t.Signal}[i])
	}, t.Rbrack, "]")
	p.space()
	p.openBody(t.Lbrace)
	for i, e := range t.Entries {
		p.bodyLine(i)
		p.token(e.NamePos, e.Name)
		p.sep()
		p.expr(e.Value)
	}
	p.closeBody(t.Rbrace, "}")
}

func (p *printer) wireBody(b *syntax.WireBody) {
	entry := func(e *syntax.WireEntry, aligned bool) {
		if e.Kind == syntax.WireReplace {
			p.token(e.Replace, "replace")
			p.space()
		}
		p.typ(e.Key)
		for _, k := range e.Keys {
			p.synth(",")
			p.space()
			p.typ(k)
		}
		if e.Kind == syntax.WireEmbed {
			return
		}
		if aligned {
			p.sep()
		} else {
			p.space()
		}
		p.token(e.Assign, "=")
		p.space()
		p.expr(e.Value)
	}
	if p.oneLine(b.Lbrace, b.Rbrace, nodes(b.Entries)...) {
		p.token(b.Lbrace, "{")
		if len(b.Entries) == 1 {
			p.space()
			entry(b.Entries[0], false)
			p.space()
		}
		p.token(b.Rbrace, "}")
		return
	}
	p.openBody(b.Lbrace)
	for i, e := range b.Entries {
		p.bodyLine(i)
		entry(e, true)
	}
	p.closeBody(b.Rbrace, "}")
}

// ---------------------------------------------------------------- statements

// block prints a braced statement list. A function body, function literal,
// trailing block or go block stays on one line when the source has it on
// one line with at most one statement (gofmt's function-body rule); a
// statement block is always multi-line.
func (p *printer) block(b *syntax.BlockStmt, oneLineOK bool) {
	list := nonEmpty(b.List)
	if oneLineOK && p.oneLine(b.Lbrace, b.Rbrace, nodes(list)...) {
		p.token(b.Lbrace, "{")
		if len(list) == 1 {
			p.space()
			p.stmt(list[0])
			p.space()
		}
		p.token(b.Rbrace, "}")
		return
	}
	p.openBody(b.Lbrace)
	p.stmtList(list)
	p.closeBody(b.Rbrace, "}")
}

// blockOneLine reports whether a function-body-like block prints on one line.
func (p *printer) blockOneLine(b *syntax.BlockStmt) bool {
	return p.oneLine(b.Lbrace, b.Rbrace, nodes(nonEmpty(b.List))...)
}

// indexDepth is the depth of an index element: gofmt's depth+1, except for
// a union of types (`retry.On[Failed | timeout.Expired]`), which keeps its
// blanks as an error row does.
func indexDepth(x syntax.Expr, depth int) int {
	if b, ok := x.(*syntax.BinaryExpr); ok && b.Op == syntax.OR {
		return 1
	}
	return depth + 1
}

func nonEmpty(list []syntax.Stmt) []syntax.Stmt {
	out := list[:0:0]
	for _, s := range list {
		if _, ok := s.(*syntax.EmptyStmt); !ok {
			out = append(out, s)
		}
	}
	return out
}

func (p *printer) stmtList(list []syntax.Stmt) {
	for i, s := range nonEmpty(list) {
		p.bodyLine(i)
		p.stmt(s)
	}
}

func (p *printer) stmt(s syntax.Stmt) {
	switch s := s.(type) {
	case *syntax.ExprStmt:
		p.expr(s.X)
	case *syntax.DeclStmt:
		p.varDecl(s.Decl)
	case *syntax.LabeledStmt:
		p.indent--
		p.ident(s.Label)
		p.token(s.Colon, ":")
		p.indent++
		switch body := s.Stmt.(type) {
		case *syntax.EmptyStmt:
			// gofmt: an explicit empty statement stays, on its own line,
			// so the statement after it stays unlabelled.
			if !body.Implicit {
				p.newline()
				p.token(body.Semicolon, ";")
			}
		default:
			p.newline()
			p.stmt(s.Stmt)
		}
	case *syntax.SendStmt:
		p.expr(s.Chan)
		p.space()
		p.token(s.Arrow, "<-")
		p.space()
		p.expr(s.Value)
	case *syntax.IncDecStmt:
		p.expr(s.X)
		p.token(s.TokPos, s.Tok.String())
	case *syntax.AssignStmt:
		p.exprList(s.Lhs, 1)
		p.space()
		p.token(s.TokPos, s.Tok.String())
		p.assignRHS(s.Rhs)
	case *syntax.DeferStmt:
		p.token(s.Defer, "defer")
		p.space()
		p.stmt(s.Stmt)
	case *syntax.ReturnStmt:
		p.token(s.Return, "return")
		if len(s.Results) > 0 {
			p.space()
			p.exprList(s.Results, 1)
		}
	case *syntax.BranchStmt:
		p.token(s.TokPos, s.Tok.String())
		if s.Label != nil {
			p.space()
			p.ident(s.Label)
		}
	case *syntax.BlockStmt:
		p.block(s, false)
	case *syntax.IfStmt:
		p.ifStmt(s)
	case *syntax.SwitchStmt:
		p.token(s.Switch, "switch")
		p.header(s.Init, func() {
			if s.Tag != nil {
				p.expr(s.Tag)
			}
		}, s.Tag != nil)
		p.caseClauses(s.Lbrace, s.Clauses, s.Rbrace, 1)
	case *syntax.TypeSwitchStmt:
		p.token(s.Switch, "switch")
		p.header(s.Init, func() {
			if len(s.Binds) > 0 {
				p.identList(s.Binds, true)
				p.space()
				p.token(s.Define, ":=")
				p.space()
			}
			for i, g := range s.Subjects {
				if i > 0 {
					p.synth(",")
					p.space()
				}
				p.expr(g)
			}
		}, true)
		p.caseClauses(s.Lbrace, s.Clauses, s.Rbrace, len(s.Subjects))
	case *syntax.SelectStmt:
		p.token(s.Select, "select")
		p.space()
		p.clauses(s.Lbrace, len(s.Clauses), func(i int) syntax.Pos { return s.Clauses[i].Case }, func(i int) {
			c := s.Clauses[i]
			if c.Comm == nil {
				p.token(c.Case, "default")
			} else {
				p.token(c.Case, "case")
				p.space()
				p.stmt(c.Comm)
			}
			p.token(c.Colon, ":")
		}, func(i int) []syntax.Stmt { return s.Clauses[i].Body }, s.Rbrace)
	case *syntax.ForStmt:
		p.token(s.For, "for")
		switch {
		case s.Init != nil || s.Post != nil:
			p.space()
			if s.Init != nil {
				p.stmt(s.Init)
			}
			p.synth(";")
			if s.Cond != nil {
				p.space()
				p.expr(s.Cond)
			}
			p.synth(";")
			if s.Post != nil {
				p.space()
				p.stmt(s.Post)
			}
		case s.Cond != nil:
			p.space()
			p.expr(s.Cond)
		}
		p.space()
		p.block(s.Body, false)
	case *syntax.RangeStmt:
		p.token(s.For, "for")
		p.space()
		if s.Key != nil {
			p.expr(s.Key)
			if s.Value != nil {
				p.synth(",")
				p.space()
				p.expr(s.Value)
			}
			p.space()
			p.token(s.TokPos, s.Tok.String())
			p.space()
		}
		p.token(s.Range, "range")
		p.space()
		p.expr(s.X)
		p.space()
		p.block(s.Body, false)
	}
}

// header prints `[init; ]x ` of a switch.
func (p *printer) header(init syntax.Stmt, x func(), hasX bool) {
	p.space()
	if init != nil {
		p.stmt(init)
		p.synth(";")
		if hasX {
			p.space()
		}
	}
	x()
	if init != nil || hasX {
		p.space()
	}
}

func (p *printer) ifStmt(s *syntax.IfStmt) {
	p.token(s.If, "if")
	p.space()
	if s.Init != nil {
		p.stmt(s.Init)
		p.synth(";")
		p.space()
	}
	p.expr(s.Cond)
	p.space()
	p.block(s.Body, false)
	if s.Else != nil {
		p.space()
		p.token(s.ElsePos, "else")
		p.space()
		if e, ok := s.Else.(*syntax.IfStmt); ok {
			p.ifStmt(e)
		} else {
			p.block(s.Else.(*syntax.BlockStmt), false)
		}
	}
}

// clauses prints the clause list of a switch, select or catch: each case
// at the statement's indentation, its body one level in (D6 comment rule).
func (p *printer) clauses(lbrace syntax.Pos, n int, casePos func(i int) syntax.Pos, head func(i int), body func(i int) []syntax.Stmt, rbrace syntax.Pos) {
	base := p.indent
	p.token(lbrace, "{")
	p.noBlank = true
	for i := 0; i < n; i++ {
		if i > 0 {
			p.newline()
			p.flushClauseComments(casePos(i))
			p.indent = base
			p.newline()
		} else {
			p.newlineNoBlank()
		}
		head(i)
		p.indent = base + 1
		p.stmtList(body(i))
	}
	if n > 0 {
		p.newline()
		p.flushClauseComments(rbrace)
	}
	p.indent = base
	p.newline()
	p.flushComments(rbrace)
	p.newlineNoBlank()
	p.token(rbrace, "}")
}

func (p *printer) caseClauses(lbrace syntax.Pos, list []*syntax.CaseClause, rbrace syntax.Pos, subjects int) {
	p.clauses(lbrace, len(list), func(i int) syntax.Pos { return list[i].Case }, func(i int) {
		c := list[i]
		if c.List == nil {
			p.token(c.Case, "default")
		} else {
			p.token(c.Case, "case")
			p.space()
			p.exprList(c.List, 1)
		}
		p.token(c.Colon, ":")
	}, func(i int) []syntax.Stmt { return list[i].Body }, rbrace)
}

// ---------------------------------------------------------------- expressions

func (p *printer) expr(x syntax.Expr) { p.expr1(x, 1) }

func (p *printer) basicLit(x *syntax.BasicLit) { p.tokenEnd(x.ValuePos, x.Value, x.ValueEnd) }

func (p *printer) expr1(x syntax.Expr, depth int) {
	switch x := x.(type) {
	case *syntax.Ident:
		p.ident(x)
	case *syntax.BasicLit:
		p.basicLit(x)
	case *syntax.BinaryExpr:
		// A line break after an operator indents the rest of the binary
		// expression once (gofmt).
		indented := binaryBreaks(p, x)
		if indented {
			p.indent++
		}
		p.binary(x, depth)
		if indented {
			p.indent--
		}
	case *syntax.UnaryExpr:
		p.token(x.OpPos, x.Op.String())
		p.expr1(x.X, depth)
	case *syntax.StarExpr:
		p.token(x.Star, "*")
		p.expr1(x.X, 1)
	case *syntax.ParenExpr:
		p.token(x.Lparen, "(")
		broken := p.gap(x.X.Pos(), false)
		if broken {
			p.noBlank = true
			p.indent++
		}
		p.expr1(x.X, max(depth-1, 1))
		if broken {
			p.indent--
		}
		p.gap(x.Rparen, false)
		p.token(x.Rparen, ")")
	case *syntax.SelectorExpr:
		p.expr1(x.X, depth)
		p.synth(".")
		if p.breakBefore(x.Sel.Pos()) {
			p.indent++
			p.newline()
			p.ident(x.Sel)
			p.indent--
		} else {
			p.ident(x.Sel)
		}
	case *syntax.IndexExpr:
		p.expr1(x.X, depth)
		p.list(x.Lbrack, "[", nodes(x.Indices), func(i int) { p.expr1(x.Indices[i], indexDepth(x.Indices[i], depth)) }, x.Rbrack, "]")
	case *syntax.SliceExpr:
		p.expr1(x.X, depth)
		p.token(x.Lbrack, "[")
		p.slice(x, depth)
		p.token(x.Rbrack, "]")
	case *syntax.TypeGuard:
		p.expr1(x.X, depth)
		p.synth(".")
		p.token(x.Lparen, "(")
		p.synth("type")
		p.token(x.Rparen, ")")
	case *syntax.CallExpr:
		p.expr1(x.Fun, depth)
		argDepth := depth
		if len(x.Args) > 1 {
			argDepth++
		}
		p.list(x.Lparen, "(", nodes(x.Args), func(i int) {
			p.expr1(x.Args[i], argDepth)
			if i == len(x.Args)-1 && x.Ellipsis.IsValid() {
				p.token(x.Ellipsis, "...")
			}
		}, x.Rparen, ")")
		if x.Trailing != nil {
			p.space()
			p.block(x.Trailing, true)
		}
	case *syntax.CompositeLit:
		if x.Type != nil {
			p.expr1(x.Type, depth)
		}
		p.list(x.Lbrace, "{", nodes(x.Elts), func(i int) { p.element(x.Elts[i]) }, x.Rbrace, "}")
	case *syntax.KeyValueExpr:
		p.element(x)
	case *syntax.FuncLit:
		p.funcType(x.Type)
		p.space()
		p.block(x.Body, true)
	case *syntax.TryExpr:
		p.token(x.Try, "try")
		p.space()
		p.expr1(x.X, depth)
	case *syntax.CatchExpr:
		p.expr1(x.X, depth)
		p.space()
		p.token(x.Catch, "catch")
		if x.Bind != nil {
			p.space()
			p.ident(x.Bind)
		}
		p.space()
		p.clauses(x.Lbrace, len(x.Arms), func(i int) syntax.Pos { return x.Arms[i].Case }, func(i int) {
			a := x.Arms[i]
			p.token(a.Case, "case")
			p.space()
			p.exprList(a.Types, 1)
			p.token(a.Colon, ":")
		}, func(i int) []syntax.Stmt { return x.Arms[i].Body }, x.Rbrace)
	case *syntax.WireLit:
		p.token(x.Wire, "wire")
		p.wireBody(x.Body)
	case *syntax.GoExpr:
		p.token(x.Go, "go")
		p.space()
		if x.Block != nil {
			p.block(x.Block, true)
		} else {
			p.expr1(x.Call, depth)
		}
	case *syntax.Ellipsis:
		p.token(x.Ellipsis, "...")
		p.typ(x.Elt)
	case *syntax.ArrayType:
		p.token(x.Lbrack, "[")
		if x.Len != nil {
			p.expr(x.Len)
		}
		p.synth("]")
		p.typ(x.Elt)
	case *syntax.MapType:
		p.token(x.Map, "map")
		p.synth("[")
		p.typ(x.Key)
		p.synth("]")
		p.typ(x.Value)
	case *syntax.ChanType:
		switch x.Dir {
		case syntax.ChanRecv:
			p.token(x.Begin, "<-")
			p.synth("chan")
		case syntax.ChanSend:
			p.token(x.Begin, "chan")
			p.token(x.Arrow, "<-")
		default:
			p.token(x.Begin, "chan")
		}
		p.space()
		p.typ(x.Value)
	case *syntax.FuncType:
		p.funcType(x)
	case *syntax.StructType:
		p.structType(x)
	case *syntax.InterfaceType:
		p.interfaceType(x)
	case *syntax.MachineType:
		p.machineType(x)
	case *syntax.ErrorRow:
		p.token(x.Error, "error")
		p.token(x.Lbrack, "[")
		p.union(x.Types)
		p.token(x.Rbrack, "]")
	case *syntax.TupleExpr:
		p.list(x.Lparen, "(", nodes(x.Elts), func(i int) { p.typ(x.Elts[i]) }, x.Rparen, ")")
	}
}

func (p *printer) funcType(t *syntax.FuncType) {
	if t.Func.IsValid() {
		p.token(t.Func, "func")
	}
	p.signature(t)
}

// element prints a composite literal element. A keyed element that starts
// its line aligns its value with the keyed elements of the lines around it.
func (p *printer) element(x syntax.Expr) {
	kv, ok := x.(*syntax.KeyValueExpr)
	if !ok {
		p.expr(x)
		return
	}
	startsLine := !p.started || p.pendingNL > 0
	p.expr(kv.Key)
	p.token(kv.Colon, ":")
	if p.gap(kv.Value.Pos(), false) {
		p.indent++
		p.expr(kv.Value)
		p.indent--
		return
	}
	if startsLine {
		p.sep()
	} else {
		p.space()
	}
	p.expr(kv.Value)
}

// slice prints the indices of a slice expression with gofmt's blanks: around
// the colons when more than one index is present and one is binary.
func (p *printer) slice(x *syntax.SliceExpr, depth int) {
	indices := []syntax.Expr{x.Low, x.High}
	if x.Slice3 {
		indices = append(indices, x.Max)
	}
	blanks := false
	if depth <= 1 {
		count, binary := 0, false
		for _, e := range indices {
			if e != nil {
				count++
				_, isBinary := e.(*syntax.BinaryExpr)
				binary = binary || isBinary
			}
		}
		blanks = count > 1 && binary
	}
	for i, e := range indices {
		if i > 0 {
			if indices[i-1] != nil && blanks {
				p.space()
			}
			p.synth(":")
			if e != nil && blanks {
				p.space()
			}
		}
		if e != nil {
			p.expr1(e, depth+1)
		}
	}
}

// ---------------------------------------------------------------- binary expressions (go/printer's spacing)

// binary prints x with gofmt's blanks: an operator of a precedence at or
// above the expression's cutoff is written without blanks (x*y + z).
func (p *printer) binary(x *syntax.BinaryExpr, depth int) {
	prec := x.Op.Precedence()
	blank := prec < p.cutoff(x, depth)
	p.binaryOperand(x.X, depth+diffPrec(x.X, prec))
	if blank {
		p.space()
	}
	p.token(x.OpPos, x.Op.String())
	p.gap(x.Y.Pos(), blank)
	p.binaryOperand(x.Y, depth+1)
}

func (p *printer) binaryOperand(x syntax.Expr, depth int) {
	if b, ok := x.(*syntax.BinaryExpr); ok {
		p.binary(b, depth)
		return
	}
	p.expr1(x, depth)
}

// binaryBreaks reports whether the source breaks a line after an operator
// of the binary expression x or its binary operands.
func binaryBreaks(p *printer, x *syntax.BinaryExpr) bool {
	if p.line(x.Y.Pos()) > p.line(x.OpPos) {
		return true
	}
	for _, e := range []syntax.Expr{x.X, x.Y} {
		if b, ok := e.(*syntax.BinaryExpr); ok && binaryBreaks(p, b) {
			return true
		}
	}
	return false
}

func (p *printer) cutoff(e *syntax.BinaryExpr, depth int) int {
	has4, has5, maxProblem := p.walkBinary(e)
	if maxProblem > 0 {
		return maxProblem + 1
	}
	if has4 && has5 {
		if depth == 1 {
			return 5
		}
		return 4
	}
	if depth == 1 {
		return 6
	}
	return 4
}

func diffPrec(x syntax.Expr, prec int) int {
	b, ok := x.(*syntax.BinaryExpr)
	if !ok || prec != b.Op.Precedence() {
		return 1
	}
	return 0
}

// walkBinary is go/printer's walkBinary, memoised per node: cutoff asks it for
// every operator of a chain, which would be quadratic in the chain's length.
func (p *printer) walkBinary(e *syntax.BinaryExpr) (has4, has5 bool, maxProblem int) {
	if r, ok := p.walked[e]; ok {
		return r.has4, r.has5, r.maxProblem
	}
	defer func() { p.walked[e] = walkResult{has4, has5, maxProblem} }()
	switch e.Op.Precedence() {
	case 4:
		has4 = true
	case 5:
		has5 = true
	}
	if l, ok := e.X.(*syntax.BinaryExpr); ok && l.Op.Precedence() >= e.Op.Precedence() {
		h4, h5, mp := p.walkBinary(l)
		has4, has5, maxProblem = has4 || h4, has5 || h5, max(maxProblem, mp)
	}
	switch r := e.Y.(type) {
	case *syntax.BinaryExpr:
		if r.Op.Precedence() > e.Op.Precedence() {
			h4, h5, mp := p.walkBinary(r)
			has4, has5, maxProblem = has4 || h4, has5 || h5, max(maxProblem, mp)
		}
	case *syntax.StarExpr:
		if e.Op == syntax.QUO {
			maxProblem = 5
		}
	case *syntax.UnaryExpr:
		switch e.Op.String() + r.Op.String() {
		case "/*", "&&", "&^":
			maxProblem = 5
		case "++", "--":
			maxProblem = max(maxProblem, 4)
		}
	}
	return
}

var _ = strings.TrimSpace
