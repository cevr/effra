# Declarative layer composition

Source comparison, 2026-10-06. This records adoption decisions and finite acceptance cases; first-class layers are not implemented yet. Exact application pins and primary-source pointers live in [the prior-art index](../../PRIOR_ARTS.md); private source pointers remain in local research notes. No production application tests were run for this comparison.

Production servers repeatedly compose intermediate provider bundles, choose output-retaining versus output-hiding provision, and reinject already constructed application services into connection or request graphs. Resource hosts separately implement node traversal, replacement, cycle paths, compatible-input sharing and disposal. Test roots carry fixture options through constructor helpers or wrap plans to obtain fresh acquisition. Those are recurring contracts a small language surface can expose directly.

Adopt `service` for operations, `impl` for behavior and construction capture, and `layer` for checked assembly. Construction requirements determine edges, eliminating repeated manual provide chains. Provide the finished plan once at an application, command, handler or test owner. Domain operations retain their own service requirements. [The finite contract](../specs/layers.md) carries the guarantees and implementation order.

Important source differences prevent copying an application graph builder unchanged: roots-only output inference contradicts exposing all bindings; branch-local providers and last-writer precedence contradict ordinary duplicate errors. Some provider adapters convert construction failures into defects or ignore cleanup failures. Effra retains expected construction failures and composite cleanup causes. Custom locks, TTLs, reload fallback and global memo maps demonstrate policy needs, not generic layer correctness.

The pinned Effect runtime supplies useful implementation references: [pending acquisition and observer registration](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Layer.ts#L235), [memo-map entry publication](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Layer.ts#L405), [construction failure scope closure](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Layer.ts#L348), and [explicit fresh memo maps](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Layer.ts#L2162). Object-keyed memoization alone does not discriminate arbitrary different input contexts; Effra must specify that separately. These are source inputs, not newly executed Effra conformance.

## Cases the audit adds

- Replacement changes construction dependencies and failures before cycle detection; all substitutions apply consistently, with no merge-order winner. Diagnose unknown or repeated replacement targets.
- The same node cannot capture whichever of two incompatible input contexts happened to acquire first. Compatibility follows dependency/provider identities, never arbitrary structural equality of configuration.
- Selected migrations, listeners, workers and finalizers survive output narrowing, including zero-output startup nodes. Truly unused declarations introduce no startup effects.
- Existing application services may be borrowed by a request graph. Request shutdown releases its resources and leaves the application services alive. Cross-build leases require an explicit external owner.
- Parameterized plans capture ordinary configuration without acquisition. Plan instances and fresh subtrees have explicit identities; keyed caching and retirement remain libraries.
- Runtime-selected plugins expose a checked contract and visibly incomplete concrete graph. Isolated plugin failure and atomic application startup are different policies.
- Concurrent waiters, cancellation, failed startup, cleanup defects and dependent-first shutdown need causal executable tests. No source-level DAG or copied upstream test supplies those runtime proofs.

## Ergonomic acceptance

Illustrative syntax, pending implementation:

```ef
layer Storage {
    Config = EnvConfig
    Database = Postgres
}
layer Server provides { Users, Logger } {
    merge Platform, Storage
    Users = UsersLive
}
layer Command {
    merge Platform, Storage
    Billing = BillingLive
}
layer TestServer {
    merge Server
    replace Database = FixtureDatabase
    replace Logger = RecordingLogger
}
```

A server and a command are unrelated callers of the same storage composition. Previously each must repeat constructor provision and choose which intermediate outputs to retain. A test previously carries fixture options through those helpers. The new plan states selection and substitution once, while inspection explains hidden acquisition, failures, remaining inputs and ownership. Separate graph builds remain independent.

Completion requires executable versions of both callers, explicit missing-input/duplicate/cycle/error-bound negatives, the same Go/JS lifetime outcomes and identical CLI/MCP graph facts. This is the architecture-loop test of explicit, declarative and delightful Effects: useful primitives remove ceremony while preserving the contract.
