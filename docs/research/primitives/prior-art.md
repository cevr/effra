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
- All of these are library functions taking a trailing lambda. `launch` and `async` are extensions on the `CoroutineScope` receiver, so a spawn needs an owner in scope. The compiler supplies the CPS transform and the state machines for `suspend` functions.
- *Right:* structured scopes as ordinary functions, and an owner required to spawn.
- *Don't copy:* constructs and library are indistinguishable (a trailing lambda looks like a block). Fail-fast interacts badly with fork + join + catch: catching a child's failure at its join is too late, because the scope has already cancelled the siblings.

**Swift.**
- `async let` (SE-0304/0317, 0317 lines 295, 313) binds a child task to the enclosing scope implicitly, and the task is awaited or cancelled at scope exit.
- `withDeadline` (SE-0526, status "Accepted with modifications", `0526-deadline.md:5`) takes the minimum of nested deadlines (lines 167–172) and makes deadlines lexically visible (842–857). It "cancels the operation when the deadline expires, but waits for the operation to return", so it may run past the deadline (127–131). That is H's restated K7: the deadline bounds the cancellation request, not publication.
- SE-0311 task-locals bind for the rest of a scope (1172–1188).
- *Right:* cleanup is awaited before the deadline is published. `async let` is the construct precedent for H's owned fork: a guardrail that bounds a child to its block.
- *Don't copy:* an `async let` child that is never awaited is cancelled implicitly at scope exit with no visible statement. In H the owner block is written out.

**Trio.**
- Nurseries and cancel scopes (`open_nursery`, `move_on_after`, `fail_after`) are library context managers.
- Spawning needs a nursery value: `nursery.start_soon(f)`. No ownerless spawn exists.
- The effective deadline is the minimum of the enclosing ones, except across a shielded scope (`shield=True`), which ignores outer cancellation.
- After a `move_on_after` block, a flag says whether the block was cancelled.
- *Right:* a cancel scope is a value with a protocol (`with`), deadlines nest by minimum, and a spawn requires an owner.
- *Don't copy:* the "was it cancelled?" flag read after the block, instead of a typed result.

**Java Loom** (JDK 26, JEP 525, sixth preview). `fork` is a method of `StructuredTaskScope`, so a subtask needs a scope value. Structure violations (forking from a thread outside the scope, closing out of order) are detected at run time with `StructureViolationException`, not by the compiler. The group policy is a `Joiner` (for example `allSuccessfulOrThrow` and `awaitAll`), reached after a long series of preview changes.
- *Right:* an owner is required to spawn.
- *Lesson:* group failure policy is the hard part. H proposes one, the fail-fast `all { }` block (primitives.md §6.6), and asks the owner (O3).

**OCaml Eio** (`7fe3513d`).
- `Switch.run` owns fibers and resources; `on_release` is LIFO and protected (`eio__core.mli:81-95`).
- `Fiber.fork : sw:Switch.t -> (unit -> unit) -> unit` (`eio__core.mli:258`): a fork needs a switch.
- `Fiber.first` awaits the loser (`fiber.ml:147`).
- `with_timeout` is `Fiber.first` against a sleep (`time.ml:64-65`).
- *Right:* the derivation H's library follows (timeout is library over first completion), and an owner required to spawn.

**Erlang and Elixir.**
- `receive … after` (`expressions.md:728-748`) is the only construct-level timeout in the survey, and it exists because `receive` binds a mailbox pattern.
- `Task.await(task, timeout)` takes a timeout argument, and its default timeout exits the caller (`task.ex:776-781`). `Task.yield || Task.shutdown` is the cooperative alternative (`:1092-1118`).
- `with … else` flattens matching (`special_forms.ex:1690`).
- *Don't copy:* a timeout that kills the caller by default.
- *Don't copy:* constructs and library that look the same. `if`, `unless` and `|>` are macros in `Kernel`, while `with`, `for` and `case` are special forms. A reader cannot tell which by the syntax, which is what Effra's visual rule forbids.

