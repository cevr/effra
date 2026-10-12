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
| `Effect.provide(layer)` | `scopedWith` + `buildWithScope`; a fresh scope per call, but the current MemoMap is reused (`CurrentMemoMap.forkOrCreate`) | `internal/layer.ts:8-22`; `internal/effect.ts:4125-4131`; `Layer.ts:582-586`, `:765-775` |
| `Effect.scoped` | a root scope not attached to the parent | `internal/effect.ts:4100-4110` |
| `acquireRelease`, `acquireUseRelease` | scope finalizer + masking | `:4134-4150`, `:4396-4408` |
| `ensuring`, `onExit` | OnExit with uninterruptible finalizers | `:4171-4176` |
| scope close | LIFO finalizers | `:3982` |
| `forkChild` | attaches the child to the parent fiber, not to a scope: `forkUnsafe` adds it to `parentRuntime.children()`; the parent's `interruptChildren` middleware interrupts children when the parent ends | `:5532-5535` (attachment); `:599-603` (middleware); `:784-789`, `:5506` |
| `forkDetach` | daemon | `:5566`, `:5534` |
| `forkIn`, `forkScoped` | bind a child's lifetime to a given scope: an interrupt finalizer added at spawn and removed when the child completes; no resources transfer | `:5594-5663` |
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

Layer memoisation: `buildWithScope` uses `CurrentMemoMap.forkOrCreate` (`Layer.ts:582-586`, `:765-775`), so a provide inside another provide reuses the current MemoMap, and only a root run creates one. `{ local: true }` opts out with a fresh map (`internal/layer.ts:17-18`). The test "subsequent calls share MemoMap" (`Effect.test.ts:4644-4660`) expects a build count of 2 for one local provide plus two non-local provides. So nested non-local provides build once, and separate root runs build twice. Only leaf layers memoise (`Layer.ts:972`, `:1073`, `:1136`), and the memo key is object identity (`Layer.ts:416`). Effect ships a lint against repeated provides (`multipleEffectProvide`). Layer behaviour tests: `Layer.test.ts:35`, `:231`, `:240`, `:264`, `:621`, `:660`.

`FiberRef` is gone in v4; fiber-local state is a `Context.Reference` (`Context.ts:1327-1334`). Pipeable dual APIs: `Function.ts:102` (`dual`), `Pipeable.ts:564`.

## 3. What composes

- Every operation is an ordinary function over `Effect<A, E, R>`, so every operation wraps losslessly: `timeout` is ten lines over `raceFirst` and `sleep`, and a user can write the same.
- Requirements compose by type: `Effect.scoped` has type `Effect<A, E, Exclude<R, Scope>>`; `provide` subtracts what the layer outputs and adds what it needs. This is exactly F3b (`{ R - S }`) in Effra's vocabulary.
- Time is a service: `sleep` reads `Clock` from context (`:6373`), so tests swap the clock without touching the program (TestClock).
- Policy is data: `Schedule` is an inert value (`Schedule.ts:53`), interpreted by `retry`/`repeat`.

## 4. What hurts

- **No static lifetimes.** A fiber handle from `forkChild` or a resource from `acquireRelease` can escape its scope; Effect relies on runtime behaviour (the child is interrupted when its parent fiber ends) rather than a type error. A child also outlives the function that forked it. Effra's checker refuses the escape today (EF123), and H's owned fork removes the outliving child (primitives.md §6.2).
- **Layer sharing depends on placement.** Nested non-local provides share one MemoMap, separate root runs build again, and `{ local: true }` opts out. The only guard against accidental rebuilds is a lint.
- **Catch zoo.** About fifteen catch variants survive in v4, after v4 removed `catchAll`, `orElse`, `either`, `timeoutFail`, `timeoutTo` and `forkDaemon`. The churn shows that a large library surface costs readers.
- **No documented laws.** The behaviour lives in tests (`Effect.test.ts`) and implementation; there is no law file comparable to cats-effect's `GenSpawnLaws`.
- **Pipe noise.** `x.pipe(Effect.timeout("2 seconds"), Effect.provide(App))` is the idiom; method chaining and `dual` exist to soften it (`Function.ts:102`).

## 5. Mapping to Effra

| Effect | Effra today | Option H (chosen) | Option P (round 1, losing) |
|---|---|---|---|
| `yield*` inside `Effect.gen` | `run` | `run` (construct) | `run` |
| `Effect.fail` | `fail` | `fail` (construct) | `fail` |
| `Effect.fn` | `effect fn` | `effect fn` (construct) | `effect fn` |
| `forkChild` | `fork X` (construct, dynamic owner) | `fork X`, legal only inside an owner block | `run X.fork()` (no carrier, B1) |
| `Fiber.join` | `.join()` (compiler-known) | `run child` (construct) | `.join()` (no carrier, B1) |
| `Fiber.interrupt` | `.interrupt()` (compiler-known) | `interrupt child`, raises the child's row | `.interrupt()` (no carrier, B1) |
| `raceFirst` | none (hidden inside `.timeout`) | `race { fork a  fork b }` (construct over P13) | `.race(other)` (needs Q11) |
| `Effect.scoped` | `scope { }` (construct) | `scope { }` (construct) | `.scoped()` (needs Q11) |
| concurrent `forEach`, `all` | none (composite cause refused) | `all { fork … }` (proposed, P14) | intrinsic (no contract, M5) |
| `acquireRelease` | intrinsic | host primitive P5 | host primitive P5 |
| `timeout`, `timeoutOption`, `timeoutOrElse` | `.timeout(ms)` (compiler-known, charges `Scheduler`) | library over a race block + `Clock.sleep` | library over `.race` |
| `retry(schedule)` | none | library; `Schedule` is data | same |
| `provideService` | `.provide<S>(p)` (compiler-known) | `.provideService<S>(p)` (library, #132), or an `S = p` entry in a provide block | `.provide<S>(p)` (library) |
| `Effect.provide(layer)` | `.provide(Layer)` (compiler-known; plan read at the site) | `provide App { }` (construct; plan in the header) | library over a typed `Layer` (unsound, M4) |
| `catch`, `catchTag` | `.catch<E>(v)`, `.recover<E>(h)` | library (#131, #133) | same |
| `Layer.mergeAll`, `Layer.provide` | `layer` declaration with `merge`/`replace` | unchanged (construct) | unchanged |
