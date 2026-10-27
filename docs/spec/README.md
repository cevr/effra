# Effra language specification, v0

Version 0, 2026-10-27. Ticket [#193](https://github.com/cevr/effra/issues/193), part of the compiler track [#192](https://github.com/cevr/effra/issues/192).

This directory is the normative specification of the Go-shaped Effra language: Effect's guarantees with Go's spelling, compiled to Go (owner, 2026-10-26: "this will be effect for Go with this kind of new language as well"). `.ef` source is authoritative. The parser ([#195](https://github.com/cevr/effra/issues/195)), the checker ([#196](https://github.com/cevr/effra/issues/196)) and the linearity pass ([#197](https://github.com/cevr/effra/issues/197)) implement this text.

## Scope

- **In scope.** Lexical rules and grammar, types, error and capability rows, resources, the runtime rules, the standard-library outline and the diagnostics of the core.
- **Sources.** The design sketches of 2026-10-26, numbered 8, 9 and 10, their two independent reviews, and the merged prototype compiler in [`prototypes/sketch9`](../../prototypes/sketch9/README.md) with its [receipt](../../prototypes/sketch9/RECEIPT.md). The [language evolution preview](../previews/2026-10-27-go-sketch.md) shows the language as of this version.
- **Out of scope.** The earlier `.ef` language that `internal/compiler` implements today. Its documents stay in force for that compiler until [#205](https://github.com/cevr/effra/issues/205) retires it. [`docs/design.md`](../design.md) marks each section this specification supersedes.
- **Terms.** Every domain term is defined only in the [glossary](../../GLOSSARY.md). These files state rules and link to it.
- **Direction.** [NORTH_STAR.md](../../NORTH_STAR.md) holds the pillars P1 to P7 and the tiebreaks. [PRIOR_ART.md](../../PRIOR_ART.md) holds the source comparisons.

## Conventions

- "Must", "must not" and "is an error" are normative. An error has a diagnostic code from [diagnostics.md](diagnostics.md).
- Each construct carries one status label:
  - **Implemented in prototype**: `prototypes/sketch9` accepts it and lowers it to Go, and its fixtures pin the behaviour.
  - **Partly implemented in prototype**: the prototype accepts a named subset. The section lists what is not yet implemented.
  - **Designed, not yet implemented**: decided in the sketches or by the owner, with no compiler support yet.
- A design choice between options cites prior art in one line. The standing comparison set is Go, Gleam, ReScript, Elixir/OTP, MoonBit, Rust, Swift, Effect and OCaml/Koka.
- Examples are fenced as `go` because the surface is Go-shaped. They are Effra source, not Go.

## Reading order

1. [grammar.md](grammar.md): source text, tokens, keywords and productions.
2. [types.md](types.md): structs, unions, capabilities, generics and laws.
3. [rows.md](rows.md): `error[...]` and `uses` rows, `try`/`catch`, totality, `wire` and `with`.
4. [resources.md](resources.md): automatic release, owner scopes, moves and lending.
5. [runtime.md](runtime.md): tasks, interruption (I1, I2), observability (G1 to G4), timeouts, machines and actors.
6. [stdlib.md](stdlib.md): the standard-library outline ([#206](https://github.com/cevr/effra/issues/206)).
7. [diagnostics.md](diagnostics.md): every diagnostic code, its rule, its section and a fixture.

## Status per construct

| Construct | Status | Section |
| --- | --- | --- |
| Package clause, one `import` path per line | Implemented in prototype | [grammar](grammar.md#declarations) |
| Identifiers by decoded code point (round-7 rule) | Implemented in prototype | [grammar](grammar.md#identifiers) |
| Integer, duration and string literals | Implemented in prototype | [grammar](grammar.md#literals) |
| `duration` as a type name in signatures and fields | Designed, not yet implemented (Gap 25) | [types](types.md#basic-types) |
| Struct types, keyed and positional literals | Implemented in prototype | [types](types.md#structs) |
| Union interfaces `interface{ A \| B }` | Implemented in prototype | [types](types.md#unions) |
| Capability interfaces | Implemented in prototype | [types](types.md#capabilities) |
| Interface types as parameter and result types | Partly implemented in prototype: accepted, but methods cannot be called on such a value (Gap 29) | [types](types.md#capabilities) |
| Value-receiver methods on local structs | Partly implemented in prototype: declared, and called only through a capability (Gap 27) | [types](types.md#methods) |
| Single-subject type switch | Partly implemented in prototype: totality is not enforced (Gap 3) | [rows](rows.md#totality) |
| `if`/`else` | Partly implemented in prototype: no `else if` | [grammar](grammar.md#statements) |
| `:=` definitions | Implemented in prototype | [grammar](grammar.md#statements) |
| Operators `==` (on `string`, `int64`, `bool`) and `+` | Implemented in prototype; `==` on `duration` is not (Gap 26) | [types](types.md#operators) |
| Other operators, `&&`, `\|\|` | Designed, not yet implemented (Gap 10) | [types](types.md#operators) |
| `error[...]` rows and `try` | Implemented in prototype | [rows](rows.md#error-rows) |
| `catch` without `try` (total) and `try … catch` (partial) | Implemented in prototype | [rows](rows.md#catch) |
| `catch e { … }` binding, statements in arms | Designed, not yet implemented | [rows](rows.md#catch) |
| `uses` rows | Implemented in prototype | [rows](rows.md#capability-rows) |
| Footprints and purity | Designed, not yet implemented | [rows](rows.md#footprints) |
| Named `wire` with provider literals | Partly implemented in prototype: closed wires only (Gap 6); a repeated capability entry is accepted (Gap 28) | [rows](rows.md#wire) |
| Inline `wire{}` | Implemented in prototype | [rows](rows.md#wire) |
| Wire constructors, embedding, `replace` | Designed, not yet implemented | [rows](rows.md#wire) |
| `with(W) { block }` | Implemented in prototype | [rows](rows.md#with) |
| `with` context values (G1) | Designed, not yet implemented | [runtime](runtime.md#g1-context-through-with) |
| `timeout.After(d) { block }` | Implemented in prototype as a name-keyed exception (Gap 31); designed as a library function | [runtime](runtime.md#timeouts) |
| Trailing block as a function-literal argument | Designed, not yet implemented | [grammar](grammar.md#expressions) |
| Resources and automatic release | Designed, not yet implemented | [resources](resources.md) |
| `go`, `Task`, `Wait`, `Stop` | Designed, not yet implemented | [runtime](runtime.md#tasks) |
| Task tree, stop propagation, defect propagation | Implemented in prototype (runtime `rt`) | [runtime](runtime.md#tasks) |
| I1 checks before capability calls | Implemented in prototype | [runtime](runtime.md#i1-interruption-points) |
| I1 at loops and channel operations; I2 masks | Designed, not yet implemented | [runtime](runtime.md#i2-automatic-masks) |
| G1 to G4 | Designed, not yet implemented | [runtime](runtime.md#observability-and-supervision) |
| `for`, `range`, channels, `select`, `defer`, `var` | Designed, not yet implemented; reserved in prototype | [grammar](grammar.md#keywords) |
| Borrows `*T` | Designed, not yet implemented | [types](types.md#borrows) |
| Generics | Designed, not yet implemented | [types](types.md#generics) |
| Laws `func LawX` | Designed, not yet implemented | [types](types.md#laws) |
| `machine` types and actors | Designed, not yet implemented | [runtime](runtime.md#machines-and-actors) |
| Go imports | Designed, not yet implemented | [grammar](grammar.md#declarations) |
| Packages and visibility by capitalisation | Designed, not yet implemented (Gap 8, decided) | [grammar](grammar.md#visibility) |
| Standard library | Designed, not yet implemented | [stdlib](stdlib.md) |

## Pillars

| Pillar | Where this specification serves it |
| --- | --- |
| P1 traceability and observability | G1 to G3, spans keyed to definitions, release lists in `ef inspect` |
| P2 Effect's core guarantees | typed rows, I1/I2, automatic release, exit causes |
| P3 Go simplicity | Go keywords and spelling, 27 keywords, no lifetime annotations |
| P4 laws and proofs | laws by name, composition laws in [rows.md](rows.md#composition-laws) |
| P5 error and dependency tracking | `error[...]`, `uses`, `wire`, `with` |
| P6 agent interpretability | stable diagnostic codes, inlay hints, `ef inspect`, `ef ps` |
| P7 performance | lowering by erasure, direct Go calls, no closure per step |

## Gaps

The prototype's behaviour and the sketches leave the points below unstated or in conflict. Each has a recommendation. A rule in another file that settles a gap says so.

1. **Total-catch default arm.** The sketches say a total `catch` has no default arm. The prototype's emitter adds one that passes `rt.Interrupted` on, because interruption is not a typed error. *Recommendation:* adopted as the rule in [rows.md](rows.md#catch): a `catch` covers the typed row only, and interruption and defects always pass through it. Lowering must keep that path.
2. **Interruption in a function with no error row.** The prototype panics with `rt.Interrupted` through `t.Check()`, and the nearest task boundary recovers it; `main` exits with status 130. *Recommendation:* adopted in [runtime.md](runtime.md#exit-causes): interruption is part of the exit cause, never of the error row. The lowering is free to choose the carrier.
3. **Type-switch totality.** The prototype accepts a type switch that omits a variant when the function needs no value after it (measured: a two-variant switch with one `case` checks with exit 0). The sketches say switches are checked for totality. *Recommendation:* a type switch over a union must name every variant or have a `default` arm, as Gleam, Rust and Swift require. The rule is in [rows.md](rows.md#totality); the prototype does not enforce it.
4. **`main` with an error row.** The prototype rejects it (S109). The sketches write `func main() error[ConnectFailed] uses Console`. *Recommendation:* allow it. The entry prints each cause and exits 1, or 130 when every cause is an interruption, as the current entry failure report does.
5. **Host capabilities for `main`.** The prototype gives `main` only `Console`. Decision #102 makes `Clock`, `Scheduler`, `Random` and `Console` ordinary interfaces bound by `wire`. *Recommendation:* the entry supplies a predeclared host wire with these four live providers. A test replaces any of them with `with`. The exact host set is open.
6. **Closed top-level wires.** The prototype requires a named `wire` to need no capability from its caller and to let no failure escape. The sketches give a named wire constructors whose rows join the `with` site. *Recommendation:* the closed rule is a prototype limit. The designed rule is in [rows.md](rows.md#wire).
7. **Wire entries as provider literals only.** The prototype rejects a constructor entry (S109). *Recommendation:* constructors are designed ([rows.md](rows.md#wire)); the parser and checker take both forms.
8. **Visibility (closed 2026-10-27).** Every sketch since the fourth relies on Go's capitalised export, for example an unexported field that makes `ParseQuantity` the only constructor. The owner chose explicit `pub` on 2026-10-08. Asked again with the recommendation "Go's capitalisation", the owner answered on 2026-10-27: "Yeah, let's go with your [recommendations]". *Decision:* a name is exported when it starts with an upper-case letter, and `pub` is superseded. The rule is in [grammar.md](grammar.md#visibility); the [NORTH_STAR tiebreak](../../NORTH_STAR.md#tiebreaks) records the change.
9. **Integer types.** The prototype has one integer type, `int64`, and the sketches use `int` and `int64`. *Recommendation:* [#81](https://github.com/cevr/effra/issues/81) decides the numeric types. Until then, `int64` is the only integer type with checked meaning.
10. **Short-circuit operators.** The prototype rejects `&&` and `||` (S109) because hoisted calls would run out of order. *Recommendation:* allow them with Go's short-circuit order. The lowering must not hoist a right operand above the left.
11. **`catch e` binding and statements in arms.** The prototype's arms are single expressions with no binding. The sketches bind the error (`catch e { case X: … }`) and use `return` in an arm. *Recommendation:* adopt both, as in [rows.md](rows.md#catch). An arm is a statement list whose last statement yields the arm's value when the catch is an expression.
12. **Multi-subject type switch.** The sketches write `switch s, sig := s.(type), sig.(type)` with tuple cases and `_`. *Recommendation:* adopt it as the machine-step form, with totality over the product. It is designed only.
13. **Automatic release versus checked `defer`.** The 2026-10-26 sketch 9 made a resource linear with a checked `defer` and `build.Scope.Acquire`. The owner's later direction of the same day ("make it simpler so that defers are automatic") replaced that with automatic release. *Resolved 2026-10-27:* [resources.md](resources.md) follows automatic release. `defer` stays for statements that are not releases. `build.Scope` becomes a library for dynamic registration. The [#65](https://github.com/cevr/effra/issues/65) body and the [#206](https://github.com/cevr/effra/issues/206) table now say the same.
14. **Resources behind interfaces.** Review 9 (M2) showed that converting a resource to an interface without `Close` loses release authority. *Recommendation:* adopted in [resources.md](resources.md#conversions): that conversion is an error.
15. **Moves into aggregates.** The owner's rule lists four moves. Building a struct whose field is a resource also transfers ownership, as the `Postgres` example does. *Recommendation:* adopted in [resources.md](resources.md#moves) as a fifth move.
16. **Reserved names of the prototype's output.** The prototype reserves Go's predeclared identifiers, `rt`, `time`, `fmt`, `init`, `Error`, `timeout`, `duration`, names with `__` and names ending in `_` (S109). *Recommendation:* these are prototype limits. The main compiler mangles generated names, so only the language's own reserved words stay reserved ([grammar.md](grammar.md#reserved-names)).
17. **Capabilities in provider methods.** The prototype rejects a `uses` clause on a method and a capability call inside a provider method (S109). *Recommendation:* a provider method may use the capabilities its constructor captured, as Effect's layers capture their dependencies. A method signature still cannot widen the capability's row.
18. **Diagnostic codes.** The prototype's codes (S001, S101 to S110) are not in the main compiler's EF registry ([#79](https://github.com/cevr/effra/issues/79)). *Recommendation:* the main compiler gives each rule in [diagnostics.md](diagnostics.md) an EF code, and keeps the rule text and fixture links.
19. **No fixture file for S107.** The missing-return rule is pinned only by a table test in `sketch/sketch_test.go`. *Recommendation:* add a probe file with `// want S107` when the prototype is next touched.
20. **Codec discovery by name.** The sketches find a type's codec by the names `ParseX` and `Wire`, as Go finds `TestX`. A rule keyed on a name risks the special-cased-library pattern the owner rejected. *Recommendation:* state it as a general method-set convention with laws in [#49](https://github.com/cevr/effra/issues/49), so that a userland codec family can use the same rule.
21. **Go interop.** The 2026-10-26 sketch 9 first dropped user-level Go interop and then restored `import go` at the owner's request. Decision #9 maps Go imports automatically. *Recommendation:* `import go "path"` with the `go.Foreign` capability, as in [grammar.md](grammar.md#declarations). The mapping rules stay with [#32](https://github.com/cevr/effra/issues/32).
22. **`Layer.fresh`.** There is no form for an independent build of one node inside a build. *Recommendation:* a contextual `fresh` inside `wire`, decided with [#37](https://github.com/cevr/effra/issues/37).
23. **Generic surface.** The sketches use single capital letters as implicit type parameters (`func Map(xs []A, …)`). No rule says which names are parameters. *Recommendation:* [#135](https://github.com/cevr/effra/issues/135) decides. The spelling in [types.md](types.md#generics) is provisional.
24. **Purity of function values.** Review 9 (M3) withdrew "an empty-row function type is pure". *Recommendation:* adopted in [rows.md](rows.md#footprints): a function type carries requirements only, and an unknown function value counts as impure.
25. **`duration` in signatures.** The prototype types duration literals and the `timeout.After` argument, but `func delay(d duration)` is S104, because `duration` is not one of its basic type names. *Recommendation:* `duration` is a basic type usable in every type position ([types.md](types.md#basic-types)). *Prior art:* Go's `time.Duration` is an ordinary named `int64` type, usable in every type position.
26. **Equality on `duration`.** `1s == 1s` is S109 in the prototype. *Recommendation:* `==` applies to every basic type, `duration` included ([types.md](types.md#operators)). *Prior art:* Go compares `time.Duration` values with `==`, as every integer-based type.
27. **Method calls on values.** The prototype calls a method only through a capability. A direct call such as `S{}.M()` or `v.M()` is S109. *Recommendation:* Go's method calls on values, including on a composite literal ([types.md](types.md#methods)). *Prior art:* Go method values and calls; Gleam has no methods and calls module functions instead.
28. **Repeated capability entry in one wire.** The prototype accepts two entries for one capability in one wire, and the later one wins (measured: `wire{ Store = Mem{V: "first"}; Store = Mem{V: "second"} }` prints `second`). *Recommendation:* a second entry for a capability in one wire is an error, S111 (designed), unless it is a `replace` of an embedded entry ([rows.md](rows.md#wire)). *Prior art:* Google Wire rejects two providers of one type in a set; Effect's `Layer.merge` lets the later layer win silently, which Effra does not copy.
29. **Capabilities as values.** The prototype accepts a capability interface as a parameter and result type (`func identity(c Store) Store { return c }` checks), but a method call on such a value is S109, and assigning a struct value to an interface type (`func NewStore() Store { return Mem{} }`) is S108, because the prototype's assignability is identity plus the variant-to-union rule. Both are prototype limits. *Recommendation:* interface assignability by method-row compatibility ([types.md](types.md#interface-assignability)), and distinguish the capability from the interface type ([types.md](types.md#capabilities)). A capability name used as an expression, other than as the receiver of a method call, is an error: the bound provider cannot be passed, returned or stored. The same interface used in a type position is an ordinary interface type, and its values are provider values whose method calls carry the methods' declared rows. This keeps decision #102 (capabilities are ordinary interfaces bound by `wire`) and keeps ambient authority out of values. *Prior art:* Effect separates a service's tag (the requirement) from a service value; Scala 3 capture checking tracks capabilities apart from their types.
30. **Channels of resources.** v0 makes a channel whose element type is a resource an error ([resources.md](resources.md#channels)). *Recommendation for a later version:* a send moves the value; the channel's buffer owns values in flight; releasing a channel releases its buffered values; a failed or interrupted send leaves ownership with the sender. *Prior art:* Rust `mpsc` moves values into the channel and drops buffered values with the receiver; Go channels copy and leave cleanup to the programmer.
31. **`timeout.After` as a name-keyed prototype exception.** The prototype recognises `timeout.After` by its imported name: the parser gives it a trailing block, and the checker charges `Scheduler`, adds `timeout.Expired` and makes its block a failure target. That is the special-cased-library pattern the owner rejected. *Recommendation:* `timeout.After` is an ordinary library function over kernel primitives ([runtime.md](runtime.md#timeouts)): a trailing block is a function-literal argument ([grammar.md](grammar.md#expressions)), and a row-generic signature gives the same rows. The prototype's special casing stays as an exception until the main checker has trailing blocks and row-generic functions.
32. **Resource map keys.** v0 makes a map whose key type is a resource an error (R40 in [resources.md](resources.md#containers); S112, designed), because a key has no release owner under R34 to R37. *Recommendation for a later version:* a map with a resource key type is itself a resource and owns its keys; inserting moves the key in; a delete-and-return moves the key and value out together; replacing the value for an existing key releases the old value and keeps the old key, releasing the new key argument; releasing the map releases every key and value. *Prior art:* Rust's `HashMap` owns its keys and drops them with the map, and `HashMap::insert` on an existing key keeps the old key.
