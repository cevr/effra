# Prior art for #134

Appendix to [primitives.md](../primitives.md). Each entry gives what the system gets right and what Effra should not copy. Pins are to the checkouts and documents read for this spike; versions are the ones named. The Effect study is a separate appendix ([effect-study.md](effect-study.md)).

## 1. Structured concurrency and deadlines

**Go (spec go1.27).**
- `go`, `defer` and `select` are statements. `context.WithTimeout`/`WithDeadline` are functions, and a child deadline is the minimum of its own and its parent's.
- `errgroup` cancels the group on the first error.
- `context.Value` is untyped dependency injection.
- *Right:* a deadline is a value passed down; first-completion is a statement (`select`) because it binds channels.
- *Don't copy:* `context` fuses cancellation, deadline and values in one bag. Goroutines have no owner, so a goroutine can outlive its caller and a forgotten `wg.Wait` is silent.

**Kotlin coroutines (1.10.2).**
- `coroutineScope { }` is fail-fast: a child failure cancels its siblings.
- `withTimeout` is a `ScopeCoroutine`; `withTimeoutOrNull` is the fallback variant.
- All of these are library functions taking a trailing lambda.
- *Right:* structured scopes as ordinary functions.
- *Don't copy:* constructs and library are indistinguishable (a trailing lambda looks like a block). Fail-fast interacts badly with fork + join + catch: catching a child's failure at its join is too late, because the scope has already cancelled the siblings.

**Swift.**
- `async let` (SE-0304/0317, 0317 lines 295, 313) binds a child task to the enclosing scope implicitly, and the task is awaited or cancelled at scope exit.
- `withDeadline` (SE-0526) takes the minimum of nested deadlines (lines 167–172), waits for the body (129–131), and makes deadlines lexically visible (842–857).
- SE-0311 task-locals bind for the rest of a scope (1172–1188).
- *Right:* cleanup is awaited before the deadline is published.
- *Don't copy:* implicit task lifetimes tied to syntax nesting that the reader cannot see.

**Trio.**
- Nurseries and cancel scopes (`open_nursery`, `move_on_after`, `fail_after`) are library context managers.
- The effective deadline is the minimum of the enclosing ones.
- After a `move_on_after` block, a flag says whether the block was cancelled.
- *Right:* a cancel scope is a value with a protocol (`with`), and deadlines nest by minimum.
- *Don't copy:* the "was it cancelled?" flag read after the block, instead of a typed result.

**Java Loom** (JEP 505/525). `StructuredTaskScope` went through a long series of Joiner policies across previews.
- *Lesson:* group failure policy is the hard part. Effra defers it (O3) rather than guess.

**OCaml Eio** (`7fe3513d`).
- `Switch.run` owns fibers and resources; `on_release` is LIFO and protected (`eio__core.mli:81-95`).
- `Fiber.first` awaits the loser (`fiber.ml:147`).
- `with_timeout` is `Fiber.first` against a sleep (`time.ml:64-65`).
- *Right:* exactly P's derivation: timeout is library over first-completion.

**Erlang and Elixir.**
- `receive … after` (`expressions.md:728-748`) is the only construct-level timeout in the survey, and it exists because `receive` binds a mailbox pattern.
- `Task.await(task, timeout)` takes a timeout argument, and its default timeout exits the caller (`task.ex:776-781`). `Task.yield || Task.shutdown` is the cooperative alternative (`:1092-1118`).
- `with … else` flattens matching (`special_forms.ex:1690`).
- *Don't copy:* a timeout that kills the caller by default.

**Gleam.**
- `process.receive(subject, within)` takes a timeout argument.
- `use` (`ast.rs:4480-4510`) passes the rest of the block as a callback. It replaced `try`, and its readability is criticised in the v0.25 post, the language tour and the v1.10 changelog (`v1.10.md:473`; `expression.rs:829-835`).
- *Lesson for D:* "the rest of the block is an argument" hides where control returns.

**tokio.**
- `select!` cancels the losing futures by dropping them.
- *Don't copy:* cancellation by drop gives no awaited cleanup.

