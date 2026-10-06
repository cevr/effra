<!-- {"id": "foundation-data", "title": "Closed application data and exhaustive interpretation", "status": "closed", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": "closed_data", "blocked_by": []} -->
# Closed application data and exhaustive interpretation

## Question

Implement the data-model clauses of [the foundation spec](../../specs/production-foundations.md): nominal records, closed payload variants, exhaustive matching, payload failures and inspectable declaration metadata on Go/JS.

## Resolution

Implemented canonical type/declaration metadata, nominal records and payload enums, exhaustive matching, structured failures, and typed Go/JS emission. Integration `49778be` includes implementation commits `ecbcea4`, `b31a8ae`, `5201606`, `e7bf8e6`, and `d807751`. Full gate, race suite, both-target fixture execution, stdio inspection, and strict TypeScript nominal checking passed. Unsupported recursive layouts, ordinary error-valued success types, and JS declaration name collisions diagnose explicitly.
