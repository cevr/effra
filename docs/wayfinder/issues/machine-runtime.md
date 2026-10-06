<!-- {"id": "machine-runtime", "title": "Owned actor execution on Go and JavaScript", "status": "open", "labels": ["implementation:task"], "parent": "state-machines", "assignee": null, "blocked_by": ["machine-plan", "foundation-testing", "foundation-ownership"]} -->
# Owned actor execution on Go and JavaScript

Implement the reusable actor runtime for checked plans. Bound admission/control delivery, serialize events, reject self-call deadlocks, fence stale entry completions and await cleanup before stable-state/terminal publication. Shared target tests cover the [spec](../../specs/state-machines.md), including exact composite causes, stop waiter interruption and logical time. Full gate/race review required.
