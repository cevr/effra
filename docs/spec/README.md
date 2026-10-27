# Effra language specification, v0

Version 0, 2026-10-27. Ticket [#193](https://github.com/cevr/effra/issues/193), part of the compiler track [#192](https://github.com/cevr/effra/issues/192).

This directory is the normative specification of the Go-shaped Effra language: Effect's guarantees with Go's spelling, compiled to Go (owner, 2026-10-26: "this will be effect for Go with this kind of new language as well"). `.ef` source is authoritative. The parser ([#195](https://github.com/cevr/effra/issues/195)), the checker ([#196](https://github.com/cevr/effra/issues/196)) and the linearity pass ([#197](https://github.com/cevr/effra/issues/197)) implement this text.

## Scope

- **In scope.** Lexical rules and grammar, types, error and capability rows, resources, the runtime rules, the standard-library outline and the diagnostics of the core.
- **Sources.** The design sketches of 2026-10-26, numbered 8, 9 and 10, their two independent reviews, and the merged prototype compiler in [`prototypes/sketch9`](../../prototypes/sketch9/README.md) with its [receipt](../../prototypes/sketch9/RECEIPT.md). The [language evolution preview](../previews/2026-10-27-go-sketch.md) shows the language as of this version.
- **Out of scope.** The earlier `.ef` language that `internal/compiler` implements today. Its documents stay in force for that compiler until [#205](https://github.com/cevr/effra/issues/205) retires it. [`docs/design.md`](../design.md) marks each section this specification supersedes.
- **Edition.** This specification is the `2026` [edition](../specs/editions.md): a module opts in with an `effra.json` of `{"edition": "2026"}`, and a file without one is the earlier language.
- **Terms.** Every domain term is defined only in the [glossary](../../GLOSSARY.md). These files state rules and link to it.
- **Direction.** [NORTH_STAR.md](../../NORTH_STAR.md) holds the pillars P1 to P7 and the tiebreaks. [PRIOR_ART.md](../../PRIOR_ART.md) holds the source comparisons.

## Conventions

- "Must", "must not" and "is an error" are normative. An error has a diagnostic code from [diagnostics.md](diagnostics.md).
- Each construct carries one status label:
  - **Checked; runs**: the checker `internal/sketch/check` ([#196](https://github.com/cevr/effra/issues/196)) checks it, and `ef build` lowers it through the prototype emitter.
  - **Checked; build refused**: the checker checks it on every surface; the prototype emitter cannot lower it, so `ef build` refuses it with S109 naming [#50](https://github.com/cevr/effra/issues/50), which lowers from the checker's output.
  - **Implemented in prototype**: `prototypes/sketch9` accepts it and lowers it to Go, and its fixtures pin the behaviour; the 2026 checker does not check it yet.
  - **Designed, not yet implemented**: decided in the sketches or by the owner, with no compiler support yet.
  - Since [#195](https://github.com/cevr/effra/issues/195), the parser `internal/sketch/syntax` parses and formats every construct of every label. A parsed construct that another ticket owns is S109 naming that ticket ([diagnostics](diagnostics.md#s109-owned-by-another-ticket)).
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
| Parsing every production of grammar.md and Go's productions where it is silent; S001 | Implemented ([#195](https://github.com/cevr/effra/issues/195), `internal/sketch/syntax`) | [grammar](grammar.md#parser) |
| Formatting, `effra-2026/formatter-1` | Implemented ([#195](https://github.com/cevr/effra/issues/195), `internal/sketch/format`) | [formatting](../specs/formatting.md#2026-edition) |
| Name resolution, Go's types where the spec is silent, S101 to S113 | Implemented ([#196](https://github.com/cevr/effra/issues/196), `internal/sketch/check`) for one package; packages and imports of module packages follow in #196's second part | [types](types.md), [rows](rows.md), [diagnostics](diagnostics.md) |
| Package clause, one `import` path per line | Checked; runs (the standard-library package `timeout`) | [grammar](grammar.md#declarations) |
| Identifiers by decoded code point (round-7 rule) | Checked; runs | [grammar](grammar.md#identifiers) |
| Integer, duration and string literals | Checked; runs. Rune literals: checked; build refused | [grammar](grammar.md#literals) |
| Every Go numeric type, with Go's typing | Checked; build refused for types other than `int64` ([#81](https://github.com/cevr/effra/issues/81) keeps the overflow policy and floating-point literals) | [types](types.md#basic-types) |
| `duration` as a type name in signatures and fields | Checked; build refused (Gap 25) | [types](types.md#basic-types) |
| Struct types, keyed and positional literals | Checked; runs | [types](types.md#structs) |
| Union interfaces `interface{ A \| B }` | Checked; runs | [types](types.md#unions) |
| Capability interfaces | Checked; runs | [types](types.md#capabilities) |
| Interface types as parameter and result types | Checked, with method calls on such a value (Gap 29); build refused for those calls | [types](types.md#capabilities) |
| Value-receiver methods on local structs | Checked, with direct method calls (Gap 27); build refused for those calls | [types](types.md#methods) |
| Single-subject type switch | Checked, total or with `default` (S103; Gap 3); runs | [rows](rows.md#totality) |
| Multi-subject type switch | Checked, total by a usefulness check (Gap 12); build refused | [rows](rows.md#totality) |
| `if`/`else`, `else if` | Checked; runs | [grammar](grammar.md#statements) |
| `:=` definitions | Checked; runs | [grammar](grammar.md#statements) |
| Operators `==` (on `string`, `int64`, `bool`) and `+` | Checked; runs | [types](types.md#operators) |
| Other operators, `&&`, `\|\|`, `==` on `duration` | Checked with Go's operand rules (Gaps 10, 26); build refused | [types](types.md#operators) |
| `error[...]` rows and `try` | Checked; runs | [rows](rows.md#error-rows) |
| `catch` without `try` (total) and `try … catch` (partial) | Checked; runs | [rows](rows.md#catch) |
| `catch e { … }` binding, statements in arms | Checked (Gap 11); build refused | [rows](rows.md#catch) |
| `uses` rows | Checked, inferred for unexported functions and literals; runs | [rows](rows.md#capability-rows) |
| Footprints and purity | Designed, not yet implemented ([#203](https://github.com/cevr/effra/issues/203)) | [rows](rows.md#footprints) |
| Named `wire` with provider literals | Checked: closed wires (Gap 6), S111 for a repeated entry (Gap 28); runs | [rows](rows.md#wire) |
| Inline `wire{}` | Checked; runs | [rows](rows.md#wire) |
| Wire embedding, `replace`, one provider for several capabilities | Checked (Gap 34); build refused | [rows](rows.md#wire) |
| Wire constructors | Designed, not yet implemented; S109 naming [#37](https://github.com/cevr/effra/issues/37) | [rows](rows.md#wire) |
| `with(W) { block }` | Checked; runs | [rows](rows.md#with) |
| `with` context values (G1) | Designed, not yet implemented; S109 naming G1 | [runtime](runtime.md#g1-context-through-with) |
| `timeout.After(d) { block }` | Checked as an ordinary library function of the package `timeout` (Gap 31); runs through the prototype emitter's own `timeout.After` | [runtime](runtime.md#timeouts) |
| Trailing block as a function-literal argument | Checked; build refused except on `with` and `timeout.After` | [grammar](grammar.md#expressions) |
| Function literals and function types | Checked, with rows from the expected type or inferred; build refused | [types](types.md#generics) |
| Resources and automatic release | Designed, not yet implemented ([#197](https://github.com/cevr/effra/issues/197)) | [resources](resources.md) |
| `go`, `Task`, `Wait`, `Stop` | Checked; build refused | [runtime](runtime.md#tasks) |
| Task tree, stop propagation, defect propagation | Implemented in prototype (runtime `rt`) | [runtime](runtime.md#tasks) |
| I1 checks before capability calls | Implemented in prototype | [runtime](runtime.md#i1-interruption-points) |
| I1 at loops and channel operations; I2 masks | Designed, not yet implemented | [runtime](runtime.md#i2-automatic-masks) |
| G1 to G4 | Designed, not yet implemented | [runtime](runtime.md#observability-and-supervision) |
| `for`, `range`, channels, `select`, `defer`, `var`, labels, `break`, `continue` | Checked with Go's rules; build refused | [grammar](grammar.md#keywords) |
| Borrows `*T` | Checked: only as a parameter or receiver type; escape and exclusivity are #197's; build refused | [types](types.md#borrows) |
| Generics | Checked: implicit parameters with kinds by position, bracket constraints, row parameters, inference by unification; build refused | [types](types.md#generics) |
| Laws `func LawX` | Checked as exported functions; discovery and running are not implemented | [types](types.md#laws) |
| `machine` types | Checked: state and signal unions, initial value, step signature, the export guard; build refused | [runtime](runtime.md#machines-and-actors) |
| Actors | Designed, not yet implemented | [runtime](runtime.md#machines-and-actors) |
| Go imports | Designed, not yet implemented; S109 naming [#202](https://github.com/cevr/effra/issues/202) | [grammar](grammar.md#declarations) |
| Packages and visibility by capitalisation | Designed, not yet implemented (Gap 8, decided); #196's second part | [grammar](grammar.md#visibility) |
| Standard library | The prelude and the package `timeout` are Effra source checked by the checker (`internal/sketch/std`); the rest is designed ([#206](https://github.com/cevr/effra/issues/206)) | [stdlib](stdlib.md) |

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
3. **Type-switch totality.** The prototype accepts a type switch that omits a variant when the function needs no value after it (measured: a two-variant switch with one `case` checks with exit 0). The sketches say switches are checked for totality. *Recommendation:* a type switch over a union must name every variant or have a `default` arm, as Gleam, Rust and Swift require. The rule is in [rows.md](rows.md#totality); the checker enforces it (S103, [#196](https://github.com/cevr/effra/issues/196)).
4. **`main` with an error row.** The prototype rejects it (S109). The sketches write `func main() error[ConnectFailed] uses Console`. *Recommendation:* allow it. The entry prints each cause and exits 1, or 130 when every cause is an interruption, as the current entry failure report does. *Status:* the checker allows it (#196); a value result is S113.
5. **Host capabilities for `main`.** The prototype gives `main` only `Console`. Decision #102 makes `Clock`, `Scheduler`, `Random` and `Console` ordinary interfaces bound by `wire`. *Recommendation:* the entry supplies a predeclared host wire with these four live providers. A test replaces any of them with `with`. The exact host set is open. *Status:* the checker does not restrict `main`'s capability row (#196); the build refuses what the host cannot supply.
6. **Closed top-level wires.** The prototype requires a named `wire` to need no capability from its caller and to let no failure escape. The sketches give a named wire constructors whose rows join the `with` site. *Recommendation:* the closed rule is a prototype limit. The designed rule is in [rows.md](rows.md#wire). *Status:* the checker keeps the closed rule (#196); open named wires are [#37](https://github.com/cevr/effra/issues/37)'s.
7. **Wire entries as provider literals only.** The prototype rejects a constructor entry (S109). *Recommendation:* constructors are designed ([rows.md](rows.md#wire)). The parser takes both forms; the checker refuses a constructor entry with S109 naming [#37](https://github.com/cevr/effra/issues/37).
8. **Visibility (closed 2026-10-27).** Every sketch since the fourth relies on Go's capitalised export, for example an unexported field that makes `ParseQuantity` the only constructor. The owner chose explicit `pub` on 2026-10-08. Asked again with the recommendation "Go's capitalisation", the owner answered on 2026-10-27: "Yeah, let's go with your [recommendations]". *Decision:* a name is exported when it starts with an upper-case letter, and `pub` is superseded. The rule is in [grammar.md](grammar.md#visibility); the [NORTH_STAR tiebreak](../../NORTH_STAR.md#tiebreaks) records the change.
9. **Integer types.** The prototype has one integer type, `int64`, and the sketches use `int` and `int64`. *Recommendation:* [#81](https://github.com/cevr/effra/issues/81) decides the numeric types. *Status:* the checker types every Go numeric type with Go's rules (#196, Q5); #81 keeps the overflow policy, checked runtime conversions and floating-point literals.
10. **Short-circuit operators.** The prototype rejects `&&` and `||` (S109) because hoisted calls would run out of order. *Recommendation:* allow them with Go's short-circuit order. The lowering must not hoist a right operand above the left. *Status:* the checker accepts them with Go's typing (#196); the build refuses them until [#50](https://github.com/cevr/effra/issues/50).
11. **`catch e` binding and statements in arms.** The prototype's arms are single expressions with no binding. The sketches bind the error (`catch e { case X: … }`) and use `return` in an arm. *Recommendation:* adopt both, as in [rows.md](rows.md#catch). An arm is a statement list whose last statement yields the arm's value when the catch is an expression. *Status:* checked (#196).
12. **Multi-subject type switch.** The sketches write `switch s, sig := s.(type), sig.(type)` with tuple cases and `_`. *Recommendation:* adopt it as the machine-step form, with totality over the product. *Status:* the checker checks it, with totality decided by a usefulness check over the case matrix (#196); the build refuses it until #50.
13. **Automatic release versus checked `defer`.** The 2026-10-26 sketch 9 made a resource linear with a checked `defer` and `build.Scope.Acquire`. The owner's later direction of the same day ("make it simpler so that defers are automatic") replaced that with automatic release. *Resolved 2026-10-27:* [resources.md](resources.md) follows automatic release. `defer` stays for statements that are not releases. `build.Scope` becomes a library for dynamic registration. The [#65](https://github.com/cevr/effra/issues/65) body and the [#206](https://github.com/cevr/effra/issues/206) table now say the same.
14. **Resources behind interfaces.** Review 9 (M2) showed that converting a resource to an interface without `Close` loses release authority. *Recommendation:* adopted in [resources.md](resources.md#conversions): that conversion is an error.
15. **Moves into aggregates.** The owner's rule lists four moves. Building a struct whose field is a resource also transfers ownership, as the `Postgres` example does. *Recommendation:* adopted in [resources.md](resources.md#moves) as a fifth move.
16. **Reserved names of the prototype's output.** The prototype reserves Go's predeclared identifiers, `rt`, `time`, `fmt`, `init`, `Error`, `timeout`, `duration`, names with `__` and names ending in `_` (S109). *Recommendation:* these are prototype limits. The main compiler mangles generated names, so only the language's own reserved words stay reserved ([grammar.md](grammar.md#reserved-names)). *Status:* the checker reserves none of them, and universe names may be shadowed (#196).
17. **Capabilities in provider methods.** The prototype rejects a `uses` clause on a method and a capability call inside a provider method (S109). *Recommendation:* a provider method may use the capabilities its constructor captured, as Effect's layers capture their dependencies. A method signature still cannot widen the capability's row. *Status:* a method may write a capability row and is charged at its call; such a method cannot implement a capability method (S106) or an interface method (S108), because its row is wider (#196). The captured form stays with #37.
18. **Diagnostic codes.** The prototype's codes (S001, S101 to S110) are not in the main compiler's EF registry ([#79](https://github.com/cevr/effra/issues/79)). *Recommendation:* the main compiler gives each rule in [diagnostics.md](diagnostics.md) an EF code, and keeps the rule text and fixture links.
19. **No fixture file for S107.** The missing-return rule is pinned only by a table test in `sketch/sketch_test.go`. *Closed:* the checker's fixture `missing-return` pins S107 (#196).
20. **Codec discovery by name.** The sketches find a type's codec by the names `ParseX` and `Wire`, as Go finds `TestX`. A rule keyed on a name risks the special-cased-library pattern the owner rejected. *Recommendation:* state it as a general method-set convention with laws in [#49](https://github.com/cevr/effra/issues/49), so that a userland codec family can use the same rule.
21. **Go interop.** The 2026-10-26 sketch 9 first dropped user-level Go interop and then restored `import go` at the owner's request. Decision #9 maps Go imports automatically. *Recommendation:* `import go "path"` with the `go.Foreign` capability, as in [grammar.md](grammar.md#declarations). The mapping rules stay with [#32](https://github.com/cevr/effra/issues/32).
22. **`Layer.fresh`.** There is no form for an independent build of one node inside a build. *Recommendation:* a contextual `fresh` inside `wire`, decided with [#37](https://github.com/cevr/effra/issues/37).
23. **Generic surface.** The sketches use single capital letters as implicit type parameters (`func Map(xs []A, …)`). No rule says which names are parameters. *Recommendation:* [#135](https://github.com/cevr/effra/issues/135) decides. The spelling in [types.md](types.md#generics) is provisional. *Status:* the checker implements rows-and-constraints §7 amendment 2 (#196): a name of one upper-case letter, optionally followed by digits, that names no declaration is an implicit parameter, and its kind follows its position.
24. **Purity of function values.** Review 9 (M3) withdrew "an empty-row function type is pure". *Recommendation:* adopted in [rows.md](rows.md#footprints): a function type carries requirements only, and an unknown function value counts as impure. *Status:* the checker converts a function value to a function type whose rows are supersets of its own, never the reverse (#196).
25. **`duration` in signatures.** The prototype types duration literals and the `timeout.After` argument, but `func delay(d duration)` is S104, because `duration` is not one of its basic type names. *Recommendation:* `duration` is a basic type usable in every type position ([types.md](types.md#basic-types)). *Prior art:* Go's `time.Duration` is an ordinary named `int64` type, usable in every type position. *Status:* the checker types `duration` in every type position (#196); the build refuses it until #50.
26. **Equality on `duration`.** `1s == 1s` is S109 in the prototype. *Recommendation:* `==` applies to every basic type, `duration` included ([types.md](types.md#operators)). *Prior art:* Go compares `time.Duration` values with `==`, as every integer-based type. *Status:* checked (#196).
27. **Method calls on values.** The prototype calls a method only through a capability. A direct call such as `S{}.M()` or `v.M()` is S109. *Recommendation:* Go's method calls on values, including on a composite literal ([types.md](types.md#methods)). *Prior art:* Go method values and calls; Gleam has no methods and calls module functions instead. *Status:* checked (#196); the build refuses such a call until #50.
28. **Repeated capability entry in one wire.** The prototype accepts two entries for one capability in one wire, and the later one wins (measured: `wire{ Store = Mem{V: "first"}; Store = Mem{V: "second"} }` prints `second`). *Recommendation:* a second entry for a capability in one wire is an error, S111, unless it is a `replace` of an embedded entry ([rows.md](rows.md#wire)). *Status:* implemented by the checker (#196). *Prior art:* Google Wire rejects two providers of one type in a set; Effect's `Layer.merge` lets the later layer win silently, which Effra does not copy.
29. **Capabilities as values.** The prototype accepts a capability interface as a parameter and result type (`func identity(c Store) Store { return c }` checks), but a method call on such a value is S109, and assigning a struct value to an interface type (`func NewStore() Store { return Mem{} }`) is S108, because the prototype's assignability is identity plus the variant-to-union rule. Both are prototype limits. *Recommendation:* interface assignability by method-row compatibility ([types.md](types.md#interface-assignability)), and distinguish the capability from the interface type ([types.md](types.md#capabilities)). A capability name used as an expression, other than as the receiver of a method call, is an error: the bound provider cannot be passed, returned or stored. The same interface used in a type position is an ordinary interface type, and its values are provider values whose method calls carry the methods' declared rows. This keeps decision #102 (capabilities are ordinary interfaces bound by `wire`) and keeps ambient authority out of values. *Prior art:* Effect separates a service's tag (the requirement) from a service value; Scala 3 capture checking tracks capabilities apart from their types. *Status:* checked (#196).
30. **Channels of resources.** v0 makes a channel whose element type is a resource an error ([resources.md](resources.md#channels)). *Recommendation for a later version:* a send moves the value; the channel's buffer owns values in flight; releasing a channel releases its buffered values; a failed or interrupted send leaves ownership with the sender. *Prior art:* Rust `mpsc` moves values into the channel and drops buffered values with the receiver; Go channels copy and leave cleanup to the programmer.
31. **`timeout.After` as a name-keyed prototype exception.** The prototype recognises `timeout.After` by its imported name: the parser gives it a trailing block, and the checker charges `Scheduler`, adds `timeout.Expired` and makes its block a failure target. That is the special-cased-library pattern the owner rejected. *Recommendation:* `timeout.After` is an ordinary library function over kernel primitives ([runtime.md](runtime.md#timeouts)): a trailing block is a function-literal argument ([grammar.md](grammar.md#expressions)), and a row-generic signature gives the same rows. *Status:* the checker (#196) checks `timeout.After` as an ordinary bodiless declaration of the package `timeout` in `internal/sketch/std`; the prototype's special casing stays inside the build path only, until #50.
32. **Resource map keys.** v0 makes a map whose key type is a resource an error (R40 in [resources.md](resources.md#containers); S112, designed), because a key has no release owner under R34 to R37. *Recommendation for a later version:* a map with a resource key type is itself a resource and owns its keys; inserting moves the key in; a delete-and-return moves the key and value out together; replacing the value for an existing key releases the old value and keeps the old key, releasing the new key argument; releasing the map releases every key and value. *Prior art:* Rust's `HashMap` owns its keys and drops them with the map, and `HashMap::insert` on an existing key keeps the old key.
33. **`wire Name uses A, B { … }`.** The corpus (`sketch-eval-8/five/layers`, both `wire-effects` files) gives a named wire a `uses` clause. The parser of [#195](https://github.com/cevr/effra/issues/195) admits it ([grammar.md](grammar.md#extensions-beyond-the-written-productions)), and its meaning was open ([rows-and-constraints.md](../research/rows-and-constraints.md) §7 item 9): the capabilities the wire's providers need from the `with` site, or the capabilities the wire provides. Both corpus uses mean exposure: the listed capabilities are what the wire provides, and its other entries stay private to its build (`wire App uses Account, Invoice { … } // exposes two; Store stays hidden`). The keyword `uses` means "requires" on every function and function type, so one word would carry two opposite meanings (pillar P6). *Status:* the meaning is exposure; the spelling is pending the owner. Until then the checker refuses the clause with S109 naming this gap (#196). *Prior art:* Effect's `Layer.provide` hides the inner layer's services; OCaml signatures and Elixir's private functions list what a module exposes.
34. **One provider for several capabilities: `Clock, Scheduler = testing.VirtualClock{}`.** The corpus binds one value to several capability rows in one wire entry. The parser of #195 admits several keys before `=`, as Go's `var a, b = …` admits several names ([grammar.md](grammar.md#extensions-beyond-the-written-productions)), and the bridge refuses it with S109. Its meaning (one value shared by several rows, or one value per row) is the checker's to define. *Status:* the checker reads it as one shared value that must implement each listed capability (#196). *Prior art:* Effect's `Layer.mergeAll` builds one layer that provides several services.
