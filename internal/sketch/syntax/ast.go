package syntax

// The AST follows go/ast: types are expressions, every node has Pos and End,
// and comments live in File.Comments in source order with a Doc group on the
// nodes that carry documentation. Effra's constructs have dedicated nodes
// (ErrorRow, UsesClause, TryExpr, CatchExpr, TypeSwitchStmt, MachineType,
// WireDecl, WireLit, GoExpr, CallExpr.Trailing). The parser resolves no
// names: with(…) and timeout.After(…) are ordinary calls.

// Node is any syntax node.
type Node interface {
	Pos() Pos // the first byte of the node
	End() Pos // the byte after the node
}

// Expr is an expression or a type.
type Expr interface {
	Node
	exprNode()
}

// Stmt is a statement.
type Stmt interface {
	Node
	stmtNode()
}

// Decl is a top-level declaration, or a local var declaration.
type Decl interface {
	Node
	declNode()
}

// ---------------------------------------------------------------- comments

// Comment is one // or /* */ comment. Text is its source text, with
// carriage returns removed.
type Comment struct {
	Slash  Pos
	Text   string
	EndPos Pos
}

func (c *Comment) Pos() Pos { return c.Slash }
func (c *Comment) End() Pos { return c.EndPos }

// CommentGroup is a run of comments with no blank line and no token between
// them.
type CommentGroup struct {
	List []*Comment
}

func (g *CommentGroup) Pos() Pos { return g.List[0].Pos() }
func (g *CommentGroup) End() Pos { return g.List[len(g.List)-1].End() }

// ---------------------------------------------------------------- file

// File is one parsed source file.
type File struct {
	Src      string
	Package  Pos
	Name     *Ident
	Decls    []Decl // imports first, in source order
	Comments []*CommentGroup
	EOF      Pos
	lines    []int
}

func (f *File) Pos() Pos { return f.Package }
func (f *File) End() Pos { return f.EOF }

// Position returns the 1-based line and byte column of pos.
func (f *File) Position(pos Pos) (line, column int) { return lineColumn(f.lines, int(pos)) }

// Line returns the 1-based line of pos.
func (f *File) Line(pos Pos) int {
	line, _ := lineColumn(f.lines, int(pos))
	return line
}

// ---------------------------------------------------------------- declarations

// ImportKind is the form of an import spec.
type ImportKind int

const (
	ImportPlain ImportKind = iota // "path"
	ImportNamed                   // name "path"
	ImportDot                     // . "path"
	ImportGo                      // go "path": a Go package
)

// ImportDecl is `import "p"` or `import ( … )`. Lparen is NoPos without
// parentheses.
type ImportDecl struct {
	Doc    *CommentGroup
	Import Pos
	Lparen Pos
	Specs  []*ImportSpec
	Rparen Pos
}

// ImportSpec is one imported path. Name is set for ImportNamed; NamePos is
// the position of the name, `.` or `go`.
type ImportSpec struct {
	Doc     *CommentGroup
	Kind    ImportKind
	Name    *Ident
	NamePos Pos
	Path    *BasicLit
}

// VarDecl is `var x = e`, `var x T` or `var ( … )`.
type VarDecl struct {
	Doc    *CommentGroup
	Var    Pos
	Lparen Pos
	Specs  []*VarSpec
	Rparen Pos
}

// VarSpec is one `names [Type] [= values]` line.
type VarSpec struct {
	Doc    *CommentGroup
	Names  []*Ident
	Type   Expr
	Assign Pos
	Values []Expr
}

// TypeDecl is `type Name[params] Spec`, where Spec is a StructType, an
// InterfaceType or a MachineType (grammar.md, Declarations 4).
type TypeDecl struct {
	Doc        *CommentGroup
	Type       Pos
	Name       *Ident
	TypeParams *FieldList // brackets; nil without a list
	Spec       Expr
}

// FuncDecl is a function or a method. Body is nil for a bodiless
// declaration, such as one bound by an //ef:go directive.
type FuncDecl struct {
	Doc        *CommentGroup
	Func       Pos
	Recv       *FieldList
	Name       *Ident
	TypeParams *FieldList
	Type       *FuncType
	Body       *BlockStmt
}

