# Shared diagnostics, complete types and editor tooling

Status: authorized owner requirement, 2026-10-06. CLI and MCP must expose LSP-grade warnings/errors and full types. Implement a shared compiler snapshot interface, then thin public adapters. Keep Go-only use independent of TypeScript or an editor process.

## One semantic snapshot

`.ef` text remains authoritative. Analysis identifies the source URI, source origin (disk or explicit buffer), target and semantic revision including imported declarations/contracts. An editor document version is separate from a semantic revision. A successful transport call does not imply checked source.

CLI/MCP disk queries report the disk snapshot. The LSP adapter checks its explicitly synchronized buffer. Do not claim the CLI/MCP observes unsaved editor changes without an explicit buffer input or shared workspace protocol. Existing expected-revision checks continue to reject stale requests. Source failures, invalid targets and budget exhaustion are explicit errors, never empty successful reports.

Analysis produces reusable facts; adapters do not reimplement checking, infer rows from display strings, or invoke another adapter's process. Standalone builds do not pay for a persistent language server, full type expansion or editor indexing.

Shared syntax traversal visits structural children once, even when named arguments or checked constructor fields reference the same expression. Scope-sensitive consumers retain their own lexical handling; deduplication must not skip real capabilities or findings. A small valid nested-data source must not trigger exponential work in lint, diagnostics, graph or test admission. Exercise real CLI/MCP completion and queued-request responsiveness, not just frontend timing or a source-byte limit.

## Diagnostics

Add `ef diagnostics FILE [--target go|js] [--strict] [--json]` and MCP `project.diagnostics`. Both use one report that combines compiler diagnostics and semantic lint advice without duplicates. Preserve existing check/lint APIs and their admission semantics.

The report includes a schema version, source identity, revision, target, checked state, strict policy, pass/fail policy result and total counts by severity. Each finding has a stable code, origin, plain-text message, original byte span and an LSP diagnostic with explicit severity and range. Mapping: compiler and lint errors are Error (1), warnings Warning (2), information Information (3), existing suggestions Hint (4). Supporting information does not require inventing informational rules. `--strict` changes failure policy for warnings, not their severity. Suppression remains limited to eligible lint advice; compiler diagnostics cannot be suppressed. Advice requiring checked semantics is unavailable on invalid source, not a claim of zero possible advice.

Ranges are zero-based and end-exclusive, with UTF-16 character offsets explicitly identified. Preserve existing UTF-8 byte-offset APIs. Use a shared source-position index rather than rescanning the prefix for every finding. Handle CRLF, astral characters, combining marks, empty spans and EOF. Do not attach out-of-file internal spans to fabricated source locations; represent unavailable locations explicitly in the report and project only valid locations into LSP.

CLI text renders actionable location/code/severity/message; JSON is stable structured stdout. Findings that fail policy exit 1; invalid invocation exits 2; operational failures remain distinguishable from a clean source report. Existing CLI conventions may determine the default text/JSON mode, but documented `--json` must be supported. MCP source diagnostics are a successful analysis result with a failed policy field; malformed request, stale revision and admission failures are tool errors.

Keep existing MCP source-size and regular-file admission guards. Bounded output must either return an explicit limit error or expose totals, truncation and revision-bound continuation. Never silently truncate errors or call a partial result complete. Initial implementation may reject oversized reports rather than adding pagination prematurely.

## Full types

Include a separate compiler/analysis-producer identity in the full semantic snapshot. Source revision, target, schema version and producer identity together qualify reusable facts; rule-pack/configuration identity additionally qualifies custom lint results. Do not treat the current source/import digest alone as a cache key across compiler upgrades. Review evidence: identical owned-sibling source has revision `69c03738135f74cb78264ae5999717e23fab0031d4e861bad456200fd049ddba` under de3ae027 and bfe0862 but different admission/ownership facts. Preserve useful cross-target source identity rather than pretending a compiler repair edited the source. Publish the actual identity strength (release/build/content or explicitly unavailable); a dirty Git tip alone is not an exact build identity. Compute producer identity once per process/build artifact, not through a Git subprocess on every query. Bind revision-scoped type lookups and caches to this qualified snapshot, with cross-build stale-fact tests. This belongs to the canonical full-type snapshot unit, not the already frozen initial diagnostics batch.

