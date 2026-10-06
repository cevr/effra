<!-- {"id": "map", "title": "Effra prototype map", "status": "open", "labels": ["wayfinder:map"], "parent": null, "assignee": null, "blocked_by": []} -->
# Effra prototype map

## Destination

Advance Effra into a credible Go-backed server language by implementing production-inspired foundation contracts and runnable acceptance fixtures, preserving explicit types, owned lifetimes, fast compilation and shared CLI/MCP inspection. Work proceeds through complete staged implementation specs while unresolved architectural decisions remain visible.

## Notes

- User explicitly invoked Wayfinder and implement-spec on 2026-10-05 to implement missing production-inspired capabilities. Decision tickets remain distinct from implementation tasks; the [production foundation spec](../../specs/production-foundations.md) has a separate implementation graph under this map.
- User explicitly requested repository creation and prototype execution. Execution is carried into this map, overriding Wayfinder's planning-only default; charting may create the first artifact but resolves no HITL ticket.
- Read wayfinder, grilling, domain-modeling, and prototype skills as relevant. Compiler prototypes are runnable source artifacts, as explicitly requested, rather than UI/HTML demos.
- Carry forward the conversation: `.ef`, `ef`, managed server scope, Go compiler, native Go executables as the default build, optional JS/Effect target, explicit exported contracts, agent introspection, fast builds.
- Source remains authoritative; one semantic model serves checking, CLI, and MCP. Performance budgets are aspirations until measured.
- No new external research is needed to chart the initial frontier: the design sketch contains the inspected reference sources. Add research tickets if a subsequent decision needs new evidence.

## Decisions so far

- [Which production patterns constrain the next foundation contracts?](research-foundations.md): timer scheduling, dependency capture and nested ownership set the foundation acceptance seams.

## Additional implementation

- [Matched server performance against TypeScript and optimized Go](server-benchmarks.md): owner-requested server equivalence, throughput/latency, resource, startup/shutdown and separate build receipts. Performance superiority remains a measured question.

## Foundation implementation receipts

- [Reasoned semantic lint suppression](foundation-lint.md): named next-line advice suppression requires a reason; malformed, unknown and unused directives diagnose without disabling compiler checks.
- [Closed application data and exhaustive interpretation](foundation-data.md): records, payload enums, structured failures and exhaustive matching execute on Go/JS and share inspectable canonical declarations; independent review repairs are integrated.

- [Dependency-capturing provider composition](foundation-providers.md): checked configuration and lexical dependency capture retain the invocation owner; recipes, materialized values and constructor origins are inspectable in the shared graph.

## Not yet specified

- The server slice's HTTP/database boundary and startup/shutdown wiring once lifecycle and backend semantics have concrete evidence.
- The shape of general higher-order row inference once the small compiler reveals its pressure points.
- Runtime/source correlation beyond current bounded scope snapshots.
- Full named host values, methods, callbacks, codecs and wire migration policy after closed data and provider construction are exercised.
- Bounded streams, replay/ACK accounting, database transactions/outbox delivery and managed subprocess fixtures after portable data, concurrency and interop foundations establish their seams.
- Incremental package checking, reusable host-import sessions, comment-preserving formatting and verified edit plans after the larger source model is established.

## Out of scope

Kernels, hard real-time guarantees, freestanding/no-GC execution and arbitrary unchecked FFI remain outside the server-language destination. No claim of complete ownership checking, universal host compatibility or production completeness follows from a staged foundation receipt.
