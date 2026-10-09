<!-- {"id": "run-binding", "title": "Generalized run sequencing and checked userland binding", "status": "open", "labels": ["wayfinder:research"], "parent": "map", "assignee": "cevr", "blocked_by": []} -->
# Generalized run sequencing and checked userland binding

## Question

Can existing `run` become checked userland binding for other effectful and pure computation families, while preserving explicit failure/service contracts, owned lifetimes, Go-like readability and zero-cost lowering opportunities?

Owner input, 2026-10-08: explore monadic binding supporting other effectful or non-effectful computations. This is an investigation, not accepted syntax or implementation.

## Investigation

- Compare ordinary typed `flatMap` calls with a minimal sequencing rule. Follow construct admission: syntax/checker machinery needs a concrete invariant ordinary libraries cannot enforce, or an explicit admitted notation exception.
- Compare explicit lexical selection with coherent nominal-family selection; diagnose ambiguity and import-dependent rebinding. Keep binding-family selection separate from effect-runtime provider selection.
- Use pure Option/Result domain code and an owned service/resource Effect workflow. Preserve lazy Effect construction, public failure/service rows, current owner, cancellation and completed cleanup. Pure functions cannot secretly execute I/O.
- The rest of a block is the continuation. Preserve short-circuit behavior, evaluation order, nested returned data and explicit family adaptation. Multi-use or retained continuations must not duplicate or escape owned resources.
- Require a third user-authored family without compiler edits, shared semantic-model/desugaring inspection, two-target runnable comparisons and causal negative controls before admission. Monad method shape/laws alone do not establish cancellation, cleanup or foreign honesty.
- Record compile/retention costs and static direct-lowering candidates; comparative performance measurements remain last.

## Primary mechanisms

[Haskell do translation](https://www.haskell.org/onlinereport/haskell2010/haskellch3.html#x8-470003.14), [GHC qualified do](https://downloads.haskell.org/ghc/9.14.1/docs/users_guide/exts/qualified_do.html), [F# computation expressions](https://learn.microsoft.com/en-us/dotnet/fsharp/language-reference/computation-expressions), and [Scala comprehensions](https://docs.scala-lang.org/tour/for-comprehensions.html) supply mechanisms and counterexamples to compare. Pinned [Effect flatMap/gen](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Effect.ts), [Option.gen](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Option.ts) and [Result.gen](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Result.ts) are separate comparisons. Locked installed4.0.1 and pinned upstream package4.0.1 have different source bytes; version names do not establish exact equivalence.

## Current limits

Accepted Effra currently permits run only for Effects in effect functions. Root ordinary-JS Option/Result controls passed, but no generalized Effra binding, Go comparison execution, lawful custom binder, performance improvement or syntax admission follows. Durable local research/source synchronization and independently reviewed prototype remain upcoming.
