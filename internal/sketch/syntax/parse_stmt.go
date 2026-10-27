package syntax

// ---------------------------------------------------------------- blocks and statements

func (p *parser) parseBlock() *BlockStmt {
	p.enter()
	defer p.leave()
	b := &BlockStmt{Lbrace: p.want(LBRACE)}
	saved, savedGuard := p.exprLev, p.guardOK
	p.exprLev, p.guardOK = 0, false
	b.List = p.parseStmtList()
	p.exprLev, p.guardOK = saved, savedGuard
	if p.tok.tok != RBRACE {
		p.errorf("expected \"}\", found %s", p.found())
	}
	b.Rbrace = p.tok.pos
	p.next()
	return b
}

// parseStmtList parses statements up to a closing brace or the next case.
func (p *parser) parseStmtList() []Stmt {
	var list []Stmt
	for p.tok.tok != RBRACE && p.tok.tok != EOF && p.tok.tok != CASE && p.tok.tok != DEFAULT {
		if p.tok.tok == SEMICOLON {
			p.next()
			continue
		}
		list = append(list, p.parseStmt())
		switch p.tok.tok {
		case SEMICOLON:
			p.next()
		case RBRACE:
		default:
			p.errorf("expected end of statement, found %s", p.found())
		}
	}
	return list
}

func (p *parser) parseStmt() Stmt {
	p.enter()
	defer p.leave()
	switch p.tok.tok {
	case VAR:
		return &DeclStmt{Decl: p.parseVarDecl(nil)}
	case RETURN:
		s := &ReturnStmt{Return: p.tok.pos}
		p.next()
		if p.tok.tok != SEMICOLON && p.tok.tok != RBRACE && p.tok.tok != CASE && p.tok.tok != DEFAULT {
			s.Results = p.parseExprList()
		}
		return s
	case BREAK, CONTINUE:
		s := &BranchStmt{TokPos: p.tok.pos, Tok: p.tok.tok}
		p.next()
		if p.tok.tok == IDENT {
			s.Label = p.parseIdent()
		}
		return s
	case IF:
		return p.parseIfStmt()
	case SWITCH:
		return p.parseSwitchStmt()
	case SELECT:
		return p.parseSelectStmt()
	case FOR:
		return p.parseForStmt()
	case DEFER:
		pos := p.tok.pos
		p.next()
		if !p.startsSimpleStmt() {
			p.errorf("defer takes a simple statement, found %s", p.found())
		}
		return &DeferStmt{Defer: pos, Stmt: p.parseSimpleStmt(simplePlain)}
	case LBRACE:
		return p.parseBlock()
	case TYPE:
		p.errorf("a type is declared at package level")
	case CASE, DEFAULT:
		p.errorf("%s outside a switch, select or catch", p.found())
	case IMPORT, PACKAGE, WIRE:
		if p.tok.tok != WIRE || p.peek(1).tok != LBRACE {
			p.errorf("unexpected %s in a statement list", p.found())
		}
	case IDENT:
		if msg, ok := removed[p.tok.lit]; ok {
			p.errorf("%s", msg)
		}
	}
	return p.parseSimpleStmt(simpleLabel)
}

// startsSimpleStmt reports whether the current token can start a simple
// statement (an expression, send, increment or assignment).
func (p *parser) startsSimpleStmt() bool {
	switch p.tok.tok {
	case IDENT, INT, DURATION, STRING, RUNE, LPAREN, LBRACK, MUL, ARROW, ADD, SUB, NOT, XOR, AND,
		FUNC, MAP, CHAN, STRUCT, INTERFACE, TRY, GO, WIRE:
		return true
	}
	return false
}

type simpleMode int

const (
	simplePlain  simpleMode = iota
	simpleLabel             // a label may start here
	simpleRange             // a for header: range may follow := or =
	simpleSwitch            // a switch header: x.(type) is admitted
)

// rangeClause is the partial result of `k, v := range x` in a for header.
type rangeClause struct {
	key, value Expr
	tokPos     Pos
	tok        Token
	rangePos   Pos
	x          Expr
}

func (*rangeClause) Pos() Pos  { return NoPos }
func (*rangeClause) End() Pos  { return NoPos }
func (*rangeClause) stmtNode() {}

