# Effra

An experimental language for servers, combining Go's native executable target with explicit effect contracts and owned lifetimes. Write `.ef` files and build them with `ef`.

Effra explores ideas from [Borgo](https://github.com/borgo-lang/borgo) and [Effect](https://github.com/Effect-TS/effect): deferred programs, explicit failures and dependencies, scopes that own children and resources, and host interop without repeating native function signatures. Types and guardrails are inspectable through the same compiler model used by the CLI and MCP.

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
./bin/ef build examples/main.ef            # standalone executable: dist/main
./dist/main
./bin/ef build examples/main.ef -o bin/demo
./bin/ef build examples/main.ef --target js # dist/main.mjs + dist/main.d.mts
./bin/ef mcp .                            # newline-delimited JSON-RPC on stdio
```

`bun run demo` compiles and runs the native Go executable. Use `--target js` with build/run/check to select JavaScript. `bun run gate` checks formatting, Go vet, compiler and runtime tests, tracker consistency, and the public CLI/MCP process.

## Example

```rust
error NotFound

service Users {
    effect fn get(id: string) -> string throws {NotFound}
}

effect fn greeting(id: string) -> string
    throws {NotFound}
    uses {Users}
{
    let name = run Users.get(id)
    "Hello, " + name
}
```

Calling `greeting("42")` constructs a deferred program. `run` executes it within another effect. Inspection reports success `string`, failure `{NotFound}`, and requirement `{Users}`. The [complete runnable example](examples/main.ef) implements and provides Users, recovers NotFound, and supplies Console explicitly.

Go builds lower checked source into typed Go closures and call `go build`; the executable needs no Effra, Bun, Node, or Effect installation to run. Generated source remains in `dist/go/<name>/main.go`.

The JavaScript library build exports functions, service keys, and providers for consumers. `--target js --entry` adds host execution; native builds and both run targets require an effect main with no parameters or remaining service requirements.

## Runnable examples

| Example | What it demonstrates | Target |
| --- | --- | --- |
| [main.ef](examples/main.ef) | Nominal services, explicit provision and typed recovery | Go / JS |
| [concurrency.ef](examples/concurrency.ef) | Child join/interrupt and deadline recovery | Go / JS |
| [imports.ef](examples/imports.ef) | Automatic native signatures, partial results and context forwarding | Go |
| [http.ef](examples/http.ef) | HTTP routes, SDK calls, file scopes and managed shutdown | Go |
| [lifecycle.ef](examples/lifecycle.ef) | Scoped files, cancellation and runtime snapshots | Go |
| [go-interop](examples/go-interop/main.go) | Calling the managed runtime from Go | Go |

```sh
./bin/ef run examples/concurrency.ef
./bin/ef run examples/concurrency.ef --target js
./bin/ef run examples/imports.ef
./bin/ef run examples/http.ef
# listening http://127.0.0.1:PORT
```

Use the printed server URL with `/health`, `/users/42`, `/users/slow`, `/users/missing`, or `/file`. Each request has an owning scope. Interrupt/SIGTERM stops admission, requests cancellation, and waits for handler cleanup. Cancellation is cooperative: a foreign call that ignores it can delay shutdown. See the [HTTP contract](docs/runtime.md#http-server).

## Go interop

```rust
import go strconv "strconv"

effect fn parse(text: string) -> bool throws {GoError} uses {Foreign} {
    run strconv.ParseBool(text).orFail()
}
```

The compiler loads callable shapes from Go export data. Imported calls are deferred and require the explicit `Foreign` capability, provided by `Host`. A native error initially remains data in `GoResult`, retaining the partial value; `.orFail()` explicitly adapts it into GoError. Optional binding metadata can forward a managed context and declare cancellation behavior. Those declarations are reviewed assertions, not guarantees inferred from a signature. See [interop boundaries](docs/interop.md).

## Agent inspection

`ef check`, `ef inspect` and `ef explain` expose checked contracts, source spans, used host signatures, behavior provenance and compiler timings as JSON. `ef mcp .` exposes read-only compiler tools over stdio. Revisions include imported Go declarations and behavior contracts so stale queries can be rejected. See [MCP setup and limits](docs/mcp.md).

## What is experimental

This is a single-file prototype with primitive values, managed File handles, and payload-free source errors. Lifecycle syntax works on Go and JS/Effect; Files/Runtime/Http and Go imports require Go. Imported package functions currently accept primitive shapes; named host types, methods, generics and arbitrary SDK objects remain unsupported. There are no ADTs, open rows, Layers, package cache, source maps, or out-of-process runtime inspector yet. See [implemented syntax and limits](docs/prototype.md), [runtime and interop contracts](docs/runtime.md), and [MCP setup](docs/mcp.md). The wider [design sketch](docs/design.md) remains a proposal.

Fast compilation is a design constraint, with separate frontend and import measurements. The current synthetic 10,000-line fixture checks in about 3.65 ms on an Apple M4 Pro; warm imported CLI checks take about 51 ms and cached imported executable builds about 156 ms. These are small, distinct fixtures, not a matched comparison with Go. Persistent import summaries and edit benchmarks remain work. See [measurement receipts](docs/prototype.md#baseline).

## Development and design

```sh
bun run gate
go test -race ./...
```

The gate checks formatting, vet, compiler/runtime tests, actual Go and JS programs, CLI/MCP inspection, and a live HTTP server. Shared lifecycle tests cover child-before-parent cleanup, unobserved child failures and timeout cleanup defects.

Project direction is recorded in [NORTH_STAR.md](NORTH_STAR.md), source comparisons in [PRIOR_ARTS.md](PRIOR_ARTS.md), and canonical terms in [GLOSSARY.md](GLOSSARY.md). The [architecture ledger](plans/architecture-loop-2026-10-05.md) records implementation evidence and unresolved work. Start with [implemented syntax](docs/prototype.md), [runtime contracts](docs/runtime.md), or the broader [design sketch](docs/design.md).

## Wayfinder

The [Effra prototype map](docs/wayfinder/issues/map.md) is the canonical index of unresolved decisions. Its first prototype ticket remains open for user feedback; building the artifact does not settle the language design.

```sh
python3 scripts/wayfinder.py list
python3 scripts/wayfinder.py frontier
```

The local Markdown tracker has [documented claims and dependency conventions](docs/wayfinder/README.md).
