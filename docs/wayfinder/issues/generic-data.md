<!-- {"id": "generic-data", "title": "Reusable generic data and product matching", "status": "open", "labels": ["implementation:task"], "parent": "native-server-spec", "assignee": null, "blocked_by": ["native-interfaces"]} -->
# Reusable generic data and product matching

Implement the finite reusable-data clauses of [language abstractions](../../specs/language-abstractions.md): ordinary first-order generic records/enums, bundled Option/Result, typed recipe values in admitted containers, and multi-subject/or-pattern matching. Keep public signatures explicit, canonical nominal identity and owner/capture provenance intact. Diagnose unsupported recursive layouts, ambiguous instantiation or exhausted analysis budgets.

Two unrelated consumers must share the generic declarations. Isolated tests cover wrong type applications, missing state/event pairs, inconsistent or-pattern bindings, unreachable arms, inner-owned handles hidden in generic fields, and safe borrowed controls. Go/JS and CLI/MCP must agree; shared-DAG and product-coverage scaling receipts precede closure. Deliver data and matching as separate compiling/gated commits.
