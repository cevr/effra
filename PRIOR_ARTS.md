# Pattern and guardrail index

Effra's public architecture is described through generic patterns. This index records the decisions that source reviews inform, without making an external application's structure part of the language contract. Read [NORTH_STAR.md](NORTH_STAR.md) and [GLOSSARY.md](GLOSSARY.md) alongside it.

## Language and library references

Owner direction refreshed 2026-10-06: preserve Go-like simplicity, Effect-style guarantees and Borgo-like algebraic data/abstractions. This repository keeps the established `PRIOR_ARTS.md` name as its canonical prior-art index.

| Source | Revision / paths | Read it for | Compare with |
| --- | --- | --- | --- |
| [Go](https://github.com/golang/go) | Toolchain-matched source; `src/net/http`, `src/context`, `src/cmd/compile` | Ordinary control flow, host declarations, native server costs and separate compile/link work | Native emission, import adapter, managed HTTP and stage measurements |
| [Borgo](https://github.com/borgo-lang/borgo) | `3b9f01578941fb00ed93756e2fadc009feb50128`; `compiler/src/type_.rs`, `infer.rs`, `exhaustive.rs`, `codegen.rs`, `compiler/test/infer-file.md` | ADTs, exhaustive patterns, functions/generics and Go ecosystem integration with an approachable surface | Canonical types, checker, Go lowering and public data/library examples |
| [Effect](https://github.com/Effect-TS/effect) | Runtime pin `effect@4.0.1`, commit `460272d30457f4697d8b8c52cad41caccbcace08`, checked out as the [conformance/upstream/effect](conformance/README.md) submodule; `packages/effect/src`, `packages/effect/test`, `packages/effect/typetest`, platform server tests | Typed effect composition, scopes, cancellation, provider construction, codecs, HTTP/RPC and behavioral/type-test invariants | Bundled interfaces/runtime, portable conformance and [native server contract](docs/specs/native-server-contracts.md) |
| [Gleam](https://github.com/gleam-lang/gleam) | `52e735c82d42811dd08d29d5508f564da081fd7d`; `compiler-core/src/javascript/tests/externals.rs`, `javascript/typescript.rs`, `build/module_loader.rs` | Explicit target implementations, generated host declarations, representation boundaries and module metadata reuse | Target capability checks, automatic imports, emitted declaration fidelity and separate build-stage receipts |
| [ReScript](https://github.com/rescript-lang/rescript) | `30ce3698ef683137039c7eb7ded2915b278d26ee`; `tests/tests/src/ffi_test.res`, `tests/gentype_tests`, `rewatch/src/build`, `compiler/core/js_source_map.ml` | Host calling conventions, generated TypeScript adapters, tagged/opaque representations, incremental compilation and source locations | Avoid routine handwritten bindings; disclose representation adapters and keep host types distinct from runtime validation |
| [TypeScript](https://github.com/microsoft/TypeScript) | `a1ef42b9ea7032fa60df127d42b4c86fd2a110ee`; `tsc/internal/contentmapper`, `tsc/internal/execute/incremental/buildinfo_contentmapper_test.go`; original [content mapper PR](https://github.com/microsoft/typescript-go/pull/4712) | Host declaration reuse, mapped virtual source, diagnostic positions and configuration-aware cache identity | Optional JS tooling projection; Effra remains the authority for effect/owner checking and Go builds must not require TypeScript |
| [XState v6](https://github.com/statelyai/xstate/pull/5543) — main machine reference | Active `next` branch pinned `2146ae26ebfc7e6a624b3a1f237f9e6ddc30b9f5` (`6.0.0-alpha.64`), superseding original PR5257; `docs/xstate-v5-to-v6.md`, `packages/core/src`, `packages/core/test`, `packages/xstate-effect` | Ordinary function transitions, state-specific contracts/input, schema metadata, internal lifecycle identity, pure stepping, Effect integration and inspection | Checked machine plan, ordinary ADT payload construction, owned actor runtime, test generation and explicit codec boundaries; do not infer v6 behavior from v5 |
| [Effect Machine](https://github.com/cevr/effect-machine) | `176697cf20006f5f539bf04ff2e609bfe1563e22`; `src/internal/{runtime,transition}.ts`, `test/{stop-completion,reenter,state-timeout,type-constraints}.test.ts` | State/actor-owned scopes, exact shutdown outcomes, re-entry, typed requirements and causal tests | Managed actor execution and upstream behavioral mapping |
| [Erlang/OTP](https://www.erlang.org/doc/system/sup_princ.html) | Official OTP29.1.1 supervision and code-loading documentation, read2026-10-06 | Restart strategies/budgets, escalation, current/old module code and explicit runtime costs | Optional local supervision; native process handover and VM-specific code replacement remain distinct |
| [Effect Cluster](https://github.com/Effect-TS/effect/tree/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/cluster) | Runtime pin460272d; Entity, ClusterSchema, MessageStorage and Sharding | Typed addressing, explicit persistence/concurrency and provider-dependent transactions | Checked actor contracts with opt-in durability, never implicit exactly-once host effects |
| [celld](https://celld.dev/docs/guarantees/) | Official guarantees docs, read2026-10-06; ordinary counter/alarm source `f2bf648663a610eefde71f3547ad61e9b896b1f0` (v0.6.1), no engine audit | Ordinary named-object handlers; conditional storage ownership, epochs, durable acknowledgement and external process supervision | Library actor behavior and qualified deployment/storage provider contracts |
| [Rivet](https://rivet.dev/actors/docs/) | Source0247751bdafc82e72f3e4f52082fc2c1694a5c81; selected native.ts/config.ts persistence paths, typed Effect handlers and ordinary queue-consumer examples; current action/queue/workflow docs | Parallel actions, handler/receive-loop behavior, receive-time queue removal versus reply completion, throttled/awaited saves and replay/versioning | Ordinary actor protocols, explicit sequential policy and acknowledgement boundaries; commented Effect messages are unsupported evidence |
| [Cloudflare Durable Objects](https://developers.cloudflare.com/durable-objects/) | Official state/gates/lifecycle/alarms docs, read2026-10-06 | Stable object identity, async interleaving, storage gates, eviction and retry boundaries | Actor ownership, safe codec-governed recovery and explicit external idempotency |
| [Serde](https://github.com/serde-rs/serde) | `6693a89cca77e0151437da1c7f890090b9ebf04c`; `serde_derive/src/{ser,de}.rs`, `internals/check.rs`, `test_suite/tests/test_annotations.rs` | Structural derivation, explicit adapters and fallible conversions, conflicting-attribute diagnostics | Finite derived codec witnesses plus ordinary typed transformation functions; no required procedural macro system |
| [Ghosts of Departed Proofs for TypeScript](https://github.com/rauchg/gdp-ts) | `ebd0af9cae423997a43a024dc6d6738b0895bbec`; `src/index.ts`, `src/lint/shared.ts`, `test/types.ts`, documented limits | Scoped subject identities, evidence construction authority, wrong-subject rejection and the limits of stale evidence | [Opaque values and checked evidence](docs/research/opaque-values-and-evidence.md); candidate Go-like authorized values before exposing generative proof parameters |
| [Ghosts of Departed Proofs paper](https://kataskeue.com/gdp.pdf) and [Haskell library](https://github.com/matt-noonan/gdp) | Matt Noonan, Haskell2018, DOI10.1145/3242744.3242755; library `853abfd43ddac462bba53fc4cceac0f8dd2bf29c`, `Theory/Named.hs`, `Data/Refined.hs`, `Logic/Proof.hs` | Hidden newtypes, nominal roles, rank-2 fresh names, relational evidence and explicit axiom trust | Small reusable language mechanisms; distinguish subject identity, construction authority, lifetime and freshness |
| [Bend](https://github.com/bendlang/bend/tree/51ab8e8ece1a336b2c76a80ba20eb0dd2694775c) | 51ab8e8ece1a336b2c76a80ba20eb0dd2694775c; bend2/{bend,comp,main,safe}.ts, bend2/bendtt.lean, guide/GUIDE.md, bend2/docs/{BendTT,BendRT}/main.typ, tests/proof/, gates/, evals/, demos/ | Laws as typed obligations, explicit dependent proof terms, live/dead trust boundaries, constructive witnesses, selective erasure and optional kernel checking; retained tasks versus measured agent outcomes | [Opaque values and checked evidence](docs/research/opaque-values-and-evidence.md), [language abstractions](docs/specs/language-abstractions.md), [testing](docs/testing.md); adopt construction/obligation discipline, preserve ordinary Go/JS and distinguish trusted predicates, tested laws, verified propositions and translation/runtime assumptions |
| [Zerolang — agent query/edit tooling](https://github.com/vercel-labs/zerolang/tree/7e1a64d27cc37671df31c6370890bce86f5135e1) | `7e1a64d27cc37671df31c6370890bce86f5135e1`; `native/zero-c/src/program_graph_{query,query_refs,patch,patch_ops,identity,store,projection}.c`, `main.c`, `fs.c`; source anchors in the local zerolang synthesis | Focused selectors, semantic handles, graph/field preconditions, structural versus semantic validation, baseline-diagnostic tolerance and single-file replacement limits | CLI/MCP queries, GraphViewV1 and checked text-edit plans; keep `.ef` authoritative, producer-qualified freshness and serialized commit-time guards; reject an authoritative graph store or writable visual graph |
| [Zerolang — toolchain-shipped agent guidance](https://github.com/vercel-labs/zerolang/tree/7e1a64d27cc37671df31c6370890bce86f5135e1/skills) | Same pin; `skills/zero/SKILL.md`, `skill-data`, `scripts/embed-skill-data.mts`, `scripts/stdlib-contracts.mts`, `native/zero-c/Makefile` | A discovery stub that retrieves binary-matched topic references; executable signature/documentation synchronization | Compiler-owned CLI/MCP resources, supported-operation discovery and diagnostic/library guidance; derive canonical facts and keep explanatory text version-matched |
| [Zerolang — diagnostic and repair metadata](https://github.com/vercel-labs/zerolang/blob/7e1a64d27cc37671df31c6370890bce86f5135e1/native/zero-c/src/main.c#L4509) | Same pin; `main.c:4509-4573,4627-4650`, `canonical_text.c`; ordinary diagnostics versus advisory fix-plan envelopes | Stable codes, expected/actual/help, repair intent and safety classifications; distinguish suggested repair from applied/checked edits | Shared Effra diagnostics and existing `{message, edits:[{span,newText}]}` suggestions; preserve source qualification, UTF-8 spans/UTF-16 ranges and complete candidate rechecking |
| [Zerolang — agent-task and conformance evaluation](https://github.com/vercel-labs/zerolang/tree/7e1a64d27cc37671df31c6370890bce86f5135e1/evals) | Same pin; `evals/src/{cases,run,source}.ts`, `scripts/validation-suite.mts`, `conformance/run.mjs`, `scripts/snapshot-command-contracts.mts` | Independent candidate compile/run checks, task-specific outputs and layered command/runtime conformance; workflow-counter and fixture-exposure limits | Effra deterministic toolchain tasks plus optional held-out live-agent trials with protected oracles and matched text-only controls; do not inherit efficiency or target-parity claims |
| [Stately Graph](https://github.com/statelyai/graph/tree/02815c6eaea83ebbf7d37fbbc39e744aa3cfcea7) | `02815c6eaea83ebbf7d37fbbc39e744aa3cfcea7`; `src/types.ts`, `schemas/`, `src/formats/{mermaid,dot}`, `src/formats/support.ts`, `src/coverage.ts`, `src/walks.ts`, `src/algorithms` | Plain-JSON graph interchange, nested nodes and ports, declared format fidelity, coverage paths and graph algorithms | GraphViewV1 for `ef graph`/MCP/editor views of dependencies, layers, application plans, machines and actors; shape-compatible data, compiler-owned semantics, no runtime dependency |

### Zero-cost abstraction comparison, 2026-10-08

The owner added a destination obligation that strong abstractions should make
the illegal states and invalid operations covered by their checked contract
unrepresentable in checked Effra source, while foreign/trusted behavior remains
explicit, without imposing an abstraction-only executable cost. The follow-up
target is explicit: a native Go application must match or beat optimized
idiomatic Go under the same contract, and JS must pursue every measurable
lowering or specialization opportunity. The following primary sources constrain
that phrase and its limits:

| Primary source | Mechanism observed | Effra comparison and limit |
| --- | --- | --- |
| [The Rust Book: Using Iterators](https://doc.rust-lang.org/book/ch13-04-performance.html) | A bounded iterator-versus-loop example is used to show that a high-level iterator can lower to comparable machine code; the page discusses unrolling as a possible runtime win and directs readers toward measurement. It does not itself establish a code-size cost. | Effra may erase or directly lower a regular abstraction when validation, ownership, cancellation and cleanup remain equivalent. Emitted size/retention and runtime tradeoffs are an Effra measurement obligation; the Rust example is counterevidence against assuming abstraction overhead, not proof of universal equivalence or zero cost. |
| [C++ Core Guidelines Per.7](https://isocpp.github.io/CppCoreGuidelines/CppCoreGuidelines#rper-efficiency), [Per.11](https://isocpp.github.io/CppCoreGuidelines/CppCoreGuidelines#rper-comp), [P.9](https://isocpp.github.io/CppCoreGuidelines/CppCoreGuidelines#Rp-waste), [Per.6](https://isocpp.github.io/CppCoreGuidelines/CppCoreGuidelines#per6-dont-make-claims-about-performance-without-measurements) | Per.7 supplies the general design-for-optimization direction; Per.11 moves computation from run time to compile time to reduce code size and run time; P.9 says time/space spent on safety is not waste; Per.6 requires measurements before performance claims. | Effra may specialize or lower a checked abstraction, but its rows, ownership evidence, cancellation checkpoints and completed cleanup are real work. A residual check or allocation must remain visible and measured; safety cannot be deleted to make a candidate look free. |
| [Go compiler pipeline](https://go.dev/src/cmd/compile/README), [GC guide: eliminating heap allocations](https://go.dev/doc/gc-guide#eliminating-heap-allocations), and [PGO](https://go.dev/doc/pgo) | Go combines inlining, devirtualization, escape analysis, desugaring and SSA lowering; escape outcomes depend on context, and profiles can guide optimization. `go build -gcflags=-m=3` exposes compiler decisions. | Optimized idiomatic Go is the native acceptance control, with the same rows, ownership, cancellation and cleanup. Compare Effra output against that control under matched toolchain and workload; preserve allocation, GC, compile and link evidence instead of assuming a source form wins. |
| [Generalized Evidence Passing for Effect Handlers](https://www.microsoft.com/en-us/research/wp-content/uploads/2021/03/multip-tr-v2.pdf) (Xie/Leijen, MSR-TR-2021-5, v2, pp. 1–2) | Canonical evidence vectors can replace handler search; tail-resumptive operations can avoid a yield/resume cycle; bind inlining and join-point sharing improve generated code. The paper establishes contextual equivalence for its optimized algebraic-handler translation and reports scoped effect-handler measurements. | A generated or specialized Effra effect runtime may use these ideas or a different strategy even when its output is not human-style code. The paper does not establish Effra structured-concurrency, cancellation or completed-cleanup laws, nor an absolute cross-language speedup; those remain matched contract tests and measurements. |
| [V8 Maglev](https://v8.dev/blog/maglev) | V8 uses runtime feedback, shapes, speculative specialized nodes, deoptimization and multiple tiers. | Effra inference: JS `match` lowering to `if`/`switch`, direct dispatch and effect-runtime specialization may vary with branch shape, warmup, engine/version and type distribution. Maglev does not compare these Effra strategies; record cold/warm behavior and preserve the pinned default Effect-compatible ABI and userland runtime contract. |
| [Pinned Effect 4.0.1 `Effect.ts`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Effect.ts) | Effect's userland runtime and type-level API provide the JS contract surface for deferred effects, failure/service rows, scopes and fibers; the pinned source is the default comparator. | A generated/specialized runtime may replace internal implementation paths while retaining the same public ABI and observable errors, cancellation, scopes and completed cleanup. It must be compared with explicit TypeScript/Effect work and its losing results retained. |
| [Effra accepted checked-match emitter](https://github.com/cevr/effra/blob/f091cd6db870c08ca1d82f100150ce0f3363e6a8/internal/compiler/emit.go) and [current emitter](internal/compiler/emit.go#L447) | The accepted multi-subject plan evaluates each subject once and emits ordered `if` branches with tag tests; the current single-subject path emits a `switch` on the tag. Effectful match branches remain wrapped in `yield* Effect.gen`, while pure matches use an IIFE. | These are existing emitted strategies, not performance results. The lowering unit compares them and generated/specialized runtime paths under the same subject/body/tag semantics, preserving the default JS ABI and lifecycle laws. |

The adopted evidence shape is a three-way semantic baseline: an Effra
candidate, an explicit Go program and an explicit TypeScript/Effect program
perform the same validation and observable work, preserve the same ownership,
cancellation and cleanup behavior, and expose the same failure/service rows.
Receipts must identify whether the abstraction was erased, directly lowered or
retained behind dispatch; they must report retained modules, emitted source,
executable bytes, allocations, dispatch and residual checks. Unequal work,
weakened guardrails, an omitted TypeScript typecheck or an isolated
microbenchmark is counterevidence, not a pass. This joins the existing
measurement ledger and benchmark-last gate; no current Effra performance claim
is made here.

The native Go acceptance cohort is an optimized idiomatic Go implementation of
the same contract, including validation, failure/service rows, ownership,
cancellation and completed cleanup. The native executable target is to match or
beat that cohort; a slower, equal or inconclusive result stays visible rather
than being reframed as a win. The JS cohort keeps the pinned Effect-compatible
ABI and userland runtime behavior while exploring every material measurable
compiler/lowering opportunity: static match dispatch to `if`/`switch`, direct
calls, join-point sharing, tail-resumptive paths and generated or specialized
effect-runtime code are examples. A potential many-times speedup is an ambition
to test, not a current result or universal multiplier.

Generated runtime code is allowed when it is an ordinary compiler/provider
implementation, not new user syntax. It must retain the same typed errors and
service rows, ownership, cancellation, scopes, interruption behavior and
completed cleanup. The pinned default JS ABI remains the compatibility boundary;
an alternate ABI requires a separate decision and conformance record.

Rejected alternatives are a universal zero-overhead promise, forcing every
abstraction to inline, removing ownership/validation checks, or judging only a
small straight-line fixture. Rewriting every `match` to `if`, relying on V8's
warm tiers without cold/warm evidence, or replacing the pinned Effect runtime
without an ABI decision are also rejected. The Rust page's scope and Effra's
size/retention measurement obligation, C++ Per.11's compile-time/code-size
tradeoff, Go's context-dependent escape analysis, V8's speculative deoptimization and the
effect-handler paper's scope of equivalence are direct reasons to retain losing
results and keep the decision scoped.

Application usage is surveyed as generic patterns below. Exact private checkout pointers stay in local research notes; private source is not copied into this public repository. Upstream reference tests retain their own license and provenance and remain distinct from passing Effra tests.

### Refreshed layer usage, 2026-10-06

Source-only comparison; no application tests were executed. Generic findings and adopted differences are in [layer composition](docs/research/layer-composition.md). Versions differ from the Effra runtime pin and do not establish inherited parity.

| Primary source | Immutable pin / inspected paths | Contract pressure |
| --- | --- | --- |
| [Server composition](https://github.com/pingdotgg/t3code/blob/17c0878941ab8ab19108fb639c5138d92a89e919/apps/server/src/orchestration-v2/runtimeLayer.ts) | `17c0878941ab8ab19108fb639c5138d92a89e919`, Effect4.0.1 with repository patch; `server.ts`, `persistence/Sqlite.ts`, `ws.ts`, test roots | Repeated provide chains, hidden startup workers/migrations, fresh fixtures and reinjection of application services into connection scopes |
| Private resource host | `191b9c4c55d4a8b485f74119d7e136e01aaf3af7`, Effect4.0.0 with repository patch; exact source/test pointers retained locally | Bottom-up construction, root acquisition counters, plugin child-scope rollback, compatible-input leases and explicit dynamic precedence |
| [Contextual infrastructure providers](https://github.com/alchemy-run/alchemy/blob/6d5e6f6001848778dc0f3ab7dcfffc770b8b4a13/packages/alchemy/src/Local/ProviderLayer.ts) | `6d5e6f6001848778dc0f3ab7dcfffc770b8b4a13`, Effect4.0.0; `Stack.ts`, `Util/ConfigProvider.ts`, `Test/Core.ts`, managed HTTP shutdown | Lazy variants, fresh recipes under different captured configuration, explicit shared process owners and request-before-dependency shutdown |
| [Explicit application graph](https://github.com/anomalyco/opencode/blob/3f393d78bfc3f0826b2c7080e57964c235704695/packages/core/src/effect/layer-node.ts) | `3f393d78bfc3f0826b2c7080e57964c235704695`, Effect4.0.0-beta.83; layer-node tests, location-services and application runtime | Replacement-before-traversal, dependency/cycle checking, parameterized fresh location graphs; reject roots-only outputs and branch-local duplicate-provider policy |
| [Plugin executor and platform roots](https://github.com/UsefulSoftwareCo/executor/blob/27dccb896fbaf9d1790496d1a8f131b790c89c68/apps/local/src/executor.ts) | Newly added source `27dccb896fbaf9d1790496d1a8f131b790c89c68`, Effect4.0.0-beta.59; SDK/testing and self-host serve roots | Owned versus borrowed database handles, fallible construction after release registration, zero-output finalizers, scoped fresh HTTP test fixtures and honest dynamic plugin contracts |

The infrastructure repository's historical alternate URL resolves to the same commit/tree, not independent corroboration. The explicit graph repository redirects from its older owner. Private local notes retain cache/patch provenance; public examples remain generic.

## Patterns

| Pattern | Adopted direction | Boundary / receipt |
| --- | --- | --- |
| Lazy effect values | Explicit execution, checked failures and nominal requirements | Shared checker and both emitters; portable conformance tests |
| Native declaration import | Consume host signatures automatically | Primitive Go free functions work; behavior metadata is a reviewed assertion |
| Owned concurrency | A scope shuts down children before releasing resources | Go runtime and JS ownership adapter; cancellation is a request |
| Focused compiler tooling | CLI/MCP share checked contracts, revisions, lint and graphs | Single-file model; no persistent semantic workspace or checked editing yet |
| Closed application states | Payload-owning alternatives and exhaustive interpretation | Records, closed ADTs and matching are implemented; external decoding remains proposed |
| Explicit wire decoding | Static types do not validate stored or incoming data | Codec and migration library remains proposed |
| Dependent providers | Construction has its own requirements, failures and owner | Configuration and captured dependency values are checked; fallible acquisition, initialization sharing and cycles remain open |
| Declarative layers | Checked bindings/merges/replacements assemble a lazy graph; provide once at an owner | Owner contract and refreshed source audit adopted; [five-unit implementation](docs/specs/layers.md) remains open |
| Bounded event delivery | Track items, bytes and in-flight acknowledgement | Streams, queues and delivery budgets remain proposed |
| Durable command admission | Correlation, transactional receipts and outbox recovery | Application/storage obligations, not a syntax guarantee |
| Owned tests | Fresh case scope, assertions, completed shutdown and preserved causes | `ef test`, causal latches and scheduler-backed virtual time have shared Go/JS receipts; reusable platform fixtures remain open |

See [showcases](docs/showcases.md), [pattern review](docs/research/effect-native-showcases.md), [tooling](docs/tooling.md), [testing](docs/testing.md) and [adoption gates](docs/contender-roadmap.md).

## Settled

- Adopt first-class layer graph assembly from the owner contract and production composition audit. Node identity determines sharing; binding visibility is independent of graph leaves and selected acquisition effects. Replace deliberately before solving effective construction edges. Reject last-writer precedence, roots-only output inference, type/config-equality memoization and constructor-failure erasure: they break **Explicit contracts and clear guardrails** and **Owned lifetimes**. Runtime concurrency/rollback/cleanup remains executable implementation work.

- A canonical shared formatter is required. Inspected Go1.27.0 `go/format` and Gleam `compiler-cli/src/format.rs` at52e735c82d42811dd08d29d5508f564da081fd7d: adopt syntax-only printing and shared check/stdin/write paths, with Effra-specific preservation of line-sensitive lint directives. The shared syntax printer is integrated; CLI/MCP adapters and editor formatting remain open under the [formatting contract](docs/specs/formatting.md).

- Use LSP's standard severities and UTF-16 range semantics as an adapter over shared compiler snapshots, retaining byte anchors and explicit checked/unknown state. The [tooling contract](docs/specs/semantic-tooling.md) records the pinned 3.18 source inspection; CLI/MCP must expose the same full types and diagnostics without requiring an editor process.

- Adopt Borgo's direction of direct Go-targeted application code with first-class closed data and patterns; records/enums/match already have Go/JS receipts. Its broader interop and inference claims are comparison inputs, not inherited Effra guarantees.
- Adopt Effect's behavioral contracts as explicit conformance inputs. Native error/service rows, service identity and ownership are compiler concepts; retry, cache, routing and other runtime policies remain reusable library implementations.
- Adopt the licensed upstream test suite, pinned by a submodule commit and selected by an Effra-owned integrity manifest, plus an executable behavior mapping. Reject vendoring a copy of upstream, and reject counting upstream TypeScript reference tests as native parity: it breaks **Explicit contracts and clear guardrails**.
- Adopt typed-AST host declaration projection, explicit target capabilities, source-location preservation and independently versioned interface/build artifacts from the [host compilation comparison](docs/research/host-language-compilation.md). Keep automatic declaration ingestion as the adoption goal; reject requiring handwritten bindings for every ordinary import. A content mapper complements Effra tooling but does not own its guarantees.
- Adopt ordinary transition functions and a declaration-only machine binding from the [XState v6 and Effect Machine comparison](docs/research/machines-and-transforming-codecs.md). The declaration is justified by its static `Stay` same-tag check, which no compared system has ([machine admission gate](docs/specs/state-machines.md#construct-admission-gate), 2026-10-07). Keep conservative graph edges explicit; require completed entry cleanup and bounded admission as separately tested Effra policies.
- Adopt opt-in structural codec derivation plus named transforming witnesses with independent directional contracts. Preserve multiple representations, versioned wire policies and transformation-specific laws; reject global witness uniqueness and automatic reversibility claims.
- `.ef` text remains authoritative. Dependency graphs are rebuildable views with revision-scoped expression IDs.
- Compiler soundness diagnostics cannot be disabled by optional lint. Unchecked files do not receive authoritative expression types or dependency graphs.
- Lazy construction does not execute an effect. Unused local recipes receive lint advice; bare discarded recipes remain compiler errors.
- A managed timeout waits for shutdown. A process watchdog may force termination and must report cleanup as unconfirmed.
- Host signatures do not prove purity, cancellation, retained-reference safety or resource ownership. Partial native results survive explicit adaptation.
- Tests supply only assertions implicitly. Fixture services stay explicit, and live host/time capabilities require opt-in. This is a capability check, not an OS sandbox.
- Application-specific state machines, identity, transaction and overflow policies remain explicit even when syntax becomes shorter.

## To survey

- Make the [Effect language/library partition](docs/research/effect-language-boundary.md) executable through common typed functions, rows and ownership rather than library-specific compiler branches. Preserve automatic Go standard-protocol interoperability through the [finite four-unit contract](docs/specs/go-protocol-interop.md): method-set assignability, native values, partial results and explicit lifetime/cancellation contracts. Installed Go1.27.0 `src/io`, `src/io/fs`, `src/os`, `src/net`, `src/context` and `src/go/types` ground the acceptance cases. Current primitive imports establish none of the wider host-object guarantees.

- Adopt the owner-refined [ordinary actor contract](docs/specs/actors.md) through service/impl/effect functions and shared runtime/library capabilities; a machine is one behavior form. Handler and receive-loop usage does not justify another DSL. Resolve portable message ownership, mutable native aliases, correlation, acknowledgement and full inspection with general checked mechanisms. Define optional supervision for either behavior through fresh factories, full causes, cleanup-before-restart and budgets. Separately qualify durable addressing, transactional acknowledgement, deduplication/fencing, hibernation and workflow replay. The [actor comparison](docs/research/actor-model-and-durability.md) is source research, not implementation or production fault-tolerance evidence.

- Validate the proposed opaque-construction boundary for identifiers, codec-validated values and authorized resources before adding general value-indexed evidence. The [comparison](docs/research/opaque-values-and-evidence.md) records compiler, foreign, mutability, freshness and cost obligations; source review is complete, language support is not implemented.

- Validate the adopted XState v6/Effect Machine profile through pure plan tests and owned Go/JS actors. Include stale identity, conservative graph edges, saturation, re-entry and completed shutdown; source comparison is complete, implementation receipts remain open.
- Validate the adopted Serde/Effect codec design with multiple named wire representations, directional rows, normalization, cancellation and bounded derivation. Source comparison is complete, implementation receipts remain open.
- Validate adopted Gleam/ReScript/TypeScript compilation patterns with Effra consumer, diagnostic-map and cache-invalidation fixtures; source comparison is complete, implementation and cost receipts remain open.
- Which finite row and ordinary type-parameter mechanisms express reusable effect combinators without complex conditional-type inference or a compiler operation per combinator?
- Validate the adopted [layer contract](docs/specs/layers.md) with causal shared acquisition, runtime input-identity validation, independent fresh builds, replacement changes to rows/cycles, inherited application services, selected hidden startup effects and completed rollback/shutdown. Pinned Effect first-requester interruption differs deliberately from build-owned producers. Static source comparisons do not prove runtime behavior.
- Which codec/endpoint declarations remove duplicate domain/transport signatures while preserving explicit validation and wire policy?
- Extend structured nominal types and checked matches with codecs, containers and package-qualified identities without whole-program inference.
- Extend integrated bounded handle-ownership summaries to typed callback results while preserving borrowed outer handles and conservative uncertainty.
- Extend checked provider construction to shared acquisition and cycle explanations; enrich the same dependency graph rather than inventing another analyzer.
- Build revision-bound fixes on the implemented reasoned named lint suppressions; validate edits before they are offered and preserve comments in formatting.
- Extend integrated scheduler-backed time and causal synchronization with reusable scoped platform fixtures; retain shared sleep/timeout behavior tests.
- Measure native import reuse and matched cold/warm/private-edit build regimes before making speed claims.
