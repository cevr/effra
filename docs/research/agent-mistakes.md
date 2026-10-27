# The mistakes agents make when they write Ergo

Status: research, 2026-10-27. Ticket [#220](https://github.com/cevr/ergo/issues/220), stage 1, mining half (1a). The live multi-model eval is a later unit (1b). This file ranks the mistakes found in data that already exists, by cause, so that stage 2 can order its diagnostics.

Every number carries a label. **Measured** means a command counted it; the scratch files are named in [Reproduction](#reproduction). **Inferred** means a reading of measured data. **Guess** means neither.

## Summary

- The data holds few natural mistakes, and almost all of them come from one kind of text: sketches written from memory without the checker. In the 395 lines of Go-fenced chat sketches written after the specification landed, I found 21 mistakes (5.3 per 100 lines, measured count; hand reading). In the 2236 lines of Ergo that agents wrote to files in the same period, I found 1 (0.04 per 100 lines, measured count). Files were gated by tests and reviews and chat was not, so the gap is mostly the check loop (inferred). Part of the gap is my method: I read the chat sketches by hand, and I scanned the files only with patterns and the checker (inferred).
- The frequent mistakes are Go's duration arithmetic, `try` in the wrong place, reading `needs` as the whole footprint, a `catch` before the expression, and a resource used after it moved (measured counts 5, 4, 4, 3 and 2).
- Today's checker detects most code mistakes, but its messages do not name the Ergo construct. Of the 18 ranked classes, 3 get a message that names the right form, 10 get a message that does not, and 5 go undetected (measured with probes).
- Some S109 messages ("a multi-value := is outside the implemented subset") suggest that a Go form will be supported later, when the specification never admits it (measured, probes `err-tuple-bind`, `comma-ok-map`).
- The sample is small, so the ranking is weak evidence (inferred). Stage 1b must re-rank it on 20 tasks or more.

## Method

### Sources and counts

| Source | What was read | Size | Ergo-code instances found |
| --- | --- | --- | --- |
| 1. Review findings of this run | `/home/exedev/.cache/baton/wf10/*/round-*-findings.md` and `review*.md` | 165 files, 558 findings (measured) | 91 findings cite Ergo text and name a construct (measured, script). By hand: 8 agent mistakes, 20 spec gaps, 5 in the earlier language, 19 compiler bugs, 1 reviewer mutant, 38 prose or claim findings (measured count, hand classification). A sample of 20 other findings held 1 more agent mistake (measured) |
| 2. Sketch-eval corpora, runs 1 to 9 | every `*.go.sketch` outside `*work*` | 67 programs, 3516 non-blank lines (measured) | 4 Go-habit lines that are mistakes under today's spec (measured count); old spellings in 57 of 67 programs (measured: `uses` alone) |
| 3. Agent transcripts | `~/.claude/projects/*effra*` and `~/.claude2/projects/*effra*` | 215 JSONL files, 549 MB (measured). The spec's "about 6 GB" is the size of both `projects` directories, not of the effra ones (measured: 2.7 GB and 3.3 GB) | 22 mistakes and 2 spec gaps after the spec landed (measured count); 59 self-correction extracts (measured) |
| 4. Today's checker | `ergo check` (2026 edition) on the 67 converted corpus programs, on 38 chat blocks, on 128 file blocks and on 48 probes | 281 runs (measured) | detection status for every ranked class (measured) |

### Sampling

- **Source 1.** A script splits every finding paragraph (labels B, M, m and N) into three buckets: it cites Ergo text and names a construct; it cites only host code; or other. I classified the 91 findings in the first bucket by hand. I sampled 20 findings of the other two buckets (`shuf -n 20`). One was an Ergo concept mistake that the script had put in the host-code bucket (actor-101 r1 M5, "Empty uses is treated as purity"; measured). So the 467 findings outside the first bucket may hold about 20 more (guess). Findings about the reviewer's own probe programs, or about mutants, are not agent mistakes.
- **Source 2.** Each program was read whole with 21 patterns (`patterns.tsv`). A hit counts as a mistake only when today's spec forbids the form and the spec rule existed when the program was written. Otherwise it is an old spelling. Copies of one program across runs count once.
- **Source 3.** The frame is every Write, Edit and MultiEdit call that targets `.ef`, `.sketch` or a Markdown file with Ergo code (745 calls, measured), plus every assistant text message from 2026-10-26 on with a Go-fenced block (33 messages, measured). Only code is scanned: whole `.ef` and `.sketch` files, and the Go blocks of Markdown files. Paths under `reject/`, `probes/`, `bad/` and `invalid/` are left out, because those fixtures are wrong on purpose. My own transcript is left out. Each hit was read in context. Go comparison blocks ("Today (Go style)") and generated Go are not mistakes. The 12 chat messages written after the specification landed (2026-10-27T00:28Z, commit 0e888f03) were read by hand in full, 395 code lines. The cap of 150 instances was not reached. The patterns gave 386 hits in file code and 134 in chat code, 306 of them `uses` (measured). After classification, 24 instances remain from after the spec (measured count).
- **Source 4.** Conversion is mechanical only: `uses` becomes `provides` on a `wire` header and `needs` elsewhere. Nothing else is changed. The parser reports its first S001 only, so a program with an old spelling hides its later mistakes.
- **Old Ergo language.** The earlier `.ef` language (EF codes) is a different language, so its mistakes are old spellings by this file's rule. The transcripts hold 6344 lines with an EF code in 174 files (measured). I did not classify them.

### Timeline used for "old spelling"

- Go-shaped sketches: 2026-10-26 (sketches 2 to 10 in the run directory).
- The v0 specification: 2026-10-27T00:28Z (commit 0e888f03).
- `uses` renamed to `needs`, and `provides` added: 2026-10-27T16:23Z (commit b2789102, [#217](https://github.com/cevr/ergo/issues/217)).

### Detection levels

- **Parser:** S001. "Helpful" means the message names the Ergo form to use.
- **Prototype checker:** an S1xx code, or S109 for a form outside the implemented subset.
- **Undetected:** no diagnostic, or the form is designed and its check is not built (#196, #197).

## Ranked table

Score = frequency × harm. Harm is 3 when the mistake goes undetected, 2 when a diagnostic fires but does not name the Ergo construct, and 1 when the message names the right form. Frequencies are measured counts of distinct instances across sources. Old spellings are not ranked.

| # | Construct | Cause | Frequency | Sources | Detected today (measured, probe) | The right Ergo form | Spec citation | Proposed diagnostic message |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | Go duration arithmetic: `2 * time.Second`, `time.Duration`, `time.Minute` | Go habit | 5 | S3: 3 (chat sketches, 2026-10-27); S2: 2 (`sketch-eval-8/loops/poller`, `wire-effects`) | Checker, unhelpful: S108 "timeout.After needs a duration, not ?"; S104 "unknown type time.Duration" | `timeout.After(2s)`; type `duration` | `docs/spec/grammar.md` Literals rule 2; `docs/spec/types.md` Basic types rule 4 | "Go's `time.Second` arithmetic is a duration literal in Ergo: write `2s`; the type is `duration`" |
| 2 | `try` placement: `try with(W) { try f() }`, `try R.Find(id).Name`, `return try f() == v` | Effect or TypeScript habit (`with` read as an effect value to run); Rust habit (`?` binds tight) | 4 | S3: 4 (chat sketches 15:52 twice, 16:23, 16:32) | Checker, unhelpful: S108 "try needs an expression that can fail", then S105 | `with(W) { try f() }`, whose inner `try` escapes to the function; `x := try f()`, then use `x` | `docs/spec/rows.md` Error rows rule 4; `docs/spec/grammar.md` Expressions rule 1 | "this `with` cannot fail: the `try` inside it already escapes to the enclosing function" / "`try` covers all of `… == v`; bind first: `x := try f()`" |
| 3 | `needs` read as every effect: "an empty `needs` row means pure" | Other-language habit (Koka- and Haskell-style rows, where an empty row is pure) | 4 | S1: readme r1 M2, sketch-eval-6 review M1, sketch-eval-9 review M3, actor-101 r1 M5 | Undetected: a concept error in prose, tooling claims and library code, not a syntax error | The footprint, not the `needs` row, decides purity | `docs/spec/rows.md` Footprints rules 1 and 2 | No diagnostic: `ergo inspect` and `ergo explain` show the footprint next to `needs`, and the skill states the rule |
| 4 | Prefix `catch X { … }` | Effect or TypeScript habit (`try { } catch`, also Java and Swift; the agent's own diagnosis) | 3 | S3: 3 (chat sketches 15:52, 16:23, 16:28); the issue's README witness | Parser, not helpful: S001 "expected an expression, found \"catch\"" | `f() catch { case T: v }`; `try f() catch { … }` | `docs/spec/rows.md` Catch | "`catch` follows the expression it handles: write `f(…) catch { case T: … }`" |
| 5 | A resource used after a move: `Postgres{pool, go keepWarm(pool)}` moves `pool` into the struct and into the task | Go habit (Go copies values freely) | 2 | S1: spec-v0 r1 M2, sketch-eval-8 review M1 | Undetected (#197); the README review measured `checked=true` for a use after a move | Move a resource once; lend it to a call or task under the structured-lend rule | `docs/spec/resources.md` Moves R12, R14, R15 | "`pool` moved into `Postgres{…}` on line N; it cannot also move into this task" |
| 6 | An error bound as a value: `v, err := f()`, `err != nil`, `err == nil`, `return v, nil` | Go habit | 2 | S3: 2 (laws in chat sketches 16:23, 16:28); the issue's README witness | Checker, misleading: S109 "a multi-value := is outside the implemented subset"; `nil` gives S104 "unknown name nil" | `v := try f()` or `f() catch { case E: … }`; absence is `Option[T]` | `docs/spec/rows.md` Error rows; `docs/spec/types.md` Zero values rule 5 | "an error row is not a value: write `v := try f()` or `f() catch { case E: … }`" / "Ergo has no `nil`; absence is `Option[T]`" |
| 7 | A pointer as a value: `s := &Counter{}`, a `*T` result | Go habit | 2 | S3: issue draft and chat 15:18/15:19 (one text), chat 15:52 | Checker, misleading: S109 "the unary operator & is outside the implemented subset" | `s := Counter{}`; return `T`; `*T` only on parameters and receivers | `docs/spec/types.md` Borrows rule 1 | "`*T` is a borrow: it appears only on parameters and receivers; store or return the value `T`" |
| 8 | A `catch` without `try` that is meant to be partial ("NotFound is not handled, so it stays in Refund's row") | Other-language habit (Swift and Java propagate an unmatched error) | 3 | S3: 3 (chat sketches 15:52, 16:23, 16:28) | Checker, close to helpful: S103 "catch without try must handle every error; missing NotFound" | `try e catch { case T: … }` lets the rest escape | `docs/spec/rows.md` Catch rules 4 and 5 | "… missing NotFound; to let it escape, write `try e catch { … }`" |
| 9 | A function-type parameter without `needs R`: `body func() (T, error[E])` | Go habit (Go function types carry no effects) | 1 | S1: spec-v0 r2 N1 | Undetected (function types are S109) | `body func() (T, error[E]) needs R` | `docs/spec/types.md` Generics rule 3 | "this function type drops the requirement `R` that the callback needs; add `needs R`" |
| 10 | `Close() error[CloseFailed]` | Go habit (`io.Closer` returns `error`) | 1 | S2: `sketch-eval-8/loops/wire-effects` line 33 (inferred: rule R2 may postdate the program) | Undetected (prototype has no resources) | `Close()` with no row; a fallible cleanup handles its failure inside `Close` | `docs/spec/resources.md` What is a resource, R2 | "`Close` cannot declare an error row: a resource's release cannot fail" |
| 11 | `sync.RWMutex` in library code | Go habit; also a spec gap | 1 | S3: the actor library sketch (2026-10-27 12:00) | Undetected (a research sketch) | `sync.Ref`, `Latch`, `Semaphore`, or STM | `docs/spec/stdlib.md` Sync and STM rule 1 | "Ergo's `sync` has no mutex: use `sync.Ref`, `sync.Semaphore` or `sync.Atomically`" |
| 12 | `func main() error[…]` | Rust habit (`fn main() -> Result`); spec gap (entry is open) | 2 | S3: chat sketches 15:52, 16:23 | Checker, helpful: S109 "main must return nothing in this prototype" | `main` returns nothing; handle failures inside | `docs/spec/rows.md` Capability rows rule 4 | Keep today's message, and add the README Gap 4 link |
| 13 | A comma-ok result: `if r, ok := x.Take(); ok` | Go habit | 1 | S1: actor-101 r2 m2 | Checker, misleading: S109 "a multi-value := is outside the implemented subset" | `switch` over `Option`'s `Some` and `None`; `m.Lookup(k)` | `docs/spec/types.md` Zero values rule 4; `docs/spec/grammar.md` Declarations rule 5 | "Ergo has no comma-ok form: `Take` returns `Option[T]`; switch on `Some` and `None`" |
| 14 | `make([]T, n)` with a type that has no zero value | Go habit (a spec gap at the time, now closed) | 1 | S1: sketch-eval-8 review M5 | Checker, misleading: S109 "this expression is outside the implemented subset" | `make([]T, 0, n)` with `append` | `docs/spec/types.md` Zero values rule 4 | "`Choice` has no zero value, so `make([]Choice, n)` cannot fill it; use `make([]Choice, 0, n)` and `append`" |
| 15 | A `func` literal whose body has no `return` | Rust habit (expression bodies; also Kotlin) | 1 | S3: chat sketch 16:23 (`tasks.All(ids, func(id ID) (Price, error[NotFound]) { Catalog.Price(id) })`) | Checker, misleading: S109 "a function literal is outside the implemented subset" | `return Catalog.Price(id)`, or a trailing block | `docs/spec/grammar.md` Expressions rule 3 | "a `func` literal returns with `return`; only a trailing block yields its last expression" |
| 16 | `timeout.After` charged to `Clock` instead of `Scheduler` | Effect or TypeScript habit (Effect's `Clock` service runs timeouts) | 1 | S3: chat sketch 16:28 | Checker, unhelpful: S104 "unknown capability Clock" in the prototype | `needs Scheduler` | `docs/spec/rows.md` Capability rows rule 2 | "`timeout.After` needs `Scheduler`, which this needs clause does not list" |
| 17 | `defer r.Close()` on a resource the scope owns | Go habit | 1 | S2: `sketch-eval-9/loops/fetchall` line 28 (inferred: automatic release existed in sketch 8); the issue lists it | Checker, misleading: S109 "a defer statement is outside the implemented subset" | Nothing: the scope releases `r`; call `r.Close()` to end it early | `docs/spec/resources.md` `defer` | "`r` is released at the end of this scope; remove the `defer`, or call `r.Close()` where it should end" |
| 18 | `return` inside a trailing block: `n := parallel { return 7 }` | Other-language habit (Kotlin, Ruby non-local return); a spec gap at the time, now closed | 1 | S1: sketch-eval-6 review M2 | Checker, helpful only for `with` and `timeout.After` (S109) | An explicit `func` literal | `docs/spec/grammar.md` Expressions rule 3 | "a trailing block cannot `return`; write `func() T { return … }`" |

### Classes the issue named that the data does not show

- Bare `go f()`, `sync.WaitGroup` and `context.Context` parameters: 0 natural instances after the spec (measured). Every `go` hit in the frame is the designed `go` expression that yields a `Task`. The one `context.Context` hit is a comment about Go interop.
- `interface{}` and `any`: 0 mistakes. The one `any` after the spec is in the std prelude (`Println(values ...any)`), and the spec says nothing about it (see Spec gaps).
- Detection is measured anyway: `go work()` gives S109 "a go expression is outside the implemented subset", `var wg sync.WaitGroup` gives S109 "a local var declaration…", and `ctx context.Context` gives S104 "unknown type context.Context". None names the Ergo construct.

### Old spellings (not ranked)

| Spelling | Today's form | Count | Notes |
| --- | --- | --- | --- |
| `uses` | `needs`, and `provides` on a wire | 167 lines in 57 corpus programs (measured); 249 code lines in transcript writes, all but 3 before the rename, and those 3 in scratch probes (measured) | 0 natural uses after the rename, outside scratch probes that are wrong on purpose (measured). The parser says only "expected end of declaration, found \"uses\"", which follows the owner's rule: "We don't need to give backwards compatible syntax parsing." |
| `type X enum { … }` | `type X interface{ A \| B }` | 19 lines in 12 programs (measured) | S001 names the allowed forms |
| `layer` declarations | `wire` | 4 programs (measured) | S001 |
| `for x in xs` | `for … range` | 7 programs (measured, first S001) | S001 |
| Other early forms: `Ctx[…]` type arguments, `var` parameters, `name = value` wire literals, a top-level `interface` block | `needs` rows, `*T` parameters, `wire{ A = … }` | 12 programs (measured, first S001) | S001 |
| `const` | package-level `var` | 1 program (measured) | S001, helpful |
| `[T any]` | implicit type parameters | 2 programs (measured) | designed, `docs/spec/types.md` Generics rule 1 |
| `defer b.Stop()` on an actor or `tasks.Set` | release at scope end | 5 lines (measured) | R19 came with the spec (inferred) |
| Earlier `.ef` language | the 2026 edition | 5 review findings; 6344 EF lines in transcripts (measured) | not classified |

Today's checker on the 67 converted programs: 1 checks clean, 35 stop at an old-spelling S001, and 31 report only S109 for designed forms that are not built (measured). No converted program reached a semantic code from S101 to S110 (measured).

## Spec gaps

Each gap names the line it would amend. Gaps the specification already lists say so.

1. **`any` and `interface{}`.** The std prelude declares `Println(values ...any)`. `docs/spec/types.md` Generics rule 1 forbids only `[T any]`. No rule says whether `any` is a type. Amend `docs/spec/types.md` Basic types rule 5: "`any` and `interface{}` are not types; a variadic host call takes …", or admit `any` there.
2. **Prefix `catch` in the S001 list.** `docs/spec/diagnostics.md` S001, the bullet "A Go form the grammar does not admit", lists Go forms only. A prefix `catch` is not a Go form, but it is the most frequent parse mistake in this data. Add: "`catch` before an expression (`catch f() { … }`): the message names `f() catch { … }`."
3. **S109 for forms the language never admits.** `docs/spec/diagnostics.md` S109 covers "the implemented subset". A multi-value `:=` that binds an error, and `return v, nil`, are never part of the language, yet they get S109. Amend S109: "A form that the specification never admits is S001 or S108 with a message that names the Ergo construct, not S109."
4. **Parenthesised `try`.** The grammar admits `(try f()) == v` through `PrimaryExpr = "(" Expression ")"`, but no rule or example says so. Amend `docs/spec/grammar.md` Expressions rule 1 with the example `(try f()) == v`, next to the precedence rule.
5. **A lock for library authors.** The actor library sketch needs an admission fence and writes `sync.RWMutex`. `docs/spec/stdlib.md` Sync and STM rule 1 lists `Ref`, `Latch`, `Semaphore` and `Deferred`, and its prior-art line implies that a mutex is left out on purpose. Amend rule 1: either add a reader-writer fence, or state that `Semaphore` covers it.
6. **Comma-ok forms for defaultable types.** `docs/spec/types.md` Zero values rule 4 forbids comma-ok forms for types without a zero value, and `docs/spec/grammar.md` Declarations rule 5 admits only `T`, `error[E]` and `(T, error[E])` results. Neither says whether `v, ok := m[k]` exists for a defaultable `V`. Amend rule 4: "There is no comma-ok form; `Lookup` returns `Option[V]`," or state where it is allowed.
7. **The receiver of a consuming method.** `func (f *Frame) Close()` reads as "mutates" under `docs/spec/types.md` Methods rule 4, but R19 makes `Close` consume. This is README Gap 35 (A1), already recorded.
8. **The entry point.** Agents write `func main() error[…]` (2 instances). `docs/spec/rows.md` Capability rows rule 4 points to README Gaps 4 and 5, which are open. Already recorded.

Gaps found by the reviews of this run and since closed by the specification, so stage 2 can check them (inferred from the current text): a trailing block's `return` (`docs/spec/grammar.md` Expressions rule 3), zero-filling builtins (`docs/spec/types.md` Zero values rule 4), STM bodies (`docs/spec/stdlib.md` Sync and STM rule 3), task containers (`docs/spec/runtime.md` Tasks rule 5), a fallible `Close` (`docs/spec/resources.md` R2), and copies of a resource (`docs/spec/resources.md` R15).

## Recommendations

### Stage 2: the top 5 diagnostics, by frequency × harm

1. **Duration arithmetic** (rank 1). Recognise `N * time.Second` (and `Millisecond`, `Minute`, `Hour`) and the type `time.Duration`, and suggest `Ns` and `duration`. The fix is `safe` for a constant `N`. Parser- or name-level, so it can land before #196.
2. **`try` placement** (rank 2). When `try` covers an expression that cannot fail and its operand holds a fallible call or a `try`, name the cause. For `try with(W) { try f() }`, say that the inner `try` already escapes. For `try a == b` and `try x.F().G`, suggest binding first. The fix is `maybe`. Checker-level (#196).
3. **Prefix `catch`** (rank 4). At `catch` in expression-start position, S001 with "`catch` follows the expression it handles", and a `safe` fix that moves the call before `catch`. Parser-level, so it can land now.
4. **An error bound as a value, and `nil`** (rank 6, and the issue's first witness). Give a multi-value `:=` whose right side has an error row, `err != nil`, `return v, nil` and the name `nil` a message that names `try`, `catch` or `Option`, instead of S109 or a bare S104. Parser-level for `nil`; checker-level for the row.
5. **A partial catch without `try`** (rank 8). Extend S103's message with "write `try e catch { … }` to let `X` escape". This is the cheapest change: one message string, measured to fire today.

Rank 3 (`needs` read as a footprint) is a concept error, not a syntax error, so it goes to the skill and to `ergo inspect`. Rank 5 (a resource used after a move) needs the linearity pass, so it belongs to #197's first test list.

### Stage 4: the skill

- Make the check loop the first rule: write, run `ergo check`, read the code, apply the `safe` fixes, check again. Every mistake class found in the chat sketches gets a diagnostic today, though most messages do not name the fix (measured with probes, rows 1, 2, 4, 6, 7, 8, 12, 15 and 16). The gated files had about 130 times fewer mistakes per line (measured rates 5.3 and 0.04 per 100 lines, from 21 and 1 instances; the cause is inferred).
- Put these rows in the Go → Ergo table, in this order: `2 * time.Second` → `2s`; `v, err := f(); if err != nil` → `v := try f()`; `nil` → `Option`; `try { } catch` → `f() catch { case T: … }`; a partial catch → `try f() catch { … }`; `&T{}` and `*T` results → values; `defer r.Close()` → nothing; `(T, bool)` → `Option`.
- State the two scope rules that the sketches broke: a `try` inside `with` escapes to the enclosing function, and a `func` literal needs `return` where a trailing block does not.
- State that purity is the footprint, not the `needs` row ([rows.md](../spec/rows.md#footprints)).
- State that `timeout.After` needs `Scheduler`, not `Clock`.
- Gate every code block like the README (`TestReadmeSmoke`). The README unit's implementor wrote nine gated blocks in 47 file calls. Their one pattern hit is a designed `*T` parameter, so the count of Go-habit mistakes is 0 (measured), and its round-3 review found that all nine "parse and format".

### For stage 1b, the eval

- Use the 18 classes above as the scoring rubric, and count old spellings separately.
- Take tasks that cover durations, laws, `with` plus `try`, resources, and actors. Those are where the chat sketches failed (inferred).
- Run each model once with no checker access and once with the check loop. The measured gap above suggests the loop is the largest effect (inferred).

## Reproduction

Scratch files are under `/home/exedev/.cache/baton/wf10/mistakes/`. They are not part of the repository.

- `scan-findings.ts` → `findings-buckets.tsv` (source 1 buckets).
- `patterns.tsv`, `corpus-files.txt`, `corpus-patterns.txt` (source 2 patterns).
- `run-corpus.sh` → `corpus-diags.tsv` (source 4 on the corpus).
- `extract-writes.sh` → `writes.jsonl`; `extract-chat.sh` → `chat.jsonl`; `scan-writes.ts` → `writes-hits.tsv`, `chat-hits.tsv`, `writes-post.tsv`, `chat-post.tsv` (source 3 frame and hits).
- `split-blocks.ts`, `check-dirs.sh` → `chat-diags.tsv`, `write-diags.tsv` (source 4 on transcript code).
- `probes.ts` → `probes.tsv` (detection per construct).
- `t-selfcorrect.txt`, `t-notergo.txt` (self-corrections in transcripts).

The checker is `go build -o bin/ergo ./cmd/ergo` at 0ce502ce. Its JSON comes from `ergo check FILE`. `--json` is accepted only by `ergo diagnostics`.
