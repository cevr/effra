# Effra: a language design sketch

Research and proposal prepared 2026-10-05. This document describes the wider proposed language. The small runnable subset and its measurements are recorded in [prototype.md](prototype.md); syntax outside that subset remains illustrative.

The [application showcases](showcases.md) apply this direction to ADTs, state transitions, decoding, task replacement, streams, durable commands and infrastructure outputs. Their [source review](research/effect-native-showcases.md) separates proposed simplifications from application policies that must remain explicit.

## Recommendation

Build a language for servers and applications with Borgo-style algebraic data types and Go interoperability, native lazy Effects, compiler-checked error and service rows, and explicit execution profiles. The JavaScript target lowers to the existing Effect library; the Go target uses managed goroutines and Go GC. Keep the language's semantic and introspection model independent of either backend's runtime.

The proposition is: **an explicit, inspectable language for servers and applications where the compiler knows what a program returns, how it can fail, what services it needs, and which targets can execute it.** Operability, agent introspection, clear guardrails, server/OS access, and compile speed are foundational requirements.

Silk already explores Effect as a language. Effra's distinguishing choice would be an inspectable server/application model with JavaScript/Effect and Go backends. Servers are the intended systems scope; kernels, bare-metal execution, and hard real-time guarantees are outside the requirements. Managed memory is appropriate for both targets.

## What the sources establish

| Source | Verified contribution | Boundary |
| --- | --- | --- |
| Borgo | Rust-like syntax, ADTs, matching, inference, Option/Result, Go code generation and imported package declarations | Existing function types have arguments, generic bounds, and a return type; no dedicated effect row. Its concurrency snapshot emits ordinary Go goroutines. |
| Effect | Lazy `Effect<A,E,R>`, typed failures, service provision, Layers, structured child fibers, scope finalization | These semantics are delivered by a TypeScript library and runtime. They do not automatically arise from syntax or from Go's scheduler. |
| Silk | Native lazy effects, explicit execution, typed failures, lexical service provision, ownership-aware requirements | Current alpha emits through LLVM; fibers are cooperative and single-threaded. It is not a Go-targeting language. |

