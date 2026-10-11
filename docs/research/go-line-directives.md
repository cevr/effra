# Source positions in generated Go

Measured on 2026-10-11 on branch `wf10/spike-line`. The baseline is main `c1403350`; the measured implementation is `438549fa`. The measurement ran on its working tree before the commit, which added only the refusal of unnameable origins (no example is affected). The question comes from Spike B of the Tov/Goose prior-art pass: can generated Go carry `.ef` positions cheaply enough that Go's own toolchain reports user code at `.ef` lines? That covers residual bounds checks, escape reports, panics, stack traces and profiles. NORTH_STAR asks for residual checks to be visible and for operability without "generated-code archaeology". `docs/design.md` asks emitters to carry source mappings.

## What Effra emits

`ef build`, `ef run` and `ef test` lower through `Result.GoApplicationFrom`, which names the source file. The generated `main.go` carries Go `//line` directives:

- every Go line lowered from a user declaration or statement is preceded by `//line <name>.ef:<line>`, where `<name>` is the source's base name;
- a line of generated glue that follows user code gets `//line main.go:<n>`, naming its own true position, so glue keeps its `main.go` positions;
- the generated runtime package carries no directives and keeps its Go paths;
- bundled library declarations carry no directives, because their spans are positions in bundled sources that have no file in the generated module;
- `Result.GoApplication`, which has no file name, emits no directives, so the compiler never invents one.

Go resolves a relative directive file name against the directory of the file that holds the directive. Under ef build's `-trimpath`, frames read `effra.generated/<name>.ef:<line>`. The bytes contain no path, so two checkouts generate identical Go and identical build cache keys.

A directive positions only the line that follows it, and every later line counts on from there. One directive per statement would therefore attribute the second Go line of a statement to the next `.ef` line. Each Go line lowered from a user statement gets its own directive instead. The emitter marks declarations and statements, formats once, and then places directives over the formatted program. It formats again and repeats until formatting moves no directive, because gofmt keeps a blank line before a top-level declaration's comment and can split a line once comments sit inside it. Only comment lines and those blank lines are added. A test checks that every example, in both entry modes, compiles exactly the tokens of the unnamed lowering, is gofmt-formatted, and is byte-identical from two different absolute checkouts.

Granularity is the statement. A multi-line `.ef` statement maps to its first line, and directives carry no column, so Go reports no column inside user code. An origin whose base name Go would misread (`main.go`, or a name ending in `:<digits>`) is refused.

## Evidence

The red/green test is `TestGoLineDirectivesAttributeStackTracesToSource` in `internal/compiler/go_line_directive_test.go`. It builds a probe whose host call prints the goroutine's stack, then lowers the stack limit and recurses until Go reports a fatal stack overflow, which no defect boundary can recover. At `c1403350` it fails: every user frame reads `effra.generated/main.go:<n>`. The failure is recorded in the unit's `red-receipt.log`. At `438549fa`:

- `efFunction_trace` reads `probe.ef:8`;
- `efFunction_main` reads `probe.ef:17` and `probe.ef:18`;
- `efFunction_depth` reads `probe.ef:4`, and the overflowing frame reads `probe.ef:3`, the declaration line, where Go places the entry stack check;
- runtime frames keep `effra.generated/runtime/*.go`, `main.main` keeps `effra.generated/main.go`, and standard-library frames keep their own paths.

The CLI shows the same result: `ef run` on a program that calls `debug.PrintStack` prints `effra.generated/demo.ef:<line>` for user frames.

`go vet ./...` passes on every generated build module that carries directives.

## Measurement

