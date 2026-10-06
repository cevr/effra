<!-- {"id": "runtime-modules", "title": "Selectable native runtime source modules", "status": "closed", "labels": ["implementation:task"], "parent": "binary-reachability", "assignee": "runtime_modules", "blocked_by": ["foundation-spec", "generated-output-ownership"]} -->
# Selectable native runtime source modules

Independent preparation for the [small executable contract](../../specs/binary-reachability.md). Separate the native platform facilities currently sharing one source file, and provide one explicit runtime-source dependency catalog with bounded selection. Preserve the existing all-source compiler behavior until checked application roots and owned output reconciliation are implemented through the parent task.

Selection must include actual transitive dependencies, reject unknown modules, and return independent source snapshots. Compile each selected module closure, including core without unused HTTP/JSON/platform imports. Existing Go/JS lifecycle and ownership conformance must remain unchanged. Source-selection tests do not prove application reachability or a size improvement; those require the parent's emitted application, dependency, symbol and byte receipts.

Deliver a compiling/gated isolated commit, independent review and raw selected-source validation. Do not alter scheduler behavior, active function/type migration, emitters, generated-output ownership or library surface syntax in this preparation.

Integration also waits for [isolated generated modules](generated-output-ownership.md): an existing additive output directory retains the old `stdlib.go` after this split. Preserve that failing upgrade receipt and land the general output boundary first, rather than deleting stale files manually.

## Resolution comments

2026-10-06: source-seam work independently reviewed and repaired through0e4f5be, then merged current integrated publisher into the branch and root at637313bdbfdec6352e0ae033e35d5d16333643ac. One catalog owns8 selectable runtime modules and their actual dependency closure; fresh snapshots and real selected-package compilation are gated. The split preserves all11 moved declaration token hashes/comments and leaves scheduler/lifecycle sources unchanged. Source-inventory coverage detects unclassified new runtime files. Normal inherited Go build-cache behavior and trimpath controls pass.

Both integration gates passed: `/tmp/effra-runtime-modules-branch-integration-gate.log` SHA2569c7ba1e0ae4c3f6f2cfaf5541e00d7778c87b753ae18f8492752a19eb60e51a8 and `/tmp/effra-runtime-modules-root-integration-gate.log` SHA25608d3b3a7719e965f1c4922061f4c8f753191483f3a34a8954a05600bef580596. Root's public example builds and executes with12 split runtime source files in a completed isolated module, without the retired stdlib in that module; all31 prior legacy output nodes, including that old stdlib, remain byte/type/mtime unchanged. Raw live receipt `/tmp/effra-runtime-split-root-live.json` and finite review `pass1/runtime-modules-review.md` retained. Application emission still selects all sources; checked reachability and small-binary evidence remain open in the parent task.
