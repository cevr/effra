# Primitives and constructs (#134)

Status: research note and decision record for wayfinder ticket #134 (run wf10, unit spike-syntax), 2026-10-12, round 2 after review 1. Owner decisions are still open where marked **O*n***.

Appendices:
- [primitives/effect-study.md](primitives/effect-study.md): Effect 4 kernel and derived operations, with file:line.
- [primitives/prior-art.md](primitives/prior-art.md): the wider prior art.
- `primitives/corpus/<option>/`: the corpus rewritten per option (H, P, A–E, and today).
- `primitives/blind-reader/<option>/`: blind-reader packets; the answer key is [primitives/blind-reader-key.md](primitives/blind-reader-key.md).
- `benchmarks/syntax/measure.ts`: the corpus metrics and the one classification of forms that every option is scored against.
- The prototype lives on branch `wf10/spike-syntax-proto`, under `docs/research/primitives/prototype/`. Head `bc263cf8` prototypes option H; `073de983` is round 1's option P and `e11047a3` adds the baseline probes run with the main compiler. That branch is never merged.

## 0. Decision in one page

**Chosen: option H, owner blocks plus library.** A form is a construct when it passes the guardrail test (§1): it makes an illegal state unrepresentable, and neither a library function nor a general type feature can do the same. Everything else is ordinary library. Every construct is a keyword or block form. Every dot form is honest library: a function written in Effra, or a primitive whose contract the accepted vocabulary can state.

**Constructs**
- Evaluation and control: `effect fn`, `run`, `fail`.
- Declarations: `service`, `impl`, `layer` (and machines, outside this ticket).
- `scope { … }`: an owner block. Whatever it owns closes when the block ends.
- `fork recipe`: starts a child. It is legal only inside an owner block of the same function, so there is no ownerless spawn and no child outlives the function that forked it.
- `run child`: running a fork handle waits for the child and gives its result (the join).
- `interrupt child`: cancels the child, waits for its cleanup, and raises the child's retained failure, so its row is the child's `raises { E }`.
- `race { fork a  fork b … }`: an owner block whose value is the first arm to complete. The losers are cancelled and cleaned up before the block yields.
- `provide App, Clock = LiveClock { … }`: a provision block whose header is a static plan, read by graph, lint and inspection at each site.
- Proposed, not prototyped: `all { fork a  fork b … }`, a fail-fast group owner (P14, §6.6).

**Notation:** duration literals (`500ms`, `2s`, `1m`).

**Library:** the timeout family (`timeout`, `timeoutOr`, `timeoutOption`, `timeoutTo`), `after`, `expire`, `either`, `delay`, `retry` with a `Schedule` value, `.provideService<S>(p)`, `.catch`, `.recover` and `.orFail`. All of it is written in `corpus/H/library.ef`, the timeout family over race blocks.

```effra
effect fn dashboard() -> string uses { Account, Invoice, Clock } {
    scope {
        let account = fork Account.label()
        let invoice = race {
            fork Invoice.label()
            fork after(500ms, "invoice: slow")
        }
        let name = run account
        name + " / " + invoice
    }
}

effect fn main() -> string {
    provide App, Clock = LiveClock {
        run dashboard().timeoutOr(2s, "gave up")
    }
}
```

The prototype runs this program on Go and JavaScript and prints the same as today's version (§8.3). It is monomorphic there, because generic `timeoutOr` needs #127 and #128 Q9.

**Why H.** Review 1 found that P's guarantees did not survive the bar (§7.1):
- P put fork, join and interrupt in dot form, but no clause of the accepted contract vocabulary can create a pending-child obligation or observe one (B1).
- P's owner guarantee rested on the closed-at-the-edge contract (#128 Q11), which the rows note records as not yet shown, scoring 0 of 29 (M1).
- P let a child outlive its function, where Trio, Loom, Eio and Swift make that unrepresentable (M6).

H keeps each guarantee in a direct construct that today's checker already enforces. It moves only what really is library into library.

Three guarantees are new:
- **No ownerless child.** A `fork` outside every owner block is a syntax error (EF002 on the prototype).
- **The deadline is library, and nothing is lost if the library is late.** `timeoutOr` is a function over a race block. If the general features it needs slip, users write the race block inline, and every guarantee holds.
- **Each provide site has a checked plan.** The plan is written at the site, so tooling never meets a provide whose plan it cannot read.

**Cost.** H adds three keywords (`race`, `provide`, `interrupt`), giving 31 against today's 28. It is 4.3% fewer tokens than today, with zero construct lookalikes on the shared classification (§8.1). The owned-fork rule refuses 32 of today's top-level compiler tests (61 subtests, one of them checking `examples/causal.ef`) until they are wrapped in `scope` (§8.3).

**Portability.** Each construct lowers to a protocol: the host primitives P1–P12, the new race primitive P13, and laws K1–K17 (§6.8). The Effect runtime and the Go runtime are two providers. A deterministic test runtime, a UI frame runtime, a request-scoped server runtime and an embedded tick loop are further providers that need no source change (follow-up U11).

