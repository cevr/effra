# Native developer tooling

The compiler's checked model supplies CLI and MCP answers. These are default capabilities, with no second analyzer or backend execution needed for lint/queries. Each invocation currently checks one file; Go imports may load host export data. There is no persistent workspace cache.

| CLI | MCP | Purpose |
| --- | --- | --- |
| `ef check FILE` | `project.check` | Non-disableable source admission diagnostics |
| `ef diagnostics FILE [--strict] [--json]` | `project.diagnostics` | One shared compiler/lint report with byte spans and UTF-16 ranges |
| `ef lint FILE [--strict] [--lint-config FILE] [--rules MANIFEST]...` | `project.lint` | Checked semantic advice merged with selected rule packs; separate `checked`, `lintPassed` and `complete` |
| `ef lint rules [--lint-config FILE] [--rules MANIFEST]...` | `lint.rules` | Every registered rule with its identity, default and effective severity, requirements and options; never runs a pack |
| `ef lint test PATH... [--update] [--lint-config FILE] [--rules MANIFEST]...` | — | Rule-pack source fixtures: `name.ef` against `name.lint.json` through the production pack path |
| `ef inspect FILE SYMBOL` | `code.inspect` | Declared/body contracts or nominal record, enum and error metadata |
| `ef explain FILE SYMBOL` | `code.explain` | Local contract contributions |
| `ef query FILE BYTE_OFFSET` | `code.typeAt` | Expression kind, type, and executed failure/requirement rows |
| `ef type FILE --symbol NAME / --offset BYTE / --definition ID` | `code.type` | Selected declaration, lexical binding/use, expression, or canonical definition |
| `ef graph FILE` | `project.graph` | Dependencies, providers, calls and provision boundaries; with view options, one selected [graph view](#graph-views) |
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

For unchecked source, the diagnostic report omits all lint findings, including malformed or unknown suppression diagnostics; its unavailable reason is explicit. Its `suppressions` still list every well-formed directive as `not-evaluated` with reason `unchecked-source`. The `ef lint` surface keeps reporting malformed and unknown directives on unchecked source. Like the diagnostic report, it never reports a directive unused there, because no rule ran. This does not allow any suppression to remove compiler diagnostics.

The CLI prints one finding per line by default and emits the complete report with `--json`. A policy failure exits 1, an invalid invocation exits 2, and file or compiler operation failures remain operational errors. MCP returns source errors as successful structured reports with `policyPassed: false`; source admission, stale-revision, path, and output-limit failures remain tool errors. MCP accepts at most 100 diagnostic findings and reports an explicit limit error above that boundary. The report's `totalCounts` and `returnedCount` are exact for every successful response.

Exit 0 means policy passed. Exit 1 with JSON report stdout means source policy failed; exit 1 without a report means an operational failure, explained on stderr. `ef diagnostics --help` also documents this distinction and exit 2 for invalid invocation.

### Diagnostic codes and absent constructs

Codes are stable and grouped by the seam that owns them: `EF001` lexical errors, `EF002` parse errors, `EF003` constructs Effra does not have, `EF1xx` checker errors and `EFLxxx` lint advice. A code names a family, not each spelling; the message and help carry the specifics.

`EF003` replaces the accidental error a familiar TypeScript, Go or Effect construct used to produce (`unknown value null`, `unsupported character '?'`, `expected expression`). It is reported where that spelling already failed (an unresolved name or type, a refused character, a refused statement form), with the span of the keyword or operator token alone. It is a diagnostic only: no program that was refused is admitted, admitted programs and the formatter are unchanged, and identifiers such as `nullable`, `asValue` or a local bound as `null` keep their meaning. A statement shaped like a `try` or `catch` block or a `for` or `while` loop reports a parse failure inside it as that construct at its keyword. The shape is the keyword, a header and a braced body. A loop header is the tokens before the first `{` outside parentheses, and must be one parenthesized group, `x in xs` or `k, v := range m` before a condition, Go's `init; cond; post`, or a condition that parses as one expression; so `while match x { ... } { ... }`, whose condition holds braces outside parentheses, keeps its own diagnostic. The shape is read from tokens alone: a name bound as `while`, `for` or `try` and followed by a braced record literal also has it, and a parse failure inside that literal reports the construct. A parse failure outside the shape, including after a bound name used any other way, keeps its own diagnostic. A construct the specifications plan says "not yet supported"; one they never plan says "Effra has no".

| Construct | Spellings | Status | Help names | Basis |
| --- | --- | --- | --- | --- |
| null | `null`, `nil`, `undefined` | absent | `Data.Option<T>` | [NORTH_STAR](../NORTH_STAR.md) owner data constraint; [absence](specs/absence-and-host-boundaries.md) |
| throw | `throw` | absent | `fail E` with `raises { E }`, `.catch<E>` | [typed failures](design.md#failure-is-more-than-a-result) |
| try/catch | `try`, `catch` | absent | `.catch<E>(fallback)`, `scope { ... }` | [typed failures](design.md#failure-is-more-than-a-result) |
| type assertion | `as` | absent | explicit conversion, `Convert.Codec` | [checked codecs](specs/bundled-interfaces.md) |
| top type | `unknown`, `any` | absent | concrete type or closed enum with `match` | [host interop](../NORTH_STAR.md) forbids an unchecked `any` |
| async | `async`, `await` | absent | `effect fn` with `run` | [surface language](design.md#surface-language) |
| module binding | `let` at module level | planned | a zero-argument function for a fixed value; a service a layer provides for shared state | [surface language](design.md#surface-language) shows a module-level `let`; [layers](specs/layers.md) own shared state |
| dynamic import | `import` called as a function | absent | top-level `import` declaration | [interop](interop.md) |
| question operator | `?` | planned | `if c { a } else { b }` for a conditional; `match` on Data.Result until propagation lands | [surface language](design.md#surface-language) keeps `?` for Result propagation; a conditional is an `if` expression |
| negation | `!` | absent | `if b { false } else { true }` | the specifications are silent, so absent |
| inequality | `!=` | absent | `==` with the branches swapped | the specifications are silent, so absent |
| logical and | `&&` | absent | `if a { b } else { false }` | the specifications are silent, so absent |
| `if` without `else` | `if` with one branch | absent | `else { void }` | [prototype](prototype.md#supported-surface) admits two-branch `if` |
| closure | `fn`, `effect` opening an anonymous function | planned | a named module function passed by name | [language abstractions](specs/language-abstractions.md) admits closures after capture checking |
| loop | `for`, `while` | planned | a recursive named function | [design](design.md#concurrency-and-resources) loop backedges; [actors](specs/actors.md) |
| assignment | `=` after a statement | planned | a new `let` name | [surface language](design.md#surface-language) plans local mutation |

The table publishes the compiler's catalog: a test fails when a row's construct, backticked spellings or status drifts from it. Each help is cross-checked by a checked replacement program in `cmd/ef/absent_syntax_process_test.go`. The `unit` type and `throws` row keyword keep their own retired-spelling messages under `EF102` and `EF002`.

## Lint

`EFL001 unused-recipe` warns when a local lazy effect binding is never referenced. Uses inside branches and nested scopes count; bindings in separate functions remain distinct. `let _ = recipe()` acknowledges intentional omission. No deletion fix is offered: constructing arguments may itself execute nested effects.

`EFL002 redundant-provision` suggests reviewing a boundary whose receiver has no requirement for that service. Stable boundaries may be intentional. `EFL003 unused-go-import` reports an import whose functions are never referenced. The import still initializes its Go package (see [binary reachability](specs/binary-reachability.md)), so the advice is to remove it only when that initialization is unneeded; deleting it is not behavior-preserving in general.

Warnings do not make checked source untyped. `--strict` makes warnings fail the lint command; suggestions remain non-failing. Compiler errors always fail, and optional advice is skipped on unchecked source. A named next-line suppression can acknowledge one optional rule on the following physical source line:

```text
// effra-lint-disable-next-line unused-recipe -- intentionally deferred hook
let forgotten = task()
```

Only a `//` line comment is a directive; a block comment containing the same text is ordinary comment text and never suppresses or diagnoses. The rule name must be known and the reason must be non-empty. Malformed, unknown, or unused suppressions are `EFL004 invalid-suppression` errors and fail lint in every mode. Suppressions are matched against semantic diagnostic spans in the same source revision; compiler correctness diagnostics cannot be suppressed. Comments remain source text, and no automatic deletion fix is offered.

A directive can also name a rule-pack rule as `namespace/rule`. A malformed qualified name, an unknown rule of a selected pack and a reserved namespace are `EFL004` errors. A namespace that no selected pack has is accepted without starting anything. Every well-formed directive has a status in `suppressions` (`ef lint`, `ef diagnostics --json`, MCP and the LSP `effra/lintStatus` notification):
- `applied`: it removed a finding.
- `unused`: its rule completed without a finding to remove. This is also an `EFL004` error.
- `not-evaluated`: its rule did not run. The reason is one of `unchecked-source`, `pack-not-selected`, `rule-off`, `pack-failed`, `facts-unavailable` or `target-unsupported`. A not-evaluated directive does not fail lint.

Only a rule that ran can show a directive unused, so a directive for a rule that is off is not-evaluated rather than an error. A rule that is off or skipped keeps that reason even when another rule of its pack makes the pack fail. Source with a syntax error keeps its directives as `unchecked-source`. See [suppression states](specs/custom-lint.md#implemented-stage-three-suppression-states).

`ef lint receipt FILE [--runs N] [--target go|js] [LINT]` prints a raw [lint cost receipt](specs/custom-lint.md#implemented-stage-three-cost-receipts-and-the-buildrun-control). Per run, it reports nanoseconds for the frontend, fact extraction, each pack's fact serialization and process phases, and the merge. It makes no performance claim. `ef build` and `ef run` never start a rule pack.

A rule from a selected rule pack that did not run fails lint with an `EFL000` lint-runner error and `complete: false`, because an enabled rule that did not run has not passed. A rule limited to other `targets` is the exception when only its pack's default or a preset enabled it: it does not apply to this target, so it is reported as `skipped` (`target-unsupported`, `inapplicable: true`) and lint stays complete and passing. Configuring the rule in the project's own lint configuration enforces it: under an unsupported target lint then fails with `EFL000`. The full contract is in [the custom lint spec](specs/custom-lint.md).

## Local types

Offsets and spans are UTF-8 bytes. `ef query`/`code.typeAt` continue to address diagnostic anchors (for example, the `run` keyword, a call name, or `provide`). The separate `ef type`/`code.type` selector accepts a declaration symbol, an original-byte offset, or a canonical type definition ID tied to its source revision. Offset selection uses retained syntax extents and observed checker bindings, including shorthand field names and Fiber operation receivers; unavailable original locations are reported rather than inferred. Unchecked source, stale revisions/producers and exhausted projection budgets are explicit refusals. Effect types distinguish deferred rows from rows executed while evaluating the expression.

Checked values carry canonical `type` and complete `contract` references. Nominal records, enums and errors retain declaration identity; `ef inspect FILE TYPE_NAME` returns source fields or variants with UTF-8 spans. Successful schema 8 inspection/query/graph responses and selected `code.type` responses include response-local reachable `types`, `rows` and nominal declarations. The selected-type response also carries `querySchemaVersion: 4`, the selected name's declaration `target` (including type-annotation, row-label and template/row parameter tokens) and a bounded plaintext `presentation` shared with LSP hover; its canonical definition selector requires the matching source/import revision. Shared children use references; absent row IDs mean empty rows. Selected roots exclude unrelated declarations and callable metadata. A snapshot is qualified by schema version, source/import revision, target, producer qualifier and reuse scope; source revision alone does not identify the compiler that checked it. Application IDs identify callee/source sites separately from structural contracts.

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

CLI and MCP limit graphs to 1,000 nodes and 2,000 edges; larger graphs fail explicitly. Selected [graph views](#graph-views) answer focused questions about larger sources. The legacy `project.lint` response keeps its separate bounded diagnostic arrays and truncation flags. Full CLI diagnostic reports remain available.

## Graph views

`ef graph FILE` without view options prints the legacy dependency graph above, byte for byte; `project.graph` without view options returns the same object. Any view option selects one GraphViewV1 instead. Making views the default would be a deliberate graph wire-version transition, not a silent change:

```sh
ef graph examples/layers.ef --kind layers --format mermaid
ef graph examples/workflow.ef --focus function:welcome --depth 2 --direction outgoing
ef graph examples/layers.ef --kind application --mode build --format dot
```

| CLI flag | MCP argument | Values |
| --- | --- | --- |
| `--kind` | `kind` | `dependency` (default), `layers`, `application` |
| `--format` | `format` | `json` (default), `mermaid`, `dot` |
| `--focus` | `focus` | exact node ID of the selected kind |
| `--depth` | `depth` | 0–64 hops from the focus; default 1; requires focus |
| `--direction` | `direction` | `outgoing`, `incoming`, `both` (default); requires focus |
| `--edge-kind` (repeatable) | `edgeKinds` | relations to traverse and publish |
| `--collapse` (repeatable) | `collapse` | containment roots whose interior is hidden |
| `--mode` | `mode` | `build` (default) or `test`; application only |

A view has the Stately Graph shape: `{id, mode: "directed", initialNodeId, nodes, edges, data}` with typed `node` and `edge` entries, optional `parentId`, `sourcePort` and `targetPort`, and every Effra fact under `data.effra`. Its `id` names the kind and normalized selection within the qualified snapshot; it is not a freshness proof. Edge IDs are injective tuples of relation, endpoints and occurrence (a source position or a layer plan), so parallel relations stay distinct and IDs never depend on traversal order. `data.effra` carries the producer and snapshot qualification, the normalized selection, completeness, limits, usage, limitations, and the view's own closed `sources`, `types`, `rows` and `declarations` tables. Every published reference resolves inside the view.

Kinds:

- `dependency` serializes the same checked facts as the legacy graph through one fact walker; contracts project only for published nodes.
- `layers` shows each checked layer plan, one canonical node per shared binding, per-plan `selects` edges with visibility, effective implementation, replacement sites and configuration, `depends-on` and `merges` edges, and open construction inputs as explicit `layer-input` boundary nodes with `requires-input` and `consumes-input` edges.
- `application` shows a native Go application plan for `build` or `test`: one node per retained requirement, typed provider-operation origins, a plan-qualified `selects` edge from each retained layer plan to each of its selected layer nodes, hidden ones included, and the runtime catalog closure as `runtime-requires` edges. A layer node shared by several retained plans stays one node with only its binding facts; each plan's effective implementation, replacement and configuration live on that plan's `selects` edge, as in the `layers` kind. Each requirement shows only its first checked witness (`retains`). It plans without building, emitting or running code, and it makes no JavaScript retention claim; the JavaScript target is refused.

Selection runs over fact topology before any contract or type table is materialized, so a focused view succeeds on a source whose whole graph exceeds the node, edge or type limits. Focus bounds materialization and publication, not enumeration: the kind's whole fact topology is still enumerated within the work limit, so a source whose topology alone exceeds that limit refuses even a focused view with `EFGRAPH_WORK_LIMIT`. Completeness reports exact fact totals, the selected and published counts, and the depth frontier. When a selected edge names a node in its metadata that the traversal did not select, such as the plan qualifying a `depends-on` edge or a selection's replacement site, that node is published as closure (`closure: true`, counted in `closureNodes`). Closure nodes are not traversed, gain no edges and never join the frontier, so focus keeps its requested boundary and every published reference still resolves. Collapse hides the interior members of a containment group (`contains` for dependency, `retains` for application; layers share bindings and refuse collapse). Members with a selected relation outside the group stay visible as children of the root, and every published edge is an original fact edge, so collapse never creates a path. Hidden members are listed on the root. Collapse never erases a depth cut: a root whose hidden members were on the frontier joins the frontier and lists them as `hiddenFrontier`.

Mermaid (`flowchart LR`) and DOT (`digraph`, not `strict`) render the one selected view with positional renderer IDs, the canonical IDs as comments or `id` attributes, and subgraphs or clusters for `parentId`. A collapse root stays a node inside its own group in both formats, so its relations attach to the root rather than to the group box; the Mermaid group takes a separate `gN` ID. Closure nodes carry the `closure` class in both formats, following Stately's flowchart emitter: Mermaid declares `classDef closure` and assigns it with `class`, and DOT gives those nodes `style=dashed` and `class="closure"`; a view without closure emits neither. Mermaid labels pass only letters, digits, space and inert punctuation, and encode every other character as an entity. DOT doubles backslashes, escapes quotes, and makes control and bidi characters visible. MCP returns the view for `json`, and `{view, rendering}` for diagrams, where `rendering.text` is exactly the CLI stdout. Renderings are lossy (`rendering.losses`): `json` is the complete interchange.

Refusals carry a stable `EFGRAPH_*` code and the same `CODE: message` text on CLI stderr and in the MCP tool error. A malformed request (unknown kind or format, an incompatible flag such as `--collapse` on the `layers` kind, an invalid or excessive depth, an unknown mode) exits 2. A refusal about the checked source exits 1: unchecked source, an unknown focus, an unsupported target or plan, and the node, edge, work, type, render and response limits. The CLI decodes `--depth` as the JSON value MCP carries, and both keep JSON numbers exact until graph admission, so any integer-valued depth meets the hop limit before it is narrowed: `3000000000` refuses with `EFGRAPH_DEPTH_LIMIT: depth 3000000000 exceeds the 64-hop limit` on both, and a literal no float64 holds, such as `1e400`, refuses with `EFGRAPH_INVOCATION` on both. MCP `expectedRevision` and `expectedProducer` refuse stale snapshots as they do for every other tool, and the next queued request still completes. `machine`, `actor` and `html` are refused as not yet available. A refusal never returns a partial view. The response budget charges the payload as MCP transmits it, once structured and once as escaped text, so the CLI refuses the same oversized request.

## Next capabilities

Revision-bound checked edit plans, multi-file identities, editor integration, complete ownership provenance and runtime/source correlation remain planned. The bounded ownership evidence described above is implemented without claiming a complete borrow checker. Canonical comment-preserving formatting is implemented by the compiler core and exposed through `ef fmt` and MCP `code.format`; the maintained authored-example selection is checked by the gate, and `ef lsp` answers textDocument/formatting with the same formatter over versioned buffers ([profile](lsp.md#document-formatting)). Compiler errors stay independent of optional style policy. Managed test time already shares scheduling with sleep/deadline primitives; see the [testing contract](testing.md) for its supported causal boundaries and foreign-operation limits.

## Gate prerequisites

`scripts/gate.sh` needs the pinned Effect reference tests, which live in a git submodule. After every clone, Rift or worktree run `scripts/init_upstream.sh` once; offline, set `EFFRA_UPSTREAM_MIRROR` to a local Effect clone that contains the pinned commit. The conformance verifier reads the corpus only from git objects ([conformance](conformance.md)).

The README's TypeScript comparison program is checked with `tsc --strict`. The repository has no TypeScript dependency, so the gate environment supplies `tsc` on PATH. Without it the gate prints a loud `typescript: unchecked` line and `TestReadmeSmoke/typescript-strict` skips; it does not fail, so contributors without `tsc` can still gate, but a gate receipt without `typescript: strict` is weaker evidence.

## Merge gate

`scripts/gate.sh` runs `go run ./scripts/gate`, a small Go runner that owns the check graph ([design and measurements](research/gate-throughput.md)). Every check is a step with declared dependencies. Independent steps run concurrently. Each step's output is printed whole, in declaration order. A failing step fails the gate and blocks only the steps that depend on it. `--list` prints every step and its command, `--no-cache` reruns everything, and `--record-durations` re-measures the slow Go packages.

- **Go tests** rely on Go's own test cache, which stays authoritative. `internal/compiler` and `cmd/ef` are split into processes using the measured durations in `scripts/gate/durations.json`. Every shard but the last selects tests by exact name; the last shard skips exactly the names the others selected. So the union of shards covers every test, including tests added since the durations were last recorded. A stale durations file costs wall time, never coverage.
- **Other steps** declare their inputs: repository paths, tool versions, environment variables, whether the tree is committable, and submodule state. They replay a recorded pass while those inputs are unchanged. Receipts live under `$XDG_CACHE_HOME/effra-gate` (by default `~/.cache/effra-gate`), never in the tree, and every worktree shares them. Failures are never recorded.
- **Environment.** Go test steps receive `EFFRA_TOOL_VERSIONS`, a digest of the Node, Bun, tsc and git versions. The JavaScript and smoke helpers read it, so a Go test result cached before a tool upgrade is not reused. The runner sets umask 022 and `GIT_OPTIONAL_LOCKS=0`. It also sets `GOGC=400` unless the caller sets `GOGC`.
- **Process smokes** that drive the CLI, MCP and LSP run as Go tests in `cmd/ef` (`*_smoke_test.go`). Each one works in a private workspace whose files are copied in the test process, so Go's test cache sees every input.

## Performance receipt

On 2026-10-05, Apple M4 Pro / macOS 27.0.1 / Go 1.27.1 arm64, three one-second warm runs of `BenchmarkCompile10KLines` measured 3.13–3.30 ms per parse/check for the synthetic 2,000-function, 10,000-line fixture (about 14.05 MB and 16,192 allocations). The preceding baseline measured 2.95–3.01 ms; this small measurement is not a controlled attribution of the difference or an end-to-end build ratio. Keep watching it as the checker grows.

`BenchmarkLintAndGraph10KLines` runs lint plus graph on an already checked fixture: 0.61–0.62 ms, about 1.16 MB / 6,132 allocations. It excludes parsing and import loading. Reproduce with `go test ./internal/compiler -run '^$' -bench 'Benchmark(Compile|LintAndGraph)10KLines' -benchmem -benchtime=1s -count=3`. Real import caching, cold builds, incremental edits and linking need separate fixtures.
