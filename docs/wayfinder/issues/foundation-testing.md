<!-- {"id": "foundation-testing", "title": "Causal synchronization and scheduler-backed tests", "status": "open", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": "scheduler_finish", "blocked_by": []} -->
# Causal synchronization and scheduler-backed tests

## Question

Implement the testing clauses of [the foundation spec](../../specs/production-foundations.md): a portable one-shot signal and scheduler-backed test time shared by sleep/deadline operations; preserve real watchdog separation.

## Review history

Implementation31dd137 passes its Linux gate/race/public probe receipts, but independent review blocks integration: a partial time advance can deadlock when scope cleanup sleeps beyond the requested target, and repeated join/deadline probes expose a quiescence-check/advance race. Preserve cleanup publication ordering while distinguishing runnable continuations from genuinely suspended cleanup. Add isolated public regressions before closure.

Claude2 counsel round1 independently confirms both and adds intermediate-timer causality (a later due timer overtakes runnable earlier completion), timeout/latch variants of partial cleanup, and JS advance before an admitted fork registers its timer. Consolidated repair must distinguish runnable from parked continuations at every time selection, drain JS before its first selection, preserve exact cleanup causes and document managed-only observation. Report and exact public probes: `/tmp/effra-claude2-scheduler-review.md` on the workbox; the permanent acceptance belongs in tests/specs, not only this local receipt.

## Integration receipt

Reviewed repairs through ac8f1e2 and the permanent fast-first join regression f03f340 are integrated at cf3783c. Branch/integration gates and the integrated full Go race suite pass; the public five-case causal example passes Go and JS with the same semantic revision. Independent review covers partial cleanup, both join orders, nested timeouts, already-closing work and composite causes. The final transferred checkout is cleanly retired with all Git refs preserved in a verified local bundle.

One follow-up guardrail batch remains before closure: zero-value native Latch handles currently have inconsistent invalid-handle behavior, and two generated-JS test fixtures assume dist already exists. Fix4a27f3b passes a fresh-output full gate and focused races; it is under independent counsel. These findings do not reopen the reviewed continuation algorithm.
