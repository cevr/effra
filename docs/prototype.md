# Implemented prototype

This is a single-file compiler experiment, not the full design in design.md. The source is authoritative; unsupported syntax produces diagnostics rather than being passed through to JavaScript.

## Supported surface

- Immutable local bindings, string/bool/unit values, string concatenation, equality, and `if` expressions with two branches.
- Ordinary `fn` and lazy `effect fn`, with explicit parameter, result, failure, and service contracts.
- Payload-free nominal errors: `error NotFound`; `fail NotFound` inside effect bodies.
- Nominal services and self-contained implementations: `impl MemoryUsers for Users`.
- `run` to execute a deferred effect within an effect body.
- `.provide<Service>(Provider)` removes that service requirement.
- `.catch<Failure>(pureFallback)` removes exactly that failure. This prototype deliberately accepts a pure replacement value rather than a lambda or effectful handler.
- Built-in `Console.log(string) -> ()`, requiring Console, and its explicit Stdout provider.

Rows are normalized sets; declarations are upper bounds. Service calls use the service's declared contract even if one provider admits fewer failures. A library may retain requirements; the executable entry is an effect function named `main`, takes no parameters, and requires no remaining services. Its admitted typed failures are reported as runtime failures with nonzero exit status.

The last expression is a block's value. Use semicolons where adjacent expressions could parse as one call; newlines are whitespace. Strings use JSON escapes. Identifiers are ASCII; source offsets and columns are UTF-8 bytes. Empty blocks produce unit.

## Execution and inspection

The frontend is Go with no third-party Go dependencies. Checked IR lowers to pinned Effect 4.0.1, verified available in the npm registry on 2026-10-05. Effect.gen owns deferred bodies; Context.Service/provideService own providers; catchTag owns selective recovery. No second JavaScript effect scheduler exists.

`ef check` returns revisioned JSON including diagnostics, contracts, body rows, and parse/check timings. `ef inspect FILE SYMBOL` and `ef explain FILE SYMBOL` return the same semantic symbol and direct executed-call/failure contributions. Explanations are currently local contributions, not a transitive proof or provision history. JSON schema version 1 is experimental.

`ef build FILE` emits an importable module and consumer declarations in dist; `--entry` also runs main. `ef run FILE` emits an entry then starts Bun (Node fallback). Build emits JavaScript; it does not produce a standalone executable. Generated JS is trusted compiler output and should be regenerated after source edits.

## Limits

No Go backend, payload-bearing errors, structs/enums, integers, generics/open rows, higher-order effect signatures, Layers, retry, timeout, scopes, fibers, resource ownership, imports, source maps, persistent/package caching, LSP, content mapper, semantic edits, runtime inspection, or type checking of generated declarations yet. Effect itself has lifecycle facilities; Effra does not expose or validate them yet. Service/error identities are local to this single-file experiment, not qualified package identities.

Inspection revisions hash source bytes. Compiler/runtime/schema versions are separate metadata; this is not a complete build-cache key. Construction of an unused recipe may retain requirements in the recipe type without adding them to the enclosing executed body. The requirements row tracks managed service access, not a proof of complete purity or race freedom.

## Baseline

Measured 2026-10-05 on Apple M4 Pro, darwin/arm64, Go 1.27.1. `BenchmarkCompile10KLines` checks 2,000 five-line independent effect functions (114,890 bytes): three runs averaged 3.15–3.23 ms/op, approximately 14 MB allocated and 16,144 allocations per compile. This is an in-process parse/check measurement; it excludes CLI startup, JSON output, emission, dependency loading, and backend execution. It contains no imports or adverse row-polymorphism cases. No claimed speed budget has been established.

Reproduce: `go test ./internal/compiler -run '^$' -bench BenchmarkCompile10KLines -benchmem -count=3`.
