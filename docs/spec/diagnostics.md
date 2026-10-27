# Diagnostics

Status: S001 is **implemented** by the parser `internal/sketch/syntax` ([#195](https://github.com/cevr/effra/issues/195)). The other codes are **implemented** by the checker `internal/sketch/check` ([#196](https://github.com/cevr/effra/issues/196)), which `ef check`, `ef diagnostics`, MCP `project.check` and `project.diagnostics`, and LSP diagnostics run for 2026 sources. The checker's code table (`internal/sketch/check/codes.go`) mirrors this page; a test keeps the table, the emitted codes and the rows below in step. `ef build` and `ef run` run the checker first, then lower through the prototype emitter (`internal/sketch/proto`) until [#50](https://github.com/cevr/effra/issues/50) lowers from the checker's output; a program the prototype cannot lower is refused at build with S109 naming #50, and nothing is written. The checker's fixtures are in `internal/sketch/check/testdata/fixtures`; the prototype's fixtures (first line `// want CODE`) keep their outcome except the changes [the parity table](../../internal/sketch/check/testdata/parity.overrides) lists. The main compiler maps each rule to a code in its EF registry ([#79](https://github.com/cevr/effra/issues/79); README Gap 18).

A diagnostic has a position, a code and a message: `file:line:col: CODE message`. A file with any diagnostic does not build. The checker reports every diagnostic, sorted by position; an invalid operand or type suppresses every diagnostic that would follow from it, so one mistake gives one diagnostic. Prototype fixture paths are relative to [`prototypes/sketch9/testdata`](../../prototypes/sketch9/testdata); checker fixture names are files in `internal/sketch/check/testdata/fixtures/negative`.

## Codes

| Code | Rule | Spec section | Fixture example |
| --- | --- | --- | --- |
| S001 | Source text must lex and parse: valid identifiers, terminated strings, known number suffixes, no Go form outside the language, no mixed keyed and positional literal. | [grammar](grammar.md) | `probes/nonletter-name.sketch` |
| S101 | Every error that can escape a function must be in its declared `error[...]` row. | [rows: error rows](rows.md#error-rows) | `reject/undeclared-error.sketch` |
| S102 | Every capability a body charges must be in its capability row or discharged by an enclosing `with`. | [rows: capability rows](rows.md#capability-rows) | `reject/missing-capability.sketch` |
| S103 | Totality: a `catch` without `try` must handle every error in the caught row, and a type switch over unions must name every variant (or tuple of variants) or have `default`. | [rows: catch](rows.md#catch), [rows: totality](rows.md#totality) | `reject/catch-not-total.sketch`, `type-switch-not-total` |
| S104 | Every name must resolve, in the role its position gives it: locals, functions, types, fields, methods, capabilities, wires and error types. | [types](types.md), [rows](rows.md) | `reject/unknown-name.sketch` |
| S105 | A fallible expression must be tried, caught or returned; it cannot stand in a value position. | [rows: error rows](rows.md#error-rows) | `probes/composite-error.sketch` |
| S106 | A wire provider must implement its capability, with the same signatures and no wider error row. | [rows: wire](rows.md#wire) | `probes/provider-parameter.sketch` |
| S107 | A function with a result must not reach the end of its body. | [rows: totality](rows.md#totality) | `missing-return` |
| S108 | Types and declarations must be used consistently: no type mismatch, duplicate declaration, invalid literal, unsatisfied constraint, kind conflict, wire cycle or misuse of a construct. | [types](types.md), [rows](rows.md) | `probes/duplicate-field.sketch` |
| S109 | A construct another ticket owns is refused, naming that ticket; at build only, a program the prototype emitter cannot lower. | README Gaps 6 and 33, [runtime: G1](runtime.md) | `wire-constructor-is-37` |
| S110 | A declared local, binding or import must be read, and an expression statement must be a call, a receive, `try` or `catch`, as in Go. | [grammar: statements](grammar.md#statements) | `probes/unused-expression.sketch` |
| S111 | A wire must not have two entries for one capability, except a `replace` of an embedded entry. | [rows: wire](rows.md#wire), README Gap 28 | `duplicate-wire-entry` |
| S112 (designed) | A map's key type must not be a resource in v0. | [resources: containers](resources.md#containers), README Gap 32 | none yet: negative example `map[Key]string{openKey(): "v"}` in resources.md |
| S113 | The entry function `main` takes no parameters, returns no value (an error row is allowed) and is not called. | [rows: capability rows](rows.md#capability-rows), README Gap 4 | `main-parameter`, `main-called`, `main-value-result` |

Designed codes are not emitted yet. Other designed errors in `docs/spec/` (resources, escape, exclusivity, channels of resources) get codes when [#197](https://github.com/cevr/effra/issues/197) implements them; construction authority (types.md Zero values 6) gets S114 with packages (#196, second part).

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
- A tree higher than 1000 levels, in any composition of chains, operators, parentheses, types, statements and blocks ([grammar](grammar.md#parser) rule 6).

The fixtures are `sketchtest.MalformedTable` and the `TestRefusedForms` cases in `internal/sketch/syntax`.

### S101: undeclared error

- An error escapes through `try` and is not in the declared row (`reject/undeclared-error.sketch`, `undeclared-error`).
- A function returns a value of an error type, a type named in an error row of its package, and its row does not list it (`probes/error-as-value.sketch`).
- A partial `try … catch` leaves an undeclared error (`partial-catch-rest`).
- An inline wire's provider expression can fail, and the enclosing function does not declare it (`probes/wire-initializer-error.sketch`).
- A named wire's provider can fail while it is built: a named wire is closed (`probes/named-wire-failure.sketch`; README Gap 6).
- The step of an exported machine type spells its rows (the export guard), so an error it does not list is S101 (`export-guard-machine-step`).

### S102: missing capability

- A body calls a capability its capability row does not list (`reject/missing-capability.sketch`, `missing-capability`).
- A callee needs a capability that no `with` provides (`reject/unprovided.sketch`, `unprovided-callee`).
- A provider expression of an inline wire needs a capability of its own wire (`probes/wire-sibling.sketch`) or one the enclosing function lacks (`probes/wire-initializer.sketch`).
- A named wire needs a capability from its caller: a named wire is closed (`probes/named-wire-requirement.sketch`; README Gap 6).
- `timeout.After` without `Scheduler`: `timeout.After` is an ordinary library function whose row lists `Scheduler` (`timeout-is-a-library-function`).
- An inferred capability reaches an exported function, which spells its row (`inferred-uses-reaches-exported`).

The checker does not restrict `main`'s capability row; the build refuses what the host cannot supply (README Gap 5).

### S103: not total

- A `catch` without `try` omits an error of the caught row (`reject/catch-not-total.sketch`, `catch-not-total`).
- A type switch over a union names neither every variant nor `default` (`type-switch-not-total`).
- A multi-subject type switch leaves a tuple of variants unhandled; `_` matches any variant. Totality is decided by a usefulness check over the case matrix, never by enumerating the product (`product-not-total`). The message names one missing tuple.

### S104: unknown name

- An unknown local, function, type, field, method, label, package or wire (`reject/unknown-name.sketch`, `unknown-local`, `unknown-type`, `unknown-field`, `unknown-selector`, `unknown-wire`).
- A capability name in an expression position other than a method call's receiver, such as `f(Orders)` (`capability-as-value`; README Gap 29).
- A local of the same name as a capability that the function's written row lists, in the same frame (`local-and-binding-one-frame`; rows-and-constraints §7 amendment 3). A local in an inner frame may shadow a package capability.
- A capability row or a wire key names a type that is not a capability (`uses-non-capability`, `wire-entry-non-capability`).
- An error row names a type that is not a named struct type (`error-row-non-struct`).

### S105: unhandled failure

- A fallible call in a struct field value (`probes/composite-error.sketch`).
- A fallible `timeout.After` duration (`probes/fallible-duration.sketch`).
- A fallible expression as a `:=` value, an expression statement, an operand, a selector receiver, a call argument, an `if` condition, a switch subject or a `catch` arm value (`unhandled-define`, `unhandled-argument`, `unhandled-condition`).
- A call of an unexported helper whose inferred error row is not empty, in an exported function that does not handle it (`inferred-error-reaches-exported`).

### S106: bad provider

- A provider method's parameter types differ from the capability's (`probes/provider-parameter.sketch`).
- A provider without the capability's method, or whose method's error row is wider (`provider-missing-method`, `provider-wider-row`).
- A provider method that writes a capability row: its row is wider than the capability method's, which writes none (`provider-method-uses`; README Gap 17 stays with #37).

### S107: missing return

- A function or function literal with a result whose body can reach its end (`missing-return`). A body terminates as in Go's terminating statements: a final `return`, an `if` with an `else` whose branches terminate, a `for` without a condition or `break`, a `switch` or `select` whose cases all terminate, and a type switch that is total (names every variant or has `default`) and whose cases all terminate.

### S108: type or declaration misuse

| Rule | Fixture |
| --- | --- |
| A type, function, wire, method, field, parameter or local is declared twice in one scope; a type and a function share a name | `duplicate-function`, `probes/duplicate-field.sketch`, `duplicate-method`, `duplicate-parameter` |
| A parameter repeats the receiver's name | `probes/receiver-parameter.sketch` |
| A field and a method of one type share a name | `probes/field-method-name.sketch` |
| A receiver is not a struct type declared in the package | `receiver-not-local` |
| A union variant is not a struct type declared in the package | `union-variant-interface` |
| A struct contains itself by value | `recursive-struct` |
| A value of one type where another is expected: an assignment, an argument, a return, a field or an element | `var-mismatch`, `argument-mismatch`, `return-mismatch` |
| A type does not implement an interface, or its method's rows are wider (interface assignability) | `interface-missing-method`, `interface-wider-row` |
| A keyed literal omits a field that has no zero value; `var x T` of a type with no zero value | `keyed-literal-nondefaultable`, `var-nondefaultable` |
| A positional literal lists fewer values than the struct's fields | `positional-literal-short` |
| A type switch on a non-union; a case that is not a variant of its subject's union; a case already covered by the cases before it (unreachable) | `type-switch-non-union`, `case-not-variant` |
| An operator on operands it is not defined for; `==` on values that are not comparable or of different types | `equality-mismatch`, `plus-on-bool` |
| An untyped integer where a `duration` is expected (only a duration literal is a duration) | `untyped-integer-as-duration` |
| A constant that overflows its type; an integer literal with a leading zero; an invalid escape; a duration that overflows `int64` nanoseconds | `constant-overflow`, `probes/decimal-leading-zero.sketch`, `probes/string-escape.sketch`, `probes/duration-overflow.sketch` |
| `try` or `catch` on an expression that cannot fail; a caught type not in the row or caught twice | `try-infallible`, `catch-arm-not-in-row` |
| A type argument that cannot be inferred; a constraint the argument does not satisfy; a parameter used as a type and as a row; a method that would have type parameters of its own | `type-argument-not-inferred`, `constraint-unsatisfied`, `parameter-kind-conflict`, `method-type-parameter` |
| `*T` outside a parameter or receiver type | `borrow-in-field` |
| A function value whose rows are wider than the function type's, such as a fallible function where an infallible type is expected | `fallible-function-value` |
| A wire literal outside `with` | `wire-literal-outside-with` |
| An interface or capability method writes a capability row | `interface-method-uses` |
| `return`, `break` or `continue` crosses a `with` block, a block argument or a bare block | `return-in-with-block` |
| An error type declares a method named `Error` | `error-method-on-error-type` |
| A named wire whose build can reach itself: a `with` of the wire in its providers, through a function, a method or a capability provider that enters it, or an embedding | `wire-cycle`, `probes/cyclic-wire.sketch`, `probes/wire-method-cycle.sketch` |
| A machine's state or signal type is not a union; its initial value is not a value of the state type; its step is not a function from the state and signal types to `Decision[S, O]` | runtime.md, Machines and actors 1 |
| An argument or an operand has no value; a no-value function returns a value | `probes/void-argument.sketch`, `probes/void-fallible-return-value.sketch` |
| Others: a wrong argument or result count; a call of a non-function; a non-`bool` condition; a field set twice; an assignment to a package `var` (immutable) or to a non-addressable value; a label defined twice; `break` or `continue` outside a loop, switch or select; `with` without a block; builtins called with the wrong arguments | the checker's messages |

### S109: owned by another ticket

The checker refuses a construct that another ticket owns, and names it, instead of checking it partly.

| Rule | Owner |
| --- | --- |
| A wire entry whose provider is a function (a constructor entry), such as `Orders = OpenOrders` | [#37](https://github.com/cevr/effra/issues/37), wire constructors (`wire-constructor-is-37`) |
| A capability row on a named wire, `wire X uses A, B`: the corpus means exposure, and the spelling is an owner question | README Gap 33 |
| A `with` argument that is not a wire: a context value | G1, [runtime.md](runtime.md) |
| `import go "…"` | [#202](https://github.com/cevr/effra/issues/202), Go imports |
| A dot import | [#206](https://github.com/cevr/effra/issues/206), the standard library |
| At build only: any refusal of the prototype bridge, checker or emitter, with the message "the prototype emitter cannot lower this program yet (#50): CODE message" at its position | [#50](https://github.com/cevr/effra/issues/50) |

### S110: unused

- A local is declared and never read (`unused-local`; `probes/same-local-as-cap.sketch`).
- A type-switch binding is read in no case; write `_` (`unused-type-switch-binding`). A `catch` binding is read in no arm.
- An expression statement is not a call, a receive, `try` or `catch` (`unused-expression`, `probes/unused-expression.sketch`).
- An import or a label is not used (Go's rules).

*Prior art:* Go rejects unused locals, imports and unused expression values; Rust and Gleam warn instead. Effra keeps Go's errors because its output is Go and because an unread value usually hides a dropped result.

### S111: duplicate wire entry

- A wire has two entries for one capability, and neither is a `replace` of an embedded entry (`duplicate-wire-entry`; README Gap 28).
- Two embedded wires bind one capability; `replace` one entry.

*Prior art:* Google Wire rejects two providers of one type; Effect's `Layer.merge` keeps the last silently, which Effra refuses.

### S112: resource map key

Designed: a map's key type must not be a resource in v0 ([resources: containers](resources.md#containers), README Gap 32). It is emitted when [#197](https://github.com/cevr/effra/issues/197) implements resources.

### S113: entry and test functions

- `main` takes parameters (`main-parameter`) or type parameters.
- `main` returns a value; only an error row is allowed, as Go's `main` returns nothing (`main-value-result`; README Gap 4).
- `main` is called (`main-called`, `probes/main-call.sketch`); only the host calls it.

*Prior art:* Go's `func main()` takes nothing and returns nothing (Go still allows a call of `main`; Effra refuses it, so the host is its only caller); Gleam's `main` takes no arguments.
