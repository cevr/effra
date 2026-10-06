<!-- {"id": "foundation-ownership", "title": "Reject proven closed-owner handle escape", "status": "open", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": "provider_capture", "blocked_by": ["foundation-data", "foundation-providers"]} -->
# Reject proven closed-owner handle escape

## Question

Track ownership provenance and reject proven inner-owned File/Fiber escapes, including data payloads and deferred captures, while allowing borrowed outer handles. Unknown summaries remain explicit; do not claim complete borrow checking.

## Implementation underway

Implementation starts from integrated data/provider tip `163040c`; predecessor receipts remain under independent review. Bounded provenance follows recipe captures separately from invocation-owned acquisitions and preserves borrowed outer handles. Unknown summaries stay explicit.

## Remote independent review

The independent review of `d2a5a6a` on 2026-10-06 blocks integration. Repairs must cover nested enum projection, execution-owned deferred results and fork results, recovery fallback provenance, field-sensitive helper summaries, bounded structural traversal and dependency-sensitive summary computation. Public source probes demonstrated missed closed-owner errors; existing aggregate tests did not isolate each acceptance case. Keep the task open until repaired source probes and independent review pass.
