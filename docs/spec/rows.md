# Rows: errors, capabilities, `wire` and `with`

Every function has two rows besides its result: an error row, `error[...]`, and a capability row, `needs`. The checker proves that each row is an upper bound on what the body can do. Rows are checked and then erased: the lowered Go has interfaces, structs, early returns and type switches. Definitions are in the [glossary](../../GLOSSARY.md).

*Prior art:* Effect's `Effect<A, E, R>` tracks the same two rows as type parameters; Koka types effects as rows and checks that each is handled; OCaml 5 has effect handlers but does not track effects in types, so an unhandled effect is a run-time exception; Go tracks neither and threads `error` and `context.Context` by hand.

## Error rows

Status: **checked** by `internal/sketch/check` ([#196](https://github.com/cevr/ergo/issues/196)); runs through the prototype emitter.

```go
func pay(id string) (string, error[OrderNotFound | GatewayDown]) needs Orders, Gateway {
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

   The failure target is the innermost enclosing function or function literal. A trailing block is a function literal ([grammar.md](grammar.md#expressions)), so the block of `timeout.After` is a failure target, as every trailing block is (README Gap 31). A `with` block is part of a construct, not a function literal, so it is not a failure target: a `try` inside it escapes to the enclosing function.
5. **Upper bound (S101).** Every error type that can escape a function must be in its declared row. A function with no row lets nothing escape. Returning a value of an error type from a function with no row is therefore S101 (fixture `error-as-value`).
6. **Value of `try`.** `try e` has `e`'s success value, or no value when `e` has none. `try` on an expression that cannot fail is an error (S108).
7. **Lowering.** `try f()` becomes `v, err := f(…); if err != nil { return zero, err }`. Error types implement Go's `error`; the row is checked and erased. *Prior art:* Borgo's tuple path for `?`; XGo's inline `expr?`; Rust's `?`; Gleam has no operator and uses `use` with `result.try`.

## Catch

Status: **checked** (#196), the binding form and statement arms included; the build refuses the binding form and statement arms until [#50](https://github.com/cevr/ergo/issues/50).

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
4. **Total catch (S103).** A `catch` without `try` must handle every error type in `e`'s row. *Prior art:* Rust and Gleam exhaustive `match` on an error enum; Swift propagates an error that no `catch` clause matches to the enclosing scope, and a non-throwing function must handle every error; Ergo's total catch is the same obligation stated per error type.
5. **Partial catch.** `try e catch { … }` handles the named types and escapes the rest through `try`. The rest escapes to the enclosing failure target and is checked against its row (rules 4 and 5 of [Error rows](#error-rows); fixture `try … catch` in `sketch/sketch_test.go`, "partial catch under try still needs the rest declared", S101).
6. **Typed failures only (README Gap 1).** A `catch` covers the typed row only. Interruption and defects are not in any row, and they always pass through a `catch`, total or partial. A total catch therefore has no default arm in source. Its lowering must still pass interruption and defects on; the prototype emits a default arm that re-panics with them.
7. **Binding and statements.** `catch e { case T: … }` binds the caught error, as `switch e := err.(type)` does; inside an arm with one type, `e` has that type. An arm may hold statements. When the `catch` is an expression, the arm's last statement is an expression that yields its value, or the arm leaves the function. With several types in one arm, `e` has the union of those types. `e` must be read in some arm (S110). `return` in an arm leaves the enclosing function, because an arm is not a block (README Gap 11).
8. **Lowering.** A catch becomes a type switch on the Go error value, inline, with no closure. *Prior art:* XGo lowers `expr!` and `expr?:default` through a generated closure; Ergo's catch stays inline.

## Capability rows

Status: **checked** (#196), with inference (rule 6); runs.

1. **Spelling.** `needs A, B` lists capabilities ([types.md](types.md#capabilities)). The row is a set.
2. **Charges.** A body charges a capability when it:
   - calls a capability method, `A.m(…)`, which charges `A`;
   - calls a function whose row lists `A`;
   - calls `timeout.After`, an ordinary library function whose signature lists `needs R, Scheduler` (README Gap 31).
3. **Upper bound (S102).** Every capability a body charges must be in its `needs` clause or discharged by an enclosing `with`. A name in a `needs` clause must denote a capability (S104).
4. **Entry.** `main` takes no parameters and returns no value; an error row is allowed (S113). It cannot be called (S113). The checker does not restrict its capability row; the build refuses what the host cannot supply (README Gaps 4 and 5).
5. **Names.** A written capability row binds each listed capability's bare name in the body's frame, beside the parameters; `needs http.Client` binds `Client` (with packages; when two packages export the same name, the body qualifies it). A local of that name in the same frame is S104. A local in an inner frame, or a local beside a capability the row does not list, may shadow a package or prelude capability (rows-and-constraints §7 amendment 3; fixtures `local-and-binding-one-frame`, `same-local-as-cap`). A capability name is the capability in a capability row, a wire key, a `replace` target and as the receiver of `X.m(…)`; in a type position it is the interface type ([types.md](types.md#capabilities)).
6. **Inference.** Inside a package the checker infers each unwritten row of an unexported function with a body and of a function literal; a written row is declared. `main`, `LawX`, exported functions, methods, interface methods, bodiless declarations and the step of an exported machine type spell their rows, and an unwritten row there is empty, so a changed body never silently widens a public contract. A literal passed where a function type is expected is checked against that type's rows. Inference runs per strongly connected component of the package's reference graph, joining rows to a fixpoint, callees before callers (rows-and-constraints §7 amendment 1). The graph's edges are the plain identifiers of each body that resolve, through the body's scopes, to a package function or var: a local, a parameter or a receiver that shadows a package function is not a reference to it (round 15), and a selector's name, a label, a type, a wire key and a struct literal's field name are not references. A composite literal's key is a field name when the literal's type is a struct and an expression otherwise (a map key, an array index), as in Go; the walk decides it from the literal's written type, through the type declarations and, for an elided type, the enclosing array, slice or map literal's element or key type, and walks a key whose literal's kind it cannot decide (a type parameter) as an expression (round 17). A struct field's value never elides its type ([types](types.md#structs) 3, round 18), so the walk looks up no field: an elided literal there is S108 and the walk only visits its elements, and the walk does linear work in the literal's size (round 18). The checker finds them by a walk of every body before any body is typed, so a type error never hides an edge and the verdict does not depend on the order of the declarations (round 16, class H). A method through a selector and a function of another package spell their rows, so they need no edge. Until its component is final, an inferred row is a row variable of the function: a type that mentions the function (`Box{f}` in a package initializer read before `f` is checked) holds the variable, and once the component is final the variable stands for the solved row, so that type equals the one written with the solved row (round 12, class F). A use of a generic function applies its type arguments to the solved row. A function used as a value inside its own recursive component (passed, stored, compared or placed in a composite literal; a call is not a value use) has the rows it writes there, and each row it does not write is empty, as for an exported function. The same holds for each row of a member that a function literal, a trailing block or a go task inside the component calls, when that literal or task is a value that would carry the row (round 15). A literal carries each row it does not write, a trailing block of a call other than `with` carries both rows, and a go expression carries the error row (its capabilities are charged at the go site). A literal called on the spot passes the rows on like a call, a `with` block passes them on to its enclosing body, and a go statement discards its task. A row the literal writes stops the member's row, and so does a row it takes from the parameter of a callee it is passed to directly whose signature was fixed before any body, when that parameter's function type has the row with no type parameter in it: the signature was resolved before any body, so the stop does not depend on typing or on order (round 17). Such a callee is a function of the package, the universe or an imported package; a local bound to a non-generic one of these (`run := each`); or a method, or a field of function type, selected from a receiver whose type is written: a parameter, the receiver, a local with a written type or bound to a composite literal, or a composite literal itself (`r.Each(…)` with `r Runner`, round 18). The receiver's type is resolved where the local is declared, so a later local that shadows the type's name does not change it; a parameter's type is the one its function's signature resolved, and a written structural type (`r struct{ Run func(…) error[E] }`) or a pointer to a named type (`r *Runner`) selects its method or field like a named one (round 19). A typed local (`var r struct{ Run func(…) error[E] } = input`) and a composite literal (`r := struct{ … }{…}`) fix a written struct type the same way, resolved at their declaration through the package scope; a struct type that names a type parameter, an implicit one or a name a local shadows stays conservative (round 20). A callee that is a value of inferred type, a generic function bound to a local, and a parameter row that names a type parameter keep the round-15 rule, which is conservative. A row taken from any other expected type does not stop it, because the value uses are found from the source before typing, like the edges (round 16). So no type inside a component depends on a row still being inferred, call admission does not depend on the rows, and the rows grow to their least fixpoint (round 14, class G). When the body of such a function needs more than the empty row, its declaration is S108 "f is used as a value at L:C inside its own recursive group; write its error row: func f(…) error[…]" ("through the function literal at L:C", "through the trailing block at L:C" or "through the go task at L:C" when one carries the use), or "write its capability row: func f(…) needs …", and every reference to it is invalid, so nothing follows from the empty row; the checker never chooses between fixpoints. The diagnostic is at the declaration, where the row is written, so its position does not depend on which use is first (round 16). The use it names is the first in the body whose kind and name sort first, so it moves with that body's declaration (round 17). Calls inside the component stay inferred, and a value use from outside the component reads the final row. The fixpoint is bounded by 2·(bodies + edges)·(D + 1) body checks, where D counts the distinct row elements the component has produced; an element removed and added again does not raise D, so the bound is finite. Past it the component is S108 "cannot infer the error row of f; write it". *Prior art:* Go export data; OCaml `.mli` signatures; Ergo's earlier rule for exported contracts. For class G: OCaml `let rec` is monomorphic inside its own group; Haskell needs a signature for polymorphic recursion; TypeScript reports a name "referenced directly or indirectly in its own initializer"; Go and Rust require every signature. Each refuses the circular case instead of choosing a fixpoint.
7. **Lowering.** Capabilities become Go interfaces whose methods take the task handle first. A function takes a hidden environment with one field per capability, and the task. When `wire` knows the concrete provider, the call may be devirtualised.

## Footprints

Status: **designed, not yet implemented**.

1. A function's footprint is every capability its body exercises, including those its own `with` provides, plus `go`. The compiler infers it and never asks for it. `ergo inspect` shows it.
2. A function is pure when its footprint is empty, not when its `needs` row is empty. `with(wire{ Console = Stdout{} }) { Console.Println("x") }` has an empty row and a non-empty footprint.
3. A function type carries requirements only (README Gap 24). A call through an unknown function value counts as impure. A call is pure only when the compiler specialises it to a known function with an empty footprint.
4. **Capability safety.** Only providers in host packages call Go. Ordinary code reaches the operating system only through a capability, so every effect appears in some footprint ([stdlib.md](stdlib.md#host-packages)).
5. A function value whose row names a capability bound by `with` cannot escape that `with`. *Prior art:* Scala 3 capture checking and scoped capabilities; Effekt second-class blocks. A purity-carrying function type stays a candidate for [#189](https://github.com/cevr/ergo/issues/189).

## Wire

Status: **checked** (#196): provider literals, closed named wires, embedding, `replace` and several keys in one entry; the build refuses embedding, `replace` and several keys until #50. Constructors are **designed, not yet implemented**: S109 naming [#37](https://github.com/cevr/ergo/issues/37).

```go
wire Live {
	Orders    = MemoryOrders{}
	Gateway   = FakeGateway{AuthID: "auth-7"}
	Scheduler = LiveScheduler{}
}
```

1. **Entries.** A `wire` binds capabilities to providers, one entry per capability: `Cap = provider`. An entry naming a non-capability is S104. Wire names are unique (S108). A second entry for a capability in one wire, other than a `replace` of an embedded entry, is an error (S111; README Gap 28). One entry may list several capabilities, `Clock, Scheduler = v`: one shared value that must implement each (README Gap 34). *Prior art:* Google Wire rejects two providers of one type in a provider set.
2. **Providers (S106).** A provider literal's type must implement every method of the capability: the same name, the same parameter types, the same result and the same fallibility. Each error type of a provider method must be in the capability method's row: a provider may fail less, never more. A provider method that writes a capability row is wider than the capability method, which writes none (S106; README Gap 17).
3. **Inline wires.** `wire{ A = X{}; B = Y{} }` appears only as the argument of `with` (S108 elsewhere). Its provider expressions are evaluated in the enclosing function, before the block's discharge, so their requirements and failures charge the enclosing function. A provider expression does not see the capabilities of its own wire (fixture `wire-sibling`, S102); it sees those of an enclosing `with` (fixture `wire-order`).
4. **Named wires.** A named wire is built by its own function at each `with` that names it, so its provider expressions do not see the caller's locals (fixture `named-wire-shadow`). A named wire is closed: its providers require no capability from the caller (S102) and let no failure escape (S101). It may use a capability it binds itself. Open named wires are #37's (README Gap 6).
5. **Cycles.** A named wire whose build can reach itself is an error (S108): a provider expression that enters it with `with`, a function or method a provider calls that does, a capability method whose provider in the package does, or an embedding. Entries of one named wire whose providers use each other's capabilities (Wire 4) form a cycle too, since none can be built first (S108, once per cycle). The checker follows every method of the called name for a call on a value of interface type, which over-approximates. On a reported embedding cycle, what an embedding of a wire on the same cycle brings is no second entry (S111), against the wire's own entries or its other embeddings, because it depends on how the cycle is fixed; a `replace` may still name it. The checker computes each wire's bindings once, over the strongly connected components of the embedding graph, and checks each wire body once for second entries (Wire 1 and 7). A component's bindings are a persistent set that shares the largest of its parts (an entry set or an embedded component's bindings) and adds the others. The proof is bounded (D16, amended for wires in rounds 19 to 21) in two parts, both proportional to the package's wire size (its wire declarations and literals, their entries and embeddings, the capability names written in wire keys and `needs` rows, plus one). Repeatable work, which can run more often than a fixed multiple of the wire source it reads, is charged before it runs against one budget per package, 128 × the wire size: a step is a set node read, copied or created, an element enumerated, a probe against a summary, or a sort's comparison or move, and a sort charges its whole bound, n⌈log₂ n⌉ comparisons and two moves per element and level, before it starts. Single-pass work, which reads each wire node a fixed number of times (entries, keys, embeddings, the steps of the embedding graph's walk, each component's members, each root), is not charged; it is at most 12 × the wire size. The pass does at most 140 × the wire size work. The count follows the source alone, so a package's verdict does not depend on the machine or on time. Nor does it depend on the order of the declarations (round 15's rule; round 20): the pass runs once, before any body, over the named wires in name order and then over the inline wires grouped by the name of their top-level declaration; sets are keyed by the capabilities' qualified names; and every union keeps the larger side, the first of equal sizes in that order. An inline wire's keys resolve in its body's scope, so the pass charges the most repeatable work its entries can do (per key, lookups as deep as the embeddings' union and the cycle's summary, and one step per collision) and checks them once, at the first check of the body. Past the budget, the wire whose summary or body was being checked is S111 "this wire's embedding graph is too large to check for duplicate entries; split it", at its name, or at each inline wire of the declarations that share the name being checked; a wire whose summary was not computed binds nothing and its dependents report nothing more from it (D3), so the checker never accepts a wire it has not checked. An embedding chain whose every wire adds an entry, a wide fan-in, an inline fan-in, an entry of 8000 keys and a replaced diamond of 8000 wires each charge under 25% of the budget, 8000 empty wires 31%, and the largest wire of the corpus and examples under 4% (checker tests `TestWireBudget`, `TestWireBudgetPrefixes`, `TestWireSinglePass`, `TestWireBudgetOrder`, `TestWireBudgetHeadroom`; #196 rounds 17 to 21). *Prior art:* go/types orders package initialization over its dependency graph once (initorder.go); OCaml's `Set`, Erlang maps and Gleam's dicts share structure between versions; TypeScript's instantiation limit (TS2589) and rustc's `recursion_limit` refuse past a fixed count, as Totality 5 does for switches.
6. **Constructors (designed; S109 naming #37).** An entry may name a constructor: `Orders = OpenPostgres`. A constructor is an ordinary function. Its `needs` row is satisfied from the same wire graph, and its error row joins the startup errors of every `with` that builds it. A constructor that returns a resource gives the wire its release ([resources.md](resources.md#wire)). *Prior art:* Effect `Layer.effect` and `Layer.scoped`; Google Wire's generated constructor with cleanups in reverse order.
7. **Composition.** A wire may embed another wire by name. `replace Cap = provider` overrides an embedded entry; a second entry for one capability without `replace` is an error (S111). Two embedded wires that bind one capability are S111 unless a `replace` entry of the embedding wire, named or inline, overrides it; a `replace` of a capability no embedded wire binds is S108. A shared node is built once per build, and each `with` is a new build. *Prior art:* Effect `Layer.merge`, `Layer.provide` and `Layer.fresh` (README Gap 22).
8. **Header (parsed; S109 naming [#196](https://github.com/cevr/ergo/issues/196) stage b).** A named wire may state what it exposes and what it requires: `wire App provides Account, Invoice needs Config { … }`. Both clauses are optional, and `provides` comes first.
   - `provides A, B` lists the capabilities a `with(App)` discharges. An entry the wire builds and does not list stays private to its build: `Store = MemoryStore{}` can serve `Account` and `Invoice` without reaching the `with` body. Without `provides`, every entry is exposed. `binds(W)` in [Composition laws](#composition-laws) is this list.
   - `needs C, D` lists the capabilities its providers require from the `with` site. They join that site's row (`needs(W)` in [Composition laws](#composition-laws)).
   - The parser and formatter implement both; the checker refuses them with S109 naming #196 stage b, which checks them. *Prior art:* Roc, where an app `provides` entry points to a platform that `requires` them; Effect's `Layer<ROut, E, RIn>`; OSGi's `Provide-Capability` and `Require-Capability`; Dagger's `@Provides` (README Gap 33).

## With

Status: **checked** (#196) for one wire argument; runs. Context values are **designed, not yet implemented**: S109 naming G1.

```go
value := with(Live) { checkout(id) }
local := with(wire{ Orders = MemoryOrders{} }) { report(id) }
```

1. `with(W) { block }` takes one wire, named or inline, and a block (S108 otherwise). An unknown wire name is S104.
2. Inside the block, every capability that `W` binds is discharged: a charge of that capability does not reach the enclosing function's row.
3. **Nested `with`.** A `with` inside another keeps both scopes:
   - the inner wire's provider expressions are evaluated in the outer scope, so they see the outer bindings;
   - an inner entry shadows an outer binding of the same capability for the inner body only;
   - each outer provider keeps its own startup errors and its own release at the end of the outer block, even when an inner entry shadows it.

   Measured (fixture `wire-order`, output `outer inner`):

   ```go
   with(wire{ Store = Holder{Value: "outer"} }) {
   	with(wire{ Store = Holder{Value: "inner"}; Box = Holder{Value: read()} }) {
   		Console.Println(Box.Get(), Store.Get()) // read() saw the outer Store
   	}
   }
   ```

   Flattening the two into one wire `wire{ Store = Holder{"inner"}; Box = Holder{read()} }` changes the program: `read()` would need `Store` from its own wire, which is S102 (fixture `wire-sibling`).
4. `with(wire{})` binds nothing and keeps the enclosing bindings (fixture `empty-wire`).
5. When the `with` is used as a value, the block's value is its last expression statement; a `with` used as a statement needs none. `return`, `break` and `continue` cannot leave the block (S108; [grammar.md](grammar.md#statements) rule 5). Locals declared in the block are scoped to it.
6. A `with` expression is fallible when its last expression is fallible. A failure that a `try` in the block escapes goes to the enclosing function. Designed: the startup errors of the wire's constructors join the `with` expression's row.
7. **Context values (designed, G1).** `with` also takes context values: `with(trace.Span("ingest"), log.Annotate("region", "eu")) { … }` ([runtime.md](runtime.md#g1-context-through-with)).
8. **Lowering.** `with` copies the environment and sets the wire's fields inline, with no closure. A named wire is its own Go function.

*Prior art:* Effect `Effect.provide` with a layer; Go `context.WithValue`; Java `ScopedValue.where(…).run(…)`; Gleam has no provision construct, and dependencies are passed as arguments.

## Totality

Status: **checked** (#196); the build refuses the multi-subject form until #50.

1. **Type switch.** `switch v := u.(type) { case A: … }` requires a union subject (S108). Each case type must be a variant of the union (S108). A case that the cases before it already cover is unreachable (S108). In a case with one type, `v` has that type; otherwise `v` has the union type. Each binding must be read in at least one case or be `_` (S110).
2. **Totality rule (S103; README Gap 3).** A type switch must name every variant of its union or have a `default` arm. *Prior art:* Gleam, Rust and Swift reject a non-exhaustive match; Go accepts one.
3. **Missing return (S107).** A function with a result must not reach the end of its body. A body terminates by Go's terminating statements: a final `return`; an `if` with an `else` whose branches both terminate; a `for` with no condition and no `break`; a `switch` or `select` whose cases all terminate; or a total type switch whose cases all terminate.
4. **Total catch.** See [Catch](#catch), rule 4.
5. **Multi-subject switch (README Gap 12).** `switch s, e := s.(type), e.(type)` with tuple cases is total over the product of the unions; `_` matches any variant. The checker decides totality by a usefulness check over the case matrix (Maranget), never by enumerating the product, and names one missing tuple (S103). The proof is bounded (D16, amended in rounds 3 and 5): the solver splits on the column that most rows need (Maranget's necessity heuristic, ties broken by the column's source position), memoizes each residual matrix, and counts its steps, and every other superlinear pass over the case matrix, against a fixed budget. The solver is deterministic for a given source. Reordering the subjects may change whether a switch near the budget is refused; it never changes a proved verdict. Totality is decided over the maximal case rows, so it does not depend on the order of the cases. A switch with `default` needs no totality proof, and its cost stays linear in its subjects. A case resolves to its variant by instance identity (D16 as amended in round 8): the union's variant index is a discrimination tree over the variants' types, built once per origin union and shared by every instance of it, with every edge hashed (D16 as amended in round 9): a constructor edge by its constructor, a variable edge by its declaration parameter, reached through the instance's type arguments indexed once by identity, and a row that mentions a parameter (`error[E]`) by its pattern, reached through the instance's bound rows indexed once by identity. A case follows its own path, so it resolves without scanning or substituting the variants that share its origin, and resolving the cases is linear in the variants and the cases, for shared origins, variable edges, row-bearing signatures, generic unions and tuple cases alike. Every type has one identity, a hash-consed id: its constructor plus its children's ids, one hash lookup per node, so equal structures share one id. The checker compares types, caches generic instances and indexes rows and variants by that id, never by a serialized type (round 11); interned types never change, an inferred row being a variable solved beside the type (round 12), and the bound identities and rows of an instance are indexed once and shared by every switch subject of that instance (round 10). As a safety net, every operation of resolving a switch's cases, each type node it interns and each row operation included, is charged to that switch's step counter in proportion to its size, and every insertion into the index to the union's own counter, against the same budget; past it the switch is S103 "too many cases to prove this switch total; split it or add default", so no uncounted pass runs before the solvers. Reachability (S108, a case the cases before it cover) follows the cases' order by definition and has a budget of its own. A switch over either budget is S103 ("too many cases to prove this switch total; split it or add default", or "… which cases of this switch are reachable; split it"); the checker never accepts a switch it has not proved. Every switch of up to 6 subjects of up to 3 variants stays far below the budget. *Prior art:* rustc's `pattern_complexity_limit`, OCaml's Parmatch, GHC's `-fmax-pmcheck-models`.

## Composition laws

Status: **designed**. These laws state the algebra of the row constructs. Law tests ([types.md](types.md#laws)) will check them.

| Composition | Error row | Capability row |
| --- | --- | --- |
| `a; b` (sequence) | `rows(a) ∪ rows(b)` | `needs(a) ∪ needs(b)` |
| `try e` | escapes `rows(e)` | `needs(e)` |
| `e catch { case T: h }` (total) | `rows(h)` | `needs(e) ∪ needs(h)` |
| `try e catch { case T: h }` | `rows(e) − {T} ∪ rows(h)` | `needs(e) ∪ needs(h)` |
| `with(W) { b }` | `rows(b) ∪ startup(W)` | `needs(b) − binds(W) ∪ needs(W)` |
| `timeout.After(d) { b }` | `rows(b) ∪ {timeout.Expired}` | `needs(b) ∪ {Scheduler}` |

1. **Wrapping.** Moving a body into a function `f` and calling `f()` changes no row, provided `f` declares the body's rows. Ordinary functions wrap every construct losslessly.
2. **Nesting keeps both scopes.** `with(W1) { with(W2) { b } }` is not, in general, one `with` of an overlaid wire: `W2`'s initializers see `W1`'s bindings, and `W1`'s providers keep their startup errors and releases ([With](#with), rule 3). The body `b` sees `W2`'s entries over `W1`'s.
3. **Overlay is a law of wire declarations only.** `wire Test { Live; replace Orders = MemoryOrders{} }` is `Live` with the `Orders` entry replaced: the replaced constructor is never built, so its startup errors and release leave `Test`. *Prior art:* Effect's `Layer.provide` against `Layer.merge` keeps the same distinction between nesting and composition.
4. **Capability rows of nesting.** In `with(W1) { with(W2) { b } }`, the body `b` has `binds(W1) ∪ binds(W2)` discharged. `W2`'s own needs are checked against `W1`'s bindings and the enclosing function, not against `W2`.
5. **Row idempotence.** `with(W) { with(W) { b } }` has the rows of `with(W) { b }`. It still builds `W` twice, once per `with`.
6. **Identity.** `with(wire{}) { b }` has the rows of `b`.
7. **Catch subtraction.** A partial catch that names every type of `rows(e)` has the rows of the total catch.