// WireDecl is `wire Name [uses A, B] { entries }`.
type WireDecl struct {
	Doc  *CommentGroup
	Wire Pos
	Name *Ident
	Uses *UsesClause
	Body *WireBody
}

func (d *ImportDecl) Pos() Pos { return d.Import }
func (d *ImportDecl) End() Pos {
	if d.Rparen.IsValid() {
		return d.Rparen + 1
	}
	return d.Specs[0].End()
}
func (s *ImportSpec) Pos() Pos {
	if s.Kind != ImportPlain {
		return s.NamePos
	}
	return s.Path.Pos()
}
func (s *ImportSpec) End() Pos { return s.Path.End() }
func (d *VarDecl) Pos() Pos    { return d.Var }
func (d *VarDecl) End() Pos {
	if d.Rparen.IsValid() {
		return d.Rparen + 1
	}
	return d.Specs[0].End()
}
func (s *VarSpec) Pos() Pos { return s.Names[0].Pos() }
func (s *VarSpec) End() Pos {
	if len(s.Values) > 0 {
		return s.Values[len(s.Values)-1].End()
	}
	if s.Type != nil {
		return s.Type.End()
	}
	return s.Names[len(s.Names)-1].End()
}
func (d *TypeDecl) Pos() Pos { return d.Type }
func (d *TypeDecl) End() Pos { return d.Spec.End() }
func (d *FuncDecl) Pos() Pos { return d.Func }
func (d *FuncDecl) End() Pos {
	if d.Body != nil {
		return d.Body.End()
	}
	return d.Type.End()
}
func (d *WireDecl) Pos() Pos { return d.Wire }
func (d *WireDecl) End() Pos { return d.Body.End() }

func (*ImportDecl) declNode() {}
func (*VarDecl) declNode()    {}
func (*TypeDecl) declNode()   {}
func (*FuncDecl) declNode()   {}
func (*WireDecl) declNode()   {}

// WireEntryKind is the form of a wire entry.
type WireEntryKind int

const (
	WireBind    WireEntryKind = iota // Key = Value
	WireEmbed                        // Key: an embedded wire
	WireReplace                      // replace Key = Value
)

// WireBody is the braced entry list of a wire declaration or literal.
type WireBody struct {
	Lbrace  Pos
	Entries []*WireEntry
	Rbrace  Pos
}

// WireEntry binds a capability (Key, a name or a qualified name) to a
// provider, embeds a wire, or replaces an embedded entry. Keys holds the
// further capabilities of `A, B = provider`, one provider serving several
// capabilities.
type WireEntry struct {
	Doc     *CommentGroup
	Kind    WireEntryKind
	Replace Pos
	Key     Expr
	Keys    []Expr
	Assign  Pos
	Value   Expr
}

func (b *WireBody) Pos() Pos { return b.Lbrace }
func (b *WireBody) End() Pos { return b.Rbrace + 1 }
func (e *WireEntry) Pos() Pos {
	if e.Kind == WireReplace {
		return e.Replace
	}
	return e.Key.Pos()
}
func (e *WireEntry) End() Pos {
	if e.Value != nil {
		return e.Value.End()
	}
	return e.Key.End()
}

// ---------------------------------------------------------------- fields and signatures

// Field is a parameter, result, receiver, struct field, type parameter or
// interface method. Names is empty for a type-only parameter. Type is nil
// for type parameters written without a constraint (`type Attempt[T]`). For
// a method of a capability interface, Names holds the method name and Type
// is its *FuncType.
type Field struct {
	Doc   *CommentGroup
	Names []*Ident
	Type  Expr
}

func (f *Field) Pos() Pos {
	if len(f.Names) > 0 {
		return f.Names[0].Pos()
	}
	return f.Type.Pos()
}
func (f *Field) End() Pos {
	if f.Type == nil {
		return f.Names[len(f.Names)-1].End()
	}
	return f.Type.End()
}

// FieldList is a parenthesised, bracketed or braced field list. Opening and
// Closing are NoPos for an unparenthesised single result.
type FieldList struct {
	Opening Pos
	List    []*Field
	Closing Pos
}

func (l *FieldList) Pos() Pos {
	if l.Opening.IsValid() {
		return l.Opening
	}
	return l.List[0].Pos()
}
func (l *FieldList) End() Pos {
	if l.Closing.IsValid() {
		return l.Closing + 1
	}
	return l.List[len(l.List)-1].End()
}

