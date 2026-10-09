# Compiler-known constructs audit

Status: research record, 2026-10-09, lane Z (`syntax-audit`). It inventories `main` at `515d49b` and the current specs. It changes no compiler behavior. Every class-1 entry, and the D1 reservation, is a proposal until a ticket implements it with tests.

## Owner direction

Owner, 2026-10-09, verbatim: "i think in the case of machines, syntax is preferred over special cased library constructs" — "and generally i should say all of our langauge constructs".

Reading: whenever the compiler must know what a construct means, for checking, inference, plan facts, inspection or special lowering, the construct is syntax. It is not a library item that the compiler special-cases by identity or name. Library code that the compiler treats like any user code stays library. This sharpens the 2026-10-07 [visible-construct refinement](../design.md#language-design-principles) from "compiler-special behavior is spelled as a construct" to an audit obligation over everything the compiler already does. The tiebreak lives in [NORTH_STAR](../../NORTH_STAR.md#tiebreaks) and the terms live in [GLOSSARY](../../GLOSSARY.md).

## Classes and fact kinds

The audit separates two kinds of compiler fact:

- **Semantic facts** are diagnostics, checked types, failure and service rows, ownership and the checked contracts that CLI, MCP and LSP report.
- **Backend facts** are retention and emission details: retained runtime modules (`runtimeModules` in application inspection), required Go imports, emitted helpers and target code.

The direction governs semantic facts. A backend fact may be computed by a construct's implementation or by an intrinsic without making anything compiler-known.

Every entry below has exactly one class.

| Class | Meaning | Disposition |
| --- | --- | --- |
| **S** | Syntax already: a keyword, operator, declaration form, postfix construct, or predeclared language identifier whose meaning the language defines. This includes the construct's own implementation: lowering helpers, runtime chunks and backend retention. The language specification of the construct, not a library signature, is the authority for these. | Compliant. Listed so that tooling and the specification present it as language, and so that collisions are visible. |
| **1** | Compiler-known semantics reached through a library identity: a bundled or prelude type, member, service or provider that the checker, planner or tooling recognises by name or identity to produce a semantic fact. | Violates the direction. Either remove every semantic branch through a general mechanism (the item becomes class 3), or make the meaning syntax, a predeclared language identifier or a language protocol. |
| **2** | Library intrinsic: the compiler substitutes a target implementation for an ordinary declared library signature. The signature is the only source of semantic facts. The intrinsic may add backend facts. | Permitted. Must be documented (D4). |
| **3** | Ordinary library: checked exactly like user code. | Permitted. |
| **H** | Host-boundary adaptation: the `import go` construct recognises a foreign Go identity, such as `context.Context` or `io.Reader`, to adapt it. | Part of the S construct `import go`, owner-mandated on 2026-10-06. Keep it as a finite, versioned protocol table. It is not class 2, because it changes checked types and rows. |

**Predeclared identifiers.** A predeclared identifier counts as language under the Go model ([Go spec, predeclared identifiers](https://go.dev/ref/spec#Predeclared_identifiers)). The language specification defines it, it is not declared in importable library source, and no library can supply a replacement.

**Language protocols versus capability services.** A **language protocol** is a nominal row label whose charging or discharge a construct's own checking rule defines. The compiler must know its meaning to state that construct's row or lifetime contract, and no ordinary library operation is what charges it. An **ordinary capability service** is a service whose label enters a row only because a declared operation, or a declared requirement, names it. Its providers are layers, and the compiler needs no knowledge of it beyond ordinary service checking.

The test: if the construct's checked contract could be written as ordinary code that calls the service's declared operations, the service is a capability. Language protocols are predeclared. Capability services are excluded from the predeclared category by NORTH_STAR's "capabilities as layers" tiebreak, even when the compiler currently declares them (as with `Files`, `Http` and `Scheduler`).

## Key findings

1. **`.catch` and `.provide` are syntax, not library.**
   - They are dedicated parser forms (`syntax.go:1599-1611`) producing `catch`, `provide` and `provideLayer` nodes, with dedicated checker cases (`semantic.go:4676-4730`, `layers.go:597`) and emitter cases (`emit.go:662-666`, `emit_go.go:1058-1067`). No library identity is involved.
   - So the machine-provider ticket (#3) wording, "the existing non-parser ownership of `.catch` and `.provide`", is factually wrong.
   - NORTH_STAR's "planned migration that turns `.catch` and `.provide` ... into ordinary library operations" is superseded: they stay syntax (decision D1).
2. **The postfix words are claimed by the parser but not declared reserved.** `catch`, `provide`, `timeout` and `orFail` are parsed as constructs after any receiver. A record may declare a field named `timeout`, and that field cannot be read. A service may declare an operation named `timeout`, and calling it produces unrelated diagnostics (see probes). D1 proposes the reservation; it is not in force.
3. **Current class-1 debt is all on capability, bundled or prelude identities:**
   - `Files`/`LiveFiles` ownership;
   - `File`/`Latch` type-name admission and handle tracking;
   - the `Http.listen` callback policy, the name-based admission of `Http`, and the HTTP provider adapter;
   - native-target restrictions keyed on service names;
   - host absence keyed on `effra/data.Option`;
   - the `Json.codec` derivation, a bundled member with no body;
   - the `Scheduler` dependency of `.timeout`;
   - test-harness policy keyed on provider names.
4. **Planned class-1 designs:**
   - the machine `Stay` rule "keyed on the bundled `Step` identity" (`docs/specs/state-machines.md:130`);
   - the `Actor<P>` projection and `actors.post` operation references (`docs/specs/actors.md:13,29`);
   - the earlier receiver-method plan to move `.catch`, `.provide`, `.timeout` and `.orFail` into `effra/effect` methods.
5. **`main` now has one general library-intrinsic mechanism, and it conforms to D4.** The `effra/i64` members `format` and `parse` have real `.ef` signatures with compiler-owned bodies (2-L1). The remaining former "intrinsics" are either implementations of syntax (class S) or providers of capability services (2-L2).

## Inventory

Line numbers are at `515d49b` and identify evidence, not permanent anchors. All paths are under `internal/compiler/` unless stated otherwise.

### S: syntax (compliant)

| # | Construct | Where | Note |
| --- | --- | --- | --- |
| S1 | `run`, `fork`, `scope` | `syntax.go:1376-1393`; `semantic.go:4595-4672` | Prefix keywords, like Go's `go`. They are reserved in pipe heads (`syntax.go:1700`). |
| S2 | `effect fn`, `raises`, `uses`, `fail`, `match`, total `if` | declaration switch `syntax.go:644-865` | Language rows and control flow. |
| S3 | `service`, `impl`, `layer` with the contextual words `merge`, `replace`, `start`, `provides` | `syntax.go:742-852`, `layers.go` | Declaration forms. |
| S4 | `derive` | `syntax.go:854-859`, `codecs.go:54-85` | The keyword is syntax. Its format selector is 1-F. |
| S5 | `import go`, `GoResult`, `v0..vN`, `.value/.hasError/.error` | `imports.go:613`, `host_types.go:769-800` | The host-interop construct and its predeclared result shape. The `Foreign` row it charges is 1-G. |
| S6 | `.catch<E>(h)`, `.provide<S>(p)`, `.provide(Layer)` | `syntax.go:1599-1611`; `semantic.go:4676-4730`; `layers.go:597` | Postfix constructs. Reservation is proposed (D1). |
| S7 | `.timeout(ms)` | `syntax.go:1585-1589`; `semantic.go:4625-4642` | Postfix construct. It adds the predeclared failure `Timeout`, and its `Scheduler` requirement is 1-G. Audit row 15 is UNDECIDED. |
| S8 | `.orFail()` | `syntax.go:1581-1584`; `semantic.go:4582-4594` | Postfix construct that adds `GoError`. Audit row 16 is LIBRARY, so it can be demoted once `GoResult` is ordinary data. |
| S9 | `.as<T>()` | `syntax.go:1590-1598`; `host_types.go:585` | Host assertion. It applies only when followed by `<`, so a field named `as` still works. Its Option result is 1-D. |
| S10 | Fiber `.join()`, `.interrupt()`, `.cancel()` | `semantic.go:5102-5136`; `emit.go:634-637` | Not a parser form. The checker dispatches on member name only when the receiver is a predeclared `Fiber`, so these never collide with other members. Compliant while `Fiber` stays predeclared. Audit row 11 is UNDECIDED. |
| S11a | Supported predeclared types `string`, `bool`, `i64`, `bytes`, `void`, `never`, `Fiber` | `semantic.go:2091,2308` | Language types. |
| S11b | Reserved, unsupported names `Effect`, `Scope`, `Exit`, `Cause`, `Context` | `semantic.go:2308`; `syntax.go:1050` (typed `Effect<...>` refused) | They can't be declared and aren't usable types. They hold space for future language types. |
| S12 | `effect fn main` entry | `semantic.go:5091` | Go's `func main` convention. |
| S13 | `\|>` pipe | `syntax.go` postfix chain | Notation exception, one-to-one sugar. |
| S14 | Implementation of these constructs | `emit_go.go:294-297` (`efCatch`, `efScoped`, `efTimeout`, `efFork`); `js_prelude.go:40-78` and `prelude/*.mjs`; `reachability.go:776-788` (`typeRuntimeModule`, applied at `reachability.go:737` and reported as `runtimeModules` through `reachability.go:274`) | Backend facts of S constructs. `typeRuntimeModule`'s `File` and `Latch` rows are keyed on capability type names, so they move with 1-A. |

### 1: current compiler-known library identities

| # | Item | Where | What the compiler grants |
| --- | --- | --- | --- |
| 1-A | `Files.openRead`, `.provide<Files>(LiveFiles)`, and `File`/`Latch` as compiler-known opaque types | `semantic.go:4391-4397` (unknown ownership for `Files.openRead`); `semantic.go:4695-4699` (owned acquisition only for the literal `Files.openRead(...).provide<Files>(LiveFiles)` shape); `semantic.go:516,563` (`n.Name == "File"` is a managed handle); `semantic.go:2091` (`File` and `Latch` become opaque type references); `semantic.go:2308` (reserved data names); `semantic.go:3171` (canonical opaque admission accepts only these names); `templates.go:351` (generic arguments accept opaque types only when named `File` or `Latch`) | Ownership, acquisition, type admission and generic admission keyed on service, operation, provider and type names. Construct-audit row 20 (SHRINK). |
| 1-B | `Http.listen` callback policy | `builtins.go:21-22`; `callables.go:564-569`; `builtins.go:59-61` (`HttpHandler`) | A `typed-failure-response` policy that absorbs handler failures and propagates requirements. Only a builtin declaration can carry it. Row 20. |
| 1-C | `Http`/`LiveHttp` admission and adapter | `builtins.go:77-106`; `builtins.go:119-150` (`builtin/http.ef`); `emit_http.go:8-20` (`goHTTPProvider` hard-codes the data field and variant names) | Prelude data, callback names and the service exist only when a program mentions `Http` or `LiveHttp`. The emitter knows the data shape. |
| 1-D | `effra/data.Option` for host absence | `host_types.go:23-25`; `bundled.go:254-257` (loaded implicitly into every Go-importing program); `host_types.go:462,608,795`; `imports.go:656` | Nil pointers, native errors and `.as<T>()` results adapt to this bundled template. A library identity carries the language's no-nil guarantee. |
| 1-E | Native-only targets by service name | `semantic.go:4240-4242`, `semantic.go:4334-4336`, `layers.go:522-524` (`Files`, `Runtime`, `Foreign`) | Target restriction EF110 keyed on names instead of a declared target capability. |
| 1-F | `Json.codec` derivation and its failure table | `bundled.go:36` (a `Derivation` with no source); `codecs.go:100-106`; `codecs.go:122-135` (failures admitted on import) | A bundled member with no body that the checker treats as a closed structural deriver. Importing `effra/json` admits `JsonDecodeFailure` and `JsonEncodeFailure` from a Go table. |
| 1-G | Rows charged by constructs: `Foreign` on every Go call; `Scheduler` required by `.timeout` | `imports.go:692`; `semantic.go:4642`; `reachability.go:855,927` | Under the definition above, `Foreign` is a language protocol that the compiler currently treats as a prelude service. `Scheduler` is a capability service named by syntax. |
| 1-H | Test harness policy by provider name | `testing.go:11` (implicit `Assertions`, `TestClock`, `TestScheduler`, `TestSync`); `testing.go:77-84` (`LiveClock`, `LiveScheduler`, `LiveEnv` require `--live`); `layer_emit.go:58`, `emit_go.go:324-326,333,978-983` (scheduler injection by provider name) | Test admission and harness wiring depend on provider names, not on declared provider properties. |

### 1: planned designs (not on `main`)

| # | Item | Where | Risk |
| --- | --- | --- | --- |
| 1-P1 | Machine `Stay` rule keyed on the bundled `Step` enum | `docs/specs/state-machines.md:76-79,130-141` | A bundled enum the checker recognises by identity. The statecharts design lane owns the decision syntax; this audit lists it as a dependency only. |
| 1-P2 | Receiver-method plan for `.catch`, `.provide`, `.timeout` and `.orFail` | earlier `docs/design.md:33`; `docs/specs/language-abstractions.md:57-61,148` | Moving forms the checker must still know into `effra/effect` methods recreates the failure mode. Superseded by D1. |
| 1-P3 | `Actor<P>` client projection, `actors.post` operation references and spawn row charging | `docs/specs/actors.md:13,29`; audit row 29 | A bundled `Actor` type and `actors` functions whose typing the compiler derives. |
| 1-P4 | Generalized `run` binding and Result `?` | `docs/research/generalized-run-binding.md:42`; audit row 33 | A binder protocol keyed on `Option` or `Result` identity would be Rust's `Try` lang item. The current recommendation (explicit `flatMap`) carries no such risk. Guard only. |

### 2: library intrinsics

| # | Item | Where | Note |
| --- | --- | --- | --- |
| 2-L1 | `effra/i64` `format` and `parse` | `bundled/i64/format.ef`, `bundled/i64/parse.ef`; `bundled.go:37`; `i64_text.go:33-47` (body marker replaced by an `intrinsic` node), `i64_text.go:52-75` (body checked against the declared signature); `interface_admission.go:129` | Conforms to D4: callers see only the declared signatures, and the compiler-owned body is checked against them. Its `I64ParseFailure` comes from the same Go failure table as 1-F (`codecs.go:106`). That is declaration-location debt: distributed modules cannot yet declare errors in `.ef`. It is not a semantic grant. |
| 2-L2 | Native bodies of builtin capability providers | `emit_go.go:307-320` (`builtinGoProviders`); `js_prelude.go` provider chunks | Their signatures are the service declarations in `builtins.go:16-50`. Those are ordinary contracts, but they are written in Go rather than `.ef`. The `LiveHttp` body is also part of 1-C, and scheduler injection is part of 1-H. |

### H: host-boundary adaptation table

| # | Foreign identity | Where | Adaptation |
| --- | --- | --- | --- |
| H1 | `context.Context` as the first parameter | `imports.go:632-635` | Forwarded from the managed fiber. Required for cooperative cancellation (`imports.go:627`). |
| H2 | `io.Reader.Read`, `io.ReaderAt.ReadAt`, `io.Writer.Write` | `host_types.go:538-542,1050` | Checked I/O contracts. A short write becomes `io.ErrShortWrite`. |
| H3 | Universe `error`, native nil | `imports.go:613`; `host_types.go:75,111` | `GoResult` with a retained error, and absence through 1-D. |

### 3: ordinary library

- **Bundled modules with no checker name branch:** `effra/functions` (`call`, `identity`, `forwardFile`, `suffixed`), `effra/conversions` (`Codec`, `witness`), `effra/data.Result`, `effra/constants`, and `effra/data.Option` apart from host adaptation (`bundled.go:32-35`).
- **Capability services:** `Console`, `Clock`, `Scheduler`, `Sync`, `Env`, `Runtime`, `Assert`, `Files` and `Http` are intended to be class 3. Their declarations in `builtins.go` grant nothing by themselves; only the class-1 branches listed above do.

## Proposals for class-1 entries

Priorities: P1 means decide or fix before more code depends on it. P2 means fix within the owning feature lane. P3 means cleanup.

**1-A (P1). Ownership and opaque admission without capability names.**
- Proposal: declare `File` and `Latch` as opaque types in their capability modules' interface source. Give opaque types a declared resource property, for example a `resource` modifier, so that handle tracking reads a declaration rather than a name. `Files.openRead` then returns an owned resource unless its provider declares the result borrowed. Any provider, and any host resource (#34), gets the same checking.
- Acceptance: every listed branch is deleted (`semantic.go:516,563,2091,2308,3171,4391-4397,4695-4699`, `templates.go:351`), and so are the `typeRuntimeModule` name rows. A grep of the checker finds no `"File"` or `"Latch"` literal. A user-declared opaque resource from a second provider is checked identically.
- Prior art: Rust grants ownership through ordinary types plus lang items such as `Drop`. Swift's `~Copyable` is syntax. Go, Elixir and MoonBit have no static resource tracking.
- Migration impact: the checker and ownership tests (11 Go test files mention `LiveFiles`), three `.ef` files, ownership-origin inspection, conformance lifecycle rows, and generic-data admission tests. Formatter and LSP change only if a modifier is added.
- Depends on #34 and #65.

**1-B and 1-C (P2). Http as an ordinary bundled service.**
- The typed-failure-response absorption needs either the trusted host-body `extern` unit named in [language abstractions](../specs/language-abstractions.md#row-parameters-on-service-operations-and-implementation-methods) or general callback-row absorption.
- Acceptance: `Http` is declared in a bundled `.ef` interface. The callback policy, the name-based admission (`builtins.go:77-106,119-150`) and the emitter's data-shape knowledge (`emit_http.go:8-20`, the `LiveHttp` provider body) are all replaced by the trusted `extern` implementation.
- This is de-specialization, not syntax; the "capabilities as layers" tiebreak already decides it.
- Prior art: Go's `net/http`, Effect's `HttpServer`, Elixir's Plug and Gleam's wisp are all pure library.
- Migration impact: `builtin/http.ef`, 4 Go test files, 2 `.ef` files, and TS declaration emission.
- Depends on #51 and #15.

**1-D (P2). Make `Option` a predeclared language type.**
- Absence is a language invariant (NORTH_STAR: no nil), and host adaptation must name it, so `Option` belongs in the language prelude, alongside the predeclared failures. Keep `Some` and `None` construction and matching unchanged. `effra/data.Option` either becomes an ordinary alias or is removed. `T?` sugar is rejected for now, to keep one Go-like spelling; it would be a notation-exception candidate.
- Prior art: MoonBit's `Option` is a compiler builtin (`moonbit-compiler@d4ada10d` `src/basic_type_path.ml:417`, `src/builtin.ml:58`) with `T?` syntax (`src/parsing_syntax.ml:1154`; docs `fundamentals.md:361`). Gleam keeps compiler-known types in a compiler-defined prelude (`gleam@52e735c8` `compiler-core/src/type_/prelude.rs:18-26`, which includes `Result`) and leaves `Option` as plain library because its compiler never needs it. Rust's `Option` variants are lang items, which is the rejected pattern. Kotlin uses `T?` syntax; Elixir uses the `nil` atom.
- Migration impact: 23 Go test files and 3 `.ef` files import `effra/data`; absence help text (`absent_syntax.go:59`); bundled interface hashes and producer identity; documentation.
- Depends on #32.

**1-E (P2). Declared target capability.**
- A provider or service declares the targets it is implemented for, as data the checker reads for every declaration alike. EF110 is then no longer keyed on names.
- Prior art: Gleam's per-target `@external` declarations; MoonBit's per-backend `extern` bodies.
- Migration impact: three branches and the EF110 tests.
- Depends on #15.

**1-F (P3). Derive profiles belong to the derive construct.**
- Spell the format as part of `derive`, for example `derive userJson = json<User>(maxBodyBytes: 4096, maxDepth: 2)` with `json` as a closed profile word, and make its failures predeclared failures of that profile.
- Prior art: Rust, MoonBit (`derive(ToJson)`, `moonbit-docs@8d9f3ba2` `derive.md:121`) and Haskell address builtin derivers by trait path. In those languages the trait is an ordinary library interface whose implementation the compiler writes. Effra has no traits, so `Json.codec` names nothing ordinary.
- Migration impact: parser, formatter, LSP definition target, 10 Go test files, 1 `.ef` file, and codec documentation.
- Depends on #49 and #55.

**1-G. Split by the language-protocol criterion.**
- *`Foreign` (P3, resolved).* `import go` charges `Foreign` on every host call because the construct's trust contract requires it. No declared operation charges it, and `Foreign` has no operations. It is therefore a language protocol. Specify it as predeclared: the name is not declarable, and the `Host` discharge stays an ordinary layer provider. This matches Go's predeclared `error`, a protocol the language itself names. Migration impact: documentation and a reserved-name diagnostic.
- *`Scheduler` (unresolved design gate, tied to audit row 15).* `Scheduler` is an ordinary capability service; its operations are declared and its providers are layers. So `.timeout` naming it is class-1 debt, and predeclaring it would contradict "capabilities as layers". The recommended criterion for row 15:
  - If the timeout owner relation becomes expressible by ordinary code, `.timeout` becomes an ordinary library function that calls `Scheduler.sleep`. Then the dependency is ordinary, and the postfix construct and its reserved word go away.
  - If `.timeout` stays syntax, it must take time from the lawful runtime contract, a language protocol of the runtime with deterministic test runtimes. Then it charges no capability service.
  - Either way, no predeclared `Scheduler`.

**1-H (P3). Test harness as an ordinary layer.**
- The runner provides a bundled `TestHarness` layer through ordinary layer provisioning. Live providers declare a general host-capability property, and `--live` checks that property.
- Acceptance: no new identity check, and the provider-name lists and scheduler injection by name (`testing.go:11,77-84`, `layer_emit.go:58`, `emit_go.go:324-326,978-983`) are deleted.
- Prior art: Effect's `TestServices` layer and `@effect/vitest` (library); Go's `go test` passes `*testing.T` by signature.

**1-P1 (dependency).** The statecharts lane decides machine decision syntax. This audit requires only that the decision not be a checker rule keyed on a bundled identity.

**1-P2.** Decided by D1.

**1-P3 (P1 design gate).** Before #11 or #57 implements `Actor<P>`, choose one of two routes:

- A general mechanism any library can use, such as Gleam OTP's `Subject(msg)` (ordinary generics, no compiler projection). The item then becomes class 3.
- Syntax for the parts the compiler must derive, for example a predeclared `actor P` handle type, a `spawn` form and a syntactic operation reference for `post`.

Elixir makes `receive` a special form (`elixir@91ee75bb` `lib/elixir/lib/kernel/special_forms.ex:2401`), while `send` and `spawn` are ordinary `Kernel` functions over BIFs (`kernel.ex:1207,1247`). A bundled `Actor` type with compiler-derived methods is ruled out.

## Decisions

**D1. `.catch`, `.provide` and `.timeout` stay syntax; reserving their words is proposed.**

Rows 12-14 move from SHRINK to KEEP as syntax. Row 15 stays UNDECIDED (see 1-G) and remains syntax meanwhile. This is a scoped Effra choice, not a claim that recovery must be syntax in every language:

- Today the checker must know these forms, and the owner direction says such constructs are syntax.
- Making recovery ordinary library needs a general mechanism that Effra currently defers. Koka's `catch` is an ordinary function over general effect-handler syntax and row-polymorphic effect types ([Koka book, handlers](https://koka-lang.github.io/koka/doc/book.html#sec-with-handlers)). Effect's `catchTag` (`effect@460272d` `packages/effect/src/Effect.ts:2743`) and `provideService` (`Effect.ts:6317`) rely on TypeScript's type-level computation. Library recovery does not inherently require conditional types; Koka shows that handlers plus row polymorphism suffice. But Effra has neither general handlers nor user-visible row difference (row parameters cannot absorb an abstract row; EF125).
- Checked-error languages without general handlers spell elimination as syntax: MoonBit's `raise`, `try`/`catch` and `noraise` (`moonbit-docs@8d9f3ba2` `error-handling.md:75,161`), Swift's typed `throws` with `do`/`catch`, and Java's checked exceptions with `try`/`catch`. Kotlin is not evidence, because its exceptions are unchecked. Elixir's `try` and `receive` special forms (`special_forms.ex:2343,2401`; "cannot be overridden by the developer", `special_forms.ex:7-8`) support the general pattern of fixed compiler-known forms, but they are not checked-row elimination.
- If Effra later adopts general handlers or row difference, moving `.catch` into a library is a new decision under the "reconsidering a prior" tiebreak.

**Spelling.** The spelling stays postfix, like Rust's reserved `.await` ([Rust reference, await expressions](https://doc.rust-lang.org/reference/expressions/await-expr.html)), so chaining is preserved.

**Proposed reservation (map ticket T1).** Declaring a field, operation or method named `catch`, `provide`, `timeout`, `orFail` or the planned `recover` becomes a diagnostic. This removes the pipe exception in [language abstractions](../specs/language-abstractions.md#pipe-operator). It is a breaking choice:

- `TestPipeIntrinsicNamesAreOrdinaryMembersAfterThePipe` (`pipe_test.go:429-448`) declares `timeout` and `orFail` service operations and asserts that their piped calls check. T1 must rewrite it as a negative control. No checked-in `.ef` program declares those names.
- Rust's raw identifiers (`r#name`, [Rust reference](https://doc.rust-lang.org/reference/identifiers.html#raw-identifiers)) show that a blanket declaration ban is a separate compatibility decision from postfix syntax. T1 recommends the ban without an escape, because Effra is pre-release and an escape form would add syntax. T1 may revisit this if host interop needs those names; a Go method named `Timeout` differs in case and is unaffected.

**`.orFail()`.** It keeps its LIBRARY verdict and is demoted only when `GoResult` is ordinary data and no semantic branch remains.

**Rejected alternatives:**

- Library methods in `effra/effect` while the checker still knows them. This is the failure mode.
- Prefix keywords. They break chaining and would churn 23 `.ef` files and 63 Go test files for no new guarantee.
- Adding row difference solely so that these forms can be library. It has one consumer family and no adopted handler design.

**D2. Classes 1 and 3 are separated by semantic branches, not by file location.** An item becomes library by losing every compiler branch that produces a semantic fact about it. Two kinds of branch remain permitted: a library intrinsic for a declared signature (D4), and the implementation of an S construct. Both may compute backend facts. Moving a declaration into a bundled module does not make it library, and a declaration in `builtins.go` with no semantic branches is still ordinary library.

**D3. Predeclared identifiers and language protocols count as language (the Go model).** This keeps `Fiber`, `GoResult`, the predeclared failures and `Foreign` compliant once specified. It gives 1-D its remedy. Capability services, `Scheduler` among them, are excluded by NORTH_STAR, and that is why the `Scheduler` half of 1-G is a design gate rather than a predeclaration.

**D4. Library-intrinsic contract.** A library intrinsic has an ordinary declared signature in library source, and that signature is the only source of semantic facts. The intrinsic swaps the implementation per target and may add backend facts, such as retained runtime modules or a required Go import. An inspection view may label it native. If removing the intrinsic would change any diagnostic, row, ownership or checked type, the item is class 1. `effra/i64` (2-L1) is the first conforming instance.

Prior art: MoonBit core bodies such as `= "%arrayview.len"` (`core@e96ede8b` `builtin/arrayview.mbt:62`; 276 such bodies in `builtin/`); Go's SSA intrinsics for `math.sqrt` and `sync/atomic` (go1.27.0 `src/cmd/compile/internal/ssagen/intrinsics.go:739,1299`); Kotlin's `kotlin.coroutines.intrinsics`. MoonBit's `#callsite(autofill(loc))` on `fail` (`builtin/assert.mbt:35`) changes call checking, so it would be class 1 here.

## Prior art summary

| Source | Syntax for | Library-recognised identity | Lesson for Effra |
| --- | --- | --- | --- |
| Rust | `?`, `for`, `async`/`.await`, `match` | Lang items declared with `#[lang]`: traits (`Try`, `Future`, `Drop`), functions (panic entry points), structs (`Box`, `Range`) and variants (`Some`, `None`) ([unstable book](https://doc.rust-lang.org/unstable-book/language-features/lang-items.html)) | Rust keeps syntax for the constructs and uses lang items to connect that syntax to library code. Effra adopts the syntax half and rejects the library-recognition half. |
| Go | `go`, `select`, `chan`, `<-`, `defer`, `map` | None. Predeclared `make`, `len`, `append`, `error`; SSA intrinsics for `math` and `sync/atomic` | Model for D3 and D4: language features are syntax or predeclared, libraries such as `context` and `sync` are ordinary, and intrinsics only swap implementations. |
| Kotlin | `suspend` | `kotlin.coroutines.intrinsics`; `launch`, `async` and `CoroutineScope` are library | One syntax point (the CPS transform), primitives as intrinsics, the rest library. Its exceptions are unchecked, so they are not row-elimination evidence. |
| Koka | `handler`, `with`, effect types | None needed: `catch` is an ordinary function over handlers | Library recovery is possible with general handlers and row polymorphism, which Effra defers. |
| Gleam | `use`, `let assert`, `case` | None in the stdlib; compiler-defined prelude (`Result`, `List`, `Nil`) | Compiler-known types live in a language prelude. `use` is one-to-one callback sugar, like the notation exception. |
| Elixir/OTP | `Kernel.SpecialForms` (`case`, `cond`, `try`, `receive`, `for`, `with`, `fn`) | None. `\|>` is an ordinary `Kernel` macro (`kernel.ex:4509`); `send` and `spawn` delegate to BIFs | Special forms are fixed and documented as language. Actor messaging is library over intrinsics, apart from `receive`. |
| MoonBit | `raise`, `try`/`catch`/`noraise`, `async`, `T?`, `derive(...)` | Builtin `Option`; `%` intrinsic bodies; `#callsite` attribute | Supports D1 for checked errors without handlers, 1-D (builtin absence) and D4 (intrinsics with declared signatures). |
| TypeScript/Effect | Generators (`function*`, `yield*`) only | None. `Effect.gen` (`Effect.ts:1431`) is library over general generators | Library-only works when the host's general mechanisms carry the typing. Effra's current mechanisms do not, so the owner direction applies. |

## Probe evidence

Run with `ef check` built from `abcbe44` (the parser paths are unchanged at `515d49b`). The files are kept in the lane scratch area, not in the repository.

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

Result: `Jobs.cancel` checks, because fiber dispatch is type-directed (S10). `Jobs.timeout` reports `EF102 unknown value Jobs` and `EF106 timeout requires an Effect and an i64 millisecond duration`.

## Documents reconciled in this change

- NORTH_STAR: added the owner direction, the language-protocol distinction and the tiebreak, and replaced the superseded `.catch`/`.provide` migration clause.
- `docs/design.md`: construct admission no longer prefers a checker rule over syntax when that rule would be keyed on a library identity. The visible-construct refinement cites the 2026-10-09 direction. The receiver-method member no longer moves compiler-known forms into `effra/effect`.
- `docs/specs/language-abstractions.md`: updated construct-status rows 12-16, 20, 28 and 29 and the pipe exception, including the recorded test migration. These forms are now called "postfix constructs", not "intrinsics".
- `docs/specs/machine-provider-contract.md`: corrected the `.catch`/`.provide` analogy.
- PRIOR_ARTS: added the comparison section. GLOSSARY: added the new terms.
- Not edited, because other owners hold them: `docs/wayfinder/**` (the #3 ticket text still says "non-parser ownership"), `docs/specs/state-machines.md` (1-P1, statecharts lane) and `docs/specs/actors.md` (1-P3).
