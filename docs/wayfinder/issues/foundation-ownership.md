<!-- {"id": "foundation-ownership", "title": "Reject proven closed-owner handle escape", "status": "closed", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": "provider_capture", "blocked_by": ["foundation-data", "foundation-providers"]} -->
# Reject proven closed-owner handle escape

## Question

Track ownership provenance and reject proven inner-owned File/Fiber escapes, including data payloads and deferred captures, while allowing borrowed outer handles. Unknown summaries remain explicit; do not claim complete borrow checking.

## Implementation underway

Implementation starts from integrated data/provider tip `163040c`; predecessor receipts remain under independent review. Bounded provenance follows recipe captures separately from invocation-owned acquisitions and preserves borrowed outer handles. Unknown summaries stay explicit.

## Remote independent review

The independent review of `d2a5a6a` on 2026-10-06 blocks integration. Repairs must cover nested enum projection, execution-owned deferred results and fork results, recovery fallback provenance, field-sensitive helper summaries, bounded structural traversal and dependency-sensitive summary computation. Public source probes demonstrated missed closed-owner errors; existing aggregate tests did not isolate each acceptance case. Keep the task open until repaired source probes and independent review pass.

The first repair commits `e8b2147`/`668e5a9` pass gates and the original reproducers. Rereview still blocks integration: root parameter projection can erase proof, deferred helpers can incorrectly rebind already materialized borrowed handles, and primitive-only shared record graphs bypass the traversal budget. These are distinct public acceptance cases, not reasons to weaken the ownership contract.

## Reviewed integration

Resolved at `a9da03a` on 2026-10-06. Independent review cleared the final bounded-domain repair `ceda2c2`, including overlapping joins, widening exclusions, deferred parameter substitution and nested source coordinates. The integrated checker retains shared syntax traversal, `raises` syntax and explicit Scheduler requirements. Both branch and combined repository gates pass.

Exact clean integrated-binary probes reject an inner acquisition escaping through a two-field selection helper with EF123, admit the same helper over two borrowed outer handles, and execute the public ownership example. Raw integrated receipts are `/tmp/effra-ownership-integrated-gate.log` and `/tmp/effra-ownership-integrated-live.json`; independent law and public-source review is retained in the architecture-loop cache as `ownership-substitution-review.md`.

This closes the finite File/Fiber provenance foundation, not general borrow checking. Bounded analysis may conservatively reject potential ownership when it cannot establish safety; foreign behavior remains explicit. Typed callback provenance belongs to the following native-interfaces task and must preserve these distinctions.
