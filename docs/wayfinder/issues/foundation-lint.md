<!-- {"id": "foundation-lint", "title": "Reasoned semantic lint suppression", "status": "closed", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": "reasoned_lint", "blocked_by": []} -->
# Reasoned semantic lint suppression

## Question

Support named next-line optional-lint suppression with non-empty reason, unknown/unused/malformed validation and byte spans. Mandatory compiler diagnostics cannot be suppressed. Preserve source text/comments.

## Resolution

Implemented named next-line optional-lint suppression with required reasons, source comment spans, and malformed/unknown/unused validation. Compiler admission errors remain mandatory. Shared CLI/MCP parity, the full gate, and Go race suite passed on integration commit `7362739`; implementation commits `efacbda` and `7362739`.
