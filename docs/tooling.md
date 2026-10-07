# Native developer tooling

The compiler's checked model supplies CLI and MCP answers. These are default capabilities, with no second analyzer or backend execution needed for lint/queries. Each invocation currently checks one file; Go imports may load host export data. There is no persistent workspace cache.

| CLI | MCP | Purpose |
| --- | --- | --- |
| `ef check FILE` | `project.check` | Non-disableable source admission diagnostics |
| `ef diagnostics FILE [--strict] [--json]` | `project.diagnostics` | One shared compiler/lint report with byte spans and UTF-16 ranges |
| `ef lint FILE [--strict]` | `project.lint` | Checked semantic advice; separate `checked` and `lintPassed` |
| `ef lint rules` | `lint.rules` | Stable codes, severity, names and rationale |
| `ef inspect FILE SYMBOL` | `code.inspect` | Declared/body contracts or nominal record, enum and error metadata |
| `ef explain FILE SYMBOL` | `code.explain` | Local contract contributions |
| `ef query FILE BYTE_OFFSET` | `code.typeAt` | Expression kind, type, and executed failure/requirement rows |
| `ef type FILE --symbol NAME / --offset BYTE / --definition ID` | `code.type` | Selected declaration, lexical binding/use, expression, or canonical definition |
| `ef graph FILE` | `project.graph` | Dependencies, providers, calls and provision boundaries |
| `ef test FILE` | `project.tests` discovers cases | CLI executes; MCP remains read-only |
| `ef fmt FILE... [--check] [--json]` / `ef fmt --stdin` | `code.format` | Canonical syntax-only formatting; CLI writes atomically, MCP returns a full-text preview and never writes |

File commands accept `--target go|js`, defaulting to Go. Semantic results identify the source/import revision and the executing compiler producer. MCP semantic file tools accept independent `expectedRevision` and `expectedProducer` guards; omitting `expectedProducer` requests fresh facts for that invocation and makes no cross-producer reuse claim. `code.format` accepts `expectedProducer`, while `expectedDigest` separately guards exact source bytes.

`ef type` requires exactly one selector. `--definition` also requires `--revision`; MCP uses `definition` with `expectedRevision`. Offset queries use original syntax extents and checked lexical bindings. Selected imported declarations can have `locationAvailable: false`; their original source extent is not inferred from the current file. A selected name also reports the declaration it denotes as `target`, with its own `locationAvailable`. Results carry the producer-qualified snapshot tuple; MCP `expectedProducer` independently guards that producer while `expectedRevision` guards source/import facts. Omitting `expectedProducer` requests fresh facts.

Formatter adapters are the exception to semantic target and revision flags: `ef fmt` is syntax-only, and MCP `code.format` accepts `expectedDigest` for exact source bytes but no `target` or `expectedRevision`.

## Formatter limits and filesystem policy

Both formatter adapters apply these finite budgets before producing or replacing a document:

| Boundary | Limit |
| --- | ---: |
| CLI file count | 100 files |
| CLI input per file | 2 MiB |
| CLI input per request | 8 MiB |
| CLI formatted output per file | 4 MiB |
| CLI formatted output per request | 16 MiB |
| MCP `code.format` source | 2 MiB |
| MCP `code.format` formatted output | 4 MiB |
| MCP newline frame before its terminal LF | 16 MiB |

The MCP frame budget excludes the terminal LF; the CR in a CRLF delimiter counts as frame content. Exact-limit frames are admitted. An oversized line is drained through its next LF without retaining the over-limit contents, returns a JSON-RPC parse error with a null ID, and leaves following requests available. A malformed partial line at EOF returns one parse error and then closes; a valid final request without LF is processed once. No peer deadline or hostile transport guarantee is implied.

`ef fmt --stdin` writes only formatted source to stdout. Human file statuses and operational errors go to stderr. File `--json` and `--check --json` reports use stdout and keep stderr empty; stdin JSON is an invocation error. Exit 0 means formatting completed (or check found no differences), exit 1 means check differences, and exit 2 means invocation, syntax, I/O, stale-source, or limit failure. Completed file entries include `path`, `requestedPaths`, digests, `changed`, `written`, and `completed`; failed entries never receive a successful human status. The stable operational codes are:

