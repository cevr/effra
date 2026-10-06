# Shared diagnostics, complete types and editor tooling

Status: authorized owner requirement, 2026-10-06. CLI and MCP must expose LSP-grade warnings/errors and full types. Implement a shared compiler snapshot interface, then thin public adapters. Keep Go-only use independent of TypeScript or an editor process.

## One semantic snapshot

`.ef` text remains authoritative. Analysis identifies the source URI, source origin (disk or explicit buffer), target and semantic revision including imported declarations/contracts. An editor document version is separate from a semantic revision. A successful transport call does not imply checked source.

CLI/MCP disk queries report the disk snapshot. The LSP adapter checks its explicitly synchronized buffer. Do not claim the CLI/MCP observes unsaved editor changes without an explicit buffer input or shared workspace protocol. Existing expected-revision checks continue to reject stale requests. Source failures, invalid targets and budget exhaustion are explicit errors, never empty successful reports.

Analysis produces reusable facts; adapters do not reimplement checking, infer rows from display strings, or invoke another adapter's process. Standalone builds do not pay for a persistent language server, full type expansion or editor indexing.

## Diagnostics

Add `ef diagnostics FILE [--target go|js] [--strict] [--json]` and MCP `project.diagnostics`. Both use one report that combines compiler diagnostics and semantic lint advice without duplicates. Preserve existing check/lint APIs and their admission semantics.

The report includes a schema version, source identity, revision, target, checked state, strict policy, pass/fail policy result and total counts by severity. Each finding has a stable code, origin, plain-text message, original byte span and an LSP diagnostic with explicit severity and range. Mapping: compiler and lint errors are Error (1), warnings Warning (2), information Information (3), existing suggestions Hint (4). Supporting information does not require inventing informational rules. `--strict` changes failure policy for warnings, not their severity. Suppression remains limited to eligible lint advice; compiler diagnostics cannot be suppressed. Advice requiring checked semantics is unavailable on invalid source, not a claim of zero possible advice.

Ranges are zero-based and end-exclusive, with UTF-16 character offsets explicitly identified. Preserve existing UTF-8 byte-offset APIs. Use a shared source-position index rather than rescanning the prefix for every finding. Handle CRLF, astral characters, combining marks, empty spans and EOF. Do not attach out-of-file internal spans to fabricated source locations; represent unavailable locations explicitly in the report and project only valid locations into LSP.

CLI text renders actionable location/code/severity/message; JSON is stable structured stdout. Findings that fail policy exit 1; invalid invocation exits 2; operational failures remain distinguishable from a clean source report. Existing CLI conventions may determine the default text/JSON mode, but documented `--json` must be supported. MCP source diagnostics are a successful analysis result with a failed policy field; malformed request, stale revision and admission failures are tool errors.

Keep existing MCP source-size and regular-file admission guards. Bounded output must either return an explicit limit error or expose totals, truncation and revision-bound continuation. Never silently truncate errors or call a partial result complete. Initial implementation may reject oversized reports rather than adding pagination prematurely.

## Full types

Include a separate compiler/analysis-producer identity in the full semantic snapshot. Source revision, target, schema version and producer identity together qualify reusable facts; rule-pack/configuration identity additionally qualifies custom lint results. Do not treat the current source/import digest alone as a cache key across compiler upgrades. Review evidence: identical owned-sibling source has revision `69c03738135f74cb78264ae5999717e23fab0031d4e861bad456200fd049ddba` under de3ae027 and bfe0862 but different admission/ownership facts. Preserve useful cross-target source identity rather than pretending a compiler repair edited the source. Publish the actual identity strength (release/build/content or explicitly unavailable); a dirty Git tip alone is not an exact build identity. Compute producer identity once per process/build artifact, not through a Git subprocess on every query. Bind revision-scoped type lookups and caches to this qualified snapshot, with cross-build stale-fact tests. This belongs to the canonical full-type snapshot unit, not the already frozen initial diagnostics batch.

Extend inspect/type queries from the canonical type representation established by bundled interfaces. A type graph contains stable identities within the snapshot, roots and one definition per reachable type; references express sharing or recursion. A display string is derived presentation. No independently parsed string grammar or opaque callback erasure is acceptable.

For supported language constructs, expose:

- Primitive and nominal identities, declaration locations, type arguments and generic parameters.
- Record fields, all enum variants and payload fields, and named failure payloads.
- Pure/effect callable parameters and result, declared failure/service rows, finite row parameters, and separate evaluation contributions. Distinguish a lazy recipe from its executed result and a running fiber.
- Service operations, provider construction dependencies/configuration, captured provider origins and invocation contracts. Reuse graph edges for dependents.
- Ownership provenance and its evidence status where the compiler records it; distinguish borrowed, owned, conservative unknown, trusted foreign behavior and runtime policy. Never infer a proof from an empty serialized list.
- Target availability and imported declaration/binding trust when applicable.

Queries support named declarations, checked expressions and lexical bindings using semantic identities. Local shadowing, match bindings, parameters and provider methods resolve to their actual declarations. Unsupported or unchecked queries explicitly say unavailable; no apparently authoritative guessed type. Declaration lookup and expression lookup remain distinguishable.

Extend the shared syntax seam with full node extents and binding-name spans while retaining diagnostic anchors for existing findings and line-sensitive suppressions. Preserve source order for pattern bindings. Facts describe original syntax, not checker-desugared substitutes; store checked resolutions/types separately where mutation would otherwise erase source facts. The formatter's token/trivia representation remains syntax-only and is not reconstructed from a checked Program. Port graph/lint binding consumers onto the recorded resolution when their fact families land, rather than adding another spelling-based resolver. Acceptance includes querying a let-name offset, navigating a pattern alias to its name token, and unchanged original syntax facts before/after checking.

The same once-computed producer identity qualifies formatter results and custom-lint analyses. Formatter style/schema version is additional metadata, not a substitute for identifying the actual producer. Distinct development builds must not claim an identical exact identity merely because their version string or Git tip matches; explicitly unavailable identity is preferable to fabricated precision.

Expose a focused `ef type FILE` query with an explicit symbol or byte offset, and the equivalent MCP `code.type`; retain existing `code.typeAt` behavior for compatibility. Full inspect output shares these definitions. Allow a named type-definition query by revision-scoped identity, so a consumer can expand a reference without fetching an entire project. Specify finite node/edge/byte budgets and clear exhaustion behavior. Do not cap fields while describing the omitted part as complete. Include deterministic ordering and an additive/versioned wire migration.

## LSP adapter

Ship `ef lsp` over stdio after shared diagnostics and types. Implement initialization, shutdown/exit, full-document open/change/close synchronization, versioned publishDiagnostics, hover and definition using the shared model. Advertise only implemented capabilities. Select UTF-16 positions initially; do not claim negotiated UTF-8/UTF-32 support without conversion tests. Plain messages and plaintext hover remain compatible without optional markup capabilities.

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