## 2. Effect systems

- **ZIO 2.**
  - `timeout` returns an `Option` (`ZIO.scala:2180`) and waits for the interrupted work (`:2172-2178`).
  - `race`, `ZIO.scoped`/`Scope` and `ZLayer` are library.
  - ZIO publishes no laws.
- **cats-effect 3.**
  - `timeoutTo` is derived from `racePair` (`IO.scala:850-859`), and `Resource` is a library data type.
  - Behaviour is pinned by law files: `MonadCancelLaws`, `GenSpawnLaws` and `GenTemporalLaws`, the last including a sleep-race-minimum law. `raceCanceledIdentity` was deprecated.
  - *Right:* laws as the contract. This is the model for K1–K15.
- **Koka** (`9c55695d`).
  - `with` desugars to passing the rest of the block (`tour.kk.md:205-245`, minimum generality `269-271`).
  - Timeout is `firstof` (`interleave.kk:57-61`), and `finally` is library (`hnd.kk:485`).
  - *Right:* handlers give a fixed syntax a pluggable meaning.
- **Effekt.** Blocks are second-class (they cannot escape), the same idea as #106.
- **Unison.** The pretty-printed libraries warn that `bracket` can leak (`pretty-print-libraries.output.md:11403-11414`).
  - *Lesson:* resource safety needs a lifetime discipline, not just a bracket function. That is the owner rule (Q11).
- **Roc.** Purity is inferred, and there is no effect polymorphism (`functions.md:121`).
  - *Don't copy:* Effra needs row polymorphism (F3b) for library wrappers.
- **MoonBit async** (`e72901a1`).
  - `with_task_group` is the structured scope; `with_timeout` and `with_timeout_opt` are built on the task group (`async.mbt:96-108`); retry takes an enum policy.
  - `defer` and `errdefer` are keywords (`fundamentals.md:1177-1235`).
  - A child error cancels its siblings (`task_group.mbt:27-34`).
  - *Right:* time as library over the scope.
- **Elm** (`Cmd`/`Sub`). Effects are data that a runtime interprets.
  - *Right:* a non-Effect runtime driven by its own loop (the model for a UI frame provider).
- **React context.** The default value of a Provider is an anti-pattern: a missing provider silently uses the default.
  - *Lesson:* static presence checking (Effra rows) is the point.
- **ReScript.** `async` passes straight through to JavaScript, and inlining bugs show the cost of a construct whose lowering differs by target.

## 3. Fixed syntax, pluggable meaning (the portability test)

The owner's portability test asks whether a construct's meaning can be a protocol that userland providers implement. The systems below give a fixed syntax a pluggable meaning. Each entry ends with its failure mode, because the risk of this design is meaning drift.

- **F# computation expressions.** The docs (learn.microsoft.com, *Computation Expressions*, "Creating a New Type of Computation Expression") give the translation:
  - `let! p = e in ce` becomes `builder.Bind(e, fun p -> …)`;
  - `use! v = e` becomes `Bind(e, fun v -> builder.Using(v, …))`;
  - `and!` uses `MergeSources`.

  "Those constructs that are not implemented are not available."
  - *Failure:* no laws. Whether `and!` runs in parallel or in sequence depends on the builder (same page, section "and!"), so the same syntax means different things per builder.
- **C# awaitable pattern.** C# standard §12.9.9.2 (dotnet/csharpstandard draft-v8, `standard/expressions.md:3786-3813`): `await t` is classified like `(t).GetAwaiter().GetResult()`, through `IsCompleted` and `INotifyCompletion`. Custom async method builders (§15.14.1, `standard/classes.md:5408`) bind a task type to a builder through `AsyncMethodBuilderAttribute`.
  - *Failure:* `SynchronizationContext` deadlocks and the `ConfigureAwait` folklore. The protocol fixes the shape but not where continuations run. (Cited from memory: Cleary 2012, Toub 2019.)
