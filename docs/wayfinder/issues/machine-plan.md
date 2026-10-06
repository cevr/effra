<!-- {"id": "machine-plan", "title": "Canonical machine plans and transition checks", "status": "open", "labels": ["implementation:task"], "parent": "state-machines", "assignee": null, "blocked_by": ["native-interfaces"]} -->
# Canonical machine plans and transition checks

Implement the checked flat-machine declaration over ordinary ADTs/function rows, bounded coverage, pure guards, explicit ignore/reject/re-entry and source-bound possible graphs. Two unrelated programs and every negative acceptance in the [spec](../../specs/state-machines.md) must pass. Unsupported advanced constructs diagnose; no whole-program reachability inference or new opaque callbacks.