Extend inspect/type queries from the canonical type representation established by bundled interfaces. A type graph contains stable identities within the snapshot, roots and one definition per reachable type; references express sharing or recursion. A display string is derived presentation. No independently parsed string grammar or opaque callback erasure is acceptable.

Exact producer content identity must describe the executing artifact, not whichever binary now occupies its installation pathname. Keep release/build declarations separate and informational; expose only bounded, whitelisted settings. On a platform where executing-artifact bytes cannot be identified, report that limitation and limit reuse to an explicitly qualified process scope. Linux may read the executing image through `/proc/self/exe`; opening the pathname returned by `os.Executable` is insufficient after replacement. See the [Linux executable-image contract](https://man7.org/linux/man-pages/man5/proc_pid_exe.5.html). Artifact identity does not authenticate a build or identify arbitrary dynamically loaded dependencies. Keep acquisition lazy and outside semantic compilation, with passive identity facts shared by adapters. Test actual atomic replacement with an old running server and a fresh process, distinct dirty builds, unavailable identity, and lazy single acquisition. Repeated unchanged digests alone do not prove single acquisition. Preserve ordinary build/emit behavior and measure first-report cost separately from subsequent reports.

For supported language constructs, expose:

- Primitive and nominal identities, declaration locations, type arguments and generic parameters.
- Record fields, all enum variants and payload fields, and named failure payloads.
- Pure/effect callable parameters and result, declared failure/service rows, finite row parameters, and separate evaluation contributions. Distinguish a lazy recipe from its executed result and a running fiber.
- Service operations, provider construction dependencies/configuration, captured provider origins and invocation contracts. Reuse graph edges for dependents.
- Ordinary actor protocol/method argument, reply and failure types; admission versus domain rows; constructor/captured requirements, owner and terminal policy evidence. Optional machine transition/entry facts specialize the behavior; inspection never requires actor syntax or executes a handler. Supervision/durable providers carry separate qualified facts under [actor contracts](actors.md).
- Ownership provenance and its evidence status where the compiler records it; distinguish borrowed, owned, conservative unknown, trusted foreign behavior and runtime policy. Never infer a proof from an empty serialized list.
- Target availability and imported declaration/binding trust when applicable.

Queries support named declarations, checked expressions and lexical bindings using semantic identities. Local shadowing, match bindings, parameters and provider methods resolve to their actual declarations. Unsupported or unchecked queries explicitly say unavailable; no apparently authoritative guessed type. Declaration lookup and expression lookup remain distinguishable.

Extend the shared syntax seam with full node extents and binding-name spans while retaining diagnostic anchors for existing findings and line-sensitive suppressions. Preserve source order for pattern bindings. Facts describe original syntax, not checker-desugared substitutes; store checked resolutions/types separately where mutation would otherwise erase source facts. The formatter's token/trivia representation remains syntax-only and is not reconstructed from a checked Program. Port graph/lint binding consumers onto the recorded resolution when their fact families land, rather than adding another spelling-based resolver. Acceptance includes querying a let-name offset, navigating a pattern alias to its name token, and unchanged original syntax facts before/after checking.

The same once-computed producer identity qualifies formatter results and custom-lint analyses. Formatter style/schema version is additional metadata, not a substitute for identifying the actual producer. Distinct development builds must not claim an identical exact identity merely because their version string or Git tip matches; explicitly unavailable identity is preferable to fabricated precision.

Expose a focused `ef type FILE` query with an explicit symbol or byte offset, and the equivalent MCP `code.type`; retain existing `code.typeAt` behavior for compatibility. Full inspect output shares these definitions. Allow a named type-definition query by revision-scoped identity, so a consumer can expand a reference without fetching an entire project. Specify finite node/edge/byte budgets and clear exhaustion behavior. Do not cap fields while describing the omitted part as complete. Include deterministic ordering and an additive/versioned wire migration.

### Implemented producer-qualified semantic and selected-type queries

Semantic CLI/MCP reports add `producer` and `snapshot` metadata without changing
source/import `revision` or canonical reference IDs. `producerIdentity` remains
the declared private checker/interface ABI; it is not an artifact fingerprint.
The snapshot tuple contains semantic schema version, revision, target, producer
qualifier and reuse scope. A consumer reusing facts must retain the entire tuple.
MCP semantic tools accept optional `expectedProducer` alongside `expectedRevision`;
an unequal qualifier is an explicit stale-producer refusal. Omitting it requests
fresh facts and does not validate cached facts from another compiler.

The shared lazy producer owner identifies the executing Go host on Linux by
streaming `/proc/self/exe` once, including when the installation pathname has
been atomically replaced. SHA-256 content identity has `executing-artifact`
strength and `artifact` reuse scope. Unreadable, unsupported or over-256-MiB
images report `unavailable`, with a random stable process qualifier and `process`
reuse scope; if that qualifier cannot be generated, scope is `none` and facts
cannot be reused. Other platforms do not claim executing-image identification.
Informative whitelisted Go/module/VCS/platform declarations are bounded and
separate from the content key. No Git subprocess runs during acquisition or
queries. Private distributed interface compatibility continues to use its ABI
and admitted content hashes, rather than this host-image digest.

Formatter reports carry the same producer identity while retaining their
independent style/schema version and input/output byte digests. `code.format`
also accepts `expectedProducer`. Built-in lint and shared diagnostic reports
carry the qualified semantic snapshot; external rule-pack analysis identities
remain part of the unfinished custom-lint unit. Compilation, emission, build,
run and raw `fmt --stdin` do not acquire inspection identity. LSP diagnostics
still publish the finite diagnostics-only protocol and expose no reusable type
references or new navigation capabilities.

The digest identifies the statically linked executing host, not authenticated
build provenance, arbitrary dynamic dependencies or in-place executable writes.
Real atomic replacement/fresh-process controls, different dirty hosts with
identical informative declarations, explicit failure fallback, and a separate
controlled-opener single-acquisition test are retained producer controls.
Producer hashing cost measurement remains deferred; no performance claim follows
from these correctness checks.

The selected-type adapters add `ef type FILE --symbol NAME`, `--offset BYTE`,
or `--definition TYPE_ID --revision REVISION`; `--target go|js` is supported.
MCP `code.type` accepts exactly one `symbol`, `offset`, or `definition`; a
definition requires `expectedRevision`. `expectedProducer` is an independent
optional guard, including when `expectedRevision` is supplied. Named selection
returns the existing function or nominal declaration contract. Byte offsets
select original declaration/name tokens, actual checked lexical uses, or
retained checked expression extents. Local shadowing and provider receivers
resolve through checker binding identities. Shorthand fields retain their name
token extent, and Fiber operation receivers use the same observed local-read
owner as other checked uses. The query adds `querySchemaVersion: 1` beside the
shared semantic schema and producer-qualified snapshot; the existing
`ef query`/`code.typeAt` diagnostic-anchor behavior remains unchanged.

Selected responses contain only the reachable canonical type/row closure and
refuse incomplete publication under the existing node, edge, row, string,
compatibility and encoded-response budgets. `locationAvailable` distinguishes
retained current-file syntax from imported or otherwise unavailable locations.
Original syntax facts are captured before checker lowering, retain separate
diagnostic anchors and ordered pattern aliases, and stop at their bounded
100,000-fact cap without retaining refused IDs. Revision-bound definition
selection expands a canonical reference without fetching a whole project. The
response's producer metadata qualifies this lookup by the actual executing
artifact (or explicit process fallback), independently of private ABI identity.
Custom rule packs, full LSP navigation and comprehensive binding-consumer
migration remain separate unfinished units.

## LSP adapter

Ship `ef lsp` over stdio after shared diagnostics and types. Implement initialization, shutdown/exit, full-document open/change/close synchronization, versioned publishDiagnostics, hover and definition using the shared model. Advertise only implemented capabilities. Select UTF-16 positions initially; do not claim negotiated UTF-8/UTF-32 support without conversion tests. Plain messages and plaintext hover remain compatible without optional markup capabilities.

For independent delivery, the diagnostics/document-lifecycle subset may land after shared diagnostics while canonical full types are being built. This preparatory `ef lsp` advertises full-document synchronization and diagnostics only; it does not close the full language-server task or advertise hover/definition/formatting. It uses the same compiler report and preserved buffer/import identity, with public framed-process tests. No duplicate semantic snapshot representation is introduced to parallelize this work.

Buffers are versioned and bounded. Reject invalid/out-of-order changes; closing a document clears its diagnostics and releases the buffer. Never publish older analysis over a newer document revision. Framing handles split/coalesced Content-Length messages, bounded bodies, protocol errors and EOF without log output on protocol stdout. Unsupported methods receive protocol errors, not fabricated results. Definition navigation follows semantic bindings, not text search.

This initial adapter is single-file plus the imports already supported by the compiler. It does not advertise workspace indexing, completion, rename, formatting or edit plans. Formatting has a [separate finite contract](formatting.md) and follow-up task; enable its capability only after that adapter is tested. Other edits require their own contracts. Hover/navigation may report no result for an unsupported location; diagnostics remain available for incomplete source.

## Acceptance and delivery

1. Shared diagnostics plus CLI/MCP: test compiler errors, lint error/warning/hint, suppression, strict severity preservation, invalid source, missing file, unsupported target, stale revision, limit boundaries and UTF-8/UTF-16 range parity through real processes. A queued MCP ping after a rejected special file still completes.
2. Full canonical type graph: record/enum nesting, failure payloads, function row forwarding, generic instantiation, provider dependency/capture inspection and lexical shadowing. Compare CLI/MCP results at one revision. Invalid source must not masquerade as checked. Large shared type DAGs remain bounded without exponential expansion or lost contracts. Measure checking separately from requested serialization.
3. LSP: drive actual framed stdio initialize/open/change/hover/definition/close/shutdown exchanges, Unicode/CRLF positions, unsaved buffers differing from disk, invalidated diagnostics, stale versions, malformed/oversized frames and clean exit. Compare all returned semantic facts with CLI/MCP snapshots of identical content.

Each unit compiles, passes the full repository gate and receives independent review. Keep the actual supported surface documented; a JSON range shape alone is not an implemented LSP server.

The owner additionally requires [custom lint rules](custom-lint.md). Keep diagnostic origin/rule identity extensible and fact availability explicit; the following rule-pack task reuses these snapshots and adapters. It does not belong inside the initial diagnostics unit.

## Prior art

Microsoft's [LSP specification](https://microsoft.github.io/language-server-protocol/) is the protocol authority. Inspected source through the repo cache at `microsoft/language-server-protocol` commit `f8c4bc9834703b7317c98c8e8053a28fa4b6b997`: `_specifications/lsp/3.18/types/{position,range,diagnostic}.md` and `language/hover.md`. Positions use negotiated code units with mandatory UTF-16 support; diagnostic severities are 1–4. Diagnostic markup in 3.18 requires a client capability, so the initial implementation uses plain text. Existing Effect tooling, TypeScript content mapping and Go declaration inspection inform capabilities; none replaces the Effra checker.