**Gleam.**
- `process.receive(subject, within)` takes a timeout argument.
- `use` (`ast.rs:4480-4510`) passes the rest of the block as a callback. It replaced `try`. The v0.25 release post that introduced it acknowledges the added complexity for newcomers (gleam.run/news/v0.25-introducing-use-expressions/), and the language tour warns against overusing it (tour.gleam.run/advanced-features/use/).
- Lowering: Gleam compiles self-tail recursion to a JavaScript loop (`compiler-core/src/javascript/expression.rs:1877`), so the host language gets no new construct.
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
  - *Right:* laws as the contract. This is the model for K1–K17.
- **Koka** (`9c55695d`).
  - `with` desugars to passing the rest of the block (`tour.kk.md:205-245`, minimum generality `269-271`).
  - Timeout is `firstof` (`interleave.kk:57-61`), and `finally` is library (`hnd.kk:485`).
  - *Right:* handlers give a fixed syntax a pluggable meaning.
- **Effekt.** Blocks are second-class (they cannot escape), the same idea as #106. Capability presence is checked statically, like Koka's handlers.
- **Unison.** The pretty-printed libraries warn that `bracket` can leak (`pretty-print-libraries.output.md:11403-11414`).
  - *Lesson:* resource safety needs a lifetime discipline, not just a bracket function. That is the owner rule (EF123), which H keeps in its owner blocks.
- **Roc.** Purity is inferred, and there is no effect polymorphism (`functions.md:121`).
  - *Don't copy:* Effra needs row polymorphism (F3b) for library wrappers.
- **MoonBit async** (`e72901a1`).
  - `with_task_group` is the structured scope; `with_timeout` and `with_timeout_opt` are built on the task group (`async.mbt:96-108`); retry takes an enum policy.
  - `defer` and `errdefer` are keywords (`fundamentals.md:1177-1235`).
  - A child error cancels its siblings (`task_group.mbt:27-34`).
  - *Right:* time as library over the scope.
  - Its compiler paths (async elimination, contification, dead-code elimination, monomorphisation) target Wasm GC, so they are not evidence for a JavaScript lowering.
- **Elm** (`Cmd`/`Sub`). Effects are data that a runtime interprets.
  - *Right:* a non-Effect runtime driven by its own loop (the model for a UI frame provider).
- **React context.** The default value of a Provider is an anti-pattern: a missing provider silently uses the default.
  - *Lesson:* static presence checking (Effra rows) is the point.
