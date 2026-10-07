# Server benchmark design: Effra, TypeScript and optimized Go

Research/design only, 2026-10-05. No benchmark results, speedups, package installation, or repository edits are claimed. Read current Effra HTTP/runtime/build source and primary runtime/tool documentation. This recommendation supports an implementation specification, not a declaration of completion.

## Recommendation

Build a matched, correctness-gated server suite first, then measure fixed offered-load latency/efficiency curves and saturation separately. Include TypeScript/Effect and native TypeScript on both installed JS runtimes, and a carefully optimized Go implementation preserving the same domain and cleanup behavior as Effra. Keep a deliberately lean Go transport floor as a separately labelled diagnostic. Do not use hello-world peak requests/second as the headline for the language.

Effra emits Go and its HTTP runtime uses Go net/http. It can outperform a particular handwritten Go implementation through specialization or better architecture; that is a workload-specific hypothesis. There is no basis to promise performance beyond equally optimized Go in general: the generated program is itself a Go program. A valid outcome may be substantial improvement over TS/Effect, near-native Go efficiency, and separately demonstrated compiler-backed contract/lifecycle advantages. Report losses and inconclusive differences as plainly as wins.

## Current source and environment receipts

Verified locally: Node v26.8.2, Bun1.4.2, Go1.27.1 darwin/arm64; Mac16,8, 12 physical/logical CPUs, 48GiB memory. Current package pins effect4.0.1. Save these again with each real run, alongside commit IDs, binary hashes, source hashes, OS, runtime flags, power/thermal conditions and generator version. A laptop result is not automatically a Linux deployment result.

Effra `runtime/effra/http.go` currently exposes `ServeHTTP(address, path -> Effect[string])`. It passes `r.URL.Path`, creates a managed request scope linked to request and server cancellation, completes scope cleanup before responding, emits status200 text/plain UTF-8 for success and generic HTTP500 for failure. It configures ReadHeaderTimeout=5s; other server fields use Go defaults. It owns listener shutdown and waits for request cleanup. This is not yet a general request/body/header/status/streaming interface.

`cmd/ef/main.go:251` writes generated Go/runtime artifacts only when content changes, then invokes `go build -trimpath`. Existing compiler timings separate imports/parse/check, not all emission/linking/process costs. `scripts/http_smoke.py` validates real routes, file scope, timeout, failure500 and SIGTERM. Reuse its observable boundaries while expanding coverage. An Effra JS domain module behind a handwritten adapter is an optional separately labelled cohort, not evidence that Effra's LiveHttp backend already works on JS.

## Cohorts

| Cohort | Purpose and fairness constraint |
| --- | --- |
| Effra -> Go native | Actual built executable using the supported runtime, with no benchmark-only bypasses. |
| Optimized native Go | Same net/http transport, domain branches, provider configuration, typed result/error behavior, cancellation, child shutdown and resource cleanup. Idiomatic direct code; no artificial generic wrappers. Profile it and review it as seriously as Effra. |
| TypeScript/Effect on Node26.8.2 | node:http adapter plus pinned Effect4.0.1 domain/service/scope code. Build shared services/runtime once, execute request effects with request cancellation, await cleanup and dispose at shutdown. |
| Native TypeScript on Node26.8.2 | Same node:http adapter/protocol and domain behavior; direct functions/promises/context and explicit cleanup as needed. Isolates Effect overhead within Node. |
| TypeScript/Effect on Bun1.4.2 | Bun.serve adapter plus the same Effect domain contract. Labels runtime+transport choice honestly. |
| Native TypeScript on Bun1.4.2 | Same Bun.serve adapter as its Effect pair. Gives the fast idiomatic TS baseline. |
| Lean Go transport floor (secondary) | Direct static/precomputed handler omitting managed domain/ownership overhead; measures transport headroom. Never present it as behaviorally equivalent to richer lifecycle workloads. |

