<!-- {"id": "machine-provider-conformance", "title": "Machine execution across independent userland runtime providers", "status": "open", "labels": ["wayfinder:task", "implementation:task"], "parent": "state-machines", "assignee": null, "blocked_by": ["machine-provider-contract", "machine-runtime"]} -->
# Machine execution across independent userland runtime providers

## Question

Can two independently authored userland runtime providers execute the same checked machine declarations and preserve their declared observable contracts without compiler changes or builtin runtime selection?

## Acceptance

- Execute two unrelated machine programs against the planned first provider and an alternative userland provider on the same target. Provider selection is ordinary typed code; declarations and compiler configuration are unchanged.
- Exercise pure and effectful steps, state/event/output identity, declared failures and service requirements, successful evaluation cleanup followed by entry cleanup before commit, Go reentry versus Stay retaining entry, and exact composite causes.
- Exercise bounded item/byte admission, reserved completion and stop controls, serialized evaluation, stale entry completion fencing, call-waiter cancellation without retracting admitted work, stop discard and awaited cleanup, and concurrent/cancelled stop waiters.
- Inspect canonical plans and snapshots through the shared CLI/MCP model without executing behavior. Refuse incompatible providers and missing requirements through normal language diagnostics.
- Retain actual target/host versions, strict TypeScript ABI consumers where applicable, causal negative controls, full gates and independent frozen review. Declare which [lawful runtime contract](../../research/lawful-runtime-contract.md) law rows each provider exercises, and record only tested, refuted or unresolved evidence bound to provider, target, fixture and revision; finite execution is never proof. Do not infer external transactional rollback, durability or exactly-once delivery.

## Dependencies

Requires the ordinary provider contract, checked machine plans and the first owned machine runtime. Each is separately completed before this execution proof; source sketches and copied test fixtures do not satisfy it.
