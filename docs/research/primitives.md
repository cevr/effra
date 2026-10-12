# Primitives and constructs (#134)

Status: research note and decision record for wayfinder ticket #134 (run wf10, unit spike-syntax), 2026-10-12. Owner decisions are still open where marked **O*n***.

Appendices:
- [primitives/effect-study.md](primitives/effect-study.md): Effect 4 kernel and derived operations, with file:line.
- [primitives/prior-art.md](primitives/prior-art.md): the wider prior art.
- `primitives/corpus/<option>/`: the corpus rewritten per option.
- `primitives/blind-reader/<option>/`: blind-reader packets; the answer key is [primitives/blind-reader-key.md](primitives/blind-reader-key.md).
- `benchmarks/syntax/measure.ts`: the corpus metrics.
- The prototype lives on branch `wf10/spike-syntax-proto` at `073de983`, under `docs/research/primitives/prototype/`. That branch is never merged.

## 0. Decision in one page

**Chosen: option P, a small kernel plus library.** The constructs are the forms whose meaning is evaluation, control or a declaration's static plan: `effect fn`, `run`, `fail`, and the declarations `service`, `impl`, `layer`, plus machines (outside this ticket). Duration literals such as `500ms` and `2s` are notation. Everything else is an ordinary method in a bundled module:

- Kernel methods: `.fork()`, `.join()`, `.interrupt()`, `.race(other)`, `.scoped()` and `acquireRelease`. These are the lawful runtime contract's P4–P8, plus one new primitive, P13 `race`. Their types are honest in the general type system, and laws pin their behaviour.
- Library over the kernel: the timeout family, `retry` with a `Schedule` value, `.provide(App)`, `.provide<S>(p)`, `.catch`, `.recover` and `.orFail`.

Under P, every dot form is library. No checker branch is keyed on a name.

```effra
effect fn dashboard() -> string uses { Account, Invoice, Clock } {
    let account = run Account.label().fork()
    let invoice = run Invoice.label().timeoutOr(500ms, "invoice: slow")
    let name = run account.join()
    name + " / " + invoice
}

effect fn main() -> string {
    run dashboard().scoped().timeoutOr(2s, "gave up").provide(App)
        .provide<Clock>(LiveClock)
}
```

That program parses, checks and runs on Go and JavaScript on the prototype branch, and prints the same as today's version (§8.3).

**Why P.** The owner's bar (§1) asks each construct to name an illegal state that it makes unrepresentable and that no library function or general type feature could rule out.