func (p *parser) parseSimpleStmt(mode simpleMode) Stmt {
	if mode == simpleSwitch {
		saved := p.guardOK
		p.guardOK = true
		defer func() { p.guardOK = saved }()
	}
	lhs := p.parseExprList()
	switch p.tok.tok {
	case DEFINE, ASSIGN, ADD_ASSIGN, SUB_ASSIGN, MUL_ASSIGN, QUO_ASSIGN, REM_ASSIGN,
		AND_ASSIGN, OR_ASSIGN, XOR_ASSIGN, SHL_ASSIGN, SHR_ASSIGN, AND_NOT_ASSIGN:
		pos, tok := p.tok.pos, p.tok.tok
		p.next()
		if mode == simpleRange && p.tok.tok == RANGE && (tok == DEFINE || tok == ASSIGN) {
			if len(lhs) > 2 {
				p.errorAt(lhs[2].Pos(), 1, "range permits at most two iteration variables")
			}
			r := &rangeClause{key: lhs[0], tokPos: pos, tok: tok, rangePos: p.tok.pos}
			if len(lhs) == 2 {
				r.value = lhs[1]
			}
			p.next()
			r.x = p.parseExpr()
			return r
		}
		if tok != DEFINE && tok != ASSIGN && len(lhs) > 1 {
			p.errorAt(pos, 2, "%s takes one operand on each side", tok)
		}
		s := &AssignStmt{Lhs: lhs, TokPos: pos, Tok: tok, Rhs: p.parseExprList()}
		if tok == DEFINE {
			for _, x := range lhs {
				if _, ok := x.(*Ident); !ok {
					p.errorAt(x.Pos(), int(x.End()-x.Pos()), "non-name on the left side of :=")
				}
			}
		}
		return s
	}
	if len(lhs) > 1 {
		p.errorf("expected := or = after an expression list, found %s", p.found())
	}
	switch p.tok.tok {
	case COLON:
		if id, ok := lhs[0].(*Ident); ok && mode == simpleLabel {
			colon := p.tok.pos
			p.next()
			if p.tok.tok == RBRACE {
				return &LabeledStmt{Label: id, Colon: colon, Stmt: &EmptyStmt{Semicolon: p.tok.pos}}
			}
			for p.tok.tok == SEMICOLON && p.tok.lit == ";" {
				p.next()
			}
			return &LabeledStmt{Label: id, Colon: colon, Stmt: p.parseStmt()}
		}
	case ARROW:
		arrow := p.tok.pos
		p.next()
		return &SendStmt{Chan: lhs[0], Arrow: arrow, Value: p.parseExpr()}
	case INC, DEC:
		s := &IncDecStmt{X: lhs[0], TokPos: p.tok.pos, Tok: p.tok.tok}
		p.next()
		return s
	}
	return &ExprStmt{X: lhs[0]}
}

func (p *parser) parseExprList() []Expr {
	list := []Expr{p.parseExpr()}
	for p.got(COMMA) {
		list = append(list, p.parseExpr())
	}
	return list
}

// headerSemicolon consumes the explicit semicolon of a control clause. An
// inserted one means the header broke across lines.
func (p *parser) headerSemicolon() bool {
	if p.tok.tok != SEMICOLON {
		return false
	}
	if p.tok.lit == "\n" {
		p.errorf("expected \"{\" after the header, found newline")
	}
	p.next()
	return true
}

func (p *parser) parseIfStmt() *IfStmt {
	s := &IfStmt{If: p.want(IF)}
	saved := p.exprLev
	p.exprLev = -1
	if p.tok.tok == LBRACE {
		p.errorf("missing condition in if statement")
	}
	var first Stmt
	if p.tok.tok != SEMICOLON {
		first = p.parseSimpleStmt(simplePlain)
	}
	if p.headerSemicolon() {
		s.Init = first
		if p.tok.tok == LBRACE {
			p.errorf("missing condition in if statement")
		}
		first = p.parseSimpleStmt(simplePlain)
	}
	cond, ok := first.(*ExprStmt)
	if !ok {
		p.errorAt(first.Pos(), 1, "expected a condition, found a statement")
	}
	s.Cond = cond.X
	p.exprLev = saved
	if p.tok.tok != LBRACE {
		p.errorf("expected \"{\", found %s", p.found())
	}
	s.Body = p.parseBlock()
	s.ElsePos = NoPos
	if p.tok.tok == ELSE {
		s.ElsePos = p.tok.pos
		p.next()
		switch p.tok.tok {
		case IF:
			s.Else = p.parseIfStmt()
		case LBRACE:
			s.Else = p.parseBlock()
		default:
			p.errorf("else must be followed by if or a block, found %s", p.found())
		}
	}
	return s
}

