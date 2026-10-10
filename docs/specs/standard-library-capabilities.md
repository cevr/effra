# Bundled library capability contracts

Status: authorized implementation plan, not implemented parity. This finite plan follows the [library direction](../standard-library.md), [language abstractions](language-abstractions.md), [native server contract](native-server-contracts.md) and [binary reachability](binary-reachability.md). Existing interface, codec, server and machine tasks keep their scope. New library algorithms use ordinary checked interfaces; adding an operation must not require a compiler branch named after it.

The reference is Effect4.0.1 at `460272d30457f4697d8b8c52cad41caccbcace08`. Source and test comparisons are inputs, not passing Effra tests. Every delivered row records Go/JS support, explicit differences, source/test anchors and executable receipts. Copied tests remain licensed reference inputs until a native behavior case actually runs.

## Contracts shared by every family

Public functions declare failures with `raises` and capabilities with `uses`. Constructing or forwarding a recipe carries its contract without executing it. Effectful callbacks preserve their own failure/service rows; named functions are sufficient for the first slice. Unsupported higher-order placement diagnoses instead of erasing evidence.

Every resource-producing computation has an owner. Shared work belongs to its containing structure, not whichever request first waited for it. Cancelling one waiter must not cancel other live waiters. Last-waiter abandonment, success expiry and failure retention are separate, visible policies. Interruption is not a reusable cached result. Fixed pool/lease policies do not require irrelevant configuration arguments.

Generic mutable storage is not automatically safe just because the cell itself owns no OS handle. A stored handle, recipe or callback may retain a shorter-lived owner. Initial Ref/Deferred/Queue payload admission may be restricted to resource-free, closed portable data. Any broader admitted placement needs transitive ownership evidence and insertion/lookup/alias escape tests; otherwise it diagnoses. Immutable records/enums keep their existing ownership behavior. This is not general mutable borrow checking.

