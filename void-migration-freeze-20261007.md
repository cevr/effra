# Effra canonical `void` migration freeze

Date: 2026-10-07

Workspace: `/home/exedev/Developer/personal/.rifts/effra/foundations-resume-2026-10-06`

## Frozen scope

The migration started at `b48cc701176ec50bb96ac21a1ec15f19047c254a`, tree `8349a77f3c2b5d1c3f6105338842d86ffa9c93b7`. That graph repair is preserved. The final graph review artifact is `graph-builtin-final-r2-astra.md`, SHA-256 `03c4e5ff3b2e127d217593af208749def2da168eaa41197cb3341a80425bdbbf`; it accepted the immutable starting commit/tree with no findings and terminal exit 0. No graph follow-up was performed during this migration.

The four authorized migration units are four separate commits, each preceded by a locked `./scripts/gate.sh` run with numeric exit 0:

| Unit | Commit | Tree | Scope |
| --- | --- | --- | --- |
| 1 | `45ae075d6dd956a06aeb47ec64804539457f170d` | `ee6215aeb1d6c97252fcc18de09c6cde24cd00fe` | Canonical parser/semantic `void`, corpus, diagnostics, versions, JS parity and immediate docs |
| 2 | `ad5122bcbcbc652e2014ee07e4960fa2983a6c07` | `0a3d3f7c1f39cf8d06822186135f927d00dcf526` | Concrete Go no-result lowering, value carriers and typed generic callable bridges |
| 3 | `939abed3c1cc27a6c0c01c3c90040f3f41f9a5c2` | `1e7e83dab8569a3f0d5dfb733d74e7cd1bda3d19` | CLI/MCP/LSP public-process and Go/JS/test-runner regressions |
| 4 | `57844f5946d253746b097567271d54ae475f8465` | `9559ee3f78c7bff4bc14123d3b88140e9e891a52` | Remaining current documentation and schema/terminology alignment |

The migration delta from the starting commit to unit 4 is 86 tracked paths, 1,334 insertions and 460 deletions; SHA-256 of `git diff --binary` is `9b4e992edf9c5649e661501733e98d348dc9e11a7c2f1a083e189746b8d2e038`.

## Raw gate manifest

All receipts retain command, environment, pre/post SHA and tree, tracked diff hash, raw/status output, `GATE_EXIT`, `DIFF_CHECK_EXIT` and `TEE_SOURCE_EXIT`. They use `/tmp/effra-foundations-validation.lock`, the pinned cache/TMPDIR, `GOPROXY=off`, `PYTHONDONTWRITEBYTECODE=1`, and the architecture-loop tool-shims path.

| Receipt | Pre SHA/tree | Gate | Tracked diff SHA-256 | Receipt SHA-256 |
| --- | --- | --- | --- | --- |
| `void-gate-unit1-success2-20261007.log` | `b48cc701` / `8349a77f` | 0 | `ef1727ed68acbca3abfa523f08537b889cfabab5dd8c01df3e3e6716a3ebd660` | `67f1f68d7898ac767a3dcfb4521461e6e6df01ef148461379c5e31ba9ae69e03` |
| `void-gate-unit2-20261007.log` | `45ae075d` / `ee6215ae` | 0 | `346ba011d86d6c3aa909d9354f30e2b1ba7974125164fd7224f242036ec47c8b` | `f121e19350a21a59846ac3feabf16ed99599193bbfee1598c484221e3a972697` |
| `void-gate-unit3-20261007.log` | `ad5122bc` / `0a3d3f7c` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` | `f6409427c3848ecf6b9f8a4a7f088f017c07ea8a8e676e3fcd9c503828b5fd` |
| `void-gate-unit4-20261007.log` | `939abed3` / `1e7e83da` | 0 | `fad8ab1fa154ea5aad90f5fd296b9a353457a2f824cd20f75225eae5196bd5da` | `537e609371ab8cdbf7fff7cf1756c1473f8cde359b013cc3fb77c0f2baa97ee9` |

The final post-report gate is retained separately as `void-gate-final-20261007.log`; its exact post-report SHA/tree and receipt hash are the authoritative final handoff values. This report is itself freeze metadata, so its own commit identity is recorded by that receipt rather than self-embedded.

## Supported contracts

- `void` is the sole canonical no-value type and explicit literal. Empty parameter and call lists remain `()`. Old type/literal spellings produce focused EF002 diagnostics; `void()` reaches ordinary callable checking. `unit` remains neither an alias nor a source no-value type.
- Empty blocks and let-final blocks complete with `void`; `never`, absence, `nil`, `null`, and `Option<void>` remain distinct. `Some { value: void }` and `None` retain their tags.
- Assignability, callable variance, failure/service rows, ownership, absence, match exhaustiveness, graph facts and projection references remain source-semantic contracts. Builtin and authored service registration retain uniform owner/identity assignment and checked call edges.
- Semantic schema is 7, producer checker ABI is 8, and formatter identity is 5. Interface schema 3, ownership schema 3, bundled interface version 1, diagnostic schema 1, query schema 1, generated-module schema 1, and protocol versions remain unchanged.
- Go pure concrete `void` functions and callable parameters have no result; value positions and generic declared `T` layouts retain `struct{}` carriers. Effectful `void` remains `efEffect[struct{}]`. Generic callable bridges are typed, lazy, single-capture adapters and do not eagerly invoke callbacks.
- Existing Go runtime `Unit`, host Go `func()`, JavaScript host syntax and JS `undefined` remain implementation representations. Error-only Go imports preserve managed error/partial-result behavior.
- CLI, MCP and LSP expose the canonical contracts through actual processes. LSP old-spelling ranges preserve UTF-16 coordinates and clear on a later valid document version. Go and JS void entrypoints are silent; `ef test` discovers and executes void tests on both targets.

## Scope audit and unresolved work

The final scoped audit found no current authored `.ef` or documentation spelling of `-> ()`, `Option<()>`, `value:()` or a unit no-value contract outside the allowlist below:

- deliberate old-syntax negatives in `internal/compiler/void_test.go` and `cmd/ef/void_process_test.go`;
- the historical measured quotation in `docs/research/native-runtime-retention.md`, intentionally untouched;
- host-language empty-call syntax and implementation carriers such as Go `func()`, `struct{}{}`, JS `() =>`/`undefined`, plus the targeted `unit` unknown-type hint.

Remaining language limits are pre-existing and intentionally not broadened: user generic functions, anonymous closures, general open-row differences, recursive layouts, and unsupported host/target capabilities remain diagnostics. No runtime synchronization algorithm, accepted ownership/row/graph fact, upstream source, historical receipt, benchmark receipt, install, push or publication was changed. HITL/Wayfinder decisions remain open.

The final tracked status is clean apart from the pre-existing untracked `bin`, `dist`, and `node_modules` entries, which were preserved.