If runtime-engine attribution matters, add node:http-under-Bun as a secondary common-adapter control; do not attribute Bun.serve versus node:http differences solely to TypeScript execution. For Effect, local `node_modules/effect/src/ManagedRuntime.ts:373` confirms cached-context runPromiseExit paths and run options; source at `Effect.ts` owns request abort/scoping APIs. A new Layer/ManagedRuntime per request would unfairly handicap the Effect cohort. Link the pinned source in artifacts rather than applying remembered v3 APIs. [Effect source](https://github.com/Effect-TS/effect/blob/main/packages/effect/src/ManagedRuntime.ts)

## Workloads within admitted capabilities

Use identical deterministic inputs, response bytes, branch proportions and provider configuration. Run each workload separately before a fixed published mixture:

1. **Transport control:** GET /health -> exact fixed text. Separates network/adapter floor from useful domain work; not a headline language result.
2. **Domain composition:** a finite catalogue of ASCII /users/<id> paths, typed record and enum state interpretation, a captured configured provider, repeated service operations, success, selective recovery and unhandled failure paths. Generate the same varying catalogue for every implementation. Do real per-request computation where the specification requires it; do not let one cohort precompute dynamic outputs.
3. **Managed async:** two bounded child operations, cancellation and waited cleanup, with a deterministic local port/service fixture. If actual timer waits are used, label it a timer/I/O workload, not CPU throughput. Avoid real public services or uncontrolled databases.
4. **Resource path:** same local fixture file, same read/close behavior and cache regime, with per-request acquisition where specified. Separate page-cache-warm results from process startup; do not claim cold disk I/O after merely restarting a process.
5. **Failure/cancellation:** small published proportions of declared recoveries and unhandled failures, plus client disconnect and server SIGTERM with active work. Verify cleanup before successful completion. Run disruptive cancellation correctness apart from throughput unless the mixture explicitly includes it.

Current language operations do not justify a general JSON parser, POST body, cryptography, numeric-loop or database benchmark without an explicit new supported boundary. Avoid implementing heavy work only in an imported Go helper for Effra while requiring slow handwritten JS in its comparison. When a host primitive is used, use corresponding optimized host primitives and label that dimension.

## Protocol and lifecycle parity before timing

Fix HTTP/1.1, keep-alive, no TLS, no compression, no pipelining, fixed connection counts, bounded headers, exact content type/status/body and request catalogue. Use uncomplicated valid ASCII paths initially so differing URL-decoding APIs do not silently benchmark different semantics. Count unexpected statuses, body mismatches, resets, timeouts and rejected connections as errors, never successes. Record relevant transport/header differences that cannot be normalized through existing Effra APIs.

Configure header/read/keep-alive limits deliberately. Bun's documented idle timeout includes stalled in-flight handlers and defaults to10s; it must not accidentally change an async fixture's behavior. Bun.stop(false) waits for active connections; it does not replace application child cancellation/cleanup. [Bun server documentation](https://bun.sh/docs/runtime/http/server)

Node server.close stops accepting and handles connection shutdown; use request AbortSignals/controllers and a tracked in-flight set to reproduce Effra's server-cancellation behavior. Do not count the listen callback as full application readiness. [Node26.8.2 HTTP API](https://nodejs.org/download/release/v26.8.2/docs/api/http.html)

Go Shutdown waits for active connections to become idle, but the caller must keep the process alive until it returns. Effra additionally propagates server cancellation into request work. The optimized Go implementation must preserve that behavior. [Go net/http Shutdown](https://pkg.go.dev/net/http#Server.Shutdown)

Preflight every cohort with identical success/recovery/error assertions, actual abort of in-flight work, and causal evidence that resources/children close. A finite shutdown watchdog should report cleanup unconfirmed after force termination. Graceful exit-code conventions may differ (current Effra reports interruption nonzero); record expected exit classification rather than discarding correct cleanup because of a cosmetic code.

## Load model and experimental schedule

Use a separate load-generator process, preferably a second machine for deployment-grade evidence. No server instrumentation or generator may consume an unreported share of the measured CPU budget. On one laptop, report co-location, monitor generator CPU and achieved rate, and repeat with additional generator capacity to expose bottlenecks. Do not run compiler tests, builds or other foundation work concurrently with measurement.

First perform an untimed capacity pilot for each cohort. Then choose common absolute offered rates below the weakest cohort's stable capacity, plus fixed higher-rate stress points. Report the full curves, including error/timeout rates. Primary metric: maximum sustained goodput satisfying a predeclared p99 target and error budget; also compare p50/p95/p99 and CPU/request at the same offered rate. A reasonable synthetic starting target is p99<=10ms and unexpected-error fraction<=0.1%, but choose workload-specific SLOs before the scored runs, especially for deliberate waits.

Closed-loop concurrency sweeps (1,8,32,128 connections) are useful throughput controls but hide queued demand when overloaded. Use scheduled/open-loop load or corrected intended-start latency for overload claims. wrk2 documents constant-rate corrected latency and an approximately1ms timing-granularity limit, making it unsuitable as the sole proof of submillisecond laptop differences. Vegeta is a maintained fixed-rate alternative; verify target versus achieved send rate, worker/connection limits and latency timestamp semantics before claiming overload accuracy. [wrk2 primary README](https://github.com/giltene/wrk2), [Vegeta primary README](https://github.com/tsenart/vegeta)

Recommended fixed schedule: 15s warmup, 30s scored steady-state samples, at least10 randomized/interleaved repetitions per primary comparison, longer runs where p99.9 or memory trends need more observations. Treat repetitions as independent samples; do not average percentile numbers from arbitrary mixtures. Save per-run histograms/raw samples, totals, error classifications and randomization seed. Choose repetition count in advance; do not keep rerunning until a desired winner appears. Go's benchstat guidance recommends at least10 samples, interleaving variants and reporting statistical uncertainty. [benchstat methodology](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)

## CPU, memory, scaling and startup

Record server user+system CPU seconds during each measurement window, normalized as CPU microseconds per validated successful request and throughput per CPU second. Include all worker processes/threads. Distinguish CPU use from event-loop delay and wall latency. For Node, cpuUsage/resourceUsage provide CPU totals; maxRSS is reported in KiB by Node's API. Use one OS-level sampling method for comparable current/peak RSS across all cohorts, explicitly normalizing platform units. Do not compare V8 heapUsed with Go heap allocation as if either were process memory. [Node26.8.2 process metrics](https://nodejs.org/download/release/v26.8.2/docs/api/process.html), [Node performance hooks](https://nodejs.org/api/perf_hooks.html)

Collect steady-state median/peak RSS, post-idle retained RSS, startup peak, GC activity and allocation profiles as explanatory diagnostics. Avoid forced GC in scored runs. CPU/heap profiling changes execution; gather profiles in separate otherwise matched runs.

Publish single-instance default-runtime results first. Go can use multiple cores; Node's main JS event loop and Bun's JS execution have different scaling. A GOMAXPROCS=1 control is useful but is not an OS-enforced one-core budget and does not constrain every background thread. For strict equal-resource scalability claims, use actual CPU quotas/affinity on a controlled deployment host and compare complete process groups (including multiple TS workers when configured). Report both core budget and consumed CPU.

Measure startup from spawn to first validated HTTP response, using an explicit readiness signal to avoid polling-granularity dominance; also record spawn-to-listen and first-request latency separately. Run30 fresh-process repetitions on warm filesystem caches; label them process-cold/cache-warm. Do not call them cold-machine starts. Measure quiet SIGTERM and active-request SIGTERM-to-cleanup-complete separately, confirming no process/socket leak afterward.

## Build measurements

Separate typed validation, runnable-artifact production and ready-to-serve time. A TS loader/transpiler does not equal a TypeScript typecheck; Node's built-in type stripping deliberately does not perform type checking. Report typecheck plus emit/bundle and direct-run startup separately. [Node TypeScript support](https://nodejs.org/download/release/v26.8.2/docs/api/typescript.html)

For Effra: compiler imports/parse/check timings, emission and file generation, Go compilation/link wall+CPU, and full ef build wall+CPU. For native Go: matched go build flags/cache state. For TS: strict checker version/options and emit/bundle time, then runtime load time. Runtime pin alone does not pin the TS checker; discover and record it before measuring.

Run four cache/edit regimes with preinstalled pinned dependencies: isolated empty task-owned build caches; warm no-op rebuild; private function-body edit; public contract edit. Do not clear shared caches or time downloads. Current Effra is single-file and has no claimed interface-summary incremental compiler; label these as observed local rebuild regimes, not proof of package-level incremental compilation. Record artifact/runtime deployment size separately.

For optional PGO, profile representative mixed workloads and optimize both Effra-generated Go and native Go using the same policy. Keep tuned and untuned results. Go automatically discovers default.pgo and supports explicit -pgo controls; capture whether optimization was actually applied. Hold back some inputs/mixes from profiling to avoid a single-fixture victory. [Go PGO guide](https://go.dev/doc/pgo)

## Interpretation and receipt requirements

Publish raw artifacts plus a compact table per workload containing goodput, offered/achieved rate, p50/p95/p99, errors, CPU/request, RSS, startup, build and shutdown. Include effect sizes and uncertainty. A runtime win requires validated behavior and matched load/resource conditions; a compile/frontend win is a separate result. Explain profile-backed causes rather than attributing every gap to language choice.

Only claim exceeding optimized Go if a predeclared workload/metric shows a reproducible difference larger than measurement noise while meeting the same correctness and lifecycle contract. State precisely which Go implementation and optimization policy were exceeded. If optimized Go is faster, show the measured gap and where Effra spends it; do not relabel the lean lower-bound fixture or omit the TS-native control to manufacture a benefit.
