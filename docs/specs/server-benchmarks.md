# Matched server benchmarks

Status: implementation authorized by the owner on 2026-10-05. This extends the production foundation work with measured server comparisons; it does not predeclare a performance advantage.

## Question and scope

How does an Effra native server compare with equivalent TypeScript/Effect, native TypeScript and optimized Go servers? The native target is accepted only when a matched Effra application meets or exceeds optimized idiomatic Go with the same guarantees; measure the cost of those guarantees separately from host runtime differences. The JS target must pursue every material measurable lowering or specialization opportunity, including match dispatch and generated effect-runtime paths, while retaining the pinned default Effect-compatible ABI and userland contract. Use generic original fixtures; public documentation must not name inspected application projects.

The path-routing server remains a transport control. The required framework receipt follows the [native server contract](native-server-contracts.md): checked JSON codecs, actual Effect HttpApi HTTP endpoints and RpcServer unary RPC, matched typed failures, limits, cancellation and completed shutdown. External databases and streams remain subsequent workloads. A constant response or transport-only control cannot establish framework-level benefits.

## Equivalence before performance

- Keep protocol, routes, response bytes, application inputs and observable failure behavior identical. Verify every route and failure case before load testing; reject responses with incorrect status/body rather than counting them as successful throughput.
- Include a minimal routing control and a service workflow with meaningful per-request computation/composition. Separate successful and handled-failure paths. Avoid hidden unequal I/O, response caches, repeated runtime construction or allocation work.
- Use Effra's default native Go executable, pinned Effect for TypeScript/Effect, native TypeScript as a runtime-overhead control, and a carefully optimized idiomatic Go implementation. Document connection, server-limit and concurrency settings and retain the same validation, failure/service rows, ownership, cancellation and completed cleanup in every scored cohort.
- The strongest comparable Go baseline must preserve cancellation, request ownership and completed shutdown. A weaker lean baseline may be reported separately as a lower-bound overhead control, with its missing guarantees visible.
- Source fixtures must remain readable and inspectable. Do not benchmark compiler internals while labeling the result server performance. Optimize all fixtures fairly, preserve correctness, and keep optimization changes reviewable.

When an abstraction or lowering is part of the comparison, pair the Effra
candidate with semantically equivalent explicit Go and TypeScript/Effect
programs. Preserve the same validation, failure/service rows, ownership,
cancellation and completed cleanup before comparing runtime, allocation or
executable size. Record erasure, direct lowering, retained dispatch and
residual checks; an abstraction win that changes the work or drops a guardrail
is invalid. This obligation is measured only in the benchmark-last phase.

For the JS cohort, retain the exact emitted match/dispatch strategy, generated
runtime module set, Effect ABI/userland version and engine/runtime version. Test
static `match` lowering to `if`/`switch`, direct calls and specialized effect
runtime candidates where source and profile evidence make them plausible. V8
warmup, runtime feedback and deoptimization can change the result, so cold,
warm and engine/type-distribution regimes stay separate. A generated runtime
that is not human-style source is permitted; its typed errors, ownership,
cancellation, scopes and completed cleanup must match the pinned contract. A
many-times speedup remains a hypothesis until matched raw samples support it.

## Measurement contract

Record toolchain versions, hardware/OS, commit, source/build hashes, CPU allowance, runtime flags, fixture configuration and cache regime. For native Go, retain the optimized compiler/linker/PGO configuration and `-gcflags=-m=3` or equivalent allocation evidence. For JS, retain engine tier/warmup settings, emitted strategy and generated runtime identity. Pin dependencies and make commands reproducible without paid services or production data.

Use one load generator and workload for every target. Warm servers before sampling; run sequentially on the same host, repeat measurements and vary ordering to limit thermal/cache bias. Record connection reuse, concurrency, request counts, errors, elapsed time and latency distributions (including p50/p95/p99), not just an average. Keep client saturation visible: a local load-generator bottleneck is not evidence of equal server capacity.

Report single-core comparisons separately from multi-core scaling. A Go runtime using several cores cannot be compared to one JS event loop without saying so; a worker configuration needs its own receipt. Include server CPU and memory observations with their collection method and limits. Measure startup-to-readiness and cancellation-to-exit/cleanup separately from steady-state request latency.

Keep build measurements separate: Effra parse/check/import/emission, Go compilation/linking, TS checking/transformation and cache state. A warm Go cache or TypeScript execution without type checking must not masquerade as equal build work. Report raw repeated samples and summary statistics; do not infer a universal benefit or statistically meaningful win from noise.

Include generated-code scaling alongside server builds: many ordinary small functions and one long straight-line effectful function must have separate emitted-source, frontend and backend receipts. The scheduler's 1024-signal regression exposed a generated-Go watchdog that includes compilation; the implementation owner reported a roughly 60-second build followed by millisecond execution, without retaining that diagnostic artifact. Reproduce under admitted host/cache conditions before attributing it to lowering or Go optimization. Preserve generated source, binary identity and raw stage logs. A fast frontend or warm build cannot hide a slow fresh backend, and shortening the regression program or extending its timeout is not a compile-speed improvement.

A subsequent [retained diagnostic](../research/generated-go-build-cost.md) observes 47.754 seconds in the Go build versus 3.894 milliseconds running the resulting test on the shared host. It establishes the stage boundary for that run, not an admitted comparative score or attribution to a specific optimizer.

## Exit gate

- Runnable versioned fixtures and a reproducible local benchmark command produce machine-readable raw samples and a concise report.
- Functional and owned-shutdown conformance pass before performance measurements; failed/forced-cleanup runs remain failures.
- Both minimal and service-workflow workloads execute against TypeScript/Effect, native TypeScript, Effra and optimized Go with comparable settings.
- Results distinguish runtime, lifecycle guarantees, compiler/build cost and workload scope. Claim meeting or exceeding optimized Go only if a repeatable matched result supports it; retain slower/equal results honestly. JS claims must identify the measured lowering/specialization and preserve losing candidates and cold/warm regressions.
- Independent review checks equivalence and measurement attribution. The repository gate passes and benchmark artifacts do not contain machine secrets or production data.
