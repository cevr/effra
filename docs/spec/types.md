# Types

Definitions of every term used here are in the [glossary](../../GLOSSARY.md). Each section carries its status.

Types are nominal. Two named types are identical only when they are the same declaration. A value of type `T` is assignable to type `U` when `T` and `U` are identical, when `U` is a union that lists `T` ([Unions](#unions)), or when `U` is an interface type that `T` implements ([Interface assignability](#interface-assignability)). *Prior art:* Go's named types and implicit interface satisfaction; Gleam and ReScript custom types are nominal too.

## Basic types

Status: **implemented in prototype** for `string`, `int64` and `bool`; **partly implemented** for `duration`, which the prototype types only as a literal and as the `timeout.After` argument (README Gap 25).

1. `string` holds bytes. A literal's value is the bytes its spelling decodes to ([grammar.md](grammar.md#literals)).
2. `int64` is the only integer type with checked meaning in v0 (README Gap 9). Non-constant arithmetic wraps; constant overflow is an error.
3. `bool` is the type of comparisons and `if` conditions.
4. `duration` is the type of duration literals. It is a basic type, usable in every type position: parameters, results, fields and type arguments. Its name is predeclared. Prototype: `duration` as a type name is S104, and the name is reserved as a source name (S109).
5. Designed, with [#81](https://github.com/cevr/effra/issues/81) and [#211](https://github.com/cevr/effra/issues/211): Go's other numeric types, `byte`, `rune`, floating point, slices `[]T`, maps `map[K]V`, channels `chan T`, function types and the prelude `Option[T]`.

## Zero values and definite initialisation

Status: **implemented in prototype** for struct literals; **designed** for the rest.

1. A type is *defaultable* when it has a valid zero value: a basic type, or a struct whose fields are all defaultable. A union, a capability, a function type, a `Task`, a resource, a borrow, and a struct that contains any of them are not defaultable. Outside its package, a struct type with an unexported field is not defaultable either (rule 6).
2. A keyed struct literal must set every field whose type is not defaultable (S108). A positional literal sets every field.
3. Designed: a variable of a non-defaultable type must be initialised before use. `var p Payment` with no value is an error. *Prior art:* Swift and Kotlin definite initialisation; Go instead gives every type a zero value, which is how a nil interface reaches a type switch.
4. Designed: operations that invent zero values need a defaultable element type: `make([]T, n)`, the comma-ok forms and a receive from a closed channel. For other element types the library offers `make([]T, 0, n)` with `append`, `m.Lookup(k)` returning `Option[V]`, and `ch.Receive()` returning `Option[T]`.
5. There is no `nil`. *Prior art:* Gleam, ReScript and Rust have no null; Go's typed-nil interface and nil map are its most cited traps.
6. **Construction authority (designed).** Outside the package that declares it, a struct type with an unexported field ([grammar.md](grammar.md#visibility)) is neither defaultable nor literal-constructible. Each of these is an error outside the package:
   - a composite literal of the type, including the empty literal `Quantity{}`;
   - an implicit zero value: `var q Quantity` with no value, `make([]Quantity, n)` with `n > 0`, a zero-filled field of type `Quantity` in an enclosing struct's literal or zero value, and the zero value of a map lookup that misses (use `m.Lookup(k)`, which returns `Option`).

   Inside the declaring package, the construction-authority restriction is lifted, and the ordinary rules 1 to 4 still apply. A struct with an unexported field of a non-defaultable type stays non-defaultable in its own package:

   ```go
   type State interface{ Idle | Running }
   type Idle struct{}
   type Running struct{ Since int64 }
   type Box struct{ state State }

   func newBox() Box { return Box{state: Idle{}} } // allowed: a literal inside the package
   func zeroBox() Box { var b Box; return b }       // error even inside the package: State is a union, so Box is not defaultable (rule 3)
   ```

   Values from outside come only from the functions the package exports, so `ParseQuantity` is the only way to build a `Quantity` and its check cannot be bypassed. Exports stay by capitalisation. *Prior art:* Rust forbids constructing a struct with private fields outside its module and gives it no `Default` unless the module derives or implements one; Gleam's `opaque` types hide constructors; Go leaves this hole open: a two-package probe builds `Quantity{}` and `make([]Quantity, 1)` from another package and reads `units=0` for both (review round 2 receipt).

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

Status: **checked** by `internal/sketch/check` ([#196](https://github.com/cevr/effra/issues/196)); runs.

```go
type Orders interface {
	Find(id string) (Order, error[OrderNotFound])
}
```

1. An interface with method signatures declares a capability. Method names in one capability are distinct (S108).
2. **A capability is not a first-class value.** The capability name denotes the provider that the nearest `with` binds. `Orders.Find(id)` calls that provider and charges `Orders` ([rows.md](rows.md#capability-rows)). Any other use of the name as an expression is an error: `Orders.Find` without a call (S108), and passing, returning, storing or capturing `Orders` (S104; README Gap 29).
3. **Capability name against interface type.** The same interface declaration has two roles, told apart by position:
   - in a `uses` clause, a `wire` entry, or as the receiver of a method call `Orders.m(…)`, the name is the **capability**: an ambient requirement that only `with` supplies;
   - in a type position (a parameter, result, field or type argument), the name is an ordinary **interface type**. A value of it is a provider value. Calling its method, `o.Find(id)`, runs that provider's method with the method's declared error row and charges no capability. A constructor that returns `Orders` returns such a value.

   This keeps decision #102: a capability is an ordinary interface bound by `wire`, and only the ambient binding is second-class. The checker implements both roles (README Gap 29); the build refuses a method call on an interface value until [#50](https://github.com/cevr/effra/issues/50). *Prior art:* Effect separates a service's tag, the requirement, from a service value; Scala 3 capture checking treats capabilities apart from their types.
4. A capability method's signature carries its error row. A capability or interface method writes no capability row (S108), because a provider's requirements are captured at construction (README Gap 17).
5. The prelude capabilities `Console`, `Clock`, `Scheduler` and `Random` are ordinary interfaces bound by `wire` (decision #102). The prelude (`internal/sketch/std/prelude.ef`) declares them with the methods the fixtures and the corpus call, provisionally until [#209](https://github.com/cevr/effra/issues/209) fixes their method sets.
6. An interface is also a generic constraint, with Go's method-set rules.

## Methods

Status: **checked** (#196); the build refuses a direct method call until #50.

1. A method call on a value, `v.M(…)`, including on a composite literal, `S{}.M(…)`, calls `M` with `v` as its receiver, as in Go. Its rows are `M`'s declared rows (README Gap 27).
2. A receiver names a struct type declared in the package (S108). A method name is unique per receiver type (S108). A parameter may not repeat the receiver's name (S108).
3. A method may write a capability row, and a call of it charges that row. Such a method cannot implement a capability method (S106) or an interface method (S108), because its row is wider (README Gap 17).
4. A pointer receiver `*T` means the method mutates its receiver ([Borrows](#borrows)).
5. An error type may not declare a method named `Error` (S108), because the emitter generates it; an error type's text comes from its fields (fixture `error-method-on-error-type`).

## Interface assignability

Status: **checked** (#196); runs where the prototype emitter can lower the program.

```go
type Store interface{ Get() string }
type Mem struct{}

func (Mem) Get() string { return "ok" }
func NewStore() Store { return Mem{} } // Mem implements Store
```

1. A value of type `T` is assignable to an interface type `I` when `T`'s method set has every method of `I`, and for each such method:
   - the parameter types and the result type are identical to `I`'s;
   - the method is fallible exactly when `I`'s method is;
   - its error row is a subset of `I`'s method's error row;
   - its `uses` row is a subset of `I`'s method's `uses` row.

   This is method-row compatibility: an implementation may fail less and require less, never more. It is the same check `wire` applies to a provider (S106; [rows.md](rows.md#wire)).
2. A union type is not an interface in this sense; it is assignable only by the variant rule.
3. Resources keep their rules: assigning a resource to an interface without `Close` is an error (R22), and assigning it to one with `Close` moves it (R23) ([resources.md](resources.md#conversions)).
4. *Prior art:* Go satisfies interfaces implicitly by method set with identical signatures; Effect requires a service implementation's error and requirement types to fit the tag's; Java and Swift let an overriding method throw fewer checked errors, never more.

## Error types

Status: **checked** (#196).

1. A type named in any error row is an error type. It must be a named struct type, of this package or of an imported one such as `timeout.Expired` (S104). Inside a package, a struct type is an error type when an error row of that package, or of the package that declares the type, names it; a struct type that no such row names is an ordinary value type.
2. An error type is not an ordinary value: returning a value of an error type is a failure ([rows.md](rows.md#error-rows); fixture `error-as-value`, S101).
3. Error rows are sets of independent struct types, not one declared sum per function. *Prior art:* Gleam returns one declared `Result` error type per function; OCaml polymorphic variants and Koka effect rows are open sets; Effra's rows are closed sets of nominal types with no open tail in v0.

## Operators

Status: **checked** (#196); the build refuses operators other than `==` and `+` until #50.

1. Go's operators on Go's operand types, with Go's untyped constants and exact constant arithmetic (an overflow is S108): arithmetic on numeric types, `+` on strings, ordering on numbers and strings, `==` and `!=` on comparable types (structs whose fields are comparable included; README Gap 26), `&&`, `||` and `!` on `bool`, shifts on integers. S108 otherwise.
2. `duration` takes `+`, `-`, ordering and `==` between durations; an untyped integer never converts to `duration` implicitly (S108). Its other arithmetic is [#175](https://github.com/cevr/effra/issues/175)'s.
3. `&&` and `||` short-circuit (README Gap 10). `comparable` is a constraint; ordering constraints such as `cmp.Ordered` are [#211](https://github.com/cevr/effra/issues/211)'s.

## Function values

Status: rule 1 is **checked** (#196): a function value converts to a function type with identical parameters and results and rows that are subsets of the type's (S108 otherwise). Rules 2 to 5 are **designed, not yet implemented** ([#197](https://github.com/cevr/effra/issues/197)).

1. A function type carries its parameters, result, error row and `uses` row. It carries requirements only, never purity ([rows.md](rows.md#footprints)).
2. A function-typed parameter is non-escaping unless its type says otherwise: the callee may call it and may hand it to a task that the call consumes, but it may not store, return or send it. A trailing block is such an argument. *Prior art:* Swift non-escaping closures by default; Effekt second-class blocks.
3. A `func` literal that escapes captures by copy and may not assign captured locals. A non-escaping inline block may assign them. A captured resource follows [resources.md](resources.md#closures).
4. **Concurrent parameters.** A function parameter that the callee hands to `go`, directly or through another call, is concurrent. The compiler infers this fact and records it in the exported interface summary. A block or literal passed to a concurrent parameter may not assign captured locals, so `parallel { n++ }` is an error. *Prior art:* Swift `@Sendable` parameters.
5. **Replayable parameters.** A transaction body ([stdlib.md](stdlib.md#sync-and-stm)) may not assign captured locals, use channels or consume a resource, and its footprint holds only `sync.Tx`.

## Borrows

Status: placement (rule 1, first sentence) is **checked** (#196): `*T` outside a parameter or receiver type is S108. The rest is **designed, not yet implemented** ([#197](https://github.com/cevr/effra/issues/197)).

1. `*T` appears only on parameters and receivers. A borrow is never nil, stored, returned, sent or captured by `go`.
2. Two mutable borrows into one value must not overlap. With constant indexes the checker decides; with dynamic indexes it needs a dominating guard or it rejects the call. *Prior art:* Swift's law of exclusivity; Rust `get_many_mut`.
3. There are no pointer fields and no reference fields. Linked structures use recursive values behind unions, index arenas or `sync.Ref`. This is a loss against Go. *Prior art:* Rust keeps both reference fields, at the cost of lifetime parameters on the struct, and owned pointer fields (`Box`, `Rc`); Effra gives up reference fields so that no signature needs a lifetime, and replaces written owned pointers with compiler-boxed recursion behind unions; Swift value types and Hylo's mutable value semantics make a similar trade.
4. Resources are lent by passing them as arguments ([resources.md](resources.md#lending)). A borrow and a lend obey the same no-escape rule.

## Generics

Status: **checked** (#196), by rows-and-constraints §3 and §7 amendment 2; the build refuses generics until #50. The spelling is provisional until [#135](https://github.com/cevr/effra/issues/135) decides it (README Gap 23).

```go
func Map(xs []A, limit int64, f func(A) (B, error[E]) uses R) ([]B, error[E]) uses R
```

1. Type parameters are implicit. A name of one upper-case letter, optionally followed by digits, that names no declaration is an implicit type parameter. Its kind follows its position: inside `error[…]` an error row (`E`), after the capability-row keyword a capability row (`R`), as an argument of a generic type the kind of that type's parameter (`Task[T, E]` makes `E` a row), elsewhere a type (`A`, `B`); one name in two kinds is S108. A bracket list is optional on a function and gives constraints and the order of explicit instantiation: `[T]` means `[T any]`, `[F error]` declares an error-row parameter, bracket parameters come first in explicit instantiation, and implicit ones are always inferred. A function whose parameters are all bracket parameters, instantiated with all of them, is a function value: `f := Id[int]` (Go's explicit instantiation); a function with implicit parameters is a value only through a call. A generic type declaration writes its bracket list. Methods take no type parameters of their own (S108). The kinds of generic type declarations are solved together, before any is resolved, so a recursive or mutually recursive head (`type T[R] struct{ F func() uses R; N *T[R] }`) gets the kind its uses imply.
2. Constraints are `any`, `comparable`, `error` (an error-row parameter) and interfaces with methods; ordering constraints such as `cmp.Ordered` are [#211](https://github.com/cevr/effra/issues/211)'s. Method constraints resolve through the type's owner package, with one answer per type. An argument that does not satisfy its constraint is S108. A constraint that names another parameter (`[T any, V I[T]]`) is checked after substitution of the arguments.
3. Rows in a generic signature may be elided only where they pass through unchanged. A declared requirement is never elided.
4. Exported signatures spell their rows and type parameters, so clients check against interface summaries, Go's export-data model.
5. **Lowering.** Generics emit as Go generics, which Go compiles by GC shape. Specialised copies need a measured win under [#190](https://github.com/cevr/effra/issues/190). An instantiation cycle, a generic type or function that instantiates itself with a growing argument (`func f[T any]() { f[[]T]() }`), is S108, as Go rejects it: its instances never end. The checker finds it on the instantiation graph, not by a depth limit, and before rows are inferred, so rows iterate over a finite set. *Prior art:* Go GC-shape stenciling; MoonBit and Rust monomorphise; Effra's earlier measurements refused frontend monomorphisation ([design.md](../design.md#compile-speed-is-a-language-constraint)).
6. **Inference.** Type arguments are inferred by unification over the value arguments, as Go infers function type arguments, never from a call's expected result. A row parameter binds to the row of a function-typed argument, less the row's written part; `error[E | Expired]` is the union. When several arguments bind one row parameter, it binds to the union of their rows: rows are finite sets, and a function with a narrower row is assignable where a wider one is expected, so the union is the least binding that admits every argument. A written type argument is not widened. A composite literal of a generic type, or a generic variant in a `case`, written without type arguments takes them from the expected type, else from its field values (`Some{n}`, `Enter{x}`). A type parameter left unbound is S108; a row parameter left unbound is empty. A result-position type parameter bound by a block with no value stands for no value, lowered to `struct{}`. *Prior art:* Go `types2/infer.go`; Koka infers effect rows; OCaml and ReScript disambiguate constructors by the expected type; Gleam's `Nil` and Rust's `()` for no value.

## Prelude types

Status: **declared** in the embedded prelude (`internal/sketch/std/prelude.ef`) and checked by the checker (#196); the build refuses them until #50, except the timeout result `timeout.Expired`. The capabilities' method sets are provisional until [#209](https://github.com/cevr/effra/issues/209); `Option` is [#99](https://github.com/cevr/effra/issues/99)'s.

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