- **Rust `async`/`await`.** One protocol, `Future::poll` plus `Waker`, and many executors: tokio for servers, embassy for embedded. Embassy compiles tasks to state machines with no allocation (`embassy README.md:11`, commit `f16efeffe`) and builds its own waker vtable (`embassy-executor/src/raw/waker.rs:5`).
  - *Failure:* the ecosystem split. "There is no asynchronous runtime in the standard library … Not all async applications, frameworks, and libraries are compatible with each other" (Async Book, *The Async Ecosystem*). The cause is that timers, spawn and IO were left outside the protocol. **Effra's kernel includes spawn (fork), race, sleep and scope for exactly this reason.**
- **Python `with` / `async with`.** PEP 343 translates `with` to `__enter__`/`__exit__` with `try`/`finally`; PEP 492 adds `__aenter__`/`__aexit__`. Trio's cancel scopes (`src/trio/_timeouts.py:14-55`; `CancelScope.__enter__` at `src/trio/_core/_run.py:612`; `NurseryManager.__aexit__` at `:1098`; pluggable `clock` at `:2424-2427`, commit `bcc9ceeb`) are library over that protocol.
  - *Failure:* `__exit__` can swallow exceptions by returning true. PEP 789 (draft) documents that yielding inside these managers in async generators means "the wrong task is canceled, timeouts are ignored, and exceptions are mishandled". The protocol cannot stop a scope from being suspended away. Effra's owner rule is static, so the analogous escape is refused (EF123).
