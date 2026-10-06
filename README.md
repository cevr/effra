# Effra

An experimental language for servers, combining Go's native executable target with explicit effect contracts and owned lifetimes. Write `.ef` files and build them with `ef`.

Effra combines a small explicit language with deferred programs, explicit failures and dependencies, scopes that own children and resources, and host interop without repeating native function signatures. Types and guardrails are inspectable through the same compiler model used by the CLI and MCP.

The direction is **Go-like directness, algebraic data types, and explicit Effect-style contracts**. The [showcase guide](docs/showcases.md) connects these ideas to real application patterns and labels which features remain proposals.

The [contender roadmap](docs/contender-roadmap.md) identifies what real ports still need: application data types, native SDK objects, dependent providers, codecs/streams, incremental tooling and measurable adoption tests.

**Status: runnable prototype.** Syntax, APIs and inspection schemas are experimental. Go is the default target; JavaScript emits pinned Effect. This is a server language experiment, with no kernel or hard real-time execution profile.

The runnable prototype is a small Go compiler that checks `.ef` source and produces native Go executables or JavaScript using Effect 4.0.1. It supports lazy effects, closed failure/service rows, nominal services, explicit provision, selective recovery, and canonical JSON inspection through CLI and read-only MCP. Both targets support owning scopes, child fibers, cooperative cancellation, and timeouts that wait for cleanup. Go additionally supports automatic primitive host imports, managed files, runtime snapshots, and an HTTP server.

## Try it

Requires Go 1.27+ and Bun; the verification gate also requires Python 3. Run from the repository root.

```sh
git clone https://github.com/cevr/effra.git
cd effra
bun install --frozen-lockfile
go build -o bin/ef ./cmd/ef
./bin/ef run examples/main.ef
# Hello, Ada
# Unknown user

./bin/ef inspect examples/main.ef greeting
./bin/ef explain examples/main.ef greeting
./bin/ef check examples/missing-service.ef  # expected failure: EF108
./bin/ef fmt --check examples/main.ef
./bin/ef build examples/main.ef            # standalone executable: dist/main
./dist/main
./bin/ef build examples/main.ef -o bin/demo
./bin/ef build examples/main.ef --target js # dist/main.mjs + dist/main.d.mts
./bin/ef mcp .                            # newline-delimited JSON-RPC on stdio
```

`bun run demo` compiles and runs the native Go executable. Use `--target js` with build/run/check to select JavaScript. `bun run gate` checks Go formatting, Go vet, compiler and runtime tests, tracker consistency, and the public CLI/MCP process.

## Example

```rust
error NotFound

service Users {
    effect fn get(id: string) -> string raises {NotFound}
}

effect fn greeting(id: string) -> string
    raises {NotFound}
    uses {Users}
{
    let name = run Users.get(id)
    "Hello, " + name
}
```

Calling `greeting("42")` constructs a deferred program. `run` executes it within another effect. Inspection reports success `string`, failure `{NotFound}`, and requirement `{Users}`. The [complete runnable example](examples/main.ef) implements and provides Users, recovers NotFound, and supplies Console explicitly.

Go builds lower checked source into typed Go closures and call `go build`; the executable needs no Effra, Bun, Node, or Effect installation to run. Generated native modules are complete immutable snapshots under `dist/go/apps/<application-id>/generations/`; their commit records preserve source ownership and reuse.

The JavaScript library build exports functions, service keys, and providers for consumers. `--target js --entry` adds host execution; native builds and both run targets require an effect main with no parameters or remaining service requirements.

## Runnable examples

| Example | What it demonstrates | Target |
| --- | --- | --- |
| [main.ef](examples/main.ef) | Nominal services, explicit provision and typed recovery | Go / JS |
| [workflow.ef](examples/workflow.ef) | Authorization, lookup and delivery with three service contracts | Go / JS |
| [latest-task.ef](examples/latest-task.ef) | Replace an owned child after interruption and cleanup finish | Go / JS |
| [concurrency.ef](examples/concurrency.ef) | Child join/interrupt and deadline recovery | Go / JS |
| [causal.ef](examples/causal.ef) | Managed virtual time, shared latches and causal cleanup tests | Go / JS |
| [imports.ef](examples/imports.ef) | Automatic native signatures, partial results and context forwarding | Go |
| [http.ef](examples/http.ef) | HTTP routes, SDK calls, file scopes and managed shutdown | Go |
| [lifecycle.ef](examples/lifecycle.ef) | Scoped files, cancellation and runtime snapshots | Go |
| [ownership.ef](examples/ownership.ef) | Borrowed outer handles and checked scoped file ownership | Go |
| [go-interop](examples/go-interop/main.go) | Calling the managed runtime from Go | Go |

```sh
./bin/ef run examples/concurrency.ef
./bin/ef run examples/concurrency.ef --target js
./bin/ef run examples/imports.ef
./bin/ef run examples/http.ef
# listening http://127.0.0.1:PORT
```

