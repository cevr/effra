<!-- {"id": "stdlib-shared-platform", "title": "Shared producers, pools and managed platform clients", "status": "open", "labels": ["implementation:task"], "parent": "map", "assignee": null, "blocked_by": ["stdlib-flow", "stdlib-services"]} -->
# Shared producers, pools and managed platform clients

Implement Shared/platform in [bundled library capability contracts](../../specs/standard-library-capabilities.md). Deliver single-flight Memo, keyed Cache with a required capacity choice ([budget choices](../../specs/standard-library-capabilities.md#budget-choices)), scoped shared leases and fixed Pool; then scoped file utilities, process and HTTP clients.

Gate independent producer/waiter ownership, last-waiter abandonment, explicit failure replay and success expiry, lease release, actual body/child cleanup and target diagnostics. Include generic credential, keyed-connection and subprocess fixtures. Research process-group/reaping semantics before promising descendant termination; a signal request is not an observed exit. Keep fixed policies simple and require actual evidence before unifying all sharing structures behind one abstraction.