- **Go 1.23 range-over-func.** `for x := range f` calls `f` with a synthesized `yield`. "If the loop body terminates … yield returns false and must not be called again" (go.dev/ref/spec#For_range; go.dev/doc/go1.23). Go's `select` is a statement (spec #Select_statements), while `context.WithTimeout` is a function (pkg.go.dev/context#WithTimeout).
  - *Failure:* the protocol rule is enforced at run time, not by types. Separately, `context` must be threaded through every call ("it spreads", Štrba 2017).
- **Kotlin dispatchers.** `withTimeout` (`kotlinx-coroutines-core/common/src/Timeout.kt:23`, `:137`) schedules through `context.delay` (`:230`). The context supplies `Delay` (`Delay.kt:149`), and `CoroutineDispatcher.dispatch` is abstract (`CoroutineDispatcher.kt:60`). The test dispatcher implements `Delay` on virtual time (`kotlinx-coroutines-test/common/src/TestDispatcher.kt:17-40`; "Delay skipping is achieved by using virtual time", `TestBuilders.kt:88`; commit `bd2e9a1b`). Time and dispatch are a provider protocol; scope and timeout are library.
  - *Failure:* "Values returned from [withTimeout] will typically be lost if the caller is cancelled" (`Timeout.kt:128-131`). Cancellation semantics leak through the library.
- **Koka and OCaml 5 effect handlers.**
  - Koka's `async` is four library effects (`lib/std/async/async.kk:70-75`), and timeout is `firstof { wait(secs); Nothing } { Just(action()) }` (`interleave.kk:60-61`).
  - Eio's core performs three effects, `Fork` (`lib_eio/core/fiber.ml:1`), `Suspend` (`suspend.ml:2`) and `Get_context` (`cancel.ml:32`). Three schedulers handle them: Linux (`lib_eio_linux/sched.ml:327-351`), POSIX (`lib_eio_posix/sched.ml:364-370`) and a mock (`lib_eio/mock/backend.ml:72-98`; commit `7fe3513`). **This is the closest match to option P: a small protocol, several providers, one of them a test provider.**
  - *Failure:* the OCaml 5 scheduler split (Eio, Miou, others). Picos exists to bridge it, and Miou's README says its maintainers no longer build bridges.
- **Scala for-comprehensions** (2.13 spec §6.19) are a purely syntactic translation to `flatMap`/`map`/`withFilter`, with no laws.
- **Haskell `RebindableSyntax`** (GHC User's Guide §6.2.10): "Do notation is translated using whatever functions (>>=), (>>), and fail, are in scope … Be warned: this is an experimental facility, with fewer checks than usual." This is unrestricted rebinding, the purest form of meaning drift.
- **cats-effect type classes** are the counterexample: one protocol (`GenSpawn.start` `:191`, `racePair` `:354`, `race` derived `:401`; `GenTemporal.timeoutTo` `:99-107`; `Resource.scala:153`, commit `de84c01f`) with executable laws:
  - `GenSpawnLaws` `raceDerivesFromRacePairLeft` `:28`, `fiberJoinIsGuaranteeCase` `:133`, `uncancelableRaceNotInherited` `:146`;
  - `GenTemporalLaws` `sleepRaceMinimum` `:32`, `startSleepMaximum` `:35`;
  - `MonadCancelLaws` `onCancelAssociatesOverUncancelableBoundary` `:61`.

  Many runtimes implement the same classes and run the same discipline suites.
  - *Failure:* ZIO 2 implements them only through the `interop-cats` bridge. That is an ecosystem split again, though a smaller one.
- **ZIO 2** (commit `716eaee`, `ZIO.scala`):
  - `race` (`:1322`) goes through `raceWith` (`:1491`) to `raceFibersWith` (`:1435`).
  - `timeout` (`:2180`) is `timeoutTo`, which races `sleep(d).interruptible` (`:5666-5676`).
  - `ZIO.scoped` (`:5733-5735`) and `provideLayer` (`:1280-1283`) are library.
  - *Failure:* timeout waits for the loser by default, and the docs send users to `.disconnect` (`:2170-2178`, `:482-493`), so timeout's meaning depends on a hidden policy. Effra fixes the policy in law K2.
- **Elixir and Gleam** take the timeout as an argument: `Task.await(task, timeout \\ 5000)` (`lib/elixir/lib/task.ex:875`), implemented with `receive … after` (`:884-891`), and Gleam's `process.receive(from, within)` (gleam_erlang v1.3.0).
  - *Failure:* Elixir's implicit 5000 ms default exits the caller (`task.ex:777-778`).
- **Elm and Foldkit (UI runtimes).**
  - Elm programs "send Cmd and Sub values to the runtime system" (guide.elm-lang.org/effects). This is a non-Effect runtime driven by its own loop.
  - Foldkit (commit `d21db42`) models a command as data (`packages/foldkit/src/command/index.ts:12-23`), coalesces renders per `requestAnimationFrame` (`runtime/renderer.ts:708-724`), and swaps Effect's scheduler service for a browser one (`runtime/browserScheduler.ts:22-30`). It is the frame-provider shape, though built on Effect.

**Effra's position.** Take the shape of Eio and cats-effect, and avoid the failures of F#, Rust and Haskell:
- **Closed operations.** Six kernel operations (fork, join, interrupt, race, scoped, acquireRelease) plus `Clock.sleep`. A provider cannot add or rename one.
- **Fixed types.** The bundled module declares the signatures. A provider implements them; it does not supply them.
- **Laws as admission.** K1–K15 and R1–R15 run as the provider suite (#124 RS1). A provider that fails a law is refused, as an incompatible machine provider is (`docs/specs/machine-provider-contract.md:45`).
- **Everything a runtime needs is inside the protocol** (spawn, first-completion, time, scope). This is what avoids Rust's runtime split.

## 4. Cross-cutting conclusions

1. Every scoped deadline in the survey is library over a cancel scope or a first-completion race. The only construct-level timeouts (Erlang `receive … after`, Go `select` with a timer channel) exist because the construct also binds a message or a channel.
2. A fallback arm attached to the deadline reads best (Trio's flag reads worst). In P that arm is the argument of `timeoutOr`, or a `catch<Timeout>`.
3. `Timeout` is a typed outcome, not a cancellation subtype (Kotlin's `TimeoutCancellationException` is the confusing counterexample).
4. No surveyed system with lexical provision checks service presence statically. Effra's rows do, whichever surface is chosen.
5. Systems that give syntax a pluggable meaning drift unless the meaning is fixed by types and laws. cats-effect's law files are the strongest existing answer.
