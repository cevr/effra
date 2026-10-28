# Retirement of the earlier `.ef` language

[#205](https://github.com/cevr/ergo/issues/205) removed the earlier `.ef` language and its compiler, `internal/compiler`. Every `ergo` command now runs on the one front end, `internal/sketchfront`. This record keeps what was removed, at base commit `dbf135bff09901e85385945593113f35b300199c` (`dbf135bf`). Read any removed file with `git show dbf135bf:<path>`, or browse it at `https://github.com/cevr/ergo/tree/dbf135bff09901e85385945593113f35b300199c/<path>`.

## What was removed

Measured on 2026-10-28: the files present at `dbf135bf` and absent after the retirement, with each file's lines counted at `dbf135bf` as `wc -l` counts them (line feeds). Files that the retirement rewrote in place, such as `cmd/ergo/main.go` and `internal/receipt/receipt.go`, are not in the table.

| Group | Files | Lines |
| --- | ---: | ---: |
| `internal/compiler` (with `machineplan`) | 391 | 90600 |
| `internal/gosyntax` | 3 | 103 |
| `internal/edition` | 16 | 2256 |
| `internal/lsp` | 10 | 2315 |
| `internal/mcp` | 7 | 2951 |
| `internal/lintpacks` | 6 | 1409 |
| `lint` (the lint-pack SDK) | 27 | 5701 |
| `cmd/ergo` (deleted files) | 49 | 14245 |
| `examples/lintpack` | 7 | 540 |
| `examples/sdk` | 1 | 18 |
| `examples/hosttypes` | 2 | 396 |
| `examples/compare` | 2 | 178 |
| `examples/editions/legacy` | 3 | 32 |
| `examples/*.ef` and `examples/fixture.txt` | 36 | 2648 |
| `conformance/size/matrix` | 1 | 503 |
| `conformance/size/fixtures` | 7 | 171 |
| `scripts/migratemeasure` | 2 | 289 |
| `scripts/residual` | 2 | 592 |
| `scripts/ownership_sweep.py`, `scripts/ownership_truth.py` | 2 | 468 |
| Manifests and records: `ergo.bindings.json`, `examples/editions/sketch/ergo.json`, `docs/research/ef-migrate/ergo.json`, `docs/specs/editions.md` | 4 | 121 |
| Total | 578 | 125536 |

`internal/receipt` was reduced in place: it keeps `Location`, `Locate`, `LocateSource`, the physical-path helpers, `GoBuildCommand` and `GoBuildFlags`. The application receipt (`ergo.application-receipt/5`), `Native`, the measurement functions and `Destination` went with `ergo build --receipt`.

## Retired diagnostic codes

The EF codes of `internal/compiler`, read from its non-test Go sources at `dbf135bf`. "Sites" counts the places that emit the code. Every code is retired except EF110, which `internal/report` keeps for the target refusal. The new front end reports its own codes ([diagnostics](../spec/diagnostics.md)).

| Code | Meaning | Sites |
| --- | --- | ---: |
| EF001 | Lexical error: an unterminated string or block comment, a standalone carriage return, an integer outside the i64 range | 9 |
| EF002 | Parse error | 2 |
| EF003 | Absent syntax: a construct Ergo leaves out, with what to use instead | 1 |
| EF101 | Duplicate declaration, field or name | 11 |
| EF102 | Unknown name: a type, service, layer, field type or module member | 39 |
| EF103 | Invalid call or operation: a value that is not callable, an unsupported compiler-owned operation, a local that shadows a Go import | 9 |
| EF104 | A provider implementation does not implement its service | 6 |
| EF105 | A provider or layer provision of the wrong kind | 14 |
| EF106 | Type or argument mismatch: missing, extra or mistyped arguments and values | 54 |
| EF107 | An undeclared or unmapped failure, including forked child failures left unobserved | 7 |
| EF108 | Missing service requirements | 1 |
| EF109 | Unreachable statement after `fail` | 1 |
| EF110 | Unsupported target (kept in `internal/report`) | 1 |
| EF111 | A declared Go import the generated program cannot import | 8 |
| EF112 | An unsupported Go symbol, method or host type | 6 |
| EF114 | Unknown field | 7 |
| EF115 | A constructor payload field of the wrong type | 5 |
| EF116 | Unknown variant | 9 |
| EF117 | Missing match arm | 6 |
| EF118 | A match arm of the wrong shape | 3 |
| EF119 | Recursive data layout is unsupported | 1 |
| EF120 | `_tag` is reserved for closed variant and error discriminators | 1 |
| EF121 | An alternative binding mismatch in a match arm | 4 |
| EF122 | Constructor arguments mix named and positional forms | 2 |
| EF123 | Ownership fault, reported once per root | 1 |
| EF125 | Row parameter error | 14 |
| EF126 | Unsupported bundled module or bundled member | 11 |
| EF127 | Codec declaration or use error | 46 |
| EF130 | Two bindings provide the same service | 1 |
| EF131 | Two replacements for one service | 3 |
| EF132 | Cyclic layer merge | 2 |
| EF133 | A layer exceeds 1000 selected nodes | 5 |
| EF134 | Layer construction exceeds its `uses` annotation | 2 |
| EF135 | Layer configuration must be pure | 6 |
| EF136 | The application plan exceeds its work bound; no partial plan | 1 |
| EF137 | Match coverage analysis exceeds its bound; no partial plan | 1 |
| EF138 | A `derive` codec that its profile cannot represent | 1 |
| EF139 | Machine `stay` misuse | 1 |
| EF140 | Machine contract: an unknown, polymorphic or missing `step` or `initial` binding | 1 |
| EF141 | Machine admission: entry work or data that machines do not admit yet | 1 |
| EF142 | Machine analysis exceeds its budget; no plan | 1 |
| EF143 | A decision form outside a decision context | 1 |
| EF150 | A refused i64 division: by zero, or without a divisor proven nonzero | 1 |

`internal/edition` declared three more, all retired with editions:

| Code | Meaning |
| --- | --- |
| EF144 | The edition manifest is unsupported or malformed |
| EF145 | The operation is not available for this edition yet |
| EF146 | The source could not be bound to an edition: it changed between open and lookup, or it is not a regular file |

The commands that do not run now print plain CLI messages, not registered diagnostics.

## Deleted examples and packages

Read each with `git show dbf135bf:<path>`.

- The 35 earlier-language examples: `examples/callables-factory.ef`, `callables-service.ef`, `callables-state.ef`, `causal.ef`, `checkout.ef`, `codec-transforms.ef`, `codecs.ef`, `concurrency.ef`, `data.ef`, `generic-settings.ef`, `generic-users.ef`, `host-types.ef`, `http-endpoints.ef`, `http-transport.ef`, `http.ef`, `imports.ef`, `latch-handoff.ef`, `latest-task.ef`, `layers-workflow.ef`, `layers.ef`, `lifecycle.ef`, `machine-breaker.ef`, `machine-search.ef`, `main.ef`, `missing-service.ef`, `ownership.ef`, `pipe.ef`, `quiescence-wakes.ef`, `quiescence.ef`, `recipes-queue.ef`, `recipes-retry.ef`, `recovery-codec.ef`, `recovery-outcome.ef`, `testing.ef`, `workflow.ef`, all under `examples/`.
- `examples/fixture.txt`, the file that `examples/http.ef`, `lifecycle.ef` and `ownership.ef` read.
- `examples/editions/legacy/` (`ergo.json`, `main.ef`, `expected.out`) and `examples/editions/sketch/ergo.json`.
- `examples/sdk/client.go`, `examples/hosttypes/hosttypes.go`, `examples/hosttypes/protocols.go` and `ergo.bindings.json`: the Go packages and binding contracts that the examples imported.
- `examples/compare/checkout.ts`, `examples/compare/go/main.go`: the checkout example written in TypeScript with Effect and in Go, for comparison.
- `examples/lintpack/`: the example rule pack, its `policy-lint` command and its two `.ef` test inputs.
- Packages: `internal/compiler` (with `internal/compiler/machineplan`), `internal/gosyntax`, `internal/edition`, `internal/lsp`, `internal/mcp`, `internal/lintpacks`, `lint`.
- Tools: `scripts/migratemeasure`, `scripts/residual`, `scripts/ownership_sweep.py`, `scripts/ownership_truth.py`, `conformance/size/matrix` and its fixtures `conformance/size/fixtures/`.

v1 examples are written in the decided syntax by the units that can compile them. `examples/editions/sketch/main.ef` and `docs/research/ef-migrate/*.ef` are the sources the front end checks today.

## Conformance cases without evidence

The evidence tests of these 17 cases of `conformance/effect-cases.json` were in `internal/compiler`. Each case is now `pending` with no evidence, and keeps its upstream provenance and its earlier limits:

- `covered`: `effect.scope.child-interrupted-on-close`, `effect.scope.failure-and-cleanup-cause`, `effect.test-clock.admitted-fork`, `effect.schema.string`, `effect.schema.boolean`, `effect.schema.void`, `effect.schema.bigint-from-string`, `effect.schema.excess-property-ignore`, `effect.schema.json-string-struct`, `effect.layer.parallel-acquisition`, `effect.layer.interrupted-shared-build-preserves-failure`, `effect.layer.interruption-releases-acquired`, `effect.layer.finalizers-dependent-first`.
- `difference`: `effect.timeout.typed-error-policy`, `effect.schema.json-string-admission`, `effect.layer.interrupted-requester-shared-build`, `effect.layer.memoizes-across-builds`.

## What #203 must restore

[#203](https://github.com/cevr/ergo/issues/203) ports the tooling to the new front end. `ergo mcp` and `ergo lsp` stay dark until then, and the removed commands print where they return.

- MCP (`ergo mcp [ROOT]`, stdio, protocol 2025-11-25, read-only): the tools `project.describe`, `project.check`, `project.diagnostics`, `code.format` (a file or a buffer, with `expectedDigest` and `expectedProducer`), `code.inspect`, `code.explain`, `code.type`, `code.typeAt`, `project.graph` (the views and renderings shared with `ergo graph`), `project.tests`, `project.lint` and `lint.rules`. `project.edition` does not return, because there are no editions.
- LSP (`ergo lsp`, Content-Length JSON-RPC on stdio, full-document sync): `initialize`, `initialized`, `shutdown`, `textDocument/didOpen`, `didChange`, `didClose`, `publishDiagnostics` with UTF-16 ranges, `textDocument/formatting`, `textDocument/hover`, `textDocument/definition` and `ergo/lintStatus`. `ergo/edition` does not return.
- CLI: `inspect`, `explain`, `query`, `type`, `graph` (with its view options), `lint` with rule packs (`--lint-config`, `--rules`, `lint rules`, `lint test`, `lint receipt`) and the lint-pack SDK (`lint/`), and `build --receipt` (application receipts; v1 item 5 measures build time separately).
- The rule to keep: the CLI and MCP project one snapshot and return identical facts, and inspection never executes behaviour.
- The removed implementation: `git show dbf135bf:<path>` for `internal/lsp`, `internal/mcp`, `internal/lintpacks`, `lint`, `internal/compiler/graph_view.go` and `internal/compiler/lint*.go`.
