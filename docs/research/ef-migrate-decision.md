# Does an ef migrate tool pay for itself?

Ticket [#194](https://github.com/cevr/effra/issues/194). Date: 2026-10-27. The corpus that [#204](https://github.com/cevr/effra/issues/204) must move to the 2026 edition is 55 `.ef` files outside `internal/compiler/` (measured with `git ls-files '*.ef'` at `854f21e7`: 35 under `examples/`, 2 in `examples/lintpack/testdata`, 7 in `conformance/size`, 11 in `benchmarks/where`). The 172 files under `internal/compiler/` go with the earlier compiler in [#205](https://github.com/cevr/effra/issues/205). This note measures five hand migrations and applies the decision rule of #194: price a tool by the rules it needs and the measured cost of one rule, and compare it with the hand cost of the mechanical share.

Each claim carries a label: **measured** (counted or timed in this unit) or **inferred** (derived from measured numbers with a stated assumption).

## Method

- **Examples.** `examples/main.ef`, `examples/data.ef`, `examples/lifecycle.ef`, `examples/checkout.ef` and `examples/machine-breaker.ef` were migrated by hand to the language that [`docs/spec/`](../spec/README.md) v0 states, not only to the prototype subset. The results are in [`ef-migrate/`](ef-migrate/), under an [edition manifest](../../GLOSSARY.md) that names `2026`.
- **Clock.** The agent that migrated the files took a `date +%s` stamp before and after each file. The minutes are agent wall-clock, not human minutes. Reading the spec once before the first file took 41 s and is not in the per-file minutes (measured).
- **Edit classes.** An edit is *mechanical* when a rewrite rule applies with no decision, and *judgment* when it needs a semantic choice. [`ledger.json`](ef-migrate/ledger.json) lists the edit groups per example, the constructs with no spec-v0 counterpart, and the 33 distinct mechanical rules. A test in `scripts/migratemeasure` checks that the edit groups sum to the totals.
- **Sources consulted.** No sketch-eval rewrite was a starting point. `prototypes/sketch9/testdata/checkout.sketch`, which is close to the checkout migration, was read earlier in this unit while the adapter was built, so the checkout minutes are biased low (measured: it is the fastest file at 17 s for the most mechanical edits).
- **Rule cost.** A throwaway rewriter over the earlier compiler's parsed `Program` applied the 18 mechanical rules that `examples/main.ef` needs. Writing it took 49 s for 180 lines. Its output equals the hand migration apart from comments and blank lines, and it checks and runs under the 2026 edition (measured). The rewriter was a scratch file and is not committed.

## Measurement

`go run ./scripts/migratemeasure` prints the table below. The code-line, token and check columns are computed from the files; the edit counts and minutes come from the ledger. The check column is the 2026 front-end today (the prototype through `internal/sketchfront`): `ok` or the first code.

| example | earlier code lines | 2026 code lines | tokens before | tokens after | mechanical | judgment | minutes | ef check (2026) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| examples/main.ef | 31 | 31 | 189 | 178 | 20 | 0 | 0.43 | ok |
| examples/data.ef | 33 | 25 | 113 | 128 | 14 | 1 | 0.33 | ok |
| examples/lifecycle.ef | 31 | 33 | 238 | 196 | 19 | 7 | 0.57 | S001 |
| examples/checkout.ef | 72 | 61 | 342 | 344 | 38 | 0 | 0.28 | ok |
| examples/machine-breaker.ef | 89 | 83 | 513 | 594 | 43 | 5 | 0.43 | S001 |

| measure | value |
| --- | ---: |
| mean minutes per example | 0.41 |
| mechanical share | 134/147 = 0.912 |
| hand cost of the mechanical share (55 files) | 20.5 min |
| measured rule cost | 18 rules in 49 s = 2.7 s/rule |
| distinct mechanical rules | 33 |
| tool cost (rules × rule cost) | 1.5 min |

Reading the table:

- Three of five migrations check in the prototype today (measured). `examples/lifecycle.ef` fails at a grouped `import ( ... )` and `examples/machine-breaker.ef` at the two-subject `switch s, sig := ...`: both are spec-v0 syntax the frozen prototype does not parse (measured: S001 at 7:8 and 39:57). [#195](https://github.com/cevr/effra/issues/195) owns the parser that admits them.
- The 2026 text is about the same size: 256 code lines before and 233 after, 1395 tokens before and 1440 after (measured). Data declarations shrink; machines grow, because each decision word becomes a struct literal.
- Judgment edits cluster where the earlier language had built-ins or keyword syntax that spec v0 moves to host packages or drops (measured from the ledger): the built-in `Files`, `Env` and `Runtime` services in `examples/lifecycle.ef`; a `main` that returns a value in `examples/data.ef` and `examples/machine-breaker.ef`; or-patterns inside one cell of a multi-subject match, and decision-word patterns, in `examples/machine-breaker.ef`.

## Cost estimate

- **Hand cost.** 55 files × 0.41 min × 0.912 = 20.5 agent-minutes for the mechanical share (inferred: it assumes the five examples are representative of the 55 files; they range from 31 to 89 code lines and cover effects, data, resources, services with errors and timeout, and machines). The judgment share costs the same with or without a tool, since a tool leaves those edits to a person, so it cancels out of the comparison.
- **Tool cost.** 33 rules × 2.7 s/rule = 1.5 agent-minutes (inferred: it prices every rule at the rate of the 18 rules measured on `examples/main.ef`).
- **Margin.** The rule-cost probe was a throwaway: it dropped comments, had no tests, and knew its one input. A tool fit for 55 files needs comment preservation, a test per rule, and the long tail of rules that the other 50 files bring. At 10 times the priced cost, the tool is 15 agent-minutes, still below 20.5 (inferred). The margin is not wide, and it narrows if the corpus long tail is large. The tool also leaves a rerunnable record of every rewrite; hand migration leaves none.
- **Clock caveat.** Both sides are agent minutes measured the same way, so the ratio holds even though a person would take longer on both (inferred).

## Scope of the tool

Build `ef migrate` for the mechanical rules only. It reads a `legacy` file with the earlier front-end, rewrites the 33 rules of the ledger, and prints 2026 source. Where a judgment edit is needed (built-in services with no spec-v0 counterpart, `main` that returns a value, or-patterns in a tuple cell), it leaves a marker comment and the file fails `ef check` until a person resolves it. It retires with the earlier front-end in #205, after #204 has run it over the corpus. This is the shape of `cargo fix --edition`.

## Prior art

- **Rust.** `cargo fix --edition` applies the compiler's machine-applicable migration lints for the next edition, then the author flips `edition` in `Cargo.toml`. Lints that need a decision stay as warnings for a person. The split of mechanical and judgment edits here follows it.
- **Go.** `go fix` rewrites code that uses old APIs to new ones, one named fixer per change. A fixer is small and runs over a parsed file, as the rule-cost probe did.
- **Gleam.** `gleam fix` was shipped for breaking syntax changes before v1, to rewrite projects to the new syntax; `gleam format` keeps the output canonical.
- **ReScript.** The move from the Reason/BuckleScript syntax to ReScript syntax shipped a converter in the compiler (`bsc -format` on a `.re` file printed `.res`), so projects migrated by tool, file by file.
- **Elixir.** Deprecations come first as warnings across releases; `mix format --migrate` rewrites deprecated forms in place.
- **MoonBit.** Breaking changes ship as deprecation warnings and are migrated with tooling; the `moon.mod.json` manifest has no edition field.

Every compile-to-host language above ships a mechanical rewriter for a syntax break and leaves semantic choices to a person. None migrates a corpus by hand when a parser for the old syntax already exists, as `internal/compiler` does here.

Decision: build ef migrate — the measured rule cost prices a 33-rule tool at 1.5 agent-minutes, and even at 10 times that it is below the 20.5 agent-minutes of migrating the mechanical share of 55 files by hand.
