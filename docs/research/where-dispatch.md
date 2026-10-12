# Where-clause dispatch: equality, ordering, hashing and derive

**Status:** design note and decision record for the spike in [#116](https://github.com/cevr/effra/issues/116), revision 4, 2026-10-12 (after reviews 1 to 3 and re-plan 1). It changes no compiler behaviour. Line references are at `main` `13e62ad4` unless stated otherwise; paths are under `internal/compiler/` unless stated otherwise. The prototype is on branch `wf10/spike-where-proto`, head `30f80104` (two commits on `13e62ad4`: `034df57d` the prototype, `30f80104` a crash fix). That branch is never merged. The measurement scripts and their receipts are under [`benchmarks/where/`](../../benchmarks/where/).

## Owner direction

- **2026-10-08.** Roc-style static dispatch (`where` constraints, specialised per type) was rejected because design.md then forbade monomorphization. The monomorphization spike disproved that rule's premise the same day: Go already compiles about one copy per GC shape, and lane SH1 kept Go generics with chosen shapes (`docs/design.md:301-303`).
- **2026-10-11, on #116.** "go's simplicity is the goal. We want to compile down to typescript." TypeScript has no type classes and Effect passes `Order<A>`/`Equal` values, so option (a) maps one-to-one onto what a TypeScript caller sees. Option (b) "must show what a TypeScript caller writes for a constrained function before (b) can win."
- **2026-10-11, the construct ladder** (memory `syntax-over-special-library`, adopted pending objection). Types are not special casing; a compiler branch keyed on an item's name is. The winner must sit on rung 1 (ordinary library) or rung 2 (library plus a general type feature). A rung-2 feature must be describable without naming the construct, serve two or more unrelated clients, read with Go's simplicity, project into the `.d.mts` with the same meaning and no `any`, and erase into plain Go or Go generics with SH1's shapes. No type-level language, no HKTs.
- **2026-10-11, orchestrator ruling during this spike.** Generated `.d.mts` projections must be fail-safe, not refuse-only. Each option's projection is attacked with the four routes from spike-rows review 4 (M1): method-parameter bivariance, global `interface Object` augmentation, a `never[]` rest, and a never-typed filler. A narrowing must depend on a type-level check that widens on failure and never lies.
- **2026-10-12, laws (#125).** Laws are a language construct that states and checks behaviour types cannot express, and they feed GDP-style evidence. #116 is its second client, so this decision must say where laws attach (§2.10).
- **2026-10-12, round 1 rulings.** Five rulings followed review 1:
  - state (b)'s gain without overstating it;
  - argue #100 by the lookup;
  - limit coherence to the `where` path, with unconditional capture, Q6, and ReScript's identity-in-the-type weighed;
  - specify conditional methods against SH1;
  - add the laws section, with the syntax reserved to #125.
- **2026-10-12, round 2 rulings.** The evidence boundary rule is binding: an exported evidence-minting function binds the canonical methods or drops the evidence in its projection, and generic forwarding obeys the same rule; the mechanism stays with #125 (§2.10). Polymorphic recursion is rescoped to generic functions, and Q6 states its cost and equality semantics.
- **2026-10-12, re-plan 1.** Review 3 confirmed the dispatch decision, conditional methods, coherence, the lowering and Q6. Its remaining findings were about evidence at the TypeScript boundary, which is #125's mechanism. So:
  - §2.10 keeps the contract recommendation and hands #125 four requirements;
  - `ts/b-evidence` becomes #125 input evidence, with review 3's attacks recorded as expected failures;
  - Q6 no longer rests on an unpinned iteration order.
  
  The owner also said the generic surface reads more like TypeScript than Go. That question is #135's, so the surface syntax here is provisional (§2.11).

## Verdict

**Choose (b): `where` constraints resolved through each type's owner module, lowered to an explicit method argument.** The scope is equality, ordering and hashing. Encoding stays an explicit codec witness, as today.

The deciding evidence:

1. **(b) erases to exactly (a).** The checker appends the resolved method as a trailing argument. Go gets a generic function with one more `func` parameter, JavaScript one more argument, and the `.d.mts` one more function-typed parameter (§2.4, §2.5). The TypeScript caller writes `sort3(xs, User.compare)`, which is what an Effect user writes with `Array.sort(xs, order)`. There is no overload set, no `any`, and no type-level computation. tsc 6.0.3 strict accepts the consumer with 0 `any` keywords in every declaration and runtime file (§3.1).
2. **The projection is fail-safe because it narrows nothing.** No declared type depends on a constraint holding. Each of the four attack routes is either refused or drops or falsifies the method argument itself, and the attack then throws at the first use of that argument (§3.2). One A4 variant returns a map whose captured methods are broken; it throws at its first insert or lookup, and Q3's boundary check would refuse it at construction. The one route that lets a mistyped value escape (key bivariance, A1b) is TypeScript's container hole: it is the same on a native `Map` and involves no constraint.
3. **(b) adds a general type-directed lookup with one answer per type.** On the `where` path, the method for a type is the one its owner module declares, so a generic caller cannot choose a different one, and a type whose owner declares none is refused there. A bare function, effect value or resource handle has no owner that declares `hash` or `equals` (§3.4, `benchmarks/where/probes/`). Under (a), any module can write a dictionary for File or an effect value, and the probes accept it.
   - Both options accept a record that wraps a File or a function when someone writes the methods or the dictionary.
   - Neither option can express identity equality on functions, handles or effects, because Effra has no `==` on them.
   - Under every option, a handle inside a record depends on spike-rows unit 4a's resource admission.
   - f64's missing default `compare` is library policy. (a) could adopt it equally by shipping no `Order<f64>`.
   - (c) refuses through a compiler-defined structural predicate, admits NaN, and needs a run-time key encoder on JavaScript.
4. **(b) costs nothing measurable in checker time or output size.** Run time and allocation were not measured. The method argument is the same function value (a) passes. (b) adds 0 Go stencils over (a). The binaries differ by under 200 bytes of 2.7 to 3.0 MB, and the JavaScript output by 48 bytes (§4.2). Checker time is linear in call sites (about 0.27 ms per site). At the recorded load it cannot be distinguished from (a): the b/a medians range from 0.92 to 1.32 over two passes, with a pooled geometric mean of 1.08.
5. **A general derive needs a type-directed lookup, and (b) supplies it as a general feature** (#100, §3.5). A field fold must find each field type's method. Under (a), derive either takes one explicit dictionary per field type, or looks dictionaries up by type inside derive. That second route is (b)'s lookup, kept private to derive. Under (b), ordinary generic code uses the same lookup, and derive adds no `eq`, `order` or `hash` profile word.
6. **Dispatch is name-neutral.** Resolution reads the method name from the `where` clause the library author writes. The prototype's only method-name literals list the bundled `i64` owner members it loads (`bundled.go:264` on the proto branch). They are data, not a branch, and W2 replaces them with an ordinary bundled import (§4.1).

**Losers, with their reasons kept (§7):**

- **(a) alone** projects to TypeScript exactly as (b) does, but it has no lookup.
  - Every call names its dictionary.
  - A hard type is admitted whenever the caller supplies the functions.
  - Two dictionaries for one key type can coexist, because nothing designates one as canonical.
  - A general derive needs per-field dictionaries or a lookup of its own.

  (a) survives inside (b) as the explicit form: every constrained library function is written over an explicit `…By(xs, compare)`, and the `where` form calls it.
- **(c)** is a rung-4 construct that names equality. Its TypeScript projection is a recursive conditional type that generic TypeScript callers cannot use (TS2589). JavaScript needs a run-time structural key encoder because a native `Map` compares records by identity. NaN is admitted at compile time, and the targets disagree unless the JavaScript lowering re-implements `cmp.Compare`: Go sorts NaN first, while JavaScript's `<` leaves `2,NaN,1` unsorted. Records still need `SortFunc`, which is (a).
- **(b) with a structural TypeScript projection** (`T extends { compare(other: T): bigint }`, methods on the values) is rejected because its declarations state a rule Effra does not check (§3.2):
  - A primitive cannot carry the method, so `sort3` over `bigint` or `string` has no projection.
  - Methods on values need classes or per-value method tables on JavaScript. Effra's frozen records have neither.
  - Owner lookup is not structural.

  Its bivariance and augmentation attacks pass tsc with no cast. They throw before returning, so they show that the method's presence can be bypassed. They do not show a false result.

## 1. Current state

- **No equality, ordering or hashing library exists.** Today's "`Order<T>` direction" is the ticket's framing: the owner's comment notes that Effect passes `Order`/`Equal` values. No `Order<T>`, `Equal`, `Hash`, map, list or sort exists in the compiler or the bundled modules.
- **Operators.** The binary operator guard (`semantic.go:5123-5137`) admits `==` on `string`, `bool` and `i64`, `+` on `string` and `i64`, and `-`, `*`, `/`, `%`, `<`, `<=`, `>`, `>=` on `i64` only. `!=` is absent syntax (`absent_syntax.go:93`). JavaScript emits `===`.
- **Host map keys.** A Go host `map` type is refused when its key is not `types.Comparable` (`host_types.go:728-731`). This is Go-only, and it is the only comparability rule in the compiler.
- **derive.** `Json.codec` is a bundled member with no body that the checker derives (`bundled.go` index `effra/json`, `codecs.go`), and `Http.endpoint`/`Http.routes` derive declarations (`http_endpoints.go`). #100 moves the codec to a closed profile word, `derive userJson = json<User>(…)`.
- **Generics.**
  - User generic functions are refused with EF127 (`semantic.go:2940-2941`).
  - User generic records and enums are admitted.
  - Recursive generic data is refused (EF127 "recursive or excessive generic data layout"; EF119).
  - A generic body that needs `T`'s field layout is refused (`field_occurrences.go:173-176`).
  - Spike-rows unit 4a (Q9 extended) plans to admit whole-value type parameters, including callable type arguments, where no layout is needed.
- **Receiver methods** are admitted but not implemented (`docs/design.md:33`, GLOSSARY "Receiver method"). They are declared in the type's owner module only, and design.md says "there are no methods on primitives or type parameters". Spike-rows unit 3 implements them.
- **SH1.** Effra generics lower to Go generics, with no frontend monomorphization (`docs/design.md:301-303`). Enum variant markers take no type arguments, so `Option[R1]` and `Option[R2]` share runtime stencils. `TestVariantMarkersShareShapes` (`shape_sharing_test.go`) holds the runtime package to 12 stenciled copies at 64 record types. A side effect matters here: because markers carry no type arguments, Go cannot infer a type parameter that appears only inside an enum, so generic calls need explicit type arguments (§2.4).
- **TypeScript projection.**
  - A record is branded with an unexported unique symbol, so TypeScript cannot forge one.
  - `i64` is `bigint`.
  - A generic record is `__ef_template_Name<T>`, made invariant by a `readonly [(value: T) => T]` brand.
  - Functions export as `__ef_function_X as X`.
  - Generic function parameters lose their names (`arg0`, …).
- **Existing stances this decision touches.**
  - `docs/specs/bundled-interfaces.md:39` keeps codecs as explicit witness values, because "one domain type can have several witnesses, such as an API codec and a storage codec".
  - PRIOR_ART's MoonBit row says "no implicit instance search".
  - `docs/design.md:300` avoids "implicit instance search" for rows.
  
  §2.2 explains why owner-module resolution is not instance search. §8 lists the text each unit amends.

## 2. Design of (b)

### 2.1 Syntax

A receiver method is declared beside its type, in the type's owner module:

```
record User {
    name: string;
    age: i64
}
fn (u: User) compare(other: User) -> i64 {
    u.age - other.age
}
```

A generic function states the methods it needs after its signature:

```
fn sort3<T: type>(xs: Three<T>) -> Three<T> where T.compare: fn(T, T) -> i64 {
    let ab = sort2(xs.a, xs.b)
    …
}
fn insert<K: type, V: type>(t: Table<K, V>, key: K, value: V) -> Table<K, V> where K.hash: fn(K) -> i64, K.equals: fn(K, K) -> bool {
    let h = K.hash(key)
    …
}
```

Inside the body, `T.compare(y, x)` calls the method. The clause states the method's full signature with the receiver as the first parameter, so the clause is the whole contract. Nothing is inferred from the body, and no named interface exists to look up.

The feature is described without naming any method: "a generic function may require that each type argument's owner module declares a method with a given name and signature; the checker supplies that method at each call." Its clients are equality, ordering and hashing, plus display/inspect, default and zero values (`T.zero`), generic arithmetic (`T.add`) and codecs' default witness (§9 Q4).

The spelling in this section, and the derive line in §2.7, is provisional until #135 decides the generic surface (§2.11). Resolution, lowering and projection do not depend on it.

### 2.2 Resolution

At a call, the checker binds the type parameters from the authored arguments, then resolves each `where` entry from the binding of its type parameter:

1. **Bound to the caller's own type parameter `U`.** The caller must declare the same entry for `U`; its method argument is passed on. If the caller does not declare it, the call is refused, with a hint to add the clause (constraint propagation).
2. **Bound to a nominal type `N`** (a record, an enum, an opaque type, or a predeclared type). The method is the receiver method `fn (x: N) m(…)` declared by `N`'s owner module. The owner of a user type is its declaring module. The owner of a predeclared type is a bundled module: `i64`, `string` and `bool` each have one. The signature must match the clause with `T = N`.
3. **Anything else** is refused: a function type, a row, `Effect<…>` without a method in `effra/effect`, or a handle without one in its owner. These have no owner module that declares the method.

**Production adds one case the prototype refuses: a generic application.** For example, `Option<User>` resolves `Option`'s conditional method `fn (o: Option<T>) compare(other: Option<T>) -> i64 where T.compare: …`, and that method's own clause is resolved for `T = User`. §2.9 specifies the uniqueness rule, the lowering (the generic body is emitted once and receives its method arguments, with no frontend monomorphization), the projection and termination.

**This is a lookup, not instance search.** For each type and method name there is at most one candidate, in one place: the type's owner module. There are no overlapping instances, no backtracking, no global instance database, no defaulting, and no search through superclasses. The rule is the receiver-method rule design.md already admits for `u.compare(v)` (`docs/design.md:33`). A `where` clause defers the same lookup from the definition to the call site, where `T` is known. Effra therefore keeps the stance that "no implicit instance search runs"; §8 unit W6 rewords the two passages that use the phrase.

**Coherence.** There is one method per type and name, declared only by the owner. A receiver method on a type the module does not own is refused; the prototype's `orphan-method.ef` is "receiver method compare on i64: methods are declared only by the module that owns i64". This is the method rule of Go, MoonBit and Roc.

**Coherence holds on the `where` path only.** Two `where`-resolved uses of `UserId.hash` always agree, because both name one declaration. It does not hold for the explicit `…By` form or for TypeScript callers, which may pass any function of the right type (§2.5). So two `HashMap<UserId, V>` values can carry different methods. §2.6 makes every collection capture its methods, so each collection agrees with itself, and Q6 settles how an operation over two collections behaves.

**What the owner rule gives up.** MoonBit pairs its owner-only method rule with traits that a third package can implement for a foreign type (`next/language/packages.md:203-205` at `8d9f3ba2`). Roc and Go have no such escape, and neither has (b). This has two consequences:
- A new `where` vocabulary, say `T.pretty`, reaches `i64`, `string`, `Option` or another module's type only through `…By`, or when that type's owner adds the method.
- A type's method names form one namespace. If two libraries want `hash` with different signatures, they conflict, and the owner's signature wins.

Both are accepted. They are the price of a single lookup site and of coherence on the `where` path, and `…By` is always available.

**A second ordering is an explicit function, not a newtype.** Library functions come in pairs: `sortBy(xs, compare: fn(T, T) -> i64)` is plain (a), and `sort(xs) where T.compare` calls `sortBy(xs, T.compare)`. Haskell's `Down` and Rust's `Reverse` wrappers are unnecessary.

### 2.3 Predeclared types

`docs/design.md:33` says "there are no methods on primitives or type parameters". (b) amends the first half: a predeclared type's methods are declared only by its bundled owner module, and user code cannot add them (the orphan rule). The second half stands: a type parameter has no methods of its own; `T.m` names a method argument the `where` clause asked for.

- `i64`: `compare`, `equals` and `hash`. The prototype ships these as `bundled/primitives/i64-{compare,equals,hash}.ef`.
- `string` and `bool`: the same three. `string.compare` is defined on Unicode code points. Go's byte order on UTF-8 already equals code-point order. JavaScript's UTF-16 code-unit order does not, so the JavaScript implementation compares code points and never uses `<` on strings.
- `f64` (#118): **no `compare`, no `equals`, no `hash`.** It offers `totalCompare` (IEEE 754 total order), `equalsBits` and a NaN-aware `ieeeEquals` under other names, so `where T.compare` never picks them. A caller writes `sortBy(xs, F64.totalCompare)`.

### 2.4 Lowering

Each `where` entry is a trailing hidden parameter. The parser elaborates it (`syntax.go:1367-1391` on the proto branch) and rewrites `T.m` in the body to that parameter (`syntax.go:1731`). The checker appends the resolved method to the call's arguments (`where_dispatch.go:36-98`), so the emitters, reachability and the `.d.mts` see an ordinary argument.

Go, emitted by the prototype for `sort-and-key.ef`:

```go
func efFunction_sort3[T any](efLocal_xs efTemplate_Three[T], efLocal_where_T_compare func(T, T) int64) efTemplate_Three[T] { … }
func efFunction_insert[K any, V any](efLocal_t efTemplate_Table[K, V], efLocal_key K, efLocal_value V,
	efLocal_where_K_hash func(K) int64, efLocal_where_K_equals func(K, K) bool) efTemplate_Table[K, V] { … }
func efFunction_User_compare(efLocal_u efType_User, efLocal_other efType_User) int64 { … }

efLocal_users := efFunction_sort3[efType_User](efTemp63, efFunction_User_compare)
efLocal_numbers := efFunction_sort3[int64](efTemp65, efBundledFunction_bundle_7ea6cd511a1bbe97) // i64's owner method
```

The call carries explicit type arguments (`emit_go.go:1377`, `where_dispatch.go:141-158`). Variant markers without type arguments (SH1) stop Go from inferring a parameter that appears only inside an enum (`Table<K, V>`'s slots), and the Effra checker already knows every binding. Spike-rows' explicit type arguments (F1, unit 3) need the same emission.

JavaScript: `__ef_function_sort3(xs, __ef_function_User_compare)`. For `i64`, the call passes the bundled function.

The method argument is a direct function value in both targets. Go calls it through a `func` value, as `slices.SortFunc` does, and the GC-shape dictionary is unchanged. The argument adds no stencil (§4.2).

### 2.5 TypeScript projection

The generated declaration is the lowered signature. The prototype emits this (`benchmarks/where/ts/runs/proto-dmts.txt`):

```ts
declare const __ef_function_sort3: <T>(arg0: __ef_template_Three<T>, arg1: (arg0: T, arg1: T) => bigint) => __ef_template_Three<T>;
declare const __ef_function_insert: <K, V>(arg0: __ef_template_Table<K, V>, arg1: K, arg2: V, arg3: (arg0: K) => bigint, arg4: (arg0: K, arg1: K) => boolean) => __ef_template_Table<K, V>;
declare const __ef_function_User_compare: (arg_u: User, arg_other: User) => bigint;
```

Production makes two changes, which the hand-written projection in `benchmarks/where/ts/b-witness/lib.d.mts` shows:

- **Each method parameter is named after its method**: `compare`, `hash`, `equals`.
- **A type's owner-module methods are exported as a companion object under the type's name**: a value merged with the interface, `export declare const User: { readonly compare: (self: User, other: User) => bigint; … }`. The TypeScript caller writes `User.compare`, which is Effra's `User.compare` spelling of the receiver method (`docs/design.md:33`).

```ts
export declare const sort3: <T>(xs: Three<T>, compare: (self: T, other: T) => bigint) => Three<T>;
export declare const empty: <K, V>(hash: (self: K) => bigint, equals: (self: K, other: K) => boolean) => HashMap<K, V>;
export declare const insert: <K, V>(map: HashMap<K, V>, key: K, value: V) => HashMap<K, V>;
```

```ts
const users = sort3(three(User.make("c", 30n), User.make("a", 10n), User.make("b", 20n)), User.compare);
let byId: HashMap<UserId, string> = empty(UserId.hash, UserId.equals);
byId = insert(byId, UserId.make("acme", 7n), "ada");
```

**The declarations claim nothing about `T`.** A `where` clause does not narrow `T` at the type level, so there is nothing that could fail to widen. A TypeScript caller supplies the method explicitly. That caller may pass any function of the right type, including a hash for a function type, and owns that choice, as every Effect `Order` user does. This is the trust envelope (spike-rows m2): Effra's compile-time refusals hold inside Effra; at the TypeScript boundary the caller writes the argument Effra would have resolved.

### 2.6 Collections capture their methods

**Every collection captures its methods at construction.** This holds whether or not the collection is exported, because a collection cannot know where its values will travel. The constructor takes the methods (`empty<K, V>() where K.hash, K.equals`, or `emptyBy(hash, equals)`), the value stores them, and later operations carry no `where` clause. As a result, one collection always agrees with itself, even when its methods came from `…By` or from a TypeScript caller, and Effra code pays the resolution once per collection.

The prototype instead passes the methods at each call (`insert`/`lookup … where K.hash, K.equals`). That is coherent on the `where` path, but a TypeScript caller could mix two hashes in one map and get missed lookups (never a crash). Prior art:
- ReScript's `Belt.Map` stores the comparator in the map value (`Belt_Map.res:24-25`).
- OCaml's `Map.Make` captures `Ord.compare` in the functor's module, not in the value; the map value is `Empty | Node …` (`stdlib/map.ml:75-79` at `6471927d`).
- Rust and MoonBit re-resolve a globally unique implementation at each method.
- Effect 4.0.1's `HashMap` uses the global `Equal`/`Hash` protocol (§5).

**Capturing protects one collection, not a pair.** Two maps can still disagree: one built through the `where` path and one through `emptyBy` or TypeScript. An operation over both (union, merge, equality) must not reuse the other map's buckets under the receiver's methods. Q6 records three candidate rules and the decision:
1. **Reinsert** the incoming entries under the receiver's methods. Chosen.
2. **Put witness identity in the type**, as ReScript does. Its documentation says "two sets would type mismatch if they use different comparison function" (`Belt_Id.resi:41-53` at `30ce3698`).
3. **Check witness identity at run time.**

### 2.7 derive under (b) (#100)

derive targets receiver methods, which are ordinary declarations. One structural rule covers equality, ordering and hashing without naming any of them:

> A derived method `m` on a record applies `m` to each field, in declaration order, through the field type's owner, and combines the results with a combinator the deriving module names. On an enum, it compares variant order first, then the fields.

A sketch:

```
derive User: compare by Order.lexicographic, equals by Bool.all, hash by Hash.mix
```

Each entry produces an ordinary `fn (u: User) compare(other: User) -> i64` that the `.d.mts` exports on the companion object. A generic record derives a conditional method (`fn (p: Pair<A, B>) compare(…) where A.compare, B.compare`, §2.9). Field types without the method are refused with the EF160 message (§3.8). Display and inspect are further clients. JSON stays a closed profile word as #100 plans, because a codec carries wire policy (field names, bounds, transformations) and one type can have several (`bundled-interfaces.md:39`). Under (b), no `eq`, `order` or `hash` profile word is ever added.

### 2.8 Diagnostics

The prototype gives EF160 (`where_dispatch.go:57-86`) on both targets:

- missing: "Item has no method compare: sort2 requires T.compare: fn(T, T) -> i64; declare `fn (value: Item) compare(...)` beside Item, or derive it";
- no owner: "File has no method hash: keyed requires K.hash: fn(K) -> i64; File has no owner module that can declare methods". The same message is given for `Effect<i64>`;
- unpropagated: "T has no method compare: sort2 requires T.compare: fn(T, T) -> i64; add `where T.compare: ...` to the calling function's signature";
- wrong signature: "method Item.compare is fn(Item, Item) -> bool, but sort2 requires T.compare: fn(T, T) -> i64 with T = Item";
- orphan: "receiver method compare on i64: methods are declared only by the module that owns i64".

Production rewords the no-owner hint for a predeclared type whose owner exists but declares no such method (f64: "f64 declares no compare; use sortBy with F64.totalCompare"). It also anchors the diagnostic on the argument that bound `T`, not on the whole call. Review 1 found three prototype message defects, which become W1 acceptance cases (§4.1).

### 2.9 Conditional methods: `Option<User>` and generic records

A **conditional method** is a receiver method on a generic type whose own `where` clause constrains that type's parameters. A derived method on a generic record is one too (§2.7).

```
fn (o: Option<T>) compare(other: Option<T>) -> i64 where T.compare: fn(T, T) -> i64 { … }
fn (p: Pair<A, B>) compare(other: Pair<A, B>) -> i64 where A.compare: fn(A, A) -> i64, B.compare: fn(B, B) -> i64 { … }
```

**Uniqueness.** Methods are keyed by (nominal type constructor identity, method name), and each key has at most one declaration, in the constructor's owner module.
- The receiver is the constructor applied to its own declared parameters, each a distinct parameter, as in `fn (p: Pair<A, B>)`.
- A receiver on one instantiation, such as `fn (o: Option<i64>) compare`, is refused. The one declaration covers every instantiation.
- The `where` clause states what that declaration needs. A type argument without the method refuses the use with EF160 and never falls back to another candidate.
- Conditional bounds therefore never create alternatives, no specialised declaration can overlap the generic one, and resolution needs no backtracking.
- The prototype refuses the generic receiver declaration as a false orphan (review probe p4: `checkReceiverOwner` compares the string `Pair<A, B>`). W1 compares constructor identity instead.

**Resolution and termination.** A conditional method's `where` entries may constrain only the receiver's type parameters. They may not constrain `Option<T>` itself or an unrelated type. Resolving `C<A1, …, An>.m` therefore creates obligations only on `A1 … An`, which are strict subterms of the type being resolved.
- Type terms are finite, so the recursion ends. Its depth is at most the depth of the type term: `Option<Option<User>>` resolves `Option.compare`, then `Option.compare`, then `User.compare`.
- A method body that calls the same method on the same type uses its own method argument, so it starts no resolution.
- *Polymorphic recursion* is a generic function, or a conditional method body, that calls itself, directly or through a mutually recursive group, at a type that grows: `depth<T>` calling `depth<Box<T>>`, or `C<T>.m` needing `C<Option<T>>.m`. It needs no recursive data. Each resolution site still terminates, but the instantiations do not.
  - Review 2's probe `review-2/probes/q1-polyrec-fn.ef` (a generic `depth` over a non-recursive `Box`) gets `checked=true` from the prototype on both targets.
  - It then fails Go's build with "instantiation cycle: T instantiated as efTemplate_Box[T]" (`src/cmd/compile/internal/types2/mono.go:142`, go1.27.0), while JavaScript prints `3`. That is an unchecked passthrough.
  - So the refusal belongs wherever user generic functions are admitted (spike-rows unit 4a and W1). It covers conditional method bodies and mutually recursive growth, and it is refused with a diagnostic, as Go's checker refuses the cycle.
- The shared substitution budget (4096 nodes, depth 64, `bundled-interfaces.md`) remains the backstop.

**Lowering: one generic body, with method arguments.** A conditional method has two entry points.
- **The body** is an ordinary generic function whose method arguments trail, exactly as for any `where` function (§2.4). A direct call `o.compare(p)` on a known `Option<User>` calls the body.
- **The curried form** takes the method arguments first and returns the method. A resolved method argument must have exactly the signature the `where` slot asks for (`fn(Option<User>, Option<User>) -> i64`), and only the curried form produces such a value without code generation per type.

```go
func efMethod_Option_compare[T any](o, other efTemplate_Option[T], T_compare func(T, T) int64) int64 { … } // body, stenciled per GC shape as usual
func efMethodC_Option_compare[T any](T_compare func(T, T) int64) func(efTemplate_Option[T], efTemplate_Option[T]) int64 {
	return func(o, other efTemplate_Option[T]) int64 { return efMethod_Option_compare[T](o, other, T_compare) }
}
var efAdapter_1 = efMethodC_Option_compare[efType_User](efFunction_User_compare) // static arguments: one package-level value
efFunction_sort3[efTemplate_Option[efType_User]](xs, efAdapter_1)
// inside a generic body with `where T.compare`: efFunction_sort3[efTemplate_Option[T]](xs, efMethodC_Option_compare[T](efLocal_where_T_compare))
```

JavaScript takes the same two entry points. The companion's `Option.compare` is the curried form: `sort3(xs, Option.compare(User.compare))`.

**SH1.** This is witness passing, not frontend monomorphization.
- Each conditional method's body and curried form are emitted once, and Go stencils them per GC shape as it does every generic function (`docs/design.md:301-303`).
- An adapter is a closure value, not a copy of code. When every method argument is static, the emitter hoists it to one package-level value per distinct (method, argument tuple) in a module.
- Inside a generic body, the adapter is built per call of the enclosing function, from the method arguments it received: one small closure allocation per level of nesting.
- The code size added per conditional method is constant. The data added is bounded by the number of distinct static adapters.
- GHC builds `Ord (Maybe a)` dictionaries from `Ord a` the same way. Rust, Roc and MoonBit monomorphize instead (§5).

**Projection.** The companion carries the curried form. Method arguments come first, one per `where` entry in declaration order, each named `<Param>_<method>`:

```ts
export declare const Option: {
  readonly compare: <T>(T_compare: (self: T, other: T) => bigint) => (self: Option<T>, other: Option<T>) => bigint;
  readonly hash: <T>(T_hash: (self: T) => bigint) => (self: Option<T>) => bigint;
  readonly equals: <T>(T_equals: (self: T, other: T) => boolean) => (self: Option<T>, other: Option<T>) => boolean;
};
export declare const Pair: {
  readonly compare: <A, B>(A_compare: (self: A, other: A) => bigint, B_compare: (self: B, other: B) => bigint) => (self: Pair<A, B>, other: Pair<A, B>) => bigint;
};
// caller: sort3(xs, Option.compare(User.compare)); sort3(nested, Option.compare(Option.compare(User.compare)));
//         sort3(pairs, Pair.compare(I64.compare, User.compare)); empty(Option.hash(UserId.hash), Option.equals(UserId.equals))
```

`benchmarks/where/ts/b-cond/` (adapted from review 1's probe) checks this with the repository's tsc 6.0.3 strict.
- Exit codes: 0 for both files.
- 0 `any` keywords.
- Output: `none,a,c none,none,b y,z,x ada,nobody`.
- 8 of 8 directives fire:
  - the inner method of another type;
  - the outer method without its argument;
  - the wrong element type;
  - one nesting level too few;
  - Pair's arguments swapped;
  - a compare where a hash is needed;
  - A1 bivariance;
  - A2 augmentation.
- A3 and A4 throw `T_compare is not a function` at first use (`ts/runs/ts-view.txt`).

Nothing is narrowed, so the projection stays fail-safe.

### 2.10 Laws, contracts and the requirements on evidence (#125)

The owner made laws a construct on 2026-10-12 (#125), and #116 is its second client. `docs/design.md:42` already admits a visible `law`/`contract` construct, and GLOSSARY defines a law as "a named obligation declared in a service or contract". (b) removes the dictionary type that #125's acceptance names as one attachment site. So this section says where laws attach instead. **The syntax is reserved to #125.** Every snippet here is a sketch. Evidence is #125's mechanism too; this section only lists what any evidence mechanism must meet.

**(i) On the receiver method.** `fn (a: User) compare(b: User) -> i64 law transitive …`. This needs no new grouping, but it has two gaps:
- A law that relates two methods (compare agrees with equals; equal values hash alike) has no single home.
- A bare `where T.compare` accepts any method of that name and signature, lawful or not, so the requiring side cannot ask for the law.

**(ii) On a named contract that groups `where` entries with their laws**, as Roc abilities group methods:

```
// sketch; syntax reserved to #125
contract Ordering<T> {
    T.compare: fn(T, T) -> i64
    T.equals: fn(T, T) -> bool
    law transitive(a: T, b: T, c: T) { … }
    law agrees(a: T, b: T) { … }
}
User satisfies Ordering              // in User's owner module; the law suite discharges it there
fn sort<T: type>(xs: Seq<T>) -> SortedBy<Seq<T>, T.compare> where T: Ordering
```

`where T: Ordering` expands to its method entries, so resolution, lowering (one method argument per entry, no dictionary record) and the `.d.mts` stay as in §2.2-§2.5. The contract adds two things: the laws, and an owner-module declaration that the type satisfies it. That declaration is where the law suite runs, and only the owner can make it, so coherence carries over.

**Recommendation: (ii).**
- Laws that relate several methods need a home.
- The requiring side can ask for lawfulness.
- It reuses design.md's existing `contract` word.

On the ladder, `where` stays rung 2. Contracts and laws are #125's construct, and their checking is compiler-special (`docs/design.md:42`). A bare `where T.compare` stays available. It requires a method, not a law.

**Evidence is #125's.** The mechanism that mints, names, carries and checks evidence belongs to #125 and the opaque-values design (`docs/research/opaque-values-and-evidence.md`). This note hands #125 three facts from dispatch and four requirements. Review 3 showed why the last three requirements are needed: it found attacks that pass TypeScript strict with no cast. Those attacks are recorded in the fixture as expected failures, not as acceptance.

**What dispatch gives #125.**
- **A name for the method.** On the `where` path, `T.compare` for `T = User` resolves to the declaration `User.compare`. That is a declaration identity, so evidence can name it (`SortedBy<Seq<User>, User.compare>`). Two sequences sorted on that path share the evidence type, and `merge(a: SortedBy<S, T.compare>, b: SortedBy<S, T.compare>)` type-checks without the rank-2 naming that GDP uses for a comparator value (`docs/research/opaque-values-and-evidence.md`, the named-comparator case).
- **A name for a conditional method.** The name in `SortedBy<S, By>` is the canonical resolved key: the outer declaration plus the keys of its method arguments. For example, `Option.compare(User.compare)` is the key (`Option.compare`, [`User.compare`]). Computing it is a finite identity calculation over the resolution tree of §2.9, not a type-level language, and it is the same key Q6's fast path would use. Conditional contract satisfaction (`Option<T>` satisfies `Ordering` when `T` does) is a #125 acceptance case.
- **Who may mint, as recommended.**
  - Only a function whose clause is satisfied through a contract (`where T: Ordering`) may mint lawful-order evidence.
  - A bare `where T.compare` mints none.
  - A `…By` closure, or a method a TypeScript caller passes, has no declaration identity. Evidence about it must name the actual comparator value through GDP's fresh-name mechanism, so until #125 and the opaque-values design provide that, neither mints.

**Requirements any evidence mechanism must meet.** Each extends the fail-safe projection principle to evidence. The opaque-values design states the general form: "Exported host APIs need their own checked boundary; target-language privacy alone does not establish this contract", and "Payloads used to justify evidence are immutable or snapshot/version-bound" (`docs/research/opaque-values-and-evidence.md:38-39`).

1. **Outward: the evidence boundary rule** (binding since round 2). An exported function that mints evidence takes one of two forms:
   - **(i) Canonical binding.** It binds the canonical methods on the Effra side. Its TypeScript signature has no method parameter, so the caller cannot choose the comparator, and the evidence it returns crosses outward. This form exists only where the type is known on the Effra side: a concrete instantiation such as `sortUsers(xs: Seq<User>)`, or a re-establishment function that checks with the canonical method and then mints.
   - **(ii) Dropped evidence.** It accepts its methods from the caller. That covers any exported function whose `where` entries become parameters, which is every exported generic one. Its `.d.mts` returns the carried type without the brand. Before Effra trusts such a value again, it re-establishes the evidence through a form (i) function.

   Evidence minted from methods a caller supplied never crosses outward as evidence. Generic forwarding follows the same rule: a generic Effra function whose method argument may come from a TypeScript caller mints nothing that reaches TypeScript, even when it forwards to a minting function inside Effra. Without this rule, a TypeScript caller passes a reversed compare to an exported generic `sort … where T: Ordering`, receives a genuine Effra-built `SortedBy`, and hands it to a trusting Effra `merge` (review 2, N1). Inside Effra, every `where` argument is resolved, so evidence flows freely.
2. **Inward: evidence that originates in the host is never trusted.** A value that arrives from the host is not evidence, whatever its TypeScript type says. That includes evidence inside containers, evidence passed to callbacks, and evidence in asynchronous results. An exported function with an evidence parameter takes one of two shapes:
   - it projects the unbranded carried type and re-establishes the evidence on entry through a form (i) check;
   - it stays private.

   The brand cannot be the inward check, because TypeScript's documented unsoundness delivers non-evidence to a branded parameter without forging the brand:
   - a spread keeps the unique-symbol key while it replaces a field (R1);
   - arrays (R2) and mutable properties (X2) are covariant;
   - method parameters are bivariant even under `strictFunctionTypes` (R3).
3. **Stability: evidence covers a deep, Effra-owned snapshot.** A minting function over host-originated data first copies or deep-validates it into an Effra-owned immutable value, then checks. Every field the evidence depends on is part of that snapshot. None of these is enough:
   - freezing only the outer object, because A2 mutates an element that was never frozen;
   - checking and returning the caller's own object, because A1 mutates it after the check;
   - checking an object with accessors, or freezing it after the check, because an accessor can return one value to the check and another to the body (A3).

   Requirement 2's entry re-check runs over requirement 3's snapshot. A re-check over the caller's own object satisfies requirement 2 in form and still fails (A3).
4. **Canonical calls use private bindings, and companion objects are frozen.** Effra-side code calls a canonical method through a module-private binding. It never looks the method up on an exported object, and the exported companion (§2.5) is frozen. Otherwise `Object.assign(User, { compare: reversed })`, which type-checks because `Object.assign` ignores `readonly`, changes what form (i) binds (R10). The prototype's emitted JavaScript already binds methods as module-private `const` functions and exports them by name: `const __ef_function_User_compare = …` and `export { __ef_function_User_compare as User_compare }` in the `out/sample.mjs` that `ts/proto/check.sh` regenerates. W3's companion keeps that.

**Input evidence for #125: `benchmarks/where/ts/b-evidence/`.** `ts/run.sh` checks it with the repository's tsc 6.0.3 strict. The fixture models requirement 1, plus one export in requirement 2's shape (`smallestChecked`: an unbranded parameter, re-checked on entry, with no snapshot first).
- **Requirement 1.** Form (i) `sortUsers` takes no method argument, and `checkSortedUsers` returns `SortedBy<…> | undefined`. Form (ii) `sort` and its generic forwarder `sortTwice` return the unbranded `Three<T>`. All eight directives fire with the codes they declare:
  - laundering through `sort` and through `sortTwice` (TS2345, in both the consumer and the attacks);
  - a method passed to form (i), and a never filler into form (i) (TS2554);
  - an unchecked value, and a re-establishment result used unchecked (TS2345);
  - a literal that lacks the brand (E6, TS2345). This shows only that such a literal does not acquire the brand; R1 shows that a literal spreading genuine evidence keeps it.

  The consumer prints `form(i) smallest=a form(ii) order=cba recheck sorted=a recheck reversed=refused`.
- **The rejected projection.** `rejected.d.mts` is the projection the round-2 sketch implied: a generic minting function with a method parameter. The same laundering route type-checks there with no cast, and the trusting consumer returns `c`, the largest user, as the smallest (`L2` in `ts/runs/ts-view.txt`).
- **Expected failures** (`attacks-expected-fail.mts`), recorded as #125 input and not as acceptance. Each case type-checks with no cast and no `any`, and `smallest` (or, for A3, `smallestChecked`) then returns a user that is not the smallest under `User.compare`. The run fails if a case stops reproducing its recorded output, so the record cannot go stale silently. The receipt line is `expected-fail: 9 reproduced, 0 not reproduced`. A1 and A2, as recorded, hand the mutated value back to the trusting `smallest`, so an entry re-check (requirement 2) refuses them as well as a snapshot does. A3 is the case that needs requirement 3 alone.

  | Case | Requirement it needs | Route | Recorded output |
  | --- | --- | --- | --- |
  | R1 | 2, inward | `smallest({ ...sorted, a: users.a })` | `smallest=c true-min=b` |
  | R2 | 2, inward | `const view: Three<User>[] = ev; view[0] = sort(users, reversed)`, then `smallest(ev[0])` | `smallest=c true-min=a` |
  | R3 | 2, inward | `const p: Picker = { pick: smallest }; p.pick(sort(users, reversed))` | `smallest=c true-min=a` |
  | X2 | 2, inward | `const wide: { v: Three<User> } = box; wide.v = sort(users, reversed)`, then `smallest(box.v)` | `smallest=c true-min=a` |
  | A1 | 2 or 3 (as recorded) | `const mutable = { ...sorted }; const ev = checkSortedUsers(mutable); mutable.a = User.make("c", 30n)` | `smallest=c true-min=b` |
  | A2 | 2 or 3 (as recorded) | `const a = { ...User.make("a", 10n) }`, sorted by form (i), then `a.age = 100n` | `smallest=a true-min=b` |
  | A3 | 3, stability | `smallestChecked({ ...three(a, b, c), get a() { return reads++ < 1 ? a : c; } })`: the entry re-check reads `a`, the body reads `c` | `smallest=c true-min=b` |
  | R10 | 4, canonical method | `Object.assign(User, { compare: reversed })`, then form (i), and separately re-establishment of a reversed triple | `smallest=c true-min=a`, both |

  #125's acceptance should turn each row into a refusal or a fail-safe result.

The same naming would let a future `HashMapBy<K, V, name>` carry witness identity in its type (Q6), so that question also waits on #125.

### 2.11 Surface syntax is provisional (#135)

The owner said on 2026-10-12 that Effra's generic signatures read more like TypeScript than Go. Spike #135 decides the generic surface against a rewritten corpus. Until it does, this note's spelling is provisional: `where T.compare: fn(T, T) -> i64` after the signature, the `<T: type>` kind annotation, and the derive line `derive User: compare by Order.lexicographic, …` (§2.7). This section does not choose among the candidates.

| Spelling | Constrained sort |
| --- | --- |
| This note (provisional) | `fn sort<T: type>(xs: Seq<T>) -> Seq<T> where T.compare: fn(T, T) -> i64` |
| Go | `func Sort[T interface{ Compare(T) int }](xs []T)` |
| A named-constraint alternative | `fn sort[T Ordering](xs: Seq<T>) -> Seq<T>`, where `Ordering` is the named contract of §2.10 |

- **Go's spelling** puts the constraint in the type-parameter list and writes the method with an implicit receiver. Go's method sets are also declared only in the type's package, so the owner rule is the same. The difference is that Go's predeclared types have no methods, so Go reaches `int` through the `cmp.Ordered` type set; under (b), a bundled owner module declares the methods (§2.3).
- **The named-constraint alternative** reads like Go's `[T cmp.Ordered]`. It needs the named contract that §2.10 recommends to #125, and it expands to the same `where` entries.
- **What #135 cannot change.** Whatever the spelling, these stand: a constraint names a method and its signature; resolution goes through the owner module (§2.2); each entry lowers to a trailing method argument (§2.4); and the `.d.mts` has one function-typed parameter per entry (§2.5). #135 decides how users write it, not what the checker checks.

## 3. Comparison of (a), (b) and (c)

| Dimension | (a) explicit dictionaries + derive | (b) `where`, owner-module resolution | (c) structural `comparable` / `Ordered` |
| --- | --- | --- | --- |
| TypeScript caller writes | `sort3(xs, userOrder)`, `empty(userIdHash)` | `sort3(xs, User.compare)`, `empty(UserId.hash, UserId.equals)` | `sort3(numbers)`; records: `sort3Func(xs, cmp)`; `empty<UserId, string>()` |
| `.d.mts` shape | dictionary-typed parameter | function-typed parameter (§2.5) | `T extends Ordered`; `Key<K>` recursive conditional type |
| `any` in `.d.mts` / `.mjs` | 0 / 0 | 0 / 0 | 0 / 0 |
| Fail-safe under the four routes (§3.2) | yes: nothing narrowed | yes: nothing narrowed | fails closed at the type level; A3/A4 reach a run-time encoder, which must refuse |
| Generic TypeScript caller | works | works | refused (TS2589, "excessively deep") even when it restates `Key<K>` |
| Go emission (§3.3) | `func sort3[T any](xs, order Order[T])` | `func sort3[T any](xs, compare func(T, T) int64)` | `func sort3[T cmp.Ordered](xs)`, `map[K]V` |
| Hard keys on the default path (§3.4) | bare File and effect keys accepted wherever someone writes the functions; a bare fn key waits on 4a | bare fn (after 4a), File and effect keys refused on the `where` path; f64 by library policy | fn keys refused; handle and effect keys only if the predicate names them; NaN admitted |
| Wrappers (a record holding a File or a function) | accepted with explicit functions | accepted with owner methods | Go: a function field is refused; a handle field compares by identity |
| #100 (§3.5) | a general fold needs one dictionary per field type, or a derive-internal lookup by type | the fold uses the general lookup; no `eq`/`order`/`hash` profile words | equality is built in; ordering falls back to (a) |
| Ladder (§6) | rung 1 | rung 2 | rung 4 |
| Checker complexity (§3.7) | none added | about 320 prototype lines; a lookup per method argument; conditional methods in W1 | structural predicate over every type; a run-time encoder for JavaScript |
| Missing-instance message (§3.8) | "incorrect argument count" | "Item has no method compare: sort2 requires …" | Go: "Item does not satisfy cmp.Ordered (…)" |

### 3.1 TypeScript projection, with tsc evidence

Every option has a projection, a runtime, a consumer, negatives and attacks under `benchmarks/where/ts/<option>/`, and `benchmarks/where/ts/run.sh` runs all of them. Each file is type-checked on its own with the repository's pinned TypeScript, `conformance/framework-ports/ports/node_modules/typescript` (6.0.3, which the gate asserts). `tools/lib.sh` resolves that copy relative to the script and checks its version. The flags are `--strict --module NodeNext --moduleResolution NodeNext --target ES2022 --lib ES2022,DOM,ESNext.Disposable`, and node 24.11.1 runs the consumer and the attacks.
- `tools/open-negatives.cjs` (adapted from spike-rows probes r4) opens every `@ts-expect-error` and prints the diagnostic it suppresses.
- `tools/count-any.cjs` counts AST `any` keywords.
- Every directive declares the TypeScript codes it expects (`// @ts-expect-error TS2345 E2 …`), and `open-negatives.cjs` requires the opened statement's distinct codes to equal that set. A directive therefore cannot pass by firing with a different code set. It can still fire for a weaker reason under the same code: E6's TS2345 once carried a stronger comment than it tested (§2.10).
- The scripts exit 1 when any tsc or node run fails, a directive suppresses nothing or fires with other codes than it declares, or an `any` keyword appears. Two fault controls show this. A type error plus a silent directive gives `== result: 2 failed` and exit 1. A wrong declared code, a missing code and a stale expected-fail record also give exit 1.
- The receipt is `benchmarks/where/ts/runs/ts-view.txt`.

| Option | tsc exit, per file | Directives (each fires) | `any` keywords in `.d.mts`, `.mjs`, consumer | Consumer output |
| --- | --- | --- | --- | --- |
| a-dict | 0, 0, 0 | 7 negatives + 2 attacks | 0 | `abc 123 ada2,bob,none cba` |
| b-witness | 0, 0, 0 | 8 + 2 | 0 | `abc 123 ada2,bob,none abc` |
| b-struct | 0, 0, 0 | 5 | 0 | `abc ada2,bob,none` |
| b-cond (conditional methods, §2.9) | 0, 0 | 7 + 1 | 0 | `none,a,c none,none,b y,z,x ada,nobody` |
| b-evidence (#125 input evidence, §2.10) | 0, 0, 0 | 6 + 2, plus 9 expected-fail cases reproduced | 0 | `form(i) smallest=a form(ii) order=cba recheck sorted=a recheck reversed=refused` |
| c-comparable | 0, 0, 0, 0 | 7 + 2 + 3 | 0 | `123 abc abc ada2,bob,none yes native Map record key: miss` |
| proto (generated by `30f80104`) | 0, 0 | 3 | 0 | `abc ada2,bob` |
| baseline-effect (Effect 4.0.1) | 0 | 0 | 0 | `abc plain:ada Data.Class:ada fn key:f NaN:NaN,1,2 native Map A1b: UserId.org is undefined` |

The exact declarations a TypeScript caller sees for the sort and the record-keyed map, by option:

```ts
// (a) benchmarks/where/ts/a-dict/lib.d.mts
export type Order<T> = { readonly [__ef_brand_template_Order]: readonly [(value: T) => T]; readonly compare: (self: T, other: T) => bigint; };
export type Hash<T> = { readonly [__ef_brand_template_Hash]: …; readonly hash: (self: T) => bigint; readonly equals: (self: T, other: T) => boolean; };
export declare const sort3: <T>(xs: Three<T>, order: Order<T>) => Three<T>;
export declare const empty: <K, V>(hash: Hash<K>) => HashMap<K, V>;
// caller: sort3(users, userOrder); empty(userIdHash); reverse(mapInput(stringOrder, (u: User) => u.name))

// (b) benchmarks/where/ts/b-witness/lib.d.mts (§2.5)
export declare const sort3: <T>(xs: Three<T>, compare: (self: T, other: T) => bigint) => Three<T>;
export declare const empty: <K, V>(hash: (self: K) => bigint, equals: (self: K, other: K) => boolean) => HashMap<K, V>;
// caller: sort3(users, User.compare); empty(UserId.hash, UserId.equals)

// (b), structural projection, rejected: benchmarks/where/ts/b-struct/lib.d.mts
export interface Comparable<T> { compare(other: T): bigint; }
export declare const sort3: <T extends Comparable<T>>(xs: Three<T>) => Three<T>;
export declare const empty: <K extends Hashable<K>, V>() => HashMap<K, V>;
// caller: sort3(users); empty()

// (c) benchmarks/where/ts/c-comparable/lib.d.mts
export type IsComparable<K> = K extends Primitive ? true : K extends (...args: never[]) => unknown ? false : K extends File ? false
  : K extends object ? (false extends { [P in keyof K]-?: IsComparable<K[P]> }[keyof K] ? false : true) : false;
export type Key<K> = [IsComparable<K>] extends [true] ? K : never;
export declare const sort3: <T extends Ordered>(xs: Three<T>) => Three<T>;
export declare const empty: <K, V>() => HashMap<Key<K>, V>;
export declare const insert: <K, V>(map: HashMap<K, V>, key: Key<K>, value: V) => HashMap<K, V>;
// caller: sort3(numbers); sort3Func(users, cmp); empty<UserId, string>()
```

Notes from the runs:

- **(a) and (b) are the same shape.** (a) wraps the function in a branded record; (b) passes the function. Both refuse a missing argument (TS2554), another type's method (TS2345), a wrong return type, and a mixed element type. N4: `Three<User | UserId>` with `User.compare` is refused because the template brand makes `Three<T>` invariant.
- **TypeScript's arity rule admits a shorter function** (b-witness A5): `UserId.hash` type-checks as a `compare` argument and returns a meaningless order. The result type is honest (`Three<UserId>`), and Effra never makes that choice because it resolves by name.
- **`K extends Comparable<K>` is refused as circular** (TS2313) when `Comparable` is a conditional type. (c) has to apply the check where `K` is used (`Key<K>`), which forces explicit type arguments on `empty<UserId, string>()`, because TypeScript cannot infer through the conditional, and makes generic callers fail (N6, N7: TS2589).
- **Effect 4.0.1 is the JavaScript runtime's own baseline.** `Order` values for ordering; `Equal.equals` is structural on plain objects (so `plain:ada` hits), defers to `Equal.symbol`, and compares functions by reference (`fn key:f`). Nothing is refused at compile time. `Order.Number` orders NaN before every number, a total order the library chose (`NaN,1,2`; `Order.ts:177-180` at `460272d3`).

### 3.2 The four attack routes

Each attack type-checks with no cast and no `any` unless a directive marks it refused; node then runs it and the run shows the outcome. A "throws" outcome raised before the attack's result could be used as a value of its static type. The exception is A4's map variant: the map is returned, and it throws at its first insert or lookup. A "returns" outcome is listed with whether its static type is true.

| Route | (a) | (b) witness | (b) structural | (c) |
| --- | --- | --- | --- | --- |
| A1 method bivariance, witness or constraint slot | refused (invariant templates) | refused (invariant `Three<T>`) | **passes**, throws `y.compare is not a function` | refused for the factory and the sort slot |
| A1b key bivariance (`insert<UserId, string>` into a `UserId \| User` key slot) | passes, returns a key typed `UserId` with `org` undefined | same | constraint bivariance passes, throws | passes, the encoder throws `hash of unhashable type function` |
| A2 `interface Object` augmentation | refused (no argument supplied) | refused | **passes** for a function, a `bigint` and a function-keyed map; throws | **over-refuses**: valid record keys become `never` (fails closed) |
| A3 `never[]` rest | passes, throws `Cannot read properties of undefined` | passes, throws `compare is not a function` | passes, throws | passes, the encoder throws |
| A4 never filler | passes, throws | passes, throws | passes, throws | passes, the encoder throws |

- **A1b is not a `where` question.** It is TypeScript's method-parameter bivariance on a container's element parameter, and `baseline-effect` shows the same on a native `Map` (`native Map A1b: UserId.org is undefined`). Every option has it, and it involves no constraint.
- **(b) witness and (a)** never claim a narrowed type, so a bypass can only drop or falsify the argument itself, and the call then fails on first use. A production runtime may check `typeof compare === "function"` at the boundary and raise a typed defect instead of a `TypeError`. This costs one comparison per exported call (§9 Q3). It detects a missing function, not a wrong or unlawful one.
- **(b) structural lets the method's presence be bypassed.** A1 and A2 pass tsc with no lying value, then throw before returning. The declared result is unchanged, so they show no false result of the spike-rows M1 kind. The projection is rejected on the grounds stated in the verdict: a primitive cannot carry the method (b-struct's consumer sorts no `bigint`), methods on values need classes on JavaScript, and owner lookup is not structural.
- **(c) fails closed at the type level, but its correctness rests on a run-time check.** Every route that gets a value through (A1b, A3, A4) reaches the key encoder, which must refuse functions and handles at run time. Go has the same split: `map[Key]string` with an interface key holding a function compiles, then panics `hash of unhashable type main.CallbackKey` (`benchmarks/where/runs/go-comparable.txt`).

### 3.3 Go emission, side by side

(a) and (b) are measured from prototype output (§4). (c) is the hand-written control in `benchmarks/where/go/testdata/comparable/main.go`, because Effra has no such constraint; it is run with `go run .` (go1.27.0).

```go
// (a) explicit dictionary (style a of the benchmark passes the function directly, as Effect's Order is a function)
func efFunction_sort3[T any](efLocal_xs efTemplate_Three[T], efLocal_compare func(T, T) int64) efTemplate_Three[T]
efFunction_sort3[efType_R1](xs, efFunction_R1_compare)

// (b) where clause, prototype output
func efFunction_sort3[T any](efLocal_xs efTemplate_Three[T], efLocal_where_T_compare func(T, T) int64) efTemplate_Three[T]
efFunction_sort3[efType_User](xs, efFunction_User_compare)
func efFunction_insert[K any, V any](t efTemplate_Table[K, V], key K, value V, where_K_hash func(K) int64, where_K_equals func(K, K) bool) efTemplate_Table[K, V]

// (c) structural
func sort3[T cmp.Ordered](xs Three[T]) Three[T]           // records: sort3Func(xs, func(a, b User) int) — option (a)
byID := map[UserID]string{}                              // struct of comparable fields: native key
keys := map[Key]string{}; keys[CallbackKey{func() {}}] = … // compiles; panics at run time
```

(a) and (b) emit the same Go. Over 16 and 64 record types, the symbol sets of the two binaries are identical, and the sizes differ by 80 to 176 bytes of names in debug information (§4.2). (c) is the shortest Go, but only for types Go already compares. Its records need `SortFunc`, its enum keys are a run-time panic, and its JavaScript needs a structural encoder that the Go never shows.

### 3.4 The hard cases: what each option refuses, and where

"Refused" means an Effra program is refused at compile time. The (a) and (b) cells cite matched Effra programs in `benchmarks/where/probes/`. They are checked on Go and JavaScript with the prototype compiler by `probes/run.sh`, which needs GNU sed and exits 2 without it, and the receipt is `benchmarks/where/runs/probes.txt` (20 of 20 outcomes as expected). Style (a) passes `hash`/`equals` explicitly; style (b) uses `where K.hash, K.equals`.

| Case | (a) | (b) | (c) |
| --- | --- | --- | --- |
| Map keyed by a bare function | **Refused today for an unrelated reason:** EF127, because callable type arguments wait for spike-rows unit 4a (`a-fn-key.ef`). After 4a, accepted wherever someone writes the two functions. | **Refused today by the same EF127** (`b-fn-key.ef`). After 4a, refused on the `where` path with EF160: a function type has no owner module. | **Refused** for a direct key. For a function inside an enum key: on Go a run-time panic, `hash of unhashable type`; on JavaScript the encoder throws. |
| Map keyed by a bare resource handle (File, Latch, #96) | **Accepted** (`a-file-key.ef`), unless unit 4a refuses resources as type arguments. | **Refused on the `where` path:** EF160 "File has no owner module that can declare methods" (`b-file-key.ef`). Under #96, a resource's owner decides; a `hash` it declares still meets 4a's resource admission. | **Refused only if** the structural predicate names every handle type (a list by name, or #96's resource marker). In TypeScript, the File check is structural (`K extends File`). |
| Map keyed by a bare effect value | **Accepted** (`a-effect-key.ef`). | **Refused on the `where` path:** `effra/effect` owns `Effect` (spike-rows Q1) and declares no `equals`/`hash` (EF160, `b-effect-key.ef`). | **Refused only if** the predicate names `Effect`. Effect lowers to a Go closure or interface, so through an enum key it is a run-time panic. |
| A record wrapping a File or a function | **Accepted** with explicit functions (`a-file-wrapper.ef`, `a-fn-wrapper.ef`). | **Accepted** with owner methods (`b-file-wrapper.ef`, `b-fn-wrapper.ef`; review-1 probes p1, p2). | Go: a struct with a function field is not comparable; a pointer-like handle field compares by identity. |
| A handle inside such a record | depends on unit 4a's resource admission | depends on unit 4a | depends on 4a and on the predicate |
| Identity equality on functions, handles or effects | **Not expressible.** Effra has no `==` on them (`semantic.go:5123-5137`). Only a degenerate equivalence can be written. | **Not expressible**, for the same reason. | A compiler rule: Go compares pointers and channels by identity. |
| Ordering on f64 with NaN (#118) | **Library policy.** Ship no `Order<f64>`, and expose `F64.totalCompare` by name. Effect's `Order.Number` is total (NaN first, `NaN,1,2`); Roc's and MoonBit's float orderings are not (§5). | **The same library policy**, applied through the owner. f64's owner declares no `compare`, so the `where` path refuses f64, and `sortBy(xs, F64.totalCompare)` is explicit (§2.3). Not built: Effra has no f64 yet (#118). | **Admitted.** f64 is `Ordered`. Go's `cmp.Compare(NaN, 1) = -1` sorts NaN first; JavaScript's `<` gives `2,NaN,1` unless the lowering re-implements `cmp.Compare`. A NaN map key is never found on Go and is found on JavaScript: the targets diverge. |
| TypeScript caller | Can pass any dictionary. | Can pass any function: the caller owns the argument (§2.5). | Refused directly; bypasses reach the run-time encoder. |

**What (b) adds over (a).** It refuses nothing that (a) cannot also avoid by convention. What it adds is a default with one answer per type. The answer is chosen by the owner, and a type whose owner gives none is refused on the `where` path. A wrapper then has to state its methods, as both options' wrapper probes do.

One prototype gap is not evidence for (b): a type parameter is treated as handle-free data, so `fn same<K: type>(key: K) -> K` admits a `File` argument (the main compiler refuses the generic function with EF127). Production depends on unit 4a's resource admission for whole-value type parameters under every option.

### 3.5 Effect on #100

Both options are compared here under the same general field fold (§2.7): derive equality, ordering or hashing for a record from its fields' methods. The fold itself produces ordinary functions under either option, and (a)'s ordinary constructors can wrap them (`a-dict/lib.d.mts`). So a fold needs no profile word of its own. What it does need is each field type's method.

| Option | Where the fold gets each field type's method | What #100 gains |
| --- | --- | --- |
| (a), explicit | One dictionary argument per field type, with the combinator named as in §2.7: `derive userCompare: compare by Order.lexicographic over (name: stringOrder, age: i64Order)` (a sketch). This needs no lookup, but every derive restates the field types' dictionaries, and generic records take them as parameters. | No lookup. The derive syntax carries the arguments. |
| (a), by lookup | Derive looks each field type's dictionary up by type. | A type-directed lookup that exists only inside derive. This is (b)'s mechanism, kept private: ordinary generic code cannot use it, and it needs its own coherence rule. |
| (b) | The fold resolves `F.compare` for each field type `F` through the same owner lookup that `where` uses (§2.2). Generic records derive conditional methods (§2.9). | No `eq`/`order`/`hash` profile words; one lookup shared with all generic code. #100's `json` profile is unchanged, because codecs stay explicit witnesses. |
| (c) | Equality and hashing need no derive, because `comparable` is built into the compiler (the rung-4 construct the ladder counts against (c)). Ordering needs (a)'s functions for records. | — |

So the argument is not that (a) must add three profile words. It is that a general derive needs a type-directed lookup, and (b) is that lookup offered as a general feature rather than as a derive-internal one. If #100 accepts explicit per-field dictionaries, (a) also works, at the cost of restating them in every derive.

### 3.6 Ladder placement

See §6 for the full table. (a) adds no type feature: rung 1. Its derive needs per-field dictionaries or a derive-internal lookup (§3.5). (b) is rung 2: the `where` clause meets every condition, and no branch names a method. (c) is rung 4: `comparable` is a predeclared structural constraint that names equality, and its TypeScript projection is a recursive conditional type.

### 3.7 Checker complexity

- **(a)** adds nothing to the checker: dictionaries are ordinary arguments of ordinary generic functions. A derive that finds field dictionaries by type adds a lookup inside derive (§3.5).
- **(b)** adds about 320 lines in the prototype: 198 in `where_dispatch.go`, 64 in `syntax.go`, 29 in `semantic.go`, 5 in `field_occurrences.go`, 23 in `bundled.go` and 2 in `emit_go.go` (`git diff --stat 13e62ad4..30f80104`).
  - Resolution per method argument is one lookup. The prototype scans `program.Functions` (O(F)); production indexes methods by (nominal type, name), which is O(1).
  - The cost is linear in call sites, measured at about 0.27 ms per site, the same as (a) within noise (§4.2).
  - The only recursion is a generic application's conditional method. It descends to strict subterms of the type and so terminates, with the substitution budget as a backstop (§2.9).
  - Conditional methods add the (constructor, name) uniqueness check, the receiver-shape rule, and emission of the curried form and its adapters (W1, W3).
  - Interface summaries must carry receiver methods and `where` clauses across modules: a schema bump (§8 W1).
- **(c)** adds a structural predicate evaluated over every key and element type: walk record fields and enum variants, and refuse functions, rows, effects and handles. It adds `Ordered` as a type set. Its JavaScript lowering needs per-type structural equality and hashing for map keys, because a native `Map` uses identity (`c-comparable` consumer: `native Map record key: miss`). That is derive under another name, generated by the emitter.

### 3.8 Error-message quality: a missing instance

| Option | Effra or Go message | TypeScript message |
| --- | --- | --- |
| (a), style-(a) program on the prototype | `EF106 incorrect argument count`; with a dictionary named but undeclared, `EF102 unknown value Item_compare` | `TS2554 Expected 2 arguments, but got 1. An argument for 'order' was not provided.` |
| (b), prototype | `EF160 Item has no method compare: sort2 requires T.compare: fn(T, T) -> i64; declare `fn (value: Item) compare(...)` beside Item, or derive it` | `TS2554 Expected 2 arguments, but got 1. An argument for 'compare' was not provided.` |
| (c), go1.27.0 on the control | `Item does not satisfy cmp.Ordered (Item missing in ~int \| ~int8 \| … \| ~string)`; `Callback does not satisfy comparable`; `invalid map key type Callback` | `TS2345 Argument of type '() => void' is not assignable to parameter of type 'never'.` |

(b)'s message names the type, the method, the required signature, the function that requires it, and the fix. (a)'s names none of them. (c)'s Go message lists a type set, and its TypeScript message says only `never`.

## 4. Prototype and measurements

### 4.1 Prototype (b)

Branch `wf10/spike-where-proto`, head `30f80104`:

- **Syntax.** Receiver methods `fn (a: User) compare(b: User) -> i64` become `Function{Receiver, Method}` with the receiver as the first parameter. A `where T.m: fn(…) -> R, …` clause appends a hidden parameter (`syntax.go:1367-1391`), and `T.m` in the body is rewritten (`syntax.go:1731`).
- **Checker.** EF127 for user generic functions is lifted, and a type parameter is opaque, handle-free data in layout and ownership walks; both are prototype relaxations. The call path binds only the authored parameters, infers type arguments from them, then calls `resolveWitnesses` (`semantic.go:4661-4665`, `4759`). Receiver ownership is checked (`semantic.go:2947`). Resolution is in `where_dispatch.go`.
- **Bundled owner of `i64`:** `bundled/primitives/i64-{compare,equals,hash}.ef`. The queue in `bundled.go` lists the members of that owner module, queued when a program has a `where` clause. It is data, not a dispatch branch; production loads the owner module's interface.
- **Method-name literals.** Resolution is name-neutral: it reads the method name from the clause (`where_dispatch.go:128`). `git diff 13e62ad4..30f80104 -- '*.go' | grep '^+.*"\(compare\|hash\|equals\|Eq\|Ord\|Order\|Hash\|derive\)"'` finds no whole-name literal.
  - That grep cannot see the substring literals `"i64_compare"`, `"i64_equals"` and `"i64_hash"`, which `bundled.go:264` queues when a program has a `where` clause. They list the bundled owner members the prototype loads. They are data, not a dispatch branch, but they are name literals.
  - W2 loads the `i64` owner through an ordinary bundled import instead.
  - W1's literal test matches substrings, as a screen and not as proof.
- **End to end.** `examples/where/sort-and-key.ef` sorts with a three-element sorting network over `Three<T>`, with `sort2` propagating the constraint. It keys a fixed-capacity three-slot hash table `Table<K, V>` by a user record (`UserId.hash`/`UserId.equals`) and by `i64` through its bundled owner. `ef run` prints `abc 123 ada2,bob,none answer` **on Go and on JavaScript**.
  - Effra has no sequence type yet (recursive generic data is refused; typed buffers are #120), so both structures are fixed-size; the dispatch is what is measured.
- **Negatives** (`examples/where/negative/`), each refused on both targets: missing method, wrong signature, file key, effect key, orphan method and unpropagated give EF160; function key gives EF127. `TestWhereDispatchFixtures` passes. `30f80104` fixes a checker panic on a short generic call, found while capturing §3.8.
- **Not covered:** generic applications (`Option<User>`) are refused (§2.2, §2.9), and the formatter prints `fn(u: User)` without the space (unit W1).
- **Prototype defects found by review 1.** The probes are in `/home/exedev/.cache/baton/wf10/spike-where/review-1/probes/`, and each defect becomes a W1 acceptance case:
  - p3: an `Option<i64>` key gets "Option has no owner module that can declare methods". Its owner is `effra/data`, so the message is false (`where_dispatch.go:57-63`).
  - p4: `fn (p: Pair<A, B>) compare` in Pair's own module gets a false orphan EF160, because `checkReceiverOwner` compares the string `Pair<A, B>`.
  - p6: an `effect fn` receiver method is refused, but the message prints two identical signatures, because the reconstruction drops the effect (`where_dispatch.go:79-85`).
  - The function-key negative passes for an unrelated reason (EF127, callable type arguments). The negatives assert codes but not messages, and the final `strings.Contains(demo, "where T.compare")` greps the source.
  - So W1 requires EF160 for function keys after 4a, owner and signature messages that are accurate, and behavioural dispatch tests.

### 4.2 Measurements

`benchmarks/where/bench.sh EF_PROTO WORKDIR` generates the programs with `benchmarks/where/gen.mjs`. Style (b) uses `where T.compare` and receiver methods. Style (a) passes the comparison explicitly as a function value, which is Effect's `Order<T>` and the one-method dictionary of (a). The script builds and checks both with the prototype, because the main compiler refuses generic functions. Host: 16 CPUs, shared, Linux 6.12.93. Binaries are built with go1.27.0. Receipts: `benchmarks/where/runs/bench-1.txt` and `bench-2.txt`.

**Checker time** (`ef check --target go`, `checkMicros`, 16 record types, median of 3, the styles interleaved within each size so load drift falls on both):

| N call sites | (a) pass 1 | (b) pass 1 | b/a | (a) pass 2 | (b) pass 2 | b/a | Load (1, 5, 15 min) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1000 | 453 ms | 596 ms | 1.32 | 267 ms | 285 ms | 1.07 | 19.4 15.4 16.7 / 15.0 15.6 16.6 |
| 2000 | 762 ms | 885 ms | 1.16 | 534 ms | 506 ms | 0.95 | 20.7 15.8 16.8 / 14.1 15.4 16.6 |
| 4000 | 1337 ms | 1231 ms | 0.92 | 1012 ms | 1218 ms | 1.20 | 21.9 16.1 16.9 / 13.9 15.4 16.5 |
| 8000 | 2376 ms | 2598 ms | 1.09 | 2134 ms | 2058 ms | 0.96 | 21.6 16.2 17.0 / 12.9 15.1 16.4 |

- Both styles are linear in N: an 8x increase in N gives 7.7x to 8.0x the time in pass 2.
- The b/a ratio changes sign between passes at the same size. The individual runs spread by up to 45% at this load, so no difference between the styles is resolvable. The pooled geometric mean is 1.08.
- (b)'s parse time is lower, because its source is 9% smaller (`bytes` column in the receipts).

**Go binary and stencils** (one call site per record type; "stencils" are text symbols `main.efFunction_sort{2,3}[go.shape.*]`; "dicts" are Go's instantiation dictionaries; "runtime" is SH1's runtime-package stencil count):

| Types | Layouts | Style | Binary bytes | Stencils | Stencil bytes | Dicts | Runtime stencils |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 16 | distinct | a | 2,773,597 | 32 | 23,712 | 32 | 7 |
| 16 | distinct | b | 2,773,517 | 32 | 23,712 | 32 | 7 |
| 16 | shared | a | 2,731,575 | 2 | 1,482 | 32 | 7 |
| 16 | shared | b | 2,731,399 | 2 | 1,482 | 32 | 7 |
| 64 | distinct | a | 2,972,212 | 128 | 94,848 | 128 | 7 |
| 64 | distinct | b | 2,972,132 | 128 | 94,848 | 128 | 7 |
| 64 | shared | a | 2,810,422 | 2 | 1,482 | 128 | 7 |
| 64 | shared | b | 2,810,438 | 2 | 1,482 | 128 | 7 |

Both passes give identical figures.

- **The method argument adds no stencil.** Go stencils a generic function once per GC shape of its type arguments, and the argument is a `func` value of a shape Go already has.
- **"Distinct" layouts** give every record its own field name, and therefore its own GC shape, as SH1's fixture does: one stencil per type per generic function, 741 bytes each.
- **"Shared" layouts** (the same fields in every record) collapse to one stencil per function.
- **SH1's runtime count stays at 7** throughout: `where` does not touch the runtime package.
- A design that wants one stencil for many record types would box `T` to a pointer shape inside generic code. That is an SH1-level choice, independent of this decision.

**JavaScript output** (16 types; the `.mjs`, raw and `gzip -9`):

| N | Style | `.mjs` bytes | gzip | `.d.mts` bytes |
| --- | --- | --- | --- | --- |
| 16 | a | 18,111 | 4,103 | 12,070 |
| 16 | b | 18,159 | 4,108 | 12,070 |
| 1000 | a | 263,152 | 11,031 | 13,867 |
| 1000 | b | 263,200 | 11,039 | 13,867 |

The 48-byte difference is the bundled `i64` owner methods' preamble. Call sites have the same length in both styles.

## 5. Prior art

Sources were read on 2026-10-12 from local clones at the pins below; nothing was built from them. Paths are relative to each repository.

The deviations from those pins:
- Rust is a sparse checkout, read with `git show`.
- Effect is read at `460272d3` (4.0.1) with `git show`, and checked against the 4.0.1 `dist` in this repository's `node_modules`.
- Go sources are the go1.27.0 toolchain module; the spec text is from the go1.26.5 `doc/go_spec.html`, because the toolchain module ships no `doc/`.
- Haskell is cited bibliographically, and not verified locally.

| Language, pin | Mechanism | Lowering path | Coherence | derive | Function key / handle key / NaN |
| --- | --- | --- | --- | --- | --- |
| **Roc** `3ee70f0c` | Methods (`is_eq`, `to_hash`, `is_lt`) live in the nominal type's own block. `where [a.is_eq : …]` names a method, not a trait (`docs/langref/static-dispatch.md:18-19`, `66-69`, `88-90`, `427-433`). | Monomorphization (`docs/langref/types.md:365-378`); an experimental `--specialize=no` boxes instead. | Methods exist only in the type's block, so nothing is orphaned. | Structural types get `is_eq` automatically; nominal types opt in with `is_eq : _` (`static-dispatch.md:92-115`). | Functions are refused at compile time, "Functions cannot be compared for equality." (`src/check/report.zig:3120`). A handle is refused unless its type opts in. NaN is silent at run time: F64 has `is_eq` and `to_hash`, and `is_lt` is false with NaN (`src/build/roc/Builtin.roc:18369-18373`, `18411-18418`). |
| **Rust** `69bccf03` | Traits `PartialEq`, `Eq`, `PartialOrd`, `Ord` (`library/core/src/cmp.rs:253`, `353`, `1045`, `1458`) and `Hash` (`core/src/hash/mod.rs:185`). | Monomorphization (`compiler/rustc_monomorphize/src/collector.rs:7-12`). | Orphan rule (`compiler/rustc_hir_analysis/src/coherence/orphan.rs:22`). | Builtin derive macros, which add a bound per type parameter (`compiler/rustc_builtin_macros/src/deriving/generic/mod.rs:10-13`). | Function pointers compare and hash by address (`core/src/ptr/mod.rs:2627-2668`). Floats are `PartialOrd` only (`cmp.rs:2290`), so `sort()` and `HashMap<f64, _>` are compile errors; `total_cmp` is explicit (`core/src/num/f64.rs:1515-1578`). |
| **Haskell** (Wadler and Blott, POPL 1989; Haskell 2010 Report, ch. 11) | Type classes `Eq`, `Ord`; `deriving`. | Dictionary passing: the classic translation adds one dictionary argument per constraint, which is exactly (b)'s lowering. | Global instance uniqueness; orphan instances are a warning. | `deriving` by the compiler for named classes. | Not verified locally: functions have no `Eq`; `Ord Double` exists and is unlawful with NaN. |
| **MoonBit** docs `8d9f3ba2`, core `e96ede8b`, compiler `d4ada10d` | Traits `Eq`, `Compare`, `Hash` (`builtin/traits.mbt:17`, `34`, `73`); methods only in the type's package (`next/language/packages.md:203-205`). | Monomorphization: `Plocal_method` is resolved to a concrete method (`src/monofy.ml:119-127`). | Implementations only in the type's or the trait's package. | `derive(Eq, Compare, Hash)` (`next/language/derive.md:15-18`, `91-95`). | No `Eq` implementation exists for function types (none found in `builtin/pkg.generated.mbti`). `Double` implements `Compare`; the wasm-gc lowering `(x>=y)-(x<=y)` makes NaN compare 0 with everything (`src/wasm_of_clam_gc.ml:394`). Maps need `K : Hash + Eq` (`builtin/linked_hash_map.mbt:108`). |
| **Go** 1.27.0 | Built-in `comparable`; `cmp.Ordered` (`src/cmp/cmp.go:18`); explicit `func(a, b T) int` for `slices.SortFunc` (`src/slices/sort.go:30`). | GC-shape stencils plus a run-time dictionary (`src/cmd/compile/internal/noder/reader.go:163-172`, `Shapify` at `:960`): the host's own form of argument passing. | Structural; there are no instances. | None. | A function key is a compile error (spec: "Slice, map, and function types are not comparable"). An interface key holding one panics, "hash of unhashable type" (`src/runtime/alg.go:164`). Pointers and channels compare by identity. A NaN key hashes randomly and is never found again (`alg.go:135-136`). `cmp.Compare` orders NaN equal to NaN and below everything else (`cmp.go:38-49`). |
| **Gleam** `52e735c8` | `==` on any type, operands of the same type (`compiler-core/src/type_/expression.rs:1819-1824`). Ordering through explicit `fn(a, a) -> Order` (stdlib `13359e5`, `list.gleam:1137`). | JavaScript: a run-time structural walk `isEqual` (`compiler-core/templates/prelude.mjs:1481-1518`). Erlang: `=:=` (`compiler-core/src/erlang.rs:1501`). The output stays simple because equality is one runtime function, and ordering is always an argument. | None needed. | None (equality is structural). | Functions compare by reference on JavaScript (`prelude.mjs:1487-1489`), and the stdlib dict hashes them by reference (`dict.mjs:147-148`). JavaScript `float.compare(NaN, x)` falls through to `Gt` (`float.gleam:100-109`). |
| **Elixir** `91ee75bb` | Structural term order for `<` and `==`. Semantic comparison is a module convention: `Enum.sort(dates, Date)` calls `Date.compare/2` (`lib/elixir/lib/enum.ex:3354-3356`). Protocols dispatch on the first argument's data type. | The module becomes `&(module.compare(&1, &2) != :gt)`, a function argument. Consolidation closes the implementation set. A consolidated call is still "two remote functions - one to identify the correct implementation, and another to call the implementation" (`protocol.ex:184-188`, `consolidate` at `:580`). | Protocols have no orphan rule (not verified at a line). | `@derive` through `__deriving__/2` (`protocol.ex:260-266`, `391-437`); `@fallback_to_any`. | Functions and references are ordered in term order (`kernel.ex:239`), and maps accept any key. |
| **ReScript** `30ce3698` | Polymorphic `compare` at run time, or `Belt.Id.MakeComparable`/`MakeHashable` (`packages/@rescript/belt/src/Belt_Id.resi:41-78`, `98-101`). | The comparator is stored in the collection: `type t = {cmp, data}` (`Belt_Map.res:24-25`, `76-78`). A branded id type makes maps with different comparators different types. Functors erase to plain records of functions. | The brand. | None built in. | Functions are refused at run time, `compare: functional value` (`packages/@rescript/runtime/Primitive_object.res:78`), except that `equal` returns early on `a === b` (`:230`). `Primitive_float.compare` orders NaN equal to NaN and below everything else. |
| **OCaml** `6471927d` | Polymorphic `compare`/`=`, or the functor `Map.Make(Ord)` (`stdlib/map.mli:49-54`). | Functor application captures `Ord.compare` in the resulting module, not in each map value. The value is `Empty \| Node …` with no comparator (`stdlib/map.ml:75-79`). | The generative module path. | None in the compiler (ppx outside). | Functions are refused at run time, `compare: functional value` (`runtime/compare.c:264`), except that `compare` returns early on physical equality. `Hashtbl.hash` hashes closures by code pointer (`runtime/hash.c:258-270`). An abstract block raises "compare: abstract value" (`compare.c:285`). `Float.compare` is total; `<` with NaN is false. |
| **OCaml modular implicits** (White, Bour and Yallop, arXiv:1512.01895) | Implicit module arguments resolved by type. | Not merged. OCaml 5.5.0 has modular *explicits* instead, `(module M : S) -> t[M]` (`Changes:924-926`): an explicit dictionary-passing precedent. | — | — | — |
| **Effect** 4.0.1 `460272d3` | `Order<A>` and `Equivalence<A>` are plain functions (`src/Order.ts:53-55`, `Equivalence.ts:60`); `Equal.symbol` and `Hash.symbol` are protocols on values (`Equal.ts:55`, `Hash.ts:32`). | Explicit values. Higher-kinded types use `TypeLambda` (`HKT.ts:140-145`, `Kind` at `:205`). | None. | Through `Data` and `Schema`, not the compiler. | Functions compare by reference (`Equal.ts:183`, `:247`). `Equal.equals(NaN, NaN)` is true. `Order.Number` orders NaN below everything else (`Order.ts:177-180`). `HashMap` uses the global `Hash.hash(key)`. 4.0.1 has no `SortedMap`. |

**Lowering paths, and what Effra takes:**

- **Roc and MoonBit** confirm owner-module resolution: methods declared beside the type, and constraints that name methods. Both monomorphize; neither passes arguments. MoonBit also lets a trait's package implement the trait for a foreign type, an escape that (b) does not offer (§2.2, "What the owner rule gives up").
  - Effra keeps Roc's surface and resolution rule, and Roc's compile-time refusal of function equality.
  - Effra lowers like Haskell's dictionary translation and like Go's own GC-shape dictionaries: a hidden argument, with Go stenciling once per shape. SH1 already rules out frontend monomorphization (`docs/design.md:301`).
  - Effra rejects Roc's and MoonBit's silent float orderings (MoonBit's `compare` returns 0 against NaN) by giving f64 no `compare`, as Rust does with `PartialOrd`.
- **Rust** supplies the orphan rule and derive adding a bound per type parameter (§2.7's generic records). Its `total_cmp` is the model for `F64.totalCompare`.
- **Go** shows the cost of (c): `comparable` admits interface types that panic at run time, the "unchecked passthrough" Effra refuses. It also shows that a hidden `func` argument fits Go generics: the host already passes a dictionary per instantiation.
- **Gleam** keeps both targets simple by making ordering an explicit function, Effra's `sortBy`, and equality one runtime walk. Effra does not adopt Gleam's `==` on every type: on JavaScript it compares functions by reference, and on Erlang it uses term order, so the targets differ on exactly the hard cases of §3.4.
- **Elixir**'s `Enum.sort(xs, Date)` passes a module as a dictionary value, but the caller names it; nothing resolves it from the type. That is (a) with module-shaped dictionaries. Elixir also has a per-type name convention much like (b)'s, but the caller is the one who applies it. Consolidation closes the implementation set, but it keeps two remote calls, identify and then call. Under (b) the identification happens at compile time, so each call passes the resolved function directly.
- **ReScript** stores the comparator in the collection value and puts its identity in the type (§2.6, Q6). **OCaml**'s `Map.Make` captures it in the functor's module instead. Both erase functors to records of functions. Both refuse functions only at run time, and only when the values are not physically equal. That is exactly the run-time-only refusal this decision avoids.
- **Effect** is the JavaScript representation the method argument lowers to: `Order` is a function. Its `TypeLambda` is an intersection-and-index encoding of higher-kinded types, `(F & { In… })["type"]`. It is a type-level language with no Go counterpart, and the ladder excludes HKTs, so Effra does not adopt it. A generic `Option<T>` comparison is expressed by a conditional method (§2.9) instead.

## 6. Ladder check

| Candidate | Describable without naming Eq/Order, two or more unrelated clients | Readable with Go's simplicity | Same meaning in TypeScript, no `any` | Erases into Go | Compiler branch keyed on a name | Rung |
| --- | --- | --- | --- | --- | --- | --- |
| (a) dictionaries as ordinary records/functions | Yes: any library value | Yes: Go `slices.SortFunc` | Yes: `Order<T>` record or function | Yes: a `func` parameter | None; a general derive needs per-field dictionaries or a derive-internal lookup (§3.5) | 1 |
| **(b) `where` clause with owner-module resolution** | **Yes:** "a type argument's owner module declares a method with this signature"; clients equality, ordering, hashing, display, zero values, generic arithmetic | **Yes:** a method in the signature, like Go's `interface{ Compare(T) int }` constraint, with no type sets | **Yes:** a function-typed parameter; nothing narrowed, so fail-safe; 0 `any` (§3.1, §3.2) | **Yes:** Go generics plus a `func` parameter; 0 extra stencils; explicit type arguments as F1 needs anyway (§4.2); conditional methods as closures, not copies (§2.9) | None in dispatch; the prototype's bundled-member list is data (§4.1) | **2** |
| (b) structural TypeScript projection | Yes | Yes | **No:** primitives cannot carry methods, and the declared rule is structural while Effra's is owner lookup (§3.2) | Yes, but methods on values need classes on JavaScript | None | Rejected |
| contract with laws (§2.10) | Yes: groups `where` entries; its clients are ordering, codecs, services and machines (#125) | Yes | Erases to the same method arguments; laws erase | Yes | Law checking is compiler-special (`docs/design.md:42`) | #125's construct |
| (c) `comparable` / `Ordered` | **No:** `comparable` is equality, by name | Yes for Go readers | **No:** a recursive conditional type; generic callers refused (TS2589); a run-time encoder on JavaScript | Yes on Go; JavaScript needs a generated structural encoder | `comparable` is predeclared and structural | 4 |
| derive by field fold (§2.7) | Yes: equals, compare, hash, display; names none | Yes: one rule | Yes: ordinary functions on the companion object | Yes: ordinary functions | None | Part of #100's derive construct |

## 7. Decision record

**Decided 2026-10-12 (spike #116, revision 4 after reviews 1 to 3 and re-plan 1).**

- **Chosen: (b).** Equality, ordering and hashing are receiver methods declared in each type's owner module.
  - Generic code requires them with `where T.m: fn(…) -> R` and calls them as `T.m(…)`.
  - The checker resolves each method at the call site through the type's owner (§2.2). It lowers the method to a trailing function argument on Go and JavaScript, and to a named function-typed parameter in the `.d.mts` (§2.4, §2.5).
  - Conditional methods on generic types are unique per (constructor, name). Each is emitted once, and it is resolved as a curried adapter closure with no frontend monomorphization (§2.9).
  - Coherence is the orphan rule, and it holds on the `where` path (§2.2).
  - Every collection captures its methods at construction (§2.6). An operation over two collections reinserts the incoming entries under the receiver's methods (Q6).
  - f64 declares none of the three, as library policy.
  - derive targets receiver methods through one field-fold rule that uses the same lookup. JSON stays a #100 profile, and codecs stay explicit witnesses.
  - Laws attach to a named contract that groups `where` entries, with the syntax reserved to #125 (§2.10).
  - Evidence is #125's mechanism. This note hands #125 four requirements (§2.10): the outward boundary rule (forms i and ii), inward admission, a deep Effra-owned snapshot, and private canonical bindings with frozen companions. `ts/b-evidence` is #125 input evidence, with review 3's attacks recorded as expected failures.
  - The surface syntax is provisional until #135 decides it (§2.11). The semantics above do not depend on it.
- **Rejected: (a) as the mechanism.** The reasons kept:
  - it has no type-directed lookup. Every call names its dictionary, and a hard type such as File or an effect value is accepted wherever someone writes one (§3.4, probes);
  - two dictionaries for one key type can coexist, because nothing designates one as canonical;
  - a general derive needs per-field dictionaries in every derive, or a lookup private to derive (§3.5);
  - its missing-instance message names nothing (§3.8).

  Not reasons: (b) refuses nothing that (a) cannot also avoid by convention, f64's missing default is library policy under either option, and neither option expresses identity equality on functions or handles. (a)'s TypeScript projection is as good as (b)'s. It is kept as the explicit `…By` form inside (b), so nothing (a) can express is lost.
- **Rejected: (c).** Reasons kept:
  - rung 4, and it names equality (§6);
  - it admits NaN, and the targets diverge on it (§3.4);
  - JavaScript needs a run-time structural encoder, because `Map` uses identity;
  - generic TypeScript callers are refused (TS2589);
  - handle and effect refusal depends on a name list;
  - records still need explicit comparisons.
- **Rejected: (b) with a structural TypeScript projection.** A primitive cannot carry the method, methods on values need classes on JavaScript, and owner lookup is not structural (§3.2). Its attacks bypass the method's presence, but they throw before returning.
- **Rejected for now: comparator identity in collection types** (ReScript's brand). On the `where` path the key type already fixes the method. A brand would only distinguish `…By` and TypeScript collections, and that needs the per-value naming that #125 and the opaque-values design own (Q6).
- **Rejected: laws on the receiver method alone** (§2.10). Laws that relate two methods have no home there, and a `where` clause could not ask for them.
- **Rejected: an Effect-style `TypeLambda` encoding** for generic `Order`/`Equal` over containers. It is an HKT encoding, which the ladder excludes, and it has no Go erasure (§5).

## 8. Follow-up units

Prerequisites already costed elsewhere:
- spike-rows unit 3: receiver methods, and F1 explicit type arguments with their Go emission;
- spike-rows unit 4a: Q9 whole-value type parameters, including callable type arguments and resource admission for type parameters.

The units below start after both; W2 and W6 can run beside W1.

| Unit | What lands | Tests and acceptance | Estimate |
| --- | --- | --- | --- |
| **W1. `where` clauses** | Parser, formatter (the receiver spacing) and LSP spans. Elaboration to method parameters. Resolution: cases 1-3 of §2.2, plus conditional methods (§2.9): the (constructor identity, name) index and uniqueness check, the receiver-shape rule, the subterm-only `where` rule, the body and curried entry points, and static adapters hoisted per module. EF160 family diagnostics anchored on the binding argument. Interface-summary transport of receiver methods and `where` clauses (schema bump, round trip, `TestEvidenceSchemaIsClassified`). Reachability of resolved methods. | The prototype's fixtures and negatives, with messages asserted. A cross-module resolution test. `Option<User>`, `Option<Option<User>>` and `Pair<i64, User>` sorting on both targets. Refusal of a receiver on one instantiation, of a duplicate (constructor, name), of a `where` entry on a non-parameter, and of polymorphic recursion in generic functions and conditional method bodies, including mutual recursion (review 2's `q1-polyrec-fn.ef` must be refused by the checker, not by Go's build). The review-1 defects fixed (§4.1): an accurate owner message for `Option`, no false orphan for `Pair<A, B>`, effects kept in the reconstructed signature, and EF160 for function keys after 4a. Behavioural dispatch tests rather than a source grep. A substring screen for method-name literals. Go and JavaScript equivalence. Checker cost at N = 1k to 8k against this note's receipts. | L, 5.5 to 6.5 days |
| **W2. Predeclared owner modules** | Bundled owner modules for `i64`, `string` (code-point order on both targets) and `bool`, each with `compare`, `equals` and `hash`, loaded through an ordinary bundled import rather than the prototype's name list. f64 declares none (coordinated with #118). Orphan refusal for predeclared types. | Go/JavaScript pairs for string order on non-ASCII input; f64 refusal once #118 lands. | S, 1 day |
| **W3. TypeScript projection** | Method parameters named after their method. Companion objects per type exposing owner methods (value plus interface merge), with the curried form for conditional methods (§2.9). Optional boundary check of method arguments (§9 Q3). The companion object is frozen, and Effra-side canonical calls use module-private bindings, never the companion (§2.10, requirement 4). | `benchmarks/where/ts/b-witness` and `b-cond` promoted to generated-consumer fixtures: consumers, negatives, attacks A1-A5 with their pinned outcomes; 0 `any`. `Object.assign` on a generated companion throws, and the module's own calls still use the canonical method (R10's route). `b-evidence` stays #125 input evidence, not W3 acceptance (§2.10). | M, 2 to 2.5 days |
| **W4. derive by field fold** | The rule of §2.7 inside #100's `derive` construct: records in field order, enums by variant order, generic records as conditional methods, library combinators named by the deriving module. | Derived `compare`/`equals`/`hash` equal hand-written ones on Go and JavaScript; a refusal for a field type without the method; no profile word for them. | M, 2.5 to 3 days, after #100's syntax |
| **W5. Collections** | `HashMap<K, V>` capturing `hash`/`equals` at construction for every map (§2.6). Cross-collection operations that reinsert under the receiver's methods (Q6). A pinned iteration order (Q6): comparator order for ordered collections, insertion order for hashed ones (removal drops a key, reinsertion appends it), the same on both targets; equality and hash ignore that order. `sort`/`sortBy` over a sequence type. | Union of a `where`-built map and an `emptyBy` map with a different hash finds every key under the receiver's hash. `{"A": 1, "a": 2}` merged into a case-insensitive receiver keeps the same key and value on Go and JavaScript. Iteration order is identical on both targets, including after removal and reinsertion. Two maps with the same entries in different insertion orders are equal and hash alike. The insertion sequence's cost is measured. Blocked on recursive generic data or a sequence type (#120). | M, 2.5 to 3.5 days once unblocked |
| **W6. Docs** | `docs/design.md:33` (predeclared types' owner modules) and `:300` (owner-module lookup is not instance search); `bundled-interfaces.md:39` (codecs stay explicit witnesses, ordering/equality do not); PRIOR_ART's Roc row open question closed; GLOSSARY "Where clause", "Method argument", "Owner module"; CHANGELOG. | Docs review. | S, 0.5 day |

Total: about 14 to 17 implementor-days plus review rounds, excluding the spike-rows prerequisites. W5 is not schedulable until a sequence type exists. The risk is in W1's conditional methods and in the summary transport. Laws (§2.10) are #125's units, not costed here.

## 9. Open questions

- **Q1. Do operators dispatch to methods?** Should `==` and `<` on a record call `equals`/`compare`? Recommendation: **no**. Operators stay predeclared on primitives, as in Go, where `==` never calls a method, and code calls `a.equals(b)`. This keeps the operator guard (`semantic.go:5123-5137`) a closed table.
- **Q2. Per-call or captured method arguments for collections?** Recommendation: **captured at construction, for every collection**, unconditionally (§2.6). A collection cannot know whether its values will reach TypeScript. Per-call arguments stay the lowering for ordinary functions.
- **Q3. A boundary check of method arguments in exported JavaScript?** Recommendation: **yes, in W3**. One `typeof` check per exported call turns A3/A4's `TypeError` into a typed defect, and catches A4's broken map at construction. Effra-internal calls skip it. It detects a missing function, not a wrong or unlawful one; lawfulness is §2.10's evidence question.
- **Q4. Should codecs get a default witness through `where`** (`where T.codec: …`) while keeping explicit witnesses for alternatives? Recommendation: **defer**. `bundled-interfaces.md:39`'s several-witnesses argument stands, and #100 lands first.
- **Q5. Which f64 names?** Recommendation: `totalCompare`, `ieeeEquals` and `equalsBits`, decided in #118. None is named `compare`, `equals` or `hash`. Consequences, as in Rust: f64 is not a default map key, and a record with an f64 field gets no derived `hash`, `equals` or `compare` unless the deriving module names the f64 function for that field explicitly.
- **Q6. How does an operation over two collections behave** (union, merge, equality, difference), when their captured methods may differ? The candidates:
  1. **Reinsert** the incoming collection's entries under the receiver's methods. The result keeps the receiver's methods, and it is always correct with respect to the receiver.
     - **Cost**, for m incoming entries into a receiver of n: a hashed collection takes O(m) hash evaluations, plus equality calls that depend on collisions. An ordered tree takes O(m log(n+m)) comparisons.
     - **Semantics.** Membership and normalisation follow the receiver's methods, so cross-collection equality is receiver-relative and not symmetric. A case-insensitive `{"A"}` equals a case-sensitive `{"a"}` when the case-insensitive set receives, but not the other way round.
     - **Which key and value win.** When the receiver's equality merges an incoming key with an existing one, the receiver's existing key is kept. Union keeps the receiver's value, and an explicit `unionWith(f)` combines the two values as `f(receiverValue, incomingValue)`. Among several incoming keys that collapse to one receiver key, the first in the incoming collection's iteration order wins.
     - **That order is pinned**, so the collapse rule gives the same result on Go and JavaScript:
       - an ordered collection iterates in its comparator's order;
       - a hashed collection iterates in insertion order, the order in which its current keys were first inserted. A union's result lists the receiver's keys first, then the new incoming keys in the incoming collection's order. Removing a key drops it from the order, and inserting it again appends it at the end; updating an existing key's value keeps its place. Roc's `Dict` instead keeps insertion order only until a removal (`src/build/roc/Builtin.roc:6212-6214` at `3ee70f0c`).
       - Equality and hashing of a hashed collection ignore its insertion order, as Roc's (`Builtin.roc:5977`) and MoonBit's (`builtin/linked_hash_map.mbt:861-870` at `e96ede8b`) do, so a field-fold derive never makes two equal maps unequal.
       - No Effra collection inherits a host map's order. Go leaves map iteration order unspecified (`doc/go_spec.html:6750`, go1.26.5), and its runtime randomizes it. JavaScript's `Map` iterates in insertion order, and MoonBit's `Map` keeps insertion order the same way (`builtin/linked_hash_map.mbt:29` at `e96ede8b`). Elixir's maps "do not follow any order" (`lib/elixir/lib/map.ex:17` at `91ee75bb`), which is the gap a pinned order closes.
     - The insertion sequence costs some space and time per entry beside the buckets; W5 measures it. W5 tests each rule on Go and JavaScript.
  2. **Put witness identity in the type** (ReScript's `Belt.Id`, `Belt_Id.resi:41-53`), so collections with different methods do not type-check together.
     - On the `where` path the key type already fixes the method, so the brand would only separate `…By` and TypeScript collections.
     - Naming a comparator value in a type is the per-value naming of §2.10, which belongs to #125 and the opaque-values design.
     - It adds a parameter to every collection type and signature.
  3. **Check identity at run time** and reuse buckets when the methods are identical.
     - Go cannot compare `func` values.
     - Code pointers coincide for closures with different captures: two `Option.compare(…)` adapters share code.
     - So the check cannot be made sound on Go.

  Recommendation: **(1) as the semantics, in W5.** A later fast path may tag methods resolved on the `where` path with a compile-time key (the declaration identity plus the keys of its method arguments). Two equal, known keys would then allow bucket reuse, while `…By` and TypeScript methods carry no key and always reinsert. That fast path is an optimization to adopt only with measurements. Revisit (2) when #125 delivers named comparators.
- **Q7. Where do laws attach?** Recommendation: on a named **contract** that groups `where` entries with their laws, satisfied by an owner-module declaration (§2.10). The syntax is reserved to #125. Evidence such as `SortedBy<…, User.compare>` is minted only through a contract-satisfied clause, and `…By` closures mint none until per-value naming exists.
  - **The mechanism is #125's.** This note hands it four requirements (§2.10; `opaque-values-and-evidence.md:38-39`):
    1. **Outward, the evidence boundary rule (binding).** An exported minting function either (i) binds the canonical methods on the Effra side, with no method parameter in its TypeScript signature, or (ii) drops the evidence in its `.d.mts` and returns the unbranded type, which Effra re-establishes before trusting. Evidence minted from methods a caller supplied never crosses outward, and generic forwarding follows the same rule.
    2. **Inward.** Host-originated evidence is never trusted. An exported function with an evidence parameter takes the unbranded type and re-establishes the evidence on entry, or stays private.
    3. **Stability.** Evidence covers a deep, Effra-owned snapshot.
    4. **Canonical method.** Canonical calls use private bindings, and companion objects are frozen.
  - `ts/b-evidence` is #125 input evidence, not acceptance. It shows requirement 1: review 2's laundering route fails to type-check. It also records review 3's and review 4's routes against requirements 2 to 4 (R1-R3, X2, A1, A2, A3, R10) as expected failures.
