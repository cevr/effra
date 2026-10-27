# Grammar

Status: the parser `internal/sketch/syntax` ([#195](https://github.com/cevr/effra/issues/195), 2026-10-27) **implements every production of this file**, those marked "prototype" and those marked "designed", and the Go productions listed in [Parser](#parser). A syntax error is S001 ([diagnostics](diagnostics.md)). The marks now state what the checker accepts: the prototype checker (`internal/sketch/proto`, from `prototypes/sketch9`) checks the "prototype" forms, and the bridge in front of it refuses every other parsed form with S109 until the checker of [#196](https://github.com/cevr/effra/issues/196). "Designed" means designed and parsed, not yet checked.

The grammar follows [the Go specification](https://go.dev/ref/spec) wherever this file is silent. A Go form that this file does not admit is an error, never a passthrough to the host (AGENTS.md).

## Source text

1. A source file is UTF-8 text. A byte sequence that is not valid UTF-8 is an error in identifier position. Inside a string literal, the literal's decoded bytes are its value (see [Literals](#literals)).
2. White space is space (U+0020), tab, carriage return and newline. Newlines are significant for semicolon insertion.
3. A comment starts with `//` and runs to the end of the line. A block comment `/* … */` does not nest, as in Go; an unterminated block comment is S001. A block comment that spans lines counts as a newline for semicolon insertion.
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
- `try`, `catch`, `uses` and `wire` are constructs that the checker must know ([rows.md](rows.md)). `machine` declares a state machine ([runtime.md](runtime.md#machines-and-actors)). Constructs use keyword syntax; a dot form is always library (owner, 2026-10-12). `timeout.After` is therefore a library function, not a construct ([runtime.md](runtime.md#timeouts)).
- **Contextual words.** `replace` (inside `wire`), `initial` and `step` (inside `machine`) are keywords only in those positions.
- **Parser status.** The parser of #195 treats all 27 words as keywords (`machine := 1` is S001) and refuses the removed words as names ([Parser](#parser) rule 4).
- **Prototype status (historical).** The prototype `prototypes/sketch9` treats `package`, `import`, `type`, `struct`, `interface`, `func`, `return`, `if`, `else`, `switch`, `case`, `wire`, `uses`, `try` and `catch` as keywords. It reserves `break`, `chan`, `continue`, `default`, `defer`, `for`, `go`, `map`, `range`, `select` and `var`, so a program that uses them is S001. It also reserves `const`, `goto` and `fallthrough`, because its output is Go. It does not reserve `machine` (measured: `machine := "m"` checks).

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
7. **Visibility.** See [Visibility](#visibility).

## Visibility

Status: **designed, not yet implemented**. The prototype compiles one file, so visibility does not arise there. Owner decision, 2026-10-27 (README Gap 8).

1. A package-level name, a struct field or a method is **exported** when its first character is an upper-case letter (Unicode category Lu). Every other name is unexported.
2. Another package may name only exported declarations, fields and methods.
3. Outside its package, a struct type with an unexported field has no composite literal, not even `Quantity{}`, and no implicit zero value ([types.md](types.md#zero-values-and-definite-initialisation), rule 6). It can be built there only through the functions its package exports (`ParseQuantity` for `Quantity{units}`). Field privacy alone does not give this: Go lets another package write `Quantity{}` and `make([]Quantity, 1)`.
4. There is no `pub` keyword. The 2026-10-08 decision for explicit `pub` and `pub readonly` is superseded.
5. Exported functions spell their rows ([rows.md](rows.md#capability-rows)).

*Prior art:* Go exports by capitalisation, which this rule copies; Gleam, Rust and MoonBit use a `pub` keyword; Elixir uses `def` against `defp`. Effra takes Go's rule because it adds no keyword and every sketch already relies on it.

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
2. `if` takes a block or another `if` after `else`. The bridge desugars `else if` to a nested `if` for the prototype checker.
3. The prototype's type switch has one subject and a binding. Designed: `default`, `case _`, and the multi-subject form `switch a, b := x.(type), y.(type)` with tuple cases `case (A, B):` (README Gap 12).
4. Designed statements: `var` declarations with definite initialisation, assignment, `for` and `for … range`, `go`, `defer` of any simple statement (including `defer done <- i`), channel send `ch <- v`, `select`, `break` and `continue`.
5. A bare block, a `with` block and a trailing block (such as the block of `timeout.After`) yield their last expression. `return`, `break` and `continue` are errors inside them (S109 in the prototype, which applies the ban to `with` and `timeout.After` blocks). A `func` literal's `return` returns from the literal, as in Go. `catch` arms are not blocks, so a designed `return` in an arm leaves the enclosing function. *Prior art:* Kotlin inline lambdas allow non-local return; Go has none, and Effra follows Go.

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
3. **Trailing blocks.** Designed: a trailing block is notation for a function-literal argument, over a restricted domain. A trailing block's body may not contain `return`, `break` or `continue`, because a block reads like a statement block and a local `return` would look like a return from the enclosing function. Within that domain, `f(a) { body }` is exactly `f(a, func() T { return body })`, where `T` and the literal's rows come from `f`'s final parameter type, and the block's last expression is the literal's result. To return early from the callback, write the explicit `func` literal; the two spellings are equivalent only for bodies without `return`. The notation adds no other checking: the call is checked as the desugared call, and the block is non-escaping unless the parameter type says otherwise ([types.md](types.md#function-values)). The prototype's S109 ban on `return` in a `timeout.After` block is its form of this rule. *Prior art:* Kotlin needs labelled returns (`return@forEach`) because a bare `return` in an inline lambda leaves the enclosing function, and Ruby's `return` in a block returns from the enclosing method; Effra avoids both readings by forbidding `return` in a block. `with(…) { … }` is the one construct that also takes a block; its block is part of the construct. Prototype: only `with(…)` and `timeout.After(…)` take a trailing block, and `timeout.After` is recognised by name (README Gap 31). *Prior art:* Swift trailing closures and Kotlin trailing lambdas are the same notation over closure arguments; Goose compile-time function values.
4. **Wire literals** appear only as the argument of `with` (S108 otherwise).
5. **`go` expressions.** Designed: `go f(x)` and `go { … }` start a task and yield a `Task` ([runtime.md](runtime.md#tasks)).

## Parser

Status: **implemented** in `internal/sketch/syntax` ([#195](https://github.com/cevr/effra/issues/195)). `syntax.Parse` returns one file's tree or the first S001, located by byte offset, line and byte column. It never returns a partial tree.

1. **Go's productions where this file is silent.** Where this file is silent, or marks a production "prototype" (`Type`, `Statement`, `CompositeLit`, `Call`, `Unary`, imports), the parser implements [Go's production](https://go.dev/ref/spec) for that form, without `goto`, `fallthrough` and `const`. That covers: grouped, named, dot and `go` imports; package-level and local `var`, single and grouped; type-parameter lists with constraints and explicit instantiation (`Pair[K, V]`, [rows-and-constraints.md](../research/rows-and-constraints.md) §3); `for`, `for … range` and `for range n`; init statements in `if`, `switch` and `for` headers, whose parts a semicolon or a line break separates (`if x := g()` and `x {` on the next line is one header, as in Go; `if x` and `{` on the next line is S001, because the condition is missing); `else if`; expression switches and `default`; labels with `break` and `continue` (`L: ;` labels an empty statement, so the statement after it is not labelled); assignment and the assignment operators; `++` and `--`; channel types, send and receive; `select`, whose receive may be parenthesised (`case (<-ch):`); a `:=` whose left side is names only, in a range clause too; `defer` of any simple statement; the unary operators, including `&` and `<-`; Go's full binary operator set and its five precedence levels; index and slice expressions; function literals; every composite-literal form (keys are expressions, inner types may be elided, and `[...]T{…}` takes its length from its elements; `[...]T` is S001 anywhere else); variadic parameters; several results; `*T`; rune literals; block comments. A composite literal and a trailing block are not parsed in an `if`, `for` or `switch` header unless parenthesised, as in Go.
2. **Effra forms from the prose of `docs/spec/`.** Error rows `error[…]`, including row variables; `uses` on declarations and on function types; `try`; `catch` and `catch e`, whose arms hold statements; single- and multi-subject type switches, with tuple cases and `_`; `machine[S, E] { initial …; step … }` with its contextual entry words; `wire` declarations and literals, with embedding, `replace` and qualified keys; a trailing block after any call; `go f(x)` and `go { … }`; bodiless declarations; `import go "path"` and `go.X` qualified names. An error row is admitted only in a result list. An error row, a catch arm and a union interface name types: each entry is a qualified name, with type arguments when it is generic (`retry.Exhausted[E]`); any other type (`[]int`, `*E`, `map[K]V`) is S001 at its first token.
3. **Type declarations** keep this file's forms (struct, capability interface, union interface) plus `machine` and a type-parameter list. Every other Go type declaration (`type A = B`, `type A B`, `type A []T`) is S001.
4. **Removed words.** `goto`, `fallthrough` and `const` are not keywords, so they scan as identifiers, but they are S001 wherever a name, a declaration or a statement starts. Generated Go reserves them, and a program that uses them is a Go program the language does not admit.
5. **Type assertions.** `x.(type)` is admitted only as the subject of a type switch, and `x.(T)` is S001: a union switch replaces it.
6. **Nesting limit.** Nesting of expressions, types, statements and blocks deeper than 1000 levels is S001. Each link of a chain counts as a level, as in `go/parser`: a selector, an index, a call, a composite literal, a binary operator and an `else if` each deepen the tree by one, so `a.b.c…` with 1001 links is S001. A hostile input therefore cannot exhaust the stack of the parser or of any consumer of the tree. *Prior art:* `go/parser` stops at a nesting depth of 100 000 with "exceeded max nesting depth"; the lower bound here keeps the formatter's recursion small.
7. **Paren-less trailing block.** A trailing block needs the call's parentheses: `sync.Atomically() { … }`. `sync.Atomically { … }` is S001, because without name resolution it cannot be told apart from the composite literal `sync.Atomically{ f() }`. Go needs parentheses around a composite literal in an `if` header for the same reason.

### Extensions beyond the written productions

The corpus of `docs/spec/README.md` needs these forms. Each one is parsed; the column "checker" states what the bridge does until #196.

| Form | Production | Checker |
| --- | --- | --- |
| `try` in operand position: `!try x`, `a + try x` | `Unary = … \| "try" Binary-rest`: `try` extends over the rest of the binary expression to its right, so it still binds looser than every binary operator (Expressions rule 1). *Prior art:* Swift needs parentheses here; Zig's `try` is a unary prefix. | Passed to the checker as `try` |
| `wire Name uses A, B { … }` | `WireDecl = "wire" identifier [ UsesClause ] WireBody .` | S109; meaning open ([rows-and-constraints.md](../research/rows-and-constraints.md) §7 item 9), [Gaps](README.md#gaps) |
| `uses (A, B)` in a function type inside a parameter or result list | `UsesClause = "uses" ( QualifiedName \| "(" QualifiedName { "," QualifiedName } ")" ) .` The parenthesised form is admitted only inside a list; there, an unparenthesised `uses` takes one name, because a comma would end the parameter. A declaration's own `uses` clause is unchanged. | S109 for function-typed parameters |
| Grouped struct field names: `From, To int` | `FieldDecl = identifier { "," identifier } Type .` (Go's `IdentifierList Type`) | Passed to the checker, one field per name |
| One provider for several capabilities: `Clock, Scheduler = testing.VirtualClock{}` | `WireEntry = QualifiedName { "," QualifiedName } "=" Expression .` (Go's `var a, b = …`). *Prior art:* Effect's `Layer.mergeAll` builds one layer for several services. | S109; meaning open, [Gaps](README.md#gaps) |
| A type parameter without a constraint: `type Attempt[T] struct { … }` | `TypeParam = identifier { "," identifier } [ Constraint ] .` The constraint defaults to `any` ([rows-and-constraints.md](../research/rows-and-constraints.md) §3; owner rule "generics: Go simplicity, implicit kinds"). **Difference from Go**, which requires `[T any]`. | S109 (type parameters) |
