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

Use upstream implementation/test evidence, real usage, independent counsel and measured results to add concrete acceptance clauses and task dependencies. Prioritize changes that make the standard library reusable, preserve explicit inspectable contracts and improve measured costs. Record new opportunities with a finite supported scope and executable exit criteria; do not convert an aspiration into an implemented guarantee. Existing HITL decision tickets remain separate and require actual owner feedback before closure. Writers never push. Owner scope, 2026-10-07: root may integrate accepted, fully gated fast-forward commits into `main` after review; that permission does not grant push authority to lane writers.

Owner update, 2026-10-06: defer benchmark development and measurement until the final phase, and increase independent implementation parallelism. Keep canonical type work, generated output ownership, upstream conformance import/mapping and [LSP diagnostics/document lifecycle](lsp-diagnostics.md) in isolated lanes. Their shared contracts determine integration order; parallel work does not bypass review or dependent feature gates.

Owner update, 2026-10-06: architecture loops must seek useful primitives making Effects explicit, declarative and delightful. Record production ceremony, compare regular alternatives and prove improvements with two unrelated runnable callers, visible contracts and negative controls. First-class layers are the next adopted case; source-only research informs acceptance without certifying runtime behavior.

Owner update, 2026-10-08: strong abstractions are a destination only when the
illegal states and invalid operations covered by their checked contract are
unrepresentable in checked Effra source, while foreign/trusted behavior remains
explicitly qualified, and they preserve the same validation, ownership,
cancellation and completed cleanup as explicit Go and TypeScript/Effect
controls. Record erasure/direct lowering/retention and measure residual
allocation, dispatch, checks, retained modules, executable bytes and runtime
cost under matched conditions; benchmark work remains last and no universal
zero-overhead claim is admitted.

Owner update, 2026-10-08: native compiled applications target performance at
least as good as optimized idiomatic Go under the same contract. The JS target
must pursue every material measurable lowering or specialization, including
static match dispatch and a generated/specialized effect runtime when useful,
while retaining the pinned default Effect-compatible ABI and userland contract.
Generated code may be machine-written; typed errors, service rows, ownership,
cancellation, scopes and completed cleanup remain unchanged. A many-times
speedup is an ambition to test, not a current result or universal multiplier.

This map indexes resolved decisions and retained implementation receipts. Open implementation work remains in the native parent, child and dependency graph; query it with `wayfinder frontier` instead of duplicating open-ticket lists here. Pragma/runtime selections must retain prior-art comparisons, NORTH_STAR checks, rejected alternatives and their evidence in the relevant spikes; the current JS ABI remains pinned Effect-compatible until a scoped provider decision says otherwise.

## Decisions so far

- [Does explicit run and contract inspection make the tiny language useful?](001.md): resolved narrowly from the owner's 2026-10-05 reaction to the public runnable prototype. The finite `.ef` slice was judged useful for Go-like simplicity and Effect-style guarantees, with more ADT/pattern showcases requested; backend/lifecycle/cost parity remains independently open.
- [Which production patterns constrain the next foundation contracts?](research-foundations.md): timer scheduling, dependency capture and nested ownership set the foundation acceptance seams.
- [Bundled standard library capability contracts](stdlib-parity.md): resolved the finite inventory/specification question through pinned source/tests, counsel and generic adoption patterns; implementation remains open in five batches.

- [Bound shared syntax traversal](syntax-traversal.md): resolved at `45b43d2` with canonical child visits across formatter, lint, diagnostics, graph, type queries and test admission. Permanent deep CLI/MCP queries retain their facts and service a queued ping; integration gates pass.
- [Canonical formatter](formatter.md): resolved through `19a450b` with one comment-preserving printer, bounded `ef fmt`/read-only MCP adapters and twelve authored examples checked by the gate. Independent review, exact-output replay and integration gates pass; editor formatting remains separately pending the language server.
- [Versioned LSP diagnostics/document lifecycle](lsp-diagnostics.md): integrated and reviewed at `6cbbed0` with real framed-process and full integration gates; full type/navigation/formatting capabilities remain pending the parent contract.
- [Pinned upstream tests and executable conformance mapping](upstream-conformance.md): integrated and reviewed at `62baa74` with a licensed immutable corpus, ten qualified behavior mappings and actual selected Go/JS acceptance. Copied tests do not count as native passes; later library/server tasks extend the same mapping.
- [Runtime source modules](runtime-modules.md): completed and integrated at `637313b` after the owned-output prerequisite; checked application reachability and size proof remain open in their native parent.
- [Isolated owned native generated modules](generated-output-ownership.md): completed and integrated at `2e23a894` with full gates and independent review. Complete immutable modules preserve legacy/unknown output and validate reuse; application reachability remains open.

## Foundation implementation receipts

- [Causal synchronization and scheduler-backed tests](foundation-testing.md): reviewed native/JS managed scheduling, partial cleanup, continuation ownership and both join orders, plus consistent invalid latch boundaries. Integrated full gates, race checks and portable public cases pass; foreign operations remain outside virtual quiescence guarantees.

- [Reasoned semantic lint suppression](foundation-lint.md): named next-line advice suppression requires a reason; malformed, unknown and unused directives diagnose without disabling compiler checks.
- [Closed application data and exhaustive interpretation](foundation-data.md): records, payload enums, structured failures and exhaustive matching execute on Go/JS and share inspectable canonical declarations; independent review repairs are integrated.

- [Dependency-capturing provider composition](foundation-providers.md): checked configuration and lexical dependency capture retain the invocation owner; recipes, materialized values and constructor origins are inspectable in the shared graph.

## Not yet specified

- The server slice's HTTP/database boundary and startup/shutdown wiring once lifecycle and backend semantics have concrete evidence.
- The shape of general higher-order row inference once the small compiler reveals its pressure points.
- Runtime/source correlation beyond current bounded scope snapshots.
- TypeScript host values/declaration consumption, broader foreign generic support and wire migration policy beyond the finite Go protocol and codec contracts.
- General transactional-program admission, durable replay/ACK accounting and database transactions/outbox delivery beyond the finite bounded-flow and managed-platform contracts.
- Incremental package checking, reusable host-import sessions and general verified edit plans after the larger source model is established. Canonical formatting now has the finite implementation contract above.

## Out of scope

Kernels, hard real-time guarantees, freestanding/no-GC execution and arbitrary unchecked FFI remain outside the server-language destination. No claim of complete ownership checking, universal host compatibility or production completeness follows from a staged foundation receipt.
