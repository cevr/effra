<!-- {"id": "host-resources", "title": "Owned Go resource borrowing and cancellation contracts", "status": "open", "labels": ["implementation:task"], "parent": "host-interop", "assignee": null, "blocked_by": ["host-protocols", "stdlib-owned-core"]} -->
# Owned Go resource borrowing and cancellation contracts

Unit3 of the [Go protocol contract](../../specs/go-protocol-interop.md): managed/native File borrow/adopt seam, one release authority across admitted aliases, retained/deferred escape constraints, explicit context/deadline/close behavior and scoped descriptors. Local files and net.Pipe establish causal cancellation/cleanup controls; borrowed streams remain borrowed. Full gate, native race tests and independent review required.
