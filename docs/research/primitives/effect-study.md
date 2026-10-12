# Effect-TS study for #134

Appendix to [primitives.md](../primitives.md). All citations are to the pinned Effect 4.0.1 checkout at `conformance/upstream/effect` (commit `460272d`), paths under `packages/effect/src/` unless stated. Tests are under `packages/effect/test/`.

## 1. What is primitive

Effect 4 has no opcode enum. Every primitive is a node made by `makePrimitive` (`internal/core.ts:435`, `:467`) and interpreted by one loop, `runLoop` (`internal/effect.ts:622`). The kernel nodes are:

| Node | Where | Role |
|---|---|---|
| Success, Failure | `internal/core.ts:561`, `:581` | Completed values and causes |
| WithFiber | `internal/core.ts:622` | Read the running fiber (the door to context, scheduler, interruption) |
| Sync, Suspend, Yield | `internal/effect.ts:962`, `:974`, `:1015` | Thunks and cooperative yield |
| Async | `internal/effect.ts:1145` | Callback suspension (sleep, host calls) |
| Iterator | `internal/effect.ts:1413` | Generator driving (`Effect.gen`) |
| OnSuccess, OnFailure, OnSuccessAndFailure | `internal/effect.ts:1471`, `:2597`, `:3565` | Continuations: flatMap and the catch family |
| Exit | `internal/effect.ts:3774` | Re-raise a completed exit |
| OnExit | `internal/effect.ts:4166` | Finalisation hook |
| SetInterruptible | `internal/effect.ts:4561` | Masking |
| While | `internal/effect.ts:4905` | Loop |

Fork, interrupt and context update are not nodes; they are methods of the fiber runtime reached through `WithFiber`: `forkUnsafe` (`internal/effect.ts:5517-5539`), `interrupt` (`:4547`), `updateContext` (`:2175-2200`).

## 2. What is derived

| Operation | Derived from | Citation |
|---|---|---|
| `Effect.gen` | `suspend(fromIteratorUnsafe)` | `internal/effect.ts:1235-1240` |
| `flatMap` | OnSuccess continuation | `internal/effect.ts:1790` |
| `catchCause`, `catch`, `catchTag`, `mapError`, `orDie`, `sandbox` | OnFailure | `:2595`, `:2674`, `:3026`, `:3372`, `:3395`, `:1598` |
| `provideService`, `provideContext` | `updateContext` | `:2334`, `:2302` |
| `Effect.provide(layer)` | `scopedWith` + `buildWithScope`, fresh root scope per run | `internal/layer.ts:8-22`; `internal/effect.ts:4125-4131` |
| `Effect.scoped` | a root scope not attached to the parent | `internal/effect.ts:4100-4110` |
| `acquireRelease`, `acquireUseRelease` | scope finalizer + masking | `:4134-4150`, `:4396-4408` |
| `ensuring`, `onExit` | OnExit with uninterruptible finalizers | `:4171-4176` |
| scope close | LIFO finalizers | `:3982` |
| `forkChild` | attaches the child to the parent fiber, not to a scope | `:599-603`, `:784-789`, `:5506` |
| `forkDetach` | daemon | `:5566`, `:5534` |
| `forkIn`, `forkScoped` | attach to a given scope | `:5623-5663` |
| `join`, `await` | fiber observers | `:839-846`, `:792-801` |
| `interrupt` (of a fiber) | interrupt then await | `:915` |
| `race` | `raceAll([self, that])`, first success; `raceAll` is `withFiber` + `callback` + `forkUnsafe` (no `raceWith` in v4) | `:1696`, `:1603` |
| `raceFirst` | `raceAllFirst`, first exit, success or failure | `:1736`, `:1657` |
| losers | interrupted and awaited through `onExitUnsafe` + `fiberInterruptAll` | `:1620`, `:1673`, `:930`; tests `Effect.test.ts:1347`, `:1366`, `:1385` |
| `all` | `forEach`; sequential by default | `:4659-4697`, `:5005-5006` |
| concurrent `forEach` | fail-fast, interrupts siblings, waits for them, combines later sibling failures | `:5169`, `:5149`, `:5132-5136`; tests `Effect.test.ts:1041`, `:774` |
| `timeout` | `timeoutOrElse` → `timeoutOption` → `raceFirst(asSome(self), as(sleep(d), none))` | `:3875-3903` |
| `sleep` | `clockWith`; Clock is a `Context.Reference` | `:6373`; `Clock.ts:189` |
| `retry`, `retryOrElse` | a `catch_` loop that runs an attempt to completion, then asks the schedule | `:4089`; `internal/schedule.ts:66-79` |
| `Schedule` | an inert value | `Schedule.ts:53`, `:377-398` |
| `Effect.fn` | `makeFn`: gen + span + stack frames + pipeable | `:1300-1345` |

