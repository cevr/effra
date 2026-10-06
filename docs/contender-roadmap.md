# What Effra needs to earn adoption

Assessment prepared 2026-10-05 against the checked prototype and the [real-world source review](research/effect-native-showcases.md). This is a proposed build order and set of acceptance gates, not implemented features or a production-readiness claim.

Effra has a credible semantic core: lazy execution, explicit failure/service contracts, native Go artifacts, owned child/resource cleanup, a shared Go/JS lifecycle corpus and compiler-backed inspection. Practical adoption now depends on representing real application data, using real host libraries, constructing real providers and preserving the operational policies those applications need.

Records, closed ADTs and match now cover the first application-data slice. The source review shows why they are insufficient by themselves: notification providers need dependent initialization; SDKs return named objects; event consumers need decoding and bounded replay; command services need transactions and stable identity.

The owner-directed [bundled standard library parity map](standard-library.md) defines the larger distribution: core contracts stay compiler-enforced, while native facilities ship as inspectable libraries under the same release. Modules, reusable types and effectful handler/combinator seams precede broad library expansion.

## Gaps exposed by the inspected applications

| Real pattern | What Effra already supplies | What blocks a faithful port | Owner |
| --- | --- | --- | --- |
| Phase states and transition decisions | Explicit function/effect contracts | Codecs, usable collections and control flow beyond the closed data slice | Compiler + libraries |
| Stored events and interaction records | Failure propagation | Explicit codecs, validation, versioned wire formats, nominal identifiers | Compiler-supported types + codec library + application migrations |
| Notification providers and service adapters | Nominal services and explicit provision | Fallible scoped initialization, acquisition sharing and cycles | Checker + runtime/provider library |
| Existing Go clients, database pools and processes | Primitive package-function imports, partial-result retention, reviewed context forwarding | Named types, pointers/nil, fields, receiver methods, interfaces, common native numbers and multi-results | Go importer/type compatibility + adapter contracts |
| Replay/live consumers | Scopes, child joining and cancellation | Bounded queues/streams, subscription acquisition, acknowledgement and overflow protocol | Runtime primitives + stream library + application protocol |
| Admission and durable receipts | Managed execution | Transactions, identity/correlation, deduplication, outbox delivery and crash recovery | Application/storage services |
| Application latest-work and process supervision | Owned replacement and native executable entry | Process/pipe/watch adapters, generation-safe publication, restart policy and bounded output | Platform adapters + concurrency library + application policy |
| Agent-guided maintenance | CLI/MCP contracts, byte-anchor types, lint, static dependency graphs and revisions | Multi-file identities, full expression ranges, imported/native type detail, editor diagnostics and runtime/source correlation | Compiler/workspace tools + runtime instrumentation |

The prototype's remaining barriers are concrete: native imported calls support only restricted free-function shapes; provider constructors accept checked configuration and capture explicit service dependencies, but cannot acquire resources fallibly or memoize scoped initialization; recovery accepts a pure replacement value. See [checker](../internal/compiler/semantic.go), [Go import normalization](../internal/compiler/imports.go), [implemented limits](prototype.md), and [runtime/HTTP contracts](runtime.md).

## Build order

### 1. Application data and package contracts

Add codecs and explicit external-data validation around records and closed ADTs. Add ordinary collections, iteration and the scalar operations needed by the first port. Option and Result should be ordinary reusable sums rather than special cases for every API. Introduce limited generics where the actual container/codec use requires them.

Extend the existing structured canonical type representation as these types arrive; payload checking, native compatibility and inspection must preserve declaration identity. Keep source-visible public contracts explicit. Matching should use declared alternatives and local payload types; whole-program inference is unnecessary for this slice.

Add modules and package-qualified nominal identities so services and types can be shared across files without collisions. Resolve the representation seam with Go interop while designing records/sums: a language-only representation that forces copying every SDK object would repeat the interop adoption problem. Efficient host values and portable wire encodings are separate contracts.