// UsesClause is a capability row: `uses A, B`, or `uses (A, B)` in a
// function type inside a list (grammar.md D8). Lparen is NoPos without
// parentheses.
type UsesClause struct {
	Uses   Pos
	Lparen Pos
	Names  []Expr
	Rparen Pos
}

func (u *UsesClause) Pos() Pos { return u.Uses }
func (u *UsesClause) End() Pos {
	if u.Rparen.IsValid() {
		return u.Rparen + 1
	}
	return u.Names[len(u.Names)-1].End()
}

// ---------------------------------------------------------------- expressions and types

type (
	// Ident is a name.
	Ident struct {
		NamePos Pos
		Name    string
	}

	// BasicLit is an INT, DURATION, STRING or RUNE literal. Value is its
	// source text.
	BasicLit struct {
		ValuePos Pos
		Kind     Token
		Value    string
		ValueEnd Pos
	}

	// CompositeLit is `T{…}`, or `{…}` with an elided type inside another
	// composite literal (Type is nil).
	CompositeLit struct {
		Type   Expr
		Lbrace Pos
		Elts   []Expr
		Rbrace Pos
	}

	// KeyValueExpr is `key: value` in a composite literal.
	KeyValueExpr struct {
		Key   Expr
		Colon Pos
		Value Expr
	}

	// FuncLit is a function literal.
	FuncLit struct {
		Type *FuncType
		Body *BlockStmt
	}

	// ParenExpr is a parenthesised expression or type.
	ParenExpr struct {
		Lparen Pos
		X      Expr
		Rparen Pos
	}

	// SelectorExpr is `x.Sel`.
	SelectorExpr struct {
		X   Expr
		Sel *Ident
	}

	// IndexExpr is `x[i]`, or an instantiation `x[A, B]`.
	IndexExpr struct {
		X       Expr
		Lbrack  Pos
		Indices []Expr
		Rbrack  Pos
	}

	// SliceExpr is `x[low:high]` or `x[low:high:max]`.
	SliceExpr struct {
		X      Expr
		Lbrack Pos
		Low    Expr
		High   Expr
		Max    Expr
		Slice3 bool
		Rbrack Pos
	}

	// TypeGuard is `x.(type)`; it appears only as a subject of a
	// TypeSwitchStmt.
	TypeGuard struct {
		X      Expr
		Lparen Pos
		Rparen Pos
	}

	// CallExpr is a call or a conversion, with an optional trailing block
	// (grammar.md, Expressions 3).
	CallExpr struct {
		Fun      Expr
		Lparen   Pos
		Args     []Expr
		Ellipsis Pos // position of `...`, or NoPos
		Rparen   Pos
		Trailing *BlockStmt
	}

	// StarExpr is `*x`: a pointer type or an indirection.
	StarExpr struct {
		Star Pos
		X    Expr
	}

	// UnaryExpr is `op x` for + - ! ^ & <-.
	UnaryExpr struct {
		OpPos Pos
		Op    Token
		X     Expr
	}

	// BinaryExpr is `x op y`.
	BinaryExpr struct {
		X     Expr
		OpPos Pos
		Op    Token
		Y     Expr
	}

	// TryExpr is `try x`. It binds looser than every binary operator, also
	// in operand position, where it extends over the rest of the binary
	// expression to its right (grammar.md D8).
	TryExpr struct {
		Try Pos
		X   Expr
	}

	// CatchExpr is `x catch [bind] { arms }`. Arms hold statements.
	CatchExpr struct {
		X      Expr
		Catch  Pos
		Bind   *Ident
		Lbrace Pos
		Arms   []*CatchArm
		Rbrace Pos
	}

	// WireLit is an inline `wire{ … }`.
	WireLit struct {
		Wire Pos
		Body *WireBody
	}

	// GoExpr is `go f(x)` or `go { … }`: it starts a task. Exactly one of
	// Call and Block is set.
	GoExpr struct {
		Go    Pos
		Call  *CallExpr
		Block *BlockStmt
	}

	// Ellipsis is `...T`, the type of a variadic parameter, or `...` with no
	// Elt, the length of an array composite literal `[...]T{…}`.
	Ellipsis struct {
		Ellipsis Pos
		Elt      Expr
	}

	// ArrayType is `[N]T`, or `[]T` when Len is nil, or `[...]T` when Len
	// is an Ellipsis (only as the type of a composite literal).
	ArrayType struct {
		Lbrack Pos
		Len    Expr
		Elt    Expr
	}

	// MapType is `map[K]V`.
	MapType struct {
		Map   Pos
		Key   Expr
		Value Expr
	}

	// ChanType is `chan T`, `chan<- T` or `<-chan T`.
	ChanType struct {
		Begin Pos
		Arrow Pos // NoPos for a bidirectional channel
		Dir   ChanDir
		Value Expr
	}

	// FuncType is a signature. Func is NoPos in a declaration or method.
	// Results is nil without results.
	FuncType struct {
		Func    Pos
		Params  *FieldList
		Results *FieldList
		Uses    *UsesClause
	}

	// StructType is `struct{ fields }`.
	StructType struct {
		Struct Pos
		Fields *FieldList
	}

	// InterfaceType is a capability interface (Methods) or a union
	// (Union, the variant types). An empty interface has neither.
	InterfaceType struct {
		Interface Pos
		Lbrace    Pos
		Methods   []*Field
		Union     []Expr
		Rbrace    Pos
	}

	// ErrorRow is `error[A | B]` in a result list.
	ErrorRow struct {
		Error  Pos
		Lbrack Pos
		Types  []Expr
		Rbrack Pos
	}

	// MachineType is `machine[S, E] { initial x; step f }`.
	MachineType struct {
		Machine Pos
		Lbrack  Pos
		State   Expr
		Signal  Expr
		Rbrack  Pos
		Lbrace  Pos
		Entries []*MachineEntry // in source order
		Rbrace  Pos
		Initial Expr
		Step    Expr
	}

	// TupleExpr is `(A, B)`, a case of a multi-subject type switch.
	TupleExpr struct {
		Lparen Pos
		Elts   []Expr
		Rparen Pos
	}
)

