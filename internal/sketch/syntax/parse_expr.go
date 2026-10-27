package syntax

// ---------------------------------------------------------------- expressions

// parseExpr parses an expression: a binary expression with an optional
// catch (grammar.md, Expressions: Expression = [ "try" ] CatchExpr).
func (p *parser) parseExpr() Expr {
	p.enter()
	defer p.leave()
	x := p.parseBinary(1)
	if p.tok.tok == CATCH {
		x = p.parseCatch(x)
	}
	return x
}

// parseBinary parses a binary expression of precedence prec or tighter. Each
// operator wraps the tree built so far in one more level: a long chain is
// as deep for every recursive consumer as the same number of parentheses.
// The loop retains the tree's height (see measure), so an operand that is
// itself deep, such as a parenthesized chain, counts in full.
func (p *parser) parseBinary(prec int) Expr {
	o := p.nest
	saved := p.measure()
	x := p.parseUnary()
	h := p.height(saved)
	defer func() { p.nest = o; p.reach(o + h) }()
	for {
		op := p.tok.tok
		opPrec := op.Precedence()
		if opPrec < prec {
			return x
		}
		if o+h+1 > MaxNesting {
			p.tooDeep()
		}
		p.enter()
		pos := p.tok.pos
		p.next()
		inner := p.measure()
		y := p.parseBinary(opPrec + 1)
		h = max(h, p.height(inner)) + 1
		p.leave()
		x = &BinaryExpr{X: x, OpPos: pos, Op: op, Y: y}
	}
}

// parseUnary parses a unary expression. A prefix operator opens its level
// before it is consumed, so past the limit the S001 is at the operator.
func (p *parser) parseUnary() Expr {
	switch p.tok.tok {
	case ADD, SUB, NOT, XOR, AND:
		pos, op := p.tok.pos, p.tok.tok
		p.enter()
		p.next()
		x := p.parseUnary()
		p.leave()
		return &UnaryExpr{OpPos: pos, Op: op, X: x}
	case ARROW:
		pos := p.tok.pos
		if p.peek(1).tok == CHAN {
			p.next()
			p.next()
			return &ChanType{Begin: pos, Arrow: pos, Dir: ChanRecv, Value: p.parseType(false)}
		}
		p.enter()
		p.next()
		x := p.parseUnary()
		p.leave()
		return &UnaryExpr{OpPos: pos, Op: ARROW, X: x}
	case MUL:
		pos := p.tok.pos
		p.enter()
		p.next()
		x := p.parseUnary()
		p.leave()
		return &StarExpr{Star: pos, X: x}
	case TRY:
		// try extends over the rest of the binary expression to its right,
		// so it binds looser than every binary operator in operand position
		// too (grammar.md D8).
		pos := p.tok.pos
		p.enter()
		p.next()
		switch p.tok.tok {
		case SEMICOLON, RBRACE, RPAREN, RBRACK, COMMA, COLON, EOF, CATCH:
			p.errorAt(pos, 3, "try needs an operand")
		}
		x := p.parseBinary(1)
		if p.tok.tok == CATCH {
			x = p.parseCatch(x)
		}
		p.leave()
		return &TryExpr{Try: pos, X: x}
	}
	return p.parsePrimaryExpr()
}

func (p *parser) parseCatch(x Expr) *CatchExpr {
	c := &CatchExpr{X: x, Catch: p.want(CATCH)}
	if p.tok.tok == IDENT {
		c.Bind = p.parseIdent()
	}
	if p.tok.tok != LBRACE {
		p.errorf("expected \"{\" or a name after catch, found %s", p.found())
	}
	c.Lbrace = p.tok.pos
	p.next()
	saved, savedGuard := p.exprLev, p.guardOK
	p.exprLev, p.guardOK = 0, false
	for p.tok.tok == CASE {
		arm := &CatchArm{Case: p.tok.pos}
		p.next()
		arm.Types = append(arm.Types, p.parseTypeName("a catch arm"))
		for p.got(COMMA) {
			arm.Types = append(arm.Types, p.parseTypeName("a catch arm"))
		}
		arm.Colon = p.want(COLON)
		arm.Body = p.parseStmtList()
		c.Arms = append(c.Arms, arm)
	}
	p.exprLev, p.guardOK = saved, savedGuard
	if p.tok.tok == DEFAULT {
		p.errorf("a catch arm names the error types it handles; default is not allowed")
	}
	if p.tok.tok != RBRACE {
		p.errorf("expected \"case\" or \"}\", found %s", p.found())
	}
	c.Rbrace = p.tok.pos
	p.next()
	return c
}

