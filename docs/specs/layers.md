# First-class layers and owned acquisition

Status: authorized implementation contract, 2026-10-06. This replaces repeated provider wiring with checked declarative composition. The owner syntax below is the intended surface; it is not current compiler support. Read [language abstractions](language-abstractions.md), [owned library contracts](standard-library-capabilities.md), [composition research](../research/layer-composition.md) and [runtime contracts](../runtime.md).

## Operations, construction and assembly

`service` declares operations. `impl` supplies behavior and declares construction dependencies. `layer` selects implementations and assembles their lazy acquisition graph. Service-operation rows remain separate from provider-construction rows; a layer does not acquire every service an operation may need later. Domain operations retain explicit requirements. Provide the assembled graph once at an application, command, handler or test boundary.

```ef
layer Platform {
    Config = EnvConfig
    Logger = JsonLogger
    Clock = LiveClock
}
layer Persistence {
    Database = Postgres
}
layer Domain {
    Users = UsersLive
    Audit = AuditLive
}
layer App provides { Users, Audit, Logger, Http } {
    merge Platform, Persistence, Domain
    Http = GoHttp
}
```

`Postgres` requires `Config`; `UsersLive` and `AuditLive` require `Database`. Requirements determine construction edges; text order does not. A bare implementation selection constructs its declared zero-argument plan when acquired, including construction dependencies. Ordinary configuration arguments remain explicit, such as `Database = Postgres(settings)`. Do not evaluate a selected effect during declaration or typechecking.

Fallible resource construction must use an ordinary checked effect factory returning an initialized provider value, with its own `raises`/`uses` and capture evidence. Admit provider value types through the canonical type owner rather than an untyped factory escape hatch. Existing pure configured implementations remain supported. The factory runs in its acquisition node's scope; acquired resources captured by the provider live there. A startup-only recipe with result `()` uses `start initializeStorage()` in the plan, with explicit rows and a declaration-site identity. It exposes no service; it is still a selected acquisition node. These additions reuse ordinary effect bodies rather than inventing a separate initializer control-flow language.

## Inferred and annotated contracts

- `provides`: exposed nominal service bindings. An unannotated layer exposes its direct bindings and merged public bindings, never only graph leaves. Explicitly hidden outputs of a merged layer remain internal; merging cannot accidentally undo that layer's annotation. Hidden nodes remain available to graph resolution and deliberate replacement.
- `raises`: union of selected constructor/startup failures, separate from service-operation failures.
- `uses`: construction inputs left unsatisfied after assembling the complete graph, including hidden selected nodes.

Optional annotations are checked contracts. `provides` narrows the available public outputs; every listed service must be exposed by the composition. `raises` sets an upper bound; selected failures cannot exceed it. An explicit `uses` is likewise an upper bound on remaining nominal inputs. Unknown identities diagnose; public annotations never silently grow. Inspection exposes inferred facts alongside declared bounds.

Narrowing hides availability, not acquisition. Every explicitly selected binding/startup node still runs and contributes construction rows and cleanup, even if no public output depends on it. Importing an unused declaration performs no initialization or acquisition. Graph retention and output projection are distinct operations.

## Merge, node identity and replacement

```ef
layer SharedDatabase { Database = Postgres }
layer Accounts {
    merge SharedDatabase
    Users = UsersLive
}
layer Payments {
    merge SharedDatabase
    Billing = BillingLive
}
layer App provides { Users, Billing, Logger } {
    merge Platform, Accounts, Payments
}
layer TestApp {
    merge App
    replace Database = FixtureDatabase
    replace Logger = RecordingLogger
}
```

Both branches reference the same `SharedDatabase` binding node. Deduplicate its identity, not its service type, implementation name or configuration values. Distinct bindings for the same nominal service are errors with both source sites, even when their constructors look equal. Repeated references to one node are sharing, not a duplicate. Merge order never selects a winner.

Apply replacements to the whole graph before acquisition or edge solving. A replacement targets one existing nominal binding, including a hidden binding, and updates every alias/consumer of that node. It must implement the service-operation contract. Recompute constructor inputs/failures from the replacement and validate effective graph contracts and cycles; a pure fixture need not preserve unused production constructor failures. A replacement introducing new inputs/failures is admitted only within the resulting layer's checked contract. Unknown targets, repeated replacements and inconsistent inherited replacements diagnose; no sequential override precedence.

Original constructors are not acquired after replacement. Old dependency edges disappear, but separately selected bindings/startup effects remain selected even if they no longer have an incoming consumer. Output hiding never supplies permission to prune them. Replacement provenance and the resulting edges are inspectable; the original layer plan remains unchanged.

`merge fresh SharedDatabase` creates a fresh occurrence of that entire selected subtree. Each syntactic fresh occurrence assigns distinct internal node identities; dependency inputs outside the subtree remain borrowed. Ordinary duplicate-service rules still apply: fresh is not a way to hide two conflicting `Database` bindings in one graph. Multiple same-service instances require separate provision boundaries or explicit distinct nominal services. Separate graph builds already have independent acquisition tables.

## Managed builds and input compatibility

```ef
effect fn main() -> ()
    raises { ConfigError, DbError, ServerError }
{
    run serve().provide(App)
}
```