func (p *parser) parseSwitchStmt() Stmt {
	pos := p.want(SWITCH)
	saved := p.exprLev
	p.exprLev = -1
	guardsBefore := len(p.guards)
	var init, header Stmt
	if p.tok.tok != LBRACE {
		if p.tok.tok != SEMICOLON {
			header = p.parseSimpleStmt(simpleSwitch)
		}
		if p.headerSemicolon() {
			init, header = header, nil
			if p.tok.tok != LBRACE {
				header = p.parseSimpleStmt(simpleSwitch)
			}
		}
	}
	p.exprLev = saved
	guards := append([]*TypeGuard(nil), p.guards[guardsBefore:]...)
	p.guards = p.guards[:guardsBefore]
	if init != nil {
		// A guard in the init statement is never a subject.
		for _, g := range guards {
			if g.Pos() < init.End() {
				p.errorAt(g.Lparen, 1, "x.(type) is allowed only as the subject of a type switch")
			}
		}
	}
	var subjects []*TypeGuard
	var binds []*Ident
	define := NoPos
	switch h := header.(type) {
	case *ExprStmt:
		if g, ok := h.X.(*TypeGuard); ok {
			subjects = []*TypeGuard{g}
		}
	case *AssignStmt:
		for _, x := range h.Rhs {
			if g, ok := x.(*TypeGuard); ok {
				subjects = append(subjects, g)
			}
		}
		if len(subjects) > 0 {
			if h.Tok != DEFINE || len(subjects) != len(h.Rhs) {
				p.errorAt(h.TokPos, 1, "a type switch binds its subjects with :=")
			}
			if len(h.Lhs) != len(h.Rhs) {
				p.errorAt(h.Lhs[0].Pos(), int(h.Lhs[len(h.Lhs)-1].End()-h.Lhs[0].Pos()), "%d bindings for %d type switch subjects", len(h.Lhs), len(h.Rhs))
			}
			for _, x := range h.Lhs {
				binds = append(binds, x.(*Ident))
			}
			define = h.TokPos
		}
	}
	if len(guards) != len(subjects) {
		for _, g := range guards {
			if !containsGuard(subjects, g) {
				p.errorAt(g.Lparen, 1, "x.(type) is allowed only as the subject of a type switch")
			}
		}
	}
	if len(subjects) > 1 && len(binds) == 0 {
		p.errorAt(subjects[0].Pos(), 1, "a multi-subject type switch binds its subjects")
	}
	if p.tok.tok != LBRACE {
		p.errorf("expected \"{\", found %s", p.found())
	}
	if subjects != nil {
		s := &TypeSwitchStmt{Switch: pos, Init: init, Binds: binds, Define: define, Subjects: subjects}
		s.Lbrace, s.Clauses, s.Rbrace = p.parseCaseClauses(len(subjects))
		return s
	}
	s := &SwitchStmt{Switch: pos, Init: init}
	if header != nil {
		x, ok := header.(*ExprStmt)
		if !ok {
			p.errorAt(header.Pos(), 1, "expected a switch expression, found a statement")
		}
		s.Tag = x.X
	}
	s.Lbrace, s.Clauses, s.Rbrace = p.parseCaseClauses(0)
	return s
}

func containsGuard(list []*TypeGuard, g *TypeGuard) bool {
	for _, x := range list {
		if x == g {
			return true
		}
	}
	return false
}

// parseCaseClauses parses the body of a switch. subjects is 0 for an
// expression switch and the subject count of a type switch.
func (p *parser) parseCaseClauses(subjects int) (Pos, []*CaseClause, Pos) {
	lbrace := p.want(LBRACE)
	var clauses []*CaseClause
	hasDefault := false
	for p.tok.tok == CASE || p.tok.tok == DEFAULT {
		c := &CaseClause{Case: p.tok.pos}
		if p.tok.tok == DEFAULT {
			if hasDefault {
				p.errorf("multiple defaults in a switch")
			}
			hasDefault = true
			p.next()
		} else {
			p.next()
			for {
				if subjects == 0 {
					c.List = append(c.List, p.parseExpr())
				} else {
					c.List = append(c.List, p.parseTypeCase(subjects))
				}
				if !p.got(COMMA) {
					break
				}
			}
		}
		c.Colon = p.want(COLON)
		c.Body = p.parseStmtList()
		clauses = append(clauses, c)
	}
	if p.tok.tok != RBRACE {
		p.errorf("expected \"case\", \"default\" or \"}\", found %s", p.found())
	}
	rbrace := p.tok.pos
	p.next()
	return lbrace, clauses, rbrace
}

