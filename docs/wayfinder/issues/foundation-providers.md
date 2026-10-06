<!-- {"id": "foundation-providers", "title": "Dependency-capturing provider composition", "status": "closed", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": null, "blocked_by": ["foundation-data"]} -->
# Dependency-capturing provider composition

## Question

Implement checked provider dependencies/configuration, lexical capture and truthful dependency graph relationships per [the foundation spec](../../specs/production-foundations.md). Caller runtime ownership remains current; provider captures do not retain a closed construction fiber.

## Implementation asset

[Construction and lexical capture seam](../../specs/provider-construction.md).

## Resolution

Integrated constructor/configuration, lexical service-value capture and graph provenance at `163040c`; independent review repair `6a84eda` is merged through `7430883`. Config-only JS recipes now allocate on each execution, and graphs link materialized values to recipes and constructor declarations with incoming dependents. Independent public JS identity, CLI graph and Go/JS current-owner/cancellation checks passed. The integrated full gate and Go race suite passed at `e5777f7`. Explicit materialized values are reusable; fallible acquisition, scoped initialization memoization and cycle solving remain unavailable.
