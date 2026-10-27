# Rows: errors, capabilities, `wire` and `with`

Every function has two rows besides its result: an error row, `error[...]`, and a capability row, `uses`. The checker proves that each row is an upper bound on what the body can do. Rows are checked and then erased: the lowered Go has interfaces, structs, early returns and type switches. Definitions are in the [glossary](../../GLOSSARY.md).

*Prior art:* Effect's `Effect<A, E, R>` tracks the same two rows as type parameters; Koka and OCaml 5 track effects in rows; Go tracks neither and threads `error` and `context.Context` by hand.

## Error rows

Status: **implemented in prototype**.

```go
func pay(id string) (string, error[OrderNotFound | GatewayDown]) uses Orders, Gateway {
	order := try Orders.Find(id)
	return Gateway.Charge(order)
}
```

1. **Spelling.** `error[A | B]` lists error types ([types.md](types.md#error-types)). The row is a set: order and repetition carry no meaning. A function with an error row and no value writes `error[A]` as its result; with a value, `(T, error[A])`.
2. **Fallible expressions.** An expression is fallible when it can fail with a non-empty row:
   - a call to a function or capability method that declares an error row;
   - `timeout.After(d) { … }` ([runtime.md](runtime.md#timeouts));
   - `with(W) { … }` whose last expression is fallible.

   A `try` expression is not fallible: its failures have already escaped (rule 4).
3. **Handling (S105).** A fallible expression must be tried, caught or returned. It is an error in every other position: a `:=` value, an expression statement, an operand, a selector's receiver, a call argument, a struct field value, an `if` condition, a switch subject, a `timeout.After` duration and a `catch` arm's value.
4. **Escapes.** A failure escapes to the enclosing failure target in exactly these ways:
   - `try e`, which escapes `e`'s whole row;
   - `return e` where `e` is fallible, which passes `e`'s success and its failures through;
   - `return v` where `v` is a value of an error type;
   - the unhandled rest of a partial `try e catch { … }`.

   The failure target is the enclosing function, or the innermost `timeout.After` block. A `with` block is not a failure target: a `try` inside it escapes to the enclosing function.
5. **Upper bound (S101).** Every error type that can escape a function must be in its declared row. A function with no row lets nothing escape. Returning a value of an error type from a function with no row is therefore S101 (fixture `error-as-value`).
6. **Value of `try`.** `try e` has `e`'s success value, or no value when `e` has none. `try` on an expression that cannot fail is an error (S108).
7. **Lowering.** `try f()` becomes `v, err := f(…); if err != nil { return zero, err }`. Error types implement Go's `error`; the row is checked and erased. *Prior art:* Borgo's tuple path for `?`; XGo's inline `expr?`; Rust's `?`; Gleam has no operator and uses `use` with `result.try`.

## Catch

Status: **implemented in prototype** for single-expression arms; the binding form is **designed, not yet implemented**.

```go
outcome := with(Live) { checkout(id) } catch {
case OrderNotFound:
	"no such order"
case GatewayDown, timeout.Expired:
	"gateway unavailable"
}

order := try Orders.Find(id) catch {
case OrderNotFound:
	Order{ID: id}
} // any other failure of Find escapes through try
```

1. **Arms.** `e catch { case T, U: v … }` handles failures of the fallible expression `e`. `catch` on an expression that cannot fail is an error (S108).
2. Each type in an arm must be in `e`'s row (S108). A type may appear in only one arm (S108).
3. **Value.** When `e` has a value of type `T`, each arm's value must be assignable to `T` (S108), and the catch expression has type `T`. When `e` has no value, an arm must have no value (S108). An arm's value whose own failure is not handled is S105.
4. **Total catch (S103).** A `catch` without `try` must handle every error type in `e`'s row. *Prior art:* Rust and Gleam exhaustive `match` on an error enum; Swift `do`/`catch` without exhaustiveness lets a failure pass silently, which this rule forbids.
5. **Partial catch.** `try e catch { … }` handles the named types and escapes the rest through `try`. The rest escapes to the enclosing failure target and is checked against its row (rules 4 and 5 of [Error rows](#error-rows); fixture `try … catch` in `sketch/sketch_test.go`, "partial catch under try still needs the rest declared", S101).
6. **Typed failures only (README Gap 1).** A `catch` covers the typed row only. Interruption and defects are not in any row, and they always pass through a `catch`, total or partial. A total catch therefore has no default arm in source. Its lowering must still pass interruption and defects on; the prototype emits a default arm that re-panics with them.
7. **Binding and statements (designed).** `catch e { case T: … }` binds the caught error, as `switch e := err.(type)` does; inside an arm with one type, `e` has that type. An arm may hold statements. When the `catch` is an expression, the arm's last statement is an expression that yields its value. `return` in an arm leaves the enclosing function, because an arm is not a block (README Gap 11).
8. **Lowering.** A catch becomes a type switch on the Go error value, inline, with no closure. *Prior art:* XGo lowers `expr!` and `expr?:default` through a generated closure; Effra's catch stays inline.

## Capability rows

Status: **implemented in prototype**.

1. **Spelling.** `uses A, B` lists capabilities ([types.md](types.md#capabilities)). The row is a set.
2. **Charges.** A body charges a capability when it:
   - calls a capability method, `A.m(…)`, which charges `A`;
   - calls a function whose row lists `A`;
   - calls `timeout.After`, which charges `Scheduler`.
3. **Upper bound (S102).** Every capability a body charges must be in its `uses` clause or discharged by an enclosing `with`. A name in a `uses` clause must denote a capability (S104).
4. **Entry.** `main` takes no parameters (S109) and cannot be called (S109). Prototype: its `uses` clause may list only `Console` (S102), and it returns nothing (S109). README Gaps 4 and 5 record the designed entry.
5. **Names.** Designed: `uses http.Client` brings the bare name `Client` into scope for the body; when two packages export the same name, the body qualifies it. Prototype: in a selector `X.m`, a capability named `X` wins over a local named `X`, so such a local is never read (fixture `same-local-as-cap`, S110). Designed: declaring a local with the name of a capability in scope is an error.
6. **Inference.** Designed: inside a package the compiler infers both rows of an unexported function. Exported functions spell them, so a changed body never silently widens a public contract. *Prior art:* Go export data; OCaml `.mli` signatures; Effra's earlier rule for exported contracts.
7. **Lowering.** Capabilities become Go interfaces whose methods take the task handle first. A function takes a hidden environment with one field per capability, and the task. When `wire` knows the concrete provider, the call may be devirtualised.

## Footprints

Status: **designed, not yet implemented**.

1. A function's footprint is every capability its body exercises, including those its own `with` provides, plus `go`. The compiler infers it and never asks for it. `ef inspect` shows it.
2. A function is pure when its footprint is empty, not when its `uses` row is empty. `with(wire{ Console = Stdout{} }) { Console.Println("x") }` has an empty row and a non-empty footprint.
3. A function type carries requirements only (README Gap 24). A call through an unknown function value counts as impure. A call is pure only when the compiler specialises it to a known function with an empty footprint.
4. **Capability safety.** Only providers in host packages call Go. Ordinary code reaches the operating system only through a capability, so every effect appears in some footprint ([stdlib.md](stdlib.md#host-packages)).
5. A function value whose row names a capability bound by `with` cannot escape that `with`. *Prior art:* Scala 3 capture checking and scoped capabilities; Effekt second-class blocks. A purity-carrying function type stays a candidate for [#189](https://github.com/cevr/effra/issues/189).

## Wire

Status: **partly implemented in prototype**: provider literals and closed named wires. Constructors, embedding and `replace` are **designed, not yet implemented**.

```go
wire Live {
	Orders    = MemoryOrders{}
	Gateway   = FakeGateway{AuthID: "auth-7"}
	Scheduler = LiveScheduler{}
}
```

1. **Entries.** A `wire` binds capabilities to providers, one entry per capability: `Cap = provider`. An entry naming a non-capability is S104. Wire names are unique (S108).
2. **Providers (S106).** A provider literal's type must implement every method of the capability: the same name, the same parameter types, the same result and the same fallibility. Each error type of a provider method must be in the capability method's row: a provider may fail less, never more. Prototype: `Scheduler` takes only `LiveScheduler{}`, `LiveScheduler{}` provides only `Scheduler`, and `Console` cannot be wired (all S106).
3. **Inline wires.** `wire{ A = X{}; B = Y{} }` appears only as the argument of `with` (S108 elsewhere). Its provider expressions are evaluated in the enclosing function, before the block's discharge, so their requirements and failures charge the enclosing function. A provider expression does not see the capabilities of its own wire (fixture `wire-sibling`, S102); it sees those of an enclosing `with` (fixture `wire-order`).
4. **Named wires.** A named wire is built by its own function at each `with` that names it, so its provider expressions do not see the caller's locals (fixture `named-wire-shadow`). Prototype: a named wire must be closed: its providers require no capability from the caller (S102) and let no failure escape (S101). It may use a capability it binds itself. Designed: README Gap 6.
5. **Cycles.** A wire whose providers reach `with` of the same wire, directly or through called functions or provider methods, is an error (S109). The prototype follows every provider method of a called capability method's name, which over-approximates.
6. **Constructors (designed).** An entry may name a constructor: `Orders = OpenPostgres`. A constructor is an ordinary function. Its `uses` row is satisfied from the same wire graph, and its error row joins the startup errors of every `with` that builds it. A constructor that returns a resource gives the wire its release ([resources.md](resources.md#wire)). *Prior art:* Effect `Layer.effect` and `Layer.scoped`; Google Wire's generated constructor with cleanups in reverse order.
7. **Composition (designed).** A wire may embed another wire by name. `replace Cap = provider` overrides an embedded entry; a second entry for one capability without `replace` is an error. A shared node is built once per build, and each `with` is a new build. *Prior art:* Effect `Layer.merge`, `Layer.provide` and `Layer.fresh` (README Gap 22).

## With

Status: **implemented in prototype** for one wire argument; context values are **designed, not yet implemented**.

```go
value := with(Live) { checkout(id) }
local := with(wire{ Orders = MemoryOrders{} }) { report(id) }
```

1. `with(W) { block }` takes one wire, named or inline, and a block (S108 otherwise). An unknown wire name is S104.
2. Inside the block, every capability that `W` binds is discharged: a charge of that capability does not reach the enclosing function's row.
3. An inner `with` that binds a capability shadows an outer binding of it for the inner block (fixture `wire-order`).
4. `with(wire{})` binds nothing and keeps the enclosing bindings (fixture `empty-wire`).
5. The block's value is its last expression, which must be an expression statement (S109). `return` cannot leave the block (S109). Locals declared in the block are scoped to it.
6. A `with` expression is fallible when its last expression is fallible. A failure that a `try` in the block escapes goes to the enclosing function. Designed: the startup errors of the wire's constructors join the `with` expression's row.
7. **Context values (designed, G1).** `with` also takes context values: `with(trace.Span("ingest"), log.Annotate("region", "eu")) { … }` ([runtime.md](runtime.md#g1-context-through-with)).
8. **Lowering.** `with` copies the environment and sets the wire's fields inline, with no closure. A named wire is its own Go function.

*Prior art:* Effect `Effect.provide` with a layer; Go `context.WithValue`; Java `ScopedValue.where(…).run(…)`; Gleam has no provision construct, and dependencies are passed as arguments.

## Totality

Status: **partly implemented in prototype**.

1. **Type switch.** `switch v := u.(type) { case A: … }` requires a union subject (S108). Each case type must be a variant of the union (S108) and appear once (S108). In a case with one type, `v` has that type; otherwise `v` has the union type. The binding must be read in at least one case (S110).
2. **Totality rule (designed; README Gap 3).** A type switch must name every variant of its union or have a `default` arm. The prototype does not enforce this rule. *Prior art:* Gleam, Rust and Swift reject a non-exhaustive match; Go accepts one.
3. **Missing return (S107).** A function with a result must not reach the end of its body. A body terminates when its last statement is a `return`; an `if` with an `else` whose branches both terminate; or a type switch whose cases all terminate and name every variant.
4. **Total catch.** See [Catch](#catch), rule 4.
5. **Multi-subject switch (designed; README Gap 12).** `switch s, e := s.(type), e.(type)` with tuple cases is total over the product of the two unions; `_` matches any variant.

## Composition laws

Status: **designed**. These laws state the algebra of the row constructs. Law tests ([types.md](types.md#laws)) will check them.

| Composition | Error row | Capability row |
| --- | --- | --- |
| `a; b` (sequence) | `rows(a) ∪ rows(b)` | `uses(a) ∪ uses(b)` |
| `try e` | escapes `rows(e)` | `uses(e)` |
| `e catch { case T: h }` (total) | `rows(h)` | `uses(e) ∪ uses(h)` |
| `try e catch { case T: h }` | `rows(e) − {T} ∪ rows(h)` | `uses(e) ∪ uses(h)` |
| `with(W) { b }` | `rows(b) ∪ startup(W)` | `uses(b) − binds(W) ∪ needs(W)` |
| `timeout.After(d) { b }` | `rows(b) ∪ {timeout.Expired}` | `uses(b) ∪ {Scheduler}` |

1. **Wrapping.** Moving a body into a function `f` and calling `f()` changes no row, provided `f` declares the body's rows. Ordinary functions wrap every construct losslessly.
2. **Nesting.** `with(W1) { with(W2) { b } }` equals a single `with` of `W1` overlaid by `W2`, where `W2`'s entries win.
3. **Idempotence.** `with(W) { with(W) { b } }` has the rows of `with(W) { b }`.
4. **Identity.** `with(wire{}) { b }` has the rows of `b`.
5. **Catch subtraction.** A partial catch that names every type of `rows(e)` has the rows of the total catch.
