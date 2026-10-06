<!-- {"id": "state-machines", "title": "Checked machines and machine-backed actors", "status": "open", "labels": ["implementation:spec"], "parent": "map", "assignee": null, "blocked_by": []} -->
# Checked machines and machine-backed actors

Owner-directed language primitive, 2026-10-06. Implement the finite [machine contract](../../specs/state-machines.md): ordinary state/event ADTs, checked transitions, owned invocation and shared graph inspection. Independent source/test research covers Effect Machine and XState. No durable/statechart parity is implied.

Owner refinement: machines provide one behavior form of [ordinary actors](ordinary-actors.md). Actor runtime/library delivery is independent of machine transition support; supervision, durable addressing and persistence apply to either behavior. Keep entry/transition guarantees in this specialization.

Children separate checked plans, runtime ownership and public conformance. Preserve explicit rows, compile bounds, Go/JS semantics and unchanged unapproved HITL decisions.
