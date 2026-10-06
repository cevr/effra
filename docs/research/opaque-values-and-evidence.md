# Opaque values and checked evidence

Status: design candidate, 2026-10-06. The owner asked whether value-specific evidence should be built into the language with a Go-like surface. This note records the comparison and a proposed sequence; it does not claim new syntax or guarantees are implemented, or expand the frozen canonical-type repair.

## Source comparison

[GDP source](https://github.com/rauchg/gdp-ts/blob/ebd0af9cae423997a43a024dc6d6738b0895bbec/src/index.ts) distinguishes value kinds from individual named subjects. Its generic callbacks introduce scoped, invariant phantom names. Trusted modules mint evidence about those names, and sensitive functions require matching evidence. Runtime named values are frozen wrappers; proof construction reuses a frozen object per kind. Compiler erasure in Effra is an opportunity, not an inherited zero-cost result.

Its [type tests](https://github.com/rauchg/gdp-ts/blob/ebd0af9cae423997a43a024dc6d6738b0895bbec/test/types.ts) cover wrong subjects, escaped names, missing evidence, swapped arguments, policy alternatives and existential evidence. Its [lint configuration](https://github.com/rauchg/gdp-ts/blob/ebd0af9cae423997a43a024dc6d6738b0895bbec/src/lint/shared.ts) restricts assertions and evidence minting. Effra should enforce any claimed language invariant in the compiler, independent of optional lint. The trusted decision procedure still requires review and tests. These tests were read, not executed or ported.

At the runtime pin, [Effect Brand](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Brand.ts) distinguishes unchecked nominal constructors from constructors that apply checks. [Schema](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Schema.ts) adds nominal output identity with `brand`, while refinement adds runtime checks. Neither nominal identity alone nor a decoded identifier establishes an authorization relationship.

## Proposed common surface

Start with ordinary opaque values and module-owned construction. A policy operation returns an authorized value containing the exact immutable subject identifiers it checked. A sensitive operation consumes that value instead of accepting a separate raw target plus independent evidence.

```effra
// Proposed API, not an executable current-language example.
let access = run projects.authorizeManage(user, project)
run projects.rename(access, "New name")
```

`authorizeManage` has ordinary explicit failures and service requirements. Its module controls construction of the result, and `rename` operates on the contained project and checked actor. Both fields matter when audit attribution depends on the actor. Avoid a separate actor or project parameter that can silently disagree. A missing authorization step becomes an ordinary argument-type error.

Use the same underlying construction boundary for distinct scalar identifiers and validated domain values. Distinct identifiers need nominal separation; validated values additionally need a constructor that actually checks their invariant. A codec for a validated domain value invokes that constructor after structural decoding. Directional encode/decode rows remain explicit. Structural derivation, zero/default initialization, record reconstruction and foreign conversion must not manufacture validation or authorization evidence.

This package of subject and authority is less general than independently composable value-indexed proofs. For two checks, combining authorized values for different projects requires explicit subject equality validation unless the checker has a supported shared identity. Do not pretend ordinary nominal records prove that relation. Add generative identities only after concrete callers need independently composed evidence or facts such as membership in a particular immutable collection. Keep any such mechanism finite and inspectable; no arbitrary proposition solver or whole-program inference.

## Required boundaries before implementation

- Construction is module-private and nominal identity is package-qualified. No structural literal, cast, raw zero value, derived decoder or unchecked host import may forge a checked value. Foreign adapters that assert guarantees remain explicit trust boundaries. Exported host APIs need their own checked boundary; target-language privacy alone does not establish this contract.
- Payloads used to justify evidence are immutable or snapshot/version-bound. Read-only fields around an aliased mutable object are insufficient. Arithmetic or transformations do not automatically preserve a validated invariant.
- Policy evidence records a successful decision; it does not prove the decision code correct or permission current forever. A lexical/request scope bounds use but cannot prevent external revocation races. Transactions, versions or revalidation establish stronger freshness separately. See the upstream [limits](https://github.com/rauchg/gdp-ts/blob/ebd0af9cae423997a43a024dc6d6738b0895bbec/skills/gdp-ts/references/limits.md).
- Authority remains an explicit value. A global service named `Authorized` cannot stand in for authority over an arbitrary resource. Reusable evidence is not automatically a single-use capability; consumption needs a separate linear protocol if required.
- Serialization does not transfer current authority. Decode raw data and reauthorize; cryptographically transferable tokens need an explicit verifier and policy. Ordinary stored domain values can have explicit codecs without serializing authority.
- Inspection explains the nominal declaration, construction authority, checked subjects and any supported owner/version restriction. It labels trusted predicates rather than presenting a proved business theorem. Diagnostics should identify the mismatched subject directly if value indexing is later admitted.
- Pure phantom information can be erased only with evidence that both targets preserve the contract. Keep actual actor/resource/version data, checks and observable ADT tags as needed. Measure frontend cost and emitted binary retention; do not introduce a mandatory runtime proof registry.

Acceptance should include two unrelated uses: a validated configuration/domain scalar and an authorized resource operation. Negative source programs cover direct construction, wrong actor/resource, raw conversion, derivation bypass, changed payload and foreign re-entry. Go/JS behavior and CLI/MCP facts must agree. More general value-indexed evidence remains a separate candidate until this smaller surface is established.
