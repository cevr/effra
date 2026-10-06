<!-- {"id": "semantic-types", "title": "Complete canonical type and binding inspection", "status": "open", "labels": ["implementation:task"], "parent": "map", "assignee": null, "blocked_by": ["native-interfaces", "semantic-diagnostics"]} -->
# Complete canonical type and binding inspection

Implement the [full type contract](../../specs/semantic-tooling.md) from canonical checked types: bounded referenced definitions, data fields/variants, callable rows, provider contracts and available ownership/trust evidence. Add focused CLI/MCP type and declaration queries, including actual lexical bindings. No recursive display-string analyzer or fabricated proof. Gate public parity, bounded sharing, shadowing and unavailable-source cases before independent review.

Qualify reusable semantic facts with compiler/producer identity as well as source revision, target and schema. Source/import digests intentionally remain identical across some compiler repairs; tests must prevent facts from a different producer being reused as current. Keep identity computation outside per-query checking costs.
