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
| Owned tests | Fresh case scope, assertions, completed shutdown and preserved causes | `ef test` works on Go/JS; causal latches and virtual time remain open |

See [showcases](docs/showcases.md), [pattern review](docs/research/effect-native-showcases.md), [tooling](docs/tooling.md), [testing](docs/testing.md) and [adoption gates](docs/contender-roadmap.md).

## Settled

- Adopt Borgo's direction of direct Go-targeted application code with first-class closed data and patterns; records/enums/match already have Go/JS receipts. Its broader interop and inference claims are comparison inputs, not inherited Effra guarantees.
- Adopt Effect's behavioral contracts as explicit conformance inputs. Native error/service rows, service identity and ownership are compiler concepts; retry, cache, routing and other runtime policies remain reusable library implementations.
- Adopt a licensed pinned upstream test snapshot plus executable behavior mapping. Reject counting copied TypeScript reference tests as native parity: it breaks **Explicit contracts and clear guardrails**.
- Adopt typed-AST host declaration projection, explicit target capabilities, source-location preservation and independently versioned interface/build artifacts from the [host compilation comparison](docs/research/host-language-compilation.md). Keep automatic declaration ingestion as the adoption goal; reject requiring handwritten bindings for every ordinary import. A content mapper complements Effra tooling but does not own its guarantees.
- `.ef` text remains authoritative. Dependency graphs are rebuildable views with revision-scoped expression IDs.
- Compiler soundness diagnostics cannot be disabled by optional lint. Unchecked files do not receive authoritative expression types or dependency graphs.
- Lazy construction does not execute an effect. Unused local recipes receive lint advice; bare discarded recipes remain compiler errors.
- A managed timeout waits for shutdown. A process watchdog may force termination and must report cleanup as unconfirmed.
- Host signatures do not prove purity, cancellation, retained-reference safety or resource ownership. Partial native results survive explicit adaptation.
- Tests supply only assertions implicitly. Fixture services stay explicit, and live host/time capabilities require opt-in. This is a capability check, not an OS sandbox.
- Application-specific state machines, identity, transaction and overflow policies remain explicit even when syntax becomes shorter.

## To survey

- State machines as a checked language construct: compare Effect Machine and XState source/tests, state/event ADTs, guarded transition coverage, owned invocation, stale completion, bounded admission, deterministic testing and graph inspection. Distinguish finite machine semantics from persistence and workflow guarantees.
- Rust/Serde-style structural derivation plus Effect-style codecs: separate wire/domain types; composable decode and encode transformations with independent error/service rows, refinements, normalization laws and explicit wire evolution. Derivation is not limited to JSON serialization.
- Validate adopted Gleam/ReScript/TypeScript compilation patterns with Effra consumer, diagnostic-map and cache-invalidation fixtures; source comparison is complete, implementation and cost receipts remain open.
- Which finite row and ordinary type-parameter mechanisms express reusable effect combinators without complex conditional-type inference or a compiler operation per combinator?
- Which provider acquisition/sharing interface makes recipe identity, materialized value identity and allocation ownership obvious at a call site?
- Which codec/endpoint declarations remove duplicate domain/transport signatures while preserving explicit validation and wire policy?
- Extend structured nominal types and checked matches with codecs, containers and package-qualified identities without whole-program inference.
- Track handle ownership provenance so proven inner-scope escapes receive static diagnostics while borrowed outer handles remain valid.
- Extend checked provider construction to shared acquisition and cycle explanations; enrich the same dependency graph rather than inventing another analyzer.
- Build revision-bound fixes on the implemented reasoned named lint suppressions; validate edits before they are offered and preserve comments in formatting.
- Build scheduler-backed test time, causal synchronization and scoped fixtures; prove they control sleeps and timeout operators together.
- Measure native import reuse and matched cold/warm/private-edit build regimes before making speed claims.