Capacity is an explicit, required choice; see [budget choices](#budget-choices). Item limits, bytes and in-flight work are distinct quantities. Byte streams and platform adapters require byte budgets; a two-chunk queue does not bound the size of either chunk. All waits respond to cancellation, and completion waits for owned children and cleanup. Watchdog termination reports cleanup unconfirmed. Cancellation masking only makes acquisition and release registration atomic; it cannot make an arbitrary host call cooperative.

Library modules are selected through versioned interfaces, with static call and runtime dependencies visible to CLI/MCP. Importing types or using a fluent spelling must not retain unused implementations or initialization. Each batch adds cold/warm/edit-stage and binary reachability receipts; shared-host observations are labelled and do not establish comparative speed.

### Budget choices

Budgets follow the [justify, don't ban](../design.md#language-design-principles) rule. Every budget is a required argument, so omitting it is a type error and the choice is visible at the call site. Each budget type has its own validated constructor and zero policy. A finite value keeps its refusal behavior and admits work before it is spawned, so no pending allocation exceeds the budget. `unbounded` is a legal value, never a default. A justifier rule in the default lint preset flags it until a next-line suppression with a reason accepts it; a suppression without a reason is `EFL004`. Failure rows follow from the operation's type, not the chosen value: an `unbounded` budget never refuses on its own dimension, but it removes no protocol failure from the row.

| Budget family | Required choice (illustrative spelling) | Justifier rule (upstream effect-oxlint id where one exists) |
| --- | --- | --- |
| Concurrency of traversal, effectful map and fan-out | `Concurrency(8)` or `Concurrency.unbounded` | `no-unbounded-concurrency` |
| Retry and repetition | attempt or elapsed-time budget, or `Attempts.unbounded` | `no-unbounded-retry`; explicit long-lived owners (actors, machine entries and, if admitted, layer start recipes) are exempt |
| Queue capacity, stream buffers and collection | `Items(n)`/`Bytes(n)`, or `Queue.unbounded`, `Items.unbounded`, `Bytes.unbounded` | `no-run-collect-on-unbounded-stream`; covering unbounded queues and buffers is an Effra extension of the upstream rule, which flags only collection |
| Process termination escalation | `killAfter: Elapsed(ms)` or `Elapsed.never` | `require-force-kill-after` |
| Actor and machine mailbox, retained-byte and reply/snapshot budgets | as in [actors](actors.md#proposed-library-surface) | `no-unbounded-mailbox` (Effra; no upstream rule) |
| Supervisor restart intensity | restart count within a period, or `unbounded` | `no-unbounded-restart` (Effra; the OTP restart-intensity analog) |
| Cache capacity, metric series cardinality, batch window and capacity | the family's capacity, or `unbounded` | `no-unbounded-cache`, `no-unbounded-metric-series`, `no-unbounded-batch` (Effra) |
| Request and codec body bytes (HTTP bodies, `Json.codec` `maxBodyBytes`) | required byte count; finite in the first profile, because the runtime refuses non-positive limits (`runtime/effra/codec.go:176`) | `unbounded` arrives with that runtime change and its justifier rule, `no-unbounded-body` (Effra) |

A semaphore's permit count and a fixed-size Pool's size are what those operations are, not protective budgets, so they take a required positive count with no `unbounded` value. An elastic pool's maximum size, if one is added, is a budget. A codec's `maxDepth` is a required parser safety cap, not a budget: it bounds parser recursion and failure-path length, and the first profile caps it at 512. Compiler analysis budgets, the machine's single reserved completion slot and the detached-fiber exclusion are separate contracts and stay fixed. Status: these rules are planned default-preset rules. Today the built-in lint rules are `EFL001`–`EFL004` and there is no built-in preset; each rule ships with its library family and the preset work, and each family's acceptance includes the finding and its reasoned suppression.

## Finite first capability set

Paths below are under the pinned `packages/effect/` unless another package is named. Each implementation ports selected positive and negative cases, retaining exact anchors in the conformance mapping.

| Family | First supported behavior | Required causal boundary | Reference anchors |
| --- | --- | --- | --- |
| Acquisition and concurrency | Public acquire/release, finalization with full Exit/Cause, ordered concurrency with a required budget, race, joined latest-work replacement | Late successful acquisition releases exactly once; loser/sibling cleanup completes before publication; no new work after failed admission | `src/internal/effect.ts`, `test/Effect.test.ts`, `test/Scope.test.ts`, `test/FiberHandle.test.ts` |
| Mutable state and synchronization | Typed one-shot Deferred, atomic Ref get/set/modify with pure callbacks, fixed-capacity semaphore | One cancelled waiter does not consume another's result; permits return on every exit; impossible permit requests diagnose/defect instead of parking forever | `src/Deferred.ts`, `src/Ref.ts`, `src/Semaphore.ts`, corresponding tests |
| Time | Ordinary Duration data, wall and monotonic clock reads, existing Scheduler-backed waits | Virtual fixtures control deadlines and retries; elapsed time uses monotonic time; invalid/overflow durations have a declared policy | `src/Clock.ts`, `src/Duration.ts`, `src/testing/TestClock.ts`, `test/TestClock.test.ts` |
| Fallible providers and layers | [First-class checked graph assembly](layers.md), owned constructor effects, identity sharing, deliberate replacement and borrowed materialized values | Concurrent waiters share once; partial startup rolls back; dependents release before dependencies; cleanup defects stay in Cause; separate builds remain independent | `src/Layer.ts`, `test/Layer.test.ts` |
| Configuration and redaction | String/integer/bool/duration/secret configuration, Env and Map providers, prefix nesting | Defaults handle Missing only; invalid data and fallible-source failures propagate; default logs and inspection redact secrets | `src/Config.ts`, `src/ConfigProvider.ts`, `src/Redacted.ts`, corresponding tests |
| Queue and publication | Required capacity choice (finite with a strategy, or `unbounded`), offer/take/end/fail/shutdown, scoped PubSub subscriptions | Cancelled offers cannot publish later; cancelled takers do not lose messages; buffered items precede declared terminal outcomes; closing subscribers release backpressure | `src/Queue.ts`, `src/PubSub.ts`, corresponding tests |
| Streams and sinks | Lazy owned pull, list/queue/subscription constructors, map/filter/take, effectful map with a concurrency budget, sequential flatMap, buffers and collect with required item/byte budgets, byte sink | Consumer cancellation joins producers; inner resources release before the next inner starts; collection limits release the source | `src/Pull.ts`, `src/Stream.ts`, `src/Sink.ts`, corresponding tests |
| Retry and repetition | Schedules with a required attempt or elapsed-time budget, fixed/exponential delays, jitter, typed predicates | Each attempt's scope closes before the next delay/attempt; interruption and defects are not retried; Random remains an explicit carried capability | `src/Schedule.ts`, `test/Schedule.test.ts`, retry cases in `test/Effect.test.ts` |
| Sharing and pools | Single-flight Memo, keyed Cache with a required capacity choice, scoped shared acquisition and fixed-size Pool | Producer/lease ownership, last-waiter cancellation, expiry, invalidation and failure replay each have independent tests | `src/Cache.ts`, `src/RcRef.ts`, `src/RcMap.ts`, `src/Pool.ts`, corresponding tests |
| Platform clients | Scoped temporary directories and byte files; managed process and HTTP client | Open response bodies close; unread process output cannot deadlock shutdown; observed exit is distinct from a termination request | `src/FileSystem.ts`, `src/http/HttpClient.ts`, `src/process/ChildProcessSpawner.ts`, platform `node-shared` process tests |
| Observability | Explicit Log/Trace/Metrics services, capture/JSON logs, annotations, spans and provider-owned metric registry with a required series budget | Annotations restore; owned buffers flush at close; unused telemetry is absent from binaries; series/cardinality limits are explicit | `src/Logger.ts`, `src/Tracer.ts`, `src/Metric.ts`, corresponding tests |
| Batching and coordinated state | Owned batcher with required window and capacity budgets, per-key outcomes, context partitioning and drainable-worker admission | Cancelled entries leave the pending batch; every admitted key completes exactly once; drain cannot race accepted work | `src/RequestResolver.ts`, `src/internal/request.ts`, `test/Request.test.ts`, transaction cases in `test/Effect.test.ts` |

End-of-input is returned data in the initial queue/stream contract; failure remains the effect's failure row. The adapter translates upstream `Done` explicitly. This representation choice is not a claim that every terminal/buffering policy already matches.

Configuration distinguishes `ConfigMissing`, `ConfigInvalid`, and `ConfigSource` when sources can fail operationally. Existing primitive environment access is not a presence test. A redacted value's display policy does not silently change a transport codec into a placeholder encoder; explicit secret access and wire encoding remain separate. No memory scrubbing is promised.

Caches expose success-only retention and explicit failure-retaining construction/policies; no API silently replays interruption. Resource leases and ordinary cached data remain distinct. First keyed interfaces may support string/integer keys while diagnosing unsupported nominal/composite keys. Sharing low-level waiter machinery is an implementation choice demonstrated by real consumers, not a requirement for one universal cache/pool abstraction.

Go process/client support can land before JS support only with explicit target diagnostics and matrix entries. Native process work must research group signalling, descendant ownership and reaping before promising more than confirmed owned-child exit. Host capabilities are not an OS sandbox. General SQL/storage transactions, authorization and durability remain application/service contracts.

## Adoption fixtures

These are original generic fixtures informed by source comparisons; they do not copy application code or imply those applications were executed.

1. Shared expiring credentials: concurrent misses use one producer, success expiry comes from the credential, and a failed resolution does not persist accidentally. A separate retained-failure policy fixture demonstrates intentional replay.
2. Scoped keyed connections: two borrowers share one acquired connection; one borrower leaving cannot close the other; last release and idle expiry are causal and observable.
3. Bounded grouped reads: batch only entries with the same host and credential/context identity, cap entries and window, preserve per-key failure, and resolve uncovered results explicitly. Keys alone cannot erase capability identity.
4. Drainable worker: enqueue and outstanding-work accounting form one atomic admission decision; cancelled/failed processing still retires accepted work; drain never reports idle while accepted work remains.
5. Managed subprocess and streamed client: unread output, cancellation, body limits and shutdown exercise actual resource owners and terminal receipts.

The first three sharing policies all have real usage: success-only caching, zero-TTL single-flight, and intentional failure replay. The library makes the choice visible rather than prescribing the same policy everywhere.

## Five implementation batches

Each batch is delivered in compiling, gated commits with independent review. Its first slice must be usable by two unrelated source callers through shared interfaces. Do not add speculative overloads or a giant implicit prelude.

| Batch | Scope and first reviewable slice | Dependencies and closure gate |
| --- | --- | --- |
| Owned core | Acquire/finalization, Deferred and Ref; then fixed semaphore, budgeted concurrency, Duration/clock | Canonical interfaces, generic data and recovery. Go/JS ownership, cancellation, race and mutable-payload negative tests; per-family reachability receipts |
| Services | First-class layers and fallible provider construction; then configuration/redaction and structured logs | Owned core and reviewed layer construction. Real construction-path diagnostics, shared-value lifetime, Missing-only defaults, secret-safe inspection and log flush |
| Flow | Finite-capacity queue/subscriptions and owned pull; then streams/sinks, retry and drainable workers; then the `unbounded` capacity and retry values with their justifier rules | Owned core. Item/byte/in-flight capacity, cancellation/no-loss, full terminal causes and deterministic virtual schedules |
| Shared/platform | Memo/Cache/shared lease/Pool; then files, process and HTTP client adapters | Flow and services. Generic sharing fixtures plus actual host resource cleanup; target exclusions remain visible |
| Telemetry/batching | Provider-owned metrics/spans with required series budgets and context-partitioned batching | Services, flow and shared/platform. Per-key completion, missing-output defects, no cross-credential batching, scope shutdown and unused-module receipts |

Ordinary [actors](actors.md) use service/impl protocols, typed handler or restricted receive behavior, explicit sequential admission and owning scopes. Actor execution is a runtime/library capability; no actor declaration is required. Message/reply/error data and host aliases follow checked portable ownership admission, and admission, completed reply and durable commit are separate contracts. Supervision, durable addressing and persistence compose with either ordinary or machine-backed behavior independently.

Actor mailboxes and flow queues should reuse sound runtime mechanisms and causal tests where appropriate. Reserved machine-completion capacity and state-entry epochs are machine-specific additional contracts; ordinary actors need no fictitious transition state. They are not erased to force every queue into one API. Full CLI/MCP types include protocol, method rows, construction/capture, terminal policy, admission and ownership facts through the shared semantic model.

## Transactions and remaining exclusions

Atomic coordinated-state consumers exist. The drainable-worker fixture is required in Flow and can use a coherent library-owned state transition. General user-programmable optimistic transactions need a separate sound admission design before becoming supported syntax/API.

A callback restricted to `uses {Txn}` is insufficient: an ordinary Ref write or queue operation may have an empty service row yet mutate state outside the transaction journal. Retrying such a callback can repeat those mutations. Do not claim transaction purity from requirement-row checking alone. A constrained transactional program or equivalent proven discipline must exclude those operations, preserve cancellation/rollback, and supply multi-reference race tests before general transactions can be marked supported. No database transaction guarantee follows from an in-memory API.

The first Flow slice ships finite queue and buffer capacities first. The `unbounded` value arrives together with its justifier rule; it is not an exclusion.

Other visible exclusions for these first batches: detached fibers, implicit global batching/metric registries, arbitrary mutable resource-containing payloads, unrestricted closures, nominal-key hashing, advanced stream/channel algebra, cron/time-zone scheduling, implicit provider memoization, and unproved host-target adapters. Exclusions remain in the parity matrix; completing these batches is not every Effect export.

## Required evidence

For every supported row retain pinned upstream test references and actual Effra source tests. Cover success, typed failure, defect, interruption and closing-owner behavior where meaningful. Use causal latches and virtual scheduling; sleeps and marker sorting cannot prove ordering. Port expected outcomes deliberately instead of treating TypeScript tests as native passes.

CLI/MCP inspect the same full type, directional rows, ownership and selected runtime dependencies. Missing/unsupported contracts fail clearly. Preserve compiler, interface and runtime versions, exact source/artifact hashes, full gates and relevant race checks. Record binary/import/build costs for minimal and used-family programs with matched flags and toolchains; performance claims require admitted comparative measurements.

## Prior art: Elixir, Erlang/OTP and MoonBit

Elixir and OTP choose budgets silently. Supervisor restart intensity defaults to 3 restarts in 5 seconds in Elixir but 1 in 5 in OTP (`lib/elixir/lib/supervisor.ex:L332-L336` at `91ee75bb`; `lib/stdlib/src/supervisor.erl:L104-L113` at OTP `516126e9`). `DynamicSupervisor` allows `:infinity` children by default, `Task.async_stream` runs `System.schedulers_online/0` tasks at once with a 5000 ms per-task timeout, and `GenServer.call` waits 5000 ms (`lib/elixir/lib/dynamic_supervisor.ex:L288-L291`, `lib/elixir/lib/task.ex:L570-L620`, `lib/elixir/lib/gen_server.ex:L1148-L1172`). MoonBit's `@json.parse` defaults `max_nesting_depth` to 1024 (core `json/parse.mbt:L62-L66` at `e96ede8b`). Two runtimes disagreeing on the same restart default is direct evidence for Effra's [budget choices](#budget-choices): every budget is a required argument, and `unbounded` is legal but justified. Pins are in [PRIOR_ART](../../PRIOR_ART.md#elixir-with-erlangotp).
