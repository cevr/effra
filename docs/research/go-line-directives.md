# Source positions in generated Go

Measured on 2026-10-11 on branch `wf10/spike-line`. The baseline is main `c1403350`. The measured placement is `28f6ece1`, and the measuring tool is at `4e93573d`. The question comes from Spike B of the Tov/Goose prior-art pass: can generated Go carry `.ef` positions cheaply enough that Go's own toolchain reports user code at `.ef` lines? That covers residual bounds checks, escape reports, panics, stack traces and profiles. NORTH_STAR asks for residual checks to be visible and for operability without "generated-code archaeology". `docs/design.md` asks emitters to carry source mappings.

## What Effra emits

`ef build`, `ef run` and `ef test` lower through `Result.GoApplicationFrom`, which names the source file. The generated `main.go` carries Go `//line` directives:

- every Go line lowered from a user declaration or statement is attributed to `<name>.ef:<line>`, where `<name>` is the source's base name;
- a line of generated glue that follows user code gets `//line main.go:<n>`, naming its own true position, so glue keeps its `main.go` positions;
- the generated runtime package carries no directives and keeps its Go paths;
- bundled library declarations carry no directives, because their spans are positions in bundled sources that have no file in the generated module;
- `Result.GoApplication`, which has no file name, emits no directives, so the compiler never invents one.

Go resolves a relative directive file name against the directory of the file that holds the directive. Under ef build's `-trimpath`, frames read `effra.generated/<name>.ef:<line>`. The bytes contain no path, so two checkouts generate identical Go and identical build cache keys.

### Placement

A directive positions only the line that follows it, and every later line counts on from there. One directive per statement would therefore attribute the second Go line of a statement to the next `.ef` line, so placement works per line.

The emitter writes a mark before each user declaration and statement, and a reset mark where glue resumes, then formats the program once. From that marked program it reads one intended position for every compiled token: the position of the mark before it, or the token's own `main.go` line for glue. Placement drops all marks and directives and derives new directives from those per-token intents alone. It writes a directive wherever Go's line counting would otherwise give a token the wrong position, and it fails with an error rather than produce a wrong result when:

- one line holds tokens with different intents;
- a mark sits beside code;
- a line that starts inside a multi-line string or comment would need a directive.

Only comment lines, and the blank line gofmt keeps before a top-level declaration's comment, are added. The program Go compiles is the unnamed lowering's.

The placed program is formatted again so that the output stays gofmt-formatted. Comments inside a construct can make gofmt split a line it kept whole: in `host-types.ef`, the one-line body `}]] { if ctx.s_Foreign == nil {` is split once directives sit inside its multi-line type. Placement then runs again from the same per-token intents, never from the directives of the previous pass. The output is the first formatted program that placement leaves unchanged, and at most four placements are tried before lowering fails. Every example settles at its second placement, which confirms the first, except `host-types.ef`, which settles at its third. The loop exists only to keep the output gofmt-stable: every placement it returns is derived from the intents, so position correctness does not depend on how often gofmt moves lines.

Round 1 rebuilt the second pass's intents from the first pass's explicit directives. Every position that pass had left implicit was then lost when gofmt moved a line, and 487 of the 14,562 compiled tokens of `host-types.ef`'s build entry read one `.ef` line early (for example, `return efLocal_point`, the statement at `host-types.ef:8`, read `:7`).

Granularity is the statement. A multi-line `.ef` statement maps to its first line, and directives carry no column, so Go reports no column inside user code.

Go's directive grammar peels a trailing `:<digits>` off the file name as a line or column and has no escape (`cmd/compile/internal/syntax/parser.go` and `go/scanner/scanner.go`, `trailingDigits`). So `GoApplicationFrom` refuses an origin whose base name ends in `:` followed by a number, and one that contains a line break. `ef` itself admits only `.ef` sources, so the refusal guards the API boundary, not the CLI. Names such as `probe:` and `probe:v2.ef` are admitted, and tests check that Go reads them back exactly.

## Evidence

The red/green test is `TestGoLineDirectivesAttributeStackTracesToSource` in `internal/compiler/go_line_directive_test.go`. It builds a probe whose host call prints the goroutine's stack, then lowers the stack limit and recurses until Go reports a fatal stack overflow, which no defect boundary can recover. At `c1403350` it fails: every user frame reads `effra.generated/main.go:<n>`. The failure is recorded in the unit's `red-receipt.log`. With directives:

