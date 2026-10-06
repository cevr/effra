<!-- {"id": "binary-reachability", "title": "Reachable runtime modules and small executable receipts", "status": "open", "labels": ["implementation:task"], "parent": "native-server-spec", "assignee": null, "blocked_by": ["native-interfaces"]} -->
# Reachable runtime modules and small executable receipts

Implement the owner-established [small executable contract](../../specs/binary-reachability.md): select reachable runtime/library modules, keep unused imports/initialization out, preserve static lowering for fluent calls, and retain callback/provider/codec roots soundly. Ship minimal and managed-effect size/dependency checks; extend the same matrix with codec/HTTP consumers as their tasks land. Report application size separately from the compiler distribution and JS external runtime.

This is a compiler/library boundary requirement, not permission to weaken lifetime or error guarantees. Full gate, raw byte/symbol/dependency receipts and independent review required before closure.