// parseTypeCase parses one case item of a type switch: a type, or for a
// switch of n > 1 subjects a tuple (A, B) of n types.
func (p *parser) parseTypeCase(n int) Expr {
	if n == 1 {
		if p.tok.tok == LPAREN {
			lparen := p.tok.pos
			p.next()
			x := p.parseType(true)
			if p.tok.tok == COMMA {
				p.errorAt(lparen, 1, "a tuple case needs a type switch with as many subjects")
			}
			return &ParenExpr{Lparen: lparen, X: x, Rparen: p.want(RPAREN)}
		}
		return p.parseType(true)
	}
	if p.tok.tok != LPAREN {
		p.errorf("a case of a %d-subject type switch is a tuple (…) of %d types, found %s", n, n, p.found())
	}
	t := &TupleExpr{Lparen: p.tok.pos}
	p.next()
	for {
		t.Elts = append(t.Elts, p.parseType(true))
		if !p.got(COMMA) {
			break
		}
	}
	t.Rparen = p.want(RPAREN)
	if len(t.Elts) != n {
		p.errorAt(t.Lparen, int(t.Rparen-t.Lparen)+1, "a tuple of %d types for %d type switch subjects", len(t.Elts), n)
	}
	return t
}

func (p *parser) parseSelectStmt() *SelectStmt {
	s := &SelectStmt{Select: p.want(SELECT)}
	s.Lbrace = p.want(LBRACE)
	hasDefault := false
	for p.tok.tok == CASE || p.tok.tok == DEFAULT {
		c := &CommClause{Case: p.tok.pos}
		if p.tok.tok == DEFAULT {
			if hasDefault {
				p.errorf("multiple defaults in a select")
			}
			hasDefault = true
			p.next()
		} else {
			p.next()
			comm := p.parseSimpleStmt(simplePlain)
			if !isCommStmt(comm) {
				p.errorAt(comm.Pos(), 1, "a select case is a send or a receive")
			}
			c.Comm = comm
		}
		c.Colon = p.want(COLON)
		c.Body = p.parseStmtList()
		s.Clauses = append(s.Clauses, c)
	}
	if p.tok.tok != RBRACE {
		p.errorf("expected \"case\", \"default\" or \"}\", found %s", p.found())
	}
	s.Rbrace = p.tok.pos
	p.next()
	return s
}

func isRecv(x Expr) bool {
	u, ok := x.(*UnaryExpr)
	return ok && u.Op == ARROW
}

func isCommStmt(s Stmt) bool {
	switch s := s.(type) {
	case *SendStmt:
		return true
	case *ExprStmt:
		return isRecv(s.X)
	case *AssignStmt:
		return (s.Tok == DEFINE || s.Tok == ASSIGN) && len(s.Lhs) <= 2 && len(s.Rhs) == 1 && isRecv(s.Rhs[0])
	}
	return false
}

func (p *parser) parseForStmt() Stmt {
	pos := p.want(FOR)
	saved := p.exprLev
	p.exprLev = -1
	defer func() { p.exprLev = saved }()
	if p.tok.tok == RANGE {
		r := &RangeStmt{For: pos, TokPos: NoPos, Tok: ILLEGAL, Range: p.tok.pos}
		p.next()
		r.X = p.parseExpr()
		p.exprLev = saved
		r.Body = p.forBody()
		return r
	}
	var init, post Stmt
	var cond Expr
	if p.tok.tok != LBRACE {
		var first Stmt
		if p.tok.tok != SEMICOLON {
			first = p.parseSimpleStmt(simpleRange)
		}
		if rc, ok := first.(*rangeClause); ok {
			p.exprLev = saved
			return &RangeStmt{For: pos, Key: rc.key, Value: rc.value, TokPos: rc.tokPos, Tok: rc.tok, Range: rc.rangePos, X: rc.x, Body: p.forBody()}
		}
		if p.headerSemicolon() {
			init = first
			if p.tok.tok != SEMICOLON {
				x := p.parseSimpleStmt(simplePlain)
				e, ok := x.(*ExprStmt)
				if !ok {
					p.errorAt(x.Pos(), 1, "expected a for condition, found a statement")
				}
				cond = e.X
			}
			if !p.headerSemicolon() {
				p.errorf("expected \";\" in the for clause, found %s", p.found())
			}
			if p.tok.tok != LBRACE {
				post = p.parseSimpleStmt(simplePlain)
				if a, ok := post.(*AssignStmt); ok && a.Tok == DEFINE {
					p.errorAt(a.TokPos, 2, "cannot declare in the post statement of a for loop")
				}
			}
		} else if first != nil {
			e, ok := first.(*ExprStmt)
			if !ok {
				p.errorAt(first.Pos(), 1, "expected a for condition, found a statement")
			}
			cond = e.X
		}
	}
	p.exprLev = saved
	return &ForStmt{For: pos, Init: init, Cond: cond, Post: post, Body: p.forBody()}
}

func (p *parser) forBody() *BlockStmt {
	if p.tok.tok != LBRACE {
		p.errorf("expected \"{\", found %s", p.found())
	}
	return p.parseBlock()
}
