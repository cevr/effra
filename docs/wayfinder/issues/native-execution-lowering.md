<!-- {"id": "native-execution-lowering", "title": "Direct native execution and matched build-cost receipts", "status": "open", "labels": ["implementation:task"], "parent": "native-server-spec", "assignee": null, "blocked_by": ["native-interfaces"]} -->
# Direct native execution and matched build-cost receipts

Implement the [finite native lowering contract](../../specs/native-execution-lowering.md) using the canonical distinction between recipe construction and execution. The retained long-body diagnostic and Go compiler profile identify a concrete factory/immediate-invocation cost; they are not performance superiority evidence.

Preserve lazy values, evaluation order, providers, cancellation, cleanup and all checked contracts. Compare general lowering strategies with identical runtime/dependency controls, stage-separated build receipts, exact generated artifacts and explicit cache/host conditions. Full conformance, gates, review and a repeatable matched improvement precede closure. Keep the historical package/cache HITL ticket separate.
