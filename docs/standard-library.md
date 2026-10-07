# A bundled Effect-grade standard library

Direction recorded from the owner on 2026-10-05. Effra's distribution should include the capabilities needed to build an Effect-style server application on Go. This is the target parity map, not a claim that the current prototype already supplies the full library.

The [finite capability contracts](specs/standard-library-capabilities.md) turn this direction into five open implementation batches, causal acceptance cases and visible exclusions. They incorporate pinned Effect4.0.1 source/tests and generic sharing, batching and worker patterns from application-source review.

## Distribution and compiler boundary

Ship the standard library with the compiler and managed runtime, under one compatible release. A Go application should not need a second language toolchain or handwritten wrappers for the basic concurrency, configuration, observability and platform facilities. JS can implement portable contracts using pinned Effect, while Go has native implementations.

The compiler owns effect rows, nominal type identity, checked provision, target restrictions and enforceable ownership rules. Bundled libraries own algorithms and policies: retry schedules, bounded queues, streams, pools, caches and adapters. A facility needs compiler support only when its contract requires new language semantics. Keep the implicit prelude small; ordinary library modules still expose explicit contracts and remain inspectable through the same CLI/MCP model.

The [Effect-to-language partition](research/effect-language-boundary.md) records the owner's 2026-10-06 direction: make contracts, sequencing, data and ownership regular language constructs; preserve runtime primitives where behavior needs them and keep facility policies in modules. Standard Go protocols and native host values must cross the interop seam automatically, with ownership/cancellation trust distinguished from method-set compatibility.

Versioned library contracts participate in semantic revisions. Describe target support and limitations alongside signatures. Unsupported capabilities must diagnose or remain unavailable; a familiar API name cannot imply unverified Effect parity. Public Go/JS facilities need shared behavior tests for cancellation, cleanup, causes and boundary cases.

Small application binaries are an explicit north star. Bundled availability does not imply retention: select only reachable runtime modules, keep unused initialization and registration out, and lower any fluent calls as ordinary statically resolved operations. The [binary reachability contract](specs/binary-reachability.md) requires minimal/core/codec/HTTP receipts and separates compiler distribution size from application and external runtime costs.

## Capability parity map

Reference families were inspected in the installed pinned Effect source, including its core, concurrency, data, scheduling and service modules. This map tracks behavioral families rather than reproducing every TypeScript overload.

| Family | Current Effra receipt | Required library contract |
| --- | --- | --- |
| Core effects, failures and recovery | Lazy recipes, explicit rows, execution, pure fallback catch | Payload-aware effectful handlers, typed effect-function values, composable row contracts |
| Data and interpretation | Nominal records, payload enums, exhaustive match | Option/Result, collections, iteration and constrained generics |
| Scope, fibers and causes | Owned forks, joins, interruption and waited cleanup on Go/JS | Public acquisition/release, finalization combinators, supervision and bounded concurrency |
| Synchronization | One-shot latch with shared Go/JS causal lifecycle tests | Typed Deferred, Ref, semaphore, bounded queue and publish/subscribe with cancellation-aware admission |
| Time and schedules | Sleep/timeouts and scheduler-backed virtual test time | Clock/time representations, retry/repeat schedules, jitter and explicit deadline policy |
| Providers | Checked configuration, dependency capture and reusable materialized values | Fallible scoped construction, shared acquisition policy, release ordering and cycle diagnostics |
| Streams and sinks | Proposed | Backpressure, bounded retained items/bytes, resource ownership, interruption and terminal failure |
| Pools, caches and batching | Proposed | Producer ownership, waiter cancellation, capacity/eviction, sharing, failure policy and resource release |
| Configuration and validation | Limited primitive services | Typed environment/configuration, secrets/redaction, external codecs, validation and wire compatibility |
| Observability | Bounded current-scope snapshots and inspectable static contracts | Structured logs, tracing, metrics and bounded source/task/resource correlation |
| Platform services | Native files and a small HTTP server; restricted automatic Go imports | Managed filesystem/process/network adapters, full HTTP contracts, codecs, SQL/client integration |
| Transactions and coordinated state | Proposed | Explicit atomic in-memory primitives where needed; database transactions remain real storage-service contracts |
| Testing | Fresh case ownership, assertions, watchdog, causal latches and virtual scheduling | Reusable fixture modules, scoped scratch resources and broader failure injection |

"Complete" means each required family has explicit accepted behavior, supported-target receipts and visible exclusions. It does not mean importing browser-specific APIs into a Go server or inheriting application-specific transaction, authorization and durability guarantees from a library name.

## Build order and gates

The detailed dependency graph is now [owned core → services/flow → shared/platform → telemetry/batching](specs/standard-library-capabilities.md#five-implementation-batches). Time primitives precede expiry/retry; bounded flow precedes streamed platform adapters. The stages below remain the broad destination; the finite contract controls implementation order and closure.

1. Establish modules, package-qualified identity, constrained generics, collections and typed effect-function/handler contracts. Gate: reusable data and recovery helpers compile without per-type compiler special cases, preserve explicit rows and remain fast to inspect.
2. Build scoped provider acquisition, synchronization and bounded concurrency primitives. Gate: producer/waiter cancellation, sharing, shutdown and cleanup failures have public causal tests, and missing dependencies explain their construction path.
3. Add codecs/configuration and the server/platform adapters needed by an actual application port. Gate: malformed external input is rejected; native handles, processes and response bodies close under cancellation; automatic host signatures remain the default.
4. Add streams/sinks, pools/caches/batching, schedules and observability on those foundations. Gate: published capacity, failure, lifecycle and runtime-inspection contracts hold under overload and failure injection.
5. Expand parity by real adoption fixtures, with explicit target matrices and version compatibility. Gate: supported Go/JS behavior agrees; genuine target restrictions and unresolved application policies remain visible.

Develop tooling and measurements throughout. Library signatures should be shipped as reusable checked interfaces; importing one facility must not require parsing or checking the whole standard library on every edit. Generate/link only required native modules, avoid hidden initialization work, and report import/check/emission/build/runtime costs separately. Full library availability must preserve the fast iteration north star.

The current [foundation spec](specs/production-foundations.md) establishes data, provider, lifetime, lint and test seams. The [server benchmarks](specs/server-benchmarks.md) measure current server costs honestly; they are a baseline for later library and runtime changes, not proof of complete parity.

The finite foundation stage is integrated and reviewed; its [retained receipts](receipts/foundations-2026-10-06/README.md) include full gates, race checks and public-tool checks. Broader library parity, first-class function contracts and the server framework remain separate open work.

## Upstream conformance inputs

Use pinned Effect implementations and their tests as direct inputs to each library family. Pin the licensed upstream test suite by commit, verify the selected files' hashes and map its behavior cases to executable Effra tests. Distinguish reference-only, ported/passing, deliberately different and not-yet-supported cases; unsupported families cannot disappear from a parity report. Real application usage should constrain composition and operational fixtures, while public examples stay generic. The [native server contract](specs/native-server-contracts.md) applies this requirement to the first codec and HTTP/RPC slice.
