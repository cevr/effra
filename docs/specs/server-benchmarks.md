# Matched server benchmarks

Status: implementation authorized by the owner on 2026-10-05. This extends the production foundation work with measured server comparisons; it does not predeclare a performance advantage.

## Question and scope

How does an Effra native server compare with equivalent TypeScript/Effect, native TypeScript and optimized Go servers? Measure whether Effra approaches or exceeds the strongest measured Go baseline, and identify the cost of its guarantees separately from host runtime differences. Use generic original fixtures; public documentation must not name inspected application projects.

The path-routing server remains a transport control. The required framework receipt follows the [native server contract](native-server-contracts.md): checked JSON codecs, actual Effect HttpApi HTTP endpoints and RpcServer unary RPC, matched typed failures, limits, cancellation and completed shutdown. External databases and streams remain subsequent workloads. A constant response or transport-only control cannot establish framework-level benefits.

## Equivalence before performance

- Keep protocol, routes, response bytes, application inputs and observable failure behavior identical. Verify every route and failure case before load testing; reject responses with incorrect status/body rather than counting them as successful throughput.
- Include a minimal routing control and a service workflow with meaningful per-request computation/composition. Separate successful and handled-failure paths. Avoid hidden unequal I/O, response caches, repeated runtime construction or allocation work.
- Use Effra's default native Go executable, pinned Effect for TypeScript/Effect, native TypeScript as a runtime-overhead control, and a carefully optimized handwritten Go implementation. Document connection, server-limit and concurrency settings.
- The strongest comparable Go baseline must preserve cancellation, request ownership and completed shutdown. A weaker lean baseline may be reported separately as a lower-bound overhead control, with its missing guarantees visible.
- Source fixtures must remain readable and inspectable. Do not benchmark compiler internals while labeling the result server performance. Optimize all fixtures fairly, preserve correctness, and keep optimization changes reviewable.

## Measurement contract

Record toolchain versions, hardware/OS, commit, source/build hashes, CPU allowance, runtime flags, fixture configuration and cache regime. Pin dependencies and make commands reproducible without paid services or production data.

Use one load generator and workload for every target. Warm servers before sampling; run sequentially on the same host, repeat measurements and vary ordering to limit thermal/cache bias. Record connection reuse, concurrency, request counts, errors, elapsed time and latency distributions (including p50/p95/p99), not just an average. Keep client saturation visible: a local load-generator bottleneck is not evidence of equal server capacity.

Report single-core comparisons separately from multi-core scaling. A Go runtime using several cores cannot be compared to one JS event loop without saying so; a worker configuration needs its own receipt. Include server CPU and memory observations with their collection method and limits. Measure startup-to-readiness and cancellation-to-exit/cleanup separately from steady-state request latency.

Keep build measurements separate: Effra parse/check/import/emission, Go compilation/linking, TS checking/transformation and cache state. A warm Go cache or TypeScript execution without type checking must not masquerade as equal build work. Report raw repeated samples and summary statistics; do not infer a universal benefit or statistically meaningful win from noise.

Include generated-code scaling alongside server builds: many ordinary small functions and one long straight-line effectful function must have separate emitted-source, frontend and backend receipts. The scheduler's 1024-signal regression exposed a generated-Go watchdog that includes compilation; the implementation owner reported a roughly 60-second build followed by millisecond execution, without retaining that diagnostic artifact. Reproduce under admitted host/cache conditions before attributing it to lowering or Go optimization. Preserve generated source, binary identity and raw stage logs. A fast frontend or warm build cannot hide a slow fresh backend, and shortening the regression program or extending its timeout is not a compile-speed improvement.

## Exit gate

- Runnable versioned fixtures and a reproducible local benchmark command produce machine-readable raw samples and a concise report.
- Functional and owned-shutdown conformance pass before performance measurements; failed/forced-cleanup runs remain failures.
- Both minimal and service-workflow workloads execute against TypeScript/Effect, native TypeScript, Effra and optimized Go with comparable settings.
- Results distinguish runtime, lifecycle guarantees, compiler/build cost and workload scope. Claim exceeding optimized Go only if a repeatable matched result supports it; retain slower/equal results honestly.
- Independent review checks equivalence and measurement attribution. The repository gate passes and benchmark artifacts do not contain machine secrets or production data.
