<!-- {"id": "runtime-modules", "title": "Selectable native runtime source modules", "status": "open", "labels": ["implementation:task"], "parent": "binary-reachability", "assignee": "runtime_modules", "blocked_by": ["foundation-spec"]} -->
# Selectable native runtime source modules

Independent preparation for the [small executable contract](../../specs/binary-reachability.md). Separate the native platform facilities currently sharing one source file, and provide one explicit runtime-source dependency catalog with bounded selection. Preserve the existing all-source compiler behavior until checked application roots and owned output reconciliation are implemented through the parent task.

Selection must include actual transitive dependencies, reject unknown modules, and return independent source snapshots. Compile each selected module closure, including core without unused HTTP/JSON/platform imports. Existing Go/JS lifecycle and ownership conformance must remain unchanged. Source-selection tests do not prove application reachability or a size improvement; those require the parent's emitted application, dependency, symbol and byte receipts.

Deliver a compiling/gated isolated commit, independent review and raw selected-source validation. Do not alter scheduler behavior, active function/type migration, emitters, generated-output ownership or library surface syntax in this preparation.
