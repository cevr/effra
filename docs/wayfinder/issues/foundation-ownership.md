<!-- {"id": "foundation-ownership", "title": "Reject proven closed-owner handle escape", "status": "open", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": null, "blocked_by": ["foundation-data", "foundation-providers"]} -->
# Reject proven closed-owner handle escape

## Question

Track ownership provenance and reject proven inner-owned File/Fiber escapes, including data payloads and deferred captures, while allowing borrowed outer handles. Unknown summaries remain explicit; do not claim complete borrow checking.