**Exit gate:** a multi-file state/decision example runs on Go and JS. Codec rejection, incompatible identifiers and undeclared failures produce focused diagnostics. CLI/MCP inspect variant payloads and point to their declarations. A representative codec fixture has measured frontend time and memory.

### 2. Real host objects and managed adapters

Extend automatic Go imports to named types, fields and receiver methods, pointers with honest nil handling, common numbers, interfaces and supported multi-results. Define checked conversion rules and explicit target restrictions. Callbacks and retained references need visible behavioral/lifetime contracts; a Go declaration does not prove them safe to retain or share.

Expose source-level managed acquisition/release and payload-preserving error adaptation. A database Rows handle, response body or process pipe must have an owner, close policy and cancellation behavior. Runtime AcquireRelease exists, but arbitrary host acquisition cannot yet be expressed ergonomically from source. Domain errors need inspectable native detail and effectful recovery where warranted.

Automatic signatures should remain the default. Supplemental metadata should describe exceptional behavior and ownership, not redeclare every callable. Host functions remain lazy; returned partial values survive explicit error adaptation. Import summaries should be cached using module/toolchain/build inputs and contract identity, with conservative invalidation when correctness is uncertain.

**Exit gate:** use an actual Go HTTP client and SQL client from `.ef`, including native options/handles and methods, without handwritten signatures for each symbol. Verify partial values, nil cases, cancellation, resource closure, and changed dependency declarations. Run the Go race detector around shared-handle cases. Record the required conversions and behavioral annotations rather than claiming universal transparent interop.

The JS target can remain a clearly defined portable subset during this Go adoption gate. TypeScript declaration consumption is a separate host bridge with its own acceptance tests; content mapping alone does not supply it.

### 3. Providers that can run a real application

Implementations now accept configuration and capture declared construction dependencies. Extend that construction contract to initialize resources effectfully with explicit failures. Providers need one scoped sharing policy, a defined release order, cycle diagnostics and explicit startup failure behavior. Supplying the same pool to two consumers should not silently create two pools.

Add typed effectful error handlers so callers can inspect payloads, translate failures or make a bounded recovery decision. Add constrained effect-function types and row composition as real library combinators need them. Preserve readable public rows and local checking instead of requiring broad higher-order inference up front.

**Exit gate:** construct a database-backed service and a notification service from explicit configuration; share their dependencies; substitute in-memory providers for tests; fail startup honestly; and close children before shared resources. Missing dependencies and provider cycles should explain the construction path. The notification example should consume a bound capability without exposing deployment plumbing to the domain function.

### 4. A small server library with deep capabilities

Grow the current HTTP provider into request/response types, bodies, status/headers, routing, codecs, streaming, disconnect cancellation and configurable limits. Add typed configuration, structured logging/tracing and clocks that deterministic concurrency tests can control.

Add bounded queues, channels/streams, concurrency limits and supervised tasks. Keep restart, retry and timeout policies explicit; retries require a real idempotency contract. Go's scheduler supplies execution, while Effra still owns admission, cancellation and joined cleanup. Scopes alone do not prove mutable data race freedom; synchronized handles and explicit sharing rules remain necessary.

Use Go drivers and SDKs behind deep services for SQL, networking and process work. A giant collection of per-function wrappers would defeat automatic interop. Shared facilities should encode a lifecycle or policy that callers would otherwise repeatedly implement.

**Exit gate:** an HTTP handler decodes input, calls a real service and maps typed failures to responses. A stream fixture proves bounded retained bytes/items, ACK accounting, overflow cleanup and disconnect shutdown. A process fixture proves pipe/output cleanup and cancellation that waits for exit. Both-target tests cover portable facilities; target-specific facilities remain labeled.

### 5. Fast, inspectable daily development

Develop this throughout the earlier slices. Add package/interface summaries and reusable import sessions, deterministic generated files, incremental body checks and explicit invalidation. Keep the Go build cache responsible for backend work. Rechecking an unchanged or privately edited module should avoid unnecessary host loading without allowing stale imported contracts.

