# Diagnostics

Status: S001 is **implemented** by the parser `internal/sketch/syntax` ([#195](https://github.com/cevr/ergo/issues/195)); the other codes are **implemented in prototype** (the prototype checker, `internal/sketch/proto/check.go`, copied from `prototypes/sketch9/sketch/check.go`), and the bridge `internal/sketch/proto/convert.go` adds the S109 refusals of parsed forms the checker lacks. Each fixture's first line is `// want CODE`; `TestProbes` and `accept.sh` run them. The main compiler maps each rule to a code in its EF registry ([#79](https://github.com/cevr/ergo/issues/79); README Gap 18).

A diagnostic has a position, a code and a message: `file:line:col: CODE message`. A file with any diagnostic does not build. Fixture paths are relative to [`prototypes/sketch9/testdata`](../../prototypes/sketch9/testdata).

## Codes

| Code | Rule | Spec section | Fixture example |
| --- | --- | --- | --- |
| S001 | Source text must lex and parse: valid identifiers, terminated strings, known number suffixes, no Go form outside the language, no mixed keyed and positional literal. | [grammar](grammar.md) | `probes/nonletter-name.ergo` |
| S101 | Every error that can escape a function must be in its declared `fails` row. | [rows: error rows](rows.md#error-rows) | `reject/undeclared-error.ergo` |
| S102 | Every capability a body charges must be in its `needs` row or discharged by an enclosing `with`. | [rows: capability rows](rows.md#capability-rows) | `reject/missing-capability.ergo` |
| S103 | A `catch` without `try` must handle every error in the caught row. | [rows: catch](rows.md#catch) | `reject/catch-not-total.ergo` |
| S104 | Every name must resolve: locals, functions, types, fields, methods, capabilities, wires and error types. | [types](types.md), [rows](rows.md) | `reject/unknown-name.ergo` |
| S105 | A fallible expression must be tried, caught or returned; it cannot stand in a value position. | [rows: error rows](rows.md#error-rows) | `probes/composite-error.ergo` |
| S106 | A wire provider must implement its capability, with the same signatures and no wider error row. | [rows: wire](rows.md#wire) | `probes/provider-parameter.ergo` |
| S107 | A function with a result must not reach the end of its body. | [rows: totality](rows.md#totality) | `sketch/sketch_test.go`, case "a missing return" (README Gap 19) |
| S108 | Types and declarations must be used consistently: no type mismatch, duplicate declaration, invalid literal or misuse of a construct. | [types](types.md), [rows](rows.md) | `probes/duplicate-field.ergo` |
| S109 | The program must stay inside the implemented subset, and source names must not collide with the prototype's generated Go. | [grammar](grammar.md#reserved-names), README Gaps 6, 10, 16, 17 | `probes/with-return.ergo` |
| S110 | A declared local must be read, and an expression statement must be a call, `try` or `catch`, as in Go. | [grammar: statements](grammar.md#statements) | `probes/unused-expression.ergo` |
| S111 (designed) | A wire must not have two entries for one capability, except a `replace` of an embedded entry. | [rows: wire](rows.md#wire), README Gap 28 | none yet: the prototype accepts the program, and the later entry wins |
| S112 (designed) | A map's key type must not be a resource in v0. | [resources: containers](resources.md#containers), README Gap 32 | none yet: the prototype has no maps; negative example `map[Key]string{openKey(): "v"}` in resources.md |

Designed codes are not in the prototype. Other designed errors in `docs/spec/` (resources, visibility, capability values, channels of resources) get codes when [#196](https://github.com/cevr/ergo/issues/196) and [#197](https://github.com/cevr/ergo/issues/197) implement them.

## Rules by code

Each rule names a fixture where one exists.

### S001: syntax

The parser `internal/sketch/syntax` ([#196](https://github.com/cevr/ergo/issues/196)) reports S001 with a specific message ("expected X, found Y"), a byte offset and length, a line and a byte column. It reports the first error only. The formatter refuses a file with S001 on every surface, naming the code and position.

- An identifier is tested on decoded code points; a code point that cannot start or continue it, or invalid UTF-8, is S001 (`probes/nonletter-name.ergo`).
- A composite literal mixes keyed and positional elements (`probes/mixed-fields.ergo`).
- A Go habit outside the decided grammar: the message names the Ergo form, as `x := e` names `let x = e` and `defer f()` names `scope.OnExit` ([grammar: Old spellings](grammar.md#old-spellings) holds the whole table).
- An old Ergo-only spelling (`error[…]` rows, `wire Name { A = x }`, `catch e { … }`, `uses`, `try` in operand position): the plain S001, with no rename hint (#217).
- A keyword or reserved word used as a name (`let match = 1`), and a word that is not in the language (`enum`) where a declaration or statement starts.
- A wire value with `needs` before `provides`.
- A form the decided grammar does not admit: a local type declaration, a `~` constraint, a type parameter list on an operation, a trailing block without the call's parentheses (`task.Go { … }`), a statement-leading `if`, `match` or `for` continued by an operator, selector or call, `case limit + 1:` (a case lists patterns), a type that is not a type name in a `fails` row or a union (`fails []int`, `type U = []int | B`).
- A malformed construct: an unknown or duplicate machine entry, a machine without `step`, `needs (A, B)` on a declaration.
- An unterminated string or block comment, a newline inside an interpreted string, an unknown number suffix (`5kg`), an unexpected character, an import after a declaration.
- A tree higher than 1000 levels, in any composition of chains, operators, parentheses, types, statements and blocks ([grammar](grammar.md#parser) rule 5).

A type that mixes fields with operations or laws is S108 from the parser ([grammar](grammar.md#declarations) rule 5). The fixtures are `sketchtest.MalformedTable` (`TestMalformed`) and `testdata/refuse` (`TestRefusedFixtures`) in `internal/sketch/syntax`.

### S101: undeclared error

- An error escapes through `try` and is not declared (`reject/undeclared-error.ergo`).
- A function with no row returns a value of an error type (`probes/error-as-value.ergo`).
- A partial `try … catch` leaves an undeclared error (`sketch/sketch_test.go`, "partial catch under try still needs the rest declared").
- An inline wire's provider expression can fail, and the enclosing function does not declare it (`probes/wire-initializer-error.ergo`).
- A named wire's provider can fail while it is built (`probes/named-wire-failure.ergo`; README Gap 6).

### S102: missing capability

- A body calls a capability its `needs` row does not list (`reject/missing-capability.ergo`).
- A callee needs a capability that no `with` provides (`reject/unprovided.ergo`, `probes/function-needs-control.ergo`).
- A provider expression of an inline wire needs a capability of its own wire (`probes/wire-sibling.ergo`) or one the enclosing function lacks (`probes/wire-initializer.ergo`).
- A named wire needs a capability from its caller (`probes/named-wire-requirement.ergo`; README Gap 6).
- `timeout.After` without `Scheduler` (`sketch/sketch_test.go`, "timeout requires Scheduler").
- `main` lists a capability the host does not provide (`sketch/sketch_test.go`, "main cannot require what the host lacks"; README Gap 5).

### S103: catch not total

- A `catch` without `try` omits an error of the caught row (`reject/catch-not-total.ergo`).

### S104: unknown name

- An unknown local, function, type, struct type, field, method, capability or wire (`reject/unknown-name.ergo`).
- An error row names a type that is not a struct declared in the file (`probes/host-error.ergo`).
- Prototype limit: `duration` as a type name, as in `func delay(d duration)` (README Gap 25).
- A capability name used as a bare expression, such as `f(Orders)` (README Gap 29).

### S105: unhandled failure

- A fallible call in a struct field value (`probes/composite-error.ergo`).
- A fallible `timeout.After` duration (`probes/fallible-duration.ergo`).
- A fallible expression as a `let` value, an expression statement, an operand, a selector receiver, a call argument, an `if` condition, a `match` subject or a `catch` arm value (`sketch/sketch_test.go`, "an unhandled fallible call").

### S106: bad provider

- A provider method's parameter types differ from the capability's (`probes/provider-parameter.ergo`).
- A provider that the host capability `Scheduler` does not take (`probes/provider-scheduler.ergo`).
- A provider without the capability's method, or whose method row is wider (`sketch/sketch_test.go`, "a provider without the method", "a provider whose row is wider").

### S107: missing return

- A function with a result whose body can reach its end (`sketch/sketch_test.go`, "a missing return"). A body terminates only through a final `return`, an `if` with an `else` whose branches terminate, or a type `match` whose cases all terminate and name every variant.

### S108: type or declaration misuse

| Rule | Fixture |
| --- | --- |
| A type, function, wire, method, capability method, field or parameter is declared twice; a type and a function share a name | `probes/duplicate-field.ergo`, `duplicate-function`, `duplicate-wire`, `duplicate-method`, `duplicate-cap-method`, `duplicate-parameter`, `type-function-name` |
| A parameter repeats the receiver's name | `probes/receiver-parameter.ergo` |
| A field and a method of one type share a name | `probes/field-method-name.ergo` |
| A receiver or union variant is not a struct declared in the file | `probes/host-method.ergo`, `host-union` |
| A struct contains itself by value | `probes/recursive-product.ergo` |
| An integer literal has a leading zero or overflows `int64`; a constant sum overflows | `probes/decimal-leading-zero.ergo`, `octal-value`, `integer-constant` |
| A duration overflows `int64` nanoseconds | `probes/duration-overflow.ergo` |
| A string literal has an invalid escape | `probes/string-escape.ergo` |
| A keyed literal omits a field that has no zero value | `probes/union-field-zero.ergo`, `transitive-nondefaultable` |
| A type is caught twice | `probes/duplicate-catch.ergo` |
| A `catch` over a no-value expression has an arm with a value | `probes/void-catch-value.ergo` |
| An argument has no value | `probes/void-argument.ergo` |
| A no-value function returns a value, including `return try f()` | `probes/void-fallible-return-value.ergo`, `try-unit-return` |
| A local shadows a function and is then called | `probes/shadowed-call.ergo` |
| Prototype limit: a struct value assigned to an interface type, such as `return Mem{}` from `func NewStore() Store`; the designed rule is [interface assignability](types.md#interface-assignability) (README Gap 29) | the checker's type-mismatch message |
| Others: `try` or `catch` on an infallible expression; a caught type not in the row; an arm value of the wrong type; `let` with no value or a redeclared name; a non-`bool` condition; a type `match` on a non-union; a case that is not a variant or is repeated; wrong return type; a field set twice or of the wrong type; a wrong argument count or type; an uncalled capability method; a wire literal outside `with`; wrong `with` or `timeout.After` arguments; mismatched `==` or `+` operands | `sketch/sketch_test.go` and the checker's messages |

### S109: outside the subset or reserved

| Rule | Fixture |
| --- | --- |
| `return` inside a `with` or `timeout.After` block | `probes/with-return.ergo`, `with-nonlocal-return` |
| A block that does not end with an expression | `probes/empty-block.ergo`, `timeout-statement-last` |
| A wire that needs itself, directly or through provider methods | `probes/cyclic-wire.ergo`, `wire-method-cycle` |
| An operator other than `==` and `+`, or `==` on a type other than `string`, `int64` and `bool`, including `duration` (README Gaps 10, 26) | `probes/operators.ergo` |
| A method call on a value, such as `S{}.M()`, or on an interface-typed parameter (README Gaps 27, 29) | the checker's "unsupported call" message |
| A `needs` clause on a capability method or method; a capability call in a provider method | `probes/capability-method-needs.ergo` |
| `main` is called, takes parameters, returns a value or is missing | `probes/main-call.ergo`, `main-parameter` |
| A source name is reserved by the prototype's Go output: Go's predeclared names, `rt`, `time`, `fmt`, `init`, `Error`, `timeout`, `duration`, names with `__` or ending in `_` | `probes/predeclared-error.ergo`, `predeclared-local`, `predeclared-panic`, `function-reserved`, `init-name`, `timeout-capability`, `duration-type-name`, `wire-predeclared-name`, `capability-parameter-name` |
| A field or method name collides with generated names (`Error`, marker methods) | `probes/marker-field.ergo`, `marker-method`, `error-method` |
| A wire entry that is not a provider literal; an unsupported expression or call | the checker's messages |
| A parsed form the prototype checker does not implement, refused by the bridge (`internal/sketch/proto/convert.go`) before the checker runs, with the message "X is outside the implemented subset": a top-level `let` that is not a wire, a local `let` with a type, several names or no value, type-parameter lists and instantiation, `machine` types, `extern` declarations, aliased and `import go` imports, unnamed parameters, tuple results, pointers, `=` and assignment operators, `for`, `break`, `continue`, a `match` on values, a `match` on a tuple, a type `match` without an `as` binding or with two binding names, `case _`, rune literals, unary operators, index and slice expressions, function literals, a trailing block on a call other than `with` and `timeout.After`, elided or qualified composite-literal types, a `catch` arm that is not one expression, a wire value's `provides` and `needs` (the message names #196, which checks them), an embedded wire, `replace`, a wire entry with several or qualified keys. A refused program never reaches the checker, so no part of it is dropped | `internal/sketch/proto/bridge_test.go`, `TestBridgeRefusesOutsideTheSubset` |

The S109 rules are prototype limits, not language rules (README Gaps 4, 6, 7, 10, 16, 17, 26, 27, 29, 31). The main compiler replaces each with the designed rule or with mangled output.

### S110: unused

- A local is declared and never read (`sketch/sketch_test.go`, "an unused variable"; `probes/same-local-as-cap.ergo`).
- An `as` binding of a type `match` is read in no case.
- An expression statement is not a call, `try` or `catch` (`probes/unused-expression.ergo`, `temp-collision`).

*Prior art:* Go rejects unused locals and unused expression values; Rust and Gleam warn instead. Ergo keeps Go's errors because its output is Go and because an unread value usually hides a dropped result.
