# The mistakes agents make when they write Ergo

Status: research, 2026-10-27. Ticket [#220](https://github.com/cevr/ergo/issues/220), stage 1, mining half (1a). The live multi-model eval is a later unit (1b). This file ranks the mistakes found in data that already exists, by cause, so that stage 2 can order its diagnostics.

Every number carries a label. **Measured** means a command counted it; the scratch files are named in [Reproduction](#reproduction). **Inferred** means a reading of measured data. **Guess** means neither. A cause ("Go habit") is always inferred from the syntax of the instance; no data here shows what a model learnt.

## Summary

- After the strict old-spelling rule, 19 instances of 11 language-mistake classes remain (measured count, hand classification; [ledger](#instance-ledger)). 14 come from chat sketches written after the specification landed, and 5 from review findings. No corpus program contributes: every Go-like form in the corpus was legal, or not yet ruled on, when it was written (measured against the sketches of that day).
- By score (frequency × harm), the leading mistakes are Go's duration arithmetic, a prefix `catch` meant to be partial, reading `needs` as the whole footprint, an error bound as a value and compared with `nil`, a pointer used as a value, and `try` over a selector or a comparison (scores 6, 6, 6, 4, 4 and 4).
- Today's checker reports 8 of the 11 classes, but none of its messages names the Ergo form to use. It does not detect the other 3 (measured with probes).
- Some S109 messages ("a multi-value := is outside the implemented subset") suggest that a Go form will be supported later, when the specification never admits it for an error row (measured, probes `err-tuple-bind`, `comma-ok-map`).
- The data cannot say whether the check loop prevents these mistakes. It is a hypothesis for stage 1b ([Chat sketches and files](#chat-sketches-and-files-a-hypothesis-not-a-result)).
- The sample is small, so the ranking is weak evidence (inferred). Stage 1b must re-rank it on 20 tasks or more.

## Method

### Sources and counts

| Source | What was read | Size | Result |
| --- | --- | --- | --- |
| 1. Review findings of this run | `/home/exedev/.cache/baton/wf10/*/round-*-findings.md` and `review*.md` | 165 files, 558 findings (measured) | 91 findings cite Ergo text and name a construct (measured, script). By hand: 8 agent mistakes, 20 spec gaps, 5 in the earlier language, 19 compiler bugs, 1 reviewer mutant, 38 prose or claim findings. A sample of 20 other findings held 1 more agent mistake. After the authoring-time check, 5 of these 10 findings count as language mistakes and 5 are historical (measured count) |
| 2. Sketch-eval corpora, runs 1 to 9 | every `*.go.sketch` outside `*work*` | 67 programs, 3516 non-blank lines (measured) | 4 Go-like lines are wrong under today's spec; all 4 were legal or unsettled when written, so 0 count (measured count). 57 of 67 programs use the old `uses` (measured) |
| 3. Agent transcripts | `~/.claude/projects/*effra*` and `~/.claude2/projects/*effra*` | 215 JSONL files, 549 MB (measured). The spec's "about 6 GB" is the size of both `projects` directories, not of the effra ones (measured: 2.7 GB and 3.3 GB) | after the spec landed: 14 language mistakes, 2 prototype-limit instances, 3 open-contract instances, and 2 correct forms first counted as mistakes (measured count) |
| 4. Today's checker | `ergo check` (2026 edition) on the 67 converted corpus programs, on 38 chat blocks, on 128 file blocks and on 48 probes | 281 runs (measured) | detection status for every class (measured) |

### Sampling

- **Source 1.** A script splits every finding paragraph (labels B, M, m and N) into three buckets: it cites Ergo text and names a construct; it cites only host code; or other. I classified the 91 findings in the first bucket by hand. I sampled 20 findings of the other two buckets (`shuf -n 20`). One was an Ergo concept mistake that the script had put in the host-code bucket (actor-101 r1 M5, "Empty uses is treated as purity"; measured). So the 467 findings outside the first bucket may hold about 20 more (guess). Findings about the reviewer's own probe programs, or about mutants, are not agent mistakes.
- **Source 2.** Each program was read whole with 21 patterns (`patterns.tsv`). Copies of one program across runs count once.
- **Source 3.** The frame is every Write, Edit and MultiEdit call that targets `.ef`, `.sketch` or a Markdown file with Ergo code (745 calls, measured), plus every assistant text message from 2026-10-26 on with a Go-fenced block (33 messages, measured). Only code is scanned: whole `.ef` and `.sketch` files, and the Go blocks of Markdown files. Paths under `reject/`, `probes/`, `bad/` and `invalid/` are left out, because those fixtures are wrong on purpose. My own transcript is left out. Each hit was read in context. Go comparison blocks ("Today (Go style)") and generated Go are not mistakes. The 11 chat messages with Go code written after the specification landed (2026-10-27T00:28Z, commit 0e888f03) were read by hand in full. The patterns gave 386 hits in file code and 134 in chat code, 306 of them `uses` (measured). The cap of 150 instances was not reached.
- **Source 4.** Conversion is mechanical only: `uses` becomes `provides` on a `wire` header and `needs` elsewhere. The parser reports its first S001 only, so a program with an old spelling hides its later mistakes.
- **Old Ergo language.** The earlier `.ef` language (EF codes) is a different language, so its mistakes are old spellings by this file's rule. The transcripts hold 6344 lines with an EF code in 174 files (measured). I did not classify them.

### The old-spelling rule, applied strictly

An instance counts as a language mistake only when today's rule already held, in the specification or in the sketch of that day, when the instance was written. Otherwise it is an old spelling or a historical design question, and it is not ranked. When the authoring-time rule is uncertain, the instance is excluded. Dates used:

- Duration literals typed `time.Duration`: [#175](https://github.com/cevr/ergo/issues/175), 2026-10-13.
- Go-shaped sketches: 2026-10-26 (sketches 2 to 10). Sketch 6 lets a trailing block `return` to the nearest `func` (`sketch-6.md:34`). Sketch 8 puts `CloseFailed` in a cleanup row (`sketch-8.md:42`). Sketch 9 brings back a checked `defer resp.Close()` (`sketch-9.md:66`).
- The v0 specification: 2026-10-27T00:28Z (commit 0e888f03).
- `uses` renamed to `needs`, and `provides` added: 2026-10-27T16:23Z (commit b2789102, [#217](https://github.com/cevr/ergo/issues/217)).

### Four kinds

- **Language mistake:** a form today's language forbids or spells differently, written after its rule held. Only these are ranked.
- **Prototype limit:** the prototype rejects a form that the specification designs or recommends.
- **Open contract:** the specification does not decide the question yet.
- **Documentation improvement:** the rule exists, but its text or message does not teach it.

### Detection levels

- **Parser:** S001. "Helpful" means the message names the Ergo form to use.
- **Prototype checker:** an S1xx code, or S109 for a form outside the implemented subset.
- **Undetected:** no diagnostic, or the form is designed and its check is not built (#196, #197).

## Ranked language mistakes

**Score** = frequency × harm. Frequency is the count of distinct instances in the [ledger](#instance-ledger) (measured). **Harm** is a judgment, not a measurement: 3 when the mistake goes undetected, 2 when a diagnostic fires but does not name the Ergo construct, 1 when the message names the right form. Rows sort by score, then frequency, then harm, then the short name. Stage 2 adds one more criterion, stated in [Recommendations](#recommendations).

| # | Short name | Construct | Cause (inferred) | Freq. | Harm | Score | Instances | Detected today (measured, probe) | The right Ergo form | Spec citation | Proposed diagnostic message |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | duration | Go duration arithmetic: `2 * time.Second`, `5 * time.Second` | Go habit | 3 | 2 | 6 | L1, L2, L3 | Checker, unhelpful: S108 "timeout.After needs a duration, not ?"; S104 "unknown type time.Duration" for the type | `timeout.After(2s)`; type `duration`; keep the unit (`time.Minute` → `1m`) | `docs/spec/grammar.md` Literals rule 2; `docs/spec/types.md` Basic types rule 4 | "Go's `time.Second` arithmetic is a duration literal in Ergo: write `2s`; the type is `duration`" |
| 2 | prefix-catch | `catch f() { case T: … }` written before the call, with the rest meant to escape | Effect or TypeScript habit (`try { } catch`, also Java and Swift; the agent's own diagnosis) | 3 | 2 | 6 | L4, L5, L6 | Parser, not helpful: S001 "expected an expression, found \"catch\"" | `try f() catch { case T: … }` when the rest escapes; `f() catch { … }` when every error is handled | `docs/spec/rows.md` Catch rules 4 and 5 | "`catch` follows the expression it handles: write `try f() catch { … }` to let the other errors escape, or `f() catch { … }` to handle them all" |
| 3 | needs-footprint | `needs` read as every effect: "an empty `needs` row means pure" | Other-language habit (Koka- and Haskell-style rows, where an empty row is pure) | 2 | 3 | 6 | L7, L8 | Undetected: a concept error in prose and library code, not a syntax error | The footprint, not the `needs` row, decides purity | `docs/spec/rows.md` Footprints rules 1 and 2 | No diagnostic: `ergo inspect` and `ergo explain` show the footprint next to `needs`, and the skill states the rule |
| 4 | error-value | An error bound as a value: `v, err := f()`, `err != nil`, `err == nil` | Go habit | 2 | 2 | 4 | L9, L10 | Checker, misleading: S109 "a multi-value := is outside the implemented subset"; `nil` gives S104 "unknown name nil" | `v := try f()` or `f() catch { case E: … }`; absence is `Option[T]` | `docs/spec/rows.md` Error rows; `docs/spec/types.md` Zero values rule 5 | "an error row is not a value: write `v := try f()` or `f() catch { case E: … }`" / "Ergo has no `nil`; absence is `Option[T]`" |
| 5 | pointer-value | A pointer as a value: `s := &Counter{}`, a `*T` result | Go habit | 2 | 2 | 4 | L11, L12 | Checker, misleading: S109 "the unary operator & is outside the implemented subset"; S109 "this type is outside the implemented subset" | `s := Counter{}`; return `T`; `*T` only on parameters and receivers | `docs/spec/types.md` Borrows rule 1 | "`*T` is a borrow: it appears only on parameters and receivers; store or return the value `T`" |
| 6 | try-placement | `try` over a selector or a comparison: `try R.Find(id).Name`, `return try f() == v` | Rust habit (`?` binds tight) | 2 | 2 | 4 | L13, L14 | Checker, unhelpful: S108 "try needs an expression that can fail", then S105 | `x := try f()`, then use `x.Name` or `x == v`; or `(try f()) == v` | `docs/spec/grammar.md` Expressions rule 1 | "`try` covers all of `… == v`; bind first: `x := try f()`" |
| 7 | callback-needs | A function-type parameter without `needs R`: `body func() (T, error[E])` beside an outer `needs R` | Go habit (Go function types carry no effects) | 1 | 3 | 3 | L15 | Undetected (function types are S109, so no row is checked) | `body func() (T, error[E]) needs R` | `docs/spec/types.md` Generics rule 3 | "this function type drops the requirement `R` that the callback needs; add `needs R`" |
| 8 | resource-move | A resource moved twice: `Postgres{pool, go keepWarm(pool)}` moves `pool` into the struct and into the task | Go habit (Go copies values freely) | 1 | 3 | 3 | L16 | Undetected (#197); the README review measured `checked=true` for a use after a move | Move a resource once; lend it to a call or task under the structured-lend rule | `docs/spec/resources.md` Moves R12, R14, R15 | "`pool` moved into `Postgres{…}` on line N; it cannot also move into this task" |
| 9 | comma-ok | A comma-ok call on an `Option` result whose element has no zero value: `if r, ok := msg.reply.Take(); ok` | Go habit | 1 | 2 | 2 | L17 | Checker, misleading: S109 "a multi-value := is outside the implemented subset" | `switch` on `Take()`'s `Option[Reply]`: `Some` and `None`. `Reply` has `Close`, so it is a resource and has no zero value | `docs/spec/types.md` Zero values rules 1 and 4 | "`Take` returns `Option[Reply]`, and `Reply` has no zero value, so there is no comma-ok form: switch on `Some` and `None`" |
| 10 | func-literal-return | A `func` literal whose body has no `return`: `func(id ID) (Price, error[NotFound]) { Catalog.Price(id) }` | Rust habit (expression bodies; also Kotlin) | 1 | 2 | 2 | L18 | Checker, misleading: S109 "a function literal is outside the implemented subset" | `return Catalog.Price(id)`, or a trailing block | `docs/spec/grammar.md` Expressions rule 3 | "a `func` literal returns with `return`; only a trailing block yields its last expression" |
| 11 | timeout-scheduler | `timeout.After` in a function that lists `Clock` and not `Scheduler` | Effect or TypeScript habit (Effect's `Clock` service runs timeouts) | 1 | 2 | 2 | L19 | Checker, unhelpful: S104 "unknown capability Clock" in the prototype | `needs Scheduler` | `docs/spec/rows.md` Capability rows rule 2 | "`timeout.After` needs `Scheduler`, which this needs clause does not list" |

### Teaching point: `try with(W) { try f() }` is right when the wire can fail

The two drafts that wrote `receipt := try with(Live) { try Checkout(…) }` build `Live` through `OpenPostgres`, which declares `error[DialFailed]` (ledger X1, X2). The inner `try` escapes `Checkout`'s failures to the enclosing function. The outer `try` handles the wire's startup errors, which join the `with` expression's row (`docs/spec/rows.md` Wire rule 6 and With rule 6). Both `try`s are required, so these are not mistakes. Only when every constructor in the wire is infallible is the outer `try` wrong: the probe `double-try-with` builds the infallible `Mem{}` and gets S108 "try needs an expression that can fail" (measured). The skill should state both cases: "the inner `try` escapes the body's errors; the outer `try` handles the wire's startup errors."

### Classes the issue named that the data does not show

- Bare `go f()`, `sync.WaitGroup` and `context.Context` parameters: 0 natural instances after the spec (measured). Every `go` hit in the frame is the designed `go` expression that yields a `Task`. The one `context.Context` hit is a comment about Go interop.
- `defer x.Close()` and `interface{}`: 0 language mistakes. The one `defer resp.Close()` in the corpus was legal under sketch 9 (ledger H3).
- Detection is measured anyway: `go work()` gives S109 "a go expression is outside the implemented subset", `var wg sync.WaitGroup` gives S109 "a local var declaration…", `ctx context.Context` gives S104 "unknown type context.Context", and `defer x.Close()` gives S109 "a defer statement is outside the implemented subset". None names the Ergo construct.

### Prototype limits (not ranked)

| Form | Instances | Today | Specification |
| --- | --- | --- | --- |
| `func main() error[…]` | P1, P2 (chat 15:52, 16:23) | S109 "main must return nothing in this prototype" (measured) | `docs/spec/README.md` Gap 4 recommends allowing it. Not a language mistake. |
| Designed forms outside the subset (`for`, `go`, `defer`, `*T`, function literals, several results) | 31 corpus programs report only S109 (measured) | S109 | Designed in `docs/spec/grammar.md` Statements rule 4 and Declarations rule 5 |

### Old spellings and historical rules (not ranked)

| Spelling or rule | Today's form | Count | Why excluded |
| --- | --- | --- | --- |
| `uses` | `needs`, and `provides` on a wire | 167 lines in 57 corpus programs (measured); 249 code lines in transcript writes, all but 3 before the rename, and those 3 in scratch probes (measured) | renamed 2026-10-27. The parser says only "expected end of declaration, found \"uses\"", which follows the owner's rule: "We don't need to give backwards compatible syntax parsing." |
| `type X enum { … }` | `type X interface{ A \| B }` | 19 lines in 12 programs (measured) | old spelling; S001 names the allowed forms |
| `layer` declarations | `wire` | 4 programs (measured) | old spelling |
| `for x in xs` | `for … range` | 7 programs (measured, first S001) | old spelling |
| Other early forms: `Ctx[…]` type arguments, `var` parameters, `name = value` wire literals, a top-level `interface` block | `needs` rows, `*T` parameters, `wire{ A = … }` | 12 programs (measured, first S001) | old spelling |
| `const` | package-level `var` | 1 program (measured) | removed with the spec; S001, helpful |
| `[T any]` | implicit type parameters | 2 programs (measured) | decided after the programs |
| `defer b.Stop()` on an actor or `tasks.Set` | release at scope end | 5 lines (measured) | R19 came with the spec (inferred) |
| `time.Duration` (H1), `time.Minute` (H2) | `duration`, `1m` | 2 lines (measured) | literals were typed `time.Duration` (#175); `time.Minute` may have been legal, so it is uncertain |
| `defer resp.Close()` (H3) | release at scope end | 1 line (measured) | sketch 9 allowed a checked `defer resp.Close()` |
| `Close() error[CloseFailed]` (H4) | `Close()` with no row (R2) | 1 line (measured) | sketch 8 had a cleanup row with `CloseFailed` |
| `return` in a trailing block (H5) | an explicit `func` literal | 1 finding | sketch 6 allowed it |
| `make([]Choice, n)` (H6) | `make([]Choice, 0, n)` | 1 finding | the zero-value rule came after the review |
| Empty requirement read as pure, in sketches 5 and 6 (H7, H8) | footprints | 2 findings | the sketches themselves classified it so |
| `Postgres{pool, go keepWarm(pool)}` in sketch 8 (H9, sketch-eval-8 review M1) | R14, R15 | 1 finding | the same review (M3) found the copy and move rules missing |
| Earlier `.ef` language | the 2026 edition | 5 review findings; 6344 EF lines in transcripts (measured) | not classified |

Today's checker on the 67 converted programs: 1 checks clean, 35 stop at an old-spelling S001, and 31 report only S109 for designed forms that are not built (measured). No converted program reached a semantic code from S101 to S110 (measured).

### Instance ledger

Every counted instance, with its kind. "chat HH:MM" is an assistant message of 2026-10-27 (UTC) in the orchestrator's transcript; its code is extracted in `chat-1027-code.txt` (scratch).

| ID | Kind | Class | Source | Date | Evidence |
| --- | --- | --- | --- | --- | --- |
| L1 | language mistake | duration | S3 chat 16:23 | after spec | `timeout.After(2 * time.Second)` |
| L2 | language mistake | duration | S3 chat 16:28 | after spec | `timeout.After(2 * time.Second)` |
| L3 | language mistake | duration | S3 chat 16:28 | after spec | `SlowGateway{Delay: 5 * time.Second}` |
| L4 | language mistake | prefix-catch | S3 chat 15:52 | after spec | `catch Checkout("ord-7", "card-9") { case Declined: … }`; NotFound left to escape |
| L5 | language mistake | prefix-catch | S3 chat 16:23 and `readme-sketch.md` (one text) | after spec | `catch Checkout(id, savedCard) { … }` "NotFound … stays in Refund's row" |
| L6 | language mistake | prefix-catch | S3 chat 16:28 | after spec | `catch Charge(card, 100) { case timeout.Expired: }`; Declined left to escape |
| L7 | language mistake | needs-footprint | S1 readme r1 M2 | after spec | README claims a needs row lists every effect |
| L8 | language mistake | needs-footprint | S1 actor-101 r1 M5 | after spec | "Empty uses is treated as purity" |
| L9 | language mistake | error-value | S3 chat 16:23 | after spec | `out, err := c.Decode(c.Encode(v))`; `return err == nil && out == v` |
| L10 | language mistake | error-value | S3 chat 16:28 | after spec | `r, err := p.Charge(l, amt)`; `return err != nil \|\| …` |
| L11 | language mistake | pointer-value | S3 issue draft and chat 15:18/15:19 (one text) | after spec | `s := &Counter{}` |
| L12 | language mistake | pointer-value | S3 chat 15:52 | after spec | `func OpenPostgres(url string) (*PgOrders, error[DialFailed])` |
| L13 | language mistake | try-placement | S3 chat 15:52 | after spec | `testing.Equal(try Orders.Get("ord-7").Paid, false)` |
| L14 | language mistake | try-placement | S3 chat 16:32 | after spec | `return try c.Decode(c.Encode(v)) == v` |
| L15 | language mistake | callback-needs | S1 spec-v0 r2 N1 | after spec | `body func() (T, error[E])` beside an outer `needs R` |
| L16 | language mistake | resource-move | S1 spec-v0 r1 M2 | after spec | `Postgres{pool, go keepWarm(pool)}` in `resources.md` |
| L17 | language mistake | comma-ok | S1 actor-101 r2 m2 | after spec | `if r, ok := msg.reply.Take(); ok` |
| L18 | language mistake | func-literal-return | S3 chat 16:23 | after spec | `tasks.All(ids, func(id ID) (Price, error[NotFound]) { Catalog.Price(id) })` |
| L19 | language mistake | timeout-scheduler | S3 chat 16:28 | after spec | `needs Accounts, Payments, Clock` around `timeout.After` |
| X1, X2 | correct form | — | S3 chat 15:52, 16:23 | after spec | `try with(Live) { try Checkout(…) }` with a fallible `OpenPostgres` |
| P1, P2 | prototype limit | fallible main | S3 chat 15:52, 16:23 | after spec | `func main() error[NotFound \| Declined \| DialFailed]` |
| O1 | open contract | `any` | S3 write, std prelude | after spec | `Println(values ...any)` |
| O2 | open contract | consuming receiver | S3 chat 16:14 | after spec | `func (f *Frame) Close()` |
| O3 | open contract | library lock | S3 write, actor library sketch | after spec | `admission sync.RWMutex` |
| H1–H9 | historical | see the table above | S1, S2 | 2026-10-26 | see the table above |

## Chat sketches and files: a hypothesis, not a result

Descriptive observation, with one denominator method for both sides: unique code lines, one per target file and trimmed line, comments and blank lines left out (measured with `scan-writes.ts`).

- Chat sketches after the spec: 14 language mistakes in 253 unique code lines, read by hand, by patterns and by the checker.
- Files written after the spec: 0 language mistakes found in 1667 unique code lines, read only by patterns and by the checker.

The two sides were not read the same way, so the comparison shows no check-loop effect. The file scan cannot detect 7 of the 11 classes: needs-footprint, try-placement, callback-needs, resource-move, comma-ok (the checker stops at S109), func-literal-return and timeout-scheduler. Prefix-catch shows only as a first S001, and the patterns see only the `&x` part of pointer-value. The file count is therefore a lower bound (inferred).

**Hypothesis for stage 1b:** writing with `ergo check` in the loop lowers the rate of language mistakes. To settle it, stage 1b must:

- freeze the specification and the task set;
- run the same tasks with fresh, matched model runs, once without `ergo check` and once with it in the loop, and control which arm has checker access;
- save the first complete artifact of each run before any checker feedback, and the final one;
- classify every arm with the same rubric (the 11 classes above, plus the four kinds) and the same manual sampling;
- record a unique task and generation ID, the first-try pass rate, mistakes per 100 unique code lines, and the repair outcome;
- test the skill as a separate factor, not mixed with checker access.

## Spec gaps and open contracts

Each item names the line it would amend. Items the specification already lists say so.

### Open contracts

1. **`any` and host variadics.** The std prelude declares `Println(values ...any)`. `docs/spec/types.md` Generics rule 1 forbids only `[T any]`. No rule says whether `any` is a type, or how a host variadic is typed. Amend `docs/spec/types.md` Basic types rule 5 to decide it.
2. **The receiver of a consuming method.** `func (f *Frame) Close()` reads as "mutates" under `docs/spec/types.md` Methods rule 4, but R19 makes `Close` consume. This is README Gap 35 (A1), already recorded.
3. **A lock for library authors.** The actor library sketch writes `sync.RWMutex` for an admission fence. `docs/spec/stdlib.md` Sync and STM rule 1 lists `Ref`, `Latch`, `Semaphore` and `Deferred` and does not ban a mutex. This is a library design question for [#208](https://github.com/cevr/ergo/issues/208): amend rule 1 to add a reader-writer fence, or to say that `Semaphore` covers it.
4. **The entry point.** `docs/spec/rows.md` Capability rows rule 4 points to README Gaps 4 and 5, which are open. Already recorded.

### Documentation improvements

The rules below exist; their text or message does not teach them.

1. **Prefix `catch` in the S001 list.** `docs/spec/diagnostics.md` S001, the bullet "A Go form the grammar does not admit", lists Go forms only. Add: "`catch` before an expression (`catch f() { … }`): the message names `try f() catch { … }` and `f() catch { … }`."
2. **S109 for forms the language never admits.** `docs/spec/diagnostics.md` S109 covers "the implemented subset". A multi-value `:=` that binds an error row, and `return v, nil`, are never part of the language, yet they get S109. Amend S109: "A form that the specification never admits is S001 or S108 with a message that names the Ergo construct, not S109."
3. **Parenthesised `try`.** The grammar admits `(try f()) == v` through `PrimaryExpr = "(" Expression ")"`. Add that example to `docs/spec/grammar.md` Expressions rule 1, next to the precedence rule.
4. **The outer `try` on a fallible wire.** `docs/spec/rows.md` With rule 6 states that startup errors join the `with` row. Add the example `v := try with(Live) { try f() }` and say what each `try` handles.

Gaps found by the reviews of this run and since closed by the specification (inferred from the current text): a trailing block's `return` (`docs/spec/grammar.md` Expressions rule 3), zero-filling builtins (`docs/spec/types.md` Zero values rule 4), STM bodies (`docs/spec/stdlib.md` Sync and STM rule 3), task containers (`docs/spec/runtime.md` Tasks rule 5), a fallible `Close` (`docs/spec/resources.md` R2), and copies of a resource (`docs/spec/resources.md` R15).

## Recommendations

### Stage 2: the top 5 diagnostics

Selection rule: take the ranked rows in order, and skip a row that no compiler diagnostic can catch. That is the one extra criterion. It skips rank 3 (needs-footprint), which is a concept error and goes to the skill and to `ergo inspect`.

1. **Duration arithmetic** (rank 1, score 6). Recognise `N * time.Second` (and `Nanosecond`, `Microsecond`, `Millisecond`, `Minute`, `Hour`) and the type `time.Duration`, and suggest `Ns` and `duration`, keeping the unit. The fix can be `safe` for a constant integer `N` once the rewritten file rechecks with no new diagnostic. Parser- or name-level, so it can land before #196.
2. **Prefix `catch`** (rank 2, score 6). At `catch` in expression-start position, report S001 with "`catch` follows the expression it handles". The fix is `maybe`, and the diagnostic carries both candidate rewrites: `try f() catch { … }`, which lets the other errors escape, and `f() catch { … }`, which must handle them all. All three sampled instances meant partial handling. Moving the call alone turns S001 into S103 (measured witness: `round-1-receipts/prefix-partial`). When the total form is chosen, S103's message should also say "write `try e catch { … }` to let `X` escape". Parser-level, so it can land now.
3. **An error bound as a value, and `nil`** (rank 4, score 4; also the issue's first witness). Give a multi-value `:=` whose right side has an error row, `err != nil`, `return v, nil` and the name `nil` a message that names `try`, `catch` or `Option`, instead of S109 or a bare S104. Parser-level for `nil`; checker-level for the row.
4. **A pointer as a value** (rank 5, score 4). For `&T{…}` outside an argument and a `*T` result, name the borrow rule and suggest the value `T`. The fix is `maybe`, because it changes a type. Checker-level (#196).
5. **`try` over a selector or a comparison** (rank 6, score 4). When `try` covers a non-fallible expression whose operand holds a fallible call, suggest binding first, or the parenthesised form. The fix is `maybe`. Checker-level (#196).

Rank 8 (resource-move) has a higher harm, but it needs the linearity pass, so it belongs to #197's first test list.

### Stage 3: fix applicability

- A fix whose result has a different error than the one it removes is never `safe`. Moving a prefix `catch` behind its call is the measured example: it removes S001 and adds S103.
- Claim `safe` only after the complete rewritten candidate rechecks with no added diagnostic and keeps every exported row.

### Stage 4: the skill

- Make the check loop the first rule: write, run `ergo check`, read the code, apply the `safe` fixes, check again. Every class found in the chat sketches gets a diagnostic today, though no message names the fix (measured with probes, rows 1, 2, 4, 5, 6, 10 and 11). Whether the loop lowers the mistake rate is the stage-1b hypothesis above; this file does not show it.
- Put these rows in the Go → Ergo table, in this order: `2 * time.Second` → `2s`; `try { } catch` → `try f() catch { case T: … }` (partial) or `f() catch { … }` (total); `v, err := f(); if err != nil` → `v := try f()`; `nil` → `Option`; `&T{}` and `*T` results → values; `try x.F().G` → bind first.
- State the comma-ok rule as the specification does: a comma-ok form needs a defaultable element type (`docs/spec/types.md` Zero values rule 4), so a method that returns `Option[T]`, such as `Take`, is matched with `Some` and `None`.
- State the scope rules that the sketches broke: the inner `try` in a `with` escapes the body's errors and the outer `try` handles the wire's startup errors; a `func` literal needs `return` where a trailing block does not.
- State that purity is the footprint, not the `needs` row ([rows.md](../spec/rows.md#footprints)), and that `timeout.After` needs `Scheduler`, not `Clock`.
- Gate every code block like the README (`TestReadmeSmoke`). The README unit's implementor wrote nine gated blocks in 47 file calls. Their one pattern hit is a designed `*T` parameter, so the count of Go-habit mistakes is 0 (measured), and its round-3 review found that all nine "parse and format".

### Stage 1b: the eval

- Use the 11 classes above as the scoring rubric, and count old spellings, prototype limits and open contracts separately.
- Take tasks that cover durations, laws, `with` plus `try` (with and without fallible constructors), resources, and actors. Those are where the chat sketches failed (inferred).
- Run the controlled checker-loop comparison described above.

## Reproduction

Scratch files are under `/home/exedev/.cache/baton/wf10/mistakes/`. They are not part of the repository.

- `scan-findings.ts` → `findings-buckets.tsv` (source 1 buckets).
- `patterns.tsv`, `corpus-files.txt`, `corpus-patterns.txt` (source 2 patterns).
- `run-corpus.sh` → `corpus-diags.tsv` (source 4 on the corpus).
- `extract-writes.sh` → `writes.jsonl`; `extract-chat.sh` → `chat.jsonl`; `scan-writes.ts WRITES PATTERNS [SINCE [UNTIL]]` → hits on stdout, call, line and unique-line counts on stderr; outputs `writes-hits.tsv`, `chat-hits.tsv`, `writes-post.tsv`, `chat-post.tsv`; `chat-1027-code.txt` holds the chat code after the spec.
- `split-blocks.ts`, `check-dirs.sh` → `chat-diags.tsv`, `write-diags.tsv` (source 4 on transcript code).
- `probes.ts` → `probes.tsv` (detection per construct).
- `t-selfcorrect.txt`, `t-notergo.txt` (self-corrections in transcripts).

The checker is `go build -o bin/ergo ./cmd/ergo` at 0ce502ce. Its JSON comes from `ergo check FILE`. `--json` is accepted only by `ergo diagnostics`.