// ChanDir is a channel direction.
type ChanDir int

const (
	ChanBoth ChanDir = iota
	ChanSend         // chan<- T
	ChanRecv         // <-chan T
)

// MachineEntry is `initial x` or `step f`.
type MachineEntry struct {
	Doc     *CommentGroup
	NamePos Pos
	Name    string // "initial" or "step"
	Value   Expr
}

func (e *MachineEntry) Pos() Pos { return e.NamePos }
func (e *MachineEntry) End() Pos { return e.Value.End() }

// CatchArm is `case A, B: statements`.
type CatchArm struct {
	Case  Pos
	Types []Expr
	Colon Pos
	Body  []Stmt
}

func (a *CatchArm) Pos() Pos { return a.Case }
func (a *CatchArm) End() Pos {
	if len(a.Body) > 0 {
		return a.Body[len(a.Body)-1].End()
	}
	return a.Colon + 1
}

func (x *Ident) Pos() Pos    { return x.NamePos }
func (x *Ident) End() Pos    { return x.NamePos + Pos(len(x.Name)) }
func (x *BasicLit) Pos() Pos { return x.ValuePos }
func (x *BasicLit) End() Pos { return x.ValueEnd }
func (x *CompositeLit) Pos() Pos {
	if x.Type != nil {
		return x.Type.Pos()
	}
	return x.Lbrace
}
func (x *CompositeLit) End() Pos { return x.Rbrace + 1 }
func (x *KeyValueExpr) Pos() Pos { return x.Key.Pos() }
func (x *KeyValueExpr) End() Pos { return x.Value.End() }
func (x *FuncLit) Pos() Pos      { return x.Type.Pos() }
func (x *FuncLit) End() Pos      { return x.Body.End() }
func (x *ParenExpr) Pos() Pos    { return x.Lparen }
func (x *ParenExpr) End() Pos    { return x.Rparen + 1 }
func (x *SelectorExpr) Pos() Pos { return x.X.Pos() }
func (x *SelectorExpr) End() Pos { return x.Sel.End() }
func (x *IndexExpr) Pos() Pos    { return x.X.Pos() }
func (x *IndexExpr) End() Pos    { return x.Rbrack + 1 }
func (x *SliceExpr) Pos() Pos    { return x.X.Pos() }
func (x *SliceExpr) End() Pos    { return x.Rbrack + 1 }
func (x *TypeGuard) Pos() Pos    { return x.X.Pos() }
func (x *TypeGuard) End() Pos    { return x.Rparen + 1 }
func (x *CallExpr) Pos() Pos     { return x.Fun.Pos() }
func (x *CallExpr) End() Pos {
	if x.Trailing != nil {
		return x.Trailing.End()
	}
	return x.Rparen + 1
}
func (x *StarExpr) Pos() Pos   { return x.Star }
func (x *StarExpr) End() Pos   { return x.X.End() }
func (x *UnaryExpr) Pos() Pos  { return x.OpPos }
func (x *UnaryExpr) End() Pos  { return x.X.End() }
func (x *BinaryExpr) Pos() Pos { return x.X.Pos() }
func (x *BinaryExpr) End() Pos { return x.Y.End() }
func (x *TryExpr) Pos() Pos    { return x.Try }
func (x *TryExpr) End() Pos    { return x.X.End() }
func (x *CatchExpr) Pos() Pos  { return x.X.Pos() }
func (x *CatchExpr) End() Pos  { return x.Rbrace + 1 }
func (x *WireLit) Pos() Pos    { return x.Wire }
func (x *WireLit) End() Pos    { return x.Body.End() }
func (x *GoExpr) Pos() Pos     { return x.Go }
func (x *GoExpr) End() Pos {
	if x.Block != nil {
		return x.Block.End()
	}
	return x.Call.End()
}
func (x *Ellipsis) Pos() Pos { return x.Ellipsis }
func (x *Ellipsis) End() Pos {
	if x.Elt == nil {
		return x.Ellipsis + 3
	}
	return x.Elt.End()
}
func (x *ArrayType) Pos() Pos { return x.Lbrack }
func (x *ArrayType) End() Pos { return x.Elt.End() }
func (x *MapType) Pos() Pos   { return x.Map }
func (x *MapType) End() Pos   { return x.Value.End() }
func (x *ChanType) Pos() Pos  { return x.Begin }
func (x *ChanType) End() Pos  { return x.Value.End() }
func (x *FuncType) Pos() Pos {
	if x.Func.IsValid() {
		return x.Func
	}
	return x.Params.Pos()
}
func (x *FuncType) End() Pos {
	switch {
	case x.Uses != nil:
		return x.Uses.End()
	case x.Results != nil:
		return x.Results.End()
	}
	return x.Params.End()
}
func (x *StructType) Pos() Pos    { return x.Struct }
func (x *StructType) End() Pos    { return x.Fields.End() }
func (x *InterfaceType) Pos() Pos { return x.Interface }
func (x *InterfaceType) End() Pos { return x.Rbrace + 1 }
func (x *ErrorRow) Pos() Pos      { return x.Error }
func (x *ErrorRow) End() Pos      { return x.Rbrack + 1 }
func (x *MachineType) Pos() Pos   { return x.Machine }
func (x *MachineType) End() Pos   { return x.Rbrace + 1 }
func (x *TupleExpr) Pos() Pos     { return x.Lparen }
func (x *TupleExpr) End() Pos     { return x.Rparen + 1 }