Evidence: [Borgo overview](https://github.com/borgo-lang/borgo), [Borgo function types](https://github.com/borgo-lang/borgo/blob/3b9f01578941fb00ed93756e2fadc009feb50128/compiler/src/type_.rs), [Borgo concurrency lowering](https://github.com/borgo-lang/borgo/blob/3b9f01578941fb00ed93756e2fadc009feb50128/compiler/test/snapshot/codegen-emit/concurrency.exp), [Effect core contract](https://github.com/Effect-TS/effect/blob/bd00773b252970e576ffe0cce17b84c10ce3c81f/packages/effect/src/Effect.ts), [Effect Layers](https://github.com/Effect-TS/effect/blob/bd00773b252970e576ffe0cce17b84c10ce3c81f/packages/effect/src/Layer.ts), [Silk effects](https://silklang.org/docs/language/effects), [Silk alpha boundaries](https://silklang.org/docs/language/alpha-status).

Local source snapshots inspected: Borgo `3b9f01578941fb00ed93756e2fadc009feb50128`; Effect `bd00773b252970e576ffe0cce17b84c10ce3c81f` (package manifest 4.0.1). The research agent also inspected Silk `4986f56e7c7f14d87b07473ab7ecdc00b038eb98`, including its target definitions and standard-library Effects. None of these projects' examples were executed for this research.

## Surface language

```rust
error NotFound { id: UserId }
error DbError { message: string }

service Users {
    effect fn find(id: UserId) -> User
        throws {NotFound, DbError}
}

service Logger {
    effect fn info(message: string) -> ()
}

effect fn greeting(id: UserId) -> string
    throws {NotFound, DbError}
    uses {Users, Logger}
{
    let user = run Users.find(id)
    run Logger.info("loaded user")
    "Hello, " + user.name
}

let pending = greeting(UserId(42))
// Effect<string, {NotFound, DbError}, {Users, Logger}>
```

Calling an `effect fn` constructs a deferred program. Its body runs only when the program is executed. Within an effect body, `run` sequences another Effect, produces its success value, and propagates its typed failures and requirements. A nested Effect returned as data stays nested. Argument evaluation follows ordinary evaluation rules; pure arguments may be computed when the recipe is constructed.

Ordinary `fn` functions cannot secretly execute I/O. They may construct Effects as data. Host entry points are the execution boundary and require all service requirements to be supplied; unhandled typed errors become reported process failures. Unsafe foreign code is an explicit escape hatch.

Use familiar syntax for data: structs, enums, exhaustive `match`, generics, Option, Result, and local mutation. Keep `?` for ordinary Result propagation; `run` handles Effect sequencing. Retry, timeout, tracing, provision, and parallel combinators transform Effect values rather than introducing special control-flow syntax for every feature.

## The type system is the main feature

Track two normalized rows alongside the success type: failures and required service keys. A row is an unordered, duplicate-free set that may have an open tail parameter.

This is a proposed Effra design, not a claim that the inputs share this representation. Silk has a requirement-row kind; its failure parameter is an ordinary type that can be a union. See [Silk Effect contracts](https://silklang.org/docs/reference/effect-contracts).

| Composition | Success | Failure row | Requirement row |
| --- | --- | --- | --- |
| Sequence P then Q | Q's result | `Ep union Eq` | `Rp union Rq` |
| Recover one error type | Original/recovery result | Remove handled type; add handler failures | Original requirements plus handler requirements |
| Provide a service value | Unchanged | Unchanged | Remove supplied key |
| Provide a Layer | Unchanged | Program failures plus construction failures | Remove supplied keys; add the Layer's inputs |
| Parallel tuple | Tuple of results | Union of child failures | Union of child requirements |
| Timeout | Original result | Add Timeout | Add Clock if not already required |

Higher-order functions must preserve open rows. In mathematical notation, `retry: Effect<A,E,R> -> Policy -> Effect<A,E,R union {Clock}>` for a pure policy, and `flatMap` unions both programs' rows. A compiler that only tracks a fixed list of services in straight-line functions cannot express the intended library.

Infer private function contracts. Require exported contracts as upper bounds, so changing an implementation does not silently enlarge every downstream API. Errors have nominal identities and ordinary payloads. Internally represent an error union as a compiler-generated tagged sum, rather than collapsing it to Go's `error` interface.

Service identity is nominal and may include a role: `Database at Primary` differs from `Database at Analytics`. A service requirement identifies replaceable behavior; it is not a global variable or merely a structural interface shape.

The `R` row tracks capabilities, not every side effect or all mutation. A stronger purity claim needs additional rules for mutation, captured state, trusted intrinsics, and foreign code. Define that boundary explicitly.

## Operability and agent introspection

Treat the compiler's semantic model as a supported inspection API, shared by the CLI, language server, and agent tools. Exported contracts remain explicit in source. Local inference must still produce a concise canonical type with stable nominal identities and source locations; no tool should require reverse-engineering generated Go, generated JS, or a large expanded generic expression.

Illustrative CLI operations, not implemented commands:

```text
ef inspect type --at src/users.ef:42 --json
ef inspect symbol app.greeting --json
ef explain requirement app.greeting Users --json
ef check --target go --json
ef inspect build --json
ef inspect runtime --pid 1234 --json
```

Type/symbol inspection returns success type, named failure alternatives, service/role requirements, generic/row constraints, supported target constraints, referenced declarations, and safety/trust boundaries. An explanation identifies the relevant call or binding that introduced an error or requirement. Providing a service records what was discharged and what remains. Diagnostics carry stable codes, expected/actual contracts, source spans, target, and a short causal path rather than only prose or a generic expansion.

Inspection output is schema-versioned and identifies the source/build revision. Package summaries provide cached public-contract inspection; inspecting a function body may lazily load or recheck its package. Detailed explanations can be computed on demand. Inspection should not force a whole-project check, default full tracing, or a giant metadata dump during every build.

Runtime inspection exposes the task/scope tree, ownership of managed resources, task states, instrumented wait reasons, cancellation requests and acknowledgements, active finalizers, queue/backpressure statistics, and Layer initialization failures. Correlate events with source symbols and build identity. Distinguish observed state from the last recorded safe point: arbitrary CPU-bound or foreign work cannot always provide a current high-level program location.

Use bounded event buffers and targeted/sampled tracing. Inspection describes a point-in-time snapshot with its consistency limits; it does not promise that all concurrent tasks freeze atomically. Default introspection returns metadata, not arbitrary service state or payloads. Managed-resource labels and counters can be explicit opt-ins.

Make each guardrail's enforcement level inspectable:

| Guardrail | Enforcement |
| --- | --- |
| Unhandled failure or missing service | Compile-time contract checking |
| Unsupported target capability | Compile-time target checking |
| Parent completion before managed child shutdown | Runtime scope/fiber protocol |
| Resource use after scope closure | Initially runtime handle checks; stronger region typing is separate work |
| Cross-task mutable aliasing | Transferability/alias rules and synchronized APIs; foreign declarations remain trusted boundaries |
| Raw pointers, ABI assumptions, unchecked foreign calls | Explicit unsafe/trusted declarations with visible scope and target |

The aim is a tool-answerable question for each failure: what is happening, who owns it, what contract applies, what boundary is trusted, and where the relevant source is.

## Compiler-backed MCP and semantic editing

Ship an optional `ef mcp` entry point alongside the CLI and language server. MCP is a protocol adapter over the same semantic workspace service, not a second analyzer or a collection of shell commands that scrape compiler output. That workspace service owns incremental parsing/checking, package summaries, stable symbol handles, revisions, and explanations. It serves CLI, LSP, MCP, and build clients without changing the standalone compiler's availability.

The useful Zerolang precedent is its compiler-mediated graph query/edit loop: semantic node handles, revision/hash expectations, checked patches, focused inspection, and version-matched agent guidance. Its architecture makes the graph authoritative and source text a projection. Sources: [Zerolang graph architecture](https://zerolang.ai/concepts/graph-architecture), [semantic edits](https://github.com/vercel-labs/zerolang/blob/7e1a64d27cc37671df31c6370890bce86f5135e1/docs/articles/concepts/semantic-vs-text.md), [bundled guidance](https://zerolang.ai/install).

For Effra, retain `.ef` text as the authoritative, editor/Git-friendly representation. Build a rebuildable semantic index and give agents compiler-checked semantic operations over that index. Human edits invalidate and rebuild the affected index entries; there is no separately authoritative graph file to import/export. This is a design choice, not a claim that Zerolang's graph-first approach is equivalent.

Proposed initial MCP tools (not implemented):

| Tool | Contract |
| --- | --- |
| `project.describe` | Compiler/protocol version, workspace revision, targets, available operations and binding/provider facts |
| `code.inspect` | Canonical symbol/type/contract and focused source at a symbol handle or location |
| `code.references` | Bounded reference/call/dependency results with source locations |
| `code.explain` | Why a diagnostic, requirement, failure alternative, target restriction, or safety boundary exists |
| `code.plan_edit` | Validate a structured semantic operation or source-body replacement in an overlay; return patch/diff and contract impact |
| `code.apply_edit` | Apply a validated plan against its expected source revisions and return a new revision plus verification status |
| `project.check` | Incremental checking for a selected package/target/revision, with structured diagnostics and timings |
| `project.test` | Run a selected test scope and return bounded results tied to a source/build revision |
| `runtime.inspect` | Query the managed runtime endpoint for task/scope/resource/wait snapshots tied to a running build |

Tool arguments and outputs have versioned schemas; MCP supports structured tool results and output schemas. Use read-only annotations for inspection tools, but enforce behavior in the server rather than relying on advisory metadata. Source: [MCP tools contract](https://modelcontextprotocol.io/specification/2025-11-25/server/tools).

Return handles scoped to a workspace revision, source locations, declaration identities, checked target, and the freshness/completeness of results. Preserve symbol identity across unambiguous edits where possible; never claim every expression ID survives an arbitrary text rewrite. Unresolved foreign calls and unavailable facts remain explicit. Paginate references and graph traversal, bound depth, and return named contracts by default with expansion on request.

Semantic edits support deliberate operations such as rename, add a declared provider, replace a body, handle a selected error, or wrap a particular Effect expression in timeout/retry. The compiler computes a minimal text patch preserving unrelated code/comments, checks the candidate overlay, and reports changed public contracts and affected dependents. Typed acceptance establishes contract validity, not application correctness; separate test results establish the behaviors exercised.

`code.plan_edit` records input file hashes, imported interface hashes, compiler/binding versions, target, and the generated patch. `code.apply_edit` rechecks all relevant preconditions at commit; stale or ambiguous plans fail without silently selecting a different node. Compiler-mediated edits are serialized, and multi-file changes use a recoverable write journal. Define crash recovery and file-watcher/editor coordination explicitly; unrelated filesystem readers cannot be promised an atomic multi-file view. Publish a committed workspace revision after the writes and index update succeed.

Do not store the whole program graph in the agent context. A typical loop is inspect the relevant symbol, explain the missing contract, plan a checked edit, apply it against the expected revision, then run focused checks/tests. Every step returns a compact receipt. MCP read queries reuse the existing incremental index and do not implicitly invoke the backend or rebuild the project. Heavy checks/builds are explicit operations with separate timings and result handles.

Expose compiler-version-matched language rules, diagnostics, examples, supported semantic edits, and target/foreign-binding contracts as searchable MCP resources and CLI documentation. Derive these from compiler-owned metadata where possible. This borrows Zerolang's bundled guidance idea and prevents an agent from inventing syntax accepted by a different compiler version.

Runtime inspection initially stays observational. It connects to explicit managed runtime endpoints; it cannot infer a complete task graph from arbitrary Go goroutines or unmanaged JS promises. The JS backend instruments Effect execution/supervision; the Go backend instruments its own managed tasks. Both project their observable facts into a common inspection schema with backend-specific fields where necessary. Mutating runtime controls, if added, are distinct operations with explicit scope and policy.

## Dependency construction and testing

Keep the distinction between a service contract, its implementation, and a Layer that constructs an implementation. Conceptually, `Layer<Provided, BuildErrors, Required>` is a recipe for acquiring services, possibly failing, with a scope that owns cleanup.

An application graph might be:

```text
Config -> DatabaseLive -> UsersLive -> request program
LoggerLive -------------------------> request program
```

The compiler checks missing services, ambiguous providers, roles, and statically visible construction cycles. The runtime memoizes shared Layer nodes within a graph build, rolls back partial construction, and releases services in dependency order. Memoization uses Layer node identity and provision context, not just a service name; explicitly fresh nodes produce independent instances.

Implementations capture their acquired dependencies. A caller requiring Users should not also have to name Database solely because UsersLive uses one internally. Caller-visible failures remain those declared by the Users operations; Layer construction failures remain in the startup path.

In tests, supply UsersMemory, a recording Logger, and a controllable Clock through the same provision mechanism. The compiler checks the resulting requirements. Fake time controls Effra timers; it does not make Go goroutine scheduling or arbitrary foreign I/O deterministic.

## Concurrency and resources

Use **scopes as the lifetime boundary** for child tasks and acquired resources. Keep three concepts distinct: Scope owns lifetimes, cancellation requests that execution stop, and Fiber represents execution plus its completion result. Cancelling one child must not close its parent's scope or release resources its siblings still use.

Scopes form a tree. Application scope owns long-lived services; request scope owns request work; operation scopes own narrower parallel groups and resources. Context may carry the cancellation signal into Go calls, but Scope closure is the operation that establishes completed shutdown.

An Effra Fiber is a managed task whose execution uses a Go goroutine. It carries parent ownership, cancellation, completion, service context, and tracing state. Ordinary sequencing stays in the current goroutine; constructing an Effect does not spawn anything.

Define different contracts for different operations:

- `all` owns its children, collects successful results, and on failure requests sibling cancellation and waits for cleanup before returning.
- `race` selects a winner, cancels the loser, and waits for loser cleanup before publishing completion.
- Low-level `forkChild` attaches lifetime to the parent. Joining propagates the child's failure; a supervisor policy governs failures that are never joined.
- Detached work is deferred from the initial language; later it would require an explicit longer-lived owner.

Cancellation is cooperative. Managed waits observe cancellation; compiler-generated effect boundaries and effectful loop backedges are safe points. Pure infinite loops and uncooperative foreign calls can delay termination. A timeout requests cancellation; returning only after cleanup can exceed the nominal deadline. An API that returns earlier must explicitly transfer ownership of still-running work.

Go context carries cancellation across compatible library boundaries, but notification alone neither kills a goroutine nor proves that it finished. See [Go context](https://go.dev/blog/context) and [errgroup](https://pkg.go.dev/golang.org/x/sync/errgroup).

A scope owns both child work and resource finalizers. Closing it stops new work/acquisitions, requests child cancellation, invokes registered cancellation hooks needed to unblock pending operations, waits for children to release resources, then runs remaining finalizers in reverse acquisition order. An adapter may need to abort/close an I/O operation during cancellation rather than wait for ordinary release; such hooks need documented safety and idempotence. Protect the acquire/register-finalizer transition from interruption. Run cleanup in a protected cancellation region and preserve cleanup defects alongside the original failure.

Make scope lifecycle explicit: Open -> Closing -> Closed. Registration and the transition to Closing must be synchronized so shutdown accounts for every task and resource. Track in-flight acquisitions before opening resources; late successful acquisition must be released before Closed is published. Close is idempotent; concurrent close callers await the same terminal outcome. Lexical resource cleanup can lower to direct generated cleanup where ownership is statically evident; a shared runtime registry is needed for dynamic/concurrent registration, not for every ordinary variable.

Start with runtime-guarded resource handles. Scopes alone do not statically prevent a resource reference escaping and being used after closure. Later, generative region identities or affine handles could enforce stronger rules. GC manages memory reclamation; sockets, files, transactions, and subscriptions still require explicit lifetime management.

Goroutines introduce parallel access to captured values. Adopt immutable shared data and synchronized Ref/Queue/Semaphore abstractions; reject direct capture of mutable local places across forks. That rule needs alias analysis or a transferability rule to be sound for reference-containing values. Foreign pointers and mutable Go objects need trusted thread-safety contracts or an unsafe boundary. Do not advertise race freedom before these rules are implemented.

## Failure is more than a Result

The runtime result should distinguish `Success(A)` from failure causes containing typed failures E, defects, or interruption. Ordinary recovery handles E. An explicit supervision/sandbox operation may inspect the full cause. Preserve multiple concurrent failures and cleanup failures instead of silently keeping only the first.

Recoverable Go panics can be captured at managed task boundaries as defects and trigger cleanup. Fatal runtime failures, process termination, and arbitrary unmanaged goroutine panics cannot receive the same guarantee. This deliberately differs from Silk's fatal-trap model, where traps may bypass finalization. See [Effect Cause](https://github.com/Effect-TS/effect/blob/bd00773b252970e576ffe0cce17b84c10ce3c81f/packages/effect/src/Cause.ts) and [Silk failure boundaries](https://silklang.org/docs/language/effects).

## Compiler and Go runtime

Effect typing and an Effect runtime are different design layers. Error/service rows and nominal capability identities can be checked and erased; they do not intrinsically require GC, fibers, boxed instructions, or a scheduler. First-class lazy computations do require a concrete representation for captures and execution, whose allocation and dispatch costs depend on lowering. For server workloads, managed memory and the existing JS/Go runtimes are appropriate, but they remain visible operational constraints rather than hidden implications of a type signature.

The JS/Effect backend deliberately adopts Effect's execution model. The Go backend may lower straight-line effect bodies to ordinary Go control flow with explicit context, result propagation, cancellation safe points, and cleanup. Optimizations may fuse away intermediate descriptions only when laziness, error/cancellation ordering, scope lifetime, and observability contracts remain equivalent. Do not promise zero allocation for arbitrary higher-order or escaping Effects.

```text
.ef source
  -> parse and resolve
  -> infer values + failure rows + capability rows
  -> typed effect IR
  -> Go code + Go package wrappers + source locations
  -> go build
  -> application binary
```

The compiler owns sum types, row inference, service identities, native Effect sequencing, target/profile constraints, and diagnostic/source-location metadata. Backend runtimes implement the contracts admitted by their profile. A Go runtime package owns managed Fibers, scope closure, Cause/Exit, Layer memoization, cancellation, timers, and tracing; the JS backend reuses Effect.

A first representation could be a lazy closure: `Effect[A,E]` containing a function from FiberContext to Exit[A,E]. Requirements can be checked by the source compiler and erased in generated Go, where service access uses nominal keys. Keep generated Go internal to this contract; external callers enter through wrappers that supply providers and translate the result.

This representation preserves laziness without requiring a port of Effect's JavaScript interpreter or another scheduler above Go. It still needs measured work on closure allocation and stack-safe deeply composed programs. Growing goroutine stacks are not an unlimited-stack guarantee. Use iterative/trampolined combinator execution or explicit suspension where necessary; optimize only after conformance tests exist.

Generated code should be readable and carry source mappings, so Go tooling remains useful. Go libraries remain Go libraries. Export wrappers translate an Effra operation to explicit Go provider/context arguments and `(T,error)` or a richer Exit API when callers need cancellation and defects separately.

## Compile speed is a language constraint

User requirement: compilation must feel exceptionally fast, comparable to Go. Treat this as a constraint on the language and build architecture from the first prototype, rather than a later optimization. Emitting Go alone does not meet it: the pipeline adds parsing, semantic analysis, and generation before Go compilation and linking.

Go's compiler operates per package and imports compiled export data, including type information and selected generic/inlining bodies, rather than reparsing imported source at every compilation. Its build command caches compilation results. These are relevant foundations, not measured proof about Effra. Sources: [Go package compilation](https://go.dev/cmd/compile/), [Go compiler export data](https://go.dev/src/cmd/compile/README), [Go build caching](https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching).

Recommended constraints:

- Compile and check packages independently. Require explicit exported type/failure/service contracts, including generic row constraints, so clients need interface summaries rather than dependency bodies.
- Store versioned interface summaries and content-addressed artifacts. Cache keys include source, imported interface hashes, compiler/runtime versions, target, options, and Go binding metadata. An implementation-only edit can preserve the Effra interface hash and avoid rechecking clients; Go independently decides which backend compilation/link artifacts remain valid.
- Cache imported Go type information and binding contracts against package/toolchain/build inputs. Do not walk and reparse the entire Go dependency graph for each edit.
- Give failure/service rows a dedicated compiler representation with interned nominal IDs, normalized sets, and explicit open-row constraints. Avoid implementing the model through recursive conditional types, implicit instance search, or unrestricted type-level computation. Benchmark higher-order row union/subtraction and pathological cases; ordinary set operations alone do not prove cheap inference.
- Preserve Go generics where feasible. Avoid a second exhaustive monomorphization pass, whole-program specialization, or generating a copy of the runtime for every program. Put runtime code in reusable Go packages.
- Emit deterministic, compact Go with stable names and file layout; write only changed bytes. Avoid timestamps and traversal-order-dependent names that invalidate otherwise reusable build artifacts.
- Parallelize independent package work. Support an optional persistent compiler for editor/watch workloads, while retaining a fast standalone compiler and reusable disk caches.

I would initially implement the compiler in Go for a native executable and close integration with the Go toolchain. That is an engineering choice, not a performance guarantee. Prototype counters and timings should separate import loading, parsing, value checking, row solving, lowering, emission, Go compilation, and linking.

Provisional performance budgets, to be validated on a fixed reference machine and matched application fixtures:

| Scenario | Proposed acceptance target |
| --- | --- |
| Warm no-op build | Effra frontend/cache overhead below 20 ms |
| Implementation edit in a roughly 10k-line package | Effra recheck/emission below 100 ms; unrelated packages not rechecked |
| Complete build or edit-to-binary | p95 wall time within 1.25x the equivalent Go application in the same cache regime |

These are desired budgets, not existing results. Compare cold builds, warm builds, private implementation edits, public contract edits, and dependency updates separately; include generation, compilation, and linking, and report memory use. Benchmark compiler scaling on packages and dependency graphs, plus adverse row-polymorphism fixtures. No-op cache speed cannot substitute for cold compilation performance. Keep a compiler performance regression gate alongside runtime conformance tests.

## Go interoperability is a semantic boundary

Borgo's importer recognizes `(T,error)` and `(T,bool)` return patterns, but those signatures alone do not tell you whether a call performs I/O, returns nullable pointers, retains callbacks, mutates shared state, or honors cancellation. Its importer also has unsupported cases. See [Borgo importer](https://github.com/borgo-lang/borgo/blob/3b9f01578941fb00ed93756e2fadc009feb50128/importer/importer.go).

Generate package declarations and direct Go wrappers from Go's type information, then attach explicit binding metadata for:

- effect classification and service requirements;
- nil/absence and partial-success behavior;
- error conversion into domain errors or an honest GoError fallback;
- context support, cancellation behavior, and blocking behavior;
- ownership, mutability, thread safety, and callback lifetime.

Do not automatically replace `(T,bool)` with Option: false can accompany a meaningful T. Do not discard partial results from `(T,error)`. Use per-operation contracts. Safe wrappers execute foreign work only inside the deferred Effect body. Unknown behavior gets a conservative foreign capability and runtime limitations, with explicit unsafe access available.

Start with curated fmt/logging, os/filesystem, net/http, context, and database/sql bindings. Complete Go interoperability is a major project, not an automatic consequence of emitting Go.

## JavaScript/Effect and Go targets

Recommendation: design one typed semantic core and target-neutral IR, with separate JavaScript/Effect and Go lowering paths. The JS backend emits ordinary Effect library code, with TypeScript declarations and proposed source maps for consumers. Native Go executables are the default deliverable. The first slice used Effect's existing runtime; the prototype now also builds native Go executables. Continue the Go backend for ecosystem interoperability and parallel managed services. Keep scheduling and concrete service implementations backend-specific; both may use managed memory.

Gleam demonstrates separate external implementations for different targets and compilation errors when an operation lacks a selected-target implementation. That is a useful precedent for explicit backend boundaries; Gleam targets Erlang/JavaScript, not Go. Source: [Gleam multi-target externals](https://gleam.run/documentation/externals/#multi-target-externals).

Keep three layers explicit:

1. Portable values and Effects: structs, ADTs, fixed-width numerical contracts, typed failures, services, and ownership/lifetime rules for managed work.
2. Target providers: HTTP, clocks, filesystem, processes, sockets, tracing, and other operations where the selected host has an implementation.
3. Target-specific systems modules: Go package types, unsafe pointers, OS-specific syscalls, browser DOM, and platform FFI.

Service requirements are runtime dependencies. Target requirements are compile-time availability constraints; satisfying one does not automatically satisfy the other. Library package summaries include target constraints, so unavailable code is diagnosed without reparsing dependency implementations. Browser JS, server JS, and Go OS/architecture combinations are different target profiles.

Portable concurrency promises structured child ownership and cancellation, not a fixed schedule or parallel execution. Go runs managed goroutines; the JS backend uses Effect's runtime/event loop. Explicit parallel/thread/atomic facilities require an available target implementation, with data-transfer contracts at worker boundaries. The JS lowering must pass the common semantic conformance corpus against the selected Effect version; it should not create a second effect runtime.

Define portable numeric and data semantics rather than silently inheriting each host's defaults. For example, signed/unsigned 64-bit arithmetic needs BigInt or another exact JS representation, and generated TypeScript declarations must expose that choice. Specify overflow, float widths, UTF-8 text/byte conversion, equality, mutation, and codec behavior. A shared source type does not imply a shared in-memory layout; define a serialization contract for cross-target exchange.

The Go target should expose efficient server facilities: byte buffers, fixed-width integers, networking, processes, synchronization, profiling, and curated OS bindings. Go-specific unsafe or ABI-dependent operations remain explicit and visible to inspection. Target constraints distinguish server JS, browser JS, and Go OS/architecture combinations. Do not label an operation portable when one backend cannot meet its contract. A freestanding backend and no-GC execution are outside the current design scope.

## TypeScript content-mapper integration

The user identified [typescript-go PR #4712, Content mappers](https://github.com/microsoft/typescript-go/pull/4712). It merged August 19, 2026. The protocol transforms foreign source into virtual JS/TS with source-span mappings and diagnostics, allowing TypeScript tooling to work across the boundary. Content-mapped inputs do not produce JavaScript through TypeScript emit; Effra must emit runnable code itself. Complex transformed syntax still needs language-specific tooling.

The merged [TypeScript follow-up #63936](https://github.com/microsoft/TypeScript/pull/63936) adds declaration maps and multi-projection hover support. [LSP middleware #64583](https://github.com/microsoft/TypeScript/pull/64583), merged October 2, permits VS Code extensions to customize language-service responses. Merge status was verified through GitHub; this research did not validate a particular installed compiler/editor version.

Proposed architecture: the compiler owns Effra parsing, checking, canonical types, and diagnostics; a content-mapper adapter exposes a virtual TypeScript/Effect projection for TS ecosystem interop and mapped editor features. Emit runnable JS/Effect and consumer declarations separately. Use the same semantic data for Effra's own LSP/agent inspection API, so Go projects do not depend on TypeScript to explain their contracts. Keep TS projection checking optional for the fast standalone build path; validate it in interop/conformance tests and editor workloads.

Mapped edits need particular care: #4712 permits write-back only through exact verbatim spans. Generated Effect sequencing is transformed syntax, so automatic refactors cannot safely be translated by ordinary position mapping alone. Effra owns language-aware edits and formatting; TypeScript supplies the features that the selected projections can map faithfully. The integration makes JS tooling more feasible; it does not supply Effra's semantic rules or Go backend.

## Implementation path and decision gates

1. **Compiler and JS/Effect slice.** Compile a tiny program with ADTs, `effect fn`, `run`, failure/service rows, provision, and canonical JSON inspection into runnable Effect code. Expose `project.describe`, `code.inspect`, `code.explain`, and `project.check` through the same CLI/MCP semantic service. Verify higher-order row inference and selective error subtraction. Establish cold/warm/private-edit/public-edit benchmarks and interface-summary caching before expanding language features.
2. **Go runtime semantics slice.** Implement managed Fibers, cancellation, scopes, and provider construction; test acquisition races, concurrent failures, timeout cleanup, and foreign blocking. Run the supported semantic corpus on both JS/Effect and Go. Benchmark frontend and backend time independently.
3. **One real server.** Run an HTTP/database application with Layer-managed startup/shutdown, concurrency, bounded queues, selective retry, and replacement services in tests. Verify JS and Go artifacts, the Go race detector, and agent-visible type/ownership/wait explanations.
4. **Interop and operability.** Add the content-mapper adapter, language-aware LSP/MCP edits, stale-plan/crash-recovery checks, runtime task/scope inspection, typed JS exports, and Go-facing exports. Grow curated foreign bindings and streams after lifecycle semantics are stable.

Use Borgo's existing front end/backend as a prototype substrate if it helps answer these questions. Its current type representation has no dedicated error/service rows, so a fork requires deep inference and IR changes. Choose long-term reuse after the vertical slice reveals whether extending that architecture remains coherent. Silk is a reference for native Effects; its LLVM and ownership design is not a ready-made Go backend.

Defer freestanding/no-GC execution, a complete Rust-like ownership checker, general resumable algebraic-effect handlers, a replacement Go scheduler, durable workflows, and the full Effect package ecosystem. Managed task/resource ownership and safe cross-task mutation boundaries remain required. Checked service/failure rows are not the same as arbitrary continuation-capturing effect handlers.

The first proof should be concrete: a compiler rejects a missing Users provider, selective recovery removes NotFound while preserving DbError, retry repeats the deferred operation, and cancelled concurrent work releases its resources before the owner reports completion. Once those hold in a real Go binary, there is a credible foundation for Effra.
