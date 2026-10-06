<!-- {"id": "stdlib-parity", "title": "Bundled standard library capability and language contracts", "status": "open", "labels": ["research:needed"], "parent": "map", "assignee": null, "blocked_by": ["foundation-spec"]} -->
# Bundled standard library capability and language contracts

## Question

Turn [the owner's bundled Effect-grade library direction](../../standard-library.md) into checked module/generic/effect-function seams and reviewable implementation specs. Inventory required capability families and public lifecycle contracts against the pinned reference; distinguish core compiler semantics from bundled library policies. Keep import and compile costs measured.

## Direction

The toolchain ships the library and runtime together. Public contracts remain explicit and CLI/MCP inspectable. Complete parity needs behavior receipts and a supported-target matrix; familiar names and a giant implicit prelude cannot replace those receipts.
