# Primitives and constructs (#134)

Status: research note and decision record for wayfinder ticket #134 (run wf10, unit spike-syntax), 2026-10-12, round 4 after review 3 and the orchestrator's re-plan. Owner decisions are still open where marked **O*n***.

Appendices:
- [primitives/effect-study.md](primitives/effect-study.md): Effect 4 kernel and derived operations, with file:line.
- [primitives/prior-art.md](primitives/prior-art.md): the wider prior art.
- `primitives/corpus/<option>/`: the corpus rewritten per option (H, P, A–E, and today).
- `primitives/corpus-main/<option>/`: the examples main added after the corpus froze (machine decisions, scheduler quiescence, latch handoff), for today and H only (§8.1).
- `primitives/blind-reader/<option>/`: blind-reader packets; the answer key is [primitives/blind-reader-key.md](primitives/blind-reader-key.md).
- `benchmarks/syntax/measure.ts`: the corpus metrics and the one classification of forms that every option is scored against.
- The prototype lives on branch `wf10/spike-syntax-proto`, under `docs/research/primitives/prototype/`. Head `110fa3da` prototypes option H after review 4; `96155c4f` was round 4's head, `0ef605b2` round 3's, `bc263cf8` round 2's H, `073de983` round 1's option P, and `e11047a3` adds the baseline probes run with the main compiler. That branch is never merged.

## 0. Decision in one page

**Chosen: option H, owner blocks plus library.** A form is a construct when it passes the guardrail test (§1): it makes an illegal state unrepresentable, and neither a library function nor a general type feature can do the same. A form is also spelled as a construct when no general feature can carry it, so the checker must know it by name (the visual rule, §4). Everything else is ordinary library. Every construct is a keyword or block form. Every dot form on the concurrency, lifetime, time and provision surface is honest library: a function written in Effra, or a primitive whose contract the accepted vocabulary can state.

**Constructs**
- Evaluation and control: `effect fn`, `run`, `fail`.
- Declarations: `service`, `impl`, `layer` (and machines, outside this ticket).
- `scope { … }`: an owner block. Whatever it owns closes when the block ends.
- `fork recipe`: starts a child. It is legal only inside an owner block of the same function, so there is no ownerless spawn and no child outlives the function that forked it.
- `run child`: a fork handle denotes its join recipe, so running it waits for the child and gives its result. Which names are handles follows their type, not their spelling (§6.3).
- `interrupt child`: cancels the child, waits for its cleanup, and raises the child's retained failure, so its row is the child's `raises { E }`.
- `race { fork a  fork b … }`: an owner block whose value is the first arm to complete. The losers are cancelled and cleaned up before the block yields.
- `provide App, Clock = LiveClock { … }`: a provision block whose header is a static plan, read by graph, lint and inspection at each site. **Every provide block owns its body**, whatever its entries, so a `fork` directly in the body is owned by the block (§6.5).
- Proposed, not prototyped: `all { fork a  fork b … }`, a fail-fast group owner (P14, §6.6).

**Not adopted: provision-lifetime workers.** Round 3 added a `layer` entry `fork w(args)`. Review 3 found it unsound: a worker failure and a body failure form a composite cause that no row declares (B1), and a diamond merge starts the worker twice (Major 3). Workers are split out into their own design unit, **U-workers** (§6.5, §9). H does not cover the gap review 2 named (M4) until that unit lands.

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

H keeps each guarantee in a direct construct that today's checker already enforces. It moves only what really is library into library. Review 2 accepted the choice and asked for the claims and gaps fixed below; the blind readers scored H 12 of 12, with a readability finding about `run` (§8.2, O10).

Three guarantees are new:
- **No ownerless child.** A `fork` outside every owner block is a syntax error (EF002 on the prototype).
- **The deadline is library, and nothing is lost if the library is late.** `timeoutOr` is a function over a race block. If the general features it needs slip, users write the race block inline, and every guarantee holds.
- **Each provide site has a checked plan and one ownership contract.** The plan is written at the site, so tooling never meets a provide whose plan it cannot read, and nothing the body acquires escapes the block.

**Cost.**
- H adds three keywords (`race`, `provide`, `interrupt`), giving 31 against today's 28.
- It is 4.5% fewer tokens than today, with zero construct lookalikes on the #134 surface. `.as<T>()`, a host type assertion outside #134, stays compiler-known in every option and is counted apart (§8.1).
- **Migration (§8.3).** Forcing the owned-fork rule on every file makes 33 existing top-level tests fail, every one because a fixture forks outside a scope (EF002). The `ef fix` prototype wraps a fork-to-join region only when nothing inside the new scope runs except the owned forks of pure operands, their plain joins (`run h.join()`, `run h.interrupt()`) and pure local values. It never trusts a call by its name or by the purity of its operands: a handler (`catch`, `recover`, `orFail`) or a `run` of any service or function in the region, `Console.log` and `Clock.sleep` included, sends the region to manual migration with a reason (review 4, Major 1). For exactly those regions it preserves:
  - **preserved:** outputs, exits, failures, the owner's close order and the resources each owner holds, because the only code that runs in the new scope is the forked recipes, each in its own child owner as before, and the joins;
  - **not preserved:** the owner tree that `Runtime.inspect` reports after the region, where a joined child moves from the enclosing owner to the new, closed scope (review 3, f2; asserted by a test).

  On the 1132 distinct sources the forced suite compiles, it changes no legacy diagnostic, and every program it wraps fully or partly that has a `main` runs the same original against fixed: 18 on Go (15 wrapped fully, one of them with the documented owner-tree difference, and 3 partly) and 13 on JavaScript (10 and 3). The 5 wrapped sources that bind Go natives (EF110) are counted on Go only. The rule is narrow on purpose: it wraps 16 sources in full and 3 in part, and reports the other 1113 for manual migration, since most sources fork into the caller's owner deliberately or run other work between a fork and its join.

**Portability.** Each construct lowers to a protocol: the host primitives P1–P12, the new race primitive P13, and laws K1–K17 (§6.8), each with a probe or a stated reason why not. The Effect runtime and the Go runtime are two providers. A deterministic test runtime, a UI frame runtime, a request-scoped server runtime and an embedded tick loop are further providers that need no source change (follow-up U11).