func (*Ident) exprNode()         {}
func (*BasicLit) exprNode()      {}
func (*CompositeLit) exprNode()  {}
func (*KeyValueExpr) exprNode()  {}
func (*FuncLit) exprNode()       {}
func (*ParenExpr) exprNode()     {}
func (*SelectorExpr) exprNode()  {}
func (*IndexExpr) exprNode()     {}
func (*SliceExpr) exprNode()     {}
func (*TypeGuard) exprNode()     {}
func (*CallExpr) exprNode()      {}
func (*StarExpr) exprNode()      {}
func (*UnaryExpr) exprNode()     {}
func (*BinaryExpr) exprNode()    {}
func (*TryExpr) exprNode()       {}
func (*CatchExpr) exprNode()     {}
func (*WireLit) exprNode()       {}
func (*GoExpr) exprNode()        {}
func (*Ellipsis) exprNode()      {}
func (*ArrayType) exprNode()     {}
func (*MapType) exprNode()       {}
func (*ChanType) exprNode()      {}
func (*FuncType) exprNode()      {}
func (*StructType) exprNode()    {}
func (*InterfaceType) exprNode() {}
func (*ErrorRow) exprNode()      {}
func (*MachineType) exprNode()   {}
func (*TupleExpr) exprNode()     {}

