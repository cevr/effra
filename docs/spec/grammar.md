# Grammar

Status: the lexical rules and the productions marked "prototype" are **implemented in prototype** (`prototypes/sketch9/sketch/lex.go`, `parse.go`). Productions marked "designed" are **designed, not yet implemented**. A syntax error is S001 ([diagnostics](diagnostics.md)).

The grammar follows [the Go specification](https://go.dev/ref/spec) wherever this file is silent. A Go form that this file does not admit is an error, never a passthrough to the host (AGENTS.md).

## Source text

1. A source file is UTF-8 text. A byte sequence that is not valid UTF-8 is an error in identifier position. Inside a string literal, the literal's decoded bytes are its value (see [Literals](#literals)).
2. White space is space (U+0020), tab, carriage return and newline. Newlines are significant for semicolon insertion.
3. A comment starts with `//` and runs to the end of the line. Block comments `/* … */` are designed, as in Go; the prototype does not accept them.
4. **Semicolon insertion.** The lexer inserts a semicolon at a newline, and at the end of the file, when the line's last token is:
   - an identifier that is not a keyword, or the keyword `return`;
   - an integer, duration or string literal;
   - one of `)`, `]` or `}`.

   Go's rule also inserts after `break`, `continue`, `++` and `--`. Those tokens are designed, and the same rule applies to them. *Prior art:* Go's rule, kept so that Go formatting habits carry over (P3).

## Identifiers

```ebnf
identifier = letter { letter | digit } .
letter     = "_" | <a code point with Unicode general category L> .
digit      = <a code point with Unicode general category Nd> .
```

1. The lexer decodes each code point before it tests it. It must never test a single byte of a multi-byte sequence.
2. An identifier starts with a letter or `_`, and continues with letters, digits or `_`. This is Go's identifier rule (`go/token.IsIdentifier`). `xª` is an identifier; `x⪪` is not.
3. A code point that cannot start an identifier, or invalid UTF-8, where an identifier may start, is an error (S001). The same holds for a code point that cannot continue the identifier it follows: `x⪪` is S001, not the identifier `x` followed by an operator.
4. Number suffixes are decoded the same way (see [Literals](#literals)).

Fixtures: `testdata/probes/letter-name.sketch` (builds), `testdata/probes/nonletter-name.sketch` (S001); `sketch/lex_test.go` sweeps every non-ASCII code point against `go/token.IsIdentifier`.

*Prior art:* the round-7 review of the prototype found a byte-wise test that rejected `À` and accepted bytes of symbols; Go, Rust and Swift all classify decoded code points.

## Keywords

The language has **27 keywords**. They are Go's 25 keywords without `goto`, `fallthrough` and `const`, plus five constructs:

```text
break     case      chan      continue  default
defer     else      for       func      go
if        import    interface map       package
range     return    select    struct    switch
type      var
try       catch     uses      wire      machine
```

- `goto` and `fallthrough` are removed: structured control flow and union switches replace them. `const` is removed because package-level `var` is immutable (see [Declarations](#declarations)). *Prior art:* Go's keyword set, minus the three forms Gleam, Rust and Swift also do without.
- `try`, `catch`, `uses` and `wire` are constructs that the checker must know ([rows.md](rows.md)). `machine` declares a state machine ([runtime.md](runtime.md#machines-and-actors)). Constructs use keyword syntax; a dot form is always library (owner, 2026-10-12).
- **Contextual words.** `replace` (inside `wire`), `initial` and `step` (inside `machine`) are keywords only in those positions.
- **Prototype status.** The prototype treats `package`, `import`, `type`, `struct`, `interface`, `func`, `return`, `if`, `else`, `switch`, `case`, `wire`, `uses`, `try` and `catch` as keywords. It reserves `break`, `chan`, `continue`, `default`, `defer`, `for`, `go`, `map`, `range`, `select` and `var`, so a program that uses them is S001. It also reserves `const`, `goto` and `fallthrough`, because its output is Go. It does not reserve `machine` (measured: `machine := "m"` checks).

## Reserved names

1. A keyword cannot be an identifier.
2. **Predeclared language identifiers** are names the language declares in the universe block: `with`, `error` (as the head of an error row), the basic types (`string`, `bool`, `int64`), `duration`, and the prelude names of [stdlib.md](stdlib.md#prelude). As with Go's predeclared identifiers, a declaration may shadow them, except where the prototype's output reserves a name (point 3). In call position, `with` always denotes the construct, whatever else the name denotes (fixtures `with-name`, `prelude-locals`). `with` is a predeclared identifier rather than a keyword, so the keyword count stays at 27; it is still a compiler-known construct (glossary).
3. **Prototype limits (not language rules).** The prototype reserves, as S109: Go's predeclared identifiers, `rt`, `time`, `fmt`, `init`, `Error`, `timeout` and `duration`; every name that contains `__` or ends in `_`; and the field names `Error`, names with `__` and names ending in `_`. These keep its generated Go free of collisions. The main compiler must mangle generated names instead (README, Gap 16).

## Literals

```ebnf
int_lit      = "0" | ( "1" … "9" ) { decimal_digit } .
duration_lit = int_lit duration_unit .
duration_unit = "ns" | "us" | "ms" | "s" | "m" | "h" .
string_lit   = interpreted_string | raw_string .
```

1. **Integers.** An integer literal is decimal. A leading zero on a literal longer than one digit is an error (S108), because Go would read it as octal. The value must fit `int64` (S108). Constant `+` folds at compile time, and an overflowing constant sum is an error (S108). Non-constant arithmetic wraps, as in Go. Hexadecimal, octal, binary, separators and floating-point literals are designed, with [#81](https://github.com/cevr/effra/issues/81).
2. **Durations.** An integer immediately followed by a unit is a `duration` literal: `500ms`, `2s`, `5m`. Its value in nanoseconds must fit `int64` (S108). Any other letter suffix is an error (S001). *Prior art:* Go writes `500 * time.Millisecond`; Kotlin and Gleam libraries write `500.milliseconds`; Effra takes the literal form of [#175](https://github.com/cevr/effra/issues/175) because a deadline is a checked argument of `timeout.After`.
3. **Strings.** An interpreted string `"…"` uses Go's escapes and is decoded with Go's rules. An invalid escape is an error (S108). A raw string is enclosed in back quotes. A newline inside an interpreted string, or a missing closing quote, is an error (S001). The value of a string is the bytes its spelling decodes to. A raw byte order mark, NUL or invalid UTF-8 inside a literal is that byte, and the emitter re-quotes it (fixtures `bom-string`, `escaped-bom`).
4. Rune literals, `true` and `false` are designed. In the prototype, `bool` values come only from comparisons.

## Operators and punctuation

Prototype: `{ } ( ) [ ] , ; . : = + - * / | < > !` and `:= == != && || <= >=`. Designed: the rest of Go's operator set, including `%`, `&`, `<-`, `...`, `++`, `--` and the assignment operators. [types.md](types.md#operators) states which operators type-check.

Binary precedence, from loosest to tightest: `||`; `&&`; `==`, `!=`, `<`, `<=`, `>`, `>=`; `+`, `-`; `*`, `/`. This is Go's order for the operators the prototype parses.

## Declarations

```ebnf
File        = PackageClause ";" { ImportDecl ";" } { TopLevelDecl ";" } .
PackageClause = "package" identifier .
ImportDecl  = "import" string_lit .                         (* prototype *)
TopLevelDecl = TypeDecl | FuncDecl | WireDecl .

TypeDecl    = "type" identifier ( StructType | CapabilityType | UnionType ) .
StructType  = "struct" "{" { identifier Type ";" } "}" .
CapabilityType = "interface" "{" { identifier Signature ";" } "}" .
UnionType   = "interface" "{" identifier { "|" identifier } "}" .
Type        = identifier [ "." identifier ] .               (* prototype: names only *)

FuncDecl    = "func" [ Receiver ] identifier Signature Block .
Receiver    = "(" [ identifier ] identifier ")" .
Signature   = "(" [ Param { "," Param } ] ")" [ Result ] [ UsesClause ] .
Param       = identifier Type .
Result      = Type | "(" Type "," ErrorRow ")" | ErrorRow .
ErrorRow    = "error" "[" QualifiedName { "|" QualifiedName } "]" .
UsesClause  = "uses" QualifiedName { "," QualifiedName } .
QualifiedName = identifier [ "." identifier ] .

WireDecl    = "wire" identifier WireBody .
WireBody    = "{" { WireEntry ";" } "}" .
WireEntry   = identifier "=" Expression .
```

1. **Package clause.** A file starts with `package name`. Prototype: one file is the whole program, and there are no packages.
2. **Imports.** Prototype: one quoted path per `import`; `timeout` is the only path with meaning. Designed: Go's grouped `import ( … )`, the dot import `. "html"` for the `html` package ([stdlib.md](stdlib.md#html)), and one Go package per source package.
3. **Go imports.** Designed: `import go "path"` imports a Go package. Every call through it charges the predeclared `go.Foreign` capability, so host access appears in rows and footprints, and a Go panic becomes a defect. A Go result that may be nil becomes `Option`; `(T, error)` becomes `(T, error[go.Error])`; slices and maps are copied at the boundary. A bodiless declaration with a `//ef:go pkg.Func` directive binds one Go function with stated types, and its generated adapter checks the claims. *Prior art:* Gleam `@external`, ReScript `external`, Go's bodiless declarations and `//go:linkname`, Kotlin platform types (rejected for `Option`). README Gap 21 records what is open.
4. **Type declarations.** `type X struct {…}`, `type X interface { methods }` and `type X interface{ A | B }` are the prototype's three forms. An `interface` body is a union when its first entry is a name not followed by `(`. Designed: generic type parameters, `machine` types ([runtime.md](runtime.md#machines-and-actors)), struct embedding inside interfaces and `wire` only.
5. **Functions and methods.** A function result is one of: nothing; a value type `T`; an error row alone, `error[E]`; or `(T, error[E])`. A receiver names a struct type declared in the package. Designed: several results, variadic parameters, `*T` receivers and parameters ([types.md](types.md#borrows)).
6. **Package-level `var`.** Designed: a package-level `var` is immutable. There are no package-level mutable variables, no `init` function and no initialisation order to learn. *Prior art:* Gleam module constants, Rust `static` without `mut`.
7. **Visibility.** Designed. README Gap 8 records the open choice between `pub` and Go's capitalisation.

## Statements

```ebnf
Block      = "{" { Statement ";" } "}" .
Statement  = DefineStmt | ReturnStmt | IfStmt | TypeSwitchStmt | ExprStmt .   (* prototype *)
DefineStmt = identifier ":=" Expression .
ReturnStmt = "return" [ Expression ] .
IfStmt     = "if" Expression Block [ "else" Block ] .
TypeSwitchStmt = "switch" identifier ":=" PrimaryExpr "." "(" "type" ")"
                 "{" { "case" QualifiedName { "," QualifiedName } ":" { Statement ";" } } "}" .
ExprStmt   = Expression .
```

1. `x := e` declares a new local. Prototype: `=` reassignment does not exist. Designed: `:=` declares only new names and `=` reassigns, without Go's "at least one new variable" rule.
2. `if` takes a block after `else`. `else if` is designed; the prototype rejects it (S001).
3. The prototype's type switch has one subject and a binding. Designed: `default`, `case _`, and the multi-subject form `switch a, b := x.(type), y.(type)` with tuple cases `case (A, B):` (README Gap 12).
4. Designed statements: `var` declarations with definite initialisation, assignment, `for` and `for … range`, `go`, `defer` of any simple statement (including `defer done <- i`), channel send `ch <- v`, `select`, `break` and `continue`.
5. A bare block, a `with` block and a `timeout.After` block yield their last expression. `return`, `break` and `continue` are errors inside them (S109 in the prototype). A `func` literal's `return` returns from the literal, as in Go. `catch` arms are not blocks, so a designed `return` in an arm leaves the enclosing function. *Prior art:* Kotlin inline lambdas allow non-local return; Go has none, and Effra follows Go.

## Expressions

```ebnf
Expression  = [ "try" ] CatchExpr .
CatchExpr   = Binary [ "catch" "{" { CatchArm } "}" ] .
CatchArm    = "case" QualifiedName { "," QualifiedName } ":" Expression .       (* prototype *)
Binary      = Unary { binary_op Unary } .
Unary       = PostfixExpr .
PostfixExpr = PrimaryExpr { Selector | Call } .
Selector    = "." identifier .
Call        = "(" [ Expression { "," Expression } ] ")" [ TrailingBlock ] .
TrailingBlock = Block .
PrimaryExpr = literal | identifier | CompositeLit | WireLit | "(" Expression ")" .
CompositeLit = identifier "{" [ Element { "," Element } [ "," ] ] "}" .
Element     = [ identifier ":" ] Expression .
WireLit     = "wire" WireBody .
```

1. **`try` and `catch`** bind looser than every binary operator. `try e catch { … }` is one partial catch ([rows.md](rows.md#catch)). Designed: `catch e { … }` binds the caught error, and an arm holds statements.
2. **Composite literals** are all keyed or all positional. Mixing them is an error (S001). Prototype: a composite literal needs a type name that starts with an upper-case ASCII letter, and it is not parsed in an `if` condition or a switch subject, as in Go.
3. **Trailing blocks.** Prototype: only `with(…)` and `timeout.After(…)` take a trailing block. Designed: a final parameter of function type takes a trailing block at any call; such a block cannot escape the call ([types.md](types.md#function-values)). *Prior art:* Swift trailing closures, Kotlin trailing lambdas, Goose compile-time function values.
4. **Wire literals** appear only as the argument of `with` (S108 otherwise).
5. **`go` expressions.** Designed: `go f(x)` and `go { … }` start a task and yield a `Task` ([runtime.md](runtime.md#tasks)).
