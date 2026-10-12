# Blind-reader answer key (do not hand to readers)

Packets: `blind-reader/<option>/` for `today`, `P`, `A`, `B`, `C`, `D`, `E`. Each holds the same three programs in that option's syntax, with comments removed, plus `questions.md` (identical in every packet). Packets for P, C and D also hold that option's `library.ef`. Give each reader one packet only. The orchestrator runs the readers; this key scores them.

Score each answer 1 (right), 0.5 (right with a wrong reason, or hedged between the right answer and one wrong one) or 0. Record time and confidence. For the owner's direction, question 1 matters most ("does the 2s include building App?"): an option whose readers miss it is hiding the order of nesting.

| # | Expected answer | Differences by option |
|---|---|---|
| 1 | No. The limit wraps only the dashboard work; App is set up outside it, before the clock starts. | P and today: `.timeoutOr(2s, …)` (today `.timeout(2000)…`) comes before `.provide(App)` in the chain, so the provide wraps the timed recipe. A: `scope provide App, … within 2s` — clauses nest left to right, provide outermost. B, C, E: the `provide` block is outside the deadline. D: `with provide(App)` comes before `with timeoutOr(2s, …)`, so it wraps it. A reader who says "yes" for A is reading the header as one flat list; record it. |
| 2 | `"gave up"`. | Same in every option. |
| 3 | The invoice text becomes `"invoice: slow"`; `dashboard` carries on, waits for the account lookup and returns `"<account> / invoice: slow"`. | Same. |
| 4 | It is cancelled, and its cleanup finishes before `main` returns `"gave up"`. It does not keep running. | P: the scope is `dashboard().scoped()`, which sits inside the deadline, so the losing side's owner is cancelled. A, B, C, E, today: `dashboard`'s `scope { }` is inside the deadline. D: `with scoped()` in `dashboard`. A reader who says "it keeps running" (Go goroutine intuition) or "the timer just stops waiting" (JavaScript `Promise.race` intuition) is wrong. |
| 5 | `Account`, `Invoice` and the time service. | P, C, D: `Clock`. today, A, B, E: `Scheduler`. P only: also acceptable to add "and the caller decides how long `dashboard`'s background work lives, with `.scoped()`". |
| 6 | `child joined`, `timed out`, `recovered`. | Same. |
| 7 | No. `stopped` is cancelled straight away by `stopped.interrupt()`, so its sleep never finishes. | Same. |
| 8 | No. The failure surfaces where `failed.join()` is run and `.catch<Bad>("recovered")` replaces it. (`main` still lists `Bad` in its signature.) | Same. |
| 9 | `"fixture:fixture"`. | Same. |
| 10 | The fixture store, `Memory("fixture")`, because `Fixture` replaces `Store`. | Same. |
| 11 | See the per-option lists below. Score 1 if the reader puts every listed construct on the "built in" side and nothing from the library list there; 0.5 for one mistake. | today is the control. Its dot forms `.timeout`, `.catch`, `.provide`, `.join` and `.interrupt` are compiler-known but look like methods, so "they look like library" counts as wrong for today. That is the trap #134 removes. |
| 12 | Yes. The 2 s lives in `main`, not in `dashboard`; the other team writes their own call with 5 s. | P: `run dashboard().scoped().timeoutOr(5s, "gave up").provide(App).provide<Clock>(LiveClock)`. A: `scope provide App, Scheduler = LiveScheduler within 5s { run dashboard() } otherwise { "gave up" }`. B: `provide App, Scheduler = LiveScheduler { within 5s { run dashboard() } otherwise { "gave up" } }`. C: `provide App, Clock = LiveClock { run dashboard().timeoutOr(5s, "gave up") }`. D: three `with` lines ending `with timeoutOr(5s, "gave up")`, then `run dashboard()`. E: `provide App, Scheduler = LiveScheduler { race { run dashboard() } after 5s { "gave up" } }`. |

## Question 11: constructs per option

| Option | Built in (constructs and declarations) | Library (ordinary functions or methods) |
|---|---|---|
| today | `effect fn`, `run`, `fail`, `scope`, `fork`, `service`, `impl`, `layer`; and compiler-known dot forms `.timeout`, `.catch`, `.provide`, `.join`, `.interrupt` | none |
| P | `effect fn`, `run`, `fail`, `service`, `impl`, `layer`; `2s` is notation for a duration | `.fork()`, `.join()`, `.interrupt()`, `.scoped()`, `.timeoutOr()`, `.provide()`, `.catch()` (kernel methods in a bundled module, or library over them) |
| A | `effect fn`, `run`, `fail`, `scope` with its `provide`, `within` and `otherwise` clauses, `fork`, declarations | `.join()`, `.interrupt()`, `.catch()` |
| B | `effect fn`, `run`, `fail`, `provide { }`, `within { } otherwise { }`, `scope`, `fork`, declarations | `.join()`, `.interrupt()`, `.catch()` |
| C | `effect fn`, `run`, `fail`, `provide { }`, `race { } or { }`, `scope`, `fork`, declarations | `.timeoutOr()`, `.join()`, `.interrupt()`, `.catch()` |
| D | `effect fn`, `run`, `fail`, `with`, `scope`, `fork`, declarations; and `provide(…)`, `provideService<…>(…)`, which look like library calls but are compiler-known | `scoped()`, `timeoutOr()`, `.join()`, `.interrupt()`, `.catch()` |
| E | `effect fn`, `run`, `fail`, `provide { }`, `race { } after d { }`, `scope`, `fork`, declarations | `.join()`, `.interrupt()`, `.catch()` |
