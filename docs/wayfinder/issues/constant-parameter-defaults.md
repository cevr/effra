<!-- {"id": "constant-parameter-defaults", "title": "Constant-only parameter defaults and explicit call contracts", "status": "open", "labels": ["wayfinder:task", "implementation:task"], "parent": "map", "assignee": "cevr", "blocked_by": ["formatter", "syntax-traversal"]} -->

## Question

Implement the owner's admitted constant-only parameter defaults as checked call-site sugar, preserving explicit argument evaluation and honest public contracts.

## Acceptance

- Admit literals and named constants; refuse calls, effects and parameter-dependent evaluation. An omitted argument lowers to the same ordinary checked call as its explicit constant argument.
- Preserve named/positional rules, once-only evaluation, pipes, generic substitution, imported callable identity, failure/service rows and target restrictions. Budgets and codec bounds remain explicitly required, enforced by checked owner roles rather than parameter-name heuristics.
- Expose defaults in canonical signatures, interface identity, inspection and formatting. A changed default is a public contract change. Generated TypeScript must describe the actual callable ABI; call-site sugar must not falsely promise runtime optional arguments.
- Prove two unrelated callers on Go/JavaScript, meaningful refusals, explicit/desugared equivalence, strict TypeScript consumption and stale-contract controls. Document Go, TypeScript/Effect and MoonBit/ReScript comparisons with primary evidence.
- Complete compiling logical units, full gates and independent review before integration. Source-only equivalence is not measured speed, size or universal zero overhead.

## Current work

The earlier owner decision already admits this notation. Its named-argument prerequisite is present in the locally accepted compiler. A GPT-6 Luna/max implementation and paired GPT-6.1 Sol/max reviewer are working in an isolated source lane, parallel to other dependency-ready work. This is an AFK implementation task; source and combined integration remain unaccepted. Public synchronization of local source and receipts is pending. No `.efi` implementation or broader optional-parameter semantics is claimed here.
