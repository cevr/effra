# Lawful runtime contract and evidence

Status: adopted design direction, 2026-10-08. This is a source-backed public
research record. It does not claim that a runtime contract checker, a second
Go runtime, cross-target law suite, runtime selector, or alternate JavaScript
representation exists.

The recovered runtime question has two separate parts. A small host contract
can describe the operations on which Effra's checker and lowering rely. A
future host package may fulfil that contract behind the trusted boundary. That
package is ordinary Go or JavaScript host code; it is not an Effra source
runtime and it does not receive an unsafe DSL. The current native provider is
the Go implementation under [`runtime/effra`](../../runtime/effra), and the JS
emitter uses the pinned Effect-compatible ABI described below.

## Primary sources, inspected paths, and mechanisms

These are the primary comparisons used for this record. Library links are
release tags or an immutable source revision. Standards links identify the
normative operation; an implementation receipt must still record its exact
toolchain revision.

| Pinned primary source and inspected path | Mechanism observed | Effra choice and limit |
| --- | --- | --- |
| [cats-effect 3.5.7 kernel](https://github.com/typelevel/cats-effect/tree/v3.5.7/kernel/shared/src/main/scala/cats/effect/kernel), especially `MonadCancel`, `GenSpawn`, `GenConcurrent`, `GenTemporal` and their `*Laws` | A small capability hierarchy separates cancellation, spawning, concurrency and time. Law modules run implementations against a virtual `TestContext`/`Ticker`. | Adopt one named operation contract and one law vocabulary rather than a generic runtime parameter in ordinary source. Keep the contract at the host boundary and make target/provider/fixture/revision part of evidence. The hierarchy is comparison material, not a request to copy every combinator. |
| [Rust `Future`](https://doc.rust-lang.org/std/future/trait.Future.html), [`Waker`](https://doc.rust-lang.org/std/task/struct.Waker.html), and the [global allocator attribute](https://doc.rust-lang.org/reference/runtime.html#the-global_allocator-attribute) | The language exposes polling and wake-up primitives; executors and a visible program-level host facility remain library/runtime choices. The allocator attribute is explicit rather than hidden in every library. | Keep the Effra core small and make any future whole-program runtime choice visible at the entry. Rust's `unsafe` executor machinery is counterevidence to an Effra-source runtime; host `extern` code must carry the trust boundary. |
| [Kotlin coroutine language specification](https://kotlinlang.org/spec/asynchronous-programming-with-coroutines.html) and pinned [kotlinx.coroutines 1.9.0 `CoroutineDispatcher`](https://github.com/Kotlin/kotlinx.coroutines/tree/1.9.0/kotlinx-coroutines-core/common/src/CoroutineDispatcher.kt) | Compiler support is limited to continuation/intrinsic machinery while builders and dispatchers are library APIs. | Keep ordinary functions, scopes and host services visible. Do not add continuation syntax or a generic runtime type merely because a library can provide a dispatcher. |
| [OCaml Eio 1.2.0 `Switch`](https://github.com/ocaml-multicore/eio/tree/v1.2.0/lib_eio) and [Eio main](https://github.com/ocaml-multicore/eio/tree/v1.2.0/lib_eio_main) | Switches and fibers are library-owned; the main runner chooses a backend through host configuration. | A backend hidden in environment configuration is not Effra runtime selection. If selection is later admitted, it is a checked, visible entry choice with a contract receipt. |
| [ZIO 2.1.19 runtime sources](https://github.com/zio/zio/tree/2.1.19/core/shared/src/main/scala/zio), especially `Runtime`, `Executor` and `FiberRef` | The interpreter is fixed while executor, supervisor and environment policies are installed through runtime/layer configuration. | Preserve the shipped interpreter/provider as the default. Purely observational policies may be entry-installed and unrowed; semantic `Scheduler` and `Clock` remain explicit requirements. |
| Pinned [Effect 4.0.1 `Effect.ts`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Effect.ts), [`Scheduler.ts`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Scheduler.ts), [`Clock.ts`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Clock.ts), and [`Tracer.ts`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Tracer.ts) | Effect supplies `Effect<A, E, R>`, `Context`, `Scope`, fibers, scheduler/clock references and tracer hooks. The current generated JS surface imports these values directly. | Keep JS Effect-compatible and use its actual references/options. A second JS representation would need a separate ABI decision and conformance receipts; it cannot be inferred from a Go runtime contract. |
| [Go `context` source](https://go.dev/src/context/context.go), [runtime `defer`/goroutine idioms](https://go.dev/doc/effective_go#concurrency), and the current Effra paths [`effect.go`](../../runtime/effra/effect.go), [`fiber.go`](../../runtime/effra/fiber.go), [`scope.go`](../../runtime/effra/scope.go), [`scheduler.go`](../../runtime/effra/scheduler.go), [`latch.go`](../../runtime/effra/latch.go) | Go keeps cancellation, goroutines, `defer`, channels and cleanup in ordinary host code. Effra's current Go runtime adds typed `Exit`/`Cause`, scopes, owned fibers, acquisition and a managed test scheduler. | Use ordinary Go host code behind the contract. Preserve explicit cancellation versus completed shutdown and never treat a Go signature as proof of foreign cancellation or cleanup. |
| [Erlang efficiency guide: processes](https://www.erlang.org/doc/system/eff_guide_processes.html) and its scheduler/reduction discussion | BEAM preemption and reduction accounting belong to the fixed VM; OTP libraries compose on top of it. | A Go `runtime` contract is not a claim that Effra can replace the BEAM/Go scheduler or add a per-operation yield counter. Scheduler policy is semantic and remains an explicit contract. |

Repository inspection also covered the lowering seams in
[`internal/compiler/emit_go.go`](../../internal/compiler/emit_go.go) and
[`internal/compiler/emit.go`](../../internal/compiler/emit.go), the existing
managed-runtime tests under `runtime/effra/*_test.go`, and the public behavior
summary in [`docs/runtime.md`](../runtime.md). The source comparison separates
what those files do today from the proposed contract and law evidence.

## P1–P12 host core

These IDs are stable research vocabulary. They are not currently a new
Effra syntax or a public Go interface. The current Go names and JS imports are
evidence for the shape, while P9 remains private in the Go core until its
public seam is separately implemented and gated.

| ID | Contract operation | Current native evidence and boundary |
| --- | --- | --- |
| P1 | Construct `Exit` success, typed failure, defect and interruption; combine `Cause`. | Go `Succeed`, `Fail`, `Die`, `Interrupt`, `FromCause`; JS `Effect.succeed`, `fail`, `die`, `interrupt`, `Cause.combine`. Complete cause identity remains observable at the host boundary. |
| P2 | `invoke` safe point: run one program, observe cancellation and convert a managed panic to a defect. | Go `Invoke` and generated checkpoints; JS `Effect` execution/ownership chunks. A panic or process-fatal exit is not a recoverable typed failure. |
| P3 | Dispatch/recover a lone named typed failure. | Go `Catch`; JS `Effect.catch`/tagged failure handling. A compound cause or cleanup defect is not silently dispatched. |
| P4 | `scoped(program)`: mint an owning scope and close it before returning. | Go `Scoped`/`Scope`; JS `Scope` and the lifecycle adapter. Closure means owned shutdown and cleanup completion. |
| P5 | Masked `acquireRelease` and `onCancel`; register release authority with a scope. | Go `AcquireRelease` and `Scope.OnCancel`; JS `acquireUseRelease`/abort hooks. Late successful acquisition releases once; a foreign acquisition must eventually return. |
| P6 | Fork a program into its dynamic owning scope and mint a `Fiber`. | Go `Fork`; JS `Effect.forkDetach`/Effra ownership adapter. Admission precedes execution, and a closing owner rejects late work. |
| P7 | Join a fiber and propagate its complete exit. | Go `Fiber.Join`; JS `Fiber.await`. Completed observation marks the child observed; an interrupted join does not. |
| P8 | Request interruption/cancellation of a fiber. | Go `Fiber.Interrupt`/`Cancel`; JS `Fiber.interruptUnsafe` through the adapter. A cancellation request is distinct from completed cleanup. |
| P9 | Managed one-shot wait/signal, minting a `Signal`/`Latch`. | Go `managedSignal`/`Latch` and the test scheduler already implement the mechanics, but the core registration helpers are private. A public host capability must use the same quiescence seam. |
| P10 | Sleep through an explicit scheduler/clock driver. | Go `SleepWithDriver`; JS `Clock`/`Scheduler` references. Semantic time cannot be an observational default. |
| P11 | Run a root program, minting the root scope and fiber context. | Go `RunContext`/`RunContextWithScheduler`; JS `Effect.runPromise`/`runForkWith`. Entry reporting and signal handling remain host glue. |
| P12 | Invoke a host call through an explicit `fromGo`/`extern` boundary. | Go `FromGo`, capability providers and generated extern calls; JS host functions wrapped in Effect. A host declaration supplies shape, not behavioral proof. |

Timeout, race, latch composition and layers are derived library behavior. They
must not become extra primitive syntax or silently change runtime selection.

## R1–R15 laws with the current cleanup policy

The following statements reconcile the original runtime spike with the
accepted E2 cleanup decision. In particular, cancellation classification is
made at the transition, the owner's own body exit is preserved, ordinary
abandoned work may be normalized, cleanup-origin failures remain visible, and
shutdown is awaited.

| ID | Law | Required evidence state |
| --- | --- | --- |
| R1 | Scope resources release in reverse acquisition order. | A trace law over two or more resources; the FIFO mutant is a causal red control. Existing Go white-box evidence does not make cross-target R1 tested. |
| R2 | Scope close stops admission, requests child cancellation, awaits child cleanup, then releases the owner's resources. | A Go and JS trace with a child whose cleanup waits; completion must precede parent release. A deadline or watchdog return is not completion. |
| R3 | At the close/cancellation transition, preserve the owner's own body outcome. Normalize only an ordinary typed body failure from an abandoned, unobserved child when the E2 cancellation policy classifies it as abandoned; retain defects and cleanup-origin failures. Await child cleanup before publishing the owner exit. | Separate normal close, cancellation-aborted owner, observed child, unobserved child, and cleanup-failure fixtures. “Drop every `Fail`” is refuted. |
| R4 | A completed join or interrupt observes the child before propagating its exit; an interrupted join does not. | Cross-target observation-state fixture; current recovered evidence marks this law unresolved. |
| R5 | Interruption acknowledges only after cleanup. Interruption-only shutdown may normalize to success; body failures, defects and cleanup failures remain. | Join/interrupt controls with a cleanup latch and complete cause comparison. |
| R6 | Preserve complete causes. Dispatch handles only a lone matching typed failure. During cancellation, normalize only ordinary abandoned child/timed-work typed failures; preserve the owner's body exit, defects and cleanup-origin failures, and await cleanup. | Equal failure occurrences, compound body plus cleanup defects, observed versus abandoned work and a typed cleanup failure. The historical JS finalizer-origin gap remains a separate repair/evidence boundary. |
| R7 | A recovery handler runs in the failing execution before its owning scope closes. | A handler timing/owner trace; current source has checker evidence but no complete law receipt. |
| R8 | Timeout owns the work in a deadline scope, waits for cleanup, and records timeout/interruption without recasting a timer defect. An ordinary typed failure from abandoned timed work may be normalized by the E2 policy; the owner body result, defects and cleanup-origin failures remain. Successful and failed deadlines retain cleanup provenance. | Successful deadline, failed deadline, timer defect, typed timed-work failure and cleanup failure on both targets. A timer return before shutdown is refuted. |
| R9 | Fork uses the dynamic scope as owner. Forking into a closing owner or joining from a non-ancestor is a defect. | Owner identity and late-admission controls; current evidence is partial. |
| R10 | Admission is visible before child execution, and an acquisition that completes during close is released exactly once. | Scheduler registration and late-acquisition trace; Go evidence exists, cross-target parity remains a task. |
| R11 | Constructing an effect runs nothing. | A construction-only fixture plus a side-effect counter; current cross-target evidence is unresolved. |
| R12 | `provide` executes the program under its provision owner and closes the selected nodes afterward. | Layer source and acquisition tests on Go/JS; retain the provision owner in the trace. |
| R13 | One layer build acquires each node once, publishes after constructors finish, rolls back before failure, closes dependents first, attempts every finalizer, and owns its producers. | Layer acquisition/failure/rollback cases and concurrent waiters; provider sharing cannot be inferred from a passing happy path. |
| R14 | Virtual-time adjustment fires deadlines at or before the target in order after managed continuations quiesce; sleep and timeout use the same clock. | Scheduler barriers and partial cleanup controls; unmanaged foreign blocking remains outside the guarantee. |
| R15 | A managed panic is a defect and cleanup still runs. | Native panic/cleanup case on each supported target; process-fatal and unmanaged goroutine panics are outside the protocol. |

## RS1 finite contract and cross-target law suite

RS1 is the first finite evidence unit for this vocabulary. It covers the full
P1–P12 host shape and R1–R15 law set together; it is a bounded execution and
receipt contract, not a new source construct, a runtime selector or a proof of
all interleavings. The suite runs two unrelated ordinary `.ef` callers: one
resource/child-lifecycle caller and one timed server or synchronization caller.
Both callers run against the native Go provider and the pinned
Effect-compatible JS provider with unchanged source contracts. Each receipt
binds the provider revision, target, toolchain, fixture revision, law IDs,
seed and raw outcome.

The reference model compares traces using an allowed partial order. A trace
event carries an owner, child/resource identity and one of `construct`,
`admit`, `start`, `observe`, `cancel-request`, `body-exit`, `cleanup`,
`release`, `deadline`, `adjust`, `handler`, `panic`, or `publish`. Events that
the relation does not order may occur in either target order; host scheduling
is not semantic evidence. The finite RS1 relations are:

| Law | Required cross-target partial order or observation |
| --- | --- |
| R1 | Every resource is acquired before its release, and releases occur in reverse acquisition order within one owner. |
| R2 | Owner close stops admission, then requests child cancellation, observes child cleanup completion, releases owner resources, and publishes the owner exit. |
| R3 | Normal close retains an unobserved child typed failure in the owner outcome; an aborted owner may normalize only an ordinary typed failure from abandoned work under the accepted E2 policy. Defects, the owner's body outcome and cleanup-origin failures remain before publication. |
| R4 | A completed join or interrupt records observation before propagating the child exit; an interrupted join records no completed observation. |
| R5 | An interruption request precedes cleanup completion and exit publication; interruption alone may normalize to success while body failures, defects and cleanup failures remain. |
| R6 | Each failure occurrence and cleanup defect remains identifiable in the complete cause. Dispatch is allowed only for one matching typed failure, never for a compound cause. |
| R7 | The recovery handler starts and completes in the failing execution before that execution's owner closes. |
| R8 | The deadline winner precedes work cancellation, cleanup completion and timeout publication. A timer defect, owner body result and cleanup-origin failure retain their identity. |
| R9 | Admission records the dynamic owner before execution; closing an owner precedes late-fork rejection, and a non-ancestor join is a defect. |
| R10 | Admission precedes child execution; an acquisition that succeeds during close is released exactly once before owner publication. |
| R11 | Effect construction has no execution event or host side effect; only a later run may emit execution events. |
| R12 | Provision establishes the provision owner before program execution and closes selected nodes after execution, before publication. |
| R13 | One build acquires each node once, publishes only after constructors complete, rolls back before failure publication, closes dependents before dependencies, attempts every finalizer, and owns its producers. |
| R14 | Managed continuations quiesce before time selection; `adjust(n)` fires registered deadlines at or before `n` in order, and sleep and timeout use one clock. |
| R15 | A managed panic precedes a defect exit, while cleanup completes before that exit is published. |

RS1 has four explicit cross-target gaps at its starting boundary: R1 has
Go-only white-box evidence and no complete JS trace; R4 has no target
observation-state fixture; R7 has checker evidence but no execution-timing
receipt; and R11 has no complete cross-target construction-only receipt. These
are unresolved obligations with named justifiers, not silently passing rows.
The remaining laws retain their narrower current evidence and must still be
bound to the same revision/target/fixture receipt before a provider is called
tested.

The causal mutants are part of the suite. `m1` changes LIFO release to FIFO
and must refute R1. `m2` releases owner resources before child cleanup and must
refute R2. `m3` drops an unobserved child failure at **normal close** and must
refute R3. The approved E2 cancellation normalization of an ordinary typed
failure from abandoned, unobserved child or timed work is a positive control,
not a failing mutant. Separate negative controls must retain the owner's body
outcome, defects and cleanup-origin failures; dropping any of those is also a
refusal condition. A finite suite never promotes a passing row to proof.

## RS2 public managed-wait boundary

RS2 is a distinct implementation/evidence unit. It does not redefine RS1's
law IDs or claim a second provider. Its finite boundary is:

1. Make the one-shot managed wait/signal operation P9 public to the reviewed
   host capability boundary, retaining scheduler registration and quiescence
   semantics.
2. Make the latch and layer implementations depend only on that public core
   surface. They may not reach into private scheduler or fiber helpers.
3. Add a source check over runtime modules that rejects references to
   unexported core identifiers, including the recovered private registration,
   suspension and turn helpers. Pair it with public virtual-time, cancellation,
   race and cleanup controls so a name-only migration cannot pass.

RS2 is complete only when the public caller and the static dependency check
both pass their target-appropriate gates. Until then P9 remains the private
Go-core boundary described above, and no public runtime substitution is
claimed.

## Shared law/contract obligations and assurance categories

One named obligation mechanism is shared by domain/opaque ingress, service
implementations and runtime providers. A domain or opaque ingress records its
owner-controlled construction/validation obligation; a service implementation
records its operation rows and named laws; and a runtime provider records its
P1–P12 fulfilment and R1–R15 obligations. The mechanism records contract and
law identity, source revision, target, provider, fixture, toolchain, outcome
receipt and a visible justifier when execution is unavailable. This is a
receipt schema and ownership rule, not an admission of `law` or `contract`
syntax; that construct remains a separate owner decision.

The five categories have the same meaning at each boundary:

| Category | Domain/opaque ingress | Service | Runtime |
| --- | --- | --- | --- |
| **Nominally checked** | Owner ingress and nested representation facts type-check. | Implementation operations, rows and law identities type-check. | Fulfilment, host symbols, target clauses and law identities type-check. |
| **Trusted decision** | The owner-controlled predicate or opaque host ingress is accepted as a named boundary. | An explicitly trusted host adapter supplies the declared implementation. | The shipped Go host implementation or pinned Effect adapter is accepted behind the host boundary. |
| **Tested law** | A finite ingress/ownership fixture passes for a revision and target. | A finite service-law fixture passes for implementation, target, fixture and revision. | A finite runtime-law fixture passes for provider, target, fixture and revision; it never proves all interleavings. |
| **Refuted** | A counterexample value or forged/nested ingress is rejected. | A counterexample service trace refuses that implementation. | A counterexample trace or causal mutant refuses that provider/law. |
| **Unresolved / unavailable** | Required fixture, target or nested proof is missing, with a visible justifier. | The law was not run, is stale, or exceeds the finite budget, with the reason retained. | The law lacks a provider/target fixture or depends on an unimplemented seam; inspection and receipts retain the reason. |

Refuted obligations refuse the affected build or provider. Unresolved or
unavailable obligations remain visible in inspection and receipts and may be
allowed only with justifier lint. Every receipt is revision/target/fixture
bound; finite tests are evidence, never proof.

The recovered spike ran three deliberately broken host variants against the
then-current tests. FIFO release (`m1`) broke R1 while the cross-target suite
passed, proving that cross-target coverage could hide a lifecycle defect.
Release-before-children (`m2`) broke R2. The corrected `m3` is specifically
dropping an unobserved child failure at normal close, which breaks R3; it is
not the approved cancellation normalization for abandoned typed work. These
are historical causal controls, not a current full law receipt. The durable
suite must retain all three, add R1/R4/R7/R11 cross-target cases, and compare
traces against a runtime-neutral reference model rather than host timing.

Current evidence therefore remains mixed: Go runtime tests and the JS
Effect-compatible adapter cover subsets of R1–R3, R5–R6, R8 and R12–R15;
R4, R7 and R11 have explicit missing cases; R9/R10 need broader owner and
target witnesses; and no second real Go provider exists. The public `runtime X`
entry item is consequently deferred. An environment variable, `go.mod replace`
or library `run(app)` that secretly changes lowering is not evidence of a
second provider.

Observational policies are narrow. Tracer, fiber observer and entry Logger may
be installed once by an ordinary layer, receive data only and default to no-op;
their failures are reported and cannot change a program result. Scheduler,
Clock and any policy capable of changing semantic execution stay in the
requirement row. Machine plans and machine providers are separate contracts;
they do not substitute for a lawful effect runtime.

## Go and TypeScript/Effect idioms

The ordinary Go baseline is a typed function receiving `context.Context`, a
goroutine or channel where concurrency is needed, and `defer` for cleanup.
Effra keeps that local readability while making the success/failure/service
rows and owning scope inspectable. A host runtime provider is an ordinary Go
package with explicit constructor and operation contracts; it does not require
users to spell raw fibers or continuations in `.ef`.

The ordinary TypeScript baseline is an `Effect<A, E, R>` value composed with
`Context`, `Scope`, `Fiber`, `Clock` and `Scheduler`. The current JS emitter
already projects this shape. Effra therefore keeps Effect as the JS ABI and
uses Effect's real references/options. It does not invent `F<A, E, R>`, erase
`R`, insert an implicit `Effect.run`, or pretend that a userland Go provider
creates a second JS runtime.

## North Star fit and tradeoff

This fits **Go-like simplicity through regular abstractions** by keeping the
host implementation and ordinary callers typed functions and scopes. It fits
**Explicit contracts and clear guardrails** by naming every required
operation, law, target, provider and evidence category. It fits **Owned
lifetimes** by defining completion as awaited child shutdown and cleanup, not
as a cancellation request. It fits **Honest target capabilities** by keeping
Go scheduling and JS Effect representation explicit. The tradeoff is a trusted
host boundary and a finite law ledger: runtime authors gain replaceable policy
only after paying for revision-bound receipts, and unresolved laws remain
visible instead of being silently treated as guarantees.

## Rejected alternatives and counterevidence

- An ML signature or trait that exposes raw fibers/continuations is rejected:
  it adds a second type-level language without making unsafe host operations
  unrepresentable. Go method type-parameter limits are a practical constraint,
  not a reason to invent a generic runtime syntax.
- A fixed import path swapped by `go.mod replace`, environment configuration,
  or a library `run(app)` is rejected because every fork/scope/timeout lowering
  must agree on the selected provider. Eio's backend environment and madsim/
  loom-style configuration are counterevidence against hidden selection.
- An Effra-source executor is rejected because mutexes, goroutines, timers and
  safe wake-up primitives would require an unsafe host sublanguage. Rust's
  `Waker`/`Pin` ecosystem demonstrates the boundary rather than a portable
  source feature.
- A second JS runtime is rejected for the current scope: Effect values are the
  public `.d.mts` contract and TS consumers rely on their `Context`/`Fiber`
  representation. A different representation may be reconsidered only in a
  scoped ABI decision with actual consumers and law receipts.
- A Go fairness/yield counter is rejected. Go's scheduler already supplies
  preemption; adding a per-`Invoke` counter would create a semantic policy
  without a caller. JS's Effect scheduler reference is the target-specific
  comparison, not a demand for parity by renaming.
- A finite test pass cannot overturn a failing mutant or prove arbitrary
  interleavings. `m1` is direct counterevidence to the old cross-target test
  sufficiency; cleanup-origin loss in the historical JS adapter is direct
  counterevidence to filtering failures by public tag or kind. The accepted
  E2 repair preserves cleanup provenance before the first cause merge, but it
  does not establish admitted `.ef` typed finalizers or a complete runtime law
  proof.

## Finite execution required before support

RS1 must run the two unrelated `.ef` callers and the partial-order, cause,
diagnostic and cleanup checks described above on Go and the pinned JS Effect
target. A second real Go provider, if one is later built, must run the same
callers without compiler edits or hidden configuration. Its receipt binds
provider revision, target, toolchain, fixture revision, law IDs, seed and raw
outcomes. RS2 must independently run its public P9 managed-wait caller and
private-core reference check.

Causal negatives must include FIFO release (`m1`), release-before-children
(`m2`), dropping an unobserved child failure at **normal close** (`m3`),
interrupted join observation, timer defects and typed cleanup failures. The
owner-body outcome, defect and cleanup-origin controls remain negative controls;
the approved E2 cancellation normalization of an ordinary abandoned typed child
or timed-work failure is a positive control. Also retain missing Scheduler/Clock
rows, implicit `Effect.run`, hidden runtime selection, row mismatch and an
unavailable Go host import. Refuted laws refuse a selected provider. Unresolved
or unavailable inputs require an explicit inspection/receipt entry and justifier
lint. Until these executions and independent review exist, this record remains
a durable design comparison, not a support or release claim.
