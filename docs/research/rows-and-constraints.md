# Row elision and named constraints in sketch signatures

**Status:** design spike and decision record for [#135](https://github.com/cevr/effra/issues/135), 2026-10-27. It changes no compiler code and does not edit [`docs/spec/`](../spec/README.md). Its decisions go to the sketch checker ([#196](https://github.com/cevr/effra/issues/196)) and the laws spelling ([#162](https://github.com/cevr/effra/issues/162)) as recommendations ([§7](#7-amendments-recommended-to-196-and-162)). The corpus, the rewrites and the counting script are under [`rows-and-constraints/corpus/`](rows-and-constraints/corpus/). Every number in a table below comes from `measure.py`; the script refuses a stale table (`--check`).

## Answer

The sketch can come within reach of Go's signatures for the code users write, without losing a checked row:

<!-- measure.py:begin answer -->
- **Row elision lands** for unexported functions, function literals and `TestX` functions. It removes 174 of the 289 row entries in user-facing signatures. In the Go-controlled loop programs, signature tokens fall from 243 to 157, against 175 for Go.
- **Named constraints land** in Go's bracket list, `func Max[T cmp.Ordered](a, b T) T`. A bracket list is written only when it carries a constraint or fixes an order for explicit type arguments. 0 user-facing signatures in the corpus need a constraint; 4 library signatures do.
- **Row parameters on function types stay explicit.** All 33 row-variable occurrences are in library code (0 user-facing). Each simplification that would remove them hides a failure from a reader or loses a resource rule.
- **Refused:** row elision for `main`, for laws and for methods; constraints inferred from bodies; one combined row variable; implicit callback rows.
<!-- measure.py:end answer -->

The complexity moves out of what users write and stays in what the checker checks. Library authors still write `E` and `R`, as Effect and Koka library authors do.

## Owner direction

- **2026-10-12, on #135.** "I feel like the language design is leading more typescript than go, especially with the generics … they look very expressive. I'm not sure if they look simple … It could be that that's the best way, but just to put some thoughts on there."
- **2026-10-12, on #135 (composability).** "the primitives have to be able to be stacked on top of each other and grow together." The issue comment asks every proposal to show lossless wrapping, nesting, first-class recipes, orthogonality and composition laws ([§8](#8-composability)).
- **2026-10-26, realignment.** Go is the only target, and sketch 8 decided implicit type parameters. Rows stayed open.
- **NORTH_STAR tiebreaks.** "Go-simple signatures beat TypeScript-style expressiveness … simplify what users write, not what the compiler checks. Elide only pass-through rows, never declared requirements, and measure each simplification against a corpus." "Explicit contracts beat terse syntax: reject shorter syntax that hides a requirement."
- **AGENTS.md.** "Public effect contracts are explicit. Do not silently admit missing services or undeclared errors."
- **Owner rules (memory, applied).** Generics lean Go-simple: implicit kinds, row elision, named constraints, measured against a corpus. General type features are not special casing; prefer them.

## Method

**Corpus.** Design sketch evaluations 8 and 9 (the "five" program set, the sketch-9 programs and the loop programs), sketch 10, every fenced `go` example in `docs/spec/`, and the fixtures of the merged prototype ([#191](https://github.com/cevr/effra/issues/191), `prototypes/sketch9/testdata`). The loop programs of evaluation 8 are earlier copies of evaluation 9's and are not counted twice. Copies of the files outside the repository are in `corpus/before/`, pinned by `before.sha256`.

**Library reconstruction.** The corpus calls library functions it does not declare: `timeout.After`, `retry.On`, `tasks.Set`, `build.Scope`, `sync.Atomically`, `json.Decode`, `http.ReadJSON`, `slices.SortFunc`, `cmp.Compare`, `min` and others. Their signatures decide whether row variables and constraints reach users. [`corpus/before/library/implied.go.sketch`](rows-and-constraints/corpus/before/library/implied.go.sketch) states each one, with its call site. It is a reconstruction for this spike, not a design.

**Definitions.**
- A *signature* is a function or method declaration, a capability method, a function literal's header, a generic or machine type declaration, or a `wire … uses` clause. A function type inside a parameter list belongs to its declaration.
- A *row entry* is one name a user writes in a row: each error type in `error[A | B]`, each capability in `uses A, B`, and each row variable (`E`, `R`) wherever it is written.
- *User-facing* code is application code. *Library-internal* code is a library package (`tasks.Map`, the reconstruction, and the two library functions shown in `docs/spec`). *Fixtures* are compiler test inputs; a *negative* fixture carries `// want`.

**Rewrites.** `measure.py` parses every signature with one parser, applies each candidate as a mechanical rewrite into `corpus/after/<candidate>/`, then parses the rewritten text again and counts from that parse.

```sh
python3 -I docs/research/rows-and-constraints/corpus/measure.py --check   # exit 0 when every table, counts.json and after/ are current
python3 -I docs/research/rows-and-constraints/corpus/measure.py --write   # regenerate them
```

<!-- measure.py:begin sources -->
| Group | Files or fenced blocks | Lines |
| --- | --- | --- |
| eval8-five | 6 | 556 |
| eval9 | 5 | 389 |
| eval9-loops | 7 | 373 |
| library | 1 | 133 |
| sketch10 | 6 | 251 |
| spec | 21 | 159 |
| fixtures | 40 | 423 |
| fixtures (negative) | 69 | 688 |
<!-- measure.py:end sources -->

**Limits.**
- The parser reads the sketch subset in the corpus, not the full grammar. It is checked by re-parsing every rewrite.
- The rewrite removes a row; it does not infer one. It assumes each written row is the row that inference computes. Reviews 8 and 9 audited these rows. A written row that is larger than the inferred row stays the declared row under the rule in [§2](#2-candidate-1-row-elision-for-unexported-functions-and-tests), so the assumption matters only for the after/ files, never for the rule.
- Token counts are a proxy for reading cost. They count identifiers and punctuation in the signature, from `func` to the end of the `uses` clause.

## 1. Inventory

Every signature, by audience and role:

<!-- measure.py:begin inventory -->
| Audience / role | Signatures | With rows | Row entries | With type parameters |
| --- | --- | --- | --- | --- |
| fixture / exported function | 1 | 0 | 0 | 0 |
| fixture / interface method | 12 | 2 | 2 | 0 |
| fixture / main | 40 | 36 | 36 | 0 |
| fixture / method | 11 | 2 | 2 | 0 |
| fixture / unexported function | 35 | 25 | 30 | 0 |
| fixture (negative) / exported function | 1 | 0 | 0 | 0 |
| fixture (negative) / interface method | 32 | 11 | 11 | 0 |
| fixture (negative) / main | 69 | 50 | 50 | 0 |
| fixture (negative) / method | 31 | 10 | 10 | 0 |
| fixture (negative) / unexported function | 48 | 35 | 45 | 0 |
| library / exported function | 23 | 13 | 42 | 19 |
| library / interface method | 3 | 0 | 0 | 0 |
| library / method | 10 | 6 | 8 | 8 |
| library / type declaration | 5 | 1 | 1 | 5 |
| library / unexported function | 1 | 0 | 0 | 1 |
| user / exported function | 20 | 11 | 37 | 0 |
| user / interface method | 23 | 15 | 15 | 0 |
| user / law | 3 | 1 | 1 | 0 |
| user / literal | 13 | 2 | 2 | 0 |
| user / main | 14 | 13 | 39 | 0 |
| user / method | 27 | 16 | 18 | 0 |
| user / test | 16 | 16 | 50 | 0 |
| user / type declaration | 5 | 1 | 2 | 2 |
| user / unexported function | 62 | 46 | 122 | 0 |
| user / wire exposure | 2 | 2 | 3 | 0 |
<!-- measure.py:end inventory -->

User-facing signatures by source:

<!-- measure.py:begin inventory-groups -->
| Source (user-facing) | Signatures | With rows | Row entries | With type parameters |
| --- | --- | --- | --- | --- |
| eval8-five | 64 | 44 | 107 | 2 |
| eval9 | 39 | 31 | 63 | 0 |
| eval9-loops | 34 | 25 | 65 | 0 |
| sketch10 | 26 | 16 | 36 | 0 |
| spec | 22 | 7 | 18 | 0 |
<!-- measure.py:end inventory-groups -->

Every corpus signature with a type or row parameter:

<!-- measure.py:begin tparams -->
| Signature | Audience | Role | Parameters (kind) | Bracket list | Constraint |
| --- | --- | --- | --- | --- | --- |
| `Attempt` (eval8-five/recipes-retry.go.sketch:5) | user | type declaration | T (type) | T | none |
| `RetryPolicy` (eval8-five/recipes-retry.go.sketch:10) | user | type declaration | T (type) | T | none |
| `Map` (eval9-loops/tasks-map.go.sketch:8) | library | exported function | A (type), B (type), E (error), R (uses) | none | none |
| `After` (library/implied.go.sketch:19) | library | exported function | A (type), E (error), R (uses) | none | none |
| `On` (library/implied.go.sketch:28) | library | exported function | A (type), E (error), F (error), R (uses) | F | error |
| `Go` (library/implied.go.sketch:37) | library | method | E (error), R (uses) | none | none |
| `Acquire` (library/implied.go.sketch:47) | library | exported function | A (type), E (error), R (uses) | none | none |
| `Go` (library/implied.go.sketch:48) | library | exported function | R (uses) | none | none |
| `TxRef` (library/implied.go.sketch:53) | library | type declaration | T (type) | T | none |
| `Ref` (library/implied.go.sketch:54) | library | type declaration | T (type) | T | none |
| `Load` (library/implied.go.sketch:57) | library | method | T (type) | T | none |
| `Store` (library/implied.go.sketch:58) | library | method | T (type) | T | none |
| `Atomically` (library/implied.go.sketch:59) | library | exported function | A (type), E (error) | none | none |
| `Update` (library/implied.go.sketch:63) | library | method | T (type) | T | none |
| `Load` (library/implied.go.sketch:64) | library | method | T (type) | T | none |
| `Codec` (library/implied.go.sketch:70) | library | type declaration | T (type) | T | none |
| `Decode` (library/implied.go.sketch:73) | library | method | T (type) | T | none |
| `Encode` (library/implied.go.sketch:74) | library | method | T (type) | T | none |
| `Decode` (library/implied.go.sketch:75) | library | exported function | T (type) | T | none |
| `Encode` (library/implied.go.sketch:76) | library | exported function | A (type) | none | none |
| `ReadJSON` (library/implied.go.sketch:85) | library | exported function | T (type) | T | none |
| `JSON` (library/implied.go.sketch:86) | library | exported function | A (type) | none | none |
| `Fail` (library/implied.go.sketch:88) | library | exported function | A (type) | none | none |
| `Routes` (library/implied.go.sketch:90) | library | type declaration | R (uses) | R | none |
| `Serve` (library/implied.go.sketch:92) | library | method | R (uses) | R | none |
| `SortFunc` (library/implied.go.sketch:97) | library | exported function | A (type) | none | none |
| `Compare` (library/implied.go.sketch:102) | library | exported function | T (type) | none | T.Compare |
| `Or` (library/implied.go.sketch:103) | library | exported function | T (type) | none | T.Equal |
| `min` (library/implied.go.sketch:108) | library | unexported function | T (type) | none | T.Compare |
| `Values` (library/implied.go.sketch:113) | library | exported function | A (type) | none | none |
| `Loader` (library/implied.go.sketch:119) | library | type declaration | K (type), V (type) | K, V | none |
| `Resolve` (library/implied.go.sketch:123) | library | exported function | K (type), P (type), V (type) | none | P.FindMany |
| `Equal` (library/implied.go.sketch:133) | library | exported function | A (type) | none | none |
| `After` (docs/spec/runtime.md#block1:1) | library | exported function | E (error), R (uses), T (type) | none | none |
| `Map` (docs/spec/types.md#block4:1) | library | exported function | A (type), B (type), E (error), R (uses) | none | none |
<!-- measure.py:end tparams -->

**What the inventory shows.**

<!-- measure.py:begin inventory-notes -->
- Users write rows, not generics. 289 row entries are in user-facing signatures; 2 user-facing declarations have a type parameter (`Attempt`, `RetryPolicy`, the generic structs of the retry recipe).
- Every row variable is in library code: 11 declarations, 0 of them user-facing.
- Unexported functions and tests hold 172 of the 289 user-facing entries; `main` holds 39. Exported functions, methods and capability methods hold 70.
<!-- measure.py:end inventory-notes -->

The row variables sit in `tasks.Map`, the reconstruction and the two library signatures of `docs/spec`.

## 2. Candidate 1: row elision for unexported functions and tests

**Rule.** The checker infers both rows of an unexported function, of a function literal and of a `TestX` function when the signature writes none. A row that is written is the declared row: the checker proves the body within it, and callers see it. Exported functions, `main`, `LawX` functions, methods and capability methods spell their rows. `docs/spec/rows.md` already designs inference for unexported functions (Capability rows, rule 6); this candidate measures it and adds literals and tests.

<!-- measure.py:begin c1 -->
| Step | User signatures elided | User row entries removed | Library row entries removed |
| --- | --- | --- | --- |
| unexported functions and literals | 48 | 124 | 0 |
| plus tests (lands) | 64 | 174 | 0 |
| plus `main` (refused) | 77 | 213 | 0 |
<!-- measure.py:end c1 -->

By role, with the landing rule applied:

<!-- measure.py:begin c1-roles -->
| User role | Signatures | Row entries before | Row entries after |
| --- | --- | --- | --- |
| exported function | 20 | 37 | 37 |
| interface method | 23 | 15 | 15 |
| law | 3 | 1 | 1 |
| literal | 13 | 2 | 0 |
| main | 14 | 39 | 39 |
| method | 27 | 18 | 18 |
| test | 16 | 50 | 0 |
| type declaration | 5 | 2 | 2 |
| unexported function | 62 | 122 | 0 |
| wire exposure | 2 | 3 | 3 |
| **all user-facing** | 185 | 289 | 115 |
<!-- measure.py:end c1-roles -->

**Against Go.** The loop programs fetchall, ledger, poller and wordcount have hand-written Go controls. Go threads `context.Context` and passes services as parameters; the sketch writes them as rows:

<!-- measure.py:begin go -->
| Program | Function | Go tokens | Sketch tokens before | Sketch tokens after candidate 1 |
| --- | --- | --- | --- | --- |
| fetchall.go.sketch | `fetch` | 16 | 20 | 7 |
| fetchall.go.sketch | `fetchAll` | 20 | 24 | 11 |
| fetchall.go.sketch | `fetchOnce` | 16 | 26 | 7 |
| fetchall.go.sketch | `main` | 4 | 20 | 20 |
| ledger.go.sketch | `apply` | 14 | 15 | 15 |
| ledger.go.sketch | `main` | 4 | 6 | 6 |
| ledger.go.sketch | `move` | 13 | 16 | 12 |
| poller.go.sketch | `batch` | 15 | 16 | 10 |
| poller.go.sketch | `poll` | 24 | 13 | 7 |
| poller.go.sketch | `Run` | 20 | 22 | 22 |
| poller_test.go.sketch | `TestRunBatches` | 9 | 12 | 4 |
| wordcount.go.sketch | `main` | 4 | 24 | 24 |
| wordcount.go.sketch | `topWords` | 16 | 29 | 12 |
| **total** |  | 175 | 243 | 157 |
<!-- measure.py:end go -->

<!-- measure.py:begin go-text -->
- `fetch`
  - Go: `func fetch(ctx context.Context, url string) (result, error)`
  - before: `func fetch(url string) (Result, error[Unauthorized]) uses http.Client, Scheduler`
  - after: `func fetch(url string) Result`
- `fetchAll`
  - Go: `func fetchAll(ctx context.Context, urls []string) ([]result, error)`
  - before: `func fetchAll(urls []string) ([]Result, error[Unauthorized]) uses http.Client, Scheduler`
  - after: `func fetchAll(urls []string) []Result`
- `fetchOnce`
  - Go: `func fetchOnce(ctx context.Context, url string) (int, error)`
  - before: `func fetchOnce(url string) (int, error[Unauthorized | Failed | timeout.Expired]) uses http.Client, Scheduler`
  - after: `func fetchOnce(url string) int`
- `main`
  - Go: `func main()`
  - before: `func main() error[Unauthorized] uses http.Client, os.Args, Scheduler, Console`
  - after: `func main() error[Unauthorized] uses http.Client, os.Args, Scheduler, Console`
- `apply`
  - Go: `func apply(accounts []account, transfers []transfer) int`
  - before: `func apply(accounts *[]Account, transfers []Transfer) int`
  - after: `func apply(accounts *[]Account, transfers []Transfer) int`
- `main`
  - Go: `func main()`
  - before: `func main() uses Console`
  - after: `func main() uses Console`
- `move`
  - Go: `func move(from, to *account, amount int64) error`
  - before: `func move(from, to *Account, amount int64) error[Insufficient]`
  - after: `func move(from, to *Account, amount int64)`
- `batch`
  - Go: `func batch(in <-chan Reading, size int, flush Sink) error`
  - before: `func batch(in chan Reading, size int) error[FlushFailed] uses Sink`
  - after: `func batch(in chan Reading, size int)`
- `poll`
  - Go: `func poll(parent, ctx context.Context, src Source, every time.Duration, out chan<- Reading) error`
  - before: `func poll(out chan Reading) uses Source, Clock, Console`
  - after: `func poll(out chan Reading)`
- `Run`
  - Go: `func Run(ctx context.Context, src Source, flush Sink, d time.Duration) error`
  - before: `func Run(d time.Duration) error[FlushFailed] uses Source, Sink, Clock, Scheduler, Console`
  - after: `func Run(d time.Duration) error[FlushFailed] uses Source, Sink, Clock, Scheduler, Console`
- `TestRunBatches`
  - Go: `func TestRunBatches(t *testing.T)`
  - before: `func TestRunBatches() error[FlushFailed] uses testing.T`
  - after: `func TestRunBatches()`
- `main`
  - Go: `func main()`
  - before: `func main() error[fs.NotFound | fs.ReadFailed] uses fs.Files, os.Args, Console`
  - after: `func main() error[fs.NotFound | fs.ReadFailed] uses fs.Files, os.Args, Console`
- `topWords`
  - Go: `func topWords(path string, n int) ([]wordCount, error)`
  - before: `func topWords(path string, n int) ([]WordCount, error[fs.NotFound | fs.ReadFailed]) uses fs.Files`
  - after: `func topWords(path string, n int) []WordCount`
<!-- measure.py:end go-text -->

After elision, each unexported sketch signature and the test are shorter than their Go controls, because the context and the service parameters are gone. The one exception is `apply`, which has no row and whose borrow `*[]Account` costs one token. The exported `Run` and every `main` keep their rows and stay longer than Go: that is the price of an explicit public contract.

### Honesty

**Meaning unchanged.** Inference computes the least row the body needs, by the composition table of `docs/spec/rows.md`. A row that a user writes stays declared, so a loose row is never silently tightened and the fixtures classified `written` in the [honesty table](#5-honesty-per-candidate) keep their S101 and S102. Every exported function still spells the union of what its unexported callees can do, so no public contract changes when an unexported body changes.

**Tests land.** Only the test runner calls a `TestX`. A failure that escapes it fails the test, which is the report a user wants. `uses testing.T` is always supplied by the runner, so omitting it hides no requirement.

**Literals land.** A literal's rows are checked against the function type it is passed to, so the parameter type states the contract. The corpus already writes literals this way (`func() { return probe("flaky") }`, `func(a, b) int`).

**`main` is refused.** `main`'s rows are the program's contract with its host: its `uses` clause names the capabilities the host wire must supply (README Gap 5), and its error row names the failures that end the process (Gap 4). Inferring them would move the S101/S102 fixtures classified `main` in the [honesty table](#5-honesty-per-candidate) from a function's row to the entry check, and `wire-initializer-error` would be accepted, because an escaping failure would join an inferred `main` row. That is an undeclared error admitted silently.

**Laws are refused.** A law's `uses` clause chooses the providers that `ef test` checks it against (`docs/spec/types.md`, Laws rule 2), so it is a declared requirement with behaviour.

**Methods are refused for v0.** When no exported declaration exposes an unexported type, another package reaches its methods only through an interface, so their rows could be inferred and checked against that interface. But Go lets an exported function return an unexported type, and then the inferred rows would leak into the public surface. Deciding reachability costs more than the method entries it would save (see the [decision table](#6-decision)).

**Exported surface closure (new rule).** Elision can leak a row into an exported declaration through a type. An exported machine type takes its rows from its step function: `actor.Start(CircuitBreaker{}, 16).Ask(…)` fails with `ProbeUnavailable` because `breakerStep` does. When the step is unexported and writes no row, nothing on the exported surface states that row. The [honesty table](#5-honesty-per-candidate) lists the corpus's exported machine types, and the after/ corpus shows the leak (`after/row-elision/spec/runtime-block2.go.sketch`). Rule: a function that an exported declaration names as part of its type, such as a machine's step, spells its rows even when it is unexported.

**Reading cost.** On an exported function, a missing error row means "cannot fail". On an unexported function it means "inferred". A reader of `func helper() string` cannot tell whether it fails. The language server's inlay hints and `ef inspect` show the inferred rows (pillar 6). The fixture `error-as-value` relies on the old meaning and must be restated with an exported function.

**Resources.** Inference covers rows only. Ownership, moves and lends stay decided by types (`docs/spec/resources.md`). A literal passed as a release callback is still checked against an infallible function type.

**Costs.**
- The checker computes rows over the package's unexported call graph, one strongly connected component at a time. Error and capability rows are finite sets ordered by inclusion, so the fixpoint terminates.
- An edit to an unexported body can change the rows of other unexported functions in the package, never an exported signature. Export data and dependent packages are unaffected. Elixir names this trade-off for full inference ([§10](#10-prior-art)); Effra keeps it inside one package.
- Diagnostics move: an over-wide catch arm on an inferred row is S108 at the caller rather than an error in the callee.

## 3. Candidate 2: named constraints

**Rule.** A constraint is a named interface: `comparable`, `cmp.Ordered`, or an interface with methods. It is written in Go's bracket list, `func Max[T cmp.Ordered](a, b T) T`. A parameter without a constraint stays implicit. A bracket list appears only where it carries information:
- a constraint on a parameter;
- the order of parameters that a caller supplies explicitly (`json.Decode[UserId]`, `http.ReadJSON[Order]`, `retry.On[Failed | timeout.Expired]`, `Attempt[string]`);
- a row parameter that appears only in the bracket list, kinded by the constraint `error`: `func On[F error](…)`. Effra's error types implement Go's `error`, so the reading is Go's.

The before spelling is the method-entry form of the #116 and #125 notes, `where T.Compare: func(T, T) int`. The rewrite is in `after/named-constraints/`.

<!-- measure.py:begin c2 -->
| Measure | Value |
| --- | --- |
| user-facing signatures that need a constraint | 0 |
| library signatures that need a constraint | 4 |
| method entries written before (`where T.M: …`) | 4 |
| named constraints written after (`[T cmp.Ordered]`) | 4 |
| signature tokens before / after | 101 / 67 |
| Gleam alternative: function parameters added | 4 |
| `T.Equal` calls on a non-comparable `[]int` | 1 |
<!-- measure.py:end c2 -->

<!-- measure.py:begin c2-list -->
- `func Compare(a, b T) int where T.Compare: func(T, T) int`
  → `func Compare[T Ordered](a, b T) int`
- `func Or(vals ...T) T where T.Equal: func(T, T) bool`
  → `func Or[T comparable](vals ...T) T`
- `func min(a, b T) T where T.Compare: func(T, T) int`
  → `func min[T cmp.Ordered](a, b T) T`
- `func Resolve(source P, w Window, m Max) Loader[K, V] where P.FindMany: func(P, []K) map[K]Option[V]`
  → `func Resolve[P Batched[K, V]](source P, w Window, m Max) Loader[K, V]`
<!-- measure.py:end c2-list -->

**Which names are parameters (README Gap 23).**

<!-- measure.py:begin gap23 -->
| Measure | Value |
| --- | --- |
| package-level declarations with a one-letter name | 1 (library/implied.go.sketch: T) |
| parameter names used (kind: signatures) | A (type): 12, B (type): 2, E (error): 8, F (error): 1, K (type): 2, P (type): 1, R (uses): 10, T (type): 17, V (type): 2 |
| bracket lists written (explicit instantiation or constraint) | 17 |
| calls with explicit type arguments (`json.Decode[…]`, `http.ReadJSON[…]`, `retry.On[…]`) | 14 |
| bare `T.Equal` calls on the imported capability `testing.T` | 19 |
<!-- measure.py:end gap23 -->

Recommended rule: in a type position, an identifier that is one upper-case letter, optionally followed by digits, and that the package does not declare is a type parameter (Koka's rule, [§10](#10-prior-art)). Its kind follows its position: inside `error[…]` an error row, after `uses` a capability row, elsewhere a type. A package may not declare a type, capability or wire with such a name. An imported name such as `testing.T` stays qualified in type positions; the bare capability name `T` that `uses testing.T` brings into scope is an expression, never a type, so the bare `T.Equal` calls counted above do not collide with a type parameter `T`.

**Honesty.** A constraint changes no row and hides nothing; it is a requirement on a type, written where Go writes it. Method constraints keep resolving through the type's owner package (`docs/spec/types.md`, Generics rule 2).

**Alternatives refused.**
- *Gleam: no constraints, explicit functions.* It works for ordering (`SortFunc` keeps an explicit compare, as Go keeps both `Sort` and `SortFunc`). It cannot express `comparable` for map keys and `==`, and it adds a parameter to each constrained library signature.
- *Roc: constraints inferred from bodies.* No user-facing signature needs a constraint, so inference has nothing to remove, and an inferred constraint on an exported generic would be a hidden requirement.

**Findings for the library.**
- Go has no generic methods (proposal 43651, "Methods may not take additional type arguments"). `build.Scope.Acquire` and `testing.T.Equal` are generic operations called as capability methods in the corpus. A Go interface cannot carry them, so the library writes them as package functions that charge the capability.
- `T.Equal` compares `[]int` in the corpus (counted in the table above). Slices are not `comparable`, so `Equal` takes no `comparable` constraint: it is structural equality over any type.

**Costs.** Two ways to introduce a type parameter, implicit and bracketed. The rule above limits the bracket list to the cases where Go code would need it anyway, and every bracket list is valid Go spelling.

## 4. Candidate 3: row parameters on function types

**3a, explicit row parameters (today's spelling).** `func Map(xs []A, limit int, f func(A) (B, error[E]) uses R) ([]B, error[E]) uses R`. Each pass-through row variable is written at the callback and at the result.

**3b, one combined row variable.** One variable covers both rows of a callback, as Koka's `e` does.

**3c, implicit callback rows.** A function-typed parameter that writes no rows is row-polymorphic, and its rows join the declaration's rows. `docs/spec/types.md` Generics rule 3 ("rows … may be elided only where they pass through unchanged") would allow it. The rewrite is in `after/function-type-rows/`.

<!-- measure.py:begin c3 -->
| Measure | Value |
| --- | --- |
| declarations with row variables / user-facing among them | 11 / 0 |
| row-variable occurrences in them | 33 |
| function types with written rows / with more than one `uses` entry | 11 / 0 |
| user-facing function types with a row variable | 0 |
| entries removed by implicit callback rows (3c) | 30 |
| library row entries before / after 3c | 51 / 21 |
| user row entries before / after 3c | 289 / 289 |
| entries saved by one combined row variable (3b) | 12 |
| callback row variables absorbed, not passed through | 1 (tasks.Go: E) |
| empty-row callbacks in generic declarations (meaning flips under 3c) | 7 |
<!-- measure.py:end c3 -->

Callbacks whose empty rows are part of the contract:

<!-- measure.py:begin c3-empty -->
- `build.Acquire: func(A)`
- `sync.Update: func(v *T)`
- `slices.SortFunc: func(A, A) int`
- `cmp.Compare: func(T, T) int`
- `cmp.Or: func(T, T) bool`
- `builtin.min: func(T, T) int`
- `batch.Resolve: func(P, []K) map[K]Option[V]`
<!-- measure.py:end c3-empty -->

**Honesty of 3c: refused.**
- It hides failures in the result. `func After(d duration, f func() A) (A, error[Expired]) uses Scheduler` reads as "fails only with `Expired`", but `After` passes on every failure of its block. `retry.On` becomes `func On[F error](p Policy, f func() A) A uses Scheduler`, which reads as infallible.
- It flips the meaning of `func(A)` in a parameter. Today `release func(A)` in `build.Acquire` is an infallible function type. Under 3c it is row-polymorphic, and a fallible `Close` would pass through the release: that breaks the resource rule that a release cannot fail with a typed error (decision #6). Every callback listed above relies on an empty row.
- It cannot state an absorbed row. `tasks.Group.Go` passes `R` through and absorbs `E` into its restart policy. 3c has no way to say "these failures stop here".
- It removes library entries only, so it simplifies nothing users write.

**Honesty of 3b: refused.** The two rows are transformed differently by the same combinators: `timeout.After` adds `Expired` to the error row and `Scheduler` to the capability row; `sync.Atomically` discharges `Tx` from the capability row and passes the error row; `tasks.Group.Go` passes the capability row and absorbs the error row; a `catch` removes error types and never capabilities. One combined variable cannot say any of these. The entries it saves are all in library code.

**3a lands.** Row parameters are written wherever they occur, with implicit kinds by position. MoonBit's anonymous `raise?` marker ([§10](#10-prior-art)) would rename the variables, not remove them, so it saves no entry.

**Grammar finding.** In a parameter list, `f func() uses A, B, g func()` is ambiguous: `B` could be a capability or the next parameter. The corpus has no function type with more than one `uses` entry. Recommendation: in a function type inside a parameter list, a `uses` clause with more than one entry is parenthesised, `uses (A, B)`; a declaration's own `uses` clause is unchanged.

## 5. Honesty, per candidate

<!-- measure.py:begin honesty -->
| Check | Value |
| --- | --- |
| exported machine types whose rows come from their step | 3 (eval8-five/machine-breaker.go.sketch: type CircuitBreaker; sketch10/sketch-10.md#block0: type Breaker; docs/spec/runtime.md#block2: type Breaker) |
| laws (rows kept) | eval9-loops/ledger.go.sketch: LawMoveConservesMoney (0 entries); docs/spec/types.md#block5: LawRoundTrip (0 entries); docs/spec/types.md#block5: LawFindAfterPut (1 entries) |
| user-facing methods (rows kept): signatures / row entries | 27 / 18 |
| prototype negative fixtures for S101 or S102 | 10 |
| … that rely on: absent | 1 (error-as-value) |
| … that rely on: main | 5 (function-uses-control, unprovided, wire-initializer, wire-initializer-error, wire-sibling) |
| … that rely on: wire | 2 (named-wire-failure, named-wire-requirement) |
| … that rely on: written | 2 (missing-capability, undeclared-error) |
| `wire … uses` exposure clauses (not in docs/spec) | eval8-five/layers.go.sketch: wire App uses Account, Invoice; eval9-loops/wire-effects.go.sketch: wire Live uses Orders |
| library signatures whose rows are derived from a type's codec (no candidate states them) | 6 |
<!-- measure.py:end honesty -->

- **Candidate 1.** Lands for unexported functions, literals and tests: every exported declaration still spells its rows, and written rows stay declared. Refused for `main`, laws and methods. Requires the exported-surface rule for machine steps.
- **Candidate 2.** Lands: a constraint is visible where Go writes it and changes no row.
- **Candidate 3.** 3c and 3b refused: 3c hides failures and loses the infallible-release rule; 3b cannot state how combinators transform each row. 3a stays.
- **Linearity.** No candidate changes resource rules. 3c would have, through the release callback, which is one more reason it is refused.

## 6. Decision

<!-- measure.py:begin decision -->
| Candidate | Decision | Measured | Cost |
| --- | --- | --- | --- |
| 1. Infer rows of unexported functions and literals | **Land** | 124 user entries removed | package-level row fixpoint; a missing row reads as "inferred" on unexported code; inlay hints needed |
| 1. Infer rows of `TestX` | **Land** | 50 more user entries removed | none beyond the above |
| 1. Infer rows of `main` | Refuse | would remove 39 entries; 5 of 10 S101/S102 fixtures change, `wire-initializer-error` is accepted | the host contract and exit failures become implicit |
| 1. Infer rows of `LawX` | Refuse | 1 entry kept | a provider law's `uses` selects the providers it is checked against |
| 1. Infer rows of methods on unexported types | Refuse for v0 | 18 entries kept | needs export reachability; leaks rows when an exported function returns the type |
| 1. Machine steps named by an exported type spell rows | **Land (new rule)** | 3 exported machine types | one more rule in the checker |
| 2. Named constraints in Go's bracket list | **Land** | 0 user, 4 library signatures; 101 → 67 signature tokens | two ways to introduce a parameter, limited to Go's cases |
| 2. Gleam: explicit functions, no constraints | Refuse | 4 parameters added | no `comparable`; callers pass compare functions |
| 2. Roc: constraints inferred | Refuse | 0 user signatures to simplify | a hidden requirement on exported generics |
| 2. Gap 23 naming rule and `[F error]` | **Land** | 1 one-letter declaration (library `testing.T`); 14 calls with explicit type arguments | one-letter type names reserved |
| 3a. Explicit row parameters, kinds by position | **Land (status quo)** | 33 row-variable occurrences, 0 in user-facing function types | library authors write `E` and `R` |
| 3b. One combined row variable | Refuse | 12 library entries saved | cannot state per-row transformations |
| 3c. Implicit callback rows | Refuse | 30 library entries saved, 0 user | hides failures; 7 empty-row callbacks flip, among them the infallible release; cannot absorb |
| Grammar: `uses (A, B)` in a function-typed parameter | **Land** | 0 corpus occurrences | none in the corpus |
<!-- measure.py:end decision -->

## 7. Amendments recommended to #196 and #162

This note does not edit `docs/spec/`. These are the recommended amendments, for the owners of #196 and #162 to apply.

**To #196, the sketch checker.**
1. `rows.md`, Capability rows rule 6: infer both rows of unexported functions, function literals and `TestX` functions when none is written. A written row is the declared row. `main`, `LawX`, methods and capability methods spell their rows. A function named by an exported type, such as a machine's step, spells its rows.
2. `types.md`, Generics rule 1 and README Gap 23: the naming rule of [§3](#3-candidate-2-named-constraints), kinds by position, and the bracket list for constraints, explicit instantiation and `[F error]`.
3. `types.md`, Generics rule 3: replace "Rows in a generic signature may be elided only where they pass through unchanged" with "Row parameters are written wherever they occur. Only the inference of rule 1 elides rows." The current text admits 3c.
4. `grammar.md`: a function type inside a parameter list parenthesises a `uses` clause of more than one entry.
5. Fixtures: keep every S101/S102 negative fixture. With `main` explicit, only `error-as-value` changes meaning. Restate `error-as-value` with an exported function, and add a positive fixture where an unexported function and a literal infer their rows.
6. Library: generic capability operations (`build.Scope.Acquire`, `testing.T.Equal`) are package functions, because Go has no generic methods.
7. Some library signatures, counted in the honesty table, have rows derived from a type's codec: `json.Decode[Order]` has the rows of every `ParseX` it reaches. No candidate states them; they stay compiler-derived until [#49](https://github.com/cevr/effra/issues/49) decides codec discovery (README Gap 20).
8. Two sketches write `wire App uses Account, Invoice` to expose part of a wire. `docs/spec/rows.md` has no such clause. Decide it with wire composition (README Gaps 6 and 22).

**To #162, the laws spelling.**
1. A law spells its rows. Its `uses` clause lists the capabilities whose providers it is checked against; it is never inferred.
2. A law over a constraint uses the bracket list: `func LawTransitive[T Order](a, b, c T) bool`, where `Order` is an interface used as a constraint. The runner discharges it at each instantiation the package reaches, as `docs/research/laws.md` §1.3 describes.
3. `==` on a type parameter requires `comparable` in the bracket list; a law on a non-comparable type compares through a method named in its constraint.

## 8. Composability

- **Lossless wrapping.** An unexported wrapper of any function infers the wrapped rows exactly. An exported wrapper spells them, and the checker proves them. A wrapper of a generic combinator writes the same `E` and `R` as the combinator (3a), so no row is lost through a library layer.
- **Nesting.** Inference composes through nested literals and `with` blocks by the composition table of `docs/spec/rows.md`; a literal inside an unexported function is inferred inside its enclosing inference.
- **First-class recipes.** Function types keep their rows (3a), so a stored recipe such as `Attempt.Work func() (T, error[Transient]) uses Console` states its contract. 3c would have weakened recipes, because a function type would stop saying what it requires.
- **Orthogonality.** Inference, constraints and row parameters are independent rules; none names a library item.
- **Laws.** Inference is idempotent and agrees with declarations: for an unexported `f` with a written row `D`, the inferred row `I` satisfies `I ⊆ D`, and writing `I` in place of nothing changes no caller. Erasing every unexported row and inferring it again gives the same rows (the after/ corpus is a test of this once the checker exists).

## 9. Pillars

| Pillar | Effect |
| --- | --- |
| P1 traceability | unchanged; rows stay in the checked model and in `ef inspect` |
| P2 Effect's guarantees | unchanged; every row is still checked |
| P3 Go simplicity | user signatures lose most of their row entries ([§2](#2-candidate-1-row-elision-for-unexported-functions-and-tests)); constraints use Go's spelling |
| P4 laws | laws keep their rows; constraint spelling given to #162 |
| P5 error and dependency tracking | exported and entry contracts stay explicit; inferred rows stay checked |
| P6 agent interpretability | needs inlay hints and `ef inspect` for inferred rows; one rule per name kind |
| P7 performance | none: rows erase, constraints lower to Go constraints, inference is compile-time only |

## 10. Prior art

Each source is cited at the commit read on 2026-10-27.

- **Go** (go1.27.0 toolchain; [proposal 43651](https://github.com/golang/proposal/blob/83ee2aaf9fdcc1a1cf4bb4d20216fc979635cf04/design/43651-type-parameters.md) at `83ee2aaf`). Type parameters are declared in a bracket list with a constraint interface, `func Compare[T Ordered](x, y T) int` (`src/cmp/cmp.go:40`); `Ordered` is a type set (`cmp.go:18`); `comparable` is predeclared (proposal lines 933-955). Type inference is "a convenience feature" (line 1175) and never infers a constraint. Methods may not take type parameters (lines 600-603). The standard library's own signatures are heavier than user code: `slices.Sort[S ~[]E, E cmp.Ordered](x S)` (`src/slices/sort.go:16`). Rob Pike's ["Less is exponentially more"](https://commandcenter.blogspot.com/2012/06/less-is-exponentially-more.html) (2012) is the design note behind the small feature set. Effra takes the bracket list and the named constraints, keeps implicit unconstrained parameters, and adds rows that Go threads by hand as `context.Context` and `error`.
- **Gleam** (compiler `7f97af0e`, 1.19.2; stdlib `13359e53`; [language tour](https://github.com/gleam-lang/language-tour/tree/234cb0284d91c458fa26e1b66e4b0276597d722f) `234cb028`). Type variables are lowercase and undeclared (tour, generic functions, lines 17-19). There are no constraints: `list.sort(list, by compare: fn(a, a) -> Order)` takes the function (`src/gleam/list.gleam:1137`), and equality is structural on every type (`list.unique`, line 1109). Annotations are optional and checked when present. Gleam has no effect rows. Lowering: types erase to Erlang or JavaScript. Effra takes undeclared parameters and keeps explicit compare functions as library (`SortFunc`), but needs `comparable` for map keys.
- **Roc** (`7de4d5c8`). Type variables stay implicit; `where [a.to_str : a -> Str]` states a method constraint (`docs/langref/static-dispatch.md:427-456`), and a *where alias* names a group, `a.Showable : where [a.to_str : a -> Str]` (lines 487-520). Without an annotation the compiler infers the `where` clause (line 456) and the error union (`docs/langref/errors.md:57`). Effects are one bit, `=>` against `->` (`docs/langref/functions.md:29-63`), not a row. Error unions are open with `..others` (`docs/langref/tag-unions.md:50-58`). Lowering: "each call site knows exactly which `to_str` implementation it's using, and the compiled program calls it directly" (lines 451-452). Effra takes the named constraint (Roc's alias is Go's constraint interface) and refuses inferred constraints on exported code.
- **Koka** (`198fc3bb`, 3.3.0). Effect rows are inferred for every function (`doc/spec/why.kk.md:109`); a wildcard `_e` asks for inference in an annotation (`doc/spec/tour.kk.md:482-492`). A polymorphic function shares one effect variable between the callback and the result, `map : (xs : list<a>, f : (a) -> e b) -> e list<b>` (line 558), with "single letters (possibly followed by digits) for polymorphic types" (line 561). Lowering: effects compile by evidence passing. Effra takes the naming rule and inference inside a package, keeps two rows instead of one variable (3b refused), and requires exported signatures to spell rows.
- **Effect-TS** (effect-smol `3a1128c7`, 4.0.0-beta.98; PRIOR_ART pins 4.0.1 at `460272d3`). Users write `E` on service interfaces, `query(sql: string): Effect.Effect<Array<unknown>, DatabaseError>`, and let `Effect.fn` infer implementations (`LLMS.md:119-140`). Library combinators write every parameter: `timeout: <A, E, R>(self: Effect<A, E, R>, duration) => Effect<A, E | Cause.TimeoutError, R>` (`packages/effect/src/Effect.ts:4494-4502`). That is Effra's split: capability methods and exported functions explicit, bodies inferred, row variables in the library.
- **OCaml** (not re-read at a pin for this note). Type variables `'a` are written without declaration; a `.mli` file states the exported signatures, and the implementation's types are inferred. Effra's exported-explicit rule is the `.mli` rule without a second file.
- **ReScript** (docs `a62233bc`; compiler `5d47cb3a`). Types are inferred, and an annotation that disagrees is an error (`docs/manual/type.mdx:17`, `:64`). A `.resi` file is the module's signature (`docs/manual/module.mdx:494-499`). Lowering: types erase to JavaScript.
- **MoonBit** (moonbit-docs `3c8bf573`). Constraints sit in the type-parameter list, `fn[T : Show] double_string(t : T) -> String` (`next/tutorial/for-go-programmers/index.md:524`). Error polymorphism uses an anonymous marker, `f : (T) -> T raise?` with result `raise?` (`next/language/error-handling.md:119-145`), allowed only in some positions (`next/language/error_codes/E4168.md`). Lowering: monomorphisation. Effra takes the constraint position, and refuses the anonymous marker because it saves no entry and two callbacks would need two markers.
- **Elixir** (`7a405793`, 1.21.0-dev). Typespecs bound type variables with `when` guards, `@spec function(arg) :: [arg] when arg: atom` (`lib/elixir/pages/references/typespecs.md:253-263`); the new type system does not read them, and they "will be phased out" (`gradual-set-theoretic-types.md:282`). The new type system infers types without signatures (`gradual-set-theoretic-types.md:16`) and states the trade-off: inference across modules "may require large projects to explicitly add type signatures for stability and compilation efficiency" (lines 224-238). Effra's package boundary is that line drawn in advance.

**Lowering paths.** Rows erase in every candidate (P7): they are checked and the lowered Go has early returns and an environment struct (`docs/spec/rows.md`). Named constraints lower to Go's own constraints (`cmp.Ordered`, `comparable`) with no adapter; method constraints lower as `docs/research/where-dispatch.md` decided. Gleam and ReScript erase types entirely; MoonBit and Roc monomorphise; Go stencils by GC shape, which Effra inherits by emitting Go generics.

## Known gaps

- The checker does not exist yet, so the after/ corpus is not type-checked. It is the acceptance input for #196.
- The rewrite assumes each removed row equals the inferred row ([Method](#method)).
- The library reconstruction is this spike's; the library's design is [#206](https://github.com/cevr/effra/issues/206).
- Codec-derived rows and the `FindMany` to `Find` mapping of `batch.Resolve` are not expressible by any candidate.
