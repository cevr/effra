<!-- {"id": "foundation-testing", "title": "Causal synchronization and scheduler-backed tests", "status": "open", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": "scheduler_finish", "blocked_by": []} -->
# Causal synchronization and scheduler-backed tests

## Question

Implement the testing clauses of [the foundation spec](../../specs/production-foundations.md): a portable one-shot signal and scheduler-backed test time shared by sleep/deadline operations; preserve real watchdog separation.

## Independent review in progress

Implementation31dd137 passes its Linux gate/race/public probe receipts, but independent review blocks integration: a partial time advance can deadlock when scope cleanup sleeps beyond the requested target, and repeated join/deadline probes expose a quiescence-check/advance race. Preserve cleanup publication ordering while distinguishing runnable continuations from genuinely suspended cleanup. Add isolated public regressions before closure.

Claude2 counsel round1 independently confirms both and adds intermediate-timer causality (a later due timer overtakes runnable earlier completion), timeout/latch variants of partial cleanup, and JS advance before an admitted fork registers its timer. Consolidated repair must distinguish runnable from parked continuations at every time selection, drain JS before its first selection, preserve exact cleanup causes and document managed-only observation. Report and exact public probes: `/tmp/effra-claude2-scheduler-review.md` on the workbox; the permanent acceptance belongs in tests/specs, not only this local receipt.
