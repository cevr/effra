# Effra

An experimental language for servers: explicit, inspectable effect contracts, `.ef` source, and the `ef` command.

Project direction is recorded in [NORTH_STAR.md](NORTH_STAR.md), research comparisons in [PRIOR_ARTS.md](PRIOR_ARTS.md), and canonical terms in [GLOSSARY.md](GLOSSARY.md). The [architecture-loop ledger](plans/architecture-loop-2026-10-05.md) separates established direction, draft questions, and pending architecture work.

The runnable prototype is a small Go compiler that checks `.ef` source and produces native Go executables or JavaScript using Effect 4.0.1. It supports lazy effects, closed failure/service rows, nominal services, explicit provision, selective recovery, and canonical JSON inspection. It also serves that semantic model through a read-only MCP server.

## Try it

Requires Go 1.27+ and Bun. Run from this repository so generated modules resolve the pinned runtime.

```sh
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

## What is experimental

This is a single-file prototype with string/bool/unit values and payload-free errors. There is no ADT support, open-row inference, Layer API, scope/fiber syntax, package cache, source maps, or runtime inspection yet. The wider [design sketch](docs/design.md) is a proposal, not the compiler's supported feature list. See [implemented syntax and limits](docs/prototype.md) and [MCP setup](docs/mcp.md).

A synthetic 10,000-line fixture currently parses/checks in roughly 3 ms on an Apple M4 Pro. That is an in-process simple-program baseline, not a promise of end-to-end build performance.

## Wayfinder

The [Effra prototype map](docs/wayfinder/issues/map.md) is the canonical index of unresolved decisions. Its first prototype ticket remains open for user feedback; building the artifact does not settle the language design.

```sh
python3 scripts/wayfinder.py list
python3 scripts/wayfinder.py frontier
```

The local Markdown tracker has [documented claims and dependency conventions](docs/wayfinder/README.md). The initial work is committed locally; no remote or publication is configured.