| Code | Meaning |
| --- | --- |
| `EFMT_INVOCATION` | incompatible options, missing input, or file-count admission failure |
| `EFMT_PATH` | empty, non-`.ef`, or otherwise invalid path |
| `EFMT_READ` | source could not be admitted or read |
| `EFMT_SYMLINK` | mutating request named a symlink leaf |
| `EFMT_SPECIAL_FILE` | directory, FIFO, or other non-regular source |
| `EFMT_INPUT_LIMIT` | per-file or aggregate input budget exceeded |
| `EFMT_OUTPUT_LIMIT` | per-file or aggregate formatted-output budget exceeded |
| `EFMT_SYNTAX` | unsupported or invalid syntax, including invalid UTF-8 |
| `EFMT_ALIAS` | multiple requested names resolve to one underlying inode |
| `EFMT_STALE` | source identity or exact bytes changed before replacement |
| `EFMT_WRITE` | temporary-file, permission, rename, or other replacement failure |

Mutating paths retain the requested display spelling while the operating system resolves ancestor symlinks and `..` components. The leaf must remain regular and non-symlink at each replacement check. Lexical spellings that resolve to the same intended path are deduplicated; distinct hardlink or inode aliases in one request are rejected. A single requested hardlink is replaced through an atomic same-directory rename, so that selected directory entry splits from unlisted hardlinks; this is documented behavior. A read-only regular file may be replaced when its containing directory is writable, and its permission, setuid, setgid, and sticky bits are preserved. Directories, FIFOs, and other special nodes are rejected before a potentially blocking read. These cooperative checks do not claim hostile-filesystem race protection or directory-fsync durability.

Formatter source must be valid UTF-8. Invalid bytes produce the shared lexical `EF001` span before formatting, with no replacement text or file write. Valid U+FFFD characters and escaped Unicode spellings remain valid source. MCP `source` strings are decoded JSON Unicode text; their `inputDigest` is the SHA-256 digest of those UTF-8 bytes.

MCP `code.format` returns the complete formatted text in `structuredContent` and a short content summary. The adapter buffers the encoded response before writing it and enforces the same 16 MiB frame cap; an encoded response that would exceed it becomes a bounded tool error with no partial replacement text. Bounded formatting responses preserve validated scalar request IDs (string, number, or null) without re-encoding their raw representation; other response fields use bounded JSON encoding. If a successful or tool-error body cannot fit, the adapter emits a compact `-32000` JSON-RPC error with the original request ID; if an argument/protocol error cannot fit, it keeps that error code with the same compact message. Correlation is preserved and no ID is truncated. Semantic responses retain their existing encoding policy.

## Diagnostic reports

`ef diagnostics` and `project.diagnostics` use the same compiler-owned report. It identifies the source with a canonical escaped `file:` URI and an `origin` such as `disk`, includes the exact semantic `revision`, selected `target`, `checked` admission state, `strict` policy, `policyPassed`, and deterministic findings. Every finding preserves its UTF-8 byte `span` and reports `code`, `origin`, optional lint `rule`, stable `severity` (`error`, `warning`, `information`, or `hint`), message, and an optional plain-text `help` naming what to write instead. A finding with a source location also has a valid zero-based UTF-16 `lsp.range`; its `lsp.message` appends any help as a `help:` line, because the message is the one field every LSP client renders, so CLI, MCP and LSP carry the same text; `locationAvailable` is false when the compiler has no source location, such as an unsupported target diagnostic.

Compiler errors always fail policy. Strict mode changes only the policy decision for warnings; it does not change finding severity. Unchecked source has `lintAvailable: false` and an explicit `lintUnavailableReason`, while compiler findings remain available. Reasoned lint suppressions can remove optional advice but cannot hide compiler errors.

Source identity is the escaped, absolute, lexically normalized requested document path, retained with the analyzed bytes. Symlink targets are not substituted into that URI, and imports use the requested document's directory. A changed disk file does not change the returned snapshot's revision. This is document identity, not a physical-inode or hostile-filesystem guarantee. Linux paths and behavior are tested; native Windows drive/UNC behavior has not been runtime-validated.

