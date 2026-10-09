<!-- {"id": "js-control-flow-lowering", "title": "Direct JavaScript control-flow lowering with preserved effects", "status": "open", "labels": ["wayfinder:task", "implementation:task"], "parent": "native-execution-lowering", "assignee": "cevr", "blocked_by": ["foundation-data", "syntax-traversal"]} -->

## Question

Lower checked JavaScript `if` and `match` directly in their enclosing function or generator where statement/tail position permits, preserving the same contracts and behavior.

## Acceptance

- Remove unnecessary nested generator or closure wrappers without changing once-only subject/operand evaluation, source order, branch laziness, bindings, failures, services, ownership or cleanup.
- Reuse checked match and shared lowering facts. Use tag-switch dispatch only when the supported checked shape proves it; retain first-match semantics and the checked condition path for general or multiple-subject patterns.
- Preserve direct self-tail lowering, native Go behavior, scope/autoScope ownership, and foreign JavaScript object identity and mutation. Nullary singleton sharing is a separate contract question and is not admitted by this task.
- Use meaningful target-paired behavioral and deterministic output controls, including causal controls detecting restored redundant wrappers or moved/duplicated effects. Reconcile labelled/defaulted self-tail calls and signed arithmetic on the actual combined integration parent.
- Document pinned MoonBit, Gleam, ReScript, TypeScript/Effect and ordinary Go comparisons. Full logical gates and independent immutable review precede integration. Performance and size claims require later matched measurement; benchmark work remains last.

## Current work

The finite direct-self-tail source prerequisite and its corrected attribution are locally reviewed and accepted. A GPT-6 Luna/max implementation and paired GPT-6.1 Sol/max reviewer are preparing this separate source candidate in parallel with numeric and map work. This is an AFK implementation task, not an accepted optimization or performance result. Public synchronization of local source and receipts is pending; the broader direct-native execution parent remains open.
