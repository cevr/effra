# Types

Definitions of every term used here are in the [glossary](../../GLOSSARY.md). Each section carries its status.

Types are nominal. Two named types are identical only when they are the same declaration. Assignability is identity, plus the variant-to-union rule in [Unions](#unions). *Prior art:* Go's named types; Gleam and ReScript custom types are nominal too.

## Basic types

Status: **implemented in prototype** for `string`, `int64`, `bool` and `duration`.

1. `string` holds bytes. A literal's value is the bytes its spelling decodes to ([grammar.md](grammar.md#literals)).
2. `int64` is the only integer type with checked meaning in v0 (README Gap 9). Non-constant arithmetic wraps; constant overflow is an error.
3. `bool` is the type of comparisons and `if` conditions.
4. `duration` is the type of duration literals. Its name is predeclared, and the prototype reserves it as a source name.
5. Designed, with [#81](https://github.com/cevr/effra/issues/81) and [#211](https://github.com/cevr/effra/issues/211): Go's other numeric types, `byte`, `rune`, floating point, slices `[]T`, maps `map[K]V`, channels `chan T`, function types and the prelude `Option[T]`.

## Zero values and definite initialisation

Status: **implemented in prototype** for struct literals; **designed** for the rest.

1. A type is *defaultable* when it has a valid zero value: a basic type, or a struct whose fields are all defaultable. A union, a capability, a function type, a `Task`, a resource, a borrow, and a struct that contains any of them are not defaultable.
2. A keyed struct literal must set every field whose type is not defaultable (S108). A positional literal sets every field.
3. Designed: a variable of a non-defaultable type must be initialised before use. `var p Payment` with no value is an error. *Prior art:* Swift and Kotlin definite initialisation; Go instead gives every type a zero value, which is how a nil interface reaches a type switch.
4. Designed: operations that invent zero values need a defaultable element type: `make([]T, n)`, the comma-ok forms and a receive from a closed channel. For other element types the library offers `make([]T, 0, n)` with `append`, `m.Lookup(k)` returning `Option[V]`, and `ch.Receive()` returning `Option[T]`.
5. There is no `nil`. *Prior art:* Gleam, ReScript and Rust have no null; Go's typed-nil interface and nil map are its most cited traps.

## Structs

Status: **implemented in prototype**.

```go
type Order struct {
	ID    string
	Total int64
}

order := Order{ID: "42", Total: 1999}
same := Order{"42", 1999}
```

1. Field names in one struct are distinct (S108). A field and a method of the same type cannot share a name (S108).
2. A composite literal is all keyed or all positional (S001). A positional literal lists every field in order (S108). A keyed literal sets each field at most once (S108) and names only declared fields (S104).
3. Each field value must be assignable to the field's type (S108). A field value whose failure is not handled is S105.
4. A struct must not contain itself by value, directly or through other structs (S108). Recursion behind a union is allowed, because a union value is a reference in the lowered Go. *Prior art:* Swift's `indirect enum` boxes recursion; Go rejects an invalid recursive type.
5. A struct with a resource field is itself a resource ([resources.md](resources.md#what-is-a-resource)).
6. Designed: struct embedding is allowed only in `wire` and in interfaces. Value semantics hold: a copy is a snapshot, and move-on-last-use keeps it cheap.

## Unions

Status: **implemented in prototype**.

```go
type Payment interface{ Pending | Authorized | Declined }

type Pending struct{}
type Authorized struct{ AuthID string }
type Declined struct{ Reason string }
```

1. A union lists one or more variants. Each variant must be a struct type declared in the package (S108); the prototype requires the same file. A variant may not be listed twice (S108).
2. A value of a variant type is assignable to every union that lists it. A struct may be a variant of several unions (fixture `multiple-unions`).
3. A union has no zero value.
4. A type switch narrows a union value to its variants ([rows.md](rows.md#totality)).
5. The emitter generates a marker method per union on each variant, so a source method with that marker's name is an error in the prototype (fixture `marker-method`, S109).
6. **Lowering.** A union becomes a sealed Go interface with an unexported marker method, and each variant keeps its own struct fields. *Prior art:* Borgo lowers an enum to a tagged struct and matches on the tag; ReScript picks a representation per variant; Effra keeps the interface form so a Go type switch narrows and each variant keeps its fields. The size trade-off between the two forms is not measured.

## Capabilities

Status: **implemented in prototype**.

```go
type Orders interface {
	Find(id string) (Order, error[OrderNotFound])
}
```

1. An interface with method signatures declares a capability. Method names in one capability are distinct (S108).
2. A capability is not a value. `Orders.Find(id)` calls the provider bound to `Orders`; `Orders.Find` without a call is an error (S108).
3. A capability method's signature carries its error row. Prototype: a `uses` clause on a capability method is an error (S109). Designed: unchanged, because a provider's requirements are captured at construction (README Gap 17).
4. The prelude capabilities `Console`, `Clock`, `Scheduler` and `Random` are ordinary interfaces bound by `wire` (decision #102). Prototype: `Console` (with a variadic `Println`) and `Scheduler` exist; `Scheduler` takes only the host provider `LiveScheduler{}`, and `Console` cannot be wired.
5. Designed: an interface is also a type and a generic constraint, with Go's method-set rules.

## Methods

Status: **implemented in prototype** for value receivers.

1. A receiver names a struct type declared in the package (S108). A method name is unique per receiver type (S108). A parameter may not repeat the receiver's name (S108).
2. Prototype: a method may not carry a `uses` clause and may not call a capability (S109); a provider method calls functions with an empty `uses` row.
3. Designed: a pointer receiver `*T` means the method mutates its receiver ([Borrows](#borrows)).
4. Prototype: an error type may not declare `Error`, because the emitter generates it (fixture `error-method`, S109). Designed: the same rule. An error type's text comes from its fields.

## Error types

Status: **implemented in prototype**.

1. A type named in any error row is an error type. It must be a struct declared in the package (S104), or `timeout.Expired` when `timeout` is imported.
2. An error type is not an ordinary value: returning a value of an error type is a failure ([rows.md](rows.md#error-rows); fixture `error-as-value`, S101).
3. Error rows are sets of independent struct types, not one declared sum per function. *Prior art:* Gleam returns one declared `Result` error type per function; OCaml polymorphic variants and Koka effect rows are open sets; Effra's rows are closed sets of nominal types with no open tail in v0.

## Operators

Status: **partly implemented in prototype**.

1. `==` compares two values of one basic type (S108 for mismatched types; S109 for a non-basic type in the prototype).
2. `+` adds two `int64` values or concatenates two `string` values (S108 otherwise).
3. Prototype: every other operator, including `!=`, `<`, `-`, `&&` and `||`, is S109. Designed: Go's operators on Go's operand types; `&&` and `||` short-circuit (README Gap 10); equality on comparable structs, with `comparable` and `cmp.Ordered` as constraints ([#211](https://github.com/cevr/effra/issues/211)).

## Function values

Status: **designed, not yet implemented**.

1. A function type carries its parameters, result, error row and `uses` row. It carries requirements only, never purity ([rows.md](rows.md#footprints)).
2. A function-typed parameter is non-escaping unless its type says otherwise: the callee may call it and may hand it to a task that the call consumes, but it may not store, return or send it. A trailing block is such an argument. *Prior art:* Swift non-escaping closures by default; Effekt second-class blocks.
3. A `func` literal that escapes captures by copy and may not assign captured locals. A non-escaping inline block may assign them.
4. **Concurrent parameters.** A function parameter that the callee hands to `go`, directly or through another call, is concurrent. The compiler infers this fact and records it in the exported interface summary. A block or literal passed to a concurrent parameter may not assign captured locals, so `parallel { n++ }` is an error. *Prior art:* Swift `@Sendable` parameters.
5. **Replayable parameters.** A transaction body ([stdlib.md](stdlib.md#sync-and-stm)) may not assign captured locals, use channels or consume a resource, and its footprint holds only `sync.Tx`.

## Borrows

Status: **designed, not yet implemented**.

1. `*T` appears only on parameters and receivers. A borrow is never nil, stored, returned, sent or captured by `go`.
2. Two mutable borrows into one value must not overlap. With constant indexes the checker decides; with dynamic indexes it needs a dominating guard or it rejects the call. *Prior art:* Swift's law of exclusivity; Rust `get_many_mut`.
3. There are no pointer fields. Linked structures use recursive values behind unions, index arenas or `sync.Ref`. This is a loss against Go that Swift, Hylo and Rust also make.
4. Resources are lent by passing them as arguments ([resources.md](resources.md#lending)). A borrow and a lend obey the same no-escape rule.

## Generics

Status: **designed, not yet implemented**. The spelling is provisional until [#135](https://github.com/cevr/effra/issues/135) decides it (README Gap 23).

```go
func Map(xs []A, limit int64, f func(A) (B, error[E]) uses R) ([]B, error[E]) uses R
```

1. Type parameters are implicit. `[T any]` is not written. A parameter may range over types (`A`, `B`), error rows (`E`) or `uses` rows (`R`).
2. Constraints are named interfaces such as `comparable` and `cmp.Ordered`, or interfaces with methods. Method constraints resolve through the type's owner package, with one answer per type.
3. Rows in a generic signature may be elided only where they pass through unchanged. A declared requirement is never elided.
4. Exported signatures spell their rows and type parameters, so clients check against interface summaries, Go's export-data model.
5. **Lowering.** Generics emit as Go generics, which Go compiles by GC shape. Specialised copies need a measured win under [#190](https://github.com/cevr/effra/issues/190). *Prior art:* Go GC-shape stenciling; MoonBit and Rust monomorphise; Effra's earlier measurements refused frontend monomorphisation ([design.md](../design.md#compile-speed-is-a-language-constraint)).

## Prelude types

Status: **designed, not yet implemented**, except the timeout result `timeout.Expired`.

- `Option[T]` with `Some` and `None` ([#99](https://github.com/cevr/effra/issues/99)).
- `Task[T, E]`, a resource ([runtime.md](runtime.md#tasks)).
- `Decision[S, O]` with `Enter`, `Stay`, `Ignore`, `Reject` and `Done` ([runtime.md](runtime.md#machines-and-actors)).
- The capabilities `Clock`, `Scheduler`, `Random` and `Console`.

## Laws

Status: **designed, not yet implemented** ([#125](https://github.com/cevr/effra/issues/125), [#170](https://github.com/cevr/effra/issues/170)).

```go
func LawRoundTrip(q Quantity) bool {
	return ParseQuantity(q.Wire()) == q
}

func LawFindAfterPut(id string, v Order) bool uses Store {
	Store.Put(id, v)
	return Store.Find(id) == v
}
```

1. A function named `LawX` that returns `bool` is a law. `ef test` checks it like a fuzz target, with generated arguments.
2. A law with a `uses` clause is a provider law: `ef test` checks it against every provider wired for that capability.
3. A refuted law refuses the build. A law that passes a finite suite is reported as tested, never as proved (NORTH_STAR pillar 4).
4. Laws are found by name, as Go finds `TestX`. The same rule serves every client: codecs, providers, schedules and machines. *Prior art:* Go `TestX`/`FuzzX` discovery; Haskell QuickCheck properties; Elixir `StreamData` properties.

## Codecs

Status: **designed, not yet implemented** ([#49](https://github.com/cevr/effra/issues/49)).

1. A type's wire form is found by name: `ParseX` decodes into `X`, and `x.Wire()` encodes. Go's `netip.ParseAddr` and `String` pair is the model.
2. A derived codec's rows are the union of its fields' rows. `json.Decode[Order]` has the error and `uses` rows of every `ParseX` it reaches.
3. README Gap 20 records the open question about discovery by name.
