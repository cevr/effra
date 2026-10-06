<!-- {"id": "formatter", "title": "Canonical comment-preserving formatter with CLI and MCP", "status": "open", "labels": ["implementation:task"], "parent": "map", "assignee": "formatter_implementation", "blocked_by": []} -->
# Canonical comment-preserving formatter with CLI and MCP

Implement units 1–2 of the [formatting contract](../../specs/formatting.md): shared ordered/comment-aware syntax, deterministic pure printer, `ef fmt` write/check/stdin modes and read-only MCP previews. Formatting does not require typechecking or host resolution. Preserve token/literal order and lint-directive meaning; test idempotence, syntax equivalence, real-process writes, stale sources and limits. Gate and review the implementation before a separate mechanical authored-example adoption commit. Do not alter pinned upstream or intentionally invalid fixtures.

Unit1 has no semantic-diagnostics dependency and is assigned in an isolated workspace now. Unit2 adapters follow reviewed shared diagnostics for source identity/position reuse; do not start the adapter slice before that integration. Editor formatting remains separately blocked on the language server.

Unit1 integrated45b43d2: pure canonical printer, ordered syntax/comments, directive preservation, complete bounded syntax comparison and shared traversal. Independent core reviews and both integration gates pass.

Unit2 adapters integrated at e2d6161 after both final reviews and finite residual closure. `ef fmt` and read-only MCP `code.format` share the printer, preserve exact source identity, bound responses while keeping request IDs, and replace files atomically in their resolved parent. The branch integration gate passes; the combined integration gate is being recorded. Mechanical authored-example adoption and its maintained gate selection remain open, so this task remains open. Editor formatting is a separate task.
