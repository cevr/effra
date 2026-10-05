<!-- {"id": "map", "title": "Effra prototype map", "status": "open", "labels": ["wayfinder:map"], "parent": null, "assignee": null, "blocked_by": []} -->
# Effra prototype map

## Destination

Build a runnable Effra prototype that checks explicit effect contracts, emits JavaScript using Effect, and exposes canonical types through CLI/MCP; use the result to decide the route to a Go-backed server language.

## Notes

- User explicitly requested repository creation and prototype execution. Execution is carried into this map, overriding Wayfinder's planning-only default; charting may create the first artifact but resolves no HITL ticket.
- Read wayfinder, grilling, domain-modeling, and prototype skills as relevant. Compiler prototypes are runnable source artifacts, as explicitly requested, rather than UI/HTML demos.
- Carry forward the conversation: `.ef`, `ef`, managed server scope, Go compiler, JS/Effect first, Go second, explicit exported contracts, agent introspection, fast builds.
- Source remains authoritative; one semantic model serves checking, CLI, and MCP. Performance budgets are aspirations until measured.
- No new external research is needed to chart the initial frontier: the design sketch contains the inspected reference sources. Add research tickets if a subsequent decision needs new evidence.

## Decisions so far

<!-- Initial index intentionally empty. Existing conversation constraints are recorded in Notes and docs/design.md. -->

## Not yet specified

- The server slice's HTTP/database boundary and startup/shutdown wiring once lifecycle and backend semantics have concrete evidence.
- The shape of general higher-order row inference and target interoperability once the small compiler reveals its pressure points.
- The useful runtime snapshot granularity once managed ownership has an implementation.

## Out of scope

Kernels, hard real-time guarantees, freestanding/no-GC execution, a complete ownership checker, TypeScript content-mapper integration, arbitrary FFI, durable workflows, and production completeness are beyond this prototype destination.
