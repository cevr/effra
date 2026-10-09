# Compiler-known constructs audit

Status: research record, 2026-10-09, lane Z (`syntax-audit`). It inventories `main` at `abcbe44` and the current specs. It changes no compiler behavior. Each category-1 entry is a proposal until a ticket implements it with tests.

## Owner direction

Owner, 2026-10-09, verbatim: "i think in the case of machines, syntax is preferred over special cased library constructs" — "and generally i should say all of our langauge constructs".

Reading: whenever the compiler must know what a construct means, for checking, inference, plan facts, inspection or special lowering, the construct is syntax. It is not a library item that the compiler special-cases by identity or name. Library code that the compiler treats like any user code stays library. This sharpens the 2026-10-07 [visible-construct refinement](../design.md#language-design-principles) from "compiler-special behavior is spelled as a construct" to an audit obligation over everything the compiler already does. The tiebreak lives in [NORTH_STAR](../../NORTH_STAR.md#tiebreaks) and the terms live in [GLOSSARY](../../GLOSSARY.md).

## Classes

Every entry below has exactly one class.

| Class | Meaning | Disposition |
| --- | --- | --- |
| **S** | Syntax already: a keyword, operator, declaration form, postfix construct, or predeclared language identifier whose meaning the language defines. | Compliant. Listed so that tooling and the specification present it as language, and so that collisions are visible. |
| **1** | Compiler-known semantics reached through a library identity: a bundled or prelude type, member, service or provider that the checker, planner or tooling recognises by name or identity to grant language meaning. | Violates the direction. Either remove every compiler branch through a general mechanism (the item becomes class 3), or make the meaning syntax or a predeclared language identifier. |
| **2** | Implementation intrinsic: the compiler or runtime swaps in a native implementation for an ordinary declared signature, with no change to checking, rows, ownership or inspection. | Permitted. Must be documented, and the declared signature must be the single checking authority. |
| **3** | Ordinary library: checked exactly like user code. | Permitted. |
| **H** | Host-boundary adaptation: the `import go` construct recognises a foreign Go identity, such as `context.Context` or `io.Reader`, to adapt it. | Part of the S construct `import go`, owner-mandated on 2026-10-06. Keep it as a finite, versioned protocol table. It is not class 2, because it changes checked types and rows. |

A predeclared identifier counts as language under the Go model ([Go spec, predeclared identifiers](https://go.dev/ref/spec#Predeclared_identifiers)). The language specification defines it, it is not declared in importable library source, and no library can supply a replacement. A capability service such as `Files` or `Http` does not qualify even though it is currently predeclared, because the owner's "capabilities as layers" tiebreak makes those services ordinary library ([NORTH_STAR](../../NORTH_STAR.md#tiebreaks)).

## Key findings

1. **`.catch` and `.provide` are syntax, not library.** They are dedicated parser forms (`internal/compiler/syntax.go:1588-1600`) producing `catch`, `provide` and `provideLayer` nodes. Those nodes have dedicated checker cases (`semantic.go:4670-4720`, `layers.go:597`) and dedicated emitter cases (`emit.go:660-664`, `emit_go.go:1053-1062`). No library identity is involved. So the wording in the machine-provider ticket (#3), "the existing non-parser ownership of `.catch` and `.provide`", is factually wrong. NORTH_STAR's "planned migration that turns `.catch` and `.provide` ... into ordinary library operations" is superseded: under the 2026-10-09 direction they stay syntax (decision D1 below).
2. **The postfix words are reserved in practice but not declared as reserved.** `catch`, `provide`, `timeout` and `orFail` are parsed as constructs after any receiver. A record may still declare a field named `timeout`, and that field is then unreadable. A service may declare an operation named `timeout`, and calling it produces unrelated diagnostics. Both cases are reproduced in the probe section below.
3. **Current class-1 debt, all on capability or bundled identities:** the `Files`/`LiveFiles`/`File` ownership branches; the `Http.listen` callback policy and the name-based admission of `Http`; native-target restrictions keyed on service names; host absence adaptation keyed on `effra/data.Option`; the `Json.codec` derivation keyed on a bundled member with no body; and test-harness policy keyed on provider names.
4. **Planned class-1 designs:** the machine `Stay` rule "keyed on the bundled `Step` identity" (`docs/specs/state-machines.md:130`); the `Actor<P>` client projection and `actors.post` operation references (`docs/specs/actors.md:13,29`); and the admitted receiver-method plan that would move `.catch`, `.provide`, `.timeout` and `.orFail` into `effra/effect` methods while the checker still needs to know them (`docs/design.md:33`, `docs/specs/language-abstractions.md:147`).
5. **No general intrinsic mechanism exists on `main`.** The class-2 entries are hard-coded Go and JavaScript implementations inside the compiler. A future `intrinsic` declaration, which the numeric lane is drafting on another branch, must meet the class-2 contract in D4.

## Inventory

Line numbers are at `abcbe44` and identify evidence, not permanent anchors. All paths are under `internal/compiler/` unless stated otherwise.

### S: syntax (compliant)

| # | Construct | Where | Note |
| --- | --- | --- | --- |
| S1 | `run`, `fork`, `scope` | `syntax.go:1365-1382`; checker `semantic.go:4591-4660` | Prefix keywords, like Go's `go`. They are reserved in pipe heads (`syntax.go:1691`). |
| S2 | `effect fn`, `raises`, `uses`, `fail`, `match`, total `if` | parser declaration switch `syntax.go:640-860` | Language rows and control flow. |
| S3 | `service`, `impl`, `layer` with the contextual words `merge`, `replace`, `start`, `provides` | `syntax.go:738-848`, `layers.go` | Declaration forms. |
| S4 | `derive` | `syntax.go:850-855`, `codecs.go:54-85` | The keyword is syntax. Its format selector is entry 1-F. |
| S5 | `import go`, `Foreign` capability row, `GoResult`, `v0..vN`, `.value/.hasError/.error` | `imports.go:613,692`, `host_types.go:769-800` | The host-interop construct and its predeclared result shape. `Foreign` is entry 1-G. |
| S6 | `.catch<E>(h)`, `.provide<S>(p)`, `.provide(Layer)` | `syntax.go:1588-1600`; `semantic.go:4670-4720`; `layers.go:597` | Postfix constructs whose words are not declared reserved (finding 2, decision D1). |
| S7 | `.timeout(ms)` | `syntax.go:1574-1578`; `semantic.go:4621-4638` | Postfix construct. It adds the predeclared failure `Timeout` and requires the `Scheduler` capability (entry 1-G). Audit row 15 is UNDECIDED. |
| S8 | `.orFail()` | `syntax.go:1570-1573`; `semantic.go:4578-4590` | Postfix construct that adds `GoError`. Audit row 16 is LIBRARY, so it can be demoted once `GoResult` is ordinary data. |
| S9 | `.as<T>()` | `syntax.go:1579-1587`; `host_types.go:585` | Host assertion. It applies only when the word is followed by `<`, so a field named `as` still works. Its Option result is entry 1-D. |
| S10 | Fiber `.join()`, `.interrupt()`, `.cancel()` | `semantic.go:5092-5126`; `emit.go:632-636` | Operations on the predeclared `Fiber` type, dispatched only when the receiver is a fiber, so they do not collide with other members. Compliant under the Go model as long as `Fiber` stays predeclared. Audit row 11 is UNDECIDED. |
| S11 | Predeclared types and reserved names `string`, `bool`, `i64`, `bytes`, `void`, `never`, `Fiber`, `Effect`, `Scope`, `Exit`, `Cause`, `Context` | `semantic.go:2304`, `syntax.go:1039` | Language types. `File` and `Latch` belong to capabilities (entry 1-A). |
| S12 | `effect fn main` entry | `semantic.go:5081` | Go's `func main` convention. |
| S13 | `\|>` pipe | `syntax.go` postfix chain | Notation exception, one-to-one sugar. |

### 1: current compiler-known library identities

| # | Item | Where | What the compiler grants |
| --- | --- | --- | --- |
| 1-A | `Files.openRead`, `.provide<Files>(LiveFiles)`, and `File` as a managed handle | `semantic.go:4387-4393` (unknown ownership for `Files.openRead`); `semantic.go:4689-4693` (owned acquisition only for the literal `Files.openRead(...).provide<Files>(LiveFiles)` shape); `semantic.go:512,559` (`n.Name == "File"` treated as a managed handle) | Ownership and acquisition facts keyed on service, operation, provider and type names. This is construct-audit row 20 (SHRINK). |
| 1-B | `Http.listen` callback policy | `builtins.go:21-22`; `callables.go:564-569`; `builtins.go:59-61` (`HttpHandler`) | A `typed-failure-response` policy that absorbs a handler's failures and propagates its requirements. Only a builtin declaration can carry it. Row 20. |
| 1-C | `Http`/`LiveHttp` admission by reference | `builtins.go:77-106`, `builtins.go:119-150` (`builtin/http.ef`) | Prelude data, callback names and the service exist only when the program mentions the names `Http` or `LiveHttp`. |
| 1-D | `effra/data.Option` for host absence | `host_types.go:23-25`; `bundled.go:250-253` (loaded implicitly into every Go-importing program); `host_types.go:462,608,795`; `imports.go:656` | Nil pointers, native errors and `.as<T>()` results adapt to this specific bundled template. A library identity therefore carries a language guarantee (no nil). |
| 1-E | Native-only targets by service name | `semantic.go:4236-4238`, `semantic.go:4330-4332`, `layers.go:522-524` (`Files`, `Runtime`, `Foreign`) | Target restriction EF110 keyed on names instead of a declared target capability. |
| 1-F | `Json.codec` derivation and `bundledFailures` | `bundled.go:33` (a `Derivation` with no source); `codecs.go:100-106`; `codecs.go:122-135` (failures admitted on import) | A bundled member with no body that the checker treats as a closed structural deriver. Importing `effra/json` admits `JsonDecodeFailure` and `JsonEncodeFailure` from a Go table. |
| 1-G | Capabilities named by constructs: `Foreign` added to every Go call row; `Scheduler` required by `.timeout` | `imports.go:692`; `semantic.go:4638`; `reachability.go:853,925` | Syntax that depends on a capability-service identity, which is the lang-item shape Rust uses for `for` and `?`. |
| 1-H | Test harness policy by provider name | `testing.go:11` (implicit `Assertions`, `TestClock`, `TestScheduler`, `TestSync`); `testing.go:77-84` (`LiveClock`, `LiveScheduler`, `LiveEnv` require `--live`) | Test admission depends on provider names, not on declared provider properties. |

### 1: planned designs (not on `main`)

| # | Item | Where | Risk |
| --- | --- | --- | --- |
| 1-P1 | Machine `Stay` rule keyed on the bundled `Step` enum | `docs/specs/state-machines.md:76-79,130-141` | A bundled enum the checker recognises by identity. The statecharts design lane owns the decision syntax. This audit lists it as a dependency only. |
| 1-P2 | Receiver-method plan for `.catch`, `.provide`, `.timeout` and `.orFail` | `docs/design.md:33`; `docs/specs/language-abstractions.md:57-61,147` | Moving forms the checker must still know into `effra/effect` methods recreates exactly the failure mode. Superseded by D1. |
| 1-P3 | `Actor<P>` client projection, `actors.post` operation references and spawn row charging | `docs/specs/actors.md:13,29`; audit row 29 | A bundled `Actor` type and `actors` functions whose typing the compiler derives. |
| 1-P4 | Generalized `run` binding and Result `?` | `docs/research/generalized-run-binding.md:42`; audit row 33 | A binder protocol keyed on `Option` or `Result` identity would be Rust's `Try` lang item. The current recommendation (explicit `flatMap`) has no such risk. Guard only. |

### 2: implementation intrinsics

| # | Item | Where |
| --- | --- | --- |
| 2-A | Native Go bodies of the builtin providers | `emit_go.go:307-320` (`builtinGoProviders`) |
| 2-B | Test-scheduler injection into `TestClock` and `TestScheduler`, and the scheduler `driver` field | `emit_go.go:324-326,333,973-978`; `layer_emit.go:58` |
| 2-C | Runtime helpers that lower constructs | `emit_go.go:294-297` (`efCatch`, `efScoped`, `efTimeout`, `efFork`); `js_prelude.go:40-76`; `internal/compiler/prelude/*.mjs` |
| 2-D | Runtime-module retention for predeclared types and constructs | `reachability.go:776-788` (`File` to the files module, `Latch` to the sync module); `reachability.go:853,925` |

These change neither checking nor rows nor inspection. Two of them are coupled to class-1 names: 2-B is keyed on provider names, and 2-D on the `File` name. They move with the fixes for 1-A and 1-H.

### H: host-boundary adaptation table

| # | Foreign identity | Where | Adaptation |
| --- | --- | --- | --- |
| H1 | `context.Context` as the first parameter | `imports.go:632-635` | Forwarded from the managed fiber. It is required for cooperative cancellation (`imports.go:627`). |
| H2 | `io.Reader.Read`, `io.Writer.Write` | `host_types.go:539-558,1050` | Checked I/O contracts. A short write becomes `io.ErrShortWrite`. |
| H3 | Universe `error`, native nil | `imports.go:613`; `host_types.go:75,111` | `GoResult` with a retained error, and absence through 1-D. |

### 3: ordinary library

`effra/functions` (`call`, `identity`, `forwardFile`, `suffixed`), `effra/conversions` (`Codec`, `witness`), `effra/data.Result`, `effra/constants`, and `effra/data.Option` apart from host adaptation (`bundled.go:29-32`). None of them has a name branch in the checker. The capability services (`Console`, `Clock`, `Scheduler`, `Sync`, `Env`, `Runtime`, `Assert`, `Files`, `Http`) are intended to be class 3 under "capabilities as layers". Their declarations live in Go (`builtins.go:16-50`) rather than in `.ef`, but the declarations themselves grant nothing. Only the branches listed under class 1 do.

## Proposals for class-1 entries

Priorities: P1 means decide or fix before more code depends on it. P2 means fix within the owning feature lane. P3 means cleanup.

**1-A (P1). Ownership without capability names.** Replace the three name branches with a declared property of an operation result. One option is a `resource` modifier on an opaque type, so that `File` is a resource and `Files.openRead` returns an owned resource unless its provider declares the result borrowed. Then any provider, and any host resource (#34), gets the same checking. Prior art: Rust grants ownership through ordinary types plus the `Drop` lang item. Swift's `~Copyable` is syntax. Go and Elixir have no static resource tracking. MoonBit has none either. Migration impact: the checker and ownership tests (11 Go test files mention `LiveFiles`), three `.ef` files, inspection of ownership origins, and the conformance lifecycle rows. Formatter and LSP are unaffected unless a modifier is added. Depends on #34 and #65.

**1-B and 1-C (P2). Http as an ordinary bundled service.** The typed-failure-response absorption needs either the trusted host-body `extern` unit already named in [language abstractions](../specs/language-abstractions.md#row-parameters-on-service-operations-and-implementation-methods) or general callback-row absorption. With that in place, `Http` moves to a bundled `.ef` interface and loses name-based admission. This is de-specialization, not syntax: the NORTH_STAR tiebreak "capabilities as layers" already decides it. Prior art: Go's `net/http` and Effect's `HttpServer` are pure library, and neither compiler knows HTTP. Elixir's Plug and Gleam's wisp are the same. Migration impact: `builtin/http.ef`, 4 Go test files, 2 `.ef` files, and TS declaration emission. Depends on #51 and #15.

**1-D (P2). Make `Option` a predeclared language type.** Absence is a language invariant (NORTH_STAR: no nil), and host adaptation must name it, so it belongs in the language prelude, alongside the predeclared failures, rather than in `effra/data`. Keep `Some` and `None` construction and matching unchanged. `effra/data.Option` either becomes an ordinary alias or is removed. `T?` sugar is rejected for now, to keep one Go-like spelling; it would be a notation-exception candidate. Prior art: MoonBit's `Option` is a compiler builtin (`moonbit-compiler@d4ada10d` `src/basic_type_path.ml:417`, `src/builtin.ml:58`) with `T?` syntax (`src/parsing_syntax.ml:1154`; docs `fundamentals.md:361`). Gleam keeps compiler-known types in a compiler-defined prelude (`gleam@52e735c8` `compiler-core/src/type_/prelude.rs:18-26`, which includes `Result`) and leaves `Option` as plain library because its compiler never needs it. Rust's `Option` is a lang item, which is the pattern rejected here. Kotlin uses `T?` syntax. Elixir uses the `nil` atom. Migration impact: 23 Go test files and 3 `.ef` files import `effra/data`; absence diagnostics help text (`absent_syntax.go:59`); bundled interface hashes and producer identity; documentation. Depends on #30 and #35 (both closed) and #32.

**1-E (P2). Declared target capability.** A provider or service declares the targets it is implemented for, as data the checker reads for every declaration alike. The checker then stops keying EF110 on names. Prior art: Gleam's `@external(erlang, ...)` and `@external(javascript, ...)` per-target declarations; MoonBit's per-backend `extern` bodies. Migration impact: three branches and the EF110 tests. Formatter changes only if new syntax is chosen. Depends on #15.

**1-F (P3). Derive profiles belong to the derive construct.** Spell the format as part of `derive`, for example `derive userJson = json<User>(maxBodyBytes: 4096, maxDepth: 2)` with `json` as a closed profile word. Its failures become predeclared failures of that profile. Then no bundled member without a body and no import-triggered failure table remain. Prior art: Rust, MoonBit (`derive(ToJson)`, `moonbit-docs@8d9f3ba2` `derive.md:121`) and Haskell address builtin derivers by trait path, but there the trait is an ordinary library interface whose implementation the compiler writes. Effra has no traits, so `Json.codec` names nothing ordinary. Migration impact: the parser, formatter, LSP definition target, 10 Go test files, 1 `.ef` file, and the codec documentation. Depends on #49 and #55.

**1-G (P3). Predeclared capabilities of constructs.** Specify `Foreign` (the trust row of `import go`) and the timer capability that `.timeout` requires as predeclared language contracts with ordinary layer providers. This follows Rust, where syntax requires a lang-item protocol, and Go's predeclared `error`. Then their names cannot be shadowed or redefined. Migration impact: documentation, plus a reserved-name diagnostic for `Foreign`. No change to source programs.

**1-H (P3). Test harness as a declared layer.** The runner provides a bundled `TestHarness` layer. Live providers declare a host-capability property, so `--live` checks a property instead of names. Prior art: Effect's `TestServices` layer and `@effect/vitest` (library); Go's `go test` passes `*testing.T` by signature. Migration impact: `testing.go`, the harness emitters (`layer_emit.go:58`) and the test documentation.

**1-P1 (dependency).** The statecharts design lane decides machine decision syntax, such as decision forms inside the `machine` declaration instead of a bundled `Step` enum that the checker recognises. This audit only requires that the decision not be a checker rule keyed on a bundled identity.

**1-P2 (decided by D1).** See D1.

**1-P3 (P1 design gate).** Before #11 or #57 implements `Actor<P>`, choose one of two routes. Either use a general mechanism that any library can use, such as Gleam OTP's `Subject(msg)`, which is ordinary generics with no compiler projection, so the item becomes class 3. Or use syntax for the parts the compiler must derive, for example a predeclared `actor P` handle type, plus a `spawn` form and a syntactic operation reference for `post`. Elixir makes `receive` a special form (`elixir@91ee75bb` `lib/elixir/lib/kernel/special_forms.ex:2401`), while `send` and `spawn` are ordinary `Kernel` functions over BIFs (`kernel.ex:1207,1247`), that is, class-2 intrinsics. The direction rules out a bundled `Actor` type with compiler-derived methods.

## Decisions

**D1. `.catch`, `.provide` and `.timeout` stay syntax and are made visibly reserved.** Construct-audit rows 12-14 change from SHRINK to KEEP (syntax). Row 15 stays UNDECIDED but remains syntax while its timeout owner needs checker knowledge. Eliminating a checked row label is a core effect-system operation, and every compared language with checked error or effect rows spells it as syntax:

- MoonBit: `raise` and `try`/`catch` (`moonbit-docs@8d9f3ba2` `error-handling.md:75,161`).
- Elixir: `try` and `receive` are special forms (`special_forms.ex:2343,2401`), which "cannot be overridden by the developer" (`special_forms.ex:7-8`).
- Kotlin: `try`/`catch`.
- Swift: typed throws with `do`/`catch`.
- Koka: handlers.

The only library precedent is Effect's `catchTag` (`effect@460272d` `packages/effect/src/Effect.ts:2743`) and `provideService` (`Effect.ts:6317`). They depend on TypeScript's general type-level computation, which NORTH_STAR rejects as a "type-level puzzle". A future finite row-difference constraint would extend these constructs to abstract rows inside helpers. It would not move them into a library.

The spelling stays postfix, like Rust's reserved `.await` ([Rust reference, await expressions](https://doc.rust-lang.org/reference/expressions/await-expr.html)), so chaining is preserved. The words `catch`, `provide`, `timeout`, `orFail` and the planned `recover` become declared reserved member words: declaring a field, operation or method with one of these names is a diagnostic. That removes the pipe exception in [language abstractions](../specs/language-abstractions.md#pipe-operator).

`.orFail()` keeps its LIBRARY verdict. It is demoted only when `GoResult` is ordinary data and no checker branch remains, and it stays reserved until then.

Rejected alternatives:

- Library methods in `effra/effect`. This is the failure mode: a lang item.
- Prefix keywords. They break chaining and would churn 23 `.ef` files and 63 Go test files for no new guarantee.
- A general row-difference type operator, added only so that these forms can be library. It has one consumer family and is a type-level puzzle.

**D2. Classes 1 and 3 are separated by compiler branches, not by file location.** An item becomes library by losing every compiler branch, never by moving its declaration into a bundled module. Conversely, a declaration in `builtins.go` with no branches is still ordinary library.

**D3. Predeclared identifiers count as syntax (the Go model).** This keeps `Fiber`, `GoResult` and the predeclared failures compliant. It also gives 1-D and 1-G a remedy that needs no new keywords. Capability services are excluded by NORTH_STAR.

**D4. Class-2 contract.** An implementation intrinsic has an ordinary declared signature in library source. That signature is the only checking, row, ownership and inspection authority. The intrinsic swaps the implementation per target, and an inspection or graph view may label it as native. If removing the intrinsic would change any diagnostic or checked fact, the item is class 1. Prior art: MoonBit core bodies written as `= "%arrayview.len"` (`core@e96ede8b` `builtin/arrayview.mbt:62`; 276 such bodies in `builtin/`); Go's SSA intrinsics for `math.sqrt` and `sync/atomic` (go1.27.0 `src/cmd/compile/internal/ssagen/intrinsics.go:739,1299`); Kotlin's `kotlin.coroutines.intrinsics`. MoonBit's `#callsite(autofill(loc))` on `fail` (`builtin/assert.mbt:35`) changes call checking, so it would be class 1 here.

## Prior art summary

| Source | Syntax for | Library-recognised identity | Lesson for Effra |
| --- | --- | --- | --- |
| Rust | `?`, `for`, `async`/`.await`, `match` | Lang items declared with `#[lang]` in `core`, for example `Option`'s `Some`/`None`, `Try`, `IntoIterator::into_iter`, `Future` and `Drop` | Even Rust keeps syntax for the constructs. Its lang items exist only because traits are its general mechanism. Effra has no traits, so a lang item would be bare identity special-casing. |
| Go | `go`, `select`, `chan`, `<-`, `defer`, `map` | None. Predeclared `make`, `len`, `append`, `error`; SSA intrinsics for `math` and `sync/atomic` | This is the model for D3 and D4: language features are syntax or predeclared, libraries such as `context` and `sync` are ordinary, and intrinsics only swap implementations. |
| Kotlin | `suspend` | `kotlin.coroutines.intrinsics`; `launch`, `async` and `CoroutineScope` are library | One syntax point (the CPS transform), primitives as intrinsics, everything else library. |
| Gleam | `use`, `let assert`, `case` | None in the stdlib; compiler-defined prelude (`Result`, `List`, `Nil`) | Compiler-known types live in a language prelude. `use` is one-to-one callback sugar, like the notation exception. |
| Elixir/OTP | `Kernel.SpecialForms` (`case`, `cond`, `try`, `receive`, `for`, `with`, `fn`) | None. `\|>` is an ordinary `Kernel` macro (`kernel.ex:4509`); `send` and `spawn` delegate to BIFs | Special forms are fixed and documented as language. Actor messaging is library over intrinsics, apart from `receive`. |
| MoonBit | `raise`, `try`/`catch`/`noraise`, `async`, `T?`, `derive(...)` | Builtin `Option`; `%` intrinsic bodies; `#callsite` attribute | This supports D1 (typed errors as syntax), 1-D (builtin absence) and D4 (intrinsics with declared signatures). |
| TypeScript/Effect | Generators (`function*`, `yield*`) only | None. `Effect.gen` (`Effect.ts:1431`) is library over general generators | Library-only works when the host language's general mechanisms carry the typing. Effra's do not, so the owner direction applies. |

## Probe evidence

Run with `ef check` built from `abcbe44`. The files are kept in the lane scratch area, not in the repository.

```ef
record Config { timeout: i64, }
fn read(c: Config) -> i64 { c.timeout }
```

Result: `EF002 expected (, found }`. The declared field cannot be read.

```ef
service Jobs { effect fn cancel(id: i64) -> void
               effect fn timeout(id: i64) -> void }
effect fn stop() -> void uses { Jobs } { run Jobs.cancel(1)
                                         run Jobs.timeout(1) }
```

Result: `Jobs.cancel` checks. `Jobs.timeout` reports `EF102 unknown value Jobs` and `EF106 timeout requires an Effect and an i64 millisecond duration`. The fiber operation `cancel` does not collide with the service operation, because dispatch is type-directed (S10).

## Documents reconciled in this change

- NORTH_STAR: the owner direction and tiebreak were added, and the superseded `.catch`/`.provide` migration clause in the machine paragraph was replaced.
- `docs/design.md`: the visible-construct refinement now cites the 2026-10-09 direction, and the receiver-method member no longer moves compiler-known forms into `effra/effect`.
- `docs/specs/language-abstractions.md`: construct-status rows 12-16, 20, 28 and 29 and the pipe exception wording were updated. These forms are called "postfix constructs", not "intrinsics".
- `docs/specs/machine-provider-contract.md`: the incorrect `.catch`/`.provide` analogy was corrected.
- PRIOR_ARTS: comparison section added. GLOSSARY: the new terms were added.
- Not edited, because other owners hold them: `docs/wayfinder/**` (the map ticket for #3 still says "non-parser ownership"), `docs/specs/state-machines.md` (1-P1, statecharts lane) and `docs/specs/actors.md` (1-P3).
