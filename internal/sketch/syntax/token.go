package syntax

// Pos is a byte offset into a File's source. NoPos marks an absent
// position, such as the parentheses of an ungrouped import.
type Pos int

// NoPos is the zero value of an absent position.
const NoPos Pos = -1

// IsValid reports whether p is a position in the source.
func (p Pos) IsValid() bool { return p >= 0 }

// Token is the kind of a lexical token.
type Token int

// The tokens. Keywords are the 28 of docs/spec/grammar.md (Keywords): Go's
// 25 without goto, fallthrough and const, plus try, catch, needs, provides,
// wire and machine. replace, initial and step are contextual and lex as
// IDENT.
const (
	ILLEGAL Token = iota
	EOF
	COMMENT

	IDENT
	INT      // 42
	DURATION // 500ms
	STRING   // "a" or `a`
	RUNE     // 'a'

	ADD // +
	SUB // -
	MUL // *
	QUO // /
	REM // %

	AND     // &
	OR      // |
	XOR     // ^
	SHL     // <<
	SHR     // >>
	AND_NOT // &^

	ADD_ASSIGN     // +=
	SUB_ASSIGN     // -=
	MUL_ASSIGN     // *=
	QUO_ASSIGN     // /=
	REM_ASSIGN     // %=
	AND_ASSIGN     // &=
	OR_ASSIGN      // |=
	XOR_ASSIGN     // ^=
	SHL_ASSIGN     // <<=
	SHR_ASSIGN     // >>=
	AND_NOT_ASSIGN // &^=

	LAND  // &&
	LOR   // ||
	ARROW // <-
	INC   // ++
	DEC   // --

	EQL    // ==
	LSS    // <
	GTR    // >
	ASSIGN // =
	NOT    // !

	NEQ      // !=
	LEQ      // <=
	GEQ      // >=
	DEFINE   // :=
	ELLIPSIS // ...

	LPAREN    // (
	LBRACK    // [
	LBRACE    // {
	COMMA     // ,
	PERIOD    // .
	RPAREN    // )
	RBRACK    // ]
	RBRACE    // }
	SEMICOLON // ;
	COLON     // :

	keywordBeg
	BREAK
	CASE
	CHAN
	CONTINUE
	DEFAULT
	DEFER
	ELSE
	FOR
	FUNC
	GO
	IF
	IMPORT
	INTERFACE
	MAP
	PACKAGE
	RANGE
	RETURN
	SELECT
	STRUCT
	SWITCH
	TYPE
	VAR
	TRY
	CATCH
	NEEDS
	PROVIDES
	WIRE
	MACHINE
	keywordEnd
)

var tokens = [...]string{
	ILLEGAL:  "ILLEGAL",
	EOF:      "EOF",
	COMMENT:  "COMMENT",
	IDENT:    "IDENT",
	INT:      "INT",
	DURATION: "DURATION",
	STRING:   "STRING",
	RUNE:     "RUNE",

	ADD: "+", SUB: "-", MUL: "*", QUO: "/", REM: "%",
	AND: "&", OR: "|", XOR: "^", SHL: "<<", SHR: ">>", AND_NOT: "&^",

	ADD_ASSIGN: "+=", SUB_ASSIGN: "-=", MUL_ASSIGN: "*=", QUO_ASSIGN: "/=", REM_ASSIGN: "%=",
	AND_ASSIGN: "&=", OR_ASSIGN: "|=", XOR_ASSIGN: "^=", SHL_ASSIGN: "<<=", SHR_ASSIGN: ">>=", AND_NOT_ASSIGN: "&^=",

	LAND: "&&", LOR: "||", ARROW: "<-", INC: "++", DEC: "--",
	EQL: "==", LSS: "<", GTR: ">", ASSIGN: "=", NOT: "!",
	NEQ: "!=", LEQ: "<=", GEQ: ">=", DEFINE: ":=", ELLIPSIS: "...",

	LPAREN: "(", LBRACK: "[", LBRACE: "{", COMMA: ",", PERIOD: ".",
	RPAREN: ")", RBRACK: "]", RBRACE: "}", SEMICOLON: ";", COLON: ":",

	BREAK: "break", CASE: "case", CHAN: "chan", CONTINUE: "continue", DEFAULT: "default",
	DEFER: "defer", ELSE: "else", FOR: "for", FUNC: "func", GO: "go",
	IF: "if", IMPORT: "import", INTERFACE: "interface", MAP: "map", PACKAGE: "package",
	RANGE: "range", RETURN: "return", SELECT: "select", STRUCT: "struct", SWITCH: "switch",
	TYPE: "type", VAR: "var",
	TRY: "try", CATCH: "catch", NEEDS: "needs", PROVIDES: "provides", WIRE: "wire", MACHINE: "machine",
}

// String is the token's spelling for operators and keywords, and its class
// name otherwise.
func (t Token) String() string {
	if 0 <= t && int(t) < len(tokens) && tokens[t] != "" {
		return tokens[t]
	}
	return "token(?)"
}

// IsKeyword reports whether t is one of the 28 keywords.
func (t Token) IsKeyword() bool { return keywordBeg < t && t < keywordEnd }

// IsLiteral reports whether t is an identifier or a basic literal.
func (t Token) IsLiteral() bool { return IDENT <= t && t <= RUNE }

var keywords = func() map[string]Token {
	m := map[string]Token{}
	for t := keywordBeg + 1; t < keywordEnd; t++ {
		m[tokens[t]] = t
	}
	return m
}()

// Keywords lists the 28 keywords in token order.
func Keywords() []string {
	var out []string
	for t := keywordBeg + 1; t < keywordEnd; t++ {
		out = append(out, tokens[t])
	}
	return out
}

// removed are Go keywords the language removes (grammar.md, Keywords). They
// lex as identifiers, because they are not keywords, but no declaration,
// statement or name may use them: generated Go reserves them, and a program
// written with them is a Go program the language does not admit.
var removed = map[string]string{
	"goto":        "goto is not part of the language; use structured control flow",
	"fallthrough": "fallthrough is not part of the language; list the cases together",
	"const":       "const is not part of the language; a package-level var is immutable",
}

// Precedence is the binary precedence of op, or 0 when op is not a binary
// operator: Go's five levels.
func (op Token) Precedence() int {
	switch op {
	case LOR:
		return 1
	case LAND:
		return 2
	case EQL, NEQ, LSS, LEQ, GTR, GEQ:
		return 3
	case ADD, SUB, OR, XOR:
		return 4
	case MUL, QUO, REM, SHL, SHR, AND, AND_NOT:
		return 5
	}
	return 0
}
