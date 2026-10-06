<!-- {"id": "effect-recovery", "title": "Payload-aware typed recovery and outcomes", "status": "open", "labels": ["implementation:task"], "parent": "native-server-spec", "assignee": null, "blocked_by": ["native-interfaces", "generic-data"]} -->
# Payload-aware typed recovery and outcomes

Implement [language abstractions](../../specs/language-abstractions.md)'s named recovery callbacks: bind the handled nominal failure payload, eliminate exactly that failure and add the callback's checked success/failure/service contract. Ordinary Result data remains separate from executing a recipe. Preserve full composite Cause; cleanup defects cannot become a recoverable typed failure.

Use a codec conversion and machine-work-to-outcome conversion as unrelated callers. Assert missing services, wrong payload/result types, undeclared errors, borrowed/new owner provenance, cancellation cleanup and Go/JS inspection parity. Reified Exit, if exposed, carries the complete Cause rather than a flattened four-way classification. No new error-handling sub-language is required.
