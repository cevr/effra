# Implemented prototype

This is a single-file compiler experiment, not the full design in design.md. The source is authoritative; unsupported syntax produces diagnostics rather than being passed through to JavaScript.

## Supported surface

- Immutable local bindings, string/bool/unit and i64 values, managed File handles, string concatenation, primitive equality, and `if` expressions with two branches. i64 literals are currently nonnegative; JS represents them as bigint.
- Ordinary `fn` and lazy `effect fn`, with explicit parameter, result, failure, and service contracts.
- Nominal records and closed enums with typed fields: `record User { id: string }`, `enum State { Ready Waiting { reason: string } }`, constructors, field access and exhaustive `match` arms. Constructor payloads accept `{ field }` shorthand; in a control position such as `match State.Ready { value } { ... }`, the following arm brace disambiguates that constructor from the control body. Static data does not decode or validate external wire values.
- Nominal errors may carry typed fields: `error NotFound { id: string }`; `fail NotFound { id: "missing" }` preserves the payload on both targets. Error declarations are failure payload types, not ordinary success values or nested record fields.
- Nominal services and implementations, including configured providers that capture declared services: `impl MemoryUsers for Users` and `impl Prefixed(prefix: string) for Greeting uses {Names}`. Construction is effectful and explicit; fallible acquisition and general memoized sharing are unsupported.
- `run` to execute a deferred effect within an effect body.
- `.provide<Service>(Provider)` removes that service requirement.
- `.catch<Failure>(pureFallback)` removes exactly that failure. This prototype deliberately accepts a pure replacement value rather than a lambda or effectful handler.
- Built-in Console/Stdout, Clock/LiveClock, Env/LiveEnv; Go additionally supplies Files/LiveFiles, Runtime/RuntimeLive and Http/GoHttp. See [runtime.md](runtime.md) for contracts and foreign adapters.
- `scope { ... }`, `fork effectCall()`, inferred fiber `join()` / `cancel()` / `interrupt()`, and lazy `.timeout(milliseconds)` adding Timeout. The checker conservatively retains child failure rows even when a child is not joined.