// ---------------------------------------------------------------- statements

type (
	// DeclStmt is a local var declaration.
	DeclStmt struct {
		Decl *VarDecl
	}

	// EmptyStmt is the empty statement of a label: an explicit `;` after
	// the colon (`L: ;`), or an implicit one before a block's `}` (`L: }`),
	// as go/ast.EmptyStmt. An explicit one is a statement of its own, so the
	// statement after it is not labelled.
	EmptyStmt struct {
		Semicolon Pos
		Implicit  bool // no `;` in the source: the label ends its block
	}

	// LabeledStmt is `label: stmt`.
	LabeledStmt struct {
		Label *Ident
		Colon Pos
		Stmt  Stmt
	}

	// ExprStmt is an expression used as a statement.
	ExprStmt struct {
		X Expr
	}

	// SendStmt is `ch <- v`.
	SendStmt struct {
		Chan  Expr
		Arrow Pos
		Value Expr
	}

	// IncDecStmt is `x++` or `x--`.
	IncDecStmt struct {
		X      Expr
		TokPos Pos
		Tok    Token
	}

	// AssignStmt is `lhs := rhs`, `lhs = rhs` or `lhs op= rhs`.
	AssignStmt struct {
		Lhs    []Expr
		TokPos Pos
		Tok    Token
		Rhs    []Expr
	}

	// DeferStmt is `defer stmt` for any simple statement.
	DeferStmt struct {
		Defer Pos
		Stmt  Stmt
	}

	// ReturnStmt is `return [results]`.
	ReturnStmt struct {
		Return  Pos
		Results []Expr
	}

	// BranchStmt is `break [label]` or `continue [label]`.
	BranchStmt struct {
		TokPos Pos
		Tok    Token
		Label  *Ident
	}

	// BlockStmt is a braced statement list.
	BlockStmt struct {
		Lbrace Pos
		List   []Stmt
		Rbrace Pos
	}

	// IfStmt is `if [init;] cond { } [else …]`; Else is an *IfStmt or a
	// *BlockStmt.
	IfStmt struct {
		If      Pos
		Init    Stmt
		Cond    Expr
		Body    *BlockStmt
		ElsePos Pos // NoPos without else
		Else    Stmt
	}

	// SwitchStmt is an expression switch.
	SwitchStmt struct {
		Switch  Pos
		Init    Stmt
		Tag     Expr
		Lbrace  Pos
		Clauses []*CaseClause
		Rbrace  Pos
	}

	// TypeSwitchStmt is `switch [binds :=] x.(type)[, y.(type)] { … }`.
	// Binds is empty without a binding; otherwise it has one name per
	// subject. Each case item of a multi-subject switch is a *TupleExpr.
	TypeSwitchStmt struct {
		Switch   Pos
		Init     Stmt
		Binds    []*Ident
		Define   Pos
		Subjects []*TypeGuard
		Lbrace   Pos
		Clauses  []*CaseClause
		Rbrace   Pos
	}

	// SelectStmt is `select { … }`.
	SelectStmt struct {
		Select  Pos
		Lbrace  Pos
		Clauses []*CommClause
		Rbrace  Pos
	}

	// ForStmt is `for [init; cond; post] { }` or `for cond { }`.
	ForStmt struct {
		For  Pos
		Init Stmt
		Cond Expr
		Post Stmt
		Body *BlockStmt
	}

	// RangeStmt is `for [key[, value] :=|=] range x { }`. Tok is ILLEGAL
	// without key and value.
	RangeStmt struct {
		For    Pos
		Key    Expr
		Value  Expr
		TokPos Pos
		Tok    Token
		Range  Pos
		X      Expr
		Body   *BlockStmt
	}
)

// CaseClause is `case list:` or `default:` (List is nil) of an expression
// or type switch.
type CaseClause struct {
	Case  Pos
	List  []Expr
	Colon Pos
	Body  []Stmt
}

func (c *CaseClause) Pos() Pos { return c.Case }
func (c *CaseClause) End() Pos {
	if len(c.Body) > 0 {
		return c.Body[len(c.Body)-1].End()
	}
	return c.Colon + 1
}

