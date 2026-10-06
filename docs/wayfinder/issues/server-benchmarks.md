<!-- {"id": "server-benchmarks", "title": "Matched server performance against TypeScript and optimized Go", "status": "open", "labels": ["implementation:spec"], "parent": "map", "assignee": "server_benchmarks", "blocked_by": ["foundation-conformance"]} -->
# Matched server performance against TypeScript and optimized Go

## Question

Implement [the matched benchmark contract](../../specs/server-benchmarks.md) to measure Effra, TypeScript/Effect, native TypeScript and optimized Go servers under equivalent observable work and lifecycle obligations. Preserve raw repeated samples and report any advantage or regression honestly.

## Implementation underway

Fixtures and runner can be developed independently against the stable native HTTP seam. Scored measurements and the final receipt wait for foundation conformance and a quiet host; concurrent builds must not contaminate them.