`go run ./scripts/residual` lowers the native build entry of every `examples/*.ef` twice: without directives ("before", identical to `c1403350`'s output) and with them ("after"). Both lowerings come from the same compiler, so the comparison isolates the directives. For each lowering it records:

- generated `main.go` bytes;
- lowering time (5 samples);
- unstripped binary bytes, built with ef build's flags (`-trimpath -mod=readonly -buildvcs=false`);
- cold build seconds over 3 samples. A nonce constant rewritten in the generated main and runtime packages forces both to recompile while the standard library stays cached. Samples interleave the two lowerings, alternate which goes first, and record the load average;
- the compiler's `Found IsInBounds` lines (from `-d=ssa/check_bce/debug=1`) and `escapes to heap` lines (from `-m`), by the file Go attributes them to: the `.ef` source, generated `main.go`, the generated runtime, or other (inlined standard library and `<autogenerated>` wrappers).

Go was 1.27.0 linux/amd64, with a dedicated `GOCACHE`. The host was shared, with a 1-minute load average between 38 and 57 throughout; each row lists its load samples. Three examples have no native build entry and are skipped: `causal.ef` and `testing.ef` are test-only, and `missing-service.ef` is a deliberate check failure. Raw JSON and logs are in `/home/exedev/.cache/baton/wf10/spike-line/residual-1.{json,md,log}`.

Attribution cells read `.ef / main.go / runtime / other`.

| example | main.go bytes before / after (delta) | lowering us median before / after | cold build s median [range] before / after | binary bytes before / after (delta) | IsInBounds before -> after | escapes before -> after | vet after |
|---|---|---|---|---|---|---|---|
| callables-factory.ef | 8893 / 13980 (+57.2%) | 2130 / 4848 | 0.73 [0.59-1.47] / 0.82 [0.64-1.05] | 2796570 / 2797042 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/62/215/63 -> 34/28/215/63 | ok |
| callables-service.ef | 6033 / 9070 (+50.3%) | 1137 / 2442 | 0.51 [0.51-0.56] / 0.56 [0.45-0.64] | 2732014 / 2731982 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/52/165/23 -> 29/23/165/23 | ok |
| callables-state.ef | 4331 / 5947 (+37.3%) | 511 / 1686 | 0.44 [0.41-0.44] / 0.49 [0.38-0.55] | 2720013 / 2720413 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/27/186/43 -> 14/13/186/43 | ok |
| checkout.ef | 13402 / 17386 (+29.7%) | 2477 / 6635 | 0.73 [0.64-0.76] / 0.70 [0.68-0.73] | 2999504 / 2999584 (+0.0%) | 0/0/28/53 -> 0/0/28/53 | 0/158/334/73 -> 91/67/334/73 | ok |
| codec-transforms.ef | 23781 / 32315 (+35.9%) | 3834 / 12145 | 0.75 [0.70-0.89] / 0.72 [0.71-0.91] | 2959471 / 2959159 (-0.0%) | 0/8/85/0 -> 0/8/85/0 | 0/229/522/98 -> 81/148/522/98 | ok |
| codecs.ef | 8035 / 8675 (+8.0%) | 1280 / 5524 | 0.53 [0.51-0.53] / 0.50 [0.45-0.55] | 2788485 / 2788805 (+0.0%) | 0/8/85/0 -> 0/8/85/0 | 0/82/438/23 -> 5/77/438/23 | ok |
| concurrency.ef | 9852 / 12582 (+27.7%) | 3000 / 6853 | 0.54 [0.50-0.56] / 0.52 [0.50-0.53] | 2871050 / 2871530 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/116/270/43 -> 40/76/270/43 | ok |
| data.ef | 3007 / 3801 (+26.4%) | 351 / 1302 | 0.45 [0.34-1.09] / 0.34 [0.33-0.49] | 2704049 / 2703777 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/11/164/23 -> 5/6/164/23 | ok |
| generic-settings.ef | 7346 / 9767 (+33.0%) | 892 / 2107 | 0.38 [0.34-0.38] / 0.36 [0.32-0.41] | 2713027 / 2713547 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/19/164/23 -> 12/7/164/23 | ok |
| generic-users.ef | 8803 / 11571 (+31.4%) | 1276 / 2614 | 0.46 [0.44-0.48] / 0.42 [0.42-0.45] | 2731160 / 2730872 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/53/186/43 -> 36/17/186/43 | ok |
| host-types.ef | 73475 / 116916 (+59.1%) | 16908 / 58436 | 1.39 [1.35-1.50] / 1.46 [1.43-1.56] | 3327255 / 3324959 (-0.1%) | 0/0/2/0 -> 0/0/2/0 | 0/520/352/168 -> 498/22/352/168 | ok |
| http-endpoints.ef | 26965 / 32924 (+22.1%) | 4453 / 10987 | 1.03 [1.00-1.03] / 1.16 [1.07-1.27] | 9181126 / 9181230 (+0.0%) | 0/9/85/0 -> 0/9/85/0 | 0/225/696/133 -> 65/160/696/133 | ok |
| http-transport.ef | 13577 / 18207 (+34.1%) | 3492 / 7137 | 0.88 [0.85-0.91] / 0.93 [0.88-0.97] | 8977372 / 8977956 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/84/366/78 -> 31/53/366/78 | ok |
| http.ef | 14484 / 16471 (+13.7%) | 1910 / 5859 | 1.07 [1.06-1.09] / 1.04 [0.99-1.15] | 9121763 / 9120531 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/147/515/123 -> 35/112/515/123 | ok |
| imports.ef | 9251 / 11681 (+26.3%) | 1355 / 4030 | 0.56 [0.52-0.57] / 0.58 [0.55-0.63] | 2906112 / 2906016 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/95/280/73 -> 34/61/280/73 | ok |
| latest-task.ef | 5843 / 7717 (+32.1%) | 1261 / 2360 | 0.48 [0.46-0.56] / 0.74 [0.44-0.96] | 2809200 / 2809448 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/59/239/43 -> 18/41/239/43 | ok |
| layers-workflow.ef | 8108 / 11939 (+47.2%) | 1848 / 4789 | 0.48 [0.43-0.58] / 0.51 [0.46-0.62] | 2872554 / 2872274 (-0.0%) | 0/0/28/53 -> 0/0/28/53 | 0/58/272/53 -> 43/15/272/53 | ok |
| layers.ef | 8699 / 11755 (+35.1%) | 1768 / 4819 | 0.46 [0.46-0.47] / 0.45 [0.44-0.48] | 2873193 / 2872905 (-0.0%) | 0/0/28/53 -> 0/0/28/53 | 0/56/281/63 -> 38/18/281/63 | ok |
| lifecycle.ef | 13152 / 15825 (+20.3%) | 3033 / 6354 | 1.03 [0.76-1.05] / 0.74 [0.73-0.81] | 4310373 / 4309877 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/159/354/58 -> 43/116/354/58 | ok |
| main.ef | 5346 / 6748 (+26.2%) | 810 / 2025 | 0.49 [0.47-0.52] / 0.48 [0.41-0.98] | 2746746 / 2746818 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/47/180/28 -> 26/21/180/28 | ok |
| ownership.ef | 4291 / 5140 (+19.8%) | 912 / 1571 | 0.46 [0.41-0.51] / 0.48 [0.47-0.50] | 2788895 / 2788391 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/53/258/43 -> 14/39/258/43 | ok |
| pipe.ef | 2684 / 3501 (+30.4%) | 488 / 1011 | 0.39 [0.37-0.40] / 0.39 [0.37-0.43] | 2711090 / 2710698 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/22/164/23 -> 13/9/164/23 | ok |
| recipes-queue.ef | 10541 / 15345 (+45.6%) | 1183 / 4092 | 0.46 [0.44-0.52] / 0.43 [0.41-0.48] | 2757466 / 2756802 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/87/180/28 -> 68/19/180/28 | ok |
| recipes-retry.ef | 7949 / 11401 (+43.4%) | 1244 / 5319 | 0.49 [0.46-0.56] / 0.43 [0.40-0.44] | 2755959 / 2755943 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/84/180/28 -> 65/19/180/28 | ok |
| recovery-codec.ef | 8025 / 11346 (+41.4%) | 1375 / 3136 | 0.49 [0.43-0.53] / 0.48 [0.46-0.76] | 2773930 / 2773698 (-0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/77/190/28 -> 47/30/190/28 | ok |
| recovery-outcome.ef | 9065 / 14070 (+55.2%) | 1412 / 4174 | 0.70 [0.59-0.72] / 0.70 [0.66-0.86] | 2779388 / 2779716 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/115/197/38 -> 90/25/197/38 | ok |
| workflow.ef | 8917 / 11877 (+33.2%) | 2610 / 7114 | 0.44 [0.39-0.49] / 0.42 [0.41-0.45] | 2762552 / 2762560 (+0.0%) | 0/0/2/0 -> 0/0/2/0 | 0/95/180/28 -> 66/29/180/28 | ok |
| **total (27)** | 323855 / 447957 (+38.3%) | | 16.84 / 16.88 (sum of medians, +0.2%) | 96470317 / 96466533 (-0.0%) | 0/25/381/159 -> 0/25/381/159 | 0/2792/7528/1491 -> 1541/1251/7528/1491 | 27 ok |

Every example's bounds-check and escape totals are identical before and after, so directives move attribution and change no check or allocation.

## Hypothesis

| Claim | Result |
| --- | --- |
| Generated source grows by under 10% | **Refuted.** It grew 38.3% in total, from +8.0% (`codecs.ef`) to +59.1% (`host-types.ef`). The cost is one directive per Go line of user code, because Go positions only the line after a directive and lowering expands one statement into several Go lines. Code dominated by user statements pays the most. |
| Cold build time stays within noise | **Confirmed.** The sum of per-example medians went from 16.84 s to 16.88 s (+0.2%). In 25 of 27 rows the before and after ranges overlap. The two that do not split both ways: `http-endpoints.ef` is slower (median +0.13 s) and `recipes-retry.ef` is faster (-0.06 s). The load average was 38-57, so differences under about 0.1 s are not resolvable. The Go scanner skips comments, so source bytes were the wrong proxy for build cost. |
| Unstripped binary grows by under 1% | **Confirmed.** The total changed by -0.004%, and no example moved by more than 0.07%. Some binaries shrink. A plausible cause is that user code moves from many distinct `main.go` lines to fewer `.ef` lines, which shortens the line tables; this was not verified. |
| Most user-code check and escape lines are attributed to `.ef` | **Confirmed for escapes.** Inside user declarations, attribution is complete by construction: every line gets a `.ef` directive. Of the generated main's 2,792 heap-escape lines, 1,541 (55%) now name `.ef` lines. The remaining 1,251 are glue: helpers, codecs, service plumbing and the entry, which keep their true `main.go` positions. In `host-types.ef`, 498 of 520 move to `.ef`. **Not exercised for bounds checks.** No example's user code leaves a bounds check. The 25 residual `IsInBounds` lines in the generated main are in codec glue and stay at `main.go`. The runtime's 381 and standard library's 159 keep their Go paths. |

Lowering time also rose. Median lowering went from 0.35-16.9 ms to 1.0-58.4 ms per example, 1.7 to 4.3 times slower, because placement scans and reformats the program at least once more. It stays under 60 ms against cold builds of 0.34 s and more, but it is real frontend cost (see Follow-ups).

The size fixtures `minimal` / `minimal-unused` and `direct` / `pipe` no longer build byte-identical executables. Their position tables name different files and lines. Their symbols by name, kind and size, and their generated mains' compiled tokens, are still identical, and the gate now checks exactly that (`cmd/ef/size_process_test.go`).

## Prior art

| System | Path | Effra |
| --- | --- | --- |
| Goose `--bce-lines` (spec §10.5) | A custom pass reports each residual bounds check by source line, behind an opt-in flag. | Same receipt, produced by Go's own `-d=ssa/check_bce` and `-m` with no Effra pass. The table above is that report summed per file. |
| Go `//line`: cgo | `cmd/cgo/out.go` writes `//line <src>:1:1` at the top of the rewritten file and `//line <file>:<line>` before generated result checks, so checks report the user's call site (Go 1.27.0, `src/cmd/cgo/out.go` L713, L1182). | **The path Effra matches.** In-band directives in generated Go, with glue named as itself. |
| Go `//line`: goyacc | Emits `//line <grammar>.y:<n>` before copied semantic actions; `-l` disables them. Not checked against a pinned source here. | Same: user-written fragments are attributed to their source, and generated tables are not. |
| templ | Its language server translates gopls positions through templ's own source map. Whether its generated Go also carries `//line` was not verified at a pin. | No claim. |
| Gleam (JS) | Source Map v3 side files, opt-in through `javascript.source_maps` (default false; `compiler-core/src/config.rs` L784 at `52e735c8`). A cursor observer records positions while the printer writes (`compiler-core/src/javascript.rs` L78-L93). | Effra's JS target has no source maps yet. Gleam's printer-integrated recording is the design that would remove Effra's reformatting pass. |
| ReScript (JS) | Source Map v3: IR nodes carry locations, and the printer records each one as it reaches it. Maps are linked, inline or hidden, off by default (`compiler/core/js_source_map.ml` L1-L18 at `30ce3698`; `rewatch/src/config.rs` L300-L337). | Same two-phase shape as Effra (locations on IR, positions fixed at print time). Effra places directives after `go/format` because gofmt, not the emitter, decides the final lines. |
| Elixir → Erlang | Every abstract-format node carries an `erl_anno` line, optionally with a column and a `generated` flag (`elixir_erl.erl` L46-L56 at `91ee75bb`). A definition from another file is preceded by `-file` (`{attribute, Anno, file, {File, Line}}`, `elixir_erl.erl` L247-L258). BEAM line tables then report `.ex` positions in stack traces. | Equivalent in-band path: positions travel with the generated code, and the host toolchain builds its line tables from them. Effra's `main.go` positions for glue play the role of `generated: true`. Erlang annotates per expression, Effra per statement. |
| MoonBit | The pinned compiler (`d4ada10d`) builds wasm-gc only. `-source-map` writes a side-car map for the wasm module (`src/moon0_main.ml` L127-L142, `src/driver_config.ml` L125). | Side-car, opt-in. Go reads no side-car maps, so it does not apply to Effra's native target. |

Go's toolchain reads positions only in-band: no side-car map reaches panics, pprof or `-m`. That makes `//line` the only path to the Goose receipt on Go. Effra emits directives by default where Gleam, ReScript and MoonBit make maps opt-in. A JS map costs a separate artifact and bundle work, while the measured Go cost is zero in build time and binary size.

## Follow-ups

- Record lowering in a position-aware printer, as Gleam and ReScript do, or write gofmt-stable output directly, so that the extra formatting pass disappears.
- Name bundled sources so that bundled declarations get directives too.
- Positions below statement granularity: expression-level lines for multi-line statements, and columns.
- A `residualChecks` section in the application receipt, from `scripts/residual`'s parser.
- Source maps for the JS target.
