<!-- {"id": "typescript-declarations", "title": "TypeScript declaration projection and reverse imports (VW3)", "status": "open", "labels": ["wayfinder:task", "implementation:task"], "parent": "host-interop", "assignee": null, "blocked_by": ["native-interfaces", "semantic-types"]} -->
# TypeScript declaration projection and reverse imports (VW3)

Implement the finite TypeScript interop bridge from the canonical Effra contract. Generated declarations and the existing strict consumer fixtures are useful seams; TypeScript host imports, declaration maps and reverse querying remain future work and are not current support.

## Acceptance

- Project checked Effra values, nominal identities, callable success/failure/service rows and target restrictions into declaration-only `.d.mts`/map outputs with stable source spans. Unsupported representation, ownership or row facts diagnose instead of becoming `any`.
- Add a separate reverse-import bridge using the TypeScript API to resolve selected `.ts`/`.d.ts` declarations and expose a bounded structured view to Effra checking and inspection. The content mapper may provide source correspondence, but it does not call back into itself or supply Effra semantics.
- Preserve configuration, compiler/producer, package and source identities in cache and diagnostic receipts. Go-only builds must not start TypeScript. Real strict consumers cover payload variants, opaque/foreign values, `bigint`, Effect-valued calls and changed dependency declarations.
- Keep runtime validation, cancellation, ownership and behavioral trust explicit; declaration shape is an input, not proof. Full gate, target checks and independent review precede any adoption claim.

This is VW3 from the reviewed view/interop work. It does not implement client lifetimes or JSX notation; those have separate tickets.
