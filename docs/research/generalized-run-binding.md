# Generalized `run` binding: current boundary and research record

**Status:** owner-requested research only. This record does not admit syntax,
change the checker, or claim a law, performance, Go, or two-target result.

The hosted question is [Generalized run sequencing and checked userland binding](https://github.com/cevr/effra/issues/73).
Its former local mirror (`docs/wayfinder/issues/run-binding.md`), the
separate GET-backed reconciliation record
(`docs/wayfinder/hosted-run-reconciliation.json`) and the public JSON response
and receipt bytes it used (`docs/wayfinder/receipts/`) were retired with the
local tracker and remain at commit `43af514c` (see the
[Wayfinder archive](../wayfinder/ARCHIVE.md)). They bound issue 73, its map
membership, and the original 71-item verifier without claiming that the
accepted source subject has been published.
The source subject for this record is commit
`4627f414414cc964840b53894b7161482687de69`, tree
`ff2e8954a6063772b5fcd7dee9abc9929b58613c`. The original 71-item hosted
migration and its 183 native edges remain a separate accepted wave; this
research ticket is the 72nd canonical issue and adds one native parent edge.

## Question and boundary

The current language has an explicit `run` form. The question is whether a
checked userland binding form could sequence another computation family, such
as an absence-aware `Option`, a validation `Result`, or a third user-authored
family, while retaining Effra's public contracts. “Generalized” here means a
future checked rule over an explicitly selected family. It does not mean that
ordinary method names become syntax, that imports silently rebind the rule, or
that a runtime provider is selected by the binding family.

The accepted boundary is deliberately narrow:

| Concern | Current accepted behavior | Candidate to investigate |
| --- | --- | --- |
| Construct | `run` is checked for an `Effect` value in an effect function | A family-specific checked binding only if it has an Effra invariant ordinary calls cannot express |
| Selection | Effect is determined by the checked value and effect context | Explicit lexical or coherent nominal-family selection; no ambient import rebinding |
| Value | Construction is deferred; `run` yields the result | A family binder must preserve its value/absence/failure contract and explicit adaptation |
| Failure and services | Rows from the executed Effect reach the enclosing contract | Option/Result must not erase Effect failure or service rows; nested wrappers stay data |
| Ownership | The owner that executes a recipe receives execution facts | A continuation may not duplicate or escape a scope-owned resource |
| Runtime | The provider and invocation runtime are separate from the `run` spelling | Keep provider selection orthogonal to family selection |
| Targets | The same semantic model emits JavaScript/Effect and Go | Inspect shared desugaring before any target-specific admission |

The recommendation from this record is to retain explicit `flatMap` and
ordinary `Option`/`Result` calls until a concrete invariant, a third custom
family, two target comparisons, and causal controls exist. The compiler must
diagnose unsupported positions; it must not pass unsupported syntax through to
JavaScript.

## What the current compiler does

The checker handles `run` in the local accepted subject's
`internal/compiler/semantic.go` (lines 4121–4148; immutable subject
`4627f414414cc964840b53894b7161482687de69`, with public source publication
still pending).
It reports `EF105` outside an effect function and when the operand is not an
Effect, changes the checked value to the recipe result, materializes deferred
ownership and captures under the executing lexical owner, and contributes the
operand's failure and service rows to the enclosing contract. An unused lazy
Effect is rejected with the same explicit diagnostic rather than being silently
discarded.

The JavaScript emitter lowers the admitted form to a generator yield in the
local accepted subject's `internal/compiler/emit.go` (lines 393–395; the
immutable source subject is `4627f414414cc964840b53894b7161482687de69`, with
public source publication still pending). The Go emitter invokes the recipe
with the current `efContext`, checks its exit, and reads its value in the
local accepted subject's `internal/compiler/emit_go.go` (lines 542–546).
The Go provider boundary intentionally overlays captured service pointers while
retaining the invocation runtime, including cancellation and owner state
(`internal/compiler/emit_go.go`, lines 376–381, in that same local accepted
subject).
Those are current implementation facts, not evidence that another family is
already admitted.

The proposed shape is therefore a checked continuation rule, not a universal
`flatMap` alias:

```text
family.bind(value, x => rest(x))
```

