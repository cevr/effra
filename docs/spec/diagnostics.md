# Diagnostics

Status: S001 is **implemented** by the parser `internal/sketch/syntax` ([#195](https://github.com/cevr/effra/issues/195)); the other codes are **implemented in prototype** (the prototype checker, `internal/sketch/proto/check.go`, copied from `prototypes/sketch9/sketch/check.go`), and the bridge `internal/sketch/proto/convert.go` adds the S109 refusals of parsed forms the checker lacks. Each fixture's first line is `// want CODE`; `TestProbes` and `accept.sh` run them. The main compiler maps each rule to a code in its EF registry ([#79](https://github.com/cevr/effra/issues/79); README Gap 18).

A diagnostic has a position, a code and a message: `file:line:col: CODE message`. A file with any diagnostic does not build. Fixture paths are relative to [`prototypes/sketch9/testdata`](../../prototypes/sketch9/testdata).

## Codes

| Code | Rule | Spec section | Fixture example |
| --- | --- | --- | --- |
| S001 | Source text must lex and parse: valid identifiers, terminated strings, known number suffixes, no Go form outside the language, no mixed keyed and positional literal. | [grammar](grammar.md) | `probes/nonletter-name.sketch` |
| S101 | Every error that can escape a function must be in its declared `error[...]` row. | [rows: error rows](rows.md#error-rows) | `reject/undeclared-error.sketch` |
| S102 | Every capability a body charges must be in its `uses` row or discharged by an enclosing `with`. | [rows: capability rows](rows.md#capability-rows) | `reject/missing-capability.sketch` |
| S103 | A `catch` without `try` must handle every error in the caught row. | [rows: catch](rows.md#catch) | `reject/catch-not-total.sketch` |
| S104 | Every name must resolve: locals, functions, types, fields, methods, capabilities, wires and error types. | [types](types.md), [rows](rows.md) | `reject/unknown-name.sketch` |
| S105 | A fallible expression must be tried, caught or returned; it cannot stand in a value position. | [rows: error rows](rows.md#error-rows) | `probes/composite-error.sketch` |
| S106 | A wire provider must implement its capability, with the same signatures and no wider error row. | [rows: wire](rows.md#wire) | `probes/provider-parameter.sketch` |
| S107 | A function with a result must not reach the end of its body. | [rows: totality](rows.md#totality) | `sketch/sketch_test.go`, case "a missing return" (README Gap 19) |
| S108 | Types and declarations must be used consistently: no type mismatch, duplicate declaration, invalid literal or misuse of a construct. | [types](types.md), [rows](rows.md) | `probes/duplicate-field.sketch` |
| S109 | The program must stay inside the implemented subset, and source names must not collide with the prototype's generated Go. | [grammar](grammar.md#reserved-names), README Gaps 6, 10, 16, 17 | `probes/with-return.sketch` |
| S110 | A declared local must be read, and an expression statement must be a call, `try` or `catch`, as in Go. | [grammar: statements](grammar.md#statements) | `probes/unused-expression.sketch` |
| S111 (designed) | A wire must not have two entries for one capability, except a `replace` of an embedded entry. | [rows: wire](rows.md#wire), README Gap 28 | none yet: the prototype accepts the program, and the later entry wins |
| S112 (designed) | A map's key type must not be a resource in v0. | [resources: containers](resources.md#containers), README Gap 32 | none yet: the prototype has no maps; negative example `map[Key]string{openKey(): "v"}` in resources.md |

Designed codes are not in the prototype. Other designed errors in `docs/spec/` (resources, visibility, capability values, channels of resources) get codes when [#196](https://github.com/cevr/effra/issues/196) and [#197](https://github.com/cevr/effra/issues/197) implement them.

## Rules by code

Each rule names a fixture where one exists.

### S001: syntax

The parser `internal/sketch/syntax` ([#195](https://github.com/cevr/effra/issues/195)) reports S001 with a specific message ("expected X, found Y"), a byte offset and length, a line and a byte column. It reports the first error only. The 2026 formatter refuses a file with S001 on every surface, naming the code and position.

- An identifier is tested on decoded code points; a code point that cannot start or continue it, or invalid UTF-8, is S001 (`probes/nonletter-name.sketch`).
- A composite literal mixes keyed and positional elements (`probes/mixed-fields.sketch`).
- A removed Go keyword (`goto`, `fallthrough`, `const`) or a word that is not in the language (`enum`) where a name, declaration or statement starts; a keyword used as a name (`machine := 1`).
- A Go form the grammar does not admit: a type alias or another defined type (`type A = B`, `type A B`, `type A []T`), a type assertion `x.(T)`, `x.(type)` outside a type switch subject, a local type declaration, an error row outside a result list, an embedded struct field, a `~` constraint, a method with type parameters, `default` in a `catch`, a trailing block without the call's parentheses (`sync.Atomically { … }`), `[...]T` outside a composite literal, a non-name on the left of `:=` (`for a.x := range xs`), a type that is not a type name in an error row, a catch arm or a union (`error[[]int]`, `case *E:`, `interface{ []int | B }`).
- A malformed construct: a multi-subject type switch without bindings, a tuple case whose arity differs from the subjects, a tuple case in a single-subject switch, an unknown or duplicate machine entry, a machine without `step`, a union interface with a method, `uses (A, B)` on a declaration, a variadic parameter that is not last.
- An unterminated string or block comment, a newline inside an interpreted string, an unknown number suffix (`5kg`), an unexpected character, an import after a declaration, a union with a trailing `|`.
- Nesting deeper than 1000 levels ([grammar](grammar.md#parser) rule 6).

The fixtures are `sketchtest.MalformedTable` and the `TestRefusedForms` cases in `internal/sketch/syntax`.

### S101: undeclared error

- An error escapes through `try` and is not declared (`reject/undeclared-error.sketch`).
- A function with no row returns a value of an error type (`probes/error-as-value.sketch`).
- A partial `try … catch` leaves an undeclared error (`sketch/sketch_test.go`, "partial catch under try still needs the rest declared").
- An inline wire's provider expression can fail, and the enclosing function does not declare it (`probes/wire-initializer-error.sketch`).
- A named wire's provider can fail while it is built (`probes/named-wire-failure.sketch`; README Gap 6).

### S102: missing capability

- A body calls a capability its `uses` row does not list (`reject/missing-capability.sketch`).
- A callee needs a capability that no `with` provides (`reject/unprovided.sketch`, `probes/function-uses-control.sketch`).
- A provider expression of an inline wire needs a capability of its own wire (`probes/wire-sibling.sketch`) or one the enclosing function lacks (`probes/wire-initializer.sketch`).
- A named wire needs a capability from its caller (`probes/named-wire-requirement.sketch`; README Gap 6).
- `timeout.After` without `Scheduler` (`sketch/sketch_test.go`, "timeout requires Scheduler").
- `main` lists a capability the host does not provide (`sketch/sketch_test.go`, "main cannot require what the host lacks"; README Gap 5).

### S103: catch not total

- A `catch` without `try` omits an error of the caught row (`reject/catch-not-total.sketch`).

### S104: unknown name

- An unknown local, function, type, struct type, field, method, capability or wire (`reject/unknown-name.sketch`).
- An error row names a type that is not a struct declared in the file (`probes/host-error.sketch`).
- Prototype limit: `duration` as a type name, as in `func delay(d duration)` (README Gap 25).
- A capability name used as a bare expression, such as `f(Orders)` (README Gap 29).

### S105: unhandled failure

- A fallible call in a struct field value (`probes/composite-error.sketch`).
- A fallible `timeout.After` duration (`probes/fallible-duration.sketch`).
- A fallible expression as a `:=` value, an expression statement, an operand, a selector receiver, a call argument, an `if` condition, a switch subject or a `catch` arm value (`sketch/sketch_test.go`, "an unhandled fallible call").

### S106: bad provider

- A provider method's parameter types differ from the capability's (`probes/provider-parameter.sketch`).
- A provider that the host capability `Scheduler` does not take (`probes/provider-scheduler.sketch`).
- A provider without the capability's method, or whose method row is wider (`sketch/sketch_test.go`, "a provider without the method", "a provider whose row is wider").

### S107: missing return

- A function with a result whose body can reach its end (`sketch/sketch_test.go`, "a missing return"). A body terminates only through a final `return`, an `if` with an `else` whose branches terminate, or a type switch whose cases all terminate and name every variant.

### S108: type or declaration misuse

| Rule | Fixture |
| --- | --- |
| A type, function, wire, method, capability method, field or parameter is declared twice; a type and a function share a name | `probes/duplicate-field.sketch`, `duplicate-function`, `duplicate-wire`, `duplicate-method`, `duplicate-cap-method`, `duplicate-parameter`, `type-function-name` |
| A parameter repeats the receiver's name | `probes/receiver-parameter.sketch` |
| A field and a method of one type share a name | `probes/field-method-name.sketch` |
| A receiver or union variant is not a struct declared in the file | `probes/host-method.sketch`, `host-union` |
| A struct contains itself by value | `probes/recursive-product.sketch` |
| An integer literal has a leading zero or overflows `int64`; a constant sum overflows | `probes/decimal-leading-zero.sketch`, `octal-value`, `integer-constant` |
| A duration overflows `int64` nanoseconds | `probes/duration-overflow.sketch` |
| A string literal has an invalid escape | `probes/string-escape.sketch` |
| A keyed literal omits a field that has no zero value | `probes/union-field-zero.sketch`, `transitive-nondefaultable` |
| A type is caught twice | `probes/duplicate-catch.sketch` |
| A `catch` over a no-value expression has an arm with a value | `probes/void-catch-value.sketch` |
| An argument has no value | `probes/void-argument.sketch` |
| A no-value function returns a value, including `return try f()` | `probes/void-fallible-return-value.sketch`, `try-unit-return` |
| A local shadows a function and is then called | `probes/shadowed-call.sketch` |
| Prototype limit: a struct value assigned to an interface type, such as `return Mem{}` from `func NewStore() Store`; the designed rule is [interface assignability](types.md#interface-assignability) (README Gap 29) | the checker's type-mismatch message |
| Others: `try` or `catch` on an infallible expression; a caught type not in the row; an arm value of the wrong type; `:=` with no value or a redeclared name; a non-`bool` condition; a type switch on a non-union; a case that is not a variant or is repeated; wrong return type; a field set twice or of the wrong type; a wrong argument count or type; an uncalled capability method; a wire literal outside `with`; wrong `with` or `timeout.After` arguments; mismatched `==` or `+` operands | `sketch/sketch_test.go` and the checker's messages |

### S109: outside the subset or reserved

| Rule | Fixture |
| --- | --- |
| `return` inside a `with` or `timeout.After` block | `probes/with-return.sketch`, `with-nonlocal-return` |
| A block that does not end with an expression | `probes/empty-block.sketch`, `timeout-statement-last` |
| A wire that needs itself, directly or through provider methods | `probes/cyclic-wire.sketch`, `wire-method-cycle` |
| An operator other than `==` and `+`, or `==` on a type other than `string`, `int64` and `bool`, including `duration` (README Gaps 10, 26) | `probes/operators.sketch` |
| A method call on a value, such as `S{}.M()`, or on an interface-typed parameter (README Gaps 27, 29) | the checker's "unsupported call" message |
| A `uses` clause on a capability method or method; a capability call in a provider method | `probes/capability-method-uses.sketch` |
| `main` is called, takes parameters, returns a value or is missing | `probes/main-call.sketch`, `main-parameter` |
| A source name is reserved by the prototype's Go output: Go's predeclared names, `rt`, `time`, `fmt`, `init`, `Error`, `timeout`, `duration`, names with `__` or ending in `_` | `probes/predeclared-error.sketch`, `predeclared-local`, `predeclared-panic`, `function-reserved`, `init-name`, `timeout-capability`, `duration-type-name`, `wire-predeclared-name`, `capability-parameter-name` |
| A field or method name collides with generated names (`Error`, marker methods) | `probes/marker-field.sketch`, `marker-method`, `error-method` |
| A wire entry that is not a provider literal; an unsupported expression or call | the checker's messages |
| A parsed form the prototype checker does not implement, refused by the bridge (`internal/sketch/proto/convert.go`) before the checker runs, with the message "X is outside the implemented subset": `var` declarations, type-parameter lists and instantiation, `machine` types, bodiless declarations, non-plain imports, unnamed or variadic parameters, several results, pointers, `=` and assignment operators, multi-value `:=`, `for`, `range`, `defer`, `select`, expression switches, labels, `break`, `continue`, send, `++`, `--`, bare blocks, `if` and `switch` init statements, multi-subject, unbound or `default` type switches, `case _`, rune literals, unary operators, index and slice expressions, function literals, `go`, a trailing block on a call other than `with` and `timeout.After`, elided or qualified composite-literal types, `catch e`, a `catch` arm that is not one expression, `wire X uses …`, an embedded wire, `replace`, a wire entry with several or qualified keys. A refused program never reaches the checker, so no part of it is dropped | `internal/sketch/proto/bridge_test.go`, `TestBridgeRefusesOutsideTheSubset` |

The S109 rules are prototype limits, not language rules (README Gaps 4, 6, 7, 10, 16, 17, 26, 27, 29, 31). The main compiler replaces each with the designed rule or with mangled output.

### S110: unused

- A local is declared and never read (`sketch/sketch_test.go`, "an unused variable"; `probes/same-local-as-cap.sketch`).
- A type-switch binding is read in no case.
- An expression statement is not a call, `try` or `catch` (`probes/unused-expression.sketch`, `temp-collision`).

*Prior art:* Go rejects unused locals and unused expression values; Rust and Gleam warn instead. Effra keeps Go's errors because its output is Go and because an unread value usually hides a dropped result.
