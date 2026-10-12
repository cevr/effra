# Blind-reader answer key (do not hand to readers)

Packets: `blind-reader/<option>/` for `today`, `H`, `P`, `A`, `B`, `C`, `D`, `E`. Each holds the same three programs in that option's syntax, with comments removed, plus `questions.md` (identical in every packet). Packets for H, P, C and D also hold that option's `library.ef`. Give each reader one packet only. The orchestrator runs the readers (round 1: n = 1 per option; round 2: n = 3 per option); this key scores them.

Score each answer 1 (right), 0.5 (right with a wrong reason, or hedged between the right answer and one wrong one) or 0. Record time and confidence. For the owner's direction, question 1 matters most ("does the 2s include building App?"): an option whose readers miss it is hiding the order of nesting. Questions 1, 4, 7, 11 and 12 carry the most weight.

| # | Expected answer | Differences by option |
|---|---|---|
| 1 | No. The limit wraps only the dashboard work; App is set up outside it, before the clock starts. | H, B, C, E: the `provide` block is outside the deadline. P and today: `.timeoutOr(2s, …)` (today `.timeout(2000)…`) comes before `.provide(App)` in the chain, so the provide wraps the timed recipe. A: `scope provide App, … within 2s` — clauses nest left to right, provide outermost. D: `with provide(App)` comes before `with timeoutOr(2s, …)`, so it wraps it. A reader who says "yes" for A is reading the header as one flat list; record it. |
| 2 | `"gave up"`. | Same in every option. |
| 3 | The invoice text becomes `"invoice: slow"`; `dashboard` carries on, waits for the account lookup and returns `"<account> / invoice: slow"`. | H: the `race` block's two arms are the lookup and `after(500ms, "invoice: slow")`; the first to finish gives the value, and the lookup is cancelled. Same answer elsewhere. |
| 4 | It is cancelled, and its cleanup finishes before `main` returns `"gave up"`. It does not keep running. | H, A, B, C, E, today: `dashboard`'s `scope { }` is inside the deadline. P: the scope is `dashboard().scoped()`, which sits inside the deadline, so the losing side's owner is cancelled. D: `with scoped()` in `dashboard`. A reader who says "it keeps running" (Go goroutine intuition) or "the timer just stops waiting" (JavaScript `Promise.race` intuition) is wrong. |
| 5 | `Account`, `Invoice` and the time service. | H, P, C, D: `Clock`. today, A, B, E: `Scheduler`. P only: also acceptable to add "and the caller decides how long `dashboard`'s background work lives, with `.scoped()`". |
| 6 | `child joined`, `timed out`, `recovered`. | Same. |
| 7 | No. `stopped` is cancelled straight away, so its sleep never finishes. | H: `interrupt stopped`. Elsewhere: `stopped.interrupt()`. |
| 8 | No. The failure surfaces where the joined result of `failed` is run, and `.catch<Bad>("recovered")` replaces it. (`main` still lists `Bad` in its signature.) | H: `run failed.catch<Bad>("recovered")` (running the child's handle waits for it). Elsewhere: `run failed.join().catch<Bad>("recovered")`. |
| 9 | `"fixture:fixture"`. | Same. |
| 10 | The fixture store, `Memory("fixture")`, because `Fixture` replaces `Store`. | Same. |
| 11 | See "Question 11" below. | Scored only on forms whose classification is not contested. |
| 12 | Yes. The 2 s lives in `main`, not in `dashboard`; the other team writes their own call with 5 s. | H and C: `provide App, Clock = LiveClock { run dashboard().timeoutOr(5s, "gave up") }`. P: `run dashboard().scoped().timeoutOr(5s, "gave up").provide(App).provide<Clock>(LiveClock)`. A: `scope provide App, Scheduler = LiveScheduler within 5s { run dashboard() } otherwise { "gave up" }`. B: `provide App, Scheduler = LiveScheduler { within 5s { run dashboard() } otherwise { "gave up" } }`. D: three `with` lines ending `with timeoutOr(5s, "gave up")`, then `run dashboard()`. E: `provide App, Scheduler = LiveScheduler { race { run dashboard() } after 5s { "gave up" } }`. |

## Question 11

Question 11 asks which forms are built in and which are ordinary functions. Round 1's key scored every form against the option's own claim, so it presupposed P's classification of `.fork()`, `.join()` and `.interrupt()` as library, which review 1 (B1) refuted. Round 2 scores against one classification shared by every option, the `forms` table in `benchmarks/syntax/measure.ts`, and scores only the forms whose class is settled.

**Scored forms.**
- **Built in, keyword or block.** Every keyword or block form in the packet: `effect fn`, `run`, `fail`, the declarations `service`, `impl` and `layer`, plus the option's own blocks and keywords: `scope`, `fork`, H's `race`, `provide` and `interrupt`, A's clauses, B's, C's and E's blocks, and D's `with`. Duration literals such as `2s` are notation, and either answer is accepted for them.
- **Built in, dot or call form with no general carrier.** These must be recognised by name, so a reader who calls them library has fallen into the #134 trap:
  - `.join()`, `.interrupt()` and `.cancel()` in today, P, A, B, C, D and E;
  - P's `.fork()`;
  - `.provide(App)`, a layer argument, in today and P;
  - D's `provide(App)` call.
- **Library.** Functions that the packet's `library.ef` defines with a body written in the language: `timeoutOr` in H, P, C and D, and `after` in H. A reader who reads them as built in has not used the library file.

**Not scored.** Some dot forms are compiler-known today and library only after a general feature lands: `.timeout`, `.catch`, `.recover`, `.orFail` and `.provide<S>`/`.provideService<S>` (rows units), and P's `.scoped()` and `.race()` (Q11). Either answer counts, and record what the reader says.

**Score.** Score 1 if the reader puts every scored built-in form they mention on the built-in side, and every scored library form they mention on the library side. Score 0.5 for one mistake, and 0 for two or more. A reader who says that a dot form "cannot be told apart from the text" gets credit for it whichever class it is in, because that is the confusion the question measures.
