# Where-clause dispatch: equality, ordering, hashing and derive

**Status:** design note and decision record for the spike in [#116](https://github.com/cevr/effra/issues/116), revision 1, 2026-10-12. It changes no compiler behaviour. Line references are at `main` `13e62ad4` unless stated otherwise; paths are under `internal/compiler/` unless stated otherwise. The prototype is on branch `wf10/spike-where-proto`, head `30f80104` (two commits on `13e62ad4`: `034df57d` the prototype, `30f80104` a crash fix). That branch is never merged. The measurement scripts and their receipts are under [`benchmarks/where/`](../../benchmarks/where/).

## Owner direction

- **2026-10-08.** Roc-style static dispatch (`where` constraints, specialised per type) was rejected because design.md then forbade monomorphization. The monomorphization spike disproved that rule's premise the same day: Go already compiles about one copy per GC shape, and lane SH1 kept Go generics with chosen shapes (`docs/design.md:301-303`).
- **2026-10-11, on #116.** "go's simplicity is the goal. We want to compile down to typescript." TypeScript has no type classes and Effect passes `Order<A>`/`Equal` values, so option (a) maps one-to-one onto what a TypeScript caller sees. Option (b) "must show what a TypeScript caller writes for a constrained function before (b) can win."
- **2026-10-11, the construct ladder** (memory `syntax-over-special-library`, adopted pending objection). Types are not special casing; a compiler branch keyed on an item's name is. The winner must sit on rung 1 (ordinary library) or rung 2 (library plus a general type feature). A rung-2 feature must be describable without naming the construct, serve two or more unrelated clients, read with Go's simplicity, project into the `.d.mts` with the same meaning and no `any`, and erase into plain Go or Go generics with SH1's shapes. No type-level language, no HKTs.
- **2026-10-11, orchestrator ruling during this spike.** Generated `.d.mts` projections must be fail-safe, not refuse-only. Each option's projection is attacked with the four routes from spike-rows review 4 (M1): method-parameter bivariance, global `interface Object` augmentation, a `never[]` rest, and a never-typed filler. A narrowing must depend on a type-level check that widens on failure and never lies.

## Verdict

**Choose (b): `where` constraints resolved through each type's owner module, lowered to an explicit method argument.** The scope is equality, ordering and hashing. Encoding stays an explicit codec witness, as today.

The deciding evidence:

1. **(b) erases to exactly (a).** The checker appends the resolved method as a trailing argument. Go gets a generic function with one more `func` parameter, JavaScript one more argument, and the `.d.mts` one more function-typed parameter (§2.4, §2.5). The TypeScript caller writes `sort3(xs, User.compare)`, which is what an Effect user writes with `Array.sort(xs, order)`. There is no overload set, no `any`, and no type-level computation. tsc 6.0.3 strict accepts the consumer with 0 `any` keywords in every declaration and runtime file (§3.1).
2. **The projection is fail-safe because it narrows nothing.** No declared type depends on a constraint holding. The four attack routes either are refused or make the call fail at its first use of the missing method, before any value escapes (§3.2). The one route that lets a mistyped value escape (key bivariance, A1b) is TypeScript's container hole: it is the same on a native `Map` and involves no constraint.
3. **(b) makes the hard cases unrepresentable at compile time in Effra.** A function, an effect value or a resource handle has no owner module that declares `hash` or `equals`, and f64's owner module declares no `compare` (§3.4). (a) cannot refuse any of these: anyone can write a dictionary for any type. (c) refuses them only through a compiler-defined structural predicate, still leaves NaN admitted, and needs a run-time key encoder on JavaScript.
4. **(b) costs nothing measurable at run time or in size.** It adds 0 Go stencils over (a). The binaries differ by under 200 bytes of 2.7 to 3.0 MB, and the JavaScript output by 48 bytes (§4.2). Checker time is linear in call sites (about 0.27 ms per site). It is indistinguishable from (a) at the recorded load: the b/a medians range from 0.92 to 1.32 over two passes, with a pooled geometric mean of 1.08.
5. **It removes compiler-named profiles rather than adding them** (#100, §3.5). Under (a), derive must produce dictionary values of library types, which needs an `order`, an `equivalence` and a `hash` profile beside `json`. Under (b), derive targets ordinary receiver methods through one structural rule, and only `json` stays a profile.
6. **No compiler branch names a method.** The method names come from the `where` clause the library author writes; the prototype diff contains no `"compare"`, `"hash"`, `"equals"`, `"Eq"`, `"Order"` or `"derive"` literal in Go code (§4.1).

**Losers, with their reasons kept (§7):**

- **(a) alone** projects identically to TypeScript, but leaves every hard case representable. It lets two maps over one key type disagree on hashing, needs combinator boilerplate at every call, and grows #100's profile list. It survives inside (b) as the explicit form: every constrained library function is written over an explicit `…By(xs, compare)` and the `where` form calls it.
- **(c)** is a rung-4 construct that names equality. Its TypeScript projection is a recursive conditional type that generic TypeScript callers cannot use (TS2589). JavaScript needs a run-time structural key encoder because a native `Map` compares records by identity. NaN is admitted at compile time, and the targets disagree unless the JavaScript lowering re-implements `cmp.Compare`: Go sorts NaN first, while JavaScript's `<` leaves `2,NaN,1` unsorted. Records still need `SortFunc`, which is (a).
- **(b) with a structural TypeScript projection** (`T extends { compare(other: T): bigint }`, methods on the values) is refuse-only. Method bivariance and `Object` augmentation get past it with no lying value, and it has no wider type to fall back to, because the function cannot run without the method (§3.2).

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

### 2.2 Resolution

At a call, the checker binds the type parameters from the authored arguments, then resolves each `where` entry from the binding of its type parameter:

1. **Bound to the caller's own type parameter `U`.** The caller must declare the same entry for `U`; its method argument is passed on. If the caller does not declare it, the call is refused, with a hint to add the clause (constraint propagation).
2. **Bound to a nominal type `N`** (a record, an enum, an opaque type, or a predeclared type). The method is the receiver method `fn (x: N) m(…)` declared by `N`'s owner module. The owner of a user type is its declaring module. The owner of a predeclared type is a bundled module: `i64`, `string` and `bool` each have one. The signature must match the clause with `T = N`.
3. **Anything else** is refused: a function type, a row, `Effect<…>` without a method in `effra/effect`, or a handle without one in its owner. These have no owner module that declares the method.

**Production adds one case the prototype refuses: a generic application.** For example, `Option<User>` resolves `Option`'s method `fn (o: Option<T>) compare(other: Option<T>) -> i64 where T.compare: …`, whose own clause is resolved recursively for `T = User`. The recursion is bounded by the type's depth and by the existing substitution budget (4096 nodes, depth 64, `bundled-interfaces.md`). For each concrete instantiation the emitter generates one top-level function, so no closure is needed. Inside a generic body, where `T`'s method arrives as an argument, the emitter generates a Go closure or JavaScript arrow that captures only that argument. Both are compiler-generated and capture only immutable method arguments, so Q10's closure-capture rules (spike-rows) are not engaged.

**This is a lookup, not instance search.** For each type and method name there is at most one candidate, in one place: the type's owner module. There are no overlapping instances, no backtracking, no global instance database, no defaulting, and no search through superclasses. The rule is the receiver-method rule design.md already admits for `u.compare(v)` (`docs/design.md:33`). A `where` clause defers the same lookup from the definition to the call site, where `T` is known. Effra therefore keeps the stance that "no implicit instance search runs"; §8 unit W6 rewords the two passages that use the phrase.

**Coherence.** There is one method per type and name, declared only by the owner. A receiver method on a type the module does not own is refused; the prototype's `orphan-method.ef` is "receiver method compare on i64: methods are declared only by the module that owns i64". This is the Go and MoonBit rule, and Roc's. Two maps over one key type therefore always hash alike.

**A second ordering is an explicit function, not a newtype.** Library functions come in pairs: `sortBy(xs, compare: fn(T, T) -> i64)` is plain (a), and `sort(xs) where T.compare` calls `sortBy(xs, T.compare)`. Haskell's `Down` and Rust's `Reverse` wrappers are unnecessary.

### 2.3 Predeclared types

`docs/design.md:33` says "there are no methods on primitives or type parameters". (b) amends the first half: a predeclared type's methods are declared only by its bundled owner module, and user code cannot add them (the orphan rule). The second half stands: a type parameter has no methods of its own; `T.m` names a method argument the `where` clause asked for.

- `i64`: `compare`, `equals` and `hash`. The prototype ships these as `bundled/primitives/i64-{compare,equals,hash}.ef`.
- `string` and `bool`: the same three. `string.compare` is defined on Unicode code points, so Go (UTF-8 bytes order the same as code points) and JavaScript (UTF-16 code units do not) agree. JavaScript's `<` on strings does not.
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

For a collection exported to TypeScript, the constructor takes the methods (`empty<K, V>() where K.hash, K.equals`), the value stores them, and later operations carry no `where` clause. A TypeScript caller then cannot insert with one hash and look up with another, and Effra code pays the resolution once per collection. The prototype passes the methods at each call (`insert`/`lookup … where K.hash, K.equals`). That is coherent inside Effra, where resolution is by type, but a TypeScript caller could mix two hashes and get missed lookups (never a crash). OCaml's `Map.Make` and ReScript's `Belt.Map` (the comparator is stored in the map value) capture the comparison the same way; Rust and MoonBit instead re-resolve a globally unique impl at each method, and Effect 4.0.1's `HashMap` uses the global `Equal`/`Hash` protocol (§5). It is a library choice that (b) allows and (a) shares; §9 Q2 records it.

### 2.7 derive under (b) (#100)

derive targets receiver methods, which are ordinary declarations. One structural rule covers equality, ordering and hashing without naming any of them:

> A derived method `m` on a record applies `m` to each field, in declaration order, through the field type's owner, and combines the results with a combinator the deriving module names. On an enum, it compares variant order first, then the fields.

A sketch:

```
derive User: compare by Order.lexicographic, equals by Bool.all, hash by Hash.mix
```

Each entry produces an ordinary `fn (u: User) compare(other: User) -> i64` that the `.d.mts` exports on the companion object. A generic record derives the method with the matching `where` clause (`fn (p: Pair<A, B>) compare(…) where A.compare, B.compare`). Field types without the method are refused with the EF160 message (§3.8). Display and inspect are further clients. JSON stays a closed profile word as #100 plans, because a codec carries wire policy (field names, bounds, transformations) and one type can have several (`bundled-interfaces.md:39`). Under (b), no `eq`, `order` or `hash` profile word is ever added.

### 2.8 Diagnostics

The prototype gives EF160 (`where_dispatch.go:57-86`) on both targets:

- missing: "Item has no method compare: sort2 requires T.compare: fn(T, T) -> i64; declare `fn (value: Item) compare(...)` beside Item, or derive it";
- no owner: "File has no method hash: keyed requires K.hash: fn(K) -> i64; File has no owner module that can declare methods". The same message is given for `Effect<i64>`;
- unpropagated: "T has no method compare: sort2 requires T.compare: fn(T, T) -> i64; add `where T.compare: ...` to the calling function's signature";
- wrong signature: "method Item.compare is fn(Item, Item) -> bool, but sort2 requires T.compare: fn(T, T) -> i64 with T = Item";
- orphan: "receiver method compare on i64: methods are declared only by the module that owns i64".

Production rewords the no-owner hint for a predeclared type whose owner exists but declares no such method (f64: "f64 declares no compare; use sortBy with F64.totalCompare"). It also anchors the diagnostic on the argument that bound `T`, not on the whole call.

## 3. Comparison of (a), (b) and (c)

| Dimension | (a) explicit dictionaries + derive | (b) `where`, owner-module resolution | (c) structural `comparable` / `Ordered` |
| --- | --- | --- | --- |
| TypeScript caller writes | `sort3(xs, userOrder)`, `empty(userIdHash)` | `sort3(xs, User.compare)`, `empty(UserId.hash, UserId.equals)` | `sort3(numbers)`; records: `sort3Func(xs, cmp)`; `empty<UserId, string>()` |
| `.d.mts` shape | dictionary-typed parameter | function-typed parameter (§2.5) | `T extends Ordered`; `Key<K>` recursive conditional type |
| `any` in `.d.mts` / `.mjs` | 0 / 0 | 0 / 0 | 0 / 0 |
| Fail-safe under the four routes (§3.2) | yes: nothing narrowed | yes: nothing narrowed | fails closed at the type level; A3/A4 reach a run-time encoder, which must refuse |
| Generic TypeScript caller | works | works | refused (TS2589, "excessively deep") even when it restates `Key<K>` |
| Go emission (§3.3) | `func sort3[T any](xs, order Order[T])` | `func sort3[T any](xs, compare func(T, T) int64)` | `func sort3[T cmp.Ordered](xs)`, `map[K]V` |
| Unrepresentable at compile time (§3.4) | none of the four cases | fn, handle and effect keys; f64 ordering | fn keys; handle and effect keys only if the predicate names them; NaN admitted |
| #100 (§3.5) | adds `order`, `equivalence`, `hash` profiles | adds none; derive targets methods | equality is built in; ordering falls back to (a) |
| Ladder (§6) | rung 1, plus three profile words | rung 2 | rung 4 |
| Checker complexity (§3.7) | none added | about 320 prototype lines; a lookup per method argument | structural predicate over every type; a run-time encoder for JavaScript |
| Missing-instance message (§3.8) | "incorrect argument count" | "Item has no method compare: sort2 requires …" | Go: "Item does not satisfy cmp.Ordered (…)" |

### 3.1 TypeScript projection, with tsc evidence

Every option has a projection, a runtime, a consumer, negatives and attacks under `benchmarks/where/ts/<option>/`, and `benchmarks/where/ts/run.sh` runs all of them. Each file is typechecked on its own with tsc 6.0.3 `--strict --module NodeNext --moduleResolution NodeNext --target ES2022 --lib ES2022,DOM,ESNext.Disposable`, and node 24.11.1 runs the consumer and the attacks. Every `@ts-expect-error` is opened by `tools/open-negatives.cjs` (copied unchanged from spike-rows probes r4), which prints the diagnostic it suppresses. `tools/count-any.cjs` counts AST `any` keywords. The receipt is `benchmarks/where/ts/runs/ts-view.txt`.

| Option | tsc exit, per file | Directives (each fires) | `any` keywords in `.d.mts`, `.mjs`, consumer | Consumer output |
| --- | --- | --- | --- | --- |
| a-dict | 0, 0, 0 | 7 negatives + 2 attacks | 0 | `abc 123 ada2,bob,none cba` |
| b-witness | 0, 0, 0 | 8 + 2 | 0 | `abc 123 ada2,bob,none abc` |
| b-struct | 0, 0, 0 | 5 | 0 | `abc ada2,bob,none` |
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

Each attack type-checks with no cast and no `any` unless a directive marks it refused; node then runs it and the run shows the outcome. A "throws" outcome fails before any value escapes. A "returns" outcome is listed with whether its static type is true.

| Route | (a) | (b) witness | (b) structural | (c) |
| --- | --- | --- | --- | --- |
| A1 method bivariance, witness or constraint slot | refused (invariant templates) | refused (invariant `Three<T>`) | **passes**, throws `y.compare is not a function` | refused for the factory and the sort slot |
| A1b key bivariance (`insert<UserId, string>` into a `UserId \| User` key slot) | passes, returns a key typed `UserId` with `org` undefined | same | constraint bivariance passes, throws | passes, the encoder throws `hash of unhashable type function` |
| A2 `interface Object` augmentation | refused (no argument supplied) | refused | **passes** for a function, a `bigint` and a function-keyed map; throws | **over-refuses**: valid record keys become `never` (fails closed) |
| A3 `never[]` rest | passes, throws `Cannot read properties of undefined` | passes, throws `compare is not a function` | passes, throws | passes, the encoder throws |
| A4 never filler | passes, throws | passes, throws | passes, throws | passes, the encoder throws |

- **A1b is not a `where` question.** It is TypeScript's method-parameter bivariance on a container's element parameter, and `baseline-effect` shows the same on a native `Map` (`native Map A1b: UserId.org is undefined`). Every option has it, and it involves no constraint.
- **(b) witness and (a)** never claim a narrowed type, so a bypass can only drop or falsify the argument itself, and the call then fails on first use. A production runtime may check `typeof compare === "function"` at the boundary and raise a typed defect instead of a `TypeError`; this costs one comparison per exported call (§9 Q3).
- **(b) structural is refuse-only.** A1 and A2 need no lying value, and there is no wider type to fall back to: `sort3` cannot run without `compare`. That is the M1 failure class, so the structural projection is rejected whatever Effra's surface syntax is.
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

### 3.4 What each option makes unrepresentable

"Compile time" means an Effra program is refused. The TypeScript column says what a TypeScript caller of the generated declarations can do.

| Case | (a) | (b) | (c) |
| --- | --- | --- | --- |
| Map keyed by a function | **Representable.** Anyone can write `hashing((f) => …, (a, b) => a === b)` for a function type (`a-dict/negatives.mts`, accepted). | **Compile time.** A function type has no owner module. The prototype refuses it with EF127 today, because callable type arguments are not admitted yet; after spike-rows unit 4a it is EF160. | **Compile time** for a direct key. For a function inside an enum key: on Go a run-time panic, `hash of unhashable type`; on JavaScript the encoder throws. |
| Map keyed by a resource handle (File, Latch, #96) | **Representable**, unless template admission refuses resources as type arguments (spike-rows unit 4a). | **Compile time.** File's owner declares no `hash`/`equals` (prototype: EF160 "File has no owner module that can declare methods"). Under #96, a resource's owner module decides; declaring `hash` on a resource would still meet unit 4a's resource admission. | **Compile time only if** the structural predicate names every handle type: a list keyed by name, or #96's resource marker. In TypeScript, the File check is structural (`K extends File`). |
| Map keyed by an effect value | **Representable.** | **Compile time.** `effra/effect` owns `Effect` (spike-rows Q1) and declares no `equals`/`hash` (prototype: EF160 on `Effect<i64>`). | **Compile time only if** the predicate refuses `Effect`. Effect lowers to a Go closure or interface, so through an enum key it is a run-time panic. |
| Ordering on f64 with NaN (#118) | **Representable.** An `Order<f64>` does whatever its author writes; Effect's `Order.Number` happens to be total (NaN first, `NaN,1,2`), Roc's and MoonBit's float orderings are not (§5). | **Compile time.** f64's owner module declares no `compare`; `sortBy(xs, F64.totalCompare)` is explicit (§2.3). Not built: Effra has no f64 yet (#118); the prototype shows the same refusal for a type without the method. | **Admitted.** f64 is `Ordered`. Go's `cmp.Compare(NaN, 1) = -1` sorts NaN first; JavaScript's `<` gives `2,NaN,1` unless the lowering re-implements `cmp.Compare`. A NaN map key is never found on Go and is found on JavaScript: the targets diverge. |
| TypeScript caller | Can pass any dictionary. | Can pass any function: the caller owns the argument (§2.5). | Refused directly; bypasses reach the run-time encoder. |

One prototype gap is not evidence for (b): a type parameter is treated as handle-free data, so `fn same<K: type>(key: K) -> K` admits a `File` argument (the main compiler refuses the generic function with EF127). Production depends on unit 4a's resource admission for whole-value type parameters.

### 3.5 Effect on #100

| Option | Profile words derive needs | Compiler-named profiles that go away |
| --- | --- | --- |
| (a) | `json`, plus `order`, `equivalence` and `hash`, each producing a value of a library record type the compiler must know | none; three are added |
| (b) | `json` only. Equality, ordering and hashing are derived as receiver methods by one structural rule that names no method (§2.7). | `order`, `equivalence` and `hash` are never introduced. #100's `json` profile is unchanged, because codecs stay explicit witnesses. |
| (c) | `json`, plus either an `order` profile or explicit functions for records | equality and hashing need no derive, because `comparable` is built into the compiler, which is the rung-4 construct the ladder counts against (c) |

### 3.6 Ladder placement

See §6 for the full table. (a) adds no type feature: rung 1, with three new profile words for derive. (b) is rung 2: the `where` clause meets every condition, and no branch names a method. (c) is rung 4: `comparable` is a predeclared structural constraint that names equality, and its TypeScript projection is a recursive conditional type.

### 3.7 Checker complexity

- **(a)** adds nothing to the checker: dictionaries are ordinary arguments of ordinary generic functions. derive must construct dictionary records of library types it knows by name.
- **(b)** adds about 320 lines in the prototype: 198 in `where_dispatch.go`, 64 in `syntax.go`, 29 in `semantic.go`, 5 in `field_occurrences.go`, 23 in `bundled.go` and 2 in `emit_go.go` (`git diff --stat 13e62ad4..30f80104`).
  - Resolution per method argument is one lookup. The prototype scans `program.Functions` (O(F)); production indexes methods by (nominal type, name), which is O(1).
  - The cost is linear in call sites, measured at about 0.27 ms per site, the same as (a) within noise (§4.2).
  - The only recursion is a generic application's conditional method (§2.2), bounded by the existing substitution budget.
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
- **Name-keyed branches:** `git diff 13e62ad4..30f80104 -- '*.go' | grep '^+.*"\(compare\|hash\|equals\|Eq\|Ord\|Order\|Hash\|derive\)"'` finds nothing.
- **End to end.** `examples/where/sort-and-key.ef` sorts with a three-element sorting network over `Three<T>`, with `sort2` propagating the constraint. It keys a fixed-capacity three-slot hash table `Table<K, V>` by a user record (`UserId.hash`/`UserId.equals`) and by `i64` through its bundled owner. `ef run` prints `abc 123 ada2,bob,none answer` **on Go and on JavaScript**.
  - Effra has no sequence type yet (recursive generic data is refused; typed buffers are #120), so both structures are fixed-size; the dispatch is what is measured.
- **Negatives** (`examples/where/negative/`), each refused on both targets: missing method, wrong signature, file key, effect key, orphan method and unpropagated give EF160; function key gives EF127. `TestWhereDispatchFixtures` passes. `30f80104` fixes a checker panic on a short generic call, found while capturing §3.8.
- **Not covered:** generic applications (`Option<User>`) are refused (§2.2), and the formatter prints `fn(u: User)` without the space (unit W1).

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
| **Elixir** `91ee75bb` | Structural term order for `<` and `==`. Semantic comparison is a module convention: `Enum.sort(dates, Date)` calls `Date.compare/2` (`lib/elixir/lib/enum.ex:3354-3356`). Protocols dispatch on the first argument's data type. | The module becomes `&(module.compare(&1, &2) != :gt)`, a function argument. Consolidation turns protocol dispatch into direct calls once the implementation set is closed (`protocol.ex:182-189`, `consolidate` at `:580`). | Protocols have no orphan rule (not verified at a line). | `@derive` through `__deriving__/2` (`protocol.ex:260-266`, `391-437`); `@fallback_to_any`. | Functions and references are ordered in term order (`kernel.ex:239`), and maps accept any key. |
| **ReScript** `30ce3698` | Polymorphic `compare` at run time, or `Belt.Id.MakeComparable`/`MakeHashable` (`packages/@rescript/belt/src/Belt_Id.resi:41-78`, `98-101`). | The comparator is stored in the collection: `type t = {cmp, data}` (`Belt_Map.res:24-25`, `76-78`). A branded id type makes maps with different comparators different types. Functors erase to plain records of functions. | The brand. | None built in. | Functions are refused at run time, `compare: functional value` (`packages/@rescript/runtime/Primitive_object.res:78`), except that `equal` returns early on `a === b` (`:230`). `Primitive_float.compare` orders NaN equal to NaN and below everything else. |
| **OCaml** `6471927d` | Polymorphic `compare`/`=`, or the functor `Map.Make(Ord)` (`stdlib/map.mli:49-54`). | Functor application captures `Ord.compare` in the module. | The generative module path. | None in the compiler (ppx outside). | Functions are refused at run time, `compare: functional value` (`runtime/compare.c:264`), except that `compare` returns early on physical equality. `Hashtbl.hash` hashes closures by code pointer (`runtime/hash.c:258-270`). An abstract block raises "compare: abstract value" (`compare.c:285`). `Float.compare` is total; `<` with NaN is false. |
| **OCaml modular implicits** (White, Bour and Yallop, arXiv:1512.01895) | Implicit module arguments resolved by type. | Not merged. OCaml 5.5.0 has modular *explicits* instead, `(module M : S) -> t[M]` (`Changes:924-926`): an explicit dictionary-passing precedent. | — | — | — |
| **Effect** 4.0.1 `460272d3` | `Order<A>` and `Equivalence<A>` are plain functions (`src/Order.ts:53-55`, `Equivalence.ts:60`); `Equal.symbol` and `Hash.symbol` are protocols on values (`Equal.ts:55`, `Hash.ts:32`). | Explicit values. Higher-kinded types use `TypeLambda` (`HKT.ts:140-145`, `Kind` at `:205`). | None. | Through `Data` and `Schema`, not the compiler. | Functions compare by reference (`Equal.ts:183`, `:247`). `Equal.equals(NaN, NaN)` is true. `Order.Number` orders NaN below everything else (`Order.ts:177-180`). `HashMap` uses the global `Hash.hash(key)`. 4.0.1 has no `SortedMap`. |

**Lowering paths, and what Effra takes:**

- **Roc and MoonBit** confirm owner-module resolution: methods declared beside the type, and constraints that name methods. Both monomorphize; neither passes arguments.
  - Effra keeps Roc's surface and resolution rule, and Roc's compile-time refusal of function equality.
  - Effra lowers like Haskell's dictionary translation and like Go's own GC-shape dictionaries: a hidden argument, with Go stenciling once per shape. SH1 already rules out frontend monomorphization (`docs/design.md:301`).
  - Effra rejects Roc's and MoonBit's silent float orderings (MoonBit's `compare` returns 0 against NaN) by giving f64 no `compare`, as Rust does with `PartialOrd`.
- **Rust** supplies the orphan rule and derive adding a bound per type parameter (§2.7's generic records). Its `total_cmp` is the model for `F64.totalCompare`.
- **Go** shows the cost of (c): `comparable` admits interface types that panic at run time, the "unchecked passthrough" Effra refuses. It also shows that a hidden `func` argument fits Go generics: the host already passes a dictionary per instantiation.
- **Gleam** keeps both targets simple by making ordering an explicit function, Effra's `sortBy`, and equality one runtime walk. Effra rejects `==` on every type: on JavaScript it compares functions by reference, and on Erlang it uses term order, so the targets differ for the cases this note makes unrepresentable.
- **Elixir**'s `Enum.sort(xs, Date)` passes a module as a dictionary value, but the caller names it; nothing resolves it from the type. That is (a) with module-shaped dictionaries. Its consolidation is the optimization (b) gets statically: with one implementation per type, every call is direct.
- **ReScript and OCaml** keep a map coherent by storing the comparator in the value (§2.6) and erase functors to records of functions. Both refuse functions only at run time, and only when the values are not physically equal: exactly the run-time-only refusal this decision avoids.
- **Effect** is the JavaScript representation the method argument lowers to: `Order` is a function. Its `TypeLambda` is an intersection-and-index encoding of higher-kinded types, `(F & { In… })["type"]`. It is a type-level language with no Go counterpart, and the ladder excludes HKTs, so Effra does not adopt it. A generic `Option<T>` comparison is expressed by a conditional method (§2.2) instead.

## 6. Ladder check

| Candidate | Describable without naming Eq/Order, two or more unrelated clients | Readable with Go's simplicity | Same meaning in TypeScript, no `any` | Erases into Go | Compiler branch keyed on a name | Rung |
| --- | --- | --- | --- | --- | --- | --- |
| (a) dictionaries as ordinary records/functions | Yes: any library value | Yes: Go `slices.SortFunc` | Yes: `Order<T>` record or function | Yes: a `func` parameter | derive needs `order`, `equivalence`, `hash` profile words that name library types | 1, plus three profile words |
| **(b) `where` clause with owner-module resolution** | **Yes:** "a type argument's owner module declares a method with this signature"; clients equality, ordering, hashing, display, zero values, generic arithmetic | **Yes:** a method in the signature, like Go's `interface{ Compare(T) int }` constraint, with no type sets | **Yes:** a function-typed parameter; nothing narrowed, so fail-safe; 0 `any` (§3.1, §3.2) | **Yes:** Go generics plus a `func` parameter; 0 extra stencils; explicit type arguments as F1 needs anyway (§4.2) | None (§4.1) | **2** |
| (b) structural TypeScript projection | Yes | Yes | **No:** refuse-only (§3.2) | Yes, but methods on values need classes on JavaScript | None | Rejected |
| (c) `comparable` / `Ordered` | **No:** `comparable` is equality, by name | Yes for Go readers | **No:** a recursive conditional type; generic callers refused (TS2589); a run-time encoder on JavaScript | Yes on Go; JavaScript needs a generated structural encoder | `comparable` is predeclared and structural | 4 |
| derive by field fold (§2.7) | Yes: equals, compare, hash, display; names none | Yes: one rule | Yes: ordinary functions on the companion object | Yes: ordinary functions | None | Part of #100's derive construct |

## 7. Decision record

**Decided 2026-10-12 (spike #116, revision 1).**

- **Chosen: (b).** Equality, ordering and hashing are receiver methods declared in each type's owner module. Generic code requires them with `where T.m: fn(…) -> R` and calls them as `T.m(…)`. The checker resolves each method at the call site through the type's owner (§2.2) and lowers it to a trailing function argument on Go and JavaScript, and to a named function-typed parameter in the `.d.mts` (§2.4, §2.5). Coherence is the orphan rule. f64 declares none of the three. derive targets receiver methods through one field-fold rule; JSON stays a #100 profile and codecs stay explicit witnesses.
- **Rejected: (a) as the mechanism.** Reasons kept:
  - it refuses none of the four hard cases at compile time (§3.4);
  - it allows incoherent dictionaries across one key type;
  - its missing-instance message names nothing (§3.8);
  - it adds three compiler-named profiles to #100 (§3.5).
  
  Its TypeScript projection is as good as (b)'s, and it is kept as the explicit `…By` form inside (b), so nothing (a) can express is lost.
- **Rejected: (c).** Reasons kept:
  - rung 4, and it names equality (§6);
  - it admits NaN, and the targets diverge on it (§3.4);
  - JavaScript needs a run-time structural encoder, because `Map` uses identity;
  - generic TypeScript callers are refused (TS2589);
  - handle and effect refusal depends on a name list;
  - records still need explicit comparisons.
- **Rejected: (b) with a structural TypeScript projection.** It is refuse-only under method bivariance and `Object` augmentation, with no wider fallback (§3.2).
- **Rejected: an Effect-style `TypeLambda` encoding** for generic `Order`/`Equal` over containers. It is an HKT encoding, which the ladder excludes, and it has no Go erasure (§5).

## 8. Follow-up units

Prerequisites already costed elsewhere:
- spike-rows unit 3: receiver methods, and F1 explicit type arguments with their Go emission;
- spike-rows unit 4a: Q9 whole-value type parameters, including callable type arguments and resource admission for type parameters.

The units below start after both; W2 and W6 can run beside W1.

| Unit | What lands | Tests and acceptance | Estimate |
| --- | --- | --- | --- |
| **W1. `where` clauses** | Parser, formatter (the receiver spacing) and LSP spans. Elaboration to method parameters. Resolution: cases 1-3 of §2.2, plus generic applications with generated instantiation functions and closures over method arguments. An index by (nominal type, name). EF160 family diagnostics anchored on the binding argument. Interface-summary transport of receiver methods and `where` clauses (schema bump, round trip, `TestEvidenceSchemaIsClassified`). Reachability of resolved methods. | The prototype's fixtures and negatives; a cross-module resolution test; `Option<User>` sorting; the grep test for name literals; Go and JavaScript equivalence; checker cost at N = 1k to 8k against this note's receipts. | L, 4.5 to 5.5 days |
| **W2. Predeclared owner modules** | Bundled owner modules for `i64`, `string` (code-point order on both targets) and `bool`, each with `compare`, `equals` and `hash`. f64 declares none (coordinated with #118). Orphan refusal for predeclared types. | Go/JavaScript pairs for string order on non-ASCII input; f64 refusal once #118 lands. | S, 1 day |
| **W3. TypeScript projection** | Method parameters named after their method; companion objects per type exposing owner methods (value plus interface merge); optional boundary check of method arguments (§9 Q3). | `benchmarks/where/ts/b-witness` promoted to a generated-consumer fixture: consumer, 8 negatives, attacks A1-A5 with their pinned outcomes; 0 `any`. | M, 1.5 to 2 days |
| **W4. derive by field fold** | The rule of §2.7 inside #100's `derive` construct: records in field order, enums by variant order, generic records with `where` clauses, library combinators named by the deriving module. | Derived `compare`/`equals`/`hash` equal hand-written ones on Go and JavaScript; a refusal for a field type without the method; no profile word for them. | M, 2.5 to 3 days, after #100's syntax |
| **W5. Collections** | `HashMap<K, V>` capturing `hash`/`equals` at construction (§2.6), and `sort`/`sortBy` over a sequence type. | Blocked on recursive generic data or a sequence type (#120). | M, 2 to 3 days once unblocked |
| **W6. Docs** | `docs/design.md:33` (predeclared types' owner modules) and `:300` (owner-module lookup is not instance search); `bundled-interfaces.md:39` (codecs stay explicit witnesses, ordering/equality do not); PRIOR_ART's Roc row open question closed; GLOSSARY "Where clause", "Method argument", "Owner module"; CHANGELOG. | Docs review. | S, 0.5 day |

Total: about 12 to 15 implementor-days plus review rounds, excluding the spike-rows prerequisites. W5 is not schedulable until a sequence type exists. The risk is in W1's generic-application case and in the summary transport.

## 9. Open questions

- **Q1. Do operators dispatch to methods?** Should `==` and `<` on a record call `equals`/`compare`? Recommendation: **no**. Operators stay predeclared on primitives, as in Go, where `==` never calls a method, and code calls `a.equals(b)`. This keeps the operator guard (`semantic.go:5123-5137`) a closed table.
- **Q2. Per-call or captured method arguments for collections?** Recommendation: **captured at construction** for every collection exported to TypeScript (§2.6). Per-call arguments stay the lowering for ordinary functions.
- **Q3. A boundary check of method arguments in exported JavaScript?** Recommendation: **yes, in W3**. One `typeof` check per exported call turns A3/A4's `TypeError` into a typed defect. Effra-internal calls skip it.
- **Q4. Should codecs get a default witness through `where`** (`where T.codec: …`) while keeping explicit witnesses for alternatives? Recommendation: **defer**. `bundled-interfaces.md:39`'s several-witnesses argument stands, and #100 lands first.
- **Q5. Which f64 names?** Recommendation: `totalCompare`, `ieeeEquals` and `equalsBits`, decided in #118. None is named `compare`, `equals` or `hash`.