// parsePrimaryExpr parses an operand and its selectors, indexes, calls and
// literals. Each one wraps the tree built so far in one more level; the
// loop retains the tree's height as parseBinary does.
func (p *parser) parsePrimaryExpr() Expr {
	o := p.nest
	saved := p.measure()
	x := p.parseOperand()
	h := p.height(saved)
	inner := 0
	defer func() { p.nest = o; p.reach(o + h) }()
	for {
		if p.nest > o {
			// The previous link's children are parsed: fold their height.
			h = max(h, p.height(inner)) + 1
			p.leave()
		}
		if p.tok.tok == LBRACE && !p.isLiteralType(x) {
			// The brace opens a body (a control clause header, a block
			// after a statement): it is a sibling, never a link, and the
			// statement parser charges it as a block.
			return x
		}
		switch p.tok.tok {
		case PERIOD, LBRACK, LPAREN, LBRACE:
			if o+h+1 > MaxNesting {
				p.tooDeep()
			}
			p.enter()
			inner = p.measure()
		}
		switch p.tok.tok {
		case PERIOD:
			p.next()
			switch p.tok.tok {
			case IDENT:
				x = &SelectorExpr{X: x, Sel: p.parseIdent()}
			case LPAREN:
				lparen := p.tok.pos
				p.next()
				if p.tok.tok != TYPE {
					p.errorAt(lparen, 1, "type assertions are not part of the language; use a type switch")
				}
				if !p.guardOK {
					p.errorAt(lparen, 1, "x.(type) is allowed only as the subject of a type switch")
				}
				p.next()
				g := &TypeGuard{X: x, Lparen: lparen, Rparen: p.want(RPAREN)}
				p.guards = append(p.guards, g)
				x = g
			default:
				p.errorf("expected a name after \".\", found %s", p.found())
			}
		case LBRACK:
			x = p.parseIndexOrSlice(x)
		case LPAREN:
			x = p.parseCall(x)
		case LBRACE:
			x = p.parseCompositeLit(x)
		default:
			return x
		}
	}
}

// isLiteralType reports whether x, followed by `{`, starts a composite
// literal: go/parser's rule, including that a plain or qualified or
// instantiated type name in a control clause header does not.
func (p *parser) isLiteralType(x Expr) bool {
	switch t := x.(type) {
	case *Ident, *SelectorExpr, *IndexExpr:
		if s, ok := t.(*SelectorExpr); ok {
			if _, ok := s.X.(*Ident); !ok {
				return false
			}
		}
		return p.exprLev >= 0
	case *ArrayType, *StructType, *MapType:
		return true
	}
	return false
}

func (p *parser) parseOperand() Expr {
	switch p.tok.tok {
	case IDENT:
		return p.parseIdent()
	case INT, DURATION, STRING, RUNE:
		return p.basicLit()
	case LPAREN:
		p.enter()
		defer p.leave()
		lparen := p.tok.pos
		p.next()
		p.exprLev++
		x := p.parseExpr()
		p.exprLev--
		return &ParenExpr{Lparen: lparen, X: x, Rparen: p.want(RPAREN)}
	case FUNC:
		pos := p.tok.pos
		p.next()
		if p.tok.tok != LPAREN {
			p.errorf("expected \"(\", found %s", p.found())
		}
		t := p.parseSignature(pos, false)
		if p.tok.tok == LBRACE {
			return &FuncLit{Type: t, Body: p.parseBlock()}
		}
		return t
	case LBRACK, MAP, CHAN, STRUCT, INTERFACE:
		if p.tok.tok == LBRACK && p.peek(1).tok == ELLIPSIS {
			// `[...]T{…}`: the length is the number of elements. It is
			// the type of a composite literal only, never a type of its
			// own (go/types rejects it elsewhere; here it is S001).
			lbrack := p.tok.pos
			p.next()
			ellipsis := p.tok.pos
			p.next()
			p.want(RBRACK)
			t := &ArrayType{Lbrack: lbrack, Len: &Ellipsis{Ellipsis: ellipsis}, Elt: p.parseType(false)}
			if p.tok.tok != LBRACE {
				p.errorAt(ellipsis, 3, "[...] is the length of an array composite literal only; expected \"{\" after the type, found %s", p.found())
			}
			return t
		}
		return p.parseType(false)
	case WIRE:
		pos := p.tok.pos
		p.next()
		if p.tok.tok != LBRACE {
			p.errorf("expected \"{\" after wire, found %s", p.found())
		}
		return &WireLit{Wire: pos, Body: p.parseWireBody()}
	case GO:
		if p.peek(1).tok == PERIOD {
			id := &Ident{NamePos: p.tok.pos, Name: "go"}
			p.next()
			return id
		}
		g := &GoExpr{Go: p.tok.pos}
		p.next()
		if p.tok.tok == LBRACE {
			g.Block = p.parseBlock()
			return g
		}
		start := p.tok.pos
		x := p.parsePrimaryExpr()
		call, ok := x.(*CallExpr)
		if !ok {
			p.errorAt(start, int(x.End()-start), "go takes a call or a block")
		}
		g.Call = call
		return g
	}
	p.errorf("expected an expression, found %s", p.found())
	return nil
}