Use the printed server URL with `/health`, `/users/42`, `/users/slow`, `/users/missing`, or `/file`. Each request has an owning scope. Interrupt/SIGTERM stops admission, requests cancellation, and waits for handler cleanup. Cancellation is cooperative: a foreign call that ignores it can delay shutdown. See the [HTTP contract](docs/runtime.md#http-server).

## Records, closed data, and pattern matching

Records and closed enums carry typed payloads. Constructors check field names and values, and `match` must cover each declared variant exactly once:

```rust
enum RunState {
    Idle
    Running { runId: string }
    Waiting { runId: string, requestId: string }
}

fn status(state: RunState) -> string {
    match state {
        RunState.Idle => "idle"
        RunState.Running { runId } => "running " + runId
        RunState.Waiting { runId, requestId } => "waiting " + requestId
    }
}
```

Each alternative owns its payload. Adding a variant should make incomplete matches fail to check. Decoding external data still needs an explicit codec; a static enum is not runtime validation. The [showcases](docs/showcases.md) cover ADTs, payload errors, decoded events, owned streams, durable commands and infrastructure outputs, with a [pattern review](docs/research/effect-native-showcases.md) of the policies each example must preserve.

## Go interop

```rust
import go strconv "strconv"

effect fn parse(text: string) -> bool raises {GoError} uses {Foreign} {
    run strconv.ParseBool(text).orFail()
}
```

The compiler loads callable shapes from Go export data. Imported calls are deferred and require the explicit `Foreign` capability, provided by `Host`. A native error initially remains data in `GoResult`, retaining the partial value; `.orFail()` explicitly adapts it into GoError. Optional binding metadata can forward a managed context and declare cancellation behavior. Those declarations are reviewed assertions, not guarantees inferred from a signature. See [interop boundaries](docs/interop.md).

## Agent inspection

`ef check`, `ef inspect` and `ef explain` expose checked contracts, source spans, used host signatures, behavior provenance and compiler timings as JSON. `ef fmt` exposes the canonical syntax-only formatter through stdin, check, write and JSON report modes; it does not typecheck or load packages. `ef mcp .` exposes read-only compiler tools over stdio, including `code.format` for one explicit buffer or guarded disk snapshot. Revisions include imported Go declarations and behavior contracts so stale semantic queries can be rejected; formatter results use a separate source-byte digest and formatter identity. See [MCP setup and limits](docs/mcp.md).

`ef diagnostics FILE --json` and MCP `project.diagnostics` share compiler errors and lint advice, with explicit severities, UTF-8 byte spans and UTF-16 editor ranges. Reports distinguish checked source, unavailable advice and policy failure. This is the shared diagnostic model; the standalone language server and full type-graph queries are still being built.

## What is experimental

This is a single-file prototype with nominal records, closed enums, typed source failures, managed File handles, and explicit effect rows. Lifecycle syntax works on Go and JS/Effect; Files/Runtime/Http and Go imports require Go. Imported package functions currently accept primitive shapes; named host types, methods, generics and arbitrary SDK objects remain unsupported. Codecs, open rows, Layers, package cache, source maps, and out-of-process runtime inspection remain future work. See [implemented syntax and limits](docs/prototype.md), [runtime and interop contracts](docs/runtime.md), and [MCP setup](docs/mcp.md). The wider [design sketch](docs/design.md) remains a proposal.

Fast compilation is a design constraint, with separate frontend and import measurements. The current synthetic 10,000-line fixture checks in about 3.65 ms on an Apple M4 Pro; warm imported CLI checks take about 51 ms and cached imported executable builds about 156 ms. These are small, distinct fixtures, not a matched comparison with Go. Persistent import summaries and edit benchmarks remain work. See [measurement receipts](docs/prototype.md#baseline).

## Development and design

```sh
bun run gate
go test -race ./...
```

The gate checks Go formatting, vet, compiler/runtime tests, actual Go and JS programs, CLI/MCP inspection and diagnostic parity, formatter CLI/MCP process parity, and a live HTTP server. Shared lifecycle tests cover child-before-parent cleanup, unobserved child failures and timeout cleanup defects.

[Upstream behavioral conformance](docs/conformance.md) maps selected pinned Effect cases to existing Go/JS acceptance, with explicit differences, pending and unsupported rows. The imported 746 reference files remain reference-only.

Project direction is recorded in [NORTH_STAR.md](NORTH_STAR.md), source comparisons in [PRIOR_ARTS.md](PRIOR_ARTS.md), and canonical terms in [GLOSSARY.md](GLOSSARY.md). The [architecture ledger](plans/architecture-loop-2026-10-05.md) records implementation evidence and unresolved work. Start with [implemented syntax](docs/prototype.md), [runtime contracts](docs/runtime.md), or the broader [design sketch](docs/design.md).

## Wayfinder

The [Effra prototype map](docs/wayfinder/issues/map.md) is the canonical index of unresolved decisions. Its first prototype ticket remains open for user feedback; building the artifact does not settle the language design.

```sh
python3 scripts/wayfinder.py list
python3 scripts/wayfinder.py frontier
```

The local Markdown tracker has [documented claims and dependency conventions](docs/wayfinder/README.md).

## Default development tools

The CLI and read-only MCP share the checked compiler model:

```sh
./bin/ef lint examples/workflow.ef --strict
./bin/ef lint rules
./bin/ef diagnostics examples/workflow.ef --strict --json
./bin/ef graph examples/workflow.ef
./bin/ef query examples/latest-task.ef 64
./bin/ef test examples/testing.ef
./bin/ef test examples/testing.ef --target js
```

[Tooling](docs/tooling.md) describes lint severity, declaration metadata, dependency edges and bounded MCP results. [Testing](docs/testing.md) covers assertions, explicit fixture providers, owned test scopes and the real-time watchdog. Codec derivation, open rows and generic containers remain proposed.