- Every candidate guarantee for scope, fork, race, timeout and provide comes from **one** mechanism: owners. An owner closes what it owns, and nothing it owns escapes. Today's checker already treats four owner kinds (child, timeout, scope, provision) as one rule (`semantic.go:160-161`).
- One mechanism with five or more clients is a general type feature: the closed-at-the-edge recipe-parameter contract (#128, rows note Q11). It is not a set of keywords.
- The only guarantee that needs a declaration-level construct is the static layer plan. It already lives on the `layer` declaration, which stays a construct.

The blocks lose (§7) for three reasons:

- They add three to five keywords.
- They cannot wrap a recipe without a helper function, which they needed in four places.
- Under the owner's 2026-10-12 rule they cannot use `else`, so each needs a new word.

**Portability.** The kernel is a protocol: six operations plus laws (§6). The Effect runtime and the Go runtime are two providers. A deterministic test runtime, a UI frame runtime, a request-scoped server runtime and an embedded tick loop are further lawful providers that need no source change. Today only `Clock` and `Scheduler` are pluggable, so making the whole kernel pluggable is follow-up U11.

**What blocks P today.** Writing the kernel signatures and the library in `.ef` needs receiver methods (#127), whole-value type parameters (#128 Q9), row difference (F3b, #126) and the closed-at-the-edge contract (#128 Q11). The prototype receipt `walls.ef` reproduces each wall: EF127, EF125 and EF123. Until those land, the surface can change, but the checker still keys owners on the parse kinds (**O4**).

## 1. The question and the bar

#134 asks which concurrency, lifetime, time and provision operations Effra builds in, and in what syntax. Three owner directions set the bar.

1. Standing (memory, 2026-10-09): constructs look distinct (keyword or block forms), and any dot or pipe form is ordinary library. The ladder runs: library → library plus a general type feature → 1:1 notation → construct. A construct is justified only when its meaning is evaluation, lifetime or control, or a single-client type feature, or a structure that tooling must read. General type features (TS-like) are not special-casing and are preferred.
2. 2026-10-12 (a): "else is overloading if else … scope and all these things are actually just normal methods in typescript effect. Time out, I look at it as just part of a schedule … why they need to be Language constructs." So `else` and other reused keywords are dropped from every option, the default hypothesis flips to primitive plus library, and the owner's schedule idea gets an evaluation (§5).
3. 2026-10-12 (b): "enough guardrails so that bad code or illegal states cannot be represented … layers … portable across different domains … useland implementations." Two tests follow:
   - **Guardrail.** Name the illegal state or static check, and show that no library function or general type feature can provide it.
   - **Portability.** The construct's meaning is a protocol: operations plus laws (#125) that any lawful provider, including userland, can implement.

   Blocks and primitives may coexist. P wins only where the guardrail test fails.

## 2. Effect-TS, in short

The full study with file:line is in [effect-study.md](primitives/effect-study.md). Facts that decide this ticket:

- **Effect's kernel is small.** About fifteen interpreter nodes (`internal/core.ts:435,467`; `internal/effect.ts:622`) plus three fiber-runtime methods: fork (`internal/effect.ts:5517-5539`), interrupt (`:4547`) and context update (`:2175-2200`).
- **Timeout is derived:** `timeout → timeoutOrElse → timeoutOption → raceFirst(asSome(self), as(sleep(d), none))` (`internal/effect.ts:3875-3903`). Race losers are interrupted and awaited (`:1620`, `:1673`, `:930`; tests `Effect.test.ts:1347,1366,1385`).
- **Scope, acquireRelease and provide are library.** `Effect.scoped` is a root scope that removes `Scope` from R (`:4100-4110`). `acquireRelease` adds `Scope` (`:4134-4150`). `Effect.provide(layer)` is `scopedWith` + `buildWithScope`, with a fresh MemoMap per call (`internal/layer.ts:8-22`; `Layer.ts:769-775`).
- **Schedule is an inert value** (`Schedule.ts:53`). `retry` runs an attempt to completion and only then consults the schedule (`internal/schedule.ts:66-79`).
- **What hurts:** there are no static lifetimes, so handles and resources can escape their scope. Two provides build a layer twice, guarded only by a lint (`Effect.test.ts:4653`). The catch zoo churned (v4 removed `catchAll`, `orElse`, `either`, `timeoutFail`, `timeoutTo`, `forkDaemon`). Effect documents no laws.

Effra already matches the kernel shape: the lawful runtime contract lists host primitives P1–P12 and states that "Timeout, race, latch composition and layers are derived library behavior. They must not become extra primitive syntax" (`docs/research/lawful-runtime-contract.md:63`). Today's language contradicts that sentence: `.timeout` and `.provide(Layer)` are parser and checker constructs (`syntax.go:1640-1675`; `semantic.go:4979`, `:5047`).

## 3. Prior art, in short

The detail and pins are in [prior-art.md](primitives/prior-art.md).

**Mainstream systems ship these as functions over a small runtime kernel:**
- Effect, ZIO and cats-effect put timeout, race and Resource/Scope in libraries; cats-effect derives `timeoutTo` from `racePair`.
- Kotlin's `withTimeout` and `coroutineScope` are library functions.
- Trio's cancel scopes and nurseries are library context managers.
- Go's `context.WithTimeout` is a function, though `select` is a statement.
- Elixir's `Task.await` and Gleam's `process.receive` take a timeout argument.
- Eio derives `with_timeout` from `Fiber.first` (`time.ml:64-65`), Koka derives timeout from `firstof` (`interleave.kk:57-61`), and MoonBit derives `with_timeout` from a task group (`async.mbt:96-108`).

**The constructs that do exist give a fixed syntax a pluggable meaning:**
- F# computation expressions (a builder supplies `Bind` and `Using`).
- C#'s awaitable pattern.
- Rust `async`/`await` with pluggable executors (tokio, embassy).
- Python's `with` protocol.
- Go 1.23 range-over-func.
- Kotlin dispatchers.
- Koka and OCaml 5 handlers (Eio is one scheduler among several).
- Scala's for-comprehension desugaring.

Their known failure is meaning drift: F# builders and Haskell `RebindableSyntax` let the same syntax mean anything. Effra's answer is a closed operation set with fixed types plus a law suite that a provider must pass (§6.3).

**No lexical-provide system checks service presence statically** (React context, Go `context.Value`, Kotlin `CoroutineContext`). Effra's rows already do, whether provide is a construct or a library call.

## 4. The bar, construct by construct

For each candidate:
- (a) the static guarantee the compiler gives today, or the illegal state a construct would rule out;
- (b) whether a general type feature or a runtime law can carry it;
- (c) whether the loss matters to users;
- the verdict.

| Candidate | (a) Guarantee today | (b) General carrier | (c) Loss matters? | Verdict |
|---|---|---|---|---|
| `run` | Evaluation is explicit. Constructing a recipe runs nothing (R11), and a discarded effect is diagnosed. | None. `run` *is* the bind (Effect `yield*`, F# `let!`, Rust `.await`); a library function cannot sequence its caller. | Yes | **Construct.** It lowers to the protocol's sequencing, so it is portable to any provider. |
| `fail` | Exit control with a typed label in `raises`. | `run fail(E)` would work, but `fail` reads like `return` and `throw`. | Minor | **Construct** (control) |
| `effect fn` | Declares a recipe-producing function, with explicit rows. | none | Yes | **Construct** (declaration) |
| `scope { }` | An owned value cannot escape the closing scope (EF123). The scope raises unobserved children's failures at close (rows) and releases in LIFO order (R1, R2). | The closed-at-the-edge parameter contract (#128 Q11) on `scoped`'s recipe parameter, the same rule the checker applies to all four owner kinds (`semantic.go:160-161`). Laws R1–R3 cover behaviour. | Yes: resource and handle safety | **Library `.scoped()` + Q11.** The guarantee is real but not scope-specific; it is the owner mechanism, which has at least five clients (scoped, race, timeout, provide(Layer), acquireUseRelease, user wrappers). |
| `fork` | The handle is owned by the current owner and cannot escape it. Child failures are live, then pending, in rows. A forgotten join is not illegal: it surfaces at owner close. | A declared ownership contract in inferred vocabulary (#130) on the kernel signature: "the result is owned by the caller's owner". Law R9. | Yes | **Kernel method `.fork()`** (P6) |
| `.join()`, `.interrupt()` | An observed child does not re-raise at close (R4). A timed-out join observes nothing (`semantic.go:4994-4999`). | A declared contract with an observation clause (#130). Laws R4 and R5. | Yes | **Kernel methods** (P7, P8) |
| race (none today; hidden inside `.timeout`) | none | The prototype race rule joins two closed owners (§8.3), the same owner rule again. Laws K1–K5. | | **Kernel method `.race(other)`** (new P13) |
| `.timeout(d)` | It adds `Timeout`, charges `Scheduler` (1-G), and runs the work under a timeout owner. | Derivable: `work.race(failAfter(d))`. Every guarantee comes from race (the owner rule) and from Clock (rows). Law K6. | No | **Library.** It re-tests the rows note's NO-GO; see below. |
| `.provide(Layer)` | Missing services caught at build time. The static layer plan is read at the site by graph, lint and inspection (`graph.go:474`, `lint_facts.go:309`). A provision owner (R12). | Missing services are caught by rows through F3b: `uses { R - Out, In }`. The plan moves to the `layer` declaration, whose type is `Layer<Out, E, In>`. The owner comes from Q11. Laws R12, R13. | Partly: a layer chosen at run time makes the graph show a set of plans | **Library over a typed `Layer` value.** The `layer` **declaration** stays a construct and holds the plan. |
| `.provide<S>(p)` | The service leaves the row. | F3b | No | **Library** (#132, already decided) |
| `.catch`, `.recover` | The label leaves the row. | F2, F3b | No | **Library** (#131, #133) |
| `.orFail()` | GoResult to typed failure. | ordinary data once GoResult is | No | **Library** (rows note) |
| `acquireRelease` | Release runs exactly once, masked (R10, P5). | A kernel operation with a `Scope` row | Yes | **Kernel function** (P5) |
| `all`, `both`, `forEach` | n/a | Not writable as library today: the checker refuses the composite cause (probe receipt: EF107 at `both.ef:14:11`). It needs a group failure policy (**O3**). | | **Library intrinsic** with a declared contract, until O3 |
| `defer e` (Go-like) | n/a | `acquireRelease` and `onExit` cover it | | Not proposed |
| duration literal `2s` | n/a | 1:1 notation for `Duration.ms(2000)`; a second client is byte sizes | | **Notation** (rung 3) |
| `layer` declaration | The static plan: construction graph, `merge`/`replace`, missing and duplicate services inside the layer. | None: tooling must read the structure. | Yes | **Construct** (unchanged; matches the owner's "layers … earn keep") |

**The illegal states the owner named:**
- **A fiber handle used after its owner ends; a resource escaping its lifetime.** Ruled out today by EF123. Under P, the closed-at-the-edge contract (Q11) keeps the rule. The rule is per owner, not per keyword.
- **A forgotten join.** Not ruled out by any option. It surfaces as a pending failure at owner close, and a lint can flag an unjoined handle. A construct would not change this.
- **A missing service caught at build time.** Rows catch it in every option, including library `provide` with an F3b signature.
- **A deadline that silently fails to cover acquisition.** Not an illegal state: both orders are legitimate intents. Chain order says which one is meant. The prototype checks both `run S.timeoutOr(2s, …).provide(App)` (acquisition outside) and `run S.provide(App).timeoutOr(2s, …)` (acquisition inside) (`walls.ef`). Option A's clause order expresses the same thing, so a construct does not remove the choice. It only moves it into header order.

**The rows note's NO-GO claims, re-tested:**
- **timeout**, claimed NO-GO because "first-completion race and abandonment of the losing work are control". The premise is true, and the control lives in race. Once race is a kernel operation, timeout is library. Effect (`internal/effect.ts:3875-3903`), Eio (`time.ml:64-65`), Koka (`interleave.kk:57-61`), cats-effect (`IO.scala:850-859`) and MoonBit (`async.mbt:96-108`) all derive it. The prototype derives it as well (`race-library.ef`, §8.3). **Overturned.**
- **provide(Layer)**, claimed NO-GO because "tooling reads the static layer plan". The plan is a property of the `layer` declaration, not of the provide site. With `Layer<Out, E, In>` as the declaration's type, graph, lint and inspection read the plan from the argument's declaration. When the argument is not statically one declaration (a parameter, or an `if` between two layers), tooling shows the set of plans the type admits. **Overturned conditionally** on the typed `Layer` value and F3b (§9, U7).

## 5. Timeout and Schedule

The owner suggested treating a deadline as part of a schedule. Concretely: a time-policy value would compose with `Schedule` (`upTo`, `during`, recurrence limits), and `work.retry(policy)` would carry both the retry and the deadline.

**What breaks.** A schedule acts between attempts. Effect's `retry` runs an attempt to completion, then asks the schedule whether and when to run the next one (`internal/schedule.ts:66-79`); `Schedule` is inert data (`Schedule.ts:53`). `Schedule.upTo(2s)` therefore bounds when the *next* attempt may start. It cannot stop an attempt that hangs. Effect's own documentation says so: "Long-running effects can cause the duration limit to be detected on the following schedule step" (`Schedule.ts:1271-1273`; `during` at `:750` checks elapsed time only when stepped). Stopping a running attempt is preemption: a race against a timer. That is why Effect keeps `timeout` (a race, `internal/effect.ts:3875-3903`) separate from `Schedule` (data). cats-effect, ZIO and Kotlin draw the same line.

**What survives of the idea.** Both are time policy, and they compose by order, as ordinary library:
- `work.timeout(500ms).retry<Timeout>(policy)`: each attempt gets 500 ms, and timed-out attempts are retried.
- `work.retry<E>(policy).timeout(2s)`: the whole retry loop gets 2 s.
- `Schedule` stays a plain enum, with `Spaced`, `Times` and later `Exponential` and `UpTo`. A `Deadline`/`Duration` value is the preemption policy.

Laws K14 and K15 (§6.3) state the difference, so a reader and a provider cannot confuse the two.

## 6. Option P design

### 6.1 Kernel

The kernel is the lawful runtime contract's host primitives, unchanged, plus one:

| Kernel | Spelling | Contract row | Signature (needs #127, #128, F3b, #130) |
|---|---|---|---|
| fork | `work.fork()` | P6 | `-> Fiber<A, { E }> uses { R }`; the result is owned by the caller's owner (#130) |
| join | `child.join()` | P7 | `-> A raises { E }`; observes the child |
| interrupt | `child.interrupt()` | P8 | `-> void`; awaits cleanup |
| race | `left.race(right)` | **P13 (new)** | `-> A raises { E, F } uses { R, S }`; each side runs under a closed owner |
| scoped | `work.scoped()` | P4 | `-> A raises { E } uses { R - Scope }`; the parameter is closed at the edge (Q11) |
| acquireRelease | `acquireRelease(acquire, release)` | P5 | `-> A raises { E } uses { R, Scope }` |
| sleep | `Clock.sleep(d)` | P10 | an ordinary service operation |

P13 is new because today's timeout runtime is already a hidden race. Effect does not need race as a primitive: it derives `raceAll` from `withFiber`, the general `callback` primitive and `forkUnsafe` (`internal/effect.ts:1603`, `:1657`). Effra has no general callback or mutable cell in the language, so first-completion is the one operation it must add to the host kernel. Go `select` and cats-effect `racePair` (`GenSpawn.scala:354`, with `race` derived at `:401`) make the same choice. The prototype refactors Go's `TimeoutWithEffect` into a generic `raceWith`, with `Timeout` and `Race` as two clients of it (`runtime/effra/effect.go` on the prototype branch).

The library over the kernel is written out in `corpus/P/library.ef`:
- the timeout family: `timeout`, `timeoutOr`, `timeoutOption`, `timeoutTo`;
- `either`, `delay`, and `retry` over a `Schedule` value;
- `provide(Layer)` and `provide<S>`.

### 6.2 Fork ownership and `Scope` (O2)

Effect's `forkChild` attaches the child to the parent fiber (`internal/effect.ts:599-603`), and only `acquireRelease` charges `Scope`. P follows Effect: `acquireRelease` charges `Scope`, `.scoped()` removes it, and `fork` charges no row. The child belongs to the dynamic owner (R9), and its failures are live or pending in rows exactly as today. A function whose whole body was `scope { … }` becomes an ordinary body, and its callers decide the lifetime with `.scoped()`. The corpus shows the cost:
- 10 scope sites became 8 call-site `.scoped()` calls;
- 2 entry-point unwraps, where the root scope already owns everything;
- 1 extracted helper, for the one nested scope in 26 files (`corpus/P/causal.ef`, `interruptOneWaiter`).

### 6.3 Protocol and portability

The construct-like meaning of P lives in the kernel. That meaning is a protocol: the operations in §6.1, their types, and the laws below. Effra already works this way for machines, where a checked declaration lowers to a provider-independent plan that any userland provider executes (`docs/specs/machine-provider-contract.md:7`, `:45`), and for time (`uses { Scheduler }`, `TestScheduler`).

Providers that pass the same law suite, with source unchanged and only the `provide` differing:

| Provider | fork | race | scoped / close | sleep | Use |
|---|---|---|---|---|---|
| Effect runtime (JS) | `Effect.forkChild` adapter | `Effect.raceFirst` + interrupt + await (prototype `race.mjs`) | `Scope` | `Clock` | today |
| Go runtime | goroutine + `Scope` | `er.Race` (prototype) | `er.Scoped` | `SleepWithDriver` | today |
| Deterministic test runtime | a queued task | decided by virtual time (R14), ties broken by a seed | LIFO close | `TestClock` | today, partly (`TestScheduler`) |
| UI frame runtime (#121, cf. Foldkit and Elm `Cmd`/`Sub`) | a task stepped once per frame | first of an event or a frame-clock deadline | component mount/unmount is the owner; unmount interrupts | frame clock | U11 |
| Request-scoped server runtime | a goroutine bounded by the request | as Go | the request is the root owner; client disconnect interrupts | the deadline from the request budget (Go `context.WithDeadline`) | U11 |
| Embedded or game tick loop (cf. embassy) | a cooperative task | first completion per tick | static owner tree | tick counter | U11 |

**Kernel laws** (handed to #125; generalising R1–R15 of `lawful-runtime-contract.md:76-90`):

- **K1 first completion.** `left.race(right)` publishes the exit of whichever side completes first, success or failure (Effect `raceFirst`, `internal/effect.ts:1736`).
- **K2 loser cleanup precedes publication.** The loser is interrupted and its cleanup completes before the race publishes. This generalises R8.
- **K3 abandonment.** The loser's ordinary typed failures are discarded; its defects and cleanup failures are kept (R3, R6).
- **K4 symmetry.** `a.race(b)` and `b.race(a)` agree whenever completion order is determined.
- **K5 identity.** `a.race(never)` behaves as `a`.
- **K6 timeout derivation.** `w.timeout(d)` behaves as `w.race(failAfter(d))`; likewise `timeoutOr` and `timeoutOption`.
- **K7 nested deadlines.** `w.timeout(d1).timeout(d2)` publishes no later than `min(d1, d2)` (one clock, R14).
- **K8 scope nesting.** `w.scoped().scoped()` behaves as `w.scoped()`. Owners nest associatively.
- **K9 provide/catch commute.** `w.provide(L).catch<E>(v)` behaves as `w.catch<E>(v).provide(L)` when building L cannot raise E.
- **K10 shadowing.** The inner provider wins: `w.provide<S>(p).provide<S>(q)` uses `p`.
- **K11 lossless wrapping.** For `effect fn wrap(x: Effect<…>) { run x.op(args) }`, `run wrap(r)` behaves as `run r.op(args)`, with the same rows and the same ownership evidence. This needs Q11.
- **K12 fork/join.** `run w.fork().join()` behaves as `run w`.
- **K13 interrupt after completion** is a no-op that observes the child.
- **K14 schedules do not preempt.** `retry` consults the schedule only after an attempt has completed.
- **K15 placement.** `w.timeout(d).retry(s)` bounds each attempt; `w.retry(s).timeout(d)` bounds the whole loop.

**Drift control.** F# builders and `RebindableSyntax` let a library change what syntax means. P fixes the meaning in three ways:
- The operation set is closed: six kernel operations.
- Their types are fixed by the bundled module.
- The law suite (#124, RS1) is the admission test. A provider that fails a law is refused, the same way an incompatible machine provider is a checked diagnostic (`machine-provider-contract.md:45`).

Providers choose an implementation. They cannot choose a meaning.

### 6.4 Typing

- **No name-keyed branch.** Each kernel and library operation is an ordinary declaration in a bundled module. Its rows are expressed with F3b (`{ R - Scope }`, `{ R - Out, In }`) and recipe parameters (#128 4a, in-place half shown). Its ownership comes from the closed-at-the-edge half (Q11) and declared contracts in inferred vocabulary (#130).
- **TypeScript projection is fail-safe.** `{ R - S }` projects to `Exclude<R, S>`, which is Effect's own type for `Effect.scoped`. A row the projector cannot express projects to an opaque nominal type, never `any`.
- **Go lowering.** Rows erase. Kernel operations become runtime calls (`er.Fork`, `er.Race`, `er.Scoped`), with the same R1–R15 laws on both targets.

### 6.5 Composability

**Lossless wrapping.** A wrapper is an ordinary `effect fn` over a recipe parameter, so wrappers nest freely. Receipts:
- `probes/p1.ef` (a monomorphic `timeoutOr` over today's `.timeout`) runs on Go and JS.
- `race-library.ef`'s `deadlineOr` nests inside itself and inside the kernel, on both targets.

The generic version is blocked only by #128 Q9 (EF127), F3b (EF125) and Q11 (EF123) (`walls.ef`).

**First-class recipes.** `retry`, `either`, the timeout family, `.provide` and `.scoped` are values you can pass, store and return, because they are functions over recipes. A block (options A, B, C, E) yields a value, not a recipe: wrapping one needs a helper function. The corpus needed 2–3 such helpers per block option and none in P or D.

**Orthogonality.** Each kernel operation does one thing:
- fork: start;
- join: observe;
- interrupt: cancel;
- race: first completion;
- scoped: lifetime;
- acquireRelease: finalise.

Provide (context), time (Clock), recurrence (Schedule) and recovery (catch) do not overlap. Nothing fuses a deadline with a scope, so Go's `context` problem (cancellation, deadline and values in one bag) does not arise.

**Nesting matrix.** The outer operation is the row, the inner one is the column. Every cell composes; the cell names the law that pins it or the note that applies.

| outer \ inner | scoped | fork | race | timeout | provide(L) | catch | retry |
|---|---|---|---|---|---|---|---|
| **scoped** | K8 | the scope owns the child (R2) | each side's owner nests in the scope | ditto | the layer closes before the scope (R12, R2) | as written | as written |
| **fork** | the child owns its scope | grandchild (R9) | the race runs in the child | ditto | the child has its own provision | the child's failure is caught inside | as written |
| **race** | each side closes (K2) | forks inside a side are owned by it (K2) | flattens (K1) | K7 | per side | per side | per side |
| **timeout** | covers the scope's close (K2) | covers the forked child's lifetime only if it is joined inside | K7 | K7 | covers the build (§4) | K15 | K15 |
| **provide(L)** | the scope sees L | the child inherits L | both sides see L | the build is outside the deadline (§4) | K10 | K9 | as written |
| **catch** | catches the scope's failures | catches the join's failures | catches the winner's failure | catches `Timeout` | K9 | ordered | the last attempt's failure |
| **retry** | re-runs the scope | re-runs the fork | re-runs the race | K15 | rebuilds L per attempt (as Effect does, `Layer.ts:769-775`) | as written | nested policies |

## 7. Losing options

Each option was measured on the same 26-file corpus (§8). `else` is removed everywhere, per the owner.

- **A, clauses on `scope`:** `scope provide App, Clock = LiveClock within 2s { … } otherwise { … }`.
  - It adds 3 keywords (`provide`, `within`, `otherwise`) and keeps the 1-G debt (the deadline charges `Scheduler` by name).
  - Clause order *is* nesting order, which readers must learn. That is the risk blind-reader question 1 tests.
  - It needs 3 helpers for forked or stored recipes.
  - The A agent reported a semantic trap in `ownership.ef`: `let file = scope provide Files = LiveFiles { … }` makes a resource-acquiring call escape a scope owner.
- **B, separate blocks:** `provide … { within 2s { … } otherwise { … } }`.
  - It has the same keyword cost and helpers as A, and one more nesting level.
  - `otherwise` is a new word the owner has not seen.
- **C, a race construct with time as library:** `race { … } or { … }`.
  - Its time handling is P's (library over race). The difference is that C spells race and provide as blocks.
  - `or` is the boolean operator in Python and SQL: a reused word, against the owner's rule.
  - Blocks cannot be wrapped without helpers.
  - It is the best of the block options by tokens (-3.7%).
- **D, `with` block sugar (Koka `with`, Gleam `use`):**
  - It keeps 99 compiler-known call forms (`provide(…)`, `provideService<…>(…)`, `.provide`). That is the lang-item pattern the owner rejected on 2026-10-09.
  - It overloads the existing `derive … with` keyword (`codec-transforms.ef`, `http-endpoints.ef`).
  - "The rest of the block is an argument" is the Gleam `use` criticism (prior-art.md).
- **E, race with an `after` arm:** `race { … } after 2s { … }`.
  - `after` is a new word (Erlang `receive … after` is the precedent).
  - It is a deadline construct that charges `Scheduler` by name (1-G), and it still needs helpers.

## 8. Evidence

### 8.1 Corpus metrics

The 25 examples that use `.timeout`, `.provide` or `.orFail`, plus the worked example, are rewritten per option under `primitives/corpus/<option>/`. Rewrites were done by one agent per option from a shared rules file, with deterministic hoisting. The metrics come from `bun benchmarks/syntax/measure.ts`, which exits non-zero on lex failure, unbalanced braces, a missing file, or any construct lookalike in the chosen option.

| Option | Tokens | vs today | Code lines | Distinct keywords | Punct/line | Construct lookalikes | Dot forms awaiting general features |
|---|---|---|---|---|---|---|---|
| today | 11090 | 0 | 1664 | 28 | 3.31 | 187 | n/a |
| **P** | 11104 | +0.1% | 1646 | **26** | 3.36 | **0** | 198 |
| A | 10772 | -2.9% | 1771 | 31 | 2.93 | 0 | 57 |
| B | 10710 | -3.4% | 1773 | 31 | 2.93 | 0 | 57 |
| C | 10678 | -3.7% | 1746 | 31 | 2.97 | 0 | 64 |
| D | 10922 | -1.5% | 1670 | 28 | 3.21 | 99 | 64 |
| E | 10720 | -3.3% | 1777 | 31 | 2.93 | 0 | 57 |

Reading the table:
- **P has the fewest keywords.** `scope` and `fork` retire, and nothing is added.
- **P has zero lookalikes.** Every dot form is library.
- **P has the fewest lines.** It costs about the same tokens as today. Its punctuation per line is the highest because chains carry more dots and parentheses on one line.
- **The block options save 3–4% of tokens, almost entirely from hoisting.** `recipes-queue`, `recipes-retry` and `recovery-outcome` repeat `.provide<Console>(Stdout)` on each statement today. P keeps that repetition only because the rewrite was mechanical; idiomatic P provides once at the entry point, as `concurrency.ef` does.
- **Max nesting depth** (9 in every option, set by `http-transport.ef`) does not discriminate. In the files that change, P's depth is lowest or tied lowest: `causal`, `concurrency`, `lifecycle` and `ownership` drop to 1. The block options raise `dashboard` to 3, and A, B and E raise `concurrency` to 3 as well.
- **The last column counts dot forms that are ordinary library only after #127, #128, F3b, #131 and #133 land.** It is the dependency, made visible. P's count is the largest because P moves the most onto general features.

### 8.2 Blind readers

Packets are in `primitives/blind-reader/<option>/` for today, P and A–E. Each holds the three programs with comments removed, plus the option's `library.ef` for P, C and D, and the same 12 questions. The answer key is separate (`blind-reader-key.md`). The orchestrator runs the readers. Question 1, "does the 2 s include building App?", is the one the owner flagged for P. Results are pending.

### 8.3 Prototype

Branch `wf10/spike-syntax-proto`, head `073de983`, cut from `wf10/spike-syntax`. It lowers P onto existing machinery:
- duration literals, as millisecond integers in the lexer;
- `run X.fork()` → today's fork;
- `X.scoped()` → a recipe-level scope owner (a checker case like timeout's, without rows);
- `X.race(Y)` → a new kernel. The checker joins two closed owners. Go `er.Race` shares the timeout runtime through a generic `raceWith`. JS `__ef_race` uses `Effect.raceFirst`, then interrupts and awaits both fibers;
- `timeoutOr` → timeout + catch, until #127/#128 let it be written in `.ef`.

`timeout` charges `Clock` instead of `Scheduler` (1-G, rows note Q5).

| File (prototype dir) | Go | JS | Output equals today's |
|---|---|---|---|
| `dashboard.ef` (P worked example) | exit 0, `live / live` | exit 0, `live / live` | yes |
| `concurrency.ef` (P rewrite) | exit 0, `child joined` / `timed out` / `recovered` | same | yes |
| `layers.ef` (unchanged in P) | exit 0, `fixture:fixture` | same | yes |
| `race-library.ef` (race kernel; `deadlineOr` written in `.ef` over race + `Clock.sleep`) | exit 0, `fast` / `failure won` / `abandoned loser` / `gave up` / `work` / `outer` | same | n/a (new) |
| `walls.ef` | `ef check`: EF127 (user generic functions), EF125 (row difference), EF123 (owned value escapes a closing scope, i.e. Q11), EF105 (stored fork recipe) | | the walls are the evidence |

**Test status on the prototype branch.** One run of `go test ./internal/compiler ./runtime/...`:
- `runtime/effra` passes. The `raceWith` refactor keeps every timeout runtime test green.
- `internal/compiler` reports 150 failing top-level tests. About 127 of them fail only because the prototype worktree has no `node_modules` ("pinned JavaScript packages: stat …/node_modules").
- The remaining ~23 are the deliberate Scheduler→Clock change: EF108 "missing service requirements: Clock" in fixtures that provide `Scheduler`, the custom-Scheduler authority tests in `causal_test.go`, and ownership-matrix and cascade expectations for timeout.

None of the failures points at the new kernel cases. The branch is evidence, not a mergeable change.

## 9. Decision record

**Chosen set and syntax.**
- **Constructs:** `effect fn`, `run`, `fail`; the declarations `service`, `impl`, `layer` (and machines).
- **Notation:** duration literals (`ms`, `s`, `m`).
- **Kernel methods:** `.fork()`, `.join()`, `.interrupt()`, `.race(x)`, `.scoped()`, `acquireRelease(a, r)`.
- **Library:** the timeout family, `retry`/`Schedule`, `either`, `delay`, `.provide(Layer)`, `.provide<S>(p)`, `.catch`, `.recover`, `.orFail`.
- **Library intrinsic until O3:** `all`/`both`/`forEach`.
- **Retired:** the `scope { }` and `fork` keywords.

**Losing options:** A, B, C, D, E for the reasons in §7. In short: more keywords, helpers for every wrapped recipe, reused or new words for the fallback arm, and (A, B, E) the `Scheduler` named by syntax. D keeps a compiler-known call surface.

**Effect on tickets:**
- **#95** (reserve postfix words): **superseded.** Postfix words are ordinary methods (#127); nothing is reserved.
- **#132** (provide as library): **re-scoped** to cover `.provide(Layer)` as well as `.provide<S>`, with `Layer<Out, E, In>` as the declaration's type and the plan read from the declaration. This conditionally overturns the rows note's NO-GO.
- **#126** (F3, row difference): **priority raised.** `scoped` and both provides depend on F3b.
- **#127** (F1 + receiver methods): **priority raised.** Every P spelling is a receiver method.
- **#128** (4a): Q9 is needed for the generic library, and **Q11 becomes load-bearing**: it carries the owner guarantee for every library owner (`walls.ef` EF123). It is the main risk.
- **#129** (4b): unchanged.
- **#130** (4c): fork's and join's declared ownership and observation contracts use its vocabulary.
- **#131, #133** (catch, recover): unchanged. `catch<Timeout>` is just catch.
- **#106** (non-escaping block closures): no longer required by #134. It would only make inline sub-scopes nicer (1 site in 26 files).
- **#124, #125:** receive K1–K15 and P13.
- **#121:** a UI frame runtime is a named provider client (§6.3).

**Follow-up units** (implementor-days are estimates; the order is relative to rows phase 2, which delivers F3b, #127 and #128):

| Unit | Work | Days | Order |
|---|---|---|---|
| U1 | P13 race kernel in both runtimes, with K1–K5 law tests | 2 | now; the prototype exists |
| U2 | Duration literals + nominal `Duration`; the formatter prints literals | 2 | now |
| U3 | timeout charges `Clock` (1-G); migrate tests and examples | 1–2 | now (decides Q5) |
| U4 | Parser, formatter and LSP for `.fork()`/`.scoped()`/`.race()`; `ef fix` migration of `scope { }`/`fork` | 3 | after O4 |
| U5 | Bundled kernel module with declared signatures | 5 | after rows phase 2 (#127, #128 Q9, F3b, #130) |
| U6 | Library over the kernel: timeout family, retry/`Schedule`, either, delay | 3 | after U5 |
| U7 | `Layer<Out, E, In>` typed value; `.provide(Layer)` as library; graph, lint and inspection read the plan from the declaration | 5 | after F3b |
| U8 | Q11 closed-at-the-edge contract | 5–8 | inside #128; critical path |
| U9 | Remove the name-keyed checker branches (`timeout`, `provide`, `provideLayer`, `scope`, `fork`) | 3 | after U5–U8 |
| U10 | `all`/`both`/`forEach` after O3 | 3 | after O3 |
| U11 | Kernel as a provider protocol (a `Runtime` provider interface plus the RS1 law suite), first non-Effect provider: the deterministic test runtime | 8–10 | after U9; with #124 |

**Open owner questions** (each with a recommendation):

- **O1. Adopt P?** The constructs are `effect fn`/`run`/`fail`/declarations, everything else is kernel methods or library, and the `scope`/`fork` keywords retire. *Recommend yes.*
- **O2. Fork ownership.**
  - Effect parity: fork charges no row, and the child belongs to the dynamic owner.
  - The alternative: fork charges `Scope`, and `.scoped()` removes it, so every forking function shows `Scope` in `uses`.

  *Recommend Effect parity:* `acquireRelease` charges `Scope`, fork does not. The rows stay small, and R9 already defines the owner.
- **O3. Group failure policy for `all`/`both`.**
  - Fail-fast: interrupt siblings, wait for them, and combine later failures (Effect concurrent `forEach`, `internal/effect.ts:5132-5169`; Kotlin; errgroup).
  - Wait-all.

  *Recommend fail-fast*, as a library intrinsic with a declared contract, and note the Kotlin pitfall for fork+join+catch.
- **O4. Interim before Q11.**
  - Switch the surface early, with the checker keyed on the kernel module's declarations as temporary lang items.
  - Keep today's surface until U5–U8.

  *Recommend keeping today's surface* and shipping U1–U3 now. No interim lang items, per the 2026-10-09 direction.
- **O5. Duration units.** *Recommend* `ms`, `s` and `m` only, with no `h` or `d` in source until a client needs them.
- **O6. Timeout charges `Clock`, not `Scheduler` (1-G, rows note Q5).** *Recommend yes.* `Scheduler` keeps test control (`advance`, `awaitRegistration`).
- **O7. Inline sub-scopes need a helper function under P.** *Recommend accepting this.* Revisit with #106 if the corpus grows more than the 1 site in 26 files seen here.

## 10. Direction change (2026-10-12)

The spike started with a block-construct hypothesis, leading option C. Two owner directions on 2026-10-12 changed it:

- **(a)** No reused keywords such as `else`, and primitives plus library by default.
- **(b)** Constructs must pass a guardrail test and a portability test, with meaning as a protocol plus laws that userland providers implement.

What changed as a result:
- `else` became `otherwise` in A and B.
- Option P was added and made the leading candidate.
- The bar in §4 was applied to each construct.
- The schedule idea was evaluated (§5).
- The kernel was framed as a protocol, with providers and laws (§6.3).
- The prototype was rebuilt for P.

The block corpus (A–E) is kept as the losing comparison.
