<!-- {"id": "generic-data", "title": "Reusable generic data and product matching", "status": "open", "labels": ["implementation:task"], "parent": "native-server-spec", "assignee": null, "blocked_by": ["native-interfaces"]} -->
# Reusable generic data and product matching

Implement the finite reusable-data clauses of [language abstractions](../../specs/language-abstractions.md): ordinary first-order generic records/enums, bundled Option/Result, typed recipe values in admitted containers, and multi-subject/or-pattern matching. Keep public signatures explicit, canonical nominal identity and owner/capture provenance intact. Diagnose unsupported recursive layouts, ambiguous instantiation or exhausted analysis budgets.

Two unrelated consumers must share the generic declarations. Isolated tests cover wrong type applications, missing state/event pairs, inconsistent or-pattern bindings, unreachable arms, inner-owned handles hidden in generic fields, and safe borrowed controls. Go/JS and CLI/MCP must agree; shared-DAG and product-coverage scaling receipts precede closure. Deliver data and matching as separate compiling/gated commits.

Owner clarification, 2026-10-06: no nil/null language values, nullable ordinary types or observable uninitialized fields. Bundled Option is ordinary closed data; `Some` requires an admitted value. Add source rejection and exhaustive presence controls from [absence and host boundaries](../../specs/absence-and-host-boundaries.md). Automatic Go host-type adaptation depends on this reusable seam instead of introducing an opaque nullable primitive.
