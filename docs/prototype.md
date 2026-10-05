# Implemented prototype

This is a single-file compiler experiment, not the full design in design.md. The source is authoritative; unsupported syntax produces diagnostics rather than being passed through to JavaScript.

## Supported surface

- Immutable local bindings, string/bool/unit and i64 values, managed File handles, string concatenation, primitive equality, and `if` expressions with two branches. i64 literals are currently nonnegative; JS represents them as bigint.
- Ordinary `fn` and lazy `effect fn`, with explicit parameter, result, failure, and service contracts.
- Payload-free nominal errors: `error NotFound`; `fail NotFound` inside effect bodies.
- Nominal services and self-contained implementations: `impl MemoryUsers for Users`.
- `run` to execute a deferred effect within an effect body.
- `.provide<Service>(Provider)` removes that service requirement.
- `.catch<Failure>(pureFallback)` removes exactly that failure. This prototype deliberately accepts a pure replacement value rather than a lambda or effectful handler.
- Built-in Console/Stdout, Clock/LiveClock, Env/LiveEnv; Go additionally supplies Files/LiveFiles and Runtime/RuntimeLive. See [runtime.md](runtime.md) for contracts and foreign adapters.
- Go-only `scope { ... }`, `fork effectCall()`, inferred fiber `join()` / `cancel()` / `interrupt()`, and lazy `.timeout(milliseconds)` adding Timeout. The checker conservatively retains child failure rows even when a child is not joined.

Rows are normalized sets; declarations are upper bounds. Service calls use the service's declared contract even if one provider admits fewer failures. A library may retain requirements; the executable entry is an effect function named `main`, takes no parameters, and requires no remaining services. Its admitted typed failures are reported as runtime failures with nonzero exit status.

The last expression is a block's value. Use semicolons where adjacent expressions could parse as one call; newlines are whitespace. Strings use JSON escapes. Identifiers are ASCII; source offsets and columns are UTF-8 bytes. Empty blocks produce unit.

## Execution and inspection

The frontend is Go with no third-party Go dependencies. The default Go target lowers checked IR to typed lazy closures, explicit Exit propagation, nominal provider structs, and a standalone executable through `go build`. No JavaScript runtime is involved in the native artifact.

The optional JavaScript target lowers to pinned Effect 4.0.1, verified available in the npm registry on 2026-10-05. Effect.gen owns deferred bodies; Context.Service/provideService own providers; catchTag owns selective recovery. No second JavaScript effect scheduler exists.

`ef check` returns revisioned JSON including diagnostics, contracts, body rows, and parse/check timings. `ef inspect FILE SYMBOL` and `ef explain FILE SYMBOL` return the same semantic symbol and direct executed-call/failure contributions. Explanations are currently local contributions, not a transitive proof or provision history. JSON schema version 1 is experimental.

`ef build FILE` emits `dist/go/<name>/main.go` and produces the standalone executable `dist/<name>`. `-o PATH` selects its output path. `ef run FILE` builds and executes the native artifact.

`ef build FILE --target js` emits an importable module and consumer declarations in dist; `--entry` adds host execution. `ef run FILE --target js` emits an entry then starts Bun (Node fallback). Generated JS is trusted compiler output and should be regenerated after source edits.

## Limits

No payload-bearing source errors, structs/enums, generics/open rows, higher-order effect signatures, Layers, retry, source-level host imports, source maps, persistent/package caching, LSP, content mapper, semantic edits, or automated consumer TypeScript-check gate yet. Go runtime failures can carry native payloads; the source language cannot inspect them yet. Go scope snapshots exist, but there is no process-wide runtime endpoint or instrumented wait-reason tree. JS lifecycle parity is not established; those operations produce EF110 on JS. Service/error identities remain local to this single-file experiment.

Inspection revisions hash source bytes. Compiler/runtime/schema versions are separate metadata; this is not a complete build-cache key. Construction of an unused recipe may retain requirements in the recipe type without adding them to the enclosing executed body. The requirements row tracks managed service access, not a proof of complete purity or race freedom.

## Baseline

Measured 2026-10-05 on Apple M4 Pro, darwin/arm64, Go 1.27.1. `BenchmarkCompile10KLines` checks 2,000 five-line independent effect functions (114,890 bytes): three runs averaged 3.15–3.23 ms/op, approximately 14 MB allocated and 16,144 allocations per compile. This is an in-process parse/check measurement; it excludes CLI startup, JSON output, emission, dependency loading, and backend execution. It contains no imports or adverse row-polymorphism cases. No claimed speed budget has been established.

Reproduce: `go test ./internal/compiler -run '^$' -bench BenchmarkCompile10KLines -benchmem -count=3`.

After managed-runtime integration, the same command on the same M4 Pro/Go 1.27.1 environment measured 3.30–3.35 ms/op, approximately 14.05 MB and 16,176 allocations. It still excludes imports, generation, Go compilation and linking; it establishes no matched end-to-end ratio.

## Validation receipt

The gate verifies formatting, Go vet, compiler diagnostics/contracts, both backends' sequential laziness/replay/recovery, provider isolation, nested execution, emitted library imports, nonzero host failure exits, and MCP initialization/query behavior. The generated Go conformance probe uses the race detector. Go runtime tests additionally exercise acquisition-close races, concurrent close, child-before-parent cleanup, LIFO release, composite failure/defect preservation, cancellation hooks, timeout shutdown, native partial results, and closed File handles. The public smoke harness runs `examples/lifecycle.ef` and verifies JS rejects its Go-only capabilities. Use `go test -race ./...` for the full race check.

A prior short parser/checker/emitter fuzz run completed over 1.2 million inputs without a crash before the explicit nesting bound was added. This is limited historical fuzz evidence, not a correctness proof or fresh lifecycle fuzz coverage. The compiler rejects parser nesting beyond 256.

## Native Go lowering boundaries

Go success values are typed primitives/handles, not boxed interpreter values. Effects are lazy `func(efContext) efExit[A]` closures; `run` emits ordinary calls and explicit Exit propagation. Failure/service rows remain frontend checks and are erased in generated Go. Context copies implement lexical provider binding. Ordinary sequencing stays on the current goroutine; managed fork creates a goroutine with an owning scope.

Generated programs import the reusable managed runtime bundled from `runtime/effra` into `dist/go/runtime`. They use exactly the tested runtime source, not a second handwritten scheduler prelude. Managed effect/finalizer panics become defects; arbitrary unmanaged goroutine panics and fatal process/runtime failures remain outside that guarantee. Native entry points forward interrupt/SIGTERM through context and wait for owned cleanup before reporting failure.

Native builds rely on the installed Go toolchain and its normal compiler/build cache. Generated source is deterministic and unchanged bytes are not rewritten. Effra does not yet cache its own parsing/checking or interface summaries. Backend build/link time must be measured separately from the frontend microbenchmark.

Historical warm native build sample, before managed runtime integration: on the same Apple M4 Pro/Go 1.27.1 setup, five unchanged `ef build examples/main.ef` invocations took 40.44–42.26 ms, median 42.08 ms after one warm-up. This includes CLI startup, source checking/emission, and cached Go compilation. It is a tiny no-op fixture; no cold-build, private-edit, public-edit, or matched-Go ratio has been established.