- `efFunction_trace` reads `probe.ef:8`;
- `efFunction_main` reads `probe.ef:17` and `probe.ef:18`;
- `efFunction_depth` reads `probe.ef:4`, and the overflowing frame reads `probe.ef:3`, the declaration line, where Go places the entry stack check;
- runtime frames keep `effra.generated/runtime/*.go`, `main.main` keeps `effra.generated/main.go`, and standard-library frames keep their own paths.

`TestGoLineDirectivesPutEveryTokenAtItsIntendedPosition` is the per-token check. For every compiled token of every example, in both entry modes, it compares the position the marks intend with the position Go's scanner assigns after the directives. Glue tokens must sit at `main.go` on their own line. The round-1 placement fails it on `host-types.ef` with the 487 tokens above (the unit's `round-2-red-receipt.log`); the current placement passes it.

`TestGoLineDirectivesAreDeterministicCommentOnlyAndWellFormed` checks that every example's directive lowering is gofmt-formatted, parses to the same syntax tree as the unnamed lowering, and is byte-identical from two different absolute checkouts. The tree comparison (`internal/gosyntax`) drops comments and reduces each position to whether it is set, because go/ast records a type alias's `=` and a call's trailing `...` only as positions; two programs compare equal exactly when they differ only in layout and comments.

The CLI shows the same result: `ef run` on a program that calls `debug.PrintStack` prints `effra.generated/demo.ef:<line>` for user frames.

`go vet ./...` passes on every generated build module that carries directives.

## Measurement

