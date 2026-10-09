# First-class layers and owned acquisition

Status: authorized implementation contract, 2026-10-06. This replaces repeated provider wiring with checked declarative composition. The owner syntax below is the intended surface; it is not current compiler support. Read [language abstractions](language-abstractions.md), [owned library contracts](standard-library-capabilities.md), [composition research](../research/layer-composition.md) and [runtime contracts](../runtime.md).

## Operations, construction and assembly

`service` declares operations. `impl` supplies behavior and declares construction dependencies. `layer` selects implementations and assembles their lazy acquisition graph. Service-operation rows remain separate from provider-construction rows; a layer does not acquire every service an operation may need later. Domain operations retain explicit requirements. Provide the assembled graph once at an application, command, handler or test boundary.

Service-operation rows may be row-polymorphic: the operation declares `E: raises` and `R: uses` parameters, and its `uses` row may name only its declared `uses` parameters. Implementation methods declare matching row parameters and may also repeat concrete constructor captures in their own `uses` row. These are properties of the operation and method, not construction rows; operation rows are solved at each call from callback arguments, so a fixed `uses { Console }` on a service operation remains `EF103` and construction requirements stay on `impl ... uses`. See [language abstractions](language-abstractions.md#row-parameters-on-service-operations-and-implementation-methods).

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
    Http = LiveHttp
}
```

`Postgres` requires `Config`; `UsersLive` and `AuditLive` require `Database`. Requirements determine construction edges; text order does not. A bare implementation selection constructs its declared zero-argument plan when acquired, including construction dependencies. In the static first increment, configuration arguments are pure expressions over module constants, evaluated when constructing the lazy plan. Typed layer parameters follow in Unit4. Do not evaluate a selected effect during declaration or typechecking.

Constructing a plan happens at an explicit plan application/provision site, never through module-initialization execution of a declaration. Constant folding of admitted pure arguments is permitted; imports of unused declarations evaluate no argument code and acquire nothing.

Fallible resource construction must use an ordinary checked effect factory returning an initialized provider value, with its own `raises`/`uses` and capture evidence. Admit provider value types through the canonical type owner rather than an untyped factory escape hatch. Existing pure configured implementations remain supported. The factory runs in its acquisition node's scope; acquired resources captured by the provider live there. A startup-only recipe with result `()` uses `start initializeStorage()` in the plan, with explicit rows and a declaration-site identity. It exposes no service; it is still a selected acquisition node. These additions reuse ordinary effect bodies rather than inventing a separate initializer control-flow language.

Publish the build only after every selected constructor and startup recipe completes. Startup recipes have no implied ordering against sibling constructors. Initialization required by consumers belongs inside the factory that publishes their provider, for example `Database = openMigrated()`, rather than a sibling `start migrate()`.

Constructor evaluation completes without closing the node's resource owner. Ordinary `fork` inside a constructor/start recipe admits work to that longer-lived node scope. The first profile retains child failures for observation at owner closure, following the existing lifecycle contract; it does not automatically stop the provided program or promise a healthy background worker after startup. Inspection reports this failure policy. A recipe that never returns prevents startup publication. Supervision and fail-fast monitoring require a separate explicit capability. Startup-only nodes are not replacement/removal targets in this increment; keep optional workers in independently selected bundles.

## Inferred and annotated contracts

- `provides`: exposed nominal service bindings. An unannotated layer exposes its direct bindings and the union of merged public bindings, never only graph leaves. A node exposed by any direct binding or merged public path is public before applying this layer's annotation. A solely hidden path stays internal; an additional public path deliberately exposes the same shared node. Hidden nodes remain available to construction graph resolution and deliberate replacement, not arbitrary domain operations.
- `raises`: union of selected constructor/startup failures, separate from service-operation failures.
- `uses`: construction inputs left unsatisfied after assembling the complete graph, including hidden selected nodes.

Optional annotations are checked contracts. `provides` narrows the available public outputs; every listed service must be exposed by the composition. `raises` sets an upper bound; selected failures cannot exceed it. An explicit `uses` is likewise an upper bound on remaining nominal inputs. Unknown identities diagnose; public annotations never silently grow. Inspection exposes inferred facts alongside declared bounds.

Owner-directed layer inference is an explicit exception to the exported-function upper-bound rule: exported layers also infer contracts unless annotated. Their interface summaries carry the complete selected graph, including hidden/startup nodes, stable identities, visibility and per-node construction rows. Changing hidden bindings can change composition compatibility and invalidates downstream summaries. Hidden availability does not mean composition-private implementation data.

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

Apply replacements to the whole graph before acquisition or edge solving. A replacement targets the merged composition's effective nominal binding, including a hidden binding or an inherited replacement, and updates every alias/consumer of that node while preserving visibility. It must implement the service-operation contract. Recompute constructor inputs/failures from the replacement and validate effective graph contracts and cycles; a pure fixture need not preserve unused production constructor failures. A replacement introducing new inputs/failures is admitted only within the resulting layer's checked contract. Unknown targets and two replacements of one target in the same declaration diagnose. Sibling merges with conflicting effective replacements of the same shared node diagnose unless the enclosing declaration deliberately replaces that node itself. Outer replacement is lexical and explicit; merge order never supplies override precedence. Distinct-node duplicate service bindings remain errors.

Original constructors are not acquired after replacement. Old dependency edges disappear, but separately selected bindings/startup effects remain selected even if they no longer have an incoming consumer. Output hiding never supplies permission to prune them. Replacement provenance and the resulting edges are inspectable; the original layer plan remains unchanged.

Fresh acquisition is an explicit separate provision/build boundary. There is no `merge fresh` in this static increment: copying a subtree into a flat single-binding graph either has no additional useful effect or creates a duplicate-service error. Multiple same-service instances require separate provision boundaries or explicit distinct nominal services. Separate graph builds have independent acquisition tables; no structural configuration equality or global memoization merges them.

## Managed builds and input compatibility

```ef
effect fn main() -> void
    raises { ConfigError, DbError, ServerError }
{
    run serve().provide(App)
}
```

Constructing a plan is pure and lazy. Providing it creates an owning execution scope for the build and provided program, checks/supplies remaining inputs, and adds construction failures to the operation's execution contract. It removes only exposed satisfied operation requirements. Internal hidden outputs do not become available to arbitrary domain operations. Node resources survive constructor completion and close after the provided program's owned work, before provision returns. Values cannot escape this boundary carrying resources owned by its build or nodes; the common ownership checker enforces that constraint.

One build owns its acquisition table, producer lifetimes and node scopes. The table records pending, successful or failed acquisition by node identity. Static assembly resolves exactly one input context per node. Runtime adapters must validate compatibility: identical resolved dependency instances/borrowed provider identities and captured plan environment, never structural comparison of arbitrary values or first-context wins. Until a dynamic caller exists, incompatible inputs are a runtime-seam invariant rather than an additional statically reachable language case.

Concurrent constructor waiters await one acquisition and observe the same provider or construction outcome. Producers belong to the build, not its first waiter. Cancelling/closing the build cancels its producers, waits for cooperative completion and releases late successful acquisition exactly once. At the runtime seam, cancelling an individual waiter does not cancel another's acquisition; the static surface currently only cancels these waiters through whole-build cancellation. This deliberately differs from the pinned Effect first-requester interruption behavior and must be marked different in upstream mapping. Failed entries have a shared terminal outcome for that build; retry requires a new explicit build, not hidden memo eviction/retry.

Startup failure cancels/joins admitted sibling construction and completes rollback before publishing failure. Preserve actual typed construction failures together with cleanup defects/interruption as full Cause. If concurrent producers fail, retain each producer failure once, without duplicating shared failures for their waiters; choose the primary expected failure by canonical node order after joined rollback. Abort-induced interruption does not fabricate a domain failure. On shutdown, dependents finish and release before dependencies; independent nodes may close concurrently. Attempt all finalizers even when one fails. Acquisition/register-release remains atomic under cancellation. A scope or DAG cannot force uncooperative host code to stop; forced process watchdog termination reports cleanup unconfirmed.

Canonical node order is the stable declaration/application-site node identity order, independent of producer completion timing, waiter count or merge traversal order. Identities compare by Unicode code point, which is Go's bytewise UTF-8 order; the JS runtime compares code points rather than UTF-16 units, so primary failure selection and close order cannot diverge between targets for supplementary-plane identities. A locale collation or per-target string order was rejected because it makes canonical order target- or host-dependent.

The runtime seam validates each plan graph identically on both targets before plan initialization or any constructor: plan and node identities are present, node identities are unique, every dependency names a distinct node of the same plan, node kinds are binding or startup, the graph is acyclic and the shared node/edge/UTF-8 metadata bounds hold. A violation is a defect, never an unchecked build or a build that waits forever on a cycle; the compiler never emits such a plan, so this is a runtime-seam invariant like incompatible inputs. With one checked input context per build, dependency instance compatibility is structural; multi-context compatible-input validation arrives with plan instances in Unit 4.

Requests inherit materialized application services without rebuilding the application layer. Resources selected/acquired by a request graph belong to its request scope. Borrowed application/host services are not closed by the borrower. Explicit cross-build leases and keyed eviction remain libraries with a separately owning scope, not global layer memoization.

## Parameters, dynamic plans and inspection

Following the static first increment, admit ordinary typed configuration parameters: `layer Tenant(settings: Settings) { ... }`. Applying a parameterized declaration constructs a new lazy plan instance with declaration-site identities extended by its application site. Repeating a reference to the same plan value shares its internal identities; two applications remain distinct even for equal arguments and cannot bind the same nominal service twice within one flat graph. Their useful role is typed configuration capture and separate provision, not bypassing duplicate checks. Capture/escape evidence must prevent shorter-lived resources from entering longer-lived plans. No unrestricted type-level graph inference or structural configuration hashing.

Runtime plugin selection follows later through a checked layer interface with explicit allowed outputs, failures, inputs and ownership/trust. Inspection marks unresolved concrete nodes; static graph completeness cannot be inferred from an arbitrary host list. Atomic application startup is the default. Isolated plugin rollback, reload fallback, precedence and TTL policies belong to explicit libraries. Foreign runtime handles use scoped adapters and disposal; common Effra code does not require framework runtime construction.

CLI/MCP/LSP use the canonical compiler model and source tables. Expose inferred/declared contracts, public/hidden/startup nodes, stable nominal binding identities, merge/replacement/application provenance, shared occurrences, incoming dependents, construction paths, target restrictions, planned acquisition owner, child-failure observation policy and proved versus runtime/trusted facts. Construction and operation edges are different relations. Missing provision inputs, ambiguous bindings and visible cycles retain primary source anchors and related path/site information. Inferred remaining inputs on an open layer are valid; missing inputs diagnose when a closed provision boundary cannot supply them. Explain selected nodes without consumers after replacement, repeated startup selections, and request graphs that reselect application constructors; these observations never authorize effect pruning.

Keep checking bounded and package-local where possible. Normalize finite nominal rows; use graph work queues and cycle algorithms over interned nodes, not repeated recursive subtree rebuilding. Interface summaries transport the facts consumers need without reparsing implementation bodies. Retain only reachable layer declarations/runtime facilities; fluent/direct provision has the same retention. Performance and size acceptance is measured in the final measurement phase, with no current improvement claim.

## Gated implementation sequence

1. **Static plans and construction graph:** parse/format declarations, merge/replace, inferred/checked contracts, identity deduplication, complete source-grounded canonical inspection and diagnostic matrix. Supported pure providers execute both targets; unsupported fallible/startup forms diagnose until subsequent units land. Remove interim pure lowering when the shared managed seam lands.
2. **Managed acquisition:** this unit owns the agreed runtime plan/build interface: node identity, dependency identities, binding/startup kind and typed constructor adapter; output visibility remains compiler-owned. Pending-waiter sharing, build-owned producer cancellation, node ownership, completed rollback and dependent-first closure integrate both emitters through this interface; race/cancellation controls.
3. **Fallible construction and startup effects:** ordinary provider-returning factories, startup-only recipe selection, full construction/operation separation and hidden-effect retention. Depends also on the public acquire/finalization slice of owned core. Resource escape and failure/cleanup controls; extend pinned upstream behavior mapping.
4. **Plan instances and inherited inputs:** parameterized plans, borrowed application/request services, runtime compatible-input validation and lifetime inspection. Two unrelated domain programs plus fixture replacement and actual request resources. Dynamic plugins remain explicit following work until a finite interface/conformance gate exists.
5. **Public tooling and adoption:** actual CLI/MCP/LSP graph parity, formatter, invalid-program service continuity, shared Go/JS executable examples, unused declaration/module controls and independent review. Preserve exact source/artifact/raw gate receipts. Final size/build/server measurements follow the owner's benchmark-last order.

Each unit compiles and passes the full gate; runtime changes add race checks. Before closure, causal tests establish a concurrent diamond, failure with concurrent waiters, build cancellation with pending waiters, build-close during acquisition, late success release, concurrent producer failure attribution, rollback with cleanup defects, dependents-before-dependencies, all finalizers attempted, replacement changing rows/cycles, inherited application resources, separate builds, hidden startup retention and no unused initialization. Individual waiter cancellation is a runtime-seam test until a real language caller is admitted. Matching upstream names or inspecting unrun tests is insufficient.
