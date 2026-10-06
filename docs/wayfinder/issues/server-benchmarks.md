<!-- {"id": "server-benchmarks", "title": "Matched server performance against TypeScript and optimized Go", "status": "open", "labels": ["implementation:spec"], "parent": "map", "assignee": "framework_benchmarks", "blocked_by": ["native-server-conformance"]} -->
# Matched server performance against TypeScript and optimized Go

## Question

Implement [the matched benchmark contract](../../specs/server-benchmarks.md) to measure Effra, TypeScript/Effect, native TypeScript and optimized Go servers under equivalent observable work and lifecycle obligations. Preserve raw repeated samples and report any advantage or regression honestly.

## Implementation underway

Fixtures and runner can be developed independently against the stable native HTTP seam. Scored measurements and the final receipt wait for foundation conformance and a quiet host; concurrent builds must not contaminate them.

The isolated provenance unit is independently clear at `a7eae33999c07e3623a7336483a6a86ee2926b37`: pinned corpus authenticity, complete owned-snapshot validation, structured malformed-metadata rejection, package versions/content, and actual Node/Bun importer resolution. Conditional overrides under both `dist/node_modules` and the scoped package parent are rejected; inspection preserves installed files and cleans owned temporary mirrors. This branch is not integrated yet. Runner ownership/accounting repair follows; wire, lifecycle, admission, the native framework fixture and scored measurements remain open.