- **ReScript.** It emits native `async`/`await` (`compiler/core/js_dump.ml:392`, `:1042`), with its own transforms around them. A construct that has a host equivalent lowers to it directly.

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
  - Eio's core performs three effects, `Fork` (`lib_eio/core/fiber.ml:1`), `Suspend` (`suspend.ml:2`) and `Get_context` (`cancel.ml:32`). Three schedulers handle them: Linux (`lib_eio_linux/sched.ml:327-351`), POSIX (`lib_eio_posix/sched.ml:364-370`) and a mock (`lib_eio/mock/backend.ml:72-98`; commit `7fe3513`). **This is the closest match to H's protocol: a small set of operations, several providers, one of them a test provider.**
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
- **Closed operations.** Host primitives P1–P12 plus P13 race (and P14 all, if admitted). A provider cannot add or rename one.
- **Fixed types.** The checker types the constructs. A provider implements them; it does not supply their types.
- **A conformance suite, not an admission proof.** K1–K17, A1–A4 and R1–R15 run as the provider suite (#124 RS1). Passing a finite suite is evidence, not proof, so the checker cannot refuse a provider for failing it. An incompatible machine provider is different: that is a static diagnostic (`docs/specs/machine-provider-contract.md:45`).
- **Everything a runtime needs is inside the protocol** (spawn, first completion, time, owner close). This is what avoids Rust's runtime split.

**Resilience pipelines.** Polly v8's `ResiliencePipeline` (pollydocs.org, "Resilience pipelines") and resilience4j's decorators (resilience4j.readme.io, "Getting started") compose Timeout and Retry as values, and the order of composition decides whether the timeout bounds each attempt or the whole loop. That supports the owner's "time policy" framing while keeping preemption and recurrence separate (primitives.md §5, K15).

## 4. Cross-cutting conclusions

1. Every scoped deadline in the survey is library over a cancel scope or a first-completion race. The only construct-level timeouts (Erlang `receive … after`, Go `select` with a timer channel) exist because the construct also binds a message or a channel.
2. A fallback arm attached to the deadline reads best (Trio's flag reads worst). In H that arm is the argument of `timeoutOr`, the `after` arm of a race block, or a `catch<Timeout>`.
3. `Timeout` is a typed outcome, not a cancellation subtype (Kotlin's `TimeoutCancellationException` is the confusing counterexample).
4. Koka handlers and Effekt capabilities check presence statically; React context, Go `context.Value` and Kotlin `CoroutineContext` do not. Effra's rows do, whichever surface is chosen.
5. Systems that give syntax a pluggable meaning drift unless the meaning is fixed by types and laws. cats-effect's law files are the strongest existing answer.
6. Every structured-concurrency library that guards lifetime makes a spawn require an owner value or type: Trio's nursery, Loom's scope, Eio's switch, Kotlin's `CoroutineScope` receiver. Swift's `async let` is the construct form. Effra's owned `fork` is the construct form too, because an owner value needs #106's second-class values (primitives.md §6.2, O4).
7. Systems that guard lifetimes bound a task by the owner it was handed, not by the function that spawned it. Effra's owned fork is stricter. The longer lifetimes come back only through separate units: provision-lifetime workers (U-workers, §5) and owner parameters (O4).

## 5. Child lifetimes, resource handoff and provision workers (rounds 3 and 4)

Review 2 (M2, M4, m1) asked three questions: what a child owns, how a worker can live as long as a provision, and how a language switches rules without inferring them from content. Pins are given only where a checkout was read for this round; other entries name the documentation section.

**What a finished child owns.**
- **Trio.** A task's resources are its own: `async with` blocks inside the task close when the task returns, before the nursery sees it finish. A nursery bounds a task by the nursery object passed to it, so a helper given a nursery can start a task that outlives the helper (Trio docs, "Tasks let you do multiple things at once", nursery semantics).
- **Kotlin.** `Job.join()` waits until the coroutine has completed, which includes its `finally` blocks and its children (kotlinx.coroutines docs, `Job.join`, "Cancellation and timeouts"). Resources acquired in the coroutine close before `join` returns.
- **Swift.** A `TaskGroup` child's resources follow its body. `withTaskGroup` awaits every child before it returns, and a value cannot leave a child except as its result (Swift Evolution SE-0304, "Structured concurrency").
- **Eio.** Resources attach to the switch they are created with. A fiber that opens a file on its own `Switch.run` releases it when that switch finishes; a resource attached to a switch passed in lives as long as that switch (Eio README, "Switches").
- **Elixir.** A process owns its ETS tables and ports, and they are released when the process exits (Erlang `ets` docs, table ownership; "Ports and port drivers"). Links and monitors report the exit to others but transfer nothing, unless the owner explicitly hands a table over with `:ets.give_away/3`.
- **Effect** binds a child's lifetime to a scope other than its parent's, but transfers no resources (corrected in round 4, review 3 Minor 3). The pinned `forkIn(scope)` (`packages/effect/src/internal/effect.ts:5594-5637`) adds an interrupt finalizer for the child to the target scope at spawn and removes it when the child completes; `forkScoped` (`:5639`) is `forkIn` on the scope in context. Effect 4 has `Scope.provide` (`Scope.ts:309`) and no `Scope.extend`. Where a child's resources go is decided by context instead: its `acquireRelease` registers the release in whichever `Scope` its context supplies, so running a child under a provided outer scope leaves its resources there. That shared provision of a scope is Effect's form of the handoff primitives.md §6.3 rejects. It needs the target scope as a value in the child's context (O4), and a release path in that scope for a child that is interrupted.

Effra chooses the majority rule (O11): a child owns what it acquires, an owned result cannot leave it (EF123), and laws K5 and K12 hold only for results without owned values.

**Provision-lifetime workers** (input to U-workers; round 3's layer entry is withdrawn from H).
- **Elixir supervisors.** A supervisor starts its children in the order they are defined and shuts them down in reverse (`lib/elixir/lib/supervisor.ex:458-470`, Elixir `7a40579`); an application shuts down its applications in reverse start order (`application.ex:284`). `GenServer.start_link/3` blocks until `init/1` returns (`gen_server.ex:534-535`). That is a synchronous start handshake: the next child starts only after this one is initialised, so a later child can use an earlier one during its own initialisation.
- **Gleam OTP.** `actor.start` returns once the actor's initialiser has run, the same handshake in typed form (gleam_otp, `gleam/otp/actor` docs). Gleam's lowering is to the BEAM's processes and supervisors, as library; on its JavaScript target there are no processes, so no worker construct exists there.
- **Trio `nursery.start`.** `await nursery.start(fn)` runs `fn` in the nursery and returns once `fn` calls `task_status.started(value)`, so the caller can rely on the task being ready (Trio docs, `Nursery.start`).
- **Effect.** `Layer.effect`/`Layer.effectDiscard` over a scoped effect (`Layer.ts:1012`, `:1104`; v4 has no `Layer.scoped`, which was v3's name) with `Effect.forkScoped` gives a worker the layer's lifetime; a node that needs it initialised waits on it explicitly (effect-study.md, Layer).
- **ReScript** compiles to promises (`js_dump.ml:392`, `:1042`), which have no owner at all; a long-lived worker is a promise that nothing awaits. It is the baseline Effra's construct improves on, not a model.
- **MoonBit** has a task group bounded by its block (`async.mbt:96-108`), the Swift shape; a worker longer than its block needs the group as a value.

Round 3's layer entry `fork w(args)` followed the supervisor's reverse-order rule (the worker starts after every node, stops before any node). Review 3 found what the comparison above leaves out: supervisors and Effect each answer the failure question, and the entry did not. An OTP supervisor restarts a failed child per its strategy (`one_for_one`, `one_for_all`, `rest_for_one`) and escalates when the restart intensity is exceeded; Elixir's `Task.Supervisor.async_nolink` runs a task whose crash reaches the caller only as a monitor message or through `Task.await`/`Task.yield`, while `Task.Supervisor.async` links it so the caller crashes too (Elixir `Task.Supervisor` docs); Effect's `forkScoped` child fails alone and its failure is observed only through its fiber; Trio's nursery, held in context, cancels the whole nursery on the first failure (fail-fast); Kotlin chooses between `coroutineScope` (fail-fast) and `supervisorScope` (isolated failures). Round 3's entry instead retained a worker failure until the body ended and then joined it with the body's failure into a composite cause no row declared (review 3, B1), and a diamond merge started it twice (Major 3). Choosing among these policies, and the handshake (Trio `nursery.start`, Elixir `init/1`, Gleam `actor.start`), is the U-workers unit (primitives.md §6.5, §9).

**Lowering paths.** Gleam (OTP over BEAM processes), Elixir (supervisors as library over links) and Effect (`Layer.effect`/`Layer.effectDiscard` + `forkScoped`; v3: `Layer.scoped`) all implement provision-lifetime workers as library over a process or fiber primitive. Round 3's prototype lowered the worker entry the same way, a fork inside the provision body's scope (`prelude/layers.mjs:231` on JS, `runtime/effra/layers.go:490` on Go), with no new host primitive; that code stays behind a flag for U-workers. What H itself keeps is narrower: a fork written directly in a provide body is owned by the block and lowered as any scope child.

**Switching rules without inference (m1).**
- **Go:** the `go` directive in `go.mod` selects language semantics per module; Go 1.22 changed loop-variable scoping only for modules declaring `go 1.22` or later (Go blog, "Fixing For Loops in Go 1.22").
- **Rust:** the `edition` key in `Cargo.toml` selects the edition per crate; `cargo fix --edition` migrates code and reports what it cannot fix (Rust Edition Guide).
- **Kotlin** (`-language-version`), **Swift** (`-swift-version`, Swift 6 language mode) and **Java** (`--release`) select the language explicitly too.

None infers the rules from what syntax a file happens to use. Effra's prototype gate does, which is why primitives.md §6.2 calls it prototype-only and O13 asks for an explicit per-package edition.