Layer memoisation: a `MemoMap` per `Effect.provide` call (`Layer.ts:769-775`, `:582-587`); `{ local: true }` opts out (`internal/layer.ts:17-18`; test `Effect.test.ts:4644-4660`); only leaf layers memoise (`Layer.ts:972`, `:1073`, `:1136`); the memo key is object identity (`Layer.ts:416`). Two `Effect.provide` calls build twice; Effect ships a lint for it (`multipleEffectProvide`, test `Effect.test.ts:4653`). Layer behaviour tests: `Layer.test.ts:35`, `:231`, `:240`, `:264`, `:621`, `:660`.

`FiberRef` is gone in v4; fiber-local state is a `Context.Reference` (`Context.ts:1327-1334`). Pipeable dual APIs: `Function.ts:102` (`dual`), `Pipeable.ts:564`.

## 3. What composes

- Every operation is an ordinary function over `Effect<A, E, R>`, so every operation wraps losslessly: `timeout` is ten lines over `raceFirst` and `sleep`, and a user can write the same.
- Requirements compose by type: `Effect.scoped` has type `Effect<A, E, Exclude<R, Scope>>`; `provide` subtracts what the layer outputs and adds what it needs. This is exactly F3b (`{ R - S }`) in Effra's vocabulary.
- Time is a service: `sleep` reads `Clock` from context (`:6373`), so tests swap the clock without touching the program (TestClock).
- Policy is data: `Schedule` is an inert value (`Schedule.ts:53`), interpreted by `retry`/`repeat`.

## 4. What hurts

- **No static lifetimes.** A fiber handle from `forkChild` or a resource from `acquireRelease` can escape its scope; Effect relies on runtime behaviour (the child is interrupted when its parent fiber ends) rather than a type error. Effra's checker refuses that escape today (EF123), and that is the guarantee P must keep (primitives.md §4).
- **Layer builds are per call.** Two `provide` calls build twice (`Layer.ts:769-775`); the only guard is a lint.
- **Catch zoo.** About fifteen catch variants survive in v4, after v4 removed `catchAll`, `orElse`, `either`, `timeoutFail`, `timeoutTo` and `forkDaemon`. The churn shows that a large library surface costs readers.
- **No documented laws.** The behaviour lives in tests (`Effect.test.ts`) and implementation; there is no law file comparable to cats-effect's `GenSpawnLaws`.
- **Pipe noise.** `x.pipe(Effect.timeout("2 seconds"), Effect.provide(App))` is the idiom; method chaining and `dual` exist to soften it (`Function.ts:102`).

## 5. Mapping to Effra

| Effect | Effra today | Option P |
|---|---|---|
| `yield*` inside `Effect.gen` | `run` | `run` (construct) |
| `Effect.fail` | `fail` | `fail` (construct) |
| `Effect.fn` | `effect fn` | `effect fn` (construct) |
| `forkChild` | `fork X` (construct) | `run X.fork()` (kernel P6) |
| `Fiber.join`, `Fiber.interrupt` | `.join()`, `.interrupt()` (compiler-known) | kernel P7, P8 |
| `raceFirst` | none (hidden inside `.timeout`) | `.race(other)` (kernel P13, new) |
| `Effect.scoped` | `scope { }` (construct) | `.scoped()` (kernel P4) |
| `acquireRelease` | intrinsic | kernel P5 |
| `timeout`, `timeoutOption`, `timeoutOrElse` | `.timeout(ms)` (construct, charges `Scheduler`) | library over `race` + `Clock.sleep` |
| `retry(schedule)` | none | library; `Schedule` is data |
| `provideService` | `.provide<S>(p)` (compiler-known) | library (#132) |
| `Effect.provide(layer)` | `.provide(Layer)` (compiler-known; plan read at the site) | library over a typed `Layer<Out, E, In>`; plan read from the `layer` declaration |
| `catch`, `catchTag` | `.catch<E>(v)`, `.recover<E>(h)` | library (#131, #133) |
| `Layer.mergeAll`, `Layer.provide` | `layer` declaration with `merge`/`replace` | unchanged (construct) |
