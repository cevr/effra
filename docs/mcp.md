# Compiler MCP server

Build `bin/ef`, then configure a client with this stdio server:

```json
{
  "mcpServers": {
    "effra": {
      "command": "/absolute/path/to/effra/bin/ef",
      "args": ["mcp", "/absolute/path/to/effra"]
    }
  }
}
```

The adapter implements a small read-only tools server using the [2025-11-25 stdio transport](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports), [initialization lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle), and [tools result contract](https://modelcontextprotocol.io/specification/2025-11-25/server/tools). It negotiates that protocol version, receives notifications/initialized before tool access, and keeps stdout strictly JSON-RPC. No HTTP transport, resources, prompts, file writes, or subscriptions are exposed.

| Tool | Arguments | Result |
| --- | --- | --- |
| project.describe | `{}` | Compiler/runtime versions, supported targets, default Go target, operations, guardrail limits |
| code.format | Exactly one of `file` (workspace-relative `.ef`) or `source` (explicit buffer); optional buffer-only `uri` and `expectedDigest` | Syntax-only formatted full text, origin, optional URI, formatter identity, input/output digests and changed state; never writes |
| project.check | `file`, optional `target` (`go`/`js`), optional `expectedRevision` | Checked status, source revision, bounded diagnostics, timing, symbol and nominal declaration metadata, bounded used host bindings, and a complete or explicitly refused schema 4 type/row projection |
| project.diagnostics | `file`, optional `target`, `expectedRevision`, boolean `strict` | Shared compiler and lint report with exact counts, UTF-8 byte spans, canonical `file:` source identity, and UTF-16 LSP ranges |
| project.lint | `file`, optional `target`, `expectedRevision`, boolean `strict` | Checked lint result, error/warning/suggestion counts and truncation flags |
| lint.rules | `{}` | Stable rule catalog |
| code.typeAt | `file`, integer `offset`, optional `target`, `expectedRevision` | Checked expression at a UTF-8 byte diagnostic anchor with response-local reachable type/row definitions |
| code.type | `file`, exactly one of `symbol`, integer `offset`, or `definition`; optional `target`, `expectedRevision` (required for `definition`) | Checked selected declaration, lexical binding/use, original-extent expression, or revision-scoped canonical definition with complete reachable type/row closure |
| project.graph | `file`, optional `target`, `expectedRevision` | Static service/provider/effect graph, up to 1,000 nodes / 2,000 edges, with response-local reachable type/row definitions |
| project.tests | `file`, optional `target`, `expectedRevision` | Selected checked cases and their complete reachable type/row closure under one cumulative catalog budget, independent of whole-source projection refusal; live capability requirement, no execution |
| code.inspect | `file`, `symbol`, optional `target`, optional `expectedRevision` | Canonical function contract or nominal record/enum/error declaration, byte span, local contributions |
| code.explain | Same as inspect | Same initial semantic detail; no transitive explanation engine yet |

Example tool call:

`code.type` reports `locationAvailable: false` when selected metadata has no original location in the current file. Binding identities come from the checker environment; offsets use original syntax extents. Its current producer field identifies the semantic ABI, with executing-artifact qualification deferred to the producer integration.

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "code.inspect",
    "arguments": { "file": "examples/main.ef", "symbol": "greeting" }
  }
}
```

Results include structuredContent; semantic tools also include a matching text representation, while `code.format` uses a short content summary to keep the full structured response bounded. `code.format` accepts an empty `source` buffer, treats `uri` as display identity only, compares `expectedDigest` with exact UTF-8 input bytes before formatting, and returns no text on parse or output-limit failure. `project.diagnostics` returns compiler errors and invalid source as a successful analysis result with `checked=false`, `lintAvailable=false` when advice cannot run, and `policyPassed=false`; strict mode changes policy only. This initial unchecked-source report omits suppression validation along with other lint. Every finding keeps its UTF-8 byte span and, when a source location exists, a valid zero-based UTF-16 LSP range. Unsupported-target findings explicitly report unavailable locations. Tool/path/stale-revision/output-limit errors return `isError=true`. Invalid tool arguments are JSON-RPC errors. Source paths are workspace-relative `.ef` files; resolved paths must remain within the root for admission. Reports separately identify the escaped absolute requested-document `file:` URI plus `origin`, captured with source loading; symlink replacement during analysis cannot relabel the snapshot as its new destination. See [diagnostic identity and line endings](tooling.md#diagnostic-reports).

The adapter accepts 16 MiB newline-delimited JSON message frames, counting bytes before the terminal LF; a CR in CRLF counts as content. Exact-limit frames are admitted. An over-limit line is drained through its next LF without unbounded accumulation, returns a parse error with a null ID, and the next queued request is still processed. A malformed line at EOF returns one parse error and closes cleanly; a valid final request without LF is processed once. `code.format` accepts a 2 MiB source buffer or disk snapshot and rejects formatted output above 4 MiB before returning replacement text. Its full encoded response is also bounded by 16 MiB: `structuredContent` carries the complete text while `content` carries a short summary, and an over-limit encoded response becomes a tool error with no partial text. Both bounded tool response profiles preserve validated scalar request IDs (string, number, or null) without re-encoding their raw representation. Semantic body fields retain JSON HTML escaping and include a matching escaped text copy; `code.format` body fields retain their encoding without optional HTML escape expansion and use a short content summary. If a successful or tool-error body cannot fit the 16 MiB frame, the adapter first replaces it with a short `isError=true` tool result under the original request ID; only if that replacement cannot fit beside the ID does it emit a compact `-32000` JSON-RPC error. If a bounded tool call's unknown-tool or argument error cannot fit, it keeps that error code with the same compact message. Protocol replies outside either bounded tool profile retain their separate JSON encoding and currently have no encoded response cap.

Every non-format tool, including `project.diagnostics`, has a separate 1 MiB encoded successful-response cap, excluding terminal LF. Admission charges both structured content and the escaped text copy, plus the tool envelope and preserved raw scalar ID. `project.diagnostics` accepts 2 MiB regular source files and at most 100 findings; these input and count bounds do not guarantee byte admission. A 101st finding or an over-budget encoded report is an explicit tool error without a partial report or truncated findings, and the next queued request, including a ping, is still processed. Refusals use the separate 16 MiB protocol budget. Legacy `project.lint` keeps its bounded arrays and truncation flags. Oversized symbol detail is rejected explicitly.

Schema 4 type and row references are complete within their response and scoped to its semantic revision. Successful inspection, query and graph responses include reachable type/row definitions and nominal field/variant declarations; shared children use IDs. Empty rows have no ID and no row definition: an absent `failureRow` or `serviceRow` means the empty row. Declaration, structural contract and source application identities have distinct roles. An application ID identifies one callee at one source site, separately from its structural type or runtime allocation. Source revision alone does not identify the compiler producer or authorize cross-build cache reuse. There is no reference lookup endpoint.

Limits are 4,096 type nodes, 8,192 canonical edges (including nominal fields and rows), 4,096 row labels, 256 KiB of string/display/name occurrences, 512 KiB of compatibility metadata, and 1 MiB of encoded successful response bytes, excluding terminal LF. Canonical CLI responses use compact JSON, matching `typeProjectionUsage.responseBytes`. That usage counts the semantic object; MCP additionally charges the complete tool envelope, preserved raw scalar ID, structured content and escaped text copy before encoding. Semantic fields retain JSON HTML escaping. A valid ID that prevents successful admission receives an explicit same-ID refusal under the separate 16 MiB protocol budget; such a refusal can exceed 1 MiB. The compiler retains the complete private arena; limits apply to public projection. Roots and traversal are charged before queues or public tables grow. Compatibility sizes are measured before expanding callable parameter/reference metadata. Refusal usage describes the charged prefix. Graphs additionally admit at most 1,000 nodes and 2,000 edges on both CLI and MCP.

Selected inspection/query/graph refuses with an explicit operational/tool error when a limit or reference validation fails, returning no partial authoritative object. It rejects unchecked source. Small selected roots remain available independently of unrelated wide declarations or a whole-source projection refusal. Selected nominal declarations describe the reachable type closure. Host bindings describe checked foreign calls in the selected function and its named call closure, bounded to 8,192 visited functions, rather than global file metadata.

`project.check` and `ef check` separate semantic validity: valid large source returns `checked: true`, `typeProjectionComplete: false`, and `typeProjectionError` while still permitting Go/JS emission and builds. A refused check retains counts and source diagnostics but omits symbols, declarations, bindings and unresolved type tables. Unchecked source reports admission diagnostics and unavailable type facts. Check diagnostics are capped at 100 with `diagnosticsTruncated`; an oversized diagnostic envelope explicitly reports `diagnosticsUnavailable` instead of shortening messages.

Each semantic query reads and checks its file. `code.format` is syntax-only and does not load host packages, check types, run lint, execute effects or build a backend. It rejects semantic `target` and `expectedRevision` fields; `expectedDigest` is the exact source-byte guard. Both backends share source contracts; semantic queries default to Go and report the selected target. The revision hashes source bytes plus imported Go export archives and normalized behavior contracts; formatter identity and input digest are separate from semantic revisions. Formatter source must be valid UTF-8, and invalid bytes produce the shared lexical `EF001` span before any replacement text. Valid U+FFFD characters and escaped Unicode spellings remain valid. There is no persistent workspace/cache or multi-file snapshot yet. `expectedRevision` and `expectedDigest` let a client reject a changed snapshot. The root is a cooperative local workspace boundary, not a security sandbox against concurrent filesystem replacement.

Validation covers initialization, listing/calling tools, CLI/MCP semantic equality, formatter CLI/MCP parity, stale revisions and digests, malformed requests, unknown tools, invalid arguments, source errors, output limits, and path escapes. scripts/smoke.py and scripts/format_smoke.py exercise the actual compiled stdio processes. No particular editor's MCP configuration has been installed or validated; LSP formatting remains a separate future adapter.

See [tooling](tooling.md) and [testing](testing.md) for severity policy, graph limits and test ownership.
