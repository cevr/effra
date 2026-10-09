<!-- {"id": "machine-provider-contract", "title": "Runtime-independent machine plans and userland provider contract", "status": "open", "labels": ["wayfinder:task", "implementation:task"], "parent": "state-machines", "assignee": null, "blocked_by": ["machine-plan"]} -->
# Runtime-independent machine plans and userland provider contract

## Question

What ordinary, typed userland provider contract makes a checked machine plan executable by another runtime without compiler/parser changes, while retaining its state/event/output identity, failure/service rows, entry lifetimes and inspection model?

## Required acceptance

- The language construct binds ordinary initial, step, entry and completion functions into a canonical checked plan. Machine-specific checking preserves transitions, nominal identity and public contracts; inspection never executes behavior.
- Provider selection and execution use ordinary library/function/service ownership, following the existing non-parser ownership of `.catch` and `.provide`. No hardcoded default-runtime dispatch, unchecked JavaScript passthrough, erased requirements or machine-only actor definition.
- Specify and implement the plan/provider boundary using finite supported language contracts. The plan exposes the facts and typed callable values a provider needs; no provider must introspect compiler ASTs or reach private builtin execution hooks.
- Keep compiler-proved declaration facts distinct from provider runtime obligations. The planned first Go/Effect-JS provider must satisfy owned actor, bounded admission, serial evaluation, entry-epoch fencing, cleanup-before-commit and exact cause contracts. The [lawful runtime contract](../../research/lawful-runtime-contract.md) defines the shared obligation categories and receipt fields; this ticket consumes provider obligations without inventing a machine-specific runtime law vocabulary.
- Define the typed boundary and adoption acceptance for two unrelated machines and two userland providers on the same target. Actual machine execution is verified by the separate provider-conformance unit after checked-plan lowering and the first runtime; this contract unit does not duplicate those dependent implementations.
- Retain negative controls for missing requirements, incompatible state/event/output identity, unsupported bindings and contract-erasing providers. Full gate and independent review bind the actual public usage to immutable source and execution evidence, including revision/target/fixture-bound runtime receipts where RS1/RS2 obligations are exercised.

## Scope

This establishes the provider boundary, not advanced statecharts, durable execution or external transactional rollback. Checked-plan lowering, the first owned runtime and cross-provider lifecycle conformance are separate dependent implementation tickets; acceptance of this contract does not claim those implementations. The owner's 2026-10-08 instruction is the requirement; the exact provider API remains work to implement and validate.
