# Small executables and reachable library code

Status: owner-established north star and authorized implementation requirement, 2026-10-06. This is a delivery contract, not a claim about the current prototype's binary size.

## Outcome

Bundling a complete standard library with the compiler must not bundle that library into every application. Native executables include the runtime facilities reachable from the program and their real dependencies. Fluent spelling must have the same retention behavior as an equivalent ordinary call.

Keep language guarantees intact: size optimizations cannot remove cancellation checks, cleanup, diagnostics or required behavior. Go remains the native backend; use its compiler/linker elimination where sufficient and fix package/import/initialization boundaries where that mechanism cannot remove unused work.

## Reachability contract

- Start from the executable entry point and genuinely exported host entry points. Account for callbacks, function values, provider methods, error/codec plans and target-specific adapters through the common checked type/interface model.
- Emit only required native runtime modules. An unused facility must not retain its imports, package initialization, global registry entries, reflection metadata or generated codec tables. Importing an interface for checking does not make its implementation a runtime root. A user-declared foreign Go import is not an unused facility: it is an initialization root (see [foreign package initialization](#foreign-go-package-initialization)).
- Reconcile previously emitted files when a program's reachable modules shrink. A stale generated HTTP file must not reintroduce imports after an HTTP-to-minimal rebuild. Use artifact-owned output identities/manifests or equivalent isolated emission; do not delete unknown user files or let one generated application silently replace another application's selected runtime.
- Bundle runtime source and checked interfaces with the compiler distribution, but select application emission separately. The compiler CLI's distribution size and the built application's size are distinct measurements.
- Lower statically resolved fluent calls to ordinary functions. Do not create a universal runtime object, method registry, reflective dispatch table or blanket init solely to support fluent syntax. This requirement does not introduce fluent syntax by itself.
- Keep conservative roots explicit when dynamic/foreign behavior needs them. Never drop callable behavior merely because a closed-world walk cannot see a call. Diagnose unsupported behavior or retain the documented finite implementation set; do not quietly retain the whole library as a default fallback.
- Use module/contract identity to share reusable codec/runtime implementations rather than copying them at every call site. Generic specialization needs code-size evidence as well as checking-time bounds.
- The JS target should preserve tree-shakable module boundaries and static imports. Report emitted module size, bundled/minified application size and external runtime dependencies separately; a tiny wrapper with an uncounted Effect dependency is not a small complete deployment.

Abstraction retention is part of this reachability contract. Compare an Effra
candidate with semantically equivalent explicit Go and TypeScript/Effect
programs that preserve validation, ownership, cancellation and cleanup. Record
whether the abstraction is erased, directly lowered or retained, and expose any
abstraction-only module, initialization, dispatch or allocation cost in the
same source/module/symbol/byte receipts. Size evidence never licenses removing
a required guardrail; benchmark-last ordering remains in force. The native Go
comparison uses an optimized idiomatic same-contract application. The JS target
may select generated or specialized effect-runtime modules, including a
machine-written representation, when it preserves the pinned default
Effect-compatible ABI and userland runtime contract. Static `match` lowering,
direct dispatch and specialized runtime paths remain candidate transformations;
their retained modules, initialization, dispatch, allocations and bytes must be
measured under cold and warm engine conditions. A possible many-times speedup is
an experiment target, not a current claim.

Before application planning, `runtime/effra.Sources()` copied every runtime source file into generated Go modules, including the HTTP file and its imports when no HTTP operation was called. Linker removal of unreachable functions alone does not establish removal of package initialization, so selection happens at the source emission boundary below.

The preparatory source seam exposes `runtime/effra.SelectSources`, which closes an explicit root set over the `core`, `layers`, `sync`, `http`, `files`, `console`, `env`, `inspect`, and `interop` modules while `Sources()` remains the full admitted set bundled with the compiler. `SelectModules` is the catalog's single dependency authority: it closes roots over declared module dependencies, and `SelectSources` copies exactly the files of that closure. `layers` depends on `core`. `core` conservatively groups effect, scope, fiber, managed-signal, and scheduler sources because their ownership and scheduling types are mutually connected; this grouping is a source boundary, not an application reachability or binary-size claim. Empty roots select no sources, and unknown roots fail explicitly.

The compiler-owned application plan (`Result.ApplicationPlan`) computes the deterministic emission closure of one concrete entry mode: the checked effect `main` for ordinary builds, or the selected `test_` cases plus the harness fixture providers for tests. It reads checker resolutions, layer plans and canonical type nodes only, never graph or projection output. It retains reachable functions, operations, providers with every method body, selected public and hidden layer nodes with their effective replacements, data declarations and templates, foreign bindings with their Go imports, a `go-initialization` root for every declared foreign Go package, lowering helpers, and native runtime modules as `SelectSources` roots. Builtin services and providers declare the runtime modules their generated implementations reference. Callable values originate only at checked function-value references, so the retained references form the finite conservative target set of every dynamic call. Every source declaration is still checked. Planning is bounded, and `EF136` refuses an exhausted closure without returning a partial plan.

Native emission consumes one plan per generation. `Result.GoApplication(mode)` lowers only the plan's services and their context fields, builtin and source providers, layers, functions, data, error and template declarations, lowering helpers and Go imports; the test harness binds exactly the plan's harness fixtures. The same application supplies the immutable generated snapshot, which contains the generated main and exactly the plan's selected runtime sources, so a declaration cannot refer to an unselected module. Managed programs always retain `core`, which owns scopes, fibers, cancellation and the scheduler; that dependency is required behavior, not removable overhead. The CLI builds, runs and tests through that application; `ef check` and MCP `project.check` report each declared native entry mode's closed runtime modules and requirement counts, or its `EF136` diagnostic, and a refused build prints the diagnostic without publishing output. An HTTP-to-minimal rebuild of one source origin publishes a new generation whose files and transitive Go dependencies equal a fresh minimal build; earlier generations and other applications remain untouched. Application receipts and the size-conformance matrix below record size, symbol and dependency evidence. Timing evidence belongs to the separate compile-performance suite.

JavaScript output has two root policies, both planned by the same planner. Entry output (`Emit(true)`) lowers `Result.ApplicationPlan` of the checked effect `main`, the closure a native build consumes. Library output (`Emit(false)`) lowers `Result.LibraryPlan`, a public surface rather than an application: its roots are every exported value (the module's functions and providers, the builtin service tags, and the builtin providers with a JavaScript implementation). `ApplicationPlan` refuses the library surface, so library exports never become application roots and an entry closure never widens to them. `ef test` lowers the test plan with the JavaScript harness. Value declarations, their exports and the matching TypeScript `declare const`/`export` lines are emitted only for plan-retained identities. TypeScript data, error, template, service-requirement and provider-shape declarations are type-only and erased, so every module keeps them with their nominal brands. Lowering helpers, builtin provider implementations and the test harness are fixed chunks; each declares the chunks it calls and the `effect` names it uses. A module selects the chunks its plan's helper, operation, layer, provider and effect-function requirements need, closes them over declared edges, and imports exactly the `effect` names they and its declarations use. JavaScript has no foreign module bindings: a Go import or native-only feature refuses JavaScript output whether or not the entry reaches it, so pruning never drops host module initialization. Tests check each authored example's modules for exact `effect` imports, bound generated names and export/declaration agreement; they also run every JavaScript entry under Node against the same program with the complete prelude, and type-check library and entry declarations with strict TypeScript. Backend conformance probes run the public library module; a probe that calls a lowering helper must root it through its source, so there is no test-only emission mode.

## Foreign Go package initialization

Adopted 2026-10-07 (decision `go-foreign-init`):

> An explicit foreign Go import declares a runtime package-initialization dependency as well as making host declarations available. Every such import in the compiled source module is retained independently of reachable Effra calls. When generated code needs a package qualifier, emission retains the required named import; otherwise it emits a blank Go import for that package. Initialization-only retention is deduplicated by resolved package identity and does not retain unused Effra functions or foreign call wrappers.
>
> Package initialization, including package-variable initialization and transitive imported-package initialization, follows Go's ordering and once-per-package semantics before the Effra entry or test harness executes. It is not deferred by an Effra recipe, governed by `Foreign` provision, or owned by an Effra scope. Foreign function and method invocation remains deferred and requires its declared capability. Import startup does not acquire a typed failure row or a managed cleanup guarantee.
>
> Compiler-distributed module availability, export-data discovery, and host metadata lookup alone are not initialization roots. Packages additionally required by retained native values, types, or adapters follow normal Go import semantics. An import removed from source loses its explicit root, although another required dependency may still initialize that package. Unsupported target imports remain diagnostics; JavaScript continues to refuse Go imports even when their callers are unreachable.

The application plan represents this obligation separately from callable use. Native build and test plans root one `go-initialization` requirement per declared package (identity: the resolved package path; reason: `declared-foreign-import`; no `Via`), charged to the same work budget and refused with `EF136` all-or-nothing. An initialization root retains no function, binding, `Foreign` service, foreign helper, provider or interop runtime module. A reachable call additionally retains its binding and a `go-import` requirement, and the package keeps both explanations. Emission writes the named import of every alias a retained call uses and one `_` import for each declared package no retained code names; a named import already initializes its package, so there is no duplicate blank import. The plan owns that per-package `named` or `blank` lowering, and emission and inspection both read it, so a later named reference (for example a host type) suppresses the blank import in one place. Because every declared import is emitted, admission refuses with `EF111` at its declaration any package the generated program cannot import under Go's rules (`package main`, a path with a `vendor` element followed by more path, or an `internal` package whose parent tree does not contain the generated main package; every standard-library `internal` package is refused, because generated programs never lie inside GOROOT), called or not. Only the native build and test entry paths root initialization: `applicationPlan` calls `goInitialization` before the shared `finish`, so `LibraryPlan` roots none. `namedGoImport` runs inside the shared drain through foreign-call traversal, which is sound because the library surface is JavaScript-only and JavaScript refuses Go imports before planning. A future Go library mode must choose its own initialization roots and apply `goImportRefusal` with its own importer path. `ef check` and MCP `project.check` report each mode's `goInitialization` packages with their rooting declarations and `named` or `blank` lowering. `EFL003 unused-go-import` advice states that the import still initializes its package. `ef test` keeps requiring `--live` for any Go import, and refuses before building or running the package otherwise.

## Application receipts and size conformance

Adopted 2026-10-09 for #14.

`ef build FILE [--target go|js] --receipt PATH` writes an application receipt (`effra.application-receipt/1`) after a successful build. A refused or unchecked build writes no receipt, and only `build` accepts the flag. A native receipt records:

- the plan's closed runtime modules and requirement counts;
- the published generation's identity and its files, with bytes and hashes;
- each generated package's direct imports, and the transitive dependencies from `go list -deps`;
- the toolchain identity and build flags;
- the executable's bytes and hash;
- a per-package summary of `go tool nm -size -type` with the complete listing digest.

`nm` runs from the generated module. The go command can select a different toolchain outside that module (`GOTOOLCHAIN=auto` with `go 1.27`), and another toolchain's `nm` classifies and orders symbols differently. A JavaScript receipt records the emitted module and declaration bytes, and every external module the artifact imports. Static imports name their bindings. Dynamic `import()` calls with literal specifiers, such as the HTTP transport's `node:http`, are marked dynamic. A computed specifier refuses the receipt, because it cannot be accounted for. Every receipt reports the compiler distribution separately: the `ef` executable and all bundled runtime sources.

The deterministic part runs in the gate. `cmd/ef/size_process_test.go` builds the public fixtures in `conformance/size/fixtures` on both targets through `--receipt`, and asserts:

- the closed runtime modules and the generated runtime files;
- that `net`, `net/http`, `crypto/tls`, `encoding/json` and `os/exec` are absent from both the dependencies and the symbol table;
- both targets' output;
- that unused declarations and pipe spelling leave the generated runtime, imports, dependencies, symbol listing, executable size and emitted JavaScript unchanged.

The HTTP row is the negative control: it must show the transport in the same fields. With all-source emission substituted, the test fails on every row with retained files, dependencies and `net`/`net/http`/`crypto/tls` symbols.

`scripts/size_conformance.py` is the explicit, expensive matrix. It uses one matched configuration: `CGO_ENABLED=0 -trimpath -mod=readonly`, plus a `-ldflags=-s -w` companion for every unstripped binary. It builds every fixture, an all-source counterfactual (the same generation with every distributed runtime source), the idiomatic Go controls, and the TypeScript/Effect 4.0.1 controls in `conformance/size/controls`. It measures every binary with the same functions and fails if an Effra receipt disagrees. It compares every program's output across cohorts. For JavaScript it reports three sizes separately: the emitted module, the minified application with `effect` external, and the minified deployment with `effect` inlined. `--record` writes the run with its commit, toolchain and fixture/control/runtime hashes. [conformance/size/README.md](../../conformance/size/README.md) lists the commands.

Decision and rejected alternatives:

- Select source modules before compilation. Do not rely on the linker. Go's linker roots `main.main` together with every package's init task (`cmd/link/internal/ld/deadcode.go`, go1.27.0), so imported packages keep their initialization and the globals it reaches. The all-source counterfactual quantifies this. The minimal row grows from 2,668,552 to 5,566,115 bytes and from 65 to 200 dependencies, and gains `net`, `net/http`, `crypto/tls` and `encoding/json`, although no HTTP function is reachable.
- Measure the artifact, not the emitter's intent. External JavaScript imports are read from the emitted module, so the receipt does not depend on JavaScript lowering internals. Bundled sizes come from a real bundler, `bun build --minify`. Bundlers drop unused modules only when the package declares that it has no side effects. Effect 4.0.1 declares `sideEffects` for a single JIT-enable file, so its bundled runtime is measured and attributed to the deployment, not assumed. This follows the [esbuild](https://esbuild.github.io/api/#tree-shaking) and Rollup tree-shaking model.
- Rejected: reporting only the stripped size. Stripping and compression are not evidence that unused code was removed. Unstripped receipts keep the symbols, and stripped companions are recorded beside them.
- Rejected: a per-fixture byte budget in the gate. Budgets come from measured baselines, which this run records for the first time. The gate asserts retention facts that do not depend on the toolchain.
- Rejected: an MCP build receipt. MCP `project.check` already reports each entry mode's closed modules and requirement counts. Building executables from MCP is a separate capability decision.

Recorded run `conformance/size/receipts/2026-10-09.json`. Commit `3dc8c5d`, go1.27.0 linux/amd64, `CGO_ENABLED=0`, Bun 1.4.2, Node v24.11.1, Effect 4.0.1. Raw bytes:

| Row | Go | Go stripped | Go deps | JS module | JS app (min) | JS deploy (min) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| minimal | 2,668,552 | 1,732,768 | 65 | 3,467 | 1,589 | 30,503 |
| minimal, all-source | 5,566,115 | 3,748,000 | 200 | | | |
| minimal-unused | 2,668,552 | 1,732,768 | 65 | 3,467 | 1,589 | 30,503 |
| managed | 2,813,732 | 1,839,264 | 65 | 9,875 | 4,842 | 35,227 |
| managed, all-source | 5,698,975 | 3,846,304 | 200 | | | |
| codec | 2,787,773 | 1,818,784 | 65 | 25,311 | 12,185 | 41,600 |
| http | 8,825,391 | 5,992,608 | 191 | 16,426 | 7,010 | 36,535 |
| direct / pipe | 2,669,458 | 1,732,768 | 65 | 4,092 | 1,739 | 30,660 |
| Go control, minimal | 2,337,969 | 1,507,488 | 60 | | | |
| Go control, managed | 2,584,714 | 1,675,424 | 62 | | | |
| TS/Effect control, minimal | | | | 598 (source) | 316 | 23,828 |
| TS/Effect control, managed | | | | 1,442 (source) | 765 | 40,796 |

The compiler executable is reported separately from these application rows; it was 16,722,909 bytes in the recorded run, and the bundled runtime sources were 15 files and 107,281 bytes.

Retention observations, not claims:

- A pure minimal entry retains the `core` module: scopes, fibers, the scheduler, and signal-driven cancellation through `os/signal`. Compared with the Go control, the largest symbol deltas are `time` (+41 KB), the generated runtime (+17 KB), `context` and `os/signal`. The Go control installs no signal handling.
- The managed row's residual over its Go control is mostly the generated runtime (166 symbols, 55 KB of text) and generated `main` (+12 KB). Apart from the generated runtime, `cmp` and `strings`, both executables retain symbols from the same packages.
- Splitting `core` so that an entry without forks, sleeps or deadlines does not retain the scheduler is a candidate transformation. It must preserve entry cancellation, and it remains unmeasured.

## Acceptance matrix

Use source-controlled public programs with matched toolchains, target, CGO mode, build tags and linker flags:

| Program | Required evidence |
| --- | --- |
| Pure minimal entry | No unused HTTP/RPC/codec/platform implementation or initialization; compare with an equivalent minimal Go entry. |
| Managed effect without platform I/O | Only the required core ownership/cancellation runtime and its dependencies; all guarantees still pass. |
| Ordinary handler actor without platform I/O | Retain selected protocol/dispatch and shared ownership/control dependencies, with no mandatory machine tables, durable storage, HTTP/RPC/codec or platform adapters. |
| Machine-backed actor without platform I/O | Retain the same shared actor ownership/control core plus selected machine functions, data and entry/completion adapter. A second machine does not duplicate the actor runtime; unused persistence/deployment modules remain absent. |
| Codec-only consumer | Required codec plans retained once; HTTP/RPC/server initialization absent. |
| HTTP application | Required transport/codec/domain behavior retained; unrelated RPC, SQL, streams and other modules absent when available. |
| Equivalent direct and fluent calls | Same semantic roots and comparable artifact size; fluent spelling does not retain other operations. Run this case when fluent syntax is admitted. |
| Unused library growth | Adding an unused module, operation or witness does not add it to application imports/symbols or materially grow the executable. |
| Rebuild after removing a facility | Build HTTP then minimal into the same managed application output; selected modules, dependencies and symbols match a fresh minimal build. A separately generated application keeps a coherent runtime. |

Record generated module selection, transitive build dependencies and native symbol evidence alongside exact byte sizes. For stripped release builds, retain an identically configured unstripped companion for symbol inspection. Record compiler/runtime/library source identities, Go/JS versions, architecture, build flags and size measurement method. Do not use debug stripping, compression or externalizing a runtime as a substitute for proving unused-code elimination.

A machine declaration available only to checking/inspection does not root the actor runtime. Actual spawning roots its implementation and necessary core dependencies. Retain snapshot tables and explicitly named payload codecs only through their real consumers; avoid global actor registries and blanket initialization. The actor runtime may require core scheduling for owned progress: do not promise removal of a necessary dependency merely because application steps contain no explicit delay.

Establish per-fixture budgets from measured baselines rather than inventing a global target. Keep raw before/after bytes and losing results. Build-stage timings belong to the separate compile-performance suite; contention-independent byte counts do not justify compile-speed claims.

## Delivery

The source-module split must also work over outputs from the previous compiler. The current additive writer leaves the former `stdlib.go` alongside its replacement declarations: both fresh versions compile, but the upgrade fails. Implement isolated, owned complete generated modules before integrating that split. This output boundary can proceed before canonical application reachability, retaining current all-source emission. Full source origin, target and ordinary/test mode distinguish artifacts; interrupted publication, concurrent builds and unchanged reuse need explicit controls. Do not satisfy this requirement by deleting old output directories in the gate or special-casing a retired filename.

1. Implement module selection through the versioned bundled interface/runtime boundary and preserve both-target behavior.
2. Add minimal and managed-effect size/dependency fixtures, then codec and HTTP fixtures as those libraries land. (Minimal, managed, codec, HTTP, unused-growth and direct/pipe rows landed 2026-10-09. Controls cover the minimal and managed rows.)
3. Integrate deterministic reachability checks into the gate and record build-size receipts through CLI/MCP capability inspection. Keep expensive toolchain/bundler matrix runs in an explicit required size-conformance command. (Landed 2026-10-09 as `ef build --receipt`, `cmd/ef/size_process_test.go` and `scripts/size_conformance.py`; MCP keeps check-time module inspection.)
4. Recheck new library modules and any fluent API against the same matrix. No capability is called tree-shakable solely because its surface uses static types.

## Prior art: Elixir, Erlang/OTP and MoonBit

An Elixir release copies every file in each included application's `ebin` and strips debug, documentation, compile-information and other non-essential chunks (`lib/mix/lib/mix/release.ex:L855-L880`, `L911-L918`, `lib/mix/lib/mix/tasks/release.ex:L382-L388` at `91ee75bb`); no module- or function-level dead-code removal was found. Protocol consolidation links implementations at build time (`lib/elixir/lib/protocol.ex:L182-L195`). Effra rejects application-granularity retention: only reachable modules and their real dependencies enter an executable. MoonBit's pinned compiler removes unused top-level definitions per package (`src/core_dce.ml:L116-L182` at `d4ada10d`). It then monomorphizes from top-level expressions and exported functions (`src/monofy_analyze.ml:L177-L208`) and shrinks the wasm to what exports reach (`src/shrink_wasmir.ml:L248`, `L292-L300`). That corroborates root-based retention, while monomorphization is the generic-specialization size tradeoff this contract asks to measure. The pinned compiler builds only wasm-gc, so it supplies no MoonBit JS size evidence. Pins are in [PRIOR_ARTS](../../PRIOR_ARTS.md#moonbit).
