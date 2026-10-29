# Types

Definitions of every term used here are in the [glossary](../../GLOSSARY.md). Each section carries its status.

Types are nominal. Two named types are identical only when they are the same declaration. A value of type `T` is assignable to type `U` when `T` and `U` are identical, when `U` is a union that lists `T` ([Unions](#unions)), or when `U` is an interface type that `T` implements ([Interface assignability](#interface-assignability)). *Prior art:* Go's named types and implicit interface satisfaction; Gleam and ReScript custom types are nominal too.

## Basic types

Status: **implemented in prototype** for `string`, `int64` and `bool`; **partly implemented** for `duration`, which the prototype types only as a literal and as the `timeout.After` argument (README Gap 25).

1. `string` holds bytes. A literal's value is the bytes its spelling decodes to ([grammar.md](grammar.md#literals)).
2. `int64` is the only integer type with checked meaning in v0 (README Gap 9). Non-constant arithmetic wraps; constant overflow is an error.
3. `bool` is the type of comparisons and `if` conditions.
4. `duration` is the type of duration literals. It is a basic type, usable in every type position: parameters, results, fields and type arguments. Its name is predeclared. Prototype: `duration` as a type name is S104, and the name is reserved as a source name (S109).
5. Designed, with [#81](https://github.com/cevr/ergo/issues/81) and [#211](https://github.com/cevr/ergo/issues/211): Go's other numeric types, `byte`, `rune`, floating point, slices `[]T`, maps `Map[K, V]`, queues `queue.Queue[T]` ([runtime](runtime.md#channels)), function types and the prelude `Option[T]`.
6. **Literals are typed where they appear** (196u; checked by `internal/sketch/check`). A literal takes the type its context expects: a written `let` type, a parameter (after the type arguments that typed arguments fixed), a result (a `return` or a tail value), a record field, a slice element, a Map key or value, the other operand of a binary operator or of `..`, an assignment's target, an index or slice bound (`int`), and a constant `case` (the subject's type). An integer literal may take any integer or float type, a float literal only a float type, a rune literal any integer type, and a string literal a string type; an integer literal is never a `duration` (S108 "an integer literal is not a duration: write a duration literal such as 5s"). Every other position gives no expected type, and the literal takes its default: `int`, `float64`, `rune`, `string`, `duration`, and `bool` for `true` and `false`. A literal that cannot take the expected type is S108 naming the form to write ("a float literal takes only a float type, not int; write 2").
7. **A name has one type.** A package-level `let` has one type, and no name converts by itself: `let b int8 = Limit` with `let Limit = 10` is S108 "Limit is int; write int8(Limit) or declare Limit int8". Constant expressions evaluate exactly, and an overflow of the typed result is S108 ("the constant 200 overflows int8" for `let y int8 = 100 + 100`); a literal fits its type after its sign, so `-128` is an `int8`. Ergo has no untyped constants: Go's untyped kinds, their defaulting order and the contextual types Go records afterwards are the counter-example (#196, 196u "Why"). *Prior art:* Rust and Swift type a literal from its context with `i32` and `Int` defaults, and `1 + 2.5` is an error in Rust; MoonBit overloads an integer literal by its expected type (`let x : Int64 = 1`); Gleam, and ReScript after OCaml, keep `Int` and `Float` apart with separate operators (`+`, `+.`); Elixir's numbers are dynamic.

## Zero values and definite initialisation

Status: **implemented in prototype** for struct literals; **designed** for the rest.

1. A type is *defaultable* when it has a valid zero value: a basic type, or a struct whose fields are all defaultable. A union, a capability, a function type, a `Task`, a resource, a borrow, and a struct that contains any of them are not defaultable. Outside its package, a struct type with an unexported field is not defaultable either (rule 6).
2. A keyed struct literal must set every field whose type is not defaultable (S108). A positional literal sets every field.
3. Designed: a variable of a non-defaultable type must be initialised before use. `let p Payment` with no value is an error. *Prior art:* Swift and Kotlin definite initialisation; Go instead gives every type a zero value, which is how a nil interface reaches a type switch.
4. Designed: an operation that invents zero values needs a defaultable element type. The decided language has no such built-in (`make`, the comma-ok forms and channels are gone, #196); the library returns `Option` instead, as `m.Lookup(k)` returns `Option[V]`.
5. There is no `nil`. *Prior art:* Gleam, ReScript and Rust have no null; Go's typed-nil interface and nil map are its most cited traps.
6. **Construction authority (designed).** Outside the package that declares it, a struct type with an unexported field ([grammar.md](grammar.md#visibility)) is neither defaultable nor literal-constructible. Each of these is an error outside the package:
   - a composite literal of the type, including the empty literal `Quantity{}`;
   - an implicit zero value: `let q Quantity` with no value, a zero-filled field of type `Quantity` in an enclosing struct's literal or zero value, and the zero value of a map lookup that misses (use `m.Lookup(k)`, which returns `Option`).

   Inside the declaring package, the construction-authority restriction is lifted, and the ordinary rules 1 to 4 still apply. A struct with an unexported field of a non-defaultable type stays non-defaultable in its own package:

   ```go
   type State = Idle | Running
   type Idle {}
   type Running { Since int64 }
   type Box { state State }

   func newBox() Box { return Box{state: Idle{}} } // allowed: a literal inside the package
   func zeroBox() Box {
   	let b Box // error even inside the package: State is a union, so Box is not defaultable (rule 3)
   	return b
   }
   ```

   Values from outside come only from the functions the package exports, so `ParseQuantity` is the only way to build a `Quantity` and its check cannot be bypassed. Exports stay by capitalisation. *Prior art:* Rust forbids constructing a struct with private fields outside its module and gives it no `Default` unless the module derives or implements one; Gleam's `opaque` types hide constructors; Go leaves this hole open: a two-package probe builds `Quantity{}` and `make([]Quantity, 1)` from another package and reads `units=0` for both (review round 2 receipt).

## Structs

Status: **implemented in prototype**.

```go
type Order {
	ID    string,
	Total int64,
}

let order = Order{ID: "42", Total: 1999}
let same = Order{"42", 1999}
```

1. Field names in one struct are distinct (S108). A field and a function on the type (see [Methods](#methods)) may share a name: `x.f` is the field first (196m).
2. A composite literal is all keyed or all positional (S001). A positional literal lists every field in order (S108). A keyed literal sets each field at most once (S108) and names only declared fields (S104).
3. Each field value must be assignable to the field's type (S108). A field value whose failure is not handled is S105. A field value that is a composite literal writes its type, keyed or positional: `Wrap[S]{value: {h: 1}}` is S108 "missing type in composite literal; write S{h: 1}" (the literal's source is `{…}` past 40 characters or across a line). As in Go, a composite literal may elide its type only as an element of an array, slice or map literal or as a map literal's key (Go spec, "Composite literals"; #196 round 17 C1). *Prior art:* go/types reports "missing type in composite literal" there, and Ergo adds the fix; Rust and Gleam always name a record's constructor.
4. A struct must not contain itself by value, directly or through other structs (S108). Recursion behind a union is allowed, because a union value is a reference in the lowered Go. *Prior art:* Swift's `indirect enum` boxes recursion; Go rejects an invalid recursive type.
5. A struct with a resource field is itself a resource ([resources.md](resources.md#what-is-a-resource)).
6. Designed: struct embedding is allowed only in `wire` and in interfaces. Value semantics hold: a copy is a snapshot, and move-on-last-use keeps it cheap.

## Unions

Status: **implemented in prototype**.

```go
type Payment = Pending | Authorized | Declined

type Pending {}
type Authorized { AuthID string }
type Declined { Reason string }
```

1. A union lists one or more variants. Each variant must be a struct type declared in the package (S108); the prototype requires the same file. A variant may not be listed twice (S108).
2. A value of a variant type is assignable to every union that lists it. A struct may be a variant of several unions (fixture `multiple-unions`).
3. A union has no zero value.
4. A type switch narrows a union value to its variants ([rows.md](rows.md#totality)).
5. The emitter generates a marker method per union on each variant, so a source method with that marker's name is an error in the prototype (fixture `marker-method`, S109).
6. **Lowering.** A union becomes a sealed Go interface with an unexported marker method, and each variant keeps its own struct fields. *Prior art:* Borgo lowers an enum to a tagged struct and matches on the tag; ReScript picks a representation per variant; Ergo keeps the interface form so a Go type switch narrows and each variant keeps its fields. The size trade-off between the two forms is not measured.

## Capabilities

Status: **checked** by `internal/sketch/check` ([#196](https://github.com/cevr/ergo/issues/196)); runs.

```go
type Orders {
	Find(id string) Order fails OrderNotFound,
}
```

1. An interface with method signatures declares a capability. Method names in one capability are distinct (S108).
2. **A capability is not a first-class value.** The capability name denotes the provider that the nearest `with` binds. `Orders.Find(id)` calls that provider and charges `Orders` ([rows.md](rows.md#capability-rows)). Any other use of the name as an expression is an error: `Orders.Find` without a call (S108), and passing, returning, storing or capturing `Orders` (S104; README Gap 29).
3. **Capability name against interface type.** The same interface declaration has two roles, told apart by position:
   - in a `needs` clause, a `wire` entry, or as the receiver of a method call `Orders.m(…)`, the name is the **capability**: an ambient requirement that only `with` supplies;
   - in a type position (a parameter, result, field or type argument), the name is an ordinary **interface type**. A value of it is a provider value. Calling its method, `o.Find(id)`, runs that provider's method with the method's declared error row and charges no capability. A constructor that returns `Orders` returns such a value.

   This keeps decision #102: a capability is an ordinary interface bound by `wire`, and only the ambient binding is second-class. The checker implements both roles (README Gap 29); the build refuses a method call on an interface value until [#50](https://github.com/cevr/ergo/issues/50). *Prior art:* Effect separates a service's tag, the requirement, from a service value; Scala 3 capture checking treats capabilities apart from their types.
4. A capability method's signature carries its error row. A capability or interface method writes no capability row (S108), because a provider's requirements are captured at construction (README Gap 17).
5. The prelude capabilities `Console`, `Clock`, `Scheduler` and `Random` are ordinary interfaces bound by `wire` (decision #102). The prelude (`internal/sketch/std/prelude.ergo`) declares them with the methods the fixtures and the corpus call, provisionally until [#209](https://github.com/cevr/ergo/issues/209) fixes their method sets.
6. An interface is also a generic constraint, with Go's method-set rules.

## Methods

Status: **checked** (#196, 196m); the build refuses a direct method call until #50.

There are no receivers. A package function whose first parameter is a named type declared in the package is a function on that type, and `x.f(y)` calls `f(x, y)` (196m).

1. `x.f(a)` resolves to a field `f` of `x` first. Otherwise it resolves to the package function `f`, in the package that declares `x`'s type, whose first parameter's type is `x`'s type: `T`, `*T` or, for a generic `T`, `T[A, B]` whose arguments are distinct type parameters of `f`, which a call on an instance binds. A first parameter written as one of the function's own bracket parameters is that parameter, never a package type of the same name. The call checks the other arguments against `f`'s signature without its first parameter, and its rows are `f`'s rows, inferred or written.
2. `x.f` without a call is only a field (S104 when `x` has no field `f`): a function on a type has no method value. Written type arguments keep the call: `x.f[int](a)` is the call `x.f(a)` with the bracket parameters of `f` that its first parameter does not bind written in order (`g.pick[string]("x")` for `func pick[T, U](g G[T], u U) U`); `x.f[int]` alone is still S104. When `f` has no such bracket parameter, the brackets are no type arguments: `s.hs[0]()` is the S104 of `s.hs`, which suggests the call `s.hs()[0]`.
3. Several package functions may share a name when their first parameters are distinct named types declared in the package. Such a name is called as `x.f(…)`; a plain call `f(x)` with more than one candidate is S108 "several functions are named f; call it as x.f(…)". Lowering emits them as Go methods on the type, which Go allows.
4. A named type satisfies an operation type when, for each operation, a function on the type exists (a field of the same name does not hide it: `x.Value` reads the field, and `func Value(c Counter) int` still provides `Value() int`) with the same remaining signature, no wider error row and no extra `needs` (S106, rows.md Wire 2). A function with type parameters beyond those its first parameter binds can be called as `x.f(…)`, but it cannot satisfy an operation, which takes no type parameters (S106, naming the parameter).
5. A first parameter `*T` needs an addressable `x` (S108), as Go's pointer receiver does ([Borrows](#borrows)).
6. An error type may not declare a function named `Error` on itself (S108), because the emitter generates it; an error type's text comes from its fields (fixture `error-method-on-error-type`).
7. A call `x.f(…)` reaches the one function `x`'s type picks, for the order of row inference and for Go's initialization order, never every function named `f`: an initialization cycle through it names it by its type, `B.m`. *Prior art:* MoonBit resolves `x.f` by `x`'s inferred type in the type's package and allows same-named methods on different types; go/types resolves a selector with LookupFieldOrMethod and orders initialization by the method it resolved.

## Interface assignability

Status: **checked** (#196); runs where the prototype emitter can lower the program.

```go
type Store { Get() string }
type Mem {}

func Get(_ Mem) string { return "ok" }
func NewStore() Store  { return Mem{} } // Mem implements Store
```

1. A value of type `T` is assignable to an interface type `I` when `T`'s method set has every method of `I`, and for each such method:
   - the parameter types and the result type are identical to `I`'s;
   - the method is fallible exactly when `I`'s method is;
   - its error row is a subset of `I`'s method's error row;
   - its `needs` row is a subset of `I`'s method's `needs` row.

   This is method-row compatibility: an implementation may fail less and require less, never more. It is the same check `wire` applies to a provider (S106; [rows.md](rows.md#wire)).
2. A union type is not an interface in this sense; it is assignable only by the variant rule.
3. Resources keep their rules: assigning a resource to an interface without `Close` is an error (R22), and assigning it to one with `Close` moves it (R23) ([resources.md](resources.md#conversions)).
4. *Prior art:* Go satisfies interfaces implicitly by method set with identical signatures; Effect requires a service implementation's error and requirement types to fit the tag's; Java and Swift let an overriding method throw fewer checked errors, never more.

## Error types

Status: **checked** (#196).

1. A type named in any error row is an error type. It must be a named struct type, of this package or of an imported one such as `timeout.Expired` (S104). Inside a package, a struct type is an error type when an error row of that package, or of the package that declares the type, names it; a struct type that no such row names is an ordinary value type.
2. An error type is not an ordinary value: returning a value of an error type is a failure ([rows.md](rows.md#error-rows); fixture `error-as-value`, S101).
3. Error rows are sets of independent struct types, not one declared sum per function. *Prior art:* Gleam returns one declared `Result` error type per function; OCaml polymorphic variants and Koka effect rows are open sets; Ergo's rows are closed sets of nominal types with no open tail in v0.

## Operators

Status: **checked** (#196); the build refuses operators other than `==` and `+` until #50.

1. Go's operators on Go's operand types, with exact constant arithmetic (an overflow of the typed result is S108): arithmetic on numeric types, `+` on strings, ordering on numbers and strings, `==` and `!=` on comparable types (structs whose fields are comparable included; README Gap 26), `&&`, `||` and `!` on `bool`, shifts on integers. S108 otherwise. A comparison is `bool`.
2. **One type per operator** (196u rule 4). Both operands of a binary operator have one type, and no operand converts by itself: `a + b` with `a int32` and `b int64` is S108 "mismatched types int32 and int64; write int64(a)". A literal takes the other operand's type (`x + 1` with `x int8` adds two `int8`s). Two literals each take the expected type, else their defaults, so `let d = 1 + 2.5` is S108 "mismatched types int and float64; write 1.0 + 2.5". *Prior art:* Rust's rule; Gleam and OCaml separate float operators.
3. **Shifts and conversions** (spec D-A3). A shift's left literal takes the expected type when that is an integer type, else `int`; any other expected type is S108 "the left operand of a shift is an integer, and float64 is expected here; write int(1) << n, then convert the result". The count is any integer type. A conversion `T(x)` gives a literal `x` no expected type: `x` is typed on its own, then converted, so `float64(1 << n)` converts an `int` shift (in Go it is invalid, a known trap). *Prior art:* Rust's `as` never types its operand from the target; Gleam and MoonBit shift through library functions on one integer type; Zig requires a fixed-width left operand unless the count is comptime-known.
4. `duration` takes `+`, `-`, ordering and `==` between durations; an integer literal is never a `duration` (S108). Its other arithmetic is [#175](https://github.com/cevr/ergo/issues/175)'s.
3. `&&` and `||` short-circuit (README Gap 10). `comparable` is a constraint; ordering constraints such as `cmp.Ordered` are [#211](https://github.com/cevr/ergo/issues/211)'s.

## Function values

Status: rule 1 is **checked** (#196): a function value converts to a function type with identical parameters and results and rows that are subsets of the type's (S108 otherwise). Rules 2 to 5 are **designed, not yet implemented** ([#197](https://github.com/cevr/ergo/issues/197)).

1. A function type carries its parameters, result, error row and `needs` row. It carries requirements only, never purity ([rows.md](rows.md#footprints)).
2. A function-typed parameter is non-escaping unless its type says otherwise: the callee may call it and may hand it to a task that the call consumes, but it may not store, return or send it. A trailing block is such an argument. *Prior art:* Swift non-escaping closures by default; Effekt second-class blocks.
3. A `func` literal that escapes captures by copy and may not assign captured locals. A non-escaping inline block may assign them. A captured resource follows [resources.md](resources.md#closures).
4. **Concurrent parameters.** A function parameter that the callee hands to `go`, directly or through another call, is concurrent. The compiler infers this fact and records it in the exported interface summary. A block or literal passed to a concurrent parameter may not assign captured locals, so `parallel { n++ }` is an error. *Prior art:* Swift `@Sendable` parameters.
5. **Replayable parameters.** A transaction body ([stdlib.md](stdlib.md#sync-and-stm)) may not assign captured locals, use channels or consume a resource, and its footprint holds only `sync.Tx`.

## Borrows

Status: placement (rule 1, first sentence) is **checked** (#196): `*T` outside a parameter or receiver type is S108. The rest is **designed, not yet implemented** ([#197](https://github.com/cevr/ergo/issues/197)).

1. `*T` appears only on parameters and receivers. A borrow is never nil, stored, returned, sent or captured by `go`.
2. Two mutable borrows into one value must not overlap. With constant indexes the checker decides; with dynamic indexes it needs a dominating guard or it rejects the call. *Prior art:* Swift's law of exclusivity; Rust `get_many_mut`.
3. There are no pointer fields and no reference fields. Linked structures use recursive values behind unions, index arenas or `sync.Ref`. This is a loss against Go. *Prior art:* Rust keeps both reference fields, at the cost of lifetime parameters on the struct, and owned pointer fields (`Box`, `Rc`); Ergo gives up reference fields so that no signature needs a lifetime, and replaces written owned pointers with compiler-boxed recursion behind unions; Swift value types and Hylo's mutable value semantics make a similar trade.
4. Resources are lent by passing them as arguments ([resources.md](resources.md#lending)). A borrow and a lend obey the same no-escape rule.

## Generics

Status: the spelling is decided on [#196](https://github.com/cevr/ergo/issues/196) (196g as amended, sixth round). The 196a checker (`internal/sketch/check`) checks the earlier implicit spelling, in which an undeclared one-letter name is a parameter; check-v1 brings it to this spelling. The build refuses generics until [#50](https://github.com/cevr/ergo/issues/50).

```go
func Transform[A, B, E, R](xs []A, limit int64, f func(A) B fails E needs R) []B fails E needs R {
	// designed
}
```

1. Type parameters are declared in brackets after the name, `[A, B, E, R]`; a bare upper-case letter is not a parameter by itself. The kinds stay implicit: a parameter may range over types (`A`, `B`), failure rows (`E`, after `fails`) or service rows (`R`, after `needs`). `[T any]` is written `[T]`.
2. A constraint follows its parameter inline, `[T ordered]`: a method set, or one of the predeclared `comparable`, `ordered` and `number`. There is no `where` clause. Method constraints resolve through the type's owner package, with one answer per type.
3. Rows in a generic signature may be elided only where they pass through unchanged. A declared requirement is never elided.
4. Exported signatures spell their rows and type parameters, so clients check against interface summaries, Go's export-data model.
5. **Lowering.** Generics emit as Go generics, which Go compiles by GC shape. Specialised copies need a measured win under [#190](https://github.com/cevr/ergo/issues/190). An instantiation cycle, a generic type or function that instantiates itself with a growing argument (`func f[T]() { f[[]T]() }`), is S108, as Go rejects it: its instances never end. A method's receiver parameters count as the type's own, as `go/types` canonicalises them, so `func (r R[T]) M() { R[[]T]{}.M() }` is a cycle too. Signatures count like bodies: a parameter, a result, a bodiless declaration, an interface-typed parameter or a constraint that instantiates with a growing argument (`func (R[T]) M(x R[[]T])`), through a renamed receiver parameter or across two receiver types, is a cycle as well. The checker finds it on the instantiation graph, not by a depth limit, and before rows are inferred, so rows iterate over a finite set. *Prior art:* Go GC-shape stenciling; MoonBit and Rust monomorphise; Ergo's earlier measurements refused frontend monomorphisation ([design.md](../design.md#compile-speed-is-a-language-constraint)).
6. **Inference.** Type arguments are inferred by unification over the value arguments with Go's unifier (`go/types` unify.go), never from a call's expected result. Inference is sound and a documented subset of Go's (D6 as amended in rounds 7 and 8): when Ergo infers, Go infers the same types for the same call after row erasure. The subset is typed value arguments, structural unification, interfaces through method sets, conflicts between arguments, literals (196u rule 6: a literal argument binds a parameter only when no typed argument binds it, and a parameter that only literals bind takes their common default, so `Two(1, 2)` infers `int` and `Two(x, 2)` with `x int8` infers `int8`; mixed defaults, `Two(1, 'a')`, are S108 "cannot infer T; write the type arguments"), and written type arguments. Every typed argument unifies first, in source order, then the literals default, and each literal is then checked with its parameter's type expected. A function literal is a typed argument, since its parameter and result types are written (inferring unwritten ones is #214); only its unwritten rows take the parameter type's, so `Apply(func(v int8) int8 { return v }, 1)` and `Rev(1, func(v int8) int8 { return v })` infer `int8`. A trailing block writes no types and binds what is left after the literals. Outside the subset, the call is S108 "cannot infer T; write the type arguments, e.g. Fn[int](…)", its only diagnostic: a parameter only a method or core-type constraint determines (`Use(x)` for `Use[T comparable, P M[T]]`, where `type M[T] { M(T) }`), and a generic function passed as an argument without all its type arguments (`Apply(Id, 1)`, `Fix(Id)`). The written forms stay valid: `Use[int](x)`, `Apply(Id[int], 1)`; a constraint error stays a constraint error when typed or written arguments fix every type argument. A call whose inference fails or is refused reports one diagnostic, and its result is invalid, so nothing cascades from it. An argument that is invalid, or is itself a failed or refused call, fails the enclosing generic call with no diagnostic of its own (one rule, in the inference loop; round 9), so `Id(Apply(Id, 1))` reports one refusal; independent errors in other arguments still report. Each typed argument is assigned to its parameter: the match may be inexact at the top (an interface on either side matches a type that has its methods, by unifying the method signatures, so a wider interface, a named generic interface, a concrete type or a pointer infers through its method set), and element types match exactly. A parameter bound by an earlier argument must match every later one: a conflict is S108 at the argument (`Two(x, y)` with `x` an interface and `y` a struct; of an interface and a non-interface neither is chosen, so the order of the arguments never decides), while `Two[I](x, y)` for an operation type `I { M() }`, with every type argument written, infers nothing and only assigns. Ergo departs from Go where its types do: a named type unifies with a type literal only when it is an interface (types are nominal), and a named union unifies with the variant its argument names. A generic function inside parentheses is a value, as in Go: `(Fn[int])` is the instance, and `(Fn)[int]` or `(Fn)(1)` is S108. In `Info`, an explicit instantiation `Fn[int]` and the parentheses around it carry the instance type while the name `Fn` keeps its generic type; in an inferred call `Fn(1)` the name carries the instance, as in `go/types`. Every literal carries its one type in `Info`, recorded when it is checked ([Basic types](#basic-types) 6; 196u rule 7): no expression's type is updated afterwards. A row parameter binds to the row of a function-typed argument, less the row's written part; `fails E | Expired` is the union. When several arguments bind one row parameter, it binds to the union of their rows: rows are finite sets, and a function with a narrower row is assignable where a wider one is expected, so the union is the least binding that admits every argument. A written type argument is not widened. A composite literal of a generic type, or a generic variant in a `case`, written without type arguments takes them from the expected type, else from its field values (`Some{n}`, `Enter{x}`). A type parameter left unbound is S108; a row parameter left unbound is empty. A result-position type parameter bound by a block with no value stands for no value, lowered to `struct{}`. *Prior art:* Go `types2/infer.go`; Koka infers effect rows; OCaml and ReScript disambiguate constructors by the expected type; Gleam's `Nil` and Rust's `()` for no value.

## Prelude types

Status: **declared** in the embedded prelude (`internal/sketch/std/prelude.ergo`) and checked by the checker (#196); the build refuses them until #50, except the timeout result `timeout.Expired`. The capabilities' method sets are provisional until [#209](https://github.com/cevr/ergo/issues/209); `Option` is [#99](https://github.com/cevr/ergo/issues/99)'s.

- `Option[T]` with `Some` and `None` ([#99](https://github.com/cevr/ergo/issues/99)).
- `Task[T, E]`, a resource ([runtime.md](runtime.md#tasks)).
- `Decision[S, O]` with `Enter`, `Stay`, `Ignore`, `Reject` and `Done` ([runtime.md](runtime.md#machines-and-actors)).
- The capabilities `Clock`, `Scheduler`, `Random` and `Console`.

## Laws

Status: **designed, not yet implemented** ([#125](https://github.com/cevr/ergo/issues/125), [#170](https://github.com/cevr/ergo/issues/170)).

```go
type Codec[T] {
	Encode(v T) string,
	Decode(s string) T fails Malformed,

	law RoundTrip(v T) { try Decode(Encode(v)) == v },
}

type Store {
	Find(id string) Order fails NotFound,
	Put(id string, o Order),

	law FindAfterPut(id string, o Order) { Put(id, o); try Find(id) == o },
}
```

1. A law is `law X(params) { … }`, a member of an operation type beside its operations (196l). Its body ends in a `bool` expression, and a failure that escapes the body refutes the law. `ergo test` checks it like a fuzz target, with generated arguments.
2. Every law is a provider law: `ergo test` checks it against every provider of its type, which the closed world lists.
3. A refuted law refuses the build. A law that passes a finite suite is reported as tested, never as proved (NORTH_STAR pillar 4).
4. Laws are declared in their type, not found by name; the `LawX` naming rule is gone. The same form serves every client: codecs, providers, schedules and machines. Until laws are implemented, the prototype refuses a law with S109 ([#162](https://github.com/cevr/ergo/issues/162)). *Prior art:* Lean type classes carry their laws as members; Haskell states class laws beside the class and checks them with QuickCheck; Elixir `StreamData` properties.

## Codecs

Status: **designed, not yet implemented** ([#49](https://github.com/cevr/ergo/issues/49)).

1. A type's wire form is found by name: `ParseX` decodes into `X`, and `x.Wire()` encodes. Go's `netip.ParseAddr` and `String` pair is the model.
2. A derived codec's rows are the union of its fields' rows. `json.Decode[Order]` has the error and `needs` rows of every `ParseX` it reaches.
3. README Gap 20 records the open question about discovery by name.
