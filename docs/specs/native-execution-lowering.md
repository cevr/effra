# Direct native execution and build cost

Status: authorized performance follow-up, not an implemented optimization. The [retained backend diagnostic](../research/generated-go-build-cost.md) separates a 47.754-second Go build from a 3.894-millisecond executable run for 1024 sequential managed operations. A separate compiler profile attributes 77.61% cumulative sampled CPU to `ir.Reassigned`. These shared-host observations identify a lowering experiment; they do not establish a comparative speedup or a cold-build baseline. Native acceptance uses optimized idiomatic Go with the same contract; JS acceptance pursues every material measurable lowering while preserving the pinned default ABI and userland runtime contract.

The canonical function model must distinguish constructing a deferred recipe, forwarding that value, and executing it. Use that distinction to avoid repeated recipe-factory creation followed by immediate invocation in native code when the compiler knows the callee and the executed contract. One candidate is a direct execution entry shared with a thin lazy recipe constructor. Unknown function values, stored recipes and combinators retain their required dynamic execution path. A general helper boundary is also a valid measured alternative; no operation-specific benchmark shortcut is authorized.

Preserve argument evaluation count and order, lazy recipe construction, repeated execution of reusable recipes, lexical provider capture, invocation ownership, declared failures, defects and composite causes. Immediate execution must retain the same cancellation checkpoints, scheduler admission, child/finalizer ownership and interruption behavior. A pure function value is not an executed effect. Do not erase a scope, provider, handler or check solely because one performance fixture does not observe it. Fluent APIs must lower through the same checked contracts, without a dynamic registry that retains otherwise unused library facilities.

Every lowering candidate uses a semantic equivalence baseline: the same
validation, failure/service rows, ownership, cancellation and completed cleanup
must be observable in the Effra program, an explicit Go program and an explicit
TypeScript/Effect program. Record whether the candidate erases the abstraction,
directly lowers it or retains a runtime representation, then measure emitted
source, retained dependencies, executable bytes, allocation, dispatch and
residual checks. A candidate that wins by omitting a guardrail or by changing
the work is rejected; no current implementation or universal zero-overhead
claim follows from this contract.

Native Go candidates must be compared with an optimized idiomatic Go program,
not a weaker handwritten control, and are accepted only when a repeatable
matched result meets or exceeds that program. JS candidates must enumerate and
measure material opportunities such as static `match` lowering to `if`/`switch`,
direct dispatch, tail-resumptive paths, join-point sharing and generated or
specialized effect-runtime code. A generated runtime is an ordinary compiler or
provider implementation and may be code a human would not write by hand; it
must preserve typed errors, service rows, ownership, cancellation, scopes,
interruption and completed cleanup, and must keep the pinned default
Effect-compatible JS ABI. A possible many-times speedup is a hypothesis for a
matched measurement, never a promised multiplier. The [C++ Per.7 guidance],
[C++ Per.11 guidance],
[generalized evidence-passing report], and [V8 Maglev report] motivate these
experiments while leaving their contextual-equivalence and engine-specific
limits explicit.

[C++ Per.7 guidance]: https://isocpp.github.io/CppCoreGuidelines/CppCoreGuidelines#rper-efficiency
[C++ Per.11 guidance]: https://isocpp.github.io/CppCoreGuidelines/CppCoreGuidelines#rper-comp
[generalized evidence-passing report]: https://www.microsoft.com/en-us/research/wp-content/uploads/2021/03/multip-tr-v2.pdf
[V8 Maglev report]: https://v8.dev/blog/maglev

The historical checked-match emitter at `f091cd6db870c08ca1d82f100150ce0f3363e6a8`
and the numeric baseline `07d861f0296a216b78cd0c9e0a1ba2896a0d06d9`
emitted ordered `if` branches uniformly. That archived baseline remains
separate from the current emitter carried from accepted LOW donor
`0a1c8b4768f3d82475d39f2ab0f5ee4d22259cb0`.

The current JavaScript [`jsMatchStatements`](../../internal/compiler/emit.go#L757)
path evaluates each subject once in source order and keeps branches lazy.
[`jsCanSwitchMatch`](../../internal/compiler/emit.go#L830) admits a tag `switch`
only for checked eligible single-subject plans with explicit, non-total variant
cells. Product matches, total cells and other general plans retain the ordered
condition chain; constrained cells compare tags and wholly total arms use a
`true` condition. At [discarded, returned and admitted self-tail sites](../../internal/compiler/emit.go#L489),
`if` and `match` emit direct statements: discarded branches continue with the
following statement, while tail branches return, fail or continue an admitted
self-tail loop. Expression-valued subpositions retain their value-producing
`yield* Effect.gen` or pure IIFE wrapper ([`jsExpr`](../../internal/compiler/emit.go#L676),
[`jsMatch`](../../internal/compiler/emit.go#L742)). The outer automatic scope and
explicit scope/cleanup boundaries remain, along with the pinned default
Effect-compatible JS ABI. These are current emitted shapes, not measured
allocation reductions or speedups. Further dispatch and generated/specialized
runtime candidates require comparisons under identical subject/body/tag
semantics and lifecycle laws.

Deliver after the [ordinary function/interface seam](language-abstractions.md), in compiling and gated units:

1. Retain an exact current baseline: source, compiler artifact, emitted files, executable, toolchain, commands, cache policy, host observations and separate frontend/emission/backend/execution timings. Use the long-body fixture (1024 straight-line signals, retired from the gated test at `0170be3` and preserved at `f4c16b05afe931706f6afc58842345fb87cb743b:internal/compiler/causal_test.go` as `TestSchedulerDrainsLongManagedContinuationAcrossTargets`), a small equivalent case, many short functions, stored and forwarded recipes, provider/catch/timeout/fork cases, and an ordinary managed server. Preserve historical receipts rather than replacing their source spelling or timings.
2. Implement the general lowering selected by source and profile evidence. Add focused semantic regressions that distinguish construction from execution and cover the obligations above on Go and JavaScript. For JavaScript, measure each material admitted lowering/specialization candidate, including match dispatch and a generated effect-runtime path where applicable; retaining the pinned ABI and userland contract is mandatory. Check emitted dependencies and executable size alongside build cost; a faster compiler is not permission to retain unused standard-library modules.
3. Measure old/new on matched source and toolchain with explicitly controlled cold, warm and edit scenarios. Distinguish frontend-only work, backend compilation, linking and execution. Compare a handwritten Go control with the same runtime operations and dependencies where that isolates lowering cost. Retain emitted source and raw repeated samples, including regressions. Profile again to show whether the identified hotspot changed. Admit no performance result while competing builds contaminate the comparison.

Acceptance requires identical observable conformance, full gates, race checks for changed ownership/runtime paths, and independently reviewed emitted-code behavior. The problematic general code shape needs a repeatable build-cost improvement. Each accepted lowering candidate separately needs a repeatable matched improvement without unexplained regressions in its controls. Native Go must meet or exceed optimized same-contract Go before a native performance claim. JS must retain receipts for every material candidate and its cold/warm engine behavior; if no candidate improves matched cost, retain the measurement and leave the path unresolved. Continue investigating rather than claiming success from a smaller fixture, disabled optimizer, removed checks or a longer watchdog. Public performance statements name the measured stage and workload. General incremental package checking and host-import session caching remain separate work. Benchmark execution remains last under the measurement ledger after functional and lifecycle gates.
