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

The adapter implements a small read-only tools server using the [2025-11-25 stdio transport](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports), [initialization lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle), and [tools result contract](https://modelcontextprotocol.io/specification/2025-11-25/server/tools). It negotiates that protocol version, receives notifications/initialized before tool access, and keeps stdout strictly JSON-RPC. No HTTP transport, resources, prompts, tool execution, edits, or subscriptions are exposed.

| Tool | Arguments | Result |
| --- | --- | --- |
| project.describe | `{}` | Compiler/runtime versions, supported targets, default Go target, operations, guardrail limits |
| project.check | `file`, optional `target` (`go`/`js`), optional `expectedRevision` | Checked status, source revision, bounded diagnostics, timing, symbol and nominal declaration metadata, bounded used host bindings |
| project.lint | `file`, optional `target`, `expectedRevision`, boolean `strict` | Checked lint result, error/warning/suggestion counts and truncation flags |
| lint.rules | `{}` | Stable rule catalog |
| code.typeAt | `file`, integer `offset`, optional `target`, `expectedRevision` | Checked expression at a UTF-8 byte diagnostic anchor |
| project.graph | `file`, optional `target`, `expectedRevision` | Static service/provider/effect graph, up to 1,000 nodes / 2,000 edges |
| project.tests | `file`, optional `target`, `expectedRevision` | Checked cases, live capability requirement; no execution |
| code.inspect | `file`, `symbol`, optional `target`, optional `expectedRevision` | Canonical function contract or nominal record/enum/error declaration, byte span, local contributions |
| code.explain | Same as inspect | Same initial semantic detail; no transitive explanation engine yet |

Example tool call:

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

Results include structuredContent and a matching text representation. Compilation diagnostics are ordinary project.check results with checked=false; tool/path/stale-revision errors return isError=true. Invalid tool arguments are JSON-RPC errors. Source paths are workspace-relative `.ef` files; resolved paths must remain within the root. The adapter accepts 1 MiB message frames, 2 MiB regular source files, and at most 100 diagnostics and 100 used host bindings per result, with truncation flags. Oversized symbol detail is rejected explicitly. Source spans use UTF-8 bytes rather than LSP UTF-16 positions.

Each query reads and checks its file. Both backends share source contracts; queries default to Go and report the selected target. The revision hashes source bytes plus imported Go export archives and normalized behavior contracts; there is no persistent workspace/cache or multi-file snapshot yet. expectedRevision lets a client reject a changed snapshot. The root is a cooperative local workspace boundary, not a security sandbox against concurrent filesystem replacement.

Validation covers initialization, listing/calling tools, CLI/MCP semantic equality, stale revisions, malformed requests, unknown tools, invalid arguments, source errors, and path escapes. scripts/smoke.py exercises the actual compiled stdio process. No particular editor's MCP configuration has been installed or validated.

See [tooling](tooling.md) and [testing](testing.md) for severity policy, graph limits and test ownership.
