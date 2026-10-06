<!-- {"id": "research-foundations", "title": "Which production patterns constrain the next foundation contracts?", "status": "closed", "labels": ["wayfinder:research"], "parent": "map", "assignee": "foundation_audit", "blocked_by": []} -->
# Which production patterns constrain the next foundation contracts?

## Question

Audit the reviewed production patterns against the current compiler/runtime, emphasizing provider capture, test scheduling, closed application data and owned handle escape. Record generic acceptance scenarios outside the repo and a concise public resolution; do not reproduce private application code.

## Resolution comments

2026-10-05 — Source audit established four constraints: sleep and timeout need one inherited timer driver; provider captures must retain dependency values while using the invocation owner; data payloads/function summaries must preserve ownership provenance; deterministic time advancement needs registration/readiness barriers. Existing documented timeout requirements also need reconciliation with checking. These are acceptance constraints, not new implementation receipts. Applied in [production foundation contracts](../../specs/production-foundations.md).