**What still depends on unbuilt features.** Only the library layer does:
- The generic timeout family needs receiver methods (#127) and whole-value type parameters (#128 Q9). The prototype's `walls.ef` reproduces EF127.
- `.provideService` needs row difference (F3b, #126).
- Lossless wrapping (K11) of resource-producing recipes needs Q11.

Without Q11 such a wrapper does not reach parity with the inline form: on the rows note's 29 programs, 12 are refused inside the wrapper and 17 get a different verdict. Those programs write the race block inline. No guarantee rests on Q11.

## 1. The question and the bar

#134 asks which concurrency, lifetime, time and provision operations Effra builds in, and in what syntax. Three owner directions set the bar.

1. Standing (memory, 2026-10-09): constructs look distinct (keyword or block forms), and any dot or pipe form is ordinary library. The ladder runs: library → library plus a general type feature → 1:1 notation → construct. A construct is justified only when its meaning is evaluation, lifetime or control, or a single-client type feature, or a structure that tooling must read. "Only when" is a necessary condition. Since direction 3, the guardrail test decides whether that condition is met.
2. 2026-10-12 (a): "else is overloading if else … scope and all these things are actually just normal methods in typescript effect. Time out, I look at it as just part of a schedule … why they need to be Language constructs." So `else` and other reused keywords are dropped from every option, primitive plus library is the default hypothesis, and the owner's schedule idea gets an evaluation (§5).
3. 2026-10-12 (b): "enough guardrails so that bad code or illegal states cannot be represented … if we can make these language constructs and they make sense, then I think we should … layers … portable across different domains … useland implementations." Two tests follow:
   - **Guardrail.** Name the illegal state or static check, and show that no library function or general type feature can provide it.
   - **Portability.** The construct's meaning is a protocol: operations plus laws (#125) that any lawful provider, including userland, can implement.

   Blocks and primitives may coexist. Round 1 read direction (a) as "P unless the guardrail test fails", and then did not run the test on fork ownership (review 1, M6). Round 2 runs it on every candidate (§4).

Two further checks apply to every option:
- **The five composability checks:** constructs wrap losslessly, nest, keep recipes first-class, stay orthogonal and state their composition laws (§6.9).
- **Readability** for Go and TypeScript readers, measured by blind readers (§8.2).

## 2. Effect-TS, in short

The full study with file:line is in [effect-study.md](primitives/effect-study.md). Facts that decide this ticket:

- **Effect's kernel is small.** About fifteen interpreter nodes (`internal/core.ts:435,467`; `internal/effect.ts:622`) plus three fiber-runtime methods: fork (`forkUnsafe`, `internal/effect.ts:5517-5539`, which attaches the child to the parent at `:5532-5535`), interrupt (`:4547`) and context update (`:2175-2200`).
- **Timeout is derived:** `timeout → timeoutOrElse → timeoutOption → raceFirst(asSome(self), as(sleep(d), none))` (`internal/effect.ts:3875-3903`). Race losers are interrupted and awaited (`:1620`, `:1673`, `:930`; tests `Effect.test.ts:1347,1366,1385`).
- **Scope, acquireRelease and provide are library.** `Effect.scoped` is a root scope that removes `Scope` from R (`:4100-4110`). `acquireRelease` adds `Scope` (`:4134-4150`). `Effect.provide(layer)` is `scopedWith` plus `buildWithScope` (`internal/layer.ts:8-22`). `buildWithScope` reuses the current MemoMap through `CurrentMemoMap.forkOrCreate` (`Layer.ts:582-586`, `:765-775`); only `{ local: true }` gets a fresh map (`internal/layer.ts:17-18`). So nested non-local provides share builds, and separate root runs build twice (test "subsequent calls share MemoMap", `Effect.test.ts:4644-4660`).
- **A fiber is not an effect in v4.** `Fiber` extends only `Pipeable` (`Fiber.ts:71`), and you join with `Fiber.join`.
- **Schedule is an inert value** (`Schedule.ts:53`). `retry` runs an attempt to completion and only then consults the schedule (`internal/schedule.ts:66-79`).
- **What hurts:**
  - There are no static lifetimes: handles and resources can escape their scope, and `forkChild` makes the child outlive the forking function.
  - The catch zoo churned: v4 removed `catchAll`, `orElse`, `either`, `timeoutFail`, `timeoutTo` and `forkDaemon`.
  - Effect documents no laws.

Effra already matches the kernel shape. The lawful runtime contract lists host primitives P1–P12 and says: "Timeout, race, latch composition and layers are derived library behavior. They must not become extra primitive syntax" (`docs/research/lawful-runtime-contract.md:63`). H amends that sentence in two places, flagged for U1 and U7:
- race becomes host primitive P13 and gets block syntax;
- provision gets block syntax, because its plan is static.

Timeout stays derived library, as the contract says. Today's language contradicts the sentence for timeout: `.timeout` and `.provide(Layer)` are parser and checker constructs (`syntax.go:1640-1675`; `semantic.go:4979`, `:5047`).

## 3. Prior art, in short

The detail and pins are in [prior-art.md](primitives/prior-art.md).

**Structured concurrency makes an ownerless spawn unrepresentable.** Every library in the survey that guards lifetimes requires an owner value or type to spawn:
- Trio's `nursery.start_soon`;
- Loom's `StructuredTaskScope.fork` (JDK 26, JEP 525), with structure violations checked at run time;
- Eio's `Fiber.fork ~sw` (`eio__core.mli:258`);
- Kotlin's `launch`, an extension on the `CoroutineScope` receiver.

Swift's `async let` is the construct form of the same guarantee: the child is bound to its block, and it is awaited or cancelled at block exit. Go's `go` and Effect's `forkChild` are the outliers, where a child can outlive its caller. **H's owned fork** is the construct form. Effra cannot use the owner-value form yet, because an owner value would be a second-class value (#106).

**Deadlines are library over first completion or a cancel scope:**
- Effect, ZIO and cats-effect (where `timeoutTo` is derived from `racePair`);
- Kotlin `withTimeout`, Trio `move_on_after` and `fail_after`, Go `context.WithTimeout`;
- Eio derives `with_timeout` from `Fiber.first` (`time.ml:64-65`), Koka derives timeout from `firstof` (`interleave.kk:57-61`), and MoonBit derives `with_timeout` from a task group (`async.mbt:96-108`).

The construct-level first-completion forms (Go `select`, Erlang `receive … after`) bind a channel or mailbox. H follows the derivation: race is the construct, timeout is library.

**The constructs that do exist give a fixed syntax a pluggable meaning:**
- F# computation expressions (a builder supplies `Bind` and `Using`);
- C#'s awaitable pattern;
- Rust `async`/`await` with pluggable executors (tokio, embassy);
- Python's `with` protocol;
- Go 1.23 range-over-func;
- Kotlin dispatchers;
- Koka and OCaml 5 handlers (Eio is one scheduler among several);
- Scala's for-comprehension desugaring.

Their known failure is meaning drift: F# builders and Haskell `RebindableSyntax` let the same syntax mean anything. Effra's answer is a closed operation set with fixed types plus a conformance suite (§6.8).

**Static presence of provided capabilities.** Koka `with` handlers and Effekt check handler or capability presence statically. React context, Go `context.Value` and Kotlin `CoroutineContext` do not. Effra's rows already check it.

**Compile-to-host languages and the visual rule:**
- Gleam keeps its constructs few and distinct. `use` passes the rest of the block as a callback, and the v0.25 release post and the language tour both warn about overusing it. That is the "rest of the block is an argument" problem, which D shares.
- ReScript emits native `async`/`await` (`js_dump.ml:392`, `:1042`).
- Elixir is the "don't copy". `if`, `unless` and `|>` are Kernel macros, while `with`, `for` and `case` are special forms. A reader cannot tell a construct from library by its shape, which is exactly what Effra's visual rule forbids.

## 4. The bar, construct by construct

For each candidate:
- (a) the illegal state that a construct rules out, or the static check it enables;
- (b) whether a library function or a general type feature can do the same;
- the verdict.

| Candidate | (a) Illegal state or static check | (b) Library or general carrier? | Verdict |
|---|---|---|---|
| `run` | A recipe that runs without being asked to. Constructing a recipe runs nothing (R11), and a discarded effect is diagnosed. | No. `run` *is* the bind (Effect `yield*`, F# `let!`, Rust `.await`); a function cannot sequence its caller. | **Construct** |
| `fail` | none (control with a typed label) | `run fail(E)` would work, but `fail` reads like `return` and `throw`. | **Construct** (control) |
| `effect fn` | none (declaration with explicit rows) | none | **Construct** (declaration) |
| `scope { }` | An owned value escaping its owner (EF123). Children and resources that are not closed in LIFO order (R1, R2). | The closed-at-the-edge contract (Q11) would carry it for a library `scoped(work)`, but it is not shown: 0 of 29 (rows note `effect-rows-as-types.md:428`, `:1041`). | **Construct.** It is also the only inline nested lifetime without closures (#106). |
| `fork` | **A child that outlives the function that forked it.** H makes it unrepresentable: `fork` is legal only inside an owner block. | An owner value (Trio, Loom, Eio, Kotlin) needs a second-class value type (#106). No clause of the contract vocabulary creates a pending-child obligation (`effect-rows-as-types.md:395-424`, `:461-480`; review 1, B1). | **Construct, owned** (§6.2) |
| `run child` (join) | A child's failure that nobody observes and nobody declares. Join observes it (R4). | No clause observes a `Fiber` value (B1). A dot form `.join()` would be a compiler-known name. | **Construct:** `run` applied to a fork handle (§6.3) |
| `interrupt child` | A retained child failure dropped by cancellation (M2). | As for join. | **Construct**, row `raises { E }` (§6.3) |
| `.cancel()` (today) | none beyond interrupt | n/a | **Dropped** (O8) |
| `race { }` | A loser that keeps running after the race yields. A loser's abandoned typed failure leaking into the owner's outcome (K3). | Not over `scope` + `fork`: a scope keeps an unobserved child's failure on a normal close (R3), and race must discard it; first completion needs a host primitive (P13). Not a function: its arms are forks, which must be lexically owned. | **Construct** (§6.4) |
| `.timeout(d)` (today) | none of its own: its guarantees are race's (owner) and Clock's (rows) | Library over a race block, as in Effect, Eio, Koka, cats-effect and MoonBit. | **Library** (§6.7) |
| `provide Plan { }` | **A provide site with no checked plan.** Graph, lint and inspection read the plan at each site (`graph.go:474`, `lint_facts.go:311-319`). A missing service at build time (EF108). | No. A first-class `Layer<Out, E, In>` value carries rows, not the plan (`effect-rows-as-types.md:961`; review 1, M4). | **Construct** (§6.5) |
| `.provideService<S>(p)` | none beyond rows | F3b: `uses { R - S }` (#132) | **Library** |
| `.catch`, `.recover` | none beyond rows | F2, F3b (#131, #133) | **Library** |
| `.orFail()` | none beyond rows | ordinary data once GoResult is | **Library** |
| `acquireRelease` | Release that runs zero times or twice (R10, P5). | A host primitive with a `Scope` row. | **Host primitive** (P5), library-shaped |
| `all { }` | A sibling that keeps running after the group has failed. A composite of the first failure and sibling failures that no handler can match. | Not as library: the composite cause cannot be declared (`docs/runtime.md:17`; `baseline/both.ef` EF107 on the main compiler). | **Construct, proposed** (P14, §6.6, O3) |
| `defer e` (Go-like) | n/a | `acquireRelease` and `onExit` cover it | Not proposed |
| duration literal `2s` | none | 1:1 notation for `Duration.ms(2000)`; a second client is byte sizes | **Notation** |
| `layer` declaration | The static plan: construction graph, `merge`/`replace`, missing and duplicate services inside the layer. | None: tooling must read the structure. | **Construct** (unchanged) |

**The illegal states the owner named:**
- **A fiber handle used after its owner ends; a resource escaping its lifetime.** Ruled out today by EF123 for every owner block. H keeps it with the same checker, with no new feature needed (prototype test `TestSpike134OwnedHandlesKeepTodaysOwnershipRules`).
- **A child that outlives its function.** Not ruled out today (review 1, probe `g1-today.ef`). Ruled out by H's owned fork. P made it worse by moving lifetimes to callers (M6).
- **A forgotten join.** Not illegal. The failure surfaces at owner close and must be declared (EF107), and the child is cancelled at close. A lint can flag an unjoined handle.
- **A missing service at build time.** Rows catch it in every option (EF108; prototype `TestSpike134ProvideBlockChecksServices`).
- **A deadline that silently fails to cover acquisition.** Not an illegal state: both placements are legitimate intents. H spells the choice by nesting. `provide App { run w.timeoutOr(2s, v) }` leaves the build outside the deadline. To include it, the provide block goes inside a function that the deadline wraps (`walls.ef`, `outside` against `inside`).

**The rows note's NO-GO claims, re-tested:**
- **timeout**, NO-GO because "first-completion race and abandonment of the losing work are control". The premise is true, and the control lives in race. With race as a construct, timeout is library. **Overturned**, and the prototype shows it (`race-library.ef`).
- **provide(Layer)**, NO-GO because "tooling reads the static layer plan". **Upheld.** Round 1 overturned it conditionally on a typed `Layer` value; review 1 (M4) showed that such a value carries rows, not plans. H keeps provision as a construct and moves the plan into the block header.

## 5. Timeout and Schedule

The owner suggested treating a deadline as part of a schedule. Concretely: a time-policy value would compose with `Schedule` (`upTo`, `during`, recurrence limits), and `work.retry(policy)` would carry both the retry and the deadline.

**What breaks.** A schedule acts between attempts. Effect's `retry` runs an attempt to completion, then asks the schedule whether and when to run the next one (`internal/schedule.ts:66-79`); `Schedule` is inert data (`Schedule.ts:53`). So `Schedule.upTo(2s)` bounds when the *next* attempt may start. It cannot stop an attempt that hangs. Effect's own documentation says so: "Long-running effects can cause the duration limit to be detected on the following schedule step" (`Schedule.ts:1271-1273`; `during` at `:750` checks elapsed time only when stepped). Stopping a running attempt is preemption: a race against a timer. That is why Effect keeps `timeout` (a race, `internal/effect.ts:3875-3903`) separate from `Schedule` (data). cats-effect, ZIO and Kotlin draw the same line.

**What survives of the idea.** Both are time policy, and they compose by order, as ordinary library. Polly v8's `ResiliencePipeline` and resilience4j compose Timeout and Retry as values in the same way, with order fixing placement.
- `work.timeout(500ms).retry<Timeout>(policy)`: each attempt gets 500 ms, and timed-out attempts are retried.
- `work.retry<E>(policy).timeout(2s)`: the whole retry loop gets 2 s.
- `Schedule` stays a plain enum, with `Spaced`, `Times` and later `Exponential` and `UpTo`. A `Duration` value is the preemption policy.

Laws K14 and K15 (§6.8) state the difference, so a reader and a provider cannot confuse the two.

## 6. Option H design

### 6.1 Syntax

```
OwnerBlock   = "scope" Block
             | "race" "{" ForkArm ForkArm { ForkArm } "}"
             | "all"  "{" ForkArm ForkArm { ForkArm } "}"        (proposed, §6.6)
ForkArm      = "fork" Expr
Fork         = "fork" Expr                       (only inside an OwnerBlock of the same function)
Join         = "run" Ident                       (Ident is bound to a fork handle)
Interrupt    = "interrupt" Ident                 (a statement)
Provide      = "provide" PlanEntry { "," PlanEntry } Block
PlanEntry    = LayerName | ServiceName "=" ProviderExpr
```

- `race`, `provide` and `interrupt` are new reserved words. None appears as an identifier in `examples/`, and after a dot (`x.provide`) they remain ordinary member names.
- Durations are literals: `ms`, `s` and `m` (O5).
- No block reuses a keyword. `else`, `or`, `with` and `after` stay free.

### 6.2 Owned fork (review 1, M6)

**The rule.** A `fork` is legal only lexically inside an owner block (`scope`, `race`, or the proposed `all`) of the same function. A fork outside every owner block is a syntax error: "fork needs an owner: put it inside scope { } or make it a race arm" (prototype EF002). Because the owner block closes before the function returns, no child outlives its function. A fork inside an `if` or a loop inside a scope is still owned.

**The guardrail test.**
- *Illegal state:* a child still running after the function that started it has returned. Today `fork` outside a scope uses the dynamic owner (R9), and probe `g1-today.ef` shows the child outliving its caller. P's corpus made this more common by moving lifetimes to callers (`corpus/P/latest-task.ef:18`, `lifecycle.ef:23`, `causal.ef:34`).
- *Library alternative:* the owner-value form (Trio, Loom, Eio, Kotlin). In Effra it needs an owner value that cannot escape, which is #106's second-class value. A `Scope` row on `fork`, discharged by `.scoped()`, makes ownership visible in the row but does not stop a caller from choosing a longer lifetime. It also needs Q11 to discharge the row losslessly.
- *Verdict:* construct. Swift's `async let` is the precedent: the child is bound to its block.

**What stays dynamic.** Resource acquisition (`Files.openRead`) keeps the dynamic owner. A resource is inert data owned by the current owner, and EF123 already stops it escaping. Making resource acquisition lexical too would need an owner for every acquiring call (O7).

**Edition.** On the prototype, the owned-fork rule and the handle reading of §6.3 apply to files that use H's syntax (a race block, or the `provide` or `interrupt` keyword), so legacy fixtures keep passing. With the rule forced on for every file, 32 existing top-level tests (61 subtests) fail with EF002, one of them because `examples/causal.ef` no longer checks. That migration is follow-up U4.

### 6.3 Fork handles: `run` joins, `interrupt` raises (review 1, M2)

A fork handle is the child's eventual result. **`run child` joins**: it waits for the child and gives its result, or raises its failure. This is the reading of Rust tokio's `JoinHandle` (a `Future`), a JavaScript `Promise` and Swift's `async let`. Effect 4 is the exception: its `Fiber` is not an effect (`Fiber.ts:71`).

Because a joined handle is a recipe, recovery works on it in place: `run child.catch<Bad>("recovered")` (prototype test; rows note clause 4, in-place half shown).

**`interrupt child`** is a statement. It requests cancellation, waits for the child's cleanup, and raises the child's retained failure. Its row is the child's `raises { E }`, matching what both runtimes and today's checker already do:
- Go returns the cause when it is more than interrupts (`runtime/effra/fiber.go:171-176`).
- JS `__ef_interrupt` re-raises it (`prelude/interrupt.mjs:6`).
- The checker gives interrupt the child's failures (`semantic.go:5513-5515`).

Prototype receipt: without `raises { Bad }`, an interrupt of a failing child is EF107.

**The handle cannot escape its owner** (EF123; prototype test). It cannot be passed as a parameter, stored in a payload or returned past its owner (`docs/runtime.md:17`).

### 6.4 The race block

`race { fork a  fork b … }` is an owner block with n fork arms. Its value is the exit of the first arm to complete, success or failure. Its row is the union of the arms' rows, because any arm may win (prototype test). Every loser is interrupted, and its cleanup completes before the block yields (K2).

**Why it is not library over `scope` + `fork`.** Inside a scope, an unobserved child's typed failure is raised at a normal close (R3). A race must discard a loser's typed failure even when the loser fails between the winner's completion and the close (K3). That needs its own owner policy and a first-completion primitive. Effect derives `raceAll` from `withFiber`, `callback` and `forkUnsafe` (`internal/effect.ts:1603`, `:1657`), but Effra has no general callback or mutable cell in the language, so first completion is host primitive P13. Go `select` and cats-effect `racePair` (`GenSpawn.scala:354`) make the same choice.

**Why it is not a function `either(a, b)`.** The arms are forks, and forks must be lexically owned. A library `either` over the block is still possible, and `corpus/H/library.ef` defines one.

**Not associative.** Under K2, `race { fork race { fork a  fork b }  fork c }` and `race { fork a  fork race { fork b  fork c } }` can publish different winners when a loser's cleanup is slow (review 1, M3). So the block is n-ary, and K1 is stated over its n arms, not by nesting binary races. The prototype accepts exactly two arms; n-ary is follow-up U1.

### 6.5 The provide block (review 1, M4)

`provide App, Clock = LiveClock { body }` builds the named layers and providers under a provision owner (R12, R13), runs the body, and closes them.

**Why it is a construct.** The header is a static plan: a list of `layer` declaration names and `Service = provider` entries. Graph, lint and inspection read it at each site. Lint already fails without a checked plan: "layer provision … has no checked plan" (`lint_facts.go:311-319`). A library `provide(layer)` over a `Layer<Out, E, In>` value would carry rows, not the plan (`effect-rows-as-types.md:961`). A parameter of that type has no unique declaration, and no set of plans can be enumerated across modules.

**No layer values.** H admits no `Layer`-typed parameter. Choosing a layer at run time is written as an `if` with two provide blocks, so every site keeps one plan, and inspection has no unresolved node to mark (`docs/specs/layers.md:110-114`).

**`provideService`.** The entry `S = p` in a header and the library form `work.provideService<S>(p)` mean the same thing (K17). The library form exists so that a recipe can be provided where it is built, for example `fork greeting("42").provideService<Directory>(FixtureDirectory)`, without a helper function (`corpus/H/testing.ef`). It is a primitive with a declared contract (#132), needing F3b for `uses { R - S }`. It was renamed from `.provide<S>` so that it never shares a word with the `provide` keyword. Effect uses the same name.

**MemoMap.** Effect shares builds across nested non-local provides (§2). H's provide block builds each named layer once per block (R13). Sharing across nested blocks is a separate optimisation that no current law requires.

### 6.6 The `all` block, proposed (review 1, M5; O3)

`all { fork a  fork b … }` runs its arms in parallel and yields their results in arm order. **Fail-fast:** the first failing arm's failure becomes the block's outcome. The block interrupts its siblings, waits for their cleanup, and discards their ordinary typed failures. Defects and cleanup failures are kept.

**Why it is a construct.** Under today's rule, two forks in a scope where the first fails leave the second as an unobserved child. Its failure joins the first at a normal close as a composite cause, which no handler matches and no row can declare (EF107 at `prototype/baseline/both.ef:14:11` on the main compiler; `docs/runtime.md:17`). The fix is an owner whose close counts as cancellation-aborted once an arm fails. Its abandoned siblings' typed failures are then discarded, as a cancellation-aborted owner already does (R3; `docs/runtime.md:19`). That is an owner policy, not a function contract.

Prior art: Kotlin `coroutineScope`, Go `errgroup`, and Loom's `Joiner.allSuccessfulOrThrow` (JDK 26).

Laws (P14, handed to #125):
- **A1.** If every arm succeeds, the block yields the results in arm order.
- **A2.** The first arm failure is the block's failure. Siblings are interrupted and cleaned up before the block publishes.
- **A3.** Siblings' ordinary typed failures are discarded, so no composite of typed failures arises. Defects and cleanup failures are kept (R3, R6).
- **A4.** The row is the union of the arms' rows.

`all` is not prototyped. It is follow-up U10, after O3.

### 6.7 Library over the constructs, and what Q11 carries (review 1, M1)

`corpus/H/library.ef` writes every library function in Effra:
- the timeout family, over race blocks with `after` and `expire` as timer arms;
- `either`, `delay`, and `retry` over a `Schedule` enum;
- the `provideService` signature.

The prototype's `race-library.ef` runs monomorphic `after`, `expire`, `deadlineOr` and `deadline` on both targets, and `deadlineOr` nests inside itself.

**What each piece needs.**
- Receiver methods (#127), for the dot spelling.
- Whole-value type parameters (#128 Q9), for the generic family (`walls.ef`, EF127).
- Label parameters (#131), for `retry<E>`.
- Row difference (F3b, #126), for `provideService`.
- Forking a recipe parameter as a race arm, which the prototype admits today.
- The closed-at-the-edge contract (Q11), for K11 lossless wrapping of a recipe that returns an owned value.

**Q11's status.** The rows note records the closed-at-the-edge half as not yet shown: the scope-bodied timeout model scores 0 of 29 (`effect-rows-as-types.md:428`, `:1041`, `:1300`, `:1320`). Of those, 12 programs are refused inside the wrapper body with "value owned by closing scope cannot escape", and 17 get a different verdict from the direct form. The rows note does not say whether any of the 17 admits more than the direct form, so U8 must settle that before a wrapper of owned-value recipes is admitted.

**Gate and fallback.**
- *Gate:* retiring today's name-keyed `.timeout` (U9) waits for the generic library (#127, Q9) and for parity of the library `timeout` with today's on the timeout corpus, including the 29 Q11 programs.
- *Fallback:* if Q11 does not land, resource-returning work is timed with an inline race block, and `.timeout` stays keyed until U9 can use the library for everything else.

No owner guarantee depends on Q11: owned fork, EF123, the race owner, provision and interrupt's row are all direct constructs.

### 6.8 Protocol, laws and portability

Each construct lowers to a provider-independent protocol: P1–P12, plus P13 race (and P14 all, when admitted). Effra already works this way for machines, where a checked declaration lowers to a plan that any userland provider executes (`docs/specs/machine-provider-contract.md:7`, `:45`), and for time (`uses { Scheduler }`, `TestScheduler`).

| Provider | fork | race | owner close | sleep | Use |
|---|---|---|---|---|---|
| Effect runtime (JS) | `Effect.forkChild` adapter | `Effect.raceFirst`, then interrupt and await (prototype `race.mjs`) | `Scope` | `Clock` | today |
| Go runtime | goroutine + `Scope` | `er.Race` over `raceWith` (prototype) | `er.Scoped` | `SleepWithDriver` | today |
| Deterministic test runtime | a queued task | decided by virtual time (R14), ties broken by a seed | LIFO close | `TestClock` | today, partly (`TestScheduler`) |
| UI frame runtime (#121; cf. Foldkit, Elm `Cmd`/`Sub`) | a task stepped once per frame | first of an event or a frame-clock deadline | mount/unmount is the owner | frame clock | U11 |
| Request-scoped server runtime | a goroutine bounded by the request | as Go | the request is the root owner; client disconnect interrupts | the request budget (Go `context.WithDeadline`) | U11 |
| Embedded or game tick loop (cf. embassy) | a cooperative task | first completion per tick | static owner tree | tick counter | U11 |

**Laws** (handed to #125; they generalise R1–R15 of `lawful-runtime-contract.md:76-90`):

- **K1 first completion.** A race block publishes the exit of whichever of its n arms completes first, success or failure (Effect `raceFirst`, `internal/effect.ts:1736`).
- **K2 loser cleanup precedes publication.** Every loser is interrupted, and its cleanup completes before the block publishes (generalises R8).
- **K3 abandonment.** Losers' ordinary typed failures are discarded. Their defects and cleanup failures are kept (R3, R6).
- **K4 symmetry.** Reordering the arms does not change the result whenever completion order is determined. No tie guarantee is made across targets (`docs/runtime.md:21`).
- **K5 identity.** An arm that never completes and needs no cleanup can be removed: `race { fork w  fork never() }` behaves as `run w`.
- **K6 timeout derivation.** `w.timeout(d)` behaves as `race { fork w  fork expire(d) }`, and likewise `timeoutOr` with `after` (definitional in `library.ef`).
- **K7 nested deadlines.** For `w.timeout(d1).timeout(d2)`, cancellation of `w` is requested no later than `min(d1, d2)` on one clock (R14). The bound covers deadline selection and the cancellation request, not publication: publication waits for cleanup (K2) and may come later. Swift SE-0526 states the same (`0526-deadline.md:127-131`).
- **K8 scope nesting.** `scope { scope { body } }` behaves as `scope { body }`.
- **K9 provide/catch commute.** Moving `.catch<E>(v)` across a provision boundary preserves behaviour only when building, releasing and cleaning up the plan raise no failure and no defect. Otherwise the outer catch sees a composite cause it cannot match (R6). Stated with that condition, or not at all.
- **K10 shadowing.** The inner provider wins: `provide S = p { provide S = q { run w } }` uses `q`.
- **K11 lossless wrapping.** For a library wrapper `effect fn wrap(x) { race { fork x  fork … } }`, `run wrap(r)` behaves as the inline block, with the same rows and the same ownership evidence. This needs Q11 when `r` returns an owned value (§6.7).
- **K12 fork/join.** `scope { let c = fork w; run c }` behaves as `run w`, with the same result and rows.
- **K13 interrupt.** Three cases:
  - after a successful completion, interrupt is a no-op that observes the child;
  - for a running child whose shutdown is interruption only, interrupt waits for cleanup, and the interruption is normalised away;
  - for a child that retained a failure (completed with one, or failed in cleanup), interrupt raises it.
- **K14 schedules do not preempt.** `retry` consults the schedule only after an attempt has completed.
- **K15 placement.** `w.timeout(d).retry(s)` bounds each attempt; `w.retry(s).timeout(d)` bounds the whole loop.
- **K16 owned fork.** A child never outlives the innermost owner block that lexically contains its `fork`. At block exit, unjoined children are interrupted and their cleanup completes (R2, R8). Joining governs observation, not ownership.
- **K17 provide entry.** `provide S = p { run w }` behaves as `run w.provideService<S>(p)`.

**Drift control.** F# builders and `RebindableSyntax` let a library change what syntax means. H fixes the meaning in three ways:
- The operation set is closed: P1–P13 (and P14).
- Their types are fixed by the checker, not by the provider.
- The laws run as a **conformance suite** (#124 RS1). Passing it is evidence, not proof: the checker cannot refuse a provider for failing a finite suite. This differs from an incompatible machine provider, which is a static diagnostic (`machine-provider-contract.md:45`).

Providers choose an implementation. They cannot choose a meaning.

### 6.9 Typing and composability

**Typing.**
- Constructs use the checker's existing owner kinds: child, timeout (reused for race), scope and provision (`semantic.go:160-161`).
- Library functions are ordinary declarations, and no checker branch is keyed on their names.
- TypeScript projection: `{ R - S }` projects to `Exclude<R, S>`, Effect's own type. A row the projector cannot express projects to an opaque nominal type, never `any`.
- Go lowering: rows erase; owner blocks become `er.Scoped`, `er.Race` and provision calls, with the same laws on both targets.

**The five composability checks.**
1. **Wrap losslessly.**
   - Direct use needs nothing.
   - A library wrapper over a race block is an ordinary `effect fn`, and `race-library.ef` nests one inside itself.
   - Wrappers of owned-value recipes need Q11 (K11).
   - A block itself is wrapped by a named function. Including the App build inside a deadline is the one case (`walls.ef` `inside`).
2. **Nest.** Every pair composes (the matrix below). Race does not flatten (§6.4).
3. **Keep recipes first-class.** Every library function takes and returns recipes. A block yields a value, so turning a block into a recipe takes a named `effect fn`. The H corpus needed none: `.provideService` on a fork replaced C's two helpers (§8.1).
4. **Stay orthogonal.**
   - scope: lifetime; fork: start; `run child`: observe; interrupt: cancel; race: first completion; provide: context.
   - Time (Clock), recurrence (Schedule) and recovery (catch) are library.
   - Nothing fuses a deadline with a scope, so Go's `context` problem (cancellation, deadline and values in one bag) does not arise.
5. **State composition laws.** K1–K17 and A1–A4.

**Nesting matrix.** The outer form is the row, the inner one is the column.

| outer \ inner | scope | fork | race | timeout (library) | provide | catch | retry |
|---|---|---|---|---|---|---|---|
| **scope** | K8 | the scope owns the child (K16) | the race owner nests in the scope | as race | the plan closes before the scope (R12, R2) | as written | as written |
| **fork** (arm or child) | the child owns its scope | grandchild, owned by the child's block (K16) | the race runs in the child | as race | the child has its own provision | the child's failure is caught inside | as written |
| **race** | each arm's scope closes before publication (K2) | forks inside an arm are owned by it (K2) | does **not** flatten; n-ary instead (§6.4) | K7 | per arm | per arm | per arm |
| **timeout** | covers the scope's close (K2) | the timeout owner closes the child at the deadline without a join (K16, R8) | K7 | K7 | covers the build only when the provide is inside the wrapped function (§4) | K15 | K15 |
| **provide** | the scope sees the plan | the child inherits the provision lexically | every arm sees it | the build is outside the deadline | K10 | K9 with its condition | builds once; the attempts reuse it |
| **catch** | catches the scope's failures, if a lone typed failure | catches a joined child's failure (`run c.catch<E>(v)`) | catches the winner's failure | catches `Timeout` | K9 | ordered | the last attempt's failure |
| **retry** | re-runs the scope | re-runs the fork | re-runs the race | K15 | a provide inside the retried recipe builds per attempt | as written | nested policies |

## 7. Losing options

Each option was measured on the same 26-file corpus (§8.1). `else` is removed everywhere, per the owner.

### 7.1 P, kernel methods plus library (round 1's choice)

P spelled fork, join, interrupt, race and scoped as dot methods (`.fork()`, `.join()`, `.interrupt()`, `.race(x)`, `.scoped()`), with the timeout family and `.provide(App)` as library. It loses on four counts:
- **B1, construct in dot form.** fork mints a pending-child obligation, and join and interrupt observe a `Fiber` value. No clause of the accepted vocabulary (`effect-rows-as-types.md:395-424`) covers either, so `.fork()`, `.join()` and `.interrupt()` have no carrier. They are compiler-known names in dot form, and the prototype keyed them on `method.text`, reserving the names on every receiver (#95's problem). On the shared classification P has **29 lookalikes**, against H's 0 and today's 17 (§8.1). Round 1's "0" came from setting P's list to empty by assertion.
- **M1, Q11.** P's owner guarantee for `.scoped()`, `.race()` and every library owner rested on Q11, which scores 0 of 29. P's `.race` and `.scoped` could become declared-contract primitives after Q11, because a closed owner publishes nothing pending. Until then they are compiler-known too.
- **M4, provide(Layer).** The conditional overturn rested on a typed `Layer` carrying the plan, which it cannot.
- **M6, lifetimes.** P moved scope decisions from forking functions to their callers, so a child could outlive its function.

P also scored lowest with round 1's blind readers: 10.5 of 12, and 3.5 of 5 on the weighted questions. P's reader hedged Q1 and named "method-chain order semantics … which dot-forms are magic vs library" as confusing (n = 1, weak; §8.2).

What P got right survives in H: timeout and retry as library over race, duration literals, the Schedule analysis, the protocol framing and the laws.

### 7.2 Block options A–E

- **A, clauses on `scope`:** `scope provide App, Clock = LiveClock within 2s { … } otherwise { … }`.
  - It adds 3 keywords (`provide`, `within`, `otherwise`), and the deadline charges `Scheduler` by name (1-G).
  - Clause order *is* nesting order, which readers must learn. Round 1's A reader found the header "ambiguous".
  - It needs 3 recipe-wrapping helpers.
  - It has a semantic trap in `ownership.ef`: `let file = scope provide Files = LiveFiles { … }` makes a resource-acquiring call escape a scope owner.
- **B, separate blocks:** `provide … { within 2s { … } otherwise { … } }`. It has the same keyword cost and helpers as A, plus one more nesting level. `otherwise` is a word the owner has not seen.
- **C, race with a fallback block:** `race { … } or { … }`.
  - C is H's closest relative: time handled as library, provide as a block.
  - It loses three ways: `or` is a reused word (boolean `or` in Python and SQL), its arms are blocks rather than forks so race is not an owner of forks, and it needed 2 recipe-wrapping helpers.
- **D, `with` block sugar (Koka `with`, Gleam `use`):**
  - It keeps compiler-known calls (`provide(App)`): 17 lookalikes.
  - It overloads the existing `derive … with` keyword.
  - "The rest of the block is an argument" is the Gleam `use` criticism.
- **E, race with an `after` arm:** `race { … } after 2s { … }`. `after` is a new keyword (after Erlang's `receive … after`). It is a deadline construct that charges `Scheduler` by name (1-G), and it needs 3 helpers.

A, B, C and E keep `.join()` and `.interrupt()` as dot forms: 12 lookalikes each.

## 8. Evidence

### 8.1 Corpus metrics

The 25 examples that use `.timeout`, `.provide` or `.orFail`, plus the worked example, are rewritten per option under `primitives/corpus/<option>/`. The metrics come from `bun benchmarks/syntax/measure.ts`, which exits non-zero on any of:
- a lex failure, unbalanced braces or a missing file;
- a construct lookalike in the chosen option;
- for H, a `fork` outside a `scope` or `race` block.

**One classification for every option.** The `forms` table gives each dot or call form a carrier:
- `none`: no general feature can make it library, so it is compiler-known, a **lookalike**. These are `.fork`, `.join`, `.interrupt`, `.cancel`, `.provide(` with a layer argument, and D's `provide(`.
- `q11`: library only after Q11. These are `.race`, `.scoped`, `scoped`, and the timeout family (`timeout`, `timeoutOr`, `timeoutOption`, `timeoutTo`).
- `rows`: library after the rows units, #127, #128 Q9, F3b, #131 and #133. These are `.catch`, `.recover`, `.orFail`, `.provide<S>`, `.provideService` and the like.

"Keyed today" counts forms that today's checker recognises by name.

| Option | Tokens | vs today | Code lines | Distinct keywords | Punct/line | Lookalikes | Awaiting Q11 | Awaiting rows units | Keyed today |
|---|---|---|---|---|---|---|---|---|---|
| today | 11090 | 0 | 1664 | 28 | 3.31 | 17 | 8 | 162 | 187 |
| **H** | **10609** | **-4.3%** | 1740 | 31 | 2.96 | **0** | 7 | 48 | 47 |
| P | 11104 | +0.1% | 1646 | 26 | 3.36 | 29 | 15 | 154 | 173 |
| A | 10772 | -2.9% | 1771 | 31 | 2.93 | 12 | 0 | 45 | 57 |
| B | 10710 | -3.4% | 1773 | 31 | 2.93 | 12 | 0 | 45 | 57 |
| C | 10678 | -3.7% | 1746 | 31 | 2.97 | 12 | 7 | 45 | 59 |
| D | 10922 | -1.5% | 1670 | 28 | 3.21 | 17 | 16 | 139 | 123 |
| E | 10720 | -3.3% | 1777 | 31 | 2.93 | 12 | 0 | 45 | 57 |

Reading the table:
- **H is the only option with zero lookalikes.** Today's 17 are `.join` (8), `.interrupt` (4) and `.provide(App)` (5); P adds `.fork` (12).
- **H is the shortest by tokens.** It hoists provision into one block per entry point, and `run child` and `interrupt child` are shorter than `.join()` and `.interrupt()`. Its line count is higher than today's because blocks take lines.
- **H's 7 forms awaiting Q11** are library deadline calls: 5 `.timeoutOr` and 2 `.timeout`. They are library, and the fallback is an inline race block (§6.7).
- **Max nesting depth** is 9 in every option, set by `http-transport.ef`, so it does not discriminate.

**Helpers, one count per option.** These are functions that a rewrite had to add, by category:

| Option | Recipe-wrapping | Nested lifetime | Total |
|---|---|---|---|
| **H** | 0 | 0 | **0** |
| P | 0 | 1 (`interruptOneWaiter`) | 1 |
| A, B, E | 3 (`delayedWithin`, `explicitVirtualSleepWithClockScheduler`, `greetingWithDirectory`) | 0 | 3 |
| C | 2 | 0 | 2 |
| D | 0 | 0 | 0 |

H replaced C's two helpers with `.provideService` on a fork (`corpus/H/causal.ef`, `testing.ef`). Since that is library awaiting F3b, today's checker would still need the helpers. The corpus scopes mostly span whole function bodies, so the inline nested-lifetime count may understate real code. That costs P a helper per site and costs H nothing, because `scope` is inline.

### 8.2 Blind readers

Packets are in `primitives/blind-reader/<option>/` for today, H, P and A–E. Each holds the three programs with comments removed, plus the option's `library.ef` for H, P, C and D, and the same 12 questions. The answer key is `blind-reader-key.md`. Its Q11 scoring now uses the §8.1 classification rather than each option's own claim (review 1, m1).

**Round 1** (n = 1 per option, Sonnet, scored by review 1):

| Option | Total /12 | Weighted (Q1, 4, 7, 11, 12) /5 | Notes |
|---|---|---|---|
| today | 11 | 4 | Q11: read the compiler-known dots as library |
| P | 10.5 | 3.5 | Q1 hedged; Q11: read `.provide`/`.scoped`/`.fork`/`.catch` as "probably built in" |
| A | 12 | 5 | found the clause header ambiguous |
| B, C, E | 12 | 5 | |
| D | 11 | 4 | Q11: read `provide()`/`provideService()` as library |

Pattern (weak evidence): readers who saw a keyword or block form classified it correctly, and readers who saw a dot or call form could not tell compiler-known from library, in either direction. That is the pattern H is built on. **Round 2** (n = 3 per option, including H) is run by the orchestrator; results are pending.

### 8.3 Prototype

Branch `wf10/spike-syntax-proto`, head `bc263cf8` ("feat(spike): prototype option H owner blocks for #134"), cut from `wf10/spike-syntax` at `effb24d4`. It replaces round 1's P keys:

- **Parser** (`syntax.go`):
  - `race { fork a  fork b }` lowers to the race kernel; a non-fork arm is EF002 "each arm of a race block is a fork".
  - `provide L, S = p { run X }` lowers to today's provision.
  - `interrupt child` lowers to today's interrupt.
  - A fork-bound name under `run` lowers to today's join.
  - A `fork` outside every owner block is EF002 "fork needs an owner".
  - The P method keys (`.fork()`, `.scoped()`, `.race()`, `.timeoutOr()`) are removed.
- **Runtime** (`runtime/effra/effect.go`): `raceWith` takes an `otherAbandoned` flag. Under external cancellation, race now strips ordinary failures from both sides, while timeout keeps its timer policy (review 1, m11).
- **Clock change reverted.** Timeout charges `Scheduler` again, as today (`timeout.mjs`, `emit_go.go`, `reachability.go`, `semantic.go`). The library deadlines charge `Clock` because they sleep on it (O6).

| File (prototype dir) | Go | JS | Output equals today's |
|---|---|---|---|
| `dashboard.ef` (worked example; provide block, race block, `run account`) | `live / live` | same | yes |
| `concurrency.ef` (scope, `run child`, `interrupt`, library deadline) | `child joined` / `timed out` / `recovered` | same | yes |
| `layers.ef` (provide block in `main`) | `fixture:fixture` | same | yes |
| `race-library.ef` (race block; `after`, `expire`, `deadlineOr`, `deadline` in `.ef`) | `fast` / `failure won` / `abandoned loser` / `gave up` / `work` / `deadline` / `outer` | same | n/a (new) |
| `walls.ef` | `ef check`: EF127 for generic `timeoutOr` (#128 Q9). `outside` and `inside` check, showing both deadline placements. | | the wall is the evidence |

**Tests** (`internal/compiler/spike134_test.go`):
- the four programs on both targets;
- fork needs an owner, with a legacy file still admitted;
- an escaping handle is EF123;
- an unobserved child is EF107;
- catch on a joined child is admitted;
- interrupt carries the child's row (EF107 without `raises { Bad }`);
- race arm shape, arm result type and union row;
- the provide block with EF108 for a missing service.

**Suite.** `go test ./internal/compiler ./runtime/... -count=1` passes in full on the prototype branch (compiler 343 s). Round 1's 22 Clock failures are gone with the revert, so the ownership truth matrix runs again. Round 1 had no prototype tests of its own (review 1, m10).

**Migration measurement.** With the owned-fork rule forced on for every file, 32 existing top-level tests (61 subtests) fail, every one with EF002 "fork needs an owner" on a fork outside a scope. One of them checks `examples/causal.ef`, the only example file affected.

## 9. Decision record

**Chosen set and syntax (H).**
- **Constructs:** `effect fn`, `run`, `fail`; `service`, `impl`, `layer` (and machines); `scope { }`; owned `fork`; `run child` as join; `interrupt child`; `race { fork … }`; `provide Plan { }`.
- **Proposed construct:** `all { fork … }` (P14, after O3).
- **Notation:** duration literals (`ms`, `s`, `m`).
- **Library:** the timeout family, `after`, `expire`, `either`, `delay`, `retry`/`Schedule`, `.provideService<S>(p)`, `.catch`, `.recover`, `.orFail`.
- **Host primitives:** P1–P12 unchanged, with P13 race added.
- **Retired:** `.timeout` as a keyed form (after U9), `.provide(Layer)` and `.provide<S>` as dot forms, `.join()`, `.interrupt()` and `.cancel()`.

**Losing options:** P (§7.1); A, B, C, D, E (§7.2).

**Effect on tickets:**
- **#95** (reserve postfix words): **superseded.** Nothing is reserved after a dot. The keywords are leading words.
- **#132** (provide as library): **re-scoped** to `.provideService<S>` only. `provide(Layer)` stays a construct as the provide block, and the rows note's NO-GO is upheld.
- **#126** (F3, row difference): needed for `.provideService` only.
- **#127** (receiver methods), **#128 Q9**: needed for the generic library.
- **#128 Q11**: no longer load-bearing for any guarantee; it affects only lossless wrapping of owned-value recipes.
- **#130** (4c): not needed by #134.
- **#131, #133** (catch, recover): unchanged.
- **#106** (non-escaping block closures): would allow owner values as parameters (O4).
- **#124, #125:** receive K1–K17, A1–A4, P13 and P14.
- **#121:** a UI frame runtime is a named provider client (§6.8).
- **`lawful-runtime-contract.md:63`:** to amend in U1, since race becomes P13 with block syntax and provision has block syntax.

**Follow-up units** (implementor-days are estimates):

| Unit | Work | Days | Order |
|---|---|---|---|
| U1 | P13 race kernel: n-ary race block in both runtimes, symmetric abandonment, K1–K5 law tests; amend `lawful-runtime-contract.md:63` | 3 | now; the prototype exists |
| U2 | Duration literals plus a nominal `Duration`; the formatter prints literals | 2 | now |
| U3 | Provide block, replacing `.provide(App)`; graph, lint and inspection read the header | 3 | now |
| U4 | Owned fork, `run child` and `interrupt child`; `ef fix` wraps the forks in the 32 tests, `causal.ef` and user code in `scope`; drop `.cancel()` | 3 | after O1 |
| U5 | Formatter and LSP for the new blocks (hover, folding, completion of plan entries) | 2 | with U3, U4 |
| U6 | Library timeout family, `either`, `delay`, `retry`/`Schedule` in `.ef` | 3 | after #127, #128 Q9 |
| U7 | `.provideService<S>` as library | 1 | after F3b (#126, #132) |
| U8 | Q11 closed-at-the-edge contract, measured on the 29 programs | 5–8 | inside #128; not on H's critical path |
| U9 | Retire keyed `.timeout` | 1–2 | after U6 and the §6.7 gate |
| U10 | `all { }` block (P14) with A1–A4 law tests | 3 | after O3 |
| U11 | Constructs as a provider protocol (a `Runtime` provider interface plus the RS1 conformance suite); first non-Effect provider is the deterministic test runtime | 8–10 | after U1, with #124 |

**Open owner questions** (each with a recommendation):

- **O1. Adopt H?** Owner blocks (`scope`, `race`, `provide`), owned `fork`, `run child` and `interrupt child` as constructs, with timeout, retry and `provideService` as library. *Recommend yes.*
- **O2. Superseded.** Round 1 asked whether fork charges a row. Owned fork replaces the question.
- **O3. Group failure policy.** *Recommend* the fail-fast `all { }` block (P14, §6.6) rather than a library intrinsic.
- **O4. Owner values as parameters** (Trio nursery, Loom scope, Eio switch), so a helper can fork into its caller's owner. *Recommend deferring* to #106, since the value must be second-class. H stays lexical-only.
- **O5. Duration units.** *Recommend* `ms`, `s` and `m` only.
- **O6. Clock or Scheduler for deadlines.** *Recommend:* today's keyed `.timeout` keeps charging `Scheduler` until U9, and the library deadlines charge `Clock` because they sleep on it. U9 then decides Q5 by retiring the keyed form.
- **O7. Resources keep the dynamic owner.** *Recommend yes.* EF123 already keeps them from escaping, and a lexical rule would need an owner at every acquisition.
- **O8. Drop `.cancel()`** (request cancellation without waiting). *Recommend folding it* into `interrupt` and owner close. No corpus program needs it, and a cancel without waiting leaves a child whose cleanup nobody observes.
- **O9. Rename `.provide<S>` to `.provideService<S>`.** *Recommend yes,* so the keyword and the library function never share a word.

## 10. Direction changes

**Round 1 (2026-10-12).** The spike started with a block-construct hypothesis, leading option C. Two owner directions changed it:
- (a) no reused keywords such as `else`, and primitives plus library by default;
- (b) a guardrail test and a portability test.

So `else` became `otherwise` in A and B, option P was added and chosen, the schedule idea was evaluated, and the kernel was framed as a protocol.

**Round 2 (2026-10-12, after review 1).** Review 1 returned changes: 1 Blocker, 6 Major, 11 Minor and 5 Optional findings. Three of them reversed the choice:
- B1: P's dot forms for fork, join and interrupt have no carrier;
- M1: Q11 is not shown;
- M6: the guardrail test was not run on fork ownership.

The coordinator asked for a hybrid. Option H keeps P's library layer (timeout, retry, provideService, catch) and P's laws. It puts each guarantee in a construct: owned fork, `run child`, `interrupt`, race and provide. H was then measured on the corpus, prototyped on both targets with tests, and given blind-reader packets.

The other findings are fixed in place:
- M2: interrupt's row and K13;
- M3: K7 and K9 restated, race flattening removed;
- M4: provide upheld as a construct;
- M5: the `all` block;
- the minors and optionals.

P and A–E are kept as the losing comparison.
