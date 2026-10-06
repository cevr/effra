<!-- {"id": "foundation-data", "title": "Closed application data and exhaustive interpretation", "status": "closed", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": null, "blocked_by": []} -->
# Closed application data and exhaustive interpretation

## Question

Implement the data-model clauses of [the foundation spec](../../specs/production-foundations.md): nominal records, closed payload variants, exhaustive matching, payload failures and inspectable declaration metadata on Go/JS.

## Integration receipt before independent review

Implemented canonical type/declaration metadata, nominal records and payload enums, exhaustive matching, structured failures, and typed Go/JS emission. Integration `49778be` includes implementation commits `ecbcea4`, `b31a8ae`, `5201606`, `e7bf8e6`, and `d807751`. Full gate, race suite, both-target fixture execution, stdio inspection, and strict TypeScript nominal checking passed. Unsupported recursive layouts, ordinary error-valued success types, and JS declaration name collisions diagnose explicitly.

## Review reopened

Independent review found admitted match-row, constructor/payload and Go field-mangling cases that need regression tests and repair before this task resolves. Passing existing gates is preliminary evidence, not completion of those acceptance clauses.

## Final resolution

Integrated review repairs `2613227`, `7040c5e` and `ccfce95` at `163040c`. Independent review verified all original findings, nested nominal identity, empty-enum elimination and multi-field control shorthand. The integrated full gate and Go race suite passed. Both-target data fixtures, declaration inspection and strict TypeScript checks passed. Recursive layouts and unsupported success-channel error values remain explicit diagnostics; this does not supply external codecs or generic collections.