Diagnostic text uses one-based UTF-16 line/column locations. JSON LSP ranges are zero-based UTF-16; original compiler spans remain UTF-8 bytes. Source supports LF and CRLF line endings. Standalone raw CR outside string literals receives EF001 with an LF/CRLF correction; a raw CR inside a string receives the JSON-escape diagnostic. `\r` inside an escaped string remains valid content. Editor-position conversion recognizes CR as a line boundary even when locating that unsupported source byte.

For unchecked source, the initial diagnostic report omits all lint evaluation, including suppression validation; its unavailable reason is explicit. The legacy `ef lint` surface retains its own suppression-validation behavior. This does not allow any suppression to remove compiler diagnostics.

The CLI prints one finding per line by default and emits the complete report with `--json`. A policy failure exits 1, an invalid invocation exits 2, and file or compiler operation failures remain operational errors. MCP returns source errors as successful structured reports with `policyPassed: false`; source admission, stale-revision, path, and output-limit failures remain tool errors. MCP accepts at most 100 diagnostic findings and reports an explicit limit error above that boundary. The report's `totalCounts` and `returnedCount` are exact for every successful response.

Exit 0 means policy passed. Exit 1 with JSON report stdout means source policy failed; exit 1 without a report means an operational failure, explained on stderr. `ef diagnostics --help` also documents this distinction and exit 2 for invalid invocation.

### Diagnostic codes and absent constructs

Codes are stable and grouped by the seam that owns them: `EF001` lexical errors, `EF002` parse errors, `EF003` constructs Effra does not have, `EF1xx` checker errors and `EFLxxx` lint advice. A code names a family, not each spelling; the message and help carry the specifics.

`EF003` replaces the accidental error a familiar TypeScript, Go or Effect construct used to produce (`unknown value null`, `unsupported character '?'`, `expected expression`). It is reported where that spelling already failed (an unresolved name or type, a refused character, a refused statement form), with the span of the keyword or operator token alone. It is a diagnostic only: no program that was refused is admitted, admitted programs and the formatter are unchanged, and identifiers such as `nullable`, `asValue` or a local bound as `null` keep their meaning. A parse failure inside the braced body of `try`, `catch`, `for` or `while` reports that construct at its keyword. A construct the specifications plan says "not yet supported"; one they never plan says "Effra has no".