- `import go alias "module/package"` loads primitive native function declarations; `.orFail()` adapts returned Go errors explicitly. See [interop](interop.md).
- `Http.serve(address, handler)` accepts a restricted effect-function reference; the native provider owns requests and shutdown. See [HTTP contract](runtime.md#http-server).

Rows are normalized sets; declarations are upper bounds. Service calls use the service's declared contract even if one provider admits fewer failures. A library may retain requirements; the executable entry is an effect function named `main`, takes no parameters, and requires no remaining services. Its admitted typed failures are reported as runtime failures with nonzero exit status.

The last expression is a block's value. Use semicolons where adjacent expressions could parse as one call; newlines are whitespace. Strings use JSON escapes. Identifiers are ASCII; source offsets and columns are UTF-8 bytes. Empty blocks produce unit.

## Execution and inspection

The frontend is Go with no third-party Go dependencies. The default Go target lowers checked IR to typed lazy closures, explicit Exit propagation, nominal provider structs, and a standalone executable through `go build`. No JavaScript runtime is involved in the native artifact.

The optional JavaScript target lowers to pinned Effect 4.0.1, verified available in the npm registry on 2026-10-05. Effect.gen owns deferred bodies; Context.Service/provideService own providers; a strict lone-failure adapter owns selective recovery and preserves composite causes. A small policy adapter owns child admission/joining over Effect’s existing scheduler and resource scopes. The test runner provides an explicit scheduler-backed clock fixture using Effect’s Scheduler dispatcher and does not make that fixture available to ordinary generated programs.

`ef check` returns revisioned JSON including diagnostics, contracts, canonical type references, bounded ownership/capture facts, nominal declarations, used host bindings, and separate import/parse/check timings. `ef inspect FILE SYMBOL` and `ef explain FILE SYMBOL` return the same semantic symbol and direct executed-call/failure contributions; passing a record, enum or error name inspects its declaration fields/variants. Explanations are currently local contributions, not a transitive proof or provision history. JSON schema version 3 is experimental.

`ef build FILE` emits `dist/go/<name>/main.go` and produces the standalone executable `dist/<name>`. `-o PATH` selects its output path. `ef run FILE` builds and executes the native artifact.

`ef build FILE --target js` emits an importable module and consumer declarations in dist; `--entry` adds host execution. `ef run FILE --target js` emits an entry then starts Bun (Node fallback). Empty enums are represented as `never` in consumer declarations, and names reserved by TypeScript are diagnosed before emission. Generated JS is trusted compiler output and should be regenerated after source edits.

## Limits

Generics/open rows, higher-order effect signatures, Layers, retry, TypeScript host imports, source maps, persistent/package caching, LSP, content mapper, semantic edits, codecs, and an automated consumer TypeScript-check gate remain unsupported. Recursive data layouts receive an explicit diagnostic, as do unknown generic type spellings. Go scope snapshots exist, but there is no process-wide runtime endpoint or instrumented wait-reason tree. Shared Go/JS lifecycle conformance covers owned cleanup, unobserved child failures and deadline cleanup defects. Files/Runtime/Http and Go imports still produce EF110 on JS. Service/type identities remain local to this single-file experiment.

Inspection revisions hash source bytes and, when imported, Go export archives and normalized behavior contracts. Compiler/runtime/schema versions are separate metadata; this is not a complete build-cache key. Construction of an unused recipe may retain requirements in the recipe type without adding them to the enclosing executed body. The requirements row tracks managed service access, not a proof of complete purity or race freedom.

## Baseline

Measured 2026-10-05 on Apple M4 Pro, darwin/arm64, Go 1.27.1. `BenchmarkCompile10KLines` checks 2,000 five-line independent effect functions (114,890 bytes): three runs averaged 3.15–3.23 ms/op, approximately 14 MB allocated and 16,144 allocations per compile. This is an in-process parse/check measurement; it excludes CLI startup, JSON output, emission, dependency loading, and backend execution. It contains no imports or adverse row-polymorphism cases. No claimed speed budget has been established.

Reproduce: `go test ./internal/compiler -run '^$' -bench BenchmarkCompile10KLines -benchmem -count=3`.

After managed-runtime integration, the same command on the same M4 Pro/Go 1.27.1 environment measured 3.30–3.35 ms/op, approximately 14.05 MB and 16,176 allocations. It still excludes imports, generation, Go compilation and linking; it establishes no matched end-to-end ratio.

## Validation receipt

The gate verifies formatting, Go vet, compiler diagnostics/contracts, both backends' sequential laziness/replay/recovery, provider isolation, nested execution, emitted library imports, nonzero host failure exits, and MCP initialization/query behavior. The generated Go conformance probe uses the race detector. Go runtime tests additionally exercise acquisition-close races, concurrent close, child-before-parent cleanup, LIFO release, composite failure/defect preservation, cancellation hooks, timeout shutdown, native partial results, and closed File handles. The public smoke harness runs the portable concurrency example on both targets, imported Go functions, and `examples/lifecycle.ef` (whose Files/Runtime capabilities remain Go-only). The HTTP harness runs a real generated server and checks routes, failure responses, deadlines, file scopes and SIGTERM shutdown. Use `go test -race ./...` for the full race check.

A prior short parser/checker/emitter fuzz run completed over 1.2 million inputs without a crash before the explicit nesting bound was added. This is limited historical fuzz evidence, not a correctness proof or fresh lifecycle fuzz coverage. The compiler rejects parser nesting beyond 256.

## Native Go lowering boundaries

Go success values are typed primitives/handles, not boxed interpreter values. Effects are lazy `func(efContext) efExit[A]` closures; `run` emits ordinary calls and explicit Exit propagation. Failure/service rows remain frontend checks and are erased in generated Go. Context copies implement lexical provider binding. Ordinary sequencing stays on the current goroutine; managed fork creates a goroutine with an owning scope.

Generated programs import the reusable managed runtime bundled from `runtime/effra` into `dist/go/runtime`. They use exactly the tested runtime source, not a second handwritten scheduler prelude. Managed effect/finalizer panics become defects; arbitrary unmanaged goroutine panics and fatal process/runtime failures remain outside that guarantee. Native entry points forward interrupt/SIGTERM through context and wait for owned cleanup before reporting failure.

Native builds rely on the installed Go toolchain and its normal compiler/build cache. Generated source is deterministic and unchanged bytes are not rewritten. Effra does not yet cache its own parsing/checking or interface summaries. Automatic imports invoke go list on each check and have separately reported loading cost. Backend build/link time must be measured separately from the frontend microbenchmark.

Historical warm native build sample, before managed runtime integration: on the same Apple M4 Pro/Go 1.27.1 setup, five unchanged `ef build examples/main.ef` invocations took 40.44–42.26 ms, median 42.08 ms after one warm-up. This includes CLI startup, source checking/emission, and cached Go compilation. It is a tiny no-op fixture; no cold-build, private-edit, public-edit, or matched-Go ratio has been established.

### Imports and lifecycle update (2026-10-05)

On the same Apple M4 Pro / Go 1.27.1 setup, the 10k-line no-import fixture measured 3.65 ms/op, 14.06 MB and 16,184 allocations in one run. This is about 9% above the previous 3.30–3.35 ms sample; no matched repeated regression attribution was performed.

Four warm public CLI checks of `examples/main.ef` took 4.01–5.33 ms, with no import work. Four checks of `examples/imports.ef` took 50.66–51.81 ms; reported import loading took 45.67–47.08 ms. Four unchanged native builds of the imported example, after one warm-up, took 154.90–157.46 ms. These are distinct tiny fixtures, not matched Go/Effra comparisons. Go toolchain process/export loading dominates the imported check; persistent normalized import summaries remain a priority. Cold builds, edit invalidation regimes and linking attribution remain unmeasured.

Reproduce by timing `./bin/ef check examples/imports.ef` and `./bin/ef build examples/imports.ef` after warm-up; inspect the JSON `timings.importMicros` separately from wall time.

## Default tooling and tests

`lint`, `lint rules`, `query` and `graph` use the same checked model as symbol inspection. MCP adds `project.lint`, `lint.rules`, `code.typeAt`, `project.graph` and `project.tests`; all tools remain read-only. Queries use expression diagnostic anchors, not full source ranges. Graphs describe provider recipes, materialized values and declared provision edges; fallible acquisition and general dependent-layer sharing remain unsupported.

`ef test` discovers checked `test_` effects and supplies `Assert`. Each case owns a fresh scope on both targets, with captured suite output and structured failure reasons. Live host/time capabilities require `--live`; the real process watchdog reports unconfirmed cleanup on forced termination. See [tooling](tooling.md), [testing](testing.md) and [runnable testing example](../examples/testing.ef).
