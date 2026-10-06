<!-- {"id": "stdlib-telemetry-batching", "title": "Owned telemetry and context-partitioned request batching", "status": "open", "labels": ["implementation:task"], "parent": "map", "assignee": null, "blocked_by": ["stdlib-services", "stdlib-flow", "stdlib-shared-platform"]} -->
# Owned telemetry and context-partitioned request batching

Implement Telemetry/batching in [bundled library capability contracts](../../specs/standard-library-capabilities.md): bounded provider-owned metrics/spans and owned request batching with explicit window/capacity and per-key outcomes.

Gate every admitted key completing once, cancelled entries leaving pending work, uncovered-output defects, host/credential context partitioning, parent span relationships, scope shutdown and cardinality/binary limits. No ambient global registry. Concrete batching adoption evidence exists; arbitrary user-programmable transactions remain a separate design exclusion, not a completed guarantee.
