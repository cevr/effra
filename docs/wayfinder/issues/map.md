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

## Autonomous implementation goal

On 2026-10-06 the owner explicitly set the goal to fully implement the authorized Wayfinder specifications and extend them as evidence reveals ergonomic improvements, stronger guarantees and performance opportunities. Continue across review and implementation units without waiting for routine human confirmation. Read the standing principles before any genuine question.

Use upstream implementation/test evidence, real usage, independent counsel and measured results to add concrete acceptance clauses and task dependencies. Prioritize changes that make the standard library reusable, preserve explicit inspectable contracts and improve measured costs. Record new opportunities with a finite supported scope and executable exit criteria; do not convert an aspiration into an implemented guarantee. Existing HITL decision tickets remain separate and require actual owner feedback before closure. No push is authorized.

## Decisions so far

- [Which production patterns constrain the next foundation contracts?](research-foundations.md): timer scheduling, dependency capture and nested ownership set the foundation acceptance seams.

## Additional implementation

- [Bound shared syntax traversal](syntax-traversal.md): resolved at45b43d2 with canonical child visits across formatter, lint, diagnostics, graph, type queries and test admission. Permanent deep CLI/MCP queries retain their facts and service a queued ping; integration gates pass.

- [Canonical formatter](formatter.md), followed by [editor formatting](editor-formatting.md): one comment-preserving printer for `ef fmt`, CI checks and revision-bound MCP/LSP previews, independent of typechecking and backend execution.

- [Versioned custom lint rules and shared rule packs](custom-lint.md): user-authored policies consume canonical facts and report through CLI/MCP/LSP, with options, source-fixture tests and optional bounded execution independent of ordinary compilation.

- [Shared editor-grade diagnostics](semantic-diagnostics.md), [complete canonical types](semantic-types.md), and a [stdio language server](language-server.md): owner-requested warnings/errors and full semantic inspection across CLI, MCP and editor, with one checked snapshot model.

- [Matched server performance against TypeScript and optimized Go](server-benchmarks.md): owner-requested server equivalence, throughput/latency, resource, startup/shutdown and separate build receipts. Performance superiority remains a measured question.

- [Bundled standard library capability and language contracts](stdlib-parity.md): owner-directed Effect-grade library distribution, with compiler-enforced semantics, ordinary inspectable library contracts and staged behavioral parity.

- [Native codecs and typed server contracts](native-server-spec.md): reusable bundled interfaces, typed function values, checked codecs, managed endpoints and compatible unary RPC precede actual framework measurements.

- [Pinned upstream tests and executable conformance mapping](upstream-conformance.md): licensed reference corpus plus explicit behavior-to-Effra test mapping; copied tests do not count as native passes.

- [Checked machines and owned actors](state-machines.md): owner-requested state-machine primitive over ordinary ADTs, explicit pure or effectful steps, owned invocation and shared graph inspection. Flat states first; advanced statecharts/durability remain unsupported until specified.

- [Reusable generic data and product matching](generic-data.md), followed by [payload-aware recovery](effect-recovery.md): ordinary Option/Result, finite generic data, exhaustive state/event decisions and typed outcome conversion shared by codecs and machines.

- [Reachable runtime modules and small executable receipts](binary-reachability.md): small binaries with unused standard-library facilities excluded, including through fluent APIs; module/import/symbol and raw size evidence accompany implementation.

- [Direct native execution and matched build cost](native-execution-lowering.md): the retained long-body compiler profile motivates a general lowering repair after canonical function contracts, preserving laziness and managed execution while measuring backend cost separately.

## Foundation implementation receipts

- [Causal synchronization and scheduler-backed tests](foundation-testing.md): reviewed native/JS managed scheduling, partial cleanup, continuation ownership and both join orders, plus consistent invalid latch boundaries. Integrated full gates, race checks and portable public cases pass; foreign operations remain outside virtual quiescence guarantees.

- [Reasoned semantic lint suppression](foundation-lint.md): named next-line advice suppression requires a reason; malformed, unknown and unused directives diagnose without disabling compiler checks.
- [Closed application data and exhaustive interpretation](foundation-data.md): records, payload enums, structured failures and exhaustive matching execute on Go/JS and share inspectable canonical declarations; independent review repairs are integrated.

- [Dependency-capturing provider composition](foundation-providers.md): checked configuration and lexical dependency capture retain the invocation owner; recipes, materialized values and constructor origins are inspectable in the shared graph.

## Not yet specified

- The server slice's HTTP/database boundary and startup/shutdown wiring once lifecycle and backend semantics have concrete evidence.
- The shape of general higher-order row inference once the small compiler reveals its pressure points.
- Runtime/source correlation beyond current bounded scope snapshots.
- Full named host values, methods, callbacks, codecs and wire migration policy after closed data and provider construction are exercised.
- Bounded streams, replay/ACK accounting, database transactions/outbox delivery and managed subprocess fixtures after portable data, concurrency and interop foundations establish their seams.
- Incremental package checking, reusable host-import sessions and general verified edit plans after the larger source model is established. Canonical formatting now has the finite implementation contract above.

## Out of scope

Kernels, hard real-time guarantees, freestanding/no-GC execution and arbitrary unchecked FFI remain outside the server-language destination. No claim of complete ownership checking, universal host compatibility or production completeness follows from a staged foundation receipt.
