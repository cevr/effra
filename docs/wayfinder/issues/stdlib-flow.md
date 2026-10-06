<!-- {"id": "stdlib-flow", "title": "Bounded queues, streams, retry and drainable workers", "status": "open", "labels": ["implementation:task"], "parent": "map", "assignee": null, "blocked_by": ["stdlib-owned-core"]} -->
# Bounded queues, streams, retry and drainable workers

Implement Flow in [bundled library capability contracts](../../specs/standard-library-capabilities.md): bounded queue/subscriptions and owned pull first; then streams/sinks, finite retry and atomic drainable-worker admission.

Gate item/byte/in-flight budgets, cancelled waiter no-loss, buffered terminal causes, per-attempt cleanup, explicit Random/Scheduler contracts and drain-versus-enqueue races. Reuse actor queue mechanisms where sound without erasing reserved-completion or stale-entry actor policies. General optimistic user transactions remain unsupported until a sound admission design exists; service-row restrictions alone cannot exclude non-journal mutation.