Constructing a plan is pure and lazy. Providing it creates a build in the execution owner's scope, checks/supplies remaining inputs, and adds construction failures to the operation's execution contract. It removes only exposed satisfied operation requirements. Internal hidden outputs do not become available to arbitrary domain operations.

One build owns its acquisition table, producer lifetimes and node scopes. The table records pending, successful or failed acquisition by node identity and compatible input identities. Compatible inputs mean identical resolved dependency instances/borrowed provider identities and the same captured plan environment. Do not compare arbitrary runtime values structurally. Static assembly resolves one input context per node; incompatible contexts diagnose or require explicit fresh identities, never first-context wins. Dynamic adapters validate this invariant at the runtime seam.

Concurrent waiters await one acquisition and observe the same provider or construction outcome. Producers belong to the build, not its first waiter. Cancelling one waiter does not cancel another's acquisition; cancelling/closing the build cancels its producers, waits for cooperative completion and releases late successful acquisition exactly once. Failed entries have a shared terminal outcome for that build; retry requires a new explicit build, not hidden memo eviction/retry.

Startup failure cancels/joins admitted sibling construction and completes rollback before publishing failure. Preserve the original typed construction failure together with cleanup defects/interruption as full Cause. On shutdown, dependents finish and release before dependencies; independent nodes may close concurrently. Attempt all finalizers even when one fails. Acquisition/register-release remains atomic under cancellation. A scope or DAG cannot force uncooperative host code to stop; forced process watchdog termination reports cleanup unconfirmed.

Requests inherit materialized application services without rebuilding the application layer. Resources selected/acquired by a request graph belong to its request scope. Borrowed application/host services are not closed by the borrower. Explicit cross-build leases and keyed eviction remain libraries with a separately owning scope, not global layer memoization.

## Parameters, dynamic plans and inspection

Following the static first increment, admit ordinary typed configuration parameters: `layer Tenant(settings: Settings) { ... }`. Applying a parameterized declaration constructs a new lazy plan instance; repeating a reference to the same plan value shares its internal identities, while two applications remain distinct even for equal arguments. Capture/escape evidence must prevent shorter-lived resources from entering longer-lived plans. No unrestricted type-level graph inference or structural configuration hashing.

Runtime plugin selection follows later through a checked layer interface with explicit allowed outputs, failures, inputs and ownership/trust. Inspection marks unresolved concrete nodes; static graph completeness cannot be inferred from an arbitrary host list. Atomic application startup is the default. Isolated plugin rollback, reload fallback, precedence and TTL policies belong to explicit libraries. Foreign runtime handles use scoped adapters and disposal; common Effra code does not require framework runtime construction.

CLI/MCP/LSP use the canonical compiler model and source tables. Expose inferred/declared contracts, public/hidden/startup nodes, stable nominal binding identities, merge/fresh/replacement provenance, shared occurrences, incoming dependents, construction paths, target restrictions, planned acquisition owner and proved versus runtime/trusted facts. Construction and operation edges are different relations. Missing provision inputs, ambiguous bindings and visible cycles retain primary source anchors and related path/site information. Inferred remaining inputs on an open layer are valid; missing inputs diagnose when a closed provision boundary cannot supply them.

Keep checking bounded and package-local where possible. Normalize finite nominal rows; use graph work queues and cycle algorithms over interned nodes, not repeated recursive subtree rebuilding. Interface summaries transport the facts consumers need without reparsing implementation bodies. Retain only reachable layer declarations/runtime facilities; fluent/direct provision has the same retention. Performance and size acceptance is measured in the final measurement phase, with no current improvement claim.

## Gated implementation sequence

1. **Static plans and construction graph:** parse/format declarations, merge/fresh/replace, inferred/checked contracts, identity deduplication, complete source-grounded canonical inspection and diagnostic matrix. Supported pure providers execute both targets; unsupported fallible/startup forms diagnose until subsequent units land.
2. **Managed acquisition:** independent runtime plan/build interface, pending-waiter sharing, build-owned producer cancellation, node ownership, completed rollback and dependent-first closure. Integrate both emitters through this interface; race/cancellation controls.
3. **Fallible construction and startup effects:** ordinary provider-returning factories, startup-only recipe selection, full construction/operation separation and hidden-effect retention. Resource escape and failure/cleanup controls; extend pinned upstream behavior mapping.
4. **Plan instances and inherited inputs:** parameterized/fresh plans, borrowed application/request services, compatible-input checks and lifetime inspection. Two unrelated server/command roots plus fixture replacement and actual request resources. Dynamic plugins remain explicit following work until a finite interface/conformance gate exists.
5. **Public tooling and adoption:** actual CLI/MCP/LSP graph parity, formatter, invalid-program service continuity, shared Go/JS executable examples, unused declaration/module controls and independent review. Preserve exact source/artifact/raw gate receipts. Final size/build/server measurements follow the owner's benchmark-last order.

Each unit compiles and passes the full gate; runtime changes add race checks. Before closure, causal tests establish a concurrent diamond, failure with concurrent waiters, one cancelled waiter, build-close during acquisition, late success release, rollback with cleanup defects, dependents-before-dependencies, all finalizers attempted, replacement changing rows/cycles, inherited application resources, fresh/separate builds, hidden startup retention and no unused initialization. Matching upstream names or inspecting unrun tests is insufficient.