`rest` is the remainder of the enclosing block. Its construction and
evaluation must remain lazy where the family is lazy, and its result must stay
in that family's explicit result type. A plain `let` remains an ordinary value
binding. A conversion between `Option`, `Result`, `Effect`, and a user family
must be written explicitly; no implicit Promise lift, unsafe dictionary, or
foreign HKT is part of the proposal.

## Primary-source comparisons

These sources provide mechanisms and counterexamples. They do not supply an
Effra implementation or prove that their laws, ownership, or scheduling
semantics transfer.

### Haskell `do`

The original hosted question pins the [Haskell 2010 do translation](https://www.haskell.org/onlinereport/haskell2010/haskellch3.html#x8-470003.14).
It translates a sequence into the selected monadic operations and binds the
continuation. This is useful as a continuation model and a warning that
notation alone does not state resource ownership, service rows, or cancellation.
The old report URL is retained as provenance; it is not replaced by a new
snapshot in this record.

### GHC `QualifiedDo`

The question's original immutable pin is
[`downloads.haskell.org/ghc/9.14.1/.../qualified_do.html`](https://downloads.haskell.org/ghc/9.14.1/docs/users_guide/exts/qualified_do.html).
That URL was unavailable during the 2026-10-08 refresh, so the current
comparison uses the official [GHC moving documentation](https://ghc.gitlab.haskell.org/ghc/doc/users_guide/exts/qualified_do.html)
as an explicitly different source. It confirms the relevant shape: a
qualifier selects the ordinary sequencing operations, the generated operations
must typecheck, nested qualified blocks select their own context, and an
explicit `return` or `(>>=)` remains the operation written by the source.
This supports explicit lexical selection and rejects an Effra design that
silently changes meaning through imports. The moving documentation is a
comparison, not a replacement for the unavailable 9.14.1 bytes.

### F# computation expressions

The official [F# computation-expression documentation](https://learn.microsoft.com/en-us/dotnet/fsharp/language-reference/computation-expressions)
shows a builder as the context and names `Bind`, `Delay`, `Return`,
`ReturnFrom`, `BindReturn`, and `Run` as separate hooks. `let!` uses `Bind`;
the builder controls delayed computation and final execution, while
`BindReturn` is an optional shape for a fused operation. The comparison is
useful because binding, delay, and final execution are distinct obligations.
It does not establish that Effra should expose a builder dictionary or that a
builder preserves Effra owner and service contracts.

### Scala comprehensions

The official [Scala for-comprehension documentation](https://docs.scala-lang.org/tour/for-comprehensions.html)
describes translation through operations such as `map`, `flatMap`, and
`withFilter`. This gives a counterexample for guards and filtering: a checked
Effra rule would need to preserve the family's failure/absence semantics and
source order rather than infer a generic method by name. It supplies no
ownership or cancellation contract.

### Effect 4.0.1 and the pinned upstream commit

The hosted question pins the Effect source at commit
`460272d30457f4697d8b8c52cad41caccbcace08`:

- [`Effect.ts`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Effect.ts)
  for lazy `flatMap`/generator sequencing;
- [`Option.ts`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Option.ts)
  for absence-aware generator sequencing;
- [`Result.ts`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Result.ts)
  for explicit success/failure sequencing.

The locked local package reports version `4.0.1`, but its source bytes differ
from the pinned upstream snapshot. The retained byte evidence is:

| Source | SHA-256 |
| --- | --- |
| locked `node_modules/effect/src/Effect.ts` | `d39dc35b3dd3afe81c9b939147078822f26fcf8ba5b33735d9d903596bce9519` |
| locked `node_modules/effect/src/Option.ts` | `c4583652ac8a5fd103f3dae00e79120d820b905fd15e0ead85d9a7715f9f2073` |
| locked `node_modules/effect/src/Result.ts` | `ce118229b4ef376174ccc0b4675c908eee2f22a252c9dabbf01ec8d1af1ad768` |
| pinned `Effect.ts` snapshot | `f0fabfcbd1caaab692733fb75e7be231229b63332aa6eaf08d73f4692a1e6745` |
| pinned `Option.ts` snapshot | `190d9c62f01a842f94831724a49d8d57d2dc5bed4ae9d95da29760ae3d1c3319` |
| pinned `Result.ts` snapshot | `3f9ca0b6ee2d3c14266201f4d7fc34ea844385b8fc266b91f27e6c546613abc8` |

The names and package version are therefore not treated as exact source
equivalence. The pinned copies and the locked package metadata remain in the
private research receipt area; no public blob is invented for them.

### Elixir `with`

At Elixir `91ee75bb` ([pins](../../PRIOR_ARTS.md#standing-comparison-languages-elixir-with-erlangotp-and-moonbit-2026-10-09)), `with` chains `pattern <- expr` clauses, and on the first non-match it returns the unmatched value unchanged. An optional `else` handles it like `case`, and if no `else` clause matches, the result is a run-time `WithClauseError` (`lib/elixir/lib/kernel/special_forms.ex:L1587-L1670`). The documentation warns that all failures flatten into one `else` (`L1673-L1675`). This is the shortest-path evidence for a Result-family binder: it is lexical and needs no builder. It also shows the costs Effra would have to remove. An untyped short-circuit value would erase which failure arrived, and an incomplete `else` becomes a run-time failure rather than a checked one. Any Effra admission would need the typed failure union and exhaustive handling required by **Explicit contracts and clear guardrails**.

### MoonBit checked errors and implicit awaits

At docs `8d9f3ba2`, a MoonBit call to a raising function rethrows implicitly, `try … catch … noraise` handles it, and conversion to `Result` is an explicit `catch` expression (`next/language/error-handling.md:L148-L210`). Async calls are also implicit awaits tracked by the compiler (`next/language/async-experimental.md:L36-L44`). MoonBit therefore needs no binding form, because effects propagate through ordinary calls. Effra rejects that for effects because construction and execution are distinct: a call builds a recipe, and `run` marks where it executes. The comparison supports keeping `run` explicit rather than generalizing it into implicit propagation.

## Continuation and contract obligations

Any future experiment must make these obligations observable in the shared
semantic model before comparing emitters:

1. Evaluate the bound expression once, in source order, and do not evaluate
   later arguments or continuation branches after the family short-circuits.
2. Pass the bound value to the remainder without changing nested returned data.
   A `Result` failure, `Option` absence, or Effect failure must retain its
   declared family contract.
3. Keep Effect laziness, failure rows, service rows, current owner,
   cancellation, and completed cleanup. A pure family cannot secretly execute
   I/O or acquire an Effect service.
4. Reject a continuation that is retained or invoked more than once when that
   would duplicate or outlive an owned resource. A callback result must retain
   its declared ownership facts.
5. Define behavior for multiple arguments, conditionals, matches, nested
   blocks, callbacks, early return/break, and cleanup. Unsupported positions
   remain diagnostics.
6. Keep provider/runtime selection separate from binding-family selection and
   preserve the same semantic model for CLI, MCP, LSP, JavaScript, and Go.

The static direct-lowering candidate is a first-order, single-use continuation
that the checker can prove has the same contract as an explicit family call.
That is a candidate for later inspection only. A retained closure, dynamic
family value, or ownership-sensitive callback must remain explicit until its
contracts are checked.

## Evidence and limits

The root retained an ordinary-JavaScript comparator receipt with exit `0`:
`Option.some("Ada")` and `Option.none` preserved the expected continuation
count, and a `Result` success/failure pair produced `3`/`Invalid` with one
continuation call. That receipt compares library behavior only. It does not
execute generalized Effra syntax, a Go fixture, a custom binder, a resource
workflow, or the compiler's shared desugaring.

The following evidence is deliberately still required before admission:

- two unrelated Effra programs: pure validation/absence and an owned
  service/resource workflow;
- a third user-authored family without compiler edits;
- shared semantic-model inspection and runnable JavaScript/Effect and Go
  comparisons;
- causal negatives for source-order changes, skipped continuations, changed
  failure/service rows, owner escape, hidden I/O in a pure function, nested
  wrappers, ambiguous binders, and unsupported targets;
- retention and compile-cost receipts for each direct-lowering candidate.

No law proof, benchmark, performance improvement, generalized Effra binding,
Go comparison execution, or syntax admission is claimed here. Comparative
performance remains last, after the semantic and ownership controls pass.