| Construct | Spellings | Status | Help names | Basis |
| --- | --- | --- | --- | --- |
| null | `null`, `nil`, `undefined` | absent | `Data.Option<T>` | [NORTH_STAR](../NORTH_STAR.md) owner data constraint; [absence](specs/absence-and-host-boundaries.md) |
| throw | `throw` | absent | `fail E` with `raises { E }`, `.catch<E>` | [typed failures](design.md#failure-is-more-than-a-result) |
| try/catch | `try`, `catch` | absent | `.catch<E>(fallback)`, `scope { ... }` | [typed failures](design.md#failure-is-more-than-a-result) |
| type assertion | `as` | absent | explicit conversion, `Convert.Codec` | [checked codecs](specs/bundled-interfaces.md) |
| top type | `unknown`, `any` | absent | concrete type or closed enum with `match` | [host interop](../NORTH_STAR.md) forbids an unchecked `any` |
| async | `async`, `await` | absent | `effect fn` with `run` | [surface language](design.md#surface-language) |
| module binding | module-level `let` | absent | a service a layer provides; a function for a fixed value | [layers](specs/layers.md) |
| dynamic import | `import(...)` | absent | top-level `import` declaration | [interop](interop.md) |
| conditional operator | `?` | absent | `if c { a } else { b }`; `match` on Data.Result | `if` is an expression; Result-propagation `?` is planned ([design](design.md#surface-language)) and also not yet supported |
| negation, inequality, logical and | `!`, `!=`, `&&` | absent | `if`/`else` forms | the specifications are silent, so absent |
| `if` without `else` | `if c { ... }` | absent | `else { void }` | [prototype](prototype.md#supported-surface) admits two-branch `if` |
| closure | `fn(...) -> T { ... }`, `effect fn(...)` | planned | a named module function passed by name | [language abstractions](specs/language-abstractions.md) admits closures after capture checking |
| loop | `for`, `while` | planned | a recursive named function | [design](design.md#concurrency-and-resources) loop backedges; [actors](specs/actors.md) |
| assignment | `x = value` after a statement | planned | a new `let` name | [surface language](design.md#surface-language) plans local mutation |

Each help is cross-checked by a checked replacement program in `cmd/ef/absent_syntax_process_test.go`. The `unit` type and `throws` row keyword keep their own retired-spelling messages under `EF102` and `EF002`.

## Lint

`EFL001 unused-recipe` warns when a local lazy effect binding is never referenced. Uses inside branches and nested scopes count; bindings in separate functions remain distinct. `let _ = recipe()` acknowledges intentional omission. No deletion fix is offered: constructing arguments may itself execute nested effects.

`EFL002 redundant-provision` suggests reviewing a boundary whose receiver has no requirement for that service. Stable boundaries may be intentional. `EFL003 unused-go-import` reports an import whose functions are never referenced. The import still initializes its Go package (see [binary reachability](specs/binary-reachability.md)), so the advice is to remove it only when that initialization is unneeded; deleting it is not behavior-preserving in general.

Warnings do not make checked source untyped. `--strict` makes warnings fail the lint command; suggestions remain non-failing. Compiler errors always fail, and optional advice is skipped on unchecked source. A named next-line suppression can acknowledge one optional rule on the following physical source line:

```text
// effra-lint-disable-next-line unused-recipe -- intentionally deferred hook
let forgotten = task()
```

Only a `//` line comment is a directive; a block comment containing the same text is ordinary comment text and never suppresses or diagnoses. The rule name must be known and the reason must be non-empty. Malformed, unknown, or unused suppressions are `EFL004 invalid-suppression` errors and fail lint in every mode. Suppressions are matched against semantic diagnostic spans in the same source revision; compiler correctness diagnostics cannot be suppressed. Comments remain source text, and no automatic deletion fix is offered.

## Local types

Offsets and spans are UTF-8 bytes. `ef query`/`code.typeAt` continue to address diagnostic anchors (for example, the `run` keyword, a call name, or `provide`). The separate `ef type`/`code.type` selector accepts a declaration symbol, an original-byte offset, or a canonical type definition ID tied to its source revision. Offset selection uses retained syntax extents and observed checker bindings, including shorthand field names and Fiber operation receivers; unavailable original locations are reported rather than inferred. Unchecked source, stale revisions/producers and exhausted projection budgets are explicit refusals. Effect types distinguish deferred rows from rows executed while evaluating the expression.

Checked values carry canonical `type` and complete `contract` references. Nominal records, enums and errors retain declaration identity; `ef inspect FILE TYPE_NAME` returns source fields or variants with UTF-8 spans. Successful schema 7 inspection/query/graph responses and selected `code.type` responses include response-local reachable `types`, `rows` and nominal declarations. The selected-type response also carries `querySchemaVersion: 2`, the selected name's declaration `target` and a bounded plaintext `presentation` shared with LSP hover; its canonical definition selector requires the matching source/import revision. Shared children use references; absent row IDs mean empty rows. Selected roots exclude unrelated declarations and callable metadata. A snapshot is qualified by schema version, source/import revision, target, producer qualifier and reuse scope; source revision alone does not identify the compiler that checked it. Application IDs identify callee/source sites separately from structural contracts.

The complete private arena controls checking and emission. Public projection separately admits at most 4,096 nodes, 8,192 canonical edges, 4,096 row labels, 256 KiB of strings/names/displays, 512 KiB of compatibility metadata and 1 MiB of encoded response bytes. Admission measures compatibility metadata before expanding it and charges traversal before building public tables. Function publication shares one cumulative compatibility budget; after refusal, later functions keep compact references, and selected inspection independently projects from retained checked roots. Ownership summary passes retain compact views too. Selected operations reject unchecked source or oversized projection with an explicit error and no partial facts. Whole-source check can remain `checked: true` with `typeProjectionComplete: false` and `typeProjectionError`; its refused envelope omits unresolved metadata while retaining counts and admission diagnostics. Both Go and JS builds remain available. MCP charges its escaped text plus structured result before encoding. See [MCP limits](mcp.md) for refusal and diagnostic-envelope details.

Checked values also expose bounded `ownership` and `captures` facts. `owned` facts name the closing scope or child that the compiler can prove owns a `File`/`Fiber`; `borrowed` facts retain the parameter origin; `unknown` facts keep foreign and custom provider behavior honest and can also represent bounded analysis that did not establish a complete proof. `EF123` is a compiler correctness diagnostic for a proven inner-scope or child-owned escape, or for potential ownership that exhausts the bounded analysis before the value can be proved safe; those cases retain distinct diagnostics. The same facts are present in `ef inspect`, `ef query`, `ef type`, `project.check`, `code.inspect`, `code.typeAt` and `code.type`; this is provenance evidence, not a complete borrow checker.

```sh
OFFSET=$(python3 -c 'from pathlib import Path; s=Path("examples/latest-task.ef").read_bytes(); print(s.index(b"run previous"))')
ef query examples/latest-task.ef "$OFFSET"
```

## Dependencies and dependents

Graph nodes represent functions, nominal services, providers, provider methods, provider recipes, materialized provider values and lexical effect expressions. Edges include `requires`, `implements`, `calls`, `contains`, `adapts`, `materializes`, `originates` and `provides`. Incoming edges identify dependents. Canonical contracts appear on function/expression nodes.

For `examples/workflow.ef`, `welcome` requires `Directory`; `DemoDirectory` implements it; a provision expression adapts the receiver and supplies that provider. The receiver keeps its original contract while the provision node shows the remaining requirements. Explicit configured or dependency-capturing provider constructors appear as lazy recipes; each `run` materializes a provider value, while aliases of a materialized value retain its identity.

This is a single-file static composition graph, including deferred calls. It does not establish execution order or runtime allocations. Provider construction is explicit and non-memoized: a reused value is one graph identity, while repeated runs of one recipe are distinct values. Fallible acquisition, lifecycle-safe arbitrary capture, general sharing keys and cycle paths will extend this model when implemented. Expression IDs contain offsets and are scoped to the revision.

CLI and MCP limit graphs to 1,000 nodes and 2,000 edges; larger graphs fail explicitly. The legacy `project.lint` response keeps its separate bounded diagnostic arrays and truncation flags. Full CLI diagnostic reports remain available.

## Next capabilities

Revision-bound checked edit plans, multi-file identities, editor integration, complete ownership provenance and runtime/source correlation remain planned. The bounded ownership evidence described above is implemented without claiming a complete borrow checker. Canonical comment-preserving formatting is implemented by the compiler core and exposed through `ef fmt` and MCP `code.format`; the maintained authored-example selection is checked by the gate, and `ef lsp` answers textDocument/formatting with the same formatter over versioned buffers ([profile](lsp.md#document-formatting)). Compiler errors stay independent of optional style policy. Managed test time already shares scheduling with sleep/deadline primitives; see the [testing contract](testing.md) for its supported causal boundaries and foreign-operation limits.

## Gate prerequisites

`scripts/gate.sh` needs the pinned Effect reference tests, which live in a git submodule. After every clone, Rift or worktree run `scripts/init_upstream.sh` once; offline, set `EFFRA_UPSTREAM_MIRROR` to a local Effect clone that contains the pinned commit. The conformance verifier reads the corpus only from git objects ([conformance](conformance.md)).

The README's TypeScript comparison program is checked with `tsc --strict`. The repository has no TypeScript dependency, so the gate environment supplies `tsc` on PATH. Without it the gate prints a loud `typescript: unchecked` line and the README smoke reports `typescript: unchecked: no tsc on PATH`; it does not fail, so contributors without `tsc` can still gate, but a gate receipt without `typescript: strict` is weaker evidence.

## Performance receipt

On 2026-10-05, Apple M4 Pro / macOS 27.0.1 / Go 1.27.1 arm64, three one-second warm runs of `BenchmarkCompile10KLines` measured 3.13–3.30 ms per parse/check for the synthetic 2,000-function, 10,000-line fixture (about 14.05 MB and 16,192 allocations). The preceding baseline measured 2.95–3.01 ms; this small measurement is not a controlled attribution of the difference or an end-to-end build ratio. Keep watching it as the checker grows.

`BenchmarkLintAndGraph10KLines` runs lint plus graph on an already checked fixture: 0.61–0.62 ms, about 1.16 MB / 6,132 allocations. It excludes parsing and import loading. Reproduce with `go test ./internal/compiler -run '^$' -bench 'Benchmark(Compile|LintAndGraph)10KLines' -benchmem -benchtime=1s -count=3`. Real import caching, cold builds, incremental edits and linking need separate fixtures.