// CommClause is a select clause; Comm is nil for `default`.
type CommClause struct {
	Case  Pos
	Comm  Stmt
	Colon Pos
	Body  []Stmt
}

func (c *CommClause) Pos() Pos { return c.Case }
func (c *CommClause) End() Pos {
	if len(c.Body) > 0 {
		return c.Body[len(c.Body)-1].End()
	}
	return c.Colon + 1
}

func (s *DeclStmt) Pos() Pos    { return s.Decl.Pos() }
func (s *DeclStmt) End() Pos    { return s.Decl.End() }
func (s *EmptyStmt) Pos() Pos   { return s.Semicolon }
func (s *EmptyStmt) End() Pos   { return s.Semicolon }
func (s *LabeledStmt) Pos() Pos { return s.Label.Pos() }
func (s *LabeledStmt) End() Pos {
	if s.Stmt == nil {
		return s.Colon + 1
	}
	return s.Stmt.End()
}
func (s *ExprStmt) Pos() Pos   { return s.X.Pos() }
func (s *ExprStmt) End() Pos   { return s.X.End() }
func (s *SendStmt) Pos() Pos   { return s.Chan.Pos() }
func (s *SendStmt) End() Pos   { return s.Value.End() }
func (s *IncDecStmt) Pos() Pos { return s.X.Pos() }
func (s *IncDecStmt) End() Pos { return s.TokPos + 2 }
func (s *AssignStmt) Pos() Pos { return s.Lhs[0].Pos() }
func (s *AssignStmt) End() Pos { return s.Rhs[len(s.Rhs)-1].End() }
func (s *DeferStmt) Pos() Pos  { return s.Defer }
func (s *DeferStmt) End() Pos  { return s.Stmt.End() }
func (s *ReturnStmt) Pos() Pos { return s.Return }
func (s *ReturnStmt) End() Pos {
	if len(s.Results) > 0 {
		return s.Results[len(s.Results)-1].End()
	}
	return s.Return + 6
}
func (s *BranchStmt) Pos() Pos { return s.TokPos }
func (s *BranchStmt) End() Pos {
	if s.Label != nil {
		return s.Label.End()
	}
	return s.TokPos + Pos(len(s.Tok.String()))
}
func (s *BlockStmt) Pos() Pos { return s.Lbrace }
func (s *BlockStmt) End() Pos { return s.Rbrace + 1 }
func (s *IfStmt) Pos() Pos    { return s.If }
func (s *IfStmt) End() Pos {
	if s.Else != nil {
		return s.Else.End()
	}
	return s.Body.End()
}
func (s *SwitchStmt) Pos() Pos     { return s.Switch }
func (s *SwitchStmt) End() Pos     { return s.Rbrace + 1 }
func (s *TypeSwitchStmt) Pos() Pos { return s.Switch }
func (s *TypeSwitchStmt) End() Pos { return s.Rbrace + 1 }
func (s *SelectStmt) Pos() Pos     { return s.Select }
func (s *SelectStmt) End() Pos     { return s.Rbrace + 1 }
func (s *ForStmt) Pos() Pos        { return s.For }
func (s *ForStmt) End() Pos        { return s.Body.End() }
func (s *RangeStmt) Pos() Pos      { return s.For }
func (s *RangeStmt) End() Pos      { return s.Body.End() }

func (*DeclStmt) stmtNode()       {}
func (*EmptyStmt) stmtNode()      {}
func (*LabeledStmt) stmtNode()    {}
func (*ExprStmt) stmtNode()       {}
func (*SendStmt) stmtNode()       {}
func (*IncDecStmt) stmtNode()     {}
func (*AssignStmt) stmtNode()     {}
func (*DeferStmt) stmtNode()      {}
func (*ReturnStmt) stmtNode()     {}
func (*BranchStmt) stmtNode()     {}
func (*BlockStmt) stmtNode()      {}
func (*IfStmt) stmtNode()         {}
func (*SwitchStmt) stmtNode()     {}
func (*TypeSwitchStmt) stmtNode() {}
func (*SelectStmt) stmtNode()     {}
func (*ForStmt) stmtNode()        {}
func (*RangeStmt) stmtNode()      {}
