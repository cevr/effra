<!-- {"id": "stdlib-parity", "title": "Bundled standard library capability and language contracts", "status": "closed", "labels": ["research:needed"], "parent": "map", "assignee": "stdlib_capability_research", "blocked_by": ["foundation-spec"]} -->
# Bundled standard library capability and language contracts

## Question

Turn [the owner's bundled Effect-grade library direction](../../standard-library.md) into checked module/generic/effect-function seams and reviewable implementation specs. Inventory required capability families and public lifecycle contracts against the pinned reference; distinguish core compiler semantics from bundled library policies. Keep import and compile costs measured.

## Direction

The toolchain ships the library and runtime together. Public contracts remain explicit and CLI/MCP inspectable. Complete parity needs behavior receipts and a supported-target matrix; familiar names and a giant implicit prelude cannot replace those receipts.

## Resolution comments

2026-10-06: pinned Effect4.0.1 source/test research, independent counsel and refreshed read-only application-source comparisons produced [finite capability contracts](../../specs/standard-library-capabilities.md) and five open implementation batches: [owned core](stdlib-owned-core.md), [services](stdlib-services.md), [flow](stdlib-flow.md), [shared/platform](stdlib-shared-platform.md), and [telemetry/batching](stdlib-telemetry-batching.md). This closes the inventory/specification question only. No new library family or native conformance pass is claimed. Mutable payload ownership, exact sharing policy, byte bounds and transaction admission remain explicit constraints rather than inferred guarantees.