func (p *parser) parseIndexOrSlice(x Expr) Expr {
	lbrack := p.tok.pos
	p.next()
	p.exprLev++
	defer func() { p.exprLev-- }()
	if p.tok.tok == RBRACK {
		p.errorf("expected an index, found %s", p.found())
	}
	var index [3]Expr
	if p.tok.tok != COLON {
		index[0] = p.parseExpr()
	}
	if p.tok.tok == COLON {
		s := &SliceExpr{X: x, Lbrack: lbrack, Low: index[0]}
		p.next()
		if p.tok.tok != COLON && p.tok.tok != RBRACK {
			s.High = p.parseExpr()
		}
		if p.tok.tok == COLON {
			s.Slice3 = true
			p.next()
			if s.High == nil {
				p.errorf("middle index required in a 3-index slice")
			}
			if p.tok.tok == RBRACK {
				p.errorf("final index required in a 3-index slice")
			}
			s.Max = p.parseExpr()
		}
		s.Rbrack = p.want(RBRACK)
		return s
	}
	ix := &IndexExpr{X: x, Lbrack: lbrack, Indices: []Expr{index[0]}}
	for p.got(COMMA) {
		if p.tok.tok == RBRACK {
			break
		}
		ix.Indices = append(ix.Indices, p.parseExpr())
	}
	ix.Rbrack = p.want(RBRACK)
	return ix
}

func (p *parser) parseCall(fun Expr) *CallExpr {
	c := &CallExpr{Fun: fun, Lparen: p.want(LPAREN), Ellipsis: NoPos}
	p.exprLev++
	for p.tok.tok != RPAREN {
		c.Args = append(c.Args, p.parseExpr())
		if p.tok.tok == ELLIPSIS {
			c.Ellipsis = p.tok.pos
			p.next()
		}
		if !p.got(COMMA) {
			break
		}
		if c.Ellipsis.IsValid() && p.tok.tok != RPAREN {
			p.errorAt(c.Ellipsis, 3, "only the final argument can be spread with ...")
		}
	}
	p.exprLev--
	if p.tok.tok != RPAREN {
		p.errorf("expected \")\" or \",\", found %s", p.found())
	}
	c.Rparen = p.tok.pos
	p.next()
	// A trailing block opens on the call's line; in a control clause header
	// the `{` opens the body instead (grammar.md, Expressions 3).
	if p.tok.tok == LBRACE && p.exprLev >= 0 {
		c.Trailing = p.parseBlock()
	}
	return c
}

func (p *parser) parseCompositeLit(typ Expr) *CompositeLit {
	p.enter()
	defer p.leave()
	lit := &CompositeLit{Type: typ, Lbrace: p.want(LBRACE)}
	saved, savedGuard := p.exprLev, p.guardOK
	p.exprLev, p.guardOK = 1, false
	keyed, positional := -1, -1
	for p.tok.tok != RBRACE {
		elt := p.parseElement()
		if _, ok := elt.(*KeyValueExpr); ok {
			keyed = len(lit.Elts)
		} else {
			positional = len(lit.Elts)
		}
		if keyed >= 0 && positional >= 0 {
			p.errorAt(elt.Pos(), int(elt.End()-elt.Pos()), "a composite literal mixes keyed and positional elements")
		}
		lit.Elts = append(lit.Elts, elt)
		if !p.got(COMMA) {
			break
		}
	}
	p.exprLev, p.guardOK = saved, savedGuard
	if p.tok.tok != RBRACE {
		p.errorf("expected \",\" or \"}\" in a composite literal, found %s", p.found())
	}
	lit.Rbrace = p.tok.pos
	p.next()
	return lit
}

func (p *parser) parseElement() Expr {
	x := p.parseElementValue()
	if p.tok.tok == COLON {
		colon := p.tok.pos
		p.next()
		return &KeyValueExpr{Key: x, Colon: colon, Value: p.parseElementValue()}
	}
	return x
}

func (p *parser) parseElementValue() Expr {
	if p.tok.tok == LBRACE {
		return p.parseCompositeLit(nil)
	}
	return p.parseExpr()
}