**What still depends on unbuilt features.**
- The generic timeout family needs receiver methods (#127) and whole-value type parameters (#128 Q9). The prototype's `walls.ef` reproduces EF127.
- `.provideService` needs row difference (F3b, #126).
- Lossless wrapping (K11) of resource-producing recipes needs Q11. Without it, such a wrapper does not reach parity with the inline form: on the rows note's 29 programs, 12 are refused inside the wrapper and 17 get a different verdict. Those programs write the race block inline. No guarantee rests on Q11.
- Owner values as parameters (O4), a recorded limitation of the lexical rule, not a library gap.
- Provision-lifetime workers, which are the U-workers unit (§6.5).

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
- **Scope, acquireRelease and provide are library.** `Effect.scoped` is a root scope that removes `Scope` from R (`:4100-4110`). `acquireRelease` adds `Scope` (`:4134-4150`). `Effect.provide(layer)` is `scopedWith` plus `buildWithScope` (`internal/layer.ts:8-22`). `buildWithScope` reuses the current MemoMap through `CurrentMemoMap.forkOrCreate` (`Layer.ts:582-586`, `:765-775`); only `{ local: true }` gets a fresh map (`internal/layer.ts:17-18`). So nested non-local provides share builds, and separate root runs build twice (test "subsequent calls share MemoMap", `Effect.test.ts:4644-4660`). A layer provide creates a scope and a service provide only updates context: Effect names the split that H's provide block removes (§6.5).
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

The detail and pins are in [prior-art.md](primitives/prior-art.md); §5 there covers round 3's lifetime questions.

**Structured concurrency makes an ownerless spawn unrepresentable.** Every library in the survey that guards lifetimes requires an owner value or type to spawn:
- Trio's `nursery.start_soon`;
- Loom's `StructuredTaskScope.fork` (JDK 26, JEP 525), with structure violations checked at run time;
- Eio's `Fiber.fork ~sw` (`eio__core.mli:258`);
- Kotlin's `launch`, an extension on the `CoroutineScope` receiver.

These bound a task by **the owner it was handed**, not by the function that spawned it: a helper given a nursery, switch or scope can start a task that outlives the helper (review 2, M4). Swift's `async let` is the construct form, where the child is bound to its block and awaited or cancelled at block exit. Go's `go` and Effect's `forkChild` are the outliers, where a child can outlive its caller.

**H's owned fork** is the construct form and is stricter than the owner-value libraries. Owners cannot be passed (an owner value would be #106's second-class value, O4), so a child is bounded by its function. The longer lifetimes that owner values buy are not part of H. They come back as two separate units:
- provision-lifetime workers, as in Elixir supervision trees and Effect's `Layer.effect`/`Layer.effectDiscard` with `forkScoped` (v3: `Layer.scoped`), are the U-workers unit (§6.5, §9);
- owner parameters later (O4).

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
- Gleam keeps its constructs few and distinct. `use` passes the rest of the block as a callback, and the v0.25 release post and the language tour both warn about overusing it. That is the "rest of the block is an argument" problem, which D shares. Long-lived workers are OTP processes under supervisors (gleam_otp), library over the BEAM.
- ReScript emits native `async`/`await` (`js_dump.ml:392`, `:1042`), so a promise has no owner at all.
- Elixir is the "don't copy" for the visual rule. `if`, `unless` and `|>` are Kernel macros, while `with`, `for` and `case` are special forms, and a reader cannot tell a construct from library by its shape. Elixir is the reference for provision-lifetime workers, though (input to U-workers): an application's supervision tree starts its children in order, stops them in reverse, and chooses a restart strategy per supervisor.

**Lowering.** H follows ReScript and Gleam: when a construct has a host equivalent, it lowers straight to it, with no interpreter layer.
- The race block becomes `Effect.raceFirst` or `er.Race`.
- A scope becomes `Scope` or `er.Scoped`.
- A provide block becomes the existing provision plus a scope around its body (`__ef_scoped`, `er.Scoped`).

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
| `run child` (join) | No new illegal state. It keeps today's checks: an unobserved child's failure must be declared at its owner (EF107, R4), the handle cannot escape (EF123), and join raises the child's failures. | No carrier: no clause observes a `Fiber` value (B1), so a dot form `.join()` would be a compiler-known name wearing method syntax. | **Construct by the visual rule** (§6.3, review 2 m4): `run` applied to a handle |
| `interrupt child` | No new illegal state. It keeps today's checks: interrupt carries the child's failure row (`semantic.go:5513-5515`), and the handle cannot escape (EF123). | As for join. | **Construct by the visual rule**, row `raises { E }` (§6.3) |
| `.cancel()` (today) | none beyond interrupt | n/a | **Dropped** (O8) |
| `race { }` | A loser that keeps running after the race yields. A loser's abandoned typed failure leaking into the owner's outcome (K3). | Not over `scope` + `fork`: a scope keeps an unobserved child's failure on a normal close (R3), and race must discard it; first completion needs a host primitive (P13). Not a function: its arms are forks, which must be lexically owned. | **Construct** (§6.4) |
| `.timeout(d)` (today) | none of its own: its guarantees are race's (owner) and Clock's (rows) | Library over a race block, as in Effect, Eio, Koka, cats-effect and MoonBit. | **Library** (§6.7) |
| `provide Plan { }` | **A provide site with no checked plan.** Graph, lint and inspection read the plan at each site (`graph.go:474`, `lint_facts.go:311-319`). A missing service at build time (EF108). A resource acquired in the block escaping it (EF123, for every header). | No. A first-class `Layer<Out, E, In>` value carries rows, not the plan (`effect-rows-as-types.md:961`; review 1, M4). | **Construct** (§6.5) |
| layer `fork w(args)` (round 3) | A provision-lifetime worker with no owner, or one that outlives the nodes it uses. | Not a function: the owner is the provision, which no value names (O4). But round 3's entry was unsound: an undeclared worker-plus-body composite (review 3, B1) and a double start under diamond merge (Major 3). | **Split out: U-workers** (§6.5, §9) |
| `.provideService<S>(p)` | none beyond rows | F3b: `uses { R - S }` (#132) | **Library** |
| `.catch`, `.recover` | none beyond rows | F2, F3b (#131, #133) | **Library** |
| `.orFail()` | none beyond rows | ordinary data once GoResult is | **Library** |
| `acquireRelease` | Release that runs zero times or twice (R10, P5). | A host primitive with a `Scope` row. | **Host primitive** (P5), library-shaped |
| `all { }` | A sibling that keeps running after the group has failed. A composite of the first failure and siblings' body failures that no handler can match. | Not as library: the composite cause cannot be declared (`docs/runtime.md:17`; `baseline/both.ef` EF107 on the main compiler). | **Construct, proposed** (P14, §6.6, O3) |
| `defer e` (Go-like) | n/a | `acquireRelease` and `onExit` cover it | Not proposed |
| duration literal `2s` | none | 1:1 notation for `Duration.ms(2000)`; a second client is byte sizes | **Notation** |
| `layer` declaration | The static plan: construction graph, `merge`/`replace`, missing and duplicate services inside the layer. | None: tooling must read the structure. | **Construct** (unchanged) |
| `.as<T>()` (host assertion) | Outside #134: Go's `v, ok := value.(T)` for a native host value (`syntax.go:1652-1660`). | None today; it is compiler-known in every option. | **Outside #134**, counted apart in §8.1 (review 2, m5) |

**The illegal states the owner named:**
- **A fiber handle used after its owner ends; a resource escaping its lifetime.** Ruled out today by EF123 for every owner block. H keeps it with the same checker, with no new feature needed (prototype test `TestSpike134OwnedHandlesKeepTodaysOwnershipRules`), and extends it to every provide block (`TestSpike134ProvideBlockIsAlwaysAnOwner`).
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
             | Provide
ForkArm      = "fork" Expr
Fork         = "fork" Expr                       (only inside an OwnerBlock of the same function)
Join         = "run" Expr                        (Expr has type Fiber<A, E>; §6.3)
Interrupt    = "interrupt" Ident                 (a statement; Ident has type Fiber<A, E>)
Provide      = "provide" PlanEntry { "," PlanEntry } Block
PlanEntry    = LayerName | ServiceName "=" ProviderExpr
```

- `race`, `provide` and `interrupt` are new reserved words. None appears as an identifier in `examples/`, and after a dot (`x.provide`) they remain ordinary member names.
- Durations are literals: `ms`, `s` and `m` (O5).
- No block reuses a keyword. `else`, `or`, `with` and `after` stay free.
- A `layer` has no `fork` entry. Round 3's worker entry is withdrawn to U-workers; the prototype refuses it with EF002 unless the recording flag is set (§6.5).

### 6.2 Owned fork (review 1, M6)

**The rule.** A `fork` is legal only lexically inside an owner block (`scope`, `race`, `provide`, or the proposed `all`) of the same function. A fork directly in a provide body is owned by the provide block (review 3, Minor 2; `TestSpike134ProvideBodyOwnsAFork` prints `tick,joined ticked,ticked,after` on both targets). On the prototype this holds for service-only headers; for a layer header it is design only, because the prototype's layer-header block still takes one `run` statement (§6.5). A fork outside every owner block is a syntax error: "fork needs an owner: put it inside scope { } or make it a race arm" (prototype EF002). Because the owner block closes before the function returns, no child outlives its function. A fork inside an `if` or a loop inside a scope is still owned.

**The guardrail test.**
- *Illegal state:* a child still running after the function that started it has returned. Today `fork` outside a scope uses the dynamic owner (R9), and probe `g1-today.ef` shows the child outliving its caller. P's corpus made this more common by moving lifetimes to callers (`corpus/P/latest-task.ef:18`, `lifecycle.ef:23`, `causal.ef:34`).
- *Library alternative:* the owner-value form (Trio, Loom, Eio, Kotlin). Those bound a child by the owner it was handed, so a helper can start a child that outlives the helper. In Effra the form needs an owner value that cannot escape, which is #106's second-class value (O4). A `Scope` row on `fork`, discharged by `.scoped()`, makes ownership visible in the row but does not stop a caller from choosing a longer lifetime. It also needs Q11 to discharge the row losslessly.
- *Verdict:* construct. Swift's `async let` is the precedent: the child is bound to its block.

**What the lexical rule gives up** (review 2, M4). A child cannot be bounded by an owner its function was handed. Two cases matter:
- **A helper that forks into its caller's owner.** The workaround is to return the recipe and let the caller fork it. Owner parameters are O4.
- **A provision-lifetime worker.** H does not cover it. Round 3's layer worker entry was withdrawn after review 3 (B1, Major 3), and the case is the U-workers unit (§6.5). Until it lands, a long-lived worker is a fork in the provide body, which the provide block owns, written at each site rather than in the layer. For a layer header that stop-gap is design only on the prototype (one `run` statement, §6.5).

**What stays dynamic.** Resource acquisition (`Files.openRead`) keeps the dynamic owner. A resource is inert data owned by the current owner, and EF123 already stops it escaping. Making resource acquisition lexical too would need an owner for every acquiring call (O7).

**Edition** (review 2, m1). On the prototype, the owned-fork rule and the handle reading of §6.3 apply to a file that uses H's syntax (a race block, or the `provide` or `interrupt` keyword), or to every file with `EFFRA_SPIKE134_OWNED=all`. This inference is prototype-only: adding an unrelated provide block to a legacy file turns an untouched fork into EF002 (review 2, probe p2-spooky).
- **Production switch:** an explicit edition per package, declared once, never inferred from content. Precedents: Go's `go` directive in `go.mod`, Rust's per-crate edition, Kotlin's `-language-version`, and Swift's and Java's explicit selection (prior-art.md §5).
- **Migration:** `ef fix` moves a package to the new edition. It wraps only regions in which nothing but owned forks and their joins runs, and reports the rest (§8.3, U4). Effra has no package manifest yet, so where the edition is declared is O13.

### 6.3 Fork handles: `run` joins, `interrupt` raises (review 1, M2; review 2, M2, m2, m3)

A fork handle denotes its join recipe. **`run child` joins**: it waits for the child and gives its result, or raises its failure. This is the reading of Rust tokio's `JoinHandle` (a `Future`), a JavaScript `Promise` and Swift's `async let`. Effect 4 is the exception: its `Fiber` is not an effect (`Fiber.ts:71`).

**The reading follows types, not names** (review 2, m3). Every read of a local of type `Fiber<A, E>` denotes its join recipe, except where the handle itself is meant:
- the operand of `interrupt`;
- a `let` whose whole value is the handle (`let alias = child`), and a branch or match arm that yields it (`let chosen = if left { a } else { b }`). These move the handle; they do not join it;
- the receiver of today's fiber methods during the transition (`child.join()`).

A handle bound through an `if` therefore joins with `run`, and an aliased or selected handle can still be interrupted (`TestSpike134HandleReadingFollowsTypes`). The prototype decides this in the checker, from the local's type and the binding's identity. Tooling keeps the source token, so a query at a handle read still names its binding.

Because a handle denotes a recipe, recovery works on it in place (`run child.catch<Bad>("recovered")`), and a handle passed where a recipe is expected passes its join recipe (`run deadlineOr(child, 1s, "deadline")`, same test).

**Teaching example** (review 2, m10, probe p1). `run c` twice observes one child, while `run w` twice runs `w` twice:

```effra
scope {
    let c = fork work()   // work starts once, here
    let a = run c         // waits for the child
    let b = run c         // the same child: no second run
    let d = run work()    // runs work again
}
```

The blind readers found this double reading of `run` the one surprise in H (§8.2). O10 records the choice: keep `run child`, teach it with this example, and add an LSP hover that says "joins child `c`".

**Racing a handle races its observation** (review 2, m2). `race { fork c  fork after(5ms, "deadline") }` forks `c`'s join recipe as an arm. When the timer wins, the race interrupts the joiner, not `c`. The child keeps running under its own scope and can be joined later (`TestSpike134RacingAHandleRacesItsObservation`: "deadline / child finished"). Likewise, a deadline on `run c` cancels the wait, not the child. Refusing handle arms would cost composability, so the note states it instead.

**`interrupt child`** is a statement. It requests cancellation, waits for the child's cleanup, and raises the child's retained failure. Its row is the child's `raises { E }`, matching what both runtimes and today's checker already do:
- Go returns the cause when it is more than interrupts (`runtime/effra/fiber.go:171-176`).
- JS `__ef_interrupt` re-raises it (`prelude/interrupt.mjs:6`).
- The checker gives interrupt the child's failures (`semantic.go:5513-5515`).

Prototype receipt: without `raises { Bad }`, an interrupt of a failing child is EF107.

**The handle cannot escape its owner** (EF123; prototype test). It cannot be passed as a parameter, stored in a payload or returned past its owner (`docs/runtime.md:17`).

**A child owns what it acquires** (review 2, M2; decision O11). A forked recipe runs under its own owner (`prelude/fork.mjs:4` wraps it in `__ef_scoped`; Go forks into a child `Scope`). So:
- a resource the child acquires and does not return is released when the child completes, before `run c` returns;
- a result that holds an owned value cannot leave the child (EF123; probe p4b, `TestSpike134LawK12ForkJoin`).

Under `run w`, by contrast, the resource stays with the caller's owner until that owner closes (`TestSpike134LawK12ResourceLifetime`: the direct owner holds one resource, the joined owner none and a `Done` child). This is the choice of Trio, Kotlin, Swift task groups, Eio's own switches and Elixir processes, where a task's resources die with the task.

*Rejected alternative:* a declared handoff, where the child's unreturned resources stay alive in the caller's owner. Effect does not do this by moving resources at join (review 3, Minor 3). Its pinned `forkIn(scope)` (`internal/effect.ts:5594-5637`) adds an interrupt finalizer for the child to the target scope at spawn and removes it when the child completes, so it binds the child's lifetime and transfers no resources; `forkScoped` (`:5639`) is `forkIn` on the context's scope. Effect 4 has `Scope.provide` (`Scope.ts:309`) and no `Scope.extend`. Effect's real alternative is shared provision of a scope: a child's `acquireRelease` registers its release in whichever `Scope` the child's context supplies, so a child run under a provided outer scope leaves its resources there. Effra's version would be a child that acquires into a named outer owner. Reasons for rejecting it:
- It makes a completed child keep resources alive in an owner other than its own, which needs the child's release path to run in that owner when the child is interrupted instead.
- It needs the target owner as a value (Effect passes a `Scope` through context), which is O4.
- It buys only release timing for resources the child does not return. Returned owned values are already refused statically.

Laws K5 and K12 state these side conditions (§6.8).

### 6.4 The race block

`race { fork a  fork b … }` is an owner block with n fork arms. Its value is the exit of the first arm to complete, success or failure. Its row is the union of the arms' rows, because any arm may win (prototype test). Every loser is interrupted, and its cleanup completes before the block yields (K2). The arms the race owns are the forks written in it. An arm that forks an outer handle owns only that handle's observation (§6.3, review 2 m2).

**The outcome is fixed when the winner is selected** (review 2, m7). Round 2's Go `raceWith` re-checked parent cancellation only when the right arm won. So a failing winner followed by parent cancellation during a slow loser cleanup kept its failure on the left and lost it on the right, against K4. The prototype now takes a symmetric race policy:
- it records parent cancellation at the moment of selection, in both branches;
- if the parent was already cancelled then, both sides are abandoned alike;
- otherwise a **failing** winner's failure is published, whatever happens to the parent during the loser's cleanup;
- a **successful** winner of a parent cancelled during the loser's cleanup is observed as interruption, not success (review 3, Minor 4). Go's `Invoke` turns a successful exit into `Interrupt` when the fiber's checkpoint shows cancellation after the program returns (`runtime/effra/effect.go:216-219`), Review 3's Counsel run observed the same on JavaScript in both arm orders; that half is an observation, not tested here, because the prototype surface cannot cancel a parent during a loser's cleanup. The race keeps the classification fixed at selection and symmetric; it does not promise to publish a success the parent can no longer receive.

The timeout keeps its timer policy. `runtime/effra/race_test.go` drives this with a latch, in both arm orders for each case:
- `TestRaceClassifiesItsOutcomeAtSelection`: a failing arm wins, the loser's cleanup waits on the latch, the parent is cancelled during that cleanup, and the result is the winner's failure. The arm-reversed case fails on round 2's code.
- `TestRaceSuccessfulWinnerOfACancelledParentIsObservedAsInterruption`: the same with a successful winner, observed as interruption.
- `TestRaceCancelledBeforeSelectionAbandonsBothArms`: the race is entered and both arms are running (an ordering point on arm-entry signals), then the parent is cancelled before either completes. The result is an interruption, over seven arm combinations (gated success, gated failure, `never`), and the race publishes only after both arms have finished: every case logs race entered 1, arms started 2, arms finished 2 (review 4, Minor 1). Round 4's version cancelled the context before the run, so `Invoke`'s first checkpoint refused it before the race was entered, and every case tested the same guard.

The JS race applies one code path to both arms (`prelude/race.mjs`), so it has no arm asymmetry.

**Why it is not library over `scope` + `fork`.** Inside a scope, an unobserved child's typed failure is raised at a normal close (R3). A race must discard a loser's typed failure even when the loser fails between the winner's completion and the close (K3). That needs its own owner policy and a first-completion primitive. Effect derives `raceAll` from `withFiber`, `callback` and `forkUnsafe` (`internal/effect.ts:1603`, `:1657`), but Effra has no general callback or mutable cell in the language, so first completion is host primitive P13. Go `select` and cats-effect `racePair` (`GenSpawn.scala:354`) make the same choice.

**Why it is not a function `either(a, b)`.** The arms are forks, and forks must be lexically owned. A library `either` over the block is still possible, and `corpus/H/library.ef` defines one.

**Not associative.** Under K2, `race { fork race { fork a  fork b }  fork c }` and `race { fork a  fork race { fork b  fork c } }` can publish different winners when a loser's cleanup is slow (review 1, M3). So the block is n-ary, and K1 is stated over its n arms, not by nesting binary races. The prototype accepts exactly two arms; n-ary is follow-up U1.

### 6.5 The provide block (review 1, M4; review 2, M3, M4)

`provide App, Clock = LiveClock { body }` builds the named layers and providers under a provision owner (R12, R13), runs the body, and closes them.

**One ownership contract** (review 2, M3). Every provide block owns its body, whatever its header holds, so results tied to the block's lifetime cannot escape it. Round 2's prototype gave the two header shapes different contracts:
- a service-only header lowered to plain context provision, with no owner, so a `File` acquired in the body escaped (probe p8);
- a layer header lowered to a provision owner, which refused the same escape (EF123).

Now a service-only header lowers to a scope around the provision. A layer header already gets the provision owner, and inside it the body runs under its own scope (`prelude/layers.mjs:231`; Go `layers.go:490`). `TestSpike134ProvideBlockIsAlwaysAnOwner` probes both shapes:
- escaping a `File` is EF123 for each;
- reading it inside the block runs, with the same output for both shapes and for the scoped library form.

A body that needs a resource after the block acquires it outside, in the caller's owner. Effect names the same split, where a layer provide creates a scope and a service provide only updates context (§2). H removes it.

`corpus/H/ownership.ef` changed with the rule. Its `let file = provide Files = LiveFiles { run Files.openRead(…) }` would now escape, so the read moved inside the block.

**Why it is a construct.** The header is a static plan: a list of `layer` declaration names and `Service = provider` entries. Graph, lint and inspection read it at each site. Lint already fails without a checked plan: "layer provision … has no checked plan" (`lint_facts.go:311-319`). A library `provide(layer)` over a `Layer<Out, E, In>` value would carry rows, not the plan (`effect-rows-as-types.md:961`). A parameter of that type has no unique declaration, and no set of plans can be enumerated across modules.

**No layer values.** H admits no `Layer`-typed parameter. Choosing a layer at run time is written as an `if` with two provide blocks, so every site keeps one plan, and inspection has no unresolved node to mark (`docs/specs/layers.md:110-114`).

**`provideService`.** The entry `S = p` and the library form `work.provideService<S>(p)` agree up to ownership: `provide S = p { run w }` behaves as `scope { run w.provideService<S>(p) }` (K17). The library form exists so that a recipe can be provided where it is built, for example `fork greeting("42").provideService<Directory>(FixtureDirectory)`, without a helper function (`corpus/H/testing.ef`). It is a primitive with a declared contract (#132), needing F3b for `uses { R - S }`. It was renamed from `.provide<S>` so that it never shares a word with the `provide` keyword. Effect uses the same name.

**MemoMap.** Effect shares builds across nested non-local provides (§2). H's provide block builds each named layer once per block (R13). Sharing across nested blocks is a separate optimisation that no current law requires.

**Lowering of a service-only header.** K17 is the lowering itself, not only a law about it. `provide S = p { body }` resolves `p` once, at the header, into a binding no source name can spell, and becomes `scope { let x = p  body' }`, where `body'` is the written body with `.provideService<S>(x)` applied to every recipe it runs or forks, at any depth (`provideToBody` in `syntax.go`). Because the provider is bound before the body, a body local spelled like the provider cannot capture it (review 4, Minor 2: `let Stdout = Silent` in the body leaves `Console = Stdout` in force; `TestSpike134ProvideBlockResolvesItsProviderAtTheHeader`, both targets). A service entry is a pure implementation value with no build and no release, so providing it per recipe is the same as providing it once. A nested inner block is lowered first, so its provider sits closer to the recipe and wins (K10). Handle operations are not wrapped: `interrupt c`, today's `c.join()`, and a bare `run c` whose name the body binds with `let c = fork …`, since a join runs no recipe a provision could reach. If such a name is rebound to a recipe in a nested block, that recipe is not provided and the checker refuses its missing service, so the approximation fails closed. Lint sees one provision per block and service: the generated provisions fold into one fact whose receiver is everything the block's recipes require, so EFL002 `redundant-provision` fires once, and only when no recipe in the block needs the service (`TestSpike134ProvideBlockIsOneProvisionForLint`). A service-only provide block therefore admits any block body, including `let` statements and forks the block owns.

**Prototype limits** (review 2, o2). A provide block with a **layer** header still takes one `run` statement, lowered to the provision chain. Providers are named without arguments. The design admits any block body and any provider expression for both header shapes, and the block is an expression (`let r = provide … { … }`, used across the corpus).

**Provision-lifetime workers: split out to U-workers** (review 2, M4; review 3, B1, Major 3, m5, O12; re-plan decision 1).

*The gap.* Review 2 (M4) asked how a worker can live as long as a provision, as an Elixir application's supervised children or Effect's `Layer.effect`/`Layer.effectDiscard` with `forkScoped` (v3: `Layer.scoped`) do. Under H's lexical rule a child is bounded by its function, and the provide block's own body is the longest owner a function can write. A worker that should live as long as the provision and be declared once, in the layer, has no form in H.

*The proposed form, not adopted.* Round 3 added a layer entry `fork w(args)`, owned by the provision body, started after every node of the plan and stopped before any node is released. Review 3 found it unsound:
- **B1, composite failure.** A worker failure during the body is a retained child of the provision owner, so a worker failure and a body failure close together into a composite cause that no row declares and no handler matches. Two failing workers do the same. The checker had no rule for it, unlike two forks in a scope, which are EF107 (R6).
- **Major 3, identity under merge.** In a diamond (`App = merge(Left, Right)` with both reaching a layer that declares a worker), the worker started once per path.
- **m5 (Minor 5), failure policy and progress.** A worker failure was retained until the body ended: in review 3's `w3-latency.ef` the worker fails at 1 ms, the body runs on for 100 ms, and the provide then fails at its edge and discards the body's success, so for a server-lifetime provision a dead worker surfaces only at shutdown. With an instant body (`w4-merge.ef`) the worker is forked and interrupted without running a step, so the entry guaranteed a start, not progress. Lifetime-owned does not mean supervised (`docs/specs/layers.md:38` disclaims supervision). Deferred surfacing, fail-fast (Kotlin `coroutineScope`, Trio, the proposed `all`) and supervised restart (OTP) were not compared.
- **O12, start versus progress.** Starting after the build means a dependent layer cannot wait for the worker during its own initialisation; that needs a start handshake (Trio's `nursery.start`, Elixir's `init/1`) or node-scoped workers (today's R13, `runtime/effra/layers_test.go:218`).

*What the prototype keeps.* The entry is refused with EF002, "layer worker entries are not part of option H: provision-lifetime workers are the U-workers design unit" (`TestSpike134WorkerEntryIsNotPartOfOptionH`). Round 3's code stays behind `EFFRA_SPIKE134_WORKERS=1` so that U-workers starts from it:
- `TestSpike134RecordedWorkerPrototype` is round 3's K18 program with the flag on (`worker started` / `program log` / `after provision` / `worker failed`, both targets).
- `TestSpike134KnownFailingW1WorkerCompositeIsUncaught` records B1 as a known-failing probe: a program whose worker and body both fail checks clean and exits 1 on both targets with both `Bad` and `Other` in the cause. Its control, two forks in a scope, is EF107.
- `TestSpike134KnownFailingW5WorkerDiamondStartsTwice` records Major 3: the diamond starts the worker twice on both targets; the control without merge starts it once.

U-workers inverts both known-failing probes. Its acceptance is in §9.

### 6.6 The `all` block, proposed (review 1, M5; O3)

`all { fork a  fork b … }` runs its arms in parallel. **Fail-fast:** the first failing arm's failure becomes the block's outcome. The block interrupts its siblings, waits for their cleanup, and discards their ordinary typed body failures. Defects and cleanup failures are kept.

**Result type** (review 2, o1). The block's value is a heterogeneous tuple of the arms' results in arm order: `all { fork a  fork b }` with `a: Effect<A, Ea, Ra>` and `b: Effect<B, Eb, Rb>` has type `(A, B)`, row `Ea ∪ Eb` and requirements `Ra ∪ Rb`. That is the parallel-tuple composition of `docs/design.md:98`. A homogeneous list over a run-time number of children is not a block of written arms. It is a separate library question (a `forEach`-style group) for U10.

**Why it is a construct.** Under today's rule, two forks in a scope where the first fails leave the second as an unobserved child. Its failure joins the first at a normal close as a composite cause, which no handler matches and no row can declare (EF107 at `prototype/baseline/both.ef:14:11` on the main compiler; `docs/runtime.md:17`). The fix is an owner whose close counts as cancellation-aborted once an arm fails. Its abandoned siblings' typed body failures are then discarded, as a cancellation-aborted owner already does (R3; `docs/runtime.md:19`). That is an owner policy, not a function contract.

Prior art: Kotlin `coroutineScope`, Go `errgroup`, and Loom's `Joiner.allSuccessfulOrThrow` (JDK 26).

Laws (P14, handed to #125):
- **A1.** If every arm succeeds, the block yields the tuple of results in arm order.
- **A2.** The first arm failure is the block's primary outcome. Siblings are interrupted and cleaned up before the block publishes.
- **A3.** Siblings' ordinary typed body failures are discarded, so no composite arises from body failures alone. R6 may still add retained cleanup causes (defects and typed cleanup failures) to the primary outcome (review 2, m6).
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

**The transition** (review 2, m8). Until U9 the keyed `.timeout` and the library `timeout` differ in two identities, not only in verdicts:
- the keyed form charges `Scheduler` and raises today's built-in `Timeout`;
- the library form charges `Clock` and declares `pub error Timeout` (`corpus/H/library.ef`).

K6 holds for the library form only.

**Gate and fallback.**
- *Gate:* retiring today's name-keyed `.timeout` (U9) waits for:
  - the generic library (#127, Q9);
  - parity of the library `timeout` with today's on the timeout corpus, including the 29 Q11 programs, in verdicts, row identity (`Clock` against `Scheduler`) and error identity (built-in against library `Timeout`).
- *Fallback:* if Q11 does not land, resource-returning work is timed with an inline race block, and `.timeout` stays keyed until U9 can use the library for everything else.

No owner guarantee depends on Q11: owned fork, EF123, the race owner, provision and interrupt's row are all direct constructs.

### 6.8 Protocol, laws and portability

Each construct lowers to a provider-independent protocol: P1–P12, plus P13 race (and P14 all, when admitted). Effra already works this way for machines, where a checked declaration lowers to a plan that any userland provider executes (`docs/specs/machine-provider-contract.md:7`, `:45`), and for time (`uses { Scheduler }`, `TestScheduler`).

| Provider | fork | race | owner close | sleep | Use |
|---|---|---|---|---|---|
| Effect runtime (JS) | `Effect.forkChild` adapter | `Effect.raceFirst`, then interrupt and await (prototype `race.mjs`) | `Scope` | `Clock` | today |
| Go runtime | goroutine + `Scope` | `er.Race` over a symmetric `raceWithPolicy` (prototype) | `er.Scoped` | `SleepWithDriver` | today |
| Deterministic test runtime | a queued task | decided by virtual time (R14), ties broken by a seed | LIFO close | `TestClock` | today, partly (`TestScheduler`) |
| UI frame runtime (#121; cf. Foldkit, Elm `Cmd`/`Sub`) | a task stepped once per frame | first of an event or a frame-clock deadline | mount/unmount is the owner | frame clock | U11 |
| Request-scoped server runtime | a goroutine bounded by the request | as Go | the request is the root owner; client disconnect interrupts | the request budget (Go `context.WithDeadline`) | U11 |
| Embedded or game tick loop (cf. embassy) | a cooperative task | first completion per tick | static owner tree | tick counter | U11 |

**Laws** (handed to #125; they generalise R1–R15 of `lawful-runtime-contract.md:76-90`). Each is stated with its side conditions (review 2, M2) and has a probe on the prototype, listed after the laws.

- **K1 first completion.** A race block publishes the exit of whichever of its n arms completes first, success or failure (Effect `raceFirst`, `internal/effect.ts:1736`).
- **K2 loser cleanup precedes publication.** Every loser is interrupted, and its cleanup completes before the block publishes (generalises R8).
- **K3 abandonment.** The winner's exit is the primary outcome. Losers' ordinary typed body failures are discarded. R6 may add the losers' retained cleanup causes (defects and typed cleanup failures) to it (review 2, m6).
- **K4 symmetry.** Reordering the arms does not change the result whenever completion order is determined. The outcome is classified when the winner is selected, so a parent cancellation after selection does not depend on which arm won (review 2, m7). A failing winner's failure is published; a successful winner of a parent cancelled during loser cleanup is observed as interruption, in either arm order (§6.4, review 3 Minor 4). No tie guarantee is made across targets (`docs/runtime.md:21`).
- **K5 identity.** `race { fork w  fork never() }`, where `never()` never completes, needs no cleanup and has **empty rows** (`raises { }` and `uses { }`), behaves as `run w` (the same success value, failure and rows). A race's rows are the union of its arms' rows (review 3, Minor 1), so a never-completing arm with a non-empty row adds that row: with the prototype's `hang()`, which sleeps on `Clock`, the race needs `Clock` where `run w` does not. The law's main clause cannot be probed today: every blocking primitive the surface has charges a row (`hang()` sleeps on `Clock`), so no empty-row `never` exists. Effect has one, `Effect.never` with `R = never`; #125 needs an equivalent primitive (or a `never` in the prelude) before the identity can be probed rather than only its union of rows (review 4, optional). The law has two further conditions:
  - `w`'s result holds no owned value. An owned result cannot leave the arm, and that is refused statically (EF123).
  - The difference in resource lifetime is accepted: resources that `w` acquires and does not return are released when the arm completes, before the block yields, where under `run w` they stay with the caller's owner (§6.3).
- **K6 timeout derivation.** For the library form, `w.timeout(d)` behaves as `race { fork w  fork expire(d) }`, and likewise `timeoutOr` with `after` (definitional in `library.ef`). It does not hold for today's keyed `.timeout` until U9 (§6.7).
- **K7 nested deadlines.** For `w.timeout(d1).timeout(d2)`, cancellation of `w` is requested no later than `min(d1, d2)` on one shared clock (R14). The bound covers deadline selection and the cancellation request, not publication: publication waits for cleanup (K2) and may come later. Swift SE-0526 states the same (`0526-deadline.md:127-131`).
- **K8 scope nesting.** `scope { scope { body } }` behaves as `scope { body }`.
- **K9 provide/catch commute.** Moving `.catch<E>(v)` across a provision boundary preserves behaviour when both of these hold:
  - building, releasing and cleaning up the plan raise no failure and no defect (otherwise the outer catch sees a composite cause it cannot match, R6);
  - no finalizer of the plan or the body observes the body's exit (scope close passes the exit to finalizers, `lifecycle.mjs:66`, so an `onExit` finalizer can tell where the catch sat).

  Both conditions are argued from the source, not probed, because the prototype cannot write their counterexamples: EF135 forbids an effectful layer factory, so a plan cannot fail in build or release, and the surface has no user finalizer that observes an exit. The probe is revisited when P5 (`acquireRelease`) and `onExit` reach the surface (review 3, optional; U-K9).
- **K10 shadowing.** The inner provider wins: `provide S = p { provide S = q { run w } }` uses `q`.
- **K11 lossless wrapping.** For a library wrapper `effect fn wrap(x) { race { fork x  fork … } }`, `run wrap(r)` behaves as the inline block, with the same rows and the same ownership evidence. This needs Q11 when `r` returns an owned value (§6.7).
- **K12 fork/join.** `scope { let c = fork w; run c }` behaves as `run w` (the same success value, failure and rows) under K5's two conditions: `w`'s result holds no owned value (an owned result is EF123), and resources `w` acquires without returning are released before the join returns instead of at the caller's owner close.
- **K13 interrupt.** Three cases:
  - after a successful completion, interrupt is a no-op that observes the child;
  - for a running child whose shutdown is interruption only, interrupt waits for cleanup, and the interruption is normalised away;
  - for a child that retained a failure (completed with one, or failed in cleanup), interrupt raises it.
- **K14 schedules do not preempt.** `retry` consults the schedule only after an attempt has completed.
- **K15 placement.** `w.timeout(d).retry(s)` bounds each attempt; `w.retry(s).timeout(d)` bounds the whole loop.
- **K16 owned fork.** A child never outlives the innermost owner block that lexically contains its `fork`. At block exit, unjoined children are interrupted and their cleanup completes (R2, R8). Joining governs observation, not ownership.
- **K17 provide entry.** `provide S = p { body }` behaves as `let x = p; scope { body' }`, where `x` is fresh (no name in `body` can refer to it) and `body'` is `body` with `.provideService<S>(x)` applied to every recipe it runs or forks. So `p` is resolved at the header, a capture-avoiding substitution, and the block is one provision boundary over its body. For one run statement, `provide S = p { run w }` behaves as `scope { run w.provideService<S>(p) }`, since a single statement binds no local before `w`. The block is an owner for every header, and a fork directly in its body is owned by it (§6.5). On the prototype K17 is the lowering of a service-only header, so it holds by construction there, header resolution included; the probes check that the lowering agrees with the layer-header shape and the library form, and that a body local cannot capture the provider.

Round 3's **K18 provision worker** is withdrawn with the worker entry (re-plan decision 1). Its clauses (start after the build, stop before release, failure at the provide edge, rows) are input to U-workers, which must add what review 3 found missing: the composite rule, identity under merge and a failure policy (§6.5, §9).

**Probes** (prototype `internal/compiler/spike134_laws_test.go` unless noted; "both" means Go and JS with equal output):

| Law | Probe | Targets |
|---|---|---|
| K1, K3, K4 | `TestSpike134LawsRaceK1K3K4`: success and failure first, abandoned loser, each in both arm orders; the loser's row stays declared (EF107 without it) | both |
| K2, K4 | `runtime/effra/race_test.go`: `TestRaceClassifiesItsOutcomeAtSelection` (the race does not complete before the latched loser cleanup, and the failing winner survives a parent cancellation in both arm orders); `TestRaceSuccessfulWinnerOfACancelledParentIsObservedAsInterruption` (both orders); `TestRaceCancelledBeforeSelectionAbandonsBothArms` (race entered and both arms started before the cancellation; seven arm combinations; entry and completion counts asserted) | Go runtime. JS: K2 by construction (`race.mjs` awaits both fibers); the successful-winner observation is untested on JS |
| K5 | `TestSpike134LawK5Identity`: equal success and failure, equal failure rows (EF107 on both sides), requirement rows a union (`run w` needs nothing, the race over `hang()` without `Clock` is EF108 `Clock`), owned result EF123 | both |
| K6 | definitional: `deadline` in `race-library.ef` runs | both |
| K7 | `TestSpike134LawK7NestedDeadlines`: the earlier deadline wins in either nesting | both |
| K8 | `TestSpike134LawK8ScopeNesting` | both |
| K9 | `TestSpike134LawK9ProvideCatch`: the positive case for a service header and for a layer header whose plan merges a shared layer (`Plan = merge Shared; Name = FailingOverStore`, a node that uses another node). The side conditions are source-argued: build and release cannot fail (EF135), and no user finalizer observes the exit (revisit with P5 and `onExit`). | both |
| K10 | `TestSpike134LawK10Shadowing` | both |
| K11 | positive case only: `deadlineOr` nested in itself (`race-library.ef`); the owned-value case waits for Q11 (U8) | both |
| K12 | `TestSpike134LawK12ForkJoin` (values, failures, rows, EF123 for p4b); `TestSpike134LawK12ResourceLifetime` (resource released before the join) | both; resource probe Go only (Files and Runtime are native) |
| K13 | `TestSpike134LawK13Interrupt`: no-op after success, interruption normalised, retained failure raised | both |
| K14, K15 | library laws over `retry`, which is not prototyped; probes land with U6 | — |
| K16 | `TestSpike134LawK16OwnedFork` (an unjoined child never prints after its block); `TestSpike134ForkNeedsAnOwnerBlock` | both |
| K17 | `TestSpike134ProvideBlockResolvesItsProviderAtTheHeader` (a body local spelled like the provider, its control, nesting, and an inner local spelled like the outer provider; both targets); `TestSpike134ProvideBlockIsOneProvisionForLint` (no spurious EFL002, one EFL002 for a block no recipe of which needs the service, the handle join unwrapped); `TestSpike134ProvideBlockIsAlwaysAnOwner`: both header shapes refuse an escaping `File` (EF123) and agree with the scoped library form (today's `.provide<S>` stands in for `.provideService<S>`); `TestSpike134ProvideBodyOwnsAFork`: a general body with `let` statements, a joined fork and an unjoined 600 s fork, both owned by the block; the unjoined child is interrupted at the block's close and never prints (`tick,joined ticked,ticked,after`) | Go (Files); fork probe both |
| K18 (withdrawn) | `TestSpike134WorkerEntryIsNotPartOfOptionH` (EF002); recorded under the flag: `TestSpike134RecordedWorkerPrototype`, and the known-failing probes `TestSpike134KnownFailingW1WorkerCompositeIsUncaught` and `TestSpike134KnownFailingW5WorkerDiamondStartsTwice`, which U-workers inverts | both |

**Drift control.** F# builders and `RebindableSyntax` let a library change what syntax means. H fixes the meaning in three ways:
- The operation set is closed: P1–P13 (and P14).
- Their types are fixed by the checker, not by the provider.
- The laws run as a **conformance suite** (#124 RS1). Passing it is evidence, not proof: the checker cannot refuse a provider for failing a finite suite. This differs from an incompatible machine provider, which is a static diagnostic (`machine-provider-contract.md:45`).

Providers choose an implementation. They cannot choose a meaning.

### 6.9 Typing and composability

**Typing.**
- Constructs use the checker's existing owner kinds: child, timeout (reused for race), scope and provision (`semantic.go:160-161`).
- Library functions are ordinary declarations, and no checker branch is keyed on their names.
- The handle reading is keyed on a local's `Fiber` type and binding identity (§6.3), not on a name or a parser desugar.
- TypeScript projection: `{ R - S }` projects to `Exclude<R, S>`, Effect's own type. A row the projector cannot express projects to an opaque nominal type, never `any`.
- Go lowering: rows erase; owner blocks become `er.Scoped`, `er.Race` and provision calls, with the same laws on both targets.

**The five composability checks.**
1. **Wrap losslessly.**
   - Direct use needs nothing.
   - A library wrapper over a race block is an ordinary `effect fn`, and `race-library.ef` nests one inside itself.
   - Wrappers of owned-value recipes need Q11 (K11).
   - A block itself is wrapped by a named function. Including the App build inside a deadline is the one case (`walls.ef` `inside`).
2. **Nest.** Every pair composes (the matrix below). Race does not flatten (§6.4).
3. **Keep recipes first-class.** Every library function takes and returns recipes, and a handle passed as a recipe is its join (§6.3). A block yields a value, so turning a block into a recipe takes a named `effect fn`. The H corpus needed none: `.provideService` on a fork replaced C's two helpers (§8.1).
4. **Stay orthogonal.**
   - scope: lifetime; fork: start; `run child`: observe; interrupt: cancel; race: first completion; provide: context and its lifetime.
   - Time (Clock), recurrence (Schedule) and recovery (catch) are library.
   - Nothing fuses a deadline with a scope, so Go's `context` problem (cancellation, deadline and values in one bag) does not arise.
5. **State composition laws.** K1–K17 and A1–A4, each with its side conditions and probe (§6.8).

**Nesting matrix.** The outer form is the row, the inner one is the column.

| outer \ inner | scope | fork | race | timeout (library) | provide | catch | retry |
|---|---|---|---|---|---|---|---|
| **scope** | K8 | the scope owns the child (K16) | the race owner nests in the scope | as race | the block owns its body and closes before the scope (R12, R2) | as written | as written |
| **fork** (arm or child) | the child owns its scope | grandchild, owned by the child's block (K16) | the race runs in the child | as race | the child has its own provision | the child's failure is caught inside | as written |
| **race** | each arm's scope closes before publication (K2) | forks inside an arm are owned by it (K2); an arm `fork c` over an outer handle races only its observation (§6.3) | does **not** flatten; n-ary instead (§6.4) | K7 | per arm | per arm | per arm |
| **timeout** | covers the scope's close (K2) | a child forked inside the wrapped work is closed at the deadline without a join (K16, R8); a deadline on `run c` for an outer handle cancels the wait, not the child (§6.3) | K7 | K7 | covers the build only when the provide is inside the wrapped function (§4) | K15 | K15 |
| **provide** | the scope sees the plan | the child inherits the provision lexically and is owned by the block (K16, K17) | every arm sees it | the build is outside the deadline | K10 | K9 with its conditions | builds once; the attempts reuse it |
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

P also scored below H with the blind readers: 10.5 of 12 in round 1 (n = 1) and 11 of 12 in round 2, where all three readers put `.join()`, `.interrupt()` and `.fork()` on the library side (§8.2).

What P got right survives in H: timeout and retry as library over race, duration literals, the Schedule analysis, the protocol framing and the laws.

### 7.2 Block options A–E

- **A, clauses on `scope`:** `scope provide App, Clock = LiveClock within 2s { … } otherwise { … }`.
  - It adds 3 keywords (`provide`, `within`, `otherwise`), and the deadline charges `Scheduler` by name (1-G).
  - Clause order *is* nesting order, which readers must learn. Round 1's A reader found the header "ambiguous", and in round 2 two of three A readers misread whether the deadline covers the build (Q1, §8.2).
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
- a #134 construct lookalike in the chosen option;
- for H, a `fork` outside a `scope` or `race` block;
- a `keyedToday` mark that disagrees with the names today's compiler keys on: the postfix method names in `syntax.go` and the fiber operations in `semantic.go`'s `fiberCall` (review 2, o3).

**One classification for every option.** The `forms` table gives each dot or call form a carrier:
- `none`: no general feature can make it library, so it is compiler-known.
  - On the #134 surface these are the **lookalikes**: `.fork`, `.join`, `.interrupt`, `.cancel`, `.provide(` with a layer argument, and D's `provide(`.
  - `.as<T>()`, the host type assertion, is also compiler-known, but it lies outside #134. It is counted in its own column and never fails the chosen option (review 2, m5).
- `q11`: library only after Q11. These are `.race`, `.scoped`, `scoped`, and the timeout family (`timeout`, `timeoutOr`, `timeoutOption`, `timeoutTo`).
- `rows`: library after the rows units, #127, #128 Q9, F3b, #131 and #133. These are `.catch`, `.recover`, `.orFail`, `.provide<S>`, `.provideService` and the like.

"Keyed today" counts forms that today's checker recognises by name.

| Option | Tokens | vs today | Code lines | Distinct keywords | Punct/line | Lookalikes (#134) | Other compiler-known | Awaiting Q11 | Awaiting rows units | Keyed today |
|---|---|---|---|---|---|---|---|---|---|---|
| today | 11090 | 0 | 1664 | 28 | 3.31 | 17 | 1 | 8 | 162 | 188 |
| **H** | **10596** | **-4.5%** | 1736 | 31 | 2.96 | **0** | 1 | 7 | 48 | 48 |
| P | 11104 | +0.1% | 1646 | 26 | 3.36 | 29 | 1 | 15 | 154 | 174 |
| A | 10772 | -2.9% | 1771 | 31 | 2.93 | 12 | 1 | 0 | 45 | 58 |
| B | 10710 | -3.4% | 1773 | 31 | 2.93 | 12 | 1 | 0 | 45 | 58 |
| C | 10678 | -3.7% | 1746 | 31 | 2.97 | 12 | 1 | 7 | 45 | 60 |
| D | 10922 | -1.5% | 1670 | 28 | 3.21 | 17 | 1 | 16 | 139 | 124 |
| E | 10720 | -3.3% | 1777 | 31 | 2.93 | 12 | 1 | 0 | 45 | 58 |

Reading the table:
- **H is the only option with zero #134 lookalikes.** Today's 17 are `.join` (8), `.interrupt` (4) and `.provide(App)` (5); P adds `.fork` (12). Every option keeps the one `.as<T>()` in `host-types.ef`.
- **H is the shortest by tokens.** It hoists provision into one block per entry point, and `run child` and `interrupt child` are shorter than `.join()` and `.interrupt()`. Its line count is higher than today's because blocks take lines. Round 3's change to `ownership.ef` (§6.5) shortened H by 13 tokens.
- **H's 7 forms awaiting Q11** are library deadline calls: 5 `.timeoutOr` and 2 `.timeout`. They are library, and the fallback is an inline race block (§6.7).
- **Max nesting depth** is 9 in every option, set by `http-transport.ef`, so it does not discriminate.
- "Keyed today" rose by one for every option because `.as` is now marked, which the o3 check required.

**Helpers, one count per option.** These are functions that a rewrite had to add, by category:

| Option | Recipe-wrapping | Nested lifetime | Total |
|---|---|---|---|
| **H** | 0 | 0 | **0** |
| P | 0 | 1 (`interruptOneWaiter`) | 1 |
| A, B, E | 3 (`delayedWithin`, `explicitVirtualSleepWithClockScheduler`, `greetingWithDirectory`) | 0 | 3 |
| C | 2 | 0 | 2 |
| D | 0 | 0 | 0 |

H replaced C's two helpers with `.provideService` on a fork (`corpus/H/causal.ef`, `testing.ef`). Since that is library awaiting F3b, today's checker would still need the helpers. The corpus scopes mostly span whole function bodies, so the inline nested-lifetime count may understate real code. That costs P a helper per site and costs H nothing, because `scope` is inline.

**Main moved on after the corpus froze** (rounds 4 and 5). Merging main brought these changes:
- Main reserved the machine decision words (`enter`, `stay`, `ignore`, `reject`, `done`), so `let done` in `recovery-outcome.ef` became `let settled` in every option, as main did in its own example. The totals above are unchanged by it.
- Main added three examples that touch the #134 surface: `machine-breaker.ef` (a provided effectful machine step) and `quiescence.ef` and `quiescence-wakes.ef` (scheduler quiescence, 25 fork sites: 10 and 15). In round 5, main added `latch-handoff.ef` (latch handoff under serialized execution, 16 fork sites; main `fb762763`). All four are rewritten for today and H only, under `primitives/corpus-main/`, because the losing options are recorded as of their round and not maintained. `measure.ts` reports the extension separately and fails on an H lookalike or an ownerless H fork there too.

| Option | Files | Tokens | vs today | Code lines | Distinct keywords | Punct/line | Lookalikes (#134) | Keyed today |
|---|---|---|---|---|---|---|---|---|
| today | 4 | 2795 | 0 | 366 | 24 | 3.65 | 52 (`.join` 33, `.interrupt` 19) | 64 |
| **H** | 4 | **2638** | **-5.6%** | 414 | 26 | 2.93 | **0** | 9 |

Round 4's three files alone measured today 1875 tokens and 35 lookalikes, H 1765 (−5.9%) and 0. In the two quiescence examples every fork but two (in one existing scope) is ownerless today, and in `latch-handoff.ef` every fork outside its two existing scopes is. H wraps 23 function bodies in `scope { }` (7, 9 and 7), which is why its line count rises. These examples are not in the prototype's forced inventory, whose base predates them (§8.3), so they add to U4's migration.

### 8.2 Blind readers

Packets are in `primitives/blind-reader/<option>/` for today, H, P and A–E. Each holds the three programs with comments removed, plus the option's `library.ef` for H, P, C and D, and the same 12 questions. The answer key is `blind-reader-key.md`. Its Q11 scoring uses the §8.1 classification rather than each option's own claim (review 1, m1).

**Round 1** (n = 1 per option, Sonnet, scored by review 1):

| Option | Total /12 | Weighted (Q1, 4, 7, 11, 12) /5 | Notes |
|---|---|---|---|
| today | 11 | 4 | Q11: read the compiler-known dots as library |
| P | 10.5 | 3.5 | Q1 hedged; Q11: read `.provide`/`.scoped`/`.fork`/`.catch` as "probably built in" |
| A | 12 | 5 | found the clause header ambiguous |
| B, C, E | 12 | 5 | |
| D | 11 | 4 | Q11: read `provide()`/`provideService()` as library |

**Round 2** (n = 3 per option: Go, TypeScript and Python readers, all Sonnet; run by the orchestrator, scored by review 2 against the key at `35cfba16`; review 2, m10):

| Option | Go | TS | Py | Mean /12 | Q1 | Q4 | Q7 | Q11 | Q12 | Weighted mean /5 |
|---|---|---|---|---|---|---|---|---|---|---|
| **H** | 12 | 12 | 12 | **12.00** | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 | **5.00** |
| C | 11 | 11.5 | 11 | 11.17 | 1.00 | 1.00 | 1.00 | 0.17 | 1.00 | 4.17 |
| today | 11 | 11 | 11 | 11.00 | 1.00 | 1.00 | 1.00 | 0.00 | 1.00 | 4.00 |
| P | 11 | 11 | 11 | 11.00 | 1.00 | 1.00 | 1.00 | 0.00 | 1.00 | 4.00 |
| A | 11 | 10.5 | 11 | 10.83 | 0.50 | 1.00 | 1.00 | 0.33 | 1.00 | 3.83 |

**Limits.** Read the table with these in mind:
- n = 3 per option, and every reader is the same model family.
- Q1, Q4, Q7 and Q12 are at ceiling for every option except A's Q1.
- **H's Q11 score is partly by construction.** H's packet has no scored dot form, so there is nothing to misclassify. That is the design intent, not independent evidence.
- The table does not support a claim of general readability superiority.

What the round does show:
- **Every non-H loss is on Q11, plus A's Q1.** Today's, P's and C's readers put `.join()` and `.interrupt()` (and P's `.fork()`, plus `.provide(App)` in today and P) on the library side. This is the pattern H is built on: readers classify keyword and block forms correctly, and cannot tell compiler-known dot forms from library.
- **A, Q1.** Two of three A readers misread whether the header's deadline covers the build.
- **Readability finding for H.** All three H readers flagged `run` as meaning both "call" and "await": "run is overloaded for calling and awaiting" (Go), "surprising that run is used for both" (Python), "fork returns a handle that run joins" (TypeScript). The TypeScript reader also found `.catch<Bad>` on a fork handle "a bit odd".

Review 2 rejected the finding as a keyword reuse (C5): `run` executes one recipe, and a handle denotes its join recipe. It is still a real learning cost. §6.3's teaching example addresses it, and O10 asks the owner to choose between keeping `run child` (recommended) and a `join` keyword.

### 8.3 Prototype

Branch `wf10/spike-syntax-proto`, head `110fa3da`, "feat(spike): close #134 review 4 on the option H prototype", on top of round 4's `96155c4f` (round 2's `bc263cf8`, then round 3's `e906c9bf` and `0ef605b2`, and round 4's `96155c4f`). The prototype base is main at `effb24d4`, so it predates main's machine decisions and quiescence examples; §8.1 measures those separately.

- **Parser** (`syntax.go`):
  - `race { fork a  fork b }` lowers to the race kernel; a non-fork arm is EF002 "each arm of a race block is a fork".
  - Every provide block is an owner block, so a `fork` directly in its body is owned (review 3, Minor 2).
  - A service-only header lowers through K17: each provider is bound at the header to a name no source can spell, and the block becomes `scope { let x = p  body' }` with the service provided by `x` to every recipe the body runs or forks (`provideToBody`), except joins of the body's own handles, so any block body is admitted. A layer header still takes one `run` statement and lowers to today's provision (§6.5).
  - `interrupt child` lowers to today's interrupt.
  - A `fork` outside every owner block is EF002 "fork needs an owner".
  - A `fork` entry in a `layer` is EF002 "layer worker entries are not part of option H", unless `EFFRA_SPIKE134_WORKERS=1` keeps round 3's worker code for U-workers (§6.5).
  - The parser does not rewrite names. It only marks the positions where a handle is moved rather than joined (§6.3).
- **Lint facts** (`lint_facts.go`): the provisions a service-only provide block generates fold into one provision fact per block and service (§6.5).
- **Checker** (`semantic.go`, `layers.go`): in an H file, a read of a `Fiber`-typed local outside those positions becomes its join. The original syntax entry is moved to the handle, so type queries keep the binding.
- **Runtime** (`runtime/effra/effect.go`): `raceWithPolicy` with the symmetric race policy (§6.4, m7), plus `race_test.go`, widened in round 4 (§6.4).
- **`ef fix` prototype** (`spike134_fix.go`): §8.3's migration below.
- **Clock change** (round 2) stays reverted: timeout charges `Scheduler`, as today.

| File (prototype dir) | Go | JS | Output equals today's |
|---|---|---|---|
| `dashboard.ef` (worked example; provide block, race block, `run account`) | `live / live` | same | yes |
| `concurrency.ef` (scope, `run child`, `interrupt`, library deadline) | `child joined` / `timed out` / `recovered` | same | yes |
| `layers.ef` (provide block in `main`) | `fixture:fixture` | same | yes |
| `race-library.ef` (race block; `after`, `expire`, `deadlineOr`, `deadline` in `.ef`) | `fast` / `failure won` / `abandoned loser` / `gave up` / `work` / `deadline` / `outer` | same | n/a (new) |
| provide body with forks (`TestSpike134ProvideBodyOwnsAFork`) | `tick` / `joined ticked` / `ticked` / `after` | same | n/a (new) |
| `walls.ef` | `ef check`: EF127 for generic `timeoutOr` (#128 Q9). `outside` and `inside` check, showing both deadline placements. | | the wall is the evidence |

**Tests.**
- `spike134_test.go`:
  - the four programs on both targets;
  - fork needs an owner, with a legacy file still admitted;
  - an escaping handle is EF123;
  - an unobserved child is EF107;
  - catch on a joined child is admitted;
  - interrupt carries the child's row;
  - race arm shape, arm result type and union row;
  - the provide block with EF108 for a missing service.
- `spike134_laws_test.go`: one probe per law (§6.8), plus m2 and m3; the provide-body fork; header resolution of the provider and the one-provision lint fact (review 4, Minor 2); the refused worker entry; the recorded worker prototype and the two known-failing worker probes (w1, w5) under the flag.
- `spike134_fix_test.go`: the `ef fix` outcomes, f1 and f2, review 4's r1, r2 and r8, the cancellation cases, and the inventory proof below.
- `runtime/effra/race_test.go`: the latch tests in both arm orders and the seven-combination cancellation test, which enters the race with both arms running before it cancels (review 4, Minor 1).
- `determinism_test.go`: the helper now fails with "compilation refused before checking" and the diagnostics when a source is refused, instead of dereferencing a missing program (review 3, Minor 6).

**Suite.** `go test ./internal/compiler ./runtime/... -count=1` passes in full on the prototype head in default mode (compiler 162 s, runtime 9 s; receipt in `round-5-request.md`).

**Migration inventory** (review 2, M1). The owned-fork rule was forced on every file with `EFFRA_SPIKE134_OWNED=all`.

| Run | Top-level tests failing | Causes |
|---|---|---|
| Round 2, `bc263cf8` (reproduced) | 34 existing + `TestSpike134ForkNeedsAnOwnerBlock`; the determinism test panics first | EF002, plus two non-EF002 causes from the name-keyed handle rewrite: typing (`TestOwnershipMatrixMatchesTruth` cells `s1_fork@1_plain_if/match`, EF106 + EF107; `TestPipeFormCompilesToTheSameOutputAsNestedCalls`, 4 programs compiled differently in pipe form) and tooling (`TestSelectedFiberOperationReceiversRetainTheirOwnedBinding`) |
| Round 3, `0ef605b2` | 33 existing + `TestSpike134ForkNeedsAnOwnerBlock`, with the determinism test run separately | EF002 only |
| Round 4, `96155c4f` | 33 existing + `TestSpike134ForkNeedsAnOwnerBlock`, in one run, the determinism test included | EF002 only. The determinism test now fails on `causal.ef` with the EF002 diagnostic instead of panicking. |
| Round 5, `110fa3da` | the same 33 existing + `TestSpike134ForkNeedsAnOwnerBlock` | EF002 only, no panic |

The 33 existing tests, by category:

| Category | Tests | What fails |
|---|---|---|
| Syntax only: a fixture forks outside a scope, and the test expects it to check or run | 22 | EF002 "fork needs an owner" |
| Syntax first: the test expects another diagnostic (EF107 ×7, EF105 ×1) on a fixture with an ownerless fork | 8 (`TestGuardrails`, `TestCompositeCausesAreRefusedAtOwnerEdges`, `TestRootEdgeAdmitsDeclaredCompositeFailures`, `TestUnobservedChildFailureStaysRefused`, `TestForkedChildFailureIsChargedWhereObserved`, `TestNestedJoinsObserveTheirChild`, `TestCarriedPendingEndsAtTypeBoundaries`, `TestConsumersAreSilentOnInvalidOperands`) | EF002 is reported before the expected code |
| Formatter refusal | 2 (`TestFormatCanonicalExpressionsAndMatchGolden`, `TestFormatExamplesAreParseableAndIdempotent` on `causal.ef`) | "source cannot be formatted: fork needs an owner" |
| Determinism (`TestRepeatedCompilationIsDeterministic` on `causal.ef`) | 1 | "compilation refused before checking: EF002"; round 3's helper panicked here |
| Typing (handle reading) | 0 | fixed by the type-based reading (§6.3) |
| Tooling | 0 | fixed by moving the syntax entry to the handle |

The prototype's own spike tests pass under the forced rule except `TestSpike134ForkNeedsAnOwnerBlock`, whose legacy-file case is the forced rule's point. The fix tests run their legacy side under today's rules and their fixed side under the owned rule, so they hold in either mode.

**`ef fix` and its proof.** U4's autofix must never change behaviour (review 2, M1). The prototype `Spike134Fix` wraps in `scope { }` only the region from a `let h = fork …` to the statement-level observations of `h` in the same block. Overlapping regions merge. Its rule is structural (review 4, Major 1, as decided by the orchestrator): **nothing inside the new scope may run anything except the owned forks of pure operands and their joins, plus pure local computation.** It never trusts a call by its name or by the purity of its operands. Concretely, it wraps a region only when all of these hold:
- **the fork's operand only builds a recipe** (review 3, Major 1a): a name, or a call whose arguments are names, literals and constructors of declared records and enums, optionally under the `timeout` and `provide` postfixes. On Go the operand is evaluated under the caller's owner before the fork, so an operand that runs, acquires or fails would move its effects into the new scope. A computed argument (`"a" + "b"`) is refused too;
- **no handler anywhere in the region**: a `catch`, `recover` or `orFail`, on an observation or on a fork operand, is reported, because a handler's code runs under whichever owner runs it (review 4, r1 and r2: a `recover` handler that acquires a file or forks a child would do so under the new scope);
- **every observation is a whole statement**: `run h.join()`, `run h.interrupt()` or `let v = run h.join()`;
- **every other statement is a `let` of a pure local value**: names, literals, constructors, comparisons and boolean operators. No `run` of any service or function, whatever its name: round 4's allowlist of `Console.log`, `Clock.sleep`, the test `Scheduler` steps and `Sync.signal` is gone, because any of those services can be provided by a user `impl` that acquires (review 4, r8). Arithmetic is excluded because it can fail at run time;
- the handle is used nowhere else;
- only the region's last statement can fail;
- no name bound in the region is used after it, except the last statement's own `let`, which becomes `let v = scope { … }`;
- the joined value is not itself a recipe, which a scope cannot yield.

*What it preserves, exactly* (review 3, Major 1b; review 4, Major 1). For regions that satisfy the rule, the only code that executes inside the new scope is the forked recipes, each under its own child owner exactly as before the fix, and the joins, which wait. Nothing else registers with any owner, so the children are the last things registered with the dynamic owner when they start, and they are done when the region ends. At every exit of the region, every child it started has been observed. So, for those regions only, the fix preserves:
- outputs and exits;
- failures, including the failure a deadline or an interrupt produces while the region runs;
- the owner's LIFO close order, and the resources each owner holds.

The claim says nothing about any other region, and the fix does not touch one. It does not preserve the owner tree that `Runtime.inspect` reports after the region. A joined child stays listed under the enclosing owner today; after the fix it is listed under the new scope, which has closed. `TestSpike134FixOwnerTreeDifferenceIsDocumented` asserts exactly this on review 3's f2: the same output before the inspection, `resourceCount` 0 on both sides, and `children` 1 against 0.

*Review 3's counterexamples are tests.*
- **f1** (an operand that acquires a file under the caller's owner): refused with "an operand of the fork may run, acquire or fail when it is evaluated" and the manual migration "bind the operand first … in the owner that should hold what it acquires" (`TestSpike134FixRefusesAnActingForkOperand`).
- **f2** (a pure operand, followed by an inspection): wrapped, with the documented owner-tree difference asserted.

*Review 4's counterexamples are tests* (`TestSpike134FixRefusesReviewFourCounterexamples`; each checks under today's rules, is refused and is left unchanged):
- **r1**, a `recover` handler that opens a file: refused, "the region contains a handler";
- **r2**, a `recover` handler that forks a child: refused, "the region contains a handler";
- **r8**, `Clock.sleep` in the region with `Clock` provided by an impl that acquires: refused, "a statement inside the fork-to-join region runs something other than the forks and their joins".

The inline cases add a pure fallback (`catch<Bad>("caught")`), a handler on the fork operand, `Console.log` and `Clock.sleep` in the region, arithmetic in the region and an observation nested in a call, all refused, and a pure local and two children, both wrapped.

*Cancellation* (review 3, Major 2). `TestSpike134FixPreservesBehaviourUnderCancellation` runs four programs whose wrapped region is cancelled, legacy against fixed, on Go and JavaScript: a deadline during the region; a deadline over two children, one of which fails late; an interrupt of the function that owns the region; and a deadline that fires after the region has completed. Round 5 removed the `Console.log` lines from the regions, which the new rule refuses, and put the interrupting program's own fork in a written scope. Each prints the same and exits the same on both sides and both targets.

*Inventory proof* (review 3, Major 2 and the per-target counts). `TestSpike134FixPreservesBehaviourOnTheInventory` runs the fix over every source the forced rule refused anywhere in the compiler suite, collected with `EFFRA_SPIKE134_DUMP` on prototype `110fa3da`: **1132 distinct sources**, which include the spike tests' own sources (f1, f2, r1, r2, r8 and the inline fix cases). For each source:
- the fixed source, under today's rules, keeps today's diagnostics;
- the owned rule either admits the fixed source with exactly today's diagnostics, or the fix reported a fork;
- a source that does not emit under the legacy rules must fail to emit identically after the fix; any other emission, build or deadline failure fails the test;
- every fully or partly wrapped program with a `main` runs original against fixed, the legacy program under today's rules (a partly wrapped program cannot check under the owned rule) and the fixed one under the owned rule for a full wrap; outputs and exits must be equal, with the documented owner-tree difference classified rather than ignored.

| Outcome | Sources | Go runs | JS runs |
|---|---|---|---|
| Wrapped in full; owned-rule diagnostics equal today's | 16 | 15 same (one of them same except the documented owner tree: f2) | 10 same |
| … binding Go natives (EF110 on JS), counted on Go only | 5 of the 16 | included above | excluded |
| … without a `main` | 1 of the 16 | — | — |
| Partly wrapped, the rest reported | 3 | 3 same | 3 same |
| Reported for manual migration | 1113 | | |
| Changed legacy diagnostics, outputs or exits | **0** | **0** | **0** |

18 programs run original against fixed: 18 on Go and 13 on JavaScript. Round 4's rule wrapped 75 sources in full and 23 in part; the 59 full and 20 partial wraps it loses ran a service operation (mostly `Console.log` and the test `Scheduler` steps) or a handler inside the region. They were correct as measured, but the rule could not prove them, so they move to manual migration.

| Reported fork sites, by reason | Sites |
|---|---|
| Fire-and-forget child (never joined or interrupted in its block) | 5669 |
| A statement in the region runs something other than the forks and their joins, forks, opens a block or fails | 118 |
| The region contains a handler (`catch`, `recover`, `orFail`) | 31 |
| The fork's operand may run, acquire or fail (all four are spike-test sources, f1 among them) | 4 |
| The handle is used other than at a join or interrupt (returned, passed, raced, cancelled) | 58 |
| The joined value is a recipe | 39 |
| The fork is not a `let` value | 2 |
| Legacy source does not check, or does not parse | 6 + 4 |

Most of the reported sources are the compiler suite's own generated fixtures, which fork into the caller's owner on purpose to test R9 and composite causes. Their migration is a decision about which owner each child should have, so it cannot be automatic.

`examples/causal.ef`, the only affected example on the prototype base, is now reported for manual migration: its regions run test `Scheduler` steps between the fork and the join, which round 4's allowlist admitted and the structural rule does not. Main's later examples (`quiescence.ef`, `quiescence-wakes.ef`, `latch-handoff.ef`; §8.1) are not in this inventory; they run `Scheduler` and `Sync` steps between forks and joins too, so they are manual under the same rule.

## 9. Decision record

**Chosen set and syntax (H).**
- **Constructs:**
  - `effect fn`, `run`, `fail`;
  - `service`, `impl`, `layer` (and machines);
  - `scope { }`; owned `fork`; `run child` as join, read by type; `interrupt child`;
  - `race { fork … }`;
  - `provide Plan { }`, always an owner, including of a fork in its body.
- **Not adopted:** layer worker entries (`fork w(args)` in a `layer`) and law K18. Provision-lifetime workers are the U-workers unit.
- **Proposed construct:** `all { fork … }` (P14, after O3), yielding a tuple in arm order.
- **Notation:** duration literals (`ms`, `s`, `m`).
- **Library:** the timeout family, `after`, `expire`, `either`, `delay`, `retry`/`Schedule`, `.provideService<S>(p)`, `.catch`, `.recover`, `.orFail`.
- **Host primitives:** P1–P12 unchanged, with P13 race added.
- **Retired:** `.timeout` as a keyed form (after U9), `.provide(Layer)` and `.provide<S>` as dot forms, `.join()`, `.interrupt()` and `.cancel()`.
- **Outside #134:** `.as<T>()` stays a compiler-known host form; the visual rule's question for it belongs to the host-interop work.

**Losing options:** P (§7.1); A, B, C, D, E (§7.2).

**Effect on tickets:**
- **#95** (reserve postfix words): **superseded.** Nothing is reserved after a dot on the #134 surface. The keywords are leading words.
- **#132** (provide as library): **re-scoped** to `.provideService<S>` only. `provide(Layer)` stays a construct as the provide block, and the rows note's NO-GO is upheld.
- **#126** (F3, row difference): needed for `.provideService` only.
- **#127** (receiver methods), **#128 Q9**: needed for the generic library.
- **#128 Q11**: no longer load-bearing for any guarantee; it affects only lossless wrapping of owned-value recipes.
- **#130** (4c): not needed by #134.
- **#131, #133** (catch, recover): unchanged.
- **#106** (non-escaping block closures): would allow owner values as parameters (O4).
- **#124, #125:** receive K1–K17 with their side conditions and probes, A1–A4, P13 and P14.
- **#121:** a UI frame runtime is a named provider client (§6.8).
- **`lawful-runtime-contract.md:63`:** to amend in U1, since race becomes P13 with block syntax and provision has block syntax.

**Follow-up units** (implementor-days are estimates):

| Unit | Work | Days | Order |
|---|---|---|---|
| U1 | P13 race kernel: n-ary race block in both runtimes, the symmetric policy classified at selection, K1–K5 law tests including the latch arm-reversal test; amend `lawful-runtime-contract.md:63` | 3 | now; the prototype exists |
| U2 | Duration literals plus a nominal `Duration`; the formatter prints literals | 2 | now |
| U3 | Provide block, replacing `.provide(App)`, always an owner of its body and of forks in it (K17), with any block body for both header shapes; graph, lint and inspection read the header | 4 | now |
| U4 | Owned fork, `run child` read by type in the checker and `interrupt child`, behind an explicit edition (O13). `ef fix` wraps only regions where nothing in the new scope runs except owned forks of pure operands, their plain joins and pure local values, and reports the rest (§8.3), with the behaviour proof in CI: per-target counts, emission failures fatal, cancellation cases, and the owner-tree difference documented to users. Migrate by hand the 33 tests, `examples/causal.ef`, main's later examples (§8.1) and the 1113 reported sources. Drop `.cancel()`. | 7 | after O1, O13 |
| U5 | Formatter and LSP for the new blocks (hover, including "joins child `c`" for O10; folding; completion of plan entries) | 2 | with U3, U4 |
| U6 | Library timeout family, `either`, `delay`, `retry`/`Schedule` in `.ef`; K14 and K15 probes | 3 | after #127, #128 Q9 |
| U7 | `.provideService<S>` as library | 1 | after F3b (#126, #132) |
| U8 | Q11 closed-at-the-edge contract, measured on the 29 programs | 5–8 | inside #128; not on H's critical path |
| U9 | Retire keyed `.timeout`, with parity in verdicts, row identity and error identity (§6.7) | 1–2 | after U6 and the §6.7 gate |
| U10 | `all { }` block (P14) with A1–A4 law tests | 3 | after O3 |
| U11 | Constructs as a provider protocol (a `Runtime` provider interface plus the RS1 conformance suite built from the §6.8 probes); first non-Effect provider is the deterministic test runtime | 8–10 | after U1, with #124 |
| U-workers | Provision-lifetime workers, split from #134 (§6.5). Acceptance below. | 5–8 | after U3; filed by the orchestrator |
| U-K9 | Revisit K9's probe with node build and release traces and an exit-observing finalizer once P5 (`acquireRelease`) and `onExit` reach the surface | 1 | with P5 |
| U-AST | Write down the in-place AST rewrite contract of the handle reading (`semantic.go:4476-4495`: the synthetic handle is marked against a second rewrite; LSP reparses through `CompileAt`, `internal/lsp/server.go:361`) before LSP gains incremental AST reuse | 0.5 | before incremental LSP |

**U-workers acceptance** (re-plan decision 1; review 2 M4, review 3 B1, Major 3, Minor 5, O12). Title: "U-workers: provision-lifetime workers for layers (split from #134)". Scope: design and prototype a worker owned by a provision, declared once in the layer (round 3's proposed form is the layer entry `fork w(args)`), lowered on Go and JavaScript. It is accepted when:
- **Composite rule.** A worker failure is registered as a pending child of the provision owner, and the R6/EF107 composite rule applies to worker plus body and to worker plus worker, including two failures with the same label; or the design states an explicit alternative (infallible workers, or a fail-fast policy that makes the body's failure and the worker's exclusive).
- **Identity.** A worker has a declaration-qualified identity; a diamond merge starts it once, merged by identity as nodes are, and it is charged to the layer's construction work.
- **Failure policy.** Deferred surfacing (round 3), fail-fast and supervision are compared against Erlang/OTP supervisors, Elixir `Task.Supervisor`, Effect `Layer.effect`/`Layer.effectDiscard` with `forkScoped` (v3: `Layer.scoped`), Trio's nursery held in context, Kotlin `coroutineScope` and `supervisorScope`, and Loom's `Joiner`; the chosen policy is a law with a probe, and the note says that lifetime-owned does not mean supervised.
- **Start and progress.** The design states whether a worker is guaranteed a start or progress, and decides the start handshake (Trio `nursery.start`, Elixir `init/1`, Gleam `actor.start`) and node-scoped workers (today's R13) that round 3 left to O12.
- **Ordering evidence.** Observable traces order worker start and stop against node build and release, on both targets.
- **Probes.** `TestSpike134KnownFailingW1WorkerCompositeIsUncaught` and `TestSpike134KnownFailingW5WorkerDiamondStartsTwice` are inverted (w1 refused or declared; w5 one start) on both targets.

**U4's estimate** (review 4, optional). The 7 days assume that the reported sources are migrated per generator, not per source: most of the 1113 come from the compiler suite's fixture generators behind the 33 failing tests, so one edit to a generator moves many sources. The structural rule (round 5) moved 85 more sources to manual, mostly hand-written tests that log or step the test scheduler between a fork and its join, which is why the estimate rose from 6 to 7 days. If the fixtures must be migrated source by source, the estimate does not hold.

**Open owner questions** (each with a recommendation):

- **O1. Adopt H?** Owner blocks (`scope`, `race`, `provide`), owned `fork`, `run child` and `interrupt child` as constructs, with timeout, retry and `provideService` as library. *Recommend yes.* (Review 2: yes.)
- **O2. Superseded.** Round 1 asked whether fork charges a row. Owned fork replaces the question.
- **O3. Group failure policy.** *Recommend* the fail-fast `all { }` block (P14, §6.6), yielding a tuple in arm order, rather than a library intrinsic.
- **O4. Owner values as parameters** (Trio nursery, Loom scope, Eio switch), so a helper can fork into its caller's owner. *Recommend deferring* to #106, since the value must be second-class. H stays lexical-only. Whether provision-lifetime workers need it is U-workers' question (§6.5). Until then a helper returns its recipe and the caller forks it.
- **O5. Duration units.** *Recommend* `ms`, `s` and `m` only.
- **O6. Clock or Scheduler for deadlines.** *Recommend:* today's keyed `.timeout` keeps charging `Scheduler` until U9, and the library deadlines charge `Clock` because they sleep on it. U9 then decides Q5 by retiring the keyed form, with the parity gate covering rows and error identity (§6.7).
- **O7. Resources keep the dynamic owner.** *Recommend yes.* EF123 already keeps them from escaping, and a lexical rule would need an owner at every acquisition. Resource ownership stays dynamic while fork admission is lexical. They guard different lifetimes, which is why K12 holds only for unowned results.
- **O8. Drop `.cancel()`** (request cancellation without waiting). *Recommend folding it* into `interrupt` and owner close.
  - No corpus program needs it.
  - Owner close already awaits every child (`lifecycle.mjs:56-65`; Go `scope.go:149-165`), so a cancel without waiting only moves the wait to the close.
  - Group shutdown keeps P8's rule: request every cancellation before awaiting any. (Round 2's rationale, "a child whose cleanup nobody observes", was wrong; review 2, m9.)
- **O9. Rename `.provide<S>` to `.provideService<S>`.** *Recommend yes,* so the keyword and the library function never share a word.
- **O10. `run` for both call and join** (§8.2). Options:
  - (a) keep `run child`, teach the run-twice example (§6.3), and add an LSP hover;
  - (b) a `join` keyword (`join child`). That is one more keyword, and a handle would stop being a recipe, so passing a handle as an arm or argument would need an explicit conversion.
  
  *Recommend (a).* A handle denoting its join recipe is what makes `fork c` arms and handle arguments compose, and review 2 found no keyword reuse.

  The question has two independent halves (review 3, optional): the **spelling** of a join (`run c` or a `join c` keyword) and the **conversion semantics** (whether a handle read where a recipe is expected denotes its join recipe). A `join` keyword does not by itself remove the recipe reading; (b) as written couples the two. The recommendation is the recipe reading in either case, and `run c` as the spelling.
- **O11. What a child owns** (review 2, M2). Options:
  - (a) a child owns what it acquires, so its unreturned resources close before the join returns and owned results are EF123. This is Trio, Kotlin, Swift, Eio and Elixir.
  - (b) a declared handoff, where the child's unreturned resources stay alive in an outer owner. Effect does not move resources at join: its `forkIn` and `forkScoped` bind the child's lifetime to a scope and transfer nothing (`internal/effect.ts:5594-5663`), and Effect 4 has `Scope.provide`, not `Scope.extend`. Effect's form of (b) is shared provision of a scope: the child's `acquireRelease` registers in whichever `Scope` its context supplies (review 3, Minor 3; §6.3).
  
  *Recommend (a)*, with K5 and K12 stated under its conditions. (b) needs an owner value passed to the child (O4) and a release path in that owner for an interrupted child.
- **O12. Provision workers** (review 2, M4). *Moved to U-workers* (re-plan decision 1). Round 3 recommended the layer entry `fork w(args)` with a later start handshake (U12); review 3 found the entry unsound (B1, Major 3) and its failure policy uncompared (Minor 5). The handshake and node-scoped workers are part of U-workers' acceptance above, and U12 is folded into it.
- **O13. Where the edition is declared** (review 2, m1). *Recommend* a per-package edition in the package manifest that Effra will need anyway (Go's `go` directive in `go.mod` is the precedent), never inferred from file content. `ef fix` performs the move. Until a manifest exists, use an explicit compiler flag.

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

**Round 3 (2026-10-12, after review 2).** Review 2 returned changes: 0 Blocker, 4 Major and 10 Minor findings. It accepted H and recommended O1. The blind readers scored H 12 of 12. The changes are to the note's claims and to gaps in the design, not to the choice:
- **M1, migration.** The count is reproduced and classified (§8.3). `ef fix` is redesigned to wrap only provably inert fork-to-join regions, and it is proved on all 1112 refused sources.
- **M2, laws.** K5, K9 and K12 are restated with exact side conditions, and the design keeps child-owned resources (O11, with the rejected handoff recorded). Every law has a probe.
- **M3, provide.** Every provide block is an owner; K17 gains its scope, and `corpus/H/ownership.ef` changed.
- **M4, workers.** The layer worker entry is specified, prototyped on both targets and given K18, with the handshake case left to O12. (Withdrawn in round 4.)

The minors are fixed in place:
- m1: an explicit edition, O13;
- m2: racing a handle;
- m3: the type-based handle reading;
- m4: the visual-rule rows in §4;
- m5: `.as<T>()` counted apart;
- m6: A3 and K3;
- m7: the Go race classified at selection, with a latch test;
- m8: K6, K7 and the U9 gate;
- m9: O8's rationale;
- m10: the round-2 reader table, its caveats, and O10.

So are the optionals:
- o1: `all`'s tuple;
- o2: the provide prototype limits;
- o3: measure.ts checks `keyedToday` against the compiler.

**Round 4 (2026-10-12, after review 3 and the orchestrator's re-plan).** Review 3 returned changes: 1 Blocker, 3 Major, 6 Minor and 3 Optional findings. It confirmed H; what failed was round 3's layer worker entry and the claims about `ef fix` and its proof. The orchestrator fixed three decisions:
- **Decision 1, workers split out.** The layer `fork` entry and K18 leave H. The gap (review 2, M4), the proposed form, and review 3's B1, Major 3, Minor 5 and O12 become U-workers' acceptance (§6.5, §9). The prototype refuses the entry and keeps round 3's code behind a flag, with w1 and w5 as known-failing probes.
- **Decision 2, the fixer's operand rule.** `ef fix` refuses to wrap a fork whose operand may act; the preserved behaviour is restated, and the owner-tree difference is documented and asserted (f1, f2).
- **Decision 3, the proof.** Emission failures fail the inventory test, Go-native sources are counted apart, partly wrapped programs run original against fixed, cancellation cases are tested, and the counts are given per target (§8.3).

The six minors are fixed in place:
- Minor 1: K5 needs an empty-row `never`; the race's rows are a union, probed;
- Minor 2: a fork directly in a provide body is owned by the block, and a service-only header admits any body (K17 as the lowering);
- Minor 3: Effect's `forkIn` binds a lifetime and transfers no resources; `Scope.provide`, not `Scope.extend` (§6.3, O11, prior-art.md §5, effect-study.md);
- Minor 4: a successful winner of a cancelled parent is observed as interruption (§6.4, K4), with a new race test;
- Minor 5: the worker failure policy moves to U-workers;
- Minor 6: probes widened (K9 layer header and merge, a K17 fork probe on both targets, the race test over seven arm combinations), source-argued evidence labelled, and the determinism helper fails instead of panicking.

The three optionals are taken as text: K9's revisit with P5 (U-K9), O10's two halves, and the AST rewrite contract (U-AST).

Main was merged into the note branch without conflicts (and again in round 5, also without conflicts, bringing `latch-handoff.ef` into `corpus-main`). Main's reserved decision words renamed `let done` in one corpus file, and main's three new examples on the #134 surface are measured for today and H (§8.1).

**Round 5 (2026-10-12, after review 4).** Review 4 returned changes: 0 Blocker, 1 Major, 3 Minor and 4 Optional findings. It found decisions 1 and 3 met, and the Effect corrections, the race wording, the main merge and the decision record sound.
- **Major 1, `ef fix`.** Round 4's claim was stronger than its rule: an admitted `recover` handler or an allowlisted service operation ran user code under the new scope (r1, r2, r8). The orchestrator fixed a structural rule: a region is wrapped only when nothing in the new scope runs except the owned forks of pure operands, their plain joins and pure local values. The allowlist is gone, handlers are reported, r1, r2 and r8 are refused tests, and the inventory was re-run: 16 sources wrapped in full and 3 in part, 1113 manual. The claim in §0 and §8.3 now covers exactly those regions, and U4 rose to 7 days on a stated per-generator assumption.
- **Minor 1, race probe.** The cancellation test now enters the race with both arms running before it cancels and asserts the entry and completion counts. The JavaScript half of the successful-winner wording is labelled an untested observation.
- **Minor 2, K17 hygiene.** The provider is resolved at the header into a binding no source can spell; handle joins are not wrapped; lint sees one provision per block. K17 is restated as a capture-avoiding substitution.
- **Minor 3.** Effect 4 has no `Layer.scoped`; the note, prior-art.md and the U-workers acceptance name `Layer.effect`/`Layer.effectDiscard`.
- **Optionals**, all taken: 25 quiescence fork sites, K5's unprobed main clause, U4's per-generator assumption, and the layer-header limit stated beside the provide-body fork rule and the worker stop-gap.

