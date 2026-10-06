# Language server diagnostics

Start `ef lsp` over stdio, or `ef lsp --target js` to select the JavaScript target. Go is the default. Both input and output use LSP Content-Length framing with UTF-8 JSON bodies and CRLF headers. Stdout contains protocol messages only; terminal transport failures go to stderr and exit 1. Invalid CLI arguments exit 2.

This preparatory adapter advertises UTF-16 positions and full-document synchronization (`openClose: true`, `change: 1`). It implements initialize/initialized, shutdown/exit, and textDocument/didOpen, didChange and didClose. Hover, definition, completion, workspace indexing, rename and formatting remain future work and receive method-not-found errors when requested. This subset does not complete the full language-server task.

Send initialize once, then the initialized notification before opening documents. Open requires `languageId: "effra"`, text, and an integer version. Changes require a strictly newer version and a nonempty list of full-text replacements; range and rangeLength fields are rejected, including null. All replacements are validated, then the final snapshot is checked. Duplicate opens, unknown changes/closes and invalid versions produce window/logMessage errors and preserve the accepted text. Close publishes an empty diagnostic list without a version and releases the buffer; reopen starts a new version sequence.

Documents use canonical escaped absolute local file URIs, such as `file:///home/me/project/main.ef`; paths with spaces use `%20`. Authorities (including localhost), queries, fragments, dot segments, noncanonical escapes and non-`.ef` paths are rejected. URI admission never reads the file or resolves symlinks. New unsaved files are supported. Captured buffer text and URI form the compiler SourceSnapshot with origin `buffer`; imports resolve from that document's directory. Disk-based CLI/MCP queries continue to describe their own disk snapshots.

Every accepted open/change runs the same compiler DiagnosticReport used by CLI/MCP. Publications retain the accepted document version, plain messages, stable codes, numeric severities and zero-based end-exclusive UTF-16 ranges. Errors, warnings and hints share the compiler projection, including CRLF, astral characters, combining marks and EOF positions. The adapter does not apply strict lint policy or silently suppress findings. A failed import, unavailable source location or report budget failure sends an explicit window/logMessage error and publishes no replacement diagnostic list. Previous published diagnostics may remain visible until the next successful analysis or close; the error is not a claim that the latest text is clean.

The session processes messages and analysis in receive order, without detached workers, debounce queues or stale result races. Each check completes before the next message is read. Cancellation for a completed or unknown request is a no-op; it cannot interrupt a synchronous document check. Compiler import loading keeps its existing per-subprocess 30-second deadline. Shutdown and EOF therefore wait for an in-progress check, and may also wait on a blocked client output stream. No stronger stop-latency guarantee is claimed. Shutdown releases all buffers before replying null; only exit is subsequently accepted. Exit after shutdown, or EOF after shutdown, exits 0; exit/EOF before shutdown exits 1.

| Admission boundary | Limit |
| --- | --- |
| Input body | 2 MiB, checked before body allocation |
| Headers | 8 KiB aggregate; 4 KiB reader line bound |
| Open documents | 32 |
| One document/replacement | 256 KiB of UTF-8 text |
| Retained document text | 2 MiB aggregate |
| Findings per report | 1,000; explicit refusal above the limit |
| Output | 2 MiB conservative pre-encoding budget and encoded-body bound |
| Request IDs | LSP signed 32-bit integers or strings; 256 encoded bytes |

The output budget charges JSON escape expansion and container/field overhead before serialization, so some messages smaller than 2 MiB may be refused. Reports are never silently truncated. Invalid JSON bodies, invalid IDs and rejected document notifications retain framing and allow the next request. Unpaired JSON UTF-16 escapes are rejected rather than replaced with different source text. Missing/duplicate/invalid Content-Length, unsupported content encoding, oversized headers/body and partial EOF are terminal framing failures because the next byte boundary cannot be trusted. Unknown notifications are ignored according to JSON-RPC; unsupported requests receive errors. Pre-initialize notifications are dropped, apart from exit.

`python3 scripts/lsp_smoke.py` exercises actual framed processes: split/coalesced frames, shared CLI/MCP diagnostic parity, Unicode ranges, unsaved buffers, version rejection, close/reopen, module-relative imports, operational failures, bounds, queued recovery and exit behavior. The repository gate includes these checks; Go tests additionally cover byte/count release, output failure and ownership of accepted snapshots.
