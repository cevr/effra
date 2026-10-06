<!-- {"id": "language-server", "title": "Shared-model stdio language server", "status": "open", "labels": ["implementation:task"], "parent": "map", "assignee": null, "blocked_by": ["semantic-diagnostics", "semantic-types", "lsp-diagnostics"]} -->
# Shared-model stdio language server

Implement `ef lsp` from the [shared tooling contract](../../specs/semantic-tooling.md): truthful capabilities, versioned full-document synchronization, diagnostics, hover, definition and clean protocol lifecycle. Test real framed transport, unsaved-buffer identity, Unicode positions, stale analysis and bounded admission. No second checker or unimplemented workspace/edit capabilities. Full gate and independent review required.
