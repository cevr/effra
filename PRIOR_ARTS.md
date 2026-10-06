# Pattern and guardrail index

Effra's public architecture is described through generic patterns. This index records the decisions that source reviews inform, without making an external application's structure part of the language contract. Read [NORTH_STAR.md](NORTH_STAR.md) and [GLOSSARY.md](GLOSSARY.md) alongside it.

## Language and library references

Owner direction refreshed 2026-10-06: preserve Go-like simplicity, Effect-style guarantees and Borgo-like algebraic data/abstractions. This repository keeps the established `PRIOR_ARTS.md` name as its canonical prior-art index.

| Source | Revision / paths | Read it for | Compare with |
| --- | --- | --- | --- |
| [Go](https://github.com/golang/go) | Toolchain-matched source; `src/net/http`, `src/context`, `src/cmd/compile` | Ordinary control flow, host declarations, native server costs and separate compile/link work | Native emission, import adapter, managed HTTP and stage measurements |
| [Borgo](https://github.com/borgo-lang/borgo) | `3b9f01578941fb00ed93756e2fadc009feb50128`; `compiler/src/type_.rs`, `infer.rs`, `exhaustive.rs`, `codegen.rs`, `compiler/test/infer-file.md` | ADTs, exhaustive patterns, functions/generics and Go ecosystem integration with an approachable surface | Canonical types, checker, Go lowering and public data/library examples |
| [Effect](https://github.com/Effect-TS/effect) | Runtime pin `effect@4.0.1`, commit `460272d30457f4697d8b8c52cad41caccbcace08`; `packages/effect/src`, `packages/effect/test`, `packages/effect/typetest`, platform server tests | Typed effect composition, scopes, cancellation, provider construction, codecs, HTTP/RPC and behavioral/type-test invariants | Bundled interfaces/runtime, portable conformance and [native server contract](docs/specs/native-server-contracts.md) |
| [Gleam](https://github.com/gleam-lang/gleam) | `52e735c82d42811dd08d29d5508f564da081fd7d`; `compiler-core/src/javascript/tests/externals.rs`, `javascript/typescript.rs`, `build/module_loader.rs` | Explicit target implementations, generated host declarations, representation boundaries and module metadata reuse | Target capability checks, automatic imports, emitted declaration fidelity and separate build-stage receipts |
| [ReScript](https://github.com/rescript-lang/rescript) | `30ce3698ef683137039c7eb7ded2915b278d26ee`; `tests/tests/src/ffi_test.res`, `tests/gentype_tests`, `rewatch/src/build`, `compiler/core/js_source_map.ml` | Host calling conventions, generated TypeScript adapters, tagged/opaque representations, incremental compilation and source locations | Avoid routine handwritten bindings; disclose representation adapters and keep host types distinct from runtime validation |
| [TypeScript](https://github.com/microsoft/TypeScript) | `a1ef42b9ea7032fa60df127d42b4c86fd2a110ee`; `tsc/internal/contentmapper`, `tsc/internal/execute/incremental/buildinfo_contentmapper_test.go`; original [content mapper PR](https://github.com/microsoft/typescript-go/pull/4712) | Host declaration reuse, mapped virtual source, diagnostic positions and configuration-aware cache identity | Optional JS tooling projection; Effra remains the authority for effect/owner checking and Go builds must not require TypeScript |
| [XState v6](https://github.com/statelyai/xstate/pull/5543) — main machine reference | Active `next` branch pinned `2146ae26ebfc7e6a624b3a1f237f9e6ddc30b9f5` (`6.0.0-alpha.64`), superseding original PR5257; `docs/xstate-v5-to-v6.md`, `packages/core/src`, `packages/core/test`, `packages/xstate-effect` | Ordinary function transitions, state-specific contracts/input, schema metadata, internal lifecycle identity, pure stepping, Effect integration and inspection | Checked machine plan, ordinary ADT payload construction, owned actor runtime, test generation and explicit codec boundaries; do not infer v6 behavior from v5 |
| [Effect Machine](https://github.com/cevr/effect-machine) | `176697cf20006f5f539bf04ff2e609bfe1563e22`; `src/internal/{runtime,transition}.ts`, `test/{stop-completion,reenter,state-timeout,type-constraints}.test.ts` | State/actor-owned scopes, exact shutdown outcomes, re-entry, typed requirements and causal tests | Managed actor execution and upstream behavioral mapping |
| [Erlang/OTP](https://www.erlang.org/doc/system/sup_princ.html) | Official OTP29.1.1 supervision and code-loading documentation, read2026-10-06 | Restart strategies/budgets, escalation, current/old module code and explicit runtime costs | Optional local supervision; native process handover and VM-specific code replacement remain distinct |
| [Effect Cluster](https://github.com/Effect-TS/effect/tree/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/cluster) | Runtime pin460272d; Entity, ClusterSchema, MessageStorage and Sharding | Typed addressing, explicit persistence/concurrency and provider-dependent transactions | Checked actor contracts with opt-in durability, never implicit exactly-once host effects |
| [celld](https://celld.dev/docs/guarantees/) | Official guarantees documentation, read2026-10-06; no engine audit | Conditional storage ownership, epochs, durability acknowledgement and external process supervision | Deployment/storage provider requirements and their qualification tests |
| [Rivet](https://rivet.dev/actors/docs/) | Source0247751bdafc82e72f3e4f52082fc2c1694a5c81; selected native.ts/config.ts persistence paths; current action/state/workflow docs | Parallel actions, throttled versus awaited saves, actor inspection and explicit replay/versioning | Ordinary serialized Effra steps, explicit acknowledgement policy and separate workflow contract |
| [Cloudflare Durable Objects](https://developers.cloudflare.com/durable-objects/) | Official state/gates/lifecycle/alarms docs, read2026-10-06 | Stable object identity, async interleaving, storage gates, eviction and retry boundaries | Actor ownership, safe codec-governed recovery and explicit external idempotency |
| [Serde](https://github.com/serde-rs/serde) | `6693a89cca77e0151437da1c7f890090b9ebf04c`; `serde_derive/src/{ser,de}.rs`, `internals/check.rs`, `test_suite/tests/test_annotations.rs` | Structural derivation, explicit adapters and fallible conversions, conflicting-attribute diagnostics | Finite derived codec witnesses plus ordinary typed transformation functions; no required procedural macro system |
| [Ghosts of Departed Proofs for TypeScript](https://github.com/rauchg/gdp-ts) | `ebd0af9cae423997a43a024dc6d6738b0895bbec`; `src/index.ts`, `src/lint/shared.ts`, `test/types.ts`, documented limits | Scoped subject identities, evidence construction authority, wrong-subject rejection and the limits of stale evidence | [Opaque values and checked evidence](docs/research/opaque-values-and-evidence.md); candidate Go-like authorized values before exposing generative proof parameters |
| [Ghosts of Departed Proofs paper](https://kataskeue.com/gdp.pdf) and [Haskell library](https://github.com/matt-noonan/gdp) | Matt Noonan, Haskell2018, DOI10.1145/3242744.3242755; library `853abfd43ddac462bba53fc4cceac0f8dd2bf29c`, `Theory/Named.hs`, `Data/Refined.hs`, `Logic/Proof.hs` | Hidden newtypes, nominal roles, rank-2 fresh names, relational evidence and explicit axiom trust | Small reusable language mechanisms; distinguish subject identity, construction authority, lifetime and freshness |

Application usage is surveyed as generic patterns below. Exact private checkout pointers stay in local research notes; private source is not copied into this public repository. Upstream reference tests retain their own license and provenance and remain distinct from passing Effra tests.

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
| Bounded event delivery | Track items, bytes and in-flight acknowledgement | Streams, queues and delivery budgets remain proposed |
| Durable command admission | Correlation, transactional receipts and outbox recovery | Application/storage obligations, not a syntax guarantee |
| Owned tests | Fresh case scope, assertions, completed shutdown and preserved causes | `ef test`, causal latches and scheduler-backed virtual time have shared Go/JS receipts; reusable platform fixtures remain open |

See [showcases](docs/showcases.md), [pattern review](docs/research/effect-native-showcases.md), [tooling](docs/tooling.md), [testing](docs/testing.md) and [adoption gates](docs/contender-roadmap.md).

## Settled

- A canonical shared formatter is required. Inspected Go1.27.0 `go/format` and Gleam `compiler-cli/src/format.rs` at52e735c82d42811dd08d29d5508f564da081fd7d: adopt syntax-only printing and shared check/stdin/write paths, with Effra-specific preservation of line-sensitive lint directives. The shared syntax printer is integrated; CLI/MCP adapters and editor formatting remain open under the [formatting contract](docs/specs/formatting.md).

- Use LSP's standard severities and UTF-16 range semantics as an adapter over shared compiler snapshots, retaining byte anchors and explicit checked/unknown state. The [tooling contract](docs/specs/semantic-tooling.md) records the pinned 3.18 source inspection; CLI/MCP must expose the same full types and diagnostics without requiring an editor process.

- Adopt Borgo's direction of direct Go-targeted application code with first-class closed data and patterns; records/enums/match already have Go/JS receipts. Its broader interop and inference claims are comparison inputs, not inherited Effra guarantees.
- Adopt Effect's behavioral contracts as explicit conformance inputs. Native error/service rows, service identity and ownership are compiler concepts; retry, cache, routing and other runtime policies remain reusable library implementations.
- Adopt a licensed pinned upstream test snapshot plus executable behavior mapping. Reject counting copied TypeScript reference tests as native parity: it breaks **Explicit contracts and clear guardrails**.
- Adopt typed-AST host declaration projection, explicit target capabilities, source-location preservation and independently versioned interface/build artifacts from the [host compilation comparison](docs/research/host-language-compilation.md). Keep automatic declaration ingestion as the adoption goal; reject requiring handwritten bindings for every ordinary import. A content mapper complements Effra tooling but does not own its guarantees.
- Adopt ordinary transition functions and a declaration-only machine binding from the [XState v6 and Effect Machine comparison](docs/research/machines-and-transforming-codecs.md). Keep conservative graph edges explicit; require completed entry cleanup and bounded admission as separately tested Effra policies.
- Adopt opt-in structural codec derivation plus named transforming witnesses with independent directional contracts. Preserve multiple representations, versioned wire policies and transformation-specific laws; reject global witness uniqueness and automatic reversibility claims.
- `.ef` text remains authoritative. Dependency graphs are rebuildable views with revision-scoped expression IDs.
- Compiler soundness diagnostics cannot be disabled by optional lint. Unchecked files do not receive authoritative expression types or dependency graphs.
- Lazy construction does not execute an effect. Unused local recipes receive lint advice; bare discarded recipes remain compiler errors.
- A managed timeout waits for shutdown. A process watchdog may force termination and must report cleanup as unconfirmed.
- Host signatures do not prove purity, cancellation, retained-reference safety or resource ownership. Partial native results survive explicit adaptation.
- Tests supply only assertions implicitly. Fixture services stay explicit, and live host/time capabilities require opt-in. This is a capability check, not an OS sandbox.
- Application-specific state machines, identity, transaction and overflow policies remain explicit even when syntax becomes shorter.

## To survey

- Make the [Effect language/library partition](docs/research/effect-language-boundary.md) executable through common typed functions, rows and ownership rather than library-specific compiler branches. Preserve automatic Go standard-protocol interoperability: method-set assignability, native values, partial results and explicit lifetime/cancellation contracts. Current primitive imports establish none of the wider host-object guarantees.

- Define the finite optional local-supervision contract after the first machine profile: fresh factories/acquisitions, full causes, cleanup-before-restart, restart budgets and graph inspection. Separately qualify durable addressing, transactional acknowledgement, deduplication/fencing, hibernation and workflow replay before implementing their providers. The [actor comparison](docs/research/actor-model-and-durability.md) is source research, not implementation or production fault-tolerance evidence.

- Validate the proposed opaque-construction boundary for identifiers, codec-validated values and authorized resources before adding general value-indexed evidence. The [comparison](docs/research/opaque-values-and-evidence.md) records compiler, foreign, mutability, freshness and cost obligations; source review is complete, language support is not implemented.

- Validate the adopted XState v6/Effect Machine profile through pure plan tests and owned Go/JS actors. Include stale identity, conservative graph edges, saturation, re-entry and completed shutdown; source comparison is complete, implementation receipts remain open.
- Validate the adopted Serde/Effect codec design with multiple named wire representations, directional rows, normalization, cancellation and bounded derivation. Source comparison is complete, implementation receipts remain open.
- Validate adopted Gleam/ReScript/TypeScript compilation patterns with Effra consumer, diagnostic-map and cache-invalidation fixtures; source comparison is complete, implementation and cost receipts remain open.
- Which finite row and ordinary type-parameter mechanisms express reusable effect combinators without complex conditional-type inference or a compiler operation per combinator?
- Which provider acquisition/sharing interface makes recipe identity, materialized value identity and allocation ownership obvious at a call site?
- Which codec/endpoint declarations remove duplicate domain/transport signatures while preserving explicit validation and wire policy?
- Extend structured nominal types and checked matches with codecs, containers and package-qualified identities without whole-program inference.
- Extend integrated bounded handle-ownership summaries to typed callback results while preserving borrowed outer handles and conservative uncertainty.
- Extend checked provider construction to shared acquisition and cycle explanations; enrich the same dependency graph rather than inventing another analyzer.
- Build revision-bound fixes on the implemented reasoned named lint suppressions; validate edits before they are offered and preserve comments in formatting.
- Extend integrated scheduler-backed time and causal synchronization with reusable scoped platform fixtures; retain shared sleep/timeout behavior tests.
- Measure native import reuse and matched cold/warm/private-edit build regimes before making speed claims.