Current receipts measure a small frontend fixture and roughly 51 ms warm imported checks, dominated by loading; they establish no matched Go build ratio. Benchmark the same application/workspace under cold, unchanged, private-edit, public-contract-edit and dependency-edit regimes. Report frontend, import, generation, compilation and linking separately. Measure HTTP latency, allocations, throughput and shutdown behavior too: compile speed is not runtime efficiency.

Add a language server, source maps and a multi-file semantic workspace shared by CLI/MCP. Runtime inspection needs bounded task/resource metadata, source/build correlation, wait-state observations and failure causes. Distinguish observed state from unknown or stale state; avoid dumping service payloads by default.

**Exit gate:** a user or agent can diagnose a missing provider, wrong match, failed initialization or blocked cleanup from source-level types and bounded runtime evidence. Stale revisions are rejected. Editing does not require reading generated Go. Performance targets are chosen from reproducible matched baselines, not an isolated microbenchmark.

## Three programs that should decide whether it is a contender

| Adoption fixture | Must prove | Where the comparison comes from |
| --- | --- | --- |
| JSON/SQL API with a background delivery worker | Validated input, payload errors, native SQL pool/transaction use, shared providers, honest auth policy, bounded concurrency, typed HTTP responses and completed SIGTERM shutdown | Service adapters and scoped capability composition |
| Agent event bridge with persisted commands | Versioned decoding, correlated decisions, transactional receipts/outbox, subscribe-before-replay ordering, bounded ACK retention, disconnect cleanup and restart recovery | Durable orchestration patterns |
| Build/process supervisor | Named host handles, pipe/watch acquisition, bounded output, failure classification, cancellation-to-exit completion and generation-safe replacement | Generic application process/latest-work patterns |

Implement each with a real external adapter and an interchangeable local provider. Compare it with an equivalent Go or Effect implementation using the same protocol, storage and workload. Record signature/wrapper duplication, conversions, readable contracts, debugging steps, compile/edit latency, runtime costs and recovery behavior. A smaller happy-path snippet with weaker semantics is not a passing port.

Inject failures: invalid JSON, duplicate/conflicting commands, cancellation during acquisition, cleanup defects, slow consumers, interrupted delivery and process restart. An at-least-once outbox needs receiver deduplication or a visible repeated-delivery outcome; the language must not advertise exactly-once remote effects from scopes or types.

These fixtures are acceptance criteria, not claims that the corresponding compiler, libraries or recovery protocols already exist. Production adoption also needs compatible language/runtime versions, reproducible releases, documentation and a maintenance path when imported libraries change.

## What can remain application or library work

SQL transactions, migrations, command receipts, correlation and outbox protocols can use existing databases and libraries behind explicit services. Effra needs to express and inspect their contracts; it does not need a new database or a universal durable workflow engine.

Deployment state and deferred output graphs remain framework responsibilities. Native `output` syntax is an optional experiment after the application gates pass; a library interface can first preserve its phase semantics. The Go server path should not require a TypeScript checker, a deployment framework or complete JS host parity.

The next concrete compiler unit is external codec support for records/closed ADTs, designed together with native representation. In parallel as a development discipline, establish the package/import measurements that prevent this richer type system from sacrificing the Go-like iteration speed that motivated the project.

## Implemented first tooling and testing slice

Native lint, a rule catalog, byte-anchor type queries, static dependency graphs and checked test discovery now share the compiler model through CLI/MCP. `ef test` supplies assertions and executes each case with a fresh owning scope on both targets. The process watchdog reports unconfirmed cleanup honestly. See [tooling](tooling.md) and [testing](testing.md).

The next testing slice needs causal latches, scheduler-backed virtual time and scoped scratch fixtures. The next ownership slice needs source-level provenance for handles: runtime rejection of a closed File/Fiber is currently stronger than static escape checking. Neither is replaced by more lint rules.