`go run ./scripts/residual` lowers the native build entry of every `examples/*.ef` twice: without directives ("before", identical to `c1403350`'s output) and with them ("after"). Both lowerings come from the same compiler, so the comparison isolates the directives. For each lowering it records:

- generated `main.go` bytes;
- lowering time (5 samples);
- unstripped binary bytes, built with ef build's flags (`-trimpath -mod=readonly -buildvcs=false`);
- cold build seconds over 3 samples. A nonce constant rewritten in the generated main and runtime packages forces both to recompile while the standard library stays cached. Samples interleave the two lowerings, alternate which goes first, and record the load average;
- the compiler's `Found IsInBounds` lines (from `-d=ssa/check_bce/debug=1`) and `escapes to heap` lines (from `-m`), grouped by the file in the position Go prints: the `.ef` source, generated `main.go`, the generated runtime, or other (inlined standard library and `<autogenerated>` wrappers). `moved to heap:` lines are not counted.

Go was 1.27.0 linux/amd64, with a dedicated `GOCACHE`. The host was shared, with a 1-minute load average that fell from 34 to 11 during the run. Three examples have no native build entry and are skipped: `causal.ef` and `testing.ef` are test-only, and `missing-service.ef` is a deliberate check failure. The raw JSON, load samples and logs (`residual-2.{json,md,log}`) are local receipts of the spike implementor's round-2 measurement, not files in this repository; `go run ./scripts/residual -work DIR -json FILE -markdown FILE` writes the raw report and table for a new run.

Attribution cells read `.ef / main.go / runtime / other`.

| example | main.go bytes before / after (delta) | lowering us median before / after | cold build s median [range] before / after | binary bytes before / after (delta) | IsInBounds before -> after | escapes before -> after | vet after |
|---|---|---|---|---|---|---|---|
| callables-factory.ef | 8893 / 13980 (+57.2%) | 2522 / 6540 | 0.42 [0.40-0.43] / 0.43 [0.40-0.44] | 2796570 / 2797042 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/62/215/63 -> 34/28/215/63 | ok |
| callables-service.ef | 6033 / 9070 (+50.3%) | 739 / 2062 | 0.39 [0.39-0.40] / 0.37 [0.35-0.37] | 2732014 / 2731982 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/52/165/23 -> 29/23/165/23 | ok |
| callables-state.ef | 4331 / 5947 (+37.3%) | 719 / 1887 | 0.35 [0.33-0.36] / 0.38 [0.34-0.38] | 2720013 / 2720413 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/27/186/43 -> 14/13/186/43 | ok |
| checkout.ef | 13402 / 17386 (+29.7%) | 2557 / 8002 | 0.60 [0.57-0.68] / 0.59 [0.57-0.69] | 2999504 / 2999584 (+0.0%) | 0/0/28/53 -> 0/0/28/53 | 0/158/334/73 -> 91/67/334/73 | ok |
| codec-transforms.ef | 23781 / 32315 (+35.9%) | 4225 / 14678 | 0.50 [0.48-0.51] / 0.55 [0.49-0.55] | 2959471 / 2959159 (-0.0%) | 0/8/85/0 -> 0/8/85/0 | 0/229/522/98 -> 81/148/522/98 | ok |
| codecs.ef | 8035 / 8675 (+8.0%) | 869 / 3009 | 0.41 [0.39-0.44] / 0.47 [0.39-0.49] | 2788485 / 2788805 (+0.0%) | 0/8/85/0 -> 0/8/85/0 | 0/82/438/23 -> 5/77/438/23 | ok |
| concurrency.ef | 9852 / 12582 (+27.7%) | 2921 / 7986 | 0.52 [0.46-0.59] / 0.50 [0.47-0.52] | 2871050 / 2871530 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/116/270/43 -> 40/76/270/43 | ok |
| data.ef | 3007 / 3801 (+26.4%) | 283 / 887 | 0.39 [0.38-0.39] / 0.40 [0.36-0.40] | 2704049 / 2703777 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/11/164/23 -> 5/6/164/23 | ok |
| generic-settings.ef | 7346 / 9767 (+33.0%) | 795 / 3698 | 0.38 [0.38-0.39] / 0.40 [0.39-0.43] | 2713027 / 2713547 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/19/164/23 -> 12/7/164/23 | ok |
| generic-users.ef | 8803 / 11571 (+31.4%) | 1577 / 3333 | 0.45 [0.41-0.52] / 0.44 [0.41-0.48] | 2731160 / 2730872 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/53/186/43 -> 36/17/186/43 | ok |
| host-types.ef | 73475 / 116402 (+58.4%) | 15515 / 67529 | 1.01 [0.97-1.12] / 1.19 [1.06-1.21] | 3327255 / 3325023 (-0.1%) | 0/0/2/0 -> 0/0/2/0 | 0/520/352/168 -> 498/22/352/168 | ok |
| http-endpoints.ef | 26965 / 32924 (+22.1%) | 3680 / 9374 | 0.85 [0.82-0.97] / 0.95 [0.85-1.05] | 9181126 / 9181230 (+0.0%) | 0/9/85/0 -> 0/9/85/0 | 0/225/696/133 -> 65/160/696/133 | ok |
| http-transport.ef | 13577 / 18207 (+34.1%) | 5244 / 11462 | 0.78 [0.75-0.81] / 0.86 [0.77-0.87] | 8977372 / 8977956 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/84/366/78 -> 31/53/366/78 | ok |
| http.ef | 14484 / 16471 (+13.7%) | 4059 / 9454 | 0.92 [0.76-1.06] / 0.92 [0.86-1.03] | 9121763 / 9120531 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/147/515/123 -> 35/112/515/123 | ok |
| imports.ef | 9251 / 11681 (+26.3%) | 1936 / 4413 | 0.44 [0.40-0.49] / 0.43 [0.42-0.52] | 2906112 / 2906016 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/95/280/73 -> 34/61/280/73 | ok |
| latest-task.ef | 5843 / 7717 (+32.1%) | 667 / 2274 | 0.45 [0.41-0.89] / 0.42 [0.40-1.15] | 2809200 / 2809448 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/59/239/43 -> 18/41/239/43 | ok |
| layers-workflow.ef | 8108 / 11939 (+47.2%) | 3802 / 17573 | 0.40 [0.40-0.43] / 0.37 [0.37-0.40] | 2872554 / 2872274 (-0.0%) | 0/0/28/53 -> 0/0/28/53 | 0/58/272/53 -> 43/15/272/53 | ok |
| layers.ef | 8699 / 11755 (+35.1%) | 2774 / 9140 | 0.41 [0.41-0.42] / 0.40 [0.38-0.42] | 2873193 / 2872905 (-0.0%) | 0/0/28/53 -> 0/0/28/53 | 0/56/281/63 -> 38/18/281/63 | ok |
| lifecycle.ef | 13152 / 15825 (+20.3%) | 3864 / 5730 | 0.61 [0.60-1.64] / 0.64 [0.63-0.84] | 4310373 / 4309877 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/159/354/58 -> 43/116/354/58 | ok |
| main.ef | 5346 / 6748 (+26.2%) | 1042 / 3261 | 0.38 [0.33-0.39] / 0.32 [0.29-0.38] | 2746746 / 2746818 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/47/180/28 -> 26/21/180/28 | ok |
| ownership.ef | 4291 / 5140 (+19.8%) | 505 / 1208 | 0.41 [0.38-0.44] / 0.44 [0.38-0.47] | 2788895 / 2788391 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/53/258/43 -> 14/39/258/43 | ok |
| pipe.ef | 2684 / 3501 (+30.4%) | 679 / 2194 | 0.31 [0.30-0.37] / 0.33 [0.32-0.34] | 2711090 / 2710698 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/22/164/23 -> 13/9/164/23 | ok |
| recipes-queue.ef | 10541 / 15345 (+45.6%) | 2551 / 5427 | 0.33 [0.33-0.35] / 0.36 [0.33-0.37] | 2757466 / 2756802 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/87/180/28 -> 68/19/180/28 | ok |
| recipes-retry.ef | 7949 / 11401 (+43.4%) | 2011 / 4167 | 0.52 [0.32-0.62] / 0.36 [0.31-0.43] | 2755959 / 2755943 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/84/180/28 -> 65/19/180/28 | ok |
| recovery-codec.ef | 8025 / 11346 (+41.4%) | 870 / 2986 | 0.33 [0.32-0.34] / 0.33 [0.33-0.35] | 2773930 / 2773698 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/77/190/28 -> 47/30/190/28 | ok |
| recovery-outcome.ef | 9065 / 14070 (+55.2%) | 1700 / 7818 | 0.43 [0.37-0.44] / 0.40 [0.36-0.43] | 2779388 / 2779716 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/115/197/38 -> 90/25/197/38 | ok |
| workflow.ef | 8917 / 11877 (+33.2%) | 2778 / 10141 | 0.37 [0.33-0.39] / 0.35 [0.34-0.40] | 2762552 / 2762560 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/95/180/28 -> 66/29/180/28 | ok |
| **total (27)** | 323855 / 447443 (+38.2%) | | 13.35 / 13.60 (sum of medians, +1.9%) | 96470317 / 96466597 (-0.0%) | 0/25/381/159 -> 0/25/381/159 | 0/2792/7528/1491 -> 1541/1251/7528/1491 | 27 ok |

Every example's bounds-check and escape totals are identical before and after, so directives change where Go prints a line, not which checks or escapes there are.

### What the attribution counts mean

`-m` and check_bce print each line at the **outermost** inlined position (`cmd/compile/internal/base/print.go` L63-L67, `Ctxt.OutermostPos`, Go 1.27.0). A count in the table is therefore "lines whose printed file:line names this file", not "lines that come from this file's code". Three cases separate the two, all observed in round 1's review at `f587106c`. `data.ef` and `recipes-queue.ef` were unaffected by the round-1 placement defect, and their generated mains have the same size at the measured head.

- Lowering turns `match` and `if` expressions into immediately invoked closures that Go inlines. Code inside them prints at the enclosing statement's line: `efTemp3 + efLocal_requestId escapes to heap` prints at `data.ef:21`, although its own line carries `data.ef:23`.
- User code inlined into glue prints at `main.go`: `efType_State(...)` from `data.ef:33` printed at a `main.go` line once `efFunction_main` was inlined into the entry's `main.func1`.
- Glue inlined into user code prints at `.ef`: `recipes-queue.ef:27: &errors.errorString{...} escapes to heap` comes from the glue `efCall_Console_log`.

So the counts show how many reports now print at `.ef` positions, which is what a reader of `-m` or check_bce sees. They cannot show how much of the user's own code is attributed, nor that the `main.go` remainder is all glue. Where lowering inlines a closure, the receipt has the precision of the enclosing statement. Go's `-json` optimization logs keep the full inline chain and could attribute by innermost position (see Follow-ups).

## Hypothesis

| Claim | Result |
| --- | --- |
| Generated source grows by under 10% | **Refuted.** It grew 38.2% in total, from +8.0% (`codecs.ef`) to +58.4% (`host-types.ef`). The cost is one directive per Go line of user code, because Go positions only the line after a directive and lowering expands one statement into several Go lines. Code dominated by user statements pays the most. |
| Cold build time stays within noise | **No difference resolvable in this run.** With 3 samples per lowering, a load average of 11-34 and about 0.1 s resolution, the sum of per-example medians went from 13.35 s to 13.60 s (+1.9%). In 26 of 27 rows the before and after ranges overlap; the one that does not, `callables-service.ef`, is faster after. The largest median difference is `host-types.ef`, +0.18 s with overlapping ranges, on the largest generated file. It does not reproduce: the round-2 review re-measured `host-types.ef` with 12 interleaved cold samples per lowering at a load of 22-32 and found 0.95 s [0.85-1.06] before and 0.96 s [0.88-1.14] after, +0.7% (`review-1/residual-ht.{md,json}` in the unit directory). Round 1, at a load of 38-57, found +0.2% in total and +0.07 s for `host-types.ef`. The Go scanner skips comments, so source bytes are a poor proxy for build cost. |
| Unstripped binary grows by under 1% | **Confirmed.** The total changed by -0.004%, and no example moved by more than 0.07%. Some binaries shrink. A plausible cause is that user code moves from many distinct `main.go` lines to fewer `.ef` lines, which shortens the line tables; this was not verified. |
| Most user-code check and escape lines are attributed to `.ef` | **Partly shown, for escapes.** Of the generated main's 2,792 heap-escape lines, 1,541 (55%) now print at `.ef` positions; in `host-types.ef`, 498 of 520 do. By the outermost-position rule above, these counts do not measure user code exactly: inlining moves reports both ways between `.ef` and `main.go`. **Bounds checks cannot be exercised.** Effra source has no indexing (the postfix parser in `internal/compiler/syntax.go` has no index form), so the current language cannot author a bounds check in user code. The 25 `IsInBounds` lines in the generated main come from generated codec adapters, which do index (`internal/compiler/codec_emit.go`), and print at `main.go`. The runtime's 381 and the standard library's 159 keep their Go paths. |

Lowering is also slower. Median lowering went from 0.28-15.5 ms to 0.89-67.5 ms per example, 1.5 to 4.7 times as long. Placement scans and reformats the program at least once more, and `host-types.ef` twice more. It stays under 70 ms against cold builds of 0.3 s and more, but it is real frontend cost (see Follow-ups).

The size fixtures `minimal` / `minimal-unused` and `direct` / `pipe` no longer build byte-identical executables, because their position tables name different files and lines. The gate (`cmd/ef/size_process_test.go`) checks that their generated mains parse to the same syntax tree, up to layout and comments, and that their complete symbol lists from `go tool nm -size -type`, without addresses, match by name, kind and size.

## Prior art

| System | Path | Effra |
| --- | --- | --- |
| Goose `--bce-lines` (spec §10.5) | A custom pass reports each residual bounds check by source line, behind an opt-in flag. | Same receipt, produced by Go's own `-d=ssa/check_bce` and `-m` with no Effra pass, at Go's outermost-position precision. The table above is that report summed per printed file. |
| Go `//line`: cgo | `cmd/cgo/out.go` writes `//line <src>:1:1` at the top of the rewritten file and, before each generated result check in an exported function's wrapper, `//line <file>:<line>` naming the user's `//export`ed function declaration (`exp.Func.Pos()`), so a failed check reports that declaration (Go 1.27.0, `src/cmd/cgo/out.go` L716, L1184-L1185). It also writes inline `/*line :L:C*/` comments in rewritten expressions (`cmd/cgo/gcc.go` L1585). | **The path Effra matches.** In-band directives in generated Go, with glue named as itself. |
| Go `//line`: cmd/cover | Writes `//line <name>:1:1` at the top of each instrumented file (`src/cmd/cover/cover.go` L821, Go 1.27.0). | Same in-band mechanism, applied to a whole file. |
| Go `//line`: go/printer | `go/printer`'s `SourcePos` mode writes `//line` comments itself wherever an AST node's position differs from the line being printed (`src/go/printer/printer.go` L1299, Go 1.27.0). It reads unadjusted positions (L184), so it needs an AST and FileSet whose positions already encode the target lines. | The in-printer path inside Go's standard library. It does not fit Effra's text emission, but it is the natural target if Effra emits `go/ast` (see Follow-ups). |
| Go `//line`: goyacc | Emits `//line <grammar>.y:<n>` before copied semantic actions; `-l` disables them. Not checked against a pinned source here. | Same: user-written fragments are attributed to their source, and generated tables are not. |
| templ | Its language server translates gopls positions through templ's own source map. Whether its generated Go also carries `//line` was not verified at a pin. | No claim. |
| Borgo (compile-to-Go) | Emits no `//line` and keeps no position mapping (`compiler/src/codegen.rs` at `3b9f0157`). | Effra goes further than the nearest compile-to-Go precedent in PRIOR_ART.md. |
| Gleam (JS) | Source Map v3 side files, opt-in through `javascript.source_maps` (default false; `compiler-core/src/config.rs` L784 at `52e735c8`). A cursor observer records positions while the printer writes (`compiler-core/src/javascript.rs` L78-L93). | Effra's JS target has no source maps yet. Gleam's printer-integrated recording is the design that would remove Effra's reformatting pass. |
| ReScript (JS) | Source Map v3: IR nodes carry locations, and the printer records each one as it reaches it (`compiler/core/js_dump.ml` L472). Maps are linked, inline or hidden, off by default (`compiler/core/js_source_map.ml` L1-L20 at `30ce3698`; `rewatch/src/config.rs` L300-L337). | Same two-phase shape as Effra: positions are carried as metadata and fixed at print time. Effra fixes them after `go/format`, because gofmt, not the emitter, decides the final lines. |
| Elixir → Erlang | Every abstract-format node carries an `erl_anno` line, optionally with a column and a `generated` flag (`elixir_erl.erl` L48-L56 at `91ee75bb`). A definition from another file is preceded by `-file` (`{attribute, Anno, file, {File, Line}}`, `elixir_erl.erl` L247-L258). BEAM line tables then report `.ex` positions in stack traces. | An in-band path like Effra's, with one difference: positions travel inside the abstract-format AST and the code is never printed and re-formatted as text, so Elixir has no placement-after-formatting problem. Effra's `main.go` positions for glue play the role of `generated: true`. Erlang annotates per expression, Effra per statement. |
| MoonBit | The pinned compiler (`d4ada10d`) builds wasm-gc only. In debug mode only, `-source-map` writes a side-car map for the wasm module (`src/moon0_main.ml` L133-L142, where the map is emitted only under `!Config.debug`; `src/driver_config.ml` L125). | Side-car, opt-in. Go reads no side-car maps, so it does not apply to Effra's native target. |

Go's toolchain reads positions only in-band: no side-car map reaches panics, pprof or `-m`. That makes `//line` the only path to the Goose receipt on Go. Effra emits directives by default where Gleam, ReScript and MoonBit make maps opt-in. A JS map costs a separate artifact and bundle work. The measured Go cost is a 38% larger generated file, 1.5 to 4.7 times slower lowering, no build-time difference resolvable on this host, and binaries within 0.07%.

## Follow-ups

- Emit `go/ast` with positions that encode the `.ef` lines and print it with `go/printer`'s `SourcePos` mode, or record positions in a printer as Gleam and ReScript do, so that placement after formatting and its extra passes disappear.
- Attribute `-m` and check_bce by innermost position through Go's `-json` optimization logs, which keep the inline chain.
- Recovered panics carry no stack. The runtime's `Invoke` (`runtime/effra/effect.go`) turns a recovered panic into `Die("panic: %v")` without the goroutine's stack, so `.ef` positions reach users only through fatal errors, `debug.PrintStack` and pprof. Capturing the stack there is where `//line` would pay off most for operability. This predates the directives.
- Name bundled sources so that bundled declarations get directives too.
- Positions below statement granularity: expression-level lines for multi-line statements, and columns.
- A `residualChecks` section in the application receipt, from `scripts/residual`'s parser.
- Source maps for the JS target.
